package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/nczz/kiro-discord-bot/internal/discordmention"
)

const defaultMentionMemberScanLimit = 5000

var mentionIDPattern = regexp.MustCompile(`^<@!?([0-9]+)>$|^([0-9]{5,})$`)

type mentionTargetState struct {
	TargetChannelID       string               `json:"target_channel_id"`
	DisableEgress         bool                 `json:"disable_egress,omitempty"`
	AllowedMentionUserIDs []string             `json:"allowed_mention_user_ids,omitempty"`
	MentionRefs           []discordmention.Ref `json:"mention_refs,omitempty"`
}

type mentionResolveResult struct {
	Resolved     []resolvedMention  `json:"resolved"`
	Ambiguous    []ambiguousMention `json:"ambiguous,omitempty"`
	Missing      []string           `json:"missing,omitempty"`
	Instructions string             `json:"instructions"`
}

type resolvedMention struct {
	Query       string `json:"query"`
	DisplayName string `json:"display_name"`
	UserID      string `json:"user_id"`
	Placeholder string `json:"placeholder"`
	Match       string `json:"match"`
}

type ambiguousMention struct {
	Query      string             `json:"query"`
	Candidates []mentionCandidate `json:"candidates"`
}

type mentionCandidate struct {
	DisplayName string `json:"display_name"`
	Username    string `json:"username,omitempty"`
	GlobalName  string `json:"global_name,omitempty"`
}

func registerResolveMentionsTool(s *server.MCPServer) {
	s.AddTool(
		mcp.NewTool("discord_resolve_mentions",
			mcp.WithDescription("Resolve comma- or newline-separated natural-language Discord member names to verified mention placeholders for the current bot task. Use this when a user asks to tag, mention, notify, or ping people who are not already listed in Discord mention references. The tool performs fresh Discord member lookup before cache fallback, grants exact/unique resolved users only for this active job, and returns placeholders such as [[discord:user:123]]; never write raw <@id>."),
			mcp.WithString("channel_id", mcp.Required(), mcp.Description("Current Discord channel_id from context; if the task is bound to a thread, the bot dynamic target state is used automatically.")),
			mcp.WithString("names", mcp.Required(), mcp.Description("Comma- or newline-separated names to resolve, for example: Wendy, Cheisy. Do not pass a whole request; extract the person/group names first.")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := ensureDiscord(); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			chID, _ := req.RequireString("channel_id")
			if err := ensureChannelAllowed(chID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if err := ensureWriteAllowed("discord_resolve_mentions", false); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			targetID := resolveWriteTargetChannel(chID)
			guildID, err := guildIDForMentionTarget(targetID)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			names := splitMentionNames(req.GetString("names", ""))
			if err := validateMentionResolveNames(names); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			result := resolveMentionNames(guildID, names)
			if len(result.Resolved) > 0 {
				refs := make([]discordmention.Ref, 0, len(result.Resolved))
				for _, item := range result.Resolved {
					refs = append(refs, discordmention.UserRef(item.UserID, item.DisplayName))
				}
				if err := grantMentionRefsForCurrentJob(refs); err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
			}
			result.Instructions = mentionResolveInstructions(result)
			raw, _ := json.MarshalIndent(result, "", "  ")
			return mcp.NewToolResultText(string(raw)), nil
		},
	)
}

func guildIDForMentionTarget(channelID string) (string, error) {
	ch, err := dg.Channel(channelID)
	if err != nil {
		return "", fmt.Errorf("resolve mention target channel: %w", err)
	}
	if strings.TrimSpace(ch.GuildID) == "" {
		return "", fmt.Errorf("channel %s has no guild_id", channelID)
	}
	return ch.GuildID, nil
}

func splitMentionNames(raw string) []string {
	repl := strings.NewReplacer("\r", "\n", "，", ",", "、", ",", ";", ",", "；", ",", " and ", ",", " 跟 ", ",", " 和 ", ",")
	raw = repl.Replace(raw)
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' })
	seen := make(map[string]bool)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		name := strings.Trim(strings.TrimSpace(part), "@：:。.!！?")
		if name == "" {
			continue
		}
		key := normalizeMentionName(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, name)
	}
	return out
}

func validateMentionResolveNames(names []string) error {
	if len(names) == 0 {
		return fmt.Errorf("names is required")
	}
	for _, name := range names {
		if mentionUserID(name) != "" {
			return fmt.Errorf("names must be display names, not Discord IDs or raw mentions")
		}
	}
	return nil
}

func resolveMentionNames(guildID string, names []string) mentionResolveResult {
	var result mentionResolveResult
	for _, name := range names {
		members, source := lookupMentionMembers(guildID, name)
		resolved, ambiguous, ok := resolveMentionName(name, source, members)
		if ok {
			result.Resolved = append(result.Resolved, resolved)
			continue
		}
		if len(ambiguous.Candidates) > 0 {
			result.Ambiguous = append(result.Ambiguous, ambiguous)
			continue
		}
		result.Missing = append(result.Missing, name)
	}
	return result
}

func lookupMentionMembers(guildID, name string) ([]*discordgo.Member, string) {
	var out []*discordgo.Member
	if searched, err := dg.GuildMembersSearch(guildID, name, 25); err == nil {
		out = appendUniqueMembers(out, searched...)
		if len(searched) == 1 {
			return out, "fresh_search_unique"
		}
	}
	if scanned := scanGuildMembersForName(guildID, name, mentionMemberScanLimit()); len(scanned) > 0 {
		out = appendUniqueMembers(out, scanned...)
	}
	if len(out) > 0 {
		return out, "fresh_lookup"
	}
	if dg != nil && dg.State != nil {
		if guild, err := dg.State.Guild(guildID); err == nil && guild != nil {
			cached := filterMembersByName(guild.Members, name)
			if len(cached) > 0 {
				return cached, "state_cache"
			}
		}
	}
	return nil, ""
}

func resolveMentionName(query, source string, members []*discordgo.Member) (resolvedMention, ambiguousMention, bool) {
	members = uniqueMembers(members)
	if len(members) == 0 {
		return resolvedMention{}, ambiguousMention{}, false
	}
	exact := make([]*discordgo.Member, 0, len(members))
	for _, member := range members {
		if memberMatchesExact(member, query) {
			exact = append(exact, member)
		}
	}
	if len(exact) == 1 {
		return resolvedMentionForMember(query, exact[0], "exact_"+source), ambiguousMention{}, true
	}
	if len(exact) > 1 {
		return resolvedMention{}, ambiguousForMembers(query, exact), false
	}
	if len(members) == 1 && (source == "fresh_search_unique" || memberNameStartsWith(members[0], query)) {
		return resolvedMentionForMember(query, members[0], "unique_prefix_"+source), ambiguousMention{}, true
	}
	return resolvedMention{}, ambiguousForMembers(query, members), false
}

func scanGuildMembersForName(guildID, name string, limit int) []*discordgo.Member {
	if limit <= 0 {
		return nil
	}
	var out []*discordgo.Member
	after := ""
	for len(out) < limit {
		remaining := limit - len(out)
		pageLimit := 1000
		if remaining < pageLimit {
			pageLimit = remaining
		}
		members, err := dg.GuildMembers(guildID, after, pageLimit)
		if err != nil || len(members) == 0 {
			return out
		}
		out = appendUniqueMembers(out, filterMembersByName(members, name)...)
		last := members[len(members)-1]
		if len(members) < pageLimit || last == nil || last.User == nil || last.User.ID == "" {
			break
		}
		after = last.User.ID
	}
	return out
}

func filterMembersByName(members []*discordgo.Member, query string) []*discordgo.Member {
	q := normalizeMentionName(query)
	if q == "" {
		return nil
	}
	var out []*discordgo.Member
	for _, member := range members {
		if member == nil || member.User == nil {
			continue
		}
		for _, candidate := range memberNameCandidates(member) {
			name := normalizeMentionName(candidate)
			if name == q || strings.HasPrefix(name, q) || strings.Contains(name, q) {
				out = append(out, member)
				break
			}
		}
	}
	return out
}

func memberMatchesExact(member *discordgo.Member, query string) bool {
	q := normalizeMentionName(query)
	if q == "" || member == nil || member.User == nil {
		return false
	}
	for _, candidate := range memberNameCandidates(member) {
		if normalizeMentionName(candidate) == q {
			return true
		}
	}
	return false
}

func memberNameStartsWith(member *discordgo.Member, query string) bool {
	q := normalizeMentionName(query)
	if q == "" {
		return false
	}
	for _, candidate := range memberNameCandidates(member) {
		if strings.HasPrefix(normalizeMentionName(candidate), q) {
			return true
		}
	}
	return false
}

func memberNameCandidates(member *discordgo.Member) []string {
	if member == nil || member.User == nil {
		return nil
	}
	return []string{member.DisplayName(), member.Nick, member.User.GlobalName, member.User.Username}
}

func resolvedMentionForMember(query string, member *discordgo.Member, match string) resolvedMention {
	display := member.DisplayName()
	if display == "" {
		display = member.User.Username
	}
	ref := discordmention.UserRef(member.User.ID, display)
	return resolvedMention{Query: query, DisplayName: display, UserID: member.User.ID, Placeholder: ref.Placeholder, Match: strings.Trim(match, "_")}
}

func ambiguousForMembers(query string, members []*discordgo.Member) ambiguousMention {
	members = uniqueMembers(members)
	sort.Slice(members, func(i, j int) bool {
		return strings.ToLower(memberDisplayName(members[i])) < strings.ToLower(memberDisplayName(members[j]))
	})
	if len(members) > 10 {
		members = members[:10]
	}
	out := ambiguousMention{Query: query, Candidates: make([]mentionCandidate, 0, len(members))}
	for _, member := range members {
		out.Candidates = append(out.Candidates, mentionCandidate{
			DisplayName: memberDisplayName(member),
			Username:    member.User.Username,
			GlobalName:  member.User.GlobalName,
		})
	}
	return out
}

func memberDisplayName(member *discordgo.Member) string {
	if member == nil || member.User == nil {
		return ""
	}
	if display := member.DisplayName(); display != "" {
		return display
	}
	return member.User.Username
}

func appendUniqueMembers(base []*discordgo.Member, extra ...*discordgo.Member) []*discordgo.Member {
	seen := make(map[string]bool, len(base)+len(extra))
	out := make([]*discordgo.Member, 0, len(base)+len(extra))
	for _, member := range base {
		if member != nil && member.User != nil && member.User.ID != "" && !seen[member.User.ID] {
			seen[member.User.ID] = true
			out = append(out, member)
		}
	}
	for _, member := range extra {
		if member != nil && member.User != nil && member.User.ID != "" && !seen[member.User.ID] {
			seen[member.User.ID] = true
			out = append(out, member)
		}
	}
	return out
}

func uniqueMembers(members []*discordgo.Member) []*discordgo.Member {
	return appendUniqueMembers(nil, members...)
}

func mentionUserID(raw string) string {
	match := mentionIDPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if len(match) == 0 {
		return ""
	}
	if match[1] != "" {
		return match[1]
	}
	return match[2]
}

func normalizeMentionName(s string) string {
	s = strings.TrimSpace(strings.TrimPrefix(s, "@"))
	s = strings.ToLower(s)
	return strings.Join(strings.Fields(s), " ")
}

func mentionMemberScanLimit() int {
	raw := strings.TrimSpace(os.Getenv("MCP_DISCORD_MEMBER_SCAN_LIMIT"))
	if raw == "" {
		return defaultMentionMemberScanLimit
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 0 {
		return defaultMentionMemberScanLimit
	}
	if limit > 50000 {
		return 50000
	}
	return limit
}

func grantMentionRefsForCurrentJob(refs []discordmention.Ref) error {
	path := strings.TrimSpace(os.Getenv("BOT_TOOLS_TARGET_STATE_PATH"))
	if path == "" {
		return fmt.Errorf("cannot grant mention refs: BOT_TOOLS_TARGET_STATE_PATH is not set")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read target state: %w", err)
	}
	var state mentionTargetState
	if err := json.Unmarshal(raw, &state); err != nil {
		return fmt.Errorf("parse target state: %w", err)
	}
	state.MentionRefs = cleanMentionRefs(append(state.MentionRefs, refs...))
	state.AllowedMentionUserIDs = allowedMentionUserIDs(state.MentionRefs)
	updated, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(updated, '\n'), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func cleanMentionRefs(refs []discordmention.Ref) []discordmention.Ref {
	seen := make(map[string]bool)
	out := make([]discordmention.Ref, 0, len(refs))
	for _, ref := range refs {
		if ref.Kind != "user" {
			continue
		}
		id := strings.TrimSpace(ref.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, discordmention.UserRef(id, ref.DisplayName))
	}
	return out
}

func allowedMentionUserIDs(refs []discordmention.Ref) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if ref.Kind != "user" {
			continue
		}
		id := strings.TrimSpace(ref.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func mentionResolveInstructions(result mentionResolveResult) string {
	var parts []string
	if len(result.Resolved) > 0 {
		parts = append(parts, "Use only the returned placeholders for mentions in your final response or bot_send_message content; do not write raw <@id>.")
	}
	if len(result.Ambiguous) > 0 {
		parts = append(parts, "Some names are ambiguous; ask the user to choose one listed candidate before mentioning them.")
	}
	if len(result.Missing) > 0 {
		parts = append(parts, "Some names were not found by fresh Discord lookup or cache fallback; ask the user to @ them once or use a more exact display name.")
	}
	if len(parts) == 0 {
		return "No mention targets resolved. Ask the user for exact names or Discord mentions."
	}
	return strings.Join(parts, " ")
}
