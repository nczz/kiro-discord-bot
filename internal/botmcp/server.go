package botmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/nczz/kiro-discord-bot/audit"
	"github.com/nczz/kiro-discord-bot/heartbeat"
	"github.com/nczz/kiro-discord-bot/internal/botegress"
	"github.com/nczz/kiro-discord-bot/internal/channelmeta"
	"github.com/nczz/kiro-discord-bot/internal/cronpolicy"
	"github.com/nczz/kiro-discord-bot/internal/secrets"
	"github.com/nczz/kiro-discord-bot/internal/timectx"
	"github.com/robfig/cron/v3"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Run starts the built-in bot tools MCP server over stdio.
func Run() error {
	return server.ServeStdio(NewServer(), server.WithErrorLogger(log.New(os.Stderr, "[mcp-bot] ", log.LstdFlags)))
}

const (
	ToolDataSummary         = "bot_data_summary"
	ToolListChannelData     = "bot_list_channel_data"
	ToolCurrentTime         = "bot_current_time"
	ToolResolveDateRange    = "bot_resolve_date_range"
	ToolSendMessage         = "bot_send_message"
	ToolSendFile            = "bot_send_file"
	ToolSendImageURL        = "bot_send_image_url"
	ToolQueryChannelHistory = "bot_query_channel_history"
	ToolMemoryList          = "bot_memory_list"
	ToolMemoryAdd           = "bot_memory_add"
	ToolMemoryRemove        = "bot_memory_remove"
	ToolMemoryClear         = "bot_memory_clear"
	ToolCreateCron          = "bot_create_cron"
	ToolUpdateCron          = "bot_update_cron"
	ToolCreateReminder      = "bot_create_reminder"
	ToolListCron            = "bot_list_cron"
	ToolDeleteCron          = "bot_delete_cron"
	ToolQueryAudit          = "bot_query_audit"
)

// DefaultSafeToolNames returns the bot-tools allowlist enabled during first channel setup.
// New tools must opt into this list deliberately; being non-destructive is not enough.
func DefaultSafeToolNames() []string {
	return []string{
		ToolDataSummary,
		ToolListChannelData,
		ToolCurrentTime,
		ToolResolveDateRange,
		ToolListCron,
		ToolSendFile,
		ToolSendImageURL,
		ToolQueryChannelHistory,
		ToolMemoryList,
		ToolMemoryAdd,
		ToolA2APeers,
		ToolA2APolicyGet,
		ToolA2ATaskStatus,
		ToolA2ARuntimePreflight,
		ToolA2APolicyPlan,
		ToolA2ATrustPeer,
		ToolA2ADelegate,
		ToolA2APolicyApply,
		ToolA2ACancel,
		ToolA2AInputReply,
		ToolA2AAuthReply,
		ToolCreateCron,
		ToolUpdateCron,
		ToolCreateReminder,
	}
}

// AuditPromptToolNames returns the private, temporary bot-tools allowlist used
// only for manager-authorized /audit prompt investigations.
func AuditPromptToolNames() []string {
	return []string{ToolQueryAudit}
}

// NewServer builds the built-in bot tools MCP server.
func NewServer() *server.MCPServer {
	s := server.NewMCPServer("bot-tools", "1.0.0", server.WithToolCapabilities(false))
	cronTZ := cronpolicy.TimezoneName(os.Getenv("CRON_TIMEZONE"))
	s.AddTool(
		readOnlyTool(ToolDataSummary, "Summarize bot state availability without returning host paths or message content"),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			summary, err := dataSummary(dataDir())
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			raw, _ := json.MarshalIndent(summary, "", "  ")
			return mcp.NewToolResultText(string(raw)), nil
		},
	)
	s.AddTool(
		readOnlyTool(ToolListChannelData, "List channel metadata and state-file presence without returning host paths or message content"),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			rows, err := listChannelData(dataDir())
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			raw, _ := json.MarshalIndent(rows, "", "  ")
			return mcp.NewToolResultText(string(raw)), nil
		},
	)
	s.AddTool(
		currentTimeTool(cronTZ),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			current, err := timectx.Current(time.Now(), os.Getenv("CRON_TIMEZONE"))
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(timectx.JSON(current)), nil
		},
	)
	s.AddTool(
		resolveDateRangeTool(cronTZ),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			includeTodaySet := false
			includeToday := false
			if raw, ok := req.GetArguments()["include_today"].(bool); ok {
				includeToday = raw
				includeTodaySet = true
			}
			rangeReq := timectx.RangeRequest{
				ReferenceTime:   req.GetString("reference_time", ""),
				RangeType:       req.GetString("range_type", ""),
				Offset:          int(req.GetFloat("offset", 0)),
				WeekIndex:       int(req.GetFloat("week_index", 0)),
				Weekday:         req.GetString("weekday", ""),
				WeekStart:       req.GetString("week_start", ""),
				MonthWeekPolicy: req.GetString("month_week_policy", ""),
				Date:            req.GetString("date", ""),
				Days:            int(req.GetFloat("days", 0)),
				Direction:       req.GetString("direction", ""),
			}
			if includeTodaySet {
				rangeReq.IncludeToday = &includeToday
			}
			result, err := timectx.ResolveDateRange(rangeReq, time.Now(), os.Getenv("CRON_TIMEZONE"))
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(timectx.JSON(result)), nil
		},
	)
	s.AddTool(
		memoryListTool(),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			channelID, err := memoryOwnerChannelID(req.GetString("channel_id", ""))
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			entries, err := readMemoryEntries(dataDir(), channelID)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			result := map[string]any{"channel_id": channelID, "entries": entries}
			raw, _ := json.MarshalIndent(result, "", "  ")
			return mcp.NewToolResultText(string(raw)), nil
		},
	)
	s.AddTool(
		memoryWriteTool(ToolMemoryAdd, "Persist a Discord-channel memory rule only when the user explicitly asks the bot to remember a channel preference or behavior rule for future turns. This is not a knowledge-base update tool: do not use it for 知識庫, knowledge base, KB, project knowledge, document corpus, searchable index, update-docs, or add-to-corpus requests. If the user asks to update a knowledge base, use a knowledge-base-specific workflow/tool when available or report that this bot-tools server cannot write that knowledge base. Do not infer durable memory from ordinary conversation. Reject secrets, credentials, private tokens, or policy-bypass instructions. Summarize the memory as one durable behavioral rule. The write is queued for the main bot and must be audit-recorded before it is accepted.", false),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if !remoteA2AMemoryWriteAllowed() {
				return mcp.NewToolResultError("Memory write is disabled for remote A2A tasks."), nil
			}
			channelID, err := memoryOwnerChannelID(req.GetString("channel_id", ""))
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			entry, err := safeMemoryEntry(req.GetString("entry", ""))
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if err := validateMemoryPurpose(entry, req.GetString("reason", "")); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			action := botegress.Action{
				Action:      botegress.ActionMemoryAdd,
				ChannelID:   channelID,
				MemoryEntry: entry,
				RequestedBy: req.GetString("requested_by", ""),
				Reason:      req.GetString("reason", ""),
			}
			id, err := queueAuditedMemoryAction(action)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Memory add queued for audited bot-side application (%s).", id)), nil
		},
	)
	s.AddTool(
		memoryWriteTool(ToolMemoryRemove, "Remove one persistent channel memory rule only when the user explicitly asks to forget a specific listed memory entry. This is not default-enabled because it changes durable context; use bot_memory_list first and pass the one-based memory_index. The queued removal must be audit-recorded before it is accepted.", true),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if !remoteA2AMemoryWriteAllowed() {
				return mcp.NewToolResultError("Memory write is disabled for remote A2A tasks."), nil
			}
			channelID, err := memoryOwnerChannelID(req.GetString("channel_id", ""))
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			index := req.GetInt("memory_index", 0)
			action := botegress.Action{
				Action:      botegress.ActionMemoryRemove,
				ChannelID:   channelID,
				MemoryIndex: index,
				RequestedBy: req.GetString("requested_by", ""),
				Reason:      req.GetString("reason", ""),
			}
			id, err := queueAuditedMemoryAction(action)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Memory removal queued for audited bot-side application (%s).", id)), nil
		},
	)
	s.AddTool(
		memoryWriteTool(ToolMemoryClear, "Clear all persistent channel memory only when a channel manager explicitly asks to remove every memory entry. This is destructive and not default-enabled. The queued clear must be audit-recorded before it is accepted.", true),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if !remoteA2AMemoryWriteAllowed() {
				return mcp.NewToolResultError("Memory write is disabled for remote A2A tasks."), nil
			}
			channelID, err := memoryOwnerChannelID(req.GetString("channel_id", ""))
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			action := botegress.Action{
				Action:      botegress.ActionMemoryClear,
				ChannelID:   channelID,
				RequestedBy: req.GetString("requested_by", ""),
				Reason:      req.GetString("reason", ""),
			}
			id, err := queueAuditedMemoryAction(action)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Memory clear queued for audited bot-side application (%s).", id)), nil
		},
	)
	s.AddTool(
		writeTool(ToolSendMessage, "Send a separate Discord message through the bot-controlled safe egress queue. This tool is not part of the default channel allowlist. Do not use it for ordinary replies or final answers; normal assistant text is already delivered, split, and displayed by the bot. Use this only when a channel manager explicitly enabled it and the user explicitly asks to send an extra Discord message, notify another target, hand off to another bot, or perform scheduled/cron egress. The main bot redacts secrets before delivery.", false),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if botToolsEgressDisabled() {
				return mcp.NewToolResultError("Discord egress is disabled for this private audit job."), nil
			}
			channelID, _ := req.RequireString("channel_id")
			if err := validateBoundChannel(channelID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			content, _ := req.RequireString("content")
			id, err := botegress.WritePending(dataDir(), botegress.Action{
				Action:    botegress.ActionSendMessage,
				ChannelID: deliveryChannelID(channelID),
				Content:   content,
			})
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Message queued for safe Discord delivery (%s).", id)), nil
		},
	)
	s.AddTool(
		writeTool(ToolSendFile, "Send a bot-local file through the bot-controlled safe egress queue. The file_path must be readable by the kiro-discord-bot process on this host/VM; do not pass paths from another MCP server, Docker container, browser profile namespace, remote host, or any tool-returned artifact namespace unless they are explicitly mounted into the bot filesystem. If another tool returns an HTTP(S) image URL, pass that URL directly to bot_send_image_url instead of saving, downloading, transcribing, base64-encoding, or converting it into a local artifact path. Text files are redacted and uploaded as sanitized copies. JPEG/PNG images are validated and uploaded as copied temp files without OCR redaction or metadata stripping. Documents with extractable readable text (PDF, DOCX, XLSX) are converted to text, redacted, and uploaded as sanitized .txt copies; original binary documents are never uploaded back.", false),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if botToolsEgressDisabled() {
				return mcp.NewToolResultError("File egress is disabled for this private audit job."), nil
			}
			channelID, _ := req.RequireString("channel_id")
			if err := validateBoundChannel(channelID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			filePath, _ := req.RequireString("file_path")
			if err := validateBotSendFilePath(filePath); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			content, _ := req.RequireString("content")
			id, err := botegress.WritePending(dataDir(), botegress.Action{
				Action:    botegress.ActionSendFile,
				ChannelID: deliveryChannelID(channelID),
				FilePath:  filePath,
				Content:   content,
			})
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("File queued for safe Discord delivery (%s).", id)), nil
		},
	)
	s.AddTool(
		writeTool(ToolSendImageURL, "Send a JPEG/PNG image from a non-secret HTTP(S) URL through the bot-controlled safe egress queue. Use this whenever another tool returns an image URL; do not download, transcribe, or base64-encode the image in the agent. The bot fetches the URL server-side, rejects URL credentials, validates the fetched bytes, and does not require the URL path to include an image filename.", false),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if botToolsEgressDisabled() {
				return mcp.NewToolResultError("Image URL egress is disabled for this private audit job."), nil
			}
			channelID, _ := req.RequireString("channel_id")
			if err := validateBoundChannel(channelID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			imageURL, _ := req.RequireString("url")
			filename, _ := req.RequireString("filename")
			filePath, err := fetchValidatedImageURL(ctx, imageURL, filename)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			id, err := botegress.WritePending(dataDir(), botegress.Action{
				Action:              botegress.ActionSendFile,
				ChannelID:           deliveryChannelID(channelID),
				FilePath:            filePath,
				Content:             req.GetString("content", ""),
				RemoveFileAfterSend: true,
			})
			if err != nil {
				_ = os.Remove(filePath)
				_ = os.Remove(filepath.Dir(filePath))
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Image queued for safe Discord delivery (%s).", id)), nil
		},
	)
	s.AddTool(
		queryChannelHistoryTool(),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			query := strings.TrimSpace(req.GetString("query", ""))
			targetID, err := auditToolTargetID(req.GetString("target_id", ""))
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			limit := req.GetInt("limit", 20)
			if limit <= 0 {
				limit = 20
			}
			if limit > 50 {
				limit = 50
			}
			offset := req.GetInt("offset", 0)
			if offset < 0 {
				offset = 0
			}
			rows, err := audit.QueryTimelineReadOnly(audit.AuditDBPath(dataDir()), audit.TimelineQueryOptions{
				GuildID:        strings.TrimSpace(os.Getenv("BOT_TOOLS_GUILD_ID")),
				TargetID:       targetID,
				Limit:          limit + 1,
				Offset:         offset,
				Contains:       query,
				IncludeContent: true,
				SearchContent:  true,
			})
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			hasMore := len(rows) > limit
			if hasMore {
				rows = rows[:limit]
			}
			page := channelHistoryPage(query, targetID, limit, offset, hasMore, rows)
			if len(rows) == 0 {
				page.Message = "No stored channel history results. Content search requires audit content retention to be enabled."
			}
			raw, _ := json.MarshalIndent(page, "", "  ")
			return mcp.NewToolResultText(string(raw)), nil
		},
	)
	s.AddTool(
		writeTool(ToolCreateCron, cronpolicy.CreateToolDescription(cronTZ), false),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			name, _ := req.RequireString("name")
			schedule, _ := req.RequireString("schedule")
			prompt, _ := req.RequireString("prompt")
			channelID, _ := req.RequireString("channel_id")
			ownerChannelID, err := cronOwnerChannelID(channelID)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			guildID, _ := req.RequireString("guild_id")
			createdBy, _ := req.RequireString("created_by")
			createdByID := strings.TrimSpace(req.GetString("created_by_id", ""))
			action := pendingAction{
				Action: "create",
				Job: &pendingJob{
					Name:        strings.TrimSpace(name),
					Schedule:    strings.TrimSpace(schedule),
					Prompt:      strings.TrimSpace(prompt),
					ChannelID:   ownerChannelID,
					GuildID:     strings.TrimSpace(guildID),
					CreatedBy:   strings.TrimSpace(createdBy),
					CreatedByID: createdByID,
				},
			}
			if err := validatePendingAction(action); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if err := writePending(dataDir(), action); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Scheduled task %q created (schedule: %s). It will activate within 60 seconds.", strings.TrimSpace(name), strings.TrimSpace(schedule))), nil
		},
	)
	s.AddTool(
		writeTool(ToolCreateReminder, cronpolicy.ReminderToolDescription(cronTZ), false),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			name := strings.TrimSpace(req.GetString("name", ""))
			timeInput, _ := req.RequireString("time")
			content, _ := req.RequireString("content")
			channelID, _ := req.RequireString("channel_id")
			if err := validateBoundChannel(channelID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			targetChannelID := deliveryChannelID(channelID)
			guildID, _ := req.RequireString("guild_id")
			createdBy, _ := req.RequireString("created_by")
			createdByID := strings.TrimSpace(req.GetString("created_by_id", ""))
			mentionUserID := strings.TrimSpace(req.GetString("mention_user_id", ""))
			if err := validateMentionUserID(mentionUserID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			target, err := parseReminderTime(timeInput, cronTZ)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			content = strings.TrimSpace(content)
			if name == "" {
				name = reminderName(content)
			}
			action := pendingAction{
				Action: "create_reminder",
				Job: &pendingJob{
					Name:          name,
					ScheduleHuman: strings.TrimSpace(timeInput),
					Prompt:        content,
					ChannelID:     targetChannelID,
					GuildID:       strings.TrimSpace(guildID),
					CreatedBy:     strings.TrimSpace(createdBy),
					CreatedByID:   createdByID,
					NextRun:       target.Format(time.RFC3339),
					MentionID:     mentionUserID,
					OneShot:       true,
				},
			}
			if err := validatePendingAction(action); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if err := writePending(dataDir(), action); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("One-time reminder %q created for %s. It will activate within 60 seconds.", name, target.In(cronLocation(cronTZ)).Format("2006/01/02 15:04"))), nil
		},
	)
	s.AddTool(
		writeTool(ToolUpdateCron, cronpolicy.UpdateToolDescription(cronTZ), false),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			jobID, _ := req.RequireString("job_id")
			channelID, _ := req.RequireString("channel_id")
			ownerChannelID, err := cronOwnerChannelID(channelID)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			update := &heartbeat.CronUpdate{}
			if value, ok := req.GetArguments()["name"]; ok {
				v, ok := value.(string)
				if !ok {
					return mcp.NewToolResultError("name must be a string"), nil
				}
				update.Name = &v
			}
			if value, ok := req.GetArguments()["schedule"]; ok {
				v, ok := value.(string)
				if !ok {
					return mcp.NewToolResultError("schedule must be a string"), nil
				}
				update.Schedule = &v
			}
			if value, ok := req.GetArguments()["prompt"]; ok {
				v, ok := value.(string)
				if !ok {
					return mcp.NewToolResultError("prompt must be a string"), nil
				}
				update.Prompt = &v
			}
			if value, ok := req.GetArguments()["enabled"]; ok {
				v, ok := value.(bool)
				if !ok {
					return mcp.NewToolResultError("enabled must be a boolean"), nil
				}
				update.Enabled = &v
			}
			action := pendingAction{Action: "update", JobID: strings.TrimSpace(jobID), ChannelID: ownerChannelID, Update: update}
			if err := validatePendingAction(action); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if err := writePending(dataDir(), action); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Cron job %q queued for update. The update will apply within 60 seconds.", strings.TrimSpace(jobID))), nil
		},
	)
	s.AddTool(
		mcp.NewTool(ToolListCron,
			mcp.WithDescription("List scheduled cron jobs for a channel"),
			mcp.WithString("channel_id", mcp.Required(), mcp.Description("Discord channel ID from context; thread IDs are normalized to the owning parent channel when bot-tools is bound to a channel")),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithOpenWorldHintAnnotation(false),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			channelID, _ := req.RequireString("channel_id")
			ownerChannelID, err := cronOwnerChannelID(channelID)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			jobs, err := listCronJobs(dataDir(), ownerChannelID)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			raw, _ := json.MarshalIndent(jobs, "", "  ")
			return mcp.NewToolResultText(string(raw)), nil
		},
	)
	s.AddTool(
		writeTool(ToolDeleteCron, "Delete a scheduled cron job by ID", true),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			jobID, _ := req.RequireString("job_id")
			channelID, _ := req.RequireString("channel_id")
			ownerChannelID, err := cronOwnerChannelID(channelID)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			action := pendingAction{
				Action:    "delete",
				JobID:     strings.TrimSpace(jobID),
				ChannelID: ownerChannelID,
			}
			if err := validatePendingAction(action); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if err := writePending(dataDir(), action); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Cron job %q scheduled for deletion. It will be removed within 60 seconds.", strings.TrimSpace(jobID))), nil
		},
	)
	s.AddTool(
		mcp.NewTool(ToolQueryAudit,
			mcp.WithDescription("Query scoped Discord audit timeline rows for the current bot-tools channel/thread context. "+audit.SchemaDescription()),
			mcp.WithString("target_id", mcp.Description("Optional target channel/thread ID. Defaults to the current bot-tools target and must match the bound channel or thread target.")),
			mcp.WithNumber("limit", mcp.Description("Maximum rows to return, 1-100. Defaults to 50.")),
			mcp.WithString("contains", mcp.Description("Optional substring filter across event metadata such as type, target, message ID, user, command, status, and error. Message content is not searched.")),
			mcp.WithString("event_type", mcp.Description("Optional exact event type filter, such as message_create or agent_tool_call.")),
			mcp.WithBoolean("include_content", mcp.Description("When true, include stored content fields and deleted-message content snippets if audit content retention is enabled. Use only when the manager's audit question requires message content.")),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithOpenWorldHintAnnotation(false),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			targetID, err := auditToolTargetID(req.GetString("target_id", ""))
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			dbPath := audit.AuditDBPath(dataDir())
			rows, err := audit.QueryTimelineReadOnly(dbPath, audit.TimelineQueryOptions{
				GuildID:        strings.TrimSpace(os.Getenv("BOT_TOOLS_GUILD_ID")),
				TargetID:       targetID,
				Limit:          req.GetInt("limit", 50),
				Contains:       req.GetString("contains", ""),
				EventType:      req.GetString("event_type", ""),
				IncludeContent: req.GetBool("include_content", false),
			})
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if len(rows) == 0 {
				return mcp.NewToolResultText("No results."), nil
			}
			raw, _ := json.MarshalIndent(rows, "", "  ")
			return mcp.NewToolResultText(string(raw)), nil
		},
	)
	registerA2ATools(s)
	return s
}

func writeTool(name, description string, destructive bool) mcp.Tool {
	t := mcp.NewTool(name,
		mcp.WithDescription(description),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(destructive),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
	)

	if name == ToolUpdateCron {
		mcp.WithIdempotentHintAnnotation(true)(&t)
	}
	switch name {
	case ToolSendMessage:
		for _, opt := range []mcp.ToolOption{
			mcp.WithString("channel_id", mcp.Required(), mcp.Description("Discord channel ID from context")),
			mcp.WithString("content", mcp.Required(), mcp.Description("Separate Discord message content to deliver after bot-side redaction; not for ordinary final replies")),
		} {
			opt(&t)
		}
	case ToolSendFile:
		for _, opt := range []mcp.ToolOption{
			mcp.WithString("channel_id", mcp.Required(), mcp.Description("Discord channel ID from context")),
			mcp.WithString("file_path", mcp.Required(), mcp.Description("Path to a file readable by the kiro-discord-bot process on this host/VM. Do not pass paths from another MCP server, Docker container, browser profile namespace, remote host, or any tool-returned artifact namespace unless explicitly mounted into the bot filesystem. If an HTTP(S) image URL is available, pass it directly to bot_send_image_url instead of using this local-file tool. Text files stay text; JPEG/PNG images are validated and uploaded as copied temp files; PDF, DOCX, and XLSX with extractable readable text are extracted to redacted .txt copies.")),
			mcp.WithString("content", mcp.Description("Optional message content to send with the sanitized file")),
		} {
			opt(&t)
		}
	case ToolSendImageURL:
		for _, opt := range []mcp.ToolOption{
			mcp.WithString("channel_id", mcp.Required(), mcp.Description("Discord channel ID from context")),
			mcp.WithString("url", mcp.Required(), mcp.Description("Non-secret HTTP(S) image URL to fetch server-side. Pass image URLs directly; do not copy base64 image data into this field. The path does not need to contain an image filename; use filename for the Discord display name.")),
			mcp.WithString("filename", mcp.Required(), mcp.Description("Display filename for the Discord attachment, for example screenshot.jpg.")),
			mcp.WithString("content", mcp.Description("Optional message content to send with the image")),
			mcp.WithOpenWorldHintAnnotation(true),
		} {
			opt(&t)
		}
	case ToolCreateCron:
		cronTZ := cronpolicy.TimezoneName(os.Getenv("CRON_TIMEZONE"))
		for _, opt := range []mcp.ToolOption{
			mcp.WithString("name", mcp.Required(), mcp.Description("Short name for the scheduled task")),
			mcp.WithString("schedule", mcp.Required(), mcp.Description(cronpolicy.ScheduleFieldDescription(cronTZ))),
			mcp.WithString("prompt", mcp.Required(), mcp.Description("The task prompt that the agent will execute on each run")),
			mcp.WithString("channel_id", mcp.Required(), mcp.Description("Discord channel ID from context; thread IDs are normalized to the owning parent channel when bot-tools is bound to a channel")),
			mcp.WithString("guild_id", mcp.Required(), mcp.Description("Discord guild ID from context")),
			mcp.WithString("created_by", mcp.Description("Username of the requester")),

			mcp.WithString("created_by_id", mcp.Description("Optional Discord user ID of the requester when available in context")),
		} {
			opt(&t)
		}
	case ToolCreateReminder:
		cronTZ := cronpolicy.TimezoneName(os.Getenv("CRON_TIMEZONE"))
		for _, opt := range []mcp.ToolOption{
			mcp.WithString("name", mcp.Description("Optional short reminder name. Defaults to a reminder prefix plus the content.")),
			mcp.WithString("time", mcp.Required(), mcp.Description(cronpolicy.ReminderTimeFieldDescription(cronTZ))),
			mcp.WithString("content", mcp.Required(), mcp.Description("Reminder message content delivered once by the bot scheduler")),
			mcp.WithString("channel_id", mcp.Required(), mcp.Description("Discord channel or thread ID from context. In a thread, the current bot-tools delivery target is used so the reminder is delivered to that thread.")),
			mcp.WithString("guild_id", mcp.Required(), mcp.Description("Discord guild ID from context")),
			mcp.WithString("created_by", mcp.Description("Username of the requester")),
			mcp.WithString("created_by_id", mcp.Description("Optional Discord user ID of the requester when available in context")),
			mcp.WithString("mention_user_id", mcp.Description("Optional verified Discord user ID to mention when the reminder fires. Use only a user ID that appears in the Discord mention references or bot peers provided in context.")),
		} {
			opt(&t)
		}
	case ToolUpdateCron:
		cronTZ := cronpolicy.TimezoneName(os.Getenv("CRON_TIMEZONE"))
		for _, opt := range []mcp.ToolOption{
			mcp.WithString("job_id", mcp.Required(), mcp.Description("Existing recurring cron job ID returned by bot_list_cron")),
			mcp.WithString("channel_id", mcp.Required(), mcp.Description("Owning Discord parent channel ID from context; the job must belong to this channel")),
			mcp.WithString("name", mcp.Description("Optional non-empty replacement name. Omit to keep the current name.")),
			mcp.WithString("schedule", mcp.Description(cronpolicy.ScheduleFieldDescription(cronTZ)+" Omit to keep the current schedule.")),
			mcp.WithString("prompt", mcp.Description("Optional non-empty replacement task prompt. Omit to keep the current prompt.")),
			mcp.WithBoolean("enabled", mcp.Description("Set false to disable without deleting; set true to resume. Omit to keep the current state.")),
		} {
			opt(&t)
		}
	case ToolDeleteCron:
		for _, opt := range []mcp.ToolOption{
			mcp.WithString("job_id", mcp.Required(), mcp.Description("The cron job ID to delete")),
			mcp.WithString("channel_id", mcp.Required(), mcp.Description("Discord channel ID from context; thread IDs are normalized to the owning parent channel when bot-tools is bound to a channel")),
		} {
			opt(&t)
		}
	}
	return t
}

func memoryListTool() mcp.Tool {
	return mcp.NewTool(ToolMemoryList,
		mcp.WithDescription("List persistent channel memory rules for the current bot-tools parent channel. Use this before adding duplicate memories or before any requested removal. Read-only and scoped to the bound Discord channel."),
		mcp.WithString("channel_id", mcp.Required(), mcp.Description("Discord parent channel ID from context; thread IDs are normalized to the owning parent channel when bot-tools is bound to a channel.")),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
}

func memoryWriteTool(name, description string, destructive bool) mcp.Tool {
	opts := []mcp.ToolOption{
		mcp.WithDescription(description),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(destructive),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("channel_id", mcp.Required(), mcp.Description("Discord parent channel ID from context; thread IDs are normalized to the owning parent channel when bot-tools is bound to a channel.")),
		mcp.WithString("requested_by", mcp.Required(), mcp.Description("Requester identity from Discord context, preferably username plus user_id.")),
		mcp.WithString("reason", mcp.Required(), mcp.Description("Short explanation of the user's explicit memory-management request for audit review. If the request is to update 知識庫/knowledge base/KB/project knowledge/docs/index/corpus, do not call this tool.")),
	}
	switch name {
	case ToolMemoryAdd:
		opts = append(opts, mcp.WithString("entry", mcp.Required(), mcp.Description("One durable Discord-channel behavior/preference rule. This is not a knowledge-base entry; do not include secrets, credentials, private tokens, raw personal data, policy-bypass instructions, or document-corpus content.")))
	case ToolMemoryRemove:
		opts = append(opts, mcp.WithNumber("memory_index", mcp.Required(), mcp.Description("One-based index from bot_memory_list for the memory entry to remove.")))
	case ToolMemoryClear:
		// common fields only
	}
	return mcp.NewTool(name, opts...)
}

func readOnlyTool(name, description string) mcp.Tool {
	return mcp.NewTool(name,
		mcp.WithDescription(description),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
}

func currentTimeTool(cronTZ string) mcp.Tool {
	return mcp.NewTool(ToolCurrentTime,
		mcp.WithDescription("Return the bot current date/time in CRON_TIMEZONE, including exact time, weekday, zh-TW weekday, day period, today/yesterday/tomorrow, and current week range. Call this before answering current time, today, tomorrow, yesterday, weekday, morning/afternoon, or exact-time questions when the injected prompt time block is missing, stale, or the task needs a fresh timestamp. Do not infer current date/time from model memory."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
}

func resolveDateRangeTool(cronTZ string) mcp.Tool {
	return mcp.NewTool(ToolResolveDateRange,
		mcp.WithDescription("Resolve deterministic calendar ranges using CRON_TIMEZONE from structured MCP arguments. Call this for calculated date ranges, relative periods, month/week boundaries, specific weekdays, nth week of a month, quarters, years, recent/future N days, or schedule-sensitive date answers. Agents must translate the user's natural-language date phrase into structured fields before calling this tool; for ranges such as '下個月第二週', pass range_type=month_week, offset=1, week_index=2. Do not calculate weekdays, month boundaries, or relative ranges mentally when this tool is available."),
		mcp.WithString("reference_time", mcp.Description("Optional RFC3339 or YYYY-MM-DD reference time. Defaults to bot current time in CRON_TIMEZONE.")),
		mcp.WithString("range_type", mcp.Required(), mcp.Description("Structured range type: day, week, month, month_week, quarter, year, relative_days, specific_weekday.")),
		mcp.WithNumber("offset", mcp.Description("Optional period offset from the reference period: previous=-1, current=0, next=1. For range_type=month_week, offset applies to the month.")),
		mcp.WithNumber("week_index", mcp.Description("Required for range_type=month_week. The 1-based week number in the target month.")),
		mcp.WithString("weekday", mcp.Description("Required for range_type=specific_weekday. Accepts monday..sunday or common zh-TW forms such as 週一.")),
		mcp.WithString("week_start", mcp.Description("Optional week start day. Defaults to monday.")),
		mcp.WithString("month_week_policy", mcp.Description("Optional nth-week policy. Defaults to calendar_row_clipped_to_month. Alternatives: full_weeks_only, day_blocks_1_7.")),
		mcp.WithString("date", mcp.Description("Optional YYYY-MM-DD date for range_type=day or weekday lookup.")),
		mcp.WithNumber("days", mcp.Description("Required for range_type=relative_days.")),
		mcp.WithString("direction", mcp.Description("Optional for range_type=relative_days: past or future. Defaults to past.")),
		mcp.WithBoolean("include_today", mcp.Description("Optional for range_type=relative_days. Defaults to true.")),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func queryChannelHistoryTool() mcp.Tool {
	return mcp.NewTool(ToolQueryChannelHistory,
		mcp.WithDescription("Search stored Discord conversation history for the current bot-tools channel/thread context. Use this when users ask about prior discussion in this channel, this thread, here, or the current session. By default target_id is the current Discord target; pass the parent channel_id to search that channel including child threads, or pass thread_id to search only one thread. Query is optional: omit it for broad/exhaustive history review, or provide a keyword/phrase to filter stored message/bot response content and timeline metadata. Results are scoped to the bound channel/thread, read-only, and return compact content snippets only when audit content retention is enabled. Responses are paginated: if has_more is true, call this tool again with offset=next_offset and the same query/target_id/limit; for requests to review all matching history, continue until has_more is false before summarizing."),
		mcp.WithString("query", mcp.Description("Optional keyword or phrase to search in stored message/bot response content and timeline metadata. Omit for broad/exhaustive scoped history review.")),
		mcp.WithString("target_id", mcp.Description("Optional channel or thread ID from the current Discord context. Defaults to the current bot-tools target. Use channel_id to include child threads; use thread_id to narrow to one thread.")),
		mcp.WithNumber("limit", mcp.Description("Maximum rows to return, 1-50. Defaults to 20.")),
		mcp.WithNumber("offset", mcp.Description("Zero-based row offset for pagination. Use next_offset from the previous response to fetch the next page; keep the same query, target_id, and limit until has_more is false.")),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
}

type summary struct {
	SessionsFile           bool `json:"sessions_file"`
	ChannelDirs            int  `json:"channel_dirs"`
	CronStore              bool `json:"cron_store"`
	AuditDB                bool `json:"audit_db"`
	MCPPolicyDB            bool `json:"mcp_policy_db"`
	KiroAgentRuntimeDir    bool `json:"kiro_agent_runtime_dir"`
	LegacyKiroRuntimeDir   bool `json:"legacy_kiro_runtime_dir"`
	RuntimeMCPConfig       bool `json:"runtime_mcp_config"`
	RuntimeCLISettingsFile bool `json:"runtime_cli_settings_file"`
}

type channelData struct {
	ChannelID       string `json:"channel_id"`
	Name            string `json:"name,omitempty"`
	Type            string `json:"type,omitempty"`
	ParentChannelID string `json:"parent_channel_id,omitempty"`
	ParentName      string `json:"parent_name,omitempty"`
	ChatLog         bool   `json:"chat_log"`
	MemoryFile      bool   `json:"memory_file"`
}

type channelHistoryPageResult struct {
	Query      string                 `json:"query"`
	TargetID   string                 `json:"target_id"`
	Limit      int                    `json:"limit"`
	Offset     int                    `json:"offset"`
	Returned   int                    `json:"returned"`
	HasMore    bool                   `json:"has_more"`
	NextOffset int                    `json:"next_offset,omitempty"`
	Message    string                 `json:"message,omitempty"`
	Results    []channelHistoryResult `json:"results"`
}

func channelHistoryPage(query, targetID string, limit, offset int, hasMore bool, rows []audit.TimelineEvent) channelHistoryPageResult {
	results := channelHistoryResults(rows)
	page := channelHistoryPageResult{
		Query:    query,
		TargetID: targetID,
		Limit:    limit,
		Offset:   offset,
		Returned: len(results),
		HasMore:  hasMore,
		Results:  results,
	}
	if hasMore {
		page.NextOffset = offset + len(results)
	}
	return page
}

type channelHistoryResult struct {
	Kind                   string `json:"kind"`
	Type                   string `json:"type"`
	ChannelID              string `json:"channel_id,omitempty"`
	TargetID               string `json:"target_id,omitempty"`
	ThreadID               string `json:"thread_id,omitempty"`
	MessageID              string `json:"message_id,omitempty"`
	UserID                 string `json:"user_id,omitempty"`
	Command                string `json:"command,omitempty"`
	Status                 string `json:"status,omitempty"`
	RecordedAt             string `json:"recorded_at"`
	ContentSnippet         string `json:"content_snippet,omitempty"`
	OriginalAuthorID       string `json:"original_author_id,omitempty"`
	OriginalAuthorUsername string `json:"original_author_username,omitempty"`
	DeletionNote           string `json:"deletion_note,omitempty"`
	DeletedMessageCount    int    `json:"deleted_message_count,omitempty"`
}

func channelHistoryResults(rows []audit.TimelineEvent) []channelHistoryResult {
	out := make([]channelHistoryResult, 0, len(rows))
	for _, row := range rows {
		snippet := strings.TrimSpace(row.Content)
		if snippet == "" {
			snippet = row.ContentSnippet
		}
		out = append(out, channelHistoryResult{
			Kind:                   row.Kind,
			Type:                   row.Type,
			ChannelID:              row.ChannelID,
			TargetID:               row.TargetID,
			ThreadID:               row.ThreadID,
			MessageID:              row.MessageID,
			UserID:                 row.UserID,
			Command:                row.Command,
			Status:                 row.Status,
			RecordedAt:             row.RecordedAt,
			ContentSnippet:         compactHistorySnippet(snippet, 500),
			OriginalAuthorID:       row.OriginalAuthorID,
			OriginalAuthorUsername: row.OriginalAuthorUsername,
			DeletionNote:           row.DeletionNote,
			DeletedMessageCount:    row.DeletedMessageCount,
		})
	}
	return out
}

func compactHistorySnippet(s string, maxRunes int) string {
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	if s == "" || maxRunes <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	if maxRunes == 1 {
		return string(runes[:1])
	}
	return string(runes[:maxRunes-1]) + "…"
}

type targetState struct {
	TargetChannelID       string   `json:"target_channel_id"`
	DisableEgress         bool     `json:"disable_egress"`
	RemoteA2A             bool     `json:"remote_a2a"`
	AllowMemoryWrite      bool     `json:"allow_memory_write"`
	DelegationDepth       int      `json:"delegation_depth"`
	RequesterID           string   `json:"requester_id"`
	RequesterName         string   `json:"requester_name"`
	AllowedMentionUserIDs []string `json:"allowed_mention_user_ids"`
}

func dataDir() string {
	if dir := strings.TrimSpace(os.Getenv("DATA_DIR")); dir != "" {
		return dir
	}
	return "./data"
}

func memoryOwnerChannelID(requested string) (string, error) {
	if err := validateBoundChannel(requested); err != nil {
		return "", err
	}
	if bound := strings.TrimSpace(os.Getenv("BOT_TOOLS_CHANNEL_ID")); bound != "" {
		return bound, nil
	}
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return "", fmt.Errorf("channel_id is required")
	}
	return requested, nil
}

func readMemoryEntries(dataDir, channelID string) ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(dataDir, "ch-"+channelID, "memory.json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read memory: %w", err)
	}
	var entries []string
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("parse memory: %w", err)
	}
	return entries, nil
}

func safeMemoryEntry(raw string) (string, error) {
	entry := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if entry == "" {
		return "", fmt.Errorf("entry is required")
	}
	if len([]rune(entry)) > 1000 {
		return "", fmt.Errorf("entry exceeds 1000 characters")
	}
	lower := strings.ToLower(entry)
	for _, blocked := range []string{"api_key", "apikey", "token", "password", "secret", "credential", "authorization", "bearer "} {
		if strings.Contains(lower, blocked) {
			return "", fmt.Errorf("entry appears to contain secret-bearing text and was not queued")
		}
	}
	if redacted := secrets.FromEnv().Redact(entry); redacted != entry {
		return "", fmt.Errorf("entry appears to contain a known secret and was not queued")
	}
	return entry, nil
}

func validateMemoryPurpose(entry, reason string) error {
	if looksLikeKnowledgeBaseUpdateRequest(reason) || looksLikeKnowledgeBaseUpdateRequest(entry) {
		return fmt.Errorf("bot_memory_add stores Discord channel memory, not knowledge base updates; use a knowledge-base-specific workflow/tool when available, or tell the user this bot cannot write that knowledge base from Discord")
	}
	return nil
}

func looksLikeKnowledgeBaseUpdateRequest(text string) bool {
	norm := normalizeMemoryPurposeText(text)
	if norm == "" {
		return false
	}
	if containsAny(norm, []string{
		"add to corpus", "add to document corpus", "update docs", "update documents",
		"index docs", "index the docs", "index documents", "reindex docs", "reindex documents",
		"ingest docs", "ingest documents", "write docs", "write documents",
	}) {
		return true
	}
	if hasChineseKnowledgeBaseTarget(norm) {
		return containsAny(norm, []string{
			"更新", "新增", "加入", "加到", "寫入", "写入", "存到", "存入", "放到", "放入", "記錄到", "记录到", "錄入", "录入",
		})
	}
	return hasKnowledgeBaseTarget(norm) && hasEnglishKnowledgeBaseWriteVerb(norm)
}

func normalizeMemoryPurposeText(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	text = strings.NewReplacer(
		"knowledge-base", "knowledge base",
		"knowledge_base", "knowledge base",
		"add-to-corpus", "add to corpus",
		"update-docs", "update docs",
		"index-docs", "index docs",
	).Replace(text)
	return strings.Join(strings.Fields(text), " ")
}

func hasChineseKnowledgeBaseTarget(text string) bool {
	return containsAny(text, []string{"知識庫", "知识库", "文件索引", "知識索引", "知识索引"})
}

func hasKnowledgeBaseTarget(text string) bool {
	if hasChineseKnowledgeBaseTarget(text) || containsAny(text, []string{
		"knowledge base", "knowledgebase", "project knowledge", "document corpus", "searchable index", "docs", "documents", "corpus",
	}) {
		return true
	}
	for _, field := range strings.Fields(text) {
		if normalizePurposeField(field) == "kb" {
			return true
		}
	}
	return false
}

func hasEnglishKnowledgeBaseWriteVerb(text string) bool {
	for _, field := range strings.Fields(text) {
		switch normalizePurposeField(field) {
		case "update", "add", "write", "store", "save", "record", "ingest", "upload", "insert":
			return true
		}
	}
	return false
}

func normalizePurposeField(field string) string {
	return strings.Trim(field, " \t\r\n.,;:!?()[]{}'\"`")
}

func containsAny(text string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func queueAuditedMemoryAction(action botegress.Action) (string, error) {
	if !botToolsAuditEnabled() {
		return "", fmt.Errorf("audit logging must be enabled before bot memory tools can write durable memory")
	}
	action, err := botegress.PrepareAction(action)
	if err != nil {
		return "", err
	}
	if err := recordQueuedMemoryAudit(action); err != nil {
		return "", fmt.Errorf("record memory audit event: %w", err)
	}
	id, err := botegress.WritePending(dataDir(), action)
	if err != nil {
		return "", err
	}
	return id, nil
}

func recordQueuedMemoryAudit(action botegress.Action) error {
	recordContent := botToolsAuditRecordContent()
	store, err := audit.Open(audit.Config{DataDir: dataDir(), DBPath: botToolsAuditDBPath(), RecordContent: recordContent})
	if err != nil {
		return err
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	content := ""
	if recordContent {
		content = action.MemoryEntry
	}
	return store.RecordBotEvent(ctx, audit.BotEvent{
		Type:      "bot_memory_update_queued",
		GuildID:   strings.TrimSpace(os.Getenv("BOT_TOOLS_GUILD_ID")),
		ChannelID: action.ChannelID,
		TargetID:  action.ChannelID,
		Command:   action.Action,
		Source:    "bot_tools_mcp",
		Status:    "queued",
		Content:   content,
		Metadata: map[string]any{
			"action":       action.Action,
			"action_id":    action.ID,
			"entry_len":    len(action.MemoryEntry),
			"memory_index": action.MemoryIndex,
			"requested_by": action.RequestedBy,
			"reason":       action.Reason,
		},
	})
}

func botToolsAuditEnabled() bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("AUDIT_LOG_ENABLED")))
	return raw == "" || raw == "1" || raw == "true" || raw == "yes" || raw == "on"
}

func botToolsAuditRecordContent() bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("AUDIT_LOG_RECORD_CONTENT")))
	return raw == "" || raw == "1" || raw == "true" || raw == "yes" || raw == "on"
}

func botToolsAuditDBPath() string {
	if path := strings.TrimSpace(os.Getenv("AUDIT_LOG_DB")); path != "" {
		return path
	}
	return audit.AuditDBPath(dataDir())
}

func validateBoundChannel(channelID string) error {
	bound := strings.TrimSpace(os.Getenv("BOT_TOOLS_CHANNEL_ID"))
	target := strings.TrimSpace(os.Getenv("BOT_TOOLS_TARGET_CHANNEL_ID"))
	currentTarget := currentTargetStateChannelID()
	channelID = strings.TrimSpace(channelID)
	if bound == "" || channelID == bound || (target != "" && channelID == target) || (currentTarget != "" && channelID == currentTarget) {
		return nil
	}
	return fmt.Errorf("channel_id %s is not allowed for this bot-tools session", channelID)
}

func deliveryChannelID(requested string) string {
	if target := currentTargetStateChannelID(); target != "" {
		return target
	}
	if target := strings.TrimSpace(os.Getenv("BOT_TOOLS_TARGET_CHANNEL_ID")); target != "" {
		return target
	}
	return strings.TrimSpace(requested)
}

func cronOwnerChannelID(requested string) (string, error) {
	if err := validateBoundChannel(requested); err != nil {
		return "", err
	}
	if bound := strings.TrimSpace(os.Getenv("BOT_TOOLS_CHANNEL_ID")); bound != "" {
		return bound, nil
	}
	return strings.TrimSpace(requested), nil
}

func auditToolTargetID(requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		if target := currentTargetStateChannelID(); target != "" {
			return target, nil
		}
		if target := strings.TrimSpace(os.Getenv("BOT_TOOLS_TARGET_CHANNEL_ID")); target != "" {
			return target, nil
		}
		requested = strings.TrimSpace(os.Getenv("BOT_TOOLS_CHANNEL_ID"))
	}
	if requested == "" {
		return "", fmt.Errorf("target_id is required")
	}
	if err := validateBoundChannel(requested); err != nil {
		return "", err
	}
	return requested, nil
}

func currentTargetStateChannelID() string {
	state, ok := currentTargetState()
	if !ok {
		return ""
	}
	return strings.TrimSpace(state.TargetChannelID)
}

func botToolsEgressDisabled() bool {
	state, ok := currentTargetState()
	return ok && state.DisableEgress
}

func remoteA2AMemoryWriteAllowed() bool {
	state, ok := currentTargetState()
	if !ok || !state.RemoteA2A {
		return true
	}
	return state.AllowMemoryWrite
}

func validateBotSendFilePath(filePath string) error {
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return fmt.Errorf("file_path is required")
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("file_path is not readable by the bot process: %w%s", err, imageURLHandoffHint())
	}
	if info.IsDir() {
		return fmt.Errorf("file_path must be a file, not a directory%s", imageURLHandoffHint())
	}
	return nil
}

func imageURLHandoffHint() string {
	return "; if another tool returned an HTTP(S) image URL, use bot_send_image_url with that URL instead of passing a local, container, remote, or tool-artifact path to bot_send_file."
}

func fetchValidatedImageURL(ctx context.Context, rawURL, filename string) (string, error) {
	u, err := validateBotImageURL(rawURL)
	if err != nil {
		return "", err
	}
	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("too many image URL redirects")
			}
			if _, err := validateBotImageURL(req.URL.String()); err != nil {
				return fmt.Errorf("image URL redirect blocked: %w", err)
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("create image URL request: %w", err)
	}
	req.Header.Set("Accept", "image/jpeg,image/png")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch image URL: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("fetch image URL: unexpected HTTP status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, botegress.MaxValidatedImageBytes+1))
	if err != nil {
		return "", fmt.Errorf("read image URL response: %w", err)
	}
	if int64(len(body)) > botegress.MaxValidatedImageBytes {
		return "", fmt.Errorf("image exceeds upload size limit (%d bytes)", botegress.MaxValidatedImageBytes)
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0]))
	if contentType != "image/jpeg" && contentType != "image/png" {
		contentType = ""
	}
	return botegress.WriteValidatedImageBytes(body, contentType, filename, filepath.Join(dataDir(), "egress", "incoming"), secrets.FromEnv())
}

func validateBotImageURL(rawURL string) (*url.URL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("url is required")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse image URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("image URL scheme must be http or https")
	}
	if u.User != nil {
		return nil, fmt.Errorf("image URL must not include credentials")
	}
	if strings.TrimSpace(u.Hostname()) == "" {
		return nil, fmt.Errorf("image URL host is required")
	}
	return u, nil
}

func validateMentionUserID(userID string) error {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil
	}
	state, ok := currentTargetState()
	if !ok || len(state.AllowedMentionUserIDs) == 0 {
		return fmt.Errorf("mention_user_id %s cannot be verified because this bot-tools session has no Discord mention allowlist", userID)
	}
	for _, allowed := range state.AllowedMentionUserIDs {
		if strings.TrimSpace(allowed) == userID {
			return nil
		}
	}
	return fmt.Errorf("mention_user_id %s is not in the verified Discord mention references for this job", userID)
}

func currentTargetState() (targetState, bool) {
	path := strings.TrimSpace(os.Getenv("BOT_TOOLS_TARGET_STATE_PATH"))
	if path == "" {
		return targetState{}, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return targetState{}, false
	}
	var state targetState
	if err := json.Unmarshal(raw, &state); err != nil {
		return state, false
	}
	return state, true
}

func dataSummary(root string) (summary, error) {
	root = filepath.Clean(root)
	rows, err := listChannelData(root)
	if err != nil {
		return summary{}, err
	}
	agentRuntimeDir := filepath.Join(root, "kiro-agent-runtime")
	return summary{
		SessionsFile:           fileExists(filepath.Join(root, "sessions.json")),
		ChannelDirs:            len(rows),
		CronStore:              fileExists(filepath.Join(root, "cron", "cron.json")),
		AuditDB:                fileExists(filepath.Join(root, "audit", "discord.sqlite")),
		MCPPolicyDB:            fileExists(filepath.Join(root, "mcp", "policy.sqlite")),
		KiroAgentRuntimeDir:    dirExists(agentRuntimeDir),
		LegacyKiroRuntimeDir:   dirExists(filepath.Join(root, "kiro-runtime")),
		RuntimeMCPConfig:       fileExists(filepath.Join(agentRuntimeDir, "settings", "mcp.json")),
		RuntimeCLISettingsFile: fileExists(filepath.Join(agentRuntimeDir, "settings", "cli.json")),
	}, nil
}

func listChannelData(root string) ([]channelData, error) {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read data dir: %w", err)
	}
	metadata, err := channelmeta.Read(root)
	if err != nil {
		return nil, err
	}
	var out []channelData
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "ch-") {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		channelID := strings.TrimPrefix(entry.Name(), "ch-")
		meta := metadata[channelID]
		parent := metadata[meta.ParentChannelID]
		out = append(out, channelData{
			ChannelID:       channelID,
			Name:            meta.Name,
			Type:            meta.Type,
			ParentChannelID: meta.ParentChannelID,
			ParentName:      parent.Name,
			ChatLog:         fileExists(filepath.Join(dir, "chat.jsonl")),
			MemoryFile:      fileExists(filepath.Join(dir, "memory.json")),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ChannelID < out[j].ChannelID })
	return out, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// --- Cron pending mechanism ---

type pendingJob struct {
	Name          string `json:"name"`
	Schedule      string `json:"schedule"`
	ScheduleHuman string `json:"schedule_human,omitempty"`
	Prompt        string `json:"prompt"`
	ChannelID     string `json:"channel_id"`
	GuildID       string `json:"guild_id"`
	CreatedBy     string `json:"created_by,omitempty"`
	CreatedByID   string `json:"created_by_id,omitempty"`
	NextRun       string `json:"next_run,omitempty"`
	MentionID     string `json:"mention_id,omitempty"`
	OneShot       bool   `json:"one_shot,omitempty"`
	UseAgent      bool   `json:"use_agent,omitempty"`
}

type pendingAction struct {
	Action    string                `json:"action"` // "create", "create_reminder", "update", or "delete"
	Job       *pendingJob           `json:"job,omitempty"`
	JobID     string                `json:"job_id,omitempty"`
	ChannelID string                `json:"channel_id,omitempty"`
	Update    *heartbeat.CronUpdate `json:"update,omitempty"`
}

func writePending(root string, action pendingAction) error {
	if err := validatePendingAction(action); err != nil {
		return err
	}
	dir := filepath.Join(root, "cron", "pending")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create pending dir: %w", err)
	}
	raw, err := json.Marshal(action)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "*.json")
	if err != nil {
		return err
	}
	if _, err := f.Write(raw); err != nil {
		f.Close()
		os.Remove(f.Name())
		return err
	}
	return f.Close()
}

func validatePendingAction(action pendingAction) error {
	switch action.Action {
	case "create":
		if action.Job == nil {
			return fmt.Errorf("create action missing job")
		}
		action.Job.Name = strings.TrimSpace(action.Job.Name)
		action.Job.Schedule = strings.TrimSpace(action.Job.Schedule)
		action.Job.Prompt = strings.TrimSpace(action.Job.Prompt)
		action.Job.ChannelID = strings.TrimSpace(action.Job.ChannelID)
		action.Job.GuildID = strings.TrimSpace(action.Job.GuildID)
		action.Job.CreatedBy = strings.TrimSpace(action.Job.CreatedBy)
		action.Job.CreatedByID = strings.TrimSpace(action.Job.CreatedByID)
		if action.Job.Name == "" || action.Job.Schedule == "" || action.Job.Prompt == "" || action.Job.ChannelID == "" || action.Job.GuildID == "" {
			return fmt.Errorf("create action requires name, schedule, prompt, channel_id, and guild_id")
		}
		if _, err := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow).Parse(action.Job.Schedule); err != nil {
			return fmt.Errorf("invalid schedule: %w", err)
		}
	case "create_reminder":
		if action.Job == nil {
			return fmt.Errorf("create_reminder action missing job")
		}
		action.Job.Name = strings.TrimSpace(action.Job.Name)
		action.Job.ScheduleHuman = strings.TrimSpace(action.Job.ScheduleHuman)
		action.Job.Prompt = strings.TrimSpace(action.Job.Prompt)
		action.Job.ChannelID = strings.TrimSpace(action.Job.ChannelID)
		action.Job.GuildID = strings.TrimSpace(action.Job.GuildID)
		action.Job.CreatedBy = strings.TrimSpace(action.Job.CreatedBy)
		action.Job.CreatedByID = strings.TrimSpace(action.Job.CreatedByID)
		action.Job.NextRun = strings.TrimSpace(action.Job.NextRun)
		action.Job.MentionID = strings.TrimSpace(action.Job.MentionID)
		if action.Job.Name == "" || action.Job.Prompt == "" || action.Job.ChannelID == "" || action.Job.GuildID == "" || action.Job.NextRun == "" {
			return fmt.Errorf("create_reminder action requires name, time, content, channel_id, and guild_id")
		}
		if _, err := time.Parse(time.RFC3339, action.Job.NextRun); err != nil {
			return fmt.Errorf("invalid reminder next_run: %w", err)
		}
	case "delete":
		if strings.TrimSpace(action.JobID) == "" || strings.TrimSpace(action.ChannelID) == "" {
			return fmt.Errorf("delete action requires job_id and channel_id")
		}
	case "update":
		if strings.TrimSpace(action.JobID) == "" || strings.TrimSpace(action.ChannelID) == "" {
			return fmt.Errorf("update action requires job_id and channel_id")
		}
		if action.Update == nil {
			return fmt.Errorf("update action requires at least one update field")
		}
		if err := heartbeat.ValidateCronUpdate(*action.Update); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown action %q", action.Action)
	}
	return nil
}

func parseReminderTime(input, tz string) (time.Time, error) {
	return heartbeat.ParseTime(input, cronLocation(tz))
}

func cronLocation(tz string) *time.Location {
	if loc, err := time.LoadLocation(cronpolicy.TimezoneName(tz)); err == nil {
		return loc
	}
	return time.Now().Location()
}

func reminderName(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return "Reminder"
	}
	const max = 30
	rs := []rune(content)
	if len(rs) > max {
		rs = append(rs[:max], '.', '.', '.')
	}
	return "Reminder: " + string(rs)
}

type cronJobEntry struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	Prompt   string `json:"prompt"`
	Enabled  bool   `json:"enabled"`
	LastRun  string `json:"last_run,omitempty"`
	NextRun  string `json:"next_run,omitempty"`
}

func listCronJobs(root, channelID string) ([]cronJobEntry, error) {
	path := filepath.Join(root, "cron", "cron.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var jobs map[string]struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		ChannelID string `json:"channel_id"`
		Schedule  string `json:"schedule"`
		Prompt    string `json:"prompt"`
		Enabled   bool   `json:"enabled"`
		LastRun   string `json:"last_run"`
		NextRun   string `json:"next_run"`
	}
	if err := json.Unmarshal(data, &jobs); err != nil {
		return nil, err
	}
	var out []cronJobEntry
	for _, j := range jobs {
		if j.ChannelID != channelID {
			continue
		}
		out = append(out, cronJobEntry{
			ID: j.ID, Name: j.Name, Schedule: j.Schedule,
			Prompt: j.Prompt, Enabled: j.Enabled,
			LastRun: j.LastRun, NextRun: j.NextRun,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
