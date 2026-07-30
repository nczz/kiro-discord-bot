package botmcp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nczz/kiro-discord-bot/a2a"
)

func TestA2AToolsPolicyPlanPolicyApply(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir:            t.TempDir(),
		Config:             a2a.Config{AgentID: "adam-n200", TaskTimeoutSec: 60, MaxDelegationDepth: 1},
		BoundGuildID:       "guild-1",
		BoundChannelID:     "channel-1",
		ConfirmationSecret: "test-secret",
		ConnectNATS:        false,
		Now:                func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()

	enable := true
	unauthorized, err := svc.PolicyPlan(context.Background(), A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "alice", RequestedByID: "user-1", Enable: &enable, ChannelRef: "case/alpha"})
	if err != nil {
		t.Fatalf("PolicyPlan unauthorized err: %v", err)
	}
	if unauthorized.OK || unauthorized.ErrorCode != a2a.ErrorPolicyDenied {
		t.Fatalf("PolicyPlan unauthorized = %+v, want policy_denied", unauthorized)
	}

	planReq := A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "manager", RequestedByID: "manager-1", ManageChannels: true, Enable: &enable, ChannelRef: "case/alpha", ExposeSkills: []string{"search-case"}, DelegateTo: []string{"peer-n100"}, DelegateSkills: []string{"summarize-case"}}
	planned, err := svc.PolicyPlan(context.Background(), planReq)
	if err != nil {
		t.Fatalf("PolicyPlan manager err: %v", err)
	}
	if !planned.OK || !planned.RequiresConfirmation || planned.ConfirmationToken == "" || planned.ChangeID == "" {
		t.Fatalf("PolicyPlan manager = %+v, want confirmation payload", planned)
	}
	if planned.Policy == nil || !planned.Policy.Enabled || planned.Policy.ChannelRef != "case/alpha" {
		t.Fatalf("planned policy = %+v, want enabled channel ref", planned.Policy)
	}

	applyReq := planReq
	applyReq.ConfirmationToken = planned.ConfirmationToken
	applied, err := svc.PolicyApply(context.Background(), applyReq)
	if err != nil {
		t.Fatalf("PolicyApply: %v", err)
	}
	if !applied.OK || applied.Policy == nil || !applied.Policy.Enabled || applied.Policy.ChannelRef != "case/alpha" {
		t.Fatalf("PolicyApply = %+v, want persisted policy", applied)
	}
}

func TestA2AToolsBoundContextDelegateQuotaCancelInputReplyAuthReply(t *testing.T) {
	svc, err := NewA2AService(A2AServiceConfig{
		DataDir:            t.TempDir(),
		Config:             a2a.Config{AgentID: "adam-n200", MaxOutboundTasksPerChannel: 1, TaskTimeoutSec: 60},
		BoundGuildID:       "guild-1",
		BoundChannelID:     "channel-1",
		ConfirmationSecret: "test-secret",
		ConnectNATS:        false,
	})
	if err != nil {
		t.Fatalf("NewA2AService: %v", err)
	}
	defer svc.Close()

	base := A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1", RequestedBy: "alice", RequestedByID: "user-1", TargetAgent: "peer-n100", SkillID: "summarize-case", Message: "summarize this"}
	if got, _ := svc.Peers(context.Background(), A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-2", RequestedBy: "alice", RequestedByID: "user-1"}); got.OK || got.ErrorCode != a2a.ErrorPolicyDenied {
		t.Fatalf("wrong bound channel response = %+v, want policy_denied", got)
	}
	if got, _ := svc.Delegate(context.Background(), base); got.OK || got.ErrorCode != a2a.ErrorChannelNotEnabled {
		t.Fatalf("Delegate disabled policy response = %+v, want channel_not_enabled", got)
	}

	if _, err := svc.tasks.CreateOutbound(context.Background(), a2a.TaskRow{MessageID: "msg_quota_1", FromAgent: "adam-n200", ToAgent: "peer-n100", ChannelID: "channel-1", State: a2a.TaskStateSubmitted}); err != nil {
		t.Fatalf("CreateOutbound: %v", err)
	}
	if err := svc.checkOutboundQuota(context.Background(), base); err == nil || !strings.Contains(err.Error(), string(a2a.ErrorOverloaded)) {
		t.Fatalf("checkOutboundQuota err = %v, want overloaded", err)
	}

	base.TaskID = "task_missing"
	base.Input = "requested input"
	base.Approve = true
	for name, call := range map[string]func(context.Context, A2AToolRequest) (A2AToolResponse, error){
		"cancel":      svc.Cancel,
		"input_reply": svc.InputReply,
		"auth_reply":  svc.AuthReply,
	} {
		got, err := call(context.Background(), base)
		if err != nil {
			t.Fatalf("%s err: %v", name, err)
		}
		if got.OK || got.ErrorCode != a2a.ErrorTaskNotFound {
			t.Fatalf("%s response = %+v, want task_not_found", name, got)
		}
	}
}

func TestA2AToolsAnnotations(t *testing.T) {
	readTool := a2aReadTool(ToolA2APeers, "peers")
	if readTool.Annotations.ReadOnlyHint == nil || !*readTool.Annotations.ReadOnlyHint {
		t.Fatalf("read tool readOnlyHint = %+v, want true", readTool.Annotations.ReadOnlyHint)
	}

	writeTool := a2aWriteTool(ToolA2APolicyApply, "apply", true, true)
	if writeTool.Annotations.ReadOnlyHint == nil || *writeTool.Annotations.ReadOnlyHint {
		t.Fatalf("write tool readOnlyHint = %+v, want false", writeTool.Annotations.ReadOnlyHint)
	}
	if writeTool.Annotations.DestructiveHint == nil || !*writeTool.Annotations.DestructiveHint {
		t.Fatalf("policy apply destructiveHint = %+v, want true", writeTool.Annotations.DestructiveHint)
	}
	if writeTool.Annotations.OpenWorldHint == nil || *writeTool.Annotations.OpenWorldHint {
		t.Fatalf("policy apply openWorldHint = %+v, want false", writeTool.Annotations.OpenWorldHint)
	}
}
