package gateway

import (
	"errors"
	"testing"

	"github.com/gorilla/websocket"
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
