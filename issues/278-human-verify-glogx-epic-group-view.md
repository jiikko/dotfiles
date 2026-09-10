# 278 human: glogx の issues view で epic group の折り畳み表示を目視確認する

起票日: 2026-09-06
期限: 2026-09-12
種別: human

## 確認すること (obaket 719 R4 の「実機の目視は未」)

obaket (`~/src/my-products/apps/obaket`) で glogx の issues view を開き:

1. 親行「▸ google-drive (N)」「▸ obaketcloud (N)」が出て、展開 / 折り畳みが動く
2. group 内の issue を `n` で group 内 `next/` へ移せる (global `issues/next/` に行かない)
3. 親行で番号 filter を Esc したとき添字が残らない (round 3 で直した箇所)

### 2026-09-06 追記 (issue 291 の実装ぶん)

4. group を展開すると、**`a` を押していない既定の状態でも** `epic/<name>/done/` の子が `✓` で、
   `epic/<name>/pending/` の子が `⏸` で見える (global の done/pending は従来どおり `a` を進めるまで見えない)
5. 親行が `▸ <name> (5 ✓2)` の形になる (done が 0 件の group は従来の `(N)` のまま)。
   **半角と全角が同じ桁に並んでいないか、実際の幅で目視する**
6. `epic/<name>/closed/` のように予約外の綴りのディレクトリに md を置くと、迷子 `?` として
   一覧に出る (消えない)

## 結果

(確認したら書いて done へ)

## 2026-09-10: 6 項目のうち **5 つは機械で覆われていた**（人が要るのは 1 つだけ）

`tmux-probe-requires-socket-isolation.md` の「human に回す前に機械で測れないか一度問う」を
この issue にも当てた。**テスト名から推測せず、本文を読んで assert を確認し、実走している**
（`go test . -run 'Issues|Epic|Group|NumberFilter'` = `ok glogx 2.095s` /
`glogx/issues` = `ok 0.338s`）。

| 項目 | 覆っているテスト | assert の中身 |
|---|---|---|
| 1. 親行の展開 / 折り畳み | `TestIssuesViewGroupsUseSeparateDisplayRowsAndToggle`<br>(`issues_group_view_test.go:42`) | `▸ alpha (1)` / `▸ beta (2)` が出て子が隠れる → `enter` で展開（`displayRows` 3→4、子が親の直下）→ `space` で折り畳み。`TestIssuesViewAutoExpandedGroupStillToggles` / `TestIssuesViewGroupChildrenAreIndentedUnderParent` も併走 |
| 2. group 内 `n` が group 内 `next/` へ | `TestEpicChildClaimPlacesSymlinkInsideGroup`<br>(`issues/nextlink_test.go:225`)<br>`TestMoveToSubdirKeepsEpicIssueInsideGroup`（`move_test.go:83`） | group 内 `next/` に symlink が置かれ、global の `issues/next/` へ行かない |
| 3. 番号 filter を Esc したとき添字が残らない | `TestIssuesViewClearingNumberFilterDropsStaleCollapsedGroups`<br>(`:203`)<br>`TestIssuesViewClearNumberFilterOnGroupRowReanchorsGroup`（`:479`）<br>`TestIssuesNumberFilterEscUnfiltersBeforeClosing`（`issues_number_filter_test.go:113`） | `/`→数字→`enter`→`space`→検索語を変えて `esc`→戻す、まで実キー列で回して `collapsedGroups` / `displayRows` を見ている |
| 4. 既定の状態でも epic の `done/` `pending/` の子が見える | `TestIssuesViewGroupParentShowsDoneCount`<br>(`:588`、後半) | `filter == FilterOpen` を前提に固定したうえで `enter` で展開し、**`707 ✓` `706 ✓` `708 ⏸` `709 ▶` `710 ○` が全部見える**ことを assert。かつ `global-done` は見えないこと（epic だけの例外）も同じテストで固定 |
| 5. 親行が `▸ <name> (5 ✓2)` の形 | 同上（前半）+ `TestIssuesViewGroupHeadRowShowsDoneCount`（`:628`）+ `TestIssuesViewGroupProgressCountsOnlyVisibleChildren`（`:752`） | `▸ alpha (5 ✓2)` と、done 0 件の group は従来の `▸ beta (1)` のまま |
| 6. 予約外の綴りの dir は迷子 `?` として消えない | `TestEpicChildStatusReachedByAllThreeConsumers`<br>(`issues_epic_status_dirs_test.go:20`) | `next/done/pending/DONE/closed/completed/hold/archive` を**受ける名前と受けない名前を混ぜて**回し、どちらでも走査が 1 件を返す（= 消えない）ことを assert。受けない名前は迷子として読む |

### 🚨 人でないと判定できないのは **項目 5 の「見え方」だけ**

本文の「**半角と全角が同じ桁に並んでいないか、実際の幅で目視する**」は、
`no-mixed-width-columns-in-terminal-ui.md` が「**幅を数えるテストでは検出できず、人が見るまで
分からない**」と明言している種類なので、機械では閉じられない。上の表の項目 5 が覆っているのは
**文字列としての書式**（`(5 ✓2)`）までで、`▸`（U+25B8）と `✓`（U+2713）が実端末で何桁を占め、
親行と子行が縦に揃って見えるかは覆っていない。

### 残っている確認（これだけ）

- [ ] obaket で glogx の issues view を開き、**epic の親行と子行が縦に揃って見えるか**を目視する
      （`▸ <name> (5 ✓2)` の行と、その下の `NNN ✓ / NNN ⏸` の子行）。ズレて見えたら
      `no-mixed-width-columns-in-terminal-ui.md` に従って**空白で埋めず全角文字へ置き換える**

項目 1 / 2 / 3 / 4 / 6 は上のテストが証拠なので、**目視の対象から外してよい**。
