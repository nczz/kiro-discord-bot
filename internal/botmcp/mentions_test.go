package botmcp

import (
	"encoding/json"
	"fmt"
	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/internal/discordmention"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type mentionRoundTripFunc func(*http.Request) (*http.Response, error)

func (f mentionRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestBotResolveMentionsUsesDiscordAndGrantsCurrentTask(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "target.json")
	if err := os.WriteFile(statePath, []byte(`{"guild_id":"guild-1","target_channel_id":"thread-1","requester_id":"user-1","mention_refs":[{"kind":"role","id":"999","display_name":"Ops","placeholder":"[[discord:role:999]]"}]}`+"\n"), 0600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", statePath)
	t.Setenv("BOT_TOOLS_GUILD_ID", "")

	session, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatalf("new Discord session: %v", err)
	}
	session.Client.Transport = mentionRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/guilds/guild-1/members/search"):
			if req.URL.Query().Get("query") != "Wendy" {
				t.Fatalf("unexpected member search request: %s", req.URL.String())
			}
		case strings.HasSuffix(req.URL.Path, "/guilds/guild-1/members"):
			if req.URL.Query().Get("limit") != "1000" {
				t.Fatalf("unexpected member scan request: %s", req.URL.String())
			}
		default:
			t.Fatalf("unexpected Discord request: %s", req.URL.String())
		}
		body := `[{"nick":"Wendy Chen","user":{"id":"123456789012345678","username":"wendy","global_name":"Wendy Chen"}}]`
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})
	previousFactory := botMentionSessionFactory
	botMentionSessionFactory = func() (*discordgo.Session, error) { return session, nil }
	t.Cleanup(func() { botMentionSessionFactory = previousFactory })

	client := initializedTestClient(t, NewServerWithOptions(ServerOptions{}))
	result := callTestTool(t, client, ToolResolveMentions, map[string]any{"names": "Wendy"})
	text := callToolText(t, result)
	var resolved botMentionResolveResult
	if err := json.Unmarshal([]byte(text), &resolved); err != nil {
		t.Fatalf("decode resolver output: %v\n%s", err, text)
	}
	if len(resolved.Resolved) != 1 || resolved.Resolved[0].Placeholder != "[[discord:user:123456789012345678]]" {
		t.Fatalf("resolver output = %+v", resolved)
	}

	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var state targetState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if !containsMentionRef(state.MentionRefs, "role", "999") || !containsMentionRef(state.MentionRefs, "user", "123456789012345678") {
		t.Fatalf("mention refs = %+v", state.MentionRefs)
	}
	if len(state.AllowedMentionUserIDs) != 1 || state.AllowedMentionUserIDs[0] != "123456789012345678" {
		t.Fatalf("allowed mention users = %+v", state.AllowedMentionUserIDs)
	}
}

func TestBotResolveMentionsHonorsDisabledBotWrites(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "target.json")
	original := `{"target_channel_id":"thread-1","requester_id":"user-1","disable_egress":true}` + "\n"
	if err := os.WriteFile(statePath, []byte(original), 0600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", statePath)
	t.Setenv("BOT_TOOLS_GUILD_ID", "guild-1")

	client := initializedTestClient(t, NewServerWithOptions(ServerOptions{}))
	result := callTestTool(t, client, ToolResolveMentions, map[string]any{"names": "Wendy"})
	if text := callToolText(t, result); !strings.Contains(text, "disabled") {
		t.Fatalf("disabled resolver error = %q", text)
	}
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if string(raw) != original {
		t.Fatalf("disabled resolver mutated target state: %s", raw)
	}
}

func TestGrantMentionRefsRequiresVerifiedTargetState(t *testing.T) {
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", "")
	if err := grantMentionRefsForCurrentJob([]discordmention.Ref{discordmention.UserRef("123", "Wendy")}); err == nil {
		t.Fatal("mention grant succeeded without bot target state")
	}
}

func TestGrantMentionRefsSerializesConcurrentUpdates(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "target.json")
	if err := os.WriteFile(statePath, []byte(`{"target_channel_id":"thread-1"}`+"\n"), 0600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", statePath)

	const grants = 32
	start := make(chan struct{})
	errs := make(chan error, grants)
	var wg sync.WaitGroup
	for i := range grants {
		userID := fmt.Sprintf("%018d", i+1)
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- grantMentionRefsForCurrentJob([]discordmention.Ref{discordmention.UserRef(userID, "User "+userID)})
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("grant mention ref: %v", err)
		}
	}

	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var state targetState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if len(state.MentionRefs) != grants || len(state.AllowedMentionUserIDs) != grants {
		t.Fatalf("concurrent grants lost updates: refs=%d allowed_users=%d, want %d", len(state.MentionRefs), len(state.AllowedMentionUserIDs), grants)
	}
}

func containsMentionRef(refs []discordmention.Ref, kind, id string) bool {
	for _, ref := range refs {
		if ref.Kind == kind && ref.ID == id {
			return true
		}
	}
	return false
}
