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
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/icholy/digest"
)

func TestExchangeOverHTTP(t *testing.T) {
	ctx := context.Background()
	upper := app{db: testDatabase(t), key: []byte(rand.Text()), devicePassword: rand.Text(), digestOnly: true}
	upstream := httptest.NewServer(upper.routes())
	defer upstream.Close()
	var received atomic.Int32
	var requestKey atomic.Value
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer mock-token" {
			http.Error(w, "unexpected token", 403)
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if key == "" {
			t.Error("callback missing idempotency key")
		}
		if received.Add(1) == 1 {
			requestKey.Store(key)
			http.Error(w, "retry", 503)
			return
		}
		if requestKey.Load() != key {
			t.Error("retry key changed")
		}
		b, _ := io.ReadAll(r.Body)
		if !bytes.Contains(b, []byte("case-person")) || bytes.Contains(b, []byte("110101199001019999")) {
			t.Error("unexpected callback metadata")
		}
		w.WriteHeader(204)
	}))
	defer hook.Close()
	const platform = "50010300001320009999"
	const subscriber = "50010300001320008888"
	var notices atomic.Int32
	notifier := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/VIID/SubscribeNotifications" || r.Header.Get("User-Identify") != platform {
			t.Error("notification address or identity mismatch")
		}
		var packet struct {
			SubscribeNotificationListObject struct{ SubscribeNotificationObject []M }
		}
		if json.NewDecoder(r.Body).Decode(&packet) != nil || len(packet.SubscribeNotificationListObject.SubscribeNotificationObject) != 1 {
			http.Error(w, "invalid", 400)
			return
		}
		notice := packet.SubscribeNotificationListObject.SubscribeNotificationObject[0]
		id := field(notice, "NotificationID")
		if field(notice, "SubscribeID") != "sub-test" || field(notice, "InfoIDs") != "case-person" || notice["PersonObjectList"] == nil || len(id) != 33 {
			t.Error("invalid notification envelope")
		}
		notices.Add(1)
		_ = write(w, M{"ResponseStatusListObject": M{"ResponseStatusObject": []M{status(id, r.URL.Path)}}})
	}))
	defer notifier.Close()
	peers := map[string]exchangePeer{
		"notice": {Mode: "notify", URL: notifier.URL, User: platform, Password: "callback-test", Subscriber: subscriber, SubscriberPassword: "subscriber-test"},
		"upper":  {Mode: "viid", URL: upstream.URL, LegacySourceRewrite: true, User: platform, Password: upper.devicePassword},
		"hook":   {Mode: "webhook", URL: hook.URL, Token: "mock-token"},
	}
	a := app{db: testDatabase(t), key: []byte(rand.Text()), devicePassword: rand.Text(), digestOnly: true, peers: peers, queryToken: rand.Text(), serverID: platform, user: "operator", password: rand.Text()}
	local := httptest.NewServer(a.routes())
	defer local.Close()
	send := func(method, path string, body any, auth string, cookie *http.Cookie, want int) *http.Response {
		t.Helper()
		b, _ := json.Marshal(body)
		req, err := http.NewRequest(method, local.URL+path, bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", auth)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		res, err := local.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { res.Body.Close() })
		if res.StatusCode != want {
			t.Fatalf("%s: got %d want %d", path, res.StatusCode, want)
		}
		return res
	}
	login := send("POST", "/api/login", M{"user": a.user, "password": a.password}, "", nil, 200)
	cookie := login.Cookies()[0]
	for _, name := range []string{"upper", "hook"} {
		send("POST", "/api/exchange/subscriptions", M{"target": name, "kind": "person"}, "", cookie, 200)
	}
	sub := newSender(exchangePeer{Mode: "viid", URL: local.URL, User: subscriber, Password: "subscriber-test"})
	if e := sub.maintain(ctx); e != nil {
		t.Fatal(e)
	}
	task := M{"SubscribeID": "sub-test", "Title": "人员采集", "ApplicantName": "模拟用户", "ApplicantOrg": "测试单位", "SubscribeDetail": "11", "OperateType": 0, "ResourceURI": platform, "BeginTime": stamp(time.Now().Add(-time.Hour)), "EndTime": stamp(time.Now().Add(time.Hour)), "ReceiveAddr": notifier.URL + "/VIID/SubscribeNotifications"}
	taskBody := M{"SubscribeListObject": M{"SubscribeObject": []M{task}}}
	for i := 0; i < 2; i++ {
		if _, e := sub.request(ctx, "POST", "/VIID/Subscribes", taskBody, ""); e != nil {
			t.Fatal(e)
		}
	}
	sender := newSender(exchangePeer{Mode: "viid", URL: local.URL, User: "50010300001320000123", Password: a.devicePassword})
	if e := sender.maintain(ctx); e != nil {
		t.Fatal(e)
	}
	if e := sender.system(ctx, "Keepalive"); e != nil {
		t.Fatal(e)
	}
	var photo bytes.Buffer
	if e := png.Encode(&photo, image.NewRGBA(image.Rect(0, 0, 2, 2))); e != nil {
		t.Fatal(e)
	}
	payload := M{"PersonListObject": M{"PersonObject": []M{{"PersonID": "case-person", "DeviceID": sender.peer.User, "ShotTime": "20250120143752", "IDNumber": "110101199001019999", "SubImageList": M{"SubImageInfoObject": []M{{"ImageID": "case-photo", "FileFormat": "PNG", "Data": base64.StdEncoding.EncodeToString(photo.Bytes())}}}}}}}
	for i := 0; i < 2; i++ {
		if _, err := sender.request(ctx, "POST", "/VIID/Persons", payload, ""); err != nil {
			t.Fatal(err)
		}
	}
	receivers := map[string]*exchangeSender{}
	for name, p := range peers {
		receivers[name] = newSender(p)
	}
	for i := 0; i < 3; i++ {
		if ok, err := a.deliverOne(ctx, receivers); err != nil || !ok {
			t.Fatalf("delivery %d: %v", i, err)
		}
	}
	if _, err := a.db.Exec(ctx, "UPDATE deliveries SET due=now() WHERE state='pending'"); err != nil {
		t.Fatal(err)
	}
	if ok, err := a.deliverOne(ctx, receivers); err != nil || !ok {
		t.Fatalf("retry: %v", err)
	}
	var count int
	if e := a.db.QueryRow(ctx, "SELECT count(*) FROM deliveries WHERE state='sent'").Scan(&count); e != nil || count != 3 {
		var notes string
		_ = a.db.QueryRow(ctx, "SELECT string_agg(message,',') FROM deliveries").Scan(&notes)
		t.Fatalf("expected both deliveries confirmed: %d %s %v", count, notes, e)
	}
	if notices.Load() != 1 {
		t.Fatal("missing or duplicate notification")
	}
	if _, err := sub.request(ctx, "PUT", "/VIID/Subscribes/sub-test", M{"SubscribeObject": M{"SubscribeID": "sub-test", "OperateType": 1, "CancelReason": "测试结束"}}, ""); err != nil {
		t.Fatal(err)
	}
	var enabled bool
	if err := a.db.QueryRow(ctx, "SELECT enabled FROM subscriptions WHERE external_id='sub-test'").Scan(&enabled); err != nil || enabled {
		t.Fatal("cancel not persisted", err)
	}
	queried, err := sub.request(ctx, "GET", "/VIID/Persons?(PersonID=case-person)&(PageRecordNum=1)&(RecordStartNo=1)", nil, "")
	if err != nil || !bytes.Contains(queried, []byte("110********9999")) {
		t.Fatalf("registered subscriber query: %v / %s", err, queried)
	}
	var content []byte
	var source string
	if e := upper.db.QueryRow(ctx, "SELECT content,raw->>'SourceDeviceID' FROM images i JOIN records r ON r.id=i.record_id WHERE i.id='case-photo' AND r.device_id=$1", platform).Scan(&content, &source); e != nil || !bytes.Equal(content, photo.Bytes()) || source != sender.peer.User {
		t.Fatalf("cascade content/source: %v", e)
	}
	send("GET", "/exchange/records", nil, "", nil, 401)
	response := send("GET", "/exchange/records?kind=person", nil, "Bearer "+a.queryToken, nil, 200)
	b, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(b), "110********9999") || strings.Contains(string(b), "110101199001019999") {
		t.Fatal("consumer query not redacted")
	}
	response = send("GET", "/exchange/images/case-photo/file", nil, "Bearer "+a.queryToken, nil, 200)
	b, _ = io.ReadAll(response.Body)
	if !bytes.Equal(b, photo.Bytes()) {
		t.Fatal("consumer photo mismatch")
	}
	response = send("GET", "/VIID/System/Time", nil, "", nil, 401)
	challenge, err := digest.FindChallenge(response.Header)
	if err != nil {
		t.Fatal(err)
	}
	path := "/VIID/System/Time"
	cred, err := digest.Digest(challenge, digest.Options{Username: sender.peer.User, Password: a.devicePassword, Method: "GET", URI: path, Count: 1, Cnonce: "test-client"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []int{200, 401} {
		req, _ := http.NewRequest("GET", local.URL+path, nil)
		req.Header.Set("User-Identify", sender.peer.User)
		req.Header.Set("Authorization", cred.String())
		res, err := local.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != want {
			t.Fatalf("digest nonce replay got %d want %d", res.StatusCode, want)
		}
	}
	receivers["upper"].heartbeat = time.Now().Add(-2 * time.Minute)
	if e := receivers["upper"].maintain(ctx); e != nil {
		t.Fatal(e)
	}
	if e := sender.system(ctx, "UnRegister"); e != nil {
		t.Fatal(e)
	}
	if _, e := sender.request(ctx, "POST", "/VIID/Persons", payload, ""); e == nil {
		t.Fatal("upload accepted after unregister")
	}
}
