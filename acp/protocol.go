package acp

// ACP protocol v1 method names (kiro-cli 2.3.0)
const (
	MethodInitialize = "initialize"
	MethodNewSession = "session/new"
	MethodPrompt     = "session/prompt"
	MethodCancel     = "session/cancel"
	NotifUpdate      = "session/update"
	NotifUpdateKiro  = "_kiro.dev/session/update"
	NotifMetadata    = "_kiro.dev/metadata"

	ClientProtocolVersion = "2025-11-16"
)

// session/update notification types (kiro-cli 1.28.2+)
const (
	UpdateToolCallChunk  = "tool_call_chunk"
	UpdateToolCall       = "tool_call"
	UpdateToolCallUpdate = "tool_call_update"
	UpdateAgentChunk     = "agent_message_chunk"
	UpdateAgentThought   = "agent_thought_chunk"
)

// ToolCallLocation represents a file affected by a tool call.
type ToolCallLocation struct {
	Path string `json:"path"`
	Line *int   `json:"line,omitempty"`
}

// ToolCallEvent carries parsed tool call notification data.
type ToolCallEvent struct {
	ToolCallID string
	Title      string // human-readable, e.g. "Running: echo hello"
	Kind       string // "read", "edit", "execute", "search", "fetch", etc.
	Status     string // "completed" / "failed" (only in tool_call_update)
	RawInput   map[string]interface{}
	RawOutput  string
	Locations  []ToolCallLocation
}

// InitializeResult holds the agent's initialize response.
type InitializeResult struct {
	ProtocolVersion interface{} `json:"protocolVersion"` // numeric 1 or string
	AgentInfo       *struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"agentInfo,omitempty"`
}

// MeteringItem represents a single metering entry from metadata.
type MeteringItem struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

// TurnMetrics holds per-turn metrics from _kiro.dev/metadata.
type TurnMetrics struct {
	ContextUsage   float64
	MeteringUsage  []MeteringItem
	TurnDurationMs int64
}
