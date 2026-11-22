package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/icholy/digest"
	"github.com/jackc/pgx/v5"
)

type exchangeSender struct {
	sessionMu    sync.Mutex
	peer         exchangePeer
	client       *http.Client
	heartbeat    time.Time
	nextRegister time.Time
}

func newSender(p exchangePeer) *exchangeSender {
	c := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if p.Mode != "webhook" {
		c.Transport = &digest.Transport{Username: p.User, Password: p.Password}
	}
	return &exchangeSender{peer: p, client: c}
}
func (s *exchangeSender) request(ctx context.Context, method, path string, payload any, deliveryID string) ([]byte, error) {
	var body []byte
	if payload != nil {
		var err error
		body, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(s.peer.URL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("目标配置无效")
	}
	if s.peer.Mode != "webhook" {
		req.Header.Set("User-Identify", s.peer.User)
		req.Header.Set("Content-Type", "application/VIID+JSON")
	} else {
		req.Header.Set("Content-Type", "application/json")
		if s.peer.Token != "" {
			req.Header.Set("Authorization", "Bearer "+s.peer.Token)
		}
	}
	if deliveryID != "" {
		req.Header.Set("Idempotency-Key", deliveryID)
	}
	res, err := s.client.Do(req)
	if err != nil {
		return nil, errors.New("连接失败或响应超时")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("目标返回 HTTP %d", res.StatusCode)
	}
	limit := int64(4 << 20)
	if strings.HasSuffix(path, "/Data") {
		limit = 12 << 20
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if int64(len(data)) > limit {
		return nil, errors.New("目标响应超过大小限制")
	}
	return data, err
}

type peerStatus struct {
	ID         string
	StatusCode json.RawMessage
}

func (st peerStatus) accepted(id string) bool {
	return st.ID == id && (string(st.StatusCode) == "0" || string(st.StatusCode) == `"0"`)
}
func (s *exchangeSender) system(ctx context.Context, op string) error {
	p := s.peer
	b, err := s.request(ctx, "POST", "/VIID/System/"+op, M{op + "Object": M{"DeviceID": p.User}}, "")
	if err != nil {
		return err
	}
	var response struct{ ResponseStatusObject peerStatus }
	if json.Unmarshal(b, &response) != nil || !response.ResponseStatusObject.accepted(p.User) {
		return errors.New("上级未确认" + op)
	}
	return nil
}
func (s *exchangeSender) maintain(ctx context.Context) error {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	if (s.peer.Mode != "viid" && s.peer.Mode != "lower") || time.Since(s.heartbeat) < 75*time.Second {
		return nil
	}
	if !s.heartbeat.IsZero() && s.system(ctx, "Keepalive") == nil {
		s.heartbeat = time.Now()
		return nil
	}
	if time.Now().Before(s.nextRegister) {
		return errors.New("等待重新注册")
	}
	if err := s.system(ctx, "Register"); err != nil {
		s.nextRegister = time.Now().Add(time.Duration(1+rand.IntN(299)) * time.Second)
		return err
	}
	b, err := s.request(ctx, "GET", "/VIID/System/Time", nil, "")
	if err != nil {
		return err
	}
	var clock struct{ SystemTimeObject struct{ LocalTime string } }
	if json.Unmarshal(b, &clock) != nil {
		return errors.New("上级校时响应无效")
	}
	if _, err = parseTime(clock.SystemTimeObject.LocalTime); err != nil {
		return err
	}
	s.heartbeat = time.Now()
	return nil
}
func (s *exchangeSender) push(ctx context.Context, tx pgx.Tx, id, target, record string) error {
	var payload json.RawMessage
	var kind string
	if err := tx.QueryRow(ctx, "SELECT to_jsonb(r),kind FROM management_records r WHERE id=$1", record).Scan(&payload, &kind); err != nil {
		return err
	}
	if s.peer.Mode == "webhook" {
		_, err := s.request(ctx, "POST", "", M{"delivery_id": id, "record": payload}, id)
		return err
	}
	if err := s.maintain(ctx); err != nil {
		return err
	}
	var raw M
	if err := tx.QueryRow(ctx, "SELECT raw FROM management_records WHERE id=$1", record).Scan(&raw); err != nil {
		return err
	}
	if s.peer.Mode == "viid" && s.peer.LegacySourceRewrite {
		if _, ok := raw["SourceDeviceID"]; !ok {
			raw["SourceDeviceID"] = raw["DeviceID"]
		}
		raw["DeviceID"] = s.peer.User
	}
	// Preserve vendor metadata while attaching locally stored data to its original sub-image.
	if group, ok := raw["SubImageList"].(map[string]any); ok {
		pictures, _ := group["SubImageInfoObject"].([]any)
		for _, item := range pictures {
			p, ok := item.(map[string]any)
			if !ok {
				continue
			}
			var content []byte
			err := tx.QueryRow(ctx, "SELECT content FROM images WHERE id=$1 AND (record_id=$2 OR EXISTS(SELECT FROM viid_image_links WHERE record_id=$2 AND image_id=$1))", field(p, "ImageID"), record).Scan(&content)
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			p["Data"] = base64.StdEncoding.EncodeToString(content)
		}
	}
	for path, t := range types {
		if t.kind == kind {
			expected := record
			list := M{t.object + "Object": []M{raw}}
			envelope := M{t.object + "ListObject": list}
			if s.peer.Mode == "notify" {
				var external, title string
				var created time.Time
				var seq int64
				err := tx.QueryRow(ctx, "SELECT s.external_id,s.details->>'Title',d.created,d.id FROM deliveries d JOIN subscriptions s ON s.id=d.subscription WHERE d.id=$1", id).Scan(&external, &title, &created, &seq)
				if err != nil {
					return err
				}
				expected = fmt.Sprintf("%s04%s%05d", s.peer.User[:12], stamp(created), seq%100000)
				notice := M{"NotificationID": expected, "SubscribeID": external, "Title": title, "TriggerTime": stamp(created), "InfoIDs": record, t.object + "ObjectList": list}
				path = "SubscribeNotifications"
				envelope = M{"SubscribeNotificationListObject": M{"SubscribeNotificationObject": []M{notice}}}
			}
			b, err := s.request(ctx, "POST", "/VIID/"+path, envelope, id)
			if err != nil {
				s.heartbeat = time.Time{}
				return err
			}
			var result struct {
				ResponseStatusListObject struct{ ResponseStatusObject []peerStatus }
			}
			if json.Unmarshal(b, &result) != nil || len(result.ResponseStatusListObject.ResponseStatusObject) != 1 || !result.ResponseStatusListObject.ResponseStatusObject[0].accepted(expected) {
				return errors.New("上级未确认对象编号或返回失败状态")
			}
			return nil
		}
	}
	return fmt.Errorf("交换目标 %s 对象类型无效", target)
}

func (a *app) deliverOne(ctx context.Context, senders map[string]*exchangeSender) (bool, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var id int64
	var record, target string
	var attempts int
	err = tx.QueryRow(ctx, `SELECT d.id,d.record_id,s.target,d.attempts FROM deliveries d JOIN subscriptions s ON s.id=d.subscription
 WHERE s.enabled AND (s.ends IS NULL OR s.ends>=now()) AND d.state='pending' AND d.due<=now() ORDER BY d.due,d.id LIMIT 1 FOR UPDATE OF d SKIP LOCKED`).Scan(&id, &record, &target, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	state, msg := "sent", ""
	sender := senders[target]
	if sender == nil {
		err = errors.New("交换目标未配置")
	} else {
		err = sender.push(ctx, tx, fmt.Sprint(id), target, record)
	}
	if err != nil {
		state = "pending"
		if attempts >= 7 {
			state = "failed"
		}
		msg = err.Error()
	}
	_, err = tx.Exec(ctx, "UPDATE deliveries SET state=$2,message=$3,attempts=attempts+1,updated=now(),due=now()+make_interval(secs=>$4) WHERE id=$1", id, state, msg, float64(int64(5)<<uint(min(attempts, 8))))
	if err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
func (a *app) runExchange(ctx context.Context) {
	senders := map[string]*exchangeSender{}
	for k, p := range a.peers {
		if p.Mode == "lower" {
			senders[k] = a.lowerClients[p.Subscriber]
		} else {
			senders[k] = newSender(p)
		}
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	heartbeat := time.NewTicker(10 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			grace, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			for _, s := range senders {
				s.sessionMu.Lock()
				if (s.peer.Mode == "viid" || s.peer.Mode == "lower") && !s.heartbeat.IsZero() {
					_ = s.system(grace, "UnRegister")
				}
				s.sessionMu.Unlock()
			}
			cancel()
			return
		case <-heartbeat.C:
			for name, s := range senders {
				if e := s.maintain(ctx); e != nil {
					log.Printf("交换目标 %s: %v", name, e)
				}
			}
		case <-tick.C:
			until := time.Now().Add(time.Second)
			for i := 0; i < 32 && time.Now().Before(until); i++ {
				sent, e := a.deliverOne(ctx, senders)
				if e != nil {
					log.Print(e)
					break
				}
				if !sent {
					break
				}
			}
		}
	}
}
