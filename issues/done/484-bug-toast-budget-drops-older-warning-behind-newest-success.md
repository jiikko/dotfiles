# 484 (bug): toast の描画予算が「最新の成功の箱」を数えず、長い警告 2 枚の古い方が出ない

起票日: 2026-09-26

## 概要

tuikit/toast で長い通知を折り返すようにした (commit「長い toast を窓の幅で折り返す / pro-con の断る案内を赤にする」と
その敵対レビュー対応 2 本) あとに、3 周目の敵対レビューが見つけた残り。**今回の変更による退行ではなく**、
折り返しを入れる前 (1 行の箱だけの頃) から同じ形はあった。長い警告で高さが増えたぶん起きやすくなっている。

glogx の `toastDrawBudget(page, warnings)` は「新しい重要警告 2 枚の実際の高さ」(`Stack.ImportantHeight(2, 幅)`) を下限に
確保する。一方 `Stack.BoxLines` は**最新の 1 枚を必ず残す**ので、最新が成功・進行中 (重要でない) だと、その箱の行数が
確保したはずの警告 2 枚ぶんの予算を先に使い、古い警告が落ちる。`toastDrawBudget` の doc にある「重要警告 2 枚ぶんを確保」
がこの組み合わせでは成り立っていない。

## 詳細 (再現)

敵対レビューが公開 API だけで再現した値 (`toastDrawBudget` の式を写して計算。未 commit の probe で、テストには残っていない):

- 積む順: 長い警告 A (約 100 桁 → 幅 40 で 3 行、箱 6 行)、長い警告 B (同)、成功 `ok` (箱 4 行)。静止まで Advance
- `BoxLines(false, toastDrawBudget(page, ImportantHeight(2, 40)), 40)` の結果:

| page | 予算 | 描画行数 | 描かれた警告 (✗) の枚数 |
|---|---|---|---|
| 20 | 12 | 10 | 1 (A が落ちる) |
| 24 | 12 | 10 | 1 |
| 30 | 15 | 10 | 1 |
| 40 | 20 | 16 | 2 (3 枚とも描かれる) |

期待: 窓に余裕がある (page 20〜30) なら、最新の成功と警告 A・B の 3 枚、少なくとも警告 2 枚は描かれる。

反証レビュー (2026-09-26、sonnet・読み取りのみ) が page 20/24/30 の行をコードから手計算で再現した。
「修正前から同じ形」「pro-con は対象外」「budgetToastModel は成功 1 枚だけ」も反証されなかった。

## 対応方針 (案)

- 最新が重要でないとき、その箱の高さも `warnings` に足す (「最新は必ず出す」と「警告 2 枚を確保」の両方を予算に入れる)。
  `ImportantHeight` の意味を「最新 + 重要 n 枚」に広げるか、glogx 側で最新の高さを別に足すかは、実装時に決める
- 上限は今の `max(page-1, BoxHeight)` のまま (窓を覆い切らない)
- 回帰テストは上の表の page=20 の組み合わせを glogx の View 経由で置く (`TestToastDrawBudgetFitsTwoWrappedWarnings` と同じ形)

## 関連して記録しておく制約 (直さない / 未測定)

- **低い窓**: 予算の上限 `page-1` が「最新 + 長い警告 2 枚」の行数より小さいと、古い警告は出ない。窓の高さの制約として受容した。
  最新は必ず出すので、押したキーの結果が消えることは無い。起票時は「page 9〜12」と書いたが、長い警告 2 枚 + 成功 (計 16 行) の
  場面では page 12〜16 がこれに当たる (修正後の敵対レビューの実測。修正前と同じ結果)
- **性能 (未実測)**: 警告が静止している間、`ImportantHeight` が毎フレーム `fullBox` を組み直すので 1 フレームあたり約 46 allocs 増える
  (敵対レビューの計測。成功だけなら 0)。`frame_alloc_test.go` の `TestFrameAllocBudget` は成功通知 1 枚の model しか持たず、
  この増加を測れない (toast-holding で 180 / 上限 186 の PASS)。フレーム時間への影響は未測定。
  **trigger**: 警告の出ている画面でカクつきの報告が出たとき、または上の対応で予算の計算を触るとき、警告 2 枚の model を
  frame_alloc の予算に足して測る

## 関連ファイル

- `src/tuikit/toast/toast.go` — `Stack.BoxLines` (最新の保護と行数の予算)、`Stack.ImportantHeight`
- `src/glogx/tui.go` — `toastDrawBudget` と、その呼び出し (`m.toast.BoxLines(...)`)
- `src/glogx/tui_global_chrome_test.go` — `TestToastDrawBudgetFitsTwoWrappedWarnings` / `TestToastShortSuccessesStayWithinHalfPage`
- `src/glogx/frame_alloc_test.go` — `budgetToastModel`
- pro-con (`src/pro-con/ui/toast.go` の `overlayToast`) は予算に region の行数をそのまま渡しているので、この issue の対象外

## 進捗

- [x] 最新の高さを予算に入れる — commit「toast の予算に最新の箱も数え、長い警告 2 枚の後の成功で古い警告が落ちないようにする (issue 484)」。
  `Stack.ImportantHeight` を `Stack.ReservedHeight` に作り替えた (最新 1 枚 + 重要な枚を最新を含め n 枚。BoxLines が描かない
  frame 0 の枚は数えない)。高さは箱を組まずに `item.height` で求める
- [x] page=20 の回帰テスト — `TestToastBudgetReservesNewestSuccessAndTwoWarnings` (glogx の View 経由)、`TestToastReservedHeight`、
  `TestToastHeightMatchesFullBox`。変異 3 本 (重要でない最新を数えない / frame 0 も数える / height を 1 行ずらす) でそれぞれ red を確認
- [x] 性能 — 最新を毎フレーム数えるため、箱を組む形だと toast-holding が 192 allocs/frame (上限 186) に増えた。`item.height` に替えて
  181 (上限 186)。敵対レビューの `testing.AllocsPerRun`: `ReservedHeight(2,40)` は成功 1 枚で 1、警告 2 枚 + 成功で 15
  (旧 ImportantHeight の約 46 より少ない)。frame_alloc に警告 2 枚の model は足していない (必要になったら上の trigger で)。
  フレーム時間は未測定

## 結果

- issue の表の場面 (長い警告 A・B + 成功、幅 40): reserved=16。page 18 / 19 / 20 / 24 / 30 / 40 で 3 枚とも描かれる (敵対レビューの実測)
- 成功・進行中だけなら reserved は最新 1 枚 (最大 6 行) で下限 8 行を超えず、予算は修正前と同じ
- `make test`: この修正の commit で lint の 1 件 (前の commit で入った toast_test.go の prealloc) を除いて通過。その 1 件を直して `make lint` rc=0
- 敵対レビュー (opus・読み取りのみ): 指摘 0 件。公開 API の probe で 4,585,536 通り (通知 6 種 × 最大 4 回 × frame 0〜12 × 幅 4 通り ×
  page 4〜60) を回し、ReservedHeight と BoxLines の保護対象の不一致 0 / 予算内で落ちる警告 0 / 予算・窓超え 0 / 途中で切れた箱 0
