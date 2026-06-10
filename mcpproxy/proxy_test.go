package mcpproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestRunHTTPProxiesAndFilters(t *testing.T) {
	// Spin up a fake MCP HTTP server.
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var msg struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body, &msg)
		switch msg.Method {
		case "tools/list":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":` + idJSON(msg.ID) + `,"result":{"tools":[{"name":"allowed"},{"name":"blocked"}]}}`))
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":` + idJSON(msg.ID) + `,"result":{}}`))
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := Config{URL: srv.URL, AllowedTools: map[string]struct{}{"allowed": {}}}
	stdin := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"blocked","arguments":{}}}` + "\n" +
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"allowed","arguments":{}}}` + "\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if err := Run(context.Background(), cfg, stdin, &stdout, &stderr); err != nil {
		t.Fatalf("runHTTP: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 response lines, got %d: %q", len(lines), stdout.String())
	}

	// Line 1: tools/list should be filtered.
	var toolsResp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &toolsResp); err != nil {
		t.Fatalf("tools/list response: %v", err)
	}
	if len(toolsResp.Result.Tools) != 1 || toolsResp.Result.Tools[0].Name != "allowed" {
		t.Fatalf("tools/list not filtered: %+v", toolsResp.Result.Tools)
	}

	// Line 2: blocked tool should get policy error.
	if !strings.Contains(lines[1], "blocked by channel policy") {
		t.Fatalf("blocked tool not rejected: %s", lines[1])
	}

	// Line 3: allowed tool should pass through.
	if !strings.Contains(lines[2], `"result"`) {
		t.Fatalf("allowed tool not forwarded: %s", lines[2])
	}
}

func TestRunHTTPReturnsJSONRPCErrorOnUpstreamFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	cfg := Config{URL: srv.URL, AllowAllTools: true}
	stdin := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n")
	var stdout bytes.Buffer

	if err := Run(context.Background(), cfg, stdin, &stdout, io.Discard); err != nil {
		t.Fatalf("runHTTP should not fail on upstream error: %v", err)
	}

	var resp struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &resp); err != nil {
		t.Fatalf("json: %v; output=%q", err, stdout.String())
	}
	if resp.Error.Code != -32002 || !strings.Contains(resp.Error.Message, "500") {
		t.Fatalf("unexpected error response: %+v", resp.Error)
	}
}

func idJSON(id any) string {
	raw, _ := json.Marshal(id)
	return string(raw)
}
