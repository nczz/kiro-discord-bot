package botegress

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nczz/kiro-discord-bot/internal/secrets"
)

const MaxSanitizableFileBytes int64 = 5 * 1024 * 1024

// extractableBinaryExt lists document extensions that should be converted to
// redacted text output instead of treated as raw text.
var extractableBinaryExt = map[string]bool{
	".pdf":  true,
	".docx": true,
	".xlsx": true,
}

var textExtensions = map[string]bool{
	".bash": true, ".cfg": true, ".conf": true, ".csv": true, ".env": true,
	".go": true, ".ini": true, ".json": true, ".log": true, ".md": true,
	".py": true, ".sh": true, ".sql": true, ".text": true, ".toml": true,
	".tsv": true, ".txt": true, ".xml": true, ".yaml": true, ".yml": true,
}

var sensitivePathFragments = []string{
	"/.env",
	"/.kiro/",
	"/kiro-runtime/",
	"/settings/mcp.json",
	"/credentials",
	"/id_rsa",
	"/id_ed25519",
	"/discord",
}

var pathLikePattern = regexp.MustCompile(`(?:~|/)[^\s"'<>]+`)

// SanitizedFile is a temporary redacted file ready for Discord upload.
type SanitizedFile struct {
	Path           string
	DisplayName    string
	RedactionCount int
	SensitivePath  bool
}

func PrepareSanitizedFile(path string, redactor *secrets.Redactor, tempRoot string) (SanitizedFile, error) {
	if redactor == nil {
		redactor = &secrets.Redactor{}
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return SanitizedFile{}, fmt.Errorf("file_path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return SanitizedFile{}, fmt.Errorf("resolve file path: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return SanitizedFile{}, fmt.Errorf("stat file: %w", err)
	}
	if info.IsDir() {
		return SanitizedFile{}, fmt.Errorf("directories cannot be sent as files")
	}
	if info.Size() > MaxSanitizableFileBytes {
		return SanitizedFile{}, fmt.Errorf("file exceeds sanitizable size limit (%d bytes)", MaxSanitizableFileBytes)
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return SanitizedFile{}, fmt.Errorf("read file: %w", err)
	}
	if !isTextFile(abs, raw) {
		// Try extraction for supported binary formats
		ext := strings.ToLower(filepath.Ext(abs))
		if extractableBinaryExt[ext] {
			return prepareExtractedFile(abs, redactor, tempRoot)
		}
		return SanitizedFile{}, fmt.Errorf("file type is not safely redactable as text")
	}
	original := string(raw)
	redacted := redactor.Redact(original)
	if int64(len(redacted)) > MaxSanitizableFileBytes {
		return SanitizedFile{}, fmt.Errorf("redacted file exceeds sanitizable size limit (%d bytes)", MaxSanitizableFileBytes)
	}
	if err := os.MkdirAll(tempRoot, 0700); err != nil {
		return SanitizedFile{}, fmt.Errorf("create sanitized temp dir: %w", err)
	}
	displayName := safeDisplayName(filepath.Base(abs), redactor)
	outPath := filepath.Join(tempRoot, displayName)
	if err := os.WriteFile(outPath, []byte(redacted), 0600); err != nil {
		return SanitizedFile{}, fmt.Errorf("write sanitized file: %w", err)
	}
	return SanitizedFile{
		Path:           outPath,
		DisplayName:    displayName,
		RedactionCount: strings.Count(redacted, "[REDACTED"),
		SensitivePath:  isSensitivePath(abs),
	}, nil
}

// prepareExtractedFile extracts readable text from a supported document format
// (PDF/DOCX/XLSX), redacts secrets, and writes a sanitized .txt copy. It never
// uploads the original binary document back to Discord.
func prepareExtractedFile(abs string, redactor *secrets.Redactor, tempRoot string) (SanitizedFile, error) {
	ext := strings.ToLower(filepath.Ext(abs))

	result, err := ExtractFile(abs)
	if err != nil {
		return SanitizedFile{}, fmt.Errorf("extract readable text (%s): %w", ext, err)
	}
	redacted := redactor.Redact(result.Text)
	if int64(len(redacted)) > MaxSanitizableFileBytes {
		return SanitizedFile{}, fmt.Errorf("redacted extracted file exceeds sanitizable size limit (%d bytes)", MaxSanitizableFileBytes)
	}
	if err := os.MkdirAll(tempRoot, 0700); err != nil {
		return SanitizedFile{}, fmt.Errorf("create sanitized temp dir: %w", err)
	}
	displayName := extractedDisplayName(filepath.Base(abs), redactor)
	outPath := filepath.Join(tempRoot, displayName)
	if err := os.WriteFile(outPath, []byte(redacted), 0600); err != nil {
		return SanitizedFile{}, fmt.Errorf("write extracted sanitized file: %w", err)
	}

	return SanitizedFile{
		Path:           outPath,
		DisplayName:    displayName,
		RedactionCount: strings.Count(redacted, "[REDACTED"),
		SensitivePath:  isSensitivePath(abs),
	}, nil
}

func extractedDisplayName(name string, redactor *secrets.Redactor) string {
	safe := safeDisplayName(name, redactor)
	if strings.Contains(safe, "[REDACTED") || safe == "" {
		return "redacted-file.redacted.txt"
	}
	ext := filepath.Ext(safe)
	base := strings.TrimSuffix(safe, ext)
	if strings.TrimSpace(base) == "" {
		base = "document"
	}
	return base + ".redacted.txt"
}

func isTextFile(path string, raw []byte) bool {
	if bytes.IndexByte(raw, 0) >= 0 {
		return false
	}
	ext := strings.ToLower(filepath.Ext(path))
	base := strings.ToLower(filepath.Base(path))
	if textExtensions[ext] || base == ".env" || strings.HasPrefix(base, ".env.") {
		return true
	}
	sample := raw
	if len(sample) > 512 {
		sample = sample[:512]
	}
	ctype := http.DetectContentType(sample)
	return strings.HasPrefix(ctype, "text/")
}

func safeDisplayName(name string, redactor *secrets.Redactor) string {
	if redactor == nil {
		redactor = &secrets.Redactor{}
	}
	name = filepath.Base(strings.TrimSpace(name))
	if name == "." || name == "" {
		name = "file"
	}
	name = redactor.Redact(name)
	name = strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(name)
	if name == "" || strings.Contains(name, "[REDACTED") {
		return "redacted-file.txt"
	}
	return name
}

func isSensitivePath(path string) bool {
	clean := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	for _, frag := range sensitivePathFragments {
		if strings.Contains(clean, frag) {
			return true
		}
	}
	ext := strings.ToLower(filepath.Ext(clean))
	return ext == ".pem" || ext == ".key" || ext == ".p12" || ext == ".pfx"
}

// RedactSensitivePaths replaces sensitive filesystem paths embedded in text.
// It is used for user-visible failure messages where os.PathError may include
// paths that identify secret-bearing files even when file contents are never sent.
func RedactSensitivePaths(text string) string {
	if text == "" {
		return ""
	}
	return pathLikePattern.ReplaceAllStringFunc(text, func(candidate string) string {
		trimmed := strings.TrimRight(candidate, ".,;:)]}")
		suffix := strings.TrimPrefix(candidate, trimmed)
		if trimmed == "" || !isSensitivePath(trimmed) {
			return candidate
		}
		return "[REDACTED:PATH]" + suffix
	})
}
