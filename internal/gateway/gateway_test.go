package gateway

import (
	"errors"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/nicolaeser/discordactivity/internal/presence"
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
	s := New(1, "a", "tok", "desktop", presence.Update{Status: "online"}, nil)
	s.GoInvisible()
	if s.currentPresence().Status != "invisible" {
		t.Fatalf("%+v", s.currentPresence())
	}
}

func TestIdentifyClient(t *testing.T) {
	t.Parallel()
	desk := New(1, "a", "tok", "desktop", presence.Update{}, nil)
	if desk.properties()["browser"] != "Discord Client" || desk.userAgent() != desktopUA {
		t.Fatalf("%v", desk.properties())
	}
	mob := New(1, "a", "tok", "mobile", presence.Update{}, nil)
	if mob.properties()["browser"] != "Discord iOS" || mob.userAgent() != mobileUA {
		t.Fatalf("%v", mob.properties())
	}
}
