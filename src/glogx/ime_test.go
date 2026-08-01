package main

import (
	"strings"
	"testing"
)

func stubInputSources(t *testing.T, current func() (string, bool), selectSource func(string) bool) {
	t.Helper()
	origCurrent, origSelect := currentInputSource, selectInputSource
	currentInputSource, selectInputSource = current, selectSource
	t.Cleanup(func() {
		currentInputSource, selectInputSource = origCurrent, origSelect
	})
}

func TestIMESwitchFinish(t *testing.T) {
	t.Run("nil ハンドルは何もしない", func(t *testing.T) {
		var s *imeSwitch
		restore, warn := s.finish()
		if warn != "" {
			t.Fatalf("warn=%q; want 空", warn)
		}
		restore()
	})

	t.Run("TIS で現在ソースを取得できなければ警告", func(t *testing.T) {
		restore, warn := (&imeSwitch{}).finish()
		if !strings.Contains(warn, "TIS") || !strings.Contains(warn, "取得") {
			t.Fatalf("warn=%q; want TIS 取得警告", warn)
		}
		restore()
	})

	t.Run("既に英数なら選択しない", func(t *testing.T) {
		called := false
		stubInputSources(t,
			func() (string, bool) { return asciiInputSource, true },
			func(string) bool { called = true; return true },
		)
		restore, warn := (&imeSwitch{prev: asciiInputSource, ok: true}).finish()
		if warn != "" || called {
			t.Fatalf("warn=%q called=%v; want 無警告・選択なし", warn, called)
		}
		restore()
	})

	t.Run("英数へ切替後に元のソースへ復元", func(t *testing.T) {
		const japanese = "com.apple.inputmethod.Kotoeri.Japanese"
		current := japanese
		var selected []string
		stubInputSources(t,
			func() (string, bool) { return current, true },
			func(id string) bool {
				selected = append(selected, id)
				current = id
				return true
			},
		)
		s := beginIMESwitch()
		restore, warn := s.finish()
		if warn != "" || current != asciiInputSource {
			t.Fatalf("切替後: current=%q warn=%q", current, warn)
		}
		restore()
		if current != japanese {
			t.Fatalf("復元後: current=%q; want %q", current, japanese)
		}
		if len(selected) != 2 || selected[0] != asciiInputSource || selected[1] != japanese {
			t.Fatalf("選択順=%v", selected)
		}
	})

	t.Run("TIS が選択を拒否したら警告", func(t *testing.T) {
		stubInputSources(t,
			func() (string, bool) { return "com.example.Japanese", true },
			func(string) bool { return false },
		)
		restore, warn := beginIMESwitch().finish()
		if !strings.Contains(warn, "切替") {
			t.Fatalf("warn=%q; want 切替警告", warn)
		}
		restore()
	})
}

func TestSwitchInputSourceRequiresObservedResult(t *testing.T) {
	stubInputSources(t,
		func() (string, bool) { return "com.example.Unchanged", true },
		func(string) bool { return true },
	)
	if switchInputSource(asciiInputSource) {
		t.Fatal("TIS が選択を受理しただけで成功扱いになった")
	}
}
