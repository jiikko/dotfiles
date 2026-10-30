package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"treefiler/filer"
)

// TREEFILER_DEBUG のログは Update ごとに状態を 1 行書く。tick で状態が前の行と同じなら書かない。
func TestDebugLogWritesKeyLinesAndSkipsSameTicks(t *testing.T) {
	t.Setenv("TREEFILER_CONFIG_DIR", t.TempDir())
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := filer.New(root, filer.Options{})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "state.jsonl")
	clock := time.Unix(1000, 0)
	d, err := openDebugLog(path, func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	a := &app{f: f, debug: d, clock: func() time.Time { return clock }}
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	a.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	for range 3 { // 動きが止まった後の tick は状態が同じ
		clock = clock.Add(5 * time.Second)
		a.Update(tickMsg{})
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var lines []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var v map[string]any
		if err := json.Unmarshal([]byte(l), &v); err != nil {
			t.Fatalf("JSON でない行 %q: %v", l, err)
		}
		lines = append(lines, v)
	}
	var key map[string]any
	ticks := 0
	for _, v := range lines {
		switch v["msg"] {
		case "key":
			key = v
		case "tick":
			ticks++
		}
	}
	if key == nil || key["key"] != "j" || key["cursor"] != "b.txt" {
		t.Fatalf("j の行が無い / カーソルが違う: %v", key)
	}
	if ticks > 1 { // 止まるまでに 1 回は状態が変わりうる (動き・裏の処理の終わり)。同じ状態の tick を毎回書けば 3
		t.Fatalf("状態の変わらない tick を %d 行書いた", ticks)
	}
}
