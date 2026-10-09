package filer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// frameAllocBudget は止まった画面 1 フレーム (View) の確保の回数の上限 (issue 693)。
// 退行は今まで glogx の TestFrameAllocBudget (filer の行) でしか赤くならず、treefiler だけの変更の CI では見えなかった
// (56c93c8f: src/treefiler の CI が success、src/glogx が 164 / 158 で failure)。
// 上限は実測 (go1.25・-race なし) に数回の余裕を足した値。描く文字列そのものを増やす変更 (案内・凡例) で上げるときは、
// 増えた分が描く文字列の増加だと A/B で確かめてから上げる。
const frameAllocBudget = 335

func TestFrameAllocBudget(t *testing.T) {
	t.Setenv("TREEFILER_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	for i := range 20 {
		mustWrite(t, filepath.Join(dir, fmt.Sprintf("file%02d.go", i)), "x\n")
	}
	for _, d := range []string{"alpha", "beta", "gamma"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
		for i := range 10 {
			mustWrite(t, filepath.Join(dir, d, fmt.Sprintf("%s%02d.md", d[:1], i)), "y\n")
		}
	}
	m := newAt(t, dir)
	m.Resize(120, 70)
	m.HandleKey("l") // alpha を開いて中へ
	// 残りのフォルダも開く (開いたフォルダの数だけ効く退行 = 芽の判定でスライスを作る形を、数回の余裕で見逃さないため)
	for _, d := range []string{"beta", "gamma"} {
		m.loadPath(filepath.Join(dir, d)).expanded = true
	}
	settleBackground(t, m)
	m.snapAll()
	// 下界: 実際に描いたか (何も描かないフレームは確保が最少で、上界だけだと緑になる)
	if v := strings.Join(m.draw().plain(), "\n"); !strings.Contains(v, "alpha/") || !strings.Contains(v, "a00.md") || !strings.Contains(v, "b00.md") || !strings.Contains(v, "g00.md") {
		t.Fatalf("前提: 3 つのフォルダを開いた木を描いていない:\n%s", v)
	}
	if raceEnabled {
		t.Log("確保の回数は -race なしの run で見る (Makefile の test)")
		return
	}
	got := testing.AllocsPerRun(50, func() { _ = m.View() })
	t.Logf("1 フレームの確保 %v 回 (上限 %d)", got, frameAllocBudget)
	if int(got) > frameAllocBudget {
		t.Errorf("1 フレームの確保が %v 回 (上限 %d)。毎フレームの作り直しが増えていないか", got, frameAllocBudget)
	}
}
