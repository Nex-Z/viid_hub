package main

import (
	"crypto/rand"
	"github.com/icholy/digest"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (a *app) challenge(w http.ResponseWriter) error {
	seed := strconv.FormatInt(time.Now().Add(5*time.Minute).Unix(), 10) + "." + rand.Text()
	c := digest.Challenge{Realm: "VIID", Nonce: seed + "." + a.sign("device:"+seed), Algorithm: "MD5", QOP: []string{"auth"}}
	w.Header().Set("WWW-Authenticate", c.String())
	return problem{401, "设备认证失败"}
}

func (a *app) inboundSecret(id string) string {
	for _, p := range a.peers {
		if (p.Mode == "notify" || p.Mode == "lower") && p.Subscriber == id {
			return p.SubscriberPassword
		}
	}
	return a.devicePassword
}

func (a *app) deviceIdentity(w http.ResponseWriter, r *http.Request) (string, error) {
	id, pw, basic := r.BasicAuth()
	if basic && !a.digestOnly && !a.lowerIdentity(id) && id != "" && equal(pw, a.inboundSecret(id)) {
		if h := r.Header.Get("User-Identify"); h != "" && h != id {
			return "", bad("User-Identify 与认证身份不一致")
		}
		return id, nil
	}
	cred, err := digest.ParseCredentials(r.Header.Get("Authorization"))
	if err != nil || cred.Username == "" || cred.Realm != "VIID" || cred.URI != r.URL.RequestURI() || cred.Cnonce == "" || cred.QOP != "auth" || cred.Nc < 1 || (cred.Algorithm != "" && cred.Algorithm != "MD5") {
		return "", a.challenge(w)
	}
	parts := strings.Split(cred.Nonce, ".")
	if len(parts) != 3 {
		return "", a.challenge(w)
	}
	expiry, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || expiry <= time.Now().Unix() || !equal(parts[2], a.sign("device:"+parts[0]+"."+parts[1])) {
		return "", a.challenge(w)
	}
	c := digest.Challenge{Realm: "VIID", Nonce: cred.Nonce, Algorithm: "MD5", QOP: []string{"auth"}}
	expected, err := digest.Digest(&c, digest.Options{Username: cred.Username, Password: a.inboundSecret(cred.Username), Method: r.Method, URI: cred.URI, Cnonce: cred.Cnonce, Count: cred.Nc})
	if err != nil || !equal(expected.Response, cred.Response) {
		return "", a.challenge(w)
	}
	if r.Header.Get("User-Identify") != cred.Username {
		return "", bad("须使用与认证身份一致的 User-Identify")
	}
	ctx := r.Context()
	if _, err = a.db.Exec(ctx, "DELETE FROM digest_counters WHERE expires<=now()"); err != nil {
		return "", err
	}
	tag, err := a.db.Exec(ctx, "INSERT INTO digest_counters(nonce,identity,cnonce,nc,expires) VALUES($1,$2,$3,$4,$5) ON CONFLICT(nonce,identity,cnonce) DO UPDATE SET nc=excluded.nc WHERE digest_counters.nc<excluded.nc", cred.Nonce, cred.Username, cred.Cnonce, cred.Nc, time.Unix(expiry, 0))
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "", a.challenge(w)
	}
	return cred.Username, nil
}
