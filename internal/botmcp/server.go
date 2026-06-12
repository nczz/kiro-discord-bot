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

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
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
	ToolListCron        = "bot_list_cron"
	ToolDeleteCron      = "bot_delete_cron"
)

// DefaultSafeToolNames returns the bot-tools allowlist enabled during first channel setup.
// New tools must opt into this list deliberately; being non-destructive is not enough.
func DefaultSafeToolNames() []string {
	return []string{
		ToolDataSummary,
		ToolListChannelData,
		ToolListCron,
		ToolSendMessage,
		ToolSendFile,
		ToolCreateCron,
	}
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
		writeTool(ToolSendMessage, "Send a separate Discord message through the bot-controlled safe egress queue. Do not use this for ordinary replies or final answers; normal assistant text is already delivered by the bot. Use this only when explicitly asked to send an extra Discord message, notify another target, hand off to another bot, or perform scheduled/cron egress. The main bot redacts secrets before delivery.", false),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
		writeTool(ToolSendFile, "Send a local text file through the bot-controlled safe egress queue. The main bot uploads only a redacted sanitized copy.", false),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
			action := pendingAction{
				Action: "create",
				Job: &pendingJob{
					Name:      strings.TrimSpace(name),
					Schedule:  strings.TrimSpace(schedule),
					Prompt:    strings.TrimSpace(prompt),
					ChannelID: ownerChannelID,
					GuildID:   strings.TrimSpace(guildID),
					CreatedBy: strings.TrimSpace(createdBy),
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
			mcp.WithString("file_path", mcp.Required(), mcp.Description("Local text file path to sanitize and upload")),
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

func currentTargetStateChannelID() string {
	path := strings.TrimSpace(os.Getenv("BOT_TOOLS_TARGET_STATE_PATH"))
	if path == "" {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var state struct {
		TargetChannelID string `json:"target_channel_id"`
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return ""
	}
	return strings.TrimSpace(state.TargetChannelID)
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
	Name      string `json:"name"`
	Schedule  string `json:"schedule"`
	Prompt    string `json:"prompt"`
	ChannelID string `json:"channel_id"`
	GuildID   string `json:"guild_id"`
	CreatedBy string `json:"created_by,omitempty"`
}

type pendingAction struct {
	Action    string      `json:"action"` // "create" or "delete"
	Job       *pendingJob `json:"job,omitempty"`
	JobID     string      `json:"job_id,omitempty"`
	ChannelID string      `json:"channel_id,omitempty"`
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
		if action.Job.Name == "" || action.Job.Schedule == "" || action.Job.Prompt == "" || action.Job.ChannelID == "" || action.Job.GuildID == "" {
			return fmt.Errorf("create action requires name, schedule, prompt, channel_id, and guild_id")
		}
		if _, err := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow).Parse(action.Job.Schedule); err != nil {
			return fmt.Errorf("invalid schedule: %w", err)
		}
	case "delete":
		if strings.TrimSpace(action.JobID) == "" || strings.TrimSpace(action.ChannelID) == "" {
			return fmt.Errorf("delete action requires job_id and channel_id")
		}
	default:
		return fmt.Errorf("unknown action %q", action.Action)
	}
	return nil
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
