package botmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/nczz/kiro-discord-bot/a2a"
	"github.com/nczz/kiro-discord-bot/audit"
	"github.com/nczz/kiro-discord-bot/heartbeat"
	"github.com/nczz/kiro-discord-bot/internal/botegress"
	"github.com/nczz/kiro-discord-bot/internal/channelmeta"
	"github.com/nczz/kiro-discord-bot/internal/cronpolicy"
)

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

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
	rawSummary, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal summary: %v", err)
	}
	if strings.Contains(string(rawSummary), dir) || strings.Contains(string(rawSummary), "data_dir") {
		t.Fatalf("summary exposed data directory path: %s", rawSummary)
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
	if !seen[ToolSendImageURL] {
		t.Fatalf("URL image egress tool should be default-enabled for MCP image delivery: %+v", tools)
	}
	if !seen[ToolQueryChannelHistory] {
		t.Fatalf("channel history query tool should be default-enabled for scoped discussion lookup: %+v", tools)
	}
	if !seen[ToolCreateReminder] {
		t.Fatalf("one-time reminder tool should be default-enabled to avoid agent-side scheduling bypasses: %+v", tools)
	}
	if !seen[ToolUpdateCron] {
		t.Fatalf("safe non-destructive update tool should be default-enabled: %+v", tools)
	}
	if !seen[ToolCurrentTime] || !seen[ToolResolveDateRange] {
		t.Fatalf("time helper tools should be default-enabled for safe date answers: %+v", tools)
	}
	if seen[ToolA2ADelegate] || seen[ToolA2ARuntimePreflight] {
		t.Fatalf("A2A tools should not be default-enabled when A2A is disabled: %+v", tools)
	}
	if seen[ToolQueryAudit] {
		t.Fatalf("audit query tool must not be default-enabled outside manager-authorized /audit prompt jobs: %+v", tools)
	}
	if !seen[ToolMemoryList] || !seen[ToolMemoryAdd] {
		t.Fatalf("memory list/add tools should be default-enabled for explicit channel memory requests: %+v", tools)
	}
	if seen[ToolMemoryRemove] || seen[ToolMemoryClear] {
		t.Fatalf("destructive memory tools must not be default-enabled: %+v", tools)
	}
	auditTools := AuditPromptToolNames()
	if len(auditTools) != 1 || auditTools[0] != ToolQueryAudit {
		t.Fatalf("audit prompt tools = %+v, want only %s", auditTools, ToolQueryAudit)
	}

	a2aTools := DefaultSafeToolNamesForA2A(true)
	a2aSeen := map[string]bool{}
	for _, tool := range a2aTools {
		a2aSeen[tool] = true
	}
	for _, tool := range []string{ToolA2APeers, ToolA2ATaskStatus, ToolA2ATrustPeer, ToolA2ADelegate, ToolA2ACancel, ToolA2AInputReply, ToolA2AAuthReply} {
		if !a2aSeen[tool] {
			t.Fatalf("A2A MCP tool %s should be default-enabled only when A2A is enabled: %+v", tool, a2aTools)
		}
	}
	for _, tool := range []string{ToolA2APolicyGet, ToolA2ARuntimePreflight, ToolA2APolicyPlan, ToolA2APolicyApply} {
		if a2aSeen[tool] {
			t.Fatalf("expert A2A MCP tool %s must not be default-enabled for normal agents: %+v", tool, a2aTools)
		}
	}
}

func TestNewServerWithOptionsGatesA2ATools(t *testing.T) {
	disabled := listToolNames(t, NewServerWithOptions(ServerOptions{}))
	if disabled[ToolA2ADelegate] || disabled[ToolA2APeers] {
		t.Fatalf("disabled A2A server exposed A2A tools: %+v", disabled)
	}
	enabled := listToolNames(t, NewServerWithOptions(ServerOptions{A2AEnabled: true}))
	if !enabled[ToolA2ADelegate] || !enabled[ToolA2APeers] {
		t.Fatalf("enabled A2A server missing A2A tools: %+v", enabled)
	}
	for _, tool := range []string{ToolA2APolicyGet, ToolA2ARuntimePreflight, ToolA2APolicyPlan, ToolA2APolicyApply} {
		if enabled[tool] {
			t.Fatalf("retired expert A2A tool %s should not be registered: %+v", tool, enabled)
		}
	}
}

func TestNewServerFromEnvDerivesA2AFromRuntimeEnv(t *testing.T) {
	t.Setenv("KIRO_BOT_A2A_ENABLED", "")
	t.Setenv("NATS_URL", "nats://127.0.0.1:4222")
	t.Setenv("A2A_AGENT_ID", "local-agent")
	tools := listToolNames(t, NewServerFromEnv())
	if !tools[ToolA2ADelegate] || !tools[ToolA2APeers] {
		t.Fatalf("A2A runtime env did not expose A2A tools: %+v", tools)
	}
}

func listToolNames(t *testing.T, srv *server.MCPServer) map[string]bool {
	t.Helper()
	client, err := mcpclient.NewInProcessClient(srv)
	if err != nil {
		t.Fatalf("new in-process client: %v", err)
	}
	defer client.Close()
	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("start client: %v", err)
	}
	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "botmcp-test", Version: "1"}
	if _, err := client.Initialize(context.Background(), initReq); err != nil {
		t.Fatalf("initialize client: %v", err)
	}
	result, err := client.ListTools(context.Background(), mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	out := map[string]bool{}
	for _, tool := range result.Tools {
		out[tool.Name] = true
	}
	return out
}

func TestMemoryOwnerChannelIDUsesBoundParentForThreadTarget(t *testing.T) {
	t.Setenv("BOT_TOOLS_CHANNEL_ID", "channel-1")
	t.Setenv("BOT_TOOLS_TARGET_CHANNEL_ID", "thread-1")

	got, err := memoryOwnerChannelID("thread-1")
	if err != nil {
		t.Fatalf("memoryOwnerChannelID: %v", err)
	}
	if got != "channel-1" {
		t.Fatalf("memory owner = %s, want channel-1", got)
	}
}

func TestSafeMemoryEntryRejectsSecretLikeText(t *testing.T) {
	t.Setenv("KIRO_API_KEY", "known-secret-value")
	if _, err := safeMemoryEntry("Always use token abc in replies"); err == nil {
		t.Fatal("safeMemoryEntry accepted token-bearing memory")
	}
	if _, err := safeMemoryEntry("Remember known-secret-value for the next login"); err == nil {
		t.Fatal("safeMemoryEntry accepted known secret value")
	}
	got, err := safeMemoryEntry("  Always reply in Traditional Chinese.  ")
	if err != nil {
		t.Fatalf("safeMemoryEntry rejected safe text: %v", err)
	}
	if got != "Always reply in Traditional Chinese." {
		t.Fatalf("normalized memory = %q", got)
	}
}

func TestValidateMemoryPurposeRejectsKnowledgeBaseUpdate(t *testing.T) {
	err := validateMemoryPurpose(
		"查詢人員時優先使用 Notion 人員對照表。",
		"使用者要求更新查詢方法到知識庫",
	)
	if err == nil {
		t.Fatal("validateMemoryPurpose accepted a knowledge-base update request")
	}
	if !strings.Contains(err.Error(), "not knowledge base updates") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateMemoryPurposeRejectsDocsCorpusAliases(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reason string
	}{
		{name: "update docs", reason: "user asked to update docs"},
		{name: "index docs", reason: "index the docs with the new query method"},
		{name: "add corpus", reason: "add to corpus for future retrieval"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateMemoryPurpose("Use the new retrieval method.", tc.reason); err == nil {
				t.Fatalf("validateMemoryPurpose accepted %q as channel memory", tc.reason)
			}
		})
	}
}

func TestValidateMemoryPurposeAllowsKnowledgeBaseMentionInBehaviorRule(t *testing.T) {
	for _, tc := range []struct {
		name  string
		entry string
	}{
		{name: "local kb fallback", entry: "查詢新品時，不要只依賴本地知識庫回答沒有資料。"},
		{name: "search kb index", entry: "Always search the knowledge base index first."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateMemoryPurpose(tc.entry, "user explicitly asked the bot to remember a retrieval behavior rule"); err != nil {
				t.Fatalf("validateMemoryPurpose rejected a memory rule that only mentions knowledge base: %v", err)
			}
		})
	}
}

func TestQueueAuditedMemoryActionWritesPendingAndAudit(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "audit", "discord.sqlite")
	t.Setenv("DATA_DIR", dir)
	t.Setenv("AUDIT_LOG_ENABLED", "true")
	t.Setenv("AUDIT_LOG_DB", dbPath)
	t.Setenv("BOT_TOOLS_GUILD_ID", "guild-1")

	id, err := queueAuditedMemoryAction(botegress.Action{
		Action:      botegress.ActionMemoryAdd,
		ChannelID:   "channel-1",
		MemoryEntry: "Always reply in Traditional Chinese.",
		RequestedBy: "alice user_id=user-1",
		Reason:      "user explicitly asked the bot to remember language preference",
	})
	if err != nil {
		t.Fatalf("queueAuditedMemoryAction: %v", err)
	}
	actions, err := botegress.ReadPending(dir)
	if err != nil {
		t.Fatalf("ReadPending: %v", err)
	}
	if len(actions) != 1 || actions[0].ID != id || actions[0].Action != botegress.ActionMemoryAdd || actions[0].MemoryEntry == "" {
		t.Fatalf("unexpected pending actions: %+v", actions)
	}
	events, err := audit.QueryTimelineReadOnly(dbPath, audit.TimelineQueryOptions{
		GuildID:        "guild-1",
		TargetID:       "channel-1",
		EventType:      "bot_memory_update_queued",
		IncludeContent: true,
		Limit:          10,
	})
	if err != nil {
		t.Fatalf("query memory audit: %v", err)
	}
	if len(events) != 1 || events[0].Command != botegress.ActionMemoryAdd || events[0].Content != "Always reply in Traditional Chinese." {
		t.Fatalf("unexpected memory audit events: %+v", events)
	}
}

func TestQueueAuditedMemoryActionHonorsAuditContentRetention(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "audit", "discord.sqlite")
	t.Setenv("DATA_DIR", dir)
	t.Setenv("AUDIT_LOG_ENABLED", "true")
	t.Setenv("AUDIT_LOG_RECORD_CONTENT", "false")
	t.Setenv("AUDIT_LOG_DB", dbPath)
	t.Setenv("BOT_TOOLS_GUILD_ID", "guild-1")

	if _, err := queueAuditedMemoryAction(botegress.Action{
		Action:      botegress.ActionMemoryAdd,
		ChannelID:   "channel-1",
		MemoryEntry: "Do not persist this audit content.",
		RequestedBy: "alice user_id=user-1",
		Reason:      "user explicitly asked for a memory",
	}); err != nil {
		t.Fatalf("queueAuditedMemoryAction: %v", err)
	}
	events, err := audit.QueryTimelineReadOnly(dbPath, audit.TimelineQueryOptions{
		GuildID:        "guild-1",
		TargetID:       "channel-1",
		EventType:      "bot_memory_update_queued",
		IncludeContent: true,
		Limit:          10,
	})
	if err != nil {
		t.Fatalf("query memory audit: %v", err)
	}
	if len(events) != 1 || events[0].Content != "" {
		t.Fatalf("audit content = %+v, want one event with empty content", events)
	}
}

func TestQueueAuditedMemoryActionDoesNotPublishPendingWhenAuditFails(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DATA_DIR", dir)
	t.Setenv("AUDIT_LOG_ENABLED", "true")
	t.Setenv("AUDIT_LOG_DB", dir)

	if _, err := queueAuditedMemoryAction(botegress.Action{
		Action:      botegress.ActionMemoryAdd,
		ChannelID:   "channel-1",
		MemoryEntry: "Always reply in Traditional Chinese.",
		RequestedBy: "alice user_id=user-1",
		Reason:      "user explicitly asked for a memory",
	}); err == nil {
		t.Fatal("queueAuditedMemoryAction accepted memory write when audit open failed")
	}
	actions, err := botegress.ReadPending(dir)
	if err != nil {
		t.Fatalf("ReadPending: %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("pending action should not be published when audit fails: %+v", actions)
	}
}

func TestQueueAuditedMemoryActionValidatesBeforeAudit(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "audit", "discord.sqlite")
	t.Setenv("DATA_DIR", dir)
	t.Setenv("AUDIT_LOG_ENABLED", "true")
	t.Setenv("AUDIT_LOG_DB", dbPath)

	if _, err := queueAuditedMemoryAction(botegress.Action{
		Action:      botegress.ActionMemoryAdd,
		ChannelID:   "channel-1",
		MemoryEntry: "Always reply in Traditional Chinese.",
		RequestedBy: "   ",
		Reason:      "user explicitly asked for a memory",
	}); err == nil {
		t.Fatal("queueAuditedMemoryAction accepted whitespace requested_by")
	}
	actions, err := botegress.ReadPending(dir)
	if err != nil {
		t.Fatalf("ReadPending: %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("pending action should not be published when validation fails: %+v", actions)
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("audit DB should not be created before validation succeeds, stat err=%v", err)
	}
}

func TestQueueAuditedMemoryActionRequiresAuditEnabled(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DATA_DIR", dir)
	t.Setenv("AUDIT_LOG_ENABLED", "false")
	if _, err := queueAuditedMemoryAction(botegress.Action{
		Action:      botegress.ActionMemoryAdd,
		ChannelID:   "channel-1",
		MemoryEntry: "Always reply in Traditional Chinese.",
		RequestedBy: "alice user_id=user-1",
		Reason:      "user explicitly asked for a memory",
	}); err == nil {
		t.Fatal("queueAuditedMemoryAction accepted memory write without audit logging")
	}
	actions, err := botegress.ReadPending(dir)
	if err != nil {
		t.Fatalf("ReadPending: %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("pending action should not be written when audit is disabled: %+v", actions)
	}
}

func TestAuditToolTargetIDIsBoundToCurrentBotToolsTarget(t *testing.T) {
	t.Setenv("BOT_TOOLS_CHANNEL_ID", "channel-1")
	t.Setenv("BOT_TOOLS_TARGET_CHANNEL_ID", "thread-1")

	got, err := auditToolTargetID("")
	if err != nil {
		t.Fatalf("default target: %v", err)
	}
	if got != "thread-1" {
		t.Fatalf("default target = %q, want thread-1", got)
	}
	if _, err := auditToolTargetID("channel-2"); err == nil {
		t.Fatal("expected cross-channel target to be rejected")
	}
}

func TestCreateCronToolDocumentsBotTimezone(t *testing.T) {
	t.Setenv("CRON_TIMEZONE", "Asia/Taipei")

	tool := writeTool(ToolCreateCron, cronpolicy.CreateToolDescription(cronpolicy.TimezoneName("Asia/Taipei")), false)

	if !strings.Contains(tool.Description, "Asia/Taipei") || !strings.Contains(tool.Description, "Do not convert user-local times to UTC") || !strings.Contains(tool.Description, "bot_create_reminder") {
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

func TestTimeToolsDocumentTimezoneAndStructuredRangeUse(t *testing.T) {
	tool := currentTimeTool("Asia/Taipei")
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Fatalf("current time readOnlyHint = %+v, want true", tool.Annotations.ReadOnlyHint)
	}
	if !strings.Contains(tool.Description, "CRON_TIMEZONE") || !strings.Contains(tool.Description, "Do not infer current date/time from model memory") {
		t.Fatalf("current time description missing timezone/model-memory guidance: %q", tool.Description)
	}
	if _, ok := tool.InputSchema.Properties["timezone"]; ok {
		t.Fatalf("current time tool must not expose timezone override: %+v", tool.InputSchema.Properties)
	}

	rangeTool := resolveDateRangeTool("Asia/Taipei")
	if rangeTool.Annotations.ReadOnlyHint == nil || !*rangeTool.Annotations.ReadOnlyHint {
		t.Fatalf("range readOnlyHint = %+v, want true", rangeTool.Annotations.ReadOnlyHint)
	}
	for _, want := range []string{"structured MCP arguments", "range_type=month_week", "Do not calculate weekdays", "translate the user's natural-language date phrase"} {
		if !strings.Contains(rangeTool.Description, want) {
			t.Fatalf("range tool description missing %q: %q", want, rangeTool.Description)
		}
	}
	for _, field := range []string{"range_type", "offset", "week_index", "month_week_policy", "include_today"} {
		if _, ok := rangeTool.InputSchema.Properties[field]; !ok {
			t.Fatalf("range tool schema missing %s: %+v", field, rangeTool.InputSchema.Properties)
		}
	}
	if _, ok := rangeTool.InputSchema.Properties["timezone"]; ok {
		t.Fatalf("range tool must not expose timezone override: %+v", rangeTool.InputSchema.Properties)
	}
	if _, ok := rangeTool.InputSchema.Properties["expression"]; ok {
		t.Fatalf("range tool must not expose natural-language expression input: %+v", rangeTool.InputSchema.Properties)
	}
	if _, ok := rangeTool.InputSchema.Properties["locale"]; ok {
		t.Fatalf("range tool must not expose natural-language locale input: %+v", rangeTool.InputSchema.Properties)
	}
}

func TestUpdateCronToolDocumentsSafePartialUpdateContract(t *testing.T) {
	t.Setenv("CRON_TIMEZONE", "Asia/Taipei")
	tool := writeTool(ToolUpdateCron, cronpolicy.UpdateToolDescription("Asia/Taipei"), false)
	for _, want := range []string{"bot_list_cron", "enabled=false", "bot_delete_cron", "One-time reminders cannot be updated", "Asia/Taipei"} {
		if !strings.Contains(tool.Description, want) {
			t.Fatalf("description missing %q: %s", want, tool.Description)
		}
	}
	if tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint {
		t.Fatal("update must not be destructive")
	}
	if tool.Annotations.IdempotentHint == nil || !*tool.Annotations.IdempotentHint {
		t.Fatal("update must be idempotent")
	}
	for _, field := range []string{"job_id", "channel_id", "name", "schedule", "prompt", "enabled"} {
		if _, ok := tool.InputSchema.Properties[field]; !ok {
			t.Fatalf("missing schema field %s", field)
		}
	}
}

func TestCreateReminderToolDocumentsOneTimeTimezone(t *testing.T) {
	t.Setenv("CRON_TIMEZONE", "Asia/Taipei")

	tool := writeTool(ToolCreateReminder, cronpolicy.ReminderToolDescription(cronpolicy.TimezoneName("Asia/Taipei")), false)

	if !strings.Contains(tool.Description, "one-time") || !strings.Contains(tool.Description, "Asia/Taipei") || !strings.Contains(tool.Description, "bot_create_cron") {
		t.Fatalf("reminder description should distinguish one-time reminders from recurring cron: %q", tool.Description)
	}
	timeProp, ok := tool.InputSchema.Properties["time"].(map[string]any)
	if !ok {
		t.Fatalf("time schema missing: %+v", tool.InputSchema.Properties["time"])
	}
	desc, _ := timeProp["description"].(string)
	if !strings.Contains(desc, "Asia/Taipei") || !strings.Contains(desc, "+30m") || !strings.Contains(desc, "tomorrow 09:00") {
		t.Fatalf("time description should document supported one-time formats: %q", desc)
	}
	mention, ok := tool.InputSchema.Properties["mention_user_id"].(map[string]any)
	if !ok {
		t.Fatalf("mention_user_id schema missing: %+v", tool.InputSchema.Properties["mention_user_id"])
	}
	mentionDesc, _ := mention["description"].(string)
	if !strings.Contains(mentionDesc, "verified Discord user ID") {
		t.Fatalf("mention_user_id description should require verified IDs: %q", mentionDesc)
	}
	if _, ok := tool.InputSchema.Properties["created_by_id"].(map[string]any); !ok {
		t.Fatalf("created_by_id schema missing: %+v", tool.InputSchema.Properties["created_by_id"])
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

func TestSendFileToolDocumentsLocalPathBoundary(t *testing.T) {
	tool := writeTool(ToolSendFile, "Send a bot-local file through the bot-controlled safe egress queue. The file_path must be readable by the kiro-discord-bot process on this host/VM; do not pass paths from another MCP server, Docker container, browser profile namespace, remote host, or any tool-returned artifact namespace unless they are explicitly mounted into the bot filesystem. If another tool returns an HTTP(S) image URL, pass that URL directly to bot_send_image_url instead of saving, downloading, transcribing, base64-encoding, or converting it into a local artifact path. Text files are redacted and uploaded as sanitized copies. JPEG/PNG images are validated and uploaded as copied temp files without OCR redaction or metadata stripping. Documents with extractable readable text (PDF, DOCX, XLSX) are converted to text, redacted, and uploaded as sanitized .txt copies; original binary documents are never uploaded back.", false)

	if !strings.Contains(tool.Description, "bot-local file") || !strings.Contains(tool.Description, "Docker container") || !strings.Contains(tool.Description, "HTTP(S) image URL") {
		t.Fatalf("send file description should document local path boundary and image URL handoff: %q", tool.Description)
	}
	filePath, ok := tool.InputSchema.Properties["file_path"].(map[string]any)
	if !ok {
		t.Fatalf("file_path schema missing: %+v", tool.InputSchema.Properties)
	}
	desc, _ := filePath["description"].(string)
	if !strings.Contains(desc, "readable by the kiro-discord-bot process") || !strings.Contains(desc, "tool-returned artifact") || !strings.Contains(desc, "bot_send_image_url") {
		t.Fatalf("file_path description should document bot-local path boundary and image URL handoff: %q", desc)
	}
}

func TestValidateBotSendFilePathRejectsNonLocalImageResourcePath(t *testing.T) {
	err := validateBotSendFilePath("/remote/tool/artifacts/screenshot.jpg")
	if err == nil {
		t.Fatal("validateBotSendFilePath accepted missing non-local artifact path")
	}
	got := err.Error()
	for _, want := range []string{"not readable", "HTTP(S) image URL", "bot_send_image_url", "tool-artifact path"} {
		if !strings.Contains(got, want) {
			t.Fatalf("error missing %q: %s", want, got)
		}
	}
}

func TestSendImageURLToolDocumentsURLBoundary(t *testing.T) {
	tool := writeTool(ToolSendImageURL, "Send a JPEG/PNG image from a non-secret HTTP(S) URL through the bot-controlled safe egress queue. Use this whenever another tool returns an image URL; do not download, transcribe, or base64-encode the image in the agent. The bot fetches the URL server-side, rejects URL credentials, validates the fetched bytes, and does not require the URL path to include an image filename.", false)

	for _, field := range []string{"url", "filename"} {
		if _, ok := tool.InputSchema.Properties[field].(map[string]any); !ok {
			t.Fatalf("%s schema missing: %+v", field, tool.InputSchema.Properties[field])
		}
	}
	if !strings.Contains(tool.Description, "whenever another tool returns an image URL") || !strings.Contains(tool.Description, "does not require the URL path") || !strings.Contains(tool.Description, "rejects URL credentials") {
		t.Fatalf("send image URL description should document URL behavior: %q", tool.Description)
	}
	urlDesc, _ := tool.InputSchema.Properties["url"].(map[string]any)["description"].(string)
	if !strings.Contains(strings.ToLower(urlDesc), "non-secret") || !strings.Contains(urlDesc, "do not copy base64") || !strings.Contains(urlDesc, "does not need to contain an image filename") {
		t.Fatalf("url description should reject base64 handoff: %q", urlDesc)
	}
	if tool.Annotations.OpenWorldHint == nil || !*tool.Annotations.OpenWorldHint {
		t.Fatalf("send image URL openWorldHint = %+v, want true", tool.Annotations.OpenWorldHint)
	}
}

func TestValidateBotImageURLAllowsHTTPAndHTTPSSources(t *testing.T) {
	for _, raw := range []string{
		"https://images.example.com/screenshot.jpg",
		"http://cdn.example.com/path/result?size=large",
		"http://127.0.0.1:19280/api/sessions/session-1/screenshot",
		"http://localhost:19280/api/sessions/session-1/screenshot",
	} {
		if _, err := validateBotImageURL(raw); err != nil {
			t.Fatalf("validateBotImageURL(%q): %v", raw, err)
		}
	}
}

func TestValidateBotImageURLRejectsNonHTTPAndCredentials(t *testing.T) {
	for _, raw := range []string{
		"http://user:pass@example.com/screenshot.jpg",
		"file:///tmp/screenshot.jpg",
		"/tmp/screenshot.jpg",
	} {
		if _, err := validateBotImageURL(raw); err == nil {
			t.Fatalf("validateBotImageURL(%q) succeeded, want blocked", raw)
		}
	}
}

func TestFetchValidatedImageURLStagesAllowedJPEG(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DATA_DIR", dir)
	pixel := image.NewRGBA(image.Rect(0, 0, 1, 1))
	pixel.Set(0, 0, color.RGBA{R: 0x10, G: 0x20, B: 0x30, A: 0xff})
	var img bytes.Buffer
	if err := jpeg.Encode(&img, pixel, nil); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(img.Bytes())
	}))
	defer srv.Close()

	path, err := fetchValidatedImageURL(context.Background(), srv.URL+"/screenshot", "screen.txt")
	if err != nil {
		t.Fatalf("fetchValidatedImageURL: %v", err)
	}
	if filepath.Base(path) != "screen.jpg" {
		t.Fatalf("staged basename = %q, want screen.jpg", filepath.Base(path))
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, img.Bytes()) {
		t.Fatal("staged fetched image bytes changed")
	}
}

func TestQueryChannelHistoryToolDocumentsCurrentContext(t *testing.T) {
	tool := queryChannelHistoryTool()
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Fatalf("channel history readOnlyHint = %+v, want true", tool.Annotations.ReadOnlyHint)
	}
	if tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint {
		t.Fatalf("channel history destructiveHint = %+v, want false", tool.Annotations.DestructiveHint)
	}
	if !strings.Contains(tool.Description, "current bot-tools channel/thread context") || !strings.Contains(tool.Description, "including child threads") || !strings.Contains(tool.Description, "Query is optional") || !strings.Contains(tool.Description, "continue until has_more is false") {
		t.Fatalf("history tool description should document current context and thread inclusion: %q", tool.Description)
	}
	for _, field := range []string{"query", "target_id", "limit", "offset"} {
		if _, ok := tool.InputSchema.Properties[field].(map[string]any); !ok {
			t.Fatalf("%s schema missing: %+v", field, tool.InputSchema.Properties[field])
		}
	}
	if containsString(tool.InputSchema.Required, "query") {
		t.Fatalf("query should be optional for broad history review: %+v", tool.InputSchema.Required)
	}
	targetDesc, _ := tool.InputSchema.Properties["target_id"].(map[string]any)["description"].(string)
	if !strings.Contains(targetDesc, "Use channel_id to include child threads") || !strings.Contains(targetDesc, "thread_id") {
		t.Fatalf("target_id description should explain channel/thread search scope: %q", targetDesc)
	}
	offsetDesc, _ := tool.InputSchema.Properties["offset"].(map[string]any)["description"].(string)
	if !strings.Contains(offsetDesc, "next_offset") || !strings.Contains(offsetDesc, "has_more is false") {
		t.Fatalf("offset description should explain pagination loop: %q", offsetDesc)
	}
}

func TestChannelHistoryResultsReturnCompactContentSnippet(t *testing.T) {
	rows := []audit.TimelineEvent{{
		Kind:       "discord",
		Type:       "message_create",
		ChannelID:  "channel-1",
		ThreadID:   "thread-1",
		MessageID:  "msg-1",
		UserID:     "user-1",
		Content:    strings.Repeat("keyword ", 100),
		RecordedAt: "2026-01-01T00:00:00Z",
	}}
	got := channelHistoryResults(rows)
	if len(got) != 1 {
		t.Fatalf("results = %d, want 1", len(got))
	}
	if got[0].ThreadID != "thread-1" || got[0].MessageID != "msg-1" {
		t.Fatalf("result metadata not preserved: %+v", got[0])
	}
	if !strings.Contains(got[0].ContentSnippet, "keyword") || len([]rune(got[0].ContentSnippet)) > 500 {
		t.Fatalf("snippet not compacted as expected: %q", got[0].ContentSnippet)
	}
}

func TestChannelHistoryPageReportsNextOffset(t *testing.T) {
	rows := []audit.TimelineEvent{{
		Kind:       "discord",
		Type:       "message_create",
		ChannelID:  "channel-1",
		MessageID:  "msg-1",
		Content:    "keyword",
		RecordedAt: "2026-01-01T00:00:00Z",
	}}
	got := channelHistoryPage("keyword", "channel-1", 1, 2, true, rows)
	if !got.HasMore || got.NextOffset != 3 || got.Returned != 1 {
		t.Fatalf("page metadata = %+v, want has_more next_offset=3 returned=1", got)
	}
}

func TestChannelHistoryPageCanReturnEmptyTerminalPage(t *testing.T) {
	got := channelHistoryPage("", "channel-1", 20, 40, false, nil)
	got.Message = "No stored channel history results."
	if got.HasMore || got.NextOffset != 0 || got.Returned != 0 || len(got.Results) != 0 {
		t.Fatalf("empty page metadata = %+v", got)
	}
	if got.Message == "" {
		t.Fatal("empty page should carry an explanatory message")
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

	reminderTool := writeTool("bot_create_reminder", "remind", false)
	if reminderTool.Annotations.ReadOnlyHint == nil || *reminderTool.Annotations.ReadOnlyHint {
		t.Fatalf("reminder readOnlyHint = %+v, want false", reminderTool.Annotations.ReadOnlyHint)
	}
	if reminderTool.Annotations.DestructiveHint == nil || *reminderTool.Annotations.DestructiveHint {
		t.Fatalf("reminder destructiveHint = %+v, want false", reminderTool.Annotations.DestructiveHint)
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

func TestBotToolsEgressDisabledReadsDynamicTargetState(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "target.json")
	if err := os.WriteFile(statePath, []byte(`{"target_channel_id":"thread-1","disable_egress":true}`), 0644); err != nil {
		t.Fatalf("write target state: %v", err)
	}
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", statePath)

	if !botToolsEgressDisabled() {
		t.Fatal("expected bot-tools egress to be disabled by dynamic target state")
	}
	if got := currentTargetStateChannelID(); got != "thread-1" {
		t.Fatalf("current target = %q, want thread-1", got)
	}
}

func TestA2ARequestContextUsesBoundRequesterState(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "target.json")
	if err := os.WriteFile(statePath, []byte(`{"target_channel_id":"thread-1","requester_id":"user-1","requester_name":"alice"}`), 0644); err != nil {
		t.Fatalf("write target state: %v", err)
	}
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", statePath)

	req := A2AToolRequest{GuildID: "guild-1", ChannelID: "channel-1"}
	req.RequestedByID = "user-1"
	req.RequestedBy = "alice"
	svc := &A2AService{cfg: A2AServiceConfig{Config: a2a.Config{AgentID: "adam-n200"}, BoundGuildID: "guild-1", BoundChannelID: "channel-1"}}
	if err := svc.validateContext(req, false); err != nil {
		t.Fatalf("bound requester rejected: %v", err)
	}

	req.RequestedByID = "mallory"
	if err := svc.validateContext(req, false); err == nil {
		t.Fatal("spoofed requester accepted despite bound target state")
	}
}

func TestAuthenticatedA2AMCPManageChannelsUsesTargetState(t *testing.T) {
	if authenticatedA2AMCPManageChannels() {
		t.Fatal("manage_channels accepted without target state")
	}

	dir := t.TempDir()
	statePath := filepath.Join(dir, "target.json")
	if err := os.WriteFile(statePath, []byte(`{"target_channel_id":"channel-1","can_manage_channel":true}`), 0644); err != nil {
		t.Fatalf("write target state: %v", err)
	}
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", statePath)

	if !authenticatedA2AMCPManageChannels() {
		t.Fatal("manage_channels not derived from authenticated target state")
	}
}

func TestA2ARemoteMemoryWriteTargetStateDefaultsDenied(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "target.json")
	if err := os.WriteFile(statePath, []byte(`{"target_channel_id":"thread-1","remote_a2a":true}`), 0644); err != nil {
		t.Fatalf("write target state: %v", err)
	}
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", statePath)

	if remoteA2AMemoryWriteAllowed() {
		t.Fatal("remote A2A memory write allowed without explicit policy")
	}
}

func TestA2ARemoteMemoryWriteTargetStateAllowsPolicyOptIn(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "target.json")
	if err := os.WriteFile(statePath, []byte(`{"target_channel_id":"thread-1","remote_a2a":true,"allow_memory_write":true}`), 0644); err != nil {
		t.Fatalf("write target state: %v", err)
	}
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", statePath)

	if !remoteA2AMemoryWriteAllowed() {
		t.Fatal("remote A2A memory write was denied despite explicit policy")
	}
}

func TestValidateMentionUserIDUsesDynamicTargetAllowlist(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "target.json")
	if err := os.WriteFile(statePath, []byte(`{"target_channel_id":"thread-1","allowed_mention_user_ids":["user-1"]}`), 0644); err != nil {
		t.Fatalf("write target state: %v", err)
	}
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", statePath)

	if err := validateMentionUserID("user-1"); err != nil {
		t.Fatalf("allowed mention rejected: %v", err)
	}
	if err := validateMentionUserID("user-2"); err == nil {
		t.Fatal("unverified mention_user_id accepted")
	}
}

func TestValidateMentionUserIDFailsClosedWithoutAllowlist(t *testing.T) {
	if err := validateMentionUserID(""); err != nil {
		t.Fatalf("empty mention should be allowed: %v", err)
	}
	if err := validateMentionUserID("user-1"); err == nil {
		t.Fatal("mention_user_id accepted without target state")
	}

	dir := t.TempDir()
	statePath := filepath.Join(dir, "target.json")
	if err := os.WriteFile(statePath, []byte(`{"target_channel_id":"thread-1"}`), 0644); err != nil {
		t.Fatalf("write target state: %v", err)
	}
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", statePath)
	if err := validateMentionUserID("user-1"); err == nil {
		t.Fatal("mention_user_id accepted without a non-empty allowlist")
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
	if err := writePending(dir, pendingAction{
		Action: "create_reminder",
		Job: &pendingJob{
			Name:      "bad reminder",
			Prompt:    "drink water",
			ChannelID: "ch-1",
			GuildID:   "guild-1",
			NextRun:   "not-rfc3339",
		},
	}); err == nil {
		t.Fatal("writePending accepted invalid reminder next_run")
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

func TestWritePendingUpdateCron(t *testing.T) {
	dir := t.TempDir()
	disabled := false
	newPrompt := "Run safely"
	if err := writePending(dir, pendingAction{
		Action:    "update",
		JobID:     "job-1",
		ChannelID: "ch-1",
		Update:    &heartbeat.CronUpdate{Enabled: &disabled, Prompt: &newPrompt},
	}); err != nil {
		t.Fatalf("writePending update: %v", err)
	}

	pendingDir := filepath.Join(dir, "cron", "pending")
	entries, err := os.ReadDir(pendingDir)
	if err != nil {
		t.Fatalf("read pending dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 pending file, got %d", len(entries))
	}
	raw, err := os.ReadFile(filepath.Join(pendingDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("read pending update: %v", err)
	}
	var got pendingAction
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode pending update: %v", err)
	}
	if got.Action != "update" || got.JobID != "job-1" || got.ChannelID != "ch-1" || got.Update == nil || got.Update.Enabled == nil || *got.Update.Enabled || got.Update.Prompt == nil || *got.Update.Prompt != newPrompt {
		t.Fatalf("unexpected pending update: %+v", got)
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
			Name:        "daily-report",
			Schedule:    "0 9 * * *",
			Prompt:      "Generate report",
			ChannelID:   ownerChannelID,
			GuildID:     "g1",
			CreatedByID: "user-1",
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
	if strings.Contains(string(raw), `"thread-1"`) || !strings.Contains(string(raw), `"channel_id":"channel-1"`) || !strings.Contains(string(raw), `"created_by_id":"user-1"`) {
		t.Fatalf("pending create should be parent-scoped, got %s", raw)
	}
}

func TestWritePendingCreateReminderTargetsCurrentThread(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "target.json")
	if err := os.WriteFile(statePath, []byte(`{"target_channel_id":"thread-1"}`), 0644); err != nil {
		t.Fatalf("write target state: %v", err)
	}
	t.Setenv("BOT_TOOLS_CHANNEL_ID", "channel-1")
	t.Setenv("BOT_TOOLS_TARGET_CHANNEL_ID", "channel-1")
	t.Setenv("BOT_TOOLS_TARGET_STATE_PATH", statePath)

	if err := validateBoundChannel("thread-1"); err != nil {
		t.Fatalf("thread target rejected: %v", err)
	}
	if err := writePending(dir, pendingAction{
		Action: "create_reminder",
		Job: &pendingJob{
			Name:          "drink-water",
			ScheduleHuman: "+2m",
			Prompt:        "drink water",
			ChannelID:     deliveryChannelID("channel-1"),
			GuildID:       "g1",
			CreatedByID:   "user-1",
			NextRun:       "2026-07-06T14:30:00+08:00",
			MentionID:     "user-1",
			OneShot:       true,
		},
	}); err != nil {
		t.Fatalf("writePending reminder: %v", err)
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
	if !strings.Contains(string(raw), `"action":"create_reminder"`) || !strings.Contains(string(raw), `"channel_id":"thread-1"`) || !strings.Contains(string(raw), `"mention_id":"user-1"`) || !strings.Contains(string(raw), `"created_by_id":"user-1"`) {
		t.Fatalf("pending reminder should target the current thread and preserve mention id, got %s", raw)
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
