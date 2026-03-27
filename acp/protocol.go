package acp

// ACP protocol v1 method names (kiro-cli 1.28.x)
const (
	MethodInitialize = "initialize"
	MethodNewSession = "session/new"
	MethodPrompt     = "session/prompt"
	MethodCancel     = "session/cancel"
	NotifUpdate      = "session/update"

	ClientProtocolVersion = "2025-11-16"
)

// InitializeResult holds the agent's initialize response.
type InitializeResult struct {
	ProtocolVersion interface{} `json:"protocolVersion"` // numeric 1 or string
	AgentInfo       *struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"agentInfo,omitempty"`
}
