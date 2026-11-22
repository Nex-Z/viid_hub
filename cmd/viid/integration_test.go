package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Run with VIID_TEST_DATABASE_URL. Each run owns a separate PostgreSQL schema.
func testDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("VIID_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set VIID_TEST_DATABASE_URL for PostgreSQL integration tests")
	}
	ctx := context.Background()
	db, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(db.Close)
	name := "viid_test_" + strings.ToLower(rand.Text())
	quoted := pgx.Identifier{name}.Sanitize()
	if _, e = db.Exec(ctx, "CREATE SCHEMA "+quoted); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if _, e = db.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE"); e != nil {
			t.Error(e)
		}
	})
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = name
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	if _, e = pool.Exec(ctx, schema); e != nil {
		t.Fatal(e)
	}
	return pool
}

func TestPostgresLifecycle(t *testing.T) {
	ctx := context.Background()
	pool := testDatabase(t)
	var e error
	a := app{db: pool, user: "operator", password: rand.Text(), devicePassword: rand.Text(), key: []byte(rand.Text())}
	server := httptest.NewServer(a.routes())
	defer server.Close()
	type reply struct {
		*http.Response
		body *bytes.Buffer
	}
	send := func(req *http.Request, want int) *reply {
		t.Helper()
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != want {
			t.Fatalf("%s %s: got %d want %d: %s", req.Method, req.URL.Path, response.StatusCode, want, body)
		}
		return &reply{response, bytes.NewBuffer(body)}
	}
	var session *http.Cookie
	call := func(method, path string, payload any, device string, want int) *reply {
		t.Helper()
		b, e := json.Marshal(payload)
		if e != nil {
			t.Fatal(e)
		}
		q, err := http.NewRequest(method, server.URL+path, bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		if device != "" {
			q.SetBasicAuth(device, a.devicePassword)
		}
		if session != nil {
			q.AddCookie(session)
		}
		return send(q, want)
	}
	count := func(table string, want int) {
		t.Helper()
		var n int
		if e := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); e != nil || n != want {
			t.Fatalf("%s count: got %d want %d, %v", table, n, want, e)
		}
	}
	call("GET", "/api/records", nil, "", 401)
	call("POST", "/api/login", M{"User": "operator", "Password": "wrong"}, "", 401)
	w := call("POST", "/api/login", M{"User": "operator", "Password": a.password}, "", 200)
	session = w.Cookies()[0]
	device := "50010300001320000099"
	lifecycle := func(op string, code int) {
		t.Helper()
		call("POST", "/VIID/System/"+op, M{op + "Object": M{"DeviceID": device}}, device, code)
	}
	lifecycle("Keepalive", 403)
	lifecycle("Register", 200)
	lifecycle("Keepalive", 200)
	call("GET", "/VIID/System/Time", nil, device, 200)
	var photo bytes.Buffer
	if e = png.Encode(&photo, image.NewRGBA(image.Rect(0, 0, 3, 4))); e != nil {
		t.Fatal(e)
	}
	const identityNumber = "110101199001019999"
	const historic = "20240505112030"
	const photoTime = "20240505112029"
	for path, typ := range types {
		id := typ.kind + "-test"
		obj := M{typ.id: id, "DeviceID": device, "ShotTime": historic, "Name": "测试", "IDNumber": identityNumber, "Extension": []M{{"idnumber": identityNumber}}, "SubImageList": M{"SubImageInfoObject": []M{{"ImageID": "image-" + id, "FileFormat": "PNG", "ShotTime": photoTime, "Data": base64.StdEncoding.EncodeToString(photo.Bytes())}}}}
		envelope := func(objects ...M) M { return M{typ.object + "ListObject": M{typ.object + "Object": objects}} }
		call("POST", "/VIID/"+path, envelope(obj), device, 200)
		call("POST", "/VIID/"+path, envelope(obj), device, 200)
		own := call("GET", "/VIID/"+path, nil, device, 200)
		if !strings.Contains(own.body.String(), identityNumber) {
			t.Fatal("device lost its original payload")
		}
		conflict := M{}
		for k, v := range obj {
			conflict[k] = v
		}
		conflict["Name"] = "冲突"
		call("POST", "/VIID/"+path, envelope(conflict), device, 409)
		invalid := M{typ.id: "extra-" + id, "DeviceID": device, "ShotTime": "invalid"}
		fresh := M{typ.id: "rollback-" + id, "DeviceID": device, "ShotTime": stamp(time.Now())}
		call("POST", "/VIID/"+path, envelope(fresh, invalid), device, 400)
		detail := call("GET", "/api/records/"+id, nil, "", 200)
		if !strings.Contains(detail.body.String(), "image-"+id) || strings.Contains(detail.body.String(), base64.StdEncoding.EncodeToString(photo.Bytes())) {
			t.Fatal("missing image or inline base64 retained")
		}
	}
	count("records", 4)
	count("images", 4)
	var shot, ingest time.Time
	if e = pool.QueryRow(ctx, "SELECT shot_at,ingested_at FROM images WHERE id='image-person-test'").Scan(&shot, &ingest); e != nil || stamp(shot) != photoTime || time.Since(ingest) > time.Minute {
		t.Fatalf("unexpected capture/receipt %v %v %v", shot, ingest, e)
	}
	for _, result := range []*reply{
		call("GET", "/api/records", nil, "", 200),
		call("GET", "/api/records/person-test", nil, "", 200),
		call("POST", "/api/records/export", M{"ids": []string{"person-test"}}, "", 200),
	} {
		if strings.Contains(result.body.String(), identityNumber) || !strings.Contains(result.body.String(), "110********9999") {
			t.Fatal("management identity redaction failed")
		}
	}
	call("POST", "/api/records/person-test/identity", nil, "", 403)
	authorized := a
	authorized.allowIdentity = true
	readerServer := httptest.NewServer(authorized.routes())
	defer readerServer.Close()
	revealReq, err := http.NewRequest("POST", readerServer.URL+"/api/records/person-test/identity", nil)
	if err != nil {
		t.Fatal(err)
	}
	revealReq.AddCookie(session)
	clearID := send(revealReq, 200)
	if !strings.Contains(clearID.body.String(), identityNumber) || !strings.Contains(clearID.Header.Get("Cache-Control"), "no-store") {
		t.Fatal("explicit reveal failed")
	}
	call("GET", "/api/images/image-person-test/file?download=1", nil, "", 200)
	if _, e = pool.Exec(ctx, "UPDATE devices SET heartbeat=now()-interval '6 minutes' WHERE id=$1", device); e != nil {
		t.Fatal(e)
	}
	delayed := M{"PersonListObject": M{"PersonObject": []M{{"PersonID": "delayed", "DeviceID": device, "ShotTime": historic}}}}
	call("POST", "/VIID/Persons", delayed, device, 403)
	call("GET", "/VIID/Persons", nil, device, 403)
	lifecycle("Register", 200)
	lifecycle("Keepalive", 200)
	call("POST", "/VIID/Persons", delayed, device, 200)
	call("POST", "/VIID/Persons", delayed, device, 200)
	count("records WHERE id='delayed'", 1)
	call("DELETE", "/api/records/delayed", nil, "", 200)
	w = call("GET", "/api/records?kind=person&device="+device+"&q="+url.QueryEscape("测试"), nil, "", 200)
	var list struct{ Total int }
	if e = json.Unmarshal(w.body.Bytes(), &list); e != nil || list.Total != 1 {
		t.Fatal("record query", e, list)
	}
	w = call("GET", "/api/images/image-person-test/file", nil, "", 200)
	if !bytes.Equal(w.body.Bytes(), photo.Bytes()) {
		t.Fatal("image payload mismatch")
	}
	var body bytes.Buffer
	mp := multipart.NewWriter(&body)
	part, e := mp.CreateFormFile("file", "manual.png")
	if e != nil {
		t.Fatal(e)
	}
	part.Write(photo.Bytes())
	mp.Close()
	req, err := http.NewRequest("POST", server.URL+"/api/images", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mp.FormDataContentType())
	req.AddCookie(session)
	w = send(req, 200)
	var upload struct{ ID string }
	if e = json.Unmarshal(w.body.Bytes(), &upload); e != nil || upload.ID == "" {
		t.Fatal("upload", e)
	}
	count("images WHERE shot_at IS NULL AND ingested_at IS NOT NULL", 1)
	call("PATCH", "/api/images/"+upload.ID, M{"Record": "person-test"}, "", 200)
	call("GET", "/api/images?record=person-test", nil, "", 200)
	call("DELETE", "/api/records/person-test", nil, "", 200)
	count("images", 5)
	count("records", 3)
	call("GET", "/api/overview?hours=168", nil, "", 200)
	for _, action := range []string{"view", "reveal_denied", "reveal", "export", "download", "delete"} {
		var exists bool
		if e = pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM operation_audit WHERE actor='operator' AND action=$1)", action).Scan(&exists); e != nil || !exists {
			t.Fatalf("missing audit %s: %v", action, e)
		}
	}
	audit := call("GET", "/api/audit", nil, "", 200)
	if strings.Contains(audit.body.String(), identityNumber) {
		t.Fatal("identity in audit")
	}
	if _, e = pool.Exec(ctx, "INSERT INTO devices(id,name) SELECT 'extra-'||lpad(n::text,3,'0'),'设备'||n FROM generate_series(1,210) n"); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, "INSERT INTO records(id,kind,device_id,captured,title,raw) SELECT 'new-'||n,'motor',$1,now(),'测试','{}'::jsonb FROM generate_series(1,24) n", device); e != nil {
		t.Fatal(e)
	}
	checkTotal := func(path string, want int) {
		t.Helper()
		b := call("GET", path, nil, "", 200)
		var r struct{ Total int }
		if err := json.Unmarshal(b.body.Bytes(), &r); err != nil || r.Total != want {
			t.Fatalf("%s: expected %d, got %d (%v)", path, want, r.Total, err)
		}
	}
	checkTotal("/api/devices?q=extra-210", 1)
	checkTotal("/api/devices?status=alerts&page=2&size=15", 210)
	checkTotal("/api/records?kind=face&device="+device+"&start=20240505000000&end=20240506000000", 1)
	checkTotal("/api/records?kind=motor&page=2&size=15", 25)
	checkTotal("/api/images?start=20240505000000&end=20240506000000", 0)
	checkTotal("/api/images?timeField=shot&start=20240505000000&end=20240506000000", 4)
	if _, e = pool.Exec(ctx, schema); e != nil {
		t.Fatalf("schema replay: %v", e)
	}
	lifecycle("UnRegister", 200)
	lifecycle("Keepalive", 403)
	call("PUT", "/api/devices/"+device, M{"Name": "已停用设备", "Enabled": false}, "", 200)
	lifecycle("Register", 403)
}
