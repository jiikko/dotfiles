package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"zundamon-kaisetsu/internal/build"
	"zundamon-kaisetsu/internal/script"
)

// view (issue 810) は、チャプターの見出し・行ごとの 行番号 / 話者 (表情) / 字幕と、図解を出す・段を進める・消す行の中身を出す。
// 合成 (wav) が無くても動く (台本を見せるのは synth の前のことがある)
func TestViewCommand(t *testing.T) {
	path := buildFixtureScript(t, func(raw map[string]any) {
		lines := raw["lines"].([]any)
		lines[1].(map[string]any)["chapter"] = "第2章"
		lines[1].(map[string]any)["show"] = map[string]any{"type": "list", "title": "理由", "items": []any{"項目A", "項目B"}, "build": true}
		lines[2].(map[string]any)["show"] = "next"
		lines[3].(map[string]any)["show"] = nil
	})
	must(t, os.RemoveAll(filepath.Join(filepath.Dir(path), "script.work")))
	env := testEnv(t)
	var out bytes.Buffer
	env.Stdout = &out
	must(t, dispatch([]string{"view", path}, env))
	got := out.String()
	s, err := script.LoadScript(path, env)
	must(t, err)
	for _, want := range []string{
		"0\t四国めたん (説明)\t" + s.Lines[0]["text"].(string) + "\n",
		"== 第2章 ==\n1\tずんだもん (驚き)\t",
		"2\t四国めたん (通常)\t", // face を省いた行は既定の表情
		"\t\t［図解］箇条書き「理由」: 項目A / 項目B  段 1/2: 項目A を強調\n",
		"\t\t［図解の段を進める］段 2/2: 項目B を強調\n",
		"\t\t［図解を消す］\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("出力に %q が無い:\n%s", want, got)
		}
	}
	// 図解の行は、それを書いた行の直後に出る (行の順を崩さない)
	if i, j := strings.Index(got, "\n1\t"), strings.Index(got, "［図解］"); i < 0 || j < i || strings.Index(got, "\n2\t") < j {
		t.Errorf("図解の行が lines[1] の直後に無い:\n%s", got)
	}
}

// mp4 を書き出したら、mp4 に入らないクレジットを HTML と同じ一覧 (Assemble の Credits) で標準出力に出す (issue 811)
func TestBuildMP4PrintsCredits(t *testing.T) {
	_ = fakeMP4Tools(t)
	env := testEnv(t)
	env.LoadAvg = func() (float64, bool) { return 0, true }
	env.Now, env.Sleep = time.Now, func(time.Duration) {}
	var stdout bytes.Buffer
	env.Stdout = &stdout
	scr := filepath.Join("testdata", "build", "script.json")
	must(t, build.CmdBuild(env, scr, filepath.Join(t.TempDir(), "out"), "mp4", 2, 64, true))
	s, err := script.LoadScript(scr, testEnv(t))
	must(t, err)
	data, _, err := build.Assemble(s, testEnv(t))
	must(t, err)
	want := strings.Join(data.Credits, "\n") + "\n"
	if len(data.Credits) < 2 || stdout.String() != want {
		t.Errorf("クレジット:\n%q\nwant\n%q", stdout.String(), want)
	}
}
