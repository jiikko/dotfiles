package statusview

import (
	"strconv"
	"strings"
	"testing"
)

// main のテスト (browseModel と組み合わせるもの) と同じヘルパーの、中のテスト用の写し (両方のテストが使っていたもの)。

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

// 外部由来の文字列が termsafe を通らずに画面 / stderr へ届いていた 4 経路。
// どれも表示にしか使わない値なので、受け取った時点で平文にしている。
const evil417 = "\x1b]52;c;cHduZWQ=\a\u009b2J\u202eevil"

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
