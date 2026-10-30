package host

import (
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/nicolaeser/discordactivity/internal/store"
)

func TestStartStopPersistEnabled(t *testing.T) {
	t.Parallel()
	db, err := store.Open(filepath.Join(t.TempDir(), "data.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := t.Context()
	h := New(ctx, db, slog.New(slog.DiscardHandler))

	saved, err := db.Insert(store.Account{Name: "main", Token: "fake-token-host", Enabled: false, Status: "online"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Start(saved.ID); err != nil {
		t.Fatal(err)
	}
	got, err := db.Get(saved.ID)
	if err != nil || !got.Enabled {
		t.Fatalf("start persist %+v %v", got, err)
	}
	if err := h.StopAccount(saved.ID); err != nil {
		t.Fatal(err)
	}
	got, err = db.Get(saved.ID)
	if err != nil || got.Enabled {
		t.Fatalf("stop persist %+v %v", got, err)
	}
	if err := h.Restart(saved.ID); err != nil {
		t.Fatal(err)
	}
	got, err = db.Get(saved.ID)
	if err != nil || !got.Enabled {
		t.Fatalf("reconnect persist %+v %v", got, err)
	}
	h.Stop(saved.ID)
}

func TestVoiceTogglePersists(t *testing.T) {
	t.Parallel()
	db, err := store.Open(filepath.Join(t.TempDir(), "data.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := t.Context()
	h := New(ctx, db, slog.New(slog.DiscardHandler))

	saved, err := db.Insert(store.Account{Name: "main", Token: "fake-token-voice", Enabled: false, Status: "online",
		VoiceGuildID: "111111111111111111", VoiceChannelID: "222222222222222222", VoiceSelfMute: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.SetVoiceEnabled(saved.ID, true); err != nil {
		t.Fatal(err)
	}
	got, err := db.Get(saved.ID)
	if err != nil || !got.VoiceEnabled {
		t.Fatalf("voice on persist %+v %v", got, err)
	}
	if err := h.SetVoiceEnabled(saved.ID, false); err != nil {
		t.Fatal(err)
	}
	got, err = db.Get(saved.ID)
	if err != nil || got.VoiceEnabled {
		t.Fatalf("voice off persist %+v %v", got, err)
	}
	if err := h.SetVoiceEnabled(saved.ID+99, true); err == nil {
		t.Fatal("expected missing account")
	}
}

func TestVoiceToggleRequiresChannel(t *testing.T) {
	t.Parallel()
	db, err := store.Open(filepath.Join(t.TempDir(), "data.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := t.Context()
	h := New(ctx, db, slog.New(slog.DiscardHandler))

	saved, err := db.Insert(store.Account{Name: "main", Token: "fake-token-voice-2", Enabled: false, Status: "online"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.SetVoiceEnabled(saved.ID, true); err == nil {
		t.Fatal("expected channel required")
	}
}
