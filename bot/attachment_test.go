package bot

import "testing"

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
