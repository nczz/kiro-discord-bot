package channel

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/acp"
	"github.com/nczz/kiro-discord-bot/audit"
	L "github.com/nczz/kiro-discord-bot/locale"
)

func TestSplitMessage_Short(t *testing.T) {
	msg := "hello world"
	parts := splitMessage(msg, 100)
	if len(parts) != 1 || parts[0] != msg {
		t.Fatalf("expected 1 part %q, got %v", msg, parts)
	}
}

func TestSplitMessage_ParagraphBoundary(t *testing.T) {
	// Build message with two paragraphs, total > limit
	p1 := strings.Repeat("a", 50)
	p2 := strings.Repeat("b", 50)
	msg := p1 + "\n\n" + p2
	parts := splitMessage(msg, 80)
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d: %v", len(parts), parts)
	}
	if parts[0] != p1 {
		t.Errorf("part[0] = %q, want %q", parts[0], p1)
	}
}

func TestSplitMessage_DemotesHeadings(t *testing.T) {
	msg := "# Title\n\nbody"
	parts := splitMessage(msg, 100)
	if len(parts) != 1 || strings.Contains(parts[0], "# Title") || !strings.Contains(parts[0], "**Title**") {
		t.Fatalf("parts = %#v, want heading demoted", parts)
	}
}

func TestSplitMessage_CodeBlockClosure(t *testing.T) {
	// Code block that spans the split point
	code := "```go\n" + strings.Repeat("x\n", 100) + "```"
	parts := splitMessage(code, 200)
	if len(parts) < 2 {
		t.Fatalf("expected multiple parts, got %d", len(parts))
	}
	// First part should end with ```
	if !strings.HasSuffix(parts[0], "```") {
		t.Errorf("first part should end with ```, got: ...%q", parts[0][len(parts[0])-20:])
	}
	// Second part should start with ```go
	if !strings.HasPrefix(parts[1], "```go\n") {
		t.Errorf("second part should start with ```go, got: %q...", parts[1][:20])
	}
}

func TestSplitMessage_NoCodeBlock(t *testing.T) {
	// Plain text split should not add ```
	msg := strings.Repeat("hello world\n", 200)
	parts := splitMessage(msg, 200)
	for i, p := range parts {
		if strings.Contains(p, "```") {
			t.Errorf("part[%d] should not contain ```: %q", i, p[:50])
		}
	}
}

func TestPromptVisibleBodySkipsDiscordMetadataBlocks(t *testing.T) {
	prompt := `[Discord bot environment] Your responses are automatically forwarded.
[Discord context] channel_id=1 guild_id=2 user=mxp

[Discord bot peers]
- BuildBot id=111111111111111111 mention=<@111111111111111111>
- ReviewBot id=333333333333333333 mention=<@333333333333333333>
[Discord bot handoff rules]
- Use the peer mention token exactly.

請幫我 review 這段內容`

	if got := promptVisibleBody(prompt); got != "請幫我 review 這段內容" {
		t.Fatalf("promptVisibleBody() = %q", got)
	}
	if got := promptSummary(prompt, 80); got != "請幫我 review 這段內容" {
		t.Fatalf("promptSummary() = %q", got)
	}
}

func TestCompactToolStartMessageHidesLongExecuteCommand(t *testing.T) {
	evt := acp.ToolCallEvent{
		Kind:  "execute",
		Title: `Running: ssh n200 "docker exec sync_epb-postgres-1 psql -U epb -d epb -c 'select * from very_large_table'"`,
	}
	got := CompactToolStartMessage("▶️", evt)
	if got != `▶️ Running: ssh n200 "docker exec sync\_epb-postgres-1 psql -U...` {
		t.Fatalf("CompactToolStartMessage() = %q", got)
	}
	if strings.Contains(got, "very_large_table") {
		t.Fatalf("compact execute message leaked long command tail: %q", got)
	}
}

func TestCompactToolStartMessageTruncatesLongNonExecuteTitle(t *testing.T) {
	evt := acp.ToolCallEvent{
		Kind:  "read",
		Title: strings.Repeat("a", 120),
	}
	got := CompactToolStartMessage("📖", evt)
	if len(got) > len("📖 ")+80 {
		t.Fatalf("compact message too long: len=%d msg=%q", len(got), got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected truncated message, got %q", got)
	}
}

func TestCompactToolStartMessageEscapesDiscordMarkdown(t *testing.T) {
	evt := acp.ToolCallEvent{
		Kind:  "read",
		Title: "# Heading **bold** > quote",
	}
	got := CompactToolStartMessage("📖", evt)
	want := "📖 \\# Heading \\*\\*bold\\*\\* \\> quote"
	if got != want {
		t.Fatalf("CompactToolStartMessage() = %q, want %q", got, want)
	}
}

func TestEscapeDiscordMarkdown(t *testing.T) {
	got := EscapeDiscordMarkdown("# H\n**bold** _em_ `code` > quote")
	want := "\\# H\n\\*\\*bold\\*\\* \\_em\\_ \\`code\\` \\> quote"
	if got != want {
		t.Fatalf("EscapeDiscordMarkdown() = %q, want %q", got, want)
	}
}

func TestBuildPromptContentEncodesImageData(t *testing.T) {
	dir := t.TempDir()
	imagePath := filepath.Join(dir, "sample.png")
	imageBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1, 2, 3}
	if err := os.WriteFile(imagePath, imageBytes, 0644); err != nil {
		t.Fatalf("write image: %v", err)
	}

	content := buildPromptContent("hello", []string{imagePath}, true)
	if len(content) != 2 {
		t.Fatalf("content len = %d, want 2", len(content))
	}
	if content[0].Type != "text" || content[0].Text != "hello" {
		t.Fatalf("text block = %+v", content[0])
	}
	img := content[1]
	if img.Type != "image" {
		t.Fatalf("image block type = %q", img.Type)
	}
	if img.MimeType != "image/png" {
		t.Fatalf("mime type = %q, want image/png", img.MimeType)
	}
	if img.Data != base64.StdEncoding.EncodeToString(imageBytes) {
		t.Fatal("image data was not base64 encoded from file bytes")
	}
}

func TestBuildPromptContentSkipsImagesWhenUnsupported(t *testing.T) {
	content := buildPromptContent("hello", []string{"/tmp/sample.png"}, false)
	if len(content) != 1 {
		t.Fatalf("content len = %d, want text-only", len(content))
	}
	if content[0].Type != "text" || content[0].Text != "hello" {
		t.Fatalf("text block = %+v", content[0])
	}
}

type fakeWorkerAgent struct {
	mu             sync.Mutex
	cancelCalls    int
	interruptCalls int
	callbacks      acp.AsyncCallbacks
	readError      func(error)
	metrics        acp.TurnMetrics
	askResponse    string
	askErr         error
}

type recordingAuditSink struct {
	mu     sync.Mutex
	events []audit.BotEvent
}

func (s *recordingAuditSink) RecordBotEvent(evt audit.BotEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, evt)
}

func (s *recordingAuditSink) Snapshot() []audit.BotEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]audit.BotEvent(nil), s.events...)
}

func (f *fakeWorkerAgent) Ask(context.Context, string, func(string)) (string, error) {
	return f.askResponse, f.askErr
}

func (f *fakeWorkerAgent) AskAsync(string, acp.AsyncCallbacks) {}

func (f *fakeWorkerAgent) AskAsyncMulti(_ []acp.PromptContent, cb acp.AsyncCallbacks) {
	f.mu.Lock()
	f.callbacks = cb
	f.mu.Unlock()
}

func (f *fakeWorkerAgent) SupportsImagePrompt() bool { return false }

func (f *fakeWorkerAgent) CancelPrompt() {
	f.mu.Lock()
	f.cancelCalls++
	f.mu.Unlock()
}

func (f *fakeWorkerAgent) Interrupt() error {
	f.mu.Lock()
	f.interruptCalls++
	f.mu.Unlock()
	return nil
}

func (f *fakeWorkerAgent) ContextUsage() float64 { return 0 }

func (f *fakeWorkerAgent) TurnMetrics() acp.TurnMetrics { return f.metrics }

func (f *fakeWorkerAgent) OnReadErrorFunc(fn func(error)) {
	f.mu.Lock()
	f.readError = fn
	f.mu.Unlock()
}

func (f *fakeWorkerAgent) RecentStderr() string { return "" }

func (f *fakeWorkerAgent) CancelCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cancelCalls
}

func (f *fakeWorkerAgent) InterruptCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.interruptCalls
}

func (f *fakeWorkerAgent) Callbacks() acp.AsyncCallbacks {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.callbacks
}

func (f *fakeWorkerAgent) TriggerReadError(err error) {
	f.mu.Lock()
	fn := f.readError
	f.mu.Unlock()
	if fn != nil {
		fn(err)
	}
}

type recordingRoundTripper struct {
	mu       sync.Mutex
	requests []string
	bodies   []string
}

func (rt *recordingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	body := ""
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		body = string(b)
	}
	rt.mu.Lock()
	rt.requests = append(rt.requests, req.Method+" "+req.URL.Path)
	rt.bodies = append(rt.bodies, body)
	rt.mu.Unlock()

	status := http.StatusNoContent
	respBody := ""
	if req.Method == http.MethodPost && strings.Contains(req.URL.Path, "/threads") {
		status = http.StatusOK
		respBody = `{"id":"thread-1","channel_id":"thread-1","parent_id":"ch1","name":"thread","type":11}`
	}
	if req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/messages") {
		status = http.StatusOK
		respBody = `{"id":"reply-1","channel_id":"ch1","content":"ok"}`
	}
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(respBody)),
		Request:    req,
	}, nil
}

func (rt *recordingRoundTripper) Snapshot() ([]string, []string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	reqs := append([]string(nil), rt.requests...)
	bodies := append([]string(nil), rt.bodies...)
	return reqs, bodies
}

type failingRoundTripper struct{}

func (rt failingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusInternalServerError,
		Status:     http.StatusText(http.StatusInternalServerError),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"message":"forced failure"}`)),
		Request:    req,
	}, nil
}

func testDiscordSession(rt http.RoundTripper) *discordgo.Session {
	ds, _ := discordgo.New("Bot test")
	ds.Client = &http.Client{Transport: rt}
	return ds
}

func TestSendLongThreadReportsDeliveryResult(t *testing.T) {
	sentCount, err := SendLongThread(testDiscordSession(&recordingRoundTripper{}), "thread-1", "hello")
	if err != nil {
		t.Fatalf("SendLongThread success err = %v", err)
	}
	if sentCount != 1 {
		t.Fatalf("sent count = %d, want 1", sentCount)
	}

	sentCount, err = SendLongThread(testDiscordSession(failingRoundTripper{}), "thread-1", "hello")
	if err == nil {
		t.Fatal("SendLongThread failure err = nil, want error")
	}
	if sentCount != 0 {
		t.Fatalf("sent count = %d, want 0 on failure", sentCount)
	}
}

func TestWorkerCancelCurrentCancelsWithoutSignalingIdle(t *testing.T) {
	agent := &fakeWorkerAgent{}
	w := newWorker("ch1", agent, 1, 30, 1, 1440, nil, "")

	ctx, cancel := context.WithCancel(context.Background())
	w.cancelMu.Lock()
	w.cancelFn = cancel
	w.cancelMu.Unlock()

	if !w.CancelCurrent() {
		t.Fatal("CancelCurrent returned false for active job")
	}

	if err := ctx.Err(); err != context.Canceled {
		t.Fatalf("ctx err = %v, want context.Canceled", err)
	}
	if got := agent.CancelCalls(); got != 1 {
		t.Fatalf("cancel calls = %d, want 1", got)
	}
	select {
	case <-w.idleCh:
		t.Fatal("CancelCurrent must not signal idle before OnComplete")
	default:
	}
}

func TestIsThreadAlreadyCreated(t *testing.T) {
	err := &discordgo.RESTError{
		Response: &http.Response{StatusCode: http.StatusBadRequest},
		Message:  &discordgo.APIErrorMessage{Code: discordgo.ErrCodeThreadAlreadyCreatedForThisMessage},
	}
	if !isThreadAlreadyCreated(err) {
		t.Fatal("expected thread already created error")
	}
	if isThreadAlreadyCreated(context.Canceled) {
		t.Fatal("did not expect non-REST error to match")
	}
}

func TestWorkerCancelCurrentNoActiveJob(t *testing.T) {
	agent := &fakeWorkerAgent{}
	w := newWorker("ch1", agent, 1, 30, 1, 1440, nil, "")

	if w.CancelCurrent() {
		t.Fatal("CancelCurrent returned true without active job")
	}

	if got := agent.CancelCalls(); got != 0 {
		t.Fatalf("cancel calls = %d, want 0", got)
	}
}

func TestWorkerIsActive(t *testing.T) {
	agent := &fakeWorkerAgent{}
	w := newWorker("ch1", agent, 1, 30, 1, 1440, nil, "")

	if w.IsActive() {
		t.Fatal("new worker should not be active")
	}

	_, cancel := context.WithCancel(context.Background())
	w.cancelMu.Lock()
	w.cancelFn = cancel
	w.currentJobSeq = 1
	w.currentJobActive = true
	w.cancelMu.Unlock()

	if !w.IsActive() {
		t.Fatal("worker with active job context should be active")
	}

	w.cancelMu.Lock()
	w.cancelFn = nil
	w.currentJobActive = false
	w.cancelMu.Unlock()

	if w.IsActive() {
		t.Fatal("worker with cleared job context should not be active")
	}
}

func TestWorkerSignalIdleDoesNotReleaseNextJobWhenStopped(t *testing.T) {
	agent := &fakeWorkerAgent{}
	w := newWorker("ch1", agent, 2, 30, 1, 1440, nil, "")
	w.OnIdleFunc(func() bool {
		w.Stop()
		return true
	})

	w.signalIdle()

	select {
	case <-w.idleCh:
		t.Fatal("signalIdle released idle token after stop")
	default:
	}
}

func TestWorkerSignalIdleDoesNotReleaseTokenAfterStop(t *testing.T) {
	agent := &fakeWorkerAgent{}
	w := newWorker("ch1", agent, 2, 30, 1, 1440, nil, "")

	w.Stop()
	w.signalIdle()

	select {
	case <-w.idleCh:
		t.Fatal("signalIdle released idle token after worker was stopped")
	default:
	}
}

func TestWorkerWaitIdleRejectsTokenAfterStop(t *testing.T) {
	agent := &fakeWorkerAgent{}
	w := newWorker("ch1", agent, 2, 30, 1, 1440, nil, "")

	w.idleCh <- struct{}{}
	w.Stop()

	if w.waitIdle() {
		t.Fatal("waitIdle accepted idle token after worker was stopped")
	}
}

func TestWorkerInterruptCurrentEscalatesWhenSameJobStillActive(t *testing.T) {
	agent := &fakeWorkerAgent{}
	w := newWorker("ch1", agent, 1, 30, 1, 1440, nil, "")

	ctx, cancel := context.WithCancel(context.Background())
	w.cancelMu.Lock()
	w.cancelFn = cancel
	w.currentJobID = "m1"
	w.currentJobSeq = 1
	w.cancelMu.Unlock()

	if !w.InterruptCurrent(5 * time.Millisecond) {
		t.Fatal("InterruptCurrent returned false for active job")
	}
	if err := ctx.Err(); err != context.Canceled {
		t.Fatalf("ctx err = %v, want context.Canceled", err)
	}
	if got := agent.CancelCalls(); got != 1 {
		t.Fatalf("cancel calls = %d, want 1", got)
	}

	deadline := time.After(200 * time.Millisecond)
	for {
		if got := agent.InterruptCalls(); got == 1 {
			return
		} else if got > 1 {
			t.Fatalf("interrupt calls = %d, want 1", got)
		}
		select {
		case <-deadline:
			t.Fatalf("interrupt calls = %d, want 1", agent.InterruptCalls())
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func TestWorkerInterruptCurrentDeduplicatesPendingEscalation(t *testing.T) {
	agent := &fakeWorkerAgent{}
	w := newWorker("ch1", agent, 1, 30, 1, 1440, nil, "")

	_, cancel := context.WithCancel(context.Background())
	w.cancelMu.Lock()
	w.cancelFn = cancel
	w.currentJobID = "m1"
	w.currentJobSeq = 1
	w.cancelMu.Unlock()

	if !w.InterruptCurrent(10 * time.Millisecond) {
		t.Fatal("first InterruptCurrent returned false for active job")
	}
	if !w.InterruptCurrent(10 * time.Millisecond) {
		t.Fatal("second InterruptCurrent returned false for active job")
	}
	if got := agent.CancelCalls(); got != 2 {
		t.Fatalf("cancel calls = %d, want 2", got)
	}

	time.Sleep(50 * time.Millisecond)
	if got := agent.InterruptCalls(); got != 1 {
		t.Fatalf("interrupt calls = %d, want 1", got)
	}
}

func TestWorkerInterruptCurrentDoesNotEscalateAfterJobClears(t *testing.T) {
	agent := &fakeWorkerAgent{}
	w := newWorker("ch1", agent, 1, 30, 1, 1440, nil, "")

	_, cancel := context.WithCancel(context.Background())
	w.cancelMu.Lock()
	w.cancelFn = cancel
	w.currentJobID = "m1"
	w.currentJobSeq = 1
	w.cancelMu.Unlock()

	if !w.InterruptCurrent(20 * time.Millisecond) {
		t.Fatal("InterruptCurrent returned false for active job")
	}

	w.cancelMu.Lock()
	w.cancelFn = nil
	w.currentJobID = ""
	w.cancelMu.Unlock()

	time.Sleep(50 * time.Millisecond)
	if got := agent.InterruptCalls(); got != 0 {
		t.Fatalf("interrupt calls = %d, want 0", got)
	}
}

func TestWorkerInterruptCurrentDoesNotEscalateDifferentJob(t *testing.T) {
	agent := &fakeWorkerAgent{}
	w := newWorker("ch1", agent, 1, 30, 1, 1440, nil, "")

	_, cancel := context.WithCancel(context.Background())
	w.cancelMu.Lock()
	w.cancelFn = cancel
	w.currentJobID = "m1"
	w.currentJobSeq = 1
	w.cancelMu.Unlock()

	if !w.InterruptCurrent(20 * time.Millisecond) {
		t.Fatal("InterruptCurrent returned false for active job")
	}

	_, nextCancel := context.WithCancel(context.Background())
	w.cancelMu.Lock()
	w.cancelFn = nextCancel
	w.currentJobID = "m2"
	w.currentJobSeq = 2
	w.cancelMu.Unlock()

	time.Sleep(50 * time.Millisecond)
	if got := agent.InterruptCalls(); got != 0 {
		t.Fatalf("interrupt calls = %d, want 0", got)
	}
}

func TestWorkerInterruptCurrentNoActiveJob(t *testing.T) {
	agent := &fakeWorkerAgent{}
	w := newWorker("ch1", agent, 1, 30, 1, 1440, nil, "")

	if w.InterruptCurrent(0) {
		t.Fatal("InterruptCurrent returned true without active job")
	}
	if got := agent.CancelCalls(); got != 0 {
		t.Fatalf("cancel calls = %d, want 0", got)
	}
	if got := agent.InterruptCalls(); got != 0 {
		t.Fatalf("interrupt calls = %d, want 0", got)
	}
}

func TestWorkerEnqueueFull(t *testing.T) {
	w := newWorker("ch1", &fakeWorkerAgent{}, 1, 30, 1, 1440, nil, "")
	if err := w.Enqueue(&Job{MessageID: "m1"}); err != nil {
		t.Fatalf("first enqueue: %v", err)
	}
	if err := w.Enqueue(&Job{MessageID: "m2"}); err == nil {
		t.Fatal("second enqueue should fail when buffer is full")
	}
}

func TestWorkerInlineDeliverySendsOnlyFinalReplyAndReplacesQueuedReaction(t *testing.T) {
	L.Load("en")
	rt := &recordingRoundTripper{}
	ds := testDiscordSession(rt)
	agent := &fakeWorkerAgent{metrics: acp.TurnMetrics{
		MeteringUsage:  []acp.MeteringItem{{Value: 0.22, Unit: "credit"}},
		TurnDurationMs: 5000,
		ContextUsage:   11,
	}}
	w := newWorker("ch1", agent, 1, 30, 1, 1440, nil, "")

	w.executeInline(&Job{
		ChannelID:    "ch1",
		MessageID:    "m1",
		Prompt:       "hello",
		Session:      ds,
		DeliveryMode: DeliveryInline,
	})
	cb := agent.Callbacks()
	if cb.OnComplete == nil || cb.OnToolCall == nil {
		t.Fatal("expected inline callbacks to be registered")
	}
	cb.OnToolCall(acp.ToolCallEvent{Kind: "execute", Title: "Running tool"})
	cb.OnComplete("final response", nil)

	reqs, bodies := rt.Snapshot()
	var messagePosts int
	var removedQueued, addedDone bool
	for i, req := range reqs {
		if strings.HasPrefix(req, "POST /api/") && strings.HasSuffix(req, "/channels/ch1/messages") {
			messagePosts++
			if !strings.Contains(bodies[i], "final response") {
				t.Fatalf("final reply body = %q, want final response", bodies[i])
			}
			if !strings.Contains(bodies[i], "⚡ 0.22 credit · 5.0s · ctx 11%") {
				t.Fatalf("final reply body = %q, want metrics footer", bodies[i])
			}
		}
		if strings.HasPrefix(req, "DELETE ") && strings.Contains(req, "/reactions/⏳/@me") {
			removedQueued = true
		}
		if strings.HasPrefix(req, "PUT ") && strings.Contains(req, "/reactions/✅/@me") {
			addedDone = true
		}
	}
	if messagePosts != 1 {
		t.Fatalf("message post count = %d, want 1; requests=%v", messagePosts, reqs)
	}
	if !removedQueued {
		t.Fatalf("expected inline pulse to remove queued reaction; requests=%v", reqs)
	}
	if !addedDone {
		t.Fatalf("expected inline pulse to add final done reaction; requests=%v", reqs)
	}
}

func TestWorkerThreadDeliveryAppendsMetricsToFinalResponse(t *testing.T) {
	L.Load("en")
	rt := &recordingRoundTripper{}
	ds := testDiscordSession(rt)
	agent := &fakeWorkerAgent{metrics: acp.TurnMetrics{
		MeteringUsage:  []acp.MeteringItem{{Value: 0.22, Unit: "credit"}},
		TurnDurationMs: 5000,
		ContextUsage:   11,
	}}
	w := newWorker("ch1", agent, 1, 30, 1, 1440, nil, "")

	w.execute(&Job{
		ChannelID: "ch1",
		ThreadID:  "thread-1",
		MessageID: "m1",
		Prompt:    "hello",
		Session:   ds,
	})
	cb := agent.Callbacks()
	if cb.OnComplete == nil {
		t.Fatal("expected thread callbacks to be registered")
	}
	cb.OnComplete("final response", nil)

	reqs, bodies := rt.Snapshot()
	var finalPosts int
	for i, req := range reqs {
		if !strings.HasPrefix(req, "POST ") || !strings.Contains(req, "/channels/thread-1/messages") {
			continue
		}
		if strings.Contains(bodies[i], "final response") {
			finalPosts++
			if !strings.Contains(bodies[i], "⚡ 0.22 credit · 5.0s · ctx 11%") {
				t.Fatalf("thread final body = %q, want metrics footer", bodies[i])
			}
		}
		if strings.Contains(bodies[i], "⚡ 0.22 credit · 5.0s · ctx 11%") && !strings.Contains(bodies[i], "final response") {
			t.Fatalf("metrics footer was sent as a separate thread message: %q", bodies[i])
		}
	}
	if finalPosts != 1 {
		t.Fatalf("thread final posts = %d, want 1; requests=%v bodies=%v", finalPosts, reqs, bodies)
	}
}

func TestWorkerThreadDrainsBotToolsBeforeFinalResponse(t *testing.T) {
	L.Load("en")
	rt := &recordingRoundTripper{}
	ds := testDiscordSession(rt)
	agent := &fakeWorkerAgent{}
	w := newWorker("ch1", agent, 1, 30, 1, 1440, nil, "")
	statePath := filepath.Join(t.TempDir(), "target.json")
	w.SetBotToolsTargetStatePath(statePath)

	var drainedTarget string
	w.OnBeforeFinalResponseFunc(func(targetChannelID string) int {
		drainedTarget = targetChannelID
		raw, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatalf("target state missing before final response: %v", err)
		}
		if !strings.Contains(string(raw), `"target_channel_id":"thread-1"`) {
			t.Fatalf("target state = %s, want thread-1", raw)
		}
		_, bodies := rt.Snapshot()
		for _, body := range bodies {
			if strings.Contains(body, "final response") {
				t.Fatalf("final response was sent before safe egress drain: %q", body)
			}
		}
		return 0
	})

	w.execute(&Job{
		ChannelID: "ch1",
		ThreadID:  "thread-1",
		MessageID: "m1",
		Prompt:    "hello",
		Session:   ds,
	})
	cb := agent.Callbacks()
	if cb.OnComplete == nil {
		t.Fatal("expected thread callbacks to be registered")
	}
	cb.OnComplete("final response", nil)

	if drainedTarget != "thread-1" {
		t.Fatalf("drained target = %q, want thread-1", drainedTarget)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("target state should be cleared after final response, stat err=%v", err)
	}
}

func TestWorkerAutoCreatedThreadUpdatesBotToolsTargetState(t *testing.T) {
	L.Load("en")
	rt := &recordingRoundTripper{}
	ds := testDiscordSession(rt)
	agent := &fakeWorkerAgent{}
	w := newWorker("ch1", agent, 1, 30, 1, 1440, nil, "")
	statePath := filepath.Join(t.TempDir(), "target.json")
	w.SetBotToolsTargetStatePath(statePath)

	var createdThread string
	w.OnThreadCreatedFunc(func(threadID string, mentionOnly bool) {
		createdThread = threadID
	})
	var drainedTarget string
	w.OnBeforeFinalResponseFunc(func(targetChannelID string) int {
		drainedTarget = targetChannelID
		raw, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatalf("target state missing before final response: %v", err)
		}
		if !strings.Contains(string(raw), `"target_channel_id":"thread-1"`) {
			t.Fatalf("target state = %s, want thread-1", raw)
		}
		return 0
	})

	w.execute(&Job{
		ChannelID: "ch1",
		MessageID: "m1",
		Prompt:    "hello",
		Session:   ds,
	})
	cb := agent.Callbacks()
	if cb.OnComplete == nil {
		t.Fatal("expected thread callbacks to be registered")
	}
	cb.OnComplete("final response", nil)

	if createdThread != "thread-1" {
		t.Fatalf("created thread = %q, want thread-1", createdThread)
	}
	if drainedTarget != "thread-1" {
		t.Fatalf("drained target = %q, want thread-1", drainedTarget)
	}
}

func TestWorkerSuppressesGenericKiroErrorAfterSafeEgressDelivery(t *testing.T) {
	L.Load("en")
	rt := &recordingRoundTripper{}
	ds := testDiscordSession(rt)
	agent := &fakeWorkerAgent{}
	w := newWorker("ch1", agent, 1, 30, 1, 1440, nil, "")

	var drainedTarget string
	w.OnBeforeFinalResponseFunc(func(targetChannelID string) int {
		drainedTarget = targetChannelID
		return 1
	})

	w.execute(&Job{
		ChannelID: "ch1",
		ThreadID:  "thread-1",
		MessageID: "m1",
		Prompt:    "hello",
		Session:   ds,
	})
	cb := agent.Callbacks()
	if cb.OnComplete == nil {
		t.Fatal("expected thread callbacks to be registered")
	}
	cb.OnComplete("", errors.New(`rpc error -32603: Internal error | data: "Kiro failed to generate a response"`))

	if drainedTarget != "thread-1" {
		t.Fatalf("drained target = %q, want thread-1", drainedTarget)
	}
	_, bodies := rt.Snapshot()
	for _, body := range bodies {
		if strings.Contains(body, "Kiro failed to generate a response") || strings.Contains(body, "❌") {
			t.Fatalf("generic Kiro error should be suppressed after safe egress delivery, body=%q", body)
		}
	}
}

func TestWorkerInlineErrorAppendsMetricsFooter(t *testing.T) {
	L.Load("en")
	rt := &recordingRoundTripper{}
	ds := testDiscordSession(rt)
	agent := &fakeWorkerAgent{metrics: acp.TurnMetrics{
		MeteringUsage:  []acp.MeteringItem{{Value: 0.22, Unit: "credit"}},
		TurnDurationMs: 5000,
		ContextUsage:   11,
	}}
	w := newWorker("ch1", agent, 1, 30, 1, 1440, nil, "")

	w.executeInline(&Job{
		ChannelID:    "ch1",
		MessageID:    "m1",
		Prompt:       "hello",
		Session:      ds,
		DeliveryMode: DeliveryInline,
	})
	cb := agent.Callbacks()
	if cb.OnComplete == nil {
		t.Fatal("expected inline callbacks to be registered")
	}
	cb.OnComplete("", context.Canceled)

	reqs, bodies := rt.Snapshot()
	for i, req := range reqs {
		if strings.HasPrefix(req, "POST /api/") && strings.HasSuffix(req, "/channels/ch1/messages") {
			if !strings.Contains(bodies[i], "❌") || !strings.Contains(bodies[i], "⚡ 0.22 credit · 5.0s · ctx 11%") {
				t.Fatalf("inline error body = %q, want error with metrics footer", bodies[i])
			}
			return
		}
	}
	t.Fatalf("expected inline error reply; requests=%v bodies=%v", reqs, bodies)
}

func TestWorkerThreadErrorAppendsMetricsFooter(t *testing.T) {
	L.Load("en")
	rt := &recordingRoundTripper{}
	ds := testDiscordSession(rt)
	agent := &fakeWorkerAgent{metrics: acp.TurnMetrics{
		MeteringUsage:  []acp.MeteringItem{{Value: 0.22, Unit: "credit"}},
		TurnDurationMs: 5000,
		ContextUsage:   11,
	}}
	w := newWorker("ch1", agent, 1, 30, 1, 1440, nil, "")

	w.execute(&Job{
		ChannelID: "ch1",
		ThreadID:  "thread-1",
		MessageID: "m1",
		Prompt:    "hello",
		Session:   ds,
	})
	cb := agent.Callbacks()
	if cb.OnComplete == nil {
		t.Fatal("expected thread callbacks to be registered")
	}
	cb.OnComplete("", context.Canceled)

	reqs, bodies := rt.Snapshot()
	for i, req := range reqs {
		if strings.HasPrefix(req, "POST ") && strings.Contains(req, "/channels/thread-1/messages") && strings.Contains(bodies[i], "❌") {
			if !strings.Contains(bodies[i], "⚡ 0.22 credit · 5.0s · ctx 11%") {
				t.Fatalf("thread error body = %q, want metrics footer", bodies[i])
			}
			return
		}
	}
	t.Fatalf("expected thread error reply; requests=%v bodies=%v", reqs, bodies)
}

func TestWorkerInlineReadErrorSendsFinalErrorReply(t *testing.T) {
	L.Load("en")
	rt := &recordingRoundTripper{}
	ds := testDiscordSession(rt)
	agent := &fakeWorkerAgent{}
	w := newWorker("ch1", agent, 1, 30, 1, 1440, nil, "")

	w.executeInline(&Job{
		ChannelID:    "ch1",
		MessageID:    "m1",
		Prompt:       "hello",
		Session:      ds,
		DeliveryMode: DeliveryInline,
	})
	agent.TriggerReadError(context.Canceled)

	reqs, bodies := rt.Snapshot()
	var messagePosts int
	var body string
	var addedWarn bool
	for i, req := range reqs {
		if strings.HasPrefix(req, "POST /api/") && strings.HasSuffix(req, "/channels/ch1/messages") {
			messagePosts++
			body = bodies[i]
		}
		if strings.HasPrefix(req, "PUT ") && strings.Contains(req, "/reactions/⚠️/@me") {
			addedWarn = true
		}
	}
	if messagePosts != 1 {
		t.Fatalf("message post count = %d, want 1; requests=%v", messagePosts, reqs)
	}
	if !strings.Contains(body, "Agent communication lost") {
		t.Fatalf("read error body = %q, want agent communication error", body)
	}
	if !addedWarn {
		t.Fatalf("expected warning reaction; requests=%v", reqs)
	}
}

func TestMetricsMetadataIncludesCostDurationAndContext(t *testing.T) {
	got := MetricsMetadata(acp.TurnMetrics{
		MeteringUsage: []acp.MeteringItem{
			{Value: 0.22, Unit: "credit"},
			{Value: 0.03, Unit: "credits"},
			{Value: 10, Unit: "tokens"},
		},
		TurnDurationMs: 5000,
		ContextUsage:   11,
	})
	if got["duration_ms"] != int64(5000) {
		t.Fatalf("duration_ms = %#v, want 5000", got["duration_ms"])
	}
	if got["context_usage"] != float64(11) {
		t.Fatalf("context_usage = %#v, want 11", got["context_usage"])
	}
	if math.Abs(got["credits"].(float64)-0.25) > 0.000001 {
		t.Fatalf("credits = %#v, want 0.25", got["credits"])
	}
	if _, ok := got["metering_usage"].([]acp.MeteringItem); !ok {
		t.Fatalf("metering_usage = %#v, want []acp.MeteringItem", got["metering_usage"])
	}
}

func TestFormatMetricsFooterSumsCreditItems(t *testing.T) {
	L.Load("en")
	got := FormatMetricsFooter(acp.TurnMetrics{
		MeteringUsage: []acp.MeteringItem{
			{Value: 10, Unit: "tokens"},
			{Value: 0.22, Unit: "credit"},
			{Value: 0.03, Unit: "credits"},
		},
		TurnDurationMs: 5000,
		ContextUsage:   11,
	})
	if !strings.Contains(got, "⚡ 0.25 credit · 5.0s · ctx 11%") {
		t.Fatalf("footer = %q, want summed credit footer", got)
	}
}

func TestFormatMetricsFooterIncludesContextOnly(t *testing.T) {
	L.Load("en")
	got := FormatMetricsFooter(acp.TurnMetrics{ContextUsage: 11})
	if !strings.Contains(got, "⚡ ctx 11%") {
		t.Fatalf("footer = %q, want context-only footer", got)
	}
}

func TestWorkerFallbackAppendsMetricsFooter(t *testing.T) {
	L.Load("en")
	rt := &recordingRoundTripper{}
	ds := testDiscordSession(rt)
	agent := &fakeWorkerAgent{
		askResponse: "fallback response",
		metrics: acp.TurnMetrics{
			MeteringUsage:  []acp.MeteringItem{{Value: 0.22, Unit: "credit"}},
			TurnDurationMs: 5000,
			ContextUsage:   11,
		},
	}
	w := newWorker("ch1", agent, 1, 30, 1, 1440, nil, "")

	w.executeFallback(&Job{
		ChannelID: "ch1",
		MessageID: "m1",
		Prompt:    "hello",
		Session:   ds,
	})

	reqs, bodies := rt.Snapshot()
	for i, req := range reqs {
		if strings.HasPrefix(req, "PATCH ") && strings.Contains(req, "/channels/ch1/messages/reply-1") {
			if !strings.Contains(bodies[i], "fallback response") {
				t.Fatalf("fallback edit body = %q, want response", bodies[i])
			}
			if !strings.Contains(bodies[i], "⚡ 0.22 credit · 5.0s · ctx 11%") {
				t.Fatalf("fallback edit body = %q, want metrics footer", bodies[i])
			}
			return
		}
	}
	t.Fatalf("expected fallback edit request; requests=%v", reqs)
}

func TestWorkerFallbackEmptyResponseStillAppendsMetricsFooter(t *testing.T) {
	L.Load("en")
	rt := &recordingRoundTripper{}
	ds := testDiscordSession(rt)
	agent := &fakeWorkerAgent{
		metrics: acp.TurnMetrics{
			MeteringUsage:  []acp.MeteringItem{{Value: 0.22, Unit: "credit"}},
			TurnDurationMs: 5000,
			ContextUsage:   11,
		},
	}
	w := newWorker("ch1", agent, 1, 30, 1, 1440, nil, "")

	w.executeFallback(&Job{
		ChannelID: "ch1",
		MessageID: "m1",
		Prompt:    "hello",
		Session:   ds,
	})

	reqs, bodies := rt.Snapshot()
	for i, req := range reqs {
		if strings.HasPrefix(req, "PATCH ") && strings.Contains(req, "/channels/ch1/messages/reply-1") {
			if !strings.Contains(bodies[i], L.Get("worker.empty_response")) || !strings.Contains(bodies[i], "⚡ 0.22 credit · 5.0s · ctx 11%") {
				t.Fatalf("fallback empty body = %q, want empty response with metrics footer", bodies[i])
			}
			return
		}
	}
	t.Fatalf("expected fallback edit request; requests=%v bodies=%v", reqs, bodies)
}

func TestWorkerFallbackErrorAppendsMetricsFooter(t *testing.T) {
	L.Load("en")
	rt := &recordingRoundTripper{}
	ds := testDiscordSession(rt)
	agent := &fakeWorkerAgent{
		askErr: context.Canceled,
		metrics: acp.TurnMetrics{
			MeteringUsage:  []acp.MeteringItem{{Value: 0.22, Unit: "credit"}},
			TurnDurationMs: 5000,
			ContextUsage:   11,
		},
	}
	w := newWorker("ch1", agent, 1, 30, 1, 1440, nil, "")

	w.executeFallback(&Job{
		ChannelID: "ch1",
		MessageID: "m1",
		Prompt:    "hello",
		Session:   ds,
	})

	reqs, bodies := rt.Snapshot()
	for i, req := range reqs {
		if strings.HasPrefix(req, "PATCH ") && strings.Contains(req, "/channels/ch1/messages/reply-1") {
			if !strings.Contains(bodies[i], "❌") || !strings.Contains(bodies[i], "⚡ 0.22 credit · 5.0s · ctx 11%") {
				t.Fatalf("fallback error body = %q, want error with metrics footer", bodies[i])
			}
			return
		}
	}
	t.Fatalf("expected fallback error edit request; requests=%v bodies=%v", reqs, bodies)
}

func TestWorkerInlineAuditStoresPureResponseWithoutMetricsFooter(t *testing.T) {
	L.Load("en")
	rt := &recordingRoundTripper{}
	ds := testDiscordSession(rt)
	agent := &fakeWorkerAgent{metrics: acp.TurnMetrics{
		MeteringUsage:  []acp.MeteringItem{{Value: 0.22, Unit: "credit"}},
		TurnDurationMs: 5000,
		ContextUsage:   11,
	}}
	sink := &recordingAuditSink{}
	w := newWorker("ch1", agent, 1, 30, 1, 1440, nil, "model-1")
	w.SetAuditSink(sink)

	w.executeInline(&Job{
		ChannelID:    "ch1",
		MessageID:    "m1",
		Prompt:       "hello",
		Session:      ds,
		DeliveryMode: DeliveryInline,
	})
	cb := agent.Callbacks()
	cb.OnComplete("final response", nil)

	for _, evt := range sink.Snapshot() {
		if evt.Type == "agent_response_sent" {
			if evt.Content != "final response" {
				t.Fatalf("audit response content = %q, want pure response", evt.Content)
			}
			if evt.Metadata["credits"] != float64(0.22) {
				t.Fatalf("audit metadata credits = %#v, want 0.22", evt.Metadata["credits"])
			}
			return
		}
	}
	t.Fatal("expected agent_response_sent audit event")
}

func TestWorkerInlineLoggerStoresResponseWithoutMetricsFooter(t *testing.T) {
	L.Load("en")
	rt := &recordingRoundTripper{}
	ds := testDiscordSession(rt)
	logger := NewChatLogger(t.TempDir())
	defer logger.Close()
	agent := &fakeWorkerAgent{metrics: acp.TurnMetrics{
		MeteringUsage:  []acp.MeteringItem{{Value: 0.22, Unit: "credit"}},
		TurnDurationMs: 5000,
		ContextUsage:   11,
	}}
	w := newWorker("ch1", agent, 1, 30, 1, 1440, logger, "model-1")

	w.executeInline(&Job{
		ChannelID:    "ch1",
		MessageID:    "m1",
		Prompt:       "hello",
		Session:      ds,
		DeliveryMode: DeliveryInline,
	})
	cb := agent.Callbacks()
	cb.OnComplete("final response", nil)

	history := logger.RecentHistory("ch1", 10)
	var assistantEntries []ChatEntry
	for _, entry := range history {
		if entry.Role == "assistant" {
			assistantEntries = append(assistantEntries, entry)
		}
	}
	if len(assistantEntries) != 1 {
		t.Fatalf("assistant history len = %d, want 1; history=%+v", len(assistantEntries), history)
	}
	if assistantEntries[0].Content != "final response" {
		t.Fatalf("assistant history content = %q, want response without metrics footer", assistantEntries[0].Content)
	}
}
