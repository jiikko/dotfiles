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

// ラベルの改行の判定 (issue 812): 1 行で出す欄の折り返し・語の途中の改行・1 字だけの行・カードのはみ出し・知らない種類・
// 測れなかったラベルを問題にし、語の境目での折り返しは通す
func TestLabelProblems(t *testing.T) {
	d := &build.PlayerData{
		Title: "タイトル", Overlays: []string{"title", "topic"},
		Chapters: []build.ChapterData{{Title: "はじめに"}, {Title: "まとめ"}},
		Shows:    []show.Data{{Type: "keyword", Text: "図はフォルダ構成ではない"}, {Type: "image"}},
		Lines:    []build.TimelineLine{{}, {Show: new(int)}},
	}
	ok := []build.LabelMeasure{
		{Key: "title", Kind: "line", Lines: []string{"タイトル"}},
		{Key: "topic:0", Kind: "line", Lines: []string{"はじめに"}},
		{Key: "topic:1", Kind: "line", Lines: []string{"まとめ"}},
		{Key: "show:0", Kind: "box", Clip: []int{0, 0}},
		{Key: "show:0:text", Kind: "wrap", Lines: []string{"モックだらけの", "テスト"}, Breaks: []bool{true}},
	}
	if p := build.LabelProblems(d, ok); len(p) != 0 {
		t.Fatalf("問題の無い計測で問題が出た: %q", p)
	}
	for name, c := range map[string]struct {
		edit func(ms []build.LabelMeasure) []build.LabelMeasure
		want string
	}{
		"タイトルが 2 行": {func(ms []build.LabelMeasure) []build.LabelMeasure {
			ms[0].Lines = []string{"そのRepository、本当に要る？", "なぜ"}
			return ms
		}, "左上のタイトル: 1 行に収まらず 2 行になる"},
		"語の途中で改行": {func(ms []build.LabelMeasure) []build.LabelMeasure {
			ms[4].Lines, ms[4].Breaks = []string{"図は", "フォルダ構成ではな", "いい"}, []bool{true, false}
			return ms
		}, "図解 0 (keyword。lines[1] から) の text: 語の途中で改行する (図は / フォルダ構成ではな / いい)"},
		"1 字だけの行": {func(ms []build.LabelMeasure) []build.LabelMeasure {
			ms[4].Lines, ms[4].Breaks = []string{"図はフォルダ構成", "で"}, []bool{true}
			return ms
		}, "1 字だけの行ができる"},
		"はみ出し": {func(ms []build.LabelMeasure) []build.LabelMeasure {
			ms[3].Clip = []int{0, 12}
			return ms
		}, "中身がカードからはみ出して隠れる (横 0 px / 縦 12 px)"},
		"知らない種類": {func(ms []build.LabelMeasure) []build.LabelMeasure {
			ms[1].Kind = "lines"
			return ms
		}, "計測モードが知らない種類"},
		"測れなかった": {func(ms []build.LabelMeasure) []build.LabelMeasure {
			return slices.DeleteFunc(ms, func(m build.LabelMeasure) bool { return m.Key == "topic:1" })
		}, "トピック名「まとめ」: 測れなかった"},
	} {
		t.Run(name, func(t *testing.T) {
			p := build.LabelProblems(d, c.edit(slices.Clone(ok)))
			if len(p) != 1 || !strings.Contains(p[0], c.want) {
				t.Errorf("問題 %q に %q が 1 つだけ出るはず", p, c.want)
			}
		})
	}
	// 図は測らない (文字のラベルが無い)。区切りのカードは cards があるときだけ測る
	if keys := build.ExpectedLabelKeys(d); !slices.Equal(keys, []string{"title", "topic:0", "topic:1", "show:0"}) {
		t.Errorf("測るはずの鍵: %q", keys)
	}
	d.Cards, d.Overlays, d.Title = [][2]int{{0, 1}}, []string{"date"}, "" // タイトルとトピック名を出さない台本
	if keys := build.ExpectedLabelKeys(d); !slices.Equal(keys, []string{"card:0", "card:1", "show:0"}) {
		t.Errorf("overlays から外した欄も測ろうとしている: %q", keys)
	}
}

// build はラベルの改行の問題を一覧にして、音声の圧縮と書き出しの前に止まる (--allow-label-wraps なら続ける)。dispatch を通す
func TestBuildStopsOnLabelWrap(t *testing.T) {
	path := buildFixtureScript(t, nil)
	bld := func(t *testing.T, allow bool) (string, string, error) {
		env := testEnv(t)
		env.MeasureLabels = fakeLabelMeasure(func(_ *build.PlayerData, ms []build.LabelMeasure) []build.LabelMeasure {
			ms[0] = build.LabelMeasure{Key: "title", Kind: "line", Lines: []string{"テスト用の台本", "<b>&"}}
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
	if !strings.Contains(stderr, "左上のタイトル: 1 行に収まらず 2 行になる (テスト用の台本 / <b>&)") {
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
			ms[0] = build.LabelMeasure{Key: "title", Kind: "line", Lines: []string{"テスト用の台本", "<b>&"}}
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
