package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

var safeSlugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,79}$`)

func NormalizeSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		keep := false
		switch {
		case r >= 'a' && r <= 'z':
			keep = true
		case r >= '0' && r <= '9':
			keep = true
		case r == '-' || r == '_' || unicode.IsSpace(r):
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
			continue
		}
		if keep {
			b.WriteRune(r)
			lastDash = false
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 80 {
		out = strings.Trim(out[:80], "-")
	}
	return out
}

func ValidateSlug(slug string) error {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return fmt.Errorf("skill slug is required")
	}
	if strings.Contains(slug, "..") || strings.ContainsAny(slug, `/\\:`) || strings.HasPrefix(slug, ".") {
		return fmt.Errorf("unsafe skill slug %q", slug)
	}
	if !safeSlugRE.MatchString(slug) {
		return fmt.Errorf("unsafe skill slug %q: use lowercase letters, digits, and '-' only", slug)
	}
	return nil
}

func NormalizeScope(scope string) string {
	switch strings.TrimSpace(strings.ToLower(scope)) {
	case ScopeGuild, "server":
		return ScopeGuild
	case ScopeChannel:
		return ScopeChannel
	case ScopeProject:
		return ScopeProject
	case ScopeChannelProject, "channel-project":
		return ScopeChannelProject
	default:
		return ""
	}
}

func ValidateScope(scope string) error {
	if NormalizeScope(scope) == "" {
		return fmt.Errorf("unsupported skill scope %q", scope)
	}
	return nil
}

func NormalizeSourceType(source string) string {
	switch strings.TrimSpace(strings.ToLower(source)) {
	case SourceConversation, SourceMarkdown, SourceURL, SourceGitHubRepo, SourceManual, SourceBuiltin:
		return strings.TrimSpace(strings.ToLower(source))
	case "github", "github-repo":
		return SourceGitHubRepo
	default:
		return SourceManual
	}
}

func ProjectCWDHash(cwd string) string {
	clean := filepath.Clean(strings.TrimSpace(cwd))
	if clean == "." || clean == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(clean))
	return hex.EncodeToString(sum[:])
}

func ContentSHA256(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func normalizeToolNames(tools []string) []string {
	seen := make(map[string]bool, len(tools))
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		tool = strings.TrimSpace(tool)
		if tool == "" || seen[tool] {
			continue
		}
		seen[tool] = true
		out = append(out, tool)
	}
	sort.Strings(out)
	return out
}

func scopePrecedence(scope string) int {
	switch NormalizeScope(scope) {
	case ScopeChannelProject:
		return 4
	case ScopeChannel:
		return 3
	case ScopeProject:
		return 2
	case ScopeGuild:
		return 1
	default:
		return 0
	}
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "1.0.0"
	}
	return v
}

func normalizeRisk(r string) string {
	switch strings.TrimSpace(strings.ToLower(r)) {
	case "low", "medium", "high", "critical":
		return strings.TrimSpace(strings.ToLower(r))
	default:
		return "low"
	}
}
