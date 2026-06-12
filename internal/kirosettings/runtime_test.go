package kirosettings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureRuntimeSettingsCopiesAllowedCLISettingsAndKeepsEmptyMCP(t *testing.T) {
	dir := t.TempDir()
	sourceHome := filepath.Join(dir, "source")
	if err := os.MkdirAll(filepath.Join(sourceHome, "settings"), 0755); err != nil {
		t.Fatal(err)
	}
	source := map[string]any{
		"chat.enableTodoList":  true,
		"chat.enableKnowledge": true,
		"inline.enabled":       true,
		"mcp.loadedBefore":     true,
		"token":                "should-not-copy",
	}
	raw, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceHome, "settings", "cli.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sourceHome, "agents"), 0755); err != nil {
		t.Fatal(err)
	}
	agentConfig := `{
		"name":"custom",
		"description":"custom agent",
		"tools":["read","@global-tools","@global-tools/search","write"],
		"allowedTools":["@global-tools"],
		"useLegacyMcpJson":true,
		"includeMcpJson":true,
		"mcpServers":{"global-tools":{"command":"/bin/echo"}}
	}`
	if err := os.WriteFile(filepath.Join(sourceHome, "agents", "custom.json"), []byte(agentConfig), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceHome, "agents", "broken.json"), []byte(`{`), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KIRO_HOME", sourceHome)

	runtimeHome := filepath.Join(dir, "runtime")
	mcpPath, err := EnsureRuntimeSettings(runtimeHome)
	if err != nil {
		t.Fatalf("EnsureRuntimeSettings: %v", err)
	}
	if mcpPath != filepath.Join(runtimeHome, "settings", "mcp.json") {
		t.Fatalf("mcp path = %q", mcpPath)
	}

	mcpRaw, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(mcpRaw) != "{\"mcpServers\":{}}\n" {
		t.Fatalf("mcp config = %q", mcpRaw)
	}

	cliRaw, err := os.ReadFile(filepath.Join(runtimeHome, "settings", "cli.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cli map[string]any
	if err := json.Unmarshal(cliRaw, &cli); err != nil {
		t.Fatalf("parse runtime cli: %v", err)
	}
	if cli["chat.enableTodoList"] != true || cli["chat.enableKnowledge"] != true || cli["inline.enabled"] != true {
		t.Fatalf("allowed settings missing: %+v", cli)
	}
	if _, ok := cli["mcp.loadedBefore"]; ok {
		t.Fatalf("mcp setting should not be copied: %+v", cli)
	}
	if _, ok := cli["token"]; ok {
		t.Fatalf("unknown setting should not be copied: %+v", cli)
	}

	agentRaw, err := os.ReadFile(filepath.Join(runtimeHome, "agents", "custom.json"))
	if err != nil {
		t.Fatalf("read runtime agent config: %v", err)
	}
	var agent map[string]any
	if err := json.Unmarshal(agentRaw, &agent); err != nil {
		t.Fatalf("parse runtime agent config: %v", err)
	}
	if agent["useLegacyMcpJson"] != false || agent["includeMcpJson"] != false {
		t.Fatalf("runtime agent config should disable legacy MCP includes: %+v", agent)
	}
	if servers, ok := agent["mcpServers"].(map[string]any); !ok || len(servers) != 0 {
		t.Fatalf("runtime agent mcpServers should be empty: %+v", agent["mcpServers"])
	}
	if _, ok := agent["allowedTools"]; ok {
		t.Fatalf("runtime agent config should drop allowedTools: %+v", agent)
	}
	tools, ok := agent["tools"].([]any)
	if !ok {
		t.Fatalf("runtime agent tools missing: %+v", agent)
	}
	for _, tool := range tools {
		if s, ok := tool.(string); ok && len(s) > 0 && s[0] == '@' {
			t.Fatalf("runtime agent tools should not include MCP selectors: %+v", tools)
		}
	}
	if _, err := os.Stat(filepath.Join(runtimeHome, "agents", "broken.json")); !os.IsNotExist(err) {
		t.Fatalf("broken agent config should be skipped, stat err=%v", err)
	}
}

func TestEnsureRuntimeSettingsDoesNotTouchExistingRuntimeMCPSymlinkTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "global-mcp.json")
	original := `{"mcpServers":{"global":{"command":"/bin/echo"}}}` + "\n"
	if err := os.WriteFile(target, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	userRuntimeHome := filepath.Join(dir, "runtime")
	link := filepath.Join(userRuntimeHome, "settings", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	agentRuntimeHome := filepath.Join(dir, "kiro-agent-runtime")
	got, err := EnsureRuntimeSettings(agentRuntimeHome)
	if err != nil {
		t.Fatalf("EnsureRuntimeSettings: %v", err)
	}
	if got != filepath.Join(agentRuntimeHome, "settings", "mcp.json") {
		t.Fatalf("path = %q", got)
	}
	if fi, err := os.Lstat(link); err != nil {
		t.Fatalf("lstat settings symlink: %v", err)
	} else if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("runtime settings symlink should be left intact")
	}
	if raw, err := os.ReadFile(target); err != nil {
		t.Fatalf("read target: %v", err)
	} else if string(raw) != original {
		t.Fatalf("symlink target was modified: %q", raw)
	}
}
