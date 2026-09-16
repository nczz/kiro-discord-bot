package bot

import (
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/heartbeat"
	L "github.com/nczz/kiro-discord-bot/locale"
)

func TestGatewayStatusReadsDiscordHeartbeatAndEvents(t *testing.T) {
	now := time.Date(2026, 9, 16, 1, 0, 0, 0, time.UTC)
	b := &Bot{discord: &discordgo.Session{DataReady: true, LastHeartbeatAck: now.Add(-5 * time.Second), LastHeartbeatSent: now.Add(-6 * time.Second)}}
	b.gatewayLastReadyAt = now.Add(-time.Minute)
	b.gatewayLastReconnectAttemptAt = now.Add(-30 * time.Second)
	b.gatewayReconnectAttempts = 2

	status := b.gatewayStatus()
	if !status.DataReady || !status.LastHeartbeatAck.Equal(now.Add(-5*time.Second)) || !status.LastReady.Equal(now.Add(-time.Minute)) || status.ReconnectAttempts != 2 {
		t.Fatalf("gatewayStatus = %+v", status)
	}
}

func TestGatewayDoctorSummaryReportsHealthyGateway(t *testing.T) {
	L.Load("en")
	now := time.Now().UTC()
	b := &Bot{
		discord:         &discordgo.Session{DataReady: true, LastHeartbeatAck: now.Add(-5 * time.Second), LastHeartbeatSent: now.Add(-6 * time.Second)},
		gatewayWatchdog: heartbeat.GatewayWatchdogConfig{Enabled: true, StaleAfter: 3 * time.Minute, ReconnectTimeout: 45 * time.Second, MaxReconnectAttempts: 3},
	}
	b.gatewayLastReadyAt = now.Add(-time.Minute)

	got := b.gatewayDoctorSummary()
	for _, want := range []string{"Gateway watchdog", "`healthy`", "heartbeat ACK age", "max attempts"} {
		if !strings.Contains(got, want) {
			t.Fatalf("gateway doctor missing %q:\n%s", want, got)
		}
	}
}

func TestGatewayDoctorSummaryReportsDisabled(t *testing.T) {
	L.Load("en")
	b := &Bot{gatewayWatchdog: heartbeat.GatewayWatchdogConfig{Enabled: false}}
	if got := b.gatewayDoctorSummary(); !strings.Contains(got, "`disabled`") {
		t.Fatalf("disabled gateway doctor = %q", got)
	}
}
