package bot

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/bwmarrin/discordgo"
)

type scheduledThreadRoundTripper struct {
	mu       sync.Mutex
	requests []string
}

func (rt *scheduledThreadRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.mu.Lock()
	rt.requests = append(rt.requests, req.Method+" "+req.URL.Path)
	rt.mu.Unlock()

	status := http.StatusNoContent
	body := ""
	if req.Method == http.MethodPost && strings.Contains(req.URL.Path, "/threads") {
		status = http.StatusOK
		body = `{"id":"thread-new","channel_id":"thread-new","parent_id":"parent-1","name":"monitor","type":11}`
	}
	if req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/messages") {
		status = http.StatusOK
		body = `{"id":"msg-1","channel_id":"thread-new","content":"ok"}`
	}
	return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

func (rt *scheduledThreadRoundTripper) Snapshot() []string {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return append([]string(nil), rt.requests...)
}

func TestPrepareScheduledThreadRejectsStoredThreadFromDifferentParent(t *testing.T) {
	rt := &scheduledThreadRoundTripper{}
	ds, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatal(err)
	}
	ds.Client = &http.Client{Transport: rt}
	if err := ds.State.GuildAdd(&discordgo.Guild{ID: "guild-1"}); err != nil {
		t.Fatal(err)
	}
	if err := ds.State.ChannelAdd(&discordgo.Channel{ID: "thread-wrong", GuildID: "guild-1", ParentID: "other-parent", Type: discordgo.ChannelTypeGuildPublicThread}); err != nil {
		t.Fatal(err)
	}

	threadID, created, err := (&Bot{}).prepareScheduledThread(ds, scheduledThreadRequest{ParentChannelID: "parent-1", ExistingThreadID: "thread-wrong", ThreadName: "monitor"})
	if err != nil {
		t.Fatalf("prepareScheduledThread: %v", err)
	}
	if !created || threadID != "thread-new" {
		t.Fatalf("threadID=%q created=%v, want new thread", threadID, created)
	}
	for _, req := range rt.Snapshot() {
		if strings.Contains(req, "/channels/thread-wrong/messages") {
			t.Fatalf("sent separator to cross-parent stored thread: requests=%+v", rt.Snapshot())
		}
	}
}

func TestScheduledThreadNameFitsDiscordLimit(t *testing.T) {
	name := scheduledThreadName("🔎 ", strings.Repeat("長", 100))
	if len([]rune(name)) > 100 {
		t.Fatalf("thread name length = %d, want <= 100: %q", len([]rune(name)), name)
	}
	editName := truncateDiscordThreadName(name + " · 05/28 12:00")
	if len([]rune(editName)) > 100 {
		t.Fatalf("edit thread name length = %d, want <= 100: %q", len([]rune(editName)), editName)
	}
}

func TestMonitorThreadLinkPrefersDiscordURL(t *testing.T) {
	if got := monitorThreadLink("guild-1", "thread-1"); got != "https://discord.com/channels/guild-1/thread-1" {
		t.Fatalf("monitor thread link = %q", got)
	}
	if got := monitorThreadLink("", "thread-1"); got != "<#thread-1>" {
		t.Fatalf("monitor fallback thread link = %q", got)
	}
}
