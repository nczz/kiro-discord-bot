package acp

// ACP protocol v1 method names (kiro-cli 2.4.2+)
const (
	MethodInitialize  = "initialize"
	MethodNewSession  = "session/new"
	MethodLoadSession = "session/load"
	MethodPrompt      = "session/prompt"
	MethodCancel      = "session/cancel"
	MethodSetModel    = "session/set_model"
	MethodSetMode     = "session/set_mode"
	// omp ACP dialect methods/notifications (omp 16.x).
	MethodSetConfigOption = "session/set_config_option" // omp model/mode setter
	NotifUsageUpdate      = "usage_update"              // omp per-session usage (size/used/cost) sessionUpdate
	NotifUpdate       = "session/update"
	NotifUpdateKiro   = "_kiro.dev/session/update"
	NotifMetadata     = "_kiro.dev/metadata"
	NotifMcpReady     = "_kiro.dev/mcp/server_initialized"
	NotifSubagent     = "_kiro.dev/subagent/list_update"

	// ClientProtocolVersion is the ACP protocol major version.
	// Per spec, this is a single integer incremented only on breaking changes.
	ClientProtocolVersion = 1
)

// session/update notification types (kiro-cli 1.28.2+)
const (
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
	ProtocolVersion   interface{}        `json:"protocolVersion"` // numeric 1 or string (legacy)
	AgentCapabilities *AgentCapabilities `json:"agentCapabilities,omitempty"`
	AgentInfo         *AgentInfo         `json:"agentInfo,omitempty"`
}

// AgentInfo holds agent implementation metadata from initialize response.
type AgentInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version"`
}

// AgentCapabilities describes features supported by the agent.
type AgentCapabilities struct {
	LoadSession         bool                   `json:"loadSession"`
	PromptCapabilities  *PromptCapabilities    `json:"promptCapabilities,omitempty"`
	McpCapabilities     *McpCapabilities       `json:"mcpCapabilities,omitempty"`
	SessionCapabilities map[string]interface{} `json:"sessionCapabilities,omitempty"`
}

// PromptCapabilities indicates content types accepted in session/prompt.
type PromptCapabilities struct {
	Image           bool `json:"image"`
	Audio           bool `json:"audio"`
	EmbeddedContext bool `json:"embeddedContext"`
}

// McpCapabilities indicates MCP transport types supported by the agent.
type McpCapabilities struct {
	HTTP bool `json:"http"`
	SSE  bool `json:"sse"`
}

// SessionNewResult holds the agent's session/new (or session/load) response.
type SessionNewResult struct {
	SessionID string      `json:"sessionId"`
	Modes     *ModeState  `json:"modes,omitempty"`
	Models    *ModelState `json:"models,omitempty"`
}

// ModeState holds available agent modes from session/new response.
type ModeState struct {
	CurrentModeID  string      `json:"currentModeId"`
	AvailableModes []ModeEntry `json:"availableModes"`
}

// ModeEntry describes a single agent mode.
type ModeEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ModelState holds available models from session/new response.
type ModelState struct {
	CurrentModelID  string       `json:"currentModelId"`
	AvailableModels []ModelEntry `json:"availableModels"`
}

// ModelEntry describes a single available model.
type ModelEntry struct {
	ModelID     string `json:"modelId"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// PromptContent represents a single content block in a session/prompt request.
type PromptContent struct {
	Type     string `json:"type"`               // "text" or "image"
	Text     string `json:"text,omitempty"`     // for type "text"
	Data     string `json:"data,omitempty"`     // base64 content for type "image"
	MimeType string `json:"mimeType,omitempty"` // MIME type for type "image"
	URI      string `json:"uri,omitempty"`      // optional source URI for type "image"
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

// session/prompt stopReason values returned in the prompt result.
// "end_turn" is the normal completion. Other values indicate the turn ended
// for a reason the user should know about.
const (
	StopEndTurn   = "end_turn"
	StopMaxTokens = "max_tokens"
	StopRefusal   = "refusal"
	StopCancelled = "cancelled"
)

// SubagentEntry is a best-effort view of a single subagent or pending stage
// from _kiro.dev/subagent/list_update. Only the top-level array shape is
// verified against kiro-cli 2.10.0; element fields are extracted defensively
// and may be empty when the agent does not provide them.
type SubagentEntry struct {
	Name        string // best-effort: "name" / "title"
	Status      string // best-effort: "status" / "state"
	Description string // best-effort: "description" / "task"
}

// SubagentState carries parsed _kiro.dev/subagent/list_update data. Counts are
// derived from the verified top-level "subagents" and "pendingStages" arrays.
type SubagentState struct {
	Subagents     []SubagentEntry
	PendingStages []SubagentEntry
}

// HasActivity reports whether there is any subagent or pending stage to show.
func (s SubagentState) HasActivity() bool {
	return len(s.Subagents) > 0 || len(s.PendingStages) > 0
}
