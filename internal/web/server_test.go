package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/nicolaeser/discord-activity/internal/host"
	"github.com/nicolaeser/discord-activity/internal/store"
)

func TestDashboardLoginAndCRUD(t *testing.T) {
	t.Parallel()
	db, err := store.Open(filepath.Join(t.TempDir(), "data.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h := host.New(ctx, db, nil)
	srv, err := Start(Config{Addr: "127.0.0.1:0", Password: "secret", Store: db, Host: h})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sh, c := context.WithTimeout(context.Background(), time.Second)
		defer c()
		_ = srv.Shutdown(sh)
	})
	base := "http://" + srv.Addr()
	client := &http.Client{Timeout: 5 * time.Second}

	res, err := client.Get(base + "/health")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("health=%d", res.StatusCode)
	}

	res, err = client.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("ui=%d", res.StatusCode)
	}

	bad, err := client.Post(base+"/login", "application/json", bytes.NewBufferString(`{"password":"nope"}`))
	if err != nil {
		t.Fatal(err)
	}
	bad.Body.Close()
	if bad.StatusCode != 401 {
		t.Fatalf("bad login=%d", bad.StatusCode)
	}

	login, err := client.Post(base+"/login", "application/json", bytes.NewBufferString(`{"password":"secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	cookie := ""
	for _, c := range login.Cookies() {
		if c.Name == cookieName {
			cookie = c.Value
		}
	}
	login.Body.Close()
	if login.StatusCode != 200 || cookie == "" {
		t.Fatalf("login=%d cookie=%q", login.StatusCode, cookie)
	}

	req, _ := http.NewRequest(http.MethodPost, base+"/api/accounts", bytes.NewBufferString(`{"name":"main","token":"discord-token-value","enabled":true,"status":"dnd","activity_type":"playing","activity_name":"Go"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: cookieName, Value: cookie})
	res, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 201 {
		t.Fatalf("create=%d", res.StatusCode)
	}
	var created struct {
		Account store.Account `json:"account"`
	}
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Account.Token != "" || created.Account.TokenHint == "" || created.Account.Status != "dnd" {
		t.Fatalf("%+v", created.Account)
	}
}
