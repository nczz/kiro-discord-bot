package botmcp

import (
	"os"
	"path/filepath"
	"testing"
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
	if err := os.MkdirAll(filepath.Join(dir, "cron"), 0755); err != nil {
		t.Fatalf("mkdir cron: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cron", "cron.json"), []byte(`[]`), 0644); err != nil {
		t.Fatalf("write cron: %v", err)
	}

	s, err := dataSummary(dir)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if !s.SessionsFile || !s.CronStore || s.ChannelDirs != 1 {
		t.Fatalf("unexpected summary: %+v", s)
	}

	rows, err := listChannelData(dir)
	if err != nil {
		t.Fatalf("list channels: %v", err)
	}
	if len(rows) != 1 || rows[0].ChannelID != "channel-1" || !rows[0].ChatLog || !rows[0].MemoryFile {
		t.Fatalf("unexpected channel rows: %+v", rows)
	}
}

func TestNewServerExists(t *testing.T) {
	if NewServer() == nil {
		t.Fatal("NewServer returned nil")
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
