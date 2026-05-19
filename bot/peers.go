package bot

import (
	"fmt"
	"strings"
)

type BotPeer struct {
	Name string
	ID   string
}

func parseBotPeers(raw string) []BotPeer {
	var peers []BotPeer
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		name, id, ok := strings.Cut(item, ":")
		name = strings.TrimSpace(name)
		id = strings.TrimSpace(id)
		if !ok || name == "" || id == "" {
			continue
		}
		peers = append(peers, BotPeer{Name: name, ID: id})
	}
	return peers
}

func (p BotPeer) Mention() string {
	return "<@" + p.ID + ">"
}

func (b *Bot) multiBotMode(selfID string) bool {
	for _, p := range b.peers {
		if p.ID != "" && p.ID != selfID {
			return true
		}
	}
	return false
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
		if p.ID == selfID {
			sb.WriteString(fmt.Sprintf("- self=%s id=%s mention=%s\n", p.Name, p.ID, p.Mention()))
			continue
		}
		hasHandoffPeer = true
		sb.WriteString(fmt.Sprintf("- handoff_peer=%s id=%s mention=%s\n", p.Name, p.ID, p.Mention()))
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
