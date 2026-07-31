package a2a

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

const (
	RuntimeCutoverSeverityBlocker = "blocker"
	RuntimeCutoverSeverityWarning = "warning"
)

type RuntimeCutoverReport struct {
	Ready              bool                  `json:"ready"`
	Mode               string                `json:"mode"`
	BotAgentID         string                `json:"botAgentId,omitempty"`
	GuildID            string                `json:"guildId,omitempty"`
	PolicyCount        int                   `json:"policyCount"`
	EnabledPolicyCount int                   `json:"enabledPolicyCount"`
	RuntimePolicyCount int                   `json:"runtimePolicyCount"`
	DiscoverableCount  int                   `json:"discoverableCount"`
	RuntimeAgentIDs    []string              `json:"runtimeAgentIds,omitempty"`
	ChannelRefs        []string              `json:"channelRefs,omitempty"`
	BlockerCount       int                   `json:"blockerCount"`
	WarningCount       int                   `json:"warningCount"`
	Issues             []RuntimeCutoverIssue `json:"issues,omitempty"`
}

type RuntimeCutoverIssue struct {
	Severity             string `json:"severity"`
	Code                 string `json:"code"`
	Message              string `json:"message"`
	GuildID              string `json:"guildId,omitempty"`
	ChannelID            string `json:"channelId,omitempty"`
	ChannelRef           string `json:"channelRef,omitempty"`
	RuntimeAgentID       string `json:"runtimeAgentId,omitempty"`
	TargetRuntimeAgentID string `json:"targetRuntimeAgentId,omitempty"`
}

func (s *SQLitePolicyStore) ListByGuild(ctx context.Context, guildID string) ([]ChannelA2APolicy, error) {
	guildID = strings.TrimSpace(guildID)
	if guildID == "" {
		return nil, fmt.Errorf("guild_id is required")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT guild_id, channel_id FROM channel_a2a_policy WHERE guild_id=? ORDER BY enabled DESC, channel_ref, channel_id`, guildID)
	if err != nil {
		return nil, err
	}
	var keys []struct {
		guildID   string
		channelID string
	}
	for rows.Next() {
		var key struct {
			guildID   string
			channelID string
		}
		if err := rows.Scan(&key.guildID, &key.channelID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	policies := make([]ChannelA2APolicy, 0, len(keys))
	for _, key := range keys {
		policy, err := s.Get(ctx, key.guildID, key.channelID)
		if err != nil {
			return nil, err
		}
		policies = append(policies, policy)
	}
	return policies, nil
}

func (s *SQLitePolicyStore) RuntimeCutoverReadiness(ctx context.Context, cfg Config, guildID string) (RuntimeCutoverReport, error) {
	policies, err := s.ListByGuild(ctx, guildID)
	if err != nil {
		return RuntimeCutoverReport{}, err
	}
	return RuntimeCutoverReadiness(cfg, strings.TrimSpace(guildID), policies), nil
}

func RuntimeCutoverReadiness(cfg Config, guildID string, policies []ChannelA2APolicy) RuntimeCutoverReport {
	mode, err := NormalizeRuntimeIDMode(cfg.RuntimeIDMode.String())
	report := RuntimeCutoverReport{Ready: true, Mode: cfg.RuntimeIDMode.String(), BotAgentID: string(cfg.AgentID), GuildID: strings.TrimSpace(guildID), PolicyCount: len(policies)}
	if err != nil {
		report.addIssue(RuntimeCutoverIssue{Severity: RuntimeCutoverSeverityBlocker, Code: "invalid_runtime_mode", Message: err.Error()})
	} else {
		report.Mode = mode.String()
		switch mode {
		case RuntimeIDModeLegacy:
			report.addIssue(RuntimeCutoverIssue{Severity: RuntimeCutoverSeverityBlocker, Code: "legacy_runtime_mode", Message: "A2A_RUNTIME_ID_MODE=legacy cannot run runtime-addressed production cutover"})
		case RuntimeIDModeDual:
			report.addIssue(RuntimeCutoverIssue{Severity: RuntimeCutoverSeverityWarning, Code: "dual_runtime_mode", Message: "dual mode is acceptable for pre-cutover drain, but production runtime cutover should finish in runtime mode"})
		}
	}
	if cfg.AgentID == "" {
		report.addIssue(RuntimeCutoverIssue{Severity: RuntimeCutoverSeverityBlocker, Code: "missing_bot_agent_id", Message: "A2A_AGENT_ID is required"})
	} else if err := ValidateAgentID(cfg.AgentID); err != nil {
		report.addIssue(RuntimeCutoverIssue{Severity: RuntimeCutoverSeverityBlocker, Code: "invalid_bot_agent_id", Message: err.Error(), RuntimeAgentID: string(cfg.AgentID)})
	}
	if len(policies) == 0 {
		report.addIssue(RuntimeCutoverIssue{Severity: RuntimeCutoverSeverityWarning, Code: "no_policies", Message: "no A2A policy rows found for guild"})
	}

	runtimeSeen := make(map[string]string)
	channelRefSeen := make(map[string]string)
	runtimeIDs := make(map[string]struct{})
	channelRefs := make(map[string]struct{})
	for _, policy := range policies {
		ctxIssue := RuntimeCutoverIssue{GuildID: policy.GuildID, ChannelID: policy.ChannelID, ChannelRef: policy.ChannelRef, RuntimeAgentID: policy.RuntimeAgentID}
		if policy.Enabled {
			report.EnabledPolicyCount++
		} else {
			if policy.Discoverable {
				issue := ctxIssue
				issue.Severity = RuntimeCutoverSeverityWarning
				issue.Code = "disabled_discoverable_policy"
				issue.Message = "disabled policy is marked discoverable"
				report.addIssue(issue)
			}
			continue
		}
		if policy.Discoverable {
			report.DiscoverableCount++
		}
		if strings.TrimSpace(policy.RuntimeAgentID) != "" {
			report.RuntimePolicyCount++
			runtimeIDs[policy.RuntimeAgentID] = struct{}{}
		}
		if strings.TrimSpace(policy.ChannelRef) != "" {
			channelRefs[policy.ChannelRef] = struct{}{}
		}
		report.checkEnabledPolicy(cfg, policy, ctxIssue, runtimeSeen, channelRefSeen)
	}
	report.RuntimeAgentIDs = sortedKeys(runtimeIDs)
	report.ChannelRefs = sortedKeys(channelRefs)
	return report
}

func (r *RuntimeCutoverReport) checkEnabledPolicy(cfg Config, policy ChannelA2APolicy, base RuntimeCutoverIssue, runtimeSeen, channelRefSeen map[string]string) {
	runtimeID := strings.TrimSpace(policy.RuntimeAgentID)
	botID := strings.TrimSpace(policy.BotAgentID)
	channelRef := strings.TrimSpace(policy.ChannelRef)
	if runtimeID == "" {
		issue := base
		issue.Severity = RuntimeCutoverSeverityBlocker
		issue.Code = "missing_runtime_agent_id"
		issue.Message = "enabled policy must have runtime_agent_id before runtime cutover"
		r.addIssue(issue)
	} else if err := ValidateAgentID(AgentID(runtimeID)); err != nil {
		issue := base
		issue.Severity = RuntimeCutoverSeverityBlocker
		issue.Code = "invalid_runtime_agent_id"
		issue.Message = err.Error()
		r.addIssue(issue)
	}
	if botID == "" {
		issue := base
		issue.Severity = RuntimeCutoverSeverityBlocker
		issue.Code = "missing_policy_bot_agent_id"
		issue.Message = "enabled policy must have bot_agent_id"
		r.addIssue(issue)
	} else if err := ValidateAgentID(AgentID(botID)); err != nil {
		issue := base
		issue.Severity = RuntimeCutoverSeverityBlocker
		issue.Code = "invalid_policy_bot_agent_id"
		issue.Message = err.Error()
		r.addIssue(issue)
	} else if cfg.AgentID != "" && botID != string(cfg.AgentID) {
		issue := base
		issue.Severity = RuntimeCutoverSeverityBlocker
		issue.Code = "foreign_policy_bot_agent_id"
		issue.Message = fmt.Sprintf("policy bot_agent_id %s does not match configured A2A_AGENT_ID %s", botID, cfg.AgentID)
		r.addIssue(issue)
	}
	if runtimeID != "" && botID != "" && runtimeID == botID {
		issue := base
		issue.Severity = RuntimeCutoverSeverityBlocker
		issue.Code = "runtime_equals_bot_agent_id"
		issue.Message = "runtime_agent_id must be distinct from bot_agent_id"
		r.addIssue(issue)
	}
	if channelRef == "" {
		issue := base
		issue.Severity = RuntimeCutoverSeverityBlocker
		issue.Code = "missing_channel_ref"
		issue.Message = "enabled policy must have channel_ref"
		r.addIssue(issue)
	} else if !skillSlugPattern.MatchString(channelRef) {
		issue := base
		issue.Severity = RuntimeCutoverSeverityBlocker
		issue.Code = "invalid_channel_ref"
		issue.Message = "channel_ref must be subject-safe"
		r.addIssue(issue)
	}
	if runtimeID != "" {
		if prev, ok := runtimeSeen[runtimeID]; ok && prev != policy.ChannelID {
			issue := base
			issue.Severity = RuntimeCutoverSeverityBlocker
			issue.Code = "duplicate_runtime_agent_id"
			issue.Message = fmt.Sprintf("runtime_agent_id also used by channel %s", prev)
			r.addIssue(issue)
		} else {
			runtimeSeen[runtimeID] = policy.ChannelID
		}
	}
	if channelRef != "" {
		if prev, ok := channelRefSeen[channelRef]; ok && prev != policy.ChannelID {
			issue := base
			issue.Severity = RuntimeCutoverSeverityBlocker
			issue.Code = "duplicate_channel_ref"
			issue.Message = fmt.Sprintf("channel_ref also used by channel %s", prev)
			r.addIssue(issue)
		} else {
			channelRefSeen[channelRef] = policy.ChannelID
		}
	}
	if len(policy.AcceptFromRuntimes) == 0 {
		issue := base
		issue.Severity = RuntimeCutoverSeverityBlocker
		issue.Code = "missing_accept_from_runtimes"
		issue.Message = "runtime cutover requires explicit accept_from_runtimes"
		r.addIssue(issue)
	}
	for _, value := range policy.AcceptFromRuntimes {
		if !validPolicyAgentSelector(value) {
			issue := base
			issue.Severity = RuntimeCutoverSeverityBlocker
			issue.Code = "invalid_accept_from_runtime"
			issue.Message = fmt.Sprintf("accept_from_runtimes contains invalid agent selector %q", value)
			r.addIssue(issue)
		}
	}
	if len(policy.AcceptSkills) == 0 {
		issue := base
		issue.Severity = RuntimeCutoverSeverityBlocker
		issue.Code = "missing_accept_skills"
		issue.Message = "runtime cutover requires explicit accepted skills"
		r.addIssue(issue)
	}
	if len(policy.AcceptFrom) > 0 {
		issue := base
		issue.Severity = RuntimeCutoverSeverityWarning
		issue.Code = "legacy_accept_from_present"
		issue.Message = "legacy accept_from remains present; runtime mode uses accept_from_runtimes when provided"
		r.addIssue(issue)
	}
	if len(policy.DelegateTo) > 0 || len(policy.DelegateSkills) > 0 {
		issue := base
		if len(policy.DelegateTargets) == 0 {
			issue.Severity = RuntimeCutoverSeverityBlocker
			issue.Code = "legacy_delegate_policy_without_runtime_targets"
			issue.Message = "legacy delegate_to/delegate_skills must have runtime delegate_targets before runtime cutover"
		} else {
			issue.Severity = RuntimeCutoverSeverityWarning
			issue.Code = "legacy_delegate_policy_present"
			issue.Message = "legacy delegate_to/delegate_skills remain as migration input; runtime delegate_targets take precedence"
		}
		r.addIssue(issue)
	}
	for _, target := range policy.DelegateTargets {
		r.checkDelegateTarget(base, target)
	}
	if len(policy.CoPresentFrom) > 0 && len(policy.CoPresentFromRuntimes) == 0 {
		issue := base
		issue.Severity = RuntimeCutoverSeverityWarning
		issue.Code = "legacy_co_present_from_present"
		issue.Message = "legacy co_present_from remains present without co_present_from_runtimes"
		r.addIssue(issue)
	}
	if policy.RemoteToolPolicy.AllowMemoryWrite {
		issue := base
		issue.Severity = RuntimeCutoverSeverityWarning
		issue.Code = "remote_memory_write_allowed"
		issue.Message = "remote memory write is allowed; confirm this is intentional before production cutover"
		r.addIssue(issue)
	}
}

func (r *RuntimeCutoverReport) checkDelegateTarget(base RuntimeCutoverIssue, target DelegateTargetPolicy) {
	targetRuntime := strings.TrimSpace(target.RuntimeAgentID)
	legacyAgent := strings.TrimSpace(target.AgentID)
	legacyChannelRef := strings.TrimSpace(target.ChannelRef)
	issue := base
	issue.TargetRuntimeAgentID = targetRuntime
	if targetRuntime == "" {
		issue.Severity = RuntimeCutoverSeverityBlocker
		issue.Code = "legacy_delegate_target"
		issue.Message = "delegate target must use runtime_agent_id before runtime cutover"
		r.addIssue(issue)
	} else if err := ValidateAgentID(AgentID(targetRuntime)); err != nil {
		issue.Severity = RuntimeCutoverSeverityBlocker
		issue.Code = "invalid_delegate_target_runtime"
		issue.Message = err.Error()
		r.addIssue(issue)
	}
	if legacyAgent != "" || legacyChannelRef != "" {
		if targetRuntime == "" {
			issue.Severity = RuntimeCutoverSeverityBlocker
			issue.Code = "legacy_delegate_target_fields"
			issue.Message = "delegate target without runtime_agent_id still uses legacy agent_id/channel_ref fields"
		} else {
			issue.Severity = RuntimeCutoverSeverityWarning
			issue.Code = "legacy_delegate_target_fields"
			issue.Message = "delegate target preserves legacy agent_id/channel_ref fields as migration input; runtime_agent_id takes precedence"
		}
		r.addIssue(issue)
	}
	if !skillPattern.MatchString(strings.TrimSpace(target.SkillID)) {
		issue.Severity = RuntimeCutoverSeverityBlocker
		issue.Code = "invalid_delegate_target_skill"
		issue.Message = fmt.Sprintf("delegate target skill %q is invalid", target.SkillID)
		r.addIssue(issue)
	}
}

func (r *RuntimeCutoverReport) addIssue(issue RuntimeCutoverIssue) {
	if issue.Severity == "" {
		issue.Severity = RuntimeCutoverSeverityBlocker
	}
	r.Issues = append(r.Issues, issue)
	switch issue.Severity {
	case RuntimeCutoverSeverityWarning:
		r.WarningCount++
	default:
		r.BlockerCount++
		r.Ready = false
	}
}

func validPolicyAgentSelector(value string) bool {
	value = strings.TrimSpace(value)
	if value == "*" {
		return true
	}
	return ValidateAgentID(AgentID(value)) == nil
}

func sortedKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
