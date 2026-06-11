package secrets

import (
	"strings"
	"testing"
)

func TestRedactorRedactsKnownEnvValues(t *testing.T) {
	r := &Redactor{}
	r.Add("KIRO_API_KEY", "kiro-secret-value")

	got := r.Redact("value=kiro-secret-value")
	if strings.Contains(got, "kiro-secret-value") {
		t.Fatalf("secret value leaked: %q", got)
	}
	if !strings.Contains(got, "[REDACTED:KIRO_API_KEY]") {
		t.Fatalf("redacted label missing: %q", got)
	}
}

func TestRedactorRedactsAssignmentsAndBearerValues(t *testing.T) {
	r := &Redactor{}
	input := "KIRO_API_KEY=abc123456789\nAuthorization: Bearer bearer-secret-123\n{\"api_key\":\"json-secret-123\"}"
	got := r.Redact(input)

	for _, leaked := range []string{"abc123456789", "bearer-secret-123", "json-secret-123"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("secret %q leaked in %q", leaked, got)
		}
	}
	if strings.Count(got, placeholder) < 3 {
		t.Fatalf("expected multiple redactions, got %q", got)
	}
}

func TestRedactorIgnoresShortKnownValues(t *testing.T) {
	r := &Redactor{}
	r.Add("API_KEY", "short")
	if got := r.Redact("short"); got != "short" {
		t.Fatalf("short value was over-redacted: %q", got)
	}
}
