package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// waitDelayLookahead は CommandContext の直後に WaitDelay の代入を探す行数。
//
// 🚨 12 なのは gitlog.go の `return runGitCmd(exec.CommandContext(...))` が、代入を **呼び先の
// runGitCmd** (+10 行) で 1 度だけ張る形だから。つまりこの窓は**関数境界を跨ぐ**ので、
// 「同じ Cmd に張っている」ことまでは保証しない (issue 303 の既知の限界)。
const waitDelayLookahead = 12

// waitDelaySeenFloor / waitDelayEnforcedFloor は canary の下限。実件数のすぐ下に置く
// (下限 0 や 1 だと「走査が壊れて対象を取りこぼした」が緑で通る)。
//
// seen = 見つけた exec.CommandContext の数、enforced = そのうち `subproc: no-waitdelay` で
// 免除されなかった数。**2 つを分けて数える**のは、注記を足すと enforced だけが減り、
// seen しか見ていない canary では「守りが薄くなった」が観測できないため。
// 実測 2026-09-09: seen=2 (gitlog.go / external_commands.go)、enforced=1。
// 正当に減らすときは、この定数も同じ commit で意識的に下げる。
const (
	waitDelaySeenFloor     = 2
	waitDelayEnforcedFloor = 1
)

// stripLineComment は行末コメントを落とす。WaitDelay の**代入**を探す窓の中だけで使う。
//
// 🚨 注記 (`subproc: no-waitdelay`) の判定には使わないこと。注記は行末コメントに書く運用なので、
// 剥がすと注記済みの行が offender に化ける。
// 文字列リテラル中の `//` (URL 等) も切るが、切りすぎは「代入が見つからない = 落ちる」側へ倒れる
// (静かに通る側には倒れない) ので許容する。
func stripLineComment(line string) string {
	if i := strings.Index(line, "//"); i >= 0 {
		return line[:i]
	}
	return line
}

// hasWaitDelayAssign は「コードとして WaitDelay を代入しているか」を判定する。
//
// 🚨 語の出現 (`strings.Contains(line, "WaitDelay")`) では駄目。実測 2026-09-09 (issue 303):
// gitlog.go の唯一の強制対象は、+8 行目のコメント `(理由は subproc.WaitDelay の doc)` に
// 引っかかって緑になっていた。**実ガード `cmd.WaitDelay = subproc.WaitDelay` を削除しても緑、
// そのコメントから語を消すと赤**という反転状態で、production の退行検出力は 0 だった。
func hasWaitDelayAssign(line string) bool {
	code := stripLineComment(line)
	return strings.Contains(code, "WaitDelay =") || strings.Contains(code, "WaitDelay:")
}

// waitDelayOffenders は 1 ファイル分の行から規律違反を返す。
//
// 走査本体と canary の**両方がこの関数を通る**。式をコピーして canary を別に書くと、
// canary は「コピーした式」を検査するだけで本走査の破損を検出しない
// (`verify-execution-not-just-exit-code.md`)。
func waitDelayOffenders(path string, lines []string) (offenders []string, seen, enforced int) {
	for i, line := range lines {
		if !strings.Contains(line, "exec.CommandContext(") {
			continue
		}
		seen++
		// 注記は生の行で見る (コメントを剥がすと注記そのものが消える)
		if strings.Contains(line, "subproc: no-waitdelay") {
			continue
		}
		enforced++
		found := false
		for j := i; j < len(lines) && j <= i+waitDelayLookahead; j++ {
			if hasWaitDelayAssign(lines[j]) {
				found = true
				break
			}
		}
		if !found {
			offenders = append(offenders, path+":"+strconv.Itoa(i+1)+": "+strings.TrimSpace(line))
		}
	}
	return offenders, seen, enforced
}

// TestEveryCommandContextSetsWaitDelay は「exec.CommandContext を呼んだら WaitDelay を張る」
// 規律をソース走査で強制する。
//
// なぜテストで縛るか (issue 105): この規律は 13 箇所中 12 箇所で守られ、1 箇所
// (issues/discover.go の RepoRoot) だけが静かに抜けていた。抜けても手元でも CI でも何も起きず、
// 実際にハングして初めて分かる種類の欠落なので、**書く人が覚えているか**に依存させない。
//
// 例外は行内に `subproc: no-waitdelay` と理由を書く。出力を取らない Run() は stdout/stderr が
// /dev/null 直結でパイプを作らないため、ctx の kill だけで足りる (WaitDelay が要るのは
// Output / CombinedOutput / Stdout に io.Writer を張る形)。
//
// 🚨 このゲートが見ているのは `exec.CommandContext(` の 1 パターンだけで、ctx 無しの
// `exec.Command(` (非テスト 7 箇所) と、別名 import (`xe "os/exec"`) は素通しする。
// 射程を広げるかどうかは issue 303 で軸を選定中。
func TestEveryCommandContextSetsWaitDelay(t *testing.T) {
	var offenders []string
	seen, enforced := 0, 0
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// subproc 自身は規律の実装元。tools/ は本体から参照しない調査ツール。
			if d.Name() == "subproc" || d.Name() == "tools" || d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		o, s, e := waitDelayOffenders(filepath.ToSlash(path), strings.Split(string(src), "\n"))
		offenders = append(offenders, o...)
		seen += s
		enforced += e
		return nil
	})
	if err != nil {
		t.Fatalf("ソース走査に失敗: %v", err)
	}
	if seen < waitDelaySeenFloor {
		t.Fatalf("exec.CommandContext の検出数が %d 件しかない (下限 %d)。"+
			"走査パスか検出パターンが壊れている疑い", seen, waitDelaySeenFloor)
	}
	if enforced < waitDelayEnforcedFloor {
		t.Fatalf("WaitDelay を強制している箇所が %d 件しかない (下限 %d)。"+
			"`subproc: no-waitdelay` の注記が増えて守りが消えていないか確認する",
			enforced, waitDelayEnforcedFloor)
	}
	if len(offenders) > 0 {
		t.Fatalf("WaitDelay を張っていない exec.CommandContext がある (subproc.CommandContext を使うか、"+
			"理由つきで `subproc: no-waitdelay` を行内に書く):\n  %s", strings.Join(offenders, "\n  "))
	}
}

// TestWaitDelayOffendersCanary は、既知の入力に対して既知の答えが出ることを本走査の前に固定する。
// 本走査と**同じ** waitDelayOffenders を通す。
//
// 目的は「production に何件あるか」に依存せずに検出力を固定すること。実件数は 2 件しかなく、
// そのうち強制対象は 1 件なので、production だけを根拠にすると検出力が実質検査できない。
func TestWaitDelayOffendersCanary(t *testing.T) {
	cases := []struct {
		name         string
		lines        []string
		wantOffender bool
		wantSeen     int
		wantEnforced int
	}{
		{
			name:         "代入が無い",
			lines:        []string{`c := exec.CommandContext(ctx, "gh")`, `return c.Output()`},
			wantOffender: true, wantSeen: 1, wantEnforced: 1,
		},
		{
			name:         "直後に代入がある",
			lines:        []string{`c := exec.CommandContext(ctx, "gh")`, `c.WaitDelay = subproc.WaitDelay`},
			wantOffender: false, wantSeen: 1, wantEnforced: 1,
		},
		{
			// issue 303 の本体。語がコメントに出るだけでは通してはいけない
			name: "コメントで WaitDelay に言及しているだけ",
			lines: []string{
				`c := exec.CommandContext(ctx, "gh")`,
				`// 理由は subproc.WaitDelay の doc を見ること`,
				`return c.Output()`,
			},
			wantOffender: true, wantSeen: 1, wantEnforced: 1,
		},
		{
			name:         "注記で免除されている",
			lines:        []string{`return exec.CommandContext(ctx, "x").Run() // subproc: no-waitdelay`},
			wantOffender: false, wantSeen: 1, wantEnforced: 0,
		},
		{
			name: "代入が lookahead の外にある",
			lines: append(append(
				[]string{`c := exec.CommandContext(ctx, "gh")`},
				make([]string, waitDelayLookahead+1)...),
				`c.WaitDelay = subproc.WaitDelay`),
			wantOffender: true, wantSeen: 1, wantEnforced: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			offenders, seen, enforced := waitDelayOffenders("fixture.go", tc.lines)
			if got := len(offenders) > 0; got != tc.wantOffender {
				t.Fatalf("offender 判定 = %v, want %v (offenders=%v)", got, tc.wantOffender, offenders)
			}
			if seen != tc.wantSeen {
				t.Fatalf("seen = %d, want %d", seen, tc.wantSeen)
			}
			if enforced != tc.wantEnforced {
				t.Fatalf("enforced = %d, want %d", enforced, tc.wantEnforced)
			}
		})
	}
}
