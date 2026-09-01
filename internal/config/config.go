package config

import (
	"os"
	"strings"
)

type Config struct {
	Addr       string
	LogLevel   string
	SQLitePath string
	Password   string
}

func Load() Config {
	return Config{
		Addr:       env("HEALTH_ADDR", ":8080"),
		LogLevel:   env("LOG_LEVEL", "info"),
		SQLitePath: env("SQLITE_PATH", "data.sqlite"),
		Password:   env("DASHBOARD_PASSWORD", env("DASHBOARD_KEY", "")),
	}
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
