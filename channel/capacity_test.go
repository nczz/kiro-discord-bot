package channel

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nczz/kiro-discord-bot/acp"
	L "github.com/nczz/kiro-discord-bot/locale"
)

func TestAgentCapacityAllowsStartWhenMemoryAndCPULookHealthy(t *testing.T) {
	m := NewManager(ManagerConfig{DataDir: t.TempDir()})
	defer m.StopAll()

	m.mu.Lock()
	status := m.agentCapacityStatusLocked(agentCapacitySnapshot{
		CPUs:                 2,
		LoadKnown:            true,
		Load1:                0.5,
		MemoryKnown:          true,
		MemoryAvailableBytes: agentMinFreeMemoryBytes + agentEstimatedMemoryBytes,
	})
	m.mu.Unlock()

	if !status.Allowed {
		t.Fatalf("capacity status allowed = false, reason=%s", status.Reason)
	}
}

func TestAgentCapacityAllowsTmiAgentSizedHostCurrentSnapshot(t *testing.T) {
	m := NewManager(ManagerConfig{DataDir: t.TempDir()})
	defer m.StopAll()

	m.mu.Lock()
	status := m.agentCapacityStatusLocked(agentCapacitySnapshot{
		CPUs:                 1,
		LoadKnown:            true,
		Load1:                0.21,
		MemoryKnown:          true,
		MemoryAvailableBytes: 555488 * 1024,
	})
	m.mu.Unlock()

	if !status.Allowed {
		t.Fatalf("tmi-agent sized capacity allowed = false, reason=%s", status.Reason)
	}
}

func TestAgentCapacityUsesMeasuredAgentMemoryWhenAvailable(t *testing.T) {
	m := NewManager(ManagerConfig{DataDir: t.TempDir()})
	defer m.StopAll()

	m.mu.Lock()
	status := m.agentCapacityStatusLocked(agentCapacitySnapshot{
		CPUs:                     1,
		MemoryKnown:              true,
		MemoryAvailableBytes:     400 * 1024 * 1024,
		MeasuredAgentMemoryBytes: 128 * 1024 * 1024,
		MeasuredAgentSamples:     2,
	})
	m.mu.Unlock()

	if !status.Allowed {
		t.Fatalf("capacity with measured memory allowed = false, reason=%s", status.Reason)
	}
	if status.EstimatedAgentMemory != 128*1024*1024 || status.MeasuredAgentSamples != 2 {
		t.Fatalf("measured estimate = %d samples=%d, want 128MiB samples=2", status.EstimatedAgentMemory, status.MeasuredAgentSamples)
	}
}

func TestAgentCapacityFallbackUsesCPUWhenResourceSignalsUnknown(t *testing.T) {
	m := NewManager(ManagerConfig{DataDir: t.TempDir()})
	defer m.StopAll()
	m.capacitySnapshot = func() agentCapacitySnapshot { return agentCapacitySnapshot{CPUs: 1} }

	m.mu.Lock()
	m.tempAgents = 3
	status := m.agentCapacityStatusLocked(agentCapacitySnapshot{CPUs: 1})
	m.mu.Unlock()

	if status.Allowed || status.Reason != "fallback_active_limit" {
		t.Fatalf("capacity status = %+v, want fallback limit rejection", status)
	}
}

func TestAgentCapacityModeOffBypassesResourceGate(t *testing.T) {
	m := NewManager(ManagerConfig{DataDir: t.TempDir(), AgentCapacityMode: " OFF "})
	defer m.StopAll()
	m.capacitySnapshot = func() agentCapacitySnapshot {
		return agentCapacitySnapshot{CPUs: 1, MemoryKnown: true, MemoryAvailableBytes: 0}
	}

	oldWorker := newWorker("thread-idle", &fakeWorkerAgent{}, 1, 30, 1, 0, nil, "")

	m.mu.Lock()
	m.threadAgents["idle"] = &threadAgentEntry{threadID: "idle", parentChannelID: "parent", worker: oldWorker, agent: &acp.Agent{}, lastActivity: time.Now().Add(-time.Hour)}
	err := m.ensureAgentCapacityLocked("thread", "new")
	_, idleExists := m.threadAgents["idle"]
	m.mu.Unlock()

	if err != nil {
		t.Fatalf("ensureAgentCapacityLocked error = %v, want nil when AGENT_CAPACITY_MODE=off", err)
	}
	if !idleExists {
		t.Fatal("idle thread agent was reclaimed while capacity gate was off")
	}
}

func TestParseDarwinResourceSignals(t *testing.T) {
	load, ok := parseDarwinLoad1("{ 2.75 2.10 1.80 }")
	if !ok || load != 2.75 {
		t.Fatalf("parseDarwinLoad1 = %v/%v, want 2.75/true", load, ok)
	}
	available, ok := parseDarwinVMStatAvailable(`Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                               10.
Pages active:                             99.
Pages inactive:                           20.
Pages speculative:                        30.
`, 4096)
	if !ok || available != 60*4096 {
		t.Fatalf("parseDarwinVMStatAvailable = %d/%v, want %d/true", available, ok, 60*4096)
	}
}

func TestAgentCapacityReclaimsIdleBeforeRefusing(t *testing.T) {
	L.Load("en")
	m := NewManager(ManagerConfig{DataDir: t.TempDir()})
	defer m.StopAll()

	oldWorker := newWorker("thread-old", &fakeWorkerAgent{}, 1, 30, 1, 0, nil, "")
	newWorker := newWorker("thread-active", &fakeWorkerAgent{}, 1, 30, 1, 0, nil, "")
	newWorker.cancelMu.Lock()
	newWorker.currentJobActive = true
	newWorker.cancelMu.Unlock()

	m.mu.Lock()
	m.threadAgents["old"] = &threadAgentEntry{threadID: "old", parentChannelID: "parent", worker: oldWorker, agent: &acp.Agent{}, lastActivity: time.Now().Add(-time.Hour)}
	m.threadAgents["active"] = &threadAgentEntry{threadID: "active", parentChannelID: "parent", worker: newWorker, agent: &acp.Agent{}, lastActivity: time.Now()}
	m.tempAgents = 1
	m.capacitySnapshot = func() agentCapacitySnapshot {
		return agentCapacitySnapshot{CPUs: 1, MemoryKnown: true, MemoryAvailableBytes: 0}
	}
	err := m.ensureAgentCapacityLocked("thread", "new")
	_, oldExists := m.threadAgents["old"]
	_, activeExists := m.threadAgents["active"]
	m.mu.Unlock()

	var capErr *AgentCapacityError
	if !errors.As(err, &capErr) {
		t.Fatalf("ensureAgentCapacityLocked error = %v, want AgentCapacityError", err)
	}
	if capErr.Reclaimed != 1 {
		t.Fatalf("reclaimed = %d, want 1", capErr.Reclaimed)
	}
	if oldExists {
		t.Fatal("idle thread agent was not reclaimed")
	}
	if !activeExists {
		t.Fatal("active thread agent should not be reclaimed")
	}
	if !strings.Contains(err.Error(), "Reclaimed 1 idle agents") {
		t.Fatalf("localized error = %q", err.Error())
	}
}

func TestAgentCapacityDoesNotReclaimDequeuedWorker(t *testing.T) {
	L.Load("en")
	m := NewManager(ManagerConfig{DataDir: t.TempDir()})
	defer m.StopAll()

	dequeuedWorker := newWorker("thread-dequeued", &fakeWorkerAgent{}, 1, 30, 1, 0, nil, "")
	dequeuedWorker.markJobDequeued(true)

	m.mu.Lock()
	m.threadAgents["dequeued"] = &threadAgentEntry{threadID: "dequeued", parentChannelID: "parent", worker: dequeuedWorker, agent: &acp.Agent{}, lastActivity: time.Now().Add(-time.Hour)}
	m.tempAgents = 1
	m.capacitySnapshot = func() agentCapacitySnapshot {
		return agentCapacitySnapshot{CPUs: 1, MemoryKnown: true, MemoryAvailableBytes: 0}
	}
	err := m.ensureAgentCapacityLocked("thread", "new")
	_, dequeuedExists := m.threadAgents["dequeued"]
	m.mu.Unlock()

	var capErr *AgentCapacityError
	if !errors.As(err, &capErr) {
		t.Fatalf("ensureAgentCapacityLocked error = %v, want AgentCapacityError", err)
	}
	if capErr.Reclaimed != 0 {
		t.Fatalf("reclaimed = %d, want 0 for dequeued worker", capErr.Reclaimed)
	}
	if !dequeuedExists {
		t.Fatal("dequeued worker was reclaimed")
	}
}
