package commitview

import (
	"glogx/internal/statusview"
	"testing"
)

// main のテスト (browseModel と組み合わせるもの) と同じヘルパーの、中のテスト用の写し (両方のテストが使っていたもの)。

// newTestStatusView は「開いた状態で porcelain を読み込んだ」viewer を返す。
// 🚨 toggle() は通さない: 返り値の tea.Batch には tea.Tick (自動更新 1.5s / プレビュー 120ms) が
// 混ざっており、テストで実行すると本当に待つことになる。読み込みは loadCmd だけを同期実行する。
func newTestStatusView(t *testing.T, porcelain string) *statusview.StatusView {
	t.Helper()
	v := statusview.NewStatusView()
	v.Fixture().SetCloseAnim(false)
	v.Fixture().SetShown(true)
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
	t.Cleanup(statusview.FixtureStubWorktree(func() (string, error) {
		state.calls++
		return state.out, state.err
	}))
	return state
}

// applyStatusLoad は v.loadCmd() を同期実行して結果を receive へ渡す (tick を含まない Cmd なので
// そのまま実行できる)。
func applyStatusLoad(t *testing.T, v *statusview.StatusView) {
	t.Helper()
	cmd := v.LoadCmd()
	if cmd == nil {
		t.Fatal("loadCmd() = nil (取得中のまま?)")
	}
	msg, ok := cmd().(statusview.StatusLoadMsg)
	if !ok {
		t.Fatalf("loadCmd() の msg = %T, want statusLoadMsg", msg)
	}
	v.Receive(msg)
}
