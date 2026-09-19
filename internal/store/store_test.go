package store

import (
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
	if saved.ID == 0 {
		t.Fatal("missing id")
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
	updated, err := db.Update(saved)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Token != "token-one" || updated.Status != "idle" {
		t.Fatalf("%+v", updated)
	}
	if err := db.Delete(saved.ID); err != nil {
		t.Fatal(err)
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
