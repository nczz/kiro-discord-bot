package botmcp

import (
	"context"
	"testing"
	"time"

	"github.com/nczz/kiro-discord-bot/a2a"
)

func TestA2APolicyPlanRequiresManagerAndConfirmation(t *testing.T) {
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

func TestA2AToolAnnotationsProtectWrites(t *testing.T) {
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
