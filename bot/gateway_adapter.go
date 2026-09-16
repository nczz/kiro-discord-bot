package bot

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/heartbeat"
	"github.com/nczz/kiro-discord-bot/internal/textutil"
	L "github.com/nczz/kiro-discord-bot/locale"
)

type GatewayWatchdogConfig = heartbeat.GatewayWatchdogConfig

type gatewayAdapter struct {
	bot *Bot
}

func (a *gatewayAdapter) GatewayStatus() heartbeat.GatewayStatus {
	if a == nil || a.bot == nil {
		return heartbeat.GatewayStatus{}
	}
	return a.bot.gatewayStatus()
}

func (a *gatewayAdapter) RecoverGateway(ctx context.Context, reason string) error {
	if a == nil || a.bot == nil {
		return errors.New("bot gateway adapter is not initialized")
	}
	return a.bot.recoverDiscordGateway(ctx, reason)
}

func (a *gatewayAdapter) ExitGatewayFailure(reason string, err error) {
	log.Printf("[discord-gateway] unrecoverable gateway failure reason=%s err=%v; exiting for process manager restart", reason, err)
	os.Exit(2)
}

func (b *Bot) markGatewayReady() {
	b.gatewayStateMu.Lock()
	b.gatewayLastReadyAt = time.Now().UTC()
	b.gatewayLastReconnectError = ""
	b.gatewayStateMu.Unlock()
}

func (b *Bot) markGatewayResumed() {
	b.gatewayStateMu.Lock()
	b.gatewayLastResumedAt = time.Now().UTC()
	b.gatewayLastReconnectError = ""
	b.gatewayStateMu.Unlock()
}

func (b *Bot) markGatewayDisconnect() {
	b.gatewayStateMu.Lock()
	b.gatewayLastDisconnectAt = time.Now().UTC()
	b.gatewayStateMu.Unlock()
}

func (b *Bot) gatewayStatus() heartbeat.GatewayStatus {
	var out heartbeat.GatewayStatus
	if b == nil {
		return out
	}
	if b.discord != nil {
		b.discord.RLock()
		out.DataReady = b.discord.DataReady
		out.LastHeartbeatAck = b.discord.LastHeartbeatAck
		out.LastHeartbeatSent = b.discord.LastHeartbeatSent
		b.discord.RUnlock()
	}
	b.gatewayStateMu.RLock()
	out.LastReady = b.gatewayLastReadyAt
	out.LastResumed = b.gatewayLastResumedAt
	out.LastDisconnect = b.gatewayLastDisconnectAt
	out.LastReconnectAttempt = b.gatewayLastReconnectAttemptAt
	out.ReconnectAttempts = b.gatewayReconnectAttempts
	out.RecoveryInProgress = b.gatewayRecoveryInProgress
	out.LastReconnectError = b.gatewayLastReconnectError
	b.gatewayStateMu.RUnlock()
	return out
}

func (b *Bot) recoverDiscordGateway(ctx context.Context, reason string) error {
	if b == nil || b.discord == nil {
		return errors.New("discord session is not initialized")
	}
	if !b.beginGatewayRecovery() {
		return errors.New("gateway recovery already in progress")
	}

	result := make(chan error, 1)
	go func() {
		defer b.endGatewayRecovery()
		result <- b.reopenDiscordGateway(reason)
	}()

	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		b.recordGatewayReconnectError(ctx.Err())
		return ctx.Err()
	}
}

func (b *Bot) beginGatewayRecovery() bool {
	b.gatewayStateMu.Lock()
	defer b.gatewayStateMu.Unlock()
	if b.gatewayRecoveryInProgress {
		return false
	}
	b.gatewayRecoveryInProgress = true
	b.gatewayLastReconnectAttemptAt = time.Now().UTC()
	b.gatewayReconnectAttempts++
	return true
}

func (b *Bot) endGatewayRecovery() {
	b.gatewayStateMu.Lock()
	b.gatewayRecoveryInProgress = false
	b.gatewayStateMu.Unlock()
}

func (b *Bot) recordGatewayReconnectError(err error) {
	b.gatewayStateMu.Lock()
	if err == nil {
		b.gatewayLastReconnectError = ""
	} else {
		b.gatewayLastReconnectError = err.Error()
	}
	b.gatewayStateMu.Unlock()
}

func (b *Bot) reopenDiscordGateway(reason string) error {
	log.Printf("[discord-gateway] reconnecting gateway reason=%s", reason)
	if err := b.discord.Close(); err != nil && !errors.Is(err, discordgo.ErrWSNotFound) {
		log.Printf("[discord-gateway] close before reconnect returned error: %v", err)
	}
	if err := b.discord.Open(); err != nil {
		err = fmt.Errorf("open discord gateway: %w", err)
		b.recordGatewayReconnectError(err)
		return err
	}
	b.recordGatewayReconnectError(nil)
	return nil
}

func (b *Bot) gatewayDoctorSummary() string {
	cfg := heartbeat.NormalizeGatewayWatchdogConfig(b.gatewayWatchdog)
	if !cfg.Enabled {
		return L.Get("doctor.discord.gateway.disabled")
	}
	status := b.gatewayStatus()
	now := time.Now().UTC()
	stale, reason := status.StaleReason(now, cfg.StaleAfter)
	state := L.Get("doctor.discord.gateway.state.healthy")
	if stale {
		state = L.Getf("doctor.discord.gateway.state.stale", reason)
	} else if status.RecoveryInProgress {
		state = L.Get("doctor.discord.gateway.state.recovering")
	} else if !status.DataReady {
		state = L.Get("doctor.discord.gateway.state.not_ready")
	}

	ackAge := L.Get("doctor.value.unknown")
	if !status.LastHeartbeatAck.IsZero() {
		ackAge = textutil.FormatUptime(now.Sub(status.LastHeartbeatAck))
	}
	lastReady := formatGatewayTime(status.LastReady)
	lastResumed := formatGatewayTime(status.LastResumed)
	lastDisconnect := formatGatewayTime(status.LastDisconnect)
	lastAttempt := formatGatewayTime(status.LastReconnectAttempt)
	lastErr := status.LastReconnectError
	if lastErr == "" {
		lastErr = L.Get("doctor.value.none")
	}

	return L.Getf("doctor.discord.gateway.summary", state, ackAge, textutil.FormatUptime(cfg.StaleAfter), textutil.FormatUptime(cfg.ReconnectTimeout), cfg.MaxReconnectAttempts, lastReady, lastResumed, lastDisconnect, lastAttempt, status.ReconnectAttempts, lastErr)
}

func formatGatewayTime(t time.Time) string {
	if t.IsZero() {
		return L.Get("doctor.value.never")
	}
	return t.UTC().Format(time.RFC3339)
}
