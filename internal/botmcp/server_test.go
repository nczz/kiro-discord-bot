package botmcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nczz/kiro-discord-bot/internal/channelmeta"
	"github.com/nczz/kiro-discord-bot/internal/cronpolicy"
)

func TestDataSummaryAndChannelListAreMetadataOnly(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sessions.json"), []byte(`{}`), 0644); err != nil {
		t.Fatalf("write sessions: %v", err)
	}
	chDir := filepath.Join(dir, "ch-channel-1")
	if err := os.MkdirAll(chDir, 0755); err != nil {
		t.Fatalf("mkdir channel dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chDir, "chat.jsonl"), []byte(`{"content":"secret"}`), 0644); err != nil {
		t.Fatalf("write chat log: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chDir, "memory.json"), []byte(`["rule"]`), 0644); err != nil {
		t.Fatalf("write memory: %v", err)
	}
	if err := channelmeta.Upsert(dir, channelmeta.Entry{ID: "channel-1", Name: "general", Type: "channel"}); err != nil {
		t.Fatalf("write channel metadata: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "cron"), 0755); err != nil {
		t.Fatalf("mkdir cron: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cron", "cron.json"), []byte(`[]`), 0644); err != nil {
		t.Fatalf("write cron: %v", err)
	}
	agentRuntimeSettings := filepath.Join(dir, "kiro-agent-runtime", "settings")
	if err := os.MkdirAll(agentRuntimeSettings, 0755); err != nil {
		t.Fatalf("mkdir agent runtime settings: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentRuntimeSettings, "mcp.json"), []byte(`{"mcpServers":{}}`), 0644); err != nil {
		t.Fatalf("write runtime mcp: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentRuntimeSettings, "cli.json"), []byte(`{}`), 0644); err != nil {
		t.Fatalf("write runtime cli: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "kiro-runtime"), 0755); err != nil {
		t.Fatalf("mkdir legacy runtime: %v", err)
	}

	s, err := dataSummary(dir)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if !s.SessionsFile || !s.CronStore || s.ChannelDirs != 1 || !s.KiroAgentRuntimeDir || !s.LegacyKiroRuntimeDir || !s.RuntimeMCPConfig || !s.RuntimeCLISettingsFile {
		t.Fatalf("unexpected summary: %+v", s)
	}

	rows, err := listChannelData(dir)
	if err != nil {
		t.Fatalf("list channels: %v", err)
	}
	if len(rows) != 1 || rows[0].ChannelID != "channel-1" || rows[0].Name != "general" || rows[0].Type != "channel" || !rows[0].ChatLog || !rows[0].MemoryFile {
		t.Fatalf("unexpected channel rows: %+v", rows)
	}
}

func TestNewServerExists(t *testing.T) {
	if NewServer() == nil {
		t.Fatal("NewServer returned nil")
	}
}

func TestDefaultSafeToolNamesExcludeDestructiveTools(t *testing.T) {
	tools := DefaultSafeToolNames()
	if len(tools) == 0 {
		t.Fatal("default safe tools are empty")
	}
	seen := map[string]bool{}
	for _, tool := range tools {
		if seen[tool] {
			t.Fatalf("duplicate default safe tool: %s", tool)
		}
		seen[tool] = true
	}
	if seen[ToolDeleteCron] {
		t.Fatalf("destructive tool %s must not be default-enabled", ToolDeleteCron)
	}
	if seen[ToolSendMessage] {
		t.Fatalf("message egress tool must not be default-enabled for ordinary replies: %+v", tools)
	}
	if !seen[ToolSendFile] {
		t.Fatalf("file egress tool should be default-enabled for interactive file delivery: %+v", tools)
	}
}

func TestCreateCronToolDocumentsBotTimezone(t *testing.T) {
	t.Setenv("CRON_TIMEZONE", "Asia/Taipei")

	tool := writeTool(ToolCreateCron, cronpolicy.CreateToolDescription(cronpolicy.TimezoneName("Asia/Taipei")), false)

	if !strings.Contains(tool.Description, "Asia/Taipei") || !strings.Contains(tool.Description, "Do not convert user-local times to UTC") {
		t.Fatalf("tool description does not include cron timezone: %q", tool.Description)
	}
	schedule, ok := tool.InputSchema.Properties["schedule"].(map[string]any)
	if !ok {
		t.Fatalf("schedule schema missing: %+v", tool.InputSchema.Properties["schedule"])
	}
	desc, _ := schedule["description"].(string)
	if !strings.Contains(desc, "Asia/Taipei") || !strings.Contains(desc, "Do not convert to UTC") {
		t.Fatalf("schedule description should pin bot timezone and forbid UTC conversion: %q", desc)
	}
}

func TestReadOnlyToolAnnotations(t *testing.T) {
	tool := readOnlyTool("bot_data_summary", "summary")
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Fatalf("readOnlyHint = %+v, want true", tool.Annotations.ReadOnlyHint)
	}
	if tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint {
		t.Fatalf("destructiveHint = %+v, want false", tool.Annotations.DestructiveHint)
	}
	if tool.Annotations.IdempotentHint == nil || !*tool.Annotations.IdempotentHint {
		t.Fatalf("idempotentHint = %+v, want true", tool.Annotations.IdempotentHint)
	}
	if tool.Annotations.OpenWorldHint == nil || *tool.Annotations.OpenWorldHint {
		t.Fatalf("openWorldHint = %+v, want false", tool.Annotations.OpenWorldHint)
	}
}

func TestWriteToolAnnotations(t *testing.T) {
	sendMessageTool := writeTool("bot_send_message", "send", false)
	if sendMessageTool.Annotations.ReadOnlyHint == nil || *sendMessageTool.Annotations.ReadOnlyHint {
		t.Fatalf("send message readOnlyHint = %+v, want false", sendMessageTool.Annotations.ReadOnlyHint)
	}
	if sendMessageTool.Annotations.DestructiveHint == nil || *sendMessageTool.Annotations.DestructiveHint {
		t.Fatalf("send message destructiveHint = %+v, want false", sendMessageTool.Annotations.DestructiveHint)
	}

	sendFileTool := writeTool("bot_send_file", "file", false)
	if sendFileTool.Annotations.ReadOnlyHint == nil || *sendFileTool.Annotations.ReadOnlyHint {
		t.Fatalf("send file readOnlyHint = %+v, want false", sendFileTool.Annotations.ReadOnlyHint)
	}
	if sendFileTool.Annotations.DestructiveHint == nil || *sendFileTool.Annotations.DestructiveHint {
		t.Fatalf("send file destructiveHint = %+v, want false", sendFileTool.Annotations.DestructiveHint)
	}

	createTool := writeTool("bot_create_cron", "create", false)
	if createTool.Annotations.ReadOnlyHint == nil || *createTool.Annotations.ReadOnlyHint {
		t.Fatalf("create readOnlyHint = %+v, want false", createTool.Annotations.ReadOnlyHint)
	}
	if createTool.Annotations.DestructiveHint == nil || *createTool.Annotations.DestructiveHint {
		t.Fatalf("create destructiveHint = %+v, want false", createTool.Annotations.DestructiveHint)
	}

	deleteTool := writeTool("bot_delete_cron", "delete", true)
	if deleteTool.Annotations.ReadOnlyHint == nil || *deleteTool.Annotations.ReadOnlyHint {
		t.Fatalf("delete readOnlyHint = %+v, want false", deleteTool.Annotations.ReadOnlyHint)
	}
	if deleteTool.Annotations.DestructiveHint == nil || !*deleteTool.Annotations.DestructiveHint {
		t.Fatalf("delete destructiveHint = %+v, want true", deleteTool.Annotations.DestructiveHint)
	}
	if deleteTool.Annotations.OpenWorldHint == nil || *deleteTool.Annotations.OpenWorldHint {
		t.Fatalf("delete openWorldHint = %+v, want false", deleteTool.Annotations.OpenWorldHint)
	}
}

func TestValidateBoundChannel(t *testing.T) {
	t.Setenv("BOT_TOOLS_CHANNEL_ID", "channel-1")
	if err := validateBoundChannel("channel-1"); err != nil {
		t.Fatalf("matching channel rejected: %v", err)
	}
	if err := validateBoundChannel("channel-2"); err == nil {
		t.Fatal("mismatched channel accepted")
	}
}

func TestDeliveryChannelPrefersTargetChannel(t *testing.T) {
	t.Setenv("BOT_TOOLS_CHANNEL_ID", "channel-1")
	t.Setenv("BOT_TOOLS_TARGET_CHANNEL_ID", "thread-1")
	if err := validateBoundChannel("thread-1"); err != nil {
		t.Fatalf("target channel rejected: %v", err)
	}
	if got := deliveryChannelID("channel-1"); got != "thread-1" {
		t.Fatalf("deliveryChannelID = %q, want thread-1", got)
	}
}

func TestDeliveryChannelPrefersDynamicTargetState(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "target.json")
	if err := os.WriteFile(statePath, []byte(`{"target_channel_id":"thread-1"}`), 0644); err != nil {
		t.Fatalf("write target state: %v", err)
	}
	t.Setenv("BOT_TOOLS_CHANNEL_ID", "channel-1")
	t.Setenv("BOT_TOOLS_TARGET_CHANNEL_ID", "channel-1")
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", statePath)

	if err := validateBoundChannel("thread-1"); err != nil {
		t.Fatalf("dynamic target channel rejected: %v", err)
	}
	if got := deliveryChannelID("channel-1"); got != "thread-1" {
		t.Fatalf("deliveryChannelID = %q, want dynamic thread target", got)
	}
}

func TestCronOwnerChannelNormalizesDynamicThreadTargetToBoundChannel(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "target.json")
	if err := os.WriteFile(statePath, []byte(`{"target_channel_id":"thread-1"}`), 0644); err != nil {
		t.Fatalf("write target state: %v", err)
	}
	t.Setenv("BOT_TOOLS_CHANNEL_ID", "channel-1")
	t.Setenv("BOT_TOOLS_TARGET_CHANNEL_ID", "channel-1")
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", statePath)

	got, err := cronOwnerChannelID("thread-1")
	if err != nil {
		t.Fatalf("cron owner rejected dynamic thread target: %v", err)
	}
	if got != "channel-1" {
		t.Fatalf("cron owner = %q, want bound parent channel", got)
	}
}

func TestCronOwnerChannelKeepsLegacyUnboundRequest(t *testing.T) {
	got, err := cronOwnerChannelID("channel-legacy")
	if err != nil {
		t.Fatalf("cron owner rejected unbound legacy request: %v", err)
	}
	if got != "channel-legacy" {
		t.Fatalf("cron owner = %q, want legacy requested channel", got)
	}
}

func TestWritePendingRejectsInvalidActions(t *testing.T) {
	dir := t.TempDir()
	if err := writePending(dir, pendingAction{
		Action: "create",
		Job: &pendingJob{
			Name:      "bad",
			Schedule:  "not cron",
			Prompt:    "run",
			ChannelID: "ch-1",
			GuildID:   "guild-1",
		},
	}); err == nil {
		t.Fatal("writePending accepted invalid cron schedule")
	}
	if _, err := os.Stat(filepath.Join(dir, "cron", "pending")); !os.IsNotExist(err) {
		t.Fatalf("invalid action should not create pending dir, stat err=%v", err)
	}
}

func TestWritePendingCreateAndListCron(t *testing.T) {
	dir := t.TempDir()

	// Write a pending create action.
	if err := writePending(dir, pendingAction{
		Action: "create",
		Job: &pendingJob{
			Name:      "daily-report",
			Schedule:  "0 9 * * *",
			Prompt:    "Generate report",
			ChannelID: "ch-1",
			GuildID:   "guild-1",
			CreatedBy: "testuser",
		},
	}); err != nil {
		t.Fatalf("writePending: %v", err)
	}

	// Verify pending file exists.
	pendingDir := filepath.Join(dir, "cron", "pending")
	entries, err := os.ReadDir(pendingDir)
	if err != nil {
		t.Fatalf("read pending dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 pending file, got %d", len(entries))
	}

	// Write a pending delete action.
	if err := writePending(dir, pendingAction{
		Action:    "delete",
		JobID:     "job-123",
		ChannelID: "ch-1",
	}); err != nil {
		t.Fatalf("writePending delete: %v", err)
	}
	entries, _ = os.ReadDir(pendingDir)
	if len(entries) != 2 {
		t.Fatalf("expected 2 pending files, got %d", len(entries))
	}
}

func TestWritePendingCreateCronNormalizesThreadTargetToBoundChannel(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "target.json")
	if err := os.WriteFile(statePath, []byte(`{"target_channel_id":"thread-1"}`), 0644); err != nil {
		t.Fatalf("write target state: %v", err)
	}
	t.Setenv("BOT_TOOLS_CHANNEL_ID", "channel-1")
	t.Setenv("BOT_TOOLS_TARGET_CHANNEL_ID", "channel-1")
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", statePath)

	ownerChannelID, err := cronOwnerChannelID("thread-1")
	if err != nil {
		t.Fatalf("cron owner: %v", err)
	}
	if err := writePending(dir, pendingAction{
		Action: "create",
		Job: &pendingJob{
			Name:      "daily-report",
			Schedule:  "0 9 * * *",
			Prompt:    "Generate report",
			ChannelID: ownerChannelID,
			GuildID:   "g1",
		},
	}); err != nil {
		t.Fatalf("writePending: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(dir, "cron", "pending"))
	if err != nil {
		t.Fatalf("read pending dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("pending entries = %d, want 1", len(entries))
	}
	raw, err := os.ReadFile(filepath.Join(dir, "cron", "pending", entries[0].Name()))
	if err != nil {
		t.Fatalf("read pending: %v", err)
	}
	if strings.Contains(string(raw), `"thread-1"`) || !strings.Contains(string(raw), `"channel_id":"channel-1"`) {
		t.Fatalf("pending create should be parent-scoped, got %s", raw)
	}
}

func TestListCronJobsFiltersByChannel(t *testing.T) {
	dir := t.TempDir()
	cronDir := filepath.Join(dir, "cron")
	if err := os.MkdirAll(cronDir, 0755); err != nil {
		t.Fatal(err)
	}
	data := `{
		"job1": {"id":"job1","name":"Report","channel_id":"ch-1","guild_id":"g1","schedule":"0 9 * * *","prompt":"run","enabled":true},
		"job2": {"id":"job2","name":"Other","channel_id":"ch-2","guild_id":"g1","schedule":"0 10 * * *","prompt":"other","enabled":false}
	}`
	if err := os.WriteFile(filepath.Join(cronDir, "cron.json"), []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	jobs, err := listCronJobs(dir, "ch-1")
	if err != nil {
		t.Fatalf("listCronJobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != "job1" || jobs[0].Name != "Report" || !jobs[0].Enabled {
		t.Fatalf("unexpected jobs: %+v", jobs)
	}

	// Empty channel returns nil.
	jobs, _ = listCronJobs(dir, "ch-nonexist")
	if len(jobs) != 0 {
		t.Fatalf("expected empty, got %+v", jobs)
	}
}
