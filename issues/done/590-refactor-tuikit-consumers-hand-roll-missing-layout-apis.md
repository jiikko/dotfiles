# 590 (refactor): tuikit に無い部品を消費者が手書きしている (右寄せ重ね・板の内側の幅・折り返し)


起票日: 2026-10-01

> **2026-10-01 にユーザーの指示で pending から再開。** 以下は凍結したときの記録: **pending (2026-10-01 の見直しで凍結)**: A・B・C どれも今は壊れていない。
> - A (右寄せの重ね)・B (板の内側の幅): 害が出るのは 4 つ目の呼び出し側が端の扱いや `-1` を忘れたときだけ。
>   **trigger**: 右寄せの板、または `Panel` の幅から中身の幅を出す呼び出しを 4 か所目として足すとき (その作業の中で寄せる)
> - C (折り返しのキーキャップ): 幅を超えるのは実測どおりだが、害は行末が `…` で欠けるだけで枠は壊れない。キーキャップが現れたのは
>   幅の不具合そのものを論じた PG の応答 3 件だけ。10 か所を寄せるコストに見合わないので格下げ。
>   **trigger**: キーキャップ以外の入力でも折り返しの幅超えが見つかったとき / 行末が欠けて読めないという報告が出たとき
> - markdown の `clipToWidth` → `termwidth.Clip` はほぼ無料なので、markdown を次に触るときについでに置き換える

## 概要

tuikit の公開 API が欠けているところを、消費者 (glogx / pro-con) と tuikit 自身の examples が
それぞれ手で組んでいる。3 つの形があり、どれも「同じ手順の複製が、片方で直した端の扱いを持たない」形になっている。
そのうち折り返し (C) は、キーキャップ入りの文で実際に幅を超える行を作ることを実測した。

## 詳細

### A. 右寄せの重ね (overlay) が 3 か所に手書き

tuikit/layout にあるのは `Overlay` と `OverlayCentered` だけで、右寄せが無い。

| 実装 | 箱が窓幅以上のとき |
|---|---|
| `src/glogx/usage_overlay.go` の `overlayBoxRight` (`overlayBoxTopRight` / `overlayBoxBottomRight` が委譲) | `clipToWidth` で箱を切る |
| `src/pro-con/ui/toast.go` の `(*Model).overlayToast` | 保護なし (`keep=0` で箱をそのまま連結) |
| `src/tuikit/examples/toast/main.go` の `overlayBottomRight` (コメント「glogx の overlayBoxRight と同じ形」) | 保護なし |

どれも「箱の幅を測る → 左を切る → reset → 空白で埋める → 箱」の同じ手順。

- 発火条件: pro-con と examples は保護が無いが、どちらも `BoxLines(…, 窓幅)` で箱の幅を窓幅に抑えているので、今は壊れない。
  新しい消費者が右寄せの板を足すとコピーが 4 つ目になり、glogx で入れた端の扱いが届かない。silent
- 母集合: `grep -rn 'func overlay\|func.*BottomRight' src | grep -v _test` で右寄せは上の 3 実装
  (glogx の `overlayBox` / `overlayCenteredBox` は tuikit の `Overlay` / `OverlayCentered` への委譲)

### B. 板 (`layout.Panel`) の内側の幅を消費者が `-1` 込みで再計算

`layout.Panel(title, rows, width, …)` の `width` は右影 1 桁を含むが、`layout.PanelInnerWidth(frameWidth)` の引数は
**影を除いた**枠の幅。「Panel に渡す幅から中身の幅を出す」関数が無いので、呼び出し側が毎回 `-1` と最小幅の押し上げを知っている:

- `src/glogx/box.go` の `withScrollbar`: `PanelInnerWidth(max(boxWidth, PanelMinWidth)-1)`
- `src/glogx/zoom.go`: `PanelInnerWidth(boxW-1)` (`boxW` は手前で `PanelMinWidth` 以上に押し上げ済み)
- `src/pro-con/ui/legend.go` の `legendSize`: `PanelInnerWidth(width - 1)` (直前のコメントに「1 桁広く折り返すと行末が … で切れる」と、踏んだ跡がある)

- 母集合: `grep -rn 'PanelInnerWidth' src | grep -v _test | grep -v '^src/tuikit/'` で上の 3 か所
- 発火条件: 影の幅・最小幅を tuikit 側で変える、または 4 つ目の呼び出し側を足して `-1` を忘れる。
  `Panel` は行を `ClipMeasure` で黙って切るので、スクロールバーの列や行末が消えてもコンパイルもテストも通る

### C. 折り返しの道具が無く、消費者が `ansi.Hardwrap` / `ansi.Wrap` を直接呼ぶ

README は「幅は `termwidth` だけで測る」と約束しているが、折り返しは tuikit に公開の関数が無い
(tuikit 内の `toast.wrapText` / `cutLine`、`markdown.wrapSpans` はどれも非公開)。消費者は x/ansi を直接呼んでいる:

- 母集合: `grep -rn 'ansi\.Hardwrap\|ansi\.Wrap\|ansi\.Wordwrap' src | grep '\.go:' | grep -v _test.go` で
  production 10 呼び出し (pro-con 9: `drawer.go` 2・`legend.go` 1・`sendconfirm.go` 2・`attachguide.go` 1・
  `answerform.go` 3 / glogx 1: `render.go` の `wrapToWidth`)

**実測 (2026-10-01、charmbracelet/x/ansi は tuikit の go.mod の版)**: 7 種の入力を幅 4〜12 で折り、各行を
`termwidth.Of` と `ansi.StringWidth` で測った。

| 入力 | Hardwrap: 幅超え行 | Wrap: 幅超え行 / クラスタを割った行 |
|---|---|---|
| `1️⃣2️⃣3️⃣4️⃣5️⃣6️⃣` (キーキャップ) | 8 | 8 / 2 |
| `#️⃣*️⃣#️⃣*️⃣#️⃣` (キーキャップ) | 6 | 6 / 1 |
| `a⚠️b⚠️c⚠️d⚠️e` / `✔️…` / `a☺️b…` (VS16) / `👍🏽…` (肌色) / 全角 | 0 | 0 / 0 |

- キーキャップを含む行は、`ansi` 自身の幅でも指定幅を超える (`termwidth.Of` と `ansi.StringWidth` の値は一致)。
  つまり x/ansi の折り返しは、キーキャップの幅を測る規則と折る規則が食い違っている
- 発火条件: PG の応答・カードの本文・質問文に `1️⃣` のようなキーキャップの列が入り、pro-con の drawer / 回答フォーム /
  送信確認で折り返す。行は `Panel` の `ClipMeasure` で切られるので枠は壊れないが、**行末が `…` で黙って欠ける**。
  `ansi.Wrap` ではキーキャップの途中で行が割れ、行頭に `️⃣` (VS16 + U+20E3) だけが残る行も出る
- 実機の pro-con 画面での確認は未実施 (上の値は関数の出力の実測)
- `src/glogx/render.go` の `wrapToWidth` のコメントは「ansi.Hardwrap は grapheme クラスタ単位で折る」として採用した経緯を書くが、
  見ていたのは ⚠️ (VS16) だけで、キーキャップは例外。寄せるときにこのコメントも直す

## 対応方針

- A: `layout.OverlayRight(window, box, width, colored, baseRow)` を足し、3 か所を寄せる。箱が窓幅以上のときの契約を
  決める (glogx は箱を切る。tuikit の `OverlayCentered` は箱を切らず `leftGap=0` で重ねるだけなので、
  そちらも揃えるかを同時に決める)
- B: `layout.PanelContentWidth(panelWidth int) int` を足し (最小幅の押し上げと影の 1 桁を内部で引く)、3 か所を置き換える。
  `PanelInnerWidth` を外へ出し続ける必要があるかも見直す
- C: `termwidth.Wrap(s, width)` (クラスタは `FirstCluster` で切り、各行の幅は `termwidth.Of` で保証する) を足し、
  消費者の 10 呼び出しを寄せる。toast の `cutLine` を土台にできるかを先に見る。回帰テストはキーキャップの列を入れ、
  全行が `termwidth.Of(l) <= width` で、連結すると元に戻ることを見る。x/ansi に戻す変異で red を確かめる
- 寄せた後、消費者が `ansi.Hardwrap` / `ansi.Wrap` を直接呼ぶ形を depguard / forbidigo で止められるか検討する
  (今の forbidigo は tuikit の中の `ansi.StringWidthWc` 等だけを禁じていて、消費者の wrap は網の外)
- 同じ「同じ契約の複製」として、`src/tuikit/markdown/render.go` の `clipToWidth` は `termwidth.Clip` と本体が逐語で同じ
  (幅 0 以下 → 空 / byte 長の fast-path / `Of` / `Truncate(…, "…")`)。markdown は既に termwidth を import しているので、
  `termwidth.Clip` への置き換えで消える (呼び出しは `markdown/render.go` の 1 か所。`markdown/links.go` のリンク桁の計算が
  コメントでこの切り詰めの挙動を前提にしているので、置き換えたら links のテストも回す)。この issue の作業に含める

## 関連ファイル

- `src/tuikit/layout/panel.go` (`Panel` / `PanelInnerWidth` / `PanelMinWidth` / `PanelChrome`)
- `src/tuikit/layout/` の `Overlay` / `OverlayCentered`
- `src/tuikit/toast/toast.go` (`wrapText` / `cutLine`)
- `src/tuikit/markdown/render.go` (`clipToWidth`)
- `src/glogx/usage_overlay.go` / `box.go` / `zoom.go` / `render.go`
- `src/pro-con/ui/toast.go` / `legend.go` / `drawer.go` / `sendconfirm.go` / `attachguide.go` / `answerform.go`

## 進捗

- [x] A: `layout.OverlayRight` を足し、glogx の `overlayBoxRight`・pro-con の `overlayToast`・examples の `overlayBottomRight` を寄せた。
  箱が窓幅以上の行は箱を窓の幅で切る (glogx の契約。pro-con と examples は切らずに連結していた)。`OverlayCentered` は今回は変えていない
- [x] B: `layout.PanelContentWidth(width)` (= `PanelInnerWidth(max(width, PanelMinWidth) - 1)`) を足し、glogx の `withScrollbar`・`zoom`、
  pro-con の `legendSize`、toast の `textWidth` を置き換えた。legend と zoom は width が `PanelMinWidth` 未満のときだけ値が変わる
  (旧は 0 以下になりえた。新は Panel の実際の押し上げと一致する)
- [x] C: `termwidth.Wrap(s, 幅, trimSpace)` / `WordWrap(s, 幅)` を足し、x/ansi を直接呼んでいた 10 か所を寄せた
  (`Hardwrap(x, w, true)` → `Wrap(x, w, false)`、glogx の `Hardwrap(x, w, false)` → `Wrap(x, w, true)`、`ansi.Wrap(x, w, "")` → `WordWrap(x, w)`)。
  glogx と pro-con の `.golangci.yml` の forbidigo に `ansi.(Hardwrap|Wrap|Wordwrap)` を足して戻りを止めた
  - 色は次の行で張り直さない (x/ansi の Hardwrap と同じ)。ESC 列の長さは x/ansi のパーサ (`DecodeSequence`) に読ませ、見える字は `FirstCluster` で測る
  - 速度 (幅 80、20 回の平均): 平文 2 KB で ansi.Hardwrap 16 µs / Wrap 26 µs、20 KB で 146 µs / 272 µs、色付き 100 KB で 538 µs / 926 µs (線形。x/ansi の約 1.7 倍)
- [x] markdown の `clipToWidth` を `termwidth.Clip` に置き換え、出口で切る理由のコメントは呼び出し箇所へ移した
- [x] README のパッケージ表に `Wrap` / `WordWrap` / `PanelContentWidth` / `OverlayRight` を足した
- [x] 回帰テスト: `TestOverlayRight` / `TestPanelContentWidthMatchesPanel` (正解役は Panel 自身) / `TestWrapProperties` (乱数 2 万件: 各行の幅・書記素の途中で始まらない・
  trim なしならつなげ直すと入力と 1 byte も違わない) / `TestWrapKeycapsStayWithinWidth` / `TestWrapContract` / `TestWrapTerminatesOnZeroWidthHeads` / `TestWrapOutputStaysProportional`
- [x] 変異 (mutate-verify) 12 本がどれも red: OverlayRight の切り詰めを外す / PanelContentWidth の影の 1 桁を外す / Wrap を ansi.Hardwrap に戻す / 空の行を足さない処理を外す /
  glogx と pro-con に ansi の折り返しを書く (lint が止める) / ESC だけの列で進まない / reset で張り直しの状態を消さない (1 回目の実装) /
  残りの ESC 列を捨てる / ESC 列を 1 byte ずつ読む / 進まない形 (先に走る乱数のテストが `go test -timeout` で落ちる) / 行ごとに SGR を張り直す
- [x] 敵対的レビュー 3 周 (sonnet、read-only):
  - 1 周目: P1 2 件 (先頭が幅 0 の字で無限ループ / Cut + DropColumns の繰り返しで 2 乗。色付き 20 KB で x/ansi の 100 倍) → 1 回の走査に書き直した。
    OverlayRight と PanelContentWidth は壊せなかった
  - 2 周目: 自前の ESC の読み方が Of と食い違う (閉じていない OSC が後ろを飲む・中間バイト付きの列が割れる)、SGR の張り直しが部分的な解除で
    積み上がる (100 KB → 68 MB)、空白だけの残りの ESC 列を捨てる → 近似の手直しをやめ、x/ansi のパーサに読ませ、張り直しをやめた
  - 3 周目: 残ったのは x/ansi 自身の不整合 1 件 (閉じていない APC / SOS / PM を `DecodeSequence` は幅 0、`StringWidth` は中身の幅で数える)。
    無害化していない壊れた端末出力でしか起きず、消費者の入力は termsafe を通るので直さない。判断と再提起の条件は `termwidth/wrap.go` のコメント
- 検証: `make -C src/tuikit lint` / `make -C src/pro-con lint` / `make -C src/glogx lint` 0 件、glogx と pro-con のテスト一式 green
- 未確認: 実機の画面での見え方 (pro-con の引き出し・回答フォーム、glogx の詳細の折り返し)。テストと probe の出力でしか見ていない
