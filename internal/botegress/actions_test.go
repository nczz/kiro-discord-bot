package botegress

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nczz/kiro-discord-bot/internal/secrets"
)

func TestWriteReadRemovePending(t *testing.T) {
	dir := t.TempDir()
	id, err := WritePending(dir, Action{
		Action:    ActionSendMessage,
		ChannelID: "channel-1",
		Content:   "hello",
	})
	if err != nil {
		t.Fatalf("WritePending: %v", err)
	}
	actions, err := ReadPending(dir)
	if err != nil {
		t.Fatalf("ReadPending: %v", err)
	}
	if len(actions) != 1 || actions[0].ID != id || actions[0].Action != ActionSendMessage {
		t.Fatalf("unexpected actions: %+v", actions)
	}
	if err := RemovePending(dir, id); err != nil {
		t.Fatalf("RemovePending: %v", err)
	}
	actions, err = ReadPending(dir)
	if err != nil {
		t.Fatalf("ReadPending after remove: %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("actions after remove = %+v, want empty", actions)
	}
}

func TestWritePendingValidatesActions(t *testing.T) {
	if _, err := WritePending(t.TempDir(), Action{Action: ActionSendMessage, ChannelID: "ch"}); err == nil {
		t.Fatal("WritePending accepted message without content")
	}
	if _, err := WritePending(t.TempDir(), Action{Action: ActionSendFile, ChannelID: "ch"}); err == nil {
		t.Fatal("WritePending accepted file without file_path")
	}
}

func TestPrepareSanitizedFileRedactsText(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, ".env")
	if err := os.WriteFile(source, []byte("KIRO_API_KEY=kiro-secret-value\nplain=ok\n"), 0644); err != nil {
		t.Fatal(err)
	}
	redactor := &secrets.Redactor{}
	redactor.Add("KIRO_API_KEY", "kiro-secret-value")

	prepared, err := PrepareSanitizedFile(source, redactor, filepath.Join(dir, "sanitized"))
	if err != nil {
		t.Fatalf("PrepareSanitizedFile: %v", err)
	}
	raw, err := os.ReadFile(prepared.Path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, "kiro-secret-value") {
		t.Fatalf("secret leaked in sanitized file: %q", text)
	}
	if !strings.Contains(text, "[REDACTED") {
		t.Fatalf("sanitized file missing redaction marker: %q", text)
	}
	if !prepared.SensitivePath {
		t.Fatalf(".env should be marked as sensitive path: %+v", prepared)
	}
}

func TestPrepareSanitizedFileRejectsBinary(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "blob.bin")
	if err := os.WriteFile(source, []byte{0, 1, 2, 3}, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareSanitizedFile(source, &secrets.Redactor{}, filepath.Join(dir, "sanitized")); err == nil {
		t.Fatal("PrepareSanitizedFile accepted binary file")
	}
}
