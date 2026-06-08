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
