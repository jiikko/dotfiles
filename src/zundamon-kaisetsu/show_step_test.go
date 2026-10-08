package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// stepLines は show の並びから台本の行を作る (show の無い行は nil で表す)。
func stepLines(shows ...any) []map[string]any {
	lines := make([]map[string]any, len(shows))
	for i, sv := range shows {
		lines[i] = map[string]any{"who": "metan", "text": "x"}
		if sv != nil {
			lines[i]["show"] = sv
		}
	}
	return lines
}

func decodeShow(t *testing.T, s string) any {
	t.Helper()
	var v any
	must(t, decodeJSON([]byte(s), &v))
	return v
}

func intsOrNil(p []*int) []any {
	out := make([]any, len(p))
	for i, v := range p {
		if v != nil {
			out[i] = *v
		}
	}
	return out
}

// 比較の build: 出した行が 0 段目、"next" ごとに 1 段進み、show の無い行は直前の段のまま。別の図解で段は 0 に戻る
func TestLineShowsSteps(t *testing.T) {
	cmp := `{"type": "compare", "build": true, "left": {"title": "前", "items": ["a", "b", "c"]}, "right": {"title": "後", "items": ["d", "e", "f"]}}`
	code := `{"type": "code", "lines": ["x", "y", "z"], "steps": [[1], [2, 3]]}`
	kw := `{"type": "keyword", "text": "語"}`
	lines := stepLines(
		decodeShow(t, cmp), nil, "next", "next", nil, // 0..4: 比較 (3 段)
		decodeShow(t, kw),           // 5: 段の無い図解
		decodeShow(t, code), "next", // 6..7: コード (2 段)
		decodeShow(t, cmp), // 8: 同じ比較をもう一度出す (表は共有、段は 0 から)
		nil,
	)
	idx, steps, table, err := lineShows("s.json", lines)
	must(t, err)
	wantSteps := []any{0, 0, 1, 2, 2, nil, 0, 1, 0, 0}
	if got := intsOrNil(steps); !equalAny(got, wantSteps) {
		t.Errorf("段: got %v want %v", got, wantSteps)
	}
	wantIdx := []any{0, 0, 0, 0, 0, 1, 2, 2, 0, 0}
	if got := intsOrNil(idx); !equalAny(got, wantIdx) {
		t.Errorf("図解の番号: got %v want %v", got, wantIdx)
	}
	if len(table) != 3 || !table[0].Build || len(table[2].Steps) != 2 {
		t.Errorf("図解の表: %+v", table)
	}
	if n := table[0].stepCount(); n != 3 {
		t.Errorf("比較の段の数: %d (項目の数 3 のはず)", n)
	}
}

func equalAny(a, b []any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}

// "next" の誤用は合成の前に止める (図解が無い・段の無い図解・段を使い切った・消した後)
func TestLineShowsRejectsBadNext(t *testing.T) {
	cmp := `{"type": "compare", "build": true, "left": {"title": "前", "items": ["a", "b"]}, "right": {"title": "後", "items": ["d", "e"]}}`
	cases := []struct {
		name  string
		shows []any
		want  string
	}{
		{"図解が無い", []any{nil, "next"}, "図解を出している間にだけ"},
		{"消した後", []any{decodeShow(t, cmp), nil}, ""}, // 前提: 消さなければ通る
		{"null の後", []any{decodeShow(t, cmp), "NULL"}, "図解を出している間にだけ"},
		{"段の無い図解", []any{decodeShow(t, `{"type": "keyword", "text": "語"}`), "next"}, "段のある図解"},
		{"段を使い切った", []any{decodeShow(t, cmp), "next", "next"}, "next\" が多い (この図解は 2 段"},
		{"綴り違い", []any{decodeShow(t, cmp), "nxt"}, "\"next\" (段を進める) で書く"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lines := stepLines(c.shows...)
			for i, v := range c.shows {
				if v == "NULL" { // stepLines は nil を「show の無い行」にするので、null の行はここで作る
					lines[i]["show"] = nil
				}
			}
			if c.name == "null の後" {
				lines = append(lines, stepLines("next")...)
			}
			_, _, _, err := lineShows("s.json", lines)
			if c.want == "" {
				must(t, err)
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v want %q", err, c.want)
			}
		})
	}
}

// 段の欄の検査: build の型・1 段しかない build・steps の形・highlight との併記
func TestShowStepFieldsRejected(t *testing.T) {
	cases := []struct{ show, want string }{
		{`{"type": "compare", "build": "yes", "left": {"title": "a", "items": ["b", "c"]}, "right": {"title": "d", "items": ["e"]}}`, "build は true か false"},
		{`{"type": "compare", "build": true, "left": {"title": "a", "items": ["b"]}, "right": {"title": "d", "items": ["e"]}}`, "2 個以上あるときだけ"},
		{`{"type": "compare", "build": true, "left": {"title": "a", "items": ["b", "c", "x"]}, "right": {"title": "d", "items": ["e"]}}`, "左右の項目の数を揃えて書く (同じ番号の項目を対にして 1 段ずつ出す。実際: 3 個と 1 個)"},
		{`{"type": "code", "lines": ["x", "y"], "steps": [[1], [2, 1], [1, 2]]}`, "steps[2] が 1 つ前の段と同じ強調"},
		{`{"type": "code", "lines": ["x", "y"], "steps": [[1]]}`, "2 段以上"},
		{`{"type": "code", "lines": ["x", "y"], "steps": [1, 2]}`, "steps[0] は行番号"},
		{`{"type": "code", "lines": ["x", "y"], "steps": [[1], [3]]}`, "steps[1] は 1〜2 の行番号"},
		{`{"type": "code", "lines": ["x", "y"], "steps": [[1], [2]], "highlight": [1]}`, "highlight と steps を両方は書けない"},
	}
	for _, c := range cases {
		_, _, _, err := lineShows("s.json", stepLines(decodeShow(t, c.show)))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v want %q", c.show, err, c.want)
		}
	}
	// build: false は段の無い比較 (書いても書かなくても同じ)
	_, steps, _, err := lineShows("s.json", stepLines(decodeShow(t, `{"type": "compare", "build": false, "left": {"title": "a", "items": ["b"]}, "right": {"title": "d", "items": ["e"]}}`)))
	must(t, err)
	if steps[0] != nil {
		t.Errorf("build: false の比較に段がある: %d", *steps[0])
	}
	// 空の段 (何も強調しない段) は書ける
	_, steps, table, err := lineShows("s.json", stepLines(decodeShow(t, `{"type": "code", "lines": ["x", "y"], "steps": [[], [2, 2, 1]]}`)))
	must(t, err)
	if steps[0] == nil || len(table[0].Steps[0]) != 0 || len(table[0].Steps[1]) != 2 || table[0].Steps[1][0] != 1 {
		t.Errorf("空の段・重複と並びの正規化: %+v", table[0].Steps)
	}
}

// 段のある図解を使うと行に step が載り、使わない台本では載らない (Python 版の golden と同じデータのまま)
func TestBuildDataCarriesStepOnlyForSteppedShows(t *testing.T) {
	data, err := buildWithShows(t, func(_ string, lines []any) {
		lines[0].(map[string]any)["show"] = decodeShow(t, `{"type": "code", "lines": ["x", "y"], "steps": [[1], [2]]}`)
		lines[1].(map[string]any)["show"] = "next"
		lines[2].(map[string]any)["show"] = nil
		lines[3].(map[string]any)["show"] = decodeShow(t, `{"type": "compare", "build": true, "left": {"title": "前", "items": ["a", "b"]}, "right": {"title": "後", "items": ["c", "d"]}}`)
	})
	must(t, err)
	// 段の中身は図解の表 (shows) に載ってプレイヤーへ届く (Go の構造体だけ見ると、JSON に出ない欄を見逃す)
	sb, err := json.Marshal(data.Shows)
	must(t, err)
	for _, want := range []string{`"steps":[[1],[2]]`, `"build":true`} {
		if !strings.Contains(string(sb), want) {
			t.Errorf("プレイヤーのデータの図解に %s が無い: %s", want, sb)
		}
	}
	b, err := json.Marshal(data.Lines)
	must(t, err)
	var lines []map[string]any
	must(t, json.Unmarshal(b, &lines))
	for i, want := range []any{0.0, 1.0, nil, 0.0} {
		if got := lines[i]["step"]; got != want {
			t.Errorf("行 %d の step: %v want %v", i, got, want)
		}
	}
	plain, err := buildWithShows(t, func(_ string, lines []any) {
		lines[0].(map[string]any)["show"] = decodeShow(t, `{"type": "keyword", "text": "語"}`)
	})
	must(t, err)
	pb, err := json.Marshal(plain)
	must(t, err)
	if strings.Contains(string(pb), `"step"`) {
		t.Error("段のある図解を使わない台本のデータに step がある")
	}
}
