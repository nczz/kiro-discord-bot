package acp_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nczz/kiro-discord-bot/acp"
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
