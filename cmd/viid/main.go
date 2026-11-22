package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

//go:embed schema.sql
var schema string
var zone = time.FixedZone("UTC+8", 8*3600)

type M = map[string]any
type app struct {
	db                             *pgxpool.Pool
	user, password, devicePassword string
	allowIdentity                  bool
	peers                          map[string]exchangePeer
	lowerClients                   map[string]*exchangeSender
	queryToken                     string
	digestOnly                     bool
	strictModel                    bool
	serverID                       string
	key                            []byte
}
type endpoint func(http.ResponseWriter, *http.Request) error
type problem struct {
	status int
	msg    string
}

func (e problem) Error() string { return e.msg }
func bad(s string) error        { return problem{400, s} }
func write(w http.ResponseWriter, v any) error {
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	}
	return json.NewEncoder(w).Encode(v)
}
func wrap(fn endpoint) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
		if r.Method != "GET" && r.Method != "HEAD" {
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || u.Host != r.Host {
					http.Error(w, "请求来源不匹配", 403)
					return
				}
			}
		}
		if err := fn(w, r); err != nil {
			p := problem{500, "服务暂时不可用"}
			var pe *pgconn.PgError
			switch {
			case errors.As(err, &p):
			case errors.Is(err, pgx.ErrNoRows):
				p = problem{404, "记录不存在"}
			case errors.As(err, &pe) && (pe.Code == "23503" || pe.Code == "23505"):
				p = problem{409, "关联记录不存在，或编号已被使用"}
			default:
				if !strings.HasPrefix(r.URL.Path, "/VIID/") {
					log.Print(err)
				}
			}
			if strings.HasPrefix(r.URL.Path, "/VIID/") {
				logProtocolRejection(w, r, p.status, err)
				w.Header().Set("Content-Type", "application/VIID+JSON")
				w.WriteHeader(p.status)
				reply := status(r.Header.Get("User-Identify"), r.URL.Path)
				reply["StatusCode"] = 1
				switch p.status {
				case 400:
					reply["StatusCode"] = 8
					if strings.Contains(p.msg, "JSON 无效") {
						reply["StatusCode"] = 7
					}
				case 401, 403, 404, 405, 409:
					reply["StatusCode"] = 4
				case 503:
					reply["StatusCode"] = 2
				}
				reply["StatusString"] = p.msg
				_ = write(w, M{"ResponseStatusObject": reply})
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(p.status)
			_ = json.NewEncoder(w).Encode(M{"error": p.msg})
		}
	}
}
func decode(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return bad("请求 JSON 无效或过大")
	}
	return nil
}
func equal(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func (a *app) sign(expires string) string {
	h := hmac.New(sha256.New, a.key)
	h.Write([]byte(expires))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
func (a *app) admin(fn endpoint) endpoint {
	return func(w http.ResponseWriter, r *http.Request) error {
		c, e := r.Cookie("viid_session")
		if e == nil {
			t, s, _ := strings.Cut(c.Value, ".")
			v, _ := strconv.ParseInt(t, 10, 64)
			if v > time.Now().Unix() && equal(s, a.sign(t)) {
				w.Header().Set("Cache-Control", "private, no-store")
				return fn(w, r)
			}
		}
		return problem{401, "请登录管理后台"}
	}
}
func (a *app) login(w http.ResponseWriter, r *http.Request) error {
	var p struct{ User, Password string }
	if e := decode(r, &p); e != nil {
		return e
	}
	if !equal(p.User, a.user) || !equal(p.Password, a.password) {
		return problem{401, "用户名或密码错误"}
	}
	expiry := time.Now().Add(8 * time.Hour)
	t := strconv.FormatInt(expiry.Unix(), 10)
	http.SetCookie(w, &http.Cookie{Name: "viid_session", Value: t + "." + a.sign(t), Path: "/api", Expires: expiry, HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode})
	return write(w, M{"user": a.user})
}
func (a *app) routes() http.Handler {
	a.lowerClients = map[string]*exchangeSender{}
	for _, p := range a.peers {
		if p.Mode == "lower" {
			a.lowerClients[p.Subscriber] = newSender(p)
		}
	}
	m := http.NewServeMux()
	m.HandleFunc("POST /api/login", wrap(a.login))
	reg := func(path string, fn endpoint) { m.HandleFunc(path, wrap(a.admin(fn))) }
	reg("GET /api/session", func(w http.ResponseWriter, r *http.Request) error { return write(w, M{"user": a.user}) })
	reg("POST /api/logout", func(w http.ResponseWriter, r *http.Request) error {
		http.SetCookie(w, &http.Cookie{Name: "viid_session", Path: "/api", MaxAge: -1, HttpOnly: true})
		return write(w, M{"ok": true})
	})
	a.exchangeRoutes(m, reg)
	reg("GET /api/overview", a.overview)
	reg("POST /api/records/{id}/identity", a.identity)
	reg("POST /api/{resource}/export", a.export)
	reg("GET /api/{resource}", a.list)
	reg("GET /api/{resource}/{id}", a.detail)
	reg("GET /api/images/{id}/file", a.file)
	reg("POST /api/images", a.upload)
	reg("PATCH /api/images/{id}", a.linkImage)
	reg("POST /api/devices", a.saveDevice)
	reg("PUT /api/devices/{id}", a.saveDevice)
	reg("DELETE /api/{resource}/{id}", a.remove)
	m.HandleFunc("GET /VIID/", wrap(a.protocol))
	m.HandleFunc("POST /VIID/", wrap(a.protocol))
	m.HandleFunc("PUT /VIID/Subscribes/{subscription}", wrap(a.protocol))
	m.HandleFunc("GET /healthz", wrap(func(w http.ResponseWriter, r *http.Request) error {
		if e := a.db.Ping(r.Context()); e != nil {
			return e
		}
		return write(w, M{"status": "ok", "database": "postgresql"})
	}))
	m.Handle("GET /", http.FileServer(http.Dir("web/dist")))
	return m
}
func main() {
	demo := flag.Bool("demo", false, "populate example data once in an empty database")
	flag.Parse()
	_ = godotenv.Load()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	a := app{user: os.Getenv("VIID_USER"), password: os.Getenv("VIID_PASSWORD"), devicePassword: os.Getenv("VIID_DEVICE_PASSWORD"), key: make([]byte, 32)}
	a.serverID = os.Getenv("VIID_SERVER_ID")
	if a.serverID == "" {
		a.serverID = "00000000002000000001"
	}
	if len(a.serverID) != 20 || strings.Trim(a.serverID, "0123456789") != "" {
		log.Fatal("VIID_SERVER_ID 须为20位数字")
	}
	if a.user == "" {
		a.user = "admin"
	}
	a.allowIdentity = a.user == os.Getenv("VIID_IDENTITY_READER")
	if a.password == "" {
		a.password = "admin"
	}
	if a.devicePassword == "" || os.Getenv("DATABASE_URL") == "" {
		log.Fatal("请设置 DATABASE_URL、VIID_DEVICE_PASSWORD")
	}
	if _, e := rand.Read(a.key); e != nil {
		log.Fatal(e)
	}
	var err error
	a.peers, err = readPeers(os.Getenv("VIID_PEERS_FILE"))
	if err != nil {
		log.Fatal(err)
	}
	a.queryToken = os.Getenv("VIID_QUERY_TOKEN")
	a.strictModel = strings.EqualFold(os.Getenv("VIID_STRICT_2017"), "true")
	a.digestOnly = !strings.EqualFold(os.Getenv("VIID_DIGEST_ONLY"), "false")
	a.db, err = pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal("数据库连接配置无效")
	}
	defer a.db.Close()
	if _, err = a.db.Exec(ctx, schema); err != nil {
		log.Fatal(err)
	}
	if *demo {
		if err = a.seed(ctx); err != nil {
			log.Fatal(err)
		}
	}
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = "127.0.0.1:14000"
	}
	fmt.Println("VIID Hub listening on", addr)
	handler := a.routes()
	done := make(chan struct{})
	go func() {
		defer close(done)
		if len(a.peers) > 0 {
			a.runExchange(ctx)
		}
	}()
	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 90 * time.Second}
	go func() {
		<-ctx.Done()
		grace, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(grace)
	}()
	if err = server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	<-done
}
