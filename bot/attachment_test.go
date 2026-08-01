package bot

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestSafeAttachmentFilename(t *testing.T) {
	tests := map[string]string{
		"report.txt":     "report.txt",
		"../secret.txt":  "secret.txt",
		"..\\secret.txt": "secret.txt",
		"spaces ok.md":   "spaces ok.md",
		"semi;colon.sh":  "semi_colon.sh",
		"中文檔名.png":       "____.png",
		"   ...   ":      "attachment",
	}

	for in, want := range tests {
		if got := safeAttachmentFilename(in); got != want {
			t.Fatalf("safeAttachmentFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDownloadAttachmentsUsesProjectCWD(t *testing.T) {
	projectDir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("xlsx"))
	}))
	defer srv.Close()

	b := &Bot{downloadClient: srv.Client(), attachmentMaxBytes: 1024}
	paths := b.downloadAttachments(projectDir, "message-1", []*discordgo.MessageAttachment{{
		URL:      srv.URL,
		Filename: "../report.xlsx",
		Size:     4,
	}})
	if len(paths) != 1 {
		t.Fatalf("downloaded paths = %v, want 1 path", paths)
	}
	wantPrefix := filepath.Join(projectDir, ".kiro-bot", "attachments", "message-1") + string(os.PathSeparator)
	if !strings.HasPrefix(paths[0], wantPrefix) {
		t.Fatalf("attachment path = %q, want under %q", paths[0], wantPrefix)
	}
	if strings.Contains(paths[0], filepath.Join("data", "ch-")) {
		t.Fatalf("attachment path exposes bot data layout: %q", paths[0])
	}
	if got, err := os.ReadFile(paths[0]); err != nil || string(got) != "xlsx" {
		t.Fatalf("downloaded content = %q, err=%v", got, err)
	}
	if filepath.Base(paths[0]) != "report.xlsx" && !strings.HasSuffix(filepath.Base(paths[0]), "-report.xlsx") {
		t.Fatalf("filename was not sanitized as expected: %q", filepath.Base(paths[0]))
	}
}
