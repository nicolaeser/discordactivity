package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestSQLiteCRUD(t *testing.T) {
	t.Parallel()
	db, err := Open(filepath.Join(t.TempDir(), "data.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	saved, err := db.Insert(Account{Name: "main", Token: "token-one", Enabled: true, Status: "dnd", ActivityType: "playing", ActivityName: "VS Code"})
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID == 0 || saved.Client != "desktop" {
		t.Fatalf("id/client %+v", saved)
	}
	list, err := db.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Public().TokenHint != "••••-one" {
		t.Fatalf("list=%+v", list)
	}
	saved.Status = "idle"
	saved.Token = ""
	saved.VoiceEnabled = true
	saved.VoiceGuildID = "123456789012345678"
	saved.VoiceChannelID = "123456789012345679"
	saved.VoiceSelfMute = true
	updated, err := db.Update(saved)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Token != "token-one" || updated.Status != "idle" {
		t.Fatalf("%+v", updated)
	}
	loaded, err := db.Get(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.VoiceEnabled || loaded.VoiceGuildID != "123456789012345678" ||
		loaded.VoiceChannelID != "123456789012345679" || !loaded.VoiceSelfMute || loaded.VoiceSelfDeaf {
		t.Fatalf("voice not persisted %+v", loaded)
	}
	if v := loaded.Voice(); !v.Enabled || v.ChannelID != "123456789012345679" || !v.SelfMute {
		t.Fatalf("voice config %+v", v)
	}
	if err := db.Delete(saved.ID); err != nil {
		t.Fatal(err)
	}
}

func TestVoiceMigrationKeepsOldRows(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "old.sqlite")
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`CREATE TABLE accounts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL DEFAULT '',
		token TEXT NOT NULL UNIQUE,
		enabled INTEGER NOT NULL DEFAULT 1,
		status TEXT NOT NULL DEFAULT 'online',
		client TEXT NOT NULL DEFAULT 'desktop',
		activity_type TEXT NOT NULL DEFAULT '',
		activity_name TEXT NOT NULL DEFAULT '',
		details TEXT NOT NULL DEFAULT '',
		state TEXT NOT NULL DEFAULT '',
		url TEXT NOT NULL DEFAULT '',
		application_id TEXT NOT NULL DEFAULT '',
		activity_start TEXT NOT NULL DEFAULT '',
		large_image TEXT NOT NULL DEFAULT '',
		large_text TEXT NOT NULL DEFAULT '',
		small_image TEXT NOT NULL DEFAULT '',
		small_text TEXT NOT NULL DEFAULT '',
		emoji TEXT NOT NULL DEFAULT '',
		button_1_label TEXT NOT NULL DEFAULT '',
		button_1_url TEXT NOT NULL DEFAULT '',
		button_2_label TEXT NOT NULL DEFAULT '',
		button_2_url TEXT NOT NULL DEFAULT ''
	)`)
	if err != nil {
		_ = raw.Close()
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO accounts (name, token) VALUES ('main','token-old')`); err != nil {
		_ = raw.Close()
		t.Fatal(err)
	}
	_ = raw.Close()

	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	list, err := db.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Token != "token-old" || list[0].VoiceEnabled || list[0].VoiceChannelID != "" {
		t.Fatalf("migrated list=%+v", list)
	}
}

func TestSetEnabled(t *testing.T) {
	t.Parallel()
	db, err := Open(filepath.Join(t.TempDir(), "data.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	saved, err := db.Insert(Account{Name: "main", Token: "token-one", Enabled: true, Status: "dnd", ActivityType: "playing", ActivityName: "VS Code"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetEnabled(saved.ID, false); err != nil {
		t.Fatal(err)
	}
	loaded, err := db.Get(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Enabled || loaded.Token != "token-one" || loaded.Status != "dnd" {
		t.Fatalf("%+v", loaded)
	}
	if err := db.SetEnabled(saved.ID, true); err != nil {
		t.Fatal(err)
	}
	loaded, err = db.Get(saved.ID)
	if err != nil || !loaded.Enabled {
		t.Fatalf("%+v %v", loaded, err)
	}
	if err := db.SetEnabled(saved.ID+99, false); err == nil {
		t.Fatal("expected missing account")
	}
}
