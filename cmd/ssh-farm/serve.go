package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/charmbracelet/ssh"

	"github.com/mynameis-nigel/ssh-farm/internal/config"
	"github.com/mynameis-nigel/ssh-farm/internal/content"
	"github.com/mynameis-nigel/ssh-farm/internal/game"
	"github.com/mynameis-nigel/ssh-farm/internal/identity"
	applog "github.com/mynameis-nigel/ssh-farm/internal/log"
	"github.com/mynameis-nigel/ssh-farm/internal/server"
	"github.com/mynameis-nigel/ssh-farm/internal/store"
)

func runServe() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "error", err)
		os.Exit(1)
	}

	logger := applog.New(cfg.LogLevel, cfg.LogFormat)
	slog.SetDefault(logger)

	c, err := content.Load(cfg.DataDir)
	if err != nil {
		logger.Error("content load failed", "error", err)
		os.Exit(1)
	}

	ctx := context.Background()
	st, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		logger.Error("store open failed", "path", cfg.DBPath, "error", err)
		os.Exit(1)
	}
	defer st.Close()

	// Backfill any save whose leaderboard columns predate migration 002 (or
	// arrived via import-v1) before the server starts serving sessions.
	if err := game.ReconcileDenormalized(ctx, st, logger); err != nil {
		logger.Error("reconcile failed", "error", err)
		os.Exit(1)
	}

	proxyKeys, err := identity.LoadProxyKeys(cfg.ProxyKeysPath)
	if err != nil {
		logger.Error("proxy keys load failed", "path", cfg.ProxyKeysPath, "error", err)
		os.Exit(1)
	}
	resolver := identity.NewResolver(cfg.DefaultSlot, proxyKeys)

	games := game.NewManager(st, c, logger, cfg.AutosaveInterval, game.Policy(cfg.SessionPolicy))
	server.RegisterShutdownHook(games.Shutdown)

	srv, err := server.New(cfg, logger, games, resolver)
	if err != nil {
		logger.Error("server init failed", "error", err)
		os.Exit(1)
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
			logger.Error("listen failed", "error", err)
			done <- syscall.SIGTERM
		}
	}()

	sig := <-done
	logger.Info("shutdown signal received", "signal", sig.String())

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelShutdown()

	if err := server.RunShutdownHooks(shutdownCtx); err != nil {
		logger.Error("shutdown hook failed", "error", err)
	}

	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
		logger.Error("shutdown failed", "error", err)
		os.Exit(1)
	}

	logger.Info("shutdown complete")
}
