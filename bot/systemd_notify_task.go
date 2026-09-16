package bot

import (
	"os"
	"time"

	"github.com/nczz/kiro-discord-bot/heartbeat"
)

type systemdNotifyTask struct {
	bot *Bot
}

func (t *systemdNotifyTask) Name() string { return "systemd_notify" }

func (t *systemdNotifyTask) ShouldRun(_ time.Time) bool {
	return os.Getenv("NOTIFY_SOCKET") != "" && os.Getenv("WATCHDOG_USEC") != ""
}

func (t *systemdNotifyTask) Run() error {
	if t == nil || t.bot == nil {
		return nil
	}
	status := t.bot.gatewayStatus()
	if !status.DataReady || status.RecoveryInProgress {
		return nil
	}
	cfg := heartbeat.NormalizeGatewayWatchdogConfig(t.bot.gatewayWatchdog)
	if cfg.Enabled {
		stale, _ := status.StaleReason(time.Now().UTC(), cfg.StaleAfter)
		if stale {
			return nil
		}
	}
	return systemdNotify("WATCHDOG=1")
}
