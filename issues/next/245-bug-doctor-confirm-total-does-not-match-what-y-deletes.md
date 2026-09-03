# 245 bug: 確認画面の「解放される見込み」と `y` が実際に消す対象が食い違う（中断した下見で発火）

起票日: 2026-09-04
出典: issue 242（P3 の実装）の敵対的レビュー。**実コードで裏を取った**（`deletable` / `selectedResults` / `confirmLines` の 3 箇所を突き合わせ）
重要度: **P1**（破壊的操作の同意を、実際より小さい数字を見せて取る）
対象: `src/glogx/doctor_delete.go` の `handleDeleteKey` の `case "y"` / `selectedResults` / `deletable` / `confirmLines`、`src/doctor/disk/delete.go` の `Delete` の DryRun 分岐

## 症状

下見（DryRun）が**部分的に**中断された状態で確認画面が出ると、そこに出る
「解放される見込み」は**中断で落ちたエントリを除外した数字**なのに、`y` を押すと
**そのエントリも含めて削除される**。見せた量より多く消える。

## 発火条件（実コードで追跡）

1. doctor で複数のエントリを Space で選ぶ
2. `d` を押す → 下見が走る
3. **1 件目の `planDelete` が終わった後**に Ctrl-C ×2 で中断する
   （issue 242 の P3-3 でこの操作をパネルに案内するようにした。機能自体は前からある）
4. 1 件目は `Planned` なので `planHasWork` が真 → 見出しは「本当に削除しますか?」、hint は「y: 削除する」
5. 2 件目以降は `OutcomeFailed`（`delete.go` の `fresh.Partial` 分岐）で `❌ できず` と出る
6. `y` を押すと **2 件目以降も消える**

## 原因（3 つが噛み合っている）

1. **DryRun ループに中断の打ち切りが無い** — `disk/delete.go` の `if opt.DryRun { for … }` は
   `ctx.Err()` を一度も見ない（非 DryRun のループは見ている）。中断後も残りを走査し、
   各エントリが個別に `Failed` になる
2. **確認画面の合計は skipped/failed を除外する** — `confirmLines` は
   `skipped := Outcome == Skipped || Failed` のエントリを `freeing` / `trashing` に足さない
   （それ自体は正しい。「消えないものを解放量に数えない」）
3. 🚨 **`y` の対象は「確認画面に出した plan」ではなく「UI の選択」** —
   `case "y"` は `v.selectedResults()` を取り、`v.deletable(r)` で再検査してから
   `startDelete(targets, false)` を呼ぶ。`deletable` が見るのは**元の走査結果**
   （`disk.Result` の Status / Items / FromSnapshot / Reused / Inspect）だけで、
   **下見の結末（`plan.Entries` の Outcome）を一切見ない**。本番は新しい ctx で
   `planDelete` からやり直すので、中断で `❌ できず` になったエントリも普通に削除される

つまり **plan と実行対象が別ソース**で、確認画面が何を約束しても実行が従わない構造になっている。

同じファイルの `confirmLines` には「合計を 2 行に割ると狭い画面で先に落ちて『1 件目のサイズだけが
見えている状態で y を受ける』形になる（敵対レビュー 2026-09-03: 78GB の削除で 1.0GB しか
見えなかった）」という記録がある。**同じ形の過小表示が、別の経路で残っている**。

## 直し方の候補

- **(a) DryRun ループに `ctx.Err()` の早期打ち切りを入れる**（[246](246-bug-doctor-dryrun-abort-has-no-reason.md) と同じ手当て）。
  残りが `Failed` にならないので、部分中断の確認画面自体が出なくなる
- **(b) 中断で終わった下見では確認を取らない**。`DeleteReport` に「中断された」を持たせ
  （今は `phaseAborted` を history にしか書いていない）、真なら `confirm` に落とさず
  「中断しました（r で取り直してください）」を出す
- **(c) 構造的な根治: `y` の対象を「確認画面に出した plan の対象」に揃える**。
  今は plan と実行対象が別ソースなので、(a)(b) を入れても「確認に出した内容と実行が一致する」
  保証にはならない

(a) は 246 のついでに入る。**(c) が本命**で、(a)(b) はその手前の緩和策。

## 同型（同じ「確認画面が結末について嘘をつく」形）

**item 階層の件数とパス一覧が過大** — これは
**[233](233-bug-doctor-confirm-counts-skipped-items.md) が既に起票済み**（`confirmLines` の
`len(e.Items)` と `deletePathLines` が、下見で `Skipped` になった Item まで数えて並べる）。

本 issue（**エントリ階層**で plan と実行対象が別ソース）と 233（**item 階層**で確認画面が
Item の Outcome を見ない）は、「確認画面が下見の結末を部分的にしか読まない」という同じ原因の
別階層。233 の修正方向（`OutcomePlanned` の Item に絞る）は本 issue の (c) と整合するので、
(c) を設計するときは 233 の結論を前提にできる。

## 却下した指摘（再提案しないこと）

- **確認画面の行が結果画面の行と同一に見える** — issue 242 の P3-1 で結末語を
  `doctorPlanOutcomeWord` に分けた際、`Skipped` は一覧と同じ「🚫 対象外」に寄せたので
  **Skipped 行は結果画面（「🚫 触れず」）と区別できる**。残るのは `Failed` 行（両方「❌ できず」）
  だけで、見出しと tail が違うため致命ではない。`📋 表示のみ` / `🚮 ゴミ箱へ` は
  変更前から両画面に在り、今回作った問題ではない

## 関連

- [246](246-bug-doctor-dryrun-abort-has-no-reason.md) — 同じ中断の経路。あちらは「理由が尻切れ」、こちらは「合計と実行対象の食い違い」
- [242](242-research-doctor-ux-audit-2026-09-04.md) P3-3 — 中断の案内をパネルに出した変更（この経路を踏みやすくした）
- 233 — 確認の件数が skipped を数えている件（同じ「確認画面の数え方」ファミリー）
