package bot

import (
	"strings"
	"testing"
)

func TestValidateCronJSON_Valid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"plain json", `{"name":"Test","schedule":"0 9 * * *","prompt":"do something"}`},
		{"with markdown", "```json\n{\"name\":\"Test\",\"schedule\":\"0 9 * * *\",\"prompt\":\"do something\"}\n```"},
		{"with surrounding text", "Here is the result:\n{\"name\":\"Test\",\"schedule\":\"0 9 * * *\",\"prompt\":\"do something\"}\nDone."},
		{"chinese", `{"name":"伺服器檢查","schedule":"30 8 * * 1-5","prompt":"檢查伺服器狀態"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := validateCronJSON(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Name == "" || result.Schedule == "" || result.Prompt == "" {
				t.Fatalf("empty fields: %+v", result)
			}
		})
	}
}

func TestValidateCronJSON_Invalid(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{"not json", "this is not json", "not valid JSON"},
		{"empty name", `{"name":"","schedule":"0 9 * * *","prompt":"x"}`, "missing 'name'"},
		{"empty schedule", `{"name":"x","schedule":"","prompt":"x"}`, "missing 'schedule'"},
		{"empty prompt", `{"name":"x","schedule":"0 9 * * *","prompt":""}`, "missing 'prompt'"},
		{"invalid cron", `{"name":"x","schedule":"0 25 * * *","prompt":"x"}`, "invalid schedule"},
		{"name too long", `{"name":"` + strings.Repeat("a", 101) + `","schedule":"0 9 * * *","prompt":"x"}`, "too long"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateCronJSON(tt.input)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestValidateCronJSON_Uncertain(t *testing.T) {
	_, err := validateCronJSON(`{"name":"x","schedule":"?","prompt":"x"}`)
	if err != ErrScheduleUncertain {
		t.Fatalf("expected ErrScheduleUncertain, got: %v", err)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
