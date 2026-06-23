package mcpproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
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

func TestRunHTTPSupportsStreamableHTTPSessionAndSSEResponses(t *testing.T) {
	var sawAccept bool
	var sawSession bool
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var msg struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body, &msg)
		if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			sawAccept = true
		}
		if msg.Method == "tools/list" && r.Header.Get("Mcp-Session-Id") == "session-1" {
			sawSession = true
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if msg.Method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "session-1")
		}
		switch msg.Method {
		case "tools/list":
			_, _ = w.Write([]byte(`event: message
data: {"jsonrpc":"2.0","id":` + idJSON(msg.ID) + `,"result":{"tools":[{"name":"allowed"},{"name":"blocked"}]}}

`))
		default:
			_, _ = w.Write([]byte(`event: message
data: {"jsonrpc":"2.0","id":` + idJSON(msg.ID) + `,"result":{}}

`))
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := Config{URL: srv.URL + "/mcp", AllowedTools: map[string]struct{}{"allowed": {}}}
	stdin := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}` + "\n")
	var stdout bytes.Buffer
	if err := Run(context.Background(), cfg, stdin, &stdout, io.Discard); err != nil {
		t.Fatalf("Run streamable HTTP: %v", err)
	}
	if !sawAccept {
		t.Fatal("proxy did not send streamable HTTP Accept header")
	}
	if !sawSession {
		t.Fatal("proxy did not preserve MCP session ID")
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 response lines, got %d: %q", len(lines), stdout.String())
	}
	var toolsResp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &toolsResp); err != nil {
		t.Fatalf("tools/list response: %v; line=%s", err, lines[1])
	}
	if len(toolsResp.Result.Tools) != 1 || toolsResp.Result.Tools[0].Name != "allowed" {
		t.Fatalf("tools/list not filtered: %+v", toolsResp.Result.Tools)
	}
}

func TestRunSSEProxiesAndFilters(t *testing.T) {
	type postedMessage struct {
		ID     any    `json:"id"`
		Method string `json:"method"`
	}
	posted := make(chan postedMessage, 4)
	sseEvents := make(chan string, 4)
	var blockedPosted bool
	var blockedMu sync.Mutex

	mux := http.NewServeMux()
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("/sse method = %s, want GET", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("response writer does not support flush")
			return
		}
		_, _ = io.WriteString(w, "event: endpoint\n")
		_, _ = io.WriteString(w, "data: /messages/?session_id=test\n\n")
		flusher.Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case event := <-sseEvents:
				_, _ = io.WriteString(w, event)
				flusher.Flush()
			}
		}
	})
	mux.HandleFunc("/messages/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var msg postedMessage
		_ = json.Unmarshal(body, &msg)
		if msg.Method == "tools/call" && strings.Contains(string(body), `"blocked"`) {
			blockedMu.Lock()
			blockedPosted = true
			blockedMu.Unlock()
		}
		posted <- msg
		w.WriteHeader(http.StatusAccepted)
		switch msg.Method {
		case "tools/list":
			sseEvents <- `event: message
data: {"jsonrpc":"2.0","id":` + idJSON(msg.ID) + `,"result":{"tools":[{"name":"allowed"},{"name":"blocked"}]}}

`
		default:
			sseEvents <- `event: message
data: {"jsonrpc":"2.0","id":` + idJSON(msg.ID) + `,"result":{}}

`
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	stdinR, stdinW := io.Pipe()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, Config{URL: srv.URL + "/sse", AllowedTools: map[string]struct{}{"allowed": {}}}, stdinR, &stdout, &stderr)
	}()
	_, _ = io.WriteString(stdinW, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`+"\n")
	_, _ = io.WriteString(stdinW, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"blocked","arguments":{}}}`+"\n")
	_, _ = io.WriteString(stdinW, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"allowed","arguments":{}}}`+"\n")

	waitFor(t, func() bool {
		return strings.Count(strings.TrimSpace(stdout.String()), "\n") >= 2
	})
	cancel()
	_ = stdinW.Close()
	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled {
			t.Fatalf("Run SSE err = %v; stderr=%s", err, stderr.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run SSE did not stop")
	}

	var gotPosted []postedMessage
collect:
	for {
		select {
		case msg := <-posted:
			gotPosted = append(gotPosted, msg)
		default:
			break collect
		}
	}
	if len(gotPosted) != 2 || gotPosted[0].Method != "tools/list" || gotPosted[1].Method != "tools/call" {
		t.Fatalf("posted messages = %+v, want tools/list and allowed tools/call", gotPosted)
	}
	blockedMu.Lock()
	gotBlockedPosted := blockedPosted
	blockedMu.Unlock()
	if gotBlockedPosted {
		t.Fatal("blocked tool call was posted upstream")
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 output lines, got %d: %q", len(lines), stdout.String())
	}
	byID := make(map[string]string)
	for _, line := range lines {
		var msg struct {
			ID any `json:"id"`
		}
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("output json: %v; line=%s", err, line)
		}
		byID[idJSON(msg.ID)] = line
	}
	var toolsResp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(byID["1"]), &toolsResp); err != nil {
		t.Fatalf("tools/list response: %v; line=%s", err, byID["1"])
	}
	if len(toolsResp.Result.Tools) != 1 || toolsResp.Result.Tools[0].Name != "allowed" {
		t.Fatalf("tools/list not filtered: %+v", toolsResp.Result.Tools)
	}
	if !strings.Contains(byID["2"], "blocked by channel policy") {
		t.Fatalf("blocked tool not rejected: %s", byID["2"])
	}
	if !strings.Contains(byID["3"], `"result"`) {
		t.Fatalf("allowed tool not forwarded: %s", byID["3"])
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func idJSON(id any) string {
	raw, _ := json.Marshal(id)
	return string(raw)
}
