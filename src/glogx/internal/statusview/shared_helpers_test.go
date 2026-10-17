package statusview

import (
	"glogx/internal/testenv"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// main のテスト (browseModel と組み合わせるもの) と同じヘルパーの、中のテスト用の写し (issue 817 で画面を移したとき、
// 両方のテストが使っていたもの)。

// newTestStatusView は「開いた状態で porcelain を読み込んだ」viewer を返す。
// 🚨 toggle() は通さない: 返り値の tea.Batch には tea.Tick (自動更新 1.5s / プレビュー 120ms) が
// 混ざっており、テストで実行すると本当に待つことになる。読み込みは loadCmd だけを同期実行する。
func newTestStatusView(t *testing.T, porcelain string) *StatusView {
	t.Helper()
	v := NewStatusView()
	v.closeAnimOff = true
	v.shown = true
	stubWorktreeStatus(t, porcelain, nil)
	applyStatusLoad(t, &v)
	return &v
}

// stubWorktreeStatus は loadWorktreeStatus を差し替える (返す porcelain を後から差し替えられる
// ように、ポインタ経由で読む)。返り値のポインタへ書くと次の読み込みの結果が変わる。
func stubWorktreeStatus(t *testing.T, porcelain string, err error) *struct {
	out   string
	err   error
	calls int
} {
	t.Helper()
	state := &struct {
		out   string
		err   error
		calls int
	}{out: porcelain, err: err}
	orig := loadWorktreeStatus
	loadWorktreeStatus = func() (worktreeStatus, error) {
		state.calls++
		if state.err != nil {
			return worktreeStatus{}, state.err
		}
		return parseWorktreeStatus(state.out), nil
	}
	t.Cleanup(func() { loadWorktreeStatus = orig })
	return state
}

// applyStatusLoad は v.loadCmd() を同期実行して結果を receive へ渡す (tick を含まない Cmd なので
// そのまま実行できる)。
func applyStatusLoad(t *testing.T, v *StatusView) {
	t.Helper()
	cmd := v.LoadCmd()
	if cmd == nil {
		t.Fatal("loadCmd() = nil (取得中のまま?)")
	}
	msg, ok := cmd().(StatusLoadMsg)
	if !ok {
		t.Fatalf("loadCmd() の msg = %T, want statusLoadMsg", msg)
	}
	v.Receive(msg)
}

// assertSanitizedAndVisible は無害化テストの 2 つの主張を対で確かめる (issue 284):
//
//  1. 制御シーケンス / OSC の中身が残っていない (本来の主張)
//  2. 🚨 **細工した値が実際に描かれている** (陽性対照)
//
// 2 が無いと、箱が空を返した瞬間に 1 の assert が 1 つも実行されず vacuous に緑を返す
// (実測: markNextBox の先頭に `return nil` を入れても PASS した)。
// 同じ罠は TestDoctorLiveDockerScanIsSanitized が sawItem で 1 本だけ塞いでいたもの。
func assertSanitizedAndVisible(t *testing.T, where string, lines []string, wantVisible ...string) {
	t.Helper()
	for _, line := range lines {
		if testenv.HasTerminalControl(line) {
			t.Errorf("%s に制御シーケンスが残った: %q", where, line)
		}
		if strings.Contains(line, "PWNED") {
			t.Errorf("%s に OSC の中身が残った: %q", where, line)
		}
	}
	joined := strings.Join(lines, "\n")
	for _, w := range wantVisible {
		if !strings.Contains(joined, w) {
			t.Fatalf("%s に %q が描かれていない (検査対象が空 = 上の assert は 1 つも sink を守っていない)",
				where, w)
		}
	}
}

// issue 417: 外部由来の文字列が termsafe を通らずに画面 / stderr へ届いていた 4 経路。
// どれも表示にしか使わない値なので、受け取った時点で平文にしている。
const evil417 = "\x1b]52;c;cHduZWQ=\a\u009b2J\u202eevil"

func assertNoTermControl(t *testing.T, what, got string) {
	t.Helper()
	if strings.ContainsAny(got, "\x1b\a\u009b\u202e") {
		t.Errorf("%s に制御シーケンスが残っている: %q", what, got)
	}
	if !strings.Contains(got, "evil") {
		t.Errorf("%s の本文まで落ちている: %q", what, got)
	}
}

// repoTmpDir は使い捨て repo の置き場 (dotfiles の ./tmp)。
//
// 🚨 無ければ作る。tmp/ は gitignore なので**新品チェックアウトには存在しない**: 開発マシンには
// 常にあるため絶対に再現しない条件で、CI だけが落ちた (run 30823977760 "stat ../../tmp: no such
// file or directory")。相対パスで決め打ちせず repo root から引くのは、テストが t.Chdir で cwd を
// 移すため (ヘルパーの呼び出し順に依存させない)。
func repoTmpDir(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("repo root を解決できない: %v", err)
	}
	dir := filepath.Join(strings.TrimSpace(string(out)), "tmp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// statusRec は `git status --porcelain -z` の 1 レコード (NUL 終端) を組む。
func statusRec(parts ...string) string {
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p)
		b.WriteByte(0)
	}
	return b.String()
}

// statusRowsFixture は st を組んで receive まで通した statusView を返す。
func statusRowsFixture(t *testing.T, files int) *StatusView {
	t.Helper()
	recs := []string{"## master...origin/master"}
	for i := range files {
		p := "src/pkg" + strconv.Itoa(i) + "/f.go"
		if i%2 == 0 {
			recs = append(recs, "M  "+p) // staged
		} else {
			recs = append(recs, " M "+p) // unstaged
		}
	}
	v := &StatusView{shown: true}
	v.Receive(StatusLoadMsg{st: parseWorktreeStatus(strings.Join(recs, "\x00") + "\x00"), gen: v.gen})
	if len(v.rows) != files {
		t.Fatalf("前提が崩れた: rows=%d want %d", len(v.rows), files)
	}
	return v
}
