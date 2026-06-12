package cronpolicy

import (
	"strings"
	"testing"
)

func TestTimezoneNameUsesConfiguredTimezone(t *testing.T) {
	if got := TimezoneName(" Asia/Taipei "); got != "Asia/Taipei" {
		t.Fatalf("TimezoneName = %q, want Asia/Taipei", got)
	}
}

func TestSchedulePolicyPinsTimezoneAndForbidsUTCConversion(t *testing.T) {
	got := SchedulePolicy("Asia/Taipei")
	for _, want := range []string{"Asia/Taipei", "Do not convert user-local times to UTC", "30 12 * * *", "12:30"} {
		if !strings.Contains(got, want) {
			t.Fatalf("SchedulePolicy missing %q: %q", want, got)
		}
	}
}

func TestToolDescriptionsShareSchedulePolicy(t *testing.T) {
	policy := SchedulePolicy("Asia/Taipei")
	if !strings.Contains(CreateToolDescription("Asia/Taipei"), policy) {
		t.Fatalf("create tool description does not include shared policy")
	}
	if !strings.Contains(ScheduleFieldDescription("Asia/Taipei"), "Asia/Taipei") || !strings.Contains(ScheduleFieldDescription("Asia/Taipei"), "Do not convert to UTC") {
		t.Fatalf("schedule field description missing timezone policy")
	}
}
