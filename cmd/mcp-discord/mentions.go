package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/nczz/kiro-discord-bot/internal/discordmention"
)

func registerSearchMembersTool(s *server.MCPServer) {
	s.AddTool(
		discordReadTool("discord_search_members",
			mcp.WithDescription("Search members in an explicit guild through the Discord REST API without bot task or channel binding"),
			mcp.WithString("guild_id", mcp.Required(), mcp.Description("Guild/server ID")),
			mcp.WithString("query", mcp.Required(), mcp.Description("Username or nickname prefix query")),
			mcp.WithNumber("limit", mcp.Description("Maximum matches, default 25, range 1-1000")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := ensureDiscord(); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			guildID, _ := req.RequireString("guild_id")
			guildID = strings.TrimSpace(guildID)
			query, _ := req.RequireString("query")
			query = strings.TrimSpace(query)
			if query == "" {
				return mcp.NewToolResultError("query is required"), nil
			}
			limit, err := discordPageLimit(req.GetFloat("limit", 25), 1000)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			members, err := dg.GuildMembersSearch(guildID, query, limit)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("search guild members: %v", err)), nil
			}
			result := discordMemberSearchResult{
				GuildID: guildID,
				Query:   query,
				Members: discordMemberRecords(members),
			}
			raw, err := marshalDiscordJSON(result)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(raw), nil
		},
	)
}

func renderDiscordTextMentions(content string) (string, []discordmention.Ref) {
	rendered, _ := discordmention.Render(content, nil)
	return rendered, nil
}
