package acp_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nczz/kiro-discord-bot/acp"
	"github.com/nczz/kiro-discord-bot/internal/kirosettings"
	"github.com/nczz/kiro-discord-bot/mcpproxy"
)

func TestStartAgentWithMCPPolicyProxy(t *testing.T) {
	if os.Getenv("RUN_MCP_PROXY_SMOKE") == "" {
		t.Skip("set RUN_MCP_PROXY_SMOKE=1 and KIRO_CLI to run MCP proxy ACP smoke")
	}
	cli := kiroCLI(t)
	dir := t.TempDir()
	botBin := filepath.Join(dir, "kiro-discord-bot")
	build := exec.Command("go", "build", "-o", botBin, ".")
	build.Dir = ".."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build bot proxy binary: %v\n%s", err, out)
	}
	modCacheRaw, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Fatalf("go env GOMODCACHE: %v", err)
	}
	mockServer := filepath.Join(strings.TrimSpace(string(modCacheRaw)), "github.com/mark3labs/mcp-go@v0.45.0", "testdata", "mockstdio_server.go")
	if _, err := os.Stat(mockServer); err != nil {
		t.Fatalf("mock stdio server not found: %v", err)
	}
	mockBin := filepath.Join(dir, "mockstdio-server")
	buildMock := exec.Command("go", "build", "-o", mockBin, mockServer)
	if out, err := buildMock.CombinedOutput(); err != nil {
		t.Fatalf("build mock MCP server: %v\n%s", err, out)
	}

	proxyEnv := mapEnv(mcpproxy.ConfigEnv(mockBin, nil, nil, []string{"test-tool"}, false))
	agent, err := acp.StartAgent("test-mcp-proxy", cli, dir, "", acp.AgentOptions{
		TrustAllTools: true,
		Env:           []string{"KIRO_HOME=" + filepath.Join(dir, "kiro-home")},
		MCPServers: []acp.MCPServerConfig{{
			Name:    "mock-policy-proxy",
			Command: botBin,
			Args:    []string{"mcp-proxy"},
			Env:     proxyEnv,
		}},
	})
	if err != nil {
		t.Fatalf("StartAgent with MCP proxy: %v", err)
	}
	defer agent.Stop()
	if agent.SessionID == "" {
		t.Fatal("expected session id")
	}
}

func TestWorkspaceMCPConfigNotLoadedWhenRuntimeMCPConfigIsEmpty(t *testing.T) {
	if os.Getenv("RUN_MCP_PROXY_SMOKE") == "" {
		t.Skip("set RUN_MCP_PROXY_SMOKE=1 and KIRO_CLI to run MCP proxy ACP smoke")
	}
	cli := kiroCLI(t)
	dir := t.TempDir()
	project := filepath.Join(dir, "project")
	workspaceSettings := filepath.Join(project, ".kiro", "settings")
	if err := os.MkdirAll(workspaceSettings, 0755); err != nil {
		t.Fatalf("mkdir workspace settings: %v", err)
	}
	modCacheRaw, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Fatalf("go env GOMODCACHE: %v", err)
	}
	mockServer := filepath.Join(strings.TrimSpace(string(modCacheRaw)), "github.com/mark3labs/mcp-go@v0.45.0", "testdata", "mockstdio_server.go")
	mockBin := filepath.Join(dir, "mockstdio-server")
	buildMock := exec.Command("go", "build", "-o", mockBin, mockServer)
	if out, err := buildMock.CombinedOutput(); err != nil {
		t.Fatalf("build mock MCP server: %v\n%s", err, out)
	}
	workspaceMCP := `{"mcpServers":{"workspace-probe":{"command":` + quoteJSON(mockBin) + `}}}` + "\n"
	if err := os.WriteFile(filepath.Join(workspaceSettings, "mcp.json"), []byte(workspaceMCP), 0644); err != nil {
		t.Fatalf("write workspace mcp: %v", err)
	}
	runtimeHome := filepath.Join(dir, "kiro-agent-runtime")
	runtimeMCP, err := kirosettings.EnsureRuntimeSettings(runtimeHome)
	if err != nil {
		t.Fatalf("prepare runtime settings: %v", err)
	}

	agent, err := acp.StartAgent("test-workspace-mcp-isolation", cli, project, "", acp.AgentOptions{
		TrustAllTools: true,
		Env: []string{
			"KIRO_HOME=" + runtimeHome,
			"KIRO_MCP_CONFIG=" + runtimeMCP,
		},
	})
	if err != nil {
		t.Fatalf("StartAgent: %v", err)
	}
	defer agent.Stop()
	time.Sleep(5 * time.Second)
	if containsString(agent.MCPReadyServers(), "workspace-probe") {
		t.Fatalf("workspace MCP server bypassed runtime isolation: %+v", agent.MCPReadyServers())
	}
}

func mapEnv(items []string) map[string]string {
	out := make(map[string]string, len(items))
	for _, item := range items {
		for i := range item {
			if item[i] == '=' {
				out[item[:i]] = item[i+1:]
				break
			}
		}
	}
	return out
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func quoteJSON(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\', '"':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
