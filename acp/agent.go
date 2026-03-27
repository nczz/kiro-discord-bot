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

	mu          sync.Mutex
	currentText strings.Builder
	onChunk     func(string)

	initResult *InitializeResult
}

// StartAgent spawns kiro-cli acp and performs the ACP handshake (initialize + session/new).
func StartAgent(name, kiroCLI, cwd, model string) (*Agent, error) {
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
	}

	a.transport.OnNotification = a.handleNotification

	go func() {
		if err := a.transport.ReadLoop(); err != nil {
			log.Printf("[agent:%s] read loop: %v", name, err)
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
	if method != NotifUpdate {
		return
	}
	var notif struct {
		Update struct {
			SessionUpdate string `json:"sessionUpdate"`
			Content       *struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content,omitempty"`
		} `json:"update"`
	}
	if json.Unmarshal(params, &notif) != nil {
		return
	}
	if notif.Update.SessionUpdate != "agent_message_chunk" || notif.Update.Content == nil || notif.Update.Content.Text == "" {
		return
	}
	a.mu.Lock()
	a.currentText.WriteString(notif.Update.Content.Text)
	cb := a.onChunk
	a.mu.Unlock()
	if cb != nil {
		cb(notif.Update.Content.Text)
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
	if a.cmd.Process == nil {
		return false
	}
	return a.cmd.ProcessState == nil // not yet waited
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

// Stop gracefully stops the agent: SIGTERM → wait 2s → SIGKILL entire process group.
func (a *Agent) Stop() {
	a.mu.Lock()
	a.state = "stopped"
	a.mu.Unlock()

	if a.cmd.Process == nil {
		return
	}

	pid := a.cmd.Process.Pid
	log.Printf("[agent:%s] stopping pid=%d", a.Name, pid)

	_ = syscall.Kill(-pid, syscall.SIGTERM)

	done := make(chan struct{})
	go func() {
		a.cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Printf("[agent:%s] exited cleanly", a.Name)
	case <-time.After(2 * time.Second):
		log.Printf("[agent:%s] force killing", a.Name)
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		<-done
	}
}

// Kill immediately kills the agent process group.
func (a *Agent) Kill() {
	if a.cmd.Process != nil {
		_ = syscall.Kill(-a.cmd.Process.Pid, syscall.SIGKILL)
		a.cmd.Wait()
	}
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
