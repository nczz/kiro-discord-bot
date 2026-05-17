package bot

import (
	"errors"
	"strings"
	"testing"

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
