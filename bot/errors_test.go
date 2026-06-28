package bot

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/channel"
	L "github.com/nczz/kiro-discord-bot/locale"
)

func TestCommandErrorCWDAllowlist(t *testing.T) {
	L.Load("en")
	msg := commandError(errors.New("working directory /tmp is outside ALLOWED_CWD_ROOTS: /home/chun/projects"))
	if !strings.Contains(msg, "outside the allowed roots") {
		t.Fatalf("missing actionable cwd text: %s", msg)
	}
	if !strings.Contains(msg, "/doctor") {
		t.Fatalf("missing doctor hint: %s", msg)
	}
}

func TestCommandErrorKiroAuth(t *testing.T) {
	L.Load("en")
	msg := commandError(errors.New("initialize: transport closed | stderr: error: You are not logged in, please log in with kiro-cli login"))
	if !strings.Contains(msg, "not authenticated") {
		t.Fatalf("missing auth text: %s", msg)
	}
	if !strings.Contains(msg, "KIRO_API_KEY") {
		t.Fatalf("missing api key hint: %s", msg)
	}
}

func TestCommandErrorStringRemovesLeadingErrorIcon(t *testing.T) {
	L.Load("en")
	msg := commandErrorString(errors.New("kiro-cli binary not found: kiro-cli"))
	if strings.HasPrefix(msg, "❌ ") {
		t.Fatalf("unexpected leading icon: %s", msg)
	}
}

func TestCommandErrorAgentBinaryMissingIsEngineNeutral(t *testing.T) {
	L.Load("en")
	msg := commandError(errors.New("agent binary not found: omp"))
	if !strings.Contains(msg, "Agent binary cannot be resolved") {
		t.Fatalf("missing agent binary text: %s", msg)
	}
	if !strings.Contains(msg, "OMP_PATH") {
		t.Fatalf("missing OMP_PATH hint: %s", msg)
	}
}

func TestCommandErrorThreadAgentLimitWithCandidates(t *testing.T) {
	L.Load("en")
	msg := commandError(&channel.ThreadAgentLimitError{
		Max:      5,
		Active:   3,
		Inactive: 2,
		Candidates: []channel.ThreadAgentLimitCandidate{
			{ThreadID: "thread-old", LastActivity: time.Now().Add(-time.Hour)},
			{ThreadID: "thread-new", LastActivity: time.Now()},
		},
	})
	for _, want := range []string{"Capacity is full", "No agent was closed automatically", "<#thread-old>", "`thread-old`", "/close-thread"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("missing %q in message:\n%s", want, msg)
		}
	}
}

func TestParseThreadIDArg(t *testing.T) {
	for _, tt := range []struct {
		raw  string
		want string
	}{
		{"123456789012345678", "123456789012345678"},
		{"<#123456789012345678>", "123456789012345678"},
		{" <#123> ", "123"},
		{"abc", ""},
		{"<#abc>", ""},
	} {
		if got := parseThreadIDArg(tt.raw); got != tt.want {
			t.Fatalf("parseThreadIDArg(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestCommandErrorThreadAgentLimitAllActive(t *testing.T) {
	L.Load("en")
	msg := commandError(&channel.ThreadAgentLimitError{Max: 5, Active: 5})
	if !strings.Contains(msg, "All thread agent slots are currently working") {
		t.Fatalf("missing all-active explanation:\n%s", msg)
	}
	if strings.Contains(msg, "Oldest inactive candidates") {
		t.Fatalf("all-active message should not list inactive candidates:\n%s", msg)
	}
}

func TestCommandErrorNoThreadAgentIsActionable(t *testing.T) {
	L.Load("en")
	msg := commandError(channel.ErrNoThreadAgent)
	for _, want := range []string{"No active or saved thread agent", "parent channel", "send a normal message"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("missing %q in message:\n%s", want, msg)
		}
	}
}

func TestCommandErrorActiveJobNotCancellable(t *testing.T) {
	L.Load("en")
	msg := commandError(errors.New("active job is not cancellable yet"))
	if !strings.Contains(msg, "not cancellable yet") {
		t.Fatalf("missing not-cancellable explanation:\n%s", msg)
	}
}

func TestValidateManagedThread(t *testing.T) {
	ds, err := discordgo.New("")
	if err != nil {
		t.Fatal(err)
	}
	if err := ds.State.GuildAdd(&discordgo.Guild{ID: "guild-1"}); err != nil {
		t.Fatal(err)
	}
	if err := ds.State.ChannelAdd(&discordgo.Channel{ID: "parent-1", GuildID: "guild-1", Type: discordgo.ChannelTypeGuildText}); err != nil {
		t.Fatal(err)
	}
	if err := ds.State.ChannelAdd(&discordgo.Channel{ID: "thread-1", GuildID: "guild-1", ParentID: "parent-1", Type: discordgo.ChannelTypeGuildPublicThread}); err != nil {
		t.Fatal(err)
	}
	b := &Bot{discord: ds}
	ctx := cmdCtx{guildID: "guild-1"}

	if err := b.validateManagedThread(ctx, "thread-1", "parent-1"); err != nil {
		t.Fatalf("validate managed thread: %v", err)
	}
	if err := b.validateManagedThread(ctx, "thread-1", "parent-2"); err == nil || !strings.Contains(err.Error(), "parent") {
		t.Fatalf("expected parent validation error, got %v", err)
	}
	ctx.guildID = "guild-2"
	if err := b.validateManagedThread(ctx, "thread-1", "parent-1"); err == nil || !strings.Contains(err.Error(), "guild") {
		t.Fatalf("expected guild validation error, got %v", err)
	}
}
