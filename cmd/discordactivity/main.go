package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/nicolaeser/DiscordActivity/internal/config"
	"github.com/nicolaeser/DiscordActivity/internal/host"
	"github.com/nicolaeser/DiscordActivity/internal/store"
	"github.com/nicolaeser/DiscordActivity/internal/web"
)

var version = "dev"

func main() {
	healthcheck := flag.Bool("healthcheck", false, "")
	flag.Parse()
	if *healthcheck {
		if err := web.Probe(config.Load().Addr); err != nil {
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		slog.Error("exit", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config.Load()
	level := slog.LevelInfo
	switch strings.ToLower(cfg.LogLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})))
	slog.Info("starting", "version", version, "sqlite", cfg.SQLitePath)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(cfg.SQLitePath)
	if err != nil {
		return err
	}
	defer db.Close()

	h := host.New(ctx, db, slog.Default())
	srv, err := web.Start(web.Config{Addr: cfg.Addr, Password: cfg.Password, Store: db, Host: h, Log: slog.Default()})
	if err != nil {
		return err
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(c)
	}()
	slog.Info("dashboard", "addr", srv.Addr())
	go func() {
		if err := h.StartAll(); err != nil && ctx.Err() == nil {
			slog.Error("start accounts", "error", err)
		}
	}()
	<-ctx.Done()
	return nil
}
