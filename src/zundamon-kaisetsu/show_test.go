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

func side(title string, items ...string) map[string]any {
	list := make([]any, len(items))
	for i, it := range items {
		list[i] = it
	}
	return map[string]any{"title": title, "items": list}
}

func cmp(left, right any) map[string]any {
	m := map[string]any{"type": "compare", "left": left}
	if right != nil {
		m["right"] = right
	}
	return m
}

func withKey(m map[string]any, k string, v any) map[string]any {
	m[k] = v
	return m
}

func TestRejectBadShow(t *testing.T) {
	for _, tc := range []struct {
		show any
		want string
	}{
		{"排他ロック", "オブジェクトか null"},
		{map[string]any{"type": "chart", "text": "x"}, "show.type は compare/keyword"},
		{map[string]any{"text": "x"}, "show.type は compare/keyword"},
		{map[string]any{"type": "keyword", "text": "x", "color": "red"}, "に書けるのは type/text/sub だけ"},
		{map[string]any{"type": "keyword", "text": " "}, "show.text は空でない文字列"},
		{map[string]any{"type": "keyword"}, "show.text は空でない文字列"},
		{map[string]any{"type": "keyword", "text": "x", "sub": 1}, "show.sub は文字列"},
		{map[string]any{"type": "keyword", "text": strings.Repeat("語", 17)}, "show.text は 16 字まで"},
		{map[string]any{"type": "keyword", "text": "x", "sub": strings.Repeat("補", 41)}, "show.sub は 40 字まで"},
		{cmp(side("前", "a"), nil), "show.right は {"},
		{cmp("前", side("後", "b")), "show.left は {"},
		{withKey(cmp(side("前", "a"), side("後", "b")), "title", "x"), "show (compare) に書けるのは type/left/right だけ"},
		{cmp(withKey(side("前", "a"), "note", "x"), side("後", "b")), "show.left に書けるのは title/items だけ"},
		{cmp(side("", "a"), side("後", "b")), "show.left.title は空でない文字列"},
		{cmp(map[string]any{"items": []any{"a"}}, side("後", "b")), "show.left.title は空でない文字列"},
		{cmp(side("前", "a", ""), side("後", "b")), "show.left.items[1] は空でない文字列"},
		{cmp(side("前", "a"), side("後", " ")), "show.right.items[0] は空でない文字列"},
		{cmp(side(strings.Repeat("見", 9), "a"), side("後", "b")), "show.left.title は 8 字まで"},
		{cmp(side("前"), side("後", "b")), "show.left.items は 1〜3 個"},
		{cmp(side("前", "a", "b", "c", "d"), side("後", "b")), "show.left.items は 1〜3 個"},
		{cmp(side("前", "a"), map[string]any{"title": "後", "items": "b"}), "show.right.items は 1〜3 個"},
		{cmp(side("前", "a"), map[string]any{"title": "後", "items": []any{"b", 1}}), "show.right.items[1] は空でない文字列"},
		{cmp(side("前", "a", strings.Repeat("項", 17)), side("後", "b")), "show.left.items[1] は 16 字まで"},
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
		map[string]any{"who": "metan", "text": "b", "show": cmp(
			side(strings.Repeat("見", compareTitleMax), strings.Repeat("項", compareItemMax), "x", "y"),
			side("後", "z"),
		)},
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
		{"who": "metan", "text": "7", "show": cmp(side("前", "a"), side("後", "b"))},
		{"who": "metan", "text": "8", "show": cmp(side("前", "a"), side("後", "c"))},   // 箇条書きが違えば別の図解
		{"who": "metan", "text": "9", "show": cmp(side("前", "a"), side("後", "b"))},   // リストを持つ図解も同じ中身なら同じ番号
		{"who": "metan", "text": "10", "show": cmp(side("前", "x"), side("後", "b"))},  // 左の項目だけが違う
		{"who": "metan", "text": "11", "show": cmp(side("前X", "a"), side("後", "b"))}, // 左の見出しだけが違う
		{"who": "metan", "text": "12", "show": cmp(side("前", "a"), side("後X", "b"))}, // 右の見出しだけが違う
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
	if want := []int{-1, 0, 0, 1, -1, -1, 0, 2, 3, 2, 4, 5, 6}; !reflect.DeepEqual(got, want) {
		t.Errorf("行ごとの図解の番号: got %v want %v", got, want)
	}
	if len(table) != 7 || !reflect.DeepEqual(table[:2], []showData{{Type: "keyword", Text: "A"}, {Type: "keyword", Text: "B"}}) ||
		table[3].Right.Items[0] != "c" {
		t.Errorf("図解の表: got %+v (want 重要語 A, B と比較 5 つ。4 つ目の右の項目は c)", table)
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
