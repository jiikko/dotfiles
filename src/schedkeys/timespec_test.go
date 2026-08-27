package main

import (
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	ok := map[string]time.Duration{
		"90":      90 * time.Minute,
		"1h30m":   90 * time.Minute,
		"1h30":    90 * time.Minute,
		"1:30":    90 * time.Minute,
		"1H30":    90 * time.Minute,
		"45m":     45 * time.Minute,
		"2h":      2 * time.Hour,
		"1:05":    65 * time.Minute,
		" 1h30m":  90 * time.Minute,
		"1 h 30m": 90 * time.Minute,
	}
	for in, want := range ok {
		got, err := parseDuration(in)
		if err != nil || got != want {
			t.Errorf("parseDuration(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "0", "0h0m", "abc", "123456", "1h30x", "-5", "1:60", "h", "1:", ":30", "1.5h"} {
		if _, err := parseDuration(in); err == nil {
			t.Errorf("parseDuration(%q) が通った (弾くべき)", in)
		}
	}
}

func TestParseClock(t *testing.T) {
	now := time.Date(2026, 8, 27, 10, 0, 30, 0, time.UTC)
	cases := map[string]time.Time{
		"10:30": time.Date(2026, 8, 27, 10, 30, 0, 0, time.UTC),
		"9:00":  time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC), // 過ぎている → 翌日
		"09:00": time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC),
		"10:00": time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC), // 同時刻 (秒で過ぎている) → 翌日
		"23:59": time.Date(2026, 8, 27, 23, 59, 0, 0, time.UTC),
		"0:00":  time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC),
	}
	for in, want := range cases {
		got, err := parseClock(in, now)
		if err != nil || !got.Equal(want) {
			t.Errorf("parseClock(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "25:00", "10:60", "1030", "10:5", "abc", "10:300", "-1:00"} {
		if _, err := parseClock(in, now); err == nil {
			t.Errorf("parseClock(%q) が通った (弾くべき)", in)
		}
	}
}

func TestParseClockCrossesDST(t *testing.T) {
	// 日付をまたぐ繰り上げが「+24h」でなく「翌日の同じ時計時刻」であること (DST のある地域で 1 時間ずれない)
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("tzdata なし")
	}
	now := time.Date(2026, 3, 7, 12, 0, 0, 0, ny) // DST 切替 (3/8) の前日
	got, err := parseClock("09:00", now)
	if err != nil {
		t.Fatal(err)
	}
	if h, m, _ := got.Clock(); h != 9 || m != 0 {
		t.Errorf("翌日繰り上げ後の時計時刻 = %02d:%02d; want 09:00", h, m)
	}
}

func TestFormatRemaining(t *testing.T) {
	cases := map[time.Duration]string{
		-time.Second:              "まもなく",
		0:                         "まもなく",
		30 * time.Second:          "30s",
		59 * time.Second:          "59s",
		60 * time.Second:          "1m",
		45 * time.Minute:          "45m",
		time.Hour:                 "1h00m",
		90 * time.Minute:          "1h30m",
		time.Hour + 5*time.Minute: "1h05m",
	}
	for d, want := range cases {
		if got := formatRemaining(d); got != want {
			t.Errorf("formatRemaining(%v) = %q; want %q", d, got, want)
		}
	}
}
