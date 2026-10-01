# 602 (test): tuikit が宣言している守りのうち 3 つが機械で止まっていない (depguard の対象漏れ・狭い窓の toast・行数 0 の markdown)

起票日: 2026-10-01

出典: tuikit の監査 (未実施の 7 タイプ、2026-10-01。[604](604-research-tuikit-audit-remaining-types-2026-10-01.md))。所見は読み取り専用の調査役 (sonnet) のもので、depguard の対象漏れは Claude がコードで確かめた。2 と 3 の変異は調査役が mktemp のコピーで当て、反証レビューが別に当て直して緑を再現した (2 は textWidth の `maxWidth-5`、3 は width < 12 で lines = nil)。

## 1. depguard の部品の純粋性の対象に toast・lineedit が無い (leaky-abstraction L1 の守りの穴)

`src/tuikit/.golangci.yml` の `no-framework-in-parts` と `parts-pure` の `files` に、`**/toast/**` と `**/lineedit/**` が無い。toast の doc は「描画フレームワークに依存しない (tuikit の約束)」と宣言し、lineedit も同じ。
今の違反は 0 件だが、toast.go / lineedit.go に bubbletea と os を import しても golangci-lint v2.5.0 の depguard は 0 件だった (調査役の実測)。

- 発火条件: toast / lineedit に描画フレームワークや os を足す変更。lint は止めない
- 直し方: 2 つの規則の files に足す。足した後、toast.go に bubbletea を import する変異で depguard が赤になることを確かめる
- 守れる範囲 (反証レビュー): `parts-pure` が止めるのは os / os/exec / context だけで、toast の時刻 (time) は forbidigo の規則が担う。files に足しても time の import は止まらない。今の toast は time と termsafe、lineedit は strings / unicode / utf8 / termwidth だけを import していて違反は 0 件

## 2. toast の BoxLines / textWidth が窓幅 10 前後未満で無検査 (false-green F5)

`toast_test.go` が `BoxLines` に渡す幅は 0 と 30 以上だけ。それ未満は `wrapText` の直呼び (幅 2〜17) しか踏まない。

- 変異 A: `BoxLines` で maxWidth < 14 のとき items = nil (通知が全部消える) にしても `go test ./toast/` は ok
- 変異 B: `textWidth` の `layout.PanelContentWidth(maxWidth)` を `maxWidth-5` にしても ok (コメントが守ると書く「最小幅から下限を導く」が無検査)
- 直し方: 幅 1〜40 を掃き、箱の幅 ≤ 窓幅・文字が欠けない・行が空でないことを見る

## 3. markdown の TestRenderNeverExceedsWidth は行数 0 でも通る (false-green F2)

幅 1〜120 を掃くが、見るのは「各行の幅 ≤ width」だけ。

- 変異: `RenderLinks` で width < 12 (< 6 でも) のとき lines = nil にしても markdown package 全体が ok。render.go のコメントが却下した「幅 N 未満は畳む」設計がこれで緑になる
- 直し方: 掃引の中で `len(lines) > 0` と、本文の文字が出力に残ることも見る

## 進捗

- [x] 1. depguard の files に toast・lineedit を足し、変異で赤を確かめる
- [x] 2. toast の狭い窓の掃引のテスト
- [x] 3. markdown の掃引に行数と本文の検査

## 結果 (2026-10-01)

- 1: `no-framework-in-parts` / `parts-pure` の files に `**/toast/**` / `**/lineedit/**` を足した。`**/toast/**` が `examples/toast/` (bubbletea を使ってよいデモ) にも当たったので
  両方に `!**/examples/**` を足した。caret (bubbletea の View.Cursor を組むのが仕事) と editor (外部のエディタを起こすのが仕事) は理由をコメントに書いて対象外のまま。
  変異: toast.go / lineedit.go に bubbletea と os を import → どちらも depguard が 2 件で赤 (mutate-verify rc=0)
- 2: `TestToastBoxAtEveryNarrowWidth` (幅 1〜40: 箱が出る・PanelMinWidth 以上は窓に収まり未満は下限で止まる・本文が欠けない)。
  変異: 狭い窓で通知を全部消す / textWidth を `maxWidth-5` → どちらもこのテストだけが赤
- 3: `TestRenderNeverExceedsWidth` に行数と本文 (段落の先頭) の検査を足した。幅 1 は全角が入らず … になるので ASCII の部分を見る。
  変異: width < 12 / < 6 で lines = nil → どちらも赤
