package main

import (
	"html"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"zundamon-kaisetsu/internal/build"
	"zundamon-kaisetsu/internal/show"
)

// ラベルの改行の判定 (issue 812): 1 行で出す欄の折り返し・語の途中の改行・1 字だけの行 (書記素で数える)・はみ出し・知らない種類・
// 測れなかったラベル・字の食い違い・壊れた計測を問題にし、語の境目での折り返しは通す
func TestLabelProblems(t *testing.T) {
	d := &build.PlayerData{
		Title: "タイトル", Overlays: []string{"title", "topic"},
		Chapters: []build.ChapterData{{Title: "はじめに"}, {Title: "まとめ"}},
		Shows:    []show.Data{{Type: "keyword", Text: "モックだらけのテスト"}, {Type: "image"}},
		Lines:    []build.TimelineLine{{}, {Show: new(int)}},
	}
	line := func(key, kind, text string, lines []string, breaks []bool, glen []int) build.LabelMeasure {
		return build.LabelMeasure{Key: key, Kind: kind, Text: text, Lines: lines, Breaks: breaks, Glen: glen, Clip: []int{0, 0}}
	}
	ok := []build.LabelMeasure{
		line("title", "line", "タイトル", []string{"タイトル"}, []bool{}, []int{4}),
		line("topic:0", "line", "はじめに", []string{"はじめに"}, []bool{}, []int{4}),
		line("topic:1", "line", "まとめ", []string{"まとめ"}, []bool{}, []int{3}),
		{Key: "show:0", Kind: "box", Clip: []int{1, 0}}, // 丸めの 1 px は数えない
		line("show:0:text", "wrap", "モックだらけのテスト", []string{"モックだらけの", "テスト"}, []bool{true}, []int{7, 3}),
	}
	if p := build.LabelProblems(d, ok); len(p) != 0 {
		t.Fatalf("問題の無い計測で問題が出た: %q", p)
	}
	for name, c := range map[string]struct {
		edit func(ms []build.LabelMeasure) []build.LabelMeasure
		want string
	}{
		"タイトルが 2 行": {func(ms []build.LabelMeasure) []build.LabelMeasure {
			ms[0] = line("title", "line", "タイトル", []string{"タイト", "ル"}, []bool{false}, []int{3, 1})
			return ms
		}, "左上のタイトル: 1 行に収まらず 2 行になる (タイト / ル)"},
		"語の途中で改行": {func(ms []build.LabelMeasure) []build.LabelMeasure {
			ms[4] = line("show:0:text", "wrap", "モックだらけのテスト", []string{"モックだらけのテ", "スト"}, []bool{false}, []int{8, 2})
			return ms
		}, "図解 0 (keyword。lines[1] から) の text: 語の途中で改行する (モックだらけのテ / スト)"},
		"1 字だけの行 (合成の濁点は 1 字)": {func(ms []build.LabelMeasure) []build.LabelMeasure {
			ms[4] = line("show:0:text", "wrap", "モックだらけのテスト", []string{"モックだらけのテス", "ト"}, []bool{true}, []int{9, 1})
			return ms
		}, ": 1 字だけの行ができる (モックだらけのテス / ト)"},
		"カードのはみ出し": {func(ms []build.LabelMeasure) []build.LabelMeasure {
			ms[3].Clip = []int{0, 12}
			return ms
		}, "箱か舞台からはみ出して隠れる (横 0 px / 縦 12 px)"},
		"文字のラベルのはみ出し (改行の位置が無い長い英字)": {func(ms []build.LabelMeasure) []build.LabelMeasure {
			ms[1].Clip = []int{310, 0}
			return ms
		}, "トピック名「はじめに」: 箱か舞台からはみ出して隠れる (横 310 px / 縦 0 px)"},
		"はみ出しの量を読めない": {func(ms []build.LabelMeasure) []build.LabelMeasure {
			ms[3].Clip = nil
			return ms
		}, "はみ出しの量を読めない"},
		"知らない種類": {func(ms []build.LabelMeasure) []build.LabelMeasure {
			ms[3].Kind = "boxes"
			return ms
		}, "計測モードが知らない種類"},
		"字が台本と違う (子要素に包まれて直下の字が無い等)": {func(ms []build.LabelMeasure) []build.LabelMeasure {
			ms[4] = line("show:0:text", "wrap", "", []string{""}, []bool{}, []int{0})
			return ms
		}, "が台本 (\"wrap\" \"モックだらけのテスト\") と違う"},
		"行の測り方が壊れている": {func(ms []build.LabelMeasure) []build.LabelMeasure {
			ms[2].Breaks = []bool{true}
			return ms
		}, "行の測り方が壊れている"},
		"測れなかった (箱)": {func(ms []build.LabelMeasure) []build.LabelMeasure {
			return slices.DeleteFunc(ms, func(m build.LabelMeasure) bool { return m.Key == "show:0" })
		}, "図解 0 (keyword。lines[1] から): 測れなかった"},
		"測れなかった (項目)": {func(ms []build.LabelMeasure) []build.LabelMeasure {
			return slices.DeleteFunc(ms, func(m build.LabelMeasure) bool { return m.Key == "show:0:text" })
		}, "の text: 測れなかった"},
	} {
		t.Run(name, func(t *testing.T) {
			p := build.LabelProblems(d, c.edit(slices.Clone(ok)))
			if len(p) != 1 || !strings.Contains(p[0], c.want) {
				t.Errorf("問題 %q に %q が 1 つだけ出るはず", p, c.want)
			}
		})
	}
}

// 測るはずのラベル: 図は測らず、overlays から外した欄は測らず、区切りのカードは cards があり名前が空白だけでないときだけ測る。
// 空白の判定は player の trim (JavaScript) と同じ文字で行う (U+FEFF だけの名前のカードは player が描かない)
func TestExpectedLabels(t *testing.T) {
	keys := func(d *build.PlayerData) []string {
		var ks []string
		for _, e := range build.ExpectedLabels(d) {
			ks = append(ks, e.Key)
		}
		return ks
	}
	d := &build.PlayerData{
		Title: "T", Overlays: []string{"title", "topic"},
		Chapters: []build.ChapterData{{Title: "はじめに"}, {Title: "\ufeff"}},
		Shows: []show.Data{{Type: "image"}, {Type: "list", Title: "理由", Items: []string{"a", "b"}},
			{Type: "compare", Left: &show.CompareSide{Title: "前", Items: []string{"x"}}, Right: &show.CompareSide{Title: "後", Items: []string{"y"}}}},
	}
	want := []string{"title", "topic:0", "topic:1", "show:1:title", "show:1:items[0]", "show:1:items[1]", "show:1",
		"show:2:left.title", "show:2:left.items[0]", "show:2:right.title", "show:2:right.items[0]", "show:2"}
	if got := keys(d); !slices.Equal(got, want) {
		t.Errorf("測るはずの鍵:\n%q\nwant\n%q", got, want)
	}
	d.Cards, d.Overlays, d.Shows = [][2]int{{0, 1}}, []string{"date"}, nil
	if got := keys(d); !slices.Equal(got, []string{"card:0"}) {
		t.Errorf("overlays から外した欄・空白だけの名前のカードを測ろうとしている: %q", got)
	}
}

// player.html の計測モードの入口と結果の置き場は、Go が決め打ちで使う (MeasureLabelsWithChrome)。字句を固定する
func TestPlayerMeasureContract(t *testing.T) {
	b, err := os.ReadFile(testEnv(t).Template())
	must(t, err)
	for _, want := range []string{"location.hash === '#measure'", "pre.id = 'measure-out'"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("player.html に %q が無い (Go の MeasureLabelsWithChrome は #measure で開き #measure-out を読む)", want)
		}
	}
}

// build はラベルの改行の問題を一覧にして、音声の圧縮と書き出しの前に止まる (--allow-label-wraps なら続ける)。dispatch を通す
func TestBuildStopsOnLabelWrap(t *testing.T) {
	path := buildFixtureScript(t, nil)
	bld := func(t *testing.T, allow bool) (string, string, error) {
		env := testEnv(t)
		env.MeasureLabels = fakeLabelMeasure(func(_ *build.PlayerData, ms []build.LabelMeasure) []build.LabelMeasure {
			ms[0].Lines, ms[0].Breaks, ms[0].Glen = []string{"テスト用の台本 ", "<b>&"}, []bool{true}, []int{8, 3}
			return ms
		})
		var stderr strings.Builder
		env.Stderr = &stderr
		out := filepath.Join(t.TempDir(), "out")
		args := []string{"build", path, "-o", out, "--format", "html", "--allow-fast-captions"} // fixture の wav は短く、字幕の速さの検査に先に止められる
		if allow {
			args = append(args, "--allow-label-wraps")
		}
		err := dispatch(args, env) // 先に走らせる (返り値の式の中では stderr を読む方が先に評価される)
		return out + ".html", stderr.String(), err
	}
	out, stderr, err := bld(t, false)
	if err == nil || !strings.Contains(err.Error(), "--allow-label-wraps") {
		t.Fatalf("止まるはず: %v", err)
	}
	if !strings.Contains(stderr, "左上のタイトル: 1 行に収まらず 2 行になる (テスト用の台本  / <b>&)") {
		t.Errorf("問題の一覧が出ていない:\n%s", stderr)
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("止めたのに HTML を書いた")
	}
	if out, _, err := bld(t, true); err != nil {
		t.Fatalf("--allow-label-wraps なら書き出すはず: %v", err)
	} else if _, err := os.Stat(out); err != nil {
		t.Errorf("書き出していない: %v", err)
	}
}

// 本物の経路 (Chrome の --dump-dom の出力から計測の結果を取り出す) を、偽の Chrome で通す。
// 計測モードの断片 (#measure) と窓の大きさ (mp4 と同じ) で起こし、結果の問題が build まで届く
func TestMeasureLabelsWithChrome(t *testing.T) {
	log := fakeMP4Tools(t)
	t.Setenv("FAKE_MEASURE_BAD", "1")
	env := testEnv(t)
	env.MeasureLabels = nil
	var stderr strings.Builder
	env.Stderr = &stderr
	err := dispatch([]string{"build", buildFixtureScript(t, nil), "-o", filepath.Join(t.TempDir(), "out"), "--format", "html", "--allow-fast-captions"}, env)
	if err == nil || !strings.Contains(stderr.String(), "左上のタイトル: 1 行に収まらず 2 行になる") {
		t.Fatalf("偽の Chrome が返した問題で止まるはず: %v\n%s", err, stderr.String())
	}
	cs := chromeCalls(t, log)
	if len(cs) != 1 || !strings.Contains(cs[0], "--dump-dom") || !strings.HasSuffix(cs[0], "#measure") ||
		!strings.Contains(cs[0], "--window-size=64,36") {
		t.Errorf("Chrome の起こし方: %q", cs)
	}
}

// fakeMeasureDump は偽の Chrome の --dump-dom (計測モードが #measure-out に書く JSON を、HTML の文字としてエスケープして出す)
func fakeMeasureDump(page string) int {
	b, err := fakeLabelMeasure(func(_ *build.PlayerData, ms []build.LabelMeasure) []build.LabelMeasure {
		if os.Getenv("FAKE_MEASURE_BAD") != "" {
			ms[0].Lines, ms[0].Breaks, ms[0].Glen = []string{"テスト用の台本 ", "<b>&"}, []bool{true}, []int{8, 3}
		}
		return ms
	})(page)
	if err != nil {
		return 5
	}
	if _, err := os.Stdout.WriteString(`<html><body><main></main><pre id="measure-out">` + html.EscapeString(string(b)) + "</pre></body></html>\n"); err != nil {
		return 6
	}
	return 0
}

// 計測の Chrome が落ちた (負荷の watchdog など) ときは、台本の問題ではないので、確かめていないことを出して書き出しを続ける
func TestMeasureLabelsChromeFailureWarns(t *testing.T) {
	_ = fakeMP4Tools(t)
	t.Setenv("FAKE_CHROME_RC", "2")
	env := testEnv(t)
	env.MeasureLabels = nil
	var stderr strings.Builder
	env.Stderr = &stderr
	out := filepath.Join(t.TempDir(), "out")
	if err := dispatch([]string{"build", buildFixtureScript(t, nil), "-o", out, "--format", "html", "--allow-fast-captions"}, env); err != nil {
		t.Fatalf("Chrome が落ちても HTML は書くはず: %v", err)
	}
	if !strings.Contains(stderr.String(), "確かめずに続ける (rc=2") {
		t.Errorf("確かめていないことを出していない:\n%s", stderr.String())
	}
}
