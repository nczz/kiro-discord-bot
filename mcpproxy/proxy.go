package mcpproxy

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
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
)

type Config struct {
	Command       string
	Args          []string
	Env           map[string]string
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

func LoadConfigFromEnv() (Config, error) {
	cfg := Config{Command: strings.TrimSpace(os.Getenv(envCommand))}
	if cfg.Command == "" {
		return cfg, fmt.Errorf("%s is required", envCommand)
	}
	_ = json.Unmarshal([]byte(os.Getenv(envArgsJSON)), &cfg.Args)
	_ = json.Unmarshal([]byte(os.Getenv(envEnvJSON)), &cfg.Env)
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
		writeMu.Lock()
		defer writeMu.Unlock()
		if _, err := w.Write(line); err != nil {
			return err
		}
		if len(line) == 0 || line[len(line)-1] != '\n' {
			_, err := w.Write([]byte("\n"))
			return err
		}
		return nil
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
