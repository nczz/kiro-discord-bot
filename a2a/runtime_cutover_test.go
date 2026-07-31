package a2a

import (
	"context"
	"testing"
)

func TestRuntimeCutoverReadinessReadyInDualMode(t *testing.T) {
	ctx := context.Background()
	store, err := OpenPolicyStore(t.TempDir(), "adam-n200")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	policy := runtimeCutoverPolicy()
	if err := store.Save(ctx, policy, "manager"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	report, err := store.RuntimeCutoverReadiness(ctx, Config{AgentID: "adam-n200", RuntimeIDMode: RuntimeIDModeDual}, "guild-1")
	if err != nil {
		t.Fatalf("RuntimeCutoverReadiness: %v", err)
	}
	if !report.Ready || report.BlockerCount != 0 || report.WarningCount != 2 {
		t.Fatalf("report = %+v, want ready with dual+legacy accept warnings", report)
	}
	if report.EnabledPolicyCount != 1 || report.RuntimePolicyCount != 1 || len(report.RuntimeAgentIDs) != 1 || report.RuntimeAgentIDs[0] != "adam-n200-main" {
		t.Fatalf("unexpected report counts: %+v", report)
	}
}

func TestRuntimeCutoverReadinessBlocksLegacyDelegation(t *testing.T) {
	policy := runtimeCutoverPolicy()
	policy.AcceptFromRuntimes = nil
	policy.DelegateTargets = []DelegateTargetPolicy{{AgentID: "eve-local", ChannelRef: "support", SkillID: "general/task"}}
	report := RuntimeCutoverReadiness(Config{AgentID: "adam-n200", RuntimeIDMode: RuntimeIDModeRuntime}, "guild-1", []ChannelA2APolicy{policy})
	if report.Ready || report.BlockerCount == 0 {
		t.Fatalf("report = %+v, want blockers", report)
	}
	for _, code := range []string{"missing_accept_from_runtimes", "legacy_delegate_target", "legacy_delegate_target_fields"} {
		if !runtimeCutoverHasIssue(report, code) {
			t.Fatalf("report missing issue %q: %+v", code, report.Issues)
		}
	}
}

func TestRuntimeCutoverReadinessAllowsRuntimeDelegationWithLegacyMigrationFields(t *testing.T) {
	policy := runtimeCutoverPolicy()
	policy.DelegateTo = []string{"eve-local"}
	policy.DelegateSkills = []string{"general/task"}
	policy.DelegateTargets = []DelegateTargetPolicy{{
		RuntimeAgentID: "eve-local-support",
		AgentID:        "eve-local",
		ChannelRef:     "support",
		SkillID:        "general/task",
	}}
	report := RuntimeCutoverReadiness(Config{AgentID: "adam-n200", RuntimeIDMode: RuntimeIDModeRuntime}, "guild-1", []ChannelA2APolicy{policy})
	if !report.Ready || report.BlockerCount != 0 {
		t.Fatalf("report = %+v, want ready with preserved migration fields as warnings", report)
	}
	for _, code := range []string{"legacy_delegate_policy_present", "legacy_delegate_target_fields"} {
		if !runtimeCutoverHasIssue(report, code) {
			t.Fatalf("report missing warning %q: %+v", code, report.Issues)
		}
	}
}

func TestRuntimeCutoverReadinessIgnoresDisabledLegacyPolicy(t *testing.T) {
	active := runtimeCutoverPolicy()
	disabled := runtimeCutoverPolicy()
	disabled.ChannelID = "channel-2"
	disabled.ChannelRef = "legacy"
	disabled.RuntimeAgentID = ""
	disabled.BotAgentID = ""
	disabled.Enabled = false
	disabled.Discoverable = false
	disabled.AcceptFromRuntimes = nil
	disabled.DelegateTargets = []DelegateTargetPolicy{{AgentID: "legacy-bot", ChannelRef: "legacy", SkillID: "task"}}
	report := RuntimeCutoverReadiness(Config{AgentID: "adam-n200", RuntimeIDMode: RuntimeIDModeRuntime}, "guild-1", []ChannelA2APolicy{active, disabled})
	if !report.Ready || report.EnabledPolicyCount != 1 || report.PolicyCount != 2 {
		t.Fatalf("report = %+v, want disabled legacy row ignored", report)
	}
}

func runtimeCutoverPolicy() ChannelA2APolicy {
	return ChannelA2APolicy{
		GuildID:             "guild-1",
		ChannelID:           "channel-1",
		Enabled:             true,
		Discoverable:        true,
		RuntimeAgentID:      "adam-n200-main",
		BotAgentID:          "adam-n200",
		ChannelRef:          "main",
		AcceptFrom:          []string{"eve-local"},
		AcceptFromRuntimes:  []string{"eve-local-support"},
		AcceptSkills:        []string{"task"},
		ExposeSkills:        []SkillPolicy{{ID: "task", InputModes: []string{"text/plain"}, OutputModes: []string{"text/plain"}}},
		DelegateTargets:     []DelegateTargetPolicy{{RuntimeAgentID: "eve-local-support", SkillID: "general/task"}},
		ResultVisibility:    "proxy",
		RemoteToolPolicy:    RemoteToolPolicy{},
		DelegateMedia:       DelegateMediaPolicy{},
		MaxConcurrent:       0,
		AutoDelegateEnabled: false,
	}
}

func runtimeCutoverHasIssue(report RuntimeCutoverReport, code string) bool {
	for _, issue := range report.Issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}
