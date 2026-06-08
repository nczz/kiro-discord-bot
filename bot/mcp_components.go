package bot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/channel"
	L "github.com/nczz/kiro-discord-bot/locale"
)

const (
	mcpCustomPrefix  = "mcpui"
	mcpMaxOptions    = 25
	mcpContentLimit  = 1900
	mcpServerTokenID = "s_"
	mcpToolTokenID   = "t_"
)

func (b *Bot) sendMCPManagePanel(ds *discordgo.Session, i *discordgo.InteractionCreate, ctx cmdCtx) {
	content, components := b.buildMCPManagePanel(ctx.channelID, "")
	sent, err := ds.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content:    content,
		Components: components,
	})
	b.recordCommandResponseDelivery(ctx, "mcp", "slash", "sent", content, map[string]any{"has_components": true, "mcp_ui": "manage"}, sent, err)
}

func (b *Bot) handleMCPComponent(ds *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.MessageComponentData()
	parts := parseMCPComponentID(data.CustomID)
	if len(parts) < 3 || parts[0] != mcpCustomPrefix {
		return
	}
	action := parts[1]
	channelID := parts[2]
	userID, _ := interactionUser(i)
	if !b.userCanManageAuditTarget(ds, userID, channelID) {
		respondInteractionEphemeral(ds, i, L.Get("mcp.forbidden"))
		return
	}
	_ = ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	})
	edit := func(content string, components []discordgo.MessageComponent) {
		if _, err := ds.InteractionResponseEdit(i.Interaction, webhookEdit(content, components)); err != nil {
			log.Printf("[mcp-ui] interaction edit failed action=%s channel=%s content_len=%d components=%d: %v", action, channelID, len(content), len(components), err)
		}
	}
	fail := func(msg string) {
		edit(msg, nil)
	}

	selected := ""
	switch action {
	case "select":
		if len(data.Values) > 0 {
			selected = b.resolveMCPServerToken(channelID, data.Values[0])
		}
	case "apply":
		if len(parts) < 5 {
			fail(L.Get("error.expired"))
			return
		}
		selected = b.resolveMCPServerToken(channelID, parts[3])
		if selected == "" {
			fail(L.Get("error.expired"))
			return
		}
		preset := parts[4]
		msg := b.applyMCPComponentAction(channelID, userID, selected, preset)
		content, components := b.buildMCPManagePanel(channelID, selected)
		if msg != "" {
			content = msg + "\n\n" + content
		}
		edit(content, components)
		return
	case "tools":
		if len(parts) < 4 {
			fail(L.Get("error.expired"))
			return
		}
		selected = b.resolveMCPServerToken(channelID, parts[3])
		if selected == "" {
			fail(L.Get("error.expired"))
			return
		}
		content, components := b.buildMCPToolsPanel(channelID, selected, "", 0)
		edit(content, components)
		return
	case "scan":
		if len(parts) < 4 {
			fail(L.Get("error.expired"))
			return
		}
		selected = b.resolveMCPServerToken(channelID, parts[3])
		if selected == "" {
			fail(L.Get("error.expired"))
			return
		}
		msg := b.discoverMCPToolsMessage(selected)
		content, components := b.buildMCPToolsPanel(channelID, selected, msg, 0)
		edit(content, components)
		return
	case "toolpage":
		if len(parts) < 5 {
			fail(L.Get("error.expired"))
			return
		}
		selected = b.resolveMCPServerToken(channelID, parts[3])
		if selected == "" {
			fail(L.Get("error.expired"))
			return
		}
		page := parseMCPPage(parts[4])
		content, components := b.buildMCPToolsPanel(channelID, selected, "", page)
		edit(content, components)
		return
	case "tooladd", "toolremove":
		if len(parts) < 5 || len(data.Values) == 0 {
			fail(L.Get("error.expired"))
			return
		}
		selected = b.resolveMCPServerToken(channelID, parts[3])
		if selected == "" {
			fail(L.Get("error.expired"))
			return
		}
		tool := b.resolveMCPToolToken(channelID, selected, data.Values[0])
		if tool == "" {
			fail(L.Get("error.expired"))
			return
		}
		page := parseMCPPage(parts[4])
		allow := action == "tooladd"
		msg := b.applyMCPToolSelection(channelID, userID, selected, tool, allow)
		content, components := b.buildMCPToolsPanel(channelID, selected, msg, page)
		edit(content, components)
		return
	case "back":
		content, components := b.buildMCPManagePanel(channelID, "")
		edit(content, components)
		return
	case "restart":
		stopped := b.manager.RestartMCPScope(channelID)
		content, components := b.buildMCPManagePanel(channelID, "")
		content = L.Getf("mcp.restarted", stopped) + "\n\n" + content
		edit(content, components)
		return
	}
	content, components := b.buildMCPManagePanel(channelID, selected)
	edit(content, components)
}

func webhookEdit(content string, components []discordgo.MessageComponent) *discordgo.WebhookEdit {
	content = truncateDiscordMessageContent(content, mcpContentLimit)
	return &discordgo.WebhookEdit{Content: &content, Components: &components}
}

func truncateDiscordMessageContent(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if max <= 0 || len(r) <= max {
		return string(r)
	}
	if max <= 3 {
		return string(r[:max])
	}
	return string(r[:max-3]) + "..."
}

func (b *Bot) applyMCPComponentAction(channelID, userID, server, preset string) string {
	switch preset {
	case "disable":
		if err := b.manager.SetMCPPolicy(channelID, userID, server, false, ""); err != nil {
			return commandError(err)
		}
		return L.Getf("mcp.disabled", server)
	case "full":
		if err := b.manager.SetMCPPolicy(channelID, userID, server, true, preset); err != nil {
			return commandError(err)
		}
		return L.Getf("mcp.enabled", server)
	default:
		return L.Get("error.expired")
	}
}

func (b *Bot) applyMCPToolSelection(channelID, userID, server, tool string, allow bool) string {
	if err := b.manager.SetMCPTool(channelID, userID, server, tool, allow); err != nil {
		return commandError(err)
	}
	return L.Getf("mcp.tool_updated", server, tool)
}

func (b *Bot) discoverMCPToolsMessage(server string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tools, err := b.manager.DiscoverMCPTools(ctx, server)
	if err != nil {
		return commandError(err)
	}
	return L.Getf("mcp.tools.scan_done", server, len(tools))
}

func (b *Bot) buildMCPManagePanel(channelID, selected string) (string, []discordgo.MessageComponent) {
	views, err := b.manager.MCPServerViews(channelID)
	if err != nil {
		return commandError(err), nil
	}
	var selectedView *channel.MCPServerView
	for i := range views {
		if views[i].Name == selected {
			selectedView = &views[i]
			break
		}
	}
	var sb strings.Builder
	sb.WriteString(L.Get("mcp.manage.header"))
	sb.WriteString("\n")
	if len(views) == 0 {
		sb.WriteString(L.Get("mcp.catalog.empty"))
		return sb.String(), nil
	}
	sb.WriteString(L.Getf("mcp.manage.summary", len(views)))
	if len(views) > mcpMaxOptions {
		sb.WriteString("\n")
		sb.WriteString(L.Getf("mcp.manage.truncated", mcpMaxOptions))
	}
	if selectedView != nil {
		sb.WriteString("\n\n")
		sb.WriteString(formatMCPServerPanel(*selectedView))
	}

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.SelectMenu{
				MenuType:    discordgo.StringSelectMenu,
				CustomID:    mcpComponentID("select", channelID),
				Placeholder: L.Get("mcp.manage.select_placeholder"),
				MaxValues:   1,
				Options:     mcpSelectOptions(views, selected),
			},
		}},
	}
	if selectedView != nil {
		components = append(components, discordgo.ActionsRow{Components: mcpActionButtons(channelID, *selectedView)})
		components = append(components, discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label:    L.Get("mcp.manage.btn.tools"),
				Style:    discordgo.PrimaryButton,
				CustomID: mcpComponentID("tools", channelID, mcpServerToken(selectedView.Name)),
			},
		}})
	}
	components = append(components, discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.Button{
			Label:    L.Get("mcp.manage.btn.restart"),
			Style:    discordgo.SecondaryButton,
			CustomID: mcpComponentID("restart", channelID),
		},
	}})
	return sb.String(), components
}

func (b *Bot) buildMCPToolsPanel(channelID, server, prefix string, page int) (string, []discordgo.MessageComponent) {
	view, err := b.manager.MCPServerView(channelID, server)
	if err != nil {
		return commandError(err), nil
	}
	tools, err := b.manager.MCPToolViews(channelID, server)
	if err != nil {
		return commandError(err), nil
	}
	var sb strings.Builder
	if prefix != "" {
		sb.WriteString(prefix)
		sb.WriteString("\n\n")
	}
	sb.WriteString(L.Getf("mcp.tools.header", server))
	sb.WriteString("\n")
	sb.WriteString(formatMCPServerPanel(view))
	sb.WriteString("\n\n")
	if len(tools) == 0 {
		sb.WriteString(L.Get("mcp.tools.empty"))
	} else {
		totalPages := mcpToolPageCount(len(tools))
		if page < 0 {
			page = 0
		}
		if page >= totalPages {
			page = totalPages - 1
		}
		start, end := mcpToolPageBounds(len(tools), page)
		allowed, blocked := mcpToolCounts(tools)
		sb.WriteString(L.Getf("mcp.tools.summary", len(tools), page+1, totalPages, start+1, end, allowed, blocked))
	}

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label:    L.Get("mcp.tools.btn.scan"),
				Style:    discordgo.SecondaryButton,
				CustomID: mcpComponentID("scan", channelID, mcpServerToken(server)),
			},
			discordgo.Button{
				Label:    L.Get("mcp.tools.btn.back"),
				Style:    discordgo.SecondaryButton,
				CustomID: mcpComponentID("back", channelID),
			},
		}},
	}
	if len(tools) > mcpMaxOptions {
		totalPages := mcpToolPageCount(len(tools))
		if page < 0 {
			page = 0
		}
		if page >= totalPages {
			page = totalPages - 1
		}
		components = append(components, discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label:    L.Get("mcp.tools.btn.prev"),
				Style:    discordgo.SecondaryButton,
				Disabled: page <= 0,
				CustomID: mcpComponentID("toolpage", channelID, mcpServerToken(server), formatMCPPage(page-1)),
			},
			discordgo.Button{
				Label:    L.Get("mcp.tools.btn.next"),
				Style:    discordgo.SecondaryButton,
				Disabled: page >= totalPages-1,
				CustomID: mcpComponentID("toolpage", channelID, mcpServerToken(server), formatMCPPage(page+1)),
			},
		}})
	}
	pageTools := tools
	if len(tools) > 0 {
		start, end := mcpToolPageBounds(len(tools), page)
		pageTools = tools[start:end]
	}
	allowOptions, removeOptions := mcpToolSelectOptions(pageTools)
	if len(allowOptions) > 0 {
		components = append(components, discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.SelectMenu{
				MenuType:    discordgo.StringSelectMenu,
				CustomID:    mcpComponentID("tooladd", channelID, mcpServerToken(server), formatMCPPage(page)),
				Placeholder: L.Get("mcp.tools.select_allow"),
				MaxValues:   1,
				Options:     allowOptions,
			},
		}})
	}
	if len(removeOptions) > 0 {
		components = append(components, discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.SelectMenu{
				MenuType:    discordgo.StringSelectMenu,
				CustomID:    mcpComponentID("toolremove", channelID, mcpServerToken(server), formatMCPPage(page)),
				Placeholder: L.Get("mcp.tools.select_remove"),
				MaxValues:   1,
				Options:     removeOptions,
			},
		}})
	}
	return sb.String(), components
}

func mcpToolSelectOptions(tools []channel.MCPToolView) ([]discordgo.SelectMenuOption, []discordgo.SelectMenuOption) {
	var allowOptions []discordgo.SelectMenuOption
	var removeOptions []discordgo.SelectMenuOption
	for _, tool := range tools {
		opt := discordgo.SelectMenuOption{
			Label:       truncateDiscordComponentText(tool.Name, 100),
			Value:       mcpToolToken(tool.Name),
			Description: truncateDiscordComponentText(tool.Description, 100),
		}
		if tool.Allowed {
			opt.Emoji = &discordgo.ComponentEmoji{Name: "🟢"}
			if len(removeOptions) < mcpMaxOptions {
				removeOptions = append(removeOptions, opt)
			}
		} else {
			opt.Emoji = &discordgo.ComponentEmoji{Name: "⚪"}
			if len(allowOptions) < mcpMaxOptions {
				allowOptions = append(allowOptions, opt)
			}
		}
	}
	return allowOptions, removeOptions
}

func mcpToolCounts(tools []channel.MCPToolView) (int, int) {
	allowed := 0
	for _, tool := range tools {
		if tool.Allowed {
			allowed++
		}
	}
	return allowed, len(tools) - allowed
}

func mcpToolPageCount(total int) int {
	if total <= 0 {
		return 1
	}
	return (total + mcpMaxOptions - 1) / mcpMaxOptions
}

func mcpToolPageBounds(total, page int) (int, int) {
	pages := mcpToolPageCount(total)
	if page < 0 {
		page = 0
	}
	if page >= pages {
		page = pages - 1
	}
	start := page * mcpMaxOptions
	if start > total {
		start = total
	}
	end := start + mcpMaxOptions
	if end > total {
		end = total
	}
	return start, end
}

func formatMCPPage(page int) string {
	if page < 0 {
		page = 0
	}
	return strconv.Itoa(page)
}

func parseMCPPage(raw string) int {
	page, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || page < 0 {
		return 0
	}
	return page
}

func formatMCPServerPanel(view channel.MCPServerView) string {
	p := view.Policy
	state := L.Get("mcp.status.state.disabled")
	if p.Enabled {
		state = L.Get("mcp.status.state.enabled")
	}
	preset := p.Preset
	if preset == "" {
		preset = L.Get("mcp.status.preset_custom")
	}
	tools := L.Get("mcp.status.tools.none")
	if p.AllowAllTools {
		tools = L.Get("mcp.status.tools.all")
	} else if items := p.EffectiveTools(); len(items) > 0 {
		tools = strings.Join(items, ", ")
	}
	return L.Getf("mcp.manage.server_detail", view.Name, state, preset, localeBool(p.AllowAllTools), tools)
}

func mcpSelectOptions(views []channel.MCPServerView, selected string) []discordgo.SelectMenuOption {
	limit := len(views)
	if limit > mcpMaxOptions {
		limit = mcpMaxOptions
	}
	options := make([]discordgo.SelectMenuOption, 0, limit)
	for _, view := range views[:limit] {
		desc := L.Get("mcp.status.state.disabled")
		if view.Policy.Enabled {
			preset := view.Policy.Preset
			if preset == "" {
				preset = L.Get("mcp.status.preset_custom")
			}
			desc = L.Getf("mcp.manage.option_enabled", preset)
		}
		options = append(options, discordgo.SelectMenuOption{
			Label:       truncateDiscordComponentText(view.Name, 100),
			Value:       mcpServerToken(view.Name),
			Description: truncateDiscordComponentText(desc, 100),
			Default:     view.Name == selected,
		})
	}
	return options
}

func mcpActionButtons(channelID string, view channel.MCPServerView) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{
		discordgo.Button{
			Label:    L.Get("mcp.manage.btn.full"),
			Style:    discordgo.SuccessButton,
			CustomID: mcpComponentID("apply", channelID, mcpServerToken(view.Name), "full"),
		},
		discordgo.Button{
			Label:    L.Get("mcp.manage.btn.disable"),
			Style:    discordgo.DangerButton,
			CustomID: mcpComponentID("apply", channelID, mcpServerToken(view.Name), "disable"),
		},
	}
}

func mcpComponentID(parts ...string) string {
	out := make([]string, 0, len(parts)+1)
	out = append(out, mcpCustomPrefix)
	out = append(out, parts...)
	return strings.Join(out, ":")
}

func parseMCPComponentID(raw string) []string {
	parts := strings.Split(raw, ":")
	for i := 1; i < len(parts); i++ {
		if decoded, err := url.QueryUnescape(parts[i]); err == nil {
			parts[i] = decoded
		}
	}
	return parts
}

func mcpServerToken(name string) string {
	return mcpServerTokenID + shortMCPToken(name)
}

func mcpToolToken(name string) string {
	return mcpToolTokenID + shortMCPToken(name)
}

func shortMCPToken(name string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(name)))
	return hex.EncodeToString(sum[:8])
}

func (b *Bot) resolveMCPServerToken(channelID, token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	views, err := b.manager.MCPServerViews(channelID)
	if err != nil {
		return ""
	}
	for _, view := range views {
		if token == mcpServerToken(view.Name) || token == view.Name {
			return view.Name
		}
	}
	return ""
}

func (b *Bot) resolveMCPToolToken(channelID, server, token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	tools, err := b.manager.MCPToolViews(channelID, server)
	if err != nil {
		return ""
	}
	for _, tool := range tools {
		if token == mcpToolToken(tool.Name) || token == tool.Name {
			return tool.Name
		}
	}
	return ""
}

func truncateDiscordComponentText(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return string(r)
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}

func respondInteractionEphemeral(ds *discordgo.Session, i *discordgo.InteractionCreate, msg string) {
	_ = ds.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: msg,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}
