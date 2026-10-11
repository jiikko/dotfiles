# 713 (research): bubbletea v2.1.0 (と x/ansi v0.11.9・ultraviolet) へ上げる余地

起票日: 2026-10-11

> 凍結 (pending)。再開の条件: go 行を 1.26 へ上げる判断をしたとき / tmux の popup の resize で消し残しが出たとき /
> v2.1.x の修正版が出て公開から 2 週間以上経ったとき (詳細は「再開の条件」)

## 概要

ユーザーの問い (2026-10-11): bubbletea のバージョンを上げる余地は無いか。

repo の 6 module (glogx / pro-con / treefiler / tuikit / schedkeys / restartable) は `charm.land/bubbletea/v2` v2.0.8・
`github.com/charmbracelet/x/ansi` v0.11.7・ultraviolet `v0.0.0-20260803092147-8b693049ce2a` を使う。出ている版は v2.0.9・v2.0.10
(2026-09-24)・v2.1.0 (2026-10-08)、x/ansi v0.11.9、ultraviolet `v0.0.0-20261008173134-6b8d4baf91b4`。

## 結論 (2026-10-11): 今は上げない

モジュールキャッシュの v2.0.8 と v2.1.0 (と ultraviolet・x/ansi) を diff した結果 (sonnet の読み取り専用の調査を検閲。幅の値は
調査役が `go run` で実測した):

1. **go 1.26.0 を要求する**: bubbletea v2.1.0 と新しい ultraviolet の go.mod は `go 1.26.0`。6 module の go 行は `go 1.25.0` で、
   `docs/glogx-bubbletea-v2.md` の「Go ツールチェーンは 1.26 へ上げない」と衝突する (上げると 6 module の go 行が 1.26.0 になる)。
   CI は `.github/go-version` = 1.26.x なので CI だけなら動く
2. **幅モデルの食い違いが新しく出る**: x/ansi v0.11.9 で WcWidth の計算が rune ごとの幅の合計に変わった (`wcwidth.go` /
   `FirstGraphemeCluster` の `wcClusterWidth`)。エンジンの既定は WcWidth のまま (`tea.go` の `setWidthMethod` は mode 2027 で
   GraphemeWidth へ上がるだけ) なので、glogx の `dispWidth` (Grapheme) と次が食い違う (Wc / Grapheme):
   - 👨‍👩‍👧‍👦 (ZWJ): v0.11.7 は 2/2 → v0.11.9 は **8/2**
   - 👍🏽 (肌色): 2/2 → **4/2**
   - 🇯🇵 (国旗): 1/2 → 2/2 (こちらは揃う)
   - keycap・VS16・ambiguous・かな・é は変わらない
   `docs/glogx-bubbletea-v2.md` の幅モデルの表 (「ZWJ 絵文字と肌色は 2/2 で一致」) が崩れ、ZWJ・肌色を含む行の右枠がずれうる
3. **日本語の行の描画が重くなりうる (未実測)**: ultraviolet の `terminal_renderer.go` に `paintLine` / `lineHasDrift` / `repaintLine` が
   入り、wide セル (日本語・絵文字) を含む行は差分描画せず行ごと描き直す。端末が mode 2027 を報告しないと autowrap の切り替えと
   カーソルの絶対位置も足す。glogx / pro-con / treefiler は日本語の行が多い
4. 速くなる側もある (未実測): x/ansi の `StringWidth` が WcWidth のとき書記素の分割を省く、ultraviolet の `Render` が行データを
   使い回す (毎フレームの確保が減る)。resize の後の消し残し (`pendingErase`)・縮めたときの古い行の修正も入った
5. 公開 API の削除・型の変更は見当たらない。`tea.Exec` / `ReleaseTerminal` / `RestoreTerminal` の手順は変わらない
   (`tuikit/foreground` の取り戻し (issue 707) はそのまま要る)。キーボード拡張の列が `CSI = flags ; 1 u` から push / pop
   (`CSI > flags u` / `CSI < 1 u`) に変わった。tmux・Terminal.app での扱いは未確認

上げる価値 (4・5 の修正) に対して、1 (toolchain の方針)・2 (幅の表の崩れ)・3 (日本語の行の描画) の確認が要り、2・3 と
キーボード拡張は実 TTY での目視が要る (`docs/glogx-bubbletea-v2.md` のチェックリスト 4。エージェントの環境からは測れない)。

## 再開の条件 (trigger)

- go 行を 1.26 へ上げる判断をしたとき (1 の衝突が消える)
- tmux の popup の resize で消し残し・古い行が残る不具合が出たとき (5 の修正が効く見込み)
- v2.1.0 の公開から 2 週間以上経ち、v2.1.x の修正版が出たとき

上げるときの手順は `docs/glogx-bubbletea-v2.md` のチェックリスト。加えて:
- ZWJ・肌色を含む行を実 TUI で描かせ、右枠の位置を ASCII 行と比べる (2)。`termwidth` の表と docs の表を測り直す
- pro-con `BenchmarkFrame*Rendered` / glogx `BenchmarkCursorMoveViewJARendered` を上げる前後で比べる (3。日本語の行)
- `src/tuikit/framebench` の冒頭の「v2.0.8 の cursedRenderer.flush を再現」を新しい flush (`pendingErase`・`viewEquals`) と読み比べる
- 6 module の版の一致は `tests/scripts/test_tuikit_consumers_aligned.sh` が見る (ultraviolet を直接 require するのは tuikit だけ)

## 未確認

- v2.0.9 / v2.0.10 だけに入った修正 (v2.0.8 と v2.1.0 の差分しか見ていない)
- 実 TUI での描画のバイト数の増減、tmux 3.7b での `CSI > u`

## 関連ファイル

- `docs/glogx-bubbletea-v2.md` (幅モデル・上げるときのチェックリスト・1.26 へ上げない理由)
- `src/tuikit/framebench/framebench.go` / `tests/scripts/test_tuikit_consumers_aligned.sh`

## 進捗

- 2026-10-11: 起票。調査の結論は「今は上げない」(上の節)
