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
	dict, err := loadReadingsDict(env)
	must(t, err)
	var got []string
	for _, is := range lintScript(s, dict) {
		got = append(got, fmt.Sprintf("%d:%s:%v", is.Line, is.Rule, is.Hint))
	}
	want := []string{"0:chapters:true", "3:noda:false", "4:show-across-chapter:false", "5:metan-run:true", "6:kanji-number:true", "7:long:false", "9:pointing:false", "10:pronoun:false", "15:readings-missing:true"}
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
		for _, is := range lintScript(s, nil) {
			if is.Rule == "chapters" { // チャプターの数の目安は TestLintMoreRules が見る (ここの小さな台本では毎回出る)
				continue
			}
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
		"60 字ちょうどは通す":              {[]map[string]any{m("metan", strings.Repeat("あ", 60))}, nil},
		"61 字は警告":                  {[]map[string]any{m("metan", strings.Repeat("あ", 61))}, []string{"0:long"}},
		"なのだ 1 回は通す":               {[]map[string]any{m("zundamon", "そうなのだ。")}, nil},
		"めたんの のだ は noda ではなく口調の目安": {[]map[string]any{m("metan", "そうなのだ、なのだ")}, []string{"0:tone"}},
		"めたんの ぼく":                  {[]map[string]any{m("metan", "ぼくはね")}, []string{"0:pronoun"}},
		"慣用語の漢数字は通す":               {[]map[string]any{m("metan", "十分に一緒で一般的で統一された一番の話よ")}, nil},
		"助数詞つきの漢数字":                {[]map[string]any{m("metan", "三人と十二個")}, []string{"0:kanji-number", "0:kanji-number"}},
		"上の方 は図を指していない":            {[]map[string]any{m("zundamon", "ずっと上の方だね")}, nil},
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

// issue 684 で足した規則 (警告: duplicate・readings-missing・show-sentence / 目安: sentences・tone・style-long・chapters・shows-per-chapter・chapter-question)
func TestLintMoreRules(t *testing.T) {
	runAll := func(dict map[string]string, raw map[string]any) []string {
		env := testEnv(t)
		s, err := loadScript(writeScript(t, raw), env)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, is := range lintScript(s, dict) {
			out = append(out, fmt.Sprintf("%d:%s:%v", is.Line, is.Rule, is.Hint))
		}
		return out
	}
	// run はチャプターの数の目安を除く (小さな台本では毎回出る)。チャプターの規則を見るケースは runAll
	run := func(dict map[string]string, raw map[string]any) []string {
		var out []string
		for _, x := range runAll(dict, raw) {
			if !strings.Contains(x, ":chapters:") {
				out = append(out, x)
			}
		}
		return out
	}
	l := func(who, text string, kv ...any) map[string]any {
		m := map[string]any{"who": who, "text": text}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i].(string)] = kv[i+1]
		}
		return m
	}
	lines := func(ls ...map[string]any) map[string]any {
		var raw []any
		for _, x := range ls {
			raw = append(raw, x)
		}
		return map[string]any{"lines": raw}
	}
	kw := map[string]any{"type": "keyword", "text": "語"}
	for name, tc := range map[string]struct {
		dict map[string]string
		raw  map[string]any
		want []string
	}{
		"同じ話者の同じ字幕が続く":  {nil, lines(l("metan", "同じ"), l("metan", "同じ")), []string{"1:duplicate:false"}},
		"別の話者の同じ相づちは通す": {nil, lines(l("metan", "うん。"), l("zundamon", "うん。")), nil},
		// 敵対的レビュー: 部分一致で別の英単語の中に当てていた (STORAGE の中の RAG)。合流すると音声まで壊れる
		"英字の語は語全体だけ":         {map[string]string{"RAG": "ラグ", "REST": "レスト"}, lines(l("metan", "STORAGEとRESTfulとRESTOREの話")), nil},
		"記号だけの語は見ない":         {map[string]string{"〜": "から"}, lines(l("zundamon", "すごいのだ〜")), nil},
		"辞書の語が readings に無い": {map[string]string{"SOLIDWORKS": "ソリッドワークス"}, lines(l("metan", "SOLIDWORKSの話")), []string{"0:readings-missing:true"}},
		"辞書の語が readings にあれば通す": {map[string]string{"SOLIDWORKS": "ソリッドワークス"},
			map[string]any{"readings": map[string]any{"SOLIDWORKS": "ソリッドワークス"}, "lines": []any{l("metan", "SOLIDWORKSの話")}}, nil},
		"read のある行は見ない": {map[string]string{"SOLIDWORKS": "ソリッドワークス"}, lines(l("metan", "SOLIDWORKSの話", "read", "ソリッドワークスの話")), nil},
		"3 文は目安":        {nil, lines(l("metan", "そう。違うの。本当よ。")), []string{"0:sentences:true"}},
		"2 文は通す":        {nil, lines(l("metan", "そう。違うの。")), nil},
		"ずんだもんの わ":      {nil, lines(l("zundamon", "知らなかったわ")), []string{"0:tone:true"}},
		"長い行の声のスタイル":    {nil, lines(l("zundamon", "ここはとても長い説明の行なので普通の声で話すべきなのだ", "style_id", 76)), []string{"0:style-long:true"}},
		"図解の補足が文":       {nil, lines(l("metan", "a", "show", map[string]any{"type": "keyword", "text": "語", "sub": "これは文です。"})), []string{"0:show-sentence:false"}},
		"比較の項目が文": {nil, lines(l("metan", "a", "show", map[string]any{"type": "compare",
			"left": map[string]any{"title": "左", "items": []any{"速い。"}}, "right": map[string]any{"title": "右", "items": []any{"遅い"}}})), []string{"0:show-sentence:false"}},
		"1 チャプターの図解が 3 回": {nil, lines(
			l("metan", "a", "chapter", "1", "show", kw), l("metan", "b", "show", kw), l("zundamon", "c", "show", kw)),
			[]string{"0:shows-per-chapter:true"}},
		"2 つ目のチャプターがずんだもんの質問でない": {nil, lines(l("metan", "a", "chapter", "1"), l("metan", "b", "chapter", "2")),
			[]string{"1:chapter-question:true"}},
		"2 つ目のチャプターがずんだもんでも質問でなければ目安": {nil, lines(l("metan", "a", "chapter", "1"), l("zundamon", "やっぱりそうなのだ！", "chapter", "2")),
			[]string{"1:chapter-question:true"}},
		"2 つ目のチャプターがずんだもんの質問なら通す": {nil, lines(l("metan", "a", "chapter", "1"), l("zundamon", "b？", "chapter", "2")),
			nil},
	} {
		if got := run(tc.dict, tc.raw); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v (want %v)", name, got, tc.want)
		}
	}
	// チャプターの数: 4〜6 は通す。長い台本 (87 行超) は 7 個以上でも通す
	ch := func(n, total int) map[string]any {
		var raw []any
		for i := 0; i < total; i++ {
			x := l("zundamon", fmt.Sprintf("行%d？", i))
			if i < n {
				x["chapter"] = fmt.Sprint(i)
			}
			raw = append(raw, x)
		}
		return map[string]any{"lines": raw}
	}
	for _, tc := range []struct {
		n, total int
		hint     bool
	}{{0, 10, true}, {4, 10, false}, {6, 10, false}, {7, 10, true}, {7, 90, false}, {3, 90, true}} {
		got := runAll(nil, ch(tc.n, tc.total))
		if has := slices.Contains(got, "0:chapters:true"); has != tc.hint {
			t.Errorf("チャプター %d 個・%d 行: 目安=%v (want %v) %v", tc.n, tc.total, has, tc.hint, got)
		}
	}
}
