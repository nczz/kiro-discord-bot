package bot

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/audit"
	"github.com/nczz/kiro-discord-bot/channel"
	L "github.com/nczz/kiro-discord-bot/locale"
	"github.com/nczz/kiro-discord-bot/webshare"
)

func TestWebShareCommandRegistrationPermissionsAndPrivacy(t *testing.T) {
	L.Load("en")
	cmds := buildSlashCommandsWithA2A(false)
	var cmd *discordgo.ApplicationCommand
	for _, c := range cmds {
		if c.Name == "webshare" {
			cmd = c
			break
		}
	}
	if cmd == nil {
		t.Fatal("/webshare command not registered")
	}
	if cmd.DefaultMemberPermissions == nil || *cmd.DefaultMemberPermissions&discordgo.PermissionManageChannels == 0 {
		t.Fatalf("/webshare default perms = %v, want ManageChannels", cmd.DefaultMemberPermissions)
	}
	if len(cmd.Options) != 4 {
		t.Fatalf("/webshare options = %d, want start/stop/status/revoke", len(cmd.Options))
	}
	if got := commandResponseVisibility("webshare", "start"); got != commandVisibilityPrivate {
		t.Fatalf("webshare visibility = %v, want private", got)
	}
}

func TestWebShareRevokeSlashOptionsPreserveNames(t *testing.T) {
	got := webshareArgsFromSlashOptions([]*discordgo.ApplicationCommandInteractionDataOption{{
		Name: "revoke",
		Type: discordgo.ApplicationCommandOptionSubCommand,
		Options: []*discordgo.ApplicationCommandInteractionDataOption{{
			Name:  "reason",
			Type:  discordgo.ApplicationCommandOptionString,
			Value: "rotation window",
		}},
	}})
	if got != "revoke --reason rotation window" {
		t.Fatalf("reason-only revoke args = %q", got)
	}
	got = webshareArgsFromSlashOptions([]*discordgo.ApplicationCommandInteractionDataOption{{
		Name: "revoke",
		Type: discordgo.ApplicationCommandOptionSubCommand,
		Options: []*discordgo.ApplicationCommandInteractionDataOption{
			{Name: "reason", Type: discordgo.ApplicationCommandOptionString, Value: "incident cleanup"},
			{Name: "share_id", Type: discordgo.ApplicationCommandOptionString, Value: "ws_target"},
		},
	}})
	if got != "revoke ws_target incident cleanup" {
		t.Fatalf("named revoke args = %q", got)
	}
}

func TestWebShareRelayURLAcceptsBaseOriginOrRoomPrefix(t *testing.T) {
	tests := []struct {
		name string
		base string
		want string
	}{
		{name: "origin", base: "https://relay.example", want: "wss://relay.example/r/room-123?role=host"},
		{name: "room prefix", base: "wss://relay.example/r", want: "wss://relay.example/r/room-123?role=host"},
		{name: "room prefix slash", base: "wss://relay.example/r/", want: "wss://relay.example/r/room-123?role=host"},
		{name: "custom prefix", base: "https://relay.example/ws", want: "wss://relay.example/ws/r/room-123?role=host"},
	}
	for _, tt := range tests {
		got, err := webshareRelayURL(tt.base, "room-123")
		if err != nil {
			t.Fatalf("%s: webshareRelayURL error: %v", tt.name, err)
		}
		if got != tt.want {
			t.Fatalf("%s: webshareRelayURL = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestWebShareStartSlashResponseIsEphemeral(t *testing.T) {
	L.Load("en")
	dataDir := t.TempDir()
	project := t.TempDir()
	store, err := webshare.OpenStore(context.Background(), dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	rt := &recordingDiscordTransport{}
	ds := testPeerPermissionSession(t, []*discordgo.PermissionOverwrite{userMemberManageOverwrite("viewer", discordgo.PermissionManageChannels), userMemberManageOverwrite("bot-1", discordgo.PermissionManageWebhooks)})
	ds.Client = &http.Client{Transport: rt}
	manager := channel.NewManager(channel.ManagerConfig{DataDir: dataDir, Store: mustSessionStore(t, dataDir), DefaultCWD: project})
	if _, err := manager.InitializeChannelCWD("channel-1", project); err != nil {
		t.Fatalf("initialize channel: %v", err)
	}
	auditStore, err := audit.Open(audit.Config{DataDir: dataDir, RecordContent: true})
	if err != nil {
		t.Fatalf("open audit store: %v", err)
	}
	recorder := audit.NewRecorder(auditStore, 20, nil, false)
	b := &Bot{discord: ds, manager: manager, guildID: "guild-1", webshareStore: store, webshareConfig: WebShareConfig{Enabled: true, RelayURL: "ws://127.0.0.1:1", PublicBaseURL: "https://relay.example", HostToken: "host-token"}, auditRecorder: recorder}
	t.Cleanup(func() {
		b.stopAllWebShareHosts()
		recorder.Close()
	})

	b.handleSlashCommand(ds, &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		ID:        "interaction-webshare-start",
		Type:      discordgo.InteractionApplicationCommand,
		GuildID:   "guild-1",
		ChannelID: "channel-1",
		AppID:     "app-1",
		Token:     "token-1",
		Member:    &discordgo.Member{User: &discordgo.User{ID: "viewer", Username: "Viewer"}},
		Data: discordgo.ApplicationCommandInteractionData{
			Name: "webshare",
			Options: []*discordgo.ApplicationCommandInteractionDataOption{{
				Name: "start",
				Type: discordgo.ApplicationCommandOptionSubCommand,
			}},
		},
	}})

	paths, bodies := waitDiscordRequests(t, rt, 2)
	var foundDeferred bool
	for i, path := range paths {
		if strings.HasPrefix(path, "POST /api/v") && strings.Contains(path, "/interactions/interaction-webshare-start/token-1/callback") {
			foundDeferred = true
			if !strings.Contains(bodies[i], `"flags":64`) {
				t.Fatalf("initial /webshare start response should be ephemeral: %s", bodies[i])
			}
		}
	}
	if !foundDeferred {
		t.Fatalf("requests = %v, want deferred interaction response", paths)
	}
	for i, body := range bodies {
		if strings.Contains(body, "Control link:") && strings.Contains(paths[i], "/channels/") {
			t.Fatalf("webshare link leaked through channel message path=%q body=%s", paths[i], body)
		}
	}
	events := waitBotAuditEvents(t, filepath.Join(dataDir, "audit", "discord.sqlite"), "bot_command_response_sent", 2)
	var foundLinkResponse bool
	for _, event := range events {
		if event.Command != "webshare" || event.Source != "slash" || event.InteractionID != "interaction-webshare-start" || event.Status != "sent" {
			continue
		}
		foundLinkResponse = true
		if strings.Contains(event.Content, "Control link:") || strings.Contains(event.Content, "View link:") || event.Metadata["content_redacted"] != true {
			t.Fatalf("webshare link audit event not redacted: %+v", event)
		}
	}
	if !foundLinkResponse {
		t.Fatalf("events = %+v, want webshare response audit event", events)
	}
}

func TestWebShareStartStatusStopRevoke(t *testing.T) {
	L.Load("en")
	dataDir := t.TempDir()
	project := t.TempDir()
	store, err := webshare.OpenStore(context.Background(), dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ds := testPeerPermissionSession(t, []*discordgo.PermissionOverwrite{userMemberManageOverwrite("viewer", discordgo.PermissionManageChannels), userMemberManageOverwrite("bot-1", discordgo.PermissionManageWebhooks)})
	manager := channel.NewManager(channel.ManagerConfig{DataDir: dataDir, Store: mustSessionStore(t, dataDir), DefaultCWD: project})
	if _, err := manager.InitializeChannelCWD("channel-1", project); err != nil {
		t.Fatalf("initialize channel: %v", err)
	}
	b := &Bot{discord: ds, manager: manager, guildID: "guild-1", webshareStore: store, webshareConfig: WebShareConfig{Enabled: true, RelayURL: "ws://127.0.0.1:1", PublicBaseURL: "https://relay.example", HostToken: "host-token"}}
	t.Cleanup(b.stopAllWebShareHosts)
	var replies []string
	ctx := cmdCtx{channelID: "channel-1", targetID: "channel-1", guildID: "guild-1", userID: "viewer", username: "Viewer", reply: func(s string) { replies = append(replies, s) }, replyWithMetadata: func(s string, _ map[string]any) { replies = append(replies, s) }}

	b.cmdWebShare(ctxWithArgs(ctx, "start"))
	if len(replies) == 0 || !strings.Contains(replies[len(replies)-1], "Control link:") || !strings.Contains(replies[len(replies)-1], "View link:") {
		t.Fatalf("start reply missing links: %#v", replies)
	}
	active, err := store.ListActive(context.Background())
	if err != nil || len(active) != 1 {
		t.Fatalf("active shares = %+v err=%v", active, err)
	}

	b.cmdWebShare(ctxWithArgs(ctx, "status"))
	if !strings.Contains(replies[len(replies)-1], active[0].ShareID) {
		t.Fatalf("status reply = %q, want share id", replies[len(replies)-1])
	}
	b.cmdWebShare(ctxWithArgs(ctx, "stop"))
	if got, _ := store.GetShare(context.Background(), active[0].ShareID); got.Status != webshare.StatusRevoked {
		t.Fatalf("stop status = %s", got.Status)
	}

	replies = nil
	b.cmdWebShare(ctxWithArgs(ctx, "start"))
	active, _ = store.ListActive(context.Background())
	if len(active) != 1 {
		t.Fatalf("active after second start = %d", len(active))
	}
	b.cmdWebShare(ctxWithArgs(ctx, "revoke "+active[0].ShareID+" emergency"))
	if got, _ := store.GetShare(context.Background(), active[0].ShareID); got.Status != webshare.StatusRevoked || got.RevokeReason != "emergency" {
		t.Fatalf("revoke result = status %s reason %q", got.Status, got.RevokeReason)
	}
}

func TestWebShareStartRequiresBotManageWebhooks(t *testing.T) {
	L.Load("en")
	dataDir := t.TempDir()
	project := t.TempDir()
	store, err := webshare.OpenStore(context.Background(), dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ds := testPeerPermissionSession(t, []*discordgo.PermissionOverwrite{userMemberManageOverwrite("viewer", discordgo.PermissionManageChannels)})
	manager := channel.NewManager(channel.ManagerConfig{DataDir: dataDir, Store: mustSessionStore(t, dataDir), DefaultCWD: project})
	if _, err := manager.InitializeChannelCWD("channel-1", project); err != nil {
		t.Fatalf("initialize channel: %v", err)
	}
	b := &Bot{discord: ds, manager: manager, guildID: "guild-1", webshareStore: store, webshareConfig: WebShareConfig{Enabled: true, RelayURL: "ws://127.0.0.1:1", PublicBaseURL: "https://relay.example", HostToken: "host-token"}}
	var replies []string
	ctx := cmdCtx{channelID: "channel-1", targetID: "channel-1", guildID: "guild-1", userID: "viewer", username: "Viewer", reply: func(s string) { replies = append(replies, s) }, replyWithMetadata: func(s string, _ map[string]any) { replies = append(replies, s) }}

	b.cmdWebShare(ctxWithArgs(ctx, "start"))
	if len(replies) != 1 || !strings.Contains(replies[0], L.Get("webshare.webhook_permission_missing")) {
		t.Fatalf("start replies = %#v, want webhook permission error", replies)
	}
	if active, err := store.ListActive(context.Background()); err != nil || len(active) != 0 {
		t.Fatalf("active shares = %+v err=%v, want none", active, err)
	}
}

func TestWebShareBangCommandDoesNotPostControlLink(t *testing.T) {
	L.Load("en")
	dataDir := t.TempDir()
	project := t.TempDir()
	manager := channel.NewManager(channel.ManagerConfig{DataDir: dataDir, Store: mustSessionStore(t, dataDir), DefaultCWD: project})
	if _, err := manager.InitializeChannelCWD("channel-1", project); err != nil {
		t.Fatalf("initialize channel: %v", err)
	}
	rt := &recordingDiscordTransport{}
	ds := testPeerPermissionSession(t, []*discordgo.PermissionOverwrite{userMemberManageOverwrite("viewer", discordgo.PermissionManageChannels)})
	ds.Client = &http.Client{Transport: rt}
	b := &Bot{manager: manager, discord: ds, seen: newSeenMessages(), setupPromptCooldown: newSetupPromptCooldown(nil)}
	defer b.seen.Stop()

	b.handleMessage(ds, &discordgo.MessageCreate{Message: &discordgo.Message{ID: "message-1", ChannelID: "channel-1", GuildID: "guild-1", Content: "!webshare start", Author: &discordgo.User{ID: "viewer", Username: "Viewer"}}})
	_, bodies := rt.Snapshot()
	joined := strings.Join(bodies, "\n")
	if strings.Contains(joined, "Control link:") || strings.Contains(joined, "View link:") {
		t.Fatalf("bang webshare leaked delegated links: %s", joined)
	}
	if !strings.Contains(joined, L.Get("usage.slash_only")) {
		t.Fatalf("bang webshare reply = %s, want slash-only guidance", joined)
	}
}

func TestWebShareLockoutParentAndManagedChild(t *testing.T) {
	store, share := newTestWebShareStoreAndShare(t)
	if err := store.RegisterManagedChildThread(context.Background(), webshare.ManagedChildThread{ShareID: share.ShareID, ParentChannelID: "channel-1", ThreadID: "thread-2"}); err != nil {
		t.Fatal(err)
	}
	b := &Bot{guildID: "guild-1", webshareStore: store}
	if !b.rejectWebShareLockedDiscordUse("viewer", "channel-1", "", "status") {
		t.Fatal("parent target should lock opener discord commands")
	}
	if !b.rejectWebShareLockedDiscordUse("viewer", "thread-2", "channel-1", "") {
		t.Fatal("managed child thread should lock opener discord prompts")
	}
	if b.rejectWebShareLockedDiscordUse("viewer", "thread-2", "channel-1", "webshare") {
		t.Fatal("webshare stop/status/revoke path should remain allowed")
	}
	if b.rejectWebShareLockedDiscordUse("other", "channel-1", "", "status") {
		t.Fatal("other users should not be opener-locked")
	}
}

func TestWebSharePostMessageSelectedMentionAllowedRawMentionInert(t *testing.T) {
	L.Load("en")
	rt := &recordingDiscordTransport{}
	ds := &discordgo.Session{State: discordgo.NewState(), Client: &http.Client{Transport: rt}, Ratelimiter: discordgo.NewRatelimiter()}
	ds.State.User = &discordgo.User{ID: "999999999999999999", Username: "bot", Bot: true}
	_ = ds.State.GuildAdd(&discordgo.Guild{ID: "guild-1"})
	_ = ds.State.MemberAdd(&discordgo.Member{GuildID: "guild-1", User: &discordgo.User{ID: "222222222222222222", Username: "Bob"}})
	b := &Bot{discord: ds, downloadClient: &http.Client{}, attachmentMaxBytes: 1024, webshareWebhookByChannel: map[string]webshareWebhookCredential{"channel-1": {ID: "webshare-hook-1", Token: "webshare-token-1"}}, webshareWebhookIDs: map[string]bool{}}
	share := webshare.Share{ShareID: "ws_1", GuildID: "guild-1", TargetID: "channel-1", OpenerUserID: "111111111111111111", OpenerUsername: "Alice", Capabilities: webshare.WriteCapabilities()}
	action := webshare.ClientAction{Text: "hello [[discord:user:222222222222222222]] raw <@333333333333333333>", AllowedMentions: webshare.AllowedMentionSelection{Users: []string{"222222222222222222"}}}
	b.websharePostChannelMessage(context.Background(), share, action, "channel-1", "")
	paths, bodies := rt.Snapshot()
	if len(bodies) != 1 {
		t.Fatalf("discord requests = %d paths=%v", len(bodies), paths)
	}
	var payload struct {
		Content         string `json:"content"`
		Username        string `json:"username"`
		AllowedMentions struct {
			Parse []string `json:"parse"`
			Users []string `json:"users"`
			Roles []string `json:"roles"`
		} `json:"allowed_mentions"`
		Flags int `json:"flags"`
	}
	if err := json.Unmarshal([]byte(bodies[0]), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Username != "Alice via WebShare" || strings.Contains(payload.Content, "via WebShare") || !strings.Contains(payload.Content, "<@222222222222222222>") {
		t.Fatalf("webhook payload = username %q content %q", payload.Username, payload.Content)
	}
	if strings.Contains(payload.Content, "<@333333333333333333>") {
		t.Fatalf("raw mention stayed active: %q", payload.Content)
	}
	if len(payload.AllowedMentions.Users) != 1 || payload.AllowedMentions.Users[0] != "222222222222222222" {
		t.Fatalf("allowed users = %+v", payload.AllowedMentions.Users)
	}
	if len(payload.AllowedMentions.Parse) != 0 || len(payload.AllowedMentions.Roles) != 0 {
		t.Fatalf("allowed mention parse/roles should be empty: %+v", payload.AllowedMentions)
	}
}

func TestWebSharePostMessageHonorsMentionCapabilities(t *testing.T) {
	L.Load("en")
	rt := &recordingDiscordTransport{}
	ds := &discordgo.Session{State: discordgo.NewState(), Client: &http.Client{Transport: rt}, Ratelimiter: discordgo.NewRatelimiter()}
	ds.State.User = &discordgo.User{ID: "999999999999999999", Username: "bot", Bot: true}
	_ = ds.State.GuildAdd(&discordgo.Guild{ID: "guild-1"})
	_ = ds.State.MemberAdd(&discordgo.Member{GuildID: "guild-1", User: &discordgo.User{ID: "222222222222222222", Username: "Bob"}})
	b := &Bot{discord: ds, downloadClient: &http.Client{}, attachmentMaxBytes: 1024, webshareWebhookByChannel: map[string]webshareWebhookCredential{"channel-1": {ID: "webshare-hook-1", Token: "webshare-token-1"}}, webshareWebhookIDs: map[string]bool{}}
	share := webshare.Share{ShareID: "ws_1", GuildID: "guild-1", TargetID: "channel-1", OpenerUserID: "111111111111111111", OpenerUsername: "Alice", Capabilities: webshare.Capabilities{Write: true, PostChannelMessage: true}}
	action := webshare.ClientAction{Text: "hello [[discord:user:222222222222222222]] [[discord:user:999999999999999999]]", AllowedMentions: webshare.AllowedMentionSelection{Users: []string{"222222222222222222"}, Bot: true}}
	b.websharePostChannelMessage(context.Background(), share, action, "channel-1", "")
	_, bodies := rt.Snapshot()
	if len(bodies) != 1 {
		t.Fatalf("discord requests = %d", len(bodies))
	}
	var payload struct {
		Content         string `json:"content"`
		AllowedMentions struct {
			Users []string `json:"users"`
		} `json:"allowed_mentions"`
	}
	if err := json.Unmarshal([]byte(bodies[0]), &payload); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payload.Content, "<@222222222222222222>") || strings.Contains(payload.Content, "<@999999999999999999>") {
		t.Fatalf("mention capability bypass in content: %q", payload.Content)
	}
	if len(payload.AllowedMentions.Users) != 0 {
		t.Fatalf("allowed users = %+v, want none", payload.AllowedMentions.Users)
	}
}

func TestWebSharePostMessageReportsWebhookPermissionMissing(t *testing.T) {
	L.Load("en")
	ds := testPeerPermissionSession(t, nil)
	b := &Bot{discord: ds, downloadClient: &http.Client{}, attachmentMaxBytes: 1024}
	share := webshare.Share{ShareID: "ws_1", GuildID: "guild-1", TargetID: "channel-1", OpenerUserID: "111111111111111111", OpenerUsername: "Alice", Capabilities: webshare.WriteCapabilities()}

	got := b.websharePostChannelMessage(context.Background(), share, webshare.ClientAction{Text: "hello"}, "channel-1", "")
	if got.Status != "rejected" || got.ReasonCode != "discord_webhook_permission_missing" || !strings.Contains(got.Content, "Manage Webhooks") {
		t.Fatalf("post result = %+v, want localized webhook permission error", got)
	}
}

func TestWebSharePromptRecordContentMentionsBotAndStripsTrigger(t *testing.T) {
	L.Load("en")
	ds := &discordgo.Session{State: discordgo.NewState()}
	ds.State.User = &discordgo.User{ID: "999999999999999999", Username: "M5Bot", Bot: true}
	b := &Bot{discord: ds}
	share := webshare.Share{OpenerUsername: "Alice", Capabilities: webshare.WriteCapabilities()}
	text := b.stripWebShareBotMention("@M5Bot please check")
	if text != "please check" {
		t.Fatalf("stripped text = %q", text)
	}
	content, allowed := b.websharePromptRecordBody(share, text, nil)
	if content != "<@999999999999999999> please check" {
		t.Fatalf("visible prompt content = %q", content)
	}
	if len(allowed.Users) != 1 || allowed.Users[0] != "999999999999999999" {
		t.Fatalf("allowed users = %+v", allowed.Users)
	}
}

func TestWebShareOwnedWebhookMessageDoesNotReenterAgentQueue(t *testing.T) {
	L.Load("en")
	ds := testPeerPermissionSession(t, nil)
	transport := &countingDiscordTransport{}
	ds.Client = &http.Client{Transport: transport}
	mgr := channel.NewManager(channel.ManagerConfig{})
	mgr.SetWebhookListen("channel-1", true)
	b := &Bot{
		manager:             mgr,
		discord:             ds,
		seen:                newSeenMessages(),
		setupPromptCooldown: newSetupPromptCooldown(nil),
		webshareWebhookIDs:  map[string]bool{"webshare-hook-1": true},
	}
	defer b.seen.Stop()

	msg := &discordgo.MessageCreate{Message: &discordgo.Message{
		ID:        "message-1",
		ChannelID: "channel-1",
		GuildID:   "guild-1",
		Content:   "<@bot-1> hi",
		Author:    &discordgo.User{ID: "webhook-author", Username: "Alice via WebShare", Bot: true},
		WebhookID: "webshare-hook-1",
	}}
	b.handleMessage(ds, msg)
	if transport.Count() != 0 {
		t.Fatalf("webshare-owned webhook should not trigger Discord setup/enqueue side effects, got %d sends", transport.Count())
	}
}

func TestWebSharePostMessageSendsUploadedAttachments(t *testing.T) {
	L.Load("en")
	store, share := newTestWebShareStoreAndShare(t)
	project := t.TempDir()
	manager := channel.NewManager(channel.ManagerConfig{DataDir: t.TempDir(), DefaultCWD: project})
	rt := &recordingDiscordTransport{}
	ds := &discordgo.Session{State: discordgo.NewState(), Client: &http.Client{Transport: rt}, Ratelimiter: discordgo.NewRatelimiter()}
	b := &Bot{discord: ds, manager: manager, webshareStore: store, attachmentMaxBytes: 1024, webshareWebhookByChannel: map[string]webshareWebhookCredential{"channel-1": {ID: "webshare-hook-1", Token: "webshare-token-1"}}, webshareWebhookIDs: map[string]bool{}}
	cwd, err := manager.ValidateCWD(project)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("file body")
	p := filepath.Join(cwd, ".kiro-bot", "attachments", "webshare-"+share.ShareID, "up_1", "note.txt")
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, content, 0644); err != nil {
		t.Fatal(err)
	}
	ref, err := store.IssueAttachmentRef(context.Background(), webshare.AttachmentRef{ShareID: share.ShareID, TargetID: share.TargetID, MessageID: "web-upload-up_1", AttachmentID: "up_1", Filename: "note.txt", Size: int64(len(content)), ContentType: "text/plain", Metadata: map[string]any{"local_path": p}})
	if err != nil {
		t.Fatal(err)
	}
	got := b.websharePostChannelMessage(context.Background(), share, webshare.ClientAction{Text: "with file", Attachments: []webshare.AttachmentRef{{ID: ref.ID}}}, "channel-1", "")
	if got.Status != "ok" {
		t.Fatalf("post result = %+v", got)
	}
	_, bodies := rt.Snapshot()
	if len(bodies) != 1 {
		t.Fatalf("discord requests = %d", len(bodies))
	}
	if !strings.Contains(bodies[0], "note.txt") || !strings.Contains(bodies[0], "file body") {
		t.Fatalf("multipart body missing attachment name or bytes: %q", bodies[0])
	}
}

func TestWebShareThreadCreateValidationAuditRedactionAndAttachmentFetch(t *testing.T) {
	store, share := newTestWebShareStoreAndShare(t)
	project := t.TempDir()
	manager := channel.NewManager(channel.ManagerConfig{DataDir: t.TempDir(), DefaultCWD: project})
	b := &Bot{manager: manager, webshareStore: store, attachmentMaxBytes: 1024}
	threadShare := share
	threadShare.TargetType = webshare.TargetThread
	if got := b.webshareCreateThread(context.Background(), threadShare, webshare.ClientAction{Name: "child"}); got.Status != "rejected" {
		t.Fatalf("thread share create thread status = %s", got.Status)
	}
	meta := sanitizeWebShareMetadata(map[string]any{"safe": "ok", "control_link": "https://secret", "local_path": "/tmp/secret", "token": "secret"})
	if _, ok := meta["safe"]; !ok || len(meta) != 1 {
		t.Fatalf("sanitized metadata = %#v", meta)
	}

	cwd, err := manager.ValidateCWD(project)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(cwd, ".kiro-bot", "attachments", "webshare-"+share.ShareID, "up_1", "note.txt")
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	ref, err := store.IssueAttachmentRef(context.Background(), webshare.AttachmentRef{ShareID: share.ShareID, TargetID: share.TargetID, MessageID: "web-upload-up_1", AttachmentID: "up_1", Filename: "note.txt", Size: 5, Metadata: map[string]any{"local_path": p}})
	if err != nil {
		t.Fatal(err)
	}
	got := b.webshareFetchAttachment(context.Background(), share, webshare.ClientAction{AttachmentRef: ref.ID})
	if got.Status != "ok" || got.Chunk != base64.StdEncoding.EncodeToString([]byte("hello")) {
		t.Fatalf("fetch event = %+v", got)
	}
	if strings.Contains(got.Content, p) || strings.Contains(got.Chunk, p) {
		t.Fatalf("fetch exposed local path: %+v", got)
	}
	if _, err := store.ResolveAttachmentRef(context.Background(), "wrong-share", ref.ID); err == nil {
		t.Fatal("wrong share resolved attachment ref")
	}
}

func TestWebShareCreateThreadIncludesServerTimestamp(t *testing.T) {
	store, share := newTestWebShareStoreAndShare(t)
	manager := channel.NewManager(channel.ManagerConfig{DataDir: t.TempDir(), DefaultCWD: t.TempDir()})
	rt := &recordingDiscordTransport{}
	ds := &discordgo.Session{State: discordgo.NewState(), Client: &http.Client{Transport: rt}, Ratelimiter: discordgo.NewRatelimiter()}
	b := &Bot{discord: ds, manager: manager, webshareStore: store}

	before := time.Now().UTC()
	got := b.webshareCreateThread(context.Background(), share, webshare.ClientAction{Name: "review"})
	if got.Type != "thread_event" || got.Status != "ok" {
		t.Fatalf("create thread event = %+v", got)
	}
	payload := got.Event.(map[string]any)
	raw, ok := payload["timestamp"].(string)
	if !ok || raw == "" {
		t.Fatalf("timestamp missing: %#v", payload)
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		t.Fatalf("timestamp parse: %v", err)
	}
	if parsed.Before(before.Add(-time.Second)) || parsed.After(time.Now().UTC().Add(time.Second)) {
		t.Fatalf("timestamp = %s outside server receive window", parsed)
	}
}

func TestWebShareChunkedUploadIssuesScopedAttachmentRef(t *testing.T) {
	store, share := newTestWebShareStoreAndShare(t)
	project := t.TempDir()
	manager := channel.NewManager(channel.ManagerConfig{DataDir: t.TempDir(), DefaultCWD: project})
	b := &Bot{manager: manager, webshareStore: store, attachmentMaxBytes: 1024}
	content := []byte("hello")
	sum := sha256.Sum256(content)

	init := b.webshareUploadInit(context.Background(), share, webshare.ClientAction{Name: "note.txt", MIME: "text/plain", Size: int64(len(content)), SHA256: base64.RawURLEncoding.EncodeToString(sum[:])}, share.TargetID, "")
	if init.Type != "upload_state" || init.Status != "accepted" || init.UploadID == "" {
		t.Fatalf("init = %+v", init)
	}
	chunk := b.webshareUploadChunk(share, webshare.ClientAction{UploadID: init.UploadID, Seq: 0, Bytes: base64.RawURLEncoding.EncodeToString(content)})
	if chunk.Status != "received" {
		t.Fatalf("chunk = %+v", chunk)
	}
	finish := b.webshareUploadFinish(context.Background(), share, webshare.ClientAction{UploadID: init.UploadID})
	if finish.Status != "complete" {
		t.Fatalf("finish = %+v", finish)
	}
	ref, err := store.ResolveAttachmentRef(context.Background(), share.ShareID, init.UploadID)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := ref.Metadata["local_path"].(string)
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) || ref.TargetID != share.TargetID || ref.ContentType != "text/plain" {
		t.Fatalf("ref=%+v content=%q", ref, got)
	}
}

func TestWebShareBroadcastsLiveDiscordMessage(t *testing.T) {
	store, share := newTestWebShareStoreAndShare(t)
	ch := make(chan webshare.ServerEvent, 1)
	b := &Bot{webshareStore: store, webshareHosts: map[string]*webshareHostLoop{share.ShareID: {send: ch}}}
	msg := &discordgo.MessageCreate{Message: &discordgo.Message{ID: "msg-1", ChannelID: share.TargetID, GuildID: share.GuildID, Content: "hello", Author: &discordgo.User{ID: "user-2", Username: "Bob"}}}
	b.broadcastWebShareDiscordMessage(context.Background(), msg, "")
	select {
	case event := <-ch:
		if event.Type != "channel_event" || event.Status != "ok" {
			t.Fatalf("event = %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("missing live event")
	}
}

func TestWebShareThreadTargetBroadcastUsesShareParentWhenResolverMisses(t *testing.T) {
	store, err := webshare.OpenStore(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	mat, err := webshare.GenerateSecretMaterial()
	if err != nil {
		t.Fatal(err)
	}
	share, err := store.CreateShare(context.Background(), webshare.CreateShareRequest{ShareID: mat.ShareID, GuildID: "guild-1", TargetType: webshare.TargetThread, TargetID: "thread-1", ParentChannelID: "channel-1", OpenerUserID: "viewer", OpenerUsername: "Viewer", RelayURL: "wss://relay/r", PublicBaseURL: "https://relay", RoomID: mat.RoomID, RoomKey: mat.RoomKey, WriteToken: mat.WriteToken, Capabilities: webshare.WriteCapabilities(), Status: webshare.StatusActive, Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	ch := make(chan webshare.ServerEvent, 3)
	b := &Bot{webshareStore: store, webshareHosts: map[string]*webshareHostLoop{share.ShareID: {send: ch}}}

	createdAt := time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Minute)
	deletedAt := createdAt.Add(2 * time.Minute)
	b.broadcastWebShareDiscordMessage(context.Background(), &discordgo.MessageCreate{Message: &discordgo.Message{
		ID: "msg-create", ChannelID: "thread-1", GuildID: "guild-1", Content: "created", Timestamp: createdAt, Author: &discordgo.User{ID: "user-2", Username: "Bob"},
	}}, "")
	b.broadcastWebShareDiscordMessageUpdate(context.Background(), &discordgo.MessageUpdate{Message: &discordgo.Message{
		ID: "msg-update", ChannelID: "thread-1", GuildID: "guild-1", Content: "updated", Timestamp: updatedAt,
	}}, "")
	b.broadcastWebShareDiscordMessageDelete(context.Background(), &discordgo.MessageDelete{Message: &discordgo.Message{
		ID: "msg-delete", ChannelID: "thread-1", GuildID: "guild-1", Timestamp: deletedAt,
	}}, "")

	for i, tc := range []struct{ action, timestamp string }{{"message", createdAt.Format(time.RFC3339Nano)}, {"updated", updatedAt.Format(time.RFC3339Nano)}, {"deleted", deletedAt.Format(time.RFC3339Nano)}} {
		select {
		case event := <-ch:
			payload := event.Event.(map[string]any)
			thread := payload["thread"].(map[string]any)
			if event.Type != "thread_event" || payload["action"] != tc.action || payload["timestamp"] != tc.timestamp || thread["id"] != "thread-1" || thread["parentChannelID"] != "channel-1" {
				t.Fatalf("event %d = %+v payload=%#v", i, event, payload)
			}
		case <-time.After(time.Second):
			t.Fatalf("missing thread event %d", i)
		}
	}
}

func TestWebShareDeleteBroadcastUsesServerTimestampFallback(t *testing.T) {
	store, share := newTestWebShareStoreAndShare(t)
	ch := make(chan webshare.ServerEvent, 1)
	b := &Bot{webshareStore: store, webshareHosts: map[string]*webshareHostLoop{share.ShareID: {send: ch}}}
	before := time.Now().UTC()
	b.broadcastWebShareDiscordMessageDelete(context.Background(), &discordgo.MessageDelete{Message: &discordgo.Message{ID: "msg-delete-zero", ChannelID: share.TargetID, GuildID: share.GuildID}}, "")
	select {
	case event := <-ch:
		payload := event.Event.(map[string]any)
		raw, ok := payload["timestamp"].(string)
		if !ok || raw == "" {
			t.Fatalf("timestamp missing: %#v", payload)
		}
		got, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			t.Fatalf("timestamp parse: %v", err)
		}
		if got.Before(before.Add(-time.Second)) || got.After(time.Now().UTC().Add(time.Second)) {
			t.Fatalf("timestamp = %s outside server receive window", got)
		}
	case <-time.After(time.Second):
		t.Fatal("missing delete event")
	}
}

func TestWebShareBroadcastIncludesReplyReference(t *testing.T) {
	store, share := newTestWebShareStoreAndShare(t)
	ch := make(chan webshare.ServerEvent, 1)
	b := &Bot{webshareStore: store, webshareHosts: map[string]*webshareHostLoop{share.ShareID: {send: ch}}}
	msg := &discordgo.MessageCreate{Message: &discordgo.Message{
		ID:        "reply-1",
		ChannelID: share.TargetID,
		GuildID:   share.GuildID,
		Content:   "reply body",
		Author:    &discordgo.User{ID: "user-2", Username: "Bob"},
		MessageReference: &discordgo.MessageReference{
			MessageID: "root-1",
			ChannelID: share.TargetID,
			GuildID:   share.GuildID,
		},
		ReferencedMessage: &discordgo.Message{
			ID:        "root-1",
			ChannelID: share.TargetID,
			GuildID:   share.GuildID,
			Content:   "root context",
			Author:    &discordgo.User{ID: "user-1", Username: "Alice"},
		},
	}}
	b.broadcastWebShareDiscordMessage(context.Background(), msg, "")
	event := <-ch
	payload := event.Event.(map[string]any)
	replyTo, ok := payload["replyTo"].(map[string]any)
	if !ok {
		t.Fatalf("replyTo missing: %#v", payload)
	}
	if replyTo["messageID"] != "root-1" || replyTo["content"] != "root context" {
		t.Fatalf("replyTo = %#v", replyTo)
	}
	author := replyTo["author"].(map[string]any)
	if author["displayName"] != "Alice" {
		t.Fatalf("reply author = %#v", author)
	}
}

func TestWebShareBroadcastIncludesFriendlyMentionMetadata(t *testing.T) {
	store, share := newTestWebShareStoreAndShare(t)
	ch := make(chan webshare.ServerEvent, 1)
	ds := &discordgo.Session{State: discordgo.NewState()}
	ds.State.User = &discordgo.User{ID: "bot-1", Username: "M5Bot", Bot: true}
	b := &Bot{discord: ds, webshareStore: store, webshareHosts: map[string]*webshareHostLoop{share.ShareID: {send: ch}}}
	msg := &discordgo.MessageCreate{Message: &discordgo.Message{
		ID:        "msg-mention-1",
		ChannelID: share.TargetID,
		GuildID:   share.GuildID,
		Content:   "<@bot-1> hi <@user-2>",
		Author:    &discordgo.User{ID: "user-1", Username: "Alice"},
		Mentions: []*discordgo.User{
			{ID: "bot-1", Username: "M5Bot", Bot: true},
			{ID: "user-2", Username: "Bob"},
		},
	}}
	b.broadcastWebShareDiscordMessage(context.Background(), msg, "")
	event := <-ch
	payload, ok := event.Event.(map[string]any)
	if !ok {
		t.Fatalf("event payload = %T", event.Event)
	}
	mentions, ok := payload["mentions"].([]map[string]any)
	if !ok || len(mentions) != 2 {
		t.Fatalf("mentions = %#v", payload["mentions"])
	}
	if mentions[0]["id"] != "bot-1" || mentions[0]["displayName"] != "M5Bot" || mentions[0]["kind"] != "bot" {
		t.Fatalf("bot mention metadata = %#v", mentions[0])
	}
}

func TestWebShareBroadcastsOtherBotMessagesWithoutAgentHandoff(t *testing.T) {
	store, share := newTestWebShareStoreAndShare(t)
	ch := make(chan webshare.ServerEvent, 1)
	ds := testPeerPermissionSession(t, nil)
	b := &Bot{
		guildID:             "guild-1",
		discord:             ds,
		manager:             channel.NewManager(channel.ManagerConfig{}),
		seen:                newSeenMessages(),
		setupPromptCooldown: newSetupPromptCooldown(nil),
		webshareStore:       store,
		webshareHosts:       map[string]*webshareHostLoop{share.ShareID: {send: ch}},
	}
	defer b.seen.Stop()
	b.handleMessage(ds, &discordgo.MessageCreate{Message: &discordgo.Message{
		ID:        "other-bot-message-1",
		ChannelID: share.TargetID,
		GuildID:   share.GuildID,
		Content:   "other bot result visible in Discord",
		Author:    &discordgo.User{ID: "bot-2", Username: "HelperBot", Bot: true},
	}})
	select {
	case event := <-ch:
		payload := event.Event.(map[string]any)
		if event.Type != "channel_event" || payload["messageID"] != "other-bot-message-1" || payload["content"] != "other bot result visible in Discord" {
			t.Fatalf("event = %+v payload=%#v", event, payload)
		}
	case <-time.After(time.Second):
		t.Fatal("missing other bot live event")
	}
}

func TestWebShareBroadcastsThreadLifecycleAndRegistersChild(t *testing.T) {
	store, share := newTestWebShareStoreAndShare(t)
	ch := make(chan webshare.ServerEvent, 1)
	b := &Bot{webshareStore: store, webshareHosts: map[string]*webshareHostLoop{share.ShareID: {send: ch}}}
	thread := &discordgo.Channel{ID: "thread-42", GuildID: share.GuildID, ParentID: share.TargetID, Name: "Spec Review", Type: discordgo.ChannelTypeGuildPublicThread}
	b.broadcastWebShareThreadLifecycle(context.Background(), thread, "created")
	select {
	case event := <-ch:
		payload := event.Event.(map[string]any)
		threadView := payload["thread"].(map[string]any)
		if event.Type != "thread_event" || payload["action"] != "created" || threadView["name"] != "Spec Review" {
			t.Fatalf("event = %+v payload=%#v", event, payload)
		}
	case <-time.After(time.Second):
		t.Fatal("missing thread lifecycle event")
	}
	child, err := store.ResolveManagedChildThread(context.Background(), share.ShareID, "thread-42")
	if err != nil || child.ParentChannelID != share.TargetID {
		t.Fatalf("registered child = %+v err=%v", child, err)
	}
	b.broadcastWebShareThreadLifecycle(context.Background(), thread, "deleted")
	select {
	case event := <-ch:
		payload := event.Event.(map[string]any)
		if event.Type != "thread_event" || payload["action"] != "deleted" {
			t.Fatalf("delete event = %+v payload=%#v", event, payload)
		}
	case <-time.After(time.Second):
		t.Fatal("missing thread delete event")
	}
	children, err := store.ListManagedChildThreads(context.Background(), share.ShareID)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 0 {
		t.Fatalf("deleted child threads = %+v, want none", children)
	}
}

func TestWebShareSkipsUnmanagedThreadLifecycle(t *testing.T) {
	for _, action := range []string{"updated", "deleted"} {
		t.Run(action, func(t *testing.T) {
			store, share := newTestWebShareStoreAndShare(t)
			ch := make(chan webshare.ServerEvent, 1)
			b := &Bot{webshareStore: store, webshareHosts: map[string]*webshareHostLoop{share.ShareID: {send: ch}}}
			thread := &discordgo.Channel{ID: "thread-42", GuildID: share.GuildID, ParentID: share.TargetID, Name: "Historic Thread", Type: discordgo.ChannelTypeGuildPublicThread}

			b.broadcastWebShareThreadLifecycle(context.Background(), thread, action)
			select {
			case event := <-ch:
				t.Fatalf("unmanaged historic thread %s was broadcast: %+v", action, event)
			case <-time.After(100 * time.Millisecond):
			}
			children, err := store.ListManagedChildThreads(context.Background(), share.ShareID)
			if err != nil {
				t.Fatal(err)
			}
			if len(children) != 0 {
				t.Fatalf("unmanaged historic thread was registered: %+v", children)
			}
		})
	}
}

func TestWebShareBroadcastsManagedThreadUpdates(t *testing.T) {
	store, share := newTestWebShareStoreAndShare(t)
	if err := store.RegisterManagedChildThread(context.Background(), webshare.ManagedChildThread{ShareID: share.ShareID, ParentChannelID: share.TargetID, ThreadID: "thread-42", Name: "Old Name"}); err != nil {
		t.Fatal(err)
	}
	ch := make(chan webshare.ServerEvent, 1)
	b := &Bot{webshareStore: store, webshareHosts: map[string]*webshareHostLoop{share.ShareID: {send: ch}}}
	thread := &discordgo.Channel{ID: "thread-42", GuildID: share.GuildID, ParentID: share.TargetID, Name: "New Name", Type: discordgo.ChannelTypeGuildPublicThread}

	b.broadcastWebShareThreadLifecycle(context.Background(), thread, "updated")
	select {
	case event := <-ch:
		payload := event.Event.(map[string]any)
		threadView := payload["thread"].(map[string]any)
		if event.Type != "thread_event" || payload["action"] != "updated" || threadView["name"] != "New Name" {
			t.Fatalf("managed update event = %+v payload=%#v", event, payload)
		}
	case <-time.After(time.Second):
		t.Fatal("missing managed thread update")
	}
	child, err := store.ResolveManagedChildThread(context.Background(), share.ShareID, "thread-42")
	if err != nil || child.Name != "New Name" {
		t.Fatalf("updated child = %+v err=%v", child, err)
	}
}

func TestWebShareWelcomeListsManagedChildThreads(t *testing.T) {
	store, share := newTestWebShareStoreAndShare(t)
	if err := store.RegisterManagedChildThread(context.Background(), webshare.ManagedChildThread{ShareID: share.ShareID, ParentChannelID: share.TargetID, ThreadID: "thread-42", Name: "Spec Review"}); err != nil {
		t.Fatal(err)
	}
	b := &Bot{webshareStore: store}
	event := b.webshareWelcomeEvent(share)
	threads, ok := event.Threads.([]map[string]any)
	if !ok || len(threads) != 1 {
		t.Fatalf("threads = %#v", event.Threads)
	}
	if threads[0]["id"] != "thread-42" || threads[0]["name"] != "Spec Review" || threads[0]["parentChannelID"] != share.TargetID {
		t.Fatalf("thread view = %#v", threads[0])
	}
}

func waitDiscordRequests(t *testing.T, rt *recordingDiscordTransport, want int) ([]string, []string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		paths, bodies := rt.Snapshot()
		if len(paths) >= want {
			return paths, bodies
		}
		time.Sleep(10 * time.Millisecond)
	}
	paths, bodies := rt.Snapshot()
	t.Fatalf("discord requests = %d, want at least %d: paths=%v bodies=%v", len(paths), want, paths, bodies)
	return nil, nil
}

func TestWebShareSkipsPartialMessageUpdates(t *testing.T) {
	store, share := newTestWebShareStoreAndShare(t)
	ch := make(chan webshare.ServerEvent, 1)
	b := &Bot{guildID: share.GuildID, webshareStore: store, webshareHosts: map[string]*webshareHostLoop{share.ShareID: {send: ch}}}
	b.handleMessageUpdate(testPeerPermissionSession(t, nil), &discordgo.MessageUpdate{Message: &discordgo.Message{
		ID:        "message-1",
		ChannelID: share.TargetID,
		GuildID:   share.GuildID,
	}})
	select {
	case event := <-ch:
		t.Fatalf("partial update should not broadcast: %+v", event)
	default:
	}
}

func TestWebShareMessageUpdateCanClearAttachments(t *testing.T) {
	store, share := newTestWebShareStoreAndShare(t)
	ch := make(chan webshare.ServerEvent, 1)
	b := &Bot{webshareStore: store, webshareHosts: map[string]*webshareHostLoop{share.ShareID: {send: ch}}}
	emptyAttachments := []*discordgo.MessageAttachment{}
	b.broadcastWebShareDiscordMessageUpdate(context.Background(), &discordgo.MessageUpdate{Message: &discordgo.Message{
		ID:          "message-2",
		ChannelID:   share.TargetID,
		GuildID:     share.GuildID,
		Content:     "edited text",
		Attachments: emptyAttachments,
	}}, "")
	select {
	case event := <-ch:
		payload := event.Event.(map[string]any)
		attachments, ok := payload["attachments"].([]map[string]any)
		if !ok || len(attachments) != 0 {
			t.Fatalf("attachments = %#v", payload["attachments"])
		}
	case <-time.After(time.Second):
		t.Fatal("missing message update event")
	}
}

func ctxWithArgs(ctx cmdCtx, args string) cmdCtx {
	ctx.args = args
	return ctx
}

func mustSessionStore(t *testing.T, dataDir string) *channel.SessionStore {
	t.Helper()
	store, err := channel.NewSessionStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func newTestWebShareStoreAndShare(t *testing.T) (*webshare.Store, webshare.Share) {
	t.Helper()
	store, err := webshare.OpenStore(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	mat, err := webshare.GenerateSecretMaterial()
	if err != nil {
		t.Fatal(err)
	}
	share, err := store.CreateShare(context.Background(), webshare.CreateShareRequest{ShareID: mat.ShareID, GuildID: "guild-1", TargetType: webshare.TargetChannel, TargetID: "channel-1", OpenerUserID: "viewer", OpenerUsername: "Viewer", RelayURL: "wss://relay/r", PublicBaseURL: "https://relay", RoomID: mat.RoomID, RoomKey: mat.RoomKey, WriteToken: mat.WriteToken, Capabilities: webshare.WriteCapabilities(), Status: webshare.StatusActive, Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	return store, *share
}
