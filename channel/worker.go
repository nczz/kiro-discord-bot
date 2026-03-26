package channel

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/acp"
)

// Job represents a single user message to be processed.
type Job struct {
	ChannelID   string
	MessageID   string // original user message ID (for reactions)
	Prompt      string
	Session     *discordgo.Session // discord session for sending replies
	UserID      string
	Username    string
	Attachments []string
}

// Worker manages a per-channel job queue and executes jobs sequentially.
type Worker struct {
	channelID       string
	agentName       string
	queue           chan *Job
	acpClient       *acp.Client
	discord         *discordgo.Session
	askTimeoutSec   int
	streamUpdateSec int
	stopCh          chan struct{}
	once            sync.Once
	logger          *ChatLogger
	model           string
}

func NewWorker(channelID, agentName string, bufSize, askTimeoutSec, streamUpdateSec int, acpClient *acp.Client, logger *ChatLogger, model string) *Worker {
	return &Worker{
		channelID:       channelID,
		agentName:       agentName,
		queue:           make(chan *Job, bufSize),
		acpClient:       acpClient,
		askTimeoutSec:   askTimeoutSec,
		streamUpdateSec: streamUpdateSec,
		stopCh:          make(chan struct{}),
		logger:          logger,
		model:           model,
	}
}

func (w *Worker) Enqueue(job *Job) error {
	select {
	case w.queue <- job:
		return nil
	default:
		return fmt.Errorf("queue full")
	}
}

func (w *Worker) QueueLen() int {
	return len(w.queue)
}

func (w *Worker) Start() {
	w.once.Do(func() {
		go w.run()
	})
}

func (w *Worker) Stop() {
	close(w.stopCh)
}

func (w *Worker) run() {
	for {
		select {
		case <-w.stopCh:
			return
		case job := <-w.queue:
			w.execute(job)
		}
	}
}

func (w *Worker) execute(job *Job) {
	ds := job.Session

	// Log user message
	if w.logger != nil {
		w.logger.Log(w.channelID, ChatEntry{
			Role:        "user",
			UserID:      job.UserID,
			Username:    job.Username,
			MessageID:   job.MessageID,
			Content:     job.Prompt,
			Attachments: job.Attachments,
		})
	}

	// ⏳ → 🔄
	swapReaction(ds, job.ChannelID, job.MessageID, "⏳", "🔄")

	// Send placeholder as reply to user message
	replyMsg, err := ds.ChannelMessageSendReply(job.ChannelID, "🔄 處理中...", &discordgo.MessageReference{
		MessageID: job.MessageID,
		ChannelID: job.ChannelID,
	})
	if err != nil {
		log.Printf("[worker %s] send placeholder: %v", w.channelID, err)
		swapReaction(ds, job.ChannelID, job.MessageID, "🔄", "❌")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(w.askTimeoutSec)*time.Second)
	defer cancel()

	var mu sync.Mutex
	accumulated := ""
	lastUpdate := time.Now()
	statusLine := "🔄 處理中..."

	onChunk := func(chunk string) {
		mu.Lock()
		accumulated += chunk
		// Detect tool usage from chunk content
		lower := strings.ToLower(chunk)
		for _, kw := range []string{"running tool", "bash", "read_file", "write_file", "fs_read", "fs_write", "execute"} {
			if strings.Contains(lower, kw) {
				statusLine = "⚙️ 執行工具中..."
				break
			}
		}
		shouldUpdate := time.Since(lastUpdate) >= time.Duration(w.streamUpdateSec)*time.Second
		snap := accumulated
		status := statusLine
		mu.Unlock()

		if shouldUpdate {
			mu.Lock()
			lastUpdate = time.Now()
			mu.Unlock()
			editMessage(ds, job.ChannelID, replyMsg.ID, status+"\n\n"+snap)
		}
	}

	result, err := w.acpClient.AskStream(ctx, w.agentName, job.Prompt, onChunk)

	if err != nil {
		errMsg := err.Error()
		if ctx.Err() == context.DeadlineExceeded {
			errMsg = fmt.Sprintf("任務超時（%ds），已取消", w.askTimeoutSec)
			_ = w.acpClient.CancelAgent(w.agentName)
			swapReaction(ds, job.ChannelID, job.MessageID, "🔄", "⚠️")
		} else {
			swapReaction(ds, job.ChannelID, job.MessageID, "🔄", "❌")
		}
		editMessage(ds, job.ChannelID, replyMsg.ID, "❌ "+errMsg)
		if w.logger != nil {
			w.logger.Log(w.channelID, ChatEntry{
				Role:    "assistant",
				Content: "❌ " + errMsg,
				Model:   w.model,
			})
		}
		return
	}

	// Final edit with complete response
	response := result.Response
	if response == "" {
		mu.Lock()
		response = accumulated
		mu.Unlock()
	}

	swapReaction(ds, job.ChannelID, job.MessageID, "🔄", "✅")
	sendLong(ds, job.ChannelID, replyMsg.ID, response)

	// Log assistant response
	if w.logger != nil {
		w.logger.Log(w.channelID, ChatEntry{
			Role:    "assistant",
			Content: response,
			Model:   w.model,
		})
	}
}

// editMessage edits a Discord message, truncating to 2000 chars.
func editMessage(ds *discordgo.Session, channelID, msgID, content string) {
	if len(content) > 2000 {
		content = content[:1997] + "..."
	}
	_, _ = ds.ChannelMessageEdit(channelID, msgID, content)
}

// sendLong edits the placeholder with the first chunk, then sends additional messages if needed.
func sendLong(ds *discordgo.Session, channelID, placeholderID, content string) {
	const limit = 1990
	parts := splitMessage(content, limit)
	if len(parts) == 0 {
		editMessage(ds, channelID, placeholderID, "(empty response)")
		return
	}

	prefix := ""
	if len(parts) > 1 {
		prefix = fmt.Sprintf("(1/%d) ", len(parts))
	}
	editMessage(ds, channelID, placeholderID, prefix+parts[0])

	for i := 1; i < len(parts); i++ {
		label := fmt.Sprintf("(%d/%d) ", i+1, len(parts))
		_, _ = ds.ChannelMessageSend(channelID, label+parts[i])
	}
}

func splitMessage(s string, limit int) []string {
	var parts []string
	for len(s) > limit {
		// Try to split at newline
		idx := strings.LastIndex(s[:limit], "\n")
		if idx < limit/2 {
			idx = limit
		}
		parts = append(parts, s[:idx])
		s = s[idx:]
	}
	if s != "" {
		parts = append(parts, s)
	}
	return parts
}
