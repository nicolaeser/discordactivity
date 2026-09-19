package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nicolaeser/DiscordActivity/internal/presence"
)

const (
	opDispatch        = 0
	opHeartbeat       = 1
	opIdentify        = 2
	opPresenceUpdate  = 3
	opResume          = 6
	opReconnect       = 7
	opInvalidSession  = 9
	opHello           = 10
	opHeartbeatACK    = 11
	closeAuthFailed   = 4004
	closeInvalidSeq   = 4007
	closeSessionTimed = 4009
	userAgent         = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	gatewayURL        = "wss://gateway.discord.gg/?v=9&encoding=json"
)

var (
	errReconnect      = errors.New("reconnect")
	errInvalidSession = errors.New("invalid session")
	errAuthFailed     = errors.New("authentication failed")
)

type Snapshot struct {
	ID        int64
	Label     string
	User      string
	Status    string
	Activity  string
	Connected bool
	LastError string
}

type Session struct {
	id       int64
	label    string
	token    string
	presence presence.Update
	log      *slog.Logger

	writeMu sync.Mutex
	conn    *websocket.Conn

	seq       atomic.Int64
	gotAck    atomic.Bool
	authFail  atomic.Bool
	connected atomic.Bool

	mu        sync.Mutex
	sessionID string
	resumeURL string
	user      string
	lastErr   string
}

type payload struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d,omitempty"`
	S  *int64          `json:"s,omitempty"`
	T  string          `json:"t,omitempty"`
}

func New(id int64, label, token string, pres presence.Update, log *slog.Logger) *Session {
	if log == nil {
		log = slog.Default()
	}
	return &Session{id: id, label: label, token: token, presence: pres, log: log.With("account", label)}
}

func (s *Session) Close() {
	s.writeMu.Lock()
	conn := s.conn
	s.conn = nil
	s.writeMu.Unlock()
	if conn == nil {
		return
	}
	_ = conn.SetReadDeadline(time.Now())
	_ = conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(4000, "stopped"),
		time.Now().Add(time.Second),
	)
	_ = conn.Close()
}

func (s *Session) Run(ctx context.Context) {
	defer s.Close()
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		s.connected.Store(false)
		err := s.connect(ctx)
		if ctx.Err() != nil {
			return
		}
		if s.authFail.Load() {
			s.setLastError(errAuthFailed.Error())
			s.log.Error("token rejected")
			return
		}
		if err == nil {
			err = io.EOF
		}
		s.setLastError(err.Error())
		s.log.Warn("disconnected", "error", err, "backoff", backoff.String())
		t := time.NewTimer(backoff + time.Duration(float64(backoff)*0.2*rand.Float64()))
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		if backoff < 60*time.Second {
			backoff *= 2
			if backoff > 60*time.Second {
				backoff = 60 * time.Second
			}
		}
	}
}

func (s *Session) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Snapshot{ID: s.id, Label: s.label, User: s.user, Status: s.presence.Status, Activity: s.presence.Label(), Connected: s.connected.Load(), LastError: s.lastErr}
}

func (s *Session) SetPresence(p presence.Update) {
	s.mu.Lock()
	s.presence = p
	s.mu.Unlock()
	s.writeMu.Lock()
	conn := s.conn
	s.writeMu.Unlock()
	if conn != nil {
		_ = s.sendPresence(conn)
	}
}

func (s *Session) connect(ctx context.Context) error {
	header := http.Header{}
	header.Set("Origin", "https://discord.com")
	header.Set("User-Agent", userAgent)
	url := gatewayURL
	if s.canResume() {
		if u := s.resumeGateway(); u != "" {
			url = withEncoding(u)
		}
	}
	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	conn, _, err := dialer.DialContext(ctx, url, header)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	conn.SetReadLimit(32 << 20)
	s.writeMu.Lock()
	s.conn = conn
	s.writeMu.Unlock()
	defer func() {
		s.connected.Store(false)
		s.writeMu.Lock()
		if s.conn == conn {
			s.conn = nil
		}
		s.writeMu.Unlock()
		_ = conn.Close()
	}()
	stopClose := context.AfterFunc(ctx, s.Close)
	defer stopClose()

	_ = conn.SetReadDeadline(time.Now().Add(20 * time.Second))
	hello, err := s.readPayload(conn)
	if err != nil {
		return s.noteClose(err)
	}
	if hello.Op != opHello {
		return fmt.Errorf("expected hello, got op %d", hello.Op)
	}
	var hd struct {
		HeartbeatInterval float64 `json:"heartbeat_interval"`
	}
	if err := json.Unmarshal(hello.D, &hd); err != nil {
		return err
	}
	interval := time.Duration(hd.HeartbeatInterval) * time.Millisecond
	if interval <= 0 {
		interval = 41250 * time.Millisecond
	}
	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go s.heartbeat(connCtx, conn, interval)
	if s.canResume() {
		err = s.send(conn, map[string]any{"op": opResume, "d": map[string]any{"token": s.token, "session_id": s.resumeSessionID(), "seq": s.seq.Load()}})
	} else {
		s.log.Info("identifying")
		err = s.send(conn, map[string]any{"op": opIdentify, "d": map[string]any{
			"token": s.token, "capabilities": 30717, "compress": false,
			"properties": map[string]any{"os": "Windows", "browser": "Chrome", "device": "", "system_locale": "en-US", "browser_user_agent": userAgent, "browser_version": "131.0.0.0", "os_version": "10", "release_channel": "stable", "client_build_number": 381059},
			"presence":   s.currentPresence(), "client_state": map[string]any{"guild_versions": map[string]int{}},
		}})
	}
	if err != nil {
		return s.noteClose(err)
	}
	return s.noteClose(s.readLoop(connCtx, conn, interval))
}

func (s *Session) readLoop(ctx context.Context, conn *websocket.Conn, interval time.Duration) error {
	deadline := interval * 2
	if deadline < 30*time.Second {
		deadline = 30 * time.Second
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_ = conn.SetReadDeadline(time.Now().Add(deadline))
		p, err := s.readPayload(conn)
		if err != nil {
			return classifyClose(err)
		}
		switch p.Op {
		case opDispatch:
			if err := s.handleDispatch(conn, p); err != nil {
				return err
			}
		case opHeartbeat:
			if err := s.sendHeartbeat(conn); err != nil {
				return err
			}
		case opReconnect:
			s.clearResume()
			return errReconnect
		case opInvalidSession:
			var resumable bool
			_ = json.Unmarshal(p.D, &resumable)
			if !resumable {
				s.clearResume()
			}
			return errInvalidSession
		case opHeartbeatACK:
			s.gotAck.Store(true)
		}
	}
}

func (s *Session) handleDispatch(conn *websocket.Conn, p payload) error {
	switch p.T {
	case "READY":
		var ready struct {
			SessionID        string `json:"session_id"`
			ResumeGatewayURL string `json:"resume_gateway_url"`
			User             struct {
				ID, Username, Discriminator, GlobalName string
			} `json:"user"`
		}
		if err := json.Unmarshal(p.D, &ready); err != nil {
			return err
		}
		user := ready.User.GlobalName
		if user == "" {
			user = ready.User.Username
			if ready.User.Discriminator != "" && ready.User.Discriminator != "0" {
				user += "#" + ready.User.Discriminator
			}
		}
		if user == "" {
			user = ready.User.ID
		}
		s.mu.Lock()
		s.sessionID, s.resumeURL, s.user, s.lastErr = ready.SessionID, ready.ResumeGatewayURL, user, ""
		s.mu.Unlock()
		s.connected.Store(true)
		s.log.Info("connected", "user", user)
		return s.sendPresence(conn)
	case "RESUMED":
		s.connected.Store(true)
		s.setLastError("")
		return s.sendPresence(conn)
	}
	return nil
}

func (s *Session) heartbeat(ctx context.Context, conn *websocket.Conn, interval time.Duration) {
	first := time.Duration(float64(interval) * (0.1 + 0.8*rand.Float64()))
	timer := time.NewTimer(first)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}
	s.gotAck.Store(false)
	if err := s.sendHeartbeat(conn); err != nil {
		_ = conn.Close()
		return
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = conn.Close()
			return
		case <-tick.C:
			if !s.gotAck.Load() {
				_ = conn.Close()
				return
			}
			s.gotAck.Store(false)
			if err := s.sendHeartbeat(conn); err != nil {
				_ = conn.Close()
				return
			}
		}
	}
}

func (s *Session) sendPresence(conn *websocket.Conn) error {
	return s.send(conn, map[string]any{"op": opPresenceUpdate, "d": s.currentPresence()})
}

func (s *Session) sendHeartbeat(conn *websocket.Conn) error {
	var d any
	if seq := s.seq.Load(); seq > 0 {
		d = seq
	}
	return s.send(conn, map[string]any{"op": opHeartbeat, "d": d})
}

func (s *Session) send(conn *websocket.Conn, v any) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
	return conn.WriteJSON(v)
}

func (s *Session) readPayload(conn *websocket.Conn) (payload, error) {
	_, data, err := conn.ReadMessage()
	if err != nil {
		return payload{}, err
	}
	var p payload
	if err := json.Unmarshal(data, &p); err != nil {
		return payload{}, err
	}
	if p.S != nil {
		s.seq.Store(*p.S)
	}
	return p, nil
}

func (s *Session) currentPresence() presence.Update {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.presence
}

func (s *Session) canResume() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionID != ""
}

func (s *Session) resumeSessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionID
}

func (s *Session) resumeGateway() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resumeURL
}

func (s *Session) clearResume() {
	s.mu.Lock()
	s.sessionID, s.resumeURL = "", ""
	s.mu.Unlock()
	s.seq.Store(0)
}

func (s *Session) setLastError(msg string) {
	s.mu.Lock()
	s.lastErr = msg
	s.mu.Unlock()
}

func (s *Session) noteClose(err error) error {
	if err == nil {
		return nil
	}
	err = classifyClose(err)
	if errors.Is(err, errAuthFailed) {
		s.authFail.Store(true)
	}
	if errors.Is(err, errInvalidSession) {
		s.clearResume()
	}
	return err
}

func classifyClose(err error) error {
	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) {
		switch closeErr.Code {
		case closeAuthFailed:
			return errAuthFailed
		case closeInvalidSeq, closeSessionTimed:
			return errInvalidSession
		}
	}
	return err
}

func withEncoding(raw string) string {
	if raw == "" {
		return gatewayURL
	}
	if strings.Contains(raw, "encoding=") {
		return raw
	}
	sep := "?"
	if strings.Contains(raw, "?") {
		sep = "&"
	}
	if strings.Contains(raw, "v=") {
		return raw + sep + "encoding=json"
	}
	return raw + sep + "v=9&encoding=json"
}
