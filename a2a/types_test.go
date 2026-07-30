package a2a

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSubjectEnvelopeTaskStateErrorCodeNatsMsgID(t *testing.T) {
	subject, err := ParseSubject("a2a.v1.task.adam-n200.eve-local.msg_123")
	if err != nil {
		t.Fatalf("ParseSubject: %v", err)
	}
	if subject.Kind != SubjectKindTask || subject.From != "adam-n200" || subject.To != "eve-local" || subject.MessageID != "msg_123" {
		t.Fatalf("unexpected subject: %+v", subject)
	}

	env := Envelope{
		Version:   Version,
		Binding:   Binding,
		MessageID: "msg_123",
		From:      "adam-n200",
		To:        "eve-local",
		Type:      EnvelopeTypeTask,
		Payload:   json.RawMessage(`{"prompt":"hello"}`),
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano),
	}
	if err := ValidateEnvelope(env, subject); err != nil {
		t.Fatalf("ValidateEnvelope: %v", err)
	}
	if !IsKnownTaskState(TaskStateAuthRequired) || !IsTerminalState(TaskStateFailed) || IsTerminalState(TaskStateWorking) {
		t.Fatalf("task state helpers returned unexpected values")
	}
	if ErrorUnsupportedOperation != "unsupported_operation" {
		t.Fatalf("unexpected unsupported_operation constant: %q", ErrorUnsupportedOperation)
	}
	if got, want := TaskNatsMsgID("adam-n200", "eve-local", "msg_123"), "task:adam-n200:eve-local:msg_123"; got != want {
		t.Fatalf("TaskNatsMsgID = %q, want %q", got, want)
	}
	if got, want := ControlNatsMsgID("eve-local", "adam-n200", "task_1", "cancel", 3), "control:eve-local:adam-n200:task_1:cancel:3"; got != want {
		t.Fatalf("ControlNatsMsgID = %q, want %q", got, want)
	}
	if got, want := PreAcceptEventNatsMsgID("adam-n200", "eve-local", "msg_123", "rejected"), "event:adam-n200:eve-local:msg_msg_123:rejected"; got != want {
		t.Fatalf("PreAcceptEventNatsMsgID = %q, want %q", got, want)
	}
}

func TestParseSubjectRejectsForbiddenAndUnversionedSubjects(t *testing.T) {
	for _, raw := range []string{
		"a2a.v1.pool.adam-n200",
		"a2a.v1.pool.>",
		"a2a.task.adam-n200.eve-local.msg_123",
		"a2a.status.adam-n200.task_1",
		"a2a.result.adam-n200.task_1",
		"a2a.cancel.adam-n200.task_1",
		"a2a.v1.task.adam-n200.eve-local.*",
		"a2a.v1.task.adam-n200.eve-local.",
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := ParseSubject(raw); err == nil {
				t.Fatalf("ParseSubject accepted forbidden subject %q", raw)
			}
		})
	}
}

func TestValidateEnvelopeRejectsSubjectMismatchOversizeAndExpired(t *testing.T) {
	subject, err := ParseSubject("a2a.v1.task.adam-n200.eve-local.msg_123")
	if err != nil {
		t.Fatal(err)
	}
	base := Envelope{
		Version:   Version,
		Binding:   Binding,
		MessageID: "msg_123",
		From:      "adam-n200",
		To:        "eve-local",
		Type:      EnvelopeTypeTask,
		Payload:   json.RawMessage(`{}`),
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano),
	}

	mismatch := base
	mismatch.To = "other-agent"
	if err := ValidateEnvelope(mismatch, subject); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("mismatch error = %v, want subject mismatch", err)
	}

	oversize := base
	oversize.Payload = make([]byte, MaxEnvelopePayloadBytes+1)
	if err := ValidateEnvelope(oversize, subject); err == nil || !strings.Contains(err.Error(), "payload exceeds") {
		t.Fatalf("oversize error = %v, want payload limit", err)
	}

	expired := base
	expired.CreatedAt = time.Now().UTC().Add(-3 * time.Minute).Format(time.RFC3339Nano)
	expired.ExpiresAt = time.Now().UTC().Add(-2 * time.Minute).Format(time.RFC3339Nano)
	if err := ValidateEnvelope(expired, subject); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired error = %v, want expired", err)
	}
}

func TestConfigValidateStartupKeepsDisabledNoopAndRejectsTokenOnlyProduction(t *testing.T) {
	if err := (Config{NATSToken: "token", ProductionSecurity: true}).ValidateStartup(); err != nil {
		t.Fatalf("disabled config should be no-op, got %v", err)
	}
	if err := (Config{NATSURL: "nats://127.0.0.1:4222", NATSToken: "token", ProductionSecurity: true, AgentID: "adam-n200"}).ValidateStartup(); err == nil || !strings.Contains(err.Error(), "token-only") {
		t.Fatalf("token-only production error = %v, want rejection", err)
	}
	if err := (Config{NATSURL: "nats://127.0.0.1:4222", NATSToken: "token", NATSTLSCAFile: "/tmp/ca.pem", ProductionSecurity: true, AgentID: "adam-n200"}).ValidateStartup(); err != nil {
		t.Fatalf("production config with TLS material rejected: %v", err)
	}
}

func TestAgentIDRejectsUnstableIdentifiers(t *testing.T) {
	for _, id := range []AgentID{"12345", "adam-pid123", "adam-1774992000000000000"} {
		if err := ValidateAgentID(id); err == nil {
			t.Fatalf("ValidateAgentID accepted unstable id %q", id)
		}
	}
	if err := ValidateAgentID("adam-n200"); err != nil {
		t.Fatalf("ValidateAgentID rejected stable id: %v", err)
	}
}
