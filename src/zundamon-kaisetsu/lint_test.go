package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// 試験用の台本 (skill の examples/review-bench) に仕込んだ 7 個を、lint がちょうど出す (ほかは出さない)。
// 正解表は同じディレクトリの answer_key.md。
func TestLintReviewBench(t *testing.T) {
	env := testEnv(t)
	s, err := loadScript(filepath.Join(env.SkillDir, "examples", "review-bench", "script.json"), env)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, is := range lintScript(s) {
		got = append(got, fmt.Sprintf("%d:%s:%v", is.Line, is.Rule, is.Hint))
	}
	want := []string{"3:noda:false", "4:show-across-chapter:false", "5:metan-run:true", "6:kanji-number:true", "7:long:false", "9:pointing:false", "10:pronoun:false"}
	if !slices.Equal(got, want) {
		t.Errorf("lint の結果が正解表と違う:\n got %v\nwant %v", got, want)
	}
}

// 規則ごとの境目と、誤検出しない形
func TestLintRules(t *testing.T) {
	run := func(lines ...map[string]any) []string {
		var raw []any
		for _, l := range lines {
			raw = append(raw, l)
		}
		env := testEnv(t)
		s, err := loadScript(writeScript(t, map[string]any{"lines": raw}), env)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, is := range lintScript(s) {
			out = append(out, fmt.Sprintf("%d:%s", is.Line, is.Rule))
		}
		return out
	}
	m := func(who, text string, kv ...any) map[string]any {
		l := map[string]any{"who": who, "text": text}
		for i := 0; i+1 < len(kv); i += 2 {
			l[kv[i].(string)] = kv[i+1]
		}
		return l
	}
	for name, tc := range map[string]struct {
		lines []map[string]any
		want  []string
	}{
		"60 字ちょうどは通す":   {[]map[string]any{m("metan", strings.Repeat("あ", 60))}, nil},
		"61 字は警告":       {[]map[string]any{m("metan", strings.Repeat("あ", 61))}, []string{"0:long"}},
		"なのだ 1 回は通す":    {[]map[string]any{m("zundamon", "そうなのだ。")}, nil},
		"めたんの のだ は見ない":  {[]map[string]any{m("metan", "そうなのだ、なのだ")}, nil},
		"めたんの ぼく":       {[]map[string]any{m("metan", "ぼくはね")}, []string{"0:pronoun"}},
		"慣用語の漢数字は通す":    {[]map[string]any{m("metan", "十分に一緒で一般的で統一された一番の話よ")}, nil},
		"助数詞つきの漢数字":     {[]map[string]any{m("metan", "三人と十二個")}, []string{"0:kanji-number", "0:kanji-number"}},
		"上の方 は図を指していない": {[]map[string]any{m("zundamon", "ずっと上の方だね")}, nil},
		"チャプターで null を書けば通す": {[]map[string]any{
			m("metan", "a", "show", map[string]any{"type": "keyword", "text": "語"}),
			m("zundamon", "b", "chapter", "次", "show", nil)}, nil},
		"null で消した後のチャプターは通す": {[]map[string]any{
			m("metan", "a", "show", map[string]any{"type": "keyword", "text": "語"}),
			m("metan", "b", "show", nil),
			m("zundamon", "c", "chapter", "次")}, nil},
		// 以下は敵対的レビュー (2026-10-08) が壊した入力
		"のだろう・のだから は数えない": {[]map[string]any{m("zundamon", "どうしてなのだろう？知っているのだから、いいのだ")}, nil},
		"文末の のだ 2 回":      {[]map[string]any{m("zundamon", "そうなのだ。いいのだ！")}, []string{"0:noda"}},
		"慣用語を取り除いてから数える":  {[]map[string]any{m("metan", "一日中作業して、一日で終わったわ")}, []string{"0:kanji-number"}},
		"大きな数と 〇":         {[]map[string]any{m("metan", "一〇〇件と約三万件と五億円")}, []string{"0:kanji-number", "0:kanji-number", "0:kanji-number"}},
		"概数は数えない":         {[]map[string]any{m("metan", "数十個と何百人")}, nil},
		"左側の は図を指していない":   {[]map[string]any{m("zundamon", "左側の車線を走るのだ")}, nil},
		// 2 周目: 1 周目に足した「僕」「ボク」が「ボクシング」「公僕」で止めた。ひらがなの「ぼく」だけを見る
		"ボクシング・公僕は一人称ではない": {[]map[string]any{m("metan", "ボクシングを見たわ。公僕として")}, nil},
		"のだっ・のだ〜も文末":       {[]map[string]any{m("zundamon", "そうなのだっ。いいのだ〜")}, []string{"0:noda"}},
		"図書館・図表は図を指していない":  {[]map[string]any{m("zundamon", "その図書館と以上の図表なのだ")}, nil},
		"この図は は図を指す":       {[]map[string]any{m("metan", "この図は大事よ")}, []string{"0:pointing"}},
		"チャプターの行の next は消したことにならない": {[]map[string]any{
			m("metan", "a", "show", map[string]any{"type": "code", "lines": []any{"x", "y"}, "steps": []any{[]any{1}, []any{2}}}),
			m("zundamon", "b", "chapter", "次", "show", "next")}, []string{"1:show-across-chapter"}},
		"中身の無いチャプター名は数えない": {[]map[string]any{
			m("metan", "a", "show", map[string]any{"type": "keyword", "text": "語"}),
			m("zundamon", "b", "chapter", "")}, nil},
		"next の行を挟んでもめたんの連続は続く": {[]map[string]any{
			m("metan", "a", "show", map[string]any{"type": "code", "lines": []any{"x", "y"}, "steps": []any{[]any{1}, []any{2}}}),
			m("metan", "b", "show", "next"), m("metan", "c"), m("metan", "d")}, []string{"0:metan-run"}},
		"next で段を進める連続は数えない": {[]map[string]any{
			m("metan", "a", "show", map[string]any{"type": "code", "lines": []any{"x", "y", "z"}, "steps": []any{[]any{1}, []any{2}, []any{3}}}),
			m("metan", "b", "show", "next"), m("metan", "c", "show", "next"), m("zundamon", "d")}, nil},
	} {
		if got := run(tc.lines...); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v (want %v)", name, got, tc.want)
		}
	}
}

// 入口: dispatch から lint が届き、警告があれば rc=1 (error)、目安 (めたんの連続発話) だけなら rc=0
func TestLintCommandExitCode(t *testing.T) {
	run := func(lines ...any) (string, error) {
		env := testEnv(t)
		var out strings.Builder
		env.Stdout = &out
		err := dispatch([]string{"lint", writeScript(t, map[string]any{"lines": lines})}, env)
		return out.String(), err
	}
	l := func(who, text string) map[string]any { return map[string]any{"who": who, "text": text} }
	if out, err := run(l("zundamon", "そうなのだ、なのだ")); err == nil || !strings.Contains(out, "0\t警告\tnoda\t") {
		t.Errorf("警告があるのに rc=0 か、出力が違う: %v\n%s", err, out)
	}
	if out, err := run(l("metan", "a"), l("metan", "b"), l("metan", "c")); err != nil || !strings.Contains(out, "0\t目安\tmetan-run\t") {
		t.Errorf("目安だけで rc=1 になった: %v\n%s", err, out)
	}
	if out, err := run(l("metan", "三人で行くわ")); err != nil || !strings.Contains(out, "0\t目安\tkanji-number\t") {
		t.Errorf("漢数字 (目安) だけで rc=1 になった: %v\n%s", err, out)
	}
	if out, err := run(l("metan", "a")); err != nil || !strings.Contains(out, "(警告なし)") {
		t.Errorf("警告の無い台本: %v\n%s", err, out)
	}
}
