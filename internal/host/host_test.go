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
