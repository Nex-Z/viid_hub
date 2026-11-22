package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// The independent Node peers exercise the wire protocol and share no Hub code or DB access.
func TestIndependent2017(t *testing.T) {
	pool := testDatabase(t)
	a := app{db: pool, key: []byte(rand.Text()), serverID: "50010300002000000001", digestOnly: true, strictModel: true,
		user: "sim", password: rand.Text(), devicePassword: rand.Text()}
	server := httptest.NewUnstartedServer(nil)
	if port := os.Getenv("VIID_ACCEPT_PORT"); port != "" {
		server.Listener.Close()
		l, err := net.Listen("tcp", "127.0.0.1:"+port)
		if err != nil {
			t.Fatal(err)
		}
		server.Listener = l
	}
	base := "http://" + server.Listener.Addr().String()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "../../scripts/interop2017.mjs", base)
	cmd.Env = []string{"SIM_ADMIN=" + a.password, "SIM_DEVICE=" + a.devicePassword}
	for _, key := range []string{"SYSTEMROOT", "PATH", "TEMP", "TMP", "HOME"} {
		cmd.Env = append(cmd.Env, key+"="+os.Getenv(key))
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	lines := bufio.NewScanner(stdout)
	if !lines.Scan() {
		_ = cmd.Wait()
		t.Fatal("simulator startup:", stderr.String())
	}
	var addr struct{ Upper, Lower string }
	if err = json.Unmarshal(lines.Bytes(), &addr); err != nil {
		t.Fatal(err)
	}
	a.peers = map[string]exchangePeer{
		"lower":      {Mode: "lower", URL: addr.Lower, User: a.serverID, Password: "to-peer", Subscriber: "50010400002000000001", SubscriberPassword: "from-lower", CallbackURL: base + "/VIID/SubscribeNotifications"},
		"upper":      {Mode: "viid", URL: addr.Upper, User: a.serverID, Password: "to-peer"},
		"subscriber": {Mode: "notify", URL: addr.Upper, User: a.serverID, Password: "to-peer", Subscriber: "50010500002000000001", SubscriberPassword: "from-upper"},
	}
	config, err := json.Marshal(a.peers)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "peers.json")
	if err = os.WriteFile(file, config, 0600); err != nil {
		t.Fatal(err)
	}
	a.peers, err = readPeers(file)
	if err != nil {
		t.Fatal(err)
	}
	server.Config.Handler = a.routes()
	server.Start()
	defer server.Close()
	worker, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); a.runExchange(worker) }()
	defer func() { stop(); <-done }()
	fmt.Fprintln(stdin, "ready")
	for lines.Scan() {
		if lines.Text() == "CHECKPOINT_REJECTED_NOTICE" {
			var notices, records, deliveries, links, newImages, boundImages int
			err := pool.QueryRow(ctx, `SELECT
				(SELECT count(*) FROM lower_notices),
				(SELECT count(*) FROM records),
				(SELECT count(*) FROM deliveries),
				(SELECT count(*) FROM viid_image_links),
				(SELECT count(*) FROM viid_image_info WHERE info IS NULL),
				(SELECT count(*) FROM images WHERE record_id IS NOT NULL)`).Scan(&notices, &records, &deliveries, &links, &newImages, &boundImages)
			if err != nil || notices+records+deliveries+links+newImages+boundImages != 0 {
				t.Errorf("rollback checkpoint notices=%d records=%d deliveries=%d links=%d metadata_placeholders=%d bound_images=%d err=%v", notices, records, deliveries, links, newImages, boundImages, err)
			} else {
				t.Log("post-403 DB checkpoint: notices=0 records=0 deliveries=0 links=0 metadata_placeholders=0 bound_images=0")
			}
			if _, err := fmt.Fprintln(stdin, "checked"); err != nil {
				t.Fatal(err)
			}
		}
		t.Log(lines.Text())
	}
	if err = cmd.Wait(); err != nil {
		t.Fatalf("independent simulator: %v\n%s", err, stderr.String())
	}
	var notices, records, photos int
	err = pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM lower_notices),(SELECT count(*) FROM records),(SELECT count(*) FROM images)").Scan(&notices, &records, &photos)
	if err != nil || notices != 1 || records != 5 || photos != 2 {
		t.Fatalf("notice/record/image counts %d/%d/%d: %v", notices, records, photos, err)
	}
	t.Logf("final DB counts notices=%d records=%d images=%d", notices, records, photos)
}
