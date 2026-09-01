package websharerelay

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultAddr            = ":8080"
	DefaultMaxRooms        = 1000
	DefaultMaxPeersPerRoom = 32
	DefaultMaxFrameBytes   = 4 * 1024 * 1024
	DefaultWriteTimeout    = 30 * time.Second
)

type Config struct {
	Addr                string
	PublicBaseURL       string
	HostToken           string
	HostTokenFile       string
	TrustProxy          bool
	MaxRooms            int
	MaxPeersPerRoom     int
	MaxFrameBytes       int64
	HostIdleTimeout     time.Duration
	GuestIdleTimeout    time.Duration
	WriteTimeout        time.Duration
	LogLevel            string
	MetricsAddr         string
}

func DefaultConfig() Config {
	return Config{
		Addr:                DefaultAddr,
		MaxRooms:            DefaultMaxRooms,
		MaxPeersPerRoom:     DefaultMaxPeersPerRoom,
		MaxFrameBytes:       DefaultMaxFrameBytes,
		WriteTimeout:        DefaultWriteTimeout,
		LogLevel:            "info",
	}
}

func ConfigFromEnv() (Config, error) {
	cfg := DefaultConfig()
	cfg.Addr = envString("RELAY_ADDR", cfg.Addr)
	cfg.PublicBaseURL = strings.TrimSpace(os.Getenv("RELAY_PUBLIC_BASE_URL"))
	cfg.HostToken = strings.TrimSpace(os.Getenv("RELAY_HOST_TOKEN"))
	cfg.HostTokenFile = strings.TrimSpace(os.Getenv("RELAY_HOST_TOKEN_FILE"))
	cfg.TrustProxy = envBool("RELAY_TRUST_PROXY", cfg.TrustProxy)
	cfg.LogLevel = strings.ToLower(strings.TrimSpace(envString("RELAY_LOG_LEVEL", cfg.LogLevel)))
	cfg.MetricsAddr = strings.TrimSpace(os.Getenv("RELAY_METRICS_ADDR"))

	var err error
	if cfg.MaxRooms, err = envInt("RELAY_MAX_ROOMS", cfg.MaxRooms); err != nil {
		return cfg, err
	}
	if cfg.MaxPeersPerRoom, err = envInt("RELAY_MAX_PEERS_PER_ROOM", cfg.MaxPeersPerRoom); err != nil {
		return cfg, err
	}
	if cfg.MaxFrameBytes, err = envInt64("RELAY_MAX_FRAME_BYTES", cfg.MaxFrameBytes); err != nil {
		return cfg, err
	}
	if cfg.HostIdleTimeout, err = envDuration("RELAY_HOST_IDLE_TIMEOUT", cfg.HostIdleTimeout); err != nil {
		return cfg, err
	}
	if cfg.GuestIdleTimeout, err = envDuration("RELAY_GUEST_IDLE_TIMEOUT", cfg.GuestIdleTimeout); err != nil {
		return cfg, err
	}
	if cfg.WriteTimeout, err = envDuration("RELAY_WRITE_TIMEOUT", cfg.WriteTimeout); err != nil {
		return cfg, err
	}

	if cfg.HostToken == "" && cfg.HostTokenFile != "" {
		b, err := os.ReadFile(cfg.HostTokenFile)
		if err != nil {
			return cfg, fmt.Errorf("read RELAY_HOST_TOKEN_FILE: %w", err)
		}
		cfg.HostToken = strings.TrimSpace(string(b))
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (cfg Config) Validate() error {
	if strings.TrimSpace(cfg.Addr) == "" {
		return fmt.Errorf("RELAY_ADDR must not be empty")
	}
	if cfg.MaxRooms <= 0 {
		return fmt.Errorf("RELAY_MAX_ROOMS must be > 0")
	}
	if cfg.MaxPeersPerRoom <= 0 {
		return fmt.Errorf("RELAY_MAX_PEERS_PER_ROOM must be > 0")
	}
	if cfg.MaxFrameBytes < 4 {
		return fmt.Errorf("RELAY_MAX_FRAME_BYTES must be at least 4")
	}
	if cfg.HostIdleTimeout < 0 || cfg.GuestIdleTimeout < 0 || cfg.WriteTimeout <= 0 {
		return fmt.Errorf("relay timeouts must be non-negative and RELAY_WRITE_TIMEOUT must be > 0")
	}
	switch cfg.LogLevel {
	case "", "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("RELAY_LOG_LEVEL must be debug, info, warn, or error")
	}
	return nil
}

func envString(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}

func envBool(name string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return fallback
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return fallback
	}
}

func envInt(name string, fallback int) (int, error) {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	return n, nil
}

func envInt64(name string, fallback int64) (int64, error) {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	return n, nil
}

func envDuration(name string, fallback time.Duration) (time.Duration, error) {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err == nil {
		return d, nil
	}
	seconds, secErr := strconv.ParseInt(v, 10, 64)
	if secErr != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	return time.Duration(seconds) * time.Second, nil
}
