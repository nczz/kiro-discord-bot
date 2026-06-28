package acp

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// Gated omp ACP smoke test. Run with:
//
//	RUN_OMP_SMOKE=1 OMP_PATH=$(which omp) go test ./acp/ -run TestOmpSmoke -v
//
// Exercises the real ompProfile through StartAgent: handshake (configOptions
// parsing), a prompt turn (streaming + stopReason), and per-turn USD usage.
func TestOmpSmoke(t *testing.T) {
	if os.Getenv("RUN_OMP_SMOKE") == "" {
		t.Skip("set RUN_OMP_SMOKE=1 (and OMP_PATH) to run")
	}
	omp := os.Getenv("OMP_PATH")
	if omp == "" {
		omp = "omp"
	}
	agent, err := StartAgent("omp-smoke", omp, os.TempDir(), "", AgentOptions{
		Dialect:       DialectOmp,
		TrustAllTools: true,
	})
	if err != nil {
		t.Fatalf("StartAgent(omp): %v", err)
	}
	defer agent.Stop()

	// configOptions parsed into models/modes.
	if len(agent.AvailableModels()) == 0 {
		t.Errorf("omp: expected configOptions models to populate AvailableModels")
	}
	if len(agent.AvailableModes()) == 0 {
		t.Errorf("omp: expected configOptions modes to populate AvailableModes")
	}
	t.Logf("omp version=%s currentModel=%s modes=%d models=%d",
		agent.AgentVersion(), agent.CurrentModelID(), len(agent.AvailableModes()), len(agent.AvailableModels()))
	if models := agent.AvailableModels(); len(models) > 1 {
		wanted := models[0].ModelID
		if wanted == agent.CurrentModelID() {
			wanted = models[1].ModelID
		}
		agent.Stop()
		agent, err = StartAgent("omp-smoke", omp, os.TempDir(), wanted, AgentOptions{
			Dialect:       DialectOmp,
			TrustAllTools: true,
		})
		if err != nil {
			t.Fatalf("StartAgent(omp, model=%s): %v", wanted, err)
		}
		defer agent.Stop()
		if got := agent.CurrentModelID(); got != wanted {
			t.Fatalf("omp current model after StartAgent model override = %q, want %q", got, wanted)
		}
		t.Logf("omp model override currentModel=%s", agent.CurrentModelID())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := agent.Ask(ctx, "Reply with exactly: OK_OMP", nil)
	if err != nil {
		t.Fatalf("omp Ask: %v", err)
	}
	if !strings.Contains(resp, "OK_OMP") {
		t.Errorf("omp response missing OK_OMP: %.120q", resp)
	}
	if sr := agent.StopReason(); sr == "" {
		t.Errorf("omp: expected a stopReason after prompt")
	} else {
		t.Logf("omp stopReason=%s", sr)
	}
	// Per-turn USD usage from usage_update.
	m := agent.TurnMetrics()
	t.Logf("omp turn metrics: ctx=%.2f%% metering=%+v", m.ContextUsage, m.MeteringUsage)
	hasUSD := false
	for _, item := range m.MeteringUsage {
		if strings.EqualFold(item.Unit, "USD") && item.Value > 0 {
			hasUSD = true
		}
	}
	if !hasUSD {
		t.Logf("omp: no USD metering captured this turn (may be 0 if cached); ctx=%.2f", m.ContextUsage)
	}
}
