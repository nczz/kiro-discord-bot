package botmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/nczz/kiro-discord-bot/audit"
	"github.com/nczz/kiro-discord-bot/heartbeat"
	"github.com/nczz/kiro-discord-bot/internal/botegress"
	"github.com/nczz/kiro-discord-bot/internal/channelmeta"
	"github.com/nczz/kiro-discord-bot/internal/cronpolicy"
	"github.com/robfig/cron/v3"
)

// Run starts the built-in bot tools MCP server over stdio.
func Run() error {
	return server.ServeStdio(NewServer(), server.WithErrorLogger(log.New(os.Stderr, "[mcp-bot] ", log.LstdFlags)))
}

const (
	ToolDataSummary     = "bot_data_summary"
	ToolListChannelData = "bot_list_channel_data"
	ToolSendMessage     = "bot_send_message"
	ToolSendFile        = "bot_send_file"
	ToolCreateCron      = "bot_create_cron"
	ToolUpdateCron      = "bot_update_cron"
	ToolCreateReminder  = "bot_create_reminder"
	ToolListCron        = "bot_list_cron"
	ToolDeleteCron      = "bot_delete_cron"
	ToolQueryAudit      = "bot_query_audit"
)

// DefaultSafeToolNames returns the bot-tools allowlist enabled during first channel setup.
// New tools must opt into this list deliberately; being non-destructive is not enough.
func DefaultSafeToolNames() []string {
	return []string{
		ToolDataSummary,
		ToolListChannelData,
		ToolListCron,
		ToolSendFile,
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
		readOnlyTool(ToolDataSummary, "Summarize the bot data directory without returning message content"),
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
		readOnlyTool(ToolListChannelData, "List channel data directories and metadata file presence without returning message content"),
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
		writeTool(ToolSendFile, "Send a local file through the bot-controlled safe egress queue. Text files are redacted and uploaded as sanitized copies. Documents with extractable readable text (PDF, DOCX, XLSX) are converted to text, redacted, and uploaded as sanitized .txt copies; original binary documents are never uploaded back.", false),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if botToolsEgressDisabled() {
				return mcp.NewToolResultError("File egress is disabled for this private audit job."), nil
			}
			channelID, _ := req.RequireString("channel_id")
			if err := validateBoundChannel(channelID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			filePath, _ := req.RequireString("file_path")
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
			mcp.WithString("file_path", mcp.Required(), mcp.Description("Local file path to sanitize and upload. Text files stay text; PDF, DOCX, and XLSX with extractable readable text are extracted to redacted .txt copies.")),
			mcp.WithString("content", mcp.Description("Optional message content to send with the sanitized file")),
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

func readOnlyTool(name, description string) mcp.Tool {
	return mcp.NewTool(name,
		mcp.WithDescription(description),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
}

type summary struct {
	DataDir                string `json:"data_dir"`
	SessionsFile           bool   `json:"sessions_file"`
	ChannelDirs            int    `json:"channel_dirs"`
	CronStore              bool   `json:"cron_store"`
	AuditDB                bool   `json:"audit_db"`
	MCPPolicyDB            bool   `json:"mcp_policy_db"`
	KiroAgentRuntimeDir    bool   `json:"kiro_agent_runtime_dir"`
	LegacyKiroRuntimeDir   bool   `json:"legacy_kiro_runtime_dir"`
	RuntimeMCPConfig       bool   `json:"runtime_mcp_config"`
	RuntimeCLISettingsFile bool   `json:"runtime_cli_settings_file"`
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

type targetState struct {
	TargetChannelID       string   `json:"target_channel_id"`
	DisableEgress         bool     `json:"disable_egress"`
	AllowedMentionUserIDs []string `json:"allowed_mention_user_ids"`
}

func dataDir() string {
	if dir := strings.TrimSpace(os.Getenv("DATA_DIR")); dir != "" {
		return dir
	}
	return "./data"
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
		DataDir:                root,
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
