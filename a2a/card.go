package a2a

import (
	"fmt"
	"mime"
	"net/url"
	"strings"
)

const (
	ProtocolBindingNATS = "urn:kiro-discord-bot:a2a:nats:v1"
	ProtocolVersion     = "1.0"
)

type ExtendedAgentCard struct {
	ChannelRef              string   `json:"channel_ref,omitempty"`
	Runtime                 string   `json:"runtime,omitempty"`
	DiscordGuildID          string   `json:"discord_guild_id,omitempty"`
	DiscordChannelID        string   `json:"discord_channel_id,omitempty"`
	DiscordThreadID         string   `json:"discord_thread_id,omitempty"`
	TriggerGuidance         string   `json:"trigger_guidance,omitempty"`
	ResultVisibilitySupport []string `json:"result_visibility_support,omitempty"`
	MaxTaskDurationClass    string   `json:"max_task_duration_class,omitempty"`
	CredentialIssuer        string   `json:"credential_issuer,omitempty"`
	CredentialFingerprint   string   `json:"credential_fingerprint,omitempty"`
	PublicKeyFingerprint    string   `json:"public_key_fingerprint,omitempty"`
	SignatureStatus         string   `json:"signature_status,omitempty"`
}

type PeerCompatibility struct {
	Supported bool
	Reasons   []string
}

func BuildPublicAgentCard(cfg Config, version string, skills []AgentSkill) (AgentCard, error) {
	if err := ValidateAgentID(cfg.AgentID); err != nil {
		return AgentCard{}, err
	}
	name := strings.TrimSpace(cfg.AgentName)
	if name == "" {
		name = string(cfg.AgentID)
	}
	description := strings.TrimSpace(cfg.AgentDescription)
	if description == "" {
		description = "Kiro Discord Bot A2A runtime"
	}
	if strings.TrimSpace(version) == "" {
		version = "unknown"
	}
	card := AgentCard{
		Name:        name,
		Description: description,
		Version:     sanitizePublicText(version),
		SupportedInterfaces: []A2AInterface{{
			URL:             sanitizeNATSURL(cfg.NATSURL),
			ProtocolBinding: ProtocolBindingNATS,
			ProtocolVersion: ProtocolVersion,
		}},
		Capabilities: map[string]bool{
			"streaming":         false,
			"pushNotifications": false,
			"extendedAgentCard": true,
		},
		DefaultInputModes:  []string{"text/plain", "application/json"},
		DefaultOutputModes: []string{"text/plain", "application/json"},
		Skills:             sanitizeSkills(skills),
	}
	card = SanitizeAgentCard(card)
	if err := ValidatePeerCard(card); err != nil {
		return AgentCard{}, err
	}
	return card, nil
}

func BuildRuntimeAgentCard(cfg Config, runtime RuntimeRecord, version string, skills []AgentSkill) (AgentCard, ExtendedAgentCard, error) {
	if err := validateRuntimeRecord(runtime); err != nil {
		return AgentCard{}, ExtendedAgentCard{}, err
	}
	runtimeCfg := cfg
	runtimeCfg.AgentID = runtime.RuntimeAgentID
	runtimeCfg.AgentName = string(runtime.RuntimeAgentID)
	if strings.TrimSpace(runtime.DisplayName) != "" {
		runtimeCfg.AgentDescription = runtime.DisplayName
	}
	runtimeSkills := make([]AgentSkill, 0, len(skills))
	for _, skill := range skills {
		skill.ID = canonicalRuntimeSkillID(runtime.ChannelRef, skill.ID)
		runtimeSkills = append(runtimeSkills, skill)
	}
	card, err := BuildPublicAgentCard(runtimeCfg, version, runtimeSkills)
	if err != nil {
		return AgentCard{}, ExtendedAgentCard{}, err
	}
	ext, err := BuildExtendedAgentCard(card, ExtendedAgentCard{
		ChannelRef:              runtime.ChannelRef,
		Runtime:                 runtime.RuntimeKind,
		DiscordGuildID:          runtime.GuildID,
		DiscordChannelID:        runtime.ChannelID,
		DiscordThreadID:         runtime.ThreadID,
		ResultVisibilitySupport: []string{"proxy", "transparent"},
	})
	if err != nil {
		return AgentCard{}, ExtendedAgentCard{}, err
	}
	return card, ext, nil
}

func canonicalRuntimeSkillID(channelRef, skillID string) string {
	channelRef = sanitizeSkillID(channelRef)
	skillID = sanitizeSkillID(skillID)
	if channelRef == "" || strings.Contains(skillID, "/") {
		return skillID
	}
	return channelRef + "/" + SkillSlug(skillID)
}

func BuildExtendedAgentCard(public AgentCard, ext ExtendedAgentCard) (ExtendedAgentCard, error) {
	if err := ValidatePeerCard(public); err != nil {
		return ExtendedAgentCard{}, err
	}
	out := ExtendedAgentCard{
		ChannelRef:              sanitizeSkillID(ext.ChannelRef),
		Runtime:                 sanitizePublicText(ext.Runtime),
		DiscordGuildID:          sanitizeDiscordID(ext.DiscordGuildID),
		DiscordChannelID:        sanitizeDiscordID(ext.DiscordChannelID),
		DiscordThreadID:         sanitizeDiscordID(ext.DiscordThreadID),
		TriggerGuidance:         sanitizePublicText(ext.TriggerGuidance),
		ResultVisibilitySupport: sanitizeStringList(ext.ResultVisibilitySupport),
		MaxTaskDurationClass:    sanitizePublicText(ext.MaxTaskDurationClass),
		CredentialIssuer:        sanitizePublicText(ext.CredentialIssuer),
		CredentialFingerprint:   sanitizeFingerprint(ext.CredentialFingerprint),
		PublicKeyFingerprint:    sanitizeFingerprint(ext.PublicKeyFingerprint),
		SignatureStatus:         sanitizePublicText(ext.SignatureStatus),
	}
	if out.ChannelRef != "" && !skillSlugPattern.MatchString(out.ChannelRef) {
		return ExtendedAgentCard{}, fmt.Errorf("extended channel_ref is invalid")
	}
	return out, nil
}

func ValidatePeerCard(card AgentCard) error {
	card = SanitizeAgentCard(card)
	if err := ValidateAgentID(AgentID(card.Name)); err != nil {
		return fmt.Errorf("card name must be stable agent id: %w", err)
	}
	if strings.TrimSpace(card.Version) == "" {
		return fmt.Errorf("card version is required")
	}
	if len(card.SupportedInterfaces) == 0 {
		return fmt.Errorf("at least one supported interface is required")
	}
	for _, iface := range card.SupportedInterfaces {
		if strings.TrimSpace(iface.ProtocolBinding) == "" || strings.TrimSpace(iface.ProtocolVersion) == "" {
			return fmt.Errorf("supported interface binding and version are required")
		}
		if hasPrivateLeak(iface.URL) || hasPrivateLeak(iface.ProtocolBinding) || hasPrivateLeak(iface.ProtocolVersion) {
			return fmt.Errorf("supported interface leaks private data")
		}
	}
	for _, mode := range append(card.DefaultInputModes, card.DefaultOutputModes...) {
		if _, _, err := mime.ParseMediaType(mode); err != nil {
			return fmt.Errorf("invalid default MIME mode %q", mode)
		}
	}
	for _, skill := range card.Skills {
		if !skillPattern.MatchString(skill.ID) || !strings.Contains(skill.ID, "/") {
			return fmt.Errorf("skill id %q must be channel_ref/skill_slug", skill.ID)
		}
		for _, mode := range append(skill.InputModes, skill.OutputModes...) {
			if _, _, err := mime.ParseMediaType(mode); err != nil {
				return fmt.Errorf("invalid skill MIME mode %q", mode)
			}
		}
		if hasPrivateLeak(skill.Name) || hasPrivateLeak(skill.Description) || len(skill.Examples) > 0 {
			return fmt.Errorf("skill leaks private data")
		}
	}
	if hasPrivateLeak(card.Name) || hasPrivateLeak(card.Description) || hasPrivateLeak(card.Version) {
		return fmt.Errorf("card leaks private data")
	}
	return nil
}

func CheckVersionCompatibility(card AgentCard) PeerCompatibility {
	var reasons []string
	for _, iface := range card.SupportedInterfaces {
		if iface.ProtocolBinding == ProtocolBindingNATS && iface.ProtocolVersion == ProtocolVersion {
			return PeerCompatibility{Supported: true}
		}
		if iface.ProtocolBinding != ProtocolBindingNATS {
			reasons = append(reasons, "unsupported binding "+iface.ProtocolBinding)
		} else if iface.ProtocolVersion != ProtocolVersion {
			reasons = append(reasons, "unsupported protocol version "+iface.ProtocolVersion)
		}
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "no supported interfaces")
	}
	return PeerCompatibility{Supported: false, Reasons: reasons}
}

func SanitizeAgentCard(card AgentCard) AgentCard {
	card.Name = sanitizePublicText(card.Name)
	card.Description = sanitizePublicText(card.Description)
	card.Version = sanitizePublicText(card.Version)
	for i := range card.SupportedInterfaces {
		card.SupportedInterfaces[i].URL = sanitizeInterfaceURL(card.SupportedInterfaces[i].URL)
		card.SupportedInterfaces[i].ProtocolBinding = sanitizePublicText(card.SupportedInterfaces[i].ProtocolBinding)
		card.SupportedInterfaces[i].ProtocolVersion = sanitizePublicText(card.SupportedInterfaces[i].ProtocolVersion)
	}
	card.Skills = sanitizeSkills(card.Skills)
	return card
}

func sanitizeSkills(skills []AgentSkill) []AgentSkill {
	out := make([]AgentSkill, 0, len(skills))
	for _, skill := range skills {
		skill.ID = sanitizeSkillID(skill.ID)
		skill.Name = sanitizePublicText(skill.Name)
		skill.Description = sanitizePublicText(skill.Description)
		skill.Tags = sanitizeStringList(skill.Tags)
		skill.InputModes = sanitizeStringList(skill.InputModes)
		skill.OutputModes = sanitizeStringList(skill.OutputModes)
		skill.Examples = nil
		out = append(out, skill)
	}
	return out
}

func sanitizeStringList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = sanitizePublicText(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func sanitizeNATSURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return sanitizeInterfaceURL(u.String())
}

func sanitizeFingerprint(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F' || r >= '0' && r <= '9' || r == ':' || r == '-' || r == '_') {
			return "[REDACTED]"
		}
	}
	return s
}

func hasPrivateLeak(s string) bool {
	return absolutePathPattern.MatchString(s) || secretWordPattern.MatchString(s) || internalURLPattern.MatchString(s) || discordIDPattern.MatchString(s)
}
