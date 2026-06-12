package channel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type botToolsTargetState struct {
	TargetChannelID string `json:"target_channel_id"`
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

func writeBotToolsTargetState(path, targetChannelID string) error {
	path = strings.TrimSpace(path)
	targetChannelID = strings.TrimSpace(targetChannelID)
	if path == "" || targetChannelID == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	raw, err := json.Marshal(botToolsTargetState{TargetChannelID: targetChannelID})
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func clearBotToolsTargetState(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	_ = os.Remove(path)
}
