package channel

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nczz/kiro-discord-bot/a2a"
	"github.com/nczz/kiro-discord-bot/acp"
	L "github.com/nczz/kiro-discord-bot/locale"
)

type phase5Harness struct {
	manager *Manager
	agent   *fakeWorkerAgent
	policy  *a2a.SQLitePolicyStore
	tasks   *a2a.SQLiteTaskStore
	dataDir string
}

func newPhase5Harness(t *testing.T, mutate func(*a2a.ChannelA2APolicy, *a2a.Config)) *phase5Harness {
	t.Helper()
	L.Load("en")
	dataDir := t.TempDir()
	cfg := a2a.Config{NATSURL: "nats://a2a.example.internal:4222", AgentID: "adam-n200", MaxInboundTasksPerChannel: 0}
	policy := a2a.ChannelA2APolicy{
		GuildID:               "guild-1",
		ChannelID:             "channel-1",
		Enabled:               true,
		ChannelRef:            "backend",
		AcceptFrom:            []string{"eve-local"},
		AcceptSkills:          []string{"review"},
		MaxConcurrent:         0,
		ResultVisibility:      "proxy",
		DiscordTranscriptMode: "delegator",
	}
	if mutate != nil {
		mutate(&policy, &cfg)
	}
	policyStore, err := a2a.OpenPolicyStore(dataDir, cfg.AgentID)
	if err != nil {
		t.Fatalf("OpenPolicyStore: %v", err)
	}
	if policy.GuildID != "" {
		if err := policyStore.Save(context.Background(), policy, "test-user"); err != nil {
			t.Fatalf("Save policy: %v", err)
		}
	}
	taskStore, err := a2a.OpenTaskStore(dataDir)
	if err != nil {
		t.Fatalf("OpenTaskStore: %v", err)
	}
	m := NewManager(ManagerConfig{
		DataDir:         dataDir,
		GuildID:         "guild-1",
		QueueBufferSize: 2,
		AskTimeoutSec:   1,
		A2A:             cfg,
		A2APolicyStore:  policyStore,
		A2ATaskStore:    taskStore,
	})
	agent := &fakeWorkerAgent{}
	worker := newWorker("channel-1", agent, 2, 1, 0, 60, nil, "")
	worker.SetBotToolsTargetStatePath(botToolsTargetStatePath(dataDir, "channel-1"))
	worker.Start()
	m.mu.Lock()
	m.workers["channel-1"] = worker
	m.mu.Unlock()
	t.Cleanup(m.StopAll)
	return &phase5Harness{manager: m, agent: agent, policy: policyStore, tasks: taskStore, dataDir: dataDir}
}

func phase5Request() a2a.TaskExecutionRequest {
	return a2a.TaskExecutionRequest{
		MessageID:          "msg_phase5",
		ClientTaskRef:      "client-1",
		ContextID:          "ctx-1",
		From:               "eve-local",
		To:                 "adam-n200",
		ChannelRef:         "backend",
		SkillID:            "backend/review",
		UserVisibleSummary: "Review this change.",
		Payload:            []byte(`{"message":{"parts":[{"kind":"text","text":"review"}]}}`),
	}
}

func admitPhase5(t *testing.T, h *phase5Harness) a2a.A2AAdmissionResult {
	t.Helper()
	res, err := h.manager.AdmitA2ATask(context.Background(), phase5Request())
	if err != nil {
		t.Fatalf("AdmitA2ATask error: %v", err)
	}
	if !res.Accepted {
		t.Fatalf("AdmitA2ATask rejected: %#v", res.Error)
	}
	return res
}

func runPhase5(t *testing.T, h *phase5Harness, admission a2a.A2AAdmission, complete func(acp.AsyncCallbacks)) a2a.TaskExecutionResult {
	t.Helper()
	resultCh := make(chan a2a.TaskExecutionResult, 1)
	go func() {
		result, err := h.manager.RunA2ATask(context.Background(), admission)
		if err != nil {
			t.Errorf("RunA2ATask error: %v", err)
		}
		resultCh <- result
	}()
	cb := waitPhase5Callbacks(t, h.agent)
	complete(cb)
	select {
	case result := <-resultCh:
		return result
	case <-time.After(2 * time.Second):
		t.Fatal("RunA2ATask did not return")
		return a2a.TaskExecutionResult{}
	}
}

func waitPhase5Callbacks(t *testing.T, agent *fakeWorkerAgent) acp.AsyncCallbacks {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		cb := agent.Callbacks()
		if cb.OnComplete != nil {
			return cb
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("worker did not start AskAsyncMulti")
	return acp.AsyncCallbacks{}
}

func TestManagerA2AIngressDisabled(t *testing.T) {
	h := newPhase5Harness(t, func(_ *a2a.ChannelA2APolicy, cfg *a2a.Config) {
		cfg.NATSURL = ""
	})
	res, err := h.manager.AdmitA2ATask(context.Background(), phase5Request())
	if err != nil {
		t.Fatalf("AdmitA2ATask error: %v", err)
	}
	if res.Accepted || res.Error.Code != a2a.ErrorChannelNotEnabled {
		t.Fatalf("disabled admission = %#v, want channel_not_enabled rejection", res)
	}
}

func TestManagerA2APolicyDenied(t *testing.T) {
	h := newPhase5Harness(t, func(p *a2a.ChannelA2APolicy, _ *a2a.Config) {
		p.AcceptFrom = []string{"mallory"}
	})
	res, err := h.manager.AdmitA2ATask(context.Background(), phase5Request())
	if err != nil {
		t.Fatalf("AdmitA2ATask error: %v", err)
	}
	if res.Accepted || res.Error.Code != a2a.ErrorSenderNotAllowed {
		t.Fatalf("policy denial = %#v, want sender_not_allowed", res)
	}
}

func TestManagerA2AAcceptsOnce(t *testing.T) {
	h := newPhase5Harness(t, nil)
	first := admitPhase5(t, h)
	second := admitPhase5(t, h)
	if first.AdmissionKey != second.AdmissionKey || first.TaskID != second.TaskID {
		t.Fatalf("duplicate admission changed identity: first=%#v second=%#v", first, second)
	}
	if got := h.manager.a2aInboundOpen["channel-1"]; got != 1 {
		t.Fatalf("duplicate admission reserved %d slots, want 1", got)
	}
}

func TestManagerA2AInboundQuota(t *testing.T) {
	h := newPhase5Harness(t, func(p *a2a.ChannelA2APolicy, _ *a2a.Config) { p.MaxConcurrent = 1 })
	admitPhase5(t, h)
	req := phase5Request()
	req.MessageID = "msg_phase5_second"
	res, err := h.manager.AdmitA2ATask(context.Background(), req)
	if err != nil {
		t.Fatalf("AdmitA2ATask error: %v", err)
	}
	if res.Accepted || res.Error.Code != a2a.ErrorOverloaded {
		t.Fatalf("quota result = %#v, want overloaded rejection", res)
	}
}

func TestManagerA2AAdmissionBeforeExecution(t *testing.T) {
	h := newPhase5Harness(t, nil)
	admitPhase5(t, h)
	if cb := h.agent.Callbacks(); cb.OnComplete != nil {
		t.Fatal("admission started worker before RunA2ATask")
	}
}

func TestManagerA2AAckAfterAdmissionNotCompletion(t *testing.T) {
	h := newPhase5Harness(t, nil)
	admission := admitPhase5(t, h)
	resultCh := make(chan a2a.TaskExecutionResult, 1)
	go func() {
		result, _ := h.manager.RunA2ATask(context.Background(), admission.Admission)
		resultCh <- result
	}()
	waitPhase5Callbacks(t, h.agent)
	select {
	case result := <-resultCh:
		t.Fatalf("RunA2ATask completed before worker callback: %#v", result)
	case <-time.After(50 * time.Millisecond):
	}
	h.agent.Callbacks().OnComplete("done", nil)
	<-resultCh
}

func TestManagerA2AUsesWorker(t *testing.T) {
	h := newPhase5Harness(t, nil)
	admission := admitPhase5(t, h)
	result := runPhase5(t, h, admission.Admission, func(cb acp.AsyncCallbacks) { cb.OnComplete("worker result", nil) })
	if result.State != a2a.TaskStateCompleted || !strings.Contains(result.Content, "worker result") {
		t.Fatalf("RunA2ATask result = %#v, want completed worker result", result)
	}
}

func TestManagerA2AProxyDisablesEgress(t *testing.T) {
	h := newPhase5Harness(t, nil)
	admission := admitPhase5(t, h)
	resultCh := make(chan a2a.TaskExecutionResult, 1)
	go func() {
		result, _ := h.manager.RunA2ATask(context.Background(), admission.Admission)
		resultCh <- result
	}()
	waitPhase5Callbacks(t, h.agent)
	statePath := botToolsTargetStatePath(h.dataDir, "channel-1")
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read target state: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, `"disable_egress":true`) || !strings.Contains(text, `"remote_a2a":true`) {
		t.Fatalf("target state %s missing remote proxy egress flags", text)
	}
	h.agent.Callbacks().OnComplete("done", nil)
	<-resultCh
}

func TestManagerA2ARemoteMemoryWriteDenied(t *testing.T) {
	h := newPhase5Harness(t, nil)
	admission := admitPhase5(t, h)
	resultCh := make(chan a2a.TaskExecutionResult, 1)
	go func() {
		result, _ := h.manager.RunA2ATask(context.Background(), admission.Admission)
		resultCh <- result
	}()
	waitPhase5Callbacks(t, h.agent)
	raw, err := os.ReadFile(botToolsTargetStatePath(h.dataDir, "channel-1"))
	if err != nil {
		t.Fatalf("read target state: %v", err)
	}
	if strings.Contains(string(raw), `"allow_memory_write":true`) {
		t.Fatalf("remote memory write unexpectedly allowed: %s", raw)
	}
	h.agent.Callbacks().OnComplete("done", nil)
	<-resultCh
}

func TestManagerA2ARemoteMemoryWriteAllowedByPolicy(t *testing.T) {
	h := newPhase5Harness(t, func(p *a2a.ChannelA2APolicy, _ *a2a.Config) {
		p.RemoteToolPolicy.AllowMemoryWrite = true
	})
	admission := admitPhase5(t, h)
	resultCh := make(chan a2a.TaskExecutionResult, 1)
	go func() {
		result, _ := h.manager.RunA2ATask(context.Background(), admission.Admission)
		resultCh <- result
	}()
	waitPhase5Callbacks(t, h.agent)
	raw, err := os.ReadFile(botToolsTargetStatePath(h.dataDir, "channel-1"))
	if err != nil {
		t.Fatalf("read target state: %v", err)
	}
	if !strings.Contains(string(raw), `"allow_memory_write":true`) {
		t.Fatalf("remote memory write not allowed by target state: %s", raw)
	}
	h.agent.Callbacks().OnComplete("done", nil)
	<-resultCh
}

func TestManagerA2ATimeout(t *testing.T) {
	h := newPhase5Harness(t, nil)
	admission := admitPhase5(t, h)
	result := runPhase5(t, h, admission.Admission, func(cb acp.AsyncCallbacks) {
		time.Sleep(1100 * time.Millisecond)
		cb.OnComplete("", errors.New("agent timeout"))
	})
	if result.State != a2a.TaskStateFailed || result.Error.Code != a2a.ErrorTimeout {
		t.Fatalf("timeout result = %#v, want failed timeout", result)
	}
}

func TestManagerA2ACancel(t *testing.T) {
	h := newPhase5Harness(t, nil)
	admission := admitPhase5(t, h)
	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan a2a.TaskExecutionResult, 1)
	go func() {
		result, _ := h.manager.RunA2ATask(ctx, admission.Admission)
		resultCh <- result
	}()
	waitPhase5Callbacks(t, h.agent)
	cancel()
	select {
	case result := <-resultCh:
		if result.State != a2a.TaskStateCanceled {
			t.Fatalf("cancel result = %#v, want canceled", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not finish")
	}
}

func TestManagerA2AInputRequired(t *testing.T) {
	h := newPhase5Harness(t, nil)
	h.agent.stopReason = "input_required"
	admission := admitPhase5(t, h)
	result := runPhase5(t, h, admission.Admission, func(cb acp.AsyncCallbacks) { cb.OnComplete("need input", nil) })
	if result.State != a2a.TaskStateInputRequired {
		t.Fatalf("input-required result = %#v", result)
	}
}

func TestManagerA2AAuthRequired(t *testing.T) {
	h := newPhase5Harness(t, nil)
	h.agent.stopReason = "auth_required"
	admission := admitPhase5(t, h)
	result := runPhase5(t, h, admission.Admission, func(cb acp.AsyncCallbacks) { cb.OnComplete("need auth", nil) })
	if result.State != a2a.TaskStateAuthRequired {
		t.Fatalf("auth-required result = %#v", result)
	}
}

func TestManagerA2AResultCapture(t *testing.T) {
	h := newPhase5Harness(t, nil)
	admission := admitPhase5(t, h)
	result := runPhase5(t, h, admission.Admission, func(cb acp.AsyncCallbacks) { cb.OnComplete("captured final text", nil) })
	if result.Content != "captured final text" {
		t.Fatalf("captured content = %q", result.Content)
	}
	row, err := h.tasks.GetByLocalID(context.Background(), admission.AdmissionKey)
	if err != nil {
		t.Fatalf("GetByLocalID: %v", err)
	}
	if row.State != a2a.TaskStateCompleted || !row.Terminal {
		t.Fatalf("task row = %#v, want completed terminal", row)
	}
}

func TestWorkerA2AInlineResultSuppressesDiscordReply(t *testing.T) {
	resultCh := make(chan a2a.TaskExecutionResult, 1)
	job := &Job{A2AResult: resultCh, FinalReply: func(string) { t.Fatal("FinalReply should not run for A2A result jobs") }}
	if err := job.sendInlineFinalReply(nil, "hello"); err != nil {
		t.Fatalf("sendInlineFinalReply: %v", err)
	}
	select {
	case <-resultCh:
		t.Fatal("sendInlineFinalReply should not emit structured result")
	default:
	}
}

func TestA2APromptContainsPayloadWithoutDiscordEgress(t *testing.T) {
	admission := a2a.A2AAdmission{TaskID: "task_abc", Request: phase5Request()}
	admission.Request.ResultVisibility = "proxy"
	prompt := buildA2APrompt(admission)
	for _, want := range []string{"[A2A remote task]", "from_agent=eve-local", "Discord egress is disabled", "Canonical A2A payload JSON"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, filepath.Clean(homedirForA2ATest())) {
		t.Fatalf("prompt leaked local home path: %s", prompt)
	}
}

func homedirForA2ATest() string {
	home, _ := os.UserHomeDir()
	return home
}
