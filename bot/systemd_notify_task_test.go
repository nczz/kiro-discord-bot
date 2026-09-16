package bot

import (
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/heartbeat"
)

func TestSystemdNotifyTaskDisabledWithoutSystemdEnv(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", "")
	t.Setenv("WATCHDOG_USEC", "")
	task := &systemdNotifyTask{}
	if task.ShouldRun(time.Now()) {
		t.Fatal("ShouldRun = true without systemd env")
	}
}

func TestSystemdNotifyTaskSuppressesWatchdogWhenGatewayStale(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", "/tmp/missing-notify.sock")
	t.Setenv("WATCHDOG_USEC", "120000000")
	now := time.Now().UTC()
	b := &Bot{
		discord:         &discordgo.Session{DataReady: true, LastHeartbeatAck: now.Add(-10 * time.Minute)},
		gatewayWatchdog: heartbeat.GatewayWatchdogConfig{Enabled: true, StaleAfter: time.Minute},
	}
	task := &systemdNotifyTask{bot: b}
	if !task.ShouldRun(now) {
		t.Fatal("ShouldRun = false with systemd env")
	}
	if err := task.Run(); err != nil {
		t.Fatalf("Run with stale gateway should suppress notify without socket error, got %v", err)
	}
}
