package bot

import (
	"os"
	"strings"
	"time"
)

// WebShareConfig controls the bot-side delegated WebShare host integration.
type WebShareConfig struct {
	Enabled            bool
	RelayURL           string
	PublicBaseURL      string
	HostTokenFile      string
	HostToken          string
	MaxFrameBytes      int64
	ReconnectInitialMS int
	ReconnectMaxMS     int
}

func (c WebShareConfig) reconnectInitial() time.Duration {
	if c.ReconnectInitialMS <= 0 {
		return time.Second
	}
	return time.Duration(c.ReconnectInitialMS) * time.Millisecond
}

func (c WebShareConfig) reconnectMax() time.Duration {
	if c.ReconnectMaxMS <= 0 {
		return 30 * time.Second
	}
	return time.Duration(c.ReconnectMaxMS) * time.Millisecond
}

func (c WebShareConfig) frameLimit() int64 {
	if c.MaxFrameBytes <= 0 {
		return 4 * 1024 * 1024
	}
	return c.MaxFrameBytes
}

func (c WebShareConfig) resolvedHostToken() string {
	if strings.TrimSpace(c.HostToken) != "" {
		return strings.TrimSpace(c.HostToken)
	}
	path := strings.TrimSpace(c.HostTokenFile)
	if path == "" {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func (c WebShareConfig) ready() bool {
	return c.Enabled && strings.TrimSpace(c.RelayURL) != "" && strings.TrimSpace(c.PublicBaseURL) != "" && c.resolvedHostToken() != ""
}
