package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 字幕が出ている間 (次の行の開始まで。最後の行は動画の終わりまで) に読む字数で判定する。空白は数えず、ちょうど上限は通す
func TestFastCaptions(t *testing.T) {
	lines := []timelineLine{
		{Text: strings.Repeat("あ", 15), Start: 0},              // 0〜2 秒 → 7.5 字/秒 (上限ちょうどは通す)
		{Text: strings.Repeat("い", 16), Start: 2},              // 2〜4 秒 → 8 字/秒
		{Text: "う　え　お く " + strings.Repeat("か", 10), Start: 4}, // 半角・全角の空白を除いて 14 字 / 2 秒 → 7 字/秒で通す (全角の空白 2 つを数えると 8 字/秒)
		{Text: strings.Repeat("き", 8), Start: 6},               // 最後の行は動画の終わり (7 秒) まで → 8 字/秒
	}
	got := fastCaptions(lines, 7)
	var idx []int
	for _, f := range got {
		idx = append(idx, f.Line)
	}
	if want := []int{1, 3}; !equalInts(idx, want) {
		t.Errorf("速い行: got %v want %v", idx, want)
	}
	// 次の行までの間 (gap・pause_after) も読める時間に数える: 同じ 16 字でも次の行が 3 秒後なら通る
	// (話している長さ (End - Start = 1 秒) で測ると 16 字/秒で速いと出るが、字幕は次の行まで 3 秒出ているので通す)
	slow := []timelineLine{{Text: strings.Repeat("い", 16), Start: 0, End: 1}, {Text: "え", Start: 3, End: 3.5}}
	if f := fastCaptions(slow, 10); len(f) != 0 {
		t.Errorf("間を足した行も速いと判定した: %+v", f)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// build は字幕の速い行を行番号つきで警告し、書き出しは止めない。速い行が無ければ何も言わない
func TestBuildWarnsFastCaptions(t *testing.T) {
	build := func(t *testing.T, edit func(lines []any)) string {
		t.Helper()
		dir := t.TempDir()
		copyTree(t, filepath.Join("testdata", "build", "script.work"), filepath.Join(dir, "script.work"))
		copyTree(t, filepath.Join("testdata", "build", "faces"), filepath.Join(dir, "faces"))
		b, err := os.ReadFile(filepath.Join("testdata", "build", "script.json"))
		must(t, err)
		var raw map[string]any
		must(t, decodeJSON(b, &raw))
		path := filepath.Join(dir, "script.json")
		writeScriptFile(t, path, raw)
		{ // testdata の音声は 1 行 1 秒未満の短いものなので、字幕は全行を 1 字にして「速い行が無い」状態から始める
			env := testEnv(t)
			s, err := loadScript(resolvePath(path), env)
			must(t, err)
			lines := raw["lines"].([]any)
			for i, l := range lines { // 音声を据え置くため、今の読み (合成の鍵) を read に固定してから字幕を変える
				l.(map[string]any)["read"] = spokenText(s, s.Lines[i])
				l.(map[string]any)["text"] = "あ"
			}
			if edit != nil {
				edit(lines)
			}
			writeScriptFile(t, path, raw)
		}
		fakeMP4Tools(t)
		env := testEnv(t)
		var stderr bytes.Buffer
		env.Stderr = &stderr
		env.Now = time.Now
		out := filepath.Join(t.TempDir(), "out")
		must(t, cmdBuild(env, path, out, "html", 1, 64))
		if _, err := os.Stat(out + ".html"); err != nil {
			t.Fatalf("警告があっても HTML は書き出すはず: %v", err)
		}
		return stderr.String()
	}
	if got := build(t, nil); strings.Contains(got, "字幕が速くて") {
		t.Fatalf("前提: 字幕を 1 字にした台本には速い行が無いはず:\n%s", got)
	}
	// 速い行を最後の行に置く (最後の行の区間は動画の終わりまでなので、build が動画の長さを渡していないと見逃す)
	got := build(t, func(lines []any) {
		lines[len(lines)-1].(map[string]any)["text"] = strings.Repeat("字幕だけ長い", 10) // 60 字を約 1 秒で
	})
	if !strings.Contains(got, "字幕が速くて読み切れないおそれのある行が 1 行") || !strings.Contains(got, "lines[4] ") {
		t.Errorf("速い行の警告が無い:\n%s", got)
	}
	if !strings.Contains(got, "「字幕だけ長い字幕だけ長い字幕だけ長い字幕だけ長い…」") {
		t.Errorf("警告の行の文の先頭 (24 字で切る) が違う:\n%s", got)
	}
}

// 警告の文: 件数・行ごとの字/秒・長い文の切り詰め
func TestWarnFastCaptionsText(t *testing.T) {
	var buf bytes.Buffer
	warnFastCaptions(&buf, []timelineLine{
		{Text: strings.Repeat("い", 16), Start: 0}, // 8.0 字/秒
		{Text: "あ", Start: 2},                     // 0.5 字/秒
		{Text: strings.Repeat("う", 30), Start: 4}, // 10.0 字/秒 (24 字で切る)
	}, 7)
	want := "build: 字幕が速くて読み切れないおそれのある行が 2 行 (1 秒 7.5 字を超える。行を分けるか、その行の pause_after で間を足す):\n" +
		"  lines[0] 8.0 字/秒 「" + strings.Repeat("い", 16) + "」\n" +
		"  lines[2] 10.0 字/秒 「" + strings.Repeat("う", 24) + "…」\n"
	if got := buf.String(); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	buf.Reset()
	warnFastCaptions(&buf, []timelineLine{{Text: "あ", Start: 0}}, 1)
	if buf.Len() != 0 {
		t.Errorf("速い行が無いのに書いた: %q", buf.String())
	}
}
