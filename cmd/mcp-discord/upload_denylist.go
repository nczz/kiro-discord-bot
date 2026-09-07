package main

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var errDiscordUploadDenied = errors.New("file blocked by Discord MCP upload denylist")

func discordUploadDenied(filePath string) bool {
	abs, err := filepath.Abs(filePath)
	if err != nil {
		abs = filePath
	}
	candidates := []string{abs}
	if realPath, err := filepath.EvalSymlinks(abs); err == nil && realPath != abs {
		candidates = append(candidates, realPath)
	}
	patterns := discordUploadDenyPatterns()
	insensitive := discordUploadDenyCaseInsensitive()
	for _, candidate := range candidates {
		for _, pattern := range patterns {
			if matchUploadDenyPattern(pattern, candidate, insensitive) {
				return true
			}
		}
	}
	return false
}

func discordUploadDenyPatterns() []string {
	seen := make(map[string]bool)
	var patterns []string
	add := func(pattern string) {
		pattern = normalizeUploadDenyPattern(pattern)
		if pattern == "" || seen[pattern] {
			return
		}
		seen[pattern] = true
		patterns = append(patterns, pattern)
	}
	addRoot := func(root string) {
		root = strings.TrimSpace(root)
		if root == "" {
			return
		}
		add(filepath.Join(root, "**"))
		if realRoot, err := filepath.EvalSymlinks(root); err == nil && realRoot != root {
			add(filepath.Join(realRoot, "**"))
		}
	}
	addFile := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		add(path)
		if realPath, err := filepath.EvalSymlinks(path); err == nil && realPath != path {
			add(realPath)
		}
	}

	if dataDir := strings.TrimSpace(os.Getenv("DATA_DIR")); dataDir != "" {
		addRoot(dataDir)
	} else {
		addRoot("./data")
	}
	addRoot(os.Getenv("KIRO_HOME"))
	addFile(os.Getenv("KIRO_MCP_CONFIG"))
	addRoot(os.Getenv("OMP_HOME"))
	addRoot(os.Getenv("OMP_SESSION_DIR"))

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		addRoot(filepath.Join(home, ".kiro"))
		addRoot(filepath.Join(home, ".config", "omp"))
	}
	if cwd, err := os.Getwd(); err == nil && looksLikeBotDeploymentDir(cwd) {
		addRoot(cwd)
	}
	if exe, err := os.Executable(); err == nil && exe != "" {
		exeDir := filepath.Dir(exe)
		addRoot(exeDir)
		if filepath.Base(exeDir) == "bin" {
			parent := filepath.Dir(exeDir)
			if looksLikeBotDeploymentDir(parent) {
				addRoot(parent)
			}
		}
	}

	for _, pattern := range splitUploadDenyEnv(os.Getenv("MCP_DISCORD_UPLOAD_DENY_PATHS")) {
		pattern = normalizeUploadDenyPatternEnv(pattern)
		add(pattern)
		add(resolvedUploadDenyPatternPrefix(pattern))
	}
	return patterns
}

func looksLikeBotDeploymentDir(dir string) bool {
	if dir == "" {
		return false
	}
	if strings.Contains(filepath.Base(dir), "kiro-discord-bot") {
		return true
	}
	if _, err := os.Stat(filepath.Join(dir, ".env")); err != nil {
		return false
	}
	for _, rel := range []string{
		filepath.Join("bin", "kiro-discord-bot"),
		filepath.Join("bin", "mcp-discord"),
		filepath.Join("bin", "mcp-discord-server"),
		"kiro-discord-bot",
		"mcp-discord",
		"mcp-discord-server",
	} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err == nil {
			return true
		}
	}
	return false
}

func splitUploadDenyEnv(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if field = strings.TrimSpace(field); field != "" {
			out = append(out, field)
		}
	}
	return out
}

func normalizeUploadDenyPattern(pattern string) string {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return ""
	}
	if !filepath.IsAbs(pattern) {
		if abs, err := filepath.Abs(pattern); err == nil {
			pattern = abs
		}
	}
	return filepath.ToSlash(filepath.Clean(pattern))
}

func normalizeUploadDenyPatternEnv(pattern string) string {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return ""
	}
	pattern, ok := expandUploadDenyPatternEnv(pattern)
	if !ok {
		return ""
	}
	pattern = expandUploadDenyTilde(pattern)
	return normalizeUploadDenyPattern(pattern)
}

func expandUploadDenyTilde(pattern string) string {
	if pattern != "~" && !strings.HasPrefix(pattern, "~/") && !strings.HasPrefix(pattern, `~`+string(filepath.Separator)) {
		return pattern
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if pattern == "~" {
			return home
		}
		return filepath.Join(home, pattern[2:])
	}
	return pattern
}

func expandUploadDenyPatternEnv(pattern string) (string, bool) {
	missing := false
	expanded := os.Expand(pattern, func(key string) string {
		value, ok := os.LookupEnv(key)
		if !ok || strings.TrimSpace(value) == "" {
			missing = true
			return ""
		}
		return value
	})
	if missing {
		return "", false
	}
	return expanded, true
}

func resolvedUploadDenyPatternPrefix(pattern string) string {
	pattern = normalizeUploadDenyPattern(pattern)
	if pattern == "" {
		return ""
	}
	if !strings.ContainsAny(pattern, "*?") {
		real, err := filepath.EvalSymlinks(filepath.FromSlash(pattern))
		if err != nil {
			return ""
		}
		real = filepath.ToSlash(filepath.Clean(real))
		if real == pattern {
			return ""
		}
		return real
	}
	wildcard := strings.IndexAny(pattern, "*?")
	slash := strings.LastIndex(pattern[:wildcard], "/")
	if slash <= 0 {
		return ""
	}
	root := pattern[:slash]
	suffix := pattern[len(root):]
	real, err := filepath.EvalSymlinks(filepath.FromSlash(root))
	if err != nil {
		return ""
	}
	real = filepath.ToSlash(filepath.Clean(real))
	if real == root {
		return ""
	}
	return real + suffix
}

func normalizeUploadDenyCandidate(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return filepath.ToSlash(filepath.Clean(path))
}

func discordUploadDenyCaseInsensitive() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("MCP_DISCORD_UPLOAD_DENY_CASE_INSENSITIVE")))
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return runtime.GOOS == "darwin" || runtime.GOOS == "windows"
}

func matchUploadDenyPattern(pattern, candidate string, caseInsensitive bool) bool {
	pattern = normalizeUploadDenyPattern(pattern)
	candidate = normalizeUploadDenyCandidate(candidate)
	if pattern == "" || candidate == "" {
		return false
	}
	if caseInsensitive {
		pattern = strings.ToLower(pattern)
		candidate = strings.ToLower(candidate)
	}
	if strings.HasSuffix(pattern, "/**") && candidate == strings.TrimSuffix(pattern, "/**") {
		return true
	}
	if !strings.ContainsAny(pattern, "*?") {
		return pattern == candidate
	}
	re, err := uploadDenyGlobRegexp(pattern)
	if err != nil {
		return false
	}
	return re.MatchString(candidate)
}

func uploadDenyGlobRegexp(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteByte('^')
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				if i+2 < len(pattern) && pattern[i+2] == '/' {
					b.WriteString("(?:.*/)?")
					i += 2
				} else {
					b.WriteString(".*")
					i++
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(pattern[i : i+1]))
		}
	}
	b.WriteByte('$')
	return regexp.Compile(b.String())
}
