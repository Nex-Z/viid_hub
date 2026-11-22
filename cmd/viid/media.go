package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func newID() string { return fmt.Sprintf("%x", rand.Text()) }
func putImage(ctx context.Context, tx pgx.Tx, id, name, record, device, kind string, data []byte, at time.Time) error {
	if len(data) == 0 || len(data) > 8<<20 {
		return bad("单张图片须在 8 MB 以内")
	}
	cfg, format, e := image.DecodeConfig(bytes.NewReader(data))
	if e != nil || (format != "jpeg" && format != "png") {
		return bad("只支持有效的 JPEG 或 PNG 图片")
	}
	if id == "" || len(id) > 200 {
		return bad("图片编号无效")
	}
	var shot any
	if !at.IsZero() {
		shot = at
	}
	_, e = tx.Exec(ctx, "INSERT INTO images(id,filename,mime,content,record_id,device_id,kind,width,height,shot_at) VALUES($1,$2,$3,$4,nullif($5,''),nullif($6,''),$7,$8,$9,$10)", id, filepath.Base(name), "image/"+format, data, record, device, kind, cfg.Width, cfg.Height, shot)
	return e
}
func (a *app) file(w http.ResponseWriter, r *http.Request) error {
	var data []byte
	var name, typ string
	e := a.db.QueryRow(r.Context(), "SELECT content,filename,mime FROM images WHERE id=$1", r.PathValue("id")).Scan(&data, &name, &typ)
	if errors.Is(e, pgx.ErrNoRows) {
		var owner string
		id := r.PathValue("id")
		if e = a.db.QueryRow(r.Context(), "SELECT device FROM viid_image_info WHERE id=$1", id).Scan(&owner); e != nil {
			return e
		}
		data, e = a.imageBytes(r.Context(), id, owner)
		if e == nil {
			typ = http.DetectContentType(data)
			name = id
		}
	}
	if e != nil {
		return e
	}
	w.Header().Set("Content-Type", typ)
	w.Header().Set("Cache-Control", "private, no-store")
	if r.URL.Query().Get("download") == "1" {
		if e = a.audit(r, a.db, "download", "images", r.PathValue("id")); e != nil {
			return e
		}
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	}
	_, e = w.Write(data)
	return e
}
func (a *app) upload(w http.ResponseWriter, r *http.Request) error {
	if e := r.ParseMultipartForm(9 << 20); e != nil {
		return bad("上传内容无效或过大")
	}
	defer r.MultipartForm.RemoveAll()
	f, h, e := r.FormFile("file")
	if e != nil {
		return bad("请选择图片")
	}
	defer f.Close()
	data, e := io.ReadAll(io.LimitReader(f, (8<<20)+1))
	if e != nil {
		return e
	}
	ctx := r.Context()
	tx, e := a.db.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	id := newID()
	record := strings.TrimSpace(r.FormValue("record_id"))
	device := r.FormValue("device_id")
	kind := r.FormValue("kind")
	if kind == "" {
		kind = "scene"
	}
	if record != "" {
		if e = tx.QueryRow(ctx, "SELECT device_id,kind FROM records WHERE id=$1 FOR KEY SHARE", record).Scan(&device, &kind); e != nil {
			return e
		}
	}
	if e = putImage(ctx, tx, id, h.Filename, record, device, kind, data, time.Time{}); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	return write(w, M{"id": id})
}
func (a *app) linkImage(w http.ResponseWriter, r *http.Request) error {
	var p struct{ Record string }
	if e := decode(r, &p); e != nil {
		return e
	}
	ctx := r.Context()
	tx, e := a.db.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	id := r.PathValue("id")
	var device, kind string
	e = tx.QueryRow(ctx, "SELECT device_id,kind FROM records WHERE id=$1", p.Record).Scan(&device, &kind)
	if e != nil {
		return e
	}
	var conflict bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT FROM viid_image_info WHERE id=$1 AND device<>$2)", id, device).Scan(&conflict); e != nil {
		return e
	}
	if conflict {
		return problem{409, "协议图片仅可关联到同一来源设备"}
	}
	tag, e := tx.Exec(ctx, "UPDATE images SET record_id=$2,device_id=$3,kind=$4 WHERE id=$1", id, p.Record, device, kind)
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	return write(w, M{"ok": true})
}
