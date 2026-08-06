# glogx: カーソル窓収束ロジックの重複を統合 (issuesView.windowOffset / windowOffsetFor)

## 現状

「カーソルを含む最小の窓へ offset を収束させる」同一アルゴリズムが 2 箇所に独立実装されている:

- `src/glogx/issues_view.go` — `func (v *issuesView) windowOffset(rows int) int` (min/max 2 行で表現)
- `src/glogx/status_view.go` — `func windowOffsetFor(offset, cursor, total, rows int) int` (if 2 本で表現)

書き方は違うが式は等価。土台の `clampScrollOffset` は共有済みだが、収束式そのものは未統合。両者とも「offset は状態でなく導出値」という同じ設計意図を doc コメントに持つ。

## 提案

`windowOffsetFor(offset, cursor, total, rows int) int` を共通ヘルパーとして 1 箇所に置き、`issuesView.windowOffset` はそのラッパーにする。

## 複雑性が下がる根拠

「カーソルが画面外に出ない」不変条件を変更するとき(窓のマージン変更等)の touch 箇所が 2→1 になる。現状は 2 実装の等価性を目視でしか確認できない。

## 注意

- 2026-08-06 時点で `status_view.go` に並行セッションの未コミット変更あり。着手前に該当関数が現状のままか要確認
- codex レビューはユーザー方針 (codex 不使用, 2026-07-17〜) により省略
