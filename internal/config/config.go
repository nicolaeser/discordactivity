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
		Addr:       listenAddr(),
		LogLevel:   env("LOG_LEVEL", "info"),
		SQLitePath: env("SQLITE_PATH", "data.sqlite"),
		Password:   firstEnv("DASHBOARD_PASSWORD", "API_KEY", "DASHBOARD_KEY"),
	}
}

func listenAddr() string {
	port := strings.TrimPrefix(env("PORT", "8080"), ":")
	if port == "" {
		port = "8080"
	}
	return ":" + port
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if v := env(key, ""); v != "" {
			return v
		}
	}
	return ""
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
