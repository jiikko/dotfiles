package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeScript(t *testing.T, raw map[string]any) string {
	t.Helper()
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "s.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRejectBadShow(t *testing.T) {
	for _, tc := range []struct {
		show any
		want string
	}{
		{"排他ロック", "オブジェクトか null"},
		{map[string]any{"type": "chart", "text": "x"}, "show.type は keyword"},
		{map[string]any{"text": "x"}, "show.type は keyword"},
		{map[string]any{"type": "keyword", "text": "x", "color": "red"}, "に書けるのは type/text/sub だけ"},
		{map[string]any{"type": "keyword", "text": " "}, "show.text は空でない文字列"},
		{map[string]any{"type": "keyword"}, "show.text は空でない文字列"},
		{map[string]any{"type": "keyword", "text": "x", "sub": 1}, "show.sub は文字列"},
		{map[string]any{"type": "keyword", "text": strings.Repeat("語", 17)}, "show.text は 16 字まで"},
		{map[string]any{"type": "keyword", "text": "x", "sub": strings.Repeat("補", 41)}, "show.sub は 40 字まで"},
	} {
		path := writeScript(t, map[string]any{"lines": []any{
			map[string]any{"who": "metan", "text": "a"},
			map[string]any{"who": "metan", "text": "b", "show": tc.show},
		}})
		_, err := loadScript(path, testEnv(t))
		if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "lines[1].show") {
			t.Errorf("%v: %q を含む拒否にならなかった (%v)", tc.show, tc.want, err)
		}
	}
}

// TestAcceptShowAtLimit は、上限ちょうどの長さの日本語が通ることを確かめる (長さは文字数で数える。バイト数で数えると、
// 全角 1 字が 3 バイトなので、画面に収まる語まで止めてしまう)。
func TestAcceptShowAtLimit(t *testing.T) {
	path := writeScript(t, map[string]any{"lines": []any{
		map[string]any{"who": "metan", "text": "a", "show": map[string]any{
			"type": "keyword", "text": strings.Repeat("語", keywordTextMax), "sub": strings.Repeat("補", keywordSubMax),
		}},
	}})
	if _, err := loadScript(path, testEnv(t)); err != nil {
		t.Errorf("上限ちょうどの長さを拒否した: %v", err)
	}
}

func TestLineShowsPersistClearAndDedupe(t *testing.T) {
	kw := func(text string) map[string]any { return map[string]any{"type": "keyword", "text": text} }
	lines := []map[string]any{
		{"who": "metan", "text": "0"},                  // 図解の前
		{"who": "metan", "text": "1", "show": kw("A")}, // A を出す
		{"who": "metan", "text": "2"},                  // 出し続ける
		{"who": "metan", "text": "3", "show": kw("B")}, // B に替える
		{"who": "metan", "text": "4", "show": nil},     // 消す
		{"who": "metan", "text": "5"},                  // 消えたまま
		{"who": "metan", "text": "6", "show": kw("A")}, // 同じ中身は表の同じ番号
	}
	idx, table, err := lineShows("s.json", lines)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]int, len(idx))
	for i, p := range idx {
		got[i] = -1
		if p != nil {
			got[i] = *p
		}
	}
	if want := []int{-1, 0, 0, 1, -1, -1, 0}; !reflect.DeepEqual(got, want) {
		t.Errorf("行ごとの図解の番号: got %v want %v", got, want)
	}
	if want := []showData{{Type: "keyword", Text: "A"}, {Type: "keyword", Text: "B"}}; !reflect.DeepEqual(table, want) {
		t.Errorf("図解の表: got %v want %v", table, want)
	}
}

// TestShowDoesNotChangeAudioOrFrames は、図解を足しても各行の時刻と撮影の状態の列が変わらないことを確かめる。
// 図解は行の関数なので、mp4 が撮る状態の種類は増えない。キャッシュの鍵に show が入ると、testdata の合成済み wav が
// 引けずに assemble が落ちる。
func TestShowDoesNotChangeAudioOrFrames(t *testing.T) {
	env := testEnv(t)
	build := func(edit func(lines []any)) *PlayerData {
		dir := t.TempDir()
		copyTree(t, filepath.Join("testdata", "build"), dir)
		path := filepath.Join(dir, "script.json")
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var raw map[string]any
		if err := decodeJSON(b, &raw); err != nil {
			t.Fatal(err)
		}
		edit(raw["lines"].([]any))
		if b, err = json.Marshal(raw); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
		s, err := loadScript(resolvePath(path), env)
		if err != nil {
			t.Fatal(err)
		}
		data, _, err := assemble(s, env)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	plain := build(func([]any) {})
	shown := build(func(lines []any) {
		lines[1].(map[string]any)["show"] = map[string]any{"type": "keyword", "text": "排他ロック"}
		lines[3].(map[string]any)["show"] = nil
	})
	if !reflect.DeepEqual(plain.Frames, shown.Frames) {
		t.Errorf("図解で撮影の状態の列が変わった")
	}
	if len(shown.Shows) != 1 {
		t.Fatalf("図解の表: got %v", shown.Shows)
	}
	for i, l := range shown.Lines {
		on := l.Show != nil && *l.Show == 0
		if want := i == 1 || i == 2; on != want {
			t.Errorf("lines[%d] の図解: got %v want %v", i, l.Show, want)
		}
		if l.Start != plain.Lines[i].Start || l.End != plain.Lines[i].End {
			t.Errorf("lines[%d] の時刻が変わった", i)
		}
	}
}
