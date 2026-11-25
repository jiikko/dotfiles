//go:build !race

// 記録との突き合わせの重い検査 (run を含むので、子は race で作ったテストのバイナリ自身になる。理由は run_recording_norace_test.go と同じ)。

package check

import (
	"os"
	"testing"

	"ja-writing/oracle"
)

// 本家の hook の main と同じ入力 (引数・標準入力のイベントの JSON・環境変数・状態フォルダ) で HookMainWith を動かし、標準出力・標準エラー・
// 終了コードと、書いた状態フォルダの木 (登録・log・hook-errors.log) が本家と同じになる。hook の中の関数と shlex は直接呼んだ戻り値と比べる。
func TestHookMatchesPythonRecording(t *testing.T) {
	withUmask(t, 0o022)
	for _, name := range []string{stateDirEnv, "YOMIYASU_SESSION", "CODEX_THREAD_ID", "CLAUDE_PROJECT_DIR", "JA_FAKE_LINT", "JA_FAKE_DIFF", "JA_TOUCH"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	var g oracle.VisibilityGolden
	if err := oracle.ReadGolden(goldenDir, oracle.CheckHookFile, &g); err != nil {
		t.Fatalf("make golden で記録を作る: %v", err)
	}
	cases := oracle.CheckHookCases()
	if len(cases) != len(g.Cases) {
		t.Fatalf("case %d 件に対して記録は %d 件。make golden で作り直す", len(cases), len(g.Cases))
	}
	sim := newRunSim(t)
	stats := map[string]*stat{}
	failed, total := 0, 0
	for i, c := range cases {
		rec := g.Cases[i]
		if rec.Name != c.Name || rec.Calls != len(c.Calls) || len(rec.Digests) != rec.Calls*oracle.DigestWidth {
			t.Fatalf("case %d が記録と違う (%s %d 件 / 記録 %s %d 件)。make golden で作り直す", i, c.Name, len(c.Calls), rec.Name, rec.Calls)
		}
		for _, m := range runCaseWith(t, c, rec, stats, sim) {
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
	for _, name := range oracle.CheckHookFunctions {
		if stats[name] == nil || stats[name].calls == 0 {
			t.Errorf("%s の呼び出しが記録に無い", name)
		}
	}
	t.Logf("hook の記録 %d case・呼び出し %d 件を全部比べた", len(cases), total)
}
