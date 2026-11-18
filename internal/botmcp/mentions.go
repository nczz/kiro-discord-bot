package botmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/bwmarrin/discordgo"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/nczz/kiro-discord-bot/internal/discordmember"
	"github.com/nczz/kiro-discord-bot/internal/discordmention"
)

const defaultBotMentionMemberScanLimit = 5000

var (
	botMentionSessionFactory = botMentionDiscordSession
	botMentionTargetStateMu  sync.Mutex
)

type botMentionResolveResult struct {
	discordmember.ResolveResult
	Instructions string `json:"instructions"`
}

func registerResolveMentionsTool(s *server.MCPServer) {
	s.AddTool(
		writeTool(ToolResolveMentions, "Resolve comma- or newline-separated Discord member names for the current bot task. This bot-owned bridge performs fresh guild member lookup, grants exact or unique matches to the active task, and returns verified [[discord:user:...]] placeholders. Ambiguous or missing names must be clarified; never write raw <@id>.", false),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if botToolsWriteDisabled() {
				return botToolsWriteDisabledResult(), nil
			}
			state, ok := currentTargetState()
			if !ok {
				return mcp.NewToolResultError("verified bot target state is unavailable"), nil
			}
			guildID := strings.TrimSpace(state.GuildID)
			if guildID == "" {
				guildID = strings.TrimSpace(os.Getenv("BOT_TOOLS_GUILD_ID"))
			}
			if guildID == "" {
				return mcp.NewToolResultError("verified bot target guild is unavailable"), nil
			}
			names := discordmember.SplitNames(req.GetString("names", ""))
			if err := discordmember.ValidateNames(names); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			session, err := botMentionSessionFactory()
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			resolved, err := discordmember.ResolveNames(session, guildID, names, botMentionMemberScanLimit())
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if len(resolved.Resolved) > 0 {
				refs := make([]discordmention.Ref, 0, len(resolved.Resolved))
				for _, item := range resolved.Resolved {
					refs = append(refs, discordmention.UserRef(item.UserID, item.DisplayName))
				}
				if err := grantMentionRefsForCurrentJob(refs); err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
			}
			result := botMentionResolveResult{ResolveResult: resolved, Instructions: botMentionResolveInstructions(resolved)}
			raw, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("encode mention result: %v", err)), nil
			}
			return mcp.NewToolResultText(string(raw)), nil
		},
	)
}

func botMentionDiscordSession() (*discordgo.Session, error) {
	token := strings.TrimSpace(os.Getenv("DISCORD_TOKEN"))
	if token == "" {
		return nil, fmt.Errorf("DISCORD_TOKEN is not set")
	}
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("create Discord session: %w", err)
	}
	return session, nil
}

func botMentionMemberScanLimit() int {
	raw := strings.TrimSpace(os.Getenv("BOT_TOOLS_MENTION_MEMBER_SCAN_LIMIT"))
	if raw == "" {
		return defaultBotMentionMemberScanLimit
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 0 {
		return defaultBotMentionMemberScanLimit
	}
	if limit > 50000 {
		return 50000
	}
	return limit
}

func grantMentionRefsForCurrentJob(refs []discordmention.Ref) error {
	path := strings.TrimSpace(os.Getenv("BOT_TOOLS_TARGET_STATE_PATH"))
	if path == "" {
		return fmt.Errorf("verified bot target state path is unavailable")
	}
	botMentionTargetStateMu.Lock()
	defer botMentionTargetStateMu.Unlock()

	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read target state: %w", err)
	}
	var state targetState
	if err := json.Unmarshal(raw, &state); err != nil {
		return fmt.Errorf("parse target state: %w", err)
	}
	state.MentionRefs = discordmention.NormalizeRefs(append(state.MentionRefs, refs...))
	state.AllowedMentionUserIDs = discordmention.UserIDs(state.MentionRefs)
	updated, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode target state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("prepare target state directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create target state update: %w", err)
	}
	tmpPath := tmp.Name()
	removeTmp := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	if _, err := tmp.Write(append(updated, '\n')); err != nil {
		removeTmp()
		return fmt.Errorf("write target state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close target state update: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace target state: %w", err)
	}
	return nil
}

func botMentionResolveInstructions(result discordmember.ResolveResult) string {
	var parts []string
	if len(result.Resolved) > 0 {
		parts = append(parts, "Use only the returned placeholders for mentions in the final response or bot_send_message content; do not write raw <@id>.")
	}
	if len(result.Ambiguous) > 0 {
		parts = append(parts, "Some names are ambiguous; ask the user to choose one listed candidate before mentioning them.")
	}
	if len(result.Missing) > 0 {
		parts = append(parts, "Some names were not found by fresh Discord lookup; ask the user to @ them once or use a more exact display name.")
	}
	if len(parts) == 0 {
		return "No mention targets resolved. Ask the user for exact names or Discord mentions."
	}
	return strings.Join(parts, " ")
}
