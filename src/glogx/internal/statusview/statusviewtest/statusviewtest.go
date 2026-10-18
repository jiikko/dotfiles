//go:build !nofixture

// 各画面の Fixture() を使うので、Fixture を外したビルド (-tags=nofixture。到達不能の検査) からはこのパッケージごと外れる。

// Package statusviewtest は status 画面と組み合わせるテストが共有する部品 (開いた status viewer を作る・git status の取得を差し替える)。
// main のテスト (browseModel と組み合わせるもの) と、別の画面のパッケージのテスト (commitview) から使う。
// 🚨 本番のコードから import しない (testing を import する。depguard が止める)。
// 🚨 internal/statusview の中のテストからは使えない (statusview を import するので import の循環になる)。中のテストは内部を直接触る自分の版を持つ。
package statusviewtest

import (
	"testing"

	"glogx/internal/statusview"
)

// NewView は「開いた状態で porcelain を読み込んだ」viewer を返す。
// 🚨 Toggle は通さない: 返り値の tea.Batch には tea.Tick (自動更新 1.5s / プレビュー 120ms) が混ざっており、
// テストで実行すると本当に待つことになる。読み込みは LoadCmd だけを同期実行する。
func NewView(t *testing.T, porcelain string) *statusview.StatusView {
	t.Helper()
	v := statusview.NewStatusView()
	v.Fixture().SetCloseAnim(false)
	v.Fixture().SetShown(true)
	StubWorktree(t, porcelain, nil)
	ApplyLoad(t, &v)
	return &v
}

// StubWorktree は git status の取得を porcelain / err を返す偽物に差し替え、テスト終了時に戻す。
func StubWorktree(t *testing.T, porcelain string, err error) {
	t.Helper()
	t.Cleanup(statusview.FixtureStubWorktree(func() (string, error) { return porcelain, err }))
}

// ApplyLoad は v.LoadCmd() を同期実行して結果を Receive へ渡す (tick を含まない Cmd なのでそのまま実行できる)。
func ApplyLoad(t *testing.T, v *statusview.StatusView) {
	t.Helper()
	cmd := v.LoadCmd()
	if cmd == nil {
		t.Fatal("LoadCmd() = nil (取得中のまま?)")
	}
	msg, ok := cmd().(statusview.StatusLoadMsg)
	if !ok {
		t.Fatalf("LoadCmd() の msg = %T, want statusview.StatusLoadMsg", msg)
	}
	v.Receive(msg)
}
