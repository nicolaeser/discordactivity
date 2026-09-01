package config

import "testing"

func TestLoad(t *testing.T) {
	t.Setenv("HEALTH_ADDR", "")
	t.Setenv("LOG_LEVEL", "")
	t.Setenv("SQLITE_PATH", "")
	t.Setenv("DASHBOARD_PASSWORD", "secret")
	t.Setenv("DASHBOARD_KEY", "")
	cfg := Load()
	if cfg.Addr != ":8080" || cfg.SQLitePath != "data.sqlite" || cfg.Password != "secret" {
		t.Fatalf("%+v", cfg)
	}

	t.Setenv("HEALTH_ADDR", "127.0.0.1:9")
	t.Setenv("SQLITE_PATH", "/tmp/x.sqlite")
	cfg = Load()
	if cfg.Addr != "127.0.0.1:9" || cfg.SQLitePath != "/tmp/x.sqlite" {
		t.Fatalf("%+v", cfg)
	}
}
