package botegress

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nczz/kiro-discord-bot/internal/secrets"
)

const (
	MaxSanitizableFileBytes int64 = 5 * 1024 * 1024
	MaxValidatedImageBytes  int64 = 25 * 1024 * 1024
	MaxValidatedImagePixels       = 40_000_000
)

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
	if prepared, ok, err := prepareValidatedImage(abs, redactor, tempRoot); ok || err != nil {
		return prepared, err
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
	displayName := safeDisplayName(filepath.Base(abs), redactor)
	outPath, err := writeSanitizedTempFile(tempRoot, "sanitized-file", displayName, []byte(redacted))
	if err != nil {
		return SanitizedFile{}, fmt.Errorf("write sanitized file: %w", err)
	}
	return SanitizedFile{
		Path:           outPath,
		DisplayName:    displayName,
		RedactionCount: strings.Count(redacted, "[REDACTED"),
		SensitivePath:  isSensitivePath(abs),
	}, nil
}

func prepareValidatedImage(abs string, redactor *secrets.Redactor, tempRoot string) (SanitizedFile, bool, error) {
	f, err := os.Open(abs)
	if err != nil {
		return SanitizedFile{}, false, nil
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		return SanitizedFile{}, false, nil
	}
	header := make([]byte, 512)
	n, err := f.Read(header)
	if err != nil && err != io.EOF {
		return SanitizedFile{}, false, nil
	}
	wantFormat, ok := validatedImageContentTypes[http.DetectContentType(header[:n])]
	if !ok {
		return SanitizedFile{}, false, nil
	}
	if info.Size() > MaxValidatedImageBytes {
		return SanitizedFile{}, true, fmt.Errorf("image exceeds upload size limit (%d bytes)", MaxValidatedImageBytes)
	}
	if _, err := f.Seek(0, 0); err != nil {
		return SanitizedFile{}, true, fmt.Errorf("rewind image file: %w", err)
	}
	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		return SanitizedFile{}, true, fmt.Errorf("invalid image file: %w", err)
	}
	if format != wantFormat || cfg.Width <= 0 || cfg.Height <= 0 {
		return SanitizedFile{}, true, fmt.Errorf("invalid image file")
	}
	if int64(cfg.Width) > MaxValidatedImagePixels/int64(cfg.Height) {
		return SanitizedFile{}, true, fmt.Errorf("image dimensions exceed upload limit (%d pixels)", MaxValidatedImagePixels)
	}
	if _, err := f.Seek(0, 0); err != nil {
		return SanitizedFile{}, true, fmt.Errorf("rewind image file: %w", err)
	}
	displayName := validatedImageDisplayName(filepath.Base(abs), wantFormat, redactor)
	out, err := createSanitizedTempFile(tempRoot, "validated-image", displayName)
	if err != nil {
		return SanitizedFile{}, true, fmt.Errorf("write validated image: %w", err)
	}
	outPath := out.Name()
	written, copyErr := io.Copy(out, io.LimitReader(f, MaxValidatedImageBytes+1))
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(outPath)
		return SanitizedFile{}, true, fmt.Errorf("write validated image: %w", copyErr)
	}
	if written > MaxValidatedImageBytes {
		_ = os.Remove(outPath)
		return SanitizedFile{}, true, fmt.Errorf("image exceeds upload size limit (%d bytes)", MaxValidatedImageBytes)
	}
	if closeErr != nil {
		_ = os.Remove(outPath)
		return SanitizedFile{}, true, fmt.Errorf("write validated image: %w", closeErr)
	}
	return SanitizedFile{
		Path:          outPath,
		DisplayName:   displayName,
		SensitivePath: isSensitivePath(abs),
	}, true, nil
}

var validatedImageContentTypes = map[string]string{
	"image/jpeg": "jpeg",
	"image/png":  "png",
}

// WriteValidatedImageBytes stages JPEG/PNG image bytes into a bot-controlled
// temporary source file. The safe egress task validates the file again before
// Discord upload; this early validation gives immediate tool feedback and avoids
// queueing oversized or malformed payloads.
func WriteValidatedImageBytes(data []byte, mimeType, filename, tempRoot string, redactor *secrets.Redactor) (string, error) {
	if redactor == nil {
		redactor = &secrets.Redactor{}
	}
	if len(data) == 0 {
		return "", fmt.Errorf("image data is required")
	}
	mimeType = strings.TrimSpace(strings.ToLower(mimeType))
	if mimeType == "" {
		mimeType = detectedImageMimeType(data)
	}
	format, err := validateImageBytes(data, mimeType)
	if err != nil {
		return "", err
	}
	displayName := validatedImageDisplayName(filename, format, redactor)
	dir := filepath.Join(tempRoot, randomID())
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create image staging dir: %w", err)
	}
	path := filepath.Join(dir, displayName)
	if err := os.WriteFile(path, data, 0600); err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("write staged image: %w", err)
	}
	return path, nil
}

func detectedImageMimeType(data []byte) string {
	sample := data
	if len(sample) > 512 {
		sample = sample[:512]
	}
	return strings.ToLower(http.DetectContentType(sample))
}
func validateImageBytes(data []byte, claimedMime string) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("image data is required")
	}
	if int64(len(data)) > MaxValidatedImageBytes {
		return "", fmt.Errorf("image exceeds upload size limit (%d bytes)", MaxValidatedImageBytes)
	}
	sample := data
	if len(sample) > 512 {
		sample = sample[:512]
	}
	actualMime := strings.ToLower(http.DetectContentType(sample))
	actualFormat, ok := validatedImageContentTypes[actualMime]
	if !ok {
		return "", fmt.Errorf("invalid image file: unsupported mime type %s", actualMime)
	}
	claimedFormat, ok := validatedImageContentTypes[normalizeImageMimeType(claimedMime)]
	if !ok {
		return "", fmt.Errorf("invalid image file: unsupported mime_type %s", claimedMime)
	}
	if claimedFormat != actualFormat {
		return "", fmt.Errorf("invalid image file: mime_type %s does not match detected %s", claimedMime, actualMime)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("invalid image file: %w", err)
	}
	if format != actualFormat || cfg.Width <= 0 || cfg.Height <= 0 {
		return "", fmt.Errorf("invalid image file")
	}
	if int64(cfg.Width) > MaxValidatedImagePixels/int64(cfg.Height) {
		return "", fmt.Errorf("image dimensions exceed upload limit (%d pixels)", MaxValidatedImagePixels)
	}
	return actualFormat, nil
}

func normalizeImageMimeType(mimeType string) string {
	mimeType = strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
	if mimeType == "image/jpg" {
		return "image/jpeg"
	}
	return mimeType
}

func writeSanitizedTempFile(tempRoot, prefix, displayName string, data []byte) (string, error) {
	out, err := createSanitizedTempFile(tempRoot, prefix, displayName)
	if err != nil {
		return "", err
	}
	outPath := out.Name()
	_, writeErr := out.Write(data)
	closeErr := out.Close()
	if writeErr != nil {
		_ = os.Remove(outPath)
		return "", writeErr
	}
	if closeErr != nil {
		_ = os.Remove(outPath)
		return "", closeErr
	}
	return outPath, nil
}

func createSanitizedTempFile(tempRoot, prefix, displayName string) (*os.File, error) {
	if err := os.MkdirAll(tempRoot, 0700); err != nil {
		return nil, fmt.Errorf("create sanitized temp dir: %w", err)
	}
	ext := filepath.Ext(displayName)
	return os.CreateTemp(tempRoot, prefix+"-*"+ext)
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
	displayName := extractedDisplayName(filepath.Base(abs), redactor)
	outPath, err := writeSanitizedTempFile(tempRoot, "extracted-file", displayName, []byte(redacted))
	if err != nil {
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

func validatedImageDisplayName(name, format string, redactor *secrets.Redactor) string {
	safe := safeDisplayName(name, redactor)
	ext := ".png"
	if format == "jpeg" {
		ext = ".jpg"
	}
	currentExt := strings.ToLower(filepath.Ext(safe))
	if (format == "jpeg" && (currentExt == ".jpg" || currentExt == ".jpeg")) || (format == "png" && currentExt == ".png") {
		return safe
	}
	base := strings.TrimSuffix(safe, filepath.Ext(safe))
	if strings.TrimSpace(base) == "" || safe == "redacted-file.txt" {
		base = "validated-image"
	}
	return base + ext
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
