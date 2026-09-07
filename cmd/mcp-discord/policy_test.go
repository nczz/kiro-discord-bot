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

func TestParseIDSet(t *testing.T) {
	got := parseIDSet(" 123,456,,123 ")
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if _, ok := got["123"]; !ok {
		t.Fatal("missing 123")
	}
	if _, ok := got["456"]; !ok {
		t.Fatal("missing 456")
	}
}

func TestDiscordPolicyGuildAllowed(t *testing.T) {
	p := discordPolicy{allowedGuilds: parseIDSet("g1,g2")}
	if !p.guildAllowed("g1") {
		t.Fatal("g1 should be allowed")
	}
	if p.guildAllowed("g3") {
		t.Fatal("g3 should be denied")
	}

	p = discordPolicy{}
	if !p.guildAllowed("anything") {
		t.Fatal("empty guild allowlist should allow")
	}
}

func TestDiscordPolicyChannelAllowed(t *testing.T) {
	p := discordPolicy{allowedChannels: parseIDSet("c1,c2")}
	if !p.channelIDAllowed("c2") {
		t.Fatal("c2 should be allowed")
	}
	if p.channelIDAllowed("c3") {
		t.Fatal("c3 should be denied")
	}

	p = discordPolicy{}
	if !p.channelIDAllowed("anything") {
		t.Fatal("empty channel allowlist should allow")
	}
}

func TestDiscordPolicyWriteAllowedDefaultsToLegacyOpen(t *testing.T) {
	p := discordPolicy{allowDestructive: true}
	if err := p.writeAllowed("discord_send_message", false); err != nil {
		t.Fatalf("default send denied: %v", err)
	}
	if err := p.writeAllowed("discord_delete_message", true); err != nil {
		t.Fatalf("default destructive denied: %v", err)
	}
}

func TestDiscordPolicyReadOnlyBlocksWrites(t *testing.T) {
	p := discordPolicy{readOnly: true, allowDestructive: true}
	if err := p.writeAllowed("discord_send_message", false); err == nil {
		t.Fatal("read-only policy should block writes")
	}
}

func TestDiscordPolicyAllowedWriteTools(t *testing.T) {
	p := discordPolicy{
		allowedWriteTools: parseIDSet("discord_send_message"),
		allowDestructive:  true,
	}
	if err := p.writeAllowed("discord_send_message", false); err != nil {
		t.Fatalf("allowed write denied: %v", err)
	}
	if err := p.writeAllowed("discord_reply_message", false); err == nil {
		t.Fatal("unlisted write should be denied")
	}

	p.allowedWriteTools = parseIDSet("discord_resolve_mentions")
	if err := p.writeAllowed("discord_resolve_mentions", false); err != nil {
		t.Fatalf("mention resolver write grant denied: %v", err)
	}
}

func TestBotToolsBindingsRestrictReadPolicyTargets(t *testing.T) {
	oldPolicy := policy
	policy = discordPolicy{allowDestructive: true}
	defer func() { policy = oldPolicy }()
	t.Setenv("BOT_TOOLS_CHANNEL_ID", "channel-1")
	t.Setenv("BOT_TOOLS_TARGET_CHANNEL_ID", "thread-1")

	if err := ensureChannelAllowed("channel-1"); err != nil {
		t.Fatalf("bound parent channel denied: %v", err)
	}
	if err := ensureChannelAllowed("thread-1"); err != nil {
		t.Fatalf("bound target thread denied: %v", err)
	}
	if err := ensureChannelAllowed("channel-2"); err == nil || !strings.Contains(err.Error(), "mcp-discord session") {
		t.Fatalf("unbound channel result = %v, want bot-tools binding denial", err)
	}
}

func TestDiscordListVisibilityRespectsBoundChannelPolicy(t *testing.T) {
	t.Setenv("BOT_TOOLS_CHANNEL_ID", "channel-1")
	t.Setenv("BOT_TOOLS_TARGET_CHANNEL_ID", "thread-1")
	oldPolicy := policy
	policy = discordPolicy{allowedGuilds: parseIDSet("guild-1"), allowedChannels: parseIDSet("channel-1"), allowDestructive: true}
	defer func() { policy = oldPolicy }()

	if !discordChannelVisibleForList(&discordgo.Channel{ID: "channel-1", GuildID: "guild-1", Type: discordgo.ChannelTypeGuildText}) {
		t.Fatal("bound parent channel should be visible")
	}
	if !discordChannelVisibleForList(&discordgo.Channel{ID: "thread-1", GuildID: "guild-1", ParentID: "channel-1", Type: discordgo.ChannelTypeGuildPublicThread}) {
		t.Fatal("bound target thread under allowed parent should be visible")
	}
	if !discordChannelVisibleForList(&discordgo.Channel{ID: "thread-2", GuildID: "guild-1", ParentID: "channel-1", Type: discordgo.ChannelTypeGuildPublicThread}) {
		t.Fatal("thread under bound allowed parent should be visible")
	}
	if discordChannelVisibleForList(&discordgo.Channel{ID: "channel-2", GuildID: "guild-1", Type: discordgo.ChannelTypeGuildText}) {
		t.Fatal("unbound channel should not be visible")
	}
	if discordChannelVisibleForList(&discordgo.Channel{ID: "thread-3", GuildID: "guild-1", ParentID: "channel-2", Type: discordgo.ChannelTypeGuildPublicThread}) {
		t.Fatal("thread under unbound parent should not be visible")
	}
	if discordChannelVisibleForList(&discordgo.Channel{ID: "channel-1", GuildID: "guild-2", Type: discordgo.ChannelTypeGuildText}) {
		t.Fatal("wrong guild channel should not be visible")
	}
}

func TestBotToolsGuildBindingRestrictsReadPolicyTargets(t *testing.T) {
	oldPolicy := policy
	policy = discordPolicy{allowDestructive: true}
	defer func() { policy = oldPolicy }()
	t.Setenv("BOT_TOOLS_GUILD_ID", "guild-1")

	if err := ensureGuildAllowed("guild-1"); err != nil {
		t.Fatalf("bound guild denied: %v", err)
	}
	if err := ensureGuildAllowed("guild-2"); err == nil || !strings.Contains(err.Error(), "mcp-discord session") {
		t.Fatalf("unbound guild result = %v, want bot-tools binding denial", err)
	}
}

func TestDiscordUserGuildScopeRequiresAllowedGuild(t *testing.T) {
	oldPolicy := policy
	policy = discordPolicy{allowDestructive: true}
	defer func() { policy = oldPolicy }()

	if _, err := discordUserGuildScope(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"user_id": "user-1"}}}); err == nil || !strings.Contains(err.Error(), "guild_id or channel_id") {
		t.Fatalf("unscoped user lookup error = %v, want scope requirement", err)
	}

	t.Setenv("BOT_TOOLS_GUILD_ID", "guild-1")
	if guildID, err := discordUserGuildScope(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"user_id": "user-1"}}}); err != nil || guildID != "guild-1" {
		t.Fatalf("bound guild scope = %q, %v; want guild-1", guildID, err)
	}
	if _, err := discordUserGuildScope(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"user_id": "user-1", "guild_id": "guild-2"}}}); err == nil || !strings.Contains(err.Error(), "mcp-discord session") {
		t.Fatalf("cross-guild user lookup error = %v, want bot-tools binding denial", err)
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

func TestResolveWriteTargetChannelUsesBotTargetStateForChildThread(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "target.json")
	if err := os.WriteFile(statePath, []byte(`{"target_channel_id":"thread-1"}`+"\n"), 0644); err != nil {
		t.Fatalf("write target state: %v", err)
	}
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", statePath)

	oldPolicy := policy
	policy = discordPolicy{allowedChannels: parseIDSet("channel-1"), allowDestructive: true}
	defer func() { policy = oldPolicy }()

	ds, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatalf("new discord session: %v", err)
	}
	ds.Client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"thread-1","type":11,"guild_id":"guild-1","parent_id":"channel-1"}`)), Request: req}, nil
	})}
	oldDG := dg
	dg = ds
	defer func() { dg = oldDG }()

	got, err := resolveWriteTargetChannel("channel-1")
	if err != nil {
		t.Fatalf("resolve target: %v", err)
	}
	if got != "thread-1" {
		t.Fatalf("resolved target = %q, want thread-1", got)
	}
}

func TestResolveWriteTargetChannelRejectsUnrelatedDynamicTarget(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "target.json")
	if err := os.WriteFile(statePath, []byte(`{"target_channel_id":"channel-2"}`+"\n"), 0644); err != nil {
		t.Fatalf("write target state: %v", err)
	}
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", statePath)

	oldPolicy := policy
	policy = discordPolicy{allowedChannels: parseIDSet("channel-1"), allowDestructive: true}
	defer func() { policy = oldPolicy }()

	ds, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatalf("new discord session: %v", err)
	}
	ds.Client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"channel-2","type":0,"guild_id":"guild-1"}`)), Request: req}, nil
	})}
	oldDG := dg
	dg = ds
	defer func() { dg = oldDG }()

	if got, err := resolveWriteTargetChannel("channel-1"); err == nil || got != "" || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("unrelated dynamic target result = %q, %v; want denial", got, err)
	}
}

func TestEnsureWriteAllowedBlocksMonitorTargetState(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "target.json")
	if err := os.WriteFile(statePath, []byte(`{"target_channel_id":"channel-1","disable_egress":true,"source":"monitor"}`+"\n"), 0644); err != nil {
		t.Fatalf("write target state: %v", err)
	}
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", statePath)
	oldPolicy := policy
	policy = discordPolicy{allowDestructive: true}
	defer func() { policy = oldPolicy }()
	if got := currentTargetStateChannelID(); got != "channel-1" {
		t.Fatalf("disabled-egress target channel = %q, want channel-1", got)
	}

	err := ensureWriteAllowed("discord_send_message", false)
	if err == nil || !strings.Contains(err.Error(), "monitor background checks") {
		t.Fatalf("ensureWriteAllowed error = %v, want monitor disable", err)
	}
}

func TestEnsureWriteAllowedBlocksRemoteA2ATargetState(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "target.json")
	if err := os.WriteFile(statePath, []byte(`{"target_channel_id":"channel-1","remote_a2a":true,"disable_egress":true}`+"\n"), 0644); err != nil {
		t.Fatalf("write target state: %v", err)
	}
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", statePath)
	oldPolicy := policy
	policy = discordPolicy{allowDestructive: true}
	defer func() { policy = oldPolicy }()

	err := ensureWriteAllowed("discord_send_message", false)
	if err == nil || !strings.Contains(err.Error(), "remote A2A") {
		t.Fatalf("ensureWriteAllowed error = %v, want remote A2A write block", err)
	}
}

func TestEnsureWriteAllowedRejectsMissingTargetState(t *testing.T) {
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", filepath.Join(t.TempDir(), "missing.json"))
	oldPolicy := policy
	policy = discordPolicy{allowDestructive: true}
	defer func() { policy = oldPolicy }()

	err := ensureWriteAllowed("discord_send_message", false)
	if err == nil || !strings.Contains(err.Error(), "target state can be verified") {
		t.Fatalf("ensureWriteAllowed error = %v, want fail-closed target-state verification", err)
	}
}

func TestEnsureWriteAllowedDestructiveRequiresAuthenticatedChannelManager(t *testing.T) {
	oldPolicy := policy
	policy = discordPolicy{allowDestructive: true}
	defer func() { policy = oldPolicy }()

	if err := ensureWriteAllowed("discord_delete_message", true); err == nil || !strings.Contains(err.Error(), "authenticated channel manager") {
		t.Fatalf("destructive write without target state error = %v, want authenticated manager denial", err)
	}

	dir := t.TempDir()
	statePath := filepath.Join(dir, "target.json")
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", statePath)

	if err := os.WriteFile(statePath, []byte(`{"target_channel_id":"channel-1","can_manage_channel":true}`+"\n"), 0644); err != nil {
		t.Fatalf("write target state: %v", err)
	}
	if err := ensureWriteAllowed("discord_delete_message", true); err == nil || !strings.Contains(err.Error(), "authenticated channel manager") {
		t.Fatalf("destructive write without requester error = %v, want authenticated manager denial", err)
	}
	if err := ensureWriteAllowed("discord_send_message", false); err != nil {
		t.Fatalf("non-destructive write should not require manager bit: %v", err)
	}

	if err := os.WriteFile(statePath, []byte(`{"target_channel_id":"channel-1","requester_id":"user-1","requester_name":"alice","can_manage_channel":false}`+"\n"), 0644); err != nil {
		t.Fatalf("write non-manager target state: %v", err)
	}
	if err := ensureWriteAllowed("discord_delete_message", true); err == nil || !strings.Contains(err.Error(), "channel management permissions") {
		t.Fatalf("destructive write without manager bit error = %v, want permission denial", err)
	}

	if err := os.WriteFile(statePath, []byte(`{"target_channel_id":"channel-1","requester_id":"user-1","requester_name":"alice","can_manage_channel":true}`+"\n"), 0644); err != nil {
		t.Fatalf("write manager target state: %v", err)
	}
	if err := ensureWriteAllowed("discord_delete_message", true); err != nil {
		t.Fatalf("destructive write with authenticated channel manager denied: %v", err)
	}
}

func TestDiscordPolicyDestructiveGuard(t *testing.T) {
	p := discordPolicy{allowDestructive: false}
	if err := p.writeAllowed("discord_send_message", false); err != nil {
		t.Fatalf("non-destructive write denied: %v", err)
	}
	if err := p.writeAllowed("discord_delete_message", true); err == nil {
		t.Fatal("destructive write should be denied")
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

func TestEnsureBoundDiscordChannelAllowsDynamicTargetState(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "target.json")
	if err := os.WriteFile(statePath, []byte(`{"target_channel_id":"thread-1"}`+"\n"), 0644); err != nil {
		t.Fatalf("write target state: %v", err)
	}
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", statePath)
	t.Setenv("BOT_TOOLS_CHANNEL_ID", "channel-1")

	if err := ensureBoundDiscordChannel("thread-1"); err != nil {
		t.Fatalf("dynamic target rejected: %v", err)
	}
	if err := ensureBoundDiscordChannel("channel-2"); err == nil {
		t.Fatal("unrelated channel accepted")
	}
}

func TestEnsureChannelAllowedAllowsDynamicTargetWhenParentAllowlisted(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "target.json")
	if err := os.WriteFile(statePath, []byte(`{"target_channel_id":"thread-1"}`+"\n"), 0644); err != nil {
		t.Fatalf("write target state: %v", err)
	}
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", statePath)
	t.Setenv("BOT_TOOLS_CHANNEL_ID", "channel-1")

	oldPolicy := policy
	policy = discordPolicy{allowedChannels: parseIDSet("channel-1"), allowDestructive: true}
	defer func() { policy = oldPolicy }()

	oldDG := dg
	ds, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatalf("new discord session: %v", err)
	}
	ds.Client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"thread-1","type":11,"guild_id":"guild-1","parent_id":"channel-1"}`)), Request: req}, nil
	})}
	dg = ds
	defer func() { dg = oldDG }()

	if err := ensureChannelAllowed("thread-1"); err != nil {
		t.Fatalf("dynamic thread denied despite allowlisted parent: %v", err)
	}
}

func TestEnsureChannelAllowedRejectsDynamicTargetOutsideAllowlistedParent(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "target.json")
	if err := os.WriteFile(statePath, []byte(`{"target_channel_id":"thread-2"}`+"\n"), 0644); err != nil {
		t.Fatalf("write target state: %v", err)
	}
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", statePath)
	t.Setenv("BOT_TOOLS_CHANNEL_ID", "channel-1")

	oldPolicy := policy
	policy = discordPolicy{allowedChannels: parseIDSet("channel-1"), allowDestructive: true}
	defer func() { policy = oldPolicy }()

	oldDG := dg
	ds, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatalf("new discord session: %v", err)
	}
	ds.Client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"thread-2","type":11,"guild_id":"guild-1","parent_id":"channel-2"}`)), Request: req}, nil
	})}
	dg = ds
	defer func() { dg = oldDG }()

	if err := ensureChannelAllowed("thread-2"); err == nil {
		t.Fatal("dynamic thread outside allowlisted parent accepted")
	}
}

func TestAuthorizeMessageWriteChannelRejectsParentWhenDynamicTargetActive(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "target.json")
	if err := os.WriteFile(statePath, []byte(`{"target_channel_id":"thread-1"}`+"\n"), 0644); err != nil {
		t.Fatalf("write target state: %v", err)
	}
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", statePath)
	t.Setenv("BOT_TOOLS_CHANNEL_ID", "channel-1")

	oldPolicy := policy
	policy = discordPolicy{allowDestructive: true}
	defer func() { policy = oldPolicy }()

	if err := authorizeMessageWriteChannel("discord_reply_message", false, "channel-1"); err == nil {
		t.Fatal("parent channel message write accepted during dynamic target session")
	}
	if err := authorizeMessageWriteChannel("discord_reply_message", false, "thread-1"); err != nil {
		t.Fatalf("dynamic target message write rejected: %v", err)
	}
}

func TestCreateThreadRejectedWhenDynamicTargetActive(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "target.json")
	if err := os.WriteFile(statePath, []byte(`{"target_channel_id":"thread-1"}`+"\n"), 0644); err != nil {
		t.Fatalf("write target state: %v", err)
	}
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", statePath)
	if err := authorizeCreateThreadChannel("thread-1"); err == nil {
		t.Fatal("create thread accepted during dynamic target session")
	}
}

func TestDiscordAttachmentAllowlistUsesURLChannel(t *testing.T) {
	oldPolicy := policy
	policy = discordPolicy{allowedChannels: parseIDSet("channel-1"), allowDestructive: true}
	defer func() { policy = oldPolicy }()

	allowedURL, err := validateDiscordAttachmentURL("https://cdn.discordapp.com/attachments/channel-1/message-1/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	allowedChannel, err := discordAttachmentChannelID(allowedURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := authorizeAttachmentChannel(allowedChannel); err != nil {
		t.Fatalf("allowed attachment channel denied: %v", err)
	}

	deniedURL, err := validateDiscordAttachmentURL("https://cdn.discordapp.com/attachments/channel-2/message-1/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	deniedChannel, err := discordAttachmentChannelID(deniedURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := authorizeAttachmentChannel(deniedChannel); err == nil || !strings.Contains(err.Error(), "MCP_DISCORD_ALLOWED_CHANNELS") {
		t.Fatalf("off-channel attachment allowlist result = %v, want channel allowlist denial", err)
	}
}

func TestDiscordAttachmentTargetStateRestrictsWithoutStaticAllowlist(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "target.json")
	if err := os.WriteFile(statePath, []byte(`{"target_channel_id":"channel-1"}`+"\n"), 0644); err != nil {
		t.Fatalf("write target state: %v", err)
	}
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", statePath)
	t.Setenv("BOT_TOOLS_CHANNEL_ID", "channel-1")

	oldPolicy := policy
	policy = discordPolicy{allowDestructive: true}
	defer func() { policy = oldPolicy }()

	if err := authorizeAttachmentChannel("channel-1"); err != nil {
		t.Fatalf("bound attachment channel denied: %v", err)
	}
	if err := authorizeAttachmentChannel("channel-2"); err == nil {
		t.Fatal("unrelated attachment channel accepted without static allowlist")
	}

	ds, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatalf("new discord session: %v", err)
	}
	ds.Client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"thread-1","type":11,"guild_id":"guild-1","parent_id":"channel-1"}`)), Request: req}, nil
	})}
	oldDG := dg
	dg = ds
	defer func() { dg = oldDG }()
	if err := authorizeAttachmentChannel("thread-1"); err != nil {
		t.Fatalf("child thread attachment channel denied: %v", err)
	}
}

func TestDiscordAttachmentTargetStateAllowsOnlySelectedThreadWhenThreadBound(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "target.json")
	if err := os.WriteFile(statePath, []byte(`{"target_channel_id":"thread-1"}`+"\n"), 0644); err != nil {
		t.Fatalf("write target state: %v", err)
	}
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", statePath)
	t.Setenv("BOT_TOOLS_CHANNEL_ID", "channel-1")

	oldPolicy := policy
	policy = discordPolicy{allowedChannels: parseIDSet("channel-1"), allowDestructive: true}
	defer func() { policy = oldPolicy }()

	ds, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatalf("new discord session: %v", err)
	}
	ds.Client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"thread-2","type":11,"guild_id":"guild-1","parent_id":"channel-1"}`)), Request: req}, nil
	})}
	oldDG := dg
	dg = ds
	defer func() { dg = oldDG }()

	if err := authorizeAttachmentChannel("thread-1"); err != nil {
		t.Fatalf("selected thread attachment channel denied: %v", err)
	}
	if err := authorizeAttachmentChannel("channel-1"); err == nil {
		t.Fatal("parent attachment channel accepted for thread-bound target")
	}
	if err := authorizeAttachmentChannel("thread-2"); err == nil {
		t.Fatal("sibling thread attachment channel accepted")
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

func containsMentionRef(refs []discordmention.Ref, kind, id string) bool {
	for _, ref := range refs {
		if ref.Kind == kind && ref.ID == id {
			return true
		}
	}
	return false
}
