package a2a

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode"
)

type RuntimeIDMode string

const (
	RuntimeIDModeLegacy  RuntimeIDMode = "legacy"
	RuntimeIDModeDual    RuntimeIDMode = "dual"
	RuntimeIDModeRuntime RuntimeIDMode = "runtime"
)

func NormalizeRuntimeIDMode(raw string) (RuntimeIDMode, error) {
	switch RuntimeIDMode(strings.ToLower(strings.TrimSpace(raw))) {
	case "", RuntimeIDModeLegacy:
		return RuntimeIDModeLegacy, nil
	case RuntimeIDModeDual:
		return RuntimeIDModeDual, nil
	case RuntimeIDModeRuntime:
		return RuntimeIDModeRuntime, nil
	default:
		return "", fmt.Errorf("A2A_RUNTIME_ID_MODE must be legacy, dual, or runtime")
	}
}

func (m RuntimeIDMode) String() string {
	if m == "" {
		return string(RuntimeIDModeLegacy)
	}
	return string(m)
}

func (m RuntimeIDMode) UsesRuntimeIDs() bool {
	return m == RuntimeIDModeDual || m == RuntimeIDModeRuntime
}

type RuntimeRecord struct {
	RuntimeAgentID AgentID   `json:"runtimeAgentId"`
	BotAgentID     AgentID   `json:"botAgentId"`
	GuildID        string    `json:"guildId"`
	ChannelID      string    `json:"channelId"`
	ThreadID       string    `json:"threadId,omitempty"`
	ChannelRef     string    `json:"channelRef"`
	DisplayName    string    `json:"displayName,omitempty"`
	RuntimeKind    string    `json:"runtimeKind"`
	Enabled        bool      `json:"enabled"`
	Discoverable   bool      `json:"discoverable"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

func GenerateRuntimeAgentID(bot AgentID, channelRef string) (AgentID, error) {
	if err := ValidateAgentID(bot); err != nil {
		return "", err
	}
	base := string(bot)
	slug := runtimeSlug(channelRef)
	if slug != "" && !digitsPattern.MatchString(slug) {
		candidate := base + "-" + slug
		if len(candidate) <= 64 {
			id := AgentID(candidate)
			if err := ValidateAgentID(id); err == nil {
				return id, nil
			}
		}
	}
	sum := sha256.Sum256([]byte(base + "\x00" + strings.TrimSpace(channelRef)))
	hash := hex.EncodeToString(sum[:6])
	prefix := base
	maxPrefix := 64 - len("-rt-") - len(hash)
	if len(prefix) > maxPrefix {
		prefix = strings.TrimRight(prefix[:maxPrefix], "-_")
	}
	if prefix == "" {
		prefix = "rt"
	}
	id := AgentID(prefix + "-rt-" + hash)
	if err := ValidateAgentID(id); err != nil {
		return "", err
	}
	return id, nil
}

func runtimeSlug(raw string) string {
	raw = strings.TrimSpace(raw)
	var b strings.Builder
	lastSep := false
	for _, r := range raw {
		valid := r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
		if !valid {
			if b.Len() > 0 && !lastSep {
				b.WriteByte('-')
				lastSep = true
			}
			continue
		}
		if r == '-' || r == '_' {
			if b.Len() == 0 || lastSep {
				continue
			}
			b.WriteByte('-')
			lastSep = true
			continue
		}
		if r > unicode.MaxASCII {
			if b.Len() > 0 && !lastSep {
				b.WriteByte('-')
				lastSep = true
			}
			continue
		}
		b.WriteByte(byte(unicode.ToLower(r)))
		lastSep = false
	}
	return strings.Trim(b.String(), "-")
}

func validateRuntimeRecord(r RuntimeRecord) error {
	if err := ValidateAgentID(r.RuntimeAgentID); err != nil {
		return fmt.Errorf("runtime_agent_id: %w", err)
	}
	if err := ValidateAgentID(r.BotAgentID); err != nil {
		return fmt.Errorf("bot_agent_id: %w", err)
	}
	if strings.TrimSpace(r.GuildID) == "" || strings.TrimSpace(r.ChannelID) == "" {
		return fmt.Errorf("guild_id and channel_id are required")
	}
	if strings.TrimSpace(r.ChannelRef) == "" {
		return fmt.Errorf("channel_ref is required")
	}
	kind := strings.TrimSpace(r.RuntimeKind)
	if kind != "channel" && kind != "thread" {
		return fmt.Errorf("runtime_kind must be channel or thread")
	}
	if r.Discoverable && !r.Enabled {
		return fmt.Errorf("discoverable runtime must be enabled")
	}
	return nil
}
