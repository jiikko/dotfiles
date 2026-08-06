# glogx: 本番未使用のドメインヘルパー 2 件の整理 (toast.visible / worktreeStatus.clean)

## 現状 (grep で確認済み, 2026-08-06)

1. **`toast.visible()`** (`src/glogx/toast.go`) — 呼び出しは全て `*_test.go`。本番コード (`tui.go` 等) からは一度も呼ばれず、テストのためだけに存在する export になっている
2. **`worktreeStatus.clean()`** (`src/glogx/worktree_status.go`) — doc コメントは「画面は開くが『クリーンです』を出す (spec 6 節)」とまさに UI 分岐向けに書かれているのに、実際の UI 分岐 (`status_view.go` の `len(v.rows) == 0` 直書き ×4 箇所) は `clean()` を呼んでいない。呼び出しはテストのみ

## 提案

1. `toast.visible()`: 本番で使う予定がなければテストヘルパーとして test 側へ移すか、等価な本番判定箇所があればそちらを置き換えて一本化
2. `status_view.go` の `len(v.rows) == 0` 分岐を `v.st.clean()` に置き換え、「クリーン判定」の定義を実装 1 箇所に閉じる (将来 untracked 除外等の定義変更時に touch 箇所が 1 になる)

## 注意

- `status_view.go` は並行編集中の可能性あり。着手前に該当行を再確認
- codex レビューはユーザー方針 (codex 不使用) により省略
