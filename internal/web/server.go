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

	"github.com/nicolaeser/discord-activity/internal/gateway"
	"github.com/nicolaeser/discord-activity/internal/host"
	"github.com/nicolaeser/discord-activity/internal/store"
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
	mux.HandleFunc("/api/accounts", s.handleAccounts)
	mux.HandleFunc("/api/accounts/", s.handleAccount)
	mux.HandleFunc("/", s.handleIndex)
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return nil, err
	}
	s.addr = ln.Addr().String()
	s.http = &http.Server{Addr: s.addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := s.http.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.log.Error("http", "error", err)
		}
	}()
	return s, nil
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
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(body)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	accounts := []host.Status{}
	if s.host != nil {
		if list := s.host.Health(); list != nil {
			accounts = list
		}
	}
	ok := true
	for _, a := range accounts {
		if !a.Connected {
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
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.password == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "DASHBOARD_PASSWORD is not set"})
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	time.Sleep(120 * time.Millisecond)
	sum := sha256.Sum256([]byte(s.password))
	got := sha256.Sum256([]byte(body.Password))
	if subtle.ConstantTimeCompare(sum[:], got[:]) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid password"})
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
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		out := make([]view, 0, len(accounts))
		for _, a := range accounts {
			out = append(out, toView(a, s.host.Runtime(a.ID)))
		}
		writeJSON(w, 200, map[string]any{"accounts": out})
	case http.MethodPost:
		var a store.Account
		if err := readJSON(r, &a); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid json"})
			return
		}
		if strings.TrimSpace(a.Token) == "" {
			writeJSON(w, 400, map[string]string{"error": "token is required"})
			return
		}
		if _, err := a.Presence(); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		saved, err := s.store.Insert(a)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		s.host.Apply(saved)
		writeJSON(w, 201, map[string]any{"account": toView(saved, s.host.Runtime(saved.ID))})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAccount(w http.ResponseWriter, r *http.Request) {
	if !s.authed(w, r) {
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/accounts/"), "/"), "/")
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, 400, map[string]string{"error": "invalid id"})
		return
	}
	if len(parts) == 2 && parts[1] == "reconnect" && r.Method == http.MethodPost {
		if err := s.host.Restart(id); err != nil {
			writeJSON(w, 404, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	if len(parts) != 1 {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		acc, err := s.store.Get(id)
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"account": toView(acc, s.host.Runtime(acc.ID))})
	case http.MethodPut:
		var a store.Account
		if err := readJSON(r, &a); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid json"})
			return
		}
		a.ID = id
		if _, err := a.Presence(); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		saved, err := s.store.Update(a)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		s.host.Apply(saved)
		writeJSON(w, 200, map[string]any{"account": toView(saved, s.host.Runtime(saved.ID))})
	case http.MethodDelete:
		if err := s.store.Delete(id); err != nil {
			writeJSON(w, 404, map[string]string{"error": err.Error()})
			return
		}
		s.host.Stop(id)
		writeJSON(w, 200, map[string]bool{"ok": true})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) authed(w http.ResponseWriter, r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	if err != nil {
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
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
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
		return false
	}
	return true
}

type view struct {
	store.Account
	Runtime runtime `json:"runtime"`
}

type runtime struct {
	User      string `json:"user,omitempty"`
	Status    string `json:"status,omitempty"`
	Activity  string `json:"activity,omitempty"`
	Connected bool   `json:"connected"`
	LastError string `json:"last_error,omitempty"`
}

func toView(a store.Account, snap gateway.Snapshot) view {
	return view{Account: a.Public(), Runtime: runtime{User: snap.User, Status: snap.Status, Activity: snap.Activity, Connected: snap.Connected, LastError: snap.LastError}}
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
