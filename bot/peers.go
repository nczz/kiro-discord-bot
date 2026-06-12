package bot

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

const peerPermissionCacheTTL = 30 * time.Second

type peerPermissionCacheEntry struct {
	canRespond bool
	expiresAt  time.Time
}

type BotPeer struct {
	Name    string
	ID      string
	RoleID  string
	Exclude bool
	Manual  bool
}

func parseBotPeers(raw string) []BotPeer {
	var peers []BotPeer
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		exclude := false
		if strings.HasPrefix(item, "!") || strings.HasPrefix(item, "-") {
			exclude = true
			item = strings.TrimSpace(item[1:])
		}
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
		if !ok && exclude {
			id = name
			name = ""
		} else if !ok {
			continue
		}
		if id == "" && roleID == "" {
			continue
		}
		peers = append(peers, BotPeer{Name: name, ID: id, RoleID: roleID, Exclude: exclude, Manual: true})
	}
	return peers
}

func activeBotPeers(peers []BotPeer) []BotPeer {
	var active []BotPeer
	for _, p := range peers {
		if p.ID != "" && !p.Exclude {
			active = append(active, p)
		}
	}
	return active
}

func mergeBotPeers(discovered, manual []BotPeer) []BotPeer {
	merged := make(map[string]BotPeer)
	keyByID := make(map[string]string)
	keyByRoleID := make(map[string]string)
	order := make([]string, 0, len(discovered)+len(manual))
	keyFor := func(p BotPeer) string {
		if p.ID != "" {
			return "id:" + p.ID
		}
		if p.RoleID != "" {
			return "role:" + p.RoleID
		}
		return ""
	}
	add := func(p BotPeer) {
		key := keyFor(p)
		if key == "" {
			return
		}
		if p.Exclude {
			if existing := keyByID[p.ID]; existing != "" {
				delete(merged, existing)
			}
			if existing := keyByRoleID[p.ID]; existing != "" {
				delete(merged, existing)
			}
			if existing := keyByRoleID[p.RoleID]; existing != "" {
				delete(merged, existing)
			}
			delete(merged, key)
			return
		}
		if p.ID != "" {
			if existing := keyByID[p.ID]; existing != "" {
				key = existing
			} else if existing := keyByRoleID[p.RoleID]; existing != "" {
				key = existing
			}
		} else if p.RoleID != "" {
			if existing := keyByRoleID[p.RoleID]; existing != "" {
				key = existing
			}
		}
		if _, ok := merged[key]; !ok {
			order = append(order, key)
		}
		merged[key] = p
		if p.ID != "" {
			keyByID[p.ID] = key
		}
		if p.RoleID != "" {
			keyByRoleID[p.RoleID] = key
		}
	}
	for _, p := range discovered {
		add(p)
	}
	for _, p := range manual {
		add(p)
	}
	var peers []BotPeer
	for _, id := range order {
		if p, ok := merged[id]; ok {
			peers = append(peers, p)
		}
	}
	return peers
}

func (b *Bot) peerSnapshot() []BotPeer {
	if b == nil {
		return nil
	}
	b.peerMu.RLock()
	defer b.peerMu.RUnlock()
	peers := make([]BotPeer, len(b.peers))
	copy(peers, b.peers)
	return peers
}

func (b *Bot) setDiscoveredPeers(discovered []BotPeer) {
	b.peerMu.Lock()
	defer b.peerMu.Unlock()
	b.peers = mergeBotPeers(discovered, b.manualPeers)
}

func (p BotPeer) Mention() string {
	if p.ID == "" {
		return ""
	}
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
	peers := b.peerSnapshot()
	selfRoles := make(map[string]bool)
	for _, p := range peers {
		if p.ID == selfID && p.RoleID != "" {
			selfRoles[p.RoleID] = true
		}
	}
	for _, p := range peers {
		if p.ID != "" && p.ID != selfID {
			return true
		}
		if p.ID == "" && p.RoleID != "" && !selfRoles[p.RoleID] {
			return true
		}
	}
	return false
}

func (b *Bot) channelMultiBotMode(ds *discordgo.Session, targetID, selfID string) bool {
	_, ok := b.channelMultiBotTrigger(ds, targetID, selfID)
	return ok
}

type peerPresenceDiagnostic struct {
	Peer     BotPeer
	Presence string
}

func (b *Bot) channelMultiBotTrigger(ds *discordgo.Session, targetID, selfID string) (peerPresenceDiagnostic, bool) {
	if !b.multiBotMode(selfID) {
		return peerPresenceDiagnostic{}, false
	}
	for _, p := range b.peerSnapshot() {
		if p.ID == selfID {
			continue
		}
		if p.ID == "" {
			if presence, ok := b.peerExplicitlyPresentInTarget(ds, p, targetID); ok {
				return peerPresenceDiagnostic{Peer: p, Presence: presence}, true
			}
			continue
		}
		presence, present := b.peerExplicitlyPresentInTarget(ds, p, targetID)
		if b.peerCanRespondInTarget(ds, p.ID, targetID) {
			if present {
				return peerPresenceDiagnostic{Peer: p, Presence: presence}, true
			}
			return peerPresenceDiagnostic{Peer: p, Presence: "effective permissions"}, true
		}
	}
	return peerPresenceDiagnostic{}, false
}

func (b *Bot) peerExplicitlyPresentInTarget(ds *discordgo.Session, p BotPeer, targetID string) (string, bool) {
	if ds == nil || ds.State == nil || targetID == "" {
		return "", false
	}
	ch, err := ds.State.Channel(targetID)
	if err != nil || ch == nil {
		return "", false
	}
	sendPermission := int64(discordgo.PermissionSendMessages)
	required := int64(discordgo.PermissionViewChannel) | sendPermission
	permissionChannel := ch
	if ch.IsThread() {
		sendPermission = int64(discordgo.PermissionSendMessagesInThreads)
		required = int64(discordgo.PermissionViewChannel) | sendPermission
		if ch.ParentID != "" {
			if parent, err := ds.State.Channel(ch.ParentID); err == nil && parent != nil {
				permissionChannel = parent
			}
		}
	}
	for _, ow := range permissionChannel.PermissionOverwrites {
		if ow == nil {
			continue
		}
		if ow.Deny&required != 0 {
			continue
		}
		matchesPeer := p.ID != "" && ow.Type == discordgo.PermissionOverwriteTypeMember && ow.ID == p.ID
		matchesRole := p.RoleID != "" && ow.Type == discordgo.PermissionOverwriteTypeRole && ow.ID == p.RoleID
		if p.ID != "" && (matchesPeer || matchesRole) {
			if ow.Allow&(int64(discordgo.PermissionViewChannel)|sendPermission) != 0 {
				if matchesPeer {
					return "member overwrite", true
				}
				return "role overwrite", true
			}
		}
		if p.ID == "" && p.Manual && matchesRole && ow.Allow&required == required {
			return "manual role overwrite", true
		}
	}
	return "", false
}

func (b *Bot) peerCanRespondInTarget(ds *discordgo.Session, peerID, targetID string) bool {
	if ds == nil || peerID == "" || targetID == "" {
		return false
	}
	key := targetID + ":" + peerID
	now := time.Now()
	if b != nil {
		b.peerPermMu.Lock()
		if entry, ok := b.peerPermCache[key]; ok && now.Before(entry.expiresAt) {
			b.peerPermMu.Unlock()
			return entry.canRespond
		}
		b.peerPermMu.Unlock()
	}
	perms, err := ds.UserChannelPermissions(peerID, targetID)
	if err != nil {
		log.Printf("[peers] channel permission check failed peer=%s channel=%s: %v", peerID, targetID, err)
		return false
	}
	required := int64(discordgo.PermissionViewChannel | discordgo.PermissionSendMessages)
	if ch, err := ds.State.Channel(targetID); err == nil && ch != nil && ch.IsThread() {
		required = int64(discordgo.PermissionViewChannel | discordgo.PermissionSendMessagesInThreads)
	}
	canRespond := perms&required == required
	if !canRespond {
		channelThreadReply := int64(discordgo.PermissionViewChannel | discordgo.PermissionCreatePublicThreads | discordgo.PermissionSendMessagesInThreads)
		canRespond = perms&channelThreadReply == channelThreadReply
	}
	if b != nil {
		b.peerPermMu.Lock()
		if b.peerPermCache == nil {
			b.peerPermCache = make(map[string]peerPermissionCacheEntry)
		}
		b.peerPermCache[key] = peerPermissionCacheEntry{canRespond: canRespond, expiresAt: now.Add(peerPermissionCacheTTL)}
		b.peerPermMu.Unlock()
	}
	return canRespond
}

func (b *Bot) slashCommandAllowedInTarget(ds *discordgo.Session, targetID string) bool {
	if ds == nil || ds.State == nil || ds.State.User == nil {
		return true
	}
	return b.peerCanRespondInTarget(ds, ds.State.User.ID, targetID)
}

func (b *Bot) mentionsOtherPeer(content, selfID string) bool {
	for _, p := range b.peerSnapshot() {
		if p.ID == "" || p.ID == selfID {
			if p.ID == selfID || !isRoleMentioned(content, p.RoleID) {
				continue
			}
			return true
		}
		if isSelfMentioned(content, p.ID) || isRoleMentioned(content, p.RoleID) {
			return true
		}
	}
	return false
}

func (b *Bot) messageMentionsOtherPeer(m *discordgo.MessageCreate, content, selfID string) bool {
	for _, p := range b.peerSnapshot() {
		if p.ID == selfID {
			continue
		}
		if p.ID != "" && messageMentionsUser(m, content, p.ID) {
			return true
		}
		if isRoleMentioned(content, p.RoleID) {
			return true
		}
	}
	if m != nil && m.Message != nil {
		for _, u := range m.Mentions {
			if u != nil && u.Bot && u.ID != "" && u.ID != selfID {
				return true
			}
		}
	}
	return false
}

func (b *Bot) messageMentionsSelf(m *discordgo.MessageCreate, content, selfID string) bool {
	if messageMentionsUser(m, content, selfID) {
		return true
	}
	for _, p := range b.peerSnapshot() {
		if p.ID == selfID && isRoleMentioned(content, p.RoleID) {
			return true
		}
	}
	return false
}

func (b *Bot) humanMessageAddressesSelf(m *discordgo.MessageCreate, content, selfID string) bool {
	content = strings.TrimSpace(content)
	if content == "" || !b.messageMentionsSelf(m, content, selfID) {
		return false
	}
	leading := b.leadingMentionPeerIDs(content)
	if len(leading) == 0 {
		return true
	}
	for _, id := range leading {
		if id == selfID {
			return true
		}
	}
	return false
}

func (b *Bot) leadingMentionPeerIDs(content string) []string {
	var ids []string
	for {
		content = strings.TrimSpace(content)
		if content == "" {
			return ids
		}
		matched := false
		for _, p := range b.peerSnapshot() {
			if p.ID != "" {
				for _, token := range []string{"<@" + p.ID + ">", "<@!" + p.ID + ">"} {
					if strings.HasPrefix(content, token) {
						ids = append(ids, p.ID)
						content = strings.TrimSpace(strings.TrimPrefix(content, token))
						matched = true
						break
					}
				}
				if matched {
					break
				}
			}
			if p.RoleID != "" {
				token := "<@&" + p.RoleID + ">"
				if strings.HasPrefix(content, token) {
					ids = append(ids, p.ID)
					content = strings.TrimSpace(strings.TrimPrefix(content, token))
					matched = true
					break
				}
			}
		}
		if !matched {
			return ids
		}
	}
}

func (b *Bot) stripOwnMentions(content, selfID string) string {
	content = stripSelfMentions(content, selfID)
	for _, p := range b.peerSnapshot() {
		if p.ID == selfID {
			content = stripRoleMention(content, p.RoleID)
		}
	}
	return strings.TrimSpace(content)
}

func (b *Bot) stripLeadingPeerMentions(content string) string {
	for {
		next := strings.TrimSpace(content)
		for _, p := range b.peerSnapshot() {
			if p.ID != "" {
				next = strings.TrimSpace(strings.TrimPrefix(next, "<@"+p.ID+">"))
				next = strings.TrimSpace(strings.TrimPrefix(next, "<@!"+p.ID+">"))
			}
			if p.RoleID != "" {
				next = strings.TrimSpace(strings.TrimPrefix(next, "<@&"+p.RoleID+">"))
			}
		}
		if next == strings.TrimSpace(content) {
			return next
		}
		content = next
	}
}

func (b *Bot) requiresHumanMention(ds *discordgo.Session, targetID, parentChannelID, selfID string) (bool, string) {
	if b.manager != nil {
		if b.manager.HasMentionOnlyOverride(targetID) {
			return true, "paused"
		}
		if b.manager.HasFullListenOverride(targetID) {
			return false, ""
		}
		if parentChannelID != "" {
			if mode, ok := b.manager.ThreadListenSnapshot(targetID); ok {
				if mode == "mention" {
					return true, "thread_snapshot_mention"
				}
				return false, ""
			}
			if b.manager.ThreadMentionOnly(targetID, parentChannelID) {
				return true, "thread_inherit"
			}
		} else if !b.manager.ThreadModeEnabled(targetID) {
			return true, "thread_mode_off"
		}
	}
	if !b.channelMultiBotMode(ds, targetID, selfID) {
		return false, ""
	}
	if b.manager == nil {
		return true, "multi_bot"
	}
	if parentChannelID != "" {
		if b.manager.HasMentionOnlyOverride(parentChannelID) {
			return true, "multi_bot_parent_paused"
		}
		if b.manager.HasFullListenOverride(parentChannelID) {
			return false, ""
		}
	}
	return true, "multi_bot"
}

func (b *Bot) peerPromptContext(selfID string) string {
	peers := b.peerSnapshot()
	if len(peers) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("[Discord bot peers]\n")
	var hasHandoffPeer bool
	for _, p := range peers {
		roleText := ""
		if roleMention := p.RoleMention(); roleMention != "" {
			roleText = fmt.Sprintf(" role_mention=%s", roleMention)
		}
		if p.ID == selfID {
			sb.WriteString(fmt.Sprintf("- self=%s id=%s mention=%s%s\n", p.Name, p.ID, p.Mention(), roleText))
			continue
		}
		hasHandoffPeer = true
		if p.ID == "" {
			sb.WriteString(fmt.Sprintf("- handoff_peer=%s%s\n", p.Name, roleText))
			continue
		}
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

func (b *Bot) discoverBotPeers(ds *discordgo.Session, ready *discordgo.Ready) {
	if ds == nil || ready == nil || ready.User == nil {
		return
	}
	guildIDs := b.peerDiscoveryGuildIDs(ready)
	if len(guildIDs) == 0 {
		log.Printf("[peers] discovery skipped: no guild available")
		return
	}

	var discovered []BotPeer
	for _, guildID := range guildIDs {
		roles, err := b.guildRoles(ds, guildID)
		if err != nil {
			log.Printf("[peers] discover roles guild=%s: %v", guildID, err)
		}
		members, err := b.guildMembers(ds, guildID)
		if err != nil {
			log.Printf("[peers] discover members guild=%s: %v", guildID, err)
			continue
		}
		roleByID := make(map[string]*discordgo.Role, len(roles))
		for _, role := range roles {
			if role != nil {
				roleByID[role.ID] = role
			}
		}
		for _, member := range members {
			if member == nil || member.User == nil || !member.User.Bot {
				continue
			}
			name := member.DisplayName()
			if name == "" {
				name = member.User.Username
			}
			discovered = append(discovered, BotPeer{
				Name:   name,
				ID:     member.User.ID,
				RoleID: botRoleID(member, roleByID),
			})
		}
		knownRoleIDs := make(map[string]bool)
		for _, p := range discovered {
			if p.RoleID != "" {
				knownRoleIDs[p.RoleID] = true
			}
		}
		for _, role := range roles {
			if role == nil || !role.Managed || knownRoleIDs[role.ID] {
				continue
			}
			discovered = append(discovered, BotPeer{Name: role.Name, RoleID: role.ID})
		}
	}
	b.setDiscoveredPeers(discovered)
	userPeers, roleOnlyPeers := botPeerCounts(b.peerSnapshot())
	log.Printf("[peers] discovery complete guilds=%d discovered=%d manual=%d active=%d user_peers=%d role_only_peers=%d", len(guildIDs), len(discovered), len(b.manualPeers), len(b.peerSnapshot()), userPeers, roleOnlyPeers)
}

func (b *Bot) peerDiscoveryGuildIDs(ready *discordgo.Ready) []string {
	if b.guildID != "" {
		return []string{b.guildID}
	}
	seen := make(map[string]bool)
	var ids []string
	for _, guild := range ready.Guilds {
		if guild != nil && guild.ID != "" && !seen[guild.ID] {
			seen[guild.ID] = true
			ids = append(ids, guild.ID)
		}
	}
	return ids
}

func (b *Bot) guildRoles(ds *discordgo.Session, guildID string) ([]*discordgo.Role, error) {
	if ds.State != nil {
		if guild, err := ds.State.Guild(guildID); err == nil && guild != nil && len(guild.Roles) > 0 {
			return guild.Roles, nil
		}
	}
	return ds.GuildRoles(guildID)
}

func (b *Bot) guildMembers(ds *discordgo.Session, guildID string) ([]*discordgo.Member, error) {
	var all []*discordgo.Member
	after := ""
	for {
		members, err := ds.GuildMembers(guildID, after, 1000)
		if err != nil {
			if ds.State != nil {
				if guild, stateErr := ds.State.Guild(guildID); stateErr == nil && guild != nil && len(guild.Members) > 0 {
					return guild.Members, nil
				}
			}
			return all, err
		}
		all = append(all, members...)
		if len(members) < 1000 {
			break
		}
		last := members[len(members)-1]
		if last == nil || last.User == nil || last.User.ID == "" {
			break
		}
		after = last.User.ID
	}
	if len(all) == 0 && ds.State != nil {
		if guild, err := ds.State.Guild(guildID); err == nil && guild != nil && len(guild.Members) > 0 {
			return guild.Members, nil
		}
	}
	return all, nil
}

func botPeerCounts(peers []BotPeer) (userPeers, roleOnlyPeers int) {
	for _, p := range peers {
		if p.ID != "" {
			userPeers++
		} else if p.RoleID != "" {
			roleOnlyPeers++
		}
	}
	return userPeers, roleOnlyPeers
}

func botRoleID(member *discordgo.Member, roleByID map[string]*discordgo.Role) string {
	if member == nil || member.User == nil {
		return ""
	}
	display := strings.ToLower(member.DisplayName())
	username := strings.ToLower(member.User.Username)
	var firstManaged string
	for _, roleID := range member.Roles {
		role := roleByID[roleID]
		if role == nil {
			continue
		}
		roleName := strings.ToLower(role.Name)
		if role.Managed && firstManaged == "" {
			firstManaged = role.ID
		}
		if role.Managed && (roleName == display || roleName == username) {
			return role.ID
		}
	}
	if firstManaged != "" {
		return firstManaged
	}
	for _, roleID := range member.Roles {
		role := roleByID[roleID]
		if role == nil {
			continue
		}
		roleName := strings.ToLower(role.Name)
		if roleName == display || roleName == username {
			return role.ID
		}
	}
	return ""
}
