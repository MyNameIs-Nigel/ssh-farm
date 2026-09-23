package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"charm.land/ssh"

	"github.com/mynameis-nigel/ssh-farm/internal/config"
	"github.com/mynameis-nigel/ssh-farm/internal/content"
	"github.com/mynameis-nigel/ssh-farm/internal/game"
	"github.com/mynameis-nigel/ssh-farm/internal/identity"
	"github.com/mynameis-nigel/ssh-farm/internal/leaderboard"
	applog "github.com/mynameis-nigel/ssh-farm/internal/log"
	"github.com/mynameis-nigel/ssh-farm/internal/moderation"
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

	// Fail closed before opening the database or binding a port: a process that
	// is not allowed to serve should not have touched player data first.
	if err := moderation.Init(cfg.ModerationPath, cfg.RequireModeration); err != nil {
		logger.Error("moderation load failed", "path", cfg.ModerationPath, "error", err)
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

	board := leaderboard.New(st, cfg.LeaderboardTTL, cfg.LeaderboardActivityWindow, cfg.LeaderboardMinCoins, time.Now)

	srv, err := server.New(cfg, logger, games, resolver, board)
	if err != nil {
		logger.Error("server init failed", "error", err)
		os.Exit(1)
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	// SIGHUP re-reads the denylist in place, so tightening it is a file edit
	// plus a signal rather than a rebuild and a redeploy — which is what the
	// retroactive-tightening requirement in docs/gameplay/03 needs, since the
	// leaderboard re-checks every name at render time. A failed reload leaves
	// the previous list in force and says so; it never drops the filter.
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		for range hup {
			if err := moderation.Reload(); err != nil {
				logger.Error("denylist reload failed, keeping the previously loaded list", "error", err)
				continue
			}
			logger.Info("denylist reloaded", "path", cfg.ModerationPath)
		}
	}()

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
