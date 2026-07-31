package channel

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nczz/kiro-discord-bot/a2a"
	"github.com/nczz/kiro-discord-bot/acp"
	"github.com/nczz/kiro-discord-bot/audit"
	"github.com/nczz/kiro-discord-bot/internal/botegress"
)

var _ a2a.Executor = (*Manager)(nil)

func (m *Manager) AdmitA2ATask(ctx context.Context, req a2a.TaskExecutionRequest) (a2a.A2AAdmissionResult, error) {
	if m == nil || !m.a2aConfig.Enabled() {
		return rejectedA2AAdmission(req, a2a.ErrorChannelNotEnabled, "A2A ingress is disabled"), nil
	}
	if err := validateA2AExecutionRequest("", req); err != nil {
		return rejectedA2AAdmission(req, a2a.ErrorInvalidEnvelope, err.Error()), nil
	}
	if m.a2aPolicies == nil {
		return rejectedA2AAdmission(req, a2a.ErrorChannelNotEnabled, "A2A policy store is unavailable"), nil
	}
	if m.a2aTasks == nil {
		return rejectedA2AAdmission(req, a2a.ErrorStoreError, "A2A task store is unavailable"), nil
	}

	policy, err := m.a2aPolicies.GetEnabledByChannelRef(ctx, req.ChannelRef)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return rejectedA2AAdmission(req, a2a.ErrorChannelNotEnabled, "channel_ref is not enabled"), nil
		}
		return rejectedA2AAdmission(req, a2a.ErrorStoreError, err.Error()), nil
	}
	executor := effectiveA2AExecutorAgent(m.a2aConfig, policy)
	if err := validateA2ATarget(m.a2aConfig, policy, req); err != nil {
		return rejectedA2AAdmission(req, a2a.ErrorInvalidEnvelope, err.Error()), nil
	}
	if err := policy.ValidateInboundRuntime(req.From, req.SkillID); err != nil {
		return rejectedA2AAdmission(req, codeFromA2AError(err, a2a.ErrorPolicyDenied), err.Error()), nil
	}
	if err := validateA2ADeliveryAgainstPolicy(req, policy); err != nil {
		return rejectedA2AAdmission(req, a2a.ErrorPolicyDenied, err.Error()), nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if row, err := m.a2aTasks.GetByDirectionMessage(ctx, "inbound", req.MessageID); err == nil && row.LocalID != "" {
		admission, ok := m.a2aAdmissions[row.LocalID]
		if !ok {
			admission = admissionFromRow(req, row)
		}
		return acceptedA2AAdmission(row, admission, executor), nil
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return rejectedA2AAdmission(req, a2a.ErrorStoreError, err.Error()), nil
	}
	worker, err := m.ensureWorkerForA2A(policy.ChannelID)
	if err != nil {
		return rejectedA2AAdmission(req, a2a.ErrorInternal, err.Error()), nil
	}
	if cap := effectiveA2AInboundCap(policy.MaxConcurrent, m.a2aConfig.MaxInboundTasksPerChannel); cap > 0 && m.a2aInboundOpen[policy.ChannelID] >= cap {
		return rejectedA2AAdmission(req, a2a.ErrorOverloaded, fmt.Sprintf("A2A inbound quota exceeded for channel_ref %s", req.ChannelRef)), nil
	}
	if worker.QueueLen() >= m.queueBufSize && m.queueBufSize > 0 {
		return rejectedA2AAdmission(req, a2a.ErrorOverloaded, fmt.Sprintf("worker queue full (%d jobs pending)", worker.QueueLen())), nil
	}

	row, err := m.a2aTasks.AdmitInbound(ctx, a2a.TaskRow{
		TaskID:                a2aTaskID(req),
		ClientTaskRef:         strings.TrimSpace(req.ClientTaskRef),
		MessageID:             req.MessageID,
		ContextID:             strings.TrimSpace(req.ContextID),
		FromAgent:             req.From,
		ToAgent:               req.To,
		ExecutorAgent:         executor,
		ChannelID:             policy.ChannelID,
		GuildID:               policy.GuildID,
		OriginRequester:       req.OriginRequester,
		ChannelRef:            policy.ChannelRef,
		SkillID:               a2a.SkillSlug(req.SkillID),
		State:                 a2a.TaskStateSubmitted,
		Revision:              1,
		ResultVisibility:      policy.ResultVisibility,
		DiscordTranscriptMode: policy.DiscordTranscriptMode,
		DiscordContextJSON:    discordContextJSON(req),
		CreatedAt:             firstTime(req.CreatedAt, time.Now().UTC()),
		ExpiresAt:             req.ExpiresAt,
	})
	if err != nil {
		return rejectedA2AAdmission(req, a2a.ErrorStoreError, err.Error()), nil
	}

	admission := a2a.A2AAdmission{
		AdmissionKey: row.LocalID,
		TaskID:       row.TaskID,
		State:        row.State,
		Revision:     row.Revision,
		Request:      req,
	}
	admission.Request.ChannelID = policy.ChannelID
	admission.Request.GuildID = policy.GuildID
	admission.Request.ChannelRef = policy.ChannelRef
	admission.Request.SkillID = row.SkillID
	admission.Request.ResultVisibility = policy.ResultVisibility
	admission.Request.DiscordTranscriptMode = policy.DiscordTranscriptMode
	m.a2aAdmissions[row.LocalID] = admission
	m.a2aInboundOpen[policy.ChannelID]++

	return a2a.A2AAdmissionResult{
		Accepted:      true,
		AdmissionKey:  row.LocalID,
		TaskID:        row.TaskID,
		State:         row.State,
		Revision:      row.Revision,
		ExecutorAgent: executor,
		ChannelID:     policy.ChannelID,
		GuildID:       policy.GuildID,
		ChannelRef:    policy.ChannelRef,
		SkillID:       row.SkillID,
		Admission:     admission,
	}, nil
}

func admissionFromRow(req a2a.TaskExecutionRequest, row a2a.TaskRow) a2a.A2AAdmission {
	admission := a2a.A2AAdmission{
		AdmissionKey: row.LocalID,
		TaskID:       row.TaskID,
		State:        row.State,
		Revision:     row.Revision,
		Request:      req,
	}
	admission.Request.ChannelID = row.ChannelID
	admission.Request.GuildID = row.GuildID
	admission.Request.ChannelRef = row.ChannelRef
	admission.Request.SkillID = row.SkillID
	admission.Request.ResultVisibility = row.ResultVisibility
	admission.Request.DiscordTranscriptMode = row.DiscordTranscriptMode
	return admission
}

func acceptedA2AAdmission(row a2a.TaskRow, admission a2a.A2AAdmission, executor a2a.AgentID) a2a.A2AAdmissionResult {
	return a2a.A2AAdmissionResult{
		Accepted:      true,
		AdmissionKey:  row.LocalID,
		TaskID:        row.TaskID,
		State:         row.State,
		Revision:      row.Revision,
		ExecutorAgent: executor,
		ChannelID:     row.ChannelID,
		GuildID:       row.GuildID,
		ChannelRef:    row.ChannelRef,
		SkillID:       row.SkillID,
		Admission:     admission,
	}
}

func (m *Manager) RunA2ATask(ctx context.Context, admitted a2a.A2AAdmission) (a2a.TaskExecutionResult, error) {
	if m == nil {
		return a2a.TaskExecutionResult{}, fmt.Errorf("manager is nil")
	}
	m.mu.Lock()
	stored, ok := m.a2aAdmissions[admitted.AdmissionKey]
	if ok {
		admitted = stored
	}
	m.mu.Unlock()
	if !ok && admitted.Continuation == nil {
		return a2a.TaskExecutionResult{TaskID: admitted.TaskID, State: a2a.TaskStateRejected, Error: a2a.TaskError{Code: a2a.ErrorTaskNotFound, Message: "A2A admission reservation not found"}}, nil
	}
	if ok {
		defer m.releaseA2AAdmission(admitted)
	}
	if m.discord == nil {
		return m.recordA2ATerminal(ctx, admitted, a2a.TaskExecutionResult{TaskID: admitted.TaskID, State: a2a.TaskStateFailed, Error: a2a.TaskError{Code: a2a.ErrorInternal, Message: "Discord session is unavailable for A2A executor conversation"}}), nil
	}

	requesterID, requesterName := a2aRequesterIdentity(admitted.Request)

	resultCh := make(chan a2a.TaskExecutionResult, 1)
	threadID := a2aConversationThreadID(admitted)
	job := &Job{
		ChannelID:              admitted.Request.ChannelID,
		GuildID:                admitted.Request.GuildID,
		Prompt:                 buildA2APrompt(admitted),
		Session:                m.discord,
		UserID:                 requesterID,
		Username:               requesterName,
		MessageID:              string(admitted.Request.MessageID),
		ThreadID:               threadID,
		Source:                 "a2a",
		DeliveryMode:           DeliveryThread,
		DisableBotEgress:       admitted.Request.ResultVisibility == "" || admitted.Request.ResultVisibility == "proxy",
		RemoteA2A:              true,
		AllowRemoteMemoryWrite: m.remoteMemoryWriteAllowed(admitted.Request.ChannelID),
		A2ADelegationDepth:     admitted.Request.Delivery.MaxDelegationDepth,
		A2AResult:              resultCh,
		A2ATaskID:              admitted.TaskID,
		A2ARevision:            admitted.Revision + 1,
		OnThreadReady: func(threadID string) {
			m.bindA2AConversationThread(ctx, admitted, threadID)
		},
	}
	if err := m.enqueueA2AJob(job); err != nil {
		return m.recordA2ATerminal(ctx, admitted, a2a.TaskExecutionResult{TaskID: admitted.TaskID, State: a2a.TaskStateFailed, Error: a2a.TaskError{Code: a2a.ErrorOverloaded, Message: err.Error()}}), nil
	}

	select {
	case result := <-resultCh:
		return m.recordA2AResult(ctx, admitted, result), nil
	case <-ctx.Done():
		m.Cancel(admitted.Request.ChannelID)
		return m.recordA2ATerminal(ctx, admitted, a2a.TaskExecutionResult{TaskID: admitted.TaskID, State: a2a.TaskStateCanceled, Error: a2a.TaskError{Code: a2a.ErrorCode("canceled"), Message: ctx.Err().Error()}}), nil
	}
}

func (m *Manager) enqueueA2AJob(job *Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	worker, err := m.ensureWorkerForA2A(job.ChannelID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(job.DisplayCWD) == "" {
		job.DisplayCWD = m.CWDPath(job.ChannelID)
	}
	if err := worker.Enqueue(job); err != nil {
		return fmt.Errorf("queue full (%d jobs pending)", worker.QueueLen())
	}
	m.recordJobEnqueued(job, worker.QueueLen(), false)
	m.channelLastActivity[job.ChannelID] = time.Now()
	return nil
}

func (m *Manager) ensureWorkerForA2A(channelID string) (*Worker, error) {
	if w, ok := m.workers[channelID]; ok {
		return w, nil
	}
	return m.ensureWorker(channelID)
}

func (m *Manager) releaseA2AAdmission(admitted a2a.A2AAdmission) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.a2aAdmissions, admitted.AdmissionKey)
	channelID := strings.TrimSpace(admitted.Request.ChannelID)
	if channelID == "" {
		return
	}
	if m.a2aInboundOpen[channelID] > 1 {
		m.a2aInboundOpen[channelID]--
	} else {
		delete(m.a2aInboundOpen, channelID)
	}
}

func (m *Manager) remoteMemoryWriteAllowed(channelID string) bool {
	if m == nil || m.a2aPolicies == nil {
		return false
	}
	policy, err := m.a2aPolicies.Get(context.Background(), m.guildID, channelID)
	if err != nil {
		return false
	}
	return policy.RemoteToolPolicy.AllowMemoryWrite
}

func a2aConversationThreadID(admitted a2a.A2AAdmission) string {
	if admitted.Continuation == nil || len(admitted.Request.Delivery.DiscordContextJSON) == 0 {
		return ""
	}
	var dc a2a.DiscordContext
	if err := json.Unmarshal(admitted.Request.Delivery.DiscordContextJSON, &dc); err != nil {
		return ""
	}
	if strings.TrimSpace(dc.GuildID) != strings.TrimSpace(admitted.Request.GuildID) {
		return ""
	}
	if strings.TrimSpace(dc.ChannelID) != strings.TrimSpace(admitted.Request.ChannelID) {
		return ""
	}
	return strings.TrimSpace(dc.ThreadID)
}

func (m *Manager) bindA2AConversationThread(ctx context.Context, admitted a2a.A2AAdmission, threadID string) {
	if m == nil || m.a2aTasks == nil || strings.TrimSpace(threadID) == "" {
		return
	}
	raw, err := json.Marshal(a2a.DiscordContext{
		GuildID:   admitted.Request.GuildID,
		ChannelID: admitted.Request.ChannelID,
		ThreadID:  threadID,
	})
	if err != nil {
		return
	}
	if _, err := m.a2aTasks.SetDiscordContext(ctx, admitted.AdmissionKey, string(raw)); err != nil {
		log.Printf("[a2a] bind conversation thread failed task=%s thread=%s err=%v", admitted.TaskID, threadID, err)
	}
}

func (m *Manager) recordA2AResult(ctx context.Context, admitted a2a.A2AAdmission, result a2a.TaskExecutionResult) a2a.TaskExecutionResult {
	if result.TaskID == "" {
		result.TaskID = admitted.TaskID
	}
	if result.Revision == 0 {
		result.Revision = admitted.Revision + 1
	}
	if a2a.IsTerminalState(result.State) {
		return m.recordA2ATerminal(ctx, admitted, result)
	}
	if m.a2aTasks != nil {
		payload, _ := json.Marshal(result)
		if row, err := m.a2aTasks.ApplyTaskEvent(ctx, "inbound", admitted.TaskID, a2a.EventRow{TaskID: admitted.TaskID, Revision: result.Revision, EventType: "status", State: result.State, PayloadJSON: string(payload)}, result.State, result.Error); err == nil {
			result.Revision = row.Revision
		}
	}
	return result
}

func (m *Manager) recordA2ATerminal(ctx context.Context, admitted a2a.A2AAdmission, result a2a.TaskExecutionResult) a2a.TaskExecutionResult {
	if result.TaskID == "" {
		result.TaskID = admitted.TaskID
	}
	if result.State == "" {
		result.State = a2a.TaskStateFailed
	}
	if !a2a.IsTerminalState(result.State) {
		result.State = a2a.TaskStateFailed
	}
	if m.a2aTasks == nil {
		return result
	}
	row, err := m.a2aTasks.MarkTerminal(ctx, admitted.AdmissionKey, result.State, result.Error)
	if err == nil {
		result.Revision = row.Revision
		return result
	}
	if result.Error.Code == "" {
		result.Error = a2a.TaskError{Code: a2a.ErrorStoreError, Message: err.Error()}
	}
	return result
}

func rejectedA2AAdmission(req a2a.TaskExecutionRequest, code a2a.ErrorCode, message string) a2a.A2AAdmissionResult {
	return a2a.A2AAdmissionResult{Accepted: false, State: a2a.TaskStateRejected, ExecutorAgent: req.To, ChannelRef: req.ChannelRef, SkillID: req.SkillID, Error: a2a.TaskError{Code: code, Message: message}}
}

func validateA2AExecutionRequest(local a2a.AgentID, req a2a.TaskExecutionRequest) error {
	if err := a2a.ValidateMessageID(req.MessageID); err != nil {
		return err
	}
	if err := a2a.ValidateAgentID(req.From); err != nil {
		return fmt.Errorf("from: %w", err)
	}
	if err := a2a.ValidateAgentID(req.To); err != nil {
		return fmt.Errorf("to: %w", err)
	}
	if local != "" && req.To != local {
		return fmt.Errorf("request target %s does not match local agent %s", req.To, local)
	}
	if strings.TrimSpace(req.ChannelRef) == "" {
		return fmt.Errorf("channel_ref is required")
	}
	if strings.TrimSpace(req.SkillID) == "" {
		return fmt.Errorf("skill_id is required")
	}
	return nil
}

func effectiveA2AExecutorAgent(cfg a2a.Config, policy a2a.ChannelA2APolicy) a2a.AgentID {
	if cfg.RuntimeIDMode.UsesRuntimeIDs() && strings.TrimSpace(policy.RuntimeAgentID) != "" {
		return a2a.AgentID(strings.TrimSpace(policy.RuntimeAgentID))
	}
	return cfg.AgentID
}

func validateA2ATarget(cfg a2a.Config, policy a2a.ChannelA2APolicy, req a2a.TaskExecutionRequest) error {
	mode, err := a2a.NormalizeRuntimeIDMode(cfg.RuntimeIDMode.String())
	if err != nil {
		return err
	}
	runtimeID := a2a.AgentID(strings.TrimSpace(policy.RuntimeAgentID))
	switch mode {
	case a2a.RuntimeIDModeRuntime:
		if runtimeID == "" {
			return fmt.Errorf("runtime_agent_id is required for runtime mode")
		}
		if req.To != runtimeID {
			return fmt.Errorf("request target %s does not match runtime %s", req.To, runtimeID)
		}
	case a2a.RuntimeIDModeDual:
		if req.To != cfg.AgentID && (runtimeID == "" || req.To != runtimeID) {
			return fmt.Errorf("request target %s does not match local agent %s or runtime %s", req.To, cfg.AgentID, runtimeID)
		}
	default:
		if req.To != cfg.AgentID {
			return fmt.Errorf("request target %s does not match local agent %s", req.To, cfg.AgentID)
		}
	}
	return nil
}

func validateA2ADeliveryAgainstPolicy(req a2a.TaskExecutionRequest, policy a2a.ChannelA2APolicy) error {
	if req.ResultVisibility != "" && req.ResultVisibility != policy.ResultVisibility {
		return fmt.Errorf("result_visibility %q is not allowed by channel policy", req.ResultVisibility)
	}
	if req.DiscordTranscriptMode != "" && req.DiscordTranscriptMode != policy.DiscordTranscriptMode {
		return fmt.Errorf("discord_transcript_mode %q is not allowed by channel policy", req.DiscordTranscriptMode)
	}
	if req.Delivery.ShareDiscordContext && !policy.ShareDiscordContext {
		return fmt.Errorf("shared Discord context is not allowed by channel policy")
	}
	if req.Delivery.DiscordContext != nil {
		dc := req.Delivery.DiscordContext
		if strings.TrimSpace(dc.GuildID) != "" && strings.TrimSpace(dc.GuildID) != policy.GuildID {
			return fmt.Errorf("Discord context guild %s is not allowed by channel policy", dc.GuildID)
		}
		if strings.TrimSpace(dc.ChannelID) != "" && strings.TrimSpace(dc.ChannelID) != policy.ChannelID {
			return fmt.Errorf("Discord context channel %s is not allowed by channel policy", dc.ChannelID)
		}
	}
	if req.Delivery.CoPresentFrom != "" && !agentAllowed(policy.CoPresentFrom, req.Delivery.CoPresentFrom) {
		return fmt.Errorf("co-present sender %s is not allowed", req.Delivery.CoPresentFrom)
	}
	return nil
}

func agentAllowed(list []string, id a2a.AgentID) bool {
	for _, item := range list {
		item = strings.TrimSpace(item)
		if item == "*" || item == string(id) {
			return true
		}
	}
	return false
}

func effectiveA2AInboundCap(policyCap, configCap int) int {
	if policyCap <= 0 {
		return configCap
	}
	if configCap <= 0 || policyCap < configCap {
		return policyCap
	}
	return configCap
}

func a2aTaskID(req a2a.TaskExecutionRequest) a2a.TaskID {
	sum := sha256.Sum256([]byte(string(req.From) + ":" + string(req.To) + ":" + string(req.MessageID)))
	return a2a.TaskID("task_" + hex.EncodeToString(sum[:12]))
}
func a2aRequesterIdentity(req a2a.TaskExecutionRequest) (string, string) {
	requesterID := ""
	requesterName := ""
	if strings.TrimSpace(req.OriginRequester.DiscordGuildID) == strings.TrimSpace(req.GuildID) {
		requesterID = strings.TrimSpace(req.OriginRequester.DiscordUserID)
		requesterName = strings.TrimSpace(req.OriginRequester.DiscordUsername)
	}
	if requesterID == "" {
		requesterID = string(req.From)
	}
	if requesterName == "" {
		requesterName = "A2A " + string(req.From)
	}
	return requesterID, requesterName
}

func discordContextJSON(req a2a.TaskExecutionRequest) string {
	if len(req.Delivery.DiscordContextJSON) > 0 {
		return string(req.Delivery.DiscordContextJSON)
	}
	if req.Delivery.DiscordContext != nil {
		raw, _ := json.Marshal(req.Delivery.DiscordContext)
		return string(raw)
	}
	return ""
}

func firstTime(value, fallback time.Time) time.Time {
	if value.IsZero() {
		return fallback
	}
	return value
}

func buildA2APrompt(admitted a2a.A2AAdmission) string {
	var sb strings.Builder
	sb.WriteString("[A2A remote task]\n")
	sb.WriteString("from_agent=" + string(admitted.Request.From) + "\n")
	sb.WriteString("to_agent=" + string(admitted.Request.To) + "\n")
	sb.WriteString("task_id=" + string(admitted.TaskID) + "\n")
	sb.WriteString("channel_ref=" + admitted.Request.ChannelRef + "\n")
	sb.WriteString("skill_id=" + admitted.Request.SkillID + "\n")
	sb.WriteString("result_visibility=" + admitted.Request.ResultVisibility + "\n")
	sb.WriteString("This executor bot owns the Discord conversation for this delegated task. Return ordinary replies as final text; do not call bot-tools for the final answer or duplicate Discord posts. Separate bot-tools egress remains policy-gated.\n\n")
	if admitted.Continuation != nil {
		sb.WriteString("[A2A continuation]\n")
		sb.WriteString("control_kind=" + admitted.Continuation.Kind + "\n")
		if reason := strings.TrimSpace(admitted.Continuation.Reason); reason != "" {
			sb.WriteString("reason=" + reason + "\n")
		}
		sb.WriteString("Use the continuation payload to resume this same task. Do not restart from the original task unless needed for context; produce the next status or final result for the same task_id.\n\n")
		if len(admitted.Continuation.Payload) > 0 {
			sb.WriteString("Canonical A2A continuation payload JSON:\n")
			sb.Write(admitted.Continuation.Payload)
			sb.WriteString("\n\n")
		}
	}
	if summary := strings.TrimSpace(admitted.Request.UserVisibleSummary); summary != "" {
		sb.WriteString(summary + "\n\n")
	}
	if len(admitted.Request.Payload) > 0 {
		sb.WriteString("Canonical A2A payload JSON:\n")
		sb.Write(admitted.Request.Payload)
		sb.WriteString("\n")
	}
	return sb.String()
}
func (m *Manager) deliverA2AEvent(ctx context.Context, row a2a.TaskRow, kind string, payload a2a.TaskEventPayload) error {
	if m == nil {
		return nil
	}
	targetID := a2aDeliveryTarget(row)
	if targetID == "" {
		targetID = row.ChannelID
	}
	delivered := false
	mode := row.DiscordTranscriptMode
	if mode == "" {
		mode = "delegator"
	}
	deliverText := kind == a2a.EventKindResult || mode == "mirror" || mode == "co_present"
	if deliverText {
		content := a2aDeliveryContent(row, kind, payload)
		if strings.TrimSpace(content) != "" {
			if _, err := botegress.WritePending(m.dataDir, botegress.Action{Action: botegress.ActionSendMessage, ChannelID: targetID, Content: content}); err != nil {
				return err
			}
			delivered = true
			m.recordA2ADeliveryAudit(row, kind, targetID, payload, 0, "")
		}
	}
	artifacts := a2aArtifactsForDelivery(kind, payload)
	for _, artifact := range artifacts {
		if err := m.deliverA2AArtifact(ctx, row, targetID, kind, payload, artifact); err != nil {
			m.recordA2ADeliveryAudit(row, kind, targetID, payload, 0, err.Error())
			return err
		}
		delivered = true
	}
	if delivered && m.safeEgressDrain != nil {
		m.safeEgressDrain(targetID)
	}
	return nil
}

func a2aDeliveryTarget(row a2a.TaskRow) string {
	var dc a2a.DiscordContext
	if strings.TrimSpace(row.DiscordContextJSON) == "" {
		return row.ChannelID
	}
	if err := json.Unmarshal([]byte(row.DiscordContextJSON), &dc); err != nil {
		return row.ChannelID
	}
	if strings.TrimSpace(dc.ThreadID) != "" {
		return strings.TrimSpace(dc.ThreadID)
	}
	if strings.TrimSpace(dc.ChannelID) != "" {
		return strings.TrimSpace(dc.ChannelID)
	}
	return row.ChannelID
}

func a2aDeliveryContent(row a2a.TaskRow, kind string, payload a2a.TaskEventPayload) string {
	content := strings.TrimSpace(payload.Content)
	if payload.Result != nil {
		content = strings.TrimSpace(payload.Result.Content)
	}
	if content == "" && payload.Error.Message != "" {
		content = payload.Error.Message
	}
	if content == "" {
		return ""
	}
	if payload.Result != nil {
		content = AppendMetricsFooter(content, a2aResultMetrics(payload.Result.Metrics))
	}
	prefix := fmt.Sprintf("A2A %s from %s", kind, row.ToAgent)
	if row.SkillID != "" {
		prefix += " (" + row.SkillID + ")"
	}
	return prefix + ":\n" + content
}

func a2aResultMetrics(metrics map[string]float64) acp.TurnMetrics {
	if len(metrics) == 0 {
		return acp.TurnMetrics{}
	}
	out := acp.TurnMetrics{
		ContextUsage:   metrics["context_usage"],
		TurnDurationMs: int64(metrics["turn_duration"]),
	}
	if credits := metrics["credits"]; credits > 0 {
		out.MeteringUsage = append(out.MeteringUsage, acp.MeteringItem{Value: credits, Unit: "credit"})
	} else if usd := metrics["usd"]; usd > 0 {
		out.MeteringUsage = append(out.MeteringUsage, acp.MeteringItem{Value: usd, Unit: "USD"})
	}
	return out
}

func a2aArtifactsForDelivery(kind string, payload a2a.TaskEventPayload) []a2a.TaskExecutionArtifact {
	if kind == a2a.EventKindArtifact && payload.Artifact != nil {
		return []a2a.TaskExecutionArtifact{*payload.Artifact}
	}
	if payload.Result != nil && len(payload.Result.Artifacts) > 0 {
		return payload.Result.Artifacts
	}
	return nil
}

func (m *Manager) deliverA2AArtifact(ctx context.Context, row a2a.TaskRow, targetID, kind string, payload a2a.TaskEventPayload, artifact a2a.TaskExecutionArtifact) error {
	if m.a2aObjects == nil {
		return fmt.Errorf("%s: A2A object store is unavailable", a2a.ErrorStoreError)
	}
	if strings.TrimSpace(artifact.ID) == "" {
		return fmt.Errorf("%s: artifact id is required", a2a.ErrorArtifactFetchFailed)
	}
	content, ref, err := m.a2aObjects.FetchObject(ctx, artifact.ID)
	if err != nil {
		return fmt.Errorf("%s: %v", a2a.ErrorArtifactFetchFailed, err)
	}
	if artifact.Digest != "" && artifact.Digest != ref.Digest {
		return fmt.Errorf("%s: artifact digest mismatch", a2a.ErrorArtifactFetchFailed)
	}
	if artifact.SizeBytes > 0 && artifact.SizeBytes != ref.Size {
		return fmt.Errorf("%s: artifact size mismatch", a2a.ErrorArtifactFetchFailed)
	}
	if err := m.validateA2AArtifactPolicy(ctx, row, artifact, ref); err != nil {
		return err
	}
	name := strings.TrimSpace(artifact.Name)
	if name == "" {
		name = artifact.ID
	}
	dir := filepath.Join(m.dataDir, "egress", "incoming", "a2a-"+string(row.TaskID)+"-"+artifact.ID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	filePath := filepath.Join(dir, safeA2AArtifactName(name))
	if err := os.WriteFile(filePath, content, 0600); err != nil {
		return err
	}
	caption := fmt.Sprintf("A2A artifact from %s: %s", row.ToAgent, safeA2AArtifactName(name))
	if _, err := botegress.WritePending(m.dataDir, botegress.Action{Action: botegress.ActionSendFile, ChannelID: targetID, FilePath: filePath, Content: caption, RemoveFileAfterSend: true}); err != nil {
		_ = os.Remove(filePath)
		return err
	}
	m.recordA2ADeliveryAudit(row, kind, targetID, payload, 1, "")
	return nil
}
func (m *Manager) validateA2AArtifactPolicy(ctx context.Context, row a2a.TaskRow, artifact a2a.TaskExecutionArtifact, ref a2a.ObjectRef) error {
	if m == nil || m.a2aPolicies == nil {
		return nil
	}
	policy, err := m.a2aPolicies.Get(ctx, row.GuildID, row.ChannelID)
	if err != nil {
		return nil
	}
	if !policy.DelegateMedia.AllowObjectRefs {
		return fmt.Errorf("%s: object artifact refs are not allowed", a2a.ErrorUnsupportedMediaType)
	}
	size := ref.Size
	if artifact.SizeBytes > 0 {
		size = artifact.SizeBytes
	}
	if policy.DelegateMedia.MaxBytes > 0 && size > policy.DelegateMedia.MaxBytes {
		return fmt.Errorf("%s: artifact exceeds media policy", a2a.ErrorPayloadTooLarge)
	}
	mediaType := strings.TrimSpace(artifact.MediaType)
	if mediaType == "" {
		mediaType = ref.MediaType
	}
	if len(policy.DelegateMedia.AllowedMIMETypes) == 0 {
		return nil
	}
	for _, allowed := range policy.DelegateMedia.AllowedMIMETypes {
		if strings.EqualFold(strings.TrimSpace(allowed), mediaType) {
			return nil
		}
	}
	return fmt.Errorf("%s: artifact media type %s is not allowed", a2a.ErrorUnsupportedMediaType, mediaType)
}

func safeA2AArtifactName(name string) string {
	base := filepath.Base(strings.TrimSpace(name))
	if base == "." || base == string(filepath.Separator) || base == "" {
		return "artifact.bin"
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '.', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, base)
}

func (m *Manager) recordA2ADeliveryAudit(row a2a.TaskRow, kind string, targetID string, payload a2a.TaskEventPayload, artifactCount int, errText string) {
	if m == nil || m.audit == nil {
		return
	}
	if payload.Result != nil && artifactCount == 0 {
		artifactCount = len(payload.Result.Artifacts)
	}
	metadata := map[string]any{
		"task_id":                  string(row.TaskID),
		"client_task_ref":          row.ClientTaskRef,
		"message_id":               string(row.MessageID),
		"context_id":               row.ContextID,
		"from_agent":               string(row.FromAgent),
		"to_agent":                 string(row.ToAgent),
		"executor_agent":           string(row.ExecutorAgent),
		"channel_id":               row.ChannelID,
		"guild_id":                 row.GuildID,
		"channel_ref":              row.ChannelRef,
		"skill_id":                 row.SkillID,
		"state":                    string(row.State),
		"revision":                 row.Revision,
		"result_visibility":        row.ResultVisibility,
		"discord_transcript_mode":  row.DiscordTranscriptMode,
		"discord_message_id":       targetID,
		"transcript_delivery_kind": kind,
		"source_event_revision":    payload.Revision,
		"error_code":               string(payload.Error.Code),
		"artifact_count":           artifactCount,
	}
	m.audit.RecordBotEvent(audit.BotEvent{
		Type:      a2a.AuditTranscriptPosted,
		GuildID:   row.GuildID,
		ChannelID: targetID,
		TargetID:  row.ChannelID,
		Command:   "a2a",
		Source:    "a2a",
		Status:    statusFromError(errText),
		Error:     errText,
		Metadata:  metadata,
	})
	if kind == a2a.EventKindResult {
		m.audit.RecordBotEvent(audit.BotEvent{
			Type:      a2a.AuditResultDelivered,
			GuildID:   row.GuildID,
			ChannelID: targetID,
			TargetID:  row.ChannelID,
			Command:   "a2a",
			Source:    "a2a",
			Status:    statusFromError(errText),
			Error:     errText,
			Metadata:  metadata,
		})
	}
}

func statusFromError(errText string) string {
	if strings.TrimSpace(errText) == "" {
		return "success"
	}
	return "error"
}

func codeFromA2AError(err error, fallback a2a.ErrorCode) a2a.ErrorCode {
	if err == nil {
		return fallback
	}
	text := err.Error()
	for _, code := range []a2a.ErrorCode{a2a.ErrorChannelNotEnabled, a2a.ErrorSenderNotAllowed, a2a.ErrorSkillNotAllowed, a2a.ErrorUnknownSkill, a2a.ErrorUnauthorizedSender} {
		if strings.Contains(text, string(code)) {
			return code
		}
	}
	return fallback
}
