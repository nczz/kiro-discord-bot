package botegress

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nczz/kiro-discord-bot/internal/secrets"
	"github.com/xuri/excelize/v2"
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

func TestPrepareSanitizedFileExtractsDOCXToRedactedText(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "report.docx")
	if err := os.WriteFile(source, buildDOCX(t, map[string]string{
		"word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>body kiro-secret-value</w:t></w:r></w:p></w:body></w:document>`,
		"word/header1.xml":  `<w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:r><w:t>header kiro-secret-value</w:t></w:r></w:p></w:hdr>`,
	}), 0644); err != nil {
		t.Fatal(err)
	}
	redactor := &secrets.Redactor{}
	redactor.Add("KIRO_API_KEY", "kiro-secret-value")

	prepared, err := PrepareSanitizedFile(source, redactor, filepath.Join(dir, "missing", "sanitized"))
	if err != nil {
		t.Fatalf("PrepareSanitizedFile DOCX: %v", err)
	}
	assertRedactedTextOutput(t, prepared, "report.redacted.txt")
}

func TestPrepareSanitizedFileExtractsXLSXToRedactedText(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "workbook.xlsx")
	if err := writeXLSX(source, "kiro-secret-value"); err != nil {
		t.Fatal(err)
	}
	redactor := &secrets.Redactor{}
	redactor.Add("KIRO_API_KEY", "kiro-secret-value")

	prepared, err := PrepareSanitizedFile(source, redactor, filepath.Join(dir, "sanitized"))
	if err != nil {
		t.Fatalf("PrepareSanitizedFile XLSX: %v", err)
	}
	assertRedactedTextOutput(t, prepared, "workbook.redacted.txt")
}

func TestPrepareSanitizedFileExtractsPDFToRedactedText(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "file.pdf")
	pdf := `%PDF-1.4
1 0 obj
<<>>
stream
BT (kiro-secret-value) Tj ET
endstream
endobj
%%EOF`
	if err := os.WriteFile(source, []byte(pdf), 0644); err != nil {
		t.Fatal(err)
	}
	redactor := &secrets.Redactor{}
	redactor.Add("KIRO_API_KEY", "kiro-secret-value")

	prepared, err := PrepareSanitizedFile(source, redactor, filepath.Join(dir, "sanitized"))
	if err != nil {
		t.Fatalf("PrepareSanitizedFile PDF: %v", err)
	}
	assertRedactedTextOutput(t, prepared, "file.redacted.txt")
}

func TestPrepareSanitizedFileExtractsFlatePDFToRedactedText(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "compressed.pdf")
	pdf := buildFlatePDF(t, "BT (kiro-secret-value) Tj ET")
	if err := os.WriteFile(source, pdf, 0644); err != nil {
		t.Fatal(err)
	}
	redactor := &secrets.Redactor{}
	redactor.Add("KIRO_API_KEY", "kiro-secret-value")

	prepared, err := PrepareSanitizedFile(source, redactor, filepath.Join(dir, "sanitized"))
	if err != nil {
		t.Fatalf("PrepareSanitizedFile compressed PDF: %v", err)
	}
	assertRedactedTextOutput(t, prepared, "compressed.redacted.txt")
}

func TestPrepareSanitizedFileRejectsDOCXExpandedTextOverLimit(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "large.docx")
	oversizedText := strings.Repeat("A", int(maxExtractedTextBytes)+1)
	if err := os.WriteFile(source, buildDOCX(t, map[string]string{
		"word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>` + oversizedText + `</w:t></w:r></w:p></w:body></w:document>`,
	}), 0644); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(source); err != nil {
		t.Fatal(err)
	} else if info.Size() >= MaxSanitizableFileBytes {
		t.Fatalf("test fixture should be compressed below source limit, got %d", info.Size())
	}

	_, err := PrepareSanitizedFile(source, &secrets.Redactor{}, filepath.Join(dir, "sanitized"))
	if err == nil || !strings.Contains(err.Error(), "exceeds extract limit") {
		t.Fatalf("PrepareSanitizedFile should reject expanded DOCX over limit, got %v", err)
	}
}

func TestPrepareSanitizedFileRejectsRedactedOutputOverLimit(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "large.txt")
	secret := "12345678"
	if err := os.WriteFile(source, []byte(strings.Repeat(secret, int(MaxSanitizableFileBytes)/len(secret))), 0644); err != nil {
		t.Fatal(err)
	}
	redactor := &secrets.Redactor{}
	redactor.Add("VERY_LONG_SECRET_NAME_THAT_EXPANDS_OUTPUT", secret)

	_, err := PrepareSanitizedFile(source, redactor, filepath.Join(dir, "sanitized"))
	if err == nil || !strings.Contains(err.Error(), "redacted file exceeds sanitizable size limit") {
		t.Fatalf("PrepareSanitizedFile should reject redacted output over limit, got %v", err)
	}
}

func TestPrepareSanitizedFileRedactsExtractedDisplayName(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "kiro-secret-value.docx")
	if err := os.WriteFile(source, buildDOCX(t, map[string]string{
		"word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>plain content</w:t></w:r></w:p></w:body></w:document>`,
	}), 0644); err != nil {
		t.Fatal(err)
	}
	redactor := &secrets.Redactor{}
	redactor.Add("SECRET_NAME", "kiro-secret-value")

	prepared, err := PrepareSanitizedFile(source, redactor, filepath.Join(dir, "sanitized"))
	if err != nil {
		t.Fatalf("PrepareSanitizedFile secret filename: %v", err)
	}
	if strings.Contains(prepared.DisplayName, "kiro-secret-value") || !strings.HasSuffix(prepared.DisplayName, ".redacted.txt") {
		t.Fatalf("unsafe extracted display name: %q", prepared.DisplayName)
	}
}

func buildFlatePDF(t *testing.T, streamText string) []byte {
	t.Helper()
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write([]byte(streamText)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n1 0 obj\n<< /Filter /FlateDecode >>\nstream\n")
	pdf.Write(compressed.Bytes())
	pdf.WriteString("\nendstream\nendobj\n%%EOF")
	return pdf.Bytes()
}

func assertRedactedTextOutput(t *testing.T, prepared SanitizedFile, wantName string) {
	t.Helper()
	if prepared.DisplayName != wantName {
		t.Fatalf("DisplayName = %q, want %q", prepared.DisplayName, wantName)
	}
	if filepath.Ext(prepared.Path) != ".txt" {
		t.Fatalf("extracted output path should be .txt, got %q", prepared.Path)
	}
	raw, err := os.ReadFile(prepared.Path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, "kiro-secret-value") {
		t.Fatalf("secret leaked in extracted sanitized file: %q", text)
	}
	if !strings.Contains(text, "[REDACTED") || prepared.RedactionCount == 0 {
		t.Fatalf("missing extracted redaction marker/count: count=%d text=%q", prepared.RedactionCount, text)
	}
}

func buildDOCX(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeXLSX(path string, secret string) error {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	if err := f.SetSheetName("Sheet1", "Visible"); err != nil {
		return err
	}
	if err := f.SetCellValue("Visible", "A1", "visible "+secret); err != nil {
		return err
	}
	hiddenIndex, err := f.NewSheet("Hidden")
	if err != nil {
		return err
	}
	if err := f.SetCellValue("Hidden", "A1", "hidden "+secret); err != nil {
		return err
	}
	if err := f.SetSheetVisible("Hidden", false); err != nil {
		return err
	}
	f.SetActiveSheet(hiddenIndex)
	return f.SaveAs(path)
}

func TestRedactSensitivePaths(t *testing.T) {
	input := `stat /tmp/work/.kiro/settings/mcp.json: no such file; open /tmp/work/public/readme.md: denied; read ~/.env.local: denied`
	got := RedactSensitivePaths(input)
	if strings.Contains(got, ".kiro") || strings.Contains(got, "mcp.json") || strings.Contains(got, ".env.local") {
		t.Fatalf("sensitive path leaked: %q", got)
	}
	if !strings.Contains(got, "/tmp/work/public/readme.md") {
		t.Fatalf("non-sensitive path should remain useful: %q", got)
	}
	if strings.Count(got, "[REDACTED:PATH]") != 2 {
		t.Fatalf("redacted path count mismatch: %q", got)
	}
}
