package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Agent wraps a kiro-cli ACP child process.
type Agent struct {
	Name      string
	SessionID string

	cmd       *exec.Cmd
	transport *Transport
	state     string // starting, idle, working, stopped

	mu           sync.Mutex
	currentText  strings.Builder
	onChunk      func(string)
	onToolUse    func(bool)          // legacy: called with true on tool_use start, false on end
	onToolCall   func(ToolCallEvent) // called on tool_call notification
	onToolResult func(ToolCallEvent) // called on tool_call_update notification
	onExit       func()              // called when child process exits unexpectedly
	onReadError  func(error)         // called when ReadLoop encounters an error

	contextUsage float64 // latest context usage percentage from metadata

	initResult *InitializeResult
	stopOnce   sync.Once
	exited     chan struct{} // closed when child process exits
}

// StartAgent spawns kiro-cli acp and performs the ACP handshake (initialize + session/new).
func StartAgent(name, kiroCLI, cwd, model string) (*Agent, error) {
	if _, err := os.Stat(kiroCLI); err != nil {
		return nil, fmt.Errorf("kiro-cli binary not found: %s", kiroCLI)
	}
	if _, err := os.Stat(cwd); err != nil {
		return nil, fmt.Errorf("working directory not found: %s", cwd)
	}

	args := []string{"acp", "--trust-all-tools"}
	if model != "" {
		args = append(args, "--model", model)
	}

	cmd := exec.Command(kiroCLI, args...)
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}

	a := &Agent{
		Name:      name,
		cmd:       cmd,
		transport: NewTransport(stdout, stdin),
		state:     "starting",
		exited:    make(chan struct{}),
	}

	a.transport.OnNotification = a.handleNotification

	go func() {
		if err := a.transport.ReadLoop(); err != nil {
			log.Printf("[agent:%s] read loop: %v", name, err)
			a.mu.Lock()
			cb := a.onReadError
			a.mu.Unlock()
			if cb != nil {
				cb(err)
			}
		}
	}()

	// Watch for unexpected child exit
	go func() {
		a.cmd.Wait()
		close(a.exited)
		a.mu.Lock()
		wasRunning := a.state != "stopped"
		if wasRunning {
			a.state = "stopped"
		}
		cb := a.onExit
		a.mu.Unlock()
		if wasRunning {
			log.Printf("[agent:%s] process exited unexpectedly", name)
			if cb != nil {
				cb()
			}
		}
	}()

	// Handshake: initialize
	initRaw, err := a.transport.Send(MethodInitialize, map[string]interface{}{
		"protocolVersion":    ClientProtocolVersion,
		"clientCapabilities": map[string]interface{}{},
	})
	if err != nil {
		a.Kill()
		return nil, fmt.Errorf("initialize: %w", err)
	}
	var initResp InitializeResult
	json.Unmarshal(initRaw, &initResp)
	a.initResult = &initResp

	version := "unknown"
	if initResp.AgentInfo != nil {
		version = initResp.AgentInfo.Version
	}
	log.Printf("[agent:%s] pid=%d protocol=%v kiro=%s", name, cmd.Process.Pid, initResp.ProtocolVersion, version)

	// Handshake: session/new
	sessRaw, err := a.transport.Send(MethodNewSession, map[string]interface{}{
		"cwd":        cwd,
		"mcpServers": []interface{}{},
	})
	if err != nil {
		a.Kill()
		return nil, fmt.Errorf("session/new: %w", err)
	}
	var sessResp struct {
		SessionID string `json:"sessionId"`
	}
	json.Unmarshal(sessRaw, &sessResp)
	a.SessionID = sessResp.SessionID
	a.state = "idle"

	log.Printf("[agent:%s] session=%s", name, a.SessionID)
	return a, nil
}

func (a *Agent) handleNotification(method string, params json.RawMessage) {
	// Handle metadata notifications (context usage)
	if method == "_kiro.dev/metadata" {
		var meta struct {
			ContextUsagePercentage float64 `json:"contextUsagePercentage"`
		}
		if json.Unmarshal(params, &meta) == nil && meta.ContextUsagePercentage > 0 {
			a.mu.Lock()
			a.contextUsage = meta.ContextUsagePercentage
			a.mu.Unlock()
		}
		return
	}

	if method != NotifUpdate && method != NotifUpdateKiro {
		return
	}
	var notif struct {
		Update struct {
			SessionUpdate string `json:"sessionUpdate"`
			Content       *struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content,omitempty"`
			ToolCallID string                 `json:"toolCallId,omitempty"`
			Title      string                 `json:"title,omitempty"`
			Kind       string                 `json:"kind,omitempty"`
			Status     string                 `json:"status,omitempty"`
			RawInput   map[string]interface{} `json:"rawInput,omitempty"`
			RawOutput  interface{}            `json:"rawOutput,omitempty"`
		} `json:"update"`
	}
	if json.Unmarshal(params, &notif) != nil {
		return
	}

	switch notif.Update.SessionUpdate {
	case UpdateAgentChunk:
		if notif.Update.Content == nil || notif.Update.Content.Text == "" {
			return
		}
		a.mu.Lock()
		a.currentText.WriteString(notif.Update.Content.Text)
		cb := a.onChunk
		a.mu.Unlock()
		if cb != nil {
			cb(notif.Update.Content.Text)
		}

	case UpdateToolCall:
		evt := ToolCallEvent{
			ToolCallID: notif.Update.ToolCallID,
			Title:      notif.Update.Title,
			Kind:       notif.Update.Kind,
			RawInput:   notif.Update.RawInput,
		}
		a.mu.Lock()
		cb := a.onToolCall
		cbLegacy := a.onToolUse
		a.mu.Unlock()
		if cb != nil {
			cb(evt)
		}
		if cbLegacy != nil {
			cbLegacy(true)
		}

	case UpdateToolCallUpdate:
		rawOut := ""
		if notif.Update.RawOutput != nil {
			if b, err := json.Marshal(notif.Update.RawOutput); err == nil {
				rawOut = string(b)
				if len(rawOut) > 500 {
					rawOut = rawOut[:500] + "..."
				}
			}
		}
		evt := ToolCallEvent{
			ToolCallID: notif.Update.ToolCallID,
			Title:      notif.Update.Title,
			Kind:       notif.Update.Kind,
			Status:     notif.Update.Status,
			RawInput:   notif.Update.RawInput,
			RawOutput:  rawOut,
		}
		a.mu.Lock()
		cb := a.onToolResult
		cbLegacy := a.onToolUse
		a.mu.Unlock()
		if cb != nil {
			cb(evt)
		}
		if cbLegacy != nil {
			cbLegacy(false)
		}

	case "tool_use_start":
		a.mu.Lock()
		cb := a.onToolUse
		a.mu.Unlock()
		if cb != nil {
			cb(true)
		}
	case "tool_use_end":
		a.mu.Lock()
		cb := a.onToolUse
		a.mu.Unlock()
		if cb != nil {
			cb(false)
		}
	}
}

// Pid returns the OS process ID.
func (a *Agent) Pid() int {
	if a.cmd.Process != nil {
		return a.cmd.Process.Pid
	}
	return 0
}

// IsAlive returns true if the child process is still running.
func (a *Agent) IsAlive() bool {
	select {
	case <-a.exited:
		return false
	default:
		return true
	}
}

// OnExitFunc sets a callback invoked when the child process exits unexpectedly.
func (a *Agent) OnExitFunc(fn func()) {
	a.mu.Lock()
	a.onExit = fn
	a.mu.Unlock()
}

// OnToolUseFunc sets a callback invoked when tool use starts (true) or ends (false).
func (a *Agent) OnToolUseFunc(fn func(bool)) {
	a.mu.Lock()
	a.onToolUse = fn
	a.mu.Unlock()
}

// OnToolCallFunc sets a callback invoked on tool_call notifications with full detail.
func (a *Agent) OnToolCallFunc(fn func(ToolCallEvent)) {
	a.mu.Lock()
	a.onToolCall = fn
	a.mu.Unlock()
}

// OnToolResultFunc sets a callback invoked on tool_call_update notifications with result.
func (a *Agent) OnToolResultFunc(fn func(ToolCallEvent)) {
	a.mu.Lock()
	a.onToolResult = fn
	a.mu.Unlock()
}

// OnReadErrorFunc sets a callback invoked when the ReadLoop encounters an error (e.g. buffer overflow).
func (a *Agent) OnReadErrorFunc(fn func(error)) {
	a.mu.Lock()
	a.onReadError = fn
	a.mu.Unlock()
}

// State returns the current agent state.
func (a *Agent) State() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state
}

// AgentVersion returns the kiro-cli version from the initialize response.
func (a *Agent) AgentVersion() string {
	if a.initResult != nil && a.initResult.AgentInfo != nil {
		return a.initResult.AgentInfo.Version
	}
	return ""
}

// ProtocolVersion returns the protocol version from the initialize response.
func (a *Agent) ProtocolVersion() interface{} {
	if a.initResult != nil {
		return a.initResult.ProtocolVersion
	}
	return nil
}

// Ask sends a prompt and waits for the complete response. onChunk is called for each streaming chunk.
func (a *Agent) Ask(ctx context.Context, prompt string, onChunk func(string)) (string, error) {
	a.mu.Lock()
	a.state = "working"
	a.currentText.Reset()
	a.onChunk = onChunk
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.state = "idle"
		a.onChunk = nil
		a.onToolUse = nil
		a.mu.Unlock()
	}()

	type askResult struct {
		raw json.RawMessage
		err error
	}
	ch := make(chan askResult, 1)
	go func() {
		raw, err := a.transport.Send(MethodPrompt, map[string]interface{}{
			"sessionId": a.SessionID,
			"prompt":    []map[string]string{{"type": "text", "text": prompt}},
		})
		ch <- askResult{raw, err}
	}()

	select {
	case <-ctx.Done():
		go a.transport.Send(MethodCancel, map[string]string{"sessionId": a.SessionID})
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
		}
		return "", ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return "", r.err
		}
		a.mu.Lock()
		text := a.currentText.String()
		a.mu.Unlock()
		return text, nil
	}
}

// AsyncCallbacks holds callbacks for AskAsync.
type AsyncCallbacks struct {
	OnChunk      func(string)
	OnToolCall   func(ToolCallEvent)
	OnToolResult func(ToolCallEvent)
	OnComplete   func(response string, err error)
}

// AskAsync sends a prompt and returns immediately. Callbacks fire as notifications arrive.
// OnComplete is called when the prompt finishes (or fails). Caller must not reuse the agent
// until OnComplete fires or IsBusy() returns false.
func (a *Agent) AskAsync(prompt string, cb AsyncCallbacks) {
	a.mu.Lock()
	a.state = "working"
	a.currentText.Reset()
	a.onChunk = cb.OnChunk
	a.onToolCall = cb.OnToolCall
	a.onToolResult = cb.OnToolResult
	a.mu.Unlock()

	go func() {
		raw, err := a.transport.Send(MethodPrompt, map[string]interface{}{
			"sessionId": a.SessionID,
			"prompt":    []map[string]string{{"type": "text", "text": prompt}},
		})

		a.mu.Lock()
		text := a.currentText.String()
		a.state = "idle"
		a.onChunk = nil
		a.onToolCall = nil
		a.onToolResult = nil
		a.mu.Unlock()

		if err != nil {
			if cb.OnComplete != nil {
				cb.OnComplete("", err)
			}
			return
		}
		_ = raw
		if cb.OnComplete != nil {
			cb.OnComplete(text, nil)
		}
	}()
}

// IsBusy returns true if the agent is currently processing a prompt.
func (a *Agent) IsBusy() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state == "working"
}

// CancelPrompt sends a session/cancel request to the agent.
func (a *Agent) CancelPrompt() {
	a.transport.Send(MethodCancel, map[string]string{"sessionId": a.SessionID})
}

// ContextUsage returns the latest context usage percentage (0-100).
func (a *Agent) ContextUsage() float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.contextUsage
}

// Stop gracefully stops the agent: SIGTERM → wait 2s → SIGKILL entire process group.
// Safe to call multiple times.
func (a *Agent) Stop() {
	a.stopOnce.Do(func() {
		a.mu.Lock()
		a.state = "stopped"
		a.mu.Unlock()

		if a.cmd.Process == nil {
			return
		}

		pid := a.cmd.Process.Pid
		log.Printf("[agent:%s] stopping pid=%d", a.Name, pid)

		_ = syscall.Kill(-pid, syscall.SIGTERM)

		select {
		case <-a.exited:
			log.Printf("[agent:%s] exited cleanly", a.Name)
		case <-time.After(2 * time.Second):
			log.Printf("[agent:%s] force killing", a.Name)
			_ = syscall.Kill(-pid, syscall.SIGKILL)
			<-a.exited
		}
	})
}

// Kill immediately kills the agent process group.
func (a *Agent) Kill() {
	a.stopOnce.Do(func() {
		a.mu.Lock()
		a.state = "stopped"
		a.mu.Unlock()
		if a.cmd.Process != nil {
			_ = syscall.Kill(-a.cmd.Process.Pid, syscall.SIGKILL)
			<-a.exited
		}
	})
}

// PreflightCheck validates the full ACP lifecycle: spawn → handshake → ask → stop.
func PreflightCheck(kiroCLI string) error {
	agent, err := StartAgent("preflight", kiroCLI, "/tmp", "")
	if err != nil {
		return fmt.Errorf("handshake failed: %w", err)
	}
	defer agent.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := agent.Ask(ctx, "Reply with exactly: OK", nil)
	if err != nil {
		return fmt.Errorf("ask failed: %w", err)
	}
	if !strings.Contains(resp, "OK") {
		return fmt.Errorf("unexpected response: %.100s", resp)
	}

	log.Printf("[preflight] kiro-cli v%s, protocol=%v, check passed", agent.AgentVersion(), agent.ProtocolVersion())
	return nil
}
