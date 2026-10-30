package gateway

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nicolaeser/discordactivity/internal/presence"
	"github.com/nicolaeser/discordactivity/internal/voice"
)

func TestWithEncoding(t *testing.T) {
	t.Parallel()
	if got := withEncoding(""); got != gatewayURL {
		t.Fatalf("%s", got)
	}
	if got := withEncoding("wss://gateway.discord.gg"); got != "wss://gateway.discord.gg?v=9&encoding=json" {
		t.Fatalf("%s", got)
	}
}

func TestClassifyClose(t *testing.T) {
	t.Parallel()
	if !errors.Is(classifyClose(&websocket.CloseError{Code: closeAuthFailed}), errAuthFailed) {
		t.Fatal("auth")
	}
	s := &Session{}
	if err := s.noteClose(&websocket.CloseError{Code: closeAuthFailed}); !errors.Is(err, errAuthFailed) || !s.authFail.Load() {
		t.Fatal(err)
	}
}

func TestCloseNilConn(t *testing.T) {
	t.Parallel()
	s := &Session{}
	s.Close()
}

func TestGoInvisible(t *testing.T) {
	t.Parallel()
	s := New(1, "a", "tok", "desktop", presence.Update{Status: "online"}, voice.Config{}, nil)
	s.GoInvisible()
	if s.currentPresence().Status != "invisible" {
		t.Fatalf("%+v", s.currentPresence())
	}
}

func TestIdentifyClient(t *testing.T) {
	t.Parallel()
	desk := New(1, "a", "tok", "desktop", presence.Update{}, voice.Config{}, nil)
	if desk.properties()["browser"] != "Discord Client" || desk.userAgent() != desktopUA {
		t.Fatalf("%v", desk.properties())
	}
	mob := New(1, "a", "tok", "mobile", presence.Update{}, voice.Config{}, nil)
	if mob.properties()["browser"] != "Discord iOS" || mob.userAgent() != mobileUA {
		t.Fatalf("%v", mob.properties())
	}
}

func TestSetVoiceSnapshot(t *testing.T) {
	t.Parallel()
	s := New(1, "a", "tok", "desktop", presence.Update{Status: "online"}, voice.Config{}, nil)
	s.SetVoice(voice.Config{Enabled: true, GuildID: "111111111111111111", ChannelID: "222222222222222222", SelfMute: true})
	snap := s.Snapshot()
	if !snap.VoiceEnabled || snap.VoiceChannelID != "222222222222222222" || snap.VoiceConnected {
		t.Fatalf("%+v", snap)
	}
	s.SetVoice(voice.Config{})
	if s.Snapshot().VoiceEnabled {
		t.Fatal("voice still enabled")
	}
}

func TestVoiceJoinPayload(t *testing.T) {
	t.Parallel()
	conn, got := dialLoopback(t)
	defer conn.Close()
	s := New(1, "a", "tok", "desktop", presence.Update{}, voice.Config{
		Enabled: true, GuildID: "111111111111111111", ChannelID: "222222222222222222", SelfMute: true,
	}, nil)
	if err := s.sendVoiceJoin(conn); err != nil {
		t.Fatal(err)
	}
	var msg struct {
		Op int `json:"op"`
		D  struct {
			GuildID   string `json:"guild_id"`
			ChannelID string `json:"channel_id"`
			SelfMute  bool   `json:"self_mute"`
			SelfDeaf  bool   `json:"self_deaf"`
		} `json:"d"`
	}
	select {
	case raw := <-got:
		if err := json.Unmarshal(raw, &msg); err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no voice join sent")
	}
	if msg.Op != opVoiceState || msg.D.GuildID != "111111111111111111" ||
		msg.D.ChannelID != "222222222222222222" || !msg.D.SelfMute || msg.D.SelfDeaf {
		t.Fatalf("%+v", msg)
	}
}

func TestVoiceJoinDisabledSendsNothing(t *testing.T) {
	t.Parallel()
	conn, got := dialLoopback(t)
	defer conn.Close()
	s := New(1, "a", "tok", "desktop", presence.Update{}, voice.Config{}, nil)
	if err := s.sendVoiceJoin(conn); err != nil {
		t.Fatal(err)
	}
	select {
	case raw := <-got:
		t.Fatalf("unexpected send: %s", raw)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestVoiceStateTracking(t *testing.T) {
	t.Parallel()
	conn, got := dialLoopback(t)
	defer conn.Close()
	s := New(1, "a", "tok", "desktop", presence.Update{}, voice.Config{
		Enabled: true, GuildID: "111111111111111111", ChannelID: "222222222222222222",
	}, nil)
	s.mu.Lock()
	s.userID = "999"
	s.mu.Unlock()

	// Another user's state is ignored.
	s.handleVoiceState(conn, json.RawMessage(`{"user_id":"123","guild_id":"111111111111111111","channel_id":"222222222222222222"}`))
	if s.Snapshot().VoiceConnected {
		t.Fatal("foreign state marked connected")
	}
	// Matching state marks us connected.
	s.handleVoiceState(conn, json.RawMessage(`{"user_id":"999","guild_id":"111111111111111111","channel_id":"222222222222222222"}`))
	if !s.Snapshot().VoiceConnected {
		t.Fatal("matching state not tracked")
	}
	// Kick (null channel) clears the flag and triggers a rejoin op 4.
	s.handleVoiceState(conn, json.RawMessage(`{"user_id":"999","guild_id":"111111111111111111","channel_id":null}`))
	if s.Snapshot().VoiceConnected {
		t.Fatal("kick still marked connected")
	}
	select {
	case raw := <-got:
		if !strings.Contains(string(raw), `"channel_id":"222222222222222222"`) {
			t.Fatalf("rejoin payload: %s", raw)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no rejoin sent after kick")
	}
	// Being dragged elsewhere also triggers a rejoin to the configured channel.
	s.handleVoiceState(conn, json.RawMessage(`{"user_id":"999","guild_id":"111111111111111111","channel_id":"333333333333333333"}`))
	select {
	case raw := <-got:
		if !strings.Contains(string(raw), `"channel_id":"222222222222222222"`) {
			t.Fatalf("move-back payload: %s", raw)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no rejoin sent after move")
	}
}

func dialLoopback(t *testing.T) (*websocket.Conn, chan []byte) {
	t.Helper()
	got := make(chan []byte, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{}
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for {
			_, msg, err := c.ReadMessage()
			if err != nil {
				return
			}
			got <- msg
		}
	}))
	t.Cleanup(srv.Close)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	return conn, got
}
