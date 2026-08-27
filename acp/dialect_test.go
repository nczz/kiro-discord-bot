package acp

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestKiroProfileLaunchArgsIncludesFlags(t *testing.T) {
	args := kiroProfile().launchArgs("claude-sonnet-4.6", AgentOptions{TrustAllTools: true, Agent: "confident"})
	joined := join(args)
	for _, want := range []string{"acp", "--trust-all-tools", "--model", "claude-sonnet-4.6", "--agent", "confident"} {
		if !contains(args, want) {
			t.Fatalf("kiro launchArgs missing %q: %v", want, args)
		}
	}
	if args[0] != "acp" {
		t.Fatalf("first arg must be acp, got %q (%s)", args[0], joined)
	}
}

func TestKiroCancelUsesNotification(t *testing.T) {
	var out bytes.Buffer
	tr := NewTransport(strings.NewReader(""), &out, 0)
	a := &Agent{SessionID: "sid-1", transport: tr, profile: kiroProfile()}

	a.activeProfile().cancel(a)

	deadline := time.Now().Add(time.Second)
	for !strings.Contains(out.String(), `"method":"session/cancel"`) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	got := out.String()
	if !strings.Contains(got, `"method":"session/cancel"`) {
		t.Fatalf("cancel notification not written: %s", got)
	}
	if strings.Contains(got, `"id"`) {
		t.Fatalf("cancel must be a notification without id: %s", got)
	}
}

func TestOmpProfileLaunchArgsUsesOnlyOmpRuntimeFlags(t *testing.T) {
	// omp acp does not take Kiro-style trust/model/agent flags; model is
	// selected through session/set_config_option after the handshake.
	args := ompProfile().launchArgs("openai-codex/gpt-5.5", AgentOptions{TrustAllTools: true, Agent: "x", TrustTools: "y"})
	if len(args) != 1 || args[0] != "acp" {
		t.Fatalf("omp launchArgs must be exactly [acp], got %v", args)
	}

	args = ompProfile().launchArgs("", AgentOptions{SessionDir: "/tmp/omp-sessions"})
	want := []string{"--session-dir", "/tmp/omp-sessions", "acp"}
	if !equalStringSlices(args, want) {
		t.Fatalf("omp launchArgs = %v, want %v", args, want)
	}
}

func TestProfileForSelectsDialect(t *testing.T) {
	if profileFor(DialectKiro).launchArgs == nil || profileFor(DialectOmp).launchArgs == nil {
		t.Fatal("both dialect profiles must populate launchArgs")
	}
	// kiro includes flags, omp does not — distinguishes the two profiles.
	k := profileFor(DialectKiro).launchArgs("m", AgentOptions{TrustAllTools: true})
	o := profileFor(DialectOmp).launchArgs("m", AgentOptions{TrustAllTools: true})
	if len(o) >= len(k) {
		t.Fatalf("omp args (%v) should be shorter than kiro args (%v)", o, k)
	}
}

func TestKiroProfileSetModeUsesSessionSetMode(t *testing.T) {
	req := captureSetModeRequest(t, kiroProfile(), "plan")

	if req.Method != MethodSetMode {
		t.Fatalf("kiro SetMode method = %q, want %q", req.Method, MethodSetMode)
	}
	if req.Params["sessionId"] != "sid-1" || req.Params["modeId"] != "plan" {
		t.Fatalf("kiro SetMode params = %#v", req.Params)
	}
	if _, ok := req.Params["configId"]; ok {
		t.Fatalf("kiro SetMode must not use configId params: %#v", req.Params)
	}
}

func TestOmpProfileSetModeUsesConfigOption(t *testing.T) {
	req := captureSetModeRequest(t, ompProfile(), "plan")

	if req.Method != MethodSetConfigOption {
		t.Fatalf("omp SetMode method = %q, want %q", req.Method, MethodSetConfigOption)
	}
	if req.Params["sessionId"] != "sid-1" || req.Params["configId"] != "mode" || req.Params["value"] != "plan" {
		t.Fatalf("omp SetMode params = %#v", req.Params)
	}
	if _, ok := req.Params["modeId"]; ok {
		t.Fatalf("omp SetMode must not send Kiro modeId param: %#v", req.Params)
	}
}

func TestSetModeDoesNotUpdateCurrentModeOnRPCError(t *testing.T) {
	pr, pw := io.Pipe()
	defer pr.Close()
	defer pw.Close()
	out := newRequestBuffer()
	tr := NewTransport(pr, out, 0)
	go func() { _ = tr.ReadLoop() }()

	a := &Agent{
		SessionID: "sid-1",
		transport: tr,
		profile:   kiroProfile(),
		sessResult: &SessionNewResult{Modes: &ModeState{
			CurrentModeID:  "default",
			AvailableModes: []ModeEntry{{ID: "default"}, {ID: "plan"}},
		}},
	}
	done := make(chan error, 1)
	go func() { done <- a.SetMode("plan") }()
	waitForWrittenRequest(t, out)
	if _, err := pw.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"Method not found"}}` + "\n")); err != nil {
		t.Fatalf("write rpc error: %v", err)
	}

	err := waitSetModeResult(t, done)
	if err == nil {
		t.Fatal("SetMode should return RPC error")
	}
	if got := a.CurrentModeID(); got != "default" {
		t.Fatalf("current mode updated on failure: got %q", got)
	}
}

type capturedRPCRequest struct {
	Method string                 `json:"method"`
	Params map[string]interface{} `json:"params"`
}

func captureSetModeRequest(t *testing.T, profile dialectProfile, modeID string) capturedRPCRequest {
	t.Helper()
	pr, pw := io.Pipe()
	defer pr.Close()
	defer pw.Close()
	out := newRequestBuffer()
	tr := NewTransport(pr, out, 0)
	go func() { _ = tr.ReadLoop() }()

	a := &Agent{
		SessionID: "sid-1",
		transport: tr,
		profile:   profile,
		sessResult: &SessionNewResult{Modes: &ModeState{
			CurrentModeID:  "default",
			AvailableModes: []ModeEntry{{ID: "default"}, {ID: modeID}},
		}},
	}
	done := make(chan error, 1)
	go func() { done <- a.SetMode(modeID) }()
	waitForWrittenRequest(t, out)
	if _, err := pw.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}` + "\n")); err != nil {
		t.Fatalf("write rpc response: %v", err)
	}
	if err := waitSetModeResult(t, done); err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	if got := a.CurrentModeID(); got != modeID {
		t.Fatalf("current mode = %q, want %q", got, modeID)
	}

	var req capturedRPCRequest
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &req); err != nil {
		t.Fatalf("request json %q: %v", string(out.Bytes()), err)
	}
	return req
}

func waitSetModeResult(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for SetMode")
		return nil
	}
}

type requestBuffer struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	written chan struct{}
	once    sync.Once
}

func newRequestBuffer() *requestBuffer {
	return &requestBuffer{written: make(chan struct{})}
}

func (b *requestBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	n, err := b.buf.Write(p)
	b.mu.Unlock()
	b.once.Do(func() { close(b.written) })
	return n, err
}

func (b *requestBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.buf.Bytes()...)
}

func waitForWrittenRequest(t *testing.T, out *requestBuffer) {
	t.Helper()
	select {
	case <-out.written:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for RPC request")
	}
}

func TestParseOmpSessionMapsConfigOptions(t *testing.T) {
	raw := json.RawMessage(`{
		"sessionId":"sid-1",
		"configOptions":[
			{"id":"mode","category":"mode","type":"select","currentValue":"default",
			 "options":[{"value":"default","name":"Default","description":"d"},{"value":"plan","name":"Plan","description":"p"}]},
			{"id":"model","category":"model","type":"select","currentValue":"openai-codex/gpt-5.5",
			 "options":[{"value":"openai-codex/gpt-5","name":"GPT-5","description":"g5"},{"value":"openai-codex/gpt-5.5","name":"GPT-5.5","description":"g55"}]}
		]
	}`)
	r := parseOmpSession(raw)
	if r.SessionID != "sid-1" {
		t.Fatalf("sessionId = %q", r.SessionID)
	}
	if r.Models == nil || r.Models.CurrentModelID != "openai-codex/gpt-5.5" || len(r.Models.AvailableModels) != 2 {
		t.Fatalf("models not mapped: %+v", r.Models)
	}
	if r.Models.AvailableModels[0].ModelID != "openai-codex/gpt-5" || r.Models.AvailableModels[0].Name != "GPT-5" {
		t.Fatalf("model entry not mapped: %+v", r.Models.AvailableModels[0])
	}
	if r.Modes == nil || r.Modes.CurrentModeID != "default" || len(r.Modes.AvailableModes) != 2 {
		t.Fatalf("modes not mapped: %+v", r.Modes)
	}
	if r.Modes.AvailableModes[1].ID != "plan" {
		t.Fatalf("mode entry not mapped: %+v", r.Modes.AvailableModes[1])
	}
}

func TestParseOmpSessionMapsGroupedConfigOptions(t *testing.T) {
	raw := json.RawMessage(`{
		"sessionId":"sid-2",
		"configOptions":[
			{"id":"model","category":"model","type":"select","currentValue":"openai-codex/gpt-5.5",
			 "options":[
				{"name":"OpenAI","options":[{"value":"openai-codex/gpt-5","name":"GPT-5","description":"g5"}]},
				{"name":"Hugging Face","options":[{"value":"huggingface/deepseek-ai/DeepSeek-R1","name":"DeepSeek-R1","description":"ds"}]}
			 ]}
		]
	}`)
	r := parseOmpSession(raw)
	if r.Models == nil || len(r.Models.AvailableModels) != 2 {
		t.Fatalf("grouped models not mapped: %+v", r.Models)
	}
	if r.Models.AvailableModels[1].ModelID != "huggingface/deepseek-ai/DeepSeek-R1" {
		t.Fatalf("grouped model entry not mapped: %+v", r.Models.AvailableModels[1])
	}
}

func TestParseOmpSessionMalformed(t *testing.T) {
	r := parseOmpSession(json.RawMessage(`not json`))
	if r == nil || r.SessionID != "" || r.Models != nil || r.Modes != nil {
		t.Fatalf("malformed payload should yield empty result, got %+v", r)
	}
}

// usage_update arrives via method "session/update" with update.sessionUpdate="usage_update".
func usageUpdate(size, used, cost float64) json.RawMessage {
	b, _ := json.Marshal(map[string]any{
		"sessionId": "sid",
		"update": map[string]any{
			"sessionUpdate": "usage_update",
			"size":          size,
			"used":          used,
			"cost":          map[string]any{"amount": cost, "currency": "USD"},
		},
	})
	return b
}

func TestOmpUsageUpdateDeltaCostAndContext(t *testing.T) {
	a := &Agent{}
	// Turn 1 baseline = 0; cumulative cost 0.114 → per-turn delta 0.114; ctx = 22670/272000*100.
	a.turnBaselineCost = a.cumulativeCost // = 0
	a.handleNotification(NotifUpdate, usageUpdate(272000, 22670, 0.114))
	m := a.TurnMetrics()
	if len(m.MeteringUsage) != 1 || m.MeteringUsage[0].Unit != "USD" {
		t.Fatalf("turn1 metering not set: %+v", m.MeteringUsage)
	}
	if !approx(m.MeteringUsage[0].Value, 0.114) {
		t.Fatalf("turn1 delta = %v, want 0.114", m.MeteringUsage[0].Value)
	}
	if !approx(m.ContextUsage, 22670.0/272000.0*100) {
		t.Fatalf("turn1 ctx = %v", m.ContextUsage)
	}
	if !approx(a.cumulativeCost, 0.114) {
		t.Fatalf("cumulative = %v", a.cumulativeCost)
	}

	// Turn 2: snapshot baseline (as Ask/AskAsyncMulti would), cumulative 0.127 → delta 0.013.
	a.turnMetrics = TurnMetrics{}
	a.turnBaselineCost = a.cumulativeCost // = 0.114
	a.handleNotification(NotifUpdate, usageUpdate(272000, 22690, 0.127))
	m = a.TurnMetrics()
	if len(m.MeteringUsage) != 1 || !approx(m.MeteringUsage[0].Value, 0.013) {
		t.Fatalf("turn2 delta = %+v, want ~0.013", m.MeteringUsage)
	}
}

func TestOmpUsageUpdateCompactionResetGuard(t *testing.T) {
	a := &Agent{cumulativeCost: 0.5, turnBaselineCost: 0.5}
	// cumulative drops below baseline (reset) → delta clamped to 0, no metering entry.
	a.handleNotification(NotifUpdate, usageUpdate(272000, 100, 0.02))
	m := a.TurnMetrics()
	if len(m.MeteringUsage) != 0 {
		t.Fatalf("reset should yield no metering entry, got %+v", m.MeteringUsage)
	}
}

func approx(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-9
}
func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
func join(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += " "
		}
		out += s
	}
	return out
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
