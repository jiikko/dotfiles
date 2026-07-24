# 027 bug: glogx のテストが production と違う幅関数を使い、東アジア locale で 12 件落ちる

## 背景

`LANG=ja_JP.UTF-8` の手元環境で `go test ./src/glogx` が **12 件**恒常的に失敗する。CI は
green なので「環境依存の既知失敗」として扱われてきたが、実際は**テスト側のバグ**であり、
CI が green なのは locale が `C` で症状が隠れているだけ。

実害は 2 つ:

- 手元でテストが常に赤く、「自分の変更で壊したのか既知の赤か」を毎回 HEAD の worktree と
  失敗集合を diff して切り分ける必要がある (2026-07-25 のトースト改修で実際に 3 回やった)
- 該当アサーションは**描画エンジンが使う幅モデルを検証していない** (下記)。幅の回帰を
  捕まえる目的のテストが、production と別の物差しで測っている

## 根本原因 (実測で確定)

production は `dispWidth` = `ansi.StringWidth` に統一されている (`src/glogx/width.go:25`。
commit 8c6fe1e「幅計算を描画エンジンと同じ ansi.StringWidth に統一」)。
一方テストは `runewidth.StringWidth` でアサートしている。両者は罫線・ブロック文字で食い違う:

| 文字列 | `runewidth` | `ansi` |
|---|---|---|
| `┌──────┐` | 16 | 8 |
| `▖▁▁▁▁▗` | 10 | 6 |
| `█▓▒░` | 7 | 4 |
| `│ row │` | 9 | 7 |

`runewidth` は box-drawing / block を East Asian **ambiguous** として扱い、
`runewidth.DefaultCondition.EastAsianWidth` が true のとき幅 2 と数える。この値は locale から
初期化されるため:

- 手元 (`LANG=ja_JP.UTF-8`) → `EastAsianWidth=true` → 罫線が 2 桁 → 幅アサート失敗
- CI (`C` / `en_US`) → false → 1 桁 → 偶然 production と一致して green

**対照実験** (仮説の確定):

```sh
cd src/glogx
go test . -count=1                       # → 12 件 FAIL
LANG=C LC_ALL=C go test . -count=1       # → ok (全て通る)
```

⚠️ `-count=1` を付けないと go test のキャッシュで前回の PASS を拾い「0 件失敗」に見える
(調査中に踏んだ)。

## 既に一部だけ移行済み

`src/glogx/box_test.go` の `withScrollbar` の幅アサートだけは `dispWidth` に移行済みで、
理由もコメントで残っている (box_test.go:163-164):

```go
// 幅: バー列を足しても buildPanelBox の本文幅を超えない。幅は描画側と同じ dispWidth
// (ansi.StringWidth) で測る — … / █ は runewidth では 2 桁扱い (ambiguous) になり食い違う。
```

つまり commit 8c6fe1e の移行が**テスト側で midway で止まっている**。この 1 箇所が
「正しい直し方」の先例になっている。

## 対応方針

**テストの幅アサートを `runewidth.StringWidth` → `dispWidth` に置き換える** (production と
同じ物差しで測る)。`stripANSI` との併用箇所は `dispWidth` が ANSI を解釈するため
`dispWidth(l)` に単純化できる場合がある (要確認、機械的に消さない)。

対象ファイル (`runewidth` を import しているテスト):

- `src/glogx/box_test.go`
- `src/glogx/toast_test.go`
- `src/glogx/tui_actions_test.go`
- `src/glogx/render_test.go`
- `src/glogx/tui_nav_test.go`
- `src/glogx/usage_overlay_test.go`

失敗している 12 件 (これが green になることが完了条件。`LANG=ja_JP.UTF-8` で確認する):

```
TestBrowseFrameView
TestBrowseWrapUsesFullWidth
TestBuildPanelBoxTitleStripsANSI
TestBuildPanelBoxWidths
TestBuildShadowPanelBoxWidths
TestJapaneseFullViewStaysInWidth
TestJapanesePanelBoxWidths
TestOverlayBoxTopRightAligns
TestOverlayBoxTopRightKeepsLeftColor
TestShadowForegroundBlocksAndFeather
TestToastBoxLinesRevealsLeftColumns
TestWrapWindowFrame
```

### 注意 (機械的置換をしないこと)

- **全 `runewidth` 呼び出しを一律置換してよいとは限らない**。`render_test.go` などで
  「端末が全角と見なす幅」を意図して検証している箇所があれば、そこは production の
  `dispWidth` に合わせるのが正しいか個別に判断する (日本語テキストの折返し検証など)
- 置換後に `LANG=C` でも green を維持すること (CI が引き続き通る)

### 再発防止 (検討)

- `runewidth` をテストから import 禁止にする lint / grep ガード (`Makefile` の
  `test-go-lint` 系に追加)。production の唯一の幅出典は `width.go` の `dispWidth` なので、
  テストが別関数を使えないようにすれば構造的に防げる
- ただし上記「意図して runewidth で測る」箇所が残るなら禁止は行き過ぎ。その場合は
  ガードを入れず、`width.go` に「テストも dispWidth で測る」旨を書く方が穏当

## 関連

- commit 8c6fe1e — production 側を `ansi.StringWidth` に統一した変更 (本 issue はその
  テスト側の未完部分)
- commit 4c8ee8d — VS16 絵文字の幅ズレ対応 (同じ幅計算まわりの経緯)
- `src/glogx/width.go` — 幅計算の単一出典

## 対応結果 (2026-07-25 完了)

- テスト 6 ファイル (`box_test.go` / `toast_test.go` / `tui_nav_test.go` / `tui_actions_test.go` /
  `render_test.go` / `usage_overlay_test.go`) の `runewidth.StringWidth` を `dispWidth` へ置換。
  `dispWidth` は ANSI を幅 0 として無視するため、併用していた `stripANSI` は外せる箇所で外した
  (`render_test.go` の生文字列アサートはそのまま)
- `usage` パッケージ (`usage/render.go` / `usage/usage_test.go`) も `ansi.StringWidth` へ統一。
  ここは「runewidth が █ を幅 2 と数えるからバーのグリフを ▰/▱ にする」という**回避策で設計が
  歪んでいた**箇所で、同じ根本原因。出力を glogx 側が `dispWidth` で測る以上、物差しは一致させる
  必要がある (グリフ自体は見た目維持のため変更なし)
- 再発防止: `.golangci.yml` の depguard に `width-single-source` を追加し、
  `github.com/mattn/go-runewidth` の import を全ファイルで禁止 (production/テスト双方)。
  `go.mod` では indirect 依存に落ちた
- 確認: `go test -count=1 ./...` が `LANG=ja_JP.UTF-8` / `LANG=C` の両方で green、`make lint` も 0 issues
