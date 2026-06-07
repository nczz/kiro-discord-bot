package audit

import "testing"

func TestRecorderTypingDefaultDisabled(t *testing.T) {
	r := NewRecorder(nil, 1, nil, false)
	defer r.Close()
	if r.recordTyping {
		t.Fatal("typing audit should be disabled by default")
	}
}

func TestRecorderTypingCanBeEnabled(t *testing.T) {
	r := NewRecorder(nil, 1, nil, true)
	defer r.Close()
	if !r.recordTyping {
		t.Fatal("typing audit should be enabled when requested")
	}
}

func TestRecorderCloseIsIdempotent(t *testing.T) {
	r := NewRecorder(nil, 1, nil, false)
	r.Close()
	r.Close()
}
