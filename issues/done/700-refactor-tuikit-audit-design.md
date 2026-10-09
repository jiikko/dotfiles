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
   (母集合: `listnav.` を使う非テストのファイル 18、うち MotionOf を使うもの 17)。linkJumpKey は ctrl+n / ctrl+p / tab を既に扱っていて、全くの重複ではない。直し方: MotionOf の結果で分岐する (循環する選択の 2 つは Top / Bottom の扱いを決める)
4. **toast の退場の tick を使う側が守る** (P3・E2 / L5) — `toast.Stack.Advance` が返す `[]Timer` を使う側が tick にして `StartLeaving` へ戻す約束。捨てると
   通知が永久に残る (実測)。変換は glogx・pro-con・treefiler に 3 実装。直し方: tea を import しない helper を 1 つ置く / Holding が長すぎたら自分で退場する
5. **使う側の別実装** (P3・duplication) — `toast.easedShown` が `anim.EaseOutCubic` を直書き・treefiler の `stripSGR` と `wrapCells` (`termwidth.Wrap(s, w, false)` と
   ほぼ同じ)・schedkeys の `stripSGR`・pro-con の `sgrReset` などの別名 (glogx は `sgr` に寄せ済み)・treefiler と schedkeys の `\x1b[0m` の直書き。1 を直してから寄せる
6. **影なしの角丸の枠が tuikit に無い** (P3・ui-components) — pro-con の `boxTop` / `boxLine` / `boxBottom` (呼び出し 75 箇所)。`layout.Panel` は影付き固定。`layout` に影なしの
   `Box` を足す。treefiler (セルの canvas) と schedkeys (frame 型) は層が違い寄せられない

## 関連

- 監査の記録: 702

## 進捗

- [ ] 未着手
- [x] 1 `termwidth.DropColumns` / `StripSGR` が ESC のシーケンスを x/ansi のパーサで読む (`escLen` = `ansi.DecodeSequence`、`ansi.Strip`)。OSC 8 のリンクで
  `Of(StripSGR(s)) == Of(s)` と DropColumns の幅の不変条件を固定した (`TestEscapesReadLikeAnsi`)。ESC 無しの早期 return は残す
- [x] 2 `lineedit.Acceptable`: 制御文字・向きを変える制御文字 (U+202A-202E / 2066-2069) を落とし、U+2028 / 2029 は改行と同じく空白にする。ZWJ と
  異体字セレクタは通す (lineedit は家族やキーキャップの絵文字を 1 文字として扱う設計で、`TestEditKeysKeepGraphemeClusters` が固定している)。
  schedkeys はこれに Cf 全般と異体字セレクタを足して打鍵ごと捨てる (描画側の幅とずれる理由は schedkeys 側)。schedkeys の editor を lineedit へ
  移すのは、表示窓や操作の API が違うので見送り、判定の正本だけを寄せた
- [x] 3 pro-con の凡例・treefiler の設定の板・glogx のリンクのジャンプが移動を `listnav.MotionOf` で読む。リンクのジャンプ中の半ページ送り
  (space など) は今までどおりジャンプを抜けて本文の pager へ渡す。凡例の半ページは `listnav.Half(m.height)`、設定の板は 1 行ずつ巡る
- 4 helper は置かない: toast は部品 (bubbletea を import しない層) なので tea.Tick への変換は使う側に置くしかなく、捨てたときの実害は helper でも
  防げない。`Stack.Advance` の doc に契約を書いた
- [x] 5 `toast.easedShown` → `anim.EaseOutCubic`、pro-con の `sgrReset` / `sgrBold` / `sgrDim` の値を tuikit/sgr から、treefiler の stripSGR を
  `termwidth.StripSGR` へ、schedkeys の stripSGR は `termwidth.StripSGR` を呼ぶ、`\x1b[0m` の直書き (treefiler の render・schedkeys) を sgr.Reset へ。
  treefiler の `wrapCells` は寄せない: `termwidth.Wrap(s, w, false)` と ESC の無い 30 万件で突き合わせると、幅 1 に全角とタブが並ぶ所で
  1.2 万件食い違った (同じ答えを出さない実装を置き換えない)
- 6 見送り: 影なしの枠を使うのは pro-con だけ (treefiler はセルの canvas、schedkeys は frame 型で層が違う)。tuikit へ移すと実装 1 つの抽象になる
- 変異 (5 本 red): DropColumns を「英字まで」の走査に戻す / BiDi を通す / 凡例の半ページ送りを消す / リンクのジャンプの Top を消す / 板の Bottom を消す
- 敵対レビュー (sonnet、1 周): P1 / P2 なし (旧実装との差は OSC 8 だけ・StripSGR の確保は ESC ありで 2 回)。直した P3: schedkeys の
  isVariationSelector が acceptable の doc の途中に入っていた・import の並び・貼り付けの U+2028 / 2029 で単語がくっつく。記録のみ: SS3 (`\x1bOP`) を
  DecodeSequence は 3 バイト・Of は 2 バイトで読む (使う側は SGR と OSC 8 しか渡さない)・テストは pgdown / f / b を各画面で見ていない
- `make test` / `make lint` (tuikit・pro-con・treefiler・schedkeys・glogx) rc=0
