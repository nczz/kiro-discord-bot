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
		{a2aReadTool(ToolA2APeers, "List known A2A peer agents, callable runtime/channel refs, wakeability, and skills visible from the current bound Discord channel context."), func(ctx context.Context, svc *A2AService, req A2AToolRequest) (A2AToolResponse, error) {
			return svc.Peers(ctx, req)
		}},
		{a2aReadTool(ToolA2APolicyGet, "Show the current bound channel A2A policy in structured, user-friendly terms."), func(ctx context.Context, svc *A2AService, req A2AToolRequest) (A2AToolResponse, error) {
			return svc.PolicyGet(ctx, req)
		}},
		{a2aRuntimePreflightTool(), func(ctx context.Context, svc *A2AService, req A2AToolRequest) (A2AToolResponse, error) {
			return svc.RuntimePreflight(ctx, req)
		}},
		{a2aTaskStatusTool(), func(ctx context.Context, svc *A2AService, req A2AToolRequest) (A2AToolResponse, error) {
			return svc.TaskStatus(ctx, req)
		}},
		{a2aPolicyPlanTool(), func(ctx context.Context, svc *A2AService, req A2AToolRequest) (A2AToolResponse, error) {
			return svc.PolicyPlan(ctx, req)
		}},
		{a2aWriteTool(ToolA2APolicyApply, "Apply a confirmed A2A policy diff after ManageChannels validation and a fresh confirmation token.", false, true), func(ctx context.Context, svc *A2AService, req A2AToolRequest) (A2AToolResponse, error) {
			return svc.PolicyApply(ctx, req)
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
			toolReq := a2aRequestFromMCP(req)
			if spec.tool.Name == ToolA2ATaskStatus {
				toolReq.ManageChannels = false
			}
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
	t := a2aReadTool(ToolA2ATaskStatus, "Authoritative A2A progress source: read TaskStore state and event history for one task or recent outbound tasks in the bound Discord channel. Use this for delegation status/progress; audit rows are only historical timeline evidence and may lag terminal state. For transparent/co_present tasks, the executor already posts the user-visible result in the shared Discord thread, so this tool omits result text and callers must not repost or paraphrase it.")
	for _, opt := range []mcp.ToolOption{
		mcp.WithString("task_id", mcp.Description("Remote A2A task ID. If no task matches, the value is also tried as a NATS message_id/Discord correlation ID.")),
		mcp.WithString("local_id", mcp.Description("Local durable task ID returned by bot_a2a_delegate.")),
		mcp.WithString("message_id", mcp.Description("Delegation NATS message_id or Discord message correlation ID when known.")),
	} {
		opt(&t)
	}
	return t
}

func a2aPolicyPlanTool() mcp.Tool {
	t := a2aReadTool(ToolA2APolicyPlan, "Plan an A2A policy change for the current bound Discord channel and return a confirmation challenge; applies nothing.")
	addA2APolicyFields(&t)
	mcp.WithBoolean("manage_channels", mcp.Required(), mcp.Description("Server-provided ManageChannels permission result for the requester."))(&t)
	return t
}

func a2aRuntimePreflightTool() mcp.Tool {
	t := a2aReadTool(ToolA2ARuntimePreflight, "Check guild-scoped A2A runtime cutover readiness without applying policy or service changes.")
	mcp.WithBoolean("manage_channels", mcp.Required(), mcp.Description("Server-provided ManageChannels permission result for the requester."))(&t)
	return t
}

func a2aDelegateTool() mcp.Tool {
	t := a2aWriteTool(ToolA2ADelegate, "Delegate a task to an approved remote A2A peer skill after outbound policy, quota, and confirmation checks.", false, false)
	for _, opt := range []mcp.ToolOption{
		mcp.WithString("target_agent", mcp.Required(), mcp.Description("Approved remote A2A agent ID.")),
		mcp.WithString("skill_id", mcp.Required(), mcp.Description("Approved remote skill ID.")),
		mcp.WithString("message", mcp.Required(), mcp.Description("Task text to send to the remote agent after redaction and policy checks.")),
		mcp.WithString("reason", mcp.Required(), mcp.Description("User-visible reason for audit.")),
		mcp.WithString("target_channel_ref", mcp.Description("Optional target runtime channel_ref; defaults to the current channel runtime.")),
		mcp.WithString("target_channel_id", mcp.Description("Optional Discord target channel ID used to derive channel_ref as discord-<id>.")),
		mcp.WithString("target_thread_id", mcp.Description("Optional Discord target thread ID used to derive channel_ref as discord-<id>.")),
		mcp.WithString("setup_mode", mcp.Description("auto, safe, or co_present. Default auto; same runtime uses co_present, cross-runtime uses proxy.")),
		mcp.WithBoolean("requires_confirmation", mcp.Description("Set true when remote data egress or sensitive skill confirmation is needed.")),
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
		mcp.WithString("change_id", mcp.Description("Policy change ID returned by bot_a2a_policy_plan.")),
		mcp.WithString("confirmation_token", mcp.Description("Fresh confirmation token returned by bot_a2a_policy_plan or bot_a2a_delegate.")),
		mcp.WithBoolean("manage_channels", mcp.Description("Server-provided ManageChannels permission result for the requester.")),
	} {
		opt(&t)
	}
	if name == ToolA2APolicyApply {
		addA2APolicyFields(&t)
	}
	return t
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

func addA2APolicyFields(t *mcp.Tool) {
	for _, opt := range []mcp.ToolOption{
		mcp.WithBoolean("enable", mcp.Description("Enable or disable A2A for this channel.")),
		mcp.WithString("channel_ref", mcp.Description("Stable subject-safe channel reference.")),
		mcp.WithString("target_agent", mcp.Description("Remote agent for setup-style policy changes; when provided with skill_id, delegate_targets is populated.")),
		mcp.WithString("skill_id", mcp.Description("Remote skill for setup-style policy changes; defaults are applied by clients.")),
		mcp.WithString("target_channel_ref", mcp.Description("Target runtime channel_ref for bot+channel scoped delegation.")),
		mcp.WithString("target_channel_id", mcp.Description("Discord target channel ID used to derive target channel_ref.")),
		mcp.WithString("target_thread_id", mcp.Description("Discord target thread ID used to derive target channel_ref.")),
		mcp.WithString("setup_mode", mcp.Description("auto, safe, or co_present setup defaults.")),
		mcp.WithArray("accept_from", mcp.Description("Inbound agent IDs to accept.")),
		mcp.WithArray("accept_skills", mcp.Description("Inbound skill IDs to accept.")),
		mcp.WithArray("expose_skills", mcp.Description("Local skill IDs to expose.")),
		mcp.WithArray("delegate_to", mcp.Description("Outbound agent IDs to delegate to.")),
		mcp.WithArray("delegate_skills", mcp.Description("Outbound skill IDs to delegate.")),
		mcp.WithArray("delegate_media_types", mcp.Description("Allowed delegated attachment MIME types.")),
		mcp.WithNumber("delegate_max_bytes", mcp.Description("Maximum delegated media bytes.")),
		mcp.WithNumber("max_concurrent", mcp.Description("Maximum concurrent inbound tasks; zero is unlimited.")),
		mcp.WithString("result_visibility", mcp.Description("proxy or transparent.")),
		mcp.WithString("transcript_mode", mcp.Description("delegator, mirror, or co_present.")),
		mcp.WithBoolean("share_discord_context", mcp.Description("Allow co-present Discord context sharing when policy also permits it.")),
		mcp.WithArray("co_present_from", mcp.Description("Delegator agent IDs allowed for co-present transcript.")),
		mcp.WithBoolean("allow_memory_write", mcp.Description("Allow remote A2A jobs to use bot memory write tools.")),
	} {
		opt(t)
	}
}
