package discordmention

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/bwmarrin/discordgo"
)

const sentinelPrefix = "\x00discordmention:"

var rawMentionPattern = regexp.MustCompile(`<(@!?|@&|#)([0-9]+)>`)
var structuredPlaceholderPattern = regexp.MustCompile(`\[\[discord:(user|role):([0-9]+)\]\]`)
var anyStructuredPlaceholderPattern = regexp.MustCompile(`\[\[discord:(user|role):[^\]\r\n]*\]\]`)

// Ref is a Discord mention token the bot has verified and made available to an
// agent. Agents must echo Placeholder exactly; raw Discord mention syntax is not
// trusted.
type Ref struct {
	Kind        string `json:"kind"`
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
	Placeholder string `json:"placeholder"`
}

// UserRef creates a verified user mention reference.
func UserRef(id, displayName string) Ref {
	id = strings.TrimSpace(id)
	return Ref{
		Kind:        "user",
		ID:          id,
		DisplayName: strings.TrimSpace(displayName),
		Placeholder: fmt.Sprintf("[[discord:user:%s]]", id),
	}
}

// RoleRef creates a verified role mention reference.
func RoleRef(id, displayName string) Ref {
	id = strings.TrimSpace(id)
	return Ref{
		Kind:        "role",
		ID:          id,
		DisplayName: strings.TrimSpace(displayName),
		Placeholder: fmt.Sprintf("[[discord:role:%s]]", id),
	}
}

// Render replaces verified placeholders with Discord mention syntax and returns
// an AllowedMentions object scoped to only the placeholders actually used.
func Render(content string, refs []Ref) (string, *discordgo.MessageAllowedMentions) {
	if content == "" {
		return content, &discordgo.MessageAllowedMentions{}
	}

	allowed := make(map[string]Ref)
	for _, ref := range refs {
		if !validRef(ref) {
			continue
		}
		allowed[ref.Placeholder] = ref
	}

	type usedRef struct {
		sentinel string
		ref      Ref
	}
	var used []usedRef
	for placeholder, ref := range allowed {
		if !strings.Contains(content, placeholder) {
			continue
		}
		sentinel := fmt.Sprintf("%s%d\x00", sentinelPrefix, len(used))
		content = strings.ReplaceAll(content, placeholder, sentinel)
		used = append(used, usedRef{sentinel: sentinel, ref: ref})
	}

	content = EscapeRaw(content)

	userSet := make(map[string]bool)
	roleSet := make(map[string]bool)
	for _, item := range used {
		token := "<@" + item.ref.ID + ">"
		if item.ref.Kind == "role" {
			token = "<@&" + item.ref.ID + ">"
		}
		content = strings.ReplaceAll(content, item.sentinel, token)
		if item.ref.Kind == "role" {
			roleSet[item.ref.ID] = true
		} else {
			userSet[item.ref.ID] = true
		}
	}
	content = structuredPlaceholderPattern.ReplaceAllStringFunc(content, func(match string) string {
		parts := structuredPlaceholderPattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return "Discord mention"
		}
		return fmt.Sprintf("Discord %s", parts[1])
	})
	content = anyStructuredPlaceholderPattern.ReplaceAllString(content, "Discord mention")

	users := make([]string, 0, len(userSet))
	for id := range userSet {
		users = append(users, id)
	}
	sort.Strings(users)
	roles := make([]string, 0, len(roleSet))
	for id := range roleSet {
		roles = append(roles, id)
	}
	sort.Strings(roles)
	return content, &discordgo.MessageAllowedMentions{Users: users, Roles: roles}
}

// AllowedMentionsForRendered returns the subset of verified refs present in a
// message that was already processed by Render.
func AllowedMentionsForRendered(content string, refs []Ref) *discordgo.MessageAllowedMentions {
	userSet := make(map[string]bool)
	roleSet := make(map[string]bool)
	for _, ref := range refs {
		if !validRef(ref) {
			continue
		}
		switch ref.Kind {
		case "user":
			if strings.Contains(content, "<@"+ref.ID+">") {
				userSet[ref.ID] = true
			}
		case "role":
			if strings.Contains(content, "<@&"+ref.ID+">") {
				roleSet[ref.ID] = true
			}
		}
	}
	users := make([]string, 0, len(userSet))
	for id := range userSet {
		users = append(users, id)
	}
	sort.Strings(users)
	roles := make([]string, 0, len(roleSet))
	for id := range roleSet {
		roles = append(roles, id)
	}
	sort.Strings(roles)
	return &discordgo.MessageAllowedMentions{Users: users, Roles: roles}
}

// EscapeRaw makes raw Discord user, role, and channel mention syntax inert.
func EscapeRaw(content string) string {
	return rawMentionPattern.ReplaceAllString(content, "<$1\u200b$2>")
}

// PromptBlock documents the available refs for an agent prompt.
func PromptBlock(refs []Ref) string {
	var lines []string
	for _, ref := range refs {
		if !validRef(ref) {
			continue
		}
		name := cleanDisplayName(ref.DisplayName)
		if name == "" {
			name = ref.ID
		}
		lines = append(lines, fmt.Sprintf("- %s %s: use %s", ref.Kind, name, ref.Placeholder))
	}
	if len(lines) == 0 {
		return ""
	}
	sort.Strings(lines)
	return "[Discord mention references]\n" +
		"Use only these exact placeholders when you need to mention a Discord user or role. Do not write raw Discord mention syntax or guess IDs; unlisted targets cannot be mentioned.\n" +
		strings.Join(lines, "\n") + "\n"
}

func validRef(ref Ref) bool {
	if ref.Kind != "user" && ref.Kind != "role" {
		return false
	}
	if !validDiscordID(ref.ID) || ref.Placeholder == "" {
		return false
	}
	return true
}

func validDiscordID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func cleanDisplayName(name string) string {
	name = strings.ReplaceAll(name, "\r", " ")
	name = strings.ReplaceAll(name, "\n", " ")
	name = strings.ReplaceAll(name, "\t", " ")
	return strings.Join(strings.Fields(name), " ")
}
