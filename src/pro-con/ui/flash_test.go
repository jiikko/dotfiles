package ui

import (
	"testing"
	"time"
)

// 操作の結果の通知は flashTTL で消える (次の操作まで古い通知を残さない)。新しい通知が出たら数え直す。
func TestFlashExpires(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	m := New(newSpy(), nil)
	m.now = func() time.Time { return now }
	m.flash = "受け付けた"
	m.Update(tickMsg{})
	now = now.Add(flashTTL - time.Second)
	m.Update(tickMsg{})
	if m.flash == "" {
		t.Fatal("flashTTL の前に消えた")
	}
	m.flash = "次の通知"
	m.Update(tickMsg{})
	now = now.Add(2 * time.Second) // 前の通知からは flashTTL を過ぎたが、次の通知からはまだ
	m.Update(tickMsg{})
	if m.flash != "次の通知" {
		t.Fatalf("新しい通知で数え直さない: %q", m.flash)
	}
	now = now.Add(flashTTL)
	m.Update(tickMsg{})
	if m.flash != "" {
		t.Fatalf("flashTTL を過ぎても消えない: %q", m.flash)
	}
}

// 起動時の知らせ (Notify) は時間では消えず、esc で消える。
func TestNotifyStaysUntilEsc(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	m := New(newSpy(), nil)
	m.now = func() time.Time { return now }
	m.Notify("daemon を起動した")
	m.Update(tickMsg{})
	now = now.Add(time.Hour)
	m.Update(tickMsg{})
	if m.sticky != "daemon を起動した" {
		t.Fatalf("起動時の知らせが時間で消えた: %q", m.sticky)
	}
	press(m, "esc")
	if m.sticky != "" {
		t.Fatal("esc で消えない")
	}
}
