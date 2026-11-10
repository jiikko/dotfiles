package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"zundamon-kaisetsu/internal/lint"
	"zundamon-kaisetsu/internal/script"
)

// 試験用の台本と架空の資料 (skill の examples/review-bench) を突き合わせた候補を固定する (正解表 answer_key.md の「8割」を含む)
func TestSourceCandidatesReviewBench(t *testing.T) {
	env := testEnv(t)
	dir := filepath.Join(env.SkillDir, "examples", "review-bench")
	s, err := script.LoadScript(filepath.Join(dir, "script.json"), env)
	must(t, err)
	src, err := os.ReadFile(filepath.Join(dir, "source.md"))
	must(t, err)
	var got []string
	for _, is := range lint.SourceCandidates(s, string(src)) {
		if !is.Hint {
			t.Errorf("突き合わせの候補は目安のはず: %+v", is)
		}
		got = append(got, fmt.Sprintf("%d:%s:%s", is.Line, is.Rule, between(is.Msg, "「", "」")))
	}
	want := []string{"9:source-number:1回", "12:source-number:8割", "15:source-word:SOLIDWORKS", "16:source-word:Boot", "16:source-word:Camp", "4:intro-boundary:それじゃあ、本題に入るわね"}
	if !slices.Equal(got, want) {
		t.Errorf("突き合わせの候補が違う:\n got %v\nwant %v", got, want)
	}
}

func between(s, a, b string) string {
	i := strings.Index(s, a)
	if i < 0 {
		return ""
	}
	s = s[i+len(a):]
	if j := strings.Index(s, b); j >= 0 {
		return s[:j]
	}
	return s
}

// 図解の中の数字も見る (字幕だけでは、図解の誤った数字が素通りする)
func TestSourceCandidatesShow(t *testing.T) {
	env := testEnv(t)
	s, err := script.LoadScript(writeScript(t, map[string]any{"lines": []any{map[string]any{"who": "metan", "text": "調査の結果よ",
		"show": map[string]any{"type": "keyword", "text": "Windows 85.03%", "sub": "Steam 調査"}}}}), env)
	must(t, err)
	var got []string
	for _, is := range lint.SourceCandidates(s, "Windows 95.03% Steam") {
		got = append(got, is.Rule+":"+between(is.Msg, "「", "」"))
	}
	if !slices.Equal(got, []string{"source-number:85.03%"}) {
		t.Errorf("図解の中の数字を見ていない: %v", got)
	}
	// 比較の見出し・項目と重要語の補足も見る
	s, err = script.LoadScript(writeScript(t, map[string]any{"lines": []any{map[string]any{"who": "metan", "text": "比べるわ",
		"show": map[string]any{"type": "compare", "left": map[string]any{"title": "2018年", "items": []any{"6割"}},
			"right": map[string]any{"title": "今", "items": []any{"8割"}}}},
		map[string]any{"who": "metan", "text": "要点よ", "show": map[string]any{"type": "keyword", "text": "語", "sub": "3倍に増えた"}}}}), env)
	must(t, err)
	got = nil
	for _, is := range lint.SourceCandidates(s, "2018年と6割") {
		got = append(got, is.Rule+":"+between(is.Msg, "「", "」"))
	}
	if !slices.Equal(got, []string{"source-number:8割", "source-number:3倍"}) {
		t.Errorf("比較の項目・重要語の補足の数字を見ていない: %v", got)
	}
	// 箇条書きの見出し・項目も見る
	s, err = script.LoadScript(writeScript(t, map[string]any{"lines": []any{map[string]any{"who": "metan", "text": "理由は 2 つよ",
		"show": map[string]any{"type": "list", "title": "2018年の調査", "items": []any{"6割", "8割"}}}}}), env)
	must(t, err)
	got = nil
	for _, is := range lint.SourceCandidates(s, "6割") {
		got = append(got, is.Rule+":"+between(is.Msg, "「", "」"))
	}
	if !slices.Equal(got, []string{"source-number:8割", "source-number:2018年"}) {
		t.Errorf("箇条書きの見出し・項目の数字を見ていない: %v", got)
	}
	if _, err := lint.ReadSource(writeEmpty(t)); err == nil {
		t.Error("空の資料を黙って読んだ")
	}
}

func writeEmpty(t *testing.T) string {
	p := filepath.Join(t.TempDir(), "empty.md")
	must(t, os.WriteFile(p, []byte("  \n"), 0o644))
	return p
}

func TestSourceCandidatesRules(t *testing.T) {
	run := func(source string, texts ...string) []string {
		var raw []any
		for _, x := range texts {
			raw = append(raw, map[string]any{"who": "metan", "text": x})
		}
		env := testEnv(t)
		s, err := script.LoadScript(writeScript(t, map[string]any{"lines": raw}), env)
		must(t, err)
		var out []string
		for _, is := range lint.SourceCandidates(s, source) {
			out = append(out, is.Rule+":"+between(is.Msg, "「", "」"))
		}
		return out
	}
	for name, tc := range map[string]struct {
		source string
		texts  []string
		want   []string
	}{
		"空白と全角を揃えて比べる":      {"回答者の 6 割と９５．０３％", []string{"6割と95.03%と６割"}, nil},
		"資料に無い数字":           {"6 割", []string{"8割"}, []string{"source-number:8割"}},
		"桁区切りのカンマ":          {"1,000件", []string{"1000件"}, nil},
		"数える〜つは見ない":         {"", []string{"1つ目と2つ"}, nil},
		"英字の語は大文字小文字を区別しない": {"windows と mac", []string{"WindowsとMac"}, nil},
		"英字の語は語の単位で比べる":     {"RESTful API", []string{"RESTの話"}, []string{"source-word:REST"}},
		"同じ行の同じ語は 1 回":      {"", []string{"VPNとVPN"}, []string{"source-word:VPN"}},
		// 部分一致で、資料の数字の中に含まれる別の数字が素通りした
		"95.03% の中の 3% は別の数字": {"95.03%", []string{"3%"}, []string{"source-number:3%"}},
		"16割 の中の 6割 は別の数字":    {"16割", []string{"6割"}, []string{"source-number:6割"}},
		"2018年度 と 2018年 は別":   {"2018年度", []string{"2018年"}, []string{"source-number:2018年"}},
		"パーセントは % に揃える":       {"95パーセント", []string{"95%"}, nil},
		"か月・ドルも単位として見る":       {"3か月と1500ドル", []string{"5か月と500ドル"}, []string{"source-number:5か月", "source-number:500ドル"}},
		"単位の無い数字は見ない":         {"", []string{"5と12"}, nil},
		// 2 周目: 万の後ろの単位・範囲の左・符号・小数のカンマ・ヵ月と才と小文字
		"万の後ろの単位も見る":  {"100万ドルと3万人", []string{"100万円と3万円"}, []string{"source-number:100万円", "source-number:3万円"}},
		"範囲の左の数字も見る":  {"10〜20%", []string{"15〜20%"}, []string{"source-number:15%"}},
		"符号を残す":       {"5%増加", []string{"-5%"}, []string{"source-number:-5%"}},
		"小数のカンマは残す":   {"105%", []string{"10,5%"}, []string{"source-number:10,5%"}},
		"桁区切りのカンマは除く": {"1000円", []string{"1,000円"}, nil},
		"ヵ月・才・小文字の単位": {"gb", []string{"5ヵ月と20才と32gb"}, []string{"source-number:5ヵ月", "source-number:20才", "source-number:32GB"}},
		// 月日: 台本の「9月22日」は月まで合わせた 1 組にして、資料の 9/22・2024/9/22・2024-09-22・9月22日 と比べる
		"資料の 9/22 の書き方の月日":          {"3/25 (火) と ２０２４／９／２２ と 2025-04-15", []string{"3月25日と9月22日と2024年と4月15日"}, nil},
		"別の月の同じ日は出る":                {"9月22日", []string{"10月22日"}, []string{"source-number:10月22日"}},
		"資料の 9月22日 の月だけ・日だけ":        {"9月22日", []string{"22日と9月"}, nil},
		"別の月日の部品では通さない":             {"9月10日と10月22日", []string{"9月22日"}, []string{"source-number:9月22日"}},
		"資料が月と日を離して書く形は出る (受ける誤検出)": {"9月に障害。22日に復旧", []string{"9月22日に復旧"}, []string{"source-number:9月22日"}},
		"資料の 9/22 から 22日 は作らない":     {"9/22", []string{"22日"}, []string{"source-number:22日"}},
		"数字か / が続く形は月日と見ない":         {"posts/33259/12 と 9/22/2024 と 112/5", []string{"9月22日と12月5日"}, []string{"source-number:9月22日", "source-number:12月5日"}},
		"成り立たない月日は分けて見る":            {"13/40", []string{"13月40日"}, []string{"source-number:13月", "source-number:40日"}},
		"月日も文の中の順に出す":               {"", []string{"5か月と10月22日と3人"}, []string{"source-number:5か月", "source-number:10月22日", "source-number:3人"}},
	} {
		if got := run(tc.source, tc.texts...); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v (want %v)", name, got, tc.want)
		}
	}
	// 導入の区切り: 2 つ目のチャプターの行か手前 3 行に「本題」があれば通す
	chap := func(boundary string) []string {
		env := testEnv(t)
		s, err := script.LoadScript(writeScript(t, map[string]any{"lines": []any{
			map[string]any{"who": "metan", "text": "a", "chapter": "はじめに"},
			map[string]any{"who": "metan", "text": boundary},
			map[string]any{"who": "zundamon", "text": "b？", "chapter": "本"},
		}}), env)
		must(t, err)
		var out []string
		for _, is := range lint.SourceCandidates(s, "") {
			out = append(out, is.Rule)
		}
		return out
	}
	if got := chap("それじゃあ、本題に入るわね"); len(got) != 0 {
		t.Errorf("区切りがあるのに候補が出た: %v", got)
	}
	if got := chap("次へ"); !slices.Equal(got, []string{"intro-boundary"}) {
		t.Errorf("区切りが無いのに候補が出ない: %v", got)
	}
}

// 入口: lint --source が dispatch から届き、候補は目安なので rc に効かない
func TestLintSourceOption(t *testing.T) {
	env := testEnv(t)
	var out strings.Builder
	env.Stdout = &out
	src := filepath.Join(t.TempDir(), "src.md")
	must(t, os.WriteFile(src, []byte("6 割"), 0o644))
	path := writeScript(t, map[string]any{"lines": []any{map[string]any{"who": "metan", "text": "8割よ", "chapter": "a"}}})
	if err := dispatch([]string{"lint", path, "--source", src}, env); err != nil {
		t.Errorf("候補 (目安) だけで rc=1 になった: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "\t目安\tsource-number\t") {
		t.Errorf("--source の候補が出ていない:\n%s", out.String())
	}
	if err := dispatch([]string{"lint", path, "--source", filepath.Join(t.TempDir(), "無い.md")}, testEnv(t)); err == nil {
		t.Error("読めない資料を黙って通した")
	}
}
