package web

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nicolaeser/discordactivity/internal/gateway"
	"github.com/nicolaeser/discordactivity/internal/host"
	"github.com/nicolaeser/discordactivity/internal/presence"
	"github.com/nicolaeser/discordactivity/internal/store"
)

//go:embed static/index.html
var staticFS embed.FS

const cookieName = "da_session"

type Server struct {
	http     *http.Server
	addr     string
	password string
	store    *store.Store
	host     *host.Host
	log      *slog.Logger
	mu       sync.Mutex
	sessions map[string]time.Time
}

type Config struct {
	Addr, Password string
	Store          *store.Store
	Host           *host.Host
	Log            *slog.Logger
}

func Start(cfg Config) (*Server, error) {
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	s := &Server{password: cfg.Password, store: cfg.Store, host: cfg.Host, log: cfg.Log, sessions: map[string]time.Time{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/logout", s.handleLogout)
	mux.HandleFunc("/api/v1", s.handleCatalog)
	mux.HandleFunc("/api/v1/", s.handleCatalog)
	mux.HandleFunc("/api/v1/login", s.handleLogin)
	mux.HandleFunc("/api/v1/logout", s.handleLogout)
	mux.HandleFunc("/api/v1/meta", s.handleMeta)
	mux.HandleFunc("/api/v1/status", s.handleStatus)
	mux.HandleFunc("/api/v1/accounts", s.handleAccounts)
	mux.HandleFunc("/api/v1/accounts/", s.handleAccount)
	mux.HandleFunc("/api/accounts", s.handleAccounts)
	mux.HandleFunc("/api/accounts/", s.handleAccount)
	mux.HandleFunc("/", s.handleIndex)
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return nil, err
	}
	s.addr = ln.Addr().String()
	s.http = &http.Server{Addr: s.addr, Handler: s.headers(mux), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := s.http.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.log.Error("http", "error", err)
		}
	}()
	return s, nil
}

func (s *Server) headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) Addr() string { return s.addr }

func (s *Server) Shutdown(ctx context.Context) error {
	if s == nil || s.http == nil {
		return nil
	}
	return s.http.Shutdown(ctx)
}

func Probe(addr string) error {
	addr = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(addr, "http://"), "https://"))
	if addr == "" {
		addr = ":8080"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		if strings.HasPrefix(addr, ":") {
			addr = "127.0.0.1" + addr
		} else {
			addr = "127.0.0.1:8080"
		}
	} else {
		if host == "" || host == "0.0.0.0" || host == "::" {
			host = "127.0.0.1"
		}
		addr = net.JoinHostPort(host, port)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	res, err := client.Get("http://" + addr + "/health")
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("health: %s", res.Status)
	}
	return nil
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" || r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	body, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "missing ui", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(body)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": "DiscordActivity"})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if !s.authed(w, r) {
		return
	}
	accounts := []host.Status{}
	if s.host != nil {
		if list := s.host.Health(); list != nil {
			accounts = list
		}
	}
	ok := true
	for _, a := range accounts {
		if a.Enabled && !a.Connected {
			ok = false
			break
		}
	}
	status := http.StatusOK
	if !ok {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{"ok": ok, "accounts": accounts})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if s.password == "" {
		writeError(w, http.StatusServiceUnavailable, "auth_unconfigured", "DASHBOARD_PASSWORD is not set")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	time.Sleep(120 * time.Millisecond)
	if !secretEqual(s.password, body.Password) {
		writeError(w, http.StatusUnauthorized, "invalid_password", "invalid password")
		return
	}
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	exp := time.Now().Add(24 * time.Hour)
	s.mu.Lock()
	s.sessions[id] = exp
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: id, Path: "/", Expires: exp, MaxAge: int(time.Until(exp).Seconds()), HttpOnly: true, SameSite: http.SameSiteLaxMode})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		s.mu.Lock()
		delete(s.sessions, c.Value)
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	if !s.authed(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		accounts, err := s.store.List()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "internal error")
			return
		}
		out := make([]view, 0, len(accounts))
		for _, a := range accounts {
			out = append(out, toView(a, s.host.Runtime(a.ID)))
		}
		writeJSON(w, http.StatusOK, map[string]any{"accounts": out})
	case http.MethodPost:
		var a store.Account
		if err := readJSON(r, &a); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
			return
		}
		if strings.TrimSpace(a.Token) == "" {
			writeError(w, http.StatusBadRequest, "token_required", "token is required")
			return
		}
		a.Enabled = false
		if _, err := a.Presence(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_presence", err.Error())
			return
		}
		saved, err := s.store.Insert(a)
		if err != nil {
			if isUnique(err) {
				writeError(w, http.StatusConflict, "token_conflict", "token already exists")
				return
			}
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		s.host.Apply(saved)
		writeJSON(w, http.StatusCreated, map[string]any{"account": toView(saved, s.host.Runtime(saved.ID))})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (s *Server) handleAccount(w http.ResponseWriter, r *http.Request) {
	if !s.authed(w, r) {
		return
	}
	id, action, ok := parseAccountPath(r.URL.Path)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid id")
		return
	}
	if action != "" {
		s.handleControl(w, r, id, action)
		return
	}
	switch r.Method {
	case http.MethodPatch:
		s.patchAccount(w, r, id)
		return
	case http.MethodGet:
		acc, err := s.store.Get(id)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"account": toView(acc, s.host.Runtime(acc.ID))})
	case http.MethodPut:
		cur, err := s.store.Get(id)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "not found")
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		r.Body.Close()
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
			return
		}
		var a store.Account
		if err := json.Unmarshal(raw, &a); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
			return
		}
		a.ID = id
		var keys map[string]json.RawMessage
		_ = json.Unmarshal(raw, &keys)
		if _, ok := keys["enabled"]; !ok {
			a.Enabled = cur.Enabled
		}
		if _, err := a.Presence(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_presence", err.Error())
			return
		}
		saved, err := s.store.Update(a)
		if err != nil {
			if isUnique(err) {
				writeError(w, http.StatusConflict, "token_conflict", "token already exists")
				return
			}
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		s.host.Apply(saved)
		writeJSON(w, http.StatusOK, map[string]any{"account": toView(saved, s.host.Runtime(saved.ID))})
	case http.MethodDelete:
		if err := s.store.Delete(id); err != nil {
			writeError(w, http.StatusNotFound, "not_found", "not found")
			return
		}
		s.host.Stop(id)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (s *Server) handleControl(w http.ResponseWriter, r *http.Request, id int64, action string) {
	switch action {
	case "start", "stop", "reconnect":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		var err error
		switch action {
		case "start":
			err = s.host.Start(id)
		case "stop":
			err = s.host.StopAccount(id)
		case "reconnect":
			err = s.host.Restart(id)
		}
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "not found")
			return
		}
		s.writeAccount(w, id, true)
	case "status":
		if r.Method != http.MethodPut && r.Method != http.MethodPatch && r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		s.patchStatus(w, r, id)
	case "presence":
		if r.Method != http.MethodPut && r.Method != http.MethodPatch && r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		s.patchAccount(w, r, id)
	default:
		writeError(w, http.StatusNotFound, "not_found", "not found")
	}
}

func (s *Server) handleCatalog(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/v1" && r.URL.Path != "/api/v1/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"service": "DiscordActivity",
		"version": "v1",
		"api":     "v1",
		"docs":    "The dashboard password is the API key. Send it as Authorization: Bearer or X-API-Key.",
		"authentication": map[string]any{
			"schemes": []map[string]string{
				{"type": "http", "scheme": "bearer", "header": "Authorization", "format": "Bearer <DASHBOARD_PASSWORD>"},
				{"type": "apiKey", "in": "header", "name": "X-API-Key"},
				{"type": "cookie", "name": "da_session", "from": "POST /api/v1/login"},
			},
		},
		"resources": []map[string]string{
			{"method": "GET", "path": "/health", "auth": "none", "description": "Process liveness"},
			{"method": "GET", "path": "/api/v1", "auth": "none", "description": "API catalog"},
			{"method": "GET", "path": "/api/v1/meta", "auth": "none", "description": "Statuses and activity types"},
			{"method": "POST", "path": "/api/v1/login", "auth": "none", "description": "Exchange password for a session cookie"},
			{"method": "POST", "path": "/api/v1/logout", "auth": "none", "description": "Clear the session cookie"},
			{"method": "GET", "path": "/api/v1/status", "auth": "required", "description": "Gateway health for all accounts"},
			{"method": "GET", "path": "/api/v1/accounts", "auth": "required", "description": "List accounts and runtime"},
			{"method": "POST", "path": "/api/v1/accounts", "auth": "required", "description": "Create an account (starts stopped)"},
			{"method": "GET", "path": "/api/v1/accounts/{id}", "auth": "required", "description": "Get one account"},
			{"method": "PUT", "path": "/api/v1/accounts/{id}", "auth": "required", "description": "Replace account and presence"},
			{"method": "PATCH", "path": "/api/v1/accounts/{id}", "auth": "required", "description": "Merge account fields"},
			{"method": "DELETE", "path": "/api/v1/accounts/{id}", "auth": "required", "description": "Delete and disconnect"},
			{"method": "PUT", "path": "/api/v1/accounts/{id}/status", "auth": "required", "description": "Set Discord status immediately"},
			{"method": "PUT", "path": "/api/v1/accounts/{id}/presence", "auth": "required", "description": "Merge presence fields"},
			{"method": "POST", "path": "/api/v1/accounts/{id}/start", "auth": "required", "description": "Enable and keep the gateway socket open"},
			{"method": "POST", "path": "/api/v1/accounts/{id}/stop", "auth": "required", "description": "Disable and close the gateway socket"},
			{"method": "POST", "path": "/api/v1/accounts/{id}/reconnect", "auth": "required", "description": "Close the live socket and identify again"},
		},
	})
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
		"statuses": []string{"online", "idle", "dnd", "invisible", "streaming"},
		"activity_types": []string{"", "playing", "streaming", "listening", "watching", "custom", "competing"},
		"clients":        []string{"desktop", "mobile"},
		"start":          []string{"now", "unix", "RFC3339"},
	})
}

func (s *Server) patchStatus(w http.ResponseWriter, r *http.Request, id int64) {
	var body struct {
		Status string `json:"status"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	if _, _, err := presence.ParseStatus(body.Status); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_status", err.Error())
		return
	}
	acc, err := s.store.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	acc.Status = body.Status
	saved, err := s.store.Update(acc)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	s.host.Apply(saved)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "account": toView(saved, s.host.Runtime(saved.ID))})
}

func (s *Server) patchAccount(w http.ResponseWriter, r *http.Request, id int64) {
	acc, err := s.store.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	var raw map[string]any
	if err := readJSON(r, &raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	if err := mergeAccount(&acc, raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if _, err := acc.Presence(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_presence", err.Error())
		return
	}
	saved, err := s.store.Update(acc)
	if err != nil {
		if isUnique(err) {
			writeError(w, http.StatusConflict, "token_conflict", "token already exists")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	s.host.Apply(saved)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "account": toView(saved, s.host.Runtime(saved.ID))})
}

func (s *Server) writeAccount(w http.ResponseWriter, id int64, ok bool) {
	acc, err := s.store.Get(id)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": ok})
		return
	}
	out := map[string]any{"account": toView(acc, s.host.Runtime(acc.ID))}
	if ok {
		out["ok"] = true
	}
	writeJSON(w, http.StatusOK, out)
}

func parseAccountPath(path string) (int64, string, bool) {
	path = strings.TrimPrefix(path, "/api/v1/accounts/")
	path = strings.TrimPrefix(path, "/api/accounts/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return 0, "", false
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		return 0, "", false
	}
	if len(parts) == 1 {
		return id, "", true
	}
	if len(parts) == 2 {
		return id, parts[1], true
	}
	return 0, "", false
}

func (s *Server) authed(w http.ResponseWriter, r *http.Request) bool {
	if s.password == "" {
		writeError(w, http.StatusServiceUnavailable, "auth_unconfigured", "DASHBOARD_PASSWORD is not set")
		return false
	}
	if key, ok := presentedKey(r); ok {
		if secretEqual(s.password, key) {
			return true
		}
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return false
	}
	c, err := r.Cookie(cookieName)
	if err != nil {
		w.Header().Set("WWW-Authenticate", `Bearer realm="DiscordActivity"`)
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return false
	}
	s.mu.Lock()
	exp, ok := s.sessions[c.Value]
	if ok && time.Now().After(exp) {
		delete(s.sessions, c.Value)
		ok = false
	}
	s.mu.Unlock()
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return false
	}
	return true
}

func presentedKey(r *http.Request) (string, bool) {
	if v := strings.TrimSpace(r.Header.Get("X-API-Key")); v != "" {
		return v, true
	}
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		if v := strings.TrimSpace(h[len(prefix):]); v != "" {
			return v, true
		}
	}
	return "", false
}

func secretEqual(want, got string) bool {
	sum := sha256.Sum256([]byte(want))
	have := sha256.Sum256([]byte(got))
	return subtle.ConstantTimeCompare(sum[:], have[:]) == 1
}

func isUnique(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "constraint")
}

type view struct {
	store.Account
	Runtime runtime `json:"runtime"`
}

type runtime struct {
	User      string `json:"user,omitempty"`
	AvatarURL string `json:"avatar_url,omitempty"`
	Status    string `json:"status,omitempty"`
	Activity  string `json:"activity,omitempty"`
	Connected bool   `json:"connected"`
	LastError string `json:"last_error,omitempty"`
}

func toView(a store.Account, snap gateway.Snapshot) view {
	return view{Account: a.Public(), Runtime: runtime{User: snap.User, AvatarURL: snap.AvatarURL, Status: snap.Status, Activity: snap.Activity, Connected: snap.Connected, LastError: snap.LastError}}
}

func mergeAccount(acc *store.Account, raw map[string]any) error {
	set := func(key string, dest *string) {
		v, ok := raw[key]
		if !ok || v == nil {
			return
		}
		s, ok := v.(string)
		if !ok {
			return
		}
		*dest = s
	}
	set("name", &acc.Name)
	set("status", &acc.Status)
	set("client", &acc.Client)
	set("activity_type", &acc.ActivityType)
	set("activity_name", &acc.ActivityName)
	set("details", &acc.Details)
	set("state", &acc.State)
	set("url", &acc.URL)
	set("application_id", &acc.ApplicationID)
	set("start", &acc.Start)
	set("large_image", &acc.LargeImage)
	set("large_text", &acc.LargeText)
	set("small_image", &acc.SmallImage)
	set("small_text", &acc.SmallText)
	set("emoji", &acc.Emoji)
	set("button_1_label", &acc.Button1Label)
	set("button_1_url", &acc.Button1URL)
	set("button_2_label", &acc.Button2Label)
	set("button_2_url", &acc.Button2URL)
	if v, ok := raw["token"]; ok {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			acc.Token = s
		}
	}
	if v, ok := raw["enabled"]; ok {
		if b, ok := v.(bool); ok {
			acc.Enabled = b
		}
	}
	return nil
}

func readJSON(r *http.Request, dest any) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(dest)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": message, "code": code})
}
