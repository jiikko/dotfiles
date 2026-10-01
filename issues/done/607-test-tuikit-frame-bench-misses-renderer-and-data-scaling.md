# 607 (test): フレームのベンチが View の文字列までしか測らず、レンダラの分 (View の約 2.3 倍) とデータ量に対する伸びが見えない


起票日: 2026-10-02

出典: [605](605-research-tuikit-perf-breakthrough-2026-10-02.md) の実測 B / C

## 概要

glogx・pro-con のフレームのベンチ (`BenchmarkView*` / `BenchmarkFrame*` / `BenchmarkDiffBoardView`) はどれも `m.View()` を回すだけ。
2 つの量が見えていない:

1. **レンダラ**: bubbletea v2.0.8 は View の文字列が前と違うたびに、画面全体を `uv.NewStyledString(content).Draw` でセルへ書き直し、
   `TerminalRenderer.Render` で差分を取る (`cursed_renderer.go` の `flush`)。pro-con の 200 × 50 で **約 0.5 ms/コマ = View (約 0.2 ms) の約 2.3 倍**
2. **データ量に対する伸び**: ベンチの盤面は件数が固定。[606](606-perf-pro-con-drawer-rerenders-all-activity-markdown-per-frame.md) (詳細の活動を View のたびに全件整形。上限 1000 件で約 7.5 ms) は、
   件数を振るベンチが無かったので見えていなかった。268 / 270 / 274 / 275 / 591 (どれも done) も同じ形 (フレームの中でデータ量に比例する走査) だった

## 実測 (2026-10-02。605 の表 B を参照)

- 揺れ: Draw だけ 341〜359 µs、Draw + 差分 + Flush 486〜497 µs。枠の滑走 539〜550 µs。静止 457〜481 µs
- 使い捨てのベンチ (pro-con/ui に置いて回した。commit していない):

```go
type countW struct{ n int }
func (c *countW) Write(p []byte) (int, error) { c.n += len(p); return len(p), nil }

// frames は演出の所要の中を frameInterval ずつ進めた View().Content を先に作っておく (View の時間を混ぜない)
func runRenderer(b *testing.B, frames []string) {
	w := &countW{}
	scr := uv.NewTerminalRenderer(w, nil)
	scr.SetFullscreen(true)
	scr.SetRelativeCursor(false)
	buf := uv.NewScreenBuffer(200, 50)
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		buf.Clear() // bubbletea の flush と同じ順
		uv.NewStyledString(frames[i%len(frames)]).Draw(buf, buf.Bounds())
		scr.Render(buf.RenderBuffer)
		_ = scr.Flush()
		i++
	}
	b.ReportMetric(float64(w.n)/float64(i), "termB/op")
}
```

🚨 bubbletea は View の文字列が前と同じなら flush を飛ばす (`viewEquals`)。上の「静止」は同じ文字列を毎回 Draw し直しているので、実際の静止画面より重く出る。
ベンチに置くなら、同じ文字列のコマは数えない形にする。

🚨 上のコードの `uv.NewTerminalRenderer(w, nil)` は `colorprofile.Detect` で書き込み先 (tty でない) と空の env から NoTTY を選び、色を全部捨てる
(ultraviolet `terminal_renderer.go` の `NewTerminalRenderer`、uv `cell.go` の `ConvertStyle`。反証レビューで判明)。そのため 605 の表 B の
「出るバイト」と「Draw + 差分 + Flush」は SGR 抜きの値で、本物より軽い (Draw だけの値は影響しない)。置くときは bubbletea と同じく
`scr.SetColorProfile(colorprofile.TrueColor)` を呼んでから測る。

## 対応方針

- tuikit に、View の文字列を bubbletea と同じ手順でセル化 + 差分まで通す計測の補助を置く (消費者から import するので公開の package にする。`internal` や `_test.go` には置けない。
  フレームワーク非依存の層の約束と、bubbletea の import が `caret` だけという例外 (`src/tuikit/CLAUDE.md`) をどう扱うかを先に決める。
  ultraviolet だけを import する形なら bubbletea 本体には依存しない)。🚨 tuikit の go.mod の uv は `f5a850f9`、glogx・pro-con は `8b693049` で版が違う。
  tuikit 単体のテストで測ると消費者と別の版を測ることになるので、補助は tuikit に置いても、測るのは消費者のベンチからglogx・pro-con のフレームのベンチから呼び、`View` と `View+renderer` を並べて出す
- データ量を振るベンチを置く: pro-con の活動件数 (0 / 30 / 300 / 1000)、glogx の issue 数・本文の長さ・ログの件数。
  合否は時間でなく **件数を増やしたときの確保量の比** (`TestDiffBoardViewAllocDoesNotGrowWithLines` と同じ形。壁時計を合否にしない)
- `bubbletea` / `ultraviolet` を上げたら、この計測で前後を比べる (`docs/glogx-bubbletea-v2.md` の「次に上げるとき測り直すもの」へ 1 行足す)

## 受け入れ条件

- [x] glogx と pro-con のフレームのベンチで、レンダラ込みの値が出る
- [x] データ量を振るベンチ / 確保の比の検査が pro-con の詳細 (606。比の検査を置くと 606 の経路で赤くなるので、606 と同じ変更で直す) と glogx の主要な画面にある。比の検査は、件数に比例する整形を戻す変異で red になる
- [x] 入口のドキュメント (tuikit の README のベンチの節 / 消費者のベンチのコメント) に使い方を書く

## 関連ファイル

- `src/glogx/tui_bench_test.go`・`src/glogx/tui_perf_bench_test.go`・`src/pro-con/ui/frame_bench_test.go`・`src/pro-con/ui/diffview_perf_test.go`
- bubbletea v2.0.8 `cursed_renderer.go` (`flush`)、ultraviolet `styled.go` (`StyledString.Draw`)・`terminal_renderer.go` (`Render`)

## 進捗

- [x] 実装 (commit「perf(pro-con): 詳細の活動の整形を…(606)」と同じ commit。607 の道具は 606 の守りと同じ変更で入れた)

### 結果 (2026-10-02)

- `src/tuikit/framebench` (新設、公開の package): `NewRenderer(幅, 高さ)` / `Frame(content, 属性)` / `Written()`。bubbletea v2.0.8 の `cursedRenderer.flush` の
  alt screen の経路 (前のコマと content・属性が同じなら描かない → Clear → Draw → Render → Flush) を再現する。色のプロファイルは TrueColor に固定
  (NoTTY だと色を捨てる)。属性は `tea.View.Cursor` を `%+v` で比べる (カーソルだけ動くコマも描く。codex の設計レビュー P2)。
  再現していないもの (カーソルの移動・同期出力の囲み・端末モード) はパッケージのコメントに書いた。tuikit の go.mod は uv・colorprofile を direct にしただけで版は上げていない
- レンダラ込みのベンチ (この Mac、`-count 3`):

  | benchmark | View だけ | レンダラ込み | 端末へ出たバイト/コマ |
  |---|---|---|---|
  | pro-con FrameBump | 203〜218 µs | 397〜414 µs | 約 278 |
  | pro-con FrameMove | 82〜95 µs | 459 µs | 約 195 |
  | pro-con FrameGlide | 155〜162 µs | 843〜878 µs | 約 4.3 K |
  | glogx CursorMoveViewJA (j と k の 2 コマ) | 60 µs | 615〜625 µs | 約 182 |
  | glogx ViewWithDiffJA (開いた直後の 1 コマ = 全面の描き直し) | 33 µs | 821〜841 µs | — |

  glogx の j/k ではレンダラが View の約 10 倍。605 の表 B (NoTTY で色を捨てていた) より重い
- データ量の伸び: pro-con は 606 の守り (`TestDrawerActivityViewAllocDoesNotGrowWithItems` / `TestDrawerActivityRedeliverReusesLines`) と `BenchmarkDrawerActivity` (0 / 210 / 1000 件)。
  glogx は `TestListFramesAllocBytesDoNotScaleWithItems` (git log の一覧と issues の一覧、40 件と 2000 件で View の確保バイトの比 ≤ 1.3。実測は 30.8 / 30.9 KB、33.8 / 35.6 KB)
- 変異 (`bin/mutate-verify`。全部想定の検査で red): git log の View で毎回全行を組み直す (20.1 倍) / issues の一覧で見えない行も組む (9.6 倍) /
  framebench の TrueColor を外す (`TestFrameKeepsColors`) / 同じコマを飛ばさない・属性を比べない (`TestFrameSkipsOnlyIdenticalFrames`)
- 入口: tuikit README のパッケージ表と「フレームの重さを測る」節、tuikit/CLAUDE.md の地図、docs/glogx-bubbletea-v2.md のチェックリストの 6 番
- 実装の敵対的レビュー (codex、read-only): framebench と flush の突き合わせ・tuikit の go.mod の変更とも「壊せなかった」
- CI: 新しいテストは各 module の `make test` (`go test -race ./...`) で走る。レンダラ込みのベンチは CI の Bench には入れていない (glogx の `tests/glogx/bench_glogx.sh` は名前を完全一致で選ぶ)

### 残り

- 605 の候補 3 (レンダラが変わっていない行を読み飛ばす) は、この計測で実例が出たら再評価する (glogx の j/k でレンダラが View の約 10 倍)。trigger は 605
