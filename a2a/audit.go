package a2a

import "encoding/json"

const (
	AuditTaskSendRequested   = "a2a_task_send_requested"
	AuditTaskPublishFailed   = "a2a_task_publish_failed"
	AuditResultDelivered     = "a2a_result_delivered"
	AuditTranscriptPosted    = "a2a_transcript_posted"
	AuditPolicyChangePlanned = "a2a_policy_change_planned"
	AuditPolicyChangeApplied = "a2a_policy_change_applied"
	AuditPolicyChangeDenied  = "a2a_policy_change_denied"
)

type AuditMetadataInput struct {
	TaskID                 TaskID
	ClientTaskRef          string
	MessageID              MessageID
	ContextID              string
	FromAgent              AgentID
	ToAgent                AgentID
	ExecutorAgent          AgentID
	ChannelID              string
	GuildID                string
	ChannelRef             string
	SkillID                string
	State                  TaskState
	Revision               int64
	ResultVisibility       string
	DiscordTranscriptMode  string
	DiscordMessageID       string
	DiscordTargetID        string
	DiscordParentChannelID string
	DiscordThreadID        string
	OriginDiscordMessageID string
	ActorAgentID           AgentID
	ActorDiscordUserID     string
	TranscriptDeliveryKind string
	SourceEventRevision    int64
	SourceEventID          string
	ErrorCode              ErrorCode
	PayloadSize            int
	ArtifactCount          int
}

func AuditMetadata(in AuditMetadataInput) map[string]any {
	out := map[string]any{}
	put := func(key string, value any) {
		switch v := value.(type) {
		case string:
			if v != "" {
				out[key] = v
			}
		case AgentID:
			if v != "" {
				out[key] = string(v)
			}
		case TaskID:
			if v != "" {
				out[key] = string(v)
			}
		case MessageID:
			if v != "" {
				out[key] = string(v)
			}
		case TaskState:
			if v != "" {
				out[key] = string(v)
			}
		case ErrorCode:
			if v != "" {
				out[key] = string(v)
			}
		case int64:
			if v != 0 {
				out[key] = v
			}
		case int:
			if v != 0 {
				out[key] = v
			}
		}
	}
	put("task_id", in.TaskID)
	put("client_task_ref", in.ClientTaskRef)
	put("message_id", in.MessageID)
	put("context_id", in.ContextID)
	put("from_agent", in.FromAgent)
	put("to_agent", in.ToAgent)
	put("executor_agent", in.ExecutorAgent)
	put("channel_id", in.ChannelID)
	put("guild_id", in.GuildID)
	put("channel_ref", in.ChannelRef)
	put("skill_id", in.SkillID)
	put("state", in.State)
	put("revision", in.Revision)
	put("result_visibility", in.ResultVisibility)
	put("discord_transcript_mode", in.DiscordTranscriptMode)
	put("discord_target_id", in.DiscordTargetID)
	put("discord_parent_channel_id", in.DiscordParentChannelID)
	put("discord_thread_id", in.DiscordThreadID)
	put("origin_discord_message_id", in.OriginDiscordMessageID)
	put("discord_message_id", in.DiscordMessageID)
	put("actor_agent_id", in.ActorAgentID)
	put("actor_discord_user_id", in.ActorDiscordUserID)
	put("transcript_delivery_kind", in.TranscriptDeliveryKind)
	put("source_event_revision", in.SourceEventRevision)
	put("source_event_id", in.SourceEventID)
	put("error_code", in.ErrorCode)
	put("payload_size", in.PayloadSize)
	put("artifact_count", in.ArtifactCount)
	return out
}

func PayloadSize(v any) int {
	raw, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return len(raw)
}
