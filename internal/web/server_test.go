package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/nicolaeser/discordactivity/internal/host"
	"github.com/nicolaeser/discordactivity/internal/store"
)

func TestDashboardLoginAndCRUD(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "secret")

	res, err := e.client.Get(e.base + "/health")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("health=%d", res.StatusCode)
	}

	res, err = e.client.Get(e.base + "/")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("ui=%d", res.StatusCode)
	}

	bad := e.do(http.MethodPost, "/login", `{"password":"nope"}`, nil)
	bad.Body.Close()
	if bad.StatusCode != 401 {
		t.Fatalf("bad login=%d", bad.StatusCode)
	}

	cookie := e.login("secret")
	created := e.json(http.MethodPost, "/api/accounts", `{"name":"main","token":"discord-token-value","enabled":false,"status":"dnd","activity_type":"playing","activity_name":"Go"}`, 201, map[string]string{}, cookie)
	acc := created["account"].(map[string]any)
	if acc["token"] != nil && acc["token"] != "" || acc["token_hint"] == nil || acc["status"] != "dnd" {
		t.Fatalf("%+v", acc)
	}
}

func TestBearerAPIKeyStartStopReconnect(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "secret")
	hdr := map[string]string{"Authorization": "Bearer secret"}
	created := e.json(http.MethodPost, "/api/v1/accounts", `{"name":"main","token":"tok-api-key-1","enabled":false,"status":"online"}`, 201, hdr)
	id := int(created["account"].(map[string]any)["id"].(float64))
	path := "/api/v1/accounts/" + strconv.Itoa(id)

	start := e.json(http.MethodPost, path+"/start", `{}`, 200, hdr)
	if start["ok"] != true || !start["account"].(map[string]any)["enabled"].(bool) {
		t.Fatalf("start %+v", start)
	}

	rec := e.json(http.MethodPost, path+"/reconnect", `{}`, 200, hdr)
	if rec["ok"] != true {
		t.Fatalf("reconnect %+v", rec)
	}

	stop := e.json(http.MethodPost, path+"/stop", `{}`, 200, hdr)
	if stop["ok"] != true || stop["account"].(map[string]any)["enabled"].(bool) {
		t.Fatalf("stop %+v", stop)
	}

	e.json(http.MethodGet, "/api/v1/accounts", "", 200, map[string]string{"X-API-Key": "secret"})
	e.status(http.MethodGet, "/api/v1/accounts", "", 401, map[string]string{"Authorization": "Bearer nope"})
	e.status(http.MethodGet, "/api/accounts", "", 401, nil)
}

func TestPutPreservesEnabled(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "secret")
	hdr := map[string]string{"Authorization": "Bearer secret"}
	created := e.json(http.MethodPost, "/api/v1/accounts", `{"name":"main","token":"tok-put-enabled","status":"online"}`, 201, hdr)
	id := int(created["account"].(map[string]any)["id"].(float64))
	path := "/api/v1/accounts/" + strconv.Itoa(id)
	e.json(http.MethodPost, path+"/start", `{}`, 200, hdr)
	got := e.json(http.MethodPut, path, `{"name":"main","status":"online","activity_type":"playing","activity_name":"Go"}`, 200, hdr)
	acc := got["account"].(map[string]any)
	if acc["enabled"] != true || acc["activity_name"] != "Go" {
		t.Fatalf("enabled dropped: %+v", acc)
	}
}

func TestCatalogMetaAndStatusPatch(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "secret")
	cat := e.json(http.MethodGet, "/api/v1", "", 200, nil)
	if cat["ok"] != true || cat["service"] != "DiscordActivity" {
		t.Fatalf("%v", cat)
	}
	meta := e.json(http.MethodGet, "/api/v1/meta", "", 200, nil)
	if meta["ok"] != true {
		t.Fatalf("%v", meta)
	}
	hdr := map[string]string{"Authorization": "Bearer secret"}
	created := e.json(http.MethodPost, "/api/v1/accounts", `{"name":"main","token":"tok-patch-1","status":"online","activity_type":"playing","activity_name":"Go"}`, 201, hdr)
	id := int(created["account"].(map[string]any)["id"].(float64))
	path := "/api/v1/accounts/" + strconv.Itoa(id)
	st := e.json(http.MethodPut, path+"/status", `{"status":"idle"}`, 200, hdr)
	acc := st["account"].(map[string]any)
	if acc["status"] != "idle" || acc["activity_name"] != "Go" {
		t.Fatalf("status patch %+v", acc)
	}
	patched := e.json(http.MethodPatch, path, `{"status":"dnd","details":"coding"}`, 200, hdr)
	acc = patched["account"].(map[string]any)
	if acc["status"] != "dnd" || acc["details"] != "coding" || acc["activity_name"] != "Go" {
		t.Fatalf("merge %+v", acc)
	}
}

func TestHealthStaysUpWhenDisconnected(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "secret")
	cookie := e.login("secret")
	e.json(http.MethodPost, "/api/accounts", `{"name":"main","token":"tok-health-1","enabled":true,"status":"online"}`, 201, nil, cookie)
	got := e.json(http.MethodGet, "/health", "", 200, nil)
	if got["ok"] != true {
		t.Fatalf("%v", got)
	}
	if err := Probe(e.addr); err != nil {
		t.Fatal(err)
	}
}

type env struct {
	t      *testing.T
	base   string
	addr   string
	client *http.Client
	host   *host.Host
}

func newEnv(t *testing.T, password string) *env {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "data.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	h := host.New(ctx, db, nil)
	srv, err := Start(Config{Addr: "127.0.0.1:0", Password: password, Store: db, Host: h})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sh, c := context.WithTimeout(context.Background(), 2*time.Second)
		defer c()
		_ = srv.Shutdown(sh)
	})
	return &env{t: t, base: "http://" + srv.Addr(), addr: srv.Addr(), client: &http.Client{Timeout: 5 * time.Second}, host: h}
}

func (e *env) login(password string) *http.Cookie {
	e.t.Helper()
	res := e.do(http.MethodPost, "/login", `{"password":"`+password+`"}`, nil)
	defer res.Body.Close()
	if res.StatusCode != 200 {
		e.t.Fatalf("login=%d", res.StatusCode)
	}
	for _, c := range res.Cookies() {
		if c.Name == cookieName && c.Value != "" {
			return c
		}
	}
	e.t.Fatal("missing cookie")
	return nil
}

func (e *env) do(method, path, body string, hdr map[string]string, cookies ...*http.Cookie) *http.Response {
	e.t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = bytes.NewBufferString(body)
	}
	req, err := http.NewRequest(method, e.base+path, rdr)
	if err != nil {
		e.t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	res, err := e.client.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	return res
}

func (e *env) status(method, path, body string, want int, hdr map[string]string, cookies ...*http.Cookie) {
	e.t.Helper()
	res := e.do(method, path, body, hdr, cookies...)
	defer res.Body.Close()
	if res.StatusCode != want {
		raw, _ := io.ReadAll(res.Body)
		e.t.Fatalf("%s %s status=%d want=%d body=%s", method, path, res.StatusCode, want, raw)
	}
}

func (e *env) json(method, path, body string, want int, hdr map[string]string, cookies ...*http.Cookie) map[string]any {
	e.t.Helper()
	res := e.do(method, path, body, hdr, cookies...)
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != want {
		e.t.Fatalf("%s %s status=%d want=%d body=%s", method, path, res.StatusCode, want, raw)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		e.t.Fatalf("json: %v body=%s", err, raw)
	}
	return out
}
