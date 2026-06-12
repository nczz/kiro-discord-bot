package textutil

import (
	"testing"
	"time"
)

func TestFormatUptime(t *testing.T) {
	cases := []struct {
		name string
		in   time.Duration
		want string
	}{
		{name: "seconds", in: 12 * time.Second, want: "12s"},
		{name: "minutes", in: 90 * time.Second, want: "1m 30s"},
		{name: "hours", in: 3*time.Hour + 2*time.Minute + time.Second, want: "3h 02m 01s"},
		{name: "days", in: 49*time.Hour + 4*time.Minute, want: "2d 01h 04m"},
		{name: "negative", in: -time.Second, want: "0s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FormatUptime(tc.in); got != tc.want {
				t.Fatalf("FormatUptime(%s) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
