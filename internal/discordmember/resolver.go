package discordmember

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/internal/discordmention"
)

var mentionIDPattern = regexp.MustCompile(`^<@!?([0-9]+)>$|^([0-9]{5,})$`)

type ResolveResult struct {
	Resolved  []ResolvedMention  `json:"resolved"`
	Ambiguous []AmbiguousMention `json:"ambiguous,omitempty"`
	Missing   []string           `json:"missing,omitempty"`
}

type ResolvedMention struct {
	Query       string `json:"query"`
	DisplayName string `json:"display_name"`
	UserID      string `json:"user_id"`
	Placeholder string `json:"placeholder"`
	Match       string `json:"match"`
}

type AmbiguousMention struct {
	Query      string             `json:"query"`
	Candidates []MentionCandidate `json:"candidates"`
}

type MentionCandidate struct {
	DisplayName string `json:"display_name"`
	Username    string `json:"username,omitempty"`
	GlobalName  string `json:"global_name,omitempty"`
}

func SplitNames(raw string) []string {
	repl := strings.NewReplacer("\r", "\n", "，", ",", "、", ",", ";", ",", "；", ",", " and ", ",", " 跟 ", ",", " 和 ", ",")
	raw = repl.Replace(raw)
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' })
	seen := make(map[string]bool)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		name := strings.Trim(strings.TrimSpace(part), "@：:。.!！？?")
		if name == "" {
			continue
		}
		key := normalizeName(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, name)
	}
	return out
}

func ValidateNames(names []string) error {
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

func ResolveNames(session *discordgo.Session, guildID string, names []string, scanLimit int) (ResolveResult, error) {
	if session == nil {
		return ResolveResult{}, fmt.Errorf("Discord session is unavailable")
	}
	searchResults := make([][]*discordgo.Member, len(names))
	for i, name := range names {
		searched, err := session.GuildMembersSearch(guildID, name, 25)
		if err != nil {
			return ResolveResult{}, fmt.Errorf("search Discord member %q: %w", name, err)
		}
		searchResults[i] = searched
	}
	var scanned []*discordgo.Member
	if scanLimit > 0 {
		var err error
		scanned, err = scanGuildMembers(session, guildID, scanLimit)
		if err != nil {
			return ResolveResult{}, err
		}
	}
	var result ResolveResult
	for i, name := range names {
		members, source := lookupMembers(name, searchResults[i], scanned)
		resolved, ambiguous, ok := ResolveName(name, source, members)
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
	return result, nil
}

func ResolveName(query, source string, members []*discordgo.Member) (ResolvedMention, AmbiguousMention, bool) {
	members = uniqueMembers(members)
	if len(members) == 0 {
		return ResolvedMention{}, AmbiguousMention{}, false
	}
	exact := make([]*discordgo.Member, 0, len(members))
	for _, member := range members {
		if memberMatchesExact(member, query) {
			exact = append(exact, member)
		}
	}
	if len(exact) == 1 {
		return resolvedForMember(query, exact[0], "exact_"+source), AmbiguousMention{}, true
	}
	if len(exact) > 1 {
		return ResolvedMention{}, AmbiguousForMembers(query, exact), false
	}
	if len(members) == 1 && (source == "fresh_search_unique" || memberNameStartsWith(members[0], query)) {
		return resolvedForMember(query, members[0], "unique_prefix_"+source), AmbiguousMention{}, true
	}
	return ResolvedMention{}, AmbiguousForMembers(query, members), false
}

func AmbiguousForMembers(query string, members []*discordgo.Member) AmbiguousMention {
	members = uniqueMembers(members)
	sort.Slice(members, func(i, j int) bool {
		return strings.ToLower(memberDisplayName(members[i])) < strings.ToLower(memberDisplayName(members[j]))
	})
	if len(members) > 10 {
		members = members[:10]
	}
	out := AmbiguousMention{Query: query, Candidates: make([]MentionCandidate, 0, len(members))}
	for _, member := range members {
		out.Candidates = append(out.Candidates, MentionCandidate{
			DisplayName: memberDisplayName(member),
			Username:    member.User.Username,
			GlobalName:  member.User.GlobalName,
		})
	}
	return out
}

func lookupMembers(name string, searched, scanned []*discordgo.Member) ([]*discordgo.Member, string) {
	out := appendUniqueMembers(nil, searched...)
	out = appendUniqueMembers(out, filterMembersByName(scanned, name)...)
	if len(out) > 0 {
		return out, "fresh_lookup"
	}
	return nil, ""
}

func scanGuildMembers(session *discordgo.Session, guildID string, limit int) ([]*discordgo.Member, error) {
	if limit <= 0 {
		return nil, nil
	}
	initialCapacity := limit
	if initialCapacity > 1000 {
		initialCapacity = 1000
	}
	out := make([]*discordgo.Member, 0, initialCapacity)
	seen := make(map[string]struct{}, initialCapacity)
	after := ""
	for scanned := 0; scanned < limit; {
		pageLimit := 1000
		if remaining := limit - scanned; remaining < pageLimit {
			pageLimit = remaining
		}
		members, err := session.GuildMembers(guildID, after, pageLimit)
		if err != nil {
			return nil, fmt.Errorf("list guild members: %w", err)
		}
		if len(members) == 0 {
			return out, nil
		}
		scanned += len(members)
		for _, member := range members {
			if member == nil || member.User == nil || member.User.ID == "" {
				continue
			}
			if _, exists := seen[member.User.ID]; exists {
				continue
			}
			seen[member.User.ID] = struct{}{}
			out = append(out, member)
		}
		last := members[len(members)-1]
		if len(members) < pageLimit || last == nil || last.User == nil || last.User.ID == "" {
			break
		}
		after = last.User.ID
	}
	return out, nil
}

func filterMembersByName(members []*discordgo.Member, query string) []*discordgo.Member {
	q := normalizeName(query)
	if q == "" {
		return nil
	}
	var out []*discordgo.Member
	for _, member := range members {
		if member == nil || member.User == nil {
			continue
		}
		for _, candidate := range memberNameCandidates(member) {
			name := normalizeName(candidate)
			if name == q || strings.HasPrefix(name, q) || strings.Contains(name, q) {
				out = append(out, member)
				break
			}
		}
	}
	return out
}

func memberMatchesExact(member *discordgo.Member, query string) bool {
	q := normalizeName(query)
	if q == "" || member == nil || member.User == nil {
		return false
	}
	for _, candidate := range memberNameCandidates(member) {
		if normalizeName(candidate) == q {
			return true
		}
	}
	return false
}

func memberNameStartsWith(member *discordgo.Member, query string) bool {
	q := normalizeName(query)
	if q == "" {
		return false
	}
	for _, candidate := range memberNameCandidates(member) {
		if strings.HasPrefix(normalizeName(candidate), q) {
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

func resolvedForMember(query string, member *discordgo.Member, match string) ResolvedMention {
	display := memberDisplayName(member)
	ref := discordmention.UserRef(member.User.ID, display)
	return ResolvedMention{Query: query, DisplayName: display, UserID: member.User.ID, Placeholder: ref.Placeholder, Match: strings.Trim(match, "_")}
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
	appendMember := func(member *discordgo.Member) {
		if member != nil && member.User != nil && member.User.ID != "" && !seen[member.User.ID] {
			seen[member.User.ID] = true
			out = append(out, member)
		}
	}
	for _, member := range base {
		appendMember(member)
	}
	for _, member := range extra {
		appendMember(member)
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

func normalizeName(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(value, "@"))
	value = strings.ToLower(value)
	return strings.Join(strings.Fields(value), " ")
}
