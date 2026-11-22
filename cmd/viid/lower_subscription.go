package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (a *app) createLowerSubscription(w http.ResponseWriter, r *http.Request) error {
	name := r.PathValue("name")
	p := a.peers[name]
	if p.Mode != "lower" {
		return bad("未配置该下级")
	}
	var in struct{ Kind, Device, Title, ApplicantName, ApplicantOrg, EndTime string }
	if err := decode(r, &in); err != nil {
		return err
	}
	detail := ""
	for code, kind := range subscribeKinds {
		if kind == in.Kind {
			detail = code
		}
	}
	if detail == "" || in.Title == "" || in.ApplicantOrg == "" || in.ApplicantName == "" {
		return bad("请指定类别、标题、申请人及单位")
	}
	now := time.Now()
	end, err := parseTime(in.EndTime)
	if err != nil || !end.After(now) {
		return bad("订阅结束时间无效")
	}
	source := in.Device
	if source == "" {
		source = p.Subscriber
	}
	ctx := r.Context()
	if source != p.Subscriber {
		var ok bool
		if err = a.db.QueryRow(ctx, "SELECT EXISTS(SELECT FROM viid_catalog WHERE id=$1 AND owner=$2 AND resource='APEs' AND current)", source, p.Subscriber).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return bad("设备不在该下级目录中")
		}
	}
	var seq int64
	if err = a.db.QueryRow(ctx, "SELECT nextval('viid_sub_seq')").Scan(&seq); err != nil {
		return err
	}
	id := fmt.Sprintf("%s03%s%05d", p.User[:12], stamp(now), seq%100000)
	obj := M{"SubscribeID": id, "Title": in.Title, "SubscribeDetail": detail, "ResourceURI": source, "ApplicantName": in.ApplicantName, "ApplicantOrg": in.ApplicantOrg, "BeginTime": stamp(now), "EndTime": stamp(end), "ReceiveAddr": p.CallbackURL, "ReportInterval": 0, "OperateType": 0}
	_, err = a.db.Exec(ctx, "INSERT INTO lower_subscriptions(id,target,owner,kind,source,details,ends,state) VALUES($1,$2,$3,$4,$5,$6,$7,'pending')", id, name, p.Subscriber, in.Kind, source, obj, end)
	if err != nil {
		return err
	}
	if err = a.sendLowerTask(ctx, id, false); err != nil {
		return err
	}
	return write(w, M{"id": id})
}
func (a *app) lowerAction(w http.ResponseWriter, r *http.Request) error {
	action := r.PathValue("action")
	if action != "retry" && action != "cancel" {
		return pgx.ErrNoRows
	}
	if err := a.sendLowerTask(r.Context(), r.PathValue("id"), action == "cancel"); err != nil {
		return err
	}
	return write(w, M{"ok": true})
}
func (a *app) sendLowerTask(ctx context.Context, id string, cancel bool) error {
	if cancel {
		_, err := a.db.Exec(ctx, "UPDATE lower_subscriptions SET state='cancelling',details=details||jsonb_build_object('CancelTime',$2::text) WHERE id=$1 AND state IN ('pending','active')", id, stamp(time.Now()))
		if err != nil {
			return err
		}
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Serialize operator actions while allowing an immediate callback in a separate transaction.
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,1))", id); err != nil {
		return err
	}
	var name, owner, state string
	var obj M
	if err = tx.QueryRow(ctx, "SELECT target,owner,state,details FROM lower_subscriptions WHERE id=$1", id).Scan(&name, &owner, &state, &obj); err != nil {
		return err
	}
	p := a.peers[name]
	if p.Mode != "lower" || p.Subscriber != owner {
		return bad("下级配置与原订阅不一致")
	}
	if state == "cancelled" || state == "active" && !cancel {
		return nil
	}
	cancel = cancel || state == "cancelling"
	sender := a.lowerClients[p.Subscriber]
	if err = sender.maintain(ctx); err != nil {
		return problem{502, "下级连接失败，订阅可重试：" + id}
	}
	method, path, next := "POST", "/VIID/Subscribes", "active"
	var payload any = M{"SubscribeListObject": M{"SubscribeObject": []M{obj}}}
	if cancel {
		method, path, next = "PUT", path+"/"+id, "cancelled"
		obj["OperateType"] = 1

		obj["SubscribeCancelOrg"] = obj["ApplicantOrg"]
		obj["SubscribeCancelPerson"] = obj["ApplicantName"]
		obj["CancelReason"] = "管理端取消"
		payload = M{"SubscribeObject": obj}
	}
	b, err := sender.request(ctx, method, path, payload, "")
	if err != nil {
		return problem{502, "下级未确认，可重试原订阅 " + id + "：" + err.Error()}
	}
	var res struct {
		ResponseStatusObject     peerStatus
		ResponseStatusListObject struct{ ResponseStatusObject []peerStatus }
	}
	if json.Unmarshal(b, &res) != nil {
		return problem{502, "下级返回无效回执"}
	}
	ok := res.ResponseStatusObject.accepted(id)
	list := res.ResponseStatusListObject.ResponseStatusObject
	if !cancel {
		ok = len(list) == 1 && list[0].accepted(id)
	}
	if !ok {
		return problem{502, "下级未确认该订阅编号"}
	}
	if _, err = tx.Exec(ctx, "UPDATE lower_subscriptions SET state=$2 WHERE id=$1 AND state=$3", id, next, state); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (a *app) receiveNotices(w http.ResponseWriter, r *http.Request, owner string) (result error) {
	if !a.lowerIdentity(owner) {
		return problem{403, "未配置该通知发送方"}
	}
	var input struct {
		SubscribeNotificationListObject struct{ SubscribeNotificationObject []M }
	}
	if err := decode(r, &input); err != nil {
		return err
	}
	items := input.SubscribeNotificationListObject.SubscribeNotificationObject
	if len(items) == 0 || len(items) > 500 {
		return bad("通知数量须为 1–500")
	}
	ctx := r.Context()
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	failure := protocolBatchFailure{stage: "sender_status"}
	defer failure.finish(ctx, tx, &result)
	var disabled bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT FROM devices WHERE id=$1 AND NOT enabled)", owner).Scan(&disabled); err != nil {
		return err
	}
	if disabled {
		return problem{403, "下级已停用"}
	}
	replies := []M{}
	for i, notice := range items {
		failure.stage, failure.item = "notice_header", i+1
		id := field(notice, "NotificationID")
		sub := field(notice, "SubscribeID")
		trigger := field(notice, "TriggerTime")
		if !businessID(id, "04") || !businessID(sub, "03") || field(notice, "Title") == "" || !validClock(trigger) {
			return bad("通知编号、标题、时间或订阅号无效")
		}
		var kind, source, state string
		var ends time.Time
		var begins string
		failure.stage = "subscription_lookup"
		err = tx.QueryRow(ctx, "SELECT kind,source,state,details->>'BeginTime',ends FROM lower_subscriptions WHERE id=$1 AND owner=$2 FOR SHARE", sub, owner).Scan(&kind, &source, &state, &begins, &ends)
		if err != nil {
			return err
		}
		failure.stage = "notice_dedup"
		body, _ := json.Marshal(notice)
		sum := sha256.Sum256(body)
		tag, e := tx.Exec(ctx, "INSERT INTO lower_notices(owner,id,subscription,fingerprint) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING", owner, id, sub, sum[:])
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			var prior []byte
			if e = tx.QueryRow(ctx, "SELECT fingerprint FROM lower_notices WHERE owner=$1 AND id=$2", owner, id).Scan(&prior); e != nil {
				return e
			}
			if !bytes.Equal(prior, sum[:]) {
				return problem{409, "相同通知编号的内容发生变化"}
			}
		} else {
			failure.stage = "subscription_scope"
			if state == "cancelled" || trigger < begins || trigger > stamp(ends) || trigger > stamp(time.Now().Add(5*time.Minute)) {
				return problem{403, "通知超出订阅有效范围"}
			}
			failure.stage = "notice_objects"
			ids := map[string]bool{}
			for _, s := range strings.Split(field(notice, "InfoIDs"), ";") {
				if s == "" || ids[s] {
					return bad("通知 InfoIDs 无效")
				}
				ids[s] = true
			}
			count := 0
			for _, t := range types {
				val, exists := notice[t.object+"ObjectList"]
				if !exists {
					continue
				}
				if t.kind != kind {
					return bad("通知类型不在订阅范围内")
				}
				group, _ := val.(map[string]any)
				objs, _ := group[t.object+"Object"].([]any)
				for _, v := range objs {
					obj, ok := v.(map[string]any)
					if !ok || !ids[field(obj, t.id)] {
						return bad("通知对象与 InfoIDs 不匹配")
					}
					if source != owner && source != field(obj, "DeviceID") {
						return problem{403, "通知设备不在订阅范围内"}
					}
					delete(ids, field(obj, t.id))
					count++
					if count > 500 {
						return bad("单通知对象超过 500 条")
					}
					failure.stage = "capture"
					if e = a.ingestCapture(ctx, tx, owner, t, obj); e != nil {
						return e
					}
				}
			}
			if count == 0 || len(ids) > 0 {
				return bad("通知缺少匹配的结构化对象")
			}
		}
		replies = append(replies, status(id, r.URL.Path))
	}
	failure.stage, failure.item = "commit", 0
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	failure.stage = "response"
	return write(w, M{"ResponseStatusListObject": M{"ResponseStatusObject": replies}})
}
