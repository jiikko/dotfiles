//go:build !race

// 記録との突き合わせの重い検査。子 (lint・diff) は race を付けて作ったこのテストのバイナリ自身なので、race の run では時間が 30 倍近くになる
// (手元で 3.5 秒 → 102 秒)。race の run からは外し、Makefile の test が race なしで別に回す。子を並べて待つ所 (children) の
// データ競争は run_signal_test.go の時間切れとシグナルのテストが race の run で見る。

package check

import (
	"os"
	"testing"
)

// 本家の run と同じ入力で check の run を動かし、標準出力・標準エラー・終了コードと、書いた状態フォルダの木が本家と同じになる。
// 木の python.version と python.implementation だけは Go の値 (本家の verify は型だけを見る)。逆向きに、その木 (python の欄を Go の値にした本家の木) に
// 本家の verify を当てた出力と、Go の verify の出力も比べる。
func TestRunMatchesPythonRecording(t *testing.T) {
	withUmask(t, 0o022)
	for _, name := range []string{stateDirEnv, "YOMIYASU_SESSION", "CODEX_THREAD_ID", "JA_FAKE_LINT", "JA_FAKE_DIFF", "JA_TOUCH"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	g, cases := loadRun(t)
	sim := newRunSim(t)
	stats := map[string]*stat{}
	failed, total := 0, 0
	for i, c := range cases {
		for _, m := range runCaseWith(t, c, g.Cases[i], stats, sim) {
			failed++
			if failed <= 15 {
				t.Error(m)
			}
		}
		total += len(c.Calls)
	}
	if failed > 15 {
		t.Errorf("ほかにも %d 件の食い違いがあります", failed-15)
	}
	for _, f := range g.Functions {
		s := stats[f.Name]
		if s == nil || s.calls != f.Calls || s.raised != f.Raised {
			t.Errorf("%s の呼び出し数・例外の数が記録と違う: Go %+v、記録 %+v", f.Name, s, f)
		}
	}
	t.Logf("run の記録 %d case・呼び出し %d 件を全部比べた", len(cases), total)
}
