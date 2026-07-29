# 030 refactor: glogx リファクタリング余地監査 (2026-07-29)

リファクタリング観点の監査結果 (sonnet 調査 → main agent が file:line で全件裏取り済み)。
codex レビュー未通過 (codex 不使用の運用指示による)。評価原則は issue 028 と同じ:
「複雑性が実際に下がるか」で判定し、行数分割は提案しない。既決事項 (018/023/028:
browseModel 分解しない・toast 調停凍結・diffOverlay/jobDetailOverlay の意図的別実装) は
再提案対象から除外済み。

## P2 (touch 箇所が実際に減るもの)

### 1. FetchCommitPR / FetchPRStatus の骨格重複 (github.go)

同じ GraphQL レスポンス形 (`Data.Repository.Object.AssociatedPullRequests.Nodes`) の
「単発 object クエリ → unmarshal → obj==nil → pickBest」が 2 関数に複製されている。差分は
query の field list と Node 型 (`PRRef` / `PRStatus`) のみ。`pickBestByState[T]` は generics
化済みなのに外側の骨格が未統合。レスポンス構造の変更時 touch が 2→1 になる。
公開シグネチャは不変で内部ヘルパー `fetchAssociatedPRs[T any](ctx, run, repo, sha, fields)`
に畳める。

### 2. PR 状態→色マップの 2 箇所実装 (render.go prBadge / pr_status_overlay.go prStateLabel)

`switch pr.State { OPEN→緑 / MERGED→マゼンタ / CLOSED→赤 / default→dim }` が独立に 2 つ存在。
片方だけ直して食い違う事故が構造的に起きうる。`prStateColor(state string) string` に集約。

### 3. withScrollbar が本番デッドコード化 (box.go)

2026-07-29 の diff/job 影付き化で両利用者が `withShadowScrollbar` へ移行し、非 shadow 版
`withScrollbar` の本番呼び出しが 0 件になった (残るは box_test.go の単体テストのみ。
`deadcode ./...` でも unreachable 検出)。非 shadow でスクロールする panel が現れるまで
削除し、box_test.go の対応テストは `withShadowScrollbar` 版へ付け替える。

### 4. 「実行中 job」判定式の 3 箇所直書き (tui.go)

`job.State == StatePending && !job.StartedAt.IsZero()` が jobTimeSuffix /
maybeFetchETABasis / panelHasRunningJob に同文で 3 回出現 (行番号確認済み)。
「実行中 (経過時間が意味を持つ) job」というドメイン概念に名前がなく、判定条件を変えるとき
3 箇所の漏れなし修正が必要。`CheckDetail` に `running() bool` を生やして置換。

## P3 (小さいが安い)

### 5. テストヘルパー uniformWidth の完全一致コピー

`tui_panel_test.go` と `tui_overlay_test.go` に同一クロージャ (エラーメッセージまで一致)。
既存の `tui_helpers_test.go` へ関数として抽出。

### 6. tab 展開の 3 箇所反復 (render.go)

`ReplaceAll(text, "\t", "    ")` + ガード + ほぼ同一コメントが 3 箇所。
`expandTabsForWidth(s, width)` に集約。文脈が微妙に違うため実利は小さい (低確信)。

### 7. -n / --max-count の同型 3 分岐 (options.go ParseArgs)

`parseCount → エラー整形 → MaxCount/HasCount 代入` の反復。ヘルパーに畳めるが
単純で読みにくくはないため優先度低。

## 見送り (調査済み・issue 化しない)

- **toast.show / showInfo の共通 3 行**: 2 メソッドのみで可読性への影響なし
- **claudeVersionCache / usageCacheEntry の TTL キャッシュ generics 化**: `cache.go` の
  per-SHA 可変 TTL 版は乗らず、無理に一般化すると複雑性が逆に上がる可能性 (低確信)
- **actionModal updating の終了ヒント直書き**: handleKey が updating 中の Ctrl-C を常に
  ブロックする設計 (自己バイナリ更新の中断防止) により `runningQuitHint()` のバリアントが
  出る余地がなく、固定文言が正しい (意図的)
- **usage/render.go の dispWidth・ANSI 定数重複**: 「切り出し時に glogx への依存を残さない
  ための自己完結」と明記済みの意図的重複
- **usage.RenderLine 等の unreachable 検出**: 「将来単独コマンド化」の設計意図が package doc
  に明記され直接テストもあるため公開 API として維持

## 着手順の推奨

1. **P2-3** (withScrollbar 削除。今日の変更の副産物で scope 最小)
2. **P2-2 / P2-4** (1 関数 / 1 メソッド追加の置換のみ)
3. **P2-1** (generics ヘルパー。テスト `github_test.go` への影響確認込み)
4. P3 は他の変更でファイルを触るときに同時に

## 関連

- issues/pending/028 — box/toast の前回監査 (P2 toast 調停は trigger 待ちのまま)
- issues/done/018 — browseModel は「これ以上触る価値なし」判定済み (再提案しない)
- `_claude/rules/verify-design-intent-before-refactor.md` — 評価原則の一次情報
