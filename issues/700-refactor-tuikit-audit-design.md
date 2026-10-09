# 700 (refactor): tuikit の設計の課題 (同じ判定の別実装・使う側が守る契約)

起票日: 2026-10-09

## 概要

702 の監査の設計の分。1 つの発見は一番狭い型に 1 回だけ数えた。

## 詳細

1. **ESC 列を読む実装が 2 系統あり、OSC 8 (リンク) で食い違う** (P2・duplication / L5) — `termwidth.StripSGR` と `DropColumns` は ESC の後ろを「英字が出るまで」
   読む自前の走査 (`isSGRTerminator`)、`Of` / `Wrap` / `Slice` / `SliceFrom` は x/ansi のパーサ。`\x1b]8;;https://example.com/a\x1b\\link\x1b]8;;\x1b\\ tail` で
   `StripSGR` が `ttps://example.com/ainkail` を返し (`ansi.Strip` は `link tail`)、`Of` は 9 なのに `Of(StripSGR(s))` は 26。`DropColumns(s,2)` は URL の途中を落とす。
   今は使う側が SGR 以外を落としてから渡すので届かない (潜在)。直し方: `ansi.Strip` / `DecodeSequence` に寄せる (か「SGR のみ」を契約にして名前どおりに絞る)
2. **lineedit が受け付ける文字が甘い** (P2・duplication / L5) — `lineedit.Line.Insert` は Cc しか落とさず、Cf (BiDi の RLO 等)・Zl・異体字セレクタを通す。
   schedkeys の自前の editor (`acceptable`) はこれらを弾く (表示の順の偽装・幅 0・VS16 でカーソルがずれる)。treefiler の `!` は `line.String()` を `sh -c` へ
   渡し、貼り付けも `Insert` を通る (見えているコマンドと実行されるものが食い違いうる。貼る本人の操作が前提)。直し方: lineedit に schedkeys と同じ拒否の集合を
   入れ、schedkeys を lineedit へ移す (glogx・pro-con が Cf・VS を通したい場面があるかは未確認)
3. **j/k/g/G の語彙を `listnav.MotionOf` を通さずに書き直した画面が 3 つ** (P2・duplication) — `pro-con/ui/legend.go`・`treefiler/filer/settings.go` の
   panelKey・`glogx/issues_linkjump.go` の linkJumpKey。pro-con の凡例は ctrl+n / ctrl+p / home / end / pgup / pgdown / ctrl+d / ctrl+u が効かない
   (母集合: `listnav.` を使うファイル 18)。直し方: MotionOf の結果で分岐する (循環する選択の 2 つは Top / Bottom の扱いを決める)
4. **toast の退場の tick を使う側が守る** (P3・E2 / L5) — `toast.Stack.Advance` が返す `[]Timer` を使う側が tick にして `StartLeaving` へ戻す約束。捨てると
   通知が永久に残る (実測)。変換は glogx・pro-con・treefiler に 3 実装。直し方: tea を import しない helper を 1 つ置く / Holding が長すぎたら自分で退場する
5. **使う側の別実装** (P3・duplication) — `toast.easedShown` が `anim.EaseOutCubic` を直書き・treefiler の `stripSGR` と `wrapCells` (`termwidth.Wrap(s, w, false)` と
   ほぼ同じ)・schedkeys の `stripSGR`・pro-con の `sgrReset` などの別名 (glogx は `sgr` に寄せ済み)・treefiler と schedkeys の `\x1b[0m` の直書き。1 を直してから寄せる
6. **影なしの角丸の枠が tuikit に無い** (P3・ui-components) — pro-con の `boxTop` / `boxLine` / `boxBottom` (72 箇所)。`layout.Panel` は影付き固定。`layout` に影なしの
   `Box` を足す。treefiler (セルの canvas) と schedkeys (frame 型) は層が違い寄せられない

## 関連

- 監査の記録: 702

## 進捗

- [ ] 未着手
