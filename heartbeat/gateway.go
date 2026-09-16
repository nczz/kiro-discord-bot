package heartbeat

import (
	"context"
	"fmt"
	"log"
	"time"
)

const (
	DefaultGatewayStaleAfter           = 180 * time.Second
	DefaultGatewayReconnectTimeout     = 45 * time.Second
	DefaultGatewayMaxReconnectAttempts = 3
)

type GatewayWatchdogConfig struct {
	Enabled              bool
	StaleAfter           time.Duration
	ReconnectTimeout     time.Duration
	MaxReconnectAttempts int
}

func NormalizeGatewayWatchdogConfig(c GatewayWatchdogConfig) GatewayWatchdogConfig {
	if c.StaleAfter <= 0 {
		c.StaleAfter = DefaultGatewayStaleAfter
	}
	if c.ReconnectTimeout <= 0 {
		c.ReconnectTimeout = DefaultGatewayReconnectTimeout
	}
	if c.MaxReconnectAttempts <= 0 {
		c.MaxReconnectAttempts = DefaultGatewayMaxReconnectAttempts
	}
	return c
}

type GatewayStatus struct {
	DataReady            bool
	LastHeartbeatAck     time.Time
	LastHeartbeatSent    time.Time
	LastReady            time.Time
	LastResumed          time.Time
	LastDisconnect       time.Time
	LastReconnectAttempt time.Time
	ReconnectAttempts    int
	RecoveryInProgress   bool
	LastReconnectError   string
}

type GatewayDeps interface {
	GatewayStatus() GatewayStatus
	RecoverGateway(ctx context.Context, reason string) error
	ExitGatewayFailure(reason string, err error)
}

type GatewayTask struct {
	deps                GatewayDeps
	config              GatewayWatchdogConfig
	consecutiveFailures int
	now                 func() time.Time
}

func NewGatewayTask(deps GatewayDeps, config GatewayWatchdogConfig) *GatewayTask {
	return &GatewayTask{deps: deps, config: NormalizeGatewayWatchdogConfig(config), now: time.Now}
}

func (g *GatewayTask) Name() string               { return "discord_gateway" }
func (g *GatewayTask) ShouldRun(_ time.Time) bool { return true }

func (g *GatewayTask) Run() error {
	if g.deps == nil || !g.config.Enabled {
		return nil
	}
	status := g.deps.GatewayStatus()
	now := g.now()
	if status.RecoveryInProgress {
		if !status.LastReconnectAttempt.IsZero() && now.Sub(status.LastReconnectAttempt) >= g.config.ReconnectTimeout {
			g.consecutiveFailures++
			err := fmt.Errorf("gateway recovery exceeded timeout %s", g.config.ReconnectTimeout.Round(time.Second))
			log.Printf("[discord-gateway] recovery still in progress attempt=%d/%d: %v", g.consecutiveFailures, g.config.MaxReconnectAttempts, err)
			if g.consecutiveFailures >= g.config.MaxReconnectAttempts {
				g.deps.ExitGatewayFailure("gateway_recovery_timeout", err)
			}
			return err
		}
		return nil
	}
	stale, reason := status.StaleReason(now, g.config.StaleAfter)
	if !stale {
		g.consecutiveFailures = 0
		return nil
	}

	log.Printf("[discord-gateway] stale gateway detected: %s", reason)
	ctx, cancel := context.WithTimeout(context.Background(), g.config.ReconnectTimeout)
	defer cancel()
	if err := g.deps.RecoverGateway(ctx, reason); err != nil {
		g.consecutiveFailures++
		log.Printf("[discord-gateway] recovery failed attempt=%d/%d: %v", g.consecutiveFailures, g.config.MaxReconnectAttempts, err)
		if g.consecutiveFailures >= g.config.MaxReconnectAttempts {
			g.deps.ExitGatewayFailure(reason, err)
		}
		return fmt.Errorf("recover discord gateway after %s: %w", reason, err)
	}

	log.Printf("[discord-gateway] recovery succeeded after %s", reason)
	g.consecutiveFailures = 0
	return nil
}

func (s GatewayStatus) StaleReason(now time.Time, staleAfter time.Duration) (bool, string) {
	if staleAfter <= 0 {
		staleAfter = DefaultGatewayStaleAfter
	}
	if now.IsZero() {
		now = time.Now()
	}
	if s.RecoveryInProgress {
		return false, ""
	}
	if !s.DataReady {
		since := mostRecentTime(s.LastDisconnect, s.LastReconnectAttempt, s.LastReady, s.LastResumed)
		if since.IsZero() || now.Sub(since) >= staleAfter {
			return true, "gateway_not_ready"
		}
		return false, ""
	}
	if s.LastHeartbeatAck.IsZero() {
		return false, ""
	}
	age := now.Sub(s.LastHeartbeatAck)
	if age >= staleAfter {
		return true, fmt.Sprintf("heartbeat_ack_stale age=%s threshold=%s", age.Round(time.Second), staleAfter.Round(time.Second))
	}
	return false, ""
}

func mostRecentTime(values ...time.Time) time.Time {
	var out time.Time
	for _, value := range values {
		if value.After(out) {
			out = value
		}
	}
	return out
}
