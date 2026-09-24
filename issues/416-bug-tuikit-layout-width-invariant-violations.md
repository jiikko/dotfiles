# 416 (bug): tuikit の幅計算が doc の不変条件を破る入力がある (TruncateLeft / Target / VS16 付き ASCII)

起票日: 2026-09-24
反証レビュー: 2026-09-24 実施 (読み取り専用のサブエージェント 1 体)。主要な主張は反証できず

## 概要

tuikit の layout / termwidth の各関数について、doc に書かれた「幅・行数の不変条件」を乱数入力
(seed 固定・4 種の文字集合 × 30 万回 = 120 万ケース。`RUNEWIDTH_EASTASIAN` は unset) で検査した結果、
P2 が 3 件・P3 が 3 件見つかった。幅は `termwidth.Of` で測っている。

出典: 2026-09-24 の設計・品質スキャン (観点 1「幅の不変条件の性質テスト」)。
P2 の 3 件は起票者が独立に再現した。P3 はスキャン報告のみ (未再現)。

## 詳細

### P2-1 `termwidth.TruncateLeft` が全角の跨ぎで 1 桁はみ出す (再現済み)

- doc: 「表示幅 width になるよう**先頭**を削り」
- `TruncateLeft("あ", 1, "")` = `"あ"` (幅 2)
- `TruncateLeft("docs/日本語.go", 5, "…")` = `"…語.go"` (幅 6)。width 7 → 8、9 → 10
- 呼び出し元: glogx の `doctorFitPath` (`src/glogx/doctor_view.go`) / `statusPathText` (`src/glogx/status_view.go`)。
  CJK を含むパスで踏む。`statusPathText` は結果をそのまま返すが、行全体は後段の `layout.Scrollbar`
  (`ClipMeasure`) で切り直されるので枠は破らない見込み。**末尾が `…` に化けるかは未確認**
- 方向: 切れ目が全角の途中に落ちたら、もう 1 桁削る

### P2-2 `DrawerGeometry.Target` が `MinList` を守らない (再現済み)

- doc: 「MinList は左に残す一覧の最小幅」「[MinList, MaxPeek] に収める」
- glogx の実寸 `{Ratio: 0.8, Extra: 10, MinList: 8, MaxPeek: 18}` で、左に残る一覧の幅:

  | total | 17 | 20 | 30 | 37 | 38 | 40 |
  |---|---|---|---|---|---|---|
  | 残る幅 | 3 | 4 | 6 | 7 | 8 | 8 |

- 原因: `return max(total-peek, ratioOnly)` で比率が `MinList` を上書きする。既存テストは total=40 だけ
- 到達: issues viewer の引き出しを 17〜37 桁の pane で開く
- 方向: `total > 2*MinList` のとき結果を `total-MinList` で上限する

### P2-3 ASCII + VS16 (キーキャップ `1️⃣` 等) で切り詰めの幅がずれる (再現済み)

- `ansi.Truncate` は「ASCII + VS16」を幅 1 と数えるが `termwidth.Of` は 2
- `Cut("1️⃣", 1)` の幅 2 / `ComposeDrawer(["1️⃣"], nil, 11, 12)` の行幅 13 (期待 12)。報告では
  `Clip` / `ClipMeasure` / `Scrollbar` / `SlideIn` / `Panel` の行 / `OverlayCentered` も同じ経路で崩れる。
  `#️⃣` / `a️` も同じで、`©️` / `™️` は崩れない
- glogx はほぼ守られている: termsafe の `DropEmojiVS16` が VS16 を落とすので、キーキャップは `1⃣`
  (幅 1 で一致) になる。**glogx の全入力が termsafe を通るかは未確認**。tuikit を使う他の画面
  (src/pro-con) は守られていない
- 方向: 切り詰めの後に `Of(result) > width` なら width-1 で切り直す (あるいは termwidth 側で VS16 を正規化する)

### P3 (スキャン報告のみ・未再現)

- **P3-1** 結合文字・単独の VS16・ZWJ で始まる文字列が、直前の罫線と 1 クラスタに結合する
  (`Of("┌"+"️")` = 2。`Panel(title="️", w=10)` の上辺が 11)。実運用では稀
- **P3-2** `DropColumns` / `StripSGR` は「最初の英字まで」を ESC シーケンスとして読むので、OSC 8 や
  不正な ESC で ansi と解釈が食い違う (`StripSGR` が OSC 8 のリンクを `"ttp://xink"` にする、
  `DropColumns("\x1b漢", 1)` = `""`)。glogx は OSC 8 を出さず (`]8;;` の grep 0 件)、termsafe が
  SGR 以外の ESC を落とすので、無害化していない入力でだけ踏む
- **P3-3** 範囲外の引数: `TruncateLeft(s, 0, "…")` = `"…"` (幅 1) / `SlideIn` に `stagger=1` で 0 除算
  (幅は破らない。glogx は定数を渡す)

### 問題なしと判断した範囲 (120 万ケースで破れなかった)

- 行数: `Panel` = len(rows)+3、`Scrollbar` / `ComposeDrawer` / `SlideIn` は入力と同じ、
  `Overlay` / `OverlayCentered` は max(len(window), page) 以下
- `ClipMeasure` の返す幅 = `Of(結果)` / `PadSpaces` の長さ・幅 = max(n, 0)
- `DropColumns` の `Of(DropColumns(s,n)) == max(Of(s)-n, 0)` (整形式の入力。CJK・SGR・キーキャップ・
  VS16・ZWJ・国旗・結合文字・C0 を含む 90 万ケース超)
- 引き出しの寸法: `Target` ∈ [0, total]、左の幅 ≤ max(MaxPeek, MinList)、`DrawerWidth` ∈ [0, target]
- 上記 P2-3 / P3 の文字を除いた集合での幅: `Panel` の行 = width+Indent、`Scrollbar` ≤ width、
  `ComposeDrawer` = total、`SlideIn` / `OverlayCentered` ≤ width、`Clip` / `ClipMeasure` / `Cut` ≤ width

## 対応方針

1. P2 の 3 件を直す (各項の「方向」)。直すたびに、その不変条件を破る入力をテストに固定する
2. 性質テスト (乱数入力で不変条件を見る) を `src/tuikit/layout` に常設するかを決める。
   スキャンのハーネスは使い捨てだったので、残すなら seed 固定・件数を出す形で書き直す

## 関連ファイル

- `src/tuikit/termwidth/termwidth.go` (`TruncateLeft` / `Clip` / `Cut` / `ClipMeasure` / `DropColumns` / `StripSGR`)
- `src/tuikit/layout/drawer.go` (`DrawerGeometry.Target` / `ComposeDrawer`)
- `src/glogx/doctor_view.go` `doctorFitPath` / `src/glogx/status_view.go` `statusPathText`

## 進捗

- [ ] P2-1 TruncateLeft
- [ ] P2-2 DrawerGeometry.Target
- [ ] P2-3 VS16 付き ASCII
- [ ] P3 の要否を判断 (直す / 記録のみ)
- [ ] 性質テストの常設を判断
