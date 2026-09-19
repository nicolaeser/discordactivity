package config

import "testing"

func TestLoad(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("LOG_LEVEL", "")
	t.Setenv("SQLITE_PATH", "")
	t.Setenv("DASHBOARD_PASSWORD", "secret")
	t.Setenv("DASHBOARD_KEY", "")
	t.Setenv("API_KEY", "")
	cfg := Load()
	if cfg.Addr != ":8080" || cfg.SQLitePath != "data.sqlite" || cfg.Password != "secret" {
		t.Fatalf("%+v", cfg)
	}

	t.Setenv("PORT", "9")
	t.Setenv("SQLITE_PATH", "/tmp/x.sqlite")
	cfg = Load()
	if cfg.Addr != ":9" || cfg.SQLitePath != "/tmp/x.sqlite" {
		t.Fatalf("%+v", cfg)
	}

	t.Setenv("PORT", ":7777")
	cfg = Load()
	if cfg.Addr != ":7777" {
		t.Fatalf("%+v", cfg)
	}

	t.Setenv("DASHBOARD_PASSWORD", "")
	t.Setenv("API_KEY", "from-api")
	cfg = Load()
	if cfg.Password != "from-api" {
		t.Fatalf("%+v", cfg)
	}
}
