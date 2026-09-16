package main

import (
	"testing"
	"time"
)

func TestLoadConfigGatewayWatchdogDefaultsEnabled(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "token")
	t.Setenv("DISCORD_GATEWAY_WATCHDOG_ENABLED", "")
	t.Setenv("DISCORD_GATEWAY_STALE_AFTER_SEC", "")
	t.Setenv("DISCORD_GATEWAY_RECONNECT_TIMEOUT_SEC", "")
	t.Setenv("DISCORD_GATEWAY_MAX_RECONNECT_ATTEMPTS", "")

	cfg := loadConfig()
	if !cfg.GatewayWatchdog.Enabled {
		t.Fatal("GatewayWatchdog.Enabled = false, want true by default")
	}
	if cfg.GatewayWatchdog.StaleAfter != 180*time.Second || cfg.GatewayWatchdog.ReconnectTimeout != 45*time.Second || cfg.GatewayWatchdog.MaxReconnectAttempts != 3 {
		t.Fatalf("GatewayWatchdog = %+v, want 180s/45s/3", cfg.GatewayWatchdog)
	}
}

func TestLoadConfigGatewayWatchdogOverrides(t *testing.T) {
	t.Setenv("DISCORD_TOKEN", "token")
	t.Setenv("DISCORD_GATEWAY_WATCHDOG_ENABLED", "false")
	t.Setenv("DISCORD_GATEWAY_STALE_AFTER_SEC", "240")
	t.Setenv("DISCORD_GATEWAY_RECONNECT_TIMEOUT_SEC", "30")
	t.Setenv("DISCORD_GATEWAY_MAX_RECONNECT_ATTEMPTS", "5")

	cfg := loadConfig()
	if cfg.GatewayWatchdog.Enabled {
		t.Fatal("GatewayWatchdog.Enabled = true, want false")
	}
	if cfg.GatewayWatchdog.StaleAfter != 240*time.Second || cfg.GatewayWatchdog.ReconnectTimeout != 30*time.Second || cfg.GatewayWatchdog.MaxReconnectAttempts != 5 {
		t.Fatalf("GatewayWatchdog = %+v, want 240s/30s/5", cfg.GatewayWatchdog)
	}
}
