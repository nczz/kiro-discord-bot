package a2a

import "context"

type TaskRecord struct {
	LocalID               string
	TaskID                TaskID
	ClientTaskRef         string
	MessageID             MessageID
	ContextID             string
	Direction             string
	Role                  string
	FromAgent             AgentID
	ToAgent               AgentID
	ExecutorAgent         AgentID
	ChannelID             string
	GuildID               string
	OriginRequester       OriginRequester
	OriginRuntimeRef      OriginRuntimeRef
	ChannelRef            string
	SkillID               string
	State                 TaskState
	Terminal              bool
	Revision              int64
	ResultVisibility      string
	DiscordTranscriptMode string
	DiscordContextJSON    string
	Error                 TaskError
}

type TaskEvent struct {
	TaskID    TaskID
	Revision  int64
	EventType string
	Payload   []byte
}

type TaskStore interface {
	CreateOutboundTask(context.Context, TaskRecord) (TaskRecord, error)
	AdmitInboundTask(context.Context, TaskRecord) (TaskRecord, error)
	BindAcceptedTask(context.Context, string, TaskID, AgentID) (TaskRecord, error)
	AppendTaskEvent(context.Context, TaskEvent) error
	MarkTerminal(context.Context, string, TaskState, TaskError) (TaskRecord, error)
}

type ChannelPolicy struct {
	GuildID               string
	ChannelID             string
	ChannelRef            string
	AllowInbound          bool
	AllowOutbound         bool
	AllowedRemoteAgents   []AgentID
	AllowedSkillIDs       []string
	RequireConfirmation   bool
	MaxDelegationDepth    int
	ShareDiscordContext   bool
	ResultVisibility      string
	DiscordTranscriptMode string
}

type PolicyDecision struct {
	Allowed bool
	Reason  string
}

type PolicyDiff struct {
	Before ChannelPolicy
	After  ChannelPolicy
	Reason string
}

type PolicyStore interface {
	GetChannelPolicy(context.Context, string, string) (ChannelPolicy, error)
	SetChannelPolicy(context.Context, ChannelPolicy, string) (PolicyDiff, error)
	ValidateInbound(context.Context, TaskExecutionRequest) (PolicyDecision, error)
	ValidateOutbound(context.Context, TaskExecutionRequest) (PolicyDecision, error)
}
