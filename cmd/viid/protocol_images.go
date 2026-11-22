package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Reserve the image identity even when the image arrives after the capture.
func linkProtocolImage(ctx context.Context, tx pgx.Tx, id, record, device, kind string) error {
	tag, err := tx.Exec(ctx, `INSERT INTO viid_image_info(id,device,record_id) VALUES($1,$2,$3)
 ON CONFLICT(id) DO UPDATE SET record_id=coalesce(viid_image_info.record_id,excluded.record_id) WHERE viid_image_info.device=excluded.device`, id, device, record)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return problem{409, "图片来源或关联记录冲突"}
	}
	var collision bool
	err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM images WHERE id=$1 AND device_id IS DISTINCT FROM $2)", id, device).Scan(&collision)
	if err != nil {
		return err
	}
	if collision {
		return problem{409, "图片编号已属于其他来源"}
	}
	if _, err = tx.Exec(ctx, "INSERT INTO viid_image_links(image_id,record_id) VALUES($1,$2) ON CONFLICT DO NOTHING", id, record); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "UPDATE images SET record_id=$2,kind=$3 WHERE id=$1 AND device_id=$4 AND record_id IS NULL", id, record, kind, device)
	return err
}
func saveProtocolData(ctx context.Context, tx pgx.Tx, id, device string, data []byte) error {
	var same bool
	err := tx.QueryRow(ctx, "SELECT device_id=$2 AND content=$3 FROM images WHERE id=$1", id, device, data).Scan(&same)
	if err == nil {
		if !same {
			return problem{409, "图片内容或来源冲突"}
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var record, kind string
	var at *time.Time
	err = tx.QueryRow(ctx, `SELECT coalesce(r.id,''),coalesce(r.kind,'scene'),r.captured FROM viid_image_info m
 LEFT JOIN records r ON r.id=m.record_id WHERE m.id=$1 AND m.device=$2`, id, device).Scan(&record, &kind, &at)
	if err != nil {
		return err
	}
	var shot time.Time
	if at != nil {
		shot = *at
	}
	return putImage(ctx, tx, id, id, record, device, kind, data, shot)
}
func decodeImageData(body io.Reader) ([]byte, error) {
	b, e := io.ReadAll(io.LimitReader(base64.NewDecoder(base64.StdEncoding, body), (8<<20)+1))
	if e != nil || len(b) == 0 || len(b) > 8<<20 {
		return nil, bad("图像数据应为不超过 8 MiB 的 Base64 图像")
	}
	return b, nil
}
func (a *app) protocolImages(w http.ResponseWriter, r *http.Request, principal, path string) error {
	if err := a.online(r, principal); err != nil {
		return err
	}
	id, part, _ := strings.Cut(path, "/")
	ctx := r.Context()
	if part != "" && part != "Info" && part != "Data" {
		return pgx.ErrNoRows
	}
	if r.Method == "GET" {
		return a.readProtocolImage(w, r, principal, id, part)
	}
	if r.Method != "POST" || (id != "" && part != "Data") {
		return problem{405, "图像接口不支持该方法"}
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if part == "Data" {
		if !imageID(id) {
			return bad("独立图像编号无效")
		}
		if err = a.authorizeSource(ctx, tx, principal, id[:20]); err == nil {
			if _, err = tx.Exec(ctx, "INSERT INTO viid_image_info(id,device) VALUES($1,$2) ON CONFLICT DO NOTHING", id, id[:20]); err != nil {
				return err
			}
		}
		var device string
		if err = tx.QueryRow(ctx, "SELECT device FROM viid_image_info WHERE id=$1 FOR UPDATE", id).Scan(&device); err != nil {
			return err
		}
		if err = a.authorizeSource(ctx, tx, principal, device); err != nil {
			return err
		}
		data, e := decodeImageData(r.Body)
		if e != nil {
			return e
		}
		if err = saveProtocolData(ctx, tx, id, device, data); err != nil {
			return err
		}
		if err = tx.Commit(ctx); err != nil {
			return err
		}
		return write(w, M{"ResponseStatusObject": status(id, r.URL.Path)})
	}
	var input struct{ ImageListObject struct{ ImageObject []M } }
	if err = decode(r, &input); err != nil {
		return err
	}
	items := input.ImageListObject.ImageObject
	if len(items) == 0 || len(items) > 500 {
		return bad("图像数量须为 1–500")
	}
	replies := []M{}
	for _, obj := range items {
		info, ok := obj["ImageInfoObject"].(map[string]any)
		if !ok {
			return bad("缺少 ImageInfoObject")
		}
		if len(obj) != 1 {
			return bad("当前独立图像上传仅支持图像信息，关联语义对象请通过对象接口提交")
		}
		pid, device := field(info, "ImageID"), field(info, "DeviceID")
		if !imageID(pid) {
			return bad("独立图像编号无效")
		}
		if err = a.authorizeSource(ctx, tx, principal, device); err != nil {
			return err
		}
		encoded := field(info, "Data")
		delete(info, "Data")
		tag, e := tx.Exec(ctx, `INSERT INTO viid_image_info(id,device,info) SELECT $1,$2,$3 WHERE NOT EXISTS(
   SELECT 1 FROM images WHERE id=$1 AND device_id IS DISTINCT FROM $2)
   ON CONFLICT(id) DO UPDATE SET info=excluded.info WHERE viid_image_info.device=excluded.device AND
   (viid_image_info.info IS NULL OR viid_image_info.info=excluded.info)`, pid, device, info)
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			return problem{409, "图像信息或来源冲突"}
		}
		if encoded != "" {
			data, e := decodeImageData(strings.NewReader(encoded))
			if e != nil {
				return e
			}
			if e = saveProtocolData(ctx, tx, pid, device, data); e != nil {
				return e
			}
		}
		replies = append(replies, status(pid, r.URL.Path))
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	return write(w, M{"ResponseStatusListObject": M{"ResponseStatusObject": replies}})
}
func (a *app) readProtocolImage(w http.ResponseWriter, r *http.Request, principal, id, part string) error {
	filters, limit, offset, e := resourceFilters(r.URL.Query(), "ImageID", "DeviceID")
	if e != nil {
		return e
	}
	if id != "" && len(r.URL.Query()) > 0 {
		return bad("单图接口不接受查询条件")
	}
	var items []M
	var total int
	var device string
	e = a.db.QueryRow(r.Context(), `WITH photos AS (
 SELECT coalesce(m.id,i.id) id,coalesce(m.device,i.device_id) device,
 jsonb_strip_nulls(jsonb_build_object('ImageID',coalesce(m.id,i.id),'DeviceID',coalesce(m.device,i.device_id),
 'Width',i.width,'Height',i.height,'FileFormat',CASE i.mime WHEN 'image/jpeg' THEN 'Jpeg' WHEN 'image/png' THEN 'Png' END))
 ||coalesce(m.info,'{}'::jsonb) info FROM viid_image_info m FULL JOIN images i USING(id))
 , matches AS (SELECT id,device,info FROM photos WHERE device IS NOT NULL AND ($1 OR device=$2 OR
 EXISTS(SELECT FROM viid_catalog c WHERE c.resource='APEs' AND c.id=device AND c.owner=$2 AND $7))
 AND ($3='' OR id=$3) AND NOT EXISTS(SELECT FROM jsonb_each_text($4) f WHERE info->>f.key IS DISTINCT FROM f.value)
 ) SELECT (SELECT count(*) FROM matches), coalesce(jsonb_agg(jsonb_build_object('ImageInfoObject',p.info) ORDER BY p.id),'[]'::jsonb),coalesce(min(p.device),'') FROM (SELECT * FROM matches ORDER BY id LIMIT $5 OFFSET $6) p`, a.peerIdentity(principal, "notify"), principal, id, filters, limit, offset, a.lowerIdentity(principal)).Scan(&total, &items, &device)
	if e != nil {
		return e
	}
	if id == "" {
		return write(w, M{"ImageListObject": M{"ImageObject": items}, "TotalNum": total})
	}
	if len(items) == 0 {
		return pgx.ErrNoRows
	}
	switch part {
	case "Info":
		return write(w, items[0])
	case "":
		return write(w, M{"ImageObject": items[0]})
	}
	data, e := a.imageBytes(r.Context(), id, device)
	if e != nil {
		return e
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "private, no-store")
	enc := base64.NewEncoder(base64.StdEncoding, w)
	if _, e = enc.Write(data); e != nil {
		return e
	}
	return enc.Close()
}

// Fetch a referenced image only from the configured lower's standard resource.
func (a *app) imageBytes(ctx context.Context, id, device string) ([]byte, error) {
	var b []byte
	err := a.db.QueryRow(ctx, "SELECT content FROM images WHERE id=$1 AND device_id=$2", id, device).Scan(&b)
	if !errors.Is(err, pgx.ErrNoRows) || !imageID(id) {
		return b, err
	}
	var owner string
	if err = a.db.QueryRow(ctx, "SELECT owner FROM viid_catalog WHERE resource='APEs' AND id=$1 AND current", device).Scan(&owner); err != nil {
		return nil, err
	}
	for _, p := range a.peers {
		if p.Mode != "lower" || p.Subscriber != owner {
			continue
		}
		sender := a.lowerClients[p.Subscriber]
		if err = sender.maintain(ctx); err != nil {
			return nil, problem{502, err.Error()}
		}
		encoded, err := sender.request(ctx, "GET", "/VIID/Images/"+id+"/Data", nil, "")
		if err != nil {
			return nil, problem{502, err.Error()}
		}
		b, err = decodeImageData(bytes.NewReader(encoded))
		if err != nil {
			return nil, err
		}
		tx, err := a.db.Begin(ctx)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback(ctx)
		var found string
		if err = tx.QueryRow(ctx, "SELECT device FROM viid_image_info WHERE id=$1 AND device=$2 FOR UPDATE", id, device).Scan(&found); err != nil {
			return nil, err
		}
		if err = saveProtocolData(ctx, tx, id, device, b); err != nil {
			return nil, err
		}
		return b, tx.Commit(ctx)
	}
	return nil, pgx.ErrNoRows
}
