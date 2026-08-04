package botmcp

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerA2ATools(s *server.MCPServer) {
	for _, spec := range []struct {
		tool mcp.Tool
		call func(context.Context, *A2AService, A2AToolRequest) (A2AToolResponse, error)
	}{
		{a2aReadTool(ToolA2APeers, "List bots/channels this Discord channel can work with. Use this before asking another bot. The trusted display flag is not permission by itself; check delegationAllowed/deliveryReadiness and verify task status after sending."), func(ctx context.Context, svc *A2AService, req A2AToolRequest) (A2AToolResponse, error) {
			return svc.Peers(ctx, req)
		}},
		{a2aTaskStatusTool(), func(ctx context.Context, svc *A2AService, req A2AToolRequest) (A2AToolResponse, error) {
			return svc.TaskStatus(ctx, req)
		}},
		{a2aTrustPeerTool(), func(ctx context.Context, svc *A2AService, req A2AToolRequest) (A2AToolResponse, error) {
			return svc.TrustPeer(ctx, req)
		}},
		{a2aDelegateTool(), func(ctx context.Context, svc *A2AService, req A2AToolRequest) (A2AToolResponse, error) {
			return svc.Delegate(ctx, req)
		}},
		{a2aWriteTool(ToolA2ACancel, "Cancel a nonterminal A2A task when requested by the task requester or a channel manager.", true, true), func(ctx context.Context, svc *A2AService, req A2AToolRequest) (A2AToolResponse, error) {
			return svc.Cancel(ctx, req)
		}},
		{a2aWriteTool(ToolA2AInputReply, "Send user-provided input for a task currently in TASK_STATE_INPUT_REQUIRED; requester or manager only.", false, true), func(ctx context.Context, svc *A2AService, req A2AToolRequest) (A2AToolResponse, error) {
			return svc.InputReply(ctx, req)
		}},
		{a2aWriteTool(ToolA2AAuthReply, "Approve or deny a task currently in TASK_STATE_AUTH_REQUIRED without carrying raw long-lived credentials.", false, true), func(ctx context.Context, svc *A2AService, req A2AToolRequest) (A2AToolResponse, error) {
			return svc.AuthReply(ctx, req)
		}},
	} {
		spec := spec
		s.AddTool(spec.tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			svc, err := NewA2AServiceFromEnv(ctx)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			defer svc.Close()
			toolReq := svc.normalizeBoundContext(a2aRequestFromMCP(req))
			toolReq.ManageChannels = authenticatedA2AMCPManageChannels()
			resp, err := spec.call(ctx, svc, toolReq)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return a2aToolResult(resp)
		})
	}
}

func a2aReadTool(name, description string) mcp.Tool {
	t := mcp.NewTool(name,
		mcp.WithDescription(description),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	addA2AContextFields(&t)
	return t
}

func a2aTaskStatusTool() mcp.Tool {
	t := a2aReadTool(ToolA2ATaskStatus, "Check the authoritative progress for one sent A2A task, or recent sent tasks in this Discord channel. Use this before telling the user another bot accepted, rejected, finished, or will reply. If shared-thread result text is omitted, the other bot already owns that Discord reply; do not repost it unless the user asks.")
	for _, opt := range []mcp.ToolOption{
		mcp.WithString("task_id", mcp.Description("Remote A2A task ID. If no task matches, the value is also tried as a NATS message_id/Discord correlation ID.")),
		mcp.WithString("local_id", mcp.Description("Local durable task ID returned by bot_a2a_delegate.")),
		mcp.WithString("message_id", mcp.Description("Delegation NATS message_id or Discord message correlation ID when known.")),
	} {
		opt(&t)
	}
	return t
}

func a2aTrustPeerTool() mcp.Tool {
	t := a2aWriteTool(ToolA2ATrustPeer, "Allow one known bot/channel runtime to send normal text work into this Discord channel. This tool only accepts target_agent and applies simple receiver-side inbound consent; expert policy changes are retired from bot-tools.", false, true)
	mcp.WithString("target_agent", mcp.Required(), mcp.Description("Bot/channel runtime ID from bot_a2a_peers to allow for normal text tasks."))(&t)
	return t
}

func a2aDelegateTool() mcp.Tool {
	t := a2aWriteTool(ToolA2ADelegate, "Send a normal text task to a known bot/channel. For typical user requests, provide only target_agent and message; omit skill_id, reason, target_channel_ref, and setup_mode. A successful call only means the request was queued. Use bot_a2a_task_status with local_id before reporting accepted/rejected/completed status; the receiving bot may still reject it.", false, false)
	for _, opt := range []mcp.ToolOption{
		mcp.WithString("target_agent", mcp.Required(), mcp.Description("Bot/channel ID from bot_a2a_peers.")),
		mcp.WithString("skill_id", mcp.Description("Leave empty for normal text tasks; inferred from the peer when possible.")),
		mcp.WithString("message", mcp.Required(), mcp.Description("Task text to send to the other bot after redaction and policy checks.")),
		mcp.WithString("reason", mcp.Description("Optional user-visible audit reason.")),
		mcp.WithString("target_channel_ref", mcp.Description("Leave empty unless the tool reports ambiguity or the user names a specific target channel label.")),
		mcp.WithString("target_channel_id", mcp.Description("Optional Discord channel ID when the user explicitly names a target channel.")),
		mcp.WithString("target_thread_id", mcp.Description("Optional Discord thread ID when the user explicitly names a target thread.")),
		mcp.WithString("setup_mode", mcp.Description("Leave empty for auto. safe keeps replies here; co_present shares the same Discord thread when allowed.")),
		mcp.WithBoolean("requires_confirmation", mcp.Description("Set true only when remote data egress or sensitive work needs explicit user approval.")),
		mcp.WithString("confirmation_token", mcp.Description("Fresh token returned by a prior confirmation challenge.")),
	} {
		opt(&t)
	}
	return t
}

func a2aWriteTool(name, description string, destructive bool, idempotent bool) mcp.Tool {
	t := mcp.NewTool(name,
		mcp.WithDescription(description),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(destructive),
		mcp.WithIdempotentHintAnnotation(idempotent),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	addA2AContextFields(&t)
	for _, opt := range []mcp.ToolOption{
		mcp.WithString("task_id", mcp.Description("Remote A2A task ID when addressing an existing task.")),
		mcp.WithString("local_id", mcp.Description("Local durable task ID when addressing an existing task.")),
		mcp.WithString("input", mcp.Description("Input text for input-required continuation.")),
		mcp.WithBoolean("approve", mcp.Description("Approve an auth-required task when true; false denies or requires deny_reason.")),
		mcp.WithString("deny_reason", mcp.Description("Reason for auth denial.")),
		mcp.WithString("reason", mcp.Description("Requester-visible reason for audit.")),
		mcp.WithString("confirmation_token", mcp.Description("Fresh confirmation token returned by bot_a2a_delegate.")),
	} {
		opt(&t)
	}
	return t
}

func authenticatedA2AMCPManageChannels() bool {
	state, ok := currentTargetState()
	return ok && state.CanManageChannel
}

func addA2AContextFields(t *mcp.Tool) {
	for _, opt := range []mcp.ToolOption{
		mcp.WithString("guild_id", mcp.Required(), mcp.Description("Discord guild ID from bound context.")),
		mcp.WithString("channel_id", mcp.Required(), mcp.Description("Discord channel ID from bound context.")),
		mcp.WithString("requested_by", mcp.Required(), mcp.Description("Requester display name from Discord context.")),
		mcp.WithString("requested_by_id", mcp.Required(), mcp.Description("Requester Discord user ID from bound context.")),
		mcp.WithNumber("limit", mcp.Description("Maximum task rows to return, 1-50.")),
	} {
		opt(t)
	}
}
