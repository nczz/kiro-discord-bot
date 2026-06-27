package acp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAgentModelAndModeHelpers(t *testing.T) {
	a := &Agent{sessResult: &SessionNewResult{
		Models: &ModelState{
			CurrentModelID:  "auto",
			AvailableModels: []ModelEntry{{ModelID: "auto"}, {ModelID: "claude-sonnet-4.6"}},
		},
		Modes: &ModeState{
			CurrentModeID:  "confident",
			AvailableModes: []ModeEntry{{ID: "confident"}, {ID: "kiro_default"}},
		},
	}}

	if !a.HasModel("auto") || a.HasModel("missing") {
		t.Fatalf("model helper mismatch")
	}
	if !a.HasMode("kiro_default") || a.HasMode("default") {
		t.Fatalf("mode helper mismatch")
	}

	a.setCurrentModelID("claude-sonnet-4.6")
	if got := a.CurrentModelID(); got != "claude-sonnet-4.6" {
		t.Fatalf("current model = %q", got)
	}
	a.setCurrentModeID("kiro_default")
	if got := a.CurrentModeID(); got != "kiro_default" {
		t.Fatalf("current mode = %q", got)
	}
}

func TestSessionParamsIncludesMCPServers(t *testing.T) {
	params := sessionParams("/tmp/project", "session-1", []MCPServerConfig{{
		Name:    "generic-tools",
		Command: "/bin/echo",
		Args:    []string{"ok"},
		Env:     map[string]string{"KIRO_HOME": "/tmp/kiro"},
	}})
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	var got struct {
		CWD        string `json:"cwd"`
		SessionID  string `json:"sessionId"`
		MCPServers []struct {
			Name string `json:"name"`
			Env  []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"env"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if got.CWD != "/tmp/project" || got.SessionID != "session-1" {
		t.Fatalf("unexpected session params: %+v", got)
	}
	if len(got.MCPServers) != 1 || got.MCPServers[0].Name != "generic-tools" || len(got.MCPServers[0].Env) != 1 || got.MCPServers[0].Env[0].Name != "KIRO_HOME" || got.MCPServers[0].Env[0].Value != "/tmp/kiro" {
		t.Fatalf("mcp server not preserved: %+v", got.MCPServers)
	}
}

func TestMCPServerConfigMarshalJSONURLType(t *testing.T) {
	cfg := MCPServerConfig{Name: "meta-ads", URL: "http://127.0.0.1:18900"}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["name"] != "meta-ads" || got["url"] != "http://127.0.0.1:18900" {
		t.Fatalf("unexpected json: %s", raw)
	}
	if _, hasCommand := got["command"]; hasCommand {
		t.Fatalf("URL type should not emit command field: %s", raw)
	}
}

func TestSessionParamsSerializesEmptyMCPServersArray(t *testing.T) {
	params := sessionParams("/tmp/project", "", nil)
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), `"mcpServers":null`) {
		t.Fatalf("mcpServers must be an empty array, got %s", raw)
	}
	var got struct {
		MCPServers []json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.MCPServers == nil || len(got.MCPServers) != 0 {
		t.Fatalf("expected empty mcpServers array, got %#v from %s", got.MCPServers, raw)
	}
}

func TestParsePromptStopReason(t *testing.T) {
	tests := []struct {
		name string
		raw  json.RawMessage
		want string
	}{
		{name: "empty", raw: nil, want: ""},
		{name: "normal", raw: json.RawMessage(`{"stopReason":"end_turn"}`), want: StopEndTurn},
		{name: "abnormal", raw: json.RawMessage(`{"stopReason":"max_tokens"}`), want: StopMaxTokens},
		{name: "trim", raw: json.RawMessage(`{"stopReason":" refusal "}`), want: StopRefusal},
		{name: "missing", raw: json.RawMessage(`{"sessionId":"s1"}`), want: ""},
		{name: "malformed", raw: json.RawMessage(`not json`), want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parsePromptStopReason(tt.raw); got != tt.want {
				t.Fatalf("parsePromptStopReason() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRecordStopReasonAccessor(t *testing.T) {
	a := &Agent{}
	if a.StopReason() != "" {
		t.Fatalf("initial stop reason should be empty, got %q", a.StopReason())
	}
	a.recordStopReason(json.RawMessage(`{"stopReason":"max_tokens"}`))
	if a.StopReason() != StopMaxTokens {
		t.Fatalf("stop reason = %q, want %q", a.StopReason(), StopMaxTokens)
	}
	a.lastStopReason = ""
	if a.StopReason() != "" {
		t.Fatalf("reset stop reason should be empty, got %q", a.StopReason())
	}
}

func TestParseSubagentStateEmpty(t *testing.T) {
	// Verified shape from kiro-cli 2.10.0: empty arrays.
	state := parseSubagentState(json.RawMessage(`{"subagents":[],"pendingStages":[]}`))
	if state.HasActivity() {
		t.Fatalf("empty payload should report no activity: %+v", state)
	}
	if len(state.Subagents) != 0 || len(state.PendingStages) != 0 {
		t.Fatalf("expected empty entries, got %+v", state)
	}
}

func TestParseSubagentStatePopulatedBestEffort(t *testing.T) {
	// Element fields are unverified; the parser extracts best-effort from common
	// key names and tolerates missing fields.
	payload := json.RawMessage(`{
		"subagents":[
			{"name":"research-a","status":"running","description":"summarize goroutines"},
			{"title":"research-b"}
		],
		"pendingStages":[{"name":"combine"}]
	}`)
	state := parseSubagentState(payload)
	if !state.HasActivity() {
		t.Fatal("populated payload should report activity")
	}
	if len(state.Subagents) != 2 || len(state.PendingStages) != 1 {
		t.Fatalf("unexpected counts: %+v", state)
	}
	if state.Subagents[0].Name != "research-a" || state.Subagents[0].Status != "running" {
		t.Fatalf("entry 0 not parsed: %+v", state.Subagents[0])
	}
	// "title" should fall back into Name.
	if state.Subagents[1].Name != "research-b" {
		t.Fatalf("title fallback failed: %+v", state.Subagents[1])
	}
}

func TestParseSubagentStateMalformed(t *testing.T) {
	// Must not panic or report activity on garbage.
	state := parseSubagentState(json.RawMessage(`not json`))
	if state.HasActivity() {
		t.Fatal("malformed payload should report no activity")
	}
}
