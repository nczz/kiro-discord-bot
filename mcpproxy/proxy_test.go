package mcpproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestFilterToolsList(t *testing.T) {
	cfg := Config{AllowedTools: map[string]struct{}{"allowed": {}}}
	line := []byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"allowed"},{"name":"blocked"}]}}`)
	got := filterToolsList(cfg, line)
	var msg struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(got, &msg); err != nil {
		t.Fatalf("json: %v", err)
	}
	if len(msg.Result.Tools) != 1 || msg.Result.Tools[0].Name != "allowed" {
		t.Fatalf("filtered tools = %+v", msg.Result.Tools)
	}
}

func TestInspectClientLineBlocksUnauthorizedTool(t *testing.T) {
	cfg := Config{AllowedTools: map[string]struct{}{"allowed": {}}}
	action, tool, id := inspectClientLine(cfg, []byte(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"blocked","arguments":{}}}`))
	if action != "block" || tool != "blocked" || id == nil {
		t.Fatalf("inspect = %q %q %v", action, tool, id)
	}
	resp := string(blockedToolResponse(id, tool))
	if !strings.Contains(resp, "blocked by channel policy") {
		t.Fatalf("unexpected blocked response: %s", resp)
	}
}

func TestInspectClientLineAllowsAuthorizedTool(t *testing.T) {
	cfg := Config{AllowedTools: map[string]struct{}{"allowed": {}}}
	action, tool, _ := inspectClientLine(cfg, []byte(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"allowed","arguments":{}}}`))
	if action != "pass" || tool != "" {
		t.Fatalf("inspect = %q %q", action, tool)
	}
}

func TestProxyServerToClientDropsNonJSONRPCOutput(t *testing.T) {
	cfg := Config{Command: "mock", AllowedTools: map[string]struct{}{"allowed": {}}}
	childOut := strings.NewReader(`{"time":"2026-06-08T14:30:48+08:00","level":"INFO","msg":"launch successful"}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"allowed"},{"name":"blocked"}]}}` + "\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	writeLine := func(w io.Writer, line []byte) error {
		if _, err := w.Write(line); err != nil {
			return err
		}
		_, err := w.Write([]byte("\n"))
		return err
	}

	if err := proxyServerToClient(context.Background(), cfg, childOut, &stdout, &stderr, writeLine); err != nil && err != io.EOF {
		t.Fatalf("proxy server to client: %v", err)
	}
	if strings.Contains(stdout.String(), "launch successful") {
		t.Fatalf("non-jsonrpc output leaked to stdout: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "dropped non-jsonrpc stdout") {
		t.Fatalf("expected dropped line on stderr, got %q", stderr.String())
	}
	var msg struct {
		JSONRPC string `json:"jsonrpc"`
		Result  struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &msg); err != nil {
		t.Fatalf("stdout json: %v; stdout=%q", err, stdout.String())
	}
	if msg.JSONRPC != "2.0" || len(msg.Result.Tools) != 1 || msg.Result.Tools[0].Name != "allowed" {
		t.Fatalf("unexpected forwarded jsonrpc: %+v", msg)
	}
}
