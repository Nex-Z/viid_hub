package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Destinations and credentials are operator-owned. API callers can select only a named peer.
type exchangePeer struct {
	Mode, URL, User, Password, Token, Subscriber, SubscriberPassword, CallbackURL string
	Catalogs                                                                      []string
	LegacySourceRewrite                                                           bool
}

func readPeers(path string) (map[string]exchangePeer, error) {
	peers := map[string]exchangePeer{}
	if path == "" {
		return peers, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(b, &peers); err != nil {
		return nil, errors.New("交换目标配置 JSON 无效")
	}
	seen := map[string]bool{}
	for name, p := range peers {
		if p.Mode == "notify" || p.Mode == "lower" {
			if len(p.User) != 20 || strings.Trim(p.User, "0123456789") != "" || seen[p.Subscriber] {
				return nil, fmt.Errorf("通知目标 %q 的平台编号无效或订阅方重复", name)
			}
			seen[p.Subscriber] = true
		}
		u, err := url.Parse(p.URL)
		if name == "" || err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
			return nil, fmt.Errorf("交换目标 %q 地址无效", name)
		}
		if p.Mode == "lower" {
			if len(p.Catalogs) > 0 {
				included := map[string]bool{}
				for _, resource := range p.Catalogs {
					if _, ok := catalogs[resource]; !ok || included[resource] {
						return nil, fmt.Errorf("下级 %q 的目录类型无效或重复", name)
					}
					included[resource] = true
				}
				if !included["APEs"] {
					return nil, fmt.Errorf("下级 %q 须包含 APEs 目录", name)
				}
			}
			cb, err := url.Parse(p.CallbackURL)
			if !digits(p.Subscriber, 20) || err != nil || cb.Host == "" || cb.User != nil || cb.RawQuery != "" || cb.Fragment != "" || (cb.Scheme != "http" && cb.Scheme != "https") || cb.Path != "/VIID/SubscribeNotifications" {
				return nil, fmt.Errorf("下级 %q 的编号或回调地址无效", name)
			}
		}
		if (p.Mode != "lower" && p.Mode != "webhook" && p.Mode != "viid" && p.Mode != "notify") || ((p.Mode == "viid" || p.Mode == "notify" || p.Mode == "lower") && (p.User == "" || p.Password == "")) || ((p.Mode == "notify" || p.Mode == "lower") && (p.Subscriber == "" || p.SubscriberPassword == "")) {
			return nil, fmt.Errorf("交换目标 %q 类型或认证配置无效", name)
		}
	}
	return peers, nil
}

func (a *app) exchangeRoutes(m *http.ServeMux, reg func(string, endpoint)) {
	reg("POST /api/exchange/lower/{name}/sync", a.syncLower)
	reg("POST /api/exchange/lower/{name}/subscribe", a.createLowerSubscription)
	reg("POST /api/exchange/lower-tasks/{id}/{action}", a.lowerAction)
	reg("GET /api/exchange/settings", a.exchangeSettings)
	reg("POST /api/exchange/subscriptions", a.subscribe)
	reg("PATCH /api/exchange/subscriptions/{id}", a.toggleSubscription)
	reg("POST /api/exchange/deliveries/{id}/retry", a.retryDelivery)
	reg("GET /api/exchange/deliveries", func(w http.ResponseWriter, r *http.Request) error {
		out, err := a.page(r.Context(), "deliveries", r.URL.Query(), "delivery_list")
		if err != nil {
			return err
		}
		return write(w, out)
	})
	consumer := func(fn endpoint) http.HandlerFunc {
		return wrap(func(w http.ResponseWriter, r *http.Request) error {
			token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || a.queryToken == "" || !equal(token, a.queryToken) {
				return problem{401, "消费端认证失败"}
			}
			w.Header().Set("Cache-Control", "private, no-store")
			return fn(w, r)
		})
	}
	m.HandleFunc("GET /exchange/{resource}", consumer(func(w http.ResponseWriter, r *http.Request) error {
		res := r.PathValue("resource")
		if res != "records" && res != "images" {
			return pgx.ErrNoRows
		}
		return a.list(w, r)
	}))
	m.HandleFunc("GET /exchange/images/{id}/file", consumer(func(w http.ResponseWriter, r *http.Request) error {
		if r.URL.RawQuery != "" {
			return bad("原图接口不接受查询参数")
		}
		return a.file(w, r)
	}))
}

func (a *app) exchangeSettings(w http.ResponseWriter, r *http.Request) error {
	lowers := []string{}
	peers := map[string]string{}
	for k, p := range a.peers {
		if p.Mode == "lower" {
			lowers = append(lowers, k)
			continue
		}
		if p.Mode != "notify" {
			peers[k] = p.Mode
		}
	}
	rows, e := jsonRows(r.Context(), a.db, "SELECT id,target,kind,device_id,enabled,external_id,ends FROM subscriptions ORDER BY id DESC")
	if e != nil {
		return e
	}
	tasks, e := jsonRows(r.Context(), a.db, "SELECT id,target,source,kind,state,ends,(SELECT count(*) FROM lower_notices WHERE subscription=s.id) AS received FROM lower_subscriptions s ORDER BY ends DESC,id")
	if e != nil {
		return e
	}
	return write(w, M{"peers": peers, "subscriptions": rows, "lowers": lowers, "lowerTasks": tasks})
}
func (a *app) subscribe(w http.ResponseWriter, r *http.Request) error {
	var p struct{ Target, Kind, Device string }
	if e := decode(r, &p); e != nil {
		return e
	}
	if target, ok := a.peers[p.Target]; !ok || target.Mode == "notify" || target.Mode == "lower" {
		return bad("交换目标未配置")
	}
	if p.Kind != "" && p.Kind != "person" && p.Kind != "face" && p.Kind != "motor" && p.Kind != "nonmotor" {
		return bad("订阅类型无效")
	}
	var id int64
	e := a.db.QueryRow(r.Context(), "INSERT INTO subscriptions(target,kind,device_id) VALUES($1,$2,$3) RETURNING id", p.Target, p.Kind, p.Device).Scan(&id)
	if e != nil {
		return e
	}
	return write(w, M{"id": id})
}
func (a *app) toggleSubscription(w http.ResponseWriter, r *http.Request) error {
	var input struct{ Enabled *bool }
	if err := decode(r, &input); err != nil {
		return err
	}
	if input.Enabled == nil {
		return bad("请选择启停状态")
	}
	tag, e := a.db.Exec(r.Context(), "UPDATE subscriptions SET enabled=$1 WHERE id=$2 AND external_id IS NULL", *input.Enabled, r.PathValue("id"))
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return write(w, M{"ok": true})
}
func (a *app) retryDelivery(w http.ResponseWriter, r *http.Request) error {
	tag, e := a.db.Exec(r.Context(), "UPDATE deliveries SET state='pending',due=now(),attempts=0,message='',updated=now() WHERE id=$1 AND state='failed'", r.PathValue("id"))
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 {
		return bad("仅终止重试的任务可重新投递")
	}
	return write(w, M{"ok": true})
}
