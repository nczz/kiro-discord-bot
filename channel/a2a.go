package channel

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nczz/kiro-discord-bot/a2a"
)

var _ a2a.Executor = (*Manager)(nil)

func (m *Manager) AdmitA2ATask(ctx context.Context, req a2a.TaskExecutionRequest) (a2a.A2AAdmissionResult, error) {
	if m == nil || !m.a2aConfig.Enabled() {
		return rejectedA2AAdmission(req, a2a.ErrorChannelNotEnabled, "A2A ingress is disabled"), nil
	}
	if err := validateA2AExecutionRequest(m.a2aConfig.AgentID, req); err != nil {
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
	if err := policy.ValidateInbound(req.From, req.SkillID); err != nil {
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
		return acceptedA2AAdmission(row, admission, m.a2aConfig.AgentID), nil
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
		ExecutorAgent:         m.a2aConfig.AgentID,
		ChannelID:             policy.ChannelID,
		GuildID:               policy.GuildID,
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
		ExecutorAgent: m.a2aConfig.AgentID,
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
	if !ok {
		return a2a.TaskExecutionResult{TaskID: admitted.TaskID, State: a2a.TaskStateRejected, Error: a2a.TaskError{Code: a2a.ErrorTaskNotFound, Message: "A2A admission reservation not found"}}, nil
	}
	defer m.releaseA2AAdmission(admitted)

	resultCh := make(chan a2a.TaskExecutionResult, 1)
	job := &Job{
		ChannelID:              admitted.Request.ChannelID,
		GuildID:                admitted.Request.GuildID,
		Prompt:                 buildA2APrompt(admitted),
		UserID:                 string(admitted.Request.From),
		Username:               "A2A " + string(admitted.Request.From),
		Source:                 "a2a",
		DeliveryMode:           DeliveryInline,
		DisableBotEgress:       admitted.Request.ResultVisibility == "" || admitted.Request.ResultVisibility == "proxy",
		RemoteA2A:              true,
		AllowRemoteMemoryWrite: m.remoteMemoryWriteAllowed(admitted.Request.ChannelID),
		A2AResult:              resultCh,
		A2ATaskID:              admitted.TaskID,
		A2ARevision:            admitted.Revision + 1,
		BotToolsTargetID:       admitted.Request.Delivery.DiscordReplyThreadID,
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
		_ = m.a2aTasks.AppendEvent(ctx, a2a.EventRow{TaskID: admitted.TaskID, Revision: result.Revision, EventType: "status", State: result.State, PayloadJSON: string(payload)})
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
	sb.WriteString("Discord egress is disabled unless this channel policy explicitly permits transparent result delivery. Return the final result as text; do not ask bot-tools to post to Discord.\n\n")
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
