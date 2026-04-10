package acp_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nczz/kiro-discord-bot/acp"
)

func kiroCLI(t *testing.T) string {
	cli := os.Getenv("KIRO_CLI")
	if cli == "" {
		t.Skip("set KIRO_CLI to run integration tests")
	}
	return cli
}

func TestStartAndAsk(t *testing.T) {
	cli := kiroCLI(t)
	agent, err := acp.StartAgent("test-ask", cli, os.TempDir(), "", acp.AgentOptions{TrustAllTools: true})
	if err != nil {
		t.Fatalf("StartAgent: %v", err)
	}
	defer agent.Stop()

	if agent.Pid() == 0 {
		t.Fatal("expected non-zero PID")
	}
	if agent.SessionID == "" {
		t.Fatal("expected non-empty session ID")
	}
	if !agent.IsAlive() {
		t.Fatal("expected agent to be alive")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := agent.Ask(ctx, "Reply with exactly: PONG", nil)
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if !strings.Contains(resp, "PONG") {
		t.Errorf("expected PONG, got: %s", resp)
	}
}

func TestStreaming(t *testing.T) {
	cli := kiroCLI(t)
	agent, err := acp.StartAgent("test-stream", cli, os.TempDir(), "", acp.AgentOptions{TrustAllTools: true})
	if err != nil {
		t.Fatalf("StartAgent: %v", err)
	}
	defer agent.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var chunks int
	resp, err := agent.Ask(ctx, "Count from 1 to 3, one number per line.", func(chunk string) {
		chunks++
	})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if chunks == 0 {
		t.Error("expected at least one chunk")
	}
	if resp == "" {
		t.Error("expected non-empty response")
	}
	t.Logf("chunks=%d response=%q", chunks, resp[:min(len(resp), 100)])
}

func TestCancel(t *testing.T) {
	cli := kiroCLI(t)
	agent, err := acp.StartAgent("test-cancel", cli, os.TempDir(), "", acp.AgentOptions{TrustAllTools: true})
	if err != nil {
		t.Fatalf("StartAgent: %v", err)
	}
	defer agent.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err = agent.Ask(ctx, "Write a 10000 word essay about the history of computing.", nil)
	if err == nil {
		t.Log("WARN: completed before timeout")
		return
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Errorf("expected deadline exceeded, got: %v", err)
	}
}

func TestStop(t *testing.T) {
	cli := kiroCLI(t)
	agent, err := acp.StartAgent("test-stop", cli, os.TempDir(), "", acp.AgentOptions{TrustAllTools: true})
	if err != nil {
		t.Fatalf("StartAgent: %v", err)
	}

	pid := agent.Pid()
	if pid == 0 {
		t.Fatal("expected non-zero PID")
	}

	agent.Stop()

	time.Sleep(500 * time.Millisecond)
	// Verify process is gone
	proc, err := os.FindProcess(pid)
	if err == nil {
		err = proc.Signal(nil)
	}
	if err == nil {
		t.Errorf("process %d still alive after Stop", pid)
	}
}

func TestContextMemory(t *testing.T) {
	cli := kiroCLI(t)
	agent, err := acp.StartAgent("test-memory", cli, os.TempDir(), "", acp.AgentOptions{TrustAllTools: true})
	if err != nil {
		t.Fatalf("StartAgent: %v", err)
	}
	defer agent.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	_, err = agent.Ask(ctx, "Remember this code: ZETA-9001", nil)
	if err != nil {
		t.Fatalf("Ask 1: %v", err)
	}

	resp, err := agent.Ask(ctx, "What code did I just give you?", nil)
	if err != nil {
		t.Fatalf("Ask 2: %v", err)
	}
	if !strings.Contains(resp, "ZETA-9001") {
		t.Errorf("expected ZETA-9001, got: %s", resp)
	}
}

func TestPreflightCheck(t *testing.T) {
	cli := kiroCLI(t)
	if err := acp.PreflightCheck(cli); err != nil {
		t.Fatalf("PreflightCheck: %v", err)
	}
}
