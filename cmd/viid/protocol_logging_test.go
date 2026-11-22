package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestProtocolRejectionTrace(t *testing.T) {
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previous) })
	a := app{devicePassword: rand.Text(), key: []byte(rand.Text()), serverID: "50010300002000000001"}
	server := httptest.NewServer(a.routes())
	defer server.Close()

	for _, tc := range []struct {
		path, body string
		status     int
		code       string
	}{
		{"/VIID/System/Time", "", 200, ""},
		{"/VIID/System/Register", "{", 400, "7"},
		{"/VIID/System/Register", `{"RegisterObject":{"DeviceID":"private-payload"}}`, 400, "8"},
		{"/VIID/private-path?private-query", "", 404, "4"},
	} {
		method := "GET"
		if tc.body != "" {
			method = "POST"
		}
		req, err := http.NewRequest(method, server.URL+tc.path, strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		req.SetBasicAuth("private-sender", a.devicePassword)
		req.Header.Set("User-Identify", "private-sender")
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != tc.status || res.Header.Get("Content-Type") != "application/VIID+JSON" {
			t.Fatalf("status or media type %d %q", res.StatusCode, res.Header.Get("Content-Type"))
		}
		if tc.status == 200 {
			continue
		}
		var reply struct {
			ResponseStatusObject struct {
				StatusCode any
				RequestURL string
				Id         string
			}
		}
		if err = json.Unmarshal(body, &reply); err != nil {
			t.Fatal(err)
		}
		wantPath, _, _ := strings.Cut(tc.path, "?")
		if fmt.Sprint(reply.ResponseStatusObject.StatusCode) != tc.code {
			t.Fatalf("VIID status %v, want %s", reply.ResponseStatusObject.StatusCode, tc.code)
		}
		if reply.ResponseStatusObject.RequestURL != wantPath || reply.ResponseStatusObject.Id != "private-sender" || reply.ResponseStatusObject.StatusCode == nil {
			t.Fatal("missing VIID rejection fields")
		}
		trace := res.Header.Get("X-Request-ID")
		if trace == "" || strings.Count(logs.String(), "request_id="+trace+" ") != 1 {
			t.Fatal("rejection response has no unique matching log entry")
		}
		t.Logf("matched response and log request_id=%s http=%d VIID=%s", trace, res.StatusCode, tc.code)
	}
	for _, secret := range []string{"private-", a.devicePassword, "Authorization"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("request data leaked to the rejection log")
		}
	}
}

func TestProtocolCaptureRollbackTrace(t *testing.T) {
	pool := testDatabase(t)
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previous) })
	a := app{db: pool, key: []byte(rand.Text()), devicePassword: rand.Text()}
	server := httptest.NewServer(a.routes())
	defer server.Close()

	post := func(path, body string, want int) string {
		t.Helper()
		req, err := http.NewRequest("POST", server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.SetBasicAuth("sim-device", a.devicePassword)
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != want {
			t.Fatalf("%s returned %d, want %d", path, res.StatusCode, want)
		}
		return res.Header.Get("X-Request-ID")
	}
	post("/VIID/System/Register", `{"RegisterObject":{"DeviceID":"sim-device"}}`, 200)
	trace := post("/VIID/Persons", `{"PersonListObject":{"PersonObject":[{"DeviceID":"sim-device","PersonID":"sim-record","PersonAppearTime":"20261121010101"},{"DeviceID":"sim-device","PersonID":""}]}}`, 400)
	if trace == "" || !strings.Contains(logs.String(), "request_id="+trace+" collection=Persons http=400 stage=capture item=2 transaction=rolled_back") {
		t.Fatal("missing correlated rollback for capture 2")
	}
	var rows int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM records").Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("records after failed batch = %d, error %v", rows, err)
	}
}

type rollbackResult struct {
	pgx.Tx
	err error
}

func (tx rollbackResult) Rollback(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return tx.err
}

func TestProtocolRollbackOutcome(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		stage   string
		dbError error
		want    string
	}{
		{"capture", nil, "rolled_back"},
		{"commit", pgx.ErrTxClosed, "closed"},
		{"response", pgx.ErrTxClosed, "committed"},
		{"capture", errors.New("connection lost"), "rollback_unconfirmed"},
	} {
		f := protocolBatchFailure{stage: tc.stage, item: 2}
		var err error = problem{403, "synthetic refusal"}
		f.finish(ctx, rollbackResult{err: tc.dbError}, &err)
		var p problem
		if !errors.As(err, &p) || p.status != 403 || f.transaction != tc.want {
			t.Fatalf("stage %s: transaction %q, error %v", tc.stage, f.transaction, err)
		}
	}
}
