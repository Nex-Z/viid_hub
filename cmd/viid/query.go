package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func jsonRows(ctx context.Context, db queryer, q string, args ...any) ([]json.RawMessage, error) {
	rows, e := db.Query(ctx, "SELECT to_jsonb(t) FROM ("+q+") t", args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var b json.RawMessage
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
func parseTime(s string) (time.Time, error) {
	for _, f := range []string{time.RFC3339Nano, "20060102150405", "2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"} {
		if t, e := time.ParseInLocation(f, s, zone); e == nil {
			return t, nil
		}
	}
	return time.Time{}, bad("日期格式无效")
}
func number(s string, def, max int) int {
	n, e := strconv.Atoi(s)
	if e != nil || n < 1 {
		return def
	}
	return min(n, max)
}

var managementViews = map[string]string{"devices": "device_list", "records": "management_records", "images": "image_list", "audit": "operation_audit"}

func (a *app) page(ctx context.Context, resource string, v url.Values, view string) (M, error) {
	if view == "" {
		return nil, problem{404, "资源不存在"}
	}
	where := " WHERE true"
	args := []any{}
	add := func(expr string, val any) {
		args = append(args, val)
		where += " AND " + strings.ReplaceAll(expr, "?", fmt.Sprintf("$%d", len(args)))
	}
	order := "captured DESC,id"
	if v.Get("sort") == "asc" {
		order = "captured,id"
	}
	timeField := "captured"
	if resource == "images" {
		timeField = "ingested_at"
		if v.Get("timeField") == "shot" {
			timeField = "shot_at"
		}
		order = timeField + " DESC NULLS LAST,id"
		if v.Get("sort") == "asc" {
			order = timeField + " ASC NULLS LAST,id"
		}
	}
	if resource == "deliveries" {
		order = "id DESC"
	} else if resource == "audit" {
		order = "seq DESC"
	} else if resource == "devices" {
		order = "id"
		if q := v.Get("q"); q != "" {
			add("(name ILIKE ? OR id ILIKE ? OR point ILIKE ?)", "%"+q+"%")
		}
		if s := v.Get("status"); s != "" {
			if s == "alerts" {
				where += " AND status IN ('超时','离线')"
				order = "heartbeat DESC NULLS LAST,id"
			} else {
				add("status=?", s)
			}
		}
		if s := v.Get("id"); s != "" {
			add("id ILIKE ?", "%"+s+"%")
		}
	} else {
		if resource == "records" {
			if origin := v.Get("origin"); origin != "" {
				add("device_id IN (SELECT id FROM viid_catalog WHERE resource='APEs' AND owner=?)", origin)
			}
			if id := v.Get("exactID"); id != "" {
				add("id=?", id)
			}
			for _, key := range []string{"Name", "PlateNo"} {
				if s := v.Get(key); s != "" {
					add("raw->>'"+key+"'=?", s)
				}
			}
		}
		if s := v.Get("kind"); s != "" {
			add("kind=?", s)
		}
		if s := v.Get("device"); s != "" {
			add("device_id=?", s)
		}
		if q := v.Get("q"); q != "" {
			expr := "(title ILIKE ? OR id ILIKE ? OR device ILIKE ?)"
			if resource == "records" {
				expr = "(title ILIKE ? OR id ILIKE ? OR device ILIKE ? OR raw->>'IDNumber' ILIKE ?)"
			} else {
				expr = "(title ILIKE ? OR id ILIKE ? OR related ILIKE ? OR record_id ILIKE ?)"
			}
			add(expr, "%"+q+"%")
		}
		for _, k := range []string{"start", "end"} {
			if s := v.Get(k); s != "" {
				t, e := parseTime(s)
				if e != nil {
					return nil, e
				}
				if k == "start" {
					add(timeField+">=?", t)
				} else {
					if len(s) == 19 && (s[10] == 'T' || s[10] == ' ') {
						add(timeField+"<?", t.Add(time.Second))
					} else {
						add(timeField+"<=?", t)
					}
				}
			}
		}
		if resource == "images" {
			if v.Get("linked") == "yes" {
				where += " AND record_id IS NOT NULL"
			}
			if v.Get("linked") == "no" {
				where += " AND record_id IS NULL"
			}
			if s := v.Get("record"); s != "" {
				add("(record_id=? OR id IN (SELECT image_id FROM viid_image_links WHERE record_id=?))", s)
			}
		} else {
			for k, field := range map[string]string{"color": "VehicleColor", "type": "VehicleClass", "direction": "Direction", "gender": "GenderCode"} {
				if s := v.Get(k); s != "" {
					add("raw->>'"+field+"'=?", s)
				}
			}
		}
	}
	size := number(v.Get("size"), 20, 200)
	page := number(v.Get("page"), 1, 10000000)
	var total int
	if e := a.db.QueryRow(ctx, "SELECT count(*) FROM "+view+where, args...).Scan(&total); e != nil {
		return nil, e
	}
	page = min(page, max(1, (total+size-1)/size))
	offset := (page - 1) * size
	if resource == "records" && v.Has("startRecord") {
		offset = number(v.Get("startRecord"), 1, 10000000) - 1
	}
	rows, e := jsonRows(ctx, a.db, "SELECT * FROM "+view+where+" ORDER BY "+order+fmt.Sprintf(" LIMIT %d OFFSET %d", size, offset), args...)
	return M{"items": rows, "total": total, "page": page, "size": size}, e
}
func (a *app) list(w http.ResponseWriter, r *http.Request) error {
	out, e := a.page(r.Context(), r.PathValue("resource"), r.URL.Query(), managementViews[r.PathValue("resource")])
	if e != nil {
		return e
	}
	return write(w, out)
}
func (a *app) detail(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := r.PathValue("id")
	resource := r.PathValue("resource")
	view := map[string]string{"devices": "device_list", "records": "management_records", "images": "image_list"}[resource]
	if view == "" {
		return problem{404, "资源不存在"}
	}
	rows, e := jsonRows(ctx, a.db, "SELECT * FROM "+view+" WHERE id=$1", id)
	if e != nil {
		return e
	}
	if len(rows) == 0 {
		return pgx.ErrNoRows
	}
	out := M{"item": rows[0]}
	if resource == "records" {
		out["canReveal"] = a.allowIdentity
		out["images"], e = jsonRows(ctx, a.db, "SELECT * FROM image_list WHERE record_id=$1 OR id IN (SELECT image_id FROM viid_image_links WHERE record_id=$1) ORDER BY captured,id", id)
		if e == nil {
			pending, err := jsonRows(ctx, a.db, "SELECT image_id AS id,image_id AS image FROM viid_image_links l WHERE record_id=$1 AND NOT EXISTS(SELECT FROM images i WHERE i.id=l.image_id) ORDER BY image_id", id)
			if err != nil {
				return err
			}
			out["images"] = append(out["images"].([]json.RawMessage), pending...)
		}
	}
	if resource == "devices" {
		out["events"], e = jsonRows(ctx, a.db, "SELECT event,at FROM device_events WHERE device_id=$1 ORDER BY at DESC,seq DESC LIMIT 30", id)
	}
	if e != nil {
		return e
	}
	if e = a.audit(r, a.db, "view", resource, id); e != nil {
		return e
	}
	return write(w, out)
}
func (a *app) overview(w http.ResponseWriter, r *http.Request) error {
	hours := number(r.URL.Query().Get("hours"), 24, 720)
	ctx := r.Context()
	out := M{}
	queries := []struct {
		name, sql string
		args      []any
	}{
		{"devices", "SELECT status,count(*) AS n FROM device_list GROUP BY status", nil},
		{"totals", "SELECT kind,count(*) AS n,count(*) FILTER(WHERE captured>=now()-make_interval(hours=>$1)) AS recent,count(*) FILTER(WHERE captured<now()-make_interval(hours=>$1) AND captured>=now()-make_interval(hours=>2*$1)) AS previous FROM records GROUP BY kind", []any{hours}},
		{"storage", "SELECT count(*) AS n,coalesce(sum(octet_length(content)),0) AS bytes,count(*) FILTER(WHERE ingested_at>=now()-make_interval(hours=>$1)) AS recent,count(*) FILTER(WHERE ingested_at<now()-make_interval(hours=>$1) AND ingested_at>=now()-make_interval(hours=>2*$1)) AS previous FROM images", []any{hours}},
		{"trend", "SELECT floor(extract(epoch FROM (captured-(now()-make_interval(hours=>$1))))*24/($1*3600))::int AS bucket,kind,count(*) AS n FROM records WHERE captured BETWEEN now()-make_interval(hours=>$1) AND now() GROUP BY 1,2 ORDER BY 1", []any{hours}},

		{"recent", "SELECT * FROM management_records WHERE captured>=now()-make_interval(hours=>$1) ORDER BY captured DESC,id LIMIT 12", []any{hours}},
		{"alerts", "SELECT id,name,status,heartbeat FROM device_list WHERE status IN ('超时','离线') ORDER BY heartbeat DESC NULLS LAST LIMIT 8", nil},
	}
	for _, q := range queries {
		rows, e := jsonRows(ctx, a.db, q.sql, q.args...)
		if e != nil {
			return e
		}
		out[q.name] = rows
	}
	out["at"] = time.Now()
	return write(w, out)
}
func (a *app) saveDevice(w http.ResponseWriter, r *http.Request) error {
	var d struct {
		ID, Name, Kind, IP, Point string
		Enabled                   *bool
	}
	if e := decode(r, &d); e != nil {
		return e
	}
	d.Name = strings.TrimSpace(d.Name)
	if d.Name == "" {
		return bad("请输入设备名称")
	}
	id := r.PathValue("id")
	ctx := r.Context()
	if id == "" {
		id = strings.TrimSpace(d.ID)
		if id == "" || len(id) > 128 {
			return bad("请输入有效设备编号")
		}
		_, e := a.db.Exec(ctx, "INSERT INTO devices(id,name,kind,ip,point,enabled) VALUES($1,$2,$3,$4,$5,coalesce($6,true))", id, d.Name, d.Kind, d.IP, d.Point, d.Enabled)
		if e != nil {
			return e
		}
	} else {
		tag, e := a.db.Exec(ctx, "UPDATE devices SET name=$2,kind=$3,ip=$4,point=$5,enabled=coalesce($6,enabled),registered=CASE WHEN $6=false THEN false ELSE registered END WHERE id=$1", id, d.Name, d.Kind, d.IP, d.Point, d.Enabled)
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
	}
	return write(w, M{"id": id})
}
func (a *app) remove(w http.ResponseWriter, r *http.Request) error {
	table := map[string]string{"records": "records", "images": "images", "devices": "devices"}[r.PathValue("resource")]
	if table == "" {
		return bad("资源无效")
	}
	ctx := r.Context()
	tx, e := a.db.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if table == "images" {
		if _, e = tx.Exec(ctx, "DELETE FROM viid_image_info WHERE id=$1", r.PathValue("id")); e != nil {
			return e
		}
	}
	tag, e := tx.Exec(ctx, "DELETE FROM "+table+" WHERE id=$1", r.PathValue("id"))
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	if e = a.audit(r, tx, "delete", table, r.PathValue("id")); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	return write(w, M{"ok": true})
}
