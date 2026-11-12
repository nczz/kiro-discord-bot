package channel

import (
	"errors"
	"net/http"
	"sync"
	"testing"

	L "github.com/nczz/kiro-discord-bot/locale"
)

type transientReactionRoundTripper struct {
	reactions       map[string]bool
	failedEmoji     string
	failedFirstDrop bool
}

func (rt *transientReactionRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	emoji := reactionEmojiFromRequest(req)
	if emoji == "" {
		return discordTestResponse(req, http.StatusNoContent, ""), nil
	}
	switch req.Method {
	case http.MethodPut:
		rt.reactions[emoji] = true
	case http.MethodDelete:
		if emoji == rt.failedEmoji && !rt.failedFirstDrop {
			rt.failedFirstDrop = true
			return nil, errors.New("transient reaction delete failure")
		}
		delete(rt.reactions, emoji)
	}
	return discordTestResponse(req, http.StatusNoContent, ""), nil
}

func TestJobReactionStateFinalRetriesTransientCleanup(t *testing.T) {
	rt := &transientReactionRoundTripper{reactions: make(map[string]bool), failedEmoji: "🔄"}
	state := newJobReactionState(testDiscordSession(rt), "ch1", "m1", false, true)
	state.Set("🔄")
	state.Final("✅")

	if !rt.failedFirstDrop {
		t.Fatal("expected the first cleanup attempt to fail")
	}
	if len(rt.reactions) != 1 || !rt.reactions["✅"] {
		t.Fatalf("final reactions = %#v, want only done", rt.reactions)
	}
}

func TestQueuedJobReactionRollbackRetriesAndClearsOwnership(t *testing.T) {
	rt := &transientReactionRoundTripper{
		reactions:   map[string]bool{"⏳": true},
		failedEmoji: "⏳",
	}
	job := &Job{ChannelID: "ch1", MessageID: "m1", QueuedReactionAdded: true}

	rollbackQueuedJobReaction(testDiscordSession(rt), job)

	if !rt.failedFirstDrop {
		t.Fatal("expected the first rollback attempt to fail")
	}
	if job.QueuedReactionAdded {
		t.Fatal("queued reaction ownership remained after successful retry")
	}
	if len(rt.reactions) != 0 {
		t.Fatalf("reactions after rollback = %#v, want none", rt.reactions)
	}
}

type messageFailReactionRoundTripper struct {
	mu        sync.Mutex
	reactions map[string]bool
}

func (rt *messageFailReactionRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if emoji := reactionEmojiFromRequest(req); emoji != "" {
		rt.mu.Lock()
		if req.Method == http.MethodPut {
			rt.reactions[emoji] = true
		} else if req.Method == http.MethodDelete {
			delete(rt.reactions, emoji)
		}
		rt.mu.Unlock()
		return discordTestResponse(req, http.StatusNoContent, ""), nil
	}
	if req.Method == http.MethodPost {
		return discordTestResponse(req, http.StatusInternalServerError, `{"message":"forced delivery failure"}`), nil
	}
	return discordTestResponse(req, http.StatusNoContent, ""), nil
}

func (rt *messageFailReactionRoundTripper) snapshot() map[string]bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	result := make(map[string]bool, len(rt.reactions))
	for emoji, present := range rt.reactions {
		result[emoji] = present
	}
	return result
}

func TestWorkerThreadDeliveryFailureClearsProgressBeforeDiagnosticHint(t *testing.T) {
	L.Load("en")
	rt := &messageFailReactionRoundTripper{reactions: make(map[string]bool)}
	ds := testDiscordSession(rt)
	agent := &fakeWorkerAgent{}
	w := newWorker("ch1", agent, 1, 30, 1, 1440, nil, "")

	w.execute(&Job{
		ChannelID: "ch1",
		ThreadID:  "thread-1",
		MessageID: "m1",
		Prompt:    "hello",
		Session:   ds,
	})
	agent.Callbacks().OnComplete("final response", nil)

	reactions := rt.snapshot()
	if len(reactions) != 2 || !reactions["❌"] || !reactions["🌐"] {
		t.Fatalf("delivery-failure reactions = %#v, want only failure and network hint", reactions)
	}
}
