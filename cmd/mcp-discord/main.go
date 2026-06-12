package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/nczz/kiro-discord-bot/internal/discordfmt"
	"github.com/nczz/kiro-discord-bot/internal/secrets"
)

var dg *discordgo.Session
var policy discordPolicy

const discordMessageLimit = 1900
const discordEmbedDescriptionLimit = 3900

type discordPolicy struct {
	allowedGuilds     map[string]struct{}
	allowedChannels   map[string]struct{}
	readOnly          bool
	allowedWriteTools map[string]struct{}
	allowDestructive  bool
}

func loadDiscordPolicy() discordPolicy {
	return discordPolicy{
		allowedGuilds:     parseIDSet(os.Getenv("MCP_DISCORD_ALLOWED_GUILDS")),
		allowedChannels:   parseIDSet(os.Getenv("MCP_DISCORD_ALLOWED_CHANNELS")),
		readOnly:          envBool("MCP_DISCORD_READ_ONLY", false),
		allowedWriteTools: parseIDSet(os.Getenv("MCP_DISCORD_ALLOWED_WRITE_TOOLS")),
		allowDestructive:  envBool("MCP_DISCORD_ALLOW_DESTRUCTIVE", true),
	}
}

func parseIDSet(raw string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, part := range strings.Split(raw, ",") {
		id := strings.TrimSpace(part)
		if id == "" {
			continue
		}
		out[id] = struct{}{}
	}
	return out
}

func (p discordPolicy) guildAllowed(guildID string) bool {
	if len(p.allowedGuilds) == 0 {
		return true
	}
	_, ok := p.allowedGuilds[guildID]
	return ok
}

func (p discordPolicy) channelIDAllowed(channelID string) bool {
	if len(p.allowedChannels) == 0 {
		return true
	}
	_, ok := p.allowedChannels[channelID]
	return ok
}

func (p discordPolicy) writeAllowed(tool string, destructive bool) error {
	if p.readOnly {
		return fmt.Errorf("%s is blocked because MCP_DISCORD_READ_ONLY=true", tool)
	}
	if len(p.allowedWriteTools) > 0 {
		if _, ok := p.allowedWriteTools[tool]; !ok {
			return fmt.Errorf("%s is not allowed by MCP_DISCORD_ALLOWED_WRITE_TOOLS", tool)
		}
	}
	if destructive && !p.allowDestructive {
		return fmt.Errorf("%s is blocked because MCP_DISCORD_ALLOW_DESTRUCTIVE=false", tool)
	}
	return nil
}

func envBool(key string, def bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if v == "" {
		return def
	}
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

func ensureGuildAllowed(guildID string) error {
	if policy.guildAllowed(guildID) {
		return nil
	}
	return fmt.Errorf("guild %s is not allowed by MCP_DISCORD_ALLOWED_GUILDS", guildID)
}

func ensureChannelAllowed(channelID string) error {
	if !policy.channelIDAllowed(channelID) {
		return fmt.Errorf("channel %s is not allowed by MCP_DISCORD_ALLOWED_CHANNELS", channelID)
	}
	if len(policy.allowedGuilds) == 0 {
		return nil
	}
	ch, err := dg.Channel(channelID)
	if err != nil {
		return fmt.Errorf("resolve channel guild: %w", err)
	}
	if ch.GuildID == "" {
		return fmt.Errorf("channel %s has no guild_id and guild allowlist is enabled", channelID)
	}
	return ensureGuildAllowed(ch.GuildID)
}

func resolveWriteTargetChannel(requestedChannelID string) string {
	target := currentTargetStateChannelID()
	if target == "" {
		return requestedChannelID
	}
	if err := ensureGuildForDynamicTarget(target); err != nil {
		log.Printf("[mcp-discord] ignoring dynamic target %s for requested channel %s: %v", target, requestedChannelID, err)
		return requestedChannelID
	}
	return target
}

func currentTargetStateChannelID() string {
	path := strings.TrimSpace(os.Getenv("BOT_TOOLS_TARGET_STATE_PATH"))
	if path == "" {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var state struct {
		TargetChannelID string `json:"target_channel_id"`
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return ""
	}
	return strings.TrimSpace(state.TargetChannelID)
}

func ensureGuildForDynamicTarget(channelID string) error {
	if len(policy.allowedGuilds) == 0 {
		return nil
	}
	ch, err := dg.Channel(channelID)
	if err != nil {
		return err
	}
	return ensureGuildAllowed(ch.GuildID)
}

func ensureWriteAllowed(tool string, destructive bool) error {
	return policy.writeAllowed(tool, destructive)
}

func validateDiscordAttachmentURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "https" {
		return nil, fmt.Errorf("attachment url must use https")
	}
	switch strings.ToLower(u.Hostname()) {
	case "cdn.discordapp.com", "media.discordapp.net", "attachments.discordapp.net":
		return u, nil
	default:
		return nil, fmt.Errorf("attachment url host %q is not allowed", u.Hostname())
	}
}

func resolveDownloadDir(requested string) (string, error) {
	root := strings.TrimSpace(os.Getenv("MCP_DISCORD_DOWNLOAD_DIR"))
	if root == "" {
		return filepath.Abs(requested)
	}
	if requested == "" || requested == os.TempDir() {
		requested = root
	}
	absRequested, err := filepath.Abs(requested)
	if err != nil {
		return "", err
	}
	policyRoot, err := canonicalPathForPolicy(root)
	if err != nil {
		return "", err
	}
	policyRequested, err := canonicalPathForPolicy(absRequested)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(policyRoot, policyRequested)
	if err != nil {
		return "", err
	}
	if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))) {
		return absRequested, nil
	}
	return "", fmt.Errorf("save_dir must be inside MCP_DISCORD_DOWNLOAD_DIR (%s)", policyRoot)
}

func canonicalPathForPolicy(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}
	parent := abs
	var missing []string
	for {
		next := filepath.Dir(parent)
		if next == parent {
			return "", fmt.Errorf("no existing parent for %s", abs)
		}
		missing = append([]string{filepath.Base(parent)}, missing...)
		parent = next
		resolvedParent, err := filepath.EvalSymlinks(parent)
		if err == nil {
			parts := append([]string{resolvedParent}, missing...)
			return filepath.Join(parts...), nil
		}
	}
}

func safeAttachmentFilename(raw string) string {
	name := filepath.Base(raw)
	if idx := strings.Index(name, "?"); idx > 0 {
		name = name[:idx]
	}
	name = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '.', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, name)
	name = strings.Trim(name, ".-")
	if name == "" {
		return "attachment"
	}
	return name
}

func sendDiscordMessageParts(channelID, content string) ([]*discordgo.Message, error) {
	parts := discordfmt.Split(secrets.RedactEnv(content), discordMessageLimit)
	if len(parts) == 0 {
		return nil, fmt.Errorf("content is empty")
	}
	var sent []*discordgo.Message
	var firstErr error
	for i, part := range parts {
		if len(parts) > 1 {
			part = discordfmt.WithPartPrefix(part, i, len(parts))
		}
		msg, err := dg.ChannelMessageSendComplex(channelID, discordTextMessage(part))
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		sent = append(sent, msg)
	}
	return sent, firstErr
}

func replyDiscordMessageParts(channelID, messageID, content string) ([]*discordgo.Message, error) {
	parts := discordfmt.Split(secrets.RedactEnv(content), discordMessageLimit)
	if len(parts) == 0 {
		return nil, fmt.Errorf("content is empty")
	}
	var sent []*discordgo.Message
	var firstErr error
	for i, part := range parts {
		if len(parts) > 1 {
			part = discordfmt.WithPartPrefix(part, i, len(parts))
		}
		var (
			msg *discordgo.Message
			err error
		)
		if i == 0 {
			send := discordTextMessage(part)
			send.Reference = &discordgo.MessageReference{MessageID: messageID, ChannelID: channelID}
			msg, err = dg.ChannelMessageSendComplex(channelID, send)
		} else {
			msg, err = dg.ChannelMessageSendComplex(channelID, discordTextMessage(part))
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		sent = append(sent, msg)
	}
	return sent, firstErr
}

func messageIDs(messages []*discordgo.Message) string {
	ids := make([]string, 0, len(messages))
	for _, msg := range messages {
		if msg != nil && msg.ID != "" {
			ids = append(ids, msg.ID)
		}
	}
	return strings.Join(ids, ", ")
}

func discordTextMessage(content string) *discordgo.MessageSend {
	return &discordgo.MessageSend{
		Content:         content,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
		Flags:           discordgo.MessageFlagsSuppressEmbeds,
	}
}

func sendDiscordEmbedParts(channelID string, embed *discordgo.MessageEmbed) ([]*discordgo.Message, error) {
	if embed == nil {
		return nil, fmt.Errorf("embed is nil")
	}
	embed = redactEmbed(embed)
	parts := discordfmt.Split(embed.Description, discordEmbedDescriptionLimit)
	if len(parts) == 0 {
		parts = []string{""}
	}
	var sent []*discordgo.Message
	var firstErr error
	for i, part := range parts {
		next := *embed
		next.Description = part
		if len(parts) > 1 {
			next.Description = discordfmt.WithPartPrefix(part, i, len(parts))
			if i > 0 {
				next.Fields = nil
			}
		}
		msg, err := dg.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
			Embeds:          []*discordgo.MessageEmbed{&next},
			AllowedMentions: &discordgo.MessageAllowedMentions{},
			Flags:           discordgo.MessageFlagsSuppressEmbeds,
		})
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		sent = append(sent, msg)
	}
	return sent, firstErr
}

func redactEmbed(embed *discordgo.MessageEmbed) *discordgo.MessageEmbed {
	if embed == nil {
		return nil
	}
	next := *embed
	next.Title = secrets.RedactEnv(next.Title)
	next.Description = secrets.RedactEnv(next.Description)
	next.URL = secrets.RedactEnv(next.URL)
	if next.Footer != nil {
		footer := *next.Footer
		footer.Text = secrets.RedactEnv(footer.Text)
		next.Footer = &footer
	}
	if len(next.Fields) > 0 {
		fields := make([]*discordgo.MessageEmbedField, 0, len(next.Fields))
		for _, field := range next.Fields {
			if field == nil {
				continue
			}
			f := *field
			f.Name = secrets.RedactEnv(f.Name)
			f.Value = secrets.RedactEnv(f.Value)
			fields = append(fields, &f)
		}
		next.Fields = fields
	}
	return &next
}

func ensureDiscord() error {
	if dg != nil {
		return nil
	}
	token := os.Getenv("DISCORD_TOKEN")
	if token == "" {
		return fmt.Errorf("DISCORD_TOKEN not set")
	}
	var err error
	dg, err = discordgo.New("Bot " + token)
	if err != nil {
		return err
	}
	policy = loadDiscordPolicy()
	// Only REST API needed, no Gateway connection
	return nil
}

func main() {
	s := server.NewMCPServer("mcp-discord", "1.0.0", server.WithToolCapabilities(false))

	// 1. List channels
	s.AddTool(
		mcp.NewTool("discord_list_channels",
			mcp.WithDescription("List text channels in a guild"),
			mcp.WithString("guild_id", mcp.Required(), mcp.Description("Guild/server ID")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := ensureDiscord(); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			guildID, _ := req.RequireString("guild_id")
			if err := ensureGuildAllowed(guildID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			channels, err := dg.GuildChannels(guildID)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			var lines []string
			for _, ch := range channels {
				if ch.Type == discordgo.ChannelTypeGuildText {
					lines = append(lines, fmt.Sprintf("#%s (%s)", ch.Name, ch.ID))
				}
			}
			return mcp.NewToolResultText(strings.Join(lines, "\n")), nil
		},
	)

	// 2. Read messages
	s.AddTool(
		mcp.NewTool("discord_read_messages",
			mcp.WithDescription("Read recent messages from a channel"),
			mcp.WithString("channel_id", mcp.Required(), mcp.Description("Channel ID")),
			mcp.WithNumber("limit", mcp.Description("Number of messages, max 100, default 20")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := ensureDiscord(); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			chID, _ := req.RequireString("channel_id")
			if err := ensureChannelAllowed(chID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			limit := int(req.GetFloat("limit", 20))
			if limit > 100 {
				limit = 100
			}
			msgs, err := dg.ChannelMessages(chID, limit, "", "", "")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			var lines []string
			for i := len(msgs) - 1; i >= 0; i-- {
				m := msgs[i]
				lines = append(lines, fmt.Sprintf("[%s] %s: %s", time.Time(m.Timestamp).Format(time.RFC3339), m.Author.Username, m.Content))
			}
			return mcp.NewToolResultText(strings.Join(lines, "\n")), nil
		},
	)

	// 3. Send message
	s.AddTool(
		mcp.NewTool("discord_send_message",
			mcp.WithDescription("Send a message to a channel"),
			mcp.WithString("channel_id", mcp.Required(), mcp.Description("Channel ID")),
			mcp.WithString("content", mcp.Required(), mcp.Description("Message content")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := ensureDiscord(); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			chID, _ := req.RequireString("channel_id")
			if err := ensureChannelAllowed(chID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if err := ensureWriteAllowed("discord_send_message", false); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			chID = resolveWriteTargetChannel(chID)
			content, _ := req.RequireString("content")
			msgs, err := sendDiscordMessageParts(chID, content)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Sent %d message part(s): %s", len(msgs), messageIDs(msgs))), nil
		},
	)

	// 4. Reply to message
	s.AddTool(
		mcp.NewTool("discord_reply_message",
			mcp.WithDescription("Reply to a specific message"),
			mcp.WithString("channel_id", mcp.Required(), mcp.Description("Channel ID")),
			mcp.WithString("message_id", mcp.Required(), mcp.Description("Message ID to reply to")),
			mcp.WithString("content", mcp.Required(), mcp.Description("Reply content")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := ensureDiscord(); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			chID, _ := req.RequireString("channel_id")
			if err := ensureChannelAllowed(chID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if err := ensureWriteAllowed("discord_reply_message", false); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			msgID, _ := req.RequireString("message_id")
			content, _ := req.RequireString("content")
			msgs, err := replyDiscordMessageParts(chID, msgID, content)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Replied with %d message part(s): %s", len(msgs), messageIDs(msgs))), nil
		},
	)

	// 5. Add reaction
	s.AddTool(
		mcp.NewTool("discord_add_reaction",
			mcp.WithDescription("Add a reaction emoji to a message"),
			mcp.WithString("channel_id", mcp.Required(), mcp.Description("Channel ID")),
			mcp.WithString("message_id", mcp.Required(), mcp.Description("Message ID")),
			mcp.WithString("emoji", mcp.Required(), mcp.Description("Emoji (unicode or name:id)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := ensureDiscord(); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			chID, _ := req.RequireString("channel_id")
			if err := ensureChannelAllowed(chID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if err := ensureWriteAllowed("discord_add_reaction", false); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			msgID, _ := req.RequireString("message_id")
			emoji, _ := req.RequireString("emoji")
			if err := dg.MessageReactionAdd(chID, msgID, emoji); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Reacted with %s", emoji)), nil
		},
	)

	// 6. List members
	s.AddTool(
		mcp.NewTool("discord_list_members",
			mcp.WithDescription("List members of a guild"),
			mcp.WithString("guild_id", mcp.Required(), mcp.Description("Guild/server ID")),
			mcp.WithNumber("limit", mcp.Description("Max members, default 50")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := ensureDiscord(); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			guildID, _ := req.RequireString("guild_id")
			if err := ensureGuildAllowed(guildID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			limit := int(req.GetFloat("limit", 50))
			members, err := dg.GuildMembers(guildID, "", limit)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			var lines []string
			for _, m := range members {
				nick := ""
				if m.Nick != "" {
					nick = fmt.Sprintf(" (%s)", m.Nick)
				}
				bot := ""
				if m.User.Bot {
					bot = " 🤖"
				}
				lines = append(lines, fmt.Sprintf("%s%s [%s]%s", m.User.Username, nick, m.User.ID, bot))
			}
			return mcp.NewToolResultText(strings.Join(lines, "\n")), nil
		},
	)

	// 7. Search messages
	s.AddTool(
		mcp.NewTool("discord_search_messages",
			mcp.WithDescription("Search recent messages in a channel by keyword"),
			mcp.WithString("channel_id", mcp.Required(), mcp.Description("Channel ID")),
			mcp.WithString("query", mcp.Required(), mcp.Description("Search keyword")),
			mcp.WithNumber("limit", mcp.Description("Messages to scan, default 50")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := ensureDiscord(); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			chID, _ := req.RequireString("channel_id")
			if err := ensureChannelAllowed(chID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			query, _ := req.RequireString("query")
			limit := int(req.GetFloat("limit", 50))
			msgs, err := dg.ChannelMessages(chID, limit, "", "", "")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			q := strings.ToLower(query)
			var lines []string
			for i := len(msgs) - 1; i >= 0; i-- {
				m := msgs[i]
				if strings.Contains(strings.ToLower(m.Content), q) {
					t := time.Time(m.Timestamp)
					lines = append(lines, fmt.Sprintf("[%s] %s: %s", t.Format(time.RFC3339), m.Author.Username, m.Content))
				}
			}
			if len(lines) == 0 {
				return mcp.NewToolResultText("No matches."), nil
			}
			return mcp.NewToolResultText(strings.Join(lines, "\n")), nil
		},
	)

	// 8. Channel info
	s.AddTool(
		mcp.NewTool("discord_channel_info",
			mcp.WithDescription("Get detailed info about a channel"),
			mcp.WithString("channel_id", mcp.Required(), mcp.Description("Channel ID")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := ensureDiscord(); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			chID, _ := req.RequireString("channel_id")
			if err := ensureChannelAllowed(chID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			ch, err := dg.Channel(chID)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			topic := ch.Topic
			if topic == "" {
				topic = "(none)"
			}
			info := fmt.Sprintf("name: #%s\nid: %s\ntype: %d\ntopic: %s\nguild_id: %s",
				ch.Name, ch.ID, ch.Type, topic, ch.GuildID)
			return mcp.NewToolResultText(info), nil
		},
	)

	// 9. Send file
	s.AddTool(
		mcp.NewTool("discord_send_file",
			mcp.WithDescription("Upload a local file to a channel as an attachment"),
			mcp.WithString("channel_id", mcp.Required(), mcp.Description("Channel ID")),
			mcp.WithString("file_path", mcp.Required(), mcp.Description("Absolute path to the local file")),
			mcp.WithString("content", mcp.Description("Optional text message to send with the file")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := ensureDiscord(); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			chID, _ := req.RequireString("channel_id")
			if err := ensureChannelAllowed(chID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if err := ensureWriteAllowed("discord_send_file", false); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			chID = resolveWriteTargetChannel(chID)
			filePath, _ := req.RequireString("file_path")
			content := secrets.RedactEnv(req.GetString("content", ""))

			f, err := os.Open(filePath)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("open file: %v", err)), nil
			}
			defer f.Close()

			info, err := f.Stat()
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("stat file: %v", err)), nil
			}
			if info.Size() > 25*1024*1024 {
				return mcp.NewToolResultError("file exceeds 25MB Discord limit"), nil
			}

			if len(discordfmt.Split(content, discordMessageLimit)) > 1 {
				if _, err := sendDiscordMessageParts(chID, content); err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				content = ""
			}
			msg, err := dg.ChannelMessageSendComplex(chID, &discordgo.MessageSend{
				Content:         content,
				AllowedMentions: &discordgo.MessageAllowedMentions{},
				Flags:           discordgo.MessageFlagsSuppressEmbeds,
				Files: []*discordgo.File{{
					Name:   filepath.Base(filePath),
					Reader: f,
				}},
			})
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			var urls []string
			for _, a := range msg.Attachments {
				urls = append(urls, a.URL)
			}
			return mcp.NewToolResultText(fmt.Sprintf("Sent message %s\n%s", msg.ID, strings.Join(urls, "\n"))), nil
		},
	)

	// 10. List attachments
	s.AddTool(
		mcp.NewTool("discord_list_attachments",
			mcp.WithDescription("List file attachments from recent messages in a channel"),
			mcp.WithString("channel_id", mcp.Required(), mcp.Description("Channel ID")),
			mcp.WithNumber("limit", mcp.Description("Messages to scan, default 50, max 100")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := ensureDiscord(); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			chID, _ := req.RequireString("channel_id")
			if err := ensureChannelAllowed(chID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			limit := int(req.GetFloat("limit", 50))
			if limit > 100 {
				limit = 100
			}
			msgs, err := dg.ChannelMessages(chID, limit, "", "", "")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			var lines []string
			for i := len(msgs) - 1; i >= 0; i-- {
				m := msgs[i]
				for _, a := range m.Attachments {
					t := time.Time(m.Timestamp).Format(time.RFC3339)
					lines = append(lines, fmt.Sprintf("[%s] %s | %s | %d bytes | msg:%s | %s",
						t, a.Filename, a.ContentType, a.Size, m.ID, a.URL))
				}
			}
			if len(lines) == 0 {
				return mcp.NewToolResultText("No attachments found."), nil
			}
			return mcp.NewToolResultText(strings.Join(lines, "\n")), nil
		},
	)

	// 11. Download attachment
	s.AddTool(
		mcp.NewTool("discord_download_attachment",
			mcp.WithDescription("Download a Discord attachment to a local file"),
			mcp.WithString("url", mcp.Required(), mcp.Description("Attachment URL (from discord_list_attachments)")),
			mcp.WithString("save_dir", mcp.Description("Directory to save the file (default: system temp dir)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := ensureDiscord(); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			url, _ := req.RequireString("url")
			saveDir := req.GetString("save_dir", os.TempDir())
			parsedURL, err := validateDiscordAttachmentURL(url)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			saveDir, err = resolveDownloadDir(saveDir)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			if err := os.MkdirAll(saveDir, 0755); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("create dir: %v", err)), nil
			}

			resp, err := http.Get(url)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("download: %v", err)), nil
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return mcp.NewToolResultError(fmt.Sprintf("download: HTTP %d", resp.StatusCode)), nil
			}

			name := safeAttachmentFilename(parsedURL.Path)
			ts := time.Now().Format("20060102-150405")
			dst := filepath.Join(saveDir, ts+"-"+name)

			f, err := os.Create(dst)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("create file: %v", err)), nil
			}
			n, err := io.Copy(f, resp.Body)
			f.Close()
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("write file: %v", err)), nil
			}
			abs, _ := filepath.Abs(dst)
			return mcp.NewToolResultText(fmt.Sprintf("Saved %s (%d bytes)", abs, n)), nil
		},
	)

	// 12. Edit message
	s.AddTool(
		mcp.NewTool("discord_edit_message",
			mcp.WithDescription("Edit a message (bot's own message)"),
			mcp.WithString("channel_id", mcp.Required(), mcp.Description("Channel ID")),
			mcp.WithString("message_id", mcp.Required(), mcp.Description("Message ID to edit")),
			mcp.WithString("content", mcp.Required(), mcp.Description("New message content")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := ensureDiscord(); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			chID, _ := req.RequireString("channel_id")
			if err := ensureChannelAllowed(chID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if err := ensureWriteAllowed("discord_edit_message", true); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			msgID, _ := req.RequireString("message_id")
			content, _ := req.RequireString("content")
			content = secrets.RedactEnv(content)
			if len(discordfmt.Split(content, discordMessageLimit)) > 1 {
				return mcp.NewToolResultError("content exceeds Discord edit limit; send a new split message instead"), nil
			}
			_, err := dg.ChannelMessageEditComplex(&discordgo.MessageEdit{
				ID:              msgID,
				Channel:         chID,
				Content:         &content,
				AllowedMentions: &discordgo.MessageAllowedMentions{},
				Flags:           discordgo.MessageFlagsSuppressEmbeds,
			})
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Edited message %s", msgID)), nil
		},
	)

	// 13. Delete message
	s.AddTool(
		mcp.NewTool("discord_delete_message",
			mcp.WithDescription("Delete a message"),
			mcp.WithString("channel_id", mcp.Required(), mcp.Description("Channel ID")),
			mcp.WithString("message_id", mcp.Required(), mcp.Description("Message ID to delete")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := ensureDiscord(); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			chID, _ := req.RequireString("channel_id")
			if err := ensureChannelAllowed(chID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if err := ensureWriteAllowed("discord_delete_message", true); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			msgID, _ := req.RequireString("message_id")
			if err := dg.ChannelMessageDelete(chID, msgID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Deleted message %s", msgID)), nil
		},
	)

	// 14. Get message
	s.AddTool(
		mcp.NewTool("discord_get_message",
			mcp.WithDescription("Get a single message by ID"),
			mcp.WithString("channel_id", mcp.Required(), mcp.Description("Channel ID")),
			mcp.WithString("message_id", mcp.Required(), mcp.Description("Message ID")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := ensureDiscord(); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			chID, _ := req.RequireString("channel_id")
			if err := ensureChannelAllowed(chID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			msgID, _ := req.RequireString("message_id")
			m, err := dg.ChannelMessage(chID, msgID)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			t := time.Time(m.Timestamp).Format(time.RFC3339)
			var parts []string
			parts = append(parts, fmt.Sprintf("id: %s\nauthor: %s (%s)\ntime: %s\ncontent: %s", m.ID, m.Author.Username, m.Author.ID, t, m.Content))
			for _, a := range m.Attachments {
				parts = append(parts, fmt.Sprintf("attachment: %s (%d bytes) %s", a.Filename, a.Size, a.URL))
			}
			for _, e := range m.Embeds {
				parts = append(parts, fmt.Sprintf("embed: %s — %s", e.Title, e.Description))
			}
			return mcp.NewToolResultText(strings.Join(parts, "\n")), nil
		},
	)

	// 15. Send embed
	s.AddTool(
		mcp.NewTool("discord_send_embed",
			mcp.WithDescription("Send a rich embed message to a channel"),
			mcp.WithString("channel_id", mcp.Required(), mcp.Description("Channel ID")),
			mcp.WithString("title", mcp.Required(), mcp.Description("Embed title")),
			mcp.WithString("description", mcp.Description("Embed description")),
			mcp.WithString("color", mcp.Description("Hex color, e.g. #FF5733")),
			mcp.WithString("footer", mcp.Description("Footer text")),
			mcp.WithString("url", mcp.Description("Title URL")),
			mcp.WithString("fields_json", mcp.Description(`JSON array of fields, e.g. [{"name":"k","value":"v","inline":true}]`)),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := ensureDiscord(); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			chID, _ := req.RequireString("channel_id")
			if err := ensureChannelAllowed(chID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if err := ensureWriteAllowed("discord_send_embed", false); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			chID = resolveWriteTargetChannel(chID)
			title, _ := req.RequireString("title")
			embed := &discordgo.MessageEmbed{
				Title:       title,
				Description: req.GetString("description", ""),
				URL:         req.GetString("url", ""),
			}
			if c := req.GetString("color", ""); c != "" {
				c = strings.TrimPrefix(c, "#")
				if v, err := strconv.ParseInt(c, 16, 64); err == nil {
					embed.Color = int(v)
				}
			}
			if f := req.GetString("footer", ""); f != "" {
				embed.Footer = &discordgo.MessageEmbedFooter{Text: f}
			}
			if fj := req.GetString("fields_json", ""); fj != "" {
				var fields []*discordgo.MessageEmbedField
				if err := json.Unmarshal([]byte(fj), &fields); err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("invalid fields_json: %v", err)), nil
				}
				embed.Fields = fields
			}
			msgs, err := sendDiscordEmbedParts(chID, embed)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Sent %d embed message part(s): %s", len(msgs), messageIDs(msgs))), nil
		},
	)

	// 16. Pin message
	s.AddTool(
		mcp.NewTool("discord_pin_message",
			mcp.WithDescription("Pin or unpin a message"),
			mcp.WithString("channel_id", mcp.Required(), mcp.Description("Channel ID")),
			mcp.WithString("message_id", mcp.Required(), mcp.Description("Message ID")),
			mcp.WithBoolean("unpin", mcp.Description("Set true to unpin instead of pin")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := ensureDiscord(); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			chID, _ := req.RequireString("channel_id")
			if err := ensureChannelAllowed(chID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if err := ensureWriteAllowed("discord_pin_message", true); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			msgID, _ := req.RequireString("message_id")
			unpin := req.GetBool("unpin", false)
			if unpin {
				if err := dg.ChannelMessageUnpin(chID, msgID); err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				return mcp.NewToolResultText(fmt.Sprintf("Unpinned message %s", msgID)), nil
			}
			if err := dg.ChannelMessagePin(chID, msgID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Pinned message %s", msgID)), nil
		},
	)

	// 17. Create thread
	s.AddTool(
		mcp.NewTool("discord_create_thread",
			mcp.WithDescription("Create a thread from a message"),
			mcp.WithString("channel_id", mcp.Required(), mcp.Description("Channel ID")),
			mcp.WithString("message_id", mcp.Required(), mcp.Description("Message ID to start thread from")),
			mcp.WithString("name", mcp.Required(), mcp.Description("Thread name")),
			mcp.WithNumber("auto_archive_duration", mcp.Description("Auto-archive in minutes: 60, 1440, 4320, or 10080 (default 1440)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := ensureDiscord(); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			chID, _ := req.RequireString("channel_id")
			if err := ensureChannelAllowed(chID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if err := ensureWriteAllowed("discord_create_thread", false); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			msgID, _ := req.RequireString("message_id")
			name, _ := req.RequireString("name")
			dur := int(req.GetFloat("auto_archive_duration", 1440))
			th, err := dg.MessageThreadStart(chID, msgID, name, dur)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Created thread #%s (%s)", th.Name, th.ID)), nil
		},
	)

	// 18. List threads
	s.AddTool(
		mcp.NewTool("discord_list_threads",
			mcp.WithDescription("List active threads in a guild"),
			mcp.WithString("guild_id", mcp.Required(), mcp.Description("Guild/server ID")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := ensureDiscord(); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			guildID, _ := req.RequireString("guild_id")
			if err := ensureGuildAllowed(guildID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			tl, err := dg.GuildThreadsActive(guildID)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if len(tl.Threads) == 0 {
				return mcp.NewToolResultText("No active threads."), nil
			}
			var lines []string
			for _, t := range tl.Threads {
				lines = append(lines, fmt.Sprintf("#%s (%s) parent:%s", t.Name, t.ID, t.ParentID))
			}
			return mcp.NewToolResultText(strings.Join(lines, "\n")), nil
		},
	)

	// 19. Remove reaction
	s.AddTool(
		mcp.NewTool("discord_remove_reaction",
			mcp.WithDescription("Remove a reaction from a message (bot's own reaction, or specify user_id)"),
			mcp.WithString("channel_id", mcp.Required(), mcp.Description("Channel ID")),
			mcp.WithString("message_id", mcp.Required(), mcp.Description("Message ID")),
			mcp.WithString("emoji", mcp.Required(), mcp.Description("Emoji (unicode or name:id)")),
			mcp.WithString("user_id", mcp.Description("User ID (default: @me)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := ensureDiscord(); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			chID, _ := req.RequireString("channel_id")
			if err := ensureChannelAllowed(chID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if err := ensureWriteAllowed("discord_remove_reaction", true); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			msgID, _ := req.RequireString("message_id")
			emoji, _ := req.RequireString("emoji")
			userID := req.GetString("user_id", "@me")
			if err := dg.MessageReactionRemove(chID, msgID, emoji, userID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Removed reaction %s from %s", emoji, msgID)), nil
		},
	)

	// 20. Get reactions
	s.AddTool(
		mcp.NewTool("discord_get_reactions",
			mcp.WithDescription("Get users who reacted with a specific emoji on a message"),
			mcp.WithString("channel_id", mcp.Required(), mcp.Description("Channel ID")),
			mcp.WithString("message_id", mcp.Required(), mcp.Description("Message ID")),
			mcp.WithString("emoji", mcp.Required(), mcp.Description("Emoji (unicode or name:id)")),
			mcp.WithNumber("limit", mcp.Description("Max users to return, default 25")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := ensureDiscord(); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			chID, _ := req.RequireString("channel_id")
			if err := ensureChannelAllowed(chID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			msgID, _ := req.RequireString("message_id")
			emoji, _ := req.RequireString("emoji")
			limit := int(req.GetFloat("limit", 25))
			users, err := dg.MessageReactions(chID, msgID, emoji, limit, "", "")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if len(users) == 0 {
				return mcp.NewToolResultText("No reactions."), nil
			}
			var lines []string
			for _, u := range users {
				lines = append(lines, fmt.Sprintf("%s (%s)", u.Username, u.ID))
			}
			return mcp.NewToolResultText(strings.Join(lines, "\n")), nil
		},
	)

	// 21. Edit channel topic
	s.AddTool(
		mcp.NewTool("discord_edit_channel_topic",
			mcp.WithDescription("Edit a channel's topic"),
			mcp.WithString("channel_id", mcp.Required(), mcp.Description("Channel ID")),
			mcp.WithString("topic", mcp.Required(), mcp.Description("New topic text (empty string to clear)")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := ensureDiscord(); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			chID, _ := req.RequireString("channel_id")
			if err := ensureChannelAllowed(chID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if err := ensureWriteAllowed("discord_edit_channel_topic", true); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			topic, _ := req.RequireString("topic")
			_, err := dg.ChannelEdit(chID, &discordgo.ChannelEdit{Topic: topic})
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Updated topic for %s", chID)), nil
		},
	)

	// 22. List roles
	s.AddTool(
		mcp.NewTool("discord_list_roles",
			mcp.WithDescription("List roles in a guild"),
			mcp.WithString("guild_id", mcp.Required(), mcp.Description("Guild/server ID")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := ensureDiscord(); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			guildID, _ := req.RequireString("guild_id")
			if err := ensureGuildAllowed(guildID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			roles, err := dg.GuildRoles(guildID)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			var lines []string
			for _, r := range roles {
				lines = append(lines, fmt.Sprintf("%s (%s) color:#%06x pos:%d", r.Name, r.ID, r.Color, r.Position))
			}
			return mcp.NewToolResultText(strings.Join(lines, "\n")), nil
		},
	)

	// 23. Get user
	s.AddTool(
		mcp.NewTool("discord_get_user",
			mcp.WithDescription("Get info about a specific user by ID"),
			mcp.WithString("user_id", mcp.Required(), mcp.Description("User ID")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := ensureDiscord(); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			userID, _ := req.RequireString("user_id")
			u, err := dg.User(userID)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			bot := ""
			if u.Bot {
				bot = " 🤖"
			}
			return mcp.NewToolResultText(fmt.Sprintf("%s#%s (%s)%s", u.Username, u.Discriminator, u.ID, bot)), nil
		},
	)

	if err := server.ServeStdio(s); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
