package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"charm.land/wish/v2"
	"charm.land/wish/v2/bubbletea"
	"charm.land/wish/v2/logging"
	"charm.land/wish/v2/ratelimiter"
	"github.com/charmbracelet/ssh"
	"golang.org/x/time/rate"

	"github.com/mynameis-nigel/ssh-farm/internal/config"
	"github.com/mynameis-nigel/ssh-farm/internal/content"
	"github.com/mynameis-nigel/ssh-farm/internal/game"
	"github.com/mynameis-nigel/ssh-farm/internal/identity"
	"github.com/mynameis-nigel/ssh-farm/internal/leaderboard"
	"github.com/mynameis-nigel/ssh-farm/internal/tui"
)

// sessionKey carries per-connection game state through the ssh.Context.
type sessionKey struct{}

type sessionState struct {
	id  identity.SessionIdentity
	res game.AttachResult
}

// SaveManager opens player saves for SSH sessions.
type SaveManager interface {
	Attach(ctx context.Context, id identity.SessionIdentity, publicKey string, now int64, kick func(reason string)) (game.AttachResult, error)
	Shutdown(ctx context.Context) error
	Content() *content.Content
}

// Server wraps the Wish SSH server and related middleware.
type Server struct {
	cfg      config.Config
	logger   *slog.Logger
	ssh      *ssh.Server
	games    SaveManager
	identity *identity.Resolver
	board    *leaderboard.Engine
}

// New constructs and configures the SSH server over the given save manager.
// board is gameplay/02's leaderboard engine, shared read-only across every
// session's tui/02 board screen.
func New(cfg config.Config, logger *slog.Logger, games SaveManager, resolver *identity.Resolver, board *leaderboard.Engine) (*Server, error) {
	if err := ensureHostKeyDir(cfg.HostKeyPath); err != nil {
		return nil, err
	}

	limits := NewSessionLimits(cfg.MaxConnections, cfg.MaxSessionsPerKey, resolver)
	rl := ratelimiter.NewRateLimiter(
		rate.Limit(cfg.RateLimitPerSecond),
		cfg.RateLimitBurst,
		cfg.RateLimitMaxEntries,
	)

	srv := &Server{cfg: cfg, logger: logger, games: games, identity: resolver, board: board}

	s, err := wish.NewServer(
		wish.WithAddress(cfg.ListenAddr()),
		wish.WithHostKeyPath(cfg.HostKeyPath),
		wish.WithIdleTimeout(cfg.IdleTimeout),
		wish.WithPublicKeyAuth(func(_ ssh.Context, key ssh.PublicKey) bool {
			return key != nil
		}),
		// Middlewares run bottom-up: logging → rate limit → caps → PTY
		// requirement → save attach → the game UI. The Bubble Tea handler
		// is the only thing a session can ever reach — there is no shell.
		wish.WithMiddleware(
			bubbletea.MiddlewareWithProgramHandler(srv.newTeaProgram),
			srv.attachSave(),
			RequirePTY(),
			limits.Middleware(),
			ratelimiter.Middleware(rl),
			logging.Middleware(),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create ssh server: %w", err)
	}

	srv.ssh = s
	return srv, nil
}

// attachSave resolves the session's identity, opens its save through the
// manager (applying the concurrency policy), and cleans up on disconnect.
func (srv *Server) attachSave() func(ssh.Handler) ssh.Handler {
	return func(next ssh.Handler) ssh.Handler {
		return func(s ssh.Session) {
			resolved, err := srv.identity.Resolve(s)
			if err != nil {
				srv.logger.Warn("identity resolution failed",
					"user", s.User(), "error", err)
				if errors.Is(err, identity.ErrProxiedIdentity) {
					_, _ = io.WriteString(s, identity.ProxiedIdentityMessage())
				} else {
					_, _ = io.WriteString(s,
						"🌧 The farm could not be opened just now. Please try again shortly.\r\n")
				}
				s.Exit(1)
				return
			}
			id := resolved.Identity

			res, err := srv.games.Attach(s.Context(), id, resolved.PublicKey, time.Now().Unix(), nil)
			if err != nil {
				srv.logger.Warn("attach failed",
					"fingerprint", id.Fingerprint, "slot", id.Slot, "error", err)
				if errors.Is(err, game.ErrSaveBusy) {
					_, _ = io.WriteString(s,
						"🌾 This farm is already open in another session.\r\n"+
							"Close it there (or wait a moment) and reconnect.\r\n")
				} else {
					_, _ = io.WriteString(s,
						"🌧 The farm could not be opened just now. Please try again shortly.\r\n")
				}
				s.Exit(1)
				return
			}
			defer res.Session.Detach()

			srv.logger.Info("session start",
				"fingerprint", id.Fingerprint,
				"slot", id.Slot,
				"proxied", resolved.Proxied,
				"remote", s.RemoteAddr().String(),
			)
			s.Context().SetValue(sessionKey{}, &sessionState{id: id, res: res})
			next(s)
		}
	}
}

// teaHandler builds the per-session Bubble Tea program.
func (srv *Server) teaHandler(s ssh.Session) (tui.Model, []tui.ProgramOption) {
	state, _ := s.Context().Value(sessionKey{}).(*sessionState)
	if state == nil {
		// attachSave always runs first; never hand Wish a nil model anyway.
		return tui.NewErrScreen(), nil
	}
	width, height := 80, 24
	if pty, _, ok := s.Pty(); ok {
		width, height = pty.Window.Width, pty.Window.Height
	}
	// Idle disconnect is enforced inside the UI: the once-a-second render
	// keeps the transport busy, so a connection-level idle timer would
	// never fire. Only key presses count as activity.
	idleSecs := int64(srv.cfg.IdleTimeout / time.Second)
	now := time.Now().Unix()
	return tui.NewGame(state.id, state.res, srv.games.Content(), srv.board, width, height, now, idleSecs), nil
}

// ListenAndServe starts accepting SSH connections.
func (s *Server) ListenAndServe() error {
	s.logger.Info("ssh server listening", "addr", s.cfg.ListenAddr())
	return s.ssh.ListenAndServe()
}

// Shutdown stops accepting new connections and waits for existing ones.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("ssh server shutting down")
	return s.ssh.Shutdown(ctx)
}

func ensureHostKeyDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0o700)
}
