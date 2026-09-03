# glogx: issues viewer の行が幅 1-2 で枠を突き破る (既存バグ・テストの掃き始めが 20)

起票日: 2026-08-14
種別: bug
優先度: **P3** (幅 1-2 の端末は現実に無い。テストの穴の方が本体)

## 観測した事実

050 の敵対的レビューで、`issuesView.rowLine` が返す行の表示幅を **width 1..1000** で
全数チェックしたところ、**width = 1 と 2 でのみ**枠を超える行が出る。

- ASCII タイトル・CJK タイトルの両方で再現
- **050 の変更前後でバイト単位に同一の失敗集合** = 本変更が持ち込んだものではない
- R1 が独立に同じ結論 (「枠越えするのは幅 1-2 のみで、HEAD と完全に同一集合」)

原因は `rowLine` の `titleW := max(width-fixed, 4)` の下限 4 で、固定部分
(溝 2 + 番号 3 + バッジ + カテゴリ 9 + 空白) だけで width を超える幅では
下限が効いて幅を超える。末尾で `clipToWidth(..., width)` を通すので通常は吸収されるが、
極小幅では固定部分自体が入り切らない。

## 本体はテストの穴

`issues_view_test.go` の `TestIssuesViewLinesAlwaysExactlyPageRows` は
**width 20 から掃いている**ので、幅 1-19 は元々未検査領域。

同型の穴が status viewer 側にもある可能性 (未確認)。

## 対応方針 (案)

1. 極小幅で「固定部分を削る」方針を決める (番号だけ / バッジだけ を出す等)。
   🚨 見た目の判断が要るのでユーザー確認が要る
2. あるいは「幅 N 未満は 1 行の省略表示にする」で一律に畳む
3. テスト側は掃き始めを 1 に下げる (方針を決めてから。今下げると red になる)

## 未確認

- 幅 1-2 の端末が現実に起こるか (tmux の極端な分割 / リサイズの一瞬)。
  **再現条件を実機で作っていない**ので、実害があるかは不明。優先度 P3 の根拠がここ
- status viewer / 一覧 (browseModel) 側に同型の穴があるか

## 関連

- `issues/done/050-perf-glogx-issue-list-reads-full-body.md` (この観測が出た経緯)

## 対応記録 (2026-08-15)

真因は issue の推測 (titleW の下限 4) では**なく**、`clipToWidth` の契約だった。全経路が
最後に clipToWidth を通しているのに溢れたのは、`width <= 0` で素通し (そのまま返す) して
いたため。`rowLine` は `o.width - scrollbarColumnWidth` を受けるので、幅 1-2 では負になり
最終 clip が無効化されていた (tui.go の `max(w-固定, 0)` ガードも同じ理由で無意味だった)。

修正 (3 点。いずれも「組んだ後に必ず切る / 入らないものは描かない」の構造側):

- `clipToWidth` / `clipMeasure`: width <= 0 は空を返す契約に変更 (0 = 無制限として使う
  呼び出しは repo に無いことを 33 箇所の列挙で確認)
- `tabLine`: フィルタバッジを clip 後に後置していたので、合成後に最終 clip を通す
- `scrollbarColumn`: contentW を 1 に床上げしていたのをやめ、バー列が入らない幅では
  バーを描かず本文だけを clip する

テスト:

- `TestIssuesViewLinesAlwaysExactlyPageRows` の幅掃きを {20,40,80} → **1..80 全数**へ
  (広げた時点で scrollbarColumn の床上げが 3 件目の漏れとして検出された)
- status viewer 側にも同型 sweep を新設 (`TestStatusViewLinesFitWidthDownToOne`)。
  status / 本文モードは幅 1..120 の probe で溢れ 0 (共有関数の修正で同時に閉じた)
- 変異検証: clipToWidth を素通しに戻すと width=1 で「→ 030 ○ feat      a」(19 セル) が
  枠を破り red になることを実測

「未確認」のうち status / 一覧側の同型: status は上記 sweep で固定。幅 1-2 の実機再現は
していない (修正が全数掃きで機械的に検証できたため不要と判断)。
