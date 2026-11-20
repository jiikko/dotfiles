//go:build !race

// 子 (lint・diff) は race を付けて作ったこのテストのバイナリ自身なので、race の run では時間が大きく延びる。run の記録と同じく race の run から外し、
// Makefile の test が race なしで別に回す。

package check

import (
	"encoding/json"
	"os"
	"testing"

	"ja-writing/oracle"
)

func loadReplay(t *testing.T) (oracle.VisibilityGolden, []oracle.VisCase) {
	t.Helper()
	var g oracle.VisibilityGolden
	if err := oracle.ReadGolden(goldenDir, oracle.CheckReplayFile, &g); err != nil {
		t.Fatalf("make golden で記録を作る: %v", err)
	}
	cases := oracle.CheckReplayCases()
	if len(cases) != len(g.Cases) {
		t.Fatalf("case %d 件に対して記録は %d 件。make golden で作り直す", len(cases), len(g.Cases))
	}
	for i, c := range cases {
		rec := g.Cases[i]
		if rec.Name != c.Name || rec.Calls != len(c.Calls) || len(rec.Digests) != rec.Calls*oracle.DigestWidth {
			t.Fatalf("case %d が記録と違う (%s %d 件 / 記録 %s %d 件)。make golden で作り直す", i, c.Name, len(c.Calls), rec.Name, rec.Calls)
		}
	}
	return g, cases
}

// 本家の run が書いた状態フォルダを戻し、check の verify --replay (と --since・--timeout) が本家と同じ stdout・stderr・終了コードを返す。
// 作り直しの子は、run の記録と同じく子の代わり (op pyexe) かテストのバイナリ (lint・diff の subcommand) にする。
func TestReplayMatchesPythonRecording(t *testing.T) {
	withUmask(t, 0o022)
	for _, name := range []string{stateDirEnv, "YOMIYASU_SESSION", "CODEX_THREAD_ID", "JA_FAKE_LINT", "JA_FAKE_DIFF", "JA_DIFF_JSON", "JA_LINT_JSON"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	g, cases := loadReplay(t)
	sim := newRunSim(t)
	sim.restore = true
	// 一時フォルダを空のフォルダにする (ほかのテストが同じ TMPDIR に書いても、verify が書いたものと見分けられる)
	sim.temporary = t.TempDir()
	t.Setenv("TMPDIR", sim.temporary)
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
	t.Logf("verify --replay の記録 %d case・呼び出し %d 件を全部比べた", len(cases), total)
}

// 空振りを止める: 作り直しの結果 (ok・mismatch・timeout) と終了コード 0・1・2・3 のどれもが記録に出る。
func TestReplayRecordingCoversOutcomes(t *testing.T) {
	g, _ := loadReplay(t)
	codes := map[int]int{}
	replays := map[string]int{}
	for _, c := range g.Cases {
		for _, k := range c.Kept {
			var r struct {
				RC     *int   `json:"rc"`
				Stdout string `json:"stdout"`
			}
			if json.Unmarshal(k.Result, &r) != nil || r.RC == nil {
				continue
			}
			codes[*r.RC]++
			var out struct {
				Replay string `json:"replay"`
			}
			if json.Unmarshal([]byte(r.Stdout), &out) == nil && out.Replay != "" {
				replays[out.Replay]++
			}
		}
	}
	for _, rc := range []int{0, 1, 2, 3} {
		if codes[rc] == 0 {
			t.Errorf("終了コード %d の verify が記録に無い (%v)", rc, codes)
		}
	}
	for _, value := range []string{"ok", "mismatch", "timeout", "skipped"} {
		if replays[value] == 0 {
			t.Errorf("replay が %s の --json の出力が記録に無い (%v)", value, replays)
		}
	}
}
