# 既存の呼び出しに「新しい値・フラグ・経路」を通す修正では、受け側のガードを先に洗う — なぜ・実例

ルール本文: `~/dotfiles/_claude/rules/survey-receiver-guards-before-passing-new-values.md`（`~/.claude/rules/` に link され、毎セッション起動時に読まれる）。
この文書は起動時には読まれない。ルールの根拠・起源・実例を保存し、ルールを疑う・改訂する・却下するときに読む。

## なぜ (起源: obaket issue 514 / 518, 2026-08-21)

同一セッションで**同じ構造の見落としを 2 回**やり、どちらも敵対的レビューが P1 として摘出した:

1. **issue 514**: 削除呼び出しに `isDirectory: true` を渡す修正を入れた結果、受け側
   (`S3Adapter.deleteImpl`) の**非空 preflight** (issue 309 で追加されたガード) を
   その経路が初めて踏むようになった。呼び出し側 (reconcile executor) の catch は
   412 系しか想定しておらず、reconcile 全体が中断 → **修正前より悪い永久 wedge** になった
2. **issue 518**: directory 行を既存の削除安全判定 (`sourceDeletionSafety`) に流した結果、
   file 前提の「snapshot 必須 = 無ければ unverifiable」が directory (S3 では snapshot が
   構造的に nil) に誤適用され、**本命ケースが恒久 attention** になった

共通点: 修正自体は正しい方向で、単体では green だった。**壊れたのは「新しい値で初めて
到達する既存ガードとの相互作用」**で、fake がガードを模していなかったためテストも素通しした。

## ルール本文から移した実例

本文には規範だけを残し、その根拠になった実例をここへ移した（元の文脈のまま）。

 (実例 2026-08-21 obaket 536:
  page ループを共通 helper に置換したら、helper の新 throw (`serviceUnavailable`) を受ける
  変換 (部分削除の件数を載せる責務) が空白になり、同日に自分で確立した不変条件
  「部分削除は明示報告」を自分の refactor で壊しかけた)

## 「自分が宣言した不変条件」節の起源 (dotfiles, 2026-09-06)

glogx の監査対応 21 件を実装したセッションの敵対的レビューで、**同じ形の欠陥が 4 件**出た。
どれも「対にして持つと決めたのに、対を崩す経路を洗わずに片方だけ更新する箇所を残した」:

| 対にしたもの | 崩した経路 | 症状 |
|---|---|---|
| `detailsLoading` / `detailsWaiting` (包含関係をコメントで宣言) | `detailMsg` / `basisMsg` が loading しか消さない | 実取得中の札が世代交代で落ち「取得中なのに CI job 情報なし」 |
| `rows` / `rowsGen` (世代カウンタ) | AST ゲートが引数経由・var 宣言経由の代入を見ない | 世代が進まず displayRows が恒久 stale |
| `diskHasDetail` (述語) / `diskDetail` (builder) | 述語を作らず `hasDetail: true` 固定 | 展開しても 0 行の行で q が飲まれ doctor が閉じない |
| 見出し / 同一性 | 見出しの不一致を「別 issue」と断定 | 移動 + 改題で本文が閉じ、起きていない事象を告げる |

修正はいずれも「更新の責務を 1 関数へ寄せる」形にした
(`clearDetailsFlags` / `collapseTargetAtCursor` / `diskHasDetail`)。
`clearDetailsFlags` については迂回を AST ゲートで止めている
(`src/glogx/details_flags_test.go`: 走査 75 件 / 札の delete 2 件 / 違反 0 件)。

なお、`collapsibleAtCursor` と `collapseAtCursor` が同じ判定の第 2 実装を持っていたのは
**既存コード側**の同型で、修正中に「片方を直したのに挙動が変わらない」ことで見つかった。
