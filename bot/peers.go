package bot

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
)

type BotPeer struct {
	Name   string
	ID     string
	RoleID string
}

func parseBotPeers(raw string) []BotPeer {
	var peers []BotPeer
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		name, rest, ok := strings.Cut(item, ":")
		name = strings.TrimSpace(name)
		id := strings.TrimSpace(rest)
		roleID := ""
		if ok {
			id, roleID, _ = strings.Cut(rest, ":")
			id = strings.TrimSpace(id)
			roleID = strings.TrimSpace(roleID)
		}
		if !ok || name == "" || id == "" {
			continue
		}
		peers = append(peers, BotPeer{Name: name, ID: id, RoleID: roleID})
	}
	return peers
}

func (p BotPeer) Mention() string {
	return "<@" + p.ID + ">"
}

func (p BotPeer) RoleMention() string {
	if p.RoleID == "" {
		return ""
	}
	return "<@&" + p.RoleID + ">"
}

func isRoleMentioned(content, roleID string) bool {
	return roleID != "" && strings.Contains(content, "<@&"+roleID+">")
}

func stripRoleMention(content, roleID string) string {
	if roleID == "" {
		return strings.TrimSpace(content)
	}
	return strings.TrimSpace(strings.ReplaceAll(content, "<@&"+roleID+">", ""))
}

func (b *Bot) multiBotMode(selfID string) bool {
	for _, p := range b.peers {
		if p.ID != "" && p.ID != selfID {
			return true
		}
	}
	return false
}

func (b *Bot) mentionsOtherPeer(content, selfID string) bool {
	for _, p := range b.peers {
		if p.ID == "" || p.ID == selfID {
			continue
		}
		if isSelfMentioned(content, p.ID) {
			return true
		}
	}
	return false
}

func (b *Bot) messageMentionsOtherPeer(m *discordgo.MessageCreate, content, selfID string) bool {
	for _, p := range b.peers {
		if p.ID == "" || p.ID == selfID {
			continue
		}
		if messageMentionsUser(m, content, p.ID) || isRoleMentioned(content, p.RoleID) {
			return true
		}
	}
	return false
}

func (b *Bot) messageMentionsSelf(m *discordgo.MessageCreate, content, selfID string) bool {
	if messageMentionsUser(m, content, selfID) {
		return true
	}
	for _, p := range b.peers {
		if p.ID == selfID && isRoleMentioned(content, p.RoleID) {
			return true
		}
	}
	return false
}

func (b *Bot) stripOwnMentions(content, selfID string) string {
	content = stripSelfMentions(content, selfID)
	for _, p := range b.peers {
		if p.ID == selfID {
			content = stripRoleMention(content, p.RoleID)
		}
	}
	return strings.TrimSpace(content)
}

func (b *Bot) requiresHumanMention(targetID, parentChannelID, selfID string) bool {
	if !b.multiBotMode(selfID) {
		return false
	}
	if b.manager == nil {
		return true
	}
	if b.manager.HasMentionOnlyOverride(targetID) {
		return true
	}
	if b.manager.HasFullListenOverride(targetID) {
		return false
	}
	if parentChannelID != "" {
		if b.manager.HasMentionOnlyOverride(parentChannelID) {
			return true
		}
		if b.manager.HasFullListenOverride(parentChannelID) {
			return false
		}
	}
	return true
}

func (b *Bot) peerPromptContext(selfID string) string {
	if len(b.peers) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("[Discord bot peers]\n")
	var hasHandoffPeer bool
	for _, p := range b.peers {
		roleText := ""
		if roleMention := p.RoleMention(); roleMention != "" {
			roleText = fmt.Sprintf(" role_mention=%s", roleMention)
		}
		if p.ID == selfID {
			sb.WriteString(fmt.Sprintf("- self=%s id=%s mention=%s%s\n", p.Name, p.ID, p.Mention(), roleText))
			continue
		}
		hasHandoffPeer = true
		sb.WriteString(fmt.Sprintf("- handoff_peer=%s id=%s mention=%s%s\n", p.Name, p.ID, p.Mention(), roleText))
	}
	sb.WriteString("[Discord bot handoff rules]\n")
	if hasHandoffPeer {
		sb.WriteString("- Use handoff_peer mention tokens exactly when explicitly asked to hand off, review, compare, or collaborate with another bot.\n")
		sb.WriteString("- Put handoff_peer mentions only in final result messages, after your own work is complete.\n")
	}
	sb.WriteString("- Never mention yourself for handoff.\n")
	sb.WriteString("- Do not mention another bot in progress updates, errors, tool output summaries, or casual replies.\n")
	return sb.String()
}
