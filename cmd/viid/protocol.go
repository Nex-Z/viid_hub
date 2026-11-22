package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type objectType struct{ kind, object, id string }

var types = map[string]objectType{"Persons": {"person", "Person", "PersonID"}, "Faces": {"face", "Face", "FaceID"}, "MotorVehicles": {"motor", "MotorVehicle", "MotorVehicleID"}, "NonMotorVehicles": {"nonmotor", "NonMotorVehicle", "NonMotorVehicleID"}}

func field(m M, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			if s := fmt.Sprint(v); s != "" {
				return s
			}
		}
	}
	return ""
}
func stamp(t time.Time) string { return t.In(zone).Format("20060102150405") }
func status(id, path string) M {
	return M{"Id": id, "LocalTime": stamp(time.Now()), "RequestURL": path, "StatusCode": 0, "StatusString": "OK"}
}
func (a *app) protocol(w http.ResponseWriter, r *http.Request) (result error) {
	w.Header().Set("Content-Type", "application/VIID+JSON")
	device, authErr := a.deviceIdentity(w, r)
	if authErr != nil {
		return authErr
	}
	path := strings.TrimPrefix(r.URL.Path, "/VIID/")
	ctx := r.Context()
	if path == "System/Time" && r.Method == "GET" {
		return write(w, M{"SystemTimeObject": M{"VIIDServerID": a.serverID, "TimeMode": "1", "TimeZone": "+08:00", "LocalTime": stamp(time.Now())}})
	}
	if strings.HasPrefix(path, "System/") {
		if r.Method != "POST" {
			return problem{405, "请求方法错误"}
		}
		op := strings.TrimPrefix(path, "System/")
		label := map[string]string{"Register": "注册", "Keepalive": "心跳", "UnRegister": "注销"}[op]
		if label == "" {
			return pgx.ErrNoRows
		}
		var body map[string]M
		if e := decode(r, &body); e != nil {
			return e
		}
		id := field(body[op+"Object"], "DeviceID")
		if a.strictModel && !digits(id, 20) {
			return bad("注册身份须为 20 位数字")
		}
		if id != device {
			return bad("报文设备编号与认证身份不一致")
		}
		tx, e := a.db.Begin(ctx)
		if e != nil {
			return e
		}
		defer tx.Rollback(ctx)
		if op == "Register" {
			if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,2))", id); e != nil {
				return e
			}
			var managed bool
			if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM viid_catalog WHERE resource='APEs' AND id=$1)", id).Scan(&managed); e != nil {
				return e
			}
			if managed {
				return problem{403, "目录设备请使用所属平台接入"}
			}
			host, _, _ := net.SplitHostPort(r.RemoteAddr)
			_, e = tx.Exec(ctx, "INSERT INTO devices(id,name,ip) VALUES($1,$1,$2) ON CONFLICT DO NOTHING", id, host)
			if e != nil {
				return e
			}
		}
		if op == "Register" && (a.lowerIdentity(id) || a.peerIdentity(id, "notify")) {
			if _, e = tx.Exec(ctx, "UPDATE devices SET kind='级联平台' WHERE id=$1", id); e != nil {
				return e
			}
		}
		tag, e := tx.Exec(ctx, "UPDATE devices SET registered=$2,heartbeat=now() WHERE id=$1 AND enabled AND ($3='Register' OR registered)", id, op != "UnRegister", op)
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			return problem{403, "设备未注册或已停用"}
		}
		if _, e = tx.Exec(ctx, "INSERT INTO device_events(device_id,event) VALUES($1,$2)", id, label); e != nil {
			return e
		}
		if e = tx.Commit(ctx); e != nil {
			return e
		}
		return write(w, M{"ResponseStatusObject": status(id, r.URL.Path)})
	}
	if path == "Subscribes" || strings.HasPrefix(path, "Subscribes/") {
		return a.protocolSubscription(w, r, device)
	}
	collection, objectID, isSingle := strings.Cut(path, "/")
	if spec, ok := catalogs[collection]; ok {
		return a.queryCatalog(w, r, device, collection, spec, strings.TrimSuffix(objectID, "/"))
	}
	if collection == "Images" {
		return a.protocolImages(w, r, device, objectID)
	}
	if path == "SubscribeNotifications" && r.Method == "POST" {
		return a.receiveNotices(w, r, device)
	}
	if isSingle && objectID == "" {
		return pgx.ErrNoRows
	}
	typ, ok := types[collection]
	if !ok {
		return pgx.ErrNoRows
	}
	if r.Method == "GET" {
		if err := a.online(r, device); err != nil {
			return err
		}
		q, err := objectQuery(r.URL.Query(), typ.id)
		if err != nil {
			return err
		}
		q.Set("kind", typ.kind)
		view := "record_list"
		if a.peerIdentity(device, "notify") {
			view = "management_records"
		}
		if a.lowerIdentity(device) {
			q.Set("origin", device)
		} else if view == "record_list" {
			if requested := q.Get("device"); requested != "" && requested != device {
				return problem{403, "设备只能查询自己的上报"}
			}
			q.Set("device", device)
		}
		if isSingle {
			q.Set("exactID", objectID)
		}
		out, e := a.page(ctx, "records", q, view)
		if e != nil {
			return e
		}
		objects := []any{}
		for _, row := range out["items"].([]json.RawMessage) {
			var m struct{ Raw json.RawMessage }
			if e = json.Unmarshal(row, &m); e != nil {
				return e
			}
			objects = append(objects, m.Raw)
		}
		if isSingle {
			if len(objects) == 0 {
				return pgx.ErrNoRows
			}
			return write(w, M{typ.object + "Object": objects[0]})
		}
		return write(w, M{typ.object + "ListObject": M{typ.object + "Object": objects}, "TotalNum": out["total"]})
	}
	if r.Method != "POST" || isSingle {
		return problem{405, "请求方法错误"}
	}
	var body map[string]map[string][]M
	if e := decode(r, &body); e != nil {
		return e
	}
	items := body[typ.object+"ListObject"][typ.object+"Object"]
	if len(items) == 0 || len(items) > 500 {
		return bad("每批应包含 1–500 条对象")
	}
	tx, e := a.db.Begin(ctx)
	if e != nil {
		return e
	}
	failure := protocolBatchFailure{stage: "device_status"}
	defer failure.finish(ctx, tx, &result)
	var active bool
	if e = tx.QueryRow(ctx, "SELECT enabled AND registered AND heartbeat>now()-interval '5 minutes' FROM devices WHERE id=$1 FOR UPDATE", device).Scan(&active); e != nil {
		return e
	}
	if !active {
		return problem{403, "设备未在线注册"}
	}
	replies := []M{}
	failure.stage = "capture"
	for i, obj := range items {
		failure.item = i + 1
		if e = a.ingestCapture(ctx, tx, device, typ, obj); e != nil {
			return e
		}
		replies = append(replies, status(field(obj, typ.id), r.URL.Path))
	}
	failure.stage, failure.item = "heartbeat", 0
	if _, e = tx.Exec(ctx, "UPDATE devices SET heartbeat=clock_timestamp() WHERE id=$1", device); e != nil {
		return e
	}
	failure.stage = "commit"
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	failure.stage = "response"
	return write(w, M{"ResponseStatusListObject": M{"ResponseStatusObject": replies}})
}

func (a *app) ingestCapture(ctx context.Context, tx pgx.Tx, principal string, typ objectType, obj M) error {
	id := field(obj, typ.id)
	if id == "" || len(id) > 200 {
		return bad("对象编号或设备编号无效")
	}
	device := field(obj, "DeviceID")
	if e := a.authorizeSource(ctx, tx, principal, device); e != nil {
		return e
	}
	if a.strictModel || a.lowerIdentity(principal) {
		if e := validateCapture(typ, obj); e != nil {
			return e
		}
	}
	captureStamp := field(obj, "ShotTime", "PassTime", "PersonAppearTime", "FaceAppearTime", "MotorVehicleAppearTime", "NonMotorVehicleAppearTime", "AppearTime", "MarkTime", "InfoAcquisitionTime")
	if captureStamp == "" {
		if src := field(obj, "SourceID"); basicID(src) {
			captureStamp = src[22:36]
		}
	}
	captured, e := parseTime(captureStamp)
	if e != nil {
		return e
	}
	title := field(obj, "Name", "PlateNo", "VehicleClass", typ.id)
	var pics []M
	if sub, ok := obj["SubImageList"].(map[string]any); ok {
		if sub["SubImageInfoObject"] != nil {
			b, _ := json.Marshal(sub["SubImageInfoObject"])
			if e = json.Unmarshal(b, &pics); e != nil {
				return bad("子图像列表格式错误")
			}
		}
	}
	type photo struct {
		id, name string
		data     []byte
		shot     time.Time
	}
	var photos []photo
	var references []string
	if src := field(obj, "SourceID"); a.lowerIdentity(principal) && imageID(src) && len(pics) == 0 {
		references = append(references, src)
	}
	for index, p := range pics {
		encoded := field(p, "Data")
		if encoded == "" {
			if pid := field(p, "ImageID"); pid != "" {
				references = append(references, pid)
			}
			continue
		}
		data, e := base64.StdEncoding.DecodeString(encoded)
		if e != nil {
			return bad("图片 Base64 无效")
		}
		pid := field(p, "ImageID")
		if pid == "" {
			pid = id + "-" + strconv.Itoa(index+1)
		}
		delete(p, "Data")
		p["ImageID"] = pid
		shot := captured
		if value := field(p, "ShotTime"); value != "" {
			shot, e = parseTime(value)
			if e != nil {
				return e
			}
		}
		photos = append(photos, photo{pid, pid + "." + strings.ToLower(field(p, "FileFormat")), data, shot})
	}
	if pics != nil {
		obj["SubImageList"] = M{"SubImageInfoObject": pics}
	}
	raw, e := json.Marshal(obj)
	if e != nil {
		return e
	}
	tag, e := tx.Exec(ctx, "INSERT INTO records(id,kind,device_id,captured,title,raw) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING", id, typ.kind, device, captured, title, raw)
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 1 {
		_, e = tx.Exec(ctx, "INSERT INTO deliveries(subscription,record_id) SELECT id,$1 FROM subscriptions WHERE enabled AND (kind='' OR kind=$2) AND (device_id='' OR device_id=$3) AND (begins IS NULL OR begins<=now()) AND (ends IS NULL OR ends>=now())", id, typ.kind, device)
		if e != nil {
			return e
		}

	} else {
		var same bool
		if e = tx.QueryRow(ctx, "SELECT device_id=$2 AND kind=$3 AND raw=$4::jsonb FROM records WHERE id=$1", id, device, typ.kind, raw).Scan(&same); e != nil {
			return e
		}
		if !same {
			return problem{409, "对象编号已被其他数据使用"}
		}
	}
	for _, p := range photos {
		if e = linkProtocolImage(ctx, tx, p.id, id, device, typ.kind); e != nil {
			return e
		}
		var stored bool
		if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT FROM images WHERE id=$1)", p.id).Scan(&stored); e != nil {
			return e
		}
		if stored {
			e = saveProtocolData(ctx, tx, p.id, device, p.data)
		} else {
			e = putImage(ctx, tx, p.id, p.name, id, device, typ.kind, p.data, p.shot)
		}
		if e != nil {
			return e
		}
	}
	for _, pid := range references {
		if e = linkProtocolImage(ctx, tx, pid, id, device, typ.kind); e != nil {
			return e
		}
	}
	return nil
}
