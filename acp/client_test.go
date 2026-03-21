package acp_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jianghongjun/kiro-discord-bot/acp"
)

// Integration tests — require acp-bridge running at ACP_BRIDGE_URL
// Run: ACP_BRIDGE_URL=http://localhost:7800 KIRO_CLI=/path/to/kiro-cli go test ./acp/... -v -tags integration

func skipIfNoEnv(t *testing.T) (bridgeURL, kiroCLI string) {
	bridgeURL = os.Getenv("ACP_BRIDGE_URL")
	kiroCLI = os.Getenv("KIRO_CLI")
	if bridgeURL == "" || kiroCLI == "" {
		t.Skip("set ACP_BRIDGE_URL and KIRO_CLI to run integration tests")
	}
	return
}

func TestStartAndAsk(t *testing.T) {
	bridgeURL, kiroCLI := skipIfNoEnv(t)
	c := acp.NewClient(bridgeURL)

	name := "test-integration-" + t.Name()
	t.Cleanup(func() { _ = c.StopAgent(name) })

	// Start
	status, err := c.StartAgent(name, kiroCLI, os.TempDir())
	if err != nil {
		t.Fatalf("StartAgent: %v", err)
	}
	if status.State != "idle" {
		t.Fatalf("expected idle, got %s", status.State)
	}
	if status.SessionID == "" {
		t.Fatal("empty sessionId")
	}
	t.Logf("agent started: sessionId=%s", status.SessionID)

	// Ask
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := c.Ask(ctx, name, "reply with exactly: PONG")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if !strings.Contains(result.Response, "PONG") {
		t.Errorf("expected PONG in response, got: %s", result.Response)
	}
	t.Logf("response: %s", result.Response)
}

func TestAskStream(t *testing.T) {
	bridgeURL, kiroCLI := skipIfNoEnv(t)
	c := acp.NewClient(bridgeURL)

	name := "test-stream-" + t.Name()
	t.Cleanup(func() { _ = c.StopAgent(name) })

	_, err := c.StartAgent(name, kiroCLI, os.TempDir())
	if err != nil {
		t.Fatalf("StartAgent: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var chunks []string
	result, err := c.AskStream(ctx, name, "count from 1 to 3, one number per line", func(chunk string) {
		chunks = append(chunks, chunk)
	})
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}
	if len(chunks) == 0 {
		t.Error("expected at least one chunk")
	}
	if result.Response == "" {
		t.Error("empty final response")
	}
	t.Logf("chunks=%d, response=%q", len(chunks), result.Response)
}

func TestGetAgentNotFound(t *testing.T) {
	bridgeURL, _ := skipIfNoEnv(t)
	c := acp.NewClient(bridgeURL)

	_, err := c.GetAgent("does-not-exist-xyz")
	if err == nil || !strings.Contains(err.Error(), "not_found") {
		t.Errorf("expected not_found error, got: %v", err)
	}
}

func TestContextMemory(t *testing.T) {
	bridgeURL, kiroCLI := skipIfNoEnv(t)
	c := acp.NewClient(bridgeURL)

	name := "test-memory-" + t.Name()
	t.Cleanup(func() { _ = c.StopAgent(name) })

	_, err := c.StartAgent(name, kiroCLI, os.TempDir())
	if err != nil {
		t.Fatalf("StartAgent: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// First turn: set a value
	_, err = c.Ask(ctx, name, "remember this code: ZETA-9001")
	if err != nil {
		t.Fatalf("Ask 1: %v", err)
	}

	// Second turn: recall
	result, err := c.Ask(ctx, name, "what code did I just give you?")
	if err != nil {
		t.Fatalf("Ask 2: %v", err)
	}
	if !strings.Contains(result.Response, "ZETA-9001") {
		t.Errorf("expected ZETA-9001 in response, got: %s", result.Response)
	}
	t.Logf("memory test passed: %s", result.Response)
}
