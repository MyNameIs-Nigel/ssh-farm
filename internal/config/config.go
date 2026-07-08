package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/mynameis-nigel/ssh-farm/internal/identity"
)

// Config holds server settings loaded from FARM_* environment variables.
type Config struct {
	ListenHost          string
	ListenPort          int
	HostKeyPath         string
	IdleTimeout         time.Duration
	MaxSessionsPerKey   int
	MaxConnections      int
	DefaultSlot         string
	LogLevel            string
	LogFormat           string
	RateLimitPerSecond  float64
	RateLimitBurst      int
	RateLimitMaxEntries int
	DBPath              string
	AutosaveInterval    time.Duration
	SessionPolicy       string // "takeover" or "refuse"
	DataDir             string // content override dir; empty = embedded
	ProxyKeysPath       string // trusted arcade proxy keys; empty = direct-only dev

	// Leaderboard (gameplay/02): both filters gate which saves count toward
	// the board at all, independent of the in-process cache TTL.
	LeaderboardTTL            time.Duration // FARM_LEADERBOARD_TTL: max snapshot staleness
	LeaderboardActivityWindow time.Duration // FARM_LEADERBOARD_ACTIVITY_DAYS: dormant-farm cutoff
	LeaderboardMinCoins       int64         // FARM_LEADERBOARD_MIN_COINS: unranked-below-this lifetime-coin floor
}

// Load reads configuration from the environment with documented defaults.
func Load() (Config, error) {
	var err error
	cfg := Config{
		ListenHost:    envOr("FARM_LISTEN_HOST", "0.0.0.0"),
		HostKeyPath:   envOr("FARM_HOST_KEY_PATH", "var/ssh_host_key"),
		DefaultSlot:   envOr("FARM_DEFAULT_SLOT", "default"),
		LogLevel:      envOr("FARM_LOG_LEVEL", "info"),
		LogFormat:     envOr("FARM_LOG_FORMAT", "text"),
		DBPath:        envOr("FARM_DB_PATH", "var/farm.db"),
		SessionPolicy: envOr("FARM_SESSION_POLICY", "takeover"),
		DataDir:       os.Getenv("FARM_DATA_DIR"),
		ProxyKeysPath: os.Getenv("FARM_PROXY_KEYS_PATH"),
	}
	if cfg.ListenPort, err = envIntOr("FARM_LISTEN_PORT", 22); err != nil {
		return Config{}, err
	}
	if cfg.IdleTimeout, err = envDurationOr("FARM_IDLE_TIMEOUT", 30*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.MaxSessionsPerKey, err = envIntOr("FARM_MAX_SESSIONS_PER_KEY", 2); err != nil {
		return Config{}, err
	}
	if cfg.MaxConnections, err = envIntOr("FARM_MAX_CONNECTIONS", 100); err != nil {
		return Config{}, err
	}
	if cfg.RateLimitPerSecond, err = envFloatOr("FARM_RATE_LIMIT_PER_SECOND", 2); err != nil {
		return Config{}, err
	}
	if cfg.RateLimitBurst, err = envIntOr("FARM_RATE_LIMIT_BURST", 5); err != nil {
		return Config{}, err
	}
	if cfg.RateLimitMaxEntries, err = envIntOr("FARM_RATE_LIMIT_MAX_IPS", 1000); err != nil {
		return Config{}, err
	}
	if cfg.AutosaveInterval, err = envDurationOr("FARM_AUTOSAVE_INTERVAL", 30*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.LeaderboardTTL, err = envDurationOr("FARM_LEADERBOARD_TTL", 15*time.Second); err != nil {
		return Config{}, err
	}
	activityDays, err := envIntOr("FARM_LEADERBOARD_ACTIVITY_DAYS", 90)
	if err != nil {
		return Config{}, err
	}
	cfg.LeaderboardActivityWindow = time.Duration(activityDays) * 24 * time.Hour
	if cfg.LeaderboardMinCoins, err = envInt64Or("FARM_LEADERBOARD_MIN_COINS", 1); err != nil {
		return Config{}, err
	}

	if cfg.ListenPort < 1 || cfg.ListenPort > 65535 {
		return Config{}, fmt.Errorf("FARM_LISTEN_PORT must be 1-65535, got %d", cfg.ListenPort)
	}
	if cfg.MaxSessionsPerKey < 1 {
		return Config{}, fmt.Errorf("FARM_MAX_SESSIONS_PER_KEY must be at least 1")
	}
	if cfg.MaxConnections < 1 {
		return Config{}, fmt.Errorf("FARM_MAX_CONNECTIONS must be at least 1")
	}
	if cfg.RateLimitPerSecond <= 0 {
		return Config{}, fmt.Errorf("FARM_RATE_LIMIT_PER_SECOND must be positive")
	}
	if cfg.RateLimitBurst < 1 {
		return Config{}, fmt.Errorf("FARM_RATE_LIMIT_BURST must be at least 1")
	}
	if cfg.IdleTimeout < 0 {
		return Config{}, fmt.Errorf("FARM_IDLE_TIMEOUT must be zero or positive")
	}
	slot := identity.SanitizeSlot(cfg.DefaultSlot)
	if slot == "" {
		return Config{}, fmt.Errorf("FARM_DEFAULT_SLOT must sanitize to 1-32 characters [a-z0-9_-]")
	}
	cfg.DefaultSlot = slot

	if cfg.AutosaveInterval < time.Second {
		return Config{}, fmt.Errorf("FARM_AUTOSAVE_INTERVAL must be at least 1s")
	}
	if cfg.SessionPolicy != "takeover" && cfg.SessionPolicy != "refuse" {
		return Config{}, fmt.Errorf("FARM_SESSION_POLICY must be %q or %q, got %q", "takeover", "refuse", cfg.SessionPolicy)
	}
	if cfg.DBPath == "" {
		return Config{}, fmt.Errorf("FARM_DB_PATH must not be empty")
	}
	if cfg.LeaderboardTTL <= 0 {
		return Config{}, fmt.Errorf("FARM_LEADERBOARD_TTL must be positive")
	}
	if cfg.LeaderboardActivityWindow < 0 {
		return Config{}, fmt.Errorf("FARM_LEADERBOARD_ACTIVITY_DAYS must be zero or positive")
	}
	if cfg.LeaderboardMinCoins < 0 {
		return Config{}, fmt.Errorf("FARM_LEADERBOARD_MIN_COINS must be zero or positive")
	}

	return cfg, nil
}

func (c Config) ListenAddr() string {
	return fmt.Sprintf("%s:%d", c.ListenHost, c.ListenPort)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envIntOr(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid integer %q", key, v)
	}
	return n, nil
}

func envInt64Or(key string, fallback int64) (int64, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid integer %q", key, v)
	}
	return n, nil
}

func envFloatOr(key string, fallback float64) (float64, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid float %q", key, v)
	}
	return f, nil
}

func envDurationOr(key string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid duration %q", key, v)
	}
	return d, nil
}
