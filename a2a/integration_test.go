package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

type integrationExecutor struct {
	tasks   *SQLiteTaskStore
	agentID AgentID

	mu       sync.Mutex
	runs     int
	admitted chan A2AAdmission
	started  chan A2AAdmission
	block    chan struct{}
	result   TaskExecutionResult
}

func newIntegrationExecutor(tasks *SQLiteTaskStore, agentID AgentID) *integrationExecutor {
	return &integrationExecutor{tasks: tasks, agentID: agentID, admitted: make(chan A2AAdmission, 10), started: make(chan A2AAdmission, 10), block: make(chan struct{})}
}

func (e *integrationExecutor) AdmitA2ATask(ctx context.Context, req TaskExecutionRequest) (A2AAdmissionResult, error) {
	taskID := TaskID("task_" + strings.TrimPrefix(string(req.MessageID), "msg_"))
	row, err := e.tasks.AdmitInbound(ctx, TaskRow{TaskID: taskID, ClientTaskRef: req.ClientTaskRef, MessageID: req.MessageID, ContextID: req.ContextID, FromAgent: req.From, ToAgent: req.To, ExecutorAgent: e.agentID, ChannelRef: req.ChannelRef, SkillID: SkillSlug(req.SkillID), State: TaskStateSubmitted, Revision: 1, ResultVisibility: firstNonEmpty(req.ResultVisibility, "proxy"), DiscordTranscriptMode: firstNonEmpty(req.DiscordTranscriptMode, "delegator")})
	if err != nil {
		return A2AAdmissionResult{}, err
	}
	admission := A2AAdmission{AdmissionKey: row.LocalID, TaskID: row.TaskID, State: row.State, Revision: row.Revision, Request: req}
	select {
	case e.admitted <- admission:
	default:
	}
	return A2AAdmissionResult{Accepted: true, AdmissionKey: row.LocalID, TaskID: row.TaskID, State: row.State, Revision: row.Revision, ExecutorAgent: e.agentID, ChannelRef: row.ChannelRef, SkillID: row.SkillID, Admission: admission}, nil
}

func (e *integrationExecutor) RunA2ATask(ctx context.Context, admission A2AAdmission) (TaskExecutionResult, error) {
	e.mu.Lock()
	e.runs++
	e.mu.Unlock()
	select {
	case e.started <- admission:
	default:
	}
	select {
	case <-e.block:
	case <-ctx.Done():
		return TaskExecutionResult{TaskID: admission.TaskID, State: TaskStateCanceled, Revision: admission.Revision + 1, Error: TaskError{Code: ErrorCode("canceled"), Message: ctx.Err().Error()}}, nil
	}
	if e.result.State != "" {
		result := e.result
		if result.TaskID == "" {
			result.TaskID = admission.TaskID
		}
		if result.Revision == 0 {
			result.Revision = admission.Revision + 1
		}
		return e.recordResult(ctx, admission, result), nil
	}
	return e.recordResult(ctx, admission, TaskExecutionResult{TaskID: admission.TaskID, State: TaskStateCompleted, Revision: admission.Revision + 1, Content: "remote result"}), nil
}

func (e *integrationExecutor) recordResult(ctx context.Context, admission A2AAdmission, result TaskExecutionResult) TaskExecutionResult {
	if e.tasks == nil {
		return result
	}
	if IsTerminalState(result.State) {
		if rec, err := e.tasks.MarkTerminal(ctx, admission.AdmissionKey, result.State, result.Error); err == nil {
			result.Revision = rec.Revision
		}
		return result
	}
	payload, _ := json.Marshal(result)
	if row, err := e.tasks.ApplyTaskEvent(ctx, "inbound", admission.TaskID, EventRow{TaskID: admission.TaskID, Revision: result.Revision, EventType: EventKindStatus, State: result.State, PayloadJSON: string(payload)}, result.State, result.Error); err == nil {
		result.Revision = row.Revision
	}
	return result
}

func (e *integrationExecutor) RunCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.runs
}

type integrationPair struct {
	aliceNode *Node
	bobNode   *Node
	alice     *Transport
	bob       *Transport
	aliceDB   *SQLiteTaskStore
	bobDB     *SQLiteTaskStore
	bobExec   *integrationExecutor
}

func newIntegrationPair(t *testing.T, maxEventRate int) *integrationPair {
	t.Helper()
	srv := runEmbeddedNATS(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	aliceNode, err := Connect(ctx, NodeConfig{Config: Config{NATSURL: srv.ClientURL(), AgentID: "eve-local"}})
	if err != nil {
		t.Fatalf("connect alice: %v", err)
	}
	bobNode, err := Connect(ctx, NodeConfig{Config: Config{NATSURL: srv.ClientURL(), AgentID: "adam-n200"}})
	if err != nil {
		t.Fatalf("connect bob: %v", err)
	}
	aliceDB, err := OpenTaskStore(t.TempDir())
	if err != nil {
		t.Fatalf("alice task store: %v", err)
	}
	bobDB, err := OpenTaskStore(t.TempDir())
	if err != nil {
		t.Fatalf("bob task store: %v", err)
	}
	aliceExec := newIntegrationExecutor(aliceDB, "eve-local")
	bobExec := newIntegrationExecutor(bobDB, "adam-n200")
	alice, err := StartTransport(ctx, TransportConfig{Node: aliceNode, Tasks: aliceDB, Executor: aliceExec, Config: Config{NATSURL: srv.ClientURL(), AgentID: "eve-local"}, MaxEventRatePerMin: maxEventRate, Logf: func(string, ...any) {}})
	if err != nil {
		t.Fatalf("start alice transport: %v", err)
	}
	bob, err := StartTransport(ctx, TransportConfig{Node: bobNode, Tasks: bobDB, Executor: bobExec, Config: Config{NATSURL: srv.ClientURL(), AgentID: "adam-n200"}, MaxEventRatePerMin: maxEventRate, Logf: func(string, ...any) {}})
	if err != nil {
		t.Fatalf("start bob transport: %v", err)
	}
	t.Cleanup(func() {
		alice.Stop()
		bob.Stop()
		aliceNode.Close()
		bobNode.Close()
		_ = aliceDB.Close()
		_ = bobDB.Close()
	})
	return &integrationPair{aliceNode: aliceNode, bobNode: bobNode, alice: alice, bob: bob, aliceDB: aliceDB, bobDB: bobDB, bobExec: bobExec}
}

func integrationRequest(messageID MessageID) TaskExecutionRequest {
	return TaskExecutionRequest{MessageID: messageID, ClientTaskRef: "client-" + string(messageID), ContextID: "ctx-1", From: "eve-local", To: "adam-n200", ChannelRef: "backend", SkillID: "backend/review", UserVisibleSummary: "review this", Payload: json.RawMessage(`{"message":{"parts":[{"kind":"text","text":"review"}]}}`), ResultVisibility: "proxy", DiscordTranscriptMode: "delegator", Delivery: DeliveryOptions{TimeoutSec: 5}}
}

func waitForTaskState(t *testing.T, store *SQLiteTaskStore, direction string, taskID TaskID, state TaskState) TaskRow {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last TaskRow
	for time.Now().Before(deadline) {
		row, err := store.GetByDirectionTaskID(context.Background(), direction, taskID)
		if err == nil {
			last = row
			if row.State == state {
				return row
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("task %s/%s state = %#v, want %s", direction, taskID, last, state)
	return TaskRow{}
}

func waitForOutboundAccepted(t *testing.T, store *SQLiteTaskStore, messageID MessageID) TaskRow {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		row, err := store.GetByDirectionMessage(context.Background(), "outbound", messageID)
		if err == nil && row.TaskID != "" {
			return row
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("outbound task for %s was not accepted", messageID)
	return TaskRow{}
}

func releaseExecutor(exec *integrationExecutor) {
	select {
	case <-exec.block:
	default:
		close(exec.block)
	}
}

func TestA2AIntegrationTargetedDelegation(t *testing.T) {
	p := newIntegrationPair(t, 0)
	if _, err := p.alice.SendTask(context.Background(), integrationRequest("msg_targeted")); err != nil {
		t.Fatalf("SendTask: %v", err)
	}
	accepted := waitForOutboundAccepted(t, p.aliceDB, "msg_targeted")
	releaseExecutor(p.bobExec)
	row := waitForTaskState(t, p.aliceDB, "outbound", accepted.TaskID, TaskStateCompleted)
	if row.ExecutorAgent != "adam-n200" || row.ToAgent != "adam-n200" {
		t.Fatalf("outbound ownership = %#v", row)
	}
}

func TestA2AIntegrationDuplicateDelivery(t *testing.T) {
	p := newIntegrationPair(t, 0)
	req := integrationRequest("msg_dupe")
	if _, err := p.alice.SendTask(context.Background(), req); err != nil {
		t.Fatalf("SendTask: %v", err)
	}
	<-p.bobExec.started
	raw := mustTaskEnvelope(t, req)
	if _, err := p.aliceNode.Publish(context.Background(), TaskSubject(req.From, req.To, req.MessageID), raw, "manual-duplicate-delivery"); err != nil {
		t.Fatalf("duplicate publish: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if got := p.bobExec.RunCount(); got != 1 {
		t.Fatalf("duplicate delivery ran %d times, want 1", got)
	}
	releaseExecutor(p.bobExec)
}

func TestA2AIntegrationCancelOwnership(t *testing.T) {
	p := newIntegrationPair(t, 0)
	if _, err := p.alice.SendTask(context.Background(), integrationRequest("msg_cancel")); err != nil {
		t.Fatalf("SendTask: %v", err)
	}
	accepted := waitForOutboundAccepted(t, p.aliceDB, "msg_cancel")
	forged := newEnvelope("mallory", "adam-n200", EnvelopeTypeControl, "forged_cancel", accepted.TaskID, accepted.Revision+1, []byte(`{"reason":"bad"}`))
	raw, _ := json.Marshal(forged)
	if _, err := p.aliceNode.Publish(context.Background(), ControlSubject("mallory", "adam-n200", accepted.TaskID, ControlKindCancel), raw, "forged-cancel"); err != nil {
		t.Fatalf("publish forged cancel: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	bobRow, err := p.bobDB.GetByDirectionTaskID(context.Background(), "inbound", accepted.TaskID)
	if err != nil {
		t.Fatalf("bob row: %v", err)
	}
	if bobRow.State == TaskStateCanceled {
		t.Fatal("forged cancel mutated inbound task")
	}
	if err := p.alice.PublishControl(context.Background(), "adam-n200", accepted.TaskID, ControlKindCancel, accepted.Revision+1, ControlPayload{Reason: "user cancel"}); err != nil {
		t.Fatalf("valid cancel: %v", err)
	}
	waitForTaskState(t, p.aliceDB, "outbound", accepted.TaskID, TaskStateCanceled)
	releaseExecutor(p.bobExec)
}

func TestA2AIntegrationEventOwnershipRejectsForgedExecutor(t *testing.T) {
	p := newIntegrationPair(t, 0)
	if _, err := p.alice.SendTask(context.Background(), integrationRequest("msg_forged_status")); err != nil {
		t.Fatalf("SendTask: %v", err)
	}
	accepted := waitForOutboundAccepted(t, p.aliceDB, "msg_forged_status")
	payload := TaskEventPayload{TaskID: accepted.TaskID, State: TaskStateFailed, Revision: accepted.Revision + 10, Error: TaskError{Code: ErrorPolicyDenied, Message: "forged"}}
	rawPayload, _ := json.Marshal(payload)
	forged := newEnvelope("mallory", "eve-local", EnvelopeTypeEvent, "msg_forged_status_event", accepted.TaskID, payload.Revision, rawPayload)
	raw, _ := json.Marshal(forged)
	if _, err := p.aliceNode.Publish(context.Background(), EventSubject("mallory", "eve-local", string(accepted.TaskID), EventKindStatus), raw, "forged-status"); err != nil {
		t.Fatalf("publish forged status: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	row, err := p.aliceDB.GetByDirectionTaskID(context.Background(), "outbound", accepted.TaskID)
	if err != nil {
		t.Fatalf("alice row: %v", err)
	}
	if row.State == TaskStateFailed || row.Error.Code == ErrorPolicyDenied {
		t.Fatalf("forged event mutated outbound task = %#v", row)
	}
	releaseExecutor(p.bobExec)
}

func TestA2AIntegrationAcceptedEventRequiresOutboundDelegator(t *testing.T) {
	srv := runEmbeddedNATS(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	node, err := Connect(ctx, NodeConfig{Config: Config{NATSURL: srv.ClientURL(), AgentID: "eve-local"}})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	store, err := OpenTaskStore(t.TempDir())
	if err != nil {
		t.Fatalf("task store: %v", err)
	}
	exec := newIntegrationExecutor(store, "eve-local")
	transport, err := StartTransport(ctx, TransportConfig{Node: node, Tasks: store, Executor: exec, Config: Config{NATSURL: srv.ClientURL(), AgentID: "eve-local", RuntimeIDMode: RuntimeIDModeRuntime}, RuntimeAgentIDs: []AgentID{"other-local"}, Logf: func(string, ...any) {}})
	if err != nil {
		t.Fatalf("start transport: %v", err)
	}
	t.Cleanup(func() {
		transport.Stop()
		node.Close()
		_ = store.Close()
	})
	if _, err := store.CreateOutbound(context.Background(), TaskRow{ClientTaskRef: "owner-1", MessageID: "msg_foreign_accept", FromAgent: "eve-local", ToAgent: "adam-n200", State: TaskStateSubmitted}); err != nil {
		t.Fatalf("CreateOutbound: %v", err)
	}
	payload := TaskEventPayload{TaskID: "task_foreign_accept", State: TaskStateWorking, Revision: 1}
	rawPayload, _ := json.Marshal(payload)
	forged := newEnvelope("adam-n200", "other-local", EnvelopeTypeEvent, "msg_foreign_accept", "task_foreign_accept", 1, rawPayload)
	raw, _ := json.Marshal(forged)
	if _, err := node.Publish(context.Background(), EventSubject("other-local", "adam-n200", "task_foreign_accept", EventKindAccepted), raw, "forged-accepted-delegator"); err != nil {
		t.Fatalf("publish forged accepted: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	got, err := store.GetByDirectionMessage(context.Background(), "outbound", "msg_foreign_accept")
	if err != nil {
		t.Fatalf("outbound row: %v", err)
	}
	if got.TaskID != "" || got.State != TaskStateSubmitted || got.ExecutorAgent != "" {
		t.Fatalf("foreign accepted event mutated outbound task = %#v", got)
	}
}

func TestA2AIntegrationPreAcceptRejectRequiresOutboundTargetExecutor(t *testing.T) {
	p := newIntegrationPair(t, 0)
	req := integrationRequest("msg_forged_reject")
	row, err := p.aliceDB.CreateOutbound(context.Background(), TaskRow{ClientTaskRef: req.ClientTaskRef, MessageID: req.MessageID, FromAgent: req.From, ToAgent: req.To, State: TaskStateSubmitted})
	if err != nil {
		t.Fatalf("CreateOutbound: %v", err)
	}
	payload := TaskEventPayload{MessageID: req.MessageID, ClientTaskRef: req.ClientTaskRef, State: TaskStateRejected, Revision: 1, Error: TaskError{Code: ErrorPolicyDenied, Message: "forged"}}
	rawPayload, _ := json.Marshal(payload)
	forged := newEnvelope("mallory", "eve-local", EnvelopeTypeEvent, req.MessageID, "", 1, rawPayload)
	raw, _ := json.Marshal(forged)
	if _, err := p.aliceNode.Publish(context.Background(), EventSubject("mallory", "eve-local", "msg_"+string(req.MessageID), EventKindRejected), raw, "forged-reject"); err != nil {
		t.Fatalf("publish forged rejected: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	got, err := p.aliceDB.GetByLocalID(context.Background(), row.LocalID)
	if err != nil {
		t.Fatalf("outbound row: %v", err)
	}
	if got.State == TaskStateRejected || got.Terminal {
		t.Fatalf("forged pre-accept reject mutated outbound task = %#v", got)
	}
}

func TestA2AIntegrationContinuationControlsPublishStatus(t *testing.T) {
	for _, tc := range []struct {
		name    string
		state   TaskState
		control string
		content string
	}{
		{name: "input", state: TaskStateInputRequired, control: ControlKindInputReply, content: "input received"},
		{name: "auth", state: TaskStateAuthRequired, control: ControlKindAuthReply, content: "authorization received"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newIntegrationPair(t, 0)
			p.bobExec.result = TaskExecutionResult{State: tc.state, Content: tc.name + " needed"}
			msgID := MessageID("msg_continue_" + tc.name)
			if _, err := p.alice.SendTask(context.Background(), integrationRequest(msgID)); err != nil {
				t.Fatalf("SendTask: %v", err)
			}
			accepted := waitForOutboundAccepted(t, p.aliceDB, msgID)
			releaseExecutor(p.bobExec)
			blocked := waitForTaskState(t, p.aliceDB, "outbound", accepted.TaskID, tc.state)
			p.bobExec.result = TaskExecutionResult{State: TaskStateCompleted, Content: tc.name + " continued"}
			if err := p.alice.PublishControl(context.Background(), "adam-n200", accepted.TaskID, tc.control, blocked.Revision+1, ControlPayload{A2A: json.RawMessage(`{"ok":true}`)}); err != nil {
				t.Fatalf("PublishControl %s: %v", tc.control, err)
			}
			done := waitForTaskState(t, p.aliceDB, "outbound", accepted.TaskID, TaskStateCompleted)
			if !done.Terminal || done.Error.Code != "" || p.bobExec.RunCount() != 2 {
				t.Fatalf("continuation result = %#v runs=%d, want terminal completion from resumed executor", done, p.bobExec.RunCount())
			}
		})
	}
}

func TestA2AIntegrationAuthDenyCompletesAsFailed(t *testing.T) {
	p := newIntegrationPair(t, 0)
	p.bobExec.result = TaskExecutionResult{State: TaskStateAuthRequired, Content: "auth needed"}
	if _, err := p.alice.SendTask(context.Background(), integrationRequest("msg_auth_deny")); err != nil {
		t.Fatalf("SendTask: %v", err)
	}
	accepted := waitForOutboundAccepted(t, p.aliceDB, "msg_auth_deny")
	releaseExecutor(p.bobExec)
	blocked := waitForTaskState(t, p.aliceDB, "outbound", accepted.TaskID, TaskStateAuthRequired)
	if err := p.alice.PublishControl(context.Background(), "adam-n200", accepted.TaskID, ControlKindAuthReply, blocked.Revision+1, ControlPayload{A2A: json.RawMessage(`{"approve":false,"denyReason":"user denied"}`)}); err != nil {
		t.Fatalf("PublishControl auth deny: %v", err)
	}
	done := waitForTaskState(t, p.aliceDB, "outbound", accepted.TaskID, TaskStateFailed)
	if !done.Terminal || done.Error.Code != ErrorAuthNotSatisfied || p.bobExec.RunCount() != 1 {
		t.Fatalf("auth deny result = %#v runs=%d, want terminal auth failure without resumed executor", done, p.bobExec.RunCount())
	}
}

func TestA2AIntegrationAcceptedBootstrap(t *testing.T) {
	p := newIntegrationPair(t, 0)
	if _, err := p.alice.SendTask(context.Background(), integrationRequest("msg_bootstrap")); err != nil {
		t.Fatalf("SendTask: %v", err)
	}
	row := waitForOutboundAccepted(t, p.aliceDB, "msg_bootstrap")
	if row.TaskID == "" || row.State != TaskStateWorking || row.ExecutorAgent != "adam-n200" {
		t.Fatalf("accepted bootstrap row = %#v", row)
	}
	releaseExecutor(p.bobExec)
}

func TestA2AIntegrationAdmissionBeforeExecution(t *testing.T) {
	p := newIntegrationPair(t, 0)
	if _, err := p.alice.SendTask(context.Background(), integrationRequest("msg_admit_first")); err != nil {
		t.Fatalf("SendTask: %v", err)
	}
	admission := <-p.bobExec.admitted
	if admission.TaskID == "" {
		t.Fatal("admission missing task id")
	}
	accepted := waitForOutboundAccepted(t, p.aliceDB, "msg_admit_first")
	if accepted.TaskID != admission.TaskID {
		t.Fatalf("accepted task = %s, admission task = %s", accepted.TaskID, admission.TaskID)
	}
	releaseExecutor(p.bobExec)
}

func TestA2AIntegrationAckAfterAdmissionNotCompletion(t *testing.T) {
	p := newIntegrationPair(t, 0)
	if _, err := p.alice.SendTask(context.Background(), integrationRequest("msg_ack")); err != nil {
		t.Fatalf("SendTask: %v", err)
	}
	<-p.bobExec.started
	accepted := waitForOutboundAccepted(t, p.aliceDB, "msg_ack")
	if accepted.State != TaskStateWorking {
		t.Fatalf("state before completion = %s, want working", accepted.State)
	}
	releaseExecutor(p.bobExec)
}

func TestA2AIntegrationReplayAfterReconnect(t *testing.T) {
	srv := runEmbeddedNATS(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	aliceNode, err := Connect(ctx, NodeConfig{Config: Config{NATSURL: srv.ClientURL(), AgentID: "eve-local"}})
	if err != nil {
		t.Fatalf("connect alice: %v", err)
	}
	bobNode, err := Connect(ctx, NodeConfig{Config: Config{NATSURL: srv.ClientURL(), AgentID: "adam-n200"}})
	if err != nil {
		t.Fatalf("connect bob: %v", err)
	}
	aliceDB, _ := OpenTaskStore(t.TempDir())
	_, err = aliceDB.CreateOutbound(context.Background(), TaskRow{MessageID: "msg_replay", FromAgent: "eve-local", ToAgent: "adam-n200", State: TaskStateSubmitted})
	if err != nil {
		t.Fatalf("create outbound: %v", err)
	}
	if err := EnsureStreams(ctx, bobNode); err != nil {
		t.Fatalf("EnsureStreams: %v", err)
	}
	pub := NewPublisher(bobNode, nil, "adam-n200", 0)
	if err := pub.PublishAccepted(ctx, "eve-local", "msg_replay", "task_replay", 1); err != nil {
		t.Fatalf("publish accepted: %v", err)
	}
	if err := pub.PublishResult(ctx, "eve-local", TaskExecutionResult{TaskID: "task_replay", State: TaskStateCompleted, Revision: 2, Content: "done"}, "msg_replay"); err != nil {
		t.Fatalf("publish result: %v", err)
	}
	aliceExec := newIntegrationExecutor(aliceDB, "eve-local")
	alice, err := StartTransport(ctx, TransportConfig{Node: aliceNode, Tasks: aliceDB, Executor: aliceExec, Config: Config{NATSURL: srv.ClientURL(), AgentID: "eve-local"}, Logf: func(string, ...any) {}})
	if err != nil {
		t.Fatalf("start alice: %v", err)
	}
	t.Cleanup(func() { alice.Stop(); aliceNode.Close(); bobNode.Close(); _ = aliceDB.Close() })
	waitForTaskState(t, aliceDB, "outbound", "task_replay", TaskStateCompleted)
}

func TestA2AIntegrationEventRateQuota(t *testing.T) {
	assertEventRateSecondPublish(t, 1, ErrorOverloaded)
}

func TestA2AIntegrationEventRateOverloaded(t *testing.T) {
	assertEventRateSecondPublish(t, 1, ErrorOverloaded)
}

func TestA2AIntegrationEventRateZeroUnlimited(t *testing.T) {
	assertEventRateSecondPublish(t, 0, "")
}

func assertEventRateSecondPublish(t *testing.T, limit int, want ErrorCode) {
	t.Helper()
	node := connectedTestNode(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := EnsureStreams(ctx, node); err != nil {
		t.Fatalf("EnsureStreams: %v", err)
	}
	pub := NewPublisher(node, nil, "adam-n200", limit)
	if err := pub.PublishStatus(ctx, "eve-local", "task_rate", 1, TaskStateWorking, "", TaskError{}); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	err := pub.PublishStatus(ctx, "eve-local", "task_rate", 2, TaskStateWorking, "", TaskError{})
	if want == "" {
		if err != nil {
			t.Fatalf("second publish: %v", err)
		}
		return
	}
	var terr TransportError
	if !strings.Contains(fmt.Sprint(err), string(want)) || !errorsAs(err, &terr) {
		t.Fatalf("second publish error = %v, want %s", err, want)
	}
}

func TestA2AIntegrationNoErrorEnvelope(t *testing.T) {
	subject, err := ParseSubject(EventSubject("adam-n200", "eve-local", "task_no_error", "error"))
	if err != nil {
		t.Fatalf("ParseSubject: %v", err)
	}
	env := Envelope{Version: Version, Binding: Binding, MessageID: "msg_error", From: "adam-n200", To: "eve-local", Type: EnvelopeType("error"), TaskID: "task_no_error", Payload: []byte(`{}`)}
	if err := ValidateEnvelope(env, subject); err == nil {
		t.Fatal("standalone error envelope was accepted")
	}
}

func TestA2AIntegrationNoPoolSubject(t *testing.T) {
	if _, err := ParseSubject("a2a.v1.pool.review"); err == nil {
		t.Fatal("pool subject accepted")
	}
	for _, cfg := range consumerConfigs("adam-n200") {
		if strings.Contains(cfg.Config.FilterSubject, "pool") {
			t.Fatalf("consumer %s has pool filter %s", cfg.Config.Durable, cfg.Config.FilterSubject)
		}
	}
}

func TestTransportStartDisabledNoop(t *testing.T) {
	transport, err := StartTransport(context.Background(), TransportConfig{Node: &Node{}, Config: Config{}})
	if err != nil {
		t.Fatalf("StartTransport disabled: %v", err)
	}
	if transport == nil {
		t.Fatal("disabled transport returned nil")
	}
	transport.Stop()
}

func TestTransportPublisherDeclaresNatsMsgID(t *testing.T) {
	node := connectedTestNode(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := EnsureStreams(ctx, node); err != nil {
		t.Fatalf("EnsureStreams: %v", err)
	}
	pub := NewPublisher(node, nil, "adam-n200", 0)
	if err := pub.PublishStatus(ctx, "eve-local", "task_msgid", 3, TaskStateWorking, "working", TaskError{}); err != nil {
		t.Fatalf("PublishStatus: %v", err)
	}
	stream, err := node.JetStream().Stream(ctx, StreamEvents)
	if err != nil {
		t.Fatalf("StreamEvents: %v", err)
	}
	raw, err := stream.GetLastMsgForSubject(ctx, EventSubject("adam-n200", "eve-local", "task_msgid", EventKindStatus))
	if err != nil {
		t.Fatalf("GetLastMsgForSubject: %v", err)
	}
	if got, want := raw.Header.Get("Nats-Msg-Id"), StatusEventNatsMsgID("adam-n200", "eve-local", "task_msgid", 3); got != want {
		t.Fatalf("Nats-Msg-Id = %q, want %q", got, want)
	}
}

func mustTaskEnvelope(t *testing.T, req TaskExecutionRequest) []byte {
	t.Helper()
	payload := SendMessagePayload{A2A: req.Payload, ChannelRef: req.ChannelRef, SkillID: req.SkillID, UserVisibleSummary: req.UserVisibleSummary, ClientTaskRef: req.ClientTaskRef, ContextID: req.ContextID}
	payload.Delivery = TransportDelivery{TimeoutSec: req.Delivery.TimeoutSec, ResultVisibility: req.ResultVisibility, DiscordTranscriptMode: req.DiscordTranscriptMode}
	raw, _ := json.Marshal(payload)
	envRaw, _ := json.Marshal(newEnvelope(req.From, req.To, EnvelopeTypeTask, req.MessageID, "", 0, raw))
	return envRaw
}

func errorsAs(err error, target interface{}) bool {
	switch t := target.(type) {
	case *TransportError:
		if v, ok := err.(TransportError); ok {
			*t = v
			return true
		}
	}
	return false
}
