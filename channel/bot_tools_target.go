package channel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/nczz/kiro-discord-bot/internal/discordmention"
)

type botToolsTargetState struct {
	GuildID               string               `json:"guild_id,omitempty"`
	TargetChannelID       string               `json:"target_channel_id"`
	DisableEgress         bool                 `json:"disable_egress,omitempty"`
	RemoteA2A             bool                 `json:"remote_a2a,omitempty"`
	AllowMemoryWrite      bool                 `json:"allow_memory_write,omitempty"`
	DelegationDepth       int                  `json:"delegation_depth,omitempty"`
	RequesterID           string               `json:"requester_id,omitempty"`
	RequesterName         string               `json:"requester_name,omitempty"`
	Source                string               `json:"source,omitempty"`
	CanManageChannel      bool                 `json:"can_manage_channel,omitempty"`
	CanManageGuild        bool                 `json:"can_manage_guild,omitempty"`
	AllowedMentionUserIDs []string             `json:"allowed_mention_user_ids,omitempty"`
	MentionRefs           []discordmention.Ref `json:"mention_refs,omitempty"`
}

// BotToolsTargetOptions controls dynamic bot-tools delivery state for a live job.
type BotToolsTargetOptions struct {
	DisableEgress bool
	Source        string
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

func writeBotToolsTargetState(path, targetChannelID, guildID string) error {
	return writeBotToolsTargetStateOptions(path, targetChannelID, false, guildID)
}

func writeBotToolsTargetStateOptions(path, targetChannelID string, disableEgress bool, guildID string) error {
	return writeBotToolsTargetStateWithRefs(path, targetChannelID, disableEgress, nil, guildID)
}

func writeBotToolsTargetStateWithRefs(path, targetChannelID string, disableEgress bool, refs []discordmention.Ref, guildID string) error {
	return writeBotToolsTargetStateWithRequester(path, targetChannelID, disableEgress, refs, false, false, "", "", 0, false, false, guildID)
}

func writeBotToolsTargetStateWithPolicy(path, targetChannelID string, disableEgress bool, refs []discordmention.Ref, remoteA2A bool, allowMemoryWrite bool, guildID string) error {
	return writeBotToolsTargetStateWithRequester(path, targetChannelID, disableEgress, refs, remoteA2A, allowMemoryWrite, "", "", 0, false, false, guildID)
}

func writeBotToolsTargetStateWithRequester(path, targetChannelID string, disableEgress bool, refs []discordmention.Ref, remoteA2A bool, allowMemoryWrite bool, requesterID, requesterName string, delegationDepth int, canManageChannel, canManageGuild bool, guildID string) error {
	return writeBotToolsTargetStateWithRequesterSource(path, targetChannelID, disableEgress, refs, remoteA2A, allowMemoryWrite, requesterID, requesterName, delegationDepth, canManageChannel, canManageGuild, "", guildID)
}

func writeBotToolsTargetStateWithRequesterSource(path, targetChannelID string, disableEgress bool, refs []discordmention.Ref, remoteA2A bool, allowMemoryWrite bool, requesterID, requesterName string, delegationDepth int, canManageChannel, canManageGuild bool, source, guildID string) error {
	path = strings.TrimSpace(path)
	targetChannelID = strings.TrimSpace(targetChannelID)
	if path == "" || targetChannelID == "" {
		return nil
	}
	if remoteA2A {
		canManageChannel = false
		canManageGuild = false
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	raw, err := json.Marshal(botToolsTargetState{
		GuildID:               strings.TrimSpace(guildID),
		TargetChannelID:       targetChannelID,
		DisableEgress:         disableEgress,
		RemoteA2A:             remoteA2A,
		AllowMemoryWrite:      allowMemoryWrite,
		DelegationDepth:       delegationDepth,
		RequesterID:           strings.TrimSpace(requesterID),
		RequesterName:         strings.TrimSpace(requesterName),
		Source:                strings.TrimSpace(source),
		CanManageChannel:      canManageChannel,
		CanManageGuild:        canManageGuild,
		AllowedMentionUserIDs: discordmention.UserIDs(refs),
		MentionRefs:           discordmention.NormalizeRefs(refs),
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

func parseDiscordUserIDSet(raw string) map[string]bool {
	out := map[string]bool{}
	for _, item := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\t' || r == ' '
	}) {
		item = strings.TrimSpace(item)
		if item != "" {
			out[item] = true
		}
	}
	return out
}

func discordUserIDSetContains(set map[string]bool, userID string) bool {
	userID = strings.TrimSpace(userID)
	return userID != "" && set[userID]
}

func botToolsRequesterPermissions(ds *discordgo.Session, userID, targetID, fallbackParentID string, gmUserIDs map[string]bool) (bool, bool) {
	userID = strings.TrimSpace(userID)
	targetID = strings.TrimSpace(targetID)
	fallbackParentID = strings.TrimSpace(fallbackParentID)
	if discordUserIDSetContains(gmUserIDs, userID) {
		return true, true
	}
	if ds == nil || userID == "" || targetID == "" {
		return false, false
	}
	if fallbackParentID != "" && fallbackParentID != targetID {
		if _, err := ds.State.Channel(targetID); err != nil {
			if parentManageChannel, parentManageGuild, parentOK := botToolsPermissionsForTarget(ds, userID, fallbackParentID); parentOK {
				return parentManageChannel, parentManageGuild
			}
		}
	}
	canManageChannel, canManageGuild, ok := botToolsPermissionsForTarget(ds, userID, targetID)
	if ok && canManageChannel {
		return canManageChannel, canManageGuild
	}
	if fallbackParentID != "" && fallbackParentID != targetID {
		parentManageChannel, parentManageGuild, parentOK := botToolsPermissionsForTarget(ds, userID, fallbackParentID)
		if parentOK && parentManageChannel {
			return parentManageChannel || canManageChannel, parentManageGuild || canManageGuild
		}
	}
	if ch, err := ds.State.Channel(targetID); err == nil && ch != nil && ch.IsThread() && strings.TrimSpace(ch.ParentID) != "" {
		parentManageChannel, parentManageGuild, parentOK := botToolsPermissionsForTarget(ds, userID, ch.ParentID)
		if parentOK {
			return parentManageChannel || canManageChannel, parentManageGuild || canManageGuild
		}
	}
	return canManageChannel, canManageGuild
}

func botToolsPermissionsForTarget(ds *discordgo.Session, userID, targetID string) (bool, bool, bool) {
	perms, err := ds.UserChannelPermissions(userID, targetID)
	if err != nil {
		return false, false, false
	}
	canManageChannel := perms&int64(discordgo.PermissionAdministrator|discordgo.PermissionManageChannels) != 0
	canManageGuild := perms&int64(discordgo.PermissionAdministrator|discordgo.PermissionManageGuild) != 0
	return canManageChannel, canManageGuild, true
}

func readBotToolsTargetMentionRefs(path string) ([]discordmention.Ref, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var state botToolsTargetState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, err
	}
	return discordmention.NormalizeRefs(state.MentionRefs), nil
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
	return writeBotToolsTargetState(botToolsTargetStatePath(m.dataDir, channelID), targetChannelID, m.guildID)
}

// SetBotToolsTargetStateOptions binds bot-tools to a live target with explicit
// egress/source controls.
func (m *Manager) SetBotToolsTargetStateOptions(channelID, targetChannelID string, opts BotToolsTargetOptions) error {
	return writeBotToolsTargetStateWithRequesterSource(
		botToolsTargetStatePath(m.dataDir, channelID),
		targetChannelID,
		opts.DisableEgress,
		nil,
		false,
		false,
		"",
		"",
		0,
		false,
		false,
		opts.Source,
		m.guildID,
	)
}

// ClearBotToolsTargetState removes a channel's dynamic bot-tools egress target.
func (m *Manager) ClearBotToolsTargetState(channelID string) {
	clearBotToolsTargetState(botToolsTargetStatePath(m.dataDir, channelID))
}
