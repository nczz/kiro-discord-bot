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

func (b *Bot) peerPromptContext() string {
	if len(b.peers) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("[Discord bot peers]\n")
	for _, p := range b.peers {
		sb.WriteString(fmt.Sprintf("- %s id=%s mention=%s\n", p.Name, p.ID, p.Mention()))
	}
	sb.WriteString("[Discord bot handoff rules]\n")
	sb.WriteString("- Use the peer mention token exactly when explicitly asked to hand off, review, compare, or collaborate with another bot.\n")
	sb.WriteString("- Put peer bot mentions only in final result messages, after your own work is complete.\n")
	sb.WriteString("- Do not mention another bot in progress updates, errors, tool output summaries, or casual replies.\n")
	return sb.String()
}
