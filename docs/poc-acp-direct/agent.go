package main

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

// ACP protocol types

type InitializeParams struct {
	ProtocolVersion    string      `json:"protocolVersion"`
	ClientCapabilities interface{} `json:"clientCapabilities"`
}

type InitializeResult struct {
	ProtocolVersion string `json:"protocolVersion"`
}

type NewSessionParams struct {
	CWD        string        `json:"cwd"`
	MCPServers []interface{} `json:"mcpServers"`
}

type NewSessionResult struct {
	SessionID string `json:"sessionId"`
}

type PromptContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type PromptParams struct {
	SessionID string          `json:"sessionId"`
	Prompt    []PromptContent `json:"prompt"`
}

type PromptResult struct {
	StopReason string `json:"stopReason"`
}

type CancelParams struct {
	SessionID string `json:"sessionId"`
}

type SessionNotification struct {
	SessionID     string `json:"sessionId"`
	SessionUpdate string `json:"sessionUpdate"`
	Content       *struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content,omitempty"`
}

// Agent wraps a kiro-cli ACP process.
type Agent struct {
	name      string
	cmd       *exec.Cmd
	transport *Transport
	sessionID string
	state     string // idle, working, stopped, error

	mu          sync.Mutex
	currentText strings.Builder
	onChunk     func(string) // streaming callback
}

// StartAgent spawns kiro-cli acp and performs the ACP handshake.
func StartAgent(name, kiroCLI, cwd, model string) (*Agent, error) {
	args := []string{"acp", "--trust-all-tools"}
	if model != "" {
		args = append(args, "--model", model)
	}

	cmd := exec.Command(kiroCLI, args...)
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	// Ensure child gets its own process group for clean kill
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr // forward agent stderr for debugging

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}

	log.Printf("[agent:%s] started pid=%d", name, cmd.Process.Pid)

	transport := NewTransport(stdout, stdin)

	a := &Agent{
		name:      name,
		cmd:       cmd,
		transport: transport,
		state:     "starting",
	}

	// Handle streaming notifications
	transport.NotificationHandler = func(method string, params json.RawMessage) {
		if method == "session/update" {
			// kiro-cli format: {"sessionId":"...","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"..."}}}
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
			if notif.Update.SessionUpdate == "agent_message_chunk" && notif.Update.Content != nil && notif.Update.Content.Text != "" {
				a.mu.Lock()
				a.currentText.WriteString(notif.Update.Content.Text)
				cb := a.onChunk
				a.mu.Unlock()
				if cb != nil {
					cb(notif.Update.Content.Text)
				}
			}
		}
	}

	// Start read loop
	go func() {
		if err := transport.ReadLoop(); err != nil {
			log.Printf("[agent:%s] read loop error: %v", name, err)
		}
		log.Printf("[agent:%s] read loop ended", name)
	}()

	// ACP handshake: initialize
	initResult, err := transport.Send("initialize", InitializeParams{
		ProtocolVersion:    "2025-11-16",
		ClientCapabilities: struct{}{},
	})
	if err != nil {
		a.Kill()
		return nil, fmt.Errorf("initialize: %w", err)
	}
	var initResp json.RawMessage
	json.Unmarshal(initResult, &initResp)
	log.Printf("[agent:%s] initialized", name)

	// ACP handshake: newSession
	// kiro-cli uses "session/new" (protocol v1), not "acp/newSession"
	sessionResult, err := transport.Send("session/new", NewSessionParams{
		CWD:        cwd,
		MCPServers: []interface{}{},
	})
	if err != nil {
		a.Kill()
		return nil, fmt.Errorf("newSession: %w", err)
	}
	var sessResp NewSessionResult
	json.Unmarshal(sessionResult, &sessResp)
	a.sessionID = sessResp.SessionID
	a.state = "idle"
	log.Printf("[agent:%s] session=%s", name, a.sessionID)

	return a, nil
}

// Pid returns the OS process ID.
func (a *Agent) Pid() int {
	if a.cmd.Process != nil {
		return a.cmd.Process.Pid
	}
	return 0
}

// Ask sends a prompt and waits for the complete response.
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

	type result struct {
		raw json.RawMessage
		err error
	}
	ch := make(chan result, 1)
	go func() {
		raw, err := a.transport.Send("session/prompt", PromptParams{
			SessionID: a.sessionID,
			Prompt:    []PromptContent{{Type: "text", Text: prompt}},
		})
		ch <- result{raw, err}
	}()

	select {
	case <-ctx.Done():
		// Cancel the agent; don't block forever waiting for response
		go a.transport.Send("session/cancel", CancelParams{SessionID: a.sessionID})
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

// Stop gracefully stops the agent: SIGTERM → wait 2s → SIGKILL.
func (a *Agent) Stop() {
	a.state = "stopped"
	if a.cmd.Process == nil {
		return
	}

	pid := a.cmd.Process.Pid
	log.Printf("[agent:%s] stopping pid=%d", a.name, pid)

	// Kill the entire process group
	_ = syscall.Kill(-pid, syscall.SIGTERM)

	done := make(chan struct{})
	go func() {
		a.cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Printf("[agent:%s] exited cleanly", a.name)
	case <-time.After(2 * time.Second):
		log.Printf("[agent:%s] force killing", a.name)
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		<-done
	}
}

// Kill immediately kills the agent.
func (a *Agent) Kill() {
	if a.cmd.Process != nil {
		_ = syscall.Kill(-a.cmd.Process.Pid, syscall.SIGKILL)
		a.cmd.Wait()
	}
}
