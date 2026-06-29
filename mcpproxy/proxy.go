package mcpproxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"
	"syscall"
)

const (
	envCommand       = "MCP_PROXY_COMMAND"
	envArgsJSON      = "MCP_PROXY_ARGS_JSON"
	envEnvJSON       = "MCP_PROXY_ENV_JSON"
	envAllowedJSON   = "MCP_PROXY_ALLOWED_TOOLS_JSON"
	envAllowAllTools = "MCP_PROXY_ALLOW_ALL_TOOLS"
	envURL           = "MCP_PROXY_URL"
	envHeadersJSON   = "MCP_PROXY_HEADERS_JSON"
)

type Config struct {
	Command       string
	Args          []string
	Env           map[string]string
	URL           string
	Headers       map[string]string
	AllowedTools  map[string]struct{}
	AllowAllTools bool
}

func ConfigEnv(command string, args []string, env map[string]string, allowedTools []string, allowAll bool) []string {
	if args == nil {
		args = []string{}
	}
	if env == nil {
		env = map[string]string{}
	}
	if allowedTools == nil {
		allowedTools = []string{}
	}
	argsRaw, _ := json.Marshal(args)
	envRaw, _ := json.Marshal(env)
	toolsRaw, _ := json.Marshal(allowedTools)
	return []string{
		envCommand + "=" + command,
		envArgsJSON + "=" + string(argsRaw),
		envEnvJSON + "=" + string(envRaw),
		envAllowedJSON + "=" + string(toolsRaw),
		envAllowAllTools + "=" + fmt.Sprintf("%t", allowAll),
	}
}

// ConfigEnvURL produces proxy env vars for a URL-based MCP server.
func ConfigEnvURL(url string, headers map[string]string, allowedTools []string, allowAll bool) []string {
	if headers == nil {
		headers = map[string]string{}
	}
	if allowedTools == nil {
		allowedTools = []string{}
	}
	headersRaw, _ := json.Marshal(headers)
	toolsRaw, _ := json.Marshal(allowedTools)
	return []string{
		envURL + "=" + url,
		envHeadersJSON + "=" + string(headersRaw),
		envAllowedJSON + "=" + string(toolsRaw),
		envAllowAllTools + "=" + fmt.Sprintf("%t", allowAll),
	}
}

func LoadConfigFromEnv() (Config, error) {
	cfg := Config{
		Command: strings.TrimSpace(os.Getenv(envCommand)),
		URL:     strings.TrimSpace(os.Getenv(envURL)),
	}
	if cfg.Command == "" && cfg.URL == "" {
		return cfg, fmt.Errorf("%s or %s is required", envCommand, envURL)
	}
	_ = json.Unmarshal([]byte(os.Getenv(envArgsJSON)), &cfg.Args)
	_ = json.Unmarshal([]byte(os.Getenv(envEnvJSON)), &cfg.Env)
	_ = json.Unmarshal([]byte(os.Getenv(envHeadersJSON)), &cfg.Headers)
	cfg.AllowAllTools = strings.EqualFold(os.Getenv(envAllowAllTools), "true")
	var allowed []string
	_ = json.Unmarshal([]byte(os.Getenv(envAllowedJSON)), &allowed)
	cfg.AllowedTools = make(map[string]struct{}, len(allowed))
	for _, tool := range allowed {
		tool = strings.TrimSpace(tool)
		if tool != "" {
			cfg.AllowedTools[tool] = struct{}{}
		}
	}
	return cfg, nil
}

func Run(ctx context.Context, cfg Config, stdin io.Reader, stdout, stderr io.Writer) error {
	if cfg.URL != "" {
		if isSSEURL(cfg.URL) {
			return runSSE(ctx, cfg, stdin, stdout, stderr)
		}
		return runHTTP(ctx, cfg, stdin, stdout, stderr)
	}
	return runStdio(ctx, cfg, stdin, stdout, stderr)
}

func writeLineLocked(mu *sync.Mutex, w io.Writer, line []byte) error {
	mu.Lock()
	defer mu.Unlock()
	if _, err := w.Write(line); err != nil {
		return err
	}
	if len(line) == 0 || line[len(line)-1] != '\n' {
		_, err := w.Write([]byte("\n"))
		return err
	}
	return nil
}

func applyCustomHeaders(req *http.Request, headers map[string]string) {
	if req == nil {
		return
	}
	for name, value := range headers {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		req.Header.Set(name, value)
	}
}

func newMCPHTTPPostRequest(ctx context.Context, rawURL string, headers map[string]string, body []byte, sessionID string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	applyCustomHeaders(req, headers)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	return req, nil
}

func runHTTP(ctx context.Context, cfg Config, stdin io.Reader, stdout, stderr io.Writer) error {
	var writeMu sync.Mutex
	writeLine := func(w io.Writer, line []byte) error {
		return writeLineLocked(&writeMu, w, line)
	}

	client := &http.Client{}
	var sessionID string
	scanner := bufio.NewScanner(stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := append([]byte(nil), scanner.Bytes()...)
		action, blockedTool, id := inspectClientLine(cfg, line)
		if action == "block" {
			if err := writeLine(stdout, blockedToolResponse(id, blockedTool)); err != nil {
				return err
			}
			continue
		}
		req, err := newMCPHTTPPostRequest(ctx, cfg.URL, cfg.Headers, line, sessionID)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			// Network error: return JSON-RPC error to keep session alive.
			reqID := extractJSONRPCID(line)
			if err2 := writeLine(stdout, httpErrorResponse(reqID, fmt.Sprintf("http: %v", err))); err2 != nil {
				return err2
			}
			if stderr != nil {
				_, _ = fmt.Fprintf(stderr, "[mcp-proxy-http] request to %s failed: %v\n", cfg.URL, err)
			}
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			reqID := extractJSONRPCID(line)
			if err2 := writeLine(stdout, httpErrorResponse(reqID, "read response body failed")); err2 != nil {
				return err2
			}
			continue
		}
		if resp.StatusCode >= 400 {
			if stderr != nil {
				_, _ = fmt.Fprintf(stderr, "[mcp-proxy-http] %s returned %d: %s\n", cfg.URL, resp.StatusCode, body)
			}
			reqID := extractJSONRPCID(line)
			if err := writeLine(stdout, httpErrorResponse(reqID, fmt.Sprintf("upstream returned status %d", resp.StatusCode))); err != nil {
				return err
			}
			continue
		}
		if gotSessionID := strings.TrimSpace(resp.Header.Get("Mcp-Session-Id")); gotSessionID != "" {
			sessionID = gotSessionID
		}
		body = bytes.TrimSpace(body)
		if len(body) == 0 {
			continue
		}
		if isEventStreamHeader(resp.Header) {
			messages := parseSSEMessages(body)
			for _, msg := range messages {
				msg = filterToolsList(cfg, msg)
				if err := writeLine(stdout, msg); err != nil {
					return err
				}
			}
			continue
		}
		body = filterToolsList(cfg, body)
		if err := writeLine(stdout, body); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func isEventStreamResponse(resp *http.Response) bool {
	if resp == nil {
		return false
	}
	return isEventStreamHeader(resp.Header)
}

func isEventStreamHeader(header http.Header) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(header.Get("Content-Type"))), "text/event-stream")
}

func runSSE(ctx context.Context, cfg Config, stdin io.Reader, stdout, stderr io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var writeMu sync.Mutex
	writeLine := func(w io.Writer, line []byte) error {
		return writeLineLocked(&writeMu, w, line)
	}

	client := &http.Client{}
	endpointCh := make(chan string, 1)
	errCh := make(chan error, 1)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.URL, nil)
	if err != nil {
		return err
	}
	applyCustomHeaders(req, cfg.Headers)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return fmt.Errorf("sse endpoint returned status %d: %s", resp.StatusCode, body)
	}
	defer resp.Body.Close()

	go func() {
		errCh <- readSSEEvents(ctx, cfg, resp.Body, stdout, stderr, writeLine, endpointCh)
	}()

	var postURL string
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		if err != nil {
			return err
		}
		return nil
	case endpoint := <-endpointCh:
		postURL, err = resolveSSEEndpoint(cfg.URL, endpoint)
		if err != nil {
			return err
		}
	}

	scanner := bufio.NewScanner(stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := append([]byte(nil), scanner.Bytes()...)
		action, blockedTool, id := inspectClientLine(cfg, line)
		if action == "block" {
			if err := writeLine(stdout, blockedToolResponse(id, blockedTool)); err != nil {
				return err
			}
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, postURL, bytes.NewReader(line))
		if err != nil {
			return err
		}
		applyCustomHeaders(req, cfg.Headers)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			reqID := extractJSONRPCID(line)
			if err2 := writeLine(stdout, httpErrorResponse(reqID, fmt.Sprintf("sse post: %v", err))); err2 != nil {
				return err2
			}
			if stderr != nil {
				_, _ = fmt.Fprintf(stderr, "[mcp-proxy-sse] request to %s failed: %v\n", postURL, err)
			}
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			reqID := extractJSONRPCID(line)
			if err2 := writeLine(stdout, httpErrorResponse(reqID, "read SSE POST response body failed")); err2 != nil {
				return err2
			}
			continue
		}
		if resp.StatusCode >= 400 {
			if stderr != nil {
				_, _ = fmt.Fprintf(stderr, "[mcp-proxy-sse] %s returned %d: %s\n", postURL, resp.StatusCode, body)
			}
			reqID := extractJSONRPCID(line)
			if err := writeLine(stdout, httpErrorResponse(reqID, fmt.Sprintf("upstream returned status %d", resp.StatusCode))); err != nil {
				return err
			}
			continue
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func isSSEURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return path.Base(strings.TrimRight(u.Path, "/")) == "sse"
}

func resolveSSEEndpoint(baseURL, endpoint string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	ref, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return "", err
	}
	return base.ResolveReference(ref).String(), nil
}

func readSSEEvents(ctx context.Context, cfg Config, r io.Reader, stdout, stderr io.Writer, writeLine func(io.Writer, []byte) error, endpointCh chan<- string) error {
	reader := bufio.NewReader(r)
	var eventName string
	var dataLines []string
	endpointSent := false
	flush := func() error {
		if len(dataLines) == 0 {
			eventName = ""
			return nil
		}
		data := strings.Join(dataLines, "\n")
		dataLines = nil
		event := eventName
		eventName = ""
		switch event {
		case "endpoint":
			if !endpointSent {
				endpointSent = true
				select {
				case endpointCh <- data:
				default:
				}
			}
		case "", "message":
			body := bytes.TrimSpace([]byte(data))
			if len(body) == 0 {
				return nil
			}
			body = filterToolsList(cfg, body)
			if err := writeLine(stdout, body); err != nil {
				return err
			}
		default:
			if stderr != nil {
				_, _ = fmt.Fprintf(stderr, "[mcp-proxy-sse] ignored event %q\n", event)
			}
		}
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line, err := reader.ReadString('\n')
		if err != nil && len(line) == 0 {
			if err == io.EOF {
				return flush()
			}
			return err
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "":
			if err := flush(); err != nil {
				return err
			}
		case strings.HasPrefix(line, ":"):
			// SSE comment/heartbeat.
		case strings.HasPrefix(line, "event:"):
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
		if err != nil {
			if err == io.EOF {
				return flush()
			}
			return err
		}
	}
}

func parseSSEMessages(raw []byte) [][]byte {
	var messages [][]byte
	reader := bufio.NewReader(bytes.NewReader(raw))
	var eventName string
	var dataLines []string
	flush := func() {
		if len(dataLines) == 0 {
			eventName = ""
			return
		}
		data := strings.TrimSpace(strings.Join(dataLines, "\n"))
		event := eventName
		eventName = ""
		dataLines = nil
		if data == "" || (event != "" && event != "message") {
			return
		}
		messages = append(messages, []byte(data))
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil && len(line) == 0 {
			flush()
			return messages
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, ":"):
		case strings.HasPrefix(line, "event:"):
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
		if err != nil {
			flush()
			return messages
		}
	}
}

func extractJSONRPCID(line []byte) any {
	var msg struct {
		ID any `json:"id"`
	}
	_ = json.Unmarshal(line, &msg)
	return msg.ID
}

func httpErrorResponse(id any, msg string) []byte {
	raw, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    -32002,
			"message": msg,
		},
	})
	return raw
}

func runStdio(ctx context.Context, cfg Config, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	cmd.Env = os.Environ()
	for k, v := range cfg.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	childIn, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	childOut, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	var writeMu sync.Mutex
	writeLine := func(w io.Writer, line []byte) error {
		return writeLineLocked(&writeMu, w, line)
	}

	errCh := make(chan error, 2)
	go func() {
		errCh <- proxyClientToServer(ctx, cfg, stdin, childIn, stdout, writeLine)
	}()
	go func() {
		errCh <- proxyServerToClient(ctx, cfg, childOut, stdout, stderr, writeLine)
	}()

	err = <-errCh
	_ = childIn.Close()
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	if err == io.EOF || err == context.Canceled {
		return nil
	}
	return err
}

func proxyClientToServer(ctx context.Context, cfg Config, stdin io.Reader, childIn io.Writer, stdout io.Writer, writeLine func(io.Writer, []byte) error) error {
	scanner := bufio.NewScanner(stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := append([]byte(nil), scanner.Bytes()...)
		action, blockedTool, id := inspectClientLine(cfg, line)
		if action == "block" {
			if err := writeLine(stdout, blockedToolResponse(id, blockedTool)); err != nil {
				return err
			}
			continue
		}
		if err := writeLine(childIn, line); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return io.EOF
}

func proxyServerToClient(ctx context.Context, cfg Config, childOut io.Reader, stdout, stderr io.Writer, writeLine func(io.Writer, []byte) error) error {
	scanner := bufio.NewScanner(childOut)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := append([]byte(nil), scanner.Bytes()...)
		if !isJSONRPCLine(line) {
			if stderr != nil {
				_, _ = fmt.Fprintf(stderr, "[mcp-proxy] dropped non-jsonrpc stdout from %s: %s\n", cfg.Command, line)
			}
			continue
		}
		line = filterToolsList(cfg, line)
		if err := writeLine(stdout, line); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return io.EOF
}

func isJSONRPCLine(line []byte) bool {
	var msg struct {
		JSONRPC string `json:"jsonrpc"`
	}
	if json.Unmarshal(line, &msg) != nil {
		return false
	}
	return msg.JSONRPC == "2.0"
}

func inspectClientLine(cfg Config, line []byte) (string, string, any) {
	var msg struct {
		ID     any    `json:"id,omitempty"`
		Method string `json:"method,omitempty"`
		Params struct {
			Name string `json:"name,omitempty"`
		} `json:"params,omitempty"`
	}
	if json.Unmarshal(line, &msg) != nil || msg.Method != "tools/call" {
		return "pass", "", nil
	}
	if toolAllowed(cfg, msg.Params.Name) {
		return "pass", "", nil
	}
	return "block", msg.Params.Name, msg.ID
}

func filterToolsList(cfg Config, line []byte) []byte {
	if cfg.AllowAllTools {
		return line
	}
	var msg map[string]any
	if json.Unmarshal(line, &msg) != nil {
		return line
	}
	result, ok := msg["result"].(map[string]any)
	if !ok {
		return line
	}
	tools, ok := result["tools"].([]any)
	if !ok {
		return line
	}
	filtered := tools[:0]
	for _, item := range tools {
		tool, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := tool["name"].(string)
		if toolAllowed(cfg, name) {
			filtered = append(filtered, item)
		}
	}
	result["tools"] = filtered
	raw, err := json.Marshal(msg)
	if err != nil {
		return line
	}
	return raw
}

func toolAllowed(cfg Config, name string) bool {
	if cfg.AllowAllTools {
		return true
	}
	_, ok := cfg.AllowedTools[name]
	return ok
}

func blockedToolResponse(id any, tool string) []byte {
	raw, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    -32001,
			"message": fmt.Sprintf("MCP tool %q is blocked by channel policy", tool),
		},
	})
	return raw
}
