package presence

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, status string
		stream     bool
	}{
		{"", "online", false},
		{"away", "idle", false},
		{"D&D", "dnd", false},
		{"invisible", "invisible", false},
		{"streaming", "online", true},
	}
	for _, tc := range cases {
		status, stream, err := ParseStatus(tc.in)
		if err != nil || status != tc.status || stream != tc.stream {
			t.Fatalf("%q => %s %v %v", tc.in, status, stream, err)
		}
	}
}

func TestBuild(t *testing.T) {
	t.Parallel()
	now := time.UnixMilli(1_700_000_000_000)
	u, err := Build(Options{Status: "streaming", Name: "Just Chatting", URL: "https://twitch.tv/x", Now: now})
	if err != nil || u.Status != "online" || u.Activities[0].Type != TypeStreaming {
		t.Fatalf("%+v %v", u, err)
	}
	u, err = Build(Options{Status: "dnd", ActivityType: "custom", Name: "working", Emoji: "💻"})
	if err != nil || u.Activities[0].State != "working" || u.Activities[0].Emoji.Name != "💻" {
		t.Fatalf("%+v %v", u, err)
	}
	u, err = Build(Options{Status: "idle", ActivityType: "playing", Name: "Minecraft", Start: "now", Button1Label: "GitHub", Button1URL: "https://github.com/x", Now: now})
	if err != nil || !u.AFK || u.Activities[0].Buttons[0] != "GitHub" {
		t.Fatalf("%+v %v", u, err)
	}
	raw, _ := json.Marshal(u)
	if !json.Valid(raw) {
		t.Fatal(string(raw))
	}
	u, err = Build(Options{Status: "invisible"})
	if err != nil || len(u.Activities) != 0 {
		t.Fatalf("%+v %v", u, err)
	}
	if _, err := Build(Options{Status: "nope"}); err == nil {
		t.Fatal("expected error")
	}
}
