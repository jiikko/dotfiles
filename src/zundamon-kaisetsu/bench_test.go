package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"zundamon-kaisetsu/internal/build"
	"zundamon-kaisetsu/internal/paths"
	"zundamon-kaisetsu/internal/script"
	"zundamon-kaisetsu/internal/wav"
)

// 性能の確認用。実時間の律速は Chrome の撮影と x264 で、Go 側は build 全体の 1% 未満 (実測)。
// ここで測るのは Go 側のメモリと CPU の傾向で、合否には使わない。
//   go test -run '^$' -bench . -benchmem

// benchScript は n 行 (各 secs 秒) の台本と、その合成のキャッシュ (wav / query.json) を dir に作る
func benchScript(b *testing.B, n int, secs float64) *script.Script {
	b.Helper()
	dir := b.TempDir()
	path := filepath.Join(dir, "s.json")
	lines := make([]map[string]any, n)
	for i := range lines {
		who := []string{"metan", "zundamon"}[i%2]
		lines[i] = map[string]any{"who": who, "text": fmt.Sprintf("これは %d 行目のセリフ", i)}
		if i%20 == 0 {
			lines[i]["chapter"] = fmt.Sprintf("章 %d", i/20)
		}
	}
	writeScriptFile(b, path, map[string]any{"title": "bench", "lines": lines})
	env := testEnv(b)
	s, err := script.LoadScript(paths.ResolvePath(path), env)
	if err != nil {
		b.Fatal(err)
	}
	wd := paths.WorkDir(s.Path)
	if err := os.MkdirAll(wd, 0o755); err != nil {
		b.Fatal(err)
	}
	pcm := make([]byte, 2*int(secs*wav.SampleRate))
	query := `{"accent_phrases":[{"moras":[{"vowel":"a","vowel_length":0.1},{"vowel":"i","vowel_length":0.1}]}],"speedScale":1.0}`
	for _, line := range s.Lines {
		p, err := script.LineParams(s, line)
		if err != nil {
			b.Fatal(err)
		}
		w, q := script.CachePaths(wd, p)
		if err := wav.Write(w, pcm); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(q, []byte(query), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	return s
}

// 300 行 × 4 秒 = 20 分の台本の組み立て (wav の読み込みと連結・口の開き・プレイヤーのデータ)
func BenchmarkAssemble(b *testing.B) {
	s := benchScript(b, 300, 4)
	env := testEnv(b)
	b.ResetTimer()
	for b.Loop() {
		if _, _, err := build.Assemble(s, env); err != nil {
			b.Fatal(err)
		}
	}
}

// 連結した 20 分の wav の書き出し
func BenchmarkWriteWav(b *testing.B) {
	pcm := make([]byte, 2*20*60*wav.SampleRate)
	p := filepath.Join(b.TempDir(), "x.wav")
	for b.Loop() {
		if err := wav.Write(p, pcm); err != nil {
			b.Fatal(err)
		}
	}
}

// 見た目の状態 (1200 種類程度) の並べ替え
func BenchmarkSortedStates(b *testing.B) {
	var states []build.VisualState
	for li := range 300 {
		for sp := range 2 {
			for lv := range build.MouthLevels {
				states = append(states, build.VisualState{li, sp, lv, 0})
			}
		}
	}
	for b.Loop() {
		_ = build.SortedStates(states)
	}
}

// 読み替えの辞書がある台本の 300 行分の読み上げ文
func BenchmarkSpokenText(b *testing.B) {
	rd := map[string]any{}
	for i := range 50 {
		rd[fmt.Sprintf("WORD%d", i)] = fmt.Sprintf("ワード%d", i)
	}
	s := &script.Script{Raw: map[string]any{"readings": rd}}
	line := map[string]any{"text": "WORD1 と WORD20 を使う説明のセリフ"}
	for b.Loop() {
		for range 300 {
			_ = script.SpokenText(s, line)
		}
	}
}
