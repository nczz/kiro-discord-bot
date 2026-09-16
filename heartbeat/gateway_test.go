package heartbeat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type gatewayDepsStub struct {
	status     GatewayStatus
	recoveries int
	recoverErr error
	exits      int
	exitReason string
	exitErr    error
}

func (s *gatewayDepsStub) GatewayStatus() GatewayStatus { return s.status }
func (s *gatewayDepsStub) RecoverGateway(context.Context, string) error {
	s.recoveries++
	return s.recoverErr
}
func (s *gatewayDepsStub) ExitGatewayFailure(reason string, err error) {
	s.exits++
	s.exitReason = reason
	s.exitErr = err
}

func TestGatewayTaskSkipsFreshGateway(t *testing.T) {
	now := time.Date(2026, 9, 16, 1, 0, 0, 0, time.UTC)
	deps := &gatewayDepsStub{status: GatewayStatus{DataReady: true, LastHeartbeatAck: now.Add(-30 * time.Second)}}
	task := NewGatewayTask(deps, GatewayWatchdogConfig{Enabled: true, StaleAfter: time.Minute})
	task.now = func() time.Time { return now }

	if err := task.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if deps.recoveries != 0 || deps.exits != 0 {
		t.Fatalf("fresh gateway recoveries=%d exits=%d, want 0", deps.recoveries, deps.exits)
	}
}

func TestGatewayTaskRecoversStaleHeartbeatAck(t *testing.T) {
	now := time.Date(2026, 9, 16, 1, 0, 0, 0, time.UTC)
	deps := &gatewayDepsStub{status: GatewayStatus{DataReady: true, LastHeartbeatAck: now.Add(-4 * time.Minute)}}
	task := NewGatewayTask(deps, GatewayWatchdogConfig{Enabled: true, StaleAfter: 3 * time.Minute})
	task.now = func() time.Time { return now }

	if err := task.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if deps.recoveries != 1 {
		t.Fatalf("recoveries=%d, want 1", deps.recoveries)
	}
}

func TestGatewayTaskExitsAfterRepeatedRecoveryFailures(t *testing.T) {
	now := time.Date(2026, 9, 16, 1, 0, 0, 0, time.UTC)
	boom := errors.New("gateway still wedged")
	deps := &gatewayDepsStub{status: GatewayStatus{DataReady: true, LastHeartbeatAck: now.Add(-10 * time.Minute)}, recoverErr: boom}
	task := NewGatewayTask(deps, GatewayWatchdogConfig{Enabled: true, StaleAfter: time.Minute, MaxReconnectAttempts: 2})
	task.now = func() time.Time { return now }

	if err := task.Run(); err == nil {
		t.Fatal("first Run err = nil, want recovery error")
	}
	if deps.exits != 0 {
		t.Fatalf("exits after first failure=%d, want 0", deps.exits)
	}
	if err := task.Run(); err == nil {
		t.Fatal("second Run err = nil, want recovery error")
	}
	if deps.exits != 1 || deps.exitErr != boom || !strings.Contains(deps.exitReason, "heartbeat_ack_stale") {
		t.Fatalf("exit = (%d, %q, %v), want one stale exit with boom", deps.exits, deps.exitReason, deps.exitErr)
	}
}

func TestGatewayTaskDisabledNoop(t *testing.T) {
	now := time.Date(2026, 9, 16, 1, 0, 0, 0, time.UTC)
	deps := &gatewayDepsStub{status: GatewayStatus{DataReady: true, LastHeartbeatAck: now.Add(-10 * time.Minute)}}
	task := NewGatewayTask(deps, GatewayWatchdogConfig{Enabled: false, StaleAfter: time.Minute})
	task.now = func() time.Time { return now }

	if err := task.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if deps.recoveries != 0 || deps.exits != 0 {
		t.Fatalf("disabled recoveries=%d exits=%d, want 0", deps.recoveries, deps.exits)
	}
}

func TestGatewayStatusDetectsLongNotReadyState(t *testing.T) {
	now := time.Date(2026, 9, 16, 1, 0, 0, 0, time.UTC)
	status := GatewayStatus{DataReady: false, LastDisconnect: now.Add(-4 * time.Minute)}
	stale, reason := status.StaleReason(now, 3*time.Minute)
	if !stale || reason != "gateway_not_ready" {
		t.Fatalf("StaleReason = (%v, %q), want gateway_not_ready", stale, reason)
	}
}

func TestGatewayTaskEscalatesHungRecovery(t *testing.T) {
	now := time.Date(2026, 9, 16, 1, 0, 0, 0, time.UTC)
	deps := &gatewayDepsStub{status: GatewayStatus{RecoveryInProgress: true, LastReconnectAttempt: now.Add(-2 * time.Minute)}}
	task := NewGatewayTask(deps, GatewayWatchdogConfig{Enabled: true, ReconnectTimeout: time.Minute, MaxReconnectAttempts: 2})
	task.now = func() time.Time { return now }

	if err := task.Run(); err == nil {
		t.Fatal("first Run err = nil, want hung recovery timeout")
	}
	if deps.exits != 0 {
		t.Fatalf("exits after first hung recovery=%d, want 0", deps.exits)
	}
	if err := task.Run(); err == nil {
		t.Fatal("second Run err = nil, want hung recovery timeout")
	}
	if deps.exits != 1 || deps.exitReason != "gateway_recovery_timeout" {
		t.Fatalf("exit = (%d, %q), want gateway_recovery_timeout", deps.exits, deps.exitReason)
	}
}
