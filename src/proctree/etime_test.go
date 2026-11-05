package proctree

import (
	"testing"
	"time"
)

func TestParseEtime(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"00:01":       time.Second,
		"59:59":       59*time.Minute + 59*time.Second,
		"01:00:00":    time.Hour,
		"34-09:36:08": 34*24*time.Hour + 9*time.Hour + 36*time.Minute + 8*time.Second,
	} {
		if got, ok := ParseEtime(in); !ok || got != want {
			t.Errorf("ParseEtime(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "1", "a:b", "1:2:3:4", "-01:00", "1--01:00", "01:-1", "01:+1", "1-", ":"} {
		if _, ok := ParseEtime(in); ok {
			t.Errorf("ParseEtime(%q) が通った", in)
		}
	}
}
