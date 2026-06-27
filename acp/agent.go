package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	startedAt time.Time

	mu           sync.Mutex
	currentText  strings.Builder
	onChunk      func(string)
	onToolUse    func(bool)          // legacy: called with true on tool_use start, false on end
	onToolCall   func(ToolCallEvent) // called on tool_call notification
	onToolResult func(ToolCallEvent) // called on tool_call_update notification
	onThought    func(string)        // called on agent_thought_chunk
	onSubagent   func(SubagentState) // called on _kiro.dev/subagent/list_update
	onExit       func()              // called when child process exits unexpectedly
	onReadError  func(error)         // called when ReadLoop encounters an error

	contextUsage   float64     // latest context usage percentage from metadata
	turnMetrics    TurnMetrics // per-turn metrics from latest metadata
	lastStopReason string      // stopReason from the most recent prompt result

	initResult    *InitializeResult
	sessResult    *SessionNewResult
	sessionLoaded bool
	dialect       Dialect        // which ACP backend this agent drives
	profile       dialectProfile // dialect-specific behavior; set in StartAgent
	stopOnce      sync.Once
	exited        chan struct{} // closed when child process exits
	stderrBuf     *ringBuffer   // captures recent stderr from kiro-cli
	mcpReady      map[string]struct{}
}

// AgentOptions configures how an agent is spawned.
type AgentOptions struct {
	MaxBuffer     int    // scanner buffer upper limit (bytes); 0 = 64 MiB
	Dialect       Dialect // ACP backend dialect; zero value = DialectKiro
	Agent         string // --agent flag; empty = kiro default
	TrustAllTools bool   // --trust-all-tools; default true
	TrustTools    string // --trust-tools <names>; comma-separated, overrides TrustAllTools
	BotName       string // clientInfo.name
	BotVersion    string // clientInfo.version
	LoadSessionID string // if non-empty, use session/load instead of session/new
	Env           []string
	MCPServers    []MCPServerConfig
}

// MCPServerConfig is passed to Kiro ACP session/new and session/load.
type MCPServerConfig struct {
	Name          string            `json:"name"`
	Command       string            `json:"command,omitempty"`
	Args          []string          `json:"args,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	URL           string            `json:"url,omitempty"`
	DisabledTools []string          `json:"disabledTools,omitempty"`
}

type mcpEnvVariable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func (c MCPServerConfig) MarshalJSON() ([]byte, error) {
	// URL-based MCP server (streamable HTTP transport).
	if c.URL != "" {
		return json.Marshal(struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		}{Name: c.Name, URL: c.URL})
	}
	// Command-based MCP server (stdio transport).
	args := c.Args
	if args == nil {
		args = []string{}
	}
	env := make([]mcpEnvVariable, 0, len(c.Env))
	keys := make([]string, 0, len(c.Env))
	for k := range c.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, mcpEnvVariable{Name: k, Value: c.Env[k]})
	}
	return json.Marshal(struct {
		Name    string           `json:"name"`
		Command string           `json:"command"`
		Args    []string         `json:"args"`
		Env     []mcpEnvVariable `json:"env"`
	}{
		Name:    c.Name,
		Command: c.Command,
		Args:    args,
		Env:     env,
	})
}

// StartAgent spawns kiro-cli acp and performs the ACP handshake (initialize + session/new).
func StartAgent(name, kiroCLI, cwd, model string, opts AgentOptions) (*Agent, error) {
	resolvedCLI, err := ResolveKiroCLI(kiroCLI)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(resolvedCLI); err != nil {
		return nil, fmt.Errorf("kiro-cli binary not found: %s", kiroCLI)
	}
	if _, err := os.Stat(cwd); err != nil {
		return nil, fmt.Errorf("working directory not found: %s", cwd)
	}

	prof := profileFor(opts.Dialect)
	args := prof.launchArgs(model, opts)

	cmd := exec.Command(resolvedCLI, args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), opts.Env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderrRing := newRingBuffer(4096)
	cmd.Stderr = io.MultiWriter(os.Stderr, stderrRing)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}

	a := &Agent{
		Name:      name,
		cmd:       cmd,
		transport: NewTransport(stdout, stdin, opts.MaxBuffer),
		state:     "starting",
		startedAt: time.Now(),
		exited:    make(chan struct{}),
		stderrBuf: stderrRing,
		mcpReady:  make(map[string]struct{}),
		dialect:   opts.Dialect,
		profile:   prof,
	}

	a.transport.OnNotification = a.handleNotification
	a.transport.OnRequest = func(method string, params json.RawMessage) interface{} {
		if opts.TrustAllTools || opts.TrustTools != "" {
			log.Printf("[agent:%s] approving server request method=%s", name, method)
			return ApproveRequestResult()
		}
		log.Printf("[agent:%s] denying server request method=%s (TRUST_ALL_TOOLS=false)", name, method)
		return DenyRequestResult()
	}

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
	initParams := map[string]interface{}{
		"protocolVersion": ClientProtocolVersion,
		"clientCapabilities": map[string]interface{}{
			"fs":       map[string]interface{}{"readTextFile": true, "writeTextFile": true},
			"terminal": true,
		},
	}
	if opts.BotName != "" || opts.BotVersion != "" {
		name := opts.BotName
		if name == "" {
			name = "kiro-discord-bot"
		}
		version := opts.BotVersion
		if version == "" {
			version = "unknown"
		}
		initParams["clientInfo"] = map[string]string{
			"name":    name,
			"version": version,
		}
	}
	initRaw, err := a.transport.Send(MethodInitialize, initParams)
	if err != nil {
		a.Kill()
		return nil, a.wrapHandshakeError("initialize", err)
	}
	var initResp InitializeResult
	json.Unmarshal(initRaw, &initResp)
	a.initResult = &initResp

	version := "unknown"
	if initResp.AgentInfo != nil {
		version = initResp.AgentInfo.Version
	}
	log.Printf("[agent:%s] pid=%d protocol=%v kiro=%s", name, cmd.Process.Pid, initResp.ProtocolVersion, version)

	// Handshake: session setup — try session/load if requested, fallback to session/new.
	var sessResp SessionNewResult
	if opts.LoadSessionID != "" && a.SupportsLoadSession() {
		loadRaw, loadErr := a.transport.Send(MethodLoadSession, sessionParams(cwd, opts.LoadSessionID, opts.MCPServers))
		if loadErr == nil {
			if r := prof.parseSession(loadRaw); r != nil {
				sessResp = *r
			}
			if sessResp.SessionID == "" {
				sessResp.SessionID = opts.LoadSessionID
			}
			a.sessionLoaded = true
			log.Printf("[agent:%s] loaded session=%s", name, sessResp.SessionID)
		} else {
			log.Printf("[agent:%s] session/load failed (%v), falling back to session/new", name, loadErr)
			opts.LoadSessionID = "" // clear to signal fallback happened
		}
	}
	if opts.LoadSessionID != "" && !a.SupportsLoadSession() {
		log.Printf("[agent:%s] session/load requested but unsupported, falling back to session/new", name)
		opts.LoadSessionID = ""
	}
	if opts.LoadSessionID == "" {
		sessRaw, sessErr := a.transport.Send(MethodNewSession, sessionParams(cwd, "", opts.MCPServers))
		if sessErr != nil {
			a.Kill()
			return nil, a.wrapHandshakeError("session/new", sessErr)
		}
		if r := prof.parseSession(sessRaw); r != nil {
			sessResp = *r
		}
	}
	a.SessionID = sessResp.SessionID
	a.sessResult = &sessResp
	a.state = "idle"

	log.Printf("[agent:%s] session=%s", name, a.SessionID)
	return a, nil
}

func sessionParams(cwd, sessionID string, servers []MCPServerConfig) map[string]interface{} {
	if servers == nil {
		servers = []MCPServerConfig{}
	}
	params := map[string]interface{}{
		"cwd":        cwd,
		"mcpServers": servers,
	}
	if sessionID != "" {
		params["sessionId"] = sessionID
	}
	return params
}

func (a *Agent) wrapHandshakeError(stage string, err error) error {
	if stderr := a.RecentStderr(); stderr != "" {
		return fmt.Errorf("%s: %w | stderr: %s", stage, err, stderr)
	}
	return fmt.Errorf("%s: %w", stage, err)
}

// ResolveKiroCLI resolves a kiro-cli path. Absolute or path-like values are
// validated directly; bare command names are resolved through PATH.
func ResolveKiroCLI(kiroCLI string) (string, error) {
	if kiroCLI == "" {
		kiroCLI = "kiro-cli"
	}
	if filepath.IsAbs(kiroCLI) || strings.Contains(kiroCLI, string(os.PathSeparator)) {
		if _, err := os.Stat(kiroCLI); err != nil {
			return "", fmt.Errorf("kiro-cli binary not found: %s", kiroCLI)
		}
		return kiroCLI, nil
	}
	resolved, err := exec.LookPath(kiroCLI)
	if err != nil {
		return "", fmt.Errorf("kiro-cli binary not found in PATH: %s", kiroCLI)
	}
	return resolved, nil
}

// parseSubagentState defensively parses a _kiro.dev/subagent/list_update payload.
// The verified shape from kiro-cli 2.10.0 is {"subagents":[...],"pendingStages":[...]}.
// Element fields are unverified (never observed populated), so they are extracted
// best-effort from common key names and may be empty.
func parseSubagentState(params json.RawMessage) SubagentState {
	var raw struct {
		Subagents     []map[string]interface{} `json:"subagents"`
		PendingStages []map[string]interface{} `json:"pendingStages"`
	}
	if json.Unmarshal(params, &raw) != nil {
		return SubagentState{}
	}
	return SubagentState{
		Subagents:     subagentEntries(raw.Subagents),
		PendingStages: subagentEntries(raw.PendingStages),
	}
}

func subagentEntries(items []map[string]interface{}) []SubagentEntry {
	if len(items) == 0 {
		return nil
	}
	out := make([]SubagentEntry, 0, len(items))
	for _, m := range items {
		out = append(out, SubagentEntry{
			Name:        firstString(m, "name", "title", "id"),
			Status:      firstString(m, "status", "state"),
			Description: firstString(m, "description", "task", "summary"),
		})
	}
	return out
}

// firstString returns the first key in keys whose value is a non-empty string.
func firstString(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func parsePromptStopReason(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var res struct {
		StopReason string `json:"stopReason"`
	}
	if json.Unmarshal(raw, &res) != nil {
		return ""
	}
	return strings.TrimSpace(res.StopReason)
}

func (a *Agent) recordStopReason(raw json.RawMessage) {
	if reason := parsePromptStopReason(raw); reason != "" {
		a.mu.Lock()
		a.lastStopReason = reason
		a.mu.Unlock()
	}
}

func (a *Agent) handleNotification(method string, params json.RawMessage) {
	// Handle metadata notifications (context usage, metering, duration)
	if method == NotifMetadata {
		var meta struct {
			ContextUsagePercentage float64        `json:"contextUsagePercentage"`
			MeteringUsage          []MeteringItem `json:"meteringUsage"`
			TurnDurationMs         int64          `json:"turnDurationMs"`
		}
		if json.Unmarshal(params, &meta) == nil {
			a.mu.Lock()
			if meta.ContextUsagePercentage > 0 {
				a.contextUsage = meta.ContextUsagePercentage
			}
			if len(meta.MeteringUsage) > 0 {
				a.turnMetrics.MeteringUsage = meta.MeteringUsage
			}
			if meta.TurnDurationMs > 0 {
				a.turnMetrics.TurnDurationMs = meta.TurnDurationMs
			}
			a.turnMetrics.ContextUsage = a.contextUsage
			a.mu.Unlock()
		}
		return
	}

	// Track MCP server initialization notifications
	if method == NotifMcpReady {
		var mcpNotif struct {
			ServerName string `json:"serverName"`
		}
		if json.Unmarshal(params, &mcpNotif) == nil && mcpNotif.ServerName != "" {
			a.mu.Lock()
			a.mcpReady[mcpNotif.ServerName] = struct{}{}
			a.mu.Unlock()
			log.Printf("[agent:%s] mcp server ready: %s", a.Name, mcpNotif.ServerName)
		}
		return
	}

	// Subagent / staging progress (kiro-cli 2.x). Only the top-level array shape
	// is verified; element fields are extracted defensively.
	if method == NotifSubagent {
		state := parseSubagentState(params)
		a.mu.Lock()
		cb := a.onSubagent
		a.mu.Unlock()
		if cb != nil && state.HasActivity() {
			cb(state)
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
			Locations  []ToolCallLocation     `json:"locations,omitempty"`
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

	case UpdateAgentThought:
		if notif.Update.Content == nil || notif.Update.Content.Text == "" {
			return
		}
		a.mu.Lock()
		cb := a.onThought
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
			Locations:  notif.Update.Locations,
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
			}
		}
		evt := ToolCallEvent{
			ToolCallID: notif.Update.ToolCallID,
			Title:      notif.Update.Title,
			Kind:       notif.Update.Kind,
			Status:     notif.Update.Status,
			RawInput:   notif.Update.RawInput,
			RawOutput:  rawOut,
			Locations:  notif.Update.Locations,
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

// RecentStderr returns the most recent stderr output from kiro-cli (up to 4 KB).
func (a *Agent) RecentStderr() string {
	if a.stderrBuf == nil {
		return ""
	}
	return strings.TrimSpace(a.stderrBuf.String())
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

// OnThoughtFunc sets a callback invoked on agent_thought_chunk notifications.
func (a *Agent) OnThoughtFunc(fn func(string)) {
	a.mu.Lock()
	a.onThought = fn
	a.mu.Unlock()
}

// OnSubagentFunc sets a callback invoked on _kiro.dev/subagent/list_update
// notifications when there is subagent or pending-stage activity.
func (a *Agent) OnSubagentFunc(fn func(SubagentState)) {
	a.mu.Lock()
	a.onSubagent = fn
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

// Uptime returns how long the current ACP process has been running.
func (a *Agent) Uptime() time.Duration {
	a.mu.Lock()
	startedAt := a.startedAt
	a.mu.Unlock()
	if startedAt.IsZero() {
		return 0
	}
	return time.Since(startedAt)
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

// AvailableModels returns the models from the session/new response, or nil.
func (a *Agent) AvailableModels() []ModelEntry {
	if a.sessResult != nil && a.sessResult.Models != nil {
		return a.sessResult.Models.AvailableModels
	}
	return nil
}

// CurrentModelID returns the active model ID from the session/new response.
func (a *Agent) CurrentModelID() string {
	if a.sessResult != nil && a.sessResult.Models != nil {
		return a.sessResult.Models.CurrentModelID
	}
	return ""
}

// AvailableModes returns the modes from the session/new response, or nil.
func (a *Agent) AvailableModes() []ModeEntry {
	if a.sessResult != nil && a.sessResult.Modes != nil {
		return a.sessResult.Modes.AvailableModes
	}
	return nil
}

// CurrentModeID returns the active mode ID from the session/new response.
func (a *Agent) CurrentModeID() string {
	if a.sessResult != nil && a.sessResult.Modes != nil {
		return a.sessResult.Modes.CurrentModeID
	}
	return ""
}

// MCPReadyServers returns MCP servers reported as initialized by the agent.
func (a *Agent) MCPReadyServers() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	servers := make([]string, 0, len(a.mcpReady))
	for server := range a.mcpReady {
		servers = append(servers, server)
	}
	sort.Strings(servers)
	return servers
}

// LoadedSession returns true when this agent was created via session/load.
func (a *Agent) LoadedSession() bool {
	return a.sessionLoaded
}

// SupportsLoadSession returns true if the agent advertises loadSession capability.
func (a *Agent) SupportsLoadSession() bool {
	return a.initResult != nil && a.initResult.AgentCapabilities != nil && a.initResult.AgentCapabilities.LoadSession
}

// SupportsImagePrompt returns true if the agent accepts image content in prompts.
func (a *Agent) SupportsImagePrompt() bool {
	return a.initResult != nil && a.initResult.AgentCapabilities != nil &&
		a.initResult.AgentCapabilities.PromptCapabilities != nil &&
		a.initResult.AgentCapabilities.PromptCapabilities.Image
}

// HasModel returns true if modelID is listed in the session's available models.
func (a *Agent) HasModel(modelID string) bool {
	for _, model := range a.AvailableModels() {
		if model.ModelID == modelID {
			return true
		}
	}
	return false
}

// HasMode returns true if modeID is listed in the session's available modes.
func (a *Agent) HasMode(modeID string) bool {
	for _, mode := range a.AvailableModes() {
		if mode.ID == modeID {
			return true
		}
	}
	return false
}

func (a *Agent) setCurrentModelID(modelID string) {
	if a.sessResult != nil && a.sessResult.Models != nil {
		a.sessResult.Models.CurrentModelID = modelID
	}
}

func (a *Agent) setCurrentModeID(modeID string) {
	if a.sessResult != nil && a.sessResult.Modes != nil {
		a.sessResult.Modes.CurrentModeID = modeID
	}
}

// Ask sends a prompt and waits for the complete response. onChunk is called for each streaming chunk.
func (a *Agent) Ask(ctx context.Context, prompt string, onChunk func(string)) (string, error) {
	a.mu.Lock()
	a.state = "working"
	a.currentText.Reset()
	a.turnMetrics = TurnMetrics{}
	a.lastStopReason = ""
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
		a.activeProfile().cancel(a)
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
		}
		return "", ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return "", r.err
		}
		a.recordStopReason(r.raw)
		a.mu.Lock()
		text := a.currentText.String()
		a.mu.Unlock()
		return text, nil
	}
}

// AsyncCallbacks holds callbacks for AskAsync.
type AsyncCallbacks struct {
	OnChunk          func(string)
	OnToolCall       func(ToolCallEvent)
	OnToolResult     func(ToolCallEvent)
	OnThought        func(string)
	OnSubagentUpdate func(SubagentState)
	OnComplete       func(response string, err error)
}

// AskAsync sends a prompt and returns immediately. Callbacks fire as notifications arrive.
// OnComplete is called when the prompt finishes (or fails). Caller must not reuse the agent
// until OnComplete fires or IsBusy() returns false.
func (a *Agent) AskAsync(prompt string, cb AsyncCallbacks) {
	a.AskAsyncMulti([]PromptContent{{Type: "text", Text: prompt}}, cb)
}

// AskAsyncMulti sends a multi-content prompt and returns immediately.
// Supports text and image content blocks. Caller must not reuse the agent
// until OnComplete fires or IsBusy() returns false.
func (a *Agent) AskAsyncMulti(content []PromptContent, cb AsyncCallbacks) {
	a.mu.Lock()
	a.state = "working"
	a.currentText.Reset()
	a.turnMetrics = TurnMetrics{}
	a.lastStopReason = ""
	a.onChunk = cb.OnChunk
	a.onToolCall = cb.OnToolCall
	a.onToolResult = cb.OnToolResult
	a.onThought = cb.OnThought
	a.onSubagent = cb.OnSubagentUpdate
	a.mu.Unlock()

	go func() {
		raw, err := a.transport.Send(MethodPrompt, map[string]interface{}{
			"sessionId": a.SessionID,
			"prompt":    content,
		})

		a.mu.Lock()
		text := a.currentText.String()
		a.state = "idle"
		a.onChunk = nil
		a.onToolCall = nil
		a.onToolResult = nil
		a.onThought = nil
		a.onSubagent = nil
		a.mu.Unlock()

		if err != nil {
			if cb.OnComplete != nil {
				cb.OnComplete("", err)
			}
			return
		}
		// Capture stopReason from the prompt result (e.g. end_turn, max_tokens,
		// refusal, cancelled). Stored turn-scoped so worker can read it in
		// OnComplete, mirroring the TurnMetrics()/ContextUsage() pattern.
		a.recordStopReason(raw)
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

// activeProfile returns this agent's dialect profile, falling back to the kiro
// profile for zero-value agents constructed directly in tests.
func (a *Agent) activeProfile() dialectProfile {
	if a.profile.cancel == nil {
		return kiroProfile()
	}
	return a.profile
}

// CancelPrompt requests cancellation of the in-flight prompt (dialect-specific:
// kiro sends a request, omp sends a notification). Non-blocking.
func (a *Agent) CancelPrompt() {
	a.activeProfile().cancel(a)
}

// Interrupt sends SIGINT to the agent process group. It is intentionally less
// terminal than Stop/Kill: if the ACP process survives, the session can keep
// serving future prompts; if it exits, Manager's existing on-exit path restarts
// on the next message without clearing persisted session metadata.
func (a *Agent) Interrupt() error {
	if a.cmd.Process == nil {
		return nil
	}
	pid := a.cmd.Process.Pid
	log.Printf("[agent:%s] interrupting pid=%d", a.Name, pid)
	return syscall.Kill(-pid, syscall.SIGINT)
}

// SetModel switches the model for the current session via session/set_model.
// Returns nil on success, or an error (including "Method not found" if unsupported).
func (a *Agent) SetModel(modelID string) error {
	err := a.activeProfile().setModel(a, modelID)
	if err == nil {
		a.setCurrentModelID(modelID)
	}
	return err
}

// SetMode switches the agent mode for the current session via session/set_mode.
// Returns nil on success, or an error (including "Method not found" if unsupported).
func (a *Agent) SetMode(modeID string) error {
	_, err := a.transport.Send(MethodSetMode, map[string]interface{}{
		"sessionId": a.SessionID,
		"modeId":    modeID,
	})
	if err == nil {
		a.setCurrentModeID(modeID)
	}
	return err
}

// ContextUsage returns the latest context usage percentage (0-100).
func (a *Agent) ContextUsage() float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.contextUsage
}

// TurnMetrics returns the metrics from the most recent turn.
func (a *Agent) TurnMetrics() TurnMetrics {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.turnMetrics
}

// StopReason returns the stopReason from the most recent completed prompt turn
// (e.g. "end_turn", "max_tokens", "refusal", "cancelled"). Empty if the agent
// did not report one. Reset at the start of each turn.
func (a *Agent) StopReason() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastStopReason
}

// Stop gracefully stops the agent: SIGTERM → wait 2s → SIGKILL entire process group.
// Safe to call multiple times.
func (a *Agent) Stop() {
	a.stopOnce.Do(func() {
		a.mu.Lock()
		a.state = "stopped"
		a.mu.Unlock()

		if a.cmd == nil || a.cmd.Process == nil {
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
		if a.cmd != nil && a.cmd.Process != nil {
			_ = syscall.Kill(-a.cmd.Process.Pid, syscall.SIGKILL)
			<-a.exited
		}
	})
}

// PreflightCheck validates the full ACP lifecycle: spawn → handshake → ask → stop.
func PreflightCheck(kiroCLI string) error {
	return PreflightCheckWithOptions(kiroCLI, AgentOptions{TrustAllTools: true})
}

// PreflightCheckWithOptions validates the full ACP lifecycle using the same
// process options as production runtime agents.
func PreflightCheckWithOptions(kiroCLI string, opts AgentOptions) error {
	opts.TrustAllTools = true
	opts.TrustTools = ""
	opts.LoadSessionID = ""
	opts.MCPServers = nil
	agent, err := StartAgent("preflight", kiroCLI, "/tmp", "", opts)
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
