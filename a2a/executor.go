package a2a

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

type OriginRequester struct {
	DiscordUserID   string `json:"discordUserId,omitempty"`
	DiscordUsername string `json:"discordUsername,omitempty"`
	DiscordGuildID  string `json:"discordGuildId,omitempty"`
}

type OriginRuntimeRef struct {
	RuntimeAgentID   AgentID `json:"runtimeAgentId,omitempty"`
	BotAgentID       AgentID `json:"botAgentId,omitempty"`
	ChannelRef       string  `json:"channelRef,omitempty"`
	DisplayName      string  `json:"displayName,omitempty"`
	DiscordGuildID   string  `json:"discordGuildId,omitempty"`
	DiscordChannelID string  `json:"discordChannelId,omitempty"`
	DiscordThreadID  string  `json:"discordThreadId,omitempty"`
	MessageID        string  `json:"messageId,omitempty"`
}

type TaskExecutionRequest struct {
	MessageID             MessageID
	ClientTaskRef         string
	ContextID             string
	From                  AgentID
	To                    AgentID
	ChannelID             string
	GuildID               string
	ChannelRef            string
	SkillID               string
	UserVisibleSummary    string
	Payload               json.RawMessage
	Delivery              DeliveryOptions
	ResultVisibility      string
	DiscordTranscriptMode string
	CreatedAt             time.Time
	ExpiresAt             time.Time
	AuditMetadata         map[string]string
	OriginRequester       OriginRequester
	OriginRuntimeRef      OriginRuntimeRef
}

type DeliveryOptions struct {
	TimeoutSec            int
	RequiresConfirmation  bool
	DiscordContext        *DiscordContext
	DiscordContextJSON    json.RawMessage
	DiscordReplyChannelID string
	DiscordReplyThreadID  string
	ShareDiscordContext   bool
	CoPresentFrom         AgentID
	MaxDelegationDepth    int
}

type DiscordContext struct {
	GuildID       string `json:"guildId,omitempty"`
	ChannelID     string `json:"channelId,omitempty"`
	ThreadID      string `json:"threadId,omitempty"`
	MessageID     string `json:"messageId,omitempty"`
	RequestedBy   string `json:"requestedBy,omitempty"`
	RequestedByID string `json:"requestedById,omitempty"`
}

type A2AAdmissionResult struct {
	Accepted      bool
	AdmissionKey  string
	TaskID        TaskID
	State         TaskState
	Revision      int64
	ExecutorAgent AgentID
	ChannelID     string
	GuildID       string
	ChannelRef    string
	SkillID       string
	Error         TaskError
	Admission     A2AAdmission
}

type A2AContinuation struct {
	Kind    string
	Payload json.RawMessage
	Reason  string
}

type A2AAdmission struct {
	AdmissionKey string
	TaskID       TaskID
	State        TaskState
	Revision     int64
	Request      TaskExecutionRequest
	Continuation *A2AContinuation
}

type TaskExecutionResult struct {
	TaskID    TaskID
	State     TaskState
	Revision  int64
	Content   string
	Artifacts []TaskExecutionArtifact
	Error     TaskError
	Metrics   map[string]float64
}

type TaskExecutionArtifact struct {
	ID        string    `json:"id,omitempty"`
	Name      string    `json:"name,omitempty"`
	MediaType string    `json:"mediaType,omitempty"`
	Digest    string    `json:"digest,omitempty"`
	SizeBytes int64     `json:"sizeBytes,omitempty"`
	URI       string    `json:"uri,omitempty"`
	Bucket    string    `json:"bucket,omitempty"`
	Key       string    `json:"key,omitempty"`
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
}

func ArtifactFromObjectRef(ref ObjectRef, name string) TaskExecutionArtifact {
	return TaskExecutionArtifact{
		ID:        ref.ArtifactID,
		Name:      strings.TrimSpace(name),
		MediaType: ref.MediaType,
		Digest:    ref.Digest,
		SizeBytes: ref.Size,
		URI:       "nats-object://" + ref.Bucket + "/" + ref.Key,
		Bucket:    ref.Bucket,
		Key:       ref.Key,
		ExpiresAt: ref.ExpiresAt,
	}
}

type TaskError struct {
	Code    ErrorCode
	Message string
}

type Executor interface {
	AdmitA2ATask(context.Context, TaskExecutionRequest) (A2AAdmissionResult, error)
	RunA2ATask(context.Context, A2AAdmission) (TaskExecutionResult, error)
}
