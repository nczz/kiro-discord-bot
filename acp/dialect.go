package acp

import "encoding/json"

// Dialect identifies which ACP-speaking agent backend an Agent drives.
// Both kiro-cli and omp speak ACP over JSON-RPC 2.0; a dialectProfile captures
// the few points where they differ. Zero value is DialectKiro for backward
// compatibility (existing callers that do not set AgentOptions.Dialect).
type Dialect int

const (
	DialectKiro Dialect = iota
	DialectOmp
)

func (d Dialect) String() string {
	switch d {
	case DialectOmp:
		return "omp"
	default:
		return "kiro"
	}
}

// dialectProfile abstracts the points where kiro-cli and omp ACP differ.
// Everything else — transport, handshake shape (initialize/session/new/
// session/prompt), streaming (agent_message_chunk), tool_call, permission
// (session/request_permission), MCP injection, stopReason, session/load — is
// shared and lives in agent.go / jsonrpc.go regardless of dialect.
type dialectProfile struct {
	// launchArgs builds the child-process arguments that follow the binary path.
	launchArgs func(model string, opts AgentOptions) []string
	// setModel switches the active model for an existing session.
	setModel func(a *Agent, modelID string) error
	// cancel requests cancellation of the in-flight prompt for the session.
	cancel func(a *Agent)
	// parseSession parses a session/new (or session/load) result into SessionNewResult.
	parseSession func(raw json.RawMessage) *SessionNewResult
}

// profileFor returns the dialect profile for the given dialect.
// The DialectOmp branch is added in Stage 2 (S2.1); until then all dialects
// resolve to the kiro profile (and no caller sets DialectOmp).
func profileFor(d Dialect) dialectProfile {
	switch d {
	case DialectOmp:
		return ompProfile()
	default:
		return kiroProfile()
	}
}

// kiroProfile reproduces the exact behavior used before dialects existed.
func kiroProfile() dialectProfile {
	return dialectProfile{
		launchArgs: func(model string, opts AgentOptions) []string {
			args := []string{"acp"}
			if opts.TrustTools != "" {
				args = append(args, "--trust-tools", opts.TrustTools)
			} else if opts.TrustAllTools {
				args = append(args, "--trust-all-tools")
			}
			if model != "" {
				args = append(args, "--model", model)
			}
			if opts.Agent != "" {
				args = append(args, "--agent", opts.Agent)
			}
			return args
		},
		setModel: func(a *Agent, modelID string) error {
			_, err := a.transport.Send(MethodSetModel, map[string]interface{}{
				"sessionId": a.SessionID,
				"modelId":   modelID,
			})
			return err
		},
		cancel: func(a *Agent) {
			go a.transport.Send(MethodCancel, map[string]string{"sessionId": a.SessionID})
		},
		parseSession: func(raw json.RawMessage) *SessionNewResult {
			var s SessionNewResult
			_ = json.Unmarshal(raw, &s)
			return &s
		},
	}
}

// ompConfigOption mirrors one entry of omp's session/new `configOptions` array.
// Verified shape (omp 16.1.23): {id, name, category, type:"select", currentValue, options:[{value,name,description}]}.
type ompConfigOption struct {
	ID           string `json:"id"`
	Category     string `json:"category"`
	CurrentValue string `json:"currentValue"`
	Options      []struct {
		Value       string `json:"value"`
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"options"`
}

// ompProfile drives the `omp acp` ACP dialect (omp 16.x). Differences from kiro:
// launch only uses generic omp flags such as --session-dir; model switch uses
// session/set_config_option; cancel must be a JSON-RPC notification; session/new
// returns configOptions (not modes/models).
func ompProfile() dialectProfile {
	return dialectProfile{
		launchArgs: func(model string, opts AgentOptions) []string {
			args := []string{}
			if opts.SessionDir != "" {
				args = append(args, "--session-dir", opts.SessionDir)
			}
			// `omp acp` takes no kiro-style flags; tool permission is always via
			// session/request_permission (handled by the shared OnRequest path).
			// Model is selected post-handshake via setModel, not at launch.
			return append(args, "acp")
		},
		setModel: func(a *Agent, modelID string) error {
			_, err := a.transport.Send(MethodSetConfigOption, map[string]interface{}{
				"sessionId": a.SessionID,
				"configId":  "model",
				"value":     modelID,
			})
			return err
		},
		cancel: func(a *Agent) {
			// omp requires session/cancel as a notification (no id), per ACP spec.
			go func() { _ = a.transport.SendNotification(MethodCancel, map[string]string{"sessionId": a.SessionID}) }()
		},
		parseSession: parseOmpSession,
	}
}

// parseOmpSession maps omp's session/new configOptions[] into SessionNewResult's
// modes/models, so the rest of the agent (AvailableModels/Modes, current ids)
// works identically across dialects.
func parseOmpSession(raw json.RawMessage) *SessionNewResult {
	var r struct {
		SessionID     string            `json:"sessionId"`
		ConfigOptions []ompConfigOption `json:"configOptions"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return &SessionNewResult{}
	}
	out := &SessionNewResult{SessionID: r.SessionID}
	for _, opt := range r.ConfigOptions {
		switch opt.Category {
		case "model":
			ms := &ModelState{CurrentModelID: opt.CurrentValue}
			for _, o := range opt.Options {
				ms.AvailableModels = append(ms.AvailableModels, ModelEntry{
					ModelID: o.Value, Name: o.Name, Description: o.Description,
				})
			}
			out.Models = ms
		case "mode":
			md := &ModeState{CurrentModeID: opt.CurrentValue}
			for _, o := range opt.Options {
				md.AvailableModes = append(md.AvailableModes, ModeEntry{
					ID: o.Value, Name: o.Name, Description: o.Description,
				})
			}
			out.Modes = md
		}
	}
	return out
}
