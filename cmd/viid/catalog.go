package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

type catalogSpec struct{ object, key string }

var catalogs = map[string]catalogSpec{
	"APEs": {"APE", "ApeID"}, "APSs": {"APS", "ApsID"}, "Tollgates": {"Tollgate", "TollgateID"}, "Lanes": {"Lane", "LaneId"},
}

func digits(s string, n int) bool { return len(s) == n && strings.Trim(s, "0123456789") == "" }
func (a *app) peerIdentity(id, mode string) bool {
	for _, p := range a.peers {
		if p.Mode == mode && p.Subscriber == id {
			return true
		}
	}
	return false
}
func (a *app) lowerIdentity(id string) bool { return a.peerIdentity(id, "lower") }
func (a *app) authorizeSource(ctx context.Context, tx pgx.Tx, principal, device string) error {
	if principal == device && !a.lowerIdentity(principal) {
		return nil
	}
	if !a.lowerIdentity(principal) {
		return bad("对象设备编号与认证身份不一致")
	}
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM viid_catalog c JOIN devices d ON d.id=c.id
 WHERE c.resource='APEs' AND c.owner=$1 AND c.id=$2 AND c.current AND d.enabled)`, principal, device).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return problem{403, "来源设备不在该下级的有效目录中"}
	}
	return nil
}
func (a *app) online(r *http.Request, id string) error {
	var online bool
	err := a.db.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM devices WHERE id=$1 AND enabled AND registered AND heartbeat>now()-interval '5 minutes')", id).Scan(&online)
	if err == nil && !online {
		return problem{403, "身份未在线注册"}
	}
	return err
}

// Only accept supported equality predicates and one-based protocol pagination.
func resourceFilters(q url.Values, allowed ...string) (M, int, int, error) {
	filters := M{}
	limit, start := 100, 1
	for key, vs := range q {
		if len(vs) != 1 {
			return nil, 0, 0, bad("重复的查询条件")
		}
		val := vs[0]
		if strings.HasPrefix(key, "(") {
			if !strings.HasSuffix(val, ")") {
				return nil, 0, 0, bad("查询括号不匹配")
			}
			key = strings.TrimPrefix(key, "(")
			val = strings.TrimSuffix(val, ")")
		}
		if key == "PageRecordNum" || key == "RecordStartNo" {
			n, err := strconv.Atoi(val)
			if err != nil || n < 1 {
				return nil, 0, 0, bad("分页参数无效")
			}
			if key == "PageRecordNum" {
				limit = min(n, 200)
			} else {
				start = n
			}
			continue
		}
		ok := false
		for _, k := range allowed {
			if key == k {
				ok = true
				break
			}
		}
		if !ok {
			return nil, 0, 0, bad("不支持的查询字段 " + key)
		}
		filters[key] = val
	}
	return filters, limit, start - 1, nil
}
func (a *app) queryCatalog(w http.ResponseWriter, r *http.Request, identity, resource string, spec catalogSpec, id string) error {
	if r.Method != "GET" {
		return problem{405, "目录仅支持查询"}
	}
	if err := a.online(r, identity); err != nil {
		return err
	}
	filters, limit, offset, err := resourceFilters(r.URL.Query(), spec.key, "Name", "OwnerApsID", "TollgateID")
	if err != nil {
		return err
	}
	var out []json.RawMessage
	var total int
	err = a.db.QueryRow(r.Context(), `WITH directory AS (
 SELECT id,owner,payload FROM viid_catalog WHERE resource=$1 AND current
 UNION ALL SELECT id,id,jsonb_build_object('ApeID',id,'Name',name,'IPAddr',ip,'Place',point,'IsOnline',CASE WHEN status='在线' THEN 1 ELSE 0 END)
 FROM device_list d WHERE $1='APEs' AND kind<>'级联平台' AND NOT EXISTS(SELECT 1 FROM viid_catalog c WHERE c.resource='APEs' AND c.id=d.id))
 , matches AS (SELECT id,payload FROM directory WHERE ($2 OR ($8 AND owner=$3) OR id=$3) AND ($4='' OR id=$4)
 AND NOT EXISTS(SELECT FROM jsonb_each_text($5) f WHERE payload->>f.key IS DISTINCT FROM f.value)
 ) SELECT (SELECT count(*) FROM matches),coalesce(jsonb_agg(p.payload ORDER BY p.id),'[]'::jsonb) FROM (SELECT * FROM matches ORDER BY id LIMIT $6 OFFSET $7) p`, resource, a.peerIdentity(identity, "notify"), identity, id, filters, limit, offset, a.lowerIdentity(identity)).Scan(&total, &out)
	if err != nil {
		return err
	}
	if id != "" {
		if len(out) == 0 {
			return pgx.ErrNoRows
		}
		return write(w, M{spec.object + "Object": out[0]})
	}
	return write(w, M{spec.object + "ListObject": M{spec.object + "Object": out}, "TotalNum": total})
}
func catalogID(resource string, spec catalogSpec, obj M) (string, error) {
	id := field(obj, spec.key, strings.ToUpper(spec.key))
	if resource == "Lanes" {
		gate := field(obj, "TollgateID")
		n, err := strconv.Atoi(id)
		if !digits(gate, 20) || err != nil || n < 0 {
			return "", bad("下级车道编号无效")
		}
		return gate + "-" + id, nil
	}
	if !digits(id, 20) {
		return "", bad("下级目录编号无效")
	}
	return id, nil
}
func (a *app) syncLower(w http.ResponseWriter, r *http.Request) error {
	p := a.peers[r.PathValue("name")]
	if p.Mode != "lower" {
		return bad("下级未配置")
	}
	sender := a.lowerClients[p.Subscriber]
	ctx := r.Context()
	if err := sender.maintain(ctx); err != nil {
		return problem{502, err.Error()}
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", p.Subscriber); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE viid_catalog SET current=false WHERE owner=$1", p.Subscriber); err != nil {
		return err
	}
	counts := map[string]int{}
	resources := p.Catalogs
	if len(resources) == 0 {
		resources = []string{"APEs", "APSs", "Tollgates", "Lanes"}
	}
	for _, resource := range resources {
		spec := catalogs[resource]
		seen := map[string]bool{}
		for start := 1; ; {
			path := fmt.Sprintf("/VIID/%s?RecordStartNo=%d&PageRecordNum=100", resource, start)
			data, err := sender.request(ctx, "GET", path, nil, "")
			if err != nil {
				return problem{502, resource + " 查询失败：" + err.Error()}
			}
			var body map[string]json.RawMessage
			var list map[string][]M
			if json.Unmarshal(data, &body) != nil || json.Unmarshal(body[spec.object+"ListObject"], &list) != nil || list[spec.object+"Object"] == nil {
				return problem{502, "下级目录响应格式错误：" + resource}
			}
			items := list[spec.object+"Object"]
			for _, obj := range items {
				id, err := catalogID(resource, spec, obj)
				if err != nil {
					return err
				}
				if seen[id] {
					return problem{502, "下级分页返回了重复目录编号"}
				}
				seen[id] = true
				for k := range obj {
					if strings.EqualFold(k, "Password") {
						delete(obj, k)
					}
				}
				if resource == "APEs" {
					if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,2))", id); err != nil {
						return err
					}
					if id == p.Subscriber || id == a.serverID || a.peerIdentity(id, "notify") || a.lowerIdentity(id) {
						return bad("采集设备与平台编号冲突")
					}
					tag, err := tx.Exec(ctx, `INSERT INTO devices(id,name,ip,point) VALUES($1,$2,$3,$4)
      ON CONFLICT(id) DO UPDATE SET name=excluded.name,ip=excluded.ip,point=excluded.point WHERE EXISTS(
       SELECT 1 FROM viid_catalog WHERE resource='APEs' AND owner=$5 AND id=excluded.id)`, id, field(obj, "Name", spec.key), field(obj, "IPAddr"), field(obj, "Place"), p.Subscriber)
					if err != nil {
						return err
					}
					if tag.RowsAffected() == 0 {
						return problem{409, "设备编号已属于其他来源"}
					}
				}
				tag, err := tx.Exec(ctx, `INSERT INTO viid_catalog(resource,id,owner,payload) VALUES($1,$2,$3,$4)
     ON CONFLICT(resource,id) DO UPDATE SET payload=excluded.payload,current=true WHERE viid_catalog.owner=excluded.owner`, resource, id, p.Subscriber, obj)
				if err != nil {
					return err
				}
				if tag.RowsAffected() == 0 {
					return problem{409, "目录编号已属于其他下级"}
				}
			}
			start += len(items)
			counts[resource] = len(seen)
			var total int
			if val, ok := body["TotalNum"]; ok {
				if json.Unmarshal(val, &total) != nil || total < 0 {
					return problem{502, "下级目录总数无效"}
				}
				if start > total {
					break
				}
				if len(items) == 0 {
					return problem{502, "下级目录缺页"}
				}
			} else if len(items) == 0 {
				break
			}
			if start > 100001 {
				return bad("单类目录超过十万项")
			}
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	return write(w, counts)
}
