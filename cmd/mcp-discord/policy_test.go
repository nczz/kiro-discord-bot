package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/nczz/kiro-discord-bot/internal/discordmention"
)

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

type recordingDiscordTransport struct {
	mu      sync.Mutex
	bodies  []string
	counter int
}

func (r *recordingDiscordTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body := ""
	if req.Body != nil {
		raw, _ := io.ReadAll(req.Body)
		body = string(raw)
	}
	r.mu.Lock()
	r.counter++
	id := fmt.Sprintf("message-%d", r.counter)
	r.bodies = append(r.bodies, body)
	r.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     make(http.Header),
		Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{
			"id":%q,
			"channel_id":"channel-1",
			"content":"ok",
			"author":{"id":"bot-1","username":"bot","discriminator":"0000","bot":true}
		}`, id))),
		Request: req,
	}, nil
}

func (r *recordingDiscordTransport) Bodies() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.bodies...)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestLegacyPolicyEnvDoesNotRestrictAttachmentGuard(t *testing.T) {
	t.Setenv("MCP_DISCORD_ALLOWED_GUILDS", "guild-1")
	t.Setenv("MCP_DISCORD_ALLOWED_CHANNELS", "channel-1")
	t.Setenv("MCP_DISCORD_ALLOWED_WRITE_TOOLS", "discord_send_message")
	t.Setenv("MCP_DISCORD_READ_ONLY", "true")
	t.Setenv("MCP_DISCORD_ALLOW_DESTRUCTIVE", "false")
	t.Setenv("BOT_TOOLS_CHANNEL_ID", "channel-1")
	t.Setenv("BOT_TOOLS_TARGET_CHANNEL_ID", "thread-1")
	t.Setenv("BOT_TOOLS_GUILD_ID", "guild-1")

	if err := authorizeAttachmentChannel("channel-2"); err != nil {
		t.Fatalf("authorizeAttachmentChannel denied by legacy env: %v", err)
	}
}

func TestAuthorizeAttachmentChannelRequiresChannelID(t *testing.T) {
	if err := authorizeAttachmentChannel(" "); err == nil {
		t.Fatal("empty attachment channel accepted")
	}
}

func TestDiscordToolAnnotationHelpersClassifyPolicy(t *testing.T) {
	cases := []struct {
		name        string
		tool        mcp.Tool
		readOnly    bool
		destructive bool
	}{
		{name: "read", tool: discordReadTool("discord_read_messages"), readOnly: true, destructive: false},
		{name: "write", tool: discordWriteTool("discord_send_message"), readOnly: false, destructive: false},
		{name: "destructive", tool: discordDestructiveTool("discord_delete_message"), readOnly: false, destructive: true},
		{name: "get_user", tool: discordReadTool("discord_get_user"), readOnly: true, destructive: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.tool.Annotations.ReadOnlyHint == nil || *tc.tool.Annotations.ReadOnlyHint != tc.readOnly {
				t.Fatalf("readOnlyHint = %v, want %v", tc.tool.Annotations.ReadOnlyHint, tc.readOnly)
			}
			if tc.tool.Annotations.DestructiveHint == nil || *tc.tool.Annotations.DestructiveHint != tc.destructive {
				t.Fatalf("destructiveHint = %v, want %v", tc.tool.Annotations.DestructiveHint, tc.destructive)
			}
		})
	}
}

func TestDiscordUserGuildScopeUsesExplicitScopeOnly(t *testing.T) {
	t.Setenv("BOT_TOOLS_GUILD_ID", "guild-1")

	if _, err := discordUserGuildScope(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"user_id": "user-1"}}}); err == nil || !strings.Contains(err.Error(), "guild_id or channel_id") {
		t.Fatalf("unscoped user lookup error = %v, want scope requirement", err)
	}
	if guildID, err := discordUserGuildScope(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"user_id": "user-1", "guild_id": "guild-2"}}}); err != nil || guildID != "guild-2" {
		t.Fatalf("explicit guild scope = %q, %v; want guild-2 without bot-tools binding restriction", guildID, err)
	}
}

func TestOpenDiscordUploadFilePreservesOriginalBytesAndName(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KIRO_API_KEY", "secret-token")
	t.Setenv("DATA_DIR", t.TempDir())
	source := filepath.Join(dir, ".env")
	want := []byte("KIRO_API_KEY=secret-token\nplain=ok\n")
	if err := os.WriteFile(source, want, 0644); err != nil {
		t.Fatal(err)
	}

	f, displayName, cleanup, err := openDiscordUploadFile(source)
	if err != nil {
		t.Fatalf("openDiscordUploadFile: %v", err)
	}
	defer cleanup()
	if displayName != ".env" {
		t.Fatalf("display name = %q, want original basename", displayName)
	}
	raw, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(want) {
		t.Fatalf("direct upload changed file bytes: got %q want %q", raw, want)
	}
	if !strings.Contains(string(raw), "secret-token") {
		t.Fatalf("direct upload unexpectedly redacted source bytes: %q", raw)
	}
	if strings.Contains(safeDiscordUploadError(fmt.Errorf("open file: open %s: permission denied", source)), source) {
		t.Fatal("direct upload error leaked source path")
	}
	if got := safeDiscordUploadError(fmt.Errorf("file exceeds upload size limit (1 bytes)")); got != "file exceeds upload size limit" {
		t.Fatalf("safe upload error = %q, want size reason", got)
	}
}

func TestValidateDiscordAttachmentURL(t *testing.T) {
	if _, err := validateDiscordAttachmentURL("https://cdn.discordapp.com/attachments/1/file.txt"); err != nil {
		t.Fatalf("discord attachment url denied: %v", err)
	}
	if _, err := validateDiscordAttachmentURL("http://cdn.discordapp.com/attachments/1/file.txt"); err == nil {
		t.Fatal("http url should be denied")
	}
	if _, err := validateDiscordAttachmentURL("https://example.com/file.txt"); err == nil {
		t.Fatal("non-discord host should be denied")
	}
	u, err := validateDiscordAttachmentURL("https://cdn.discordapp.com/attachments/channel-1/message-1/file.txt")
	if err != nil {
		t.Fatalf("discord attachment url denied: %v", err)
	}
	channelID, err := discordAttachmentChannelID(u)
	if err != nil {
		t.Fatalf("attachment channel id: %v", err)
	}
	if channelID != "channel-1" {
		t.Fatalf("attachment channel id = %q, want channel-1", channelID)
	}
	if _, err := discordAttachmentChannelID(&url.URL{Scheme: "https", Host: "cdn.discordapp.com", Path: "/not-attachments/channel-1/file.txt"}); err == nil {
		t.Fatal("non-attachment path should be denied")
	}
}

func TestCopyDiscordAttachmentToFileEnforcesLimit(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "attachment.bin")
	n, err := copyDiscordAttachmentToFile(dst, strings.NewReader("123456"), -1, 5)
	if err == nil || !strings.Contains(err.Error(), "attachment exceeds max size") {
		t.Fatalf("oversized attachment result n=%d err=%v", n, err)
	}
	if _, statErr := os.Stat(dst); !os.IsNotExist(statErr) {
		t.Fatalf("oversized partial file still exists: %v", statErr)
	}

	dst = filepath.Join(dir, "allowed.bin")
	n, err = copyDiscordAttachmentToFile(dst, strings.NewReader("12345"), -1, 5)
	if err != nil {
		t.Fatalf("allowed attachment rejected: %v", err)
	}
	if n != 5 {
		t.Fatalf("copied bytes = %d, want 5", n)
	}
}

func TestDiscordAttachmentMaxBytesKeepsDefaultForNonPositiveValues(t *testing.T) {
	t.Setenv("ATTACHMENT_MAX_MB", "0")
	if got := discordAttachmentMaxBytes(); got != 25*1024*1024 {
		t.Fatalf("zero attachment max = %d, want default", got)
	}
	t.Setenv("ATTACHMENT_MAX_MB", "-1")
	if got := discordAttachmentMaxBytes(); got != 25*1024*1024 {
		t.Fatalf("negative attachment max = %d, want default", got)
	}
}

func TestCopyDiscordAttachmentToFileRejectsOversizedContentLength(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "attachment.bin")
	n, err := copyDiscordAttachmentToFile(dst, strings.NewReader("small"), 6, 5)
	if err == nil || !strings.Contains(err.Error(), "attachment exceeds max size") {
		t.Fatalf("oversized content-length result n=%d err=%v", n, err)
	}
	if _, statErr := os.Stat(dst); !os.IsNotExist(statErr) {
		t.Fatalf("content-length rejected file exists: %v", statErr)
	}
}

func TestResolveDownloadDirWithinRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MCP_DISCORD_DOWNLOAD_DIR", root)

	got, err := resolveDownloadDir(filepath.Join(root, "nested"))
	if err != nil {
		t.Fatalf("resolve inside root: %v", err)
	}
	if got != filepath.Join(root, "nested") {
		t.Fatalf("got %q, want nested dir", got)
	}

	_, err = resolveDownloadDir(filepath.Dir(root))
	if err == nil {
		t.Fatal("outside root should be denied")
	}
	if strings.Contains(err.Error(), root) {
		t.Fatalf("outside root error leaked policy root: %q", err)
	}
}

func TestResolveDownloadDirRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "outside")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	t.Setenv("MCP_DISCORD_DOWNLOAD_DIR", root)

	if _, err := resolveDownloadDir(link); err == nil {
		t.Fatal("symlink escape should be denied")
	}
}

func TestResolveDownloadDirDefaultsToPolicyRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MCP_DISCORD_DOWNLOAD_DIR", root)

	got, err := resolveDownloadDir(os.TempDir())
	if err != nil {
		t.Fatalf("resolve default: %v", err)
	}
	if got != root {
		t.Fatalf("got %q, want policy root %q", got, root)
	}
}

func TestSafeAttachmentFilename(t *testing.T) {
	got := safeAttachmentFilename("/attachments/1/../../a b?.txt")
	if got != "a-b" {
		t.Fatalf("got %q, want sanitized base name", got)
	}
	if got := safeAttachmentFilename("/"); got != "attachment" {
		t.Fatalf("empty name got %q", got)
	}
}

func TestSendDiscordMessagePartsSplitsLongContent(t *testing.T) {
	rt := &recordingDiscordTransport{}
	ds, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatalf("new discord session: %v", err)
	}
	ds.Client = &http.Client{Transport: rt}
	oldDG := dg
	dg = ds
	defer func() { dg = oldDG }()

	msgs, err := sendDiscordMessageParts("channel-1", "API_TOKEN=super-secret-token-123\n<@123456789012345678>\n"+strings.Repeat("alpha beta gamma\n", 180))
	if err != nil {
		t.Fatalf("send parts: %v", err)
	}
	if len(msgs) < 2 {
		t.Fatalf("sent messages = %d, want split delivery", len(msgs))
	}
	seenSecret := false
	for i, body := range rt.Bodies() {
		var payload struct {
			Content         string         `json:"content"`
			AllowedMentions map[string]any `json:"allowed_mentions"`
		}
		if err := json.Unmarshal([]byte(body), &payload); err != nil {
			t.Fatalf("payload %d json: %v\n%s", i, err, body)
		}
		if utf8.RuneCountInString(payload.Content) > 2000 {
			t.Fatalf("payload %d content len = %d, want <= 2000", i, utf8.RuneCountInString(payload.Content))
		}
		if strings.Contains(payload.Content, "super-secret-token-123") {
			seenSecret = true
		}
		if payload.AllowedMentions == nil {
			t.Fatalf("payload %d missing allowed_mentions suppression: %s", i, body)
		}
	}
	if !seenSecret {
		t.Fatal("discord_send_message path redacted direct message content")
	}
}

func TestSendDiscordMessagePartsPreservesMarkdownHeading(t *testing.T) {
	rt := &recordingDiscordTransport{}
	ds, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatalf("new discord session: %v", err)
	}
	ds.Client = &http.Client{Transport: rt}
	oldDG := dg
	dg = ds
	defer func() { dg = oldDG }()

	if _, err := sendDiscordMessageParts("channel-1", "# Incident\nbody"); err != nil {
		t.Fatalf("send parts: %v", err)
	}
	var payload struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(rt.Bodies()[0]), &payload); err != nil {
		t.Fatalf("payload json: %v", err)
	}
	if payload.Content != "# Incident\nbody" {
		t.Fatalf("direct message markdown changed: %q", payload.Content)
	}
}

func TestSendDiscordMessagePartsRendersVerifiedMentionPlaceholders(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "target.json")
	rawState := `{"mention_refs":[{"kind":"user","id":"123456789012345678","display_name":"mxp.tw","placeholder":"[[discord:user:123456789012345678]]"},{"kind":"role","id":"222222222222222222","display_name":"Ops","placeholder":"[[discord:role:222222222222222222]]"}]}` + "\n"
	if err := os.WriteFile(statePath, []byte(rawState), 0644); err != nil {
		t.Fatalf("write target state: %v", err)
	}
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", statePath)
	rt := &recordingDiscordTransport{}
	ds, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatalf("new discord session: %v", err)
	}
	ds.Client = &http.Client{Transport: rt}
	oldDG := dg
	dg = ds
	defer func() { dg = oldDG }()

	if _, err := sendDiscordMessageParts("channel-1", "notify [[discord:user:123456789012345678]] and [[discord:role:222222222222222222]] raw <@999999999999999999>"); err != nil {
		t.Fatalf("send parts: %v", err)
	}

	bodies := rt.Bodies()
	if len(bodies) != 1 {
		t.Fatalf("sent bodies = %d, want 1: %v", len(bodies), bodies)
	}
	var payload struct {
		Content         string `json:"content"`
		AllowedMentions struct {
			Users []string `json:"users"`
			Roles []string `json:"roles"`
		} `json:"allowed_mentions"`
	}
	if err := json.Unmarshal([]byte(bodies[0]), &payload); err != nil {
		t.Fatalf("payload json: %v\n%s", err, bodies[0])
	}
	if !strings.Contains(payload.Content, "<@123456789012345678>") {
		t.Fatalf("content missing rendered verified mention: %q", payload.Content)
	}
	if !strings.Contains(payload.Content, "<@&222222222222222222>") {
		t.Fatalf("content missing rendered verified role mention: %q", payload.Content)
	}
	if strings.Contains(payload.Content, "<@999999999999999999>") {
		t.Fatalf("raw unverified mention stayed active: %q", payload.Content)
	}
	if len(payload.AllowedMentions.Users) != 1 || payload.AllowedMentions.Users[0] != "123456789012345678" {
		t.Fatalf("allowed users = %+v, want verified user mention only", payload.AllowedMentions.Users)
	}
	if len(payload.AllowedMentions.Roles) != 1 || payload.AllowedMentions.Roles[0] != "222222222222222222" {
		t.Fatalf("allowed roles = %+v, want verified role mention only", payload.AllowedMentions.Roles)
	}
}

func TestReplyDiscordMessagePartsSplitsLongContent(t *testing.T) {
	rt := &recordingDiscordTransport{}
	ds, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatalf("new discord session: %v", err)
	}
	ds.Client = &http.Client{Transport: rt}
	oldDG := dg
	dg = ds
	defer func() { dg = oldDG }()

	msgs, err := replyDiscordMessageParts("channel-1", "source-message", strings.Repeat("reply payload\n", 220))
	if err != nil {
		t.Fatalf("reply parts: %v", err)
	}
	if len(msgs) < 2 {
		t.Fatalf("sent replies = %d, want split delivery", len(msgs))
	}
	bodies := rt.Bodies()
	var first struct {
		Content          string `json:"content"`
		MessageReference any    `json:"message_reference"`
	}
	if err := json.Unmarshal([]byte(bodies[0]), &first); err != nil {
		t.Fatalf("first payload json: %v\n%s", err, bodies[0])
	}
	if first.MessageReference == nil {
		t.Fatalf("first split reply should preserve message reference: %s", bodies[0])
	}
	for i, body := range bodies {
		var payload struct {
			Content         string         `json:"content"`
			AllowedMentions map[string]any `json:"allowed_mentions"`
		}
		if err := json.Unmarshal([]byte(body), &payload); err != nil {
			t.Fatalf("payload %d json: %v\n%s", i, err, body)
		}
		if utf8.RuneCountInString(payload.Content) > 2000 {
			t.Fatalf("payload %d content len = %d, want <= 2000", i, utf8.RuneCountInString(payload.Content))
		}
		if payload.AllowedMentions == nil {
			t.Fatalf("payload %d missing allowed_mentions suppression: %s", i, body)
		}
	}
}

func TestSendDiscordEmbedPartsSplitsLongDescription(t *testing.T) {
	rt := &recordingDiscordTransport{}
	ds, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatalf("new discord session: %v", err)
	}
	ds.Client = &http.Client{Transport: rt}
	oldDG := dg
	dg = ds
	defer func() { dg = oldDG }()

	msgs, err := sendDiscordEmbedParts("channel-1", &discordgo.MessageEmbed{
		Title:       "Long report",
		Description: "API_TOKEN=super-secret-token-456\n" + strings.Repeat("embed payload\n", 500),
	})
	if err != nil {
		t.Fatalf("send embed parts: %v", err)
	}
	if len(msgs) < 2 {
		t.Fatalf("sent embeds = %d, want split delivery", len(msgs))
	}
	seenSecret := false
	for i, body := range rt.Bodies() {
		var payload struct {
			AllowedMentions map[string]any `json:"allowed_mentions"`
			Embeds          []struct {
				Description string `json:"description"`
			} `json:"embeds"`
		}
		if err := json.Unmarshal([]byte(body), &payload); err != nil {
			t.Fatalf("embed payload %d json: %v\n%s", i, err, body)
		}
		if len(payload.Embeds) != 1 {
			t.Fatalf("embed payload %d embeds = %d, want 1", i, len(payload.Embeds))
		}
		if utf8.RuneCountInString(payload.Embeds[0].Description) > 4096 {
			t.Fatalf("embed payload %d description len = %d, want <= 4096", i, utf8.RuneCountInString(payload.Embeds[0].Description))
		}
		if strings.Contains(payload.Embeds[0].Description, "super-secret-token-456") {
			seenSecret = true
		}
		if payload.AllowedMentions == nil {
			t.Fatalf("embed payload %d missing allowed_mentions suppression: %s", i, body)
		}
	}
	if !seenSecret {
		t.Fatal("discord_send_embed path redacted direct embed content")
	}
}

func TestSendDiscordEmbedPartsPreservesMarkdownHeading(t *testing.T) {
	rt := &recordingDiscordTransport{}
	ds, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatalf("new discord session: %v", err)
	}
	ds.Client = &http.Client{Transport: rt}
	oldDG := dg
	dg = ds
	defer func() { dg = oldDG }()

	if _, err := sendDiscordEmbedParts("channel-1", &discordgo.MessageEmbed{
		Title:       "Incident",
		Description: "# Incident\nbody",
	}); err != nil {
		t.Fatalf("send embed parts: %v", err)
	}
	var payload struct {
		Embeds []struct {
			Description string `json:"description"`
		} `json:"embeds"`
	}
	if err := json.Unmarshal([]byte(rt.Bodies()[0]), &payload); err != nil {
		t.Fatalf("payload json: %v", err)
	}
	if got := payload.Embeds[0].Description; got != "# Incident\nbody" {
		t.Fatalf("direct embed markdown changed: %q", got)
	}
}

func TestSplitMentionNamesHandlesNaturalSeparators(t *testing.T) {
	got := splitMentionNames("Wendy、Cheisy 跟 Wendy\nAlice")
	want := []string{"Wendy", "Cheisy", "Alice"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("names = %#v, want %#v", got, want)
	}
}

func TestValidateMentionResolveNamesRejectsRawIDs(t *testing.T) {
	for _, names := range [][]string{
		{"123456789012345678"},
		{"<@123456789012345678>"},
		{"<@!123456789012345678>"},
	} {
		if err := validateMentionResolveNames(names); err == nil {
			t.Fatalf("validateMentionResolveNames(%+v) accepted raw mention ID", names)
		}
	}
}

func TestValidateMentionResolveNamesAcceptsDisplayNames(t *testing.T) {
	if err := validateMentionResolveNames([]string{"Wendy Chen", "Cheisy"}); err != nil {
		t.Fatalf("validate display names: %v", err)
	}
}

func TestResolveMentionNameAllowsUniqueFreshPrefix(t *testing.T) {
	member := &discordgo.Member{Nick: "Wendy Chen", User: &discordgo.User{ID: "123", Username: "wendy", GlobalName: "Wendy Chen"}}
	got, ambiguous, ok := resolveMentionName("Wendy", "fresh_search_unique", []*discordgo.Member{member})
	if !ok {
		t.Fatalf("resolve failed ambiguous=%+v", ambiguous)
	}
	if got.UserID != "123" || got.Placeholder != "[[discord:user:123]]" {
		t.Fatalf("resolved = %+v", got)
	}
}

func TestResolveMentionNameReportsAmbiguousMatches(t *testing.T) {
	members := []*discordgo.Member{
		{Nick: "Wendy Chen", User: &discordgo.User{ID: "123", Username: "wendychen"}},
		{Nick: "Wendy Lin", User: &discordgo.User{ID: "456", Username: "wendylin"}},
	}
	_, ambiguous, ok := resolveMentionName("Wendy", "fresh_lookup", members)
	if ok {
		t.Fatal("ambiguous Wendy resolved unexpectedly")
	}
	if ambiguous.Query != "Wendy" || len(ambiguous.Candidates) != 2 {
		t.Fatalf("ambiguous = %+v", ambiguous)
	}
}

func TestAmbiguousMentionCandidatesDoNotExposeReusableUserIDs(t *testing.T) {
	members := []*discordgo.Member{
		{Nick: "Wendy Chen", User: &discordgo.User{ID: "123", Username: "wendychen"}},
		{Nick: "Wendy Lin", User: &discordgo.User{ID: "456", Username: "wendylin"}},
	}
	ambiguous := ambiguousForMembers("Wendy", members)
	raw, err := json.Marshal(ambiguous)
	if err != nil {
		t.Fatalf("marshal ambiguous candidates: %v", err)
	}
	if strings.Contains(string(raw), "user_id") || strings.Contains(string(raw), "123") || strings.Contains(string(raw), "456") {
		t.Fatalf("ambiguous candidates leaked reusable user IDs: %s", raw)
	}
}

func TestGrantMentionRefsForCurrentJobUpdatesTargetState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "target.json")
	if err := os.WriteFile(statePath, []byte(`{"target_channel_id":"thread-1","remote_a2a":true,"delegation_depth":2,"requester_id":"user-1","requester_name":"alice","can_manage_channel":true,"can_manage_guild":true,"allowed_mention_user_ids":["old"],"mention_refs":[{"kind":"user","id":"old","display_name":"Old","placeholder":"[[discord:user:old]]"},{"kind":"role","id":"999","display_name":"Ops","placeholder":"[[discord:role:999]]"}]}`+"\n"), 0644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", statePath)

	if err := grantMentionRefsForCurrentJob([]discordmention.Ref{discordmention.UserRef("123", "Wendy")}); err != nil {
		t.Fatalf("grant mention refs: %v", err)
	}
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var state mentionTargetState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("state json: %v\n%s", err, raw)
	}
	if !containsString(state.AllowedMentionUserIDs, "old") || !containsString(state.AllowedMentionUserIDs, "123") {
		t.Fatalf("allowed mention IDs = %+v", state.AllowedMentionUserIDs)
	}
	if len(state.MentionRefs) != 3 {
		t.Fatalf("mention refs = %+v", state.MentionRefs)
	}
	if !containsMentionRef(state.MentionRefs, "role", "999") {
		t.Fatalf("mention refs = %+v, want existing role preserved", state.MentionRefs)
	}
	if !state.RemoteA2A || state.DelegationDepth != 2 || state.RequesterID != "user-1" || state.RequesterName != "alice" || !state.CanManageChannel || !state.CanManageGuild {
		t.Fatalf("security target-state fields were not preserved: %+v", state)
	}
}

func TestGrantMentionRefsForCurrentJobNoTargetStatePathIsNoop(t *testing.T) {
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", "")
	if err := grantMentionRefsForCurrentJob([]discordmention.Ref{discordmention.UserRef("123", "Wendy")}); err != nil {
		t.Fatalf("grant mention refs without target state: %v", err)
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
