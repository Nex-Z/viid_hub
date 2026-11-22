package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var subscribeKinds = map[string]string{"11": "person", "12": "face", "13": "motor", "14": "nonmotor"}

func (a *app) protocolSubscription(w http.ResponseWriter, r *http.Request, device string) error {
	ctx := r.Context()
	var target string
	for name, p := range a.peers {
		if p.Mode == "notify" && p.Subscriber == device {
			target = name
			break
		}
	}
	if target == "" {
		return problem{403, "该身份未配置订阅接收端"}
	}
	var online bool
	if err := a.db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM device_list WHERE id=$1 AND status='在线')", device).Scan(&online); err != nil {
		return err
	}
	if !online {
		return problem{403, "订阅方未在线注册"}
	}
	path := r.URL.Path
	if r.Method == "GET" && path == "/VIID/Subscribes" {
		rows, err := a.db.Query(ctx, "SELECT details FROM subscriptions WHERE owner_id=$1 ORDER BY id", device)
		if err != nil {
			return err
		}
		defer rows.Close()
		list := []json.RawMessage{}
		for rows.Next() {
			var obj json.RawMessage
			if err = rows.Scan(&obj); err != nil {
				return err
			}
			list = append(list, obj)
		}
		if err = rows.Err(); err != nil {
			return err
		}
		return write(w, M{"SubscribeListObject": M{"SubscribeObject": list}})
	}
	if r.Method == "PUT" {
		var input struct{ SubscribeObject M }
		if err := decode(r, &input); err != nil {
			return err
		}
		obj := input.SubscribeObject
		id := r.PathValue("subscription")
		if id != field(obj, "SubscribeID") || field(obj, "OperateType") != "1" {
			return bad("取消订阅的编号或操作类型无效")
		}
		// Preserve the original task identity, scope and destination on cancellation.
		cancel := M{"OperateType": 1, "SubscribeStatus": 1}
		for _, key := range []string{"SubscribeCancelOrg", "SubscribeCancelPerson", "CancelTime", "CancelReason"} {
			if v := field(obj, key); v != "" {
				cancel[key] = v
			}
		}
		tag, err := a.db.Exec(ctx, "UPDATE subscriptions SET enabled=false,details=details||$3::jsonb WHERE owner_id=$1 AND external_id=$2", device, id, cancel)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return write(w, M{"ResponseStatusObject": status(id, path)})
	}
	if r.Method != "POST" || path != "/VIID/Subscribes" {
		return problem{405, "不支持的订阅操作"}
	}
	var input struct{ SubscribeListObject struct{ SubscribeObject []M } }
	if err := decode(r, &input); err != nil {
		return err
	}
	items := input.SubscribeListObject.SubscribeObject
	if len(items) == 0 || len(items) > 500 {
		return bad("订阅数须为 1–500")
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	replies := []M{}
	for _, obj := range items {
		id := field(obj, "SubscribeID")
		kind := subscribeKinds[field(obj, "SubscribeDetail")]
		if id == "" || len(id) > 64 || kind == "" || field(obj, "OperateType") != "0" || field(obj, "ApplicantName") == "" || field(obj, "ApplicantOrg") == "" || field(obj, "Title") == "" {
			return bad("订阅标识、类别、申请人、单位、标题或操作类型无效")
		}
		if field(obj, "ReceiveAddr") != strings.TrimRight(a.peers[target].URL, "/")+"/VIID/SubscribeNotifications" {
			return bad("接收地址须匹配已配置订阅方")
		}
		start, err := parseTime(field(obj, "BeginTime"))
		if err != nil {
			return err
		}
		end, err := parseTime(field(obj, "EndTime"))
		if err != nil {
			return err
		}
		if !end.After(start) || !end.After(time.Now()) {
			return bad("订阅有效期无效")
		}
		if value := field(obj, "ReportInterval"); value != "" && value != "0" && value != "-1" {
			return bad("当前仅支持实时通知")
		}
		source := field(obj, "ResourceURI")
		if source == "" {
			return bad("缺少订阅资源范围")
		}
		if source == a.serverID {
			source = ""
		}
		if source != "" {
			var known bool
			if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM devices WHERE id=$1)", source).Scan(&known); err != nil {
				return err
			}
			if !known {
				return bad("仅支持订阅本平台或已登记的单台设备")
			}
		}
		tag, err := tx.Exec(ctx, `INSERT INTO subscriptions(target,kind,device_id,external_id,owner_id,details,begins,ends)
   VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(external_id) WHERE external_id IS NOT NULL DO NOTHING`, target, kind, source, id, device, obj, start, end)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			var same bool
			if err = tx.QueryRow(ctx, "SELECT owner_id=$2 AND details=$3::jsonb AND enabled FROM subscriptions WHERE external_id=$1", id, device, obj).Scan(&same); err != nil {
				return err
			}
			if !same {
				return problem{409, "订阅编号已使用"}
			}
		}
		replies = append(replies, status(id, path))
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	return write(w, M{"ResponseStatusListObject": M{"ResponseStatusObject": replies}})
}
