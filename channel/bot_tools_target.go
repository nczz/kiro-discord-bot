package channel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/nczz/kiro-discord-bot/internal/discordmention"
)

type botToolsTargetState struct {
	TargetChannelID       string   `json:"target_channel_id"`
	DisableEgress         bool     `json:"disable_egress,omitempty"`
	AllowedMentionUserIDs []string `json:"allowed_mention_user_ids,omitempty"`
}

func botToolsTargetStatePath(dataDir, channelID string) string {
	dataDir = strings.TrimSpace(dataDir)
	channelID = strings.TrimSpace(channelID)
	if dataDir == "" || channelID == "" {
		return ""
	}
	channelID = strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(channelID)
	return filepath.Join(dataDir, "bot-tools-targets", channelID+".json")
}

func botToolsTargetStateID(channelID, targetChannelID string) string {
	channelID = strings.TrimSpace(channelID)
	targetChannelID = strings.TrimSpace(targetChannelID)
	if targetChannelID != "" {
		return targetChannelID
	}
	return channelID
}

func writeBotToolsTargetState(path, targetChannelID string) error {
	return writeBotToolsTargetStateOptions(path, targetChannelID, false)
}

func writeBotToolsTargetStateOptions(path, targetChannelID string, disableEgress bool) error {
	return writeBotToolsTargetStateWithRefs(path, targetChannelID, disableEgress, nil)
}

func writeBotToolsTargetStateWithRefs(path, targetChannelID string, disableEgress bool, refs []discordmention.Ref) error {
	path = strings.TrimSpace(path)
	targetChannelID = strings.TrimSpace(targetChannelID)
	if path == "" || targetChannelID == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	raw, err := json.Marshal(botToolsTargetState{
		TargetChannelID:       targetChannelID,
		DisableEgress:         disableEgress,
		AllowedMentionUserIDs: allowedMentionUserIDs(refs),
	})
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
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

func clearBotToolsTargetState(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	_ = os.Remove(path)
}

// SetBotToolsTargetState binds channel-scoped bot-tools egress to a live target
// such as an auto-created task thread.
func (m *Manager) SetBotToolsTargetState(channelID, targetChannelID string) error {
	return writeBotToolsTargetState(botToolsTargetStatePath(m.dataDir, channelID), targetChannelID)
}

// ClearBotToolsTargetState removes a channel's dynamic bot-tools egress target.
func (m *Manager) ClearBotToolsTargetState(channelID string) {
	clearBotToolsTargetState(botToolsTargetStatePath(m.dataDir, channelID))
}
