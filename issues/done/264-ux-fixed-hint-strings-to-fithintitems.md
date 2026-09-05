# 264 ux: 固定文字列の hint 3 本 (issues 本文 / commit diff / status pager) を fitHintItems 方式へ寄せる

起票日: 2026-09-05
カテゴリ: ux
優先度: 中 (代表幅の端末では今は切れていない。端末が代表値より狭いときだけ「出口のキー」が消える)
出典: 2026-09-05 の J/K 横展開 (commit 5d8bcdfa / 1929f64b / 309b9dc7) のセルフレビューのぼやき

## 何が起きるか

hint 行は `browseModel.hintLineText` が `hintWidth()` で末尾から黙って切る。**固定文字列**で
組んでいる hint は、端末が想定より狭いと**並びの最後 = 抜ける手段**が先に消える
(issue 155 が status viewer で実測し、issue 201 の監査で doctor / job パネル / detailOv を
`fitHintItems` へ寄せた経緯がある)。

J/K を入れた 2026-09-05 に、固定文字列で残っている 3 本を**予算ぎりぎりまで詰めた**:

| hint | 場所 | 幅 (桁) | 予算 (testHintBudget) |
|---|---|---|---|
| issues 本文 | `issues_view.go` `hint()` の `v.open != nil` 分岐 | 84 | 89 |
| commit diff | `tui.go` `hintLine()` の `m.diffOv.visible()` 分岐 | 84 | 89 |
| status pager | `status_view.go` `hint()` の `v.pagerKey != ""` 分岐 | 84 | 89 |

余裕は 5 桁。**端末幅が代表値 (content 84 桁) より 6 桁以上狭いと、3 本とも末尾の
`Enter/h/q: 戻る` / `q/h: 閉じる` / `d/q: 閉じる` が切られる**。次に 1 キー足すときは何かを
落とすか語を詰めるかの判断が毎回要る (issues 本文は今回 `g/G: 先頭/末尾` → `端`、
`一覧へ` → `戻る` に詰めて入れた)。

## 直し方

3 本を `fitHintItems(width, []hintItem{...})` (`status_view.go`) で組む。優先度 1 に「抜ける手段」、
以降は使用頻度順。status の一覧 hint と job パネル / detailOv が既にこの形なので、それに倣う。

- **issues viewer**: `issuesView.hint()` は幅を受け取っていない (`statusView.hint(width)` は受け取る)。
  `hint(width int)` に変え、`tui.go` の `hintLine()` から `m.hintWidth()` を渡す。一覧モードの hint
  (`a` の巡回で文言が変わる 3 段 / 選択中 / 絞り込み中 / URL ピッカー) も同じ経路に載せるか、
  本文だけ先に寄せるかは着手時に決める (一覧 hint も 84 桁ぎりぎりのモードがあり、
  `issues_view.go` の hint のコメントに「`i: 一覧へ` は入れられない」と書かれているのは同じ制約)
- **commit diff / status pager**: 固定文字列を項目表に置き換えるだけ

## 巻き込まれるテスト

- `TestIssuesViewBodyHintKeysAllRespond` / `advertisedHintKeys` は **hint 文字列を parse して
  案内キーの集合を取り出し、全部が効くことを確かめる**。fitHintItems にすると案内される集合が
  幅で変わるので、parse は `testHintBudget(t)` の幅で行い、「その幅で案内された集合 = 検証表」
  の一致を見る形にする (狭い幅で落ちた項目は検証表から外れるのではなく、案内されなくなるだけ)
- `TestIssuesViewHintFitsPopupWidth` / `TestHintsFitPopupWidth` は fitHintItems に移した時点で
  「収まる」が構造で保証されるので、残すなら「優先度 1 の抜ける手段が狭い幅でも残る」を見る
  assert に置き換える (hint_width_test の doctor の項と同型)
- `TestStatusHintUsesRenderBudget` (幅を frameMinWidth〜140 で掃く) の issues 版・diff 版を足すと、
  組む側と切る側の予算ずれを検出できる

## やらないこと

- 予算そのものを広げる (frame の余白を削る等)。hint は端末幅に従うもので、代表値に合わせて
  余白を動かすのは逆
- 3 本を今より短く詰める。語を削るほど案内が読めなくなる (`J/K: 隣へ` が既に限界)

## 関連

- `docs/glogx-ui-guide.md` §5 (案内の規律: 抜ける手段は必ず残す) / §6 (J/K)
- `issues/done/155-*` (status viewer の hint が切れていた実測) / `issues/done/201-*` (監査で 3 箇所を
  fitHintItems へ寄せた)
- `src/glogx/tui_helpers_test.go` `testHintBudget` (予算の正本。2026-09-05 に一本化)

## 決着 (2026-09-05)

- 3 本とも `fitHintItems` へ寄せた。issues viewer は `hint(width int)` に変え、`hintLine()` が
  `m.hintWidth()` を渡す。**本文だけでなく一覧の全モード (open/pending/all の巡回 / 選択中 /
  絞り込み中 / 番号入力中 / URL ピッカー) も同じ形にした** (幅を受け取るようにした時点で
  固定文字列を残す理由が無くなった)。文言は変えていない
- 🚨 優先度 1 は「抜ける手段」**だけ**。ラベル (`N 件選択` / `数字で絞り込み`) を 1 に置くと、
  同優先度は左から採るので極端な幅で出口の方が落ちる (実装中に sweep テストが検出)
- テスト: `TestIssuesViewHintFitsPopupWidth` を「予算幅で全項目が入る + 出口の幅から予算まで
  掃いて出口が残る」に置き換え、`TestIssuesHintUsesRenderBudget` / `TestDiffHintUsesRenderBudget`
  (組む側と切る側の予算ずれを幅 60〜140 で掃く) を足した。`advertisedHintKeys` は
  `testHintBudget` の幅で parse する (予算幅では全項目が入るので案内される集合は以前と同じ)
- 変異検証 (いずれも red → 復元): 本文の出口の優先度を 5 に下げる / 組む側の予算を +10 ずらす /
  diff の hint を固定文字列に戻す
