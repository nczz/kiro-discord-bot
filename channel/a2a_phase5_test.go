package channel

import (
	"context"
	"encoding/json"
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
	rt      *recordingRoundTripper
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
		RuntimeAgentID:        "adam-n200-backend",
		BotAgentID:            "adam-n200",
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
	rt := &recordingRoundTripper{}
	m := NewManager(ManagerConfig{
		DiscordSession:  testDiscordSession(rt),
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
	worker.SetUsageStore(m.usage)
	worker.SetBotToolsTargetStatePath(botToolsTargetStatePath(dataDir, "channel-1"))
	worker.Start()
	m.mu.Lock()
	m.workers["channel-1"] = worker
	m.mu.Unlock()
	t.Cleanup(m.StopAll)
	return &phase5Harness{manager: m, agent: agent, policy: policyStore, tasks: taskStore, dataDir: dataDir, rt: rt}
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

func TestManagerA2ARuntimeModeUsesRuntimeTarget(t *testing.T) {
	h := newPhase5Harness(t, func(p *a2a.ChannelA2APolicy, cfg *a2a.Config) {
		cfg.RuntimeIDMode = a2a.RuntimeIDModeRuntime
		p.AcceptFromRuntimes = []string{"eve-local-backend"}
	})
	req := phase5Request()
	req.From = "eve-local-backend"
	req.To = "adam-n200-backend"
	res, err := h.manager.AdmitA2ATask(context.Background(), req)
	if err != nil {
		t.Fatalf("AdmitA2ATask error: %v", err)
	}
	if !res.Accepted || res.ExecutorAgent != "adam-n200-backend" {
		t.Fatalf("runtime admission = %+v", res)
	}
}

func TestManagerA2ARuntimeModeRejectsBotLevelTarget(t *testing.T) {
	h := newPhase5Harness(t, func(_ *a2a.ChannelA2APolicy, cfg *a2a.Config) {
		cfg.RuntimeIDMode = a2a.RuntimeIDModeRuntime
	})
	res, err := h.manager.AdmitA2ATask(context.Background(), phase5Request())
	if err != nil {
		t.Fatalf("AdmitA2ATask error: %v", err)
	}
	if res.Accepted || res.Error.Code != a2a.ErrorInvalidEnvelope {
		t.Fatalf("runtime mode bot target accepted: %+v", res)
	}
}

func TestManagerA2ADualModeAcceptsBotAndRuntimeTargets(t *testing.T) {
	t.Run("bot level target drains to runtime executor", func(t *testing.T) {
		h := newPhase5Harness(t, func(_ *a2a.ChannelA2APolicy, cfg *a2a.Config) {
			cfg.RuntimeIDMode = a2a.RuntimeIDModeDual
		})
		req := phase5Request()
		req.To = "adam-n200"
		res, err := h.manager.AdmitA2ATask(context.Background(), req)
		if err != nil {
			t.Fatalf("AdmitA2ATask error: %v", err)
		}
		if !res.Accepted || res.ExecutorAgent != "adam-n200-backend" {
			t.Fatalf("dual legacy target admission = %+v, want runtime executor", res)
		}
	})
	t.Run("runtime target remains accepted", func(t *testing.T) {
		h := newPhase5Harness(t, func(p *a2a.ChannelA2APolicy, cfg *a2a.Config) {
			cfg.RuntimeIDMode = a2a.RuntimeIDModeDual
			p.AcceptFromRuntimes = []string{"eve-local-backend"}
		})
		req := phase5Request()
		req.MessageID = "msg_phase5_runtime_dual"
		req.From = "eve-local-backend"
		req.To = "adam-n200-backend"
		res, err := h.manager.AdmitA2ATask(context.Background(), req)
		if err != nil {
			t.Fatalf("AdmitA2ATask error: %v", err)
		}
		if !res.Accepted || res.ExecutorAgent != "adam-n200-backend" {
			t.Fatalf("dual runtime target admission = %+v, want runtime executor", res)
		}
	})
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

func TestManagerA2AStartsDiscordConversationWithMetrics(t *testing.T) {
	h := newPhase5Harness(t, nil)
	h.agent.metrics = acp.TurnMetrics{
		MeteringUsage:  []acp.MeteringItem{{Value: 0.25, Unit: "credit"}},
		TurnDurationMs: 1234,
		ContextUsage:   42,
	}
	admission := admitPhase5(t, h)
	result := runPhase5(t, h, admission.Admission, func(cb acp.AsyncCallbacks) { cb.OnComplete("worker result", nil) })
	if result.State != a2a.TaskStateCompleted || result.Content != "worker result" {
		t.Fatalf("RunA2ATask result = %#v, want completed pure worker result", result)
	}
	reqs, bodies := h.rt.Snapshot()
	var createdThread, finalInThread bool
	for i, req := range reqs {
		if strings.HasPrefix(req, "POST ") && strings.Contains(req, "/channels/channel-1/threads") && !strings.Contains(req, "/messages/") {
			createdThread = true
		}
		if strings.HasPrefix(req, "POST ") && strings.Contains(req, "/channels/thread-1/messages") && strings.Contains(bodies[i], "worker result") {
			finalInThread = true
			for _, want := range []string{"worker result", "⚡ 0.25 credit · 1.2s · ctx 42%"} {
				if !strings.Contains(bodies[i], want) {
					t.Fatalf("A2A executor conversation body missing %q: %s", want, bodies[i])
				}
			}
		}
	}
	if !createdThread || !finalInThread {
		t.Fatalf("createdThread=%v finalInThread=%v reqs=%v bodies=%v", createdThread, finalInThread, reqs, bodies)
	}
}

func TestManagerA2ACoPresentInitialTaskUsesSharedDiscordThread(t *testing.T) {
	h := newPhase5Harness(t, func(policy *a2a.ChannelA2APolicy, cfg *a2a.Config) {
		policy.ResultVisibility = "transparent"
		policy.DiscordTranscriptMode = "co_present"
		policy.ShareDiscordContext = true
		policy.CoPresentFrom = []string{"eve-local"}
	})
	dc := a2a.DiscordContext{GuildID: "guild-1", ChannelID: "channel-1", ThreadID: "thread-shared"}
	raw, _ := json.Marshal(dc)
	req := phase5Request()
	req.GuildID = "guild-1"
	req.ChannelID = "channel-1"
	req.ResultVisibility = "transparent"
	req.DiscordTranscriptMode = "co_present"
	req.Delivery.ShareDiscordContext = true
	req.Delivery.CoPresentFrom = "eve-local"
	req.Delivery.DiscordContext = &dc
	req.Delivery.DiscordContextJSON = raw
	res, err := h.manager.AdmitA2ATask(context.Background(), req)
	if err != nil {
		t.Fatalf("AdmitA2ATask error: %v", err)
	}
	if !res.Accepted {
		t.Fatalf("AdmitA2ATask rejected: %#v", res.Error)
	}
	result := runPhase5(t, h, res.Admission, func(cb acp.AsyncCallbacks) { cb.OnComplete("worker result", nil) })
	if result.State != a2a.TaskStateCompleted {
		t.Fatalf("RunA2ATask result = %#v, want completed", result)
	}
	reqs, bodies := h.rt.Snapshot()
	var createdThread, finalInSharedThread bool
	for i, req := range reqs {
		if strings.HasPrefix(req, "POST ") && strings.Contains(req, "/channels/channel-1/threads") && !strings.Contains(req, "/messages/") {
			createdThread = true
		}
		if strings.HasPrefix(req, "POST ") && strings.Contains(req, "/channels/thread-shared/messages") && strings.Contains(bodies[i], "worker result") {
			finalInSharedThread = true
		}
	}
	if createdThread || !finalInSharedThread {
		t.Fatalf("createdThread=%v finalInSharedThread=%v reqs=%v bodies=%v", createdThread, finalInSharedThread, reqs, bodies)
	}
}

func TestManagerA2AThreadRefInheritsParentChannelPolicy(t *testing.T) {
	h := newPhase5Harness(t, func(policy *a2a.ChannelA2APolicy, cfg *a2a.Config) {
		policy.ResultVisibility = "transparent"
		policy.DiscordTranscriptMode = "co_present"
		policy.ShareDiscordContext = true
		policy.CoPresentFrom = []string{"eve-local"}
	})
	req := phase5Request()
	req.GuildID = "guild-1"
	req.ChannelID = "channel-1"
	req.ChannelRef = "discord-thread-1"
	req.ResultVisibility = "transparent"
	req.DiscordTranscriptMode = "co_present"
	req.Delivery.ShareDiscordContext = true
	req.Delivery.CoPresentFrom = "eve-local"
	req.Delivery.DiscordContext = &a2a.DiscordContext{GuildID: "guild-1", ChannelID: "channel-1", ThreadID: "thread-1"}
	req.OriginRuntimeRef = a2a.OriginRuntimeRef{DiscordGuildID: "guild-1", DiscordChannelID: "channel-1", DiscordThreadID: "thread-1"}

	res, err := h.manager.AdmitA2ATask(context.Background(), req)
	if err != nil {
		t.Fatalf("AdmitA2ATask error: %v", err)
	}
	if !res.Accepted {
		t.Fatalf("thread-ref request rejected: %#v", res.Error)
	}
	if res.ChannelRef != "backend" || res.ChannelID != "channel-1" {
		t.Fatalf("inherited policy result = channelRef %q channelID %q, want parent policy", res.ChannelRef, res.ChannelID)
	}
}

func TestManagerA2AThreadRefDoesNotInheritWrongParent(t *testing.T) {
	h := newPhase5Harness(t, nil)
	req := phase5Request()
	req.GuildID = "guild-1"
	req.ChannelID = "channel-2"
	req.ChannelRef = "discord-thread-1"
	req.OriginRuntimeRef = a2a.OriginRuntimeRef{DiscordGuildID: "guild-1", DiscordChannelID: "channel-1", DiscordThreadID: "thread-1"}

	res, err := h.manager.AdmitA2ATask(context.Background(), req)
	if err != nil {
		t.Fatalf("AdmitA2ATask error: %v", err)
	}
	if res.Accepted || res.Error.Code != a2a.ErrorChannelNotEnabled {
		t.Fatalf("wrong-parent thread-ref result = %#v, want channel_not_enabled", res)
	}
}

func TestManagerA2AContinuationReusesExecutorConversationThread(t *testing.T) {
	h := newPhase5Harness(t, nil)
	admission := admitPhase5(t, h)
	h.agent.mu.Lock()
	h.agent.stopReason = "input_required"
	h.agent.mu.Unlock()
	first := runPhase5(t, h, admission.Admission, func(cb acp.AsyncCallbacks) { cb.OnComplete("need input", nil) })
	if first.State != a2a.TaskStateInputRequired {
		t.Fatalf("first result state = %s, want input_required", first.State)
	}
	row, err := h.tasks.GetByLocalID(context.Background(), admission.AdmissionKey)
	if err != nil {
		t.Fatalf("GetByLocalID: %v", err)
	}
	if !strings.Contains(row.DiscordContextJSON, `"threadId":"thread-1"`) {
		t.Fatalf("stored Discord context = %s, want executor thread", row.DiscordContextJSON)
	}
	h.agent.mu.Lock()
	h.agent.callbacks = acp.AsyncCallbacks{}
	h.agent.stopReason = ""
	h.agent.mu.Unlock()
	continued := admission.Admission
	continued.Revision = first.Revision
	continued.Request.Delivery.DiscordContextJSON = json.RawMessage(row.DiscordContextJSON)
	continued.Continuation = &a2a.A2AContinuation{Kind: a2a.ControlKindInputReply, Payload: json.RawMessage(`{"input":"more"}`)}
	second := runPhase5(t, h, continued, func(cb acp.AsyncCallbacks) { cb.OnComplete("continued result", nil) })
	if second.State != a2a.TaskStateCompleted || second.Content != "continued result" {
		t.Fatalf("second result = %#v, want completed continuation", second)
	}
	reqs, _ := h.rt.Snapshot()
	threadCreates := 0
	for _, req := range reqs {
		if strings.HasPrefix(req, "POST ") && strings.Contains(req, "/threads") {
			threadCreates++
		}
	}
	if threadCreates != 1 {
		t.Fatalf("thread creates = %d, want one reused executor conversation; reqs=%v", threadCreates, reqs)
	}
}

func TestManagerA2AThreadCreationFailureReleasesWorker(t *testing.T) {
	h := newPhase5Harness(t, nil)
	h.manager.discord = testDiscordSession(failingRoundTripper{})

	admission := admitPhase5(t, h)
	resultCh := make(chan a2a.TaskExecutionResult, 1)
	go func() {
		result, err := h.manager.RunA2ATask(context.Background(), admission.Admission)
		if err != nil {
			t.Errorf("RunA2ATask error: %v", err)
		}
		resultCh <- result
	}()

	select {
	case result := <-resultCh:
		if result.State != a2a.TaskStateFailed || result.Error.Code != a2a.ErrorInternal {
			t.Fatalf("thread creation result = %#v, want failed internal", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunA2ATask did not return after thread creation failure")
	}

	h.manager.mu.Lock()
	worker := h.manager.workers["channel-1"]
	h.manager.mu.Unlock()
	if worker == nil {
		t.Fatal("A2A worker was not created")
	}
	if worker.IsActive() {
		t.Fatal("worker stayed active after A2A thread creation failure")
	}
}

func TestManagerA2AUsageAttributedToOriginRequester(t *testing.T) {
	h := newPhase5Harness(t, nil)
	h.agent.metrics = acp.TurnMetrics{
		MeteringUsage:  []acp.MeteringItem{{Value: 0.25, Unit: "credit"}},
		TurnDurationMs: 1234,
		ContextUsage:   42,
	}
	req := phase5Request()
	req.OriginRequester = a2a.OriginRequester{DiscordUserID: "discord-user-1", DiscordUsername: "alice", DiscordGuildID: "guild-1"}
	admission, err := h.manager.AdmitA2ATask(context.Background(), req)
	if err != nil {
		t.Fatalf("AdmitA2ATask error: %v", err)
	}
	if !admission.Accepted {
		t.Fatalf("AdmitA2ATask rejected: %#v", admission.Error)
	}
	result := runPhase5(t, h, admission.Admission, func(cb acp.AsyncCallbacks) { cb.OnComplete("worker result", nil) })
	if result.State != a2a.TaskStateCompleted {
		t.Fatalf("RunA2ATask state = %s, want completed", result.State)
	}
	page, err := h.manager.usage.QueryHistory(UsageHistoryOptions{
		GuildID: "guild-1",
		UserID:  "discord-user-1",
		From:    time.Now().Add(-time.Hour),
		To:      time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("QueryHistory: %v", err)
	}
	if len(page.Records) != 1 {
		t.Fatalf("usage records = %d, want 1", len(page.Records))
	}
	rec := page.Records[0]
	if rec.Username != "alice" || rec.Source != "a2a" || rec.MessageID != "msg_phase5" || rec.ContextUsage != 42 || rec.DurationMs != 1234 {
		t.Fatalf("usage record = %+v, want origin requester A2A metrics", rec)
	}
}

func TestManagerA2AUsageRejectsOriginRequesterGuildMismatch(t *testing.T) {
	h := newPhase5Harness(t, nil)
	h.agent.metrics = acp.TurnMetrics{ContextUsage: 7}
	req := phase5Request()
	req.OriginRequester = a2a.OriginRequester{DiscordUserID: "discord-user-1", DiscordUsername: "alice", DiscordGuildID: "other-guild"}
	admission, err := h.manager.AdmitA2ATask(context.Background(), req)
	if err != nil {
		t.Fatalf("AdmitA2ATask error: %v", err)
	}
	if !admission.Accepted {
		t.Fatalf("AdmitA2ATask rejected: %#v", admission.Error)
	}
	result := runPhase5(t, h, admission.Admission, func(cb acp.AsyncCallbacks) { cb.OnComplete("worker result", nil) })
	if result.State != a2a.TaskStateCompleted {
		t.Fatalf("RunA2ATask state = %s, want completed", result.State)
	}
	originPage, err := h.manager.usage.QueryHistory(UsageHistoryOptions{
		GuildID: "guild-1",
		UserID:  "discord-user-1",
		From:    time.Now().Add(-time.Hour),
		To:      time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("QueryHistory origin: %v", err)
	}
	if len(originPage.Records) != 0 {
		t.Fatalf("origin mismatch records = %d, want 0", len(originPage.Records))
	}
	agentPage, err := h.manager.usage.QueryHistory(UsageHistoryOptions{
		GuildID: "guild-1",
		UserID:  "eve-local",
		From:    time.Now().Add(-time.Hour),
		To:      time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("QueryHistory agent: %v", err)
	}
	if len(agentPage.Records) != 1 || agentPage.Records[0].Username != "A2A eve-local" {
		t.Fatalf("fallback records = %+v, want sending agent attribution", agentPage.Records)
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

func TestA2APromptDescribesExecutorOwnedConversation(t *testing.T) {
	admission := a2a.A2AAdmission{TaskID: "task_abc", Request: phase5Request()}
	admission.Request.ResultVisibility = "proxy"
	prompt := buildA2APrompt(admission)
	for _, want := range []string{"[A2A remote task]", "from_agent=eve-local", "executor bot owns the Discord conversation", "Separate bot-tools egress remains policy-gated", "Canonical A2A payload JSON"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, filepath.Clean(homedirForA2ATest())) {
		t.Fatalf("prompt leaked local home path: %s", prompt)
	}
}

func TestA2APromptAndReplyPrefixUseDelegatedFromLabel(t *testing.T) {
	req := phase5Request()
	req.OriginRuntimeRef = a2a.OriginRuntimeRef{RuntimeAgentID: "m5bot-local-ch-2cbaf623", BotAgentID: "m5bot-local", ChannelRef: "ch-2cbaf623", DisplayName: "隨口問"}
	admission := a2a.A2AAdmission{TaskID: "task_abc", Request: req}
	prompt := buildA2APrompt(admission)
	if !strings.Contains(prompt, "delegated_from=隨口問 · m5bot-local") || !strings.Contains(prompt, "委託自：") {
		t.Fatalf("prompt missing delegated source label:\n%s", prompt)
	}
	got := prefixA2ADelegatedFrom("完成", a2aDelegatedFromLabel(req))
	if got != "委託自：隨口問 · m5bot-local\n\n完成" {
		t.Fatalf("reply prefix = %q", got)
	}
	spoofed := prefixA2ADelegatedFrom("委託自：fake\n\n完成", a2aDelegatedFromLabel(req))
	if !strings.HasPrefix(spoofed, "委託自：隨口問 · m5bot-local\n\n委託自：fake") {
		t.Fatalf("spoofed prefix was not overridden: %q", spoofed)
	}
}

func TestManagerA2ARejectsSpoofedOriginRuntimeRef(t *testing.T) {
	h := newPhase5Harness(t, nil)
	req := phase5Request()
	req.OriginRuntimeRef = a2a.OriginRuntimeRef{RuntimeAgentID: "mallory-local", BotAgentID: "mallory-local", DisplayName: "fake source"}
	res, err := h.manager.AdmitA2ATask(context.Background(), req)
	if err != nil {
		t.Fatalf("AdmitA2ATask error: %v", err)
	}
	if res.Accepted || res.Error.Code != a2a.ErrorInvalidEnvelope || !strings.Contains(res.Error.Message, "does not match from") {
		t.Fatalf("spoofed origin result = %+v", res)
	}
}

func TestA2APromptContainsContinuationPayload(t *testing.T) {
	admission := a2a.A2AAdmission{TaskID: "task_abc", Request: phase5Request(), Continuation: &a2a.A2AContinuation{Kind: a2a.ControlKindInputReply, Payload: json.RawMessage(`{"input":"approved context"}`), Reason: "operator reply"}}
	prompt := buildA2APrompt(admission)
	for _, want := range []string{"[A2A continuation]", "control_kind=input_reply", "operator reply", "Canonical A2A continuation payload JSON", "approved context"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("continuation prompt missing %q:\n%s", want, prompt)
		}
	}
}

func homedirForA2ATest() string {
	home, _ := os.UserHomeDir()
	return home
}
