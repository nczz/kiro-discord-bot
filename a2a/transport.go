package a2a

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

type TransportError struct {
	Code    ErrorCode
	Message string
}

func (e TransportError) Error() string {
	if strings.TrimSpace(e.Message) == "" {
		return string(e.Code)
	}
	return string(e.Code) + ": " + e.Message
}

type Publisher struct {
	node  *Node
	tasks *SQLiteTaskStore
	from  AgentID
	rate  *eventRateLimiter
}
type EventDeliverySink func(context.Context, TaskRow, string, TaskEventPayload) error

type TransportConfig struct {
	Node               *Node
	Tasks              *SQLiteTaskStore
	Executor           Executor
	Config             Config
	MaxEventRatePerMin int
	EventSink          EventDeliverySink
	RuntimeAgentIDs    []AgentID
	Logf               func(string, ...any)
}

type Transport struct {
	Publisher
	executor  Executor
	logf      func(string, ...any)
	eventSink EventDeliverySink

	mu       sync.Mutex
	consumes []jetstream.ConsumeContext
	started  map[string]struct{}
	accepts  map[AgentID]struct{}
}

type eventRateLimiter struct {
	mu     sync.Mutex
	limit  int
	events []time.Time
}

func NewPublisher(node *Node, tasks *SQLiteTaskStore, from AgentID, maxEventRatePerMin int) *Publisher {
	return &Publisher{node: node, tasks: tasks, from: from, rate: &eventRateLimiter{limit: maxEventRatePerMin}}
}

func StartTransport(ctx context.Context, cfg TransportConfig) (*Transport, error) {
	if cfg.Node == nil || !cfg.Node.IsEnabled() {
		return &Transport{Publisher: Publisher{node: cfg.Node, tasks: cfg.Tasks, from: cfg.Config.AgentID, rate: &eventRateLimiter{limit: cfg.MaxEventRatePerMin}}, executor: cfg.Executor, logf: cfg.Logf, eventSink: cfg.EventSink, started: make(map[string]struct{})}, nil
	}
	agentID := cfg.Config.AgentID
	if agentID == "" {
		agentID = cfg.Node.AgentID()
	}
	if err := ValidateAgentID(agentID); err != nil {
		return nil, err
	}
	if cfg.Tasks == nil {
		return nil, fmt.Errorf("A2A task store is required for transport")
	}
	if cfg.Executor == nil {
		return nil, fmt.Errorf("A2A executor is required for transport")
	}
	if err := EnsureStreams(ctx, cfg.Node); err != nil {
		return nil, err
	}
	agentIDs, err := transportAgentIDs(cfg, agentID)
	if err != nil {
		return nil, err
	}
	for _, id := range agentIDs {
		if err := EnsureConsumersForAgent(ctx, cfg.Node, id); err != nil {
			return nil, err
		}
	}
	t := &Transport{Publisher: Publisher{node: cfg.Node, tasks: cfg.Tasks, from: agentID, rate: &eventRateLimiter{limit: cfg.MaxEventRatePerMin}}, executor: cfg.Executor, logf: cfg.Logf, eventSink: cfg.EventSink, started: make(map[string]struct{}), accepts: agentIDSet(agentIDs)}
	for _, id := range agentIDs {
		for _, cfg := range consumerConfigs(id) {
			stream, err := t.node.JetStream().Stream(ctx, cfg.Stream)
			if err != nil {
				t.Stop()
				return nil, fmt.Errorf("open stream %s: %w", cfg.Stream, err)
			}
			consumer, err := stream.Consumer(ctx, cfg.Config.Durable)
			if err != nil {
				t.Stop()
				return nil, fmt.Errorf("open consumer %s: %w", cfg.Config.Durable, err)
			}
			consume, err := consumer.Consume(t.handlerForStream(cfg.Stream))
			if err != nil {
				t.Stop()
				return nil, fmt.Errorf("start consumer %s: %w", cfg.Config.Durable, err)
			}
			t.consumes = append(t.consumes, consume)
		}
	}
	return t, nil
}
func transportAgentIDs(cfg TransportConfig, base AgentID) ([]AgentID, error) {
	ids := []AgentID{base}
	if cfg.Config.RuntimeIDMode.UsesRuntimeIDs() {
		ids = append(ids, cfg.RuntimeAgentIDs...)
	}
	seen := make(map[AgentID]struct{}, len(ids))
	out := make([]AgentID, 0, len(ids))
	for _, id := range ids {
		id = AgentID(strings.TrimSpace(string(id)))
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		if err := ValidateAgentID(id); err != nil {
			return nil, err
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("A2A transport requires at least one agent id")
	}
	return out, nil
}

func agentIDSet(ids []AgentID) map[AgentID]struct{} {
	out := make(map[AgentID]struct{}, len(ids))
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out
}

func (t *Transport) acceptsAgent(id AgentID) bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.acceptsAgentLocked(id)
}

func (t *Transport) acceptsAgentLocked(id AgentID) bool {
	if len(t.accepts) == 0 {
		return id == t.from
	}
	_, ok := t.accepts[id]
	return ok
}
func (t *Transport) Accepts(id AgentID) bool {
	return t.acceptsAgent(id)
}

func (p *Publisher) WithFrom(from AgentID) *Publisher {
	if p == nil {
		return nil
	}
	copy := *p
	copy.from = from
	return &copy
}

func (t *Transport) publisherFrom(from AgentID) *Publisher {
	return t.Publisher.WithFrom(from)
}

func (t *Transport) AddAgent(ctx context.Context, id AgentID) error {
	id = AgentID(strings.TrimSpace(string(id)))
	if id == "" {
		return nil
	}
	if err := ValidateAgentID(id); err != nil {
		return err
	}
	if t.acceptsAgent(id) {
		return nil
	}
	if t.node == nil || !t.node.IsEnabled() {
		return nil
	}
	if err := EnsureConsumersForAgent(ctx, t.node, id); err != nil {
		return err
	}
	t.mu.Lock()
	if t.accepts == nil {
		t.accepts = map[AgentID]struct{}{t.from: {}}
	}
	if _, ok := t.accepts[id]; ok {
		t.mu.Unlock()
		return nil
	}
	t.accepts[id] = struct{}{}
	t.mu.Unlock()

	var consumes []jetstream.ConsumeContext
	for _, cfg := range consumerConfigs(id) {
		stream, err := t.node.JetStream().Stream(ctx, cfg.Stream)
		if err != nil {
			t.removeAcceptedAgent(id)
			for _, consume := range consumes {
				consume.Stop()
			}
			return fmt.Errorf("open stream %s: %w", cfg.Stream, err)
		}
		consumer, err := stream.Consumer(ctx, cfg.Config.Durable)
		if err != nil {
			t.removeAcceptedAgent(id)
			for _, consume := range consumes {
				consume.Stop()
			}
			return fmt.Errorf("open consumer %s: %w", cfg.Config.Durable, err)
		}
		consume, err := consumer.Consume(t.handlerForStream(cfg.Stream))
		if err != nil {
			t.removeAcceptedAgent(id)
			for _, consume := range consumes {
				consume.Stop()
			}
			return fmt.Errorf("start consumer %s: %w", cfg.Config.Durable, err)
		}
		consumes = append(consumes, consume)
	}
	t.mu.Lock()
	t.consumes = append(t.consumes, consumes...)
	t.mu.Unlock()
	return nil
}

func (t *Transport) removeAcceptedAgent(id AgentID) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.accepts, id)
}

func (t *Transport) Stop() {
	if t == nil {
		return
	}
	t.mu.Lock()
	consumes := append([]jetstream.ConsumeContext(nil), t.consumes...)
	t.consumes = nil
	t.mu.Unlock()
	for _, consume := range consumes {
		consume.Stop()
		select {
		case <-consume.Closed():
		case <-time.After(2 * time.Second):
		}
	}
}

func (t *Transport) handlerForStream(stream string) jetstream.MessageHandler {
	return func(msg jetstream.Msg) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var err error
		switch stream {
		case StreamTasks:
			err = t.handleTaskMessage(ctx, msg)
		case StreamControls:
			err = t.handleControlMessage(ctx, msg)
		case StreamEvents:
			err = t.handleEventMessage(ctx, msg)
		default:
			err = fmt.Errorf("unknown stream %s", stream)
		}
		if err != nil {
			t.log("[a2a] consume %s %s: %v", stream, msg.Subject(), err)
		}
	}
}

func (p *Publisher) SendTask(ctx context.Context, req TaskExecutionRequest) (TaskRow, error) {
	if p == nil {
		return TaskRow{}, fmt.Errorf("publisher is nil")
	}
	if req.From == "" {
		req.From = p.from
	}
	if err := ValidateAgentID(req.From); err != nil {
		return TaskRow{}, err
	}
	if err := ValidateAgentID(req.To); err != nil {
		return TaskRow{}, err
	}
	if err := ValidateMessageID(req.MessageID); err != nil {
		return TaskRow{}, err
	}
	delivery := req.Delivery
	if delivery.DiscordContext == nil && len(delivery.DiscordContextJSON) > 0 {
		var dc DiscordContext
		if err := json.Unmarshal(delivery.DiscordContextJSON, &dc); err != nil {
			return TaskRow{}, fmt.Errorf("discord context json: %w", err)
		}
		delivery.DiscordContext = &dc
	}
	payload := SendMessagePayload{A2A: req.Payload, ChannelRef: req.ChannelRef, SkillID: req.SkillID, UserVisibleSummary: req.UserVisibleSummary, ClientTaskRef: req.ClientTaskRef, ContextID: req.ContextID, AuditMetadata: req.AuditMetadata, OriginRequester: req.OriginRequester, OriginRuntimeRef: req.OriginRuntimeRef}
	payload.Delivery = TransportDelivery{TimeoutSec: delivery.TimeoutSec, RequiresConfirmation: delivery.RequiresConfirmation, DiscordContext: delivery.DiscordContext, DiscordReplyChannelID: delivery.DiscordReplyChannelID, DiscordReplyThreadID: delivery.DiscordReplyThreadID, ShareDiscordContext: delivery.ShareDiscordContext, CoPresentFrom: delivery.CoPresentFrom, MaxDelegationDepth: delivery.MaxDelegationDepth, ResultVisibility: req.ResultVisibility, DiscordTranscriptMode: req.DiscordTranscriptMode}
	raw, err := json.Marshal(payload)
	if err != nil {
		return TaskRow{}, err
	}
	env := newEnvelope(req.From, req.To, EnvelopeTypeTask, req.MessageID, "", 0, raw)
	envRaw, err := json.Marshal(env)
	if err != nil {
		return TaskRow{}, err
	}
	row := TaskRow{}
	if p.tasks != nil {
		row, err = p.tasks.CreateOutbound(ctx, TaskRow{ClientTaskRef: req.ClientTaskRef, MessageID: req.MessageID, ContextID: req.ContextID, FromAgent: req.From, ToAgent: req.To, ChannelID: req.ChannelID, GuildID: req.GuildID, ChannelRef: req.ChannelRef, SkillID: req.SkillID, State: TaskStateSubmitted, ResultVisibility: firstNonEmpty(req.ResultVisibility, "proxy"), DiscordTranscriptMode: firstNonEmpty(req.DiscordTranscriptMode, "delegator"), OriginRequester: req.OriginRequester, OriginRuntimeRef: req.OriginRuntimeRef})
		if err != nil {
			return TaskRow{}, err
		}
	}
	_, err = p.node.Publish(ctx, TaskSubject(req.From, req.To, req.MessageID), envRaw, TaskNatsMsgID(req.From, req.To, req.MessageID))
	return row, err
}

func (p *Publisher) PublishControl(ctx context.Context, to AgentID, taskID TaskID, kind string, revision int64, payload ControlPayload) error {
	if err := ValidateAgentID(p.from); err != nil {
		return err
	}
	if err := ValidateAgentID(to); err != nil {
		return err
	}
	if err := ValidateTaskID(taskID); err != nil {
		return err
	}
	if revision <= 0 {
		return fmt.Errorf("revision must be positive")
	}
	if payload.MessageID == "" {
		payload.MessageID = MessageID(fmt.Sprintf("%s_%s_%d", taskID, kind, revision))
	}
	payload.Revision = revision
	raw, _ := json.Marshal(payload)
	envRaw, _ := json.Marshal(newEnvelope(p.from, to, EnvelopeTypeControl, payload.MessageID, taskID, revision, raw))
	_, err := p.node.Publish(ctx, ControlSubject(p.from, to, taskID, kind), envRaw, ControlNatsMsgID(p.from, to, taskID, kind, revision))
	return err
}

func (p *Publisher) PublishAccepted(ctx context.Context, delegator AgentID, messageID MessageID, taskID TaskID, revision int64) error {
	payload := TaskEventPayload{MessageID: messageID, TaskID: taskID, State: TaskStateWorking, Revision: revision}
	return p.publishEvent(ctx, delegator, taskID, EventKindAccepted, messageID, revision, payload)
}

func (p *Publisher) PublishRejected(ctx context.Context, delegator AgentID, messageID MessageID, clientTaskRef string, taskErr TaskError) error {
	payload := TaskEventPayload{MessageID: messageID, ClientTaskRef: clientTaskRef, TaskID: TaskID("msg_" + string(messageID)), State: TaskStateRejected, Error: taskErr, Revision: 1}
	return p.publishPreAcceptEvent(ctx, delegator, messageID, EventKindRejected, payload)
}

func (p *Publisher) PublishStatus(ctx context.Context, delegator AgentID, taskID TaskID, revision int64, state TaskState, content string, taskErr TaskError) error {
	payload := TaskEventPayload{TaskID: taskID, State: state, Content: content, Error: taskErr, Revision: revision}
	return p.publishEvent(ctx, delegator, taskID, EventKindStatus, "", revision, payload)
}

func (p *Publisher) PublishResult(ctx context.Context, delegator AgentID, result TaskExecutionResult, messageID MessageID) error {
	payload := TaskEventPayload{MessageID: messageID, TaskID: result.TaskID, State: result.State, Content: result.Content, Error: result.Error, Revision: result.Revision, Result: &result}
	return p.publishEvent(ctx, delegator, result.TaskID, EventKindResult, messageID, result.Revision, payload)
}

func (p *Publisher) PublishArtifact(ctx context.Context, delegator AgentID, taskID TaskID, revision int64, artifact TaskExecutionArtifact) error {
	payload := TaskEventPayload{TaskID: taskID, State: TaskStateWorking, Revision: revision, Artifact: &artifact}
	return p.publishEvent(ctx, delegator, taskID, EventKindArtifact, MessageID(artifact.ID), revision, payload)
}

func (p *Publisher) publishPreAcceptEvent(ctx context.Context, delegator AgentID, messageID MessageID, kind string, payload TaskEventPayload) error {
	if err := p.checkEventRate(time.Now()); err != nil {
		return err
	}
	raw, _ := json.Marshal(payload)
	envRaw, _ := json.Marshal(newEnvelope(p.from, delegator, EnvelopeTypeEvent, messageID, "", 1, raw))
	_, err := p.node.Publish(ctx, EventSubject(p.from, delegator, "msg_"+string(messageID), kind), envRaw, PreAcceptEventNatsMsgID(p.from, delegator, messageID, kind))
	return err
}

func (p *Publisher) publishEvent(ctx context.Context, delegator AgentID, taskID TaskID, kind string, messageID MessageID, revision int64, payload TaskEventPayload) error {
	if err := p.checkEventRate(time.Now()); err != nil {
		return err
	}
	if messageID == "" {
		messageID = MessageID(string(taskID) + "_" + kind)
	}
	raw, _ := json.Marshal(payload)
	envRaw, _ := json.Marshal(newEnvelope(p.from, delegator, EnvelopeTypeEvent, messageID, taskID, revision, raw))
	var msgID string
	switch kind {
	case EventKindAccepted:
		msgID = AcceptedEventNatsMsgID(p.from, delegator, taskID)
	case EventKindStatus:
		msgID = StatusEventNatsMsgID(p.from, delegator, taskID, revision)
	case EventKindResult:
		msgID = ResultEventNatsMsgID(p.from, delegator, taskID)
	case EventKindArtifact:
		artifactID := ""
		if payload.Artifact != nil {
			artifactID = payload.Artifact.ID
		}
		msgID = ArtifactEventNatsMsgID(p.from, delegator, taskID, artifactID, revision)
	default:
		msgID = fmt.Sprintf("event:%s:%s:%s:%s:%d", p.from, delegator, taskID, kind, revision)
	}
	_, err := p.node.Publish(ctx, EventSubject(p.from, delegator, string(taskID), kind), envRaw, msgID)
	return err
}

func (t *Transport) handleTaskMessage(ctx context.Context, msg jetstream.Msg) error {
	subject, env, err := decodeEnvelopeMessage(msg)
	if err != nil {
		_ = msg.TermWithReason(err.Error())
		return err
	}
	if subject.Kind != SubjectKindTask || !t.acceptsAgent(subject.To) {
		_ = msg.TermWithReason("task subject target mismatch")
		return fmt.Errorf("task subject target mismatch")
	}
	if row, err := t.tasks.GetByDirectionMessage(ctx, "inbound", subject.MessageID); err == nil && row.LocalID != "" {
		if row.Terminal {
			if row.State == TaskStateRejected {
				_ = t.publisherFrom(row.ExecutorAgent).PublishRejected(ctx, row.FromAgent, row.MessageID, row.ClientTaskRef, row.Error)
			} else {
				_ = t.publisherFrom(row.ExecutorAgent).PublishResult(ctx, row.FromAgent, TaskExecutionResult{TaskID: row.TaskID, State: row.State, Revision: row.Revision, Error: row.Error}, row.MessageID)
			}
		} else if t.markStarted(row.LocalID) {
			admission := A2AAdmission{AdmissionKey: row.LocalID, TaskID: row.TaskID, State: row.State, Revision: row.Revision, Request: taskRequestFromRow(row)}
			go t.runAccepted(admission)
		}
		return msg.DoubleAck(ctx)
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		_ = msg.Nak()
		return err
	}
	req, err := taskRequestFromEnvelope(env, subject)
	if err != nil {
		taskErr := TaskError{Code: ErrorInvalidEnvelope, Message: err.Error()}
		if recErr := t.recordAndPublishRejected(ctx, req, subject, env, taskErr); recErr != nil {
			_ = msg.Nak()
			return recErr
		}
		return msg.DoubleAck(ctx)
	}
	admission, err := t.executor.AdmitA2ATask(ctx, req)
	if err != nil {
		_ = msg.Nak()
		return err
	}
	if !admission.Accepted {
		if err := t.recordAndPublishRejected(ctx, req, subject, env, admission.Error); err != nil {
			_ = msg.Nak()
			return err
		}
		return msg.DoubleAck(ctx)
	}
	if err := t.publisherFrom(subject.To).PublishAccepted(ctx, subject.From, subject.MessageID, admission.TaskID, admission.Revision); err != nil {
		_ = msg.Nak()
		return err
	}
	if err := msg.DoubleAck(ctx); err != nil {
		return err
	}
	if t.markStarted(admission.AdmissionKey) {
		go t.runAccepted(admission.Admission)
	}
	return nil
}

func (t *Transport) handleControlMessage(ctx context.Context, msg jetstream.Msg) error {
	subject, env, err := decodeEnvelopeMessage(msg)
	if err != nil {
		_ = msg.TermWithReason(err.Error())
		return err
	}
	if subject.Kind != SubjectKindControl || !t.acceptsAgent(subject.Executor) {
		_ = msg.TermWithReason("control subject target mismatch")
		return fmt.Errorf("control subject target mismatch")
	}
	row, err := t.tasks.GetByDirectionTaskID(ctx, "inbound", subject.TaskID)
	if err != nil {
		_ = msg.TermWithReason("control task ownership not found")
		return err
	}
	if row.FromAgent != subject.From || row.ExecutorAgent != subject.Executor {
		_ = msg.TermWithReason("control ownership mismatch")
		return fmt.Errorf("control ownership mismatch")
	}
	var payload ControlPayload
	_ = json.Unmarshal(env.Payload, &payload)
	switch subject.Control {
	case ControlKindCancel:
		rev := env.Revision
		if rev <= row.Revision {
			rev = row.Revision + 1
		}
		taskErr := TaskError{Code: ErrorCode("canceled"), Message: firstNonEmpty(payload.Reason, "cancel requested")}
		applied, err := t.tasks.ApplyTaskEvent(ctx, "inbound", row.TaskID, EventRow{TaskID: row.TaskID, Revision: rev, EventType: EventKindStatus, State: TaskStateCanceled, PayloadJSON: string(env.Payload)}, TaskStateCanceled, taskErr)
		if err != nil {
			_ = msg.Nak()
			return err
		}
		if err := t.publisherFrom(row.ExecutorAgent).PublishStatus(ctx, row.FromAgent, row.TaskID, applied.Revision, TaskStateCanceled, "", taskErr); err != nil {
			_ = msg.Nak()
			return err
		}
	case ControlKindStatusRequest:
		if err := t.publisherFrom(row.ExecutorAgent).PublishStatus(ctx, row.FromAgent, row.TaskID, row.Revision, row.State, "", row.Error); err != nil {
			_ = msg.Nak()
			return err
		}
	case ControlKindInputReply:
		if row.State != TaskStateInputRequired {
			if row.State == TaskStateWorking && env.Revision <= row.Revision {
				if err := t.replayContinuationControl(ctx, msg, row, env, ControlKindInputReply, "input received"); err != nil {
					return err
				}
				break
			}
			_ = msg.TermWithReason("input_reply requires TASK_STATE_INPUT_REQUIRED")
			return TransportError{Code: ErrorInvalidEnvelope, Message: "input_reply requires TASK_STATE_INPUT_REQUIRED"}
		}
		if err := t.applyContinuationControl(ctx, msg, row, env, ControlKindInputReply, "input received"); err != nil {
			return err
		}
	case ControlKindAuthReply:
		payload := controlPayloadFromEnvelope(env)
		if denied, reason := authReplyDenied(payload); denied {
			if row.State != TaskStateAuthRequired {
				_ = msg.TermWithReason("auth denial requires TASK_STATE_AUTH_REQUIRED")
				return TransportError{Code: ErrorInvalidEnvelope, Message: "auth denial requires TASK_STATE_AUTH_REQUIRED"}
			}
			if err := t.applyAuthDeniedControl(ctx, msg, row, env, reason); err != nil {
				return err
			}
			break
		}
		if row.State != TaskStateAuthRequired {
			if row.State == TaskStateWorking && env.Revision <= row.Revision {
				if err := t.replayContinuationControl(ctx, msg, row, env, ControlKindAuthReply, "authorization received"); err != nil {
					return err
				}
				break
			}
			_ = msg.TermWithReason("auth_reply requires TASK_STATE_AUTH_REQUIRED")
			return TransportError{Code: ErrorInvalidEnvelope, Message: "auth_reply requires TASK_STATE_AUTH_REQUIRED"}
		}
		if err := t.applyContinuationControl(ctx, msg, row, env, ControlKindAuthReply, "authorization received"); err != nil {
			return err
		}
	default:
		_ = msg.TermWithReason("unsupported control kind")
		return TransportError{Code: ErrorUnsupportedOperation, Message: subject.Control}
	}
	return msg.DoubleAck(ctx)
}

func (t *Transport) applyContinuationControl(ctx context.Context, msg jetstream.Msg, row TaskRow, env Envelope, kind string, content string) error {
	rev := env.Revision
	if rev <= row.Revision {
		rev = row.Revision + 1
	}
	applied, err := t.tasks.ApplyTaskEvent(ctx, "inbound", row.TaskID, EventRow{TaskID: row.TaskID, Revision: rev, EventType: kind, State: TaskStateWorking, PayloadJSON: string(env.Payload)}, TaskStateWorking, TaskError{})
	if err != nil {
		_ = msg.Nak()
		return err
	}
	if err := t.publisherFrom(row.ExecutorAgent).PublishStatus(ctx, row.FromAgent, row.TaskID, applied.Revision, TaskStateWorking, content, TaskError{}); err != nil {
		_ = msg.Nak()
		return err
	}
	t.startContinuation(row, env, applied.Revision, kind)
	return nil
}

func (t *Transport) replayContinuationControl(ctx context.Context, msg jetstream.Msg, row TaskRow, env Envelope, kind string, content string) error {
	if err := t.publisherFrom(row.ExecutorAgent).PublishStatus(ctx, row.FromAgent, row.TaskID, row.Revision, TaskStateWorking, content, TaskError{}); err != nil {
		_ = msg.Nak()
		return err
	}
	t.startContinuation(row, env, row.Revision, kind)
	return nil
}

func (t *Transport) applyAuthDeniedControl(ctx context.Context, msg jetstream.Msg, row TaskRow, env Envelope, reason string) error {
	rev := env.Revision
	if rev <= row.Revision {
		rev = row.Revision + 1
	}
	taskErr := TaskError{Code: ErrorAuthNotSatisfied, Message: firstNonEmpty(reason, "authorization denied")}
	applied, err := t.tasks.ApplyTaskEvent(ctx, "inbound", row.TaskID, EventRow{TaskID: row.TaskID, Revision: rev, EventType: ControlKindAuthReply, State: TaskStateFailed, PayloadJSON: string(env.Payload)}, TaskStateFailed, taskErr)
	if err != nil {
		_ = msg.Nak()
		return err
	}
	result := TaskExecutionResult{TaskID: row.TaskID, State: TaskStateFailed, Revision: applied.Revision, Error: taskErr, Content: taskErr.Message}
	if err := t.publisherFrom(row.ExecutorAgent).PublishResult(ctx, row.FromAgent, result, row.MessageID); err != nil {
		_ = msg.Nak()
		return err
	}
	return nil
}

func (t *Transport) startContinuation(row TaskRow, env Envelope, revision int64, kind string) {
	key := strings.TrimSpace(row.LocalID) + ":continuation:" + string(env.MessageID)
	if !t.markStarted(key) {
		return
	}
	req := taskRequestFromRow(row)
	admission := A2AAdmission{
		AdmissionKey: row.LocalID,
		TaskID:       row.TaskID,
		State:        TaskStateWorking,
		Revision:     revision,
		Request:      req,
		Continuation: &A2AContinuation{Kind: kind, Payload: controlPayloadFromEnvelope(env).A2A, Reason: controlPayloadFromEnvelope(env).Reason},
	}
	go t.runAccepted(admission)
}

func controlPayloadFromEnvelope(env Envelope) ControlPayload {
	var payload ControlPayload
	_ = json.Unmarshal(env.Payload, &payload)
	return payload
}

func authReplyDenied(payload ControlPayload) (bool, string) {
	var auth struct {
		Approve    *bool  `json:"approve,omitempty"`
		DenyReason string `json:"denyReason,omitempty"`
	}
	if len(payload.A2A) > 0 {
		_ = json.Unmarshal(payload.A2A, &auth)
	}
	if auth.Approve == nil || *auth.Approve {
		return false, ""
	}
	return true, firstNonEmpty(auth.DenyReason, payload.Reason)
}

func (t *Transport) handleEventMessage(ctx context.Context, msg jetstream.Msg) error {
	if err := t.checkEventRate(time.Now()); err != nil {
		_ = msg.Nak()
		return err
	}
	subject, env, err := decodeEnvelopeMessage(msg)
	if err != nil {
		_ = msg.TermWithReason(err.Error())
		return err
	}
	if subject.Kind != SubjectKindEvent || !t.acceptsAgent(subject.Delegator) {
		_ = msg.TermWithReason("event subject target mismatch")
		return fmt.Errorf("event subject target mismatch")
	}
	var payload TaskEventPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		_ = msg.TermWithReason("invalid event payload")
		return err
	}
	switch subject.EventKind {
	case EventKindAccepted:
		if strings.HasPrefix(subject.TaskKey, "msg_") {
			_ = msg.TermWithReason("accepted event must use task id key")
			return fmt.Errorf("accepted event must use task id key")
		}
		row, err := t.tasks.GetByDirectionMessage(ctx, "outbound", env.MessageID)
		if err != nil {
			_ = msg.Nak()
			return err
		}
		if err := validateOutboundEventOwnership(row, subject); err != nil {
			_ = msg.TermWithReason(err.Error())
			return err
		}
		row, err = t.tasks.BindAccepted(ctx, env.MessageID, TaskID(subject.TaskKey), subject.Executor)
		if err != nil {
			_ = msg.Nak()
			return err
		}
	case EventKindRejected:
		if !strings.HasPrefix(subject.TaskKey, "msg_") {
			_ = msg.TermWithReason("pre-accept rejected event must use msg_ key")
			return fmt.Errorf("pre-accept rejected event must use msg_ key")
		}
		row, err := t.tasks.GetByDirectionMessage(ctx, "outbound", env.MessageID)
		if err != nil {
			_ = msg.Nak()
			return err
		}
		if err := validateOutboundEventOwnership(row, subject); err != nil {
			_ = msg.TermWithReason(err.Error())
			return err
		}
		if _, err := t.tasks.RejectBeforeAccepted(ctx, env.MessageID, payload.ClientTaskRef, subject.Executor, payload.Error); err != nil {
			_ = msg.Nak()
			return err
		}
	case EventKindStatus, EventKindResult, EventKindArtifact:
		state := payload.State
		taskErr := payload.Error
		if payload.Result != nil {
			state = payload.Result.State
			taskErr = payload.Result.Error
		}
		if state == "" && subject.EventKind == EventKindResult {
			state = TaskStateCompleted
		}
		revision := env.Revision
		if payload.Revision > 0 {
			revision = payload.Revision
		}
		eventPayload, _ := json.Marshal(payload)
		row, err := t.tasks.GetByDirectionTaskID(ctx, "outbound", TaskID(subject.TaskKey))
		if err != nil {
			_ = msg.Nak()
			return err
		}
		if err := validateOutboundEventOwnership(row, subject); err != nil {
			_ = msg.TermWithReason(err.Error())
			return err
		}
		row, err = t.tasks.ApplyTaskEvent(ctx, "outbound", TaskID(subject.TaskKey), EventRow{TaskID: TaskID(subject.TaskKey), Revision: revision, EventType: subject.EventKind, State: state, PayloadJSON: string(eventPayload)}, state, taskErr)
		if err != nil {
			_ = msg.Nak()
			return err
		}
		if t.eventSink != nil {
			if err := t.eventSink(ctx, row, subject.EventKind, payload); err != nil {
				t.log("[a2a] event delivery failed task=%s kind=%s: %v", subject.TaskKey, subject.EventKind, err)
			}
		}
	default:
		_ = msg.TermWithReason("unsupported event kind")
		return TransportError{Code: ErrorUnsupportedOperation, Message: subject.EventKind}
	}
	return msg.DoubleAck(ctx)
}

func decodeEnvelopeMessage(msg jetstream.Msg) (Subject, Envelope, error) {
	subject, err := ParseSubject(msg.Subject())
	if err != nil {
		return Subject{}, Envelope{}, err
	}
	var env Envelope
	if err := json.Unmarshal(msg.Data(), &env); err != nil {
		return Subject{}, Envelope{}, err
	}
	if err := ValidateEnvelope(env, subject); err != nil {
		return Subject{}, Envelope{}, err
	}
	return subject, env, nil
}

func validateOutboundEventOwnership(row TaskRow, subject Subject) error {
	if row.FromAgent != subject.Delegator {
		return fmt.Errorf("event delegator %s does not match outbound task from_agent %s", subject.Delegator, row.FromAgent)
	}
	if row.ExecutorAgent != "" {
		if row.ExecutorAgent != subject.Executor {
			return fmt.Errorf("event executor %s does not match outbound task executor_agent %s", subject.Executor, row.ExecutorAgent)
		}
		return nil
	}
	if row.ToAgent != subject.Executor {
		return fmt.Errorf("event executor %s does not match outbound task target %s", subject.Executor, row.ToAgent)
	}
	return nil
}

func (t *Transport) recordAndPublishRejected(ctx context.Context, req TaskExecutionRequest, subject Subject, env Envelope, taskErr TaskError) error {
	if taskErr.Code == "" {
		taskErr.Code = ErrorPolicyDenied
	}
	row := TaskRow{TaskID: TaskID("msg_" + string(subject.MessageID)), ClientTaskRef: req.ClientTaskRef, MessageID: subject.MessageID, ContextID: req.ContextID, FromAgent: subject.From, ToAgent: subject.To, ExecutorAgent: subject.To, ChannelRef: req.ChannelRef, SkillID: req.SkillID, State: TaskStateRejected, Revision: 1, Error: taskErr, OriginRequester: req.OriginRequester, OriginRuntimeRef: req.OriginRuntimeRef}
	if _, err := t.tasks.RejectInbound(ctx, row, taskErr); err != nil {
		return err
	}
	return t.publisherFrom(subject.To).PublishRejected(ctx, subject.From, env.MessageID, req.ClientTaskRef, taskErr)
}

func (t *Transport) runAccepted(admission A2AAdmission) {
	timeout := time.Duration(admission.Request.Delivery.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	result, err := t.executor.RunA2ATask(ctx, admission)
	if err != nil {
		result = TaskExecutionResult{TaskID: admission.TaskID, State: TaskStateFailed, Revision: admission.Revision + 1, Error: TaskError{Code: ErrorInternal, Message: err.Error()}, Content: err.Error()}
	}
	if result.TaskID == "" {
		result.TaskID = admission.TaskID
	}
	if result.Revision <= 0 {
		result.Revision = admission.Revision + 1
	}
	pub := t.publisherFrom(admission.Request.To)
	for i, artifact := range result.Artifacts {
		if artifact.ID == "" {
			artifact.ID = fmt.Sprintf("artifact-%d", i+1)
		}
		if err := pub.PublishArtifact(context.Background(), admission.Request.From, result.TaskID, result.Revision+int64(i), artifact); err != nil {
			t.log("[a2a] publish artifact task=%s artifact=%s: %v", result.TaskID, artifact.ID, err)
		}
	}
	var pubErr error
	if IsTerminalState(result.State) {
		pubErr = pub.PublishResult(context.Background(), admission.Request.From, result, admission.Request.MessageID)
	} else {
		pubErr = pub.PublishStatus(context.Background(), admission.Request.From, result.TaskID, result.Revision, result.State, result.Content, result.Error)
	}
	if pubErr != nil {
		t.log("[a2a] publish result task=%s: %v", result.TaskID, pubErr)
	}
}

func (t *Transport) markStarted(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.started[key]; ok {
		return false
	}
	t.started[key] = struct{}{}
	return true
}

func taskRequestFromRow(row TaskRow) TaskExecutionRequest {
	return TaskExecutionRequest{MessageID: row.MessageID, ClientTaskRef: row.ClientTaskRef, ContextID: row.ContextID, From: row.FromAgent, To: row.ToAgent, ChannelID: row.ChannelID, GuildID: row.GuildID, ChannelRef: row.ChannelRef, SkillID: row.SkillID, ResultVisibility: row.ResultVisibility, DiscordTranscriptMode: row.DiscordTranscriptMode, Delivery: DeliveryOptions{DiscordContextJSON: json.RawMessage(row.DiscordContextJSON)}, OriginRequester: row.OriginRequester, OriginRuntimeRef: row.OriginRuntimeRef}
}

func (p *Publisher) checkEventRate(now time.Time) error {
	if p == nil || p.rate == nil {
		return nil
	}
	return p.rate.allow(now)
}

func (r *eventRateLimiter) allow(now time.Time) error {
	if r == nil || r.limit <= 0 {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cutoff := now.Add(-time.Minute)
	kept := r.events[:0]
	for _, event := range r.events {
		if event.After(cutoff) {
			kept = append(kept, event)
		}
	}
	r.events = kept
	if len(r.events) >= r.limit {
		return TransportError{Code: ErrorOverloaded, Message: "A2A event rate quota exceeded"}
	}
	r.events = append(r.events, now)
	return nil
}

func (t *Transport) log(format string, args ...any) {
	if t != nil && t.logf != nil {
		t.logf(format, args...)
		return
	}
	log.Printf(format, args...)
}
