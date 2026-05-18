package channel

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/acp"
)

func TestCodeBlockState(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantIn  bool
		wantLng string
	}{
		{"no blocks", "hello world", false, ""},
		{"open block", "text\n```go\nfunc main() {", true, "go"},
		{"closed block", "```go\ncode\n```", false, ""},
		{"open no lang", "text\n```\ncode", true, ""},
		{"two blocks closed", "```\na\n```\n```python\nb\n```", false, ""},
		{"two blocks second open", "```\na\n```\n```js\nb", true, "js"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lang, inBlock := codeBlockState(tt.input)
			if inBlock != tt.wantIn {
				t.Errorf("inBlock = %v, want %v", inBlock, tt.wantIn)
			}
			if lang != tt.wantLng {
				t.Errorf("lang = %q, want %q", lang, tt.wantLng)
			}
		})
	}
}

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
- M5Bot id=1505737846013558834 mention=<@1505737846013558834>
- ChunBot id=1495737209616072815 mention=<@1495737209616072815>
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

type fakeWorkerAgent struct {
	mu          sync.Mutex
	cancelCalls int
}

func (f *fakeWorkerAgent) Ask(context.Context, string, func(string)) (string, error) {
	return "", nil
}

func (f *fakeWorkerAgent) AskAsync(string, acp.AsyncCallbacks) {}

func (f *fakeWorkerAgent) CancelPrompt() {
	f.mu.Lock()
	f.cancelCalls++
	f.mu.Unlock()
}

func (f *fakeWorkerAgent) ContextUsage() float64 { return 0 }

func (f *fakeWorkerAgent) OnReadErrorFunc(func(error)) {}

func (f *fakeWorkerAgent) RecentStderr() string { return "" }

func (f *fakeWorkerAgent) CancelCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cancelCalls
}

func TestWorkerCancelCurrentCancelsWithoutSignalingIdle(t *testing.T) {
	agent := &fakeWorkerAgent{}
	w := newWorker("ch1", agent, 1, 30, 1, 1440, nil, "")

	ctx, cancel := context.WithCancel(context.Background())
	w.cancelMu.Lock()
	w.cancelFn = cancel
	w.cancelMu.Unlock()

	w.CancelCurrent()

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

	w.CancelCurrent()

	if got := agent.CancelCalls(); got != 0 {
		t.Fatalf("cancel calls = %d, want 0", got)
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
