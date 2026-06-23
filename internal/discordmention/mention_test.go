package discordmention

import (
	"strings"
	"testing"
)

func TestRenderAllowsOnlyVerifiedPlaceholders(t *testing.T) {
	content, allowed := Render("notify [[discord:user:123]] and raw <@456>", []Ref{UserRef("123", "Chun")})
	if !strings.Contains(content, "<@123>") {
		t.Fatalf("rendered content missing verified mention: %q", content)
	}
	if strings.Contains(content, "<@456>") {
		t.Fatalf("raw mention was not escaped: %q", content)
	}
	if !strings.Contains(content, "<@\u200b456>") {
		t.Fatalf("raw mention was not made inert: %q", content)
	}
	if allowed == nil || len(allowed.Users) != 1 || allowed.Users[0] != "123" {
		t.Fatalf("allowed mentions = %+v, want only user 123", allowed)
	}
}

func TestRenderDoesNotAllowRawMentionForSameVerifiedUser(t *testing.T) {
	content, allowed := Render("raw <@123>", []Ref{UserRef("123", "Chun")})
	if strings.Contains(content, "<@123>") {
		t.Fatalf("raw mention for verified user should still be inert: %q", content)
	}
	if len(allowed.Users) != 0 {
		t.Fatalf("allowed users = %+v, want none", allowed.Users)
	}
}

func TestRenderAllowsVerifiedRolePlaceholder(t *testing.T) {
	content, allowed := Render("handoff [[discord:role:999]] raw <@&888>", []Ref{RoleRef("999", "BuildBot")})
	if !strings.Contains(content, "<@&999>") {
		t.Fatalf("rendered content missing verified role mention: %q", content)
	}
	if strings.Contains(content, "<@&888>") {
		t.Fatalf("raw role mention was not escaped: %q", content)
	}
	if len(allowed.Roles) != 1 || allowed.Roles[0] != "999" {
		t.Fatalf("allowed roles = %+v, want only 999", allowed.Roles)
	}
}

func TestPromptBlockListsPlaceholders(t *testing.T) {
	block := PromptBlock([]Ref{UserRef("123", "Chun")})
	if !strings.Contains(block, "[[discord:user:123]]") || strings.Contains(block, "<@123>") {
		t.Fatalf("prompt block = %q", block)
	}
}

func TestRenderIgnoresInvalidDiscordIDs(t *testing.T) {
	content, allowed := Render("notify [[discord:user:not-a-snowflake]]", []Ref{UserRef("not-a-snowflake", "bad")})
	if strings.Contains(content, "<@not-a-snowflake>") {
		t.Fatalf("invalid ID rendered as mention: %q", content)
	}
	if allowed == nil || len(allowed.Users) != 0 || len(allowed.Roles) != 0 {
		t.Fatalf("allowed mentions = %+v, want none", allowed)
	}
	if block := PromptBlock([]Ref{UserRef("not-a-snowflake", "bad")}); block != "" {
		t.Fatalf("PromptBlock() = %q, want empty for invalid ID", block)
	}
}

func TestPromptBlockSanitizesDisplayNames(t *testing.T) {
	block := PromptBlock([]Ref{UserRef("123", "Chun\n- injected")})
	if strings.Contains(block, "Chun\n- injected") {
		t.Fatalf("prompt block kept multiline display name: %q", block)
	}
	if !strings.Contains(block, "Chun - injected") {
		t.Fatalf("prompt block = %q, want sanitized display name", block)
	}
}
