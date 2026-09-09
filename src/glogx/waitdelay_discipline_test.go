package main

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// ============================================================================
// 脅威モデル (このゲートを書く前に決めたこと。issue 303 / adversarial-review-own-safeguards §8)
// ============================================================================
//
// ■ 誰の・どの失敗を止めるか
//
//	src/glogx に**新しく外部コマンド実行を書いた人が、WaitDelay を張り忘れる**こと。
//	これは issue 105 の形 (13 箇所中 1 箇所だけ静かに抜け、手元でも CI でも何も起きず、
//	実際にハングして初めて分かった) で、書く人の記憶に依存させたくない種類の欠落。
//	止めたいのは**うっかり**であって、意図的な迂回ではない。
//
// ■ 検出しないと決めた形 (= 見つけても「ゲートの穴」として塞ぎに行かない)
//
//	(a) 字句を避ける書き方すべて: `exec.Cmd{Path: …}` の構造体リテラル / `syscall.Exec` /
//	    別パッケージに薄いラッパを作ってそこから呼ぶ / 呼び出しを複数行に割って
//	    `exec.Command(` が 1 行に現れない形 / reflect 経由。
//	(b) 注記 (`subproc: no-waitdelay` / `subproc: waitdelay-in`) に**書かれた理由が真か**。
//	    ゲートは注記の有無しか見ない。嘘の理由は書ける。
//	(c) 代入が**同じ Cmd** に対して行われているか。判定は行単位の窓 (waitDelayLookahead) で、
//	    窓は関数境界を跨ぐ。「近くに WaitDelay の代入がある」までしか言っていない。
//	(d) **新しい実行箇所を足すと同時に注記も足す**形。offenders は 0 のまま seen だけ増えて緑。
//	    免除機構を持つ以上これは原理的に閉じられない。
//	(e) glogx 以外の module (doctor 等)。走査は src/glogx の下だけを歩く。
//	    → doctor 側の事情は src/doctor/runner/runner.go の doc に書いてある。
//
// ■ 上記を誰が見るか
//
//	review。(a)〜(e) はいずれも「書いた本人が意図してやらないと成立しない」か
//	「読めば分かる」形なので、レビュワーの責務に置く。ゲートを迂回する指摘が出ても、
//	この節に該当するなら**塞がずに記録する** (塞ぎ続けると規則の軸が字句に固定され、
//	迂回の指摘が無限に出て収束しない。§8)。
//
// ■ 2 段構え (段ごとに別のテストで、別の退行を止める)
//
//	段 A = TestEveryCommandContextSetsWaitDelay: 字句走査。
//	       「既に os/exec を使っているファイルへの追記」(最頻の退行) を止める。
//	段 B = TestOsExecImportBoundary: import 境界。
//	       「新しいファイルが Cmd を作り始める」「別名 import で段 A の字句を避ける」を止める。
//	       段 B は段 A の**置き換えではない**: 段 B は WaitDelay を一切読まないので、
//	       既存箇所の守りが外れる退行 (issue 105 の形) には無力。
//
// ============================================================================

// waitDelayLookahead は実行箇所の直後に WaitDelay の代入を探す行数。
//
// 🚨 12 なのは gitlog.go の `return runGitCmd(exec.CommandContext(...))` が、代入を **呼び先の
// runGitCmd** (+9 行) で 1 度だけ張る形だから。つまりこの窓は**関数境界を跨ぐ**ので、
// 「同じ Cmd に張っている」ことまでは保証しない (脅威モデル (c))。
const waitDelayLookahead = 12

// execCmdPatterns は「Cmd を作っている」とみなす字句。
//
// 🚨 2 本必要。`exec.Command(` は `exec.CommandContext(` を**含まない** (前者はここで `(` を
// 要求し、後者はその位置が `C`)。実測 2026-09-09。片方だけにすると、もう片方の検査が
// 丸ごと消える (issue 303 の A 案の本文がこの誤りを含んでいた)。
var execCmdPatterns = []string{"exec.CommandContext(", "exec.Command("}

// 免除の注記。**2 種類あるのは意味が違うから**。片方で代用すると行内注記が嘘になる。
//
//	markerNoWaitDelay … WaitDelay が要らない。理由を同じ行か直上に書く
//	markerWaitDelayIn … Cmd を受け取った呼び先が張る (gitlog.go:68 → runGitCmd)
const (
	markerNoWaitDelay = "subproc: no-waitdelay"
	markerWaitDelayIn = "subproc: waitdelay-in"
)

// waitDelaySeenFloor / waitDelayEnforcedFloor は canary の下限。実件数のすぐ下に置く
// (下限 0 や 1 だと「走査が壊れて対象を取りこぼした」が緑で通る)。
//
// seen = 見つけた実行箇所の数、enforced = そのうち注記で免除されなかった数。
// **2 つを分けて数える**のは、注記を足すと enforced だけが減り、seen しか見ていない canary
// では「守りが薄くなった」が観測できないため (脅威モデル (d) の一部はここで拾える)。
//
// 実測 2026-09-09: seen=9、enforced=2 (tui.go の job ログ nvim / gitlog.go の runGitTimeout)。
// 🚨 enforced の 2 件は**注記を付けてはいけない**。ここを注記で逃がすと、
// 「WaitDelay の代入を消す」退行 (issue 105 の形) を検出する箇所が 0 になる。
// 正当に減らすときは、この定数も同じ commit で意識的に下げる。
const (
	waitDelaySeenFloor     = 8
	waitDelayEnforcedFloor = 2
)

// stripLineComment は行末コメントを落とす。
//
// 🚨 注記 (markerNoWaitDelay / markerWaitDelayIn) の判定には使わないこと。注記は行末コメントに
// 書く運用なので、剥がすと注記済みの行が offender に化ける。
// 文字列リテラル中の `//` (URL 等) も切るが、切りすぎは「代入が見つからない = 落ちる」側へ倒れる
// (静かに通る側には倒れない) ので許容する。
func stripLineComment(line string) string {
	if i := strings.Index(line, "//"); i >= 0 {
		return line[:i]
	}
	return line
}

// hasExecCmdCall は「コードとして Cmd を作っているか」を判定する。
//
// 🚨 コメントを剥がしてから見る。剥がさないと、`exec.Command(` と書いた doc コメントが
// 幽霊の offender になるうえ **seen を水増しして floor の低下を隠す**。
func hasExecCmdCall(line string) bool {
	code := stripLineComment(line)
	for _, p := range execCmdPatterns {
		if strings.Contains(code, p) {
			return true
		}
	}
	return false
}

// hasWaitDelayExemption は行内注記による免除を判定する。**生の行**を渡すこと。
func hasWaitDelayExemption(line string) bool {
	return strings.Contains(line, markerNoWaitDelay) || strings.Contains(line, markerWaitDelayIn)
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
		if !hasExecCmdCall(line) {
			continue
		}
		seen++
		// 注記は生の行で見る (コメントを剥がすと注記そのものが消える)
		if hasWaitDelayExemption(line) {
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

// walkGlogxSources は段 A / 段 B が共有する走査。非テストの .go を 1 つずつ fn へ渡す。
// 走査対象がずれると両段が同時に効かなくなるので、経路は 1 本にしておく。
func walkGlogxSources(t *testing.T, fn func(path string, lines []string)) {
	t.Helper()
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
		fn(filepath.ToSlash(path), strings.Split(string(src), "\n"))
		return nil
	})
	if err != nil {
		t.Fatalf("ソース走査に失敗: %v", err)
	}
}

// ============================================================================
// 段 A: 字句走査
// ============================================================================

// TestEveryCommandContextSetsWaitDelay は「外部コマンドの Cmd を作ったら WaitDelay を張る」
// 規律をソース走査で強制する。射程と限界は冒頭の脅威モデルを見ること。
//
// なぜテストで縛るか (issue 105): この規律は 13 箇所中 12 箇所で守られ、1 箇所
// (issues/discover.go の RepoRoot) だけが静かに抜けていた。抜けても手元でも CI でも何も起きず、
// 実際にハングして初めて分かる種類の欠落なので、**書く人が覚えているか**に依存させない。
//
// 例外は行内に注記と理由を書く (markerNoWaitDelay / markerWaitDelayIn)。出力を取らない Run() は
// stdout/stderr が /dev/null 直結でパイプを作らないため、ctx の kill だけで足りる
// (WaitDelay が要るのは Output / CombinedOutput / Stdout に io.Writer を張る形)。
func TestEveryCommandContextSetsWaitDelay(t *testing.T) {
	var offenders []string
	seen, enforced := 0, 0
	walkGlogxSources(t, func(path string, lines []string) {
		o, s, e := waitDelayOffenders(path, lines)
		offenders = append(offenders, o...)
		seen += s
		enforced += e
	})
	if seen < waitDelaySeenFloor {
		t.Fatalf("外部コマンド実行の検出数が %d 件しかない (下限 %d)。"+
			"走査パスか検出パターンが壊れている疑い", seen, waitDelaySeenFloor)
	}
	if enforced < waitDelayEnforcedFloor {
		t.Fatalf("WaitDelay を強制している箇所が %d 件しかない (下限 %d)。"+
			"注記が増えて守りが消えていないか確認する", enforced, waitDelayEnforcedFloor)
	}
	if len(offenders) > 0 {
		t.Fatalf("WaitDelay を張っていない外部コマンド実行がある (subproc.CommandContext を使うか、"+
			"理由つきで `%s` / `%s` を行内に書く):\n  %s",
			markerNoWaitDelay, markerWaitDelayIn, strings.Join(offenders, "\n  "))
	}
}

// TestWaitDelayOffendersCanary は、既知の入力に対して既知の答えが出ることを本走査の前に固定する。
// 本走査と**同じ** waitDelayOffenders を通す。
//
// 目的は「production に何件あるか」に依存せずに検出力を固定すること。
func TestWaitDelayOffendersCanary(t *testing.T) {
	cases := []struct {
		name         string
		lines        []string
		wantOffender bool
		wantSeen     int
		wantEnforced int
	}{
		{
			name:         "CommandContext で代入が無い",
			lines:        []string{`c := exec.CommandContext(ctx, "gh")`, `return c.Output()`},
			wantOffender: true, wantSeen: 1, wantEnforced: 1,
		},
		{
			name:         "CommandContext で直後に代入がある",
			lines:        []string{`c := exec.CommandContext(ctx, "gh")`, `c.WaitDelay = subproc.WaitDelay`},
			wantOffender: false, wantSeen: 1, wantEnforced: 1,
		},
		{
			// 段 A を広げた本体。ctx 無しの exec.Command( も検査対象。
			// 🚨 `exec.Command(` は `exec.CommandContext(` を含まないので、片方だけの
			// パターンにすると、このケースかひとつ上のケースのどちらかが必ず素通りする。
			name:         "ctx 無しの exec.Command で代入が無い",
			lines:        []string{`c := exec.Command("gh")`, `return c.Output()`},
			wantOffender: true, wantSeen: 1, wantEnforced: 1,
		},
		{
			name:         "ctx 無しの exec.Command で直後に代入がある",
			lines:        []string{`c := exec.Command("gh")`, `c.WaitDelay = subproc.WaitDelay`},
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
			name:         "no-waitdelay の注記で免除されている",
			lines:        []string{`return exec.CommandContext(ctx, "x").Run() // subproc: no-waitdelay — 理由`},
			wantOffender: false, wantSeen: 1, wantEnforced: 0,
		},
		{
			name:         "waitdelay-in の注記で免除されている",
			lines:        []string{`return runGitCmd(exec.Command("git")) // subproc: waitdelay-in runGitCmd`},
			wantOffender: false, wantSeen: 1, wantEnforced: 0,
		},
		{
			// コメント内の呼び出しは seen を水増ししてはいけない (floor の低下を隠す)
			name:         "コメントの中で exec.Command に言及しているだけ",
			lines:        []string{`// ここは exec.Command("gh") を使わないこと`, `return nil`},
			wantOffender: false, wantSeen: 0, wantEnforced: 0,
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

// ============================================================================
// 段 B: import 境界
// ============================================================================

// execCmdAllowlist は「*exec.Cmd を作ってよい」非テストファイルと、その理由。
//
// 🚨 母集合は「os/exec を import しているファイル」ではなく「**Cmd を作るファイル**」。
// 実測 2026-09-09: import しているのは 6 ファイルだが、cli_health.go と github.go は
// exec.ErrNotFound / exec.ExitError を使うだけで Cmd を一切作らない。import で数えると
// 「サブプロセスを起動するファイル」と「そのエラー型を触るファイル」が混ざる。
//
// 増やすときは、そのファイルが本当に外部プロセスの起動口であるべきかを考える
// (既存の入口 = external_commands.go / subproc に寄せられないか)。
var execCmdAllowlist = map[string]string{
	"external_commands.go": "ブラウザ / エディタ / クリップボードの起動口",
	"gitlog.go":            "git 実行の共通入口 (WaitDelay は runGitCmd が張る)",
	"open_workspace.go":    "e / E の前景エディタ・ファイラー起動 (tea.ExecProcess)",
	"tui.go":               "job ログを nvim の scratch で開く前景実行",
}

// osExecImportAlias は行が `"os/exec"` の import かを見て、別名 (`xe` / `_` / `.`) を返す。
// 素の import なら alias は空文字列。
func osExecImportAlias(line string) (alias string, found bool) {
	code := stripLineComment(line)
	i := strings.Index(code, `"os/exec"`)
	if i < 0 {
		return "", false
	}
	prefix := strings.TrimSpace(code[:i])
	prefix = strings.TrimSpace(strings.TrimPrefix(prefix, "import"))
	prefix = strings.TrimSpace(strings.TrimPrefix(prefix, "("))
	return prefix, true
}

// osExecFileFacts は 1 ファイル分の「os/exec との関わり方」を返す。
// 段 B の判定は本走査と canary の両方がこの関数を通る。
func osExecFileFacts(lines []string) (aliases []string, makesCmd bool) {
	for _, line := range lines {
		if alias, ok := osExecImportAlias(line); ok && alias != "" {
			aliases = append(aliases, alias)
		}
		if hasExecCmdCall(line) {
			makesCmd = true
		}
	}
	return aliases, makesCmd
}

// TestOsExecImportBoundary は段 B。字句ゲート (段 A) が原理的に見られない 2 つを止める。
//
//  1. **別名 import の禁止**。`import xe "os/exec"` を許すと `xe.Command(…)` が段 A の字句を
//     素通りする。ここで禁じることで、段 A の `exec.Command(` というパターンが
//     「この module では Cmd 生成を漏れなく指す」と言える状態を保つ (段 A の前提条件)。
//  2. **Cmd を作ってよいファイルの allowlist**。新しいファイルが外部プロセスを起こし始めたら
//     落ちる。allowlist に載せる作業が「WaitDelay の規律を読む」入口になる。
//
// 🚨 段 B は WaitDelay を一切読まない。既存箇所の代入が消える退行 (issue 105 の形) は
// 段 A だけが検出する。**段 B へ「移す」と検出力が落ちる**ので、両方を置く。
func TestOsExecImportBoundary(t *testing.T) {
	var violations []string
	observed := map[string]bool{}
	walkGlogxSources(t, func(path string, lines []string) {
		aliases, makesCmd := osExecFileFacts(lines)
		for _, alias := range aliases {
			violations = append(violations, path+`: os/exec を別名 import している (`+alias+
				`)。段 A の字句走査が素通りするので素の import にすること`)
		}
		if !makesCmd {
			return
		}
		observed[path] = true
		if _, ok := execCmdAllowlist[path]; !ok {
			violations = append(violations, path+
				": *exec.Cmd を作っているが execCmdAllowlist に無い。"+
				"subproc.CommandContext か既存の起動口 (external_commands.go) に寄せられないか検討し、"+
				"それでも要るなら理由つきで allowlist へ足す")
		}
	})
	// 逆向き: allowlist に載っているのに Cmd を作らなくなったファイルを残さない
	// (放置すると「Cmd を作るファイル」でない項目が溜まり、母集合の意味が薄れる)。
	for path := range execCmdAllowlist {
		if !observed[path] {
			violations = append(violations, path+
				": execCmdAllowlist に載っているが Cmd を作っていない (走査対象から消えたか、"+
				"外部実行をやめた)。allowlist から外すこと")
		}
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("os/exec の import 境界に違反がある:\n  %s", strings.Join(violations, "\n  "))
	}
}

// TestOsExecFileFactsCanary は段 B の判定を、本走査と同じ osExecFileFacts で固定する。
func TestOsExecFileFactsCanary(t *testing.T) {
	cases := []struct {
		name        string
		lines       []string
		wantAliases []string
		wantMakes   bool
	}{
		{
			name:      "グループ import の素の os/exec",
			lines:     []string{"import (", `	"os/exec"`, ")"},
			wantMakes: false,
		},
		{
			name:      "単独 import の素の os/exec",
			lines:     []string{`import "os/exec"`},
			wantMakes: false,
		},
		{
			name:        "グループ import の別名",
			lines:       []string{"import (", `	xe "os/exec"`, ")"},
			wantAliases: []string{"xe"},
		},
		{
			name:        "単独 import の別名",
			lines:       []string{`import xe "os/exec"`},
			wantAliases: []string{"xe"},
		},
		{
			name:        "blank import",
			lines:       []string{"import (", `	_ "os/exec"`, ")"},
			wantAliases: []string{"_"},
		},
		{
			name:        "dot import",
			lines:       []string{"import (", `	. "os/exec"`, ")"},
			wantAliases: []string{"."},
		},
		{
			name:      "コメント内の os/exec は import ではない",
			lines:     []string{`// xe "os/exec" のような別名は禁止`},
			wantMakes: false,
		},
		{
			name:      "Cmd を作る",
			lines:     []string{`c := exec.Command("gh")`},
			wantMakes: true,
		},
		{
			name:      "CommandContext で Cmd を作る",
			lines:     []string{`c := exec.CommandContext(ctx, "gh")`},
			wantMakes: true,
		},
		{
			// cli_health.go / github.go の形。import しているが Cmd は作らない
			name:      "エラー型だけ使う",
			lines:     []string{"import (", `	"os/exec"`, ")", `if errors.Is(err, exec.ErrNotFound) {`},
			wantMakes: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			aliases, makesCmd := osExecFileFacts(tc.lines)
			if strings.Join(aliases, ",") != strings.Join(tc.wantAliases, ",") {
				t.Fatalf("aliases = %v, want %v", aliases, tc.wantAliases)
			}
			if makesCmd != tc.wantMakes {
				t.Fatalf("makesCmd = %v, want %v", makesCmd, tc.wantMakes)
			}
		})
	}
}
