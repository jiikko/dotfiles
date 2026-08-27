package main

import "glogx/termwidth"

// 表示幅の実装は glogx/termwidth (単一情報源。幅モデルの選択理由・fast-path の設計・実測値も
// そちらの doc が正本)。main は呼び出し箇所が多い (dispWidth だけで 40 箇所超) ので、
// gitOpTimeout と同じ形で別名を置き、呼び出し側を termwidth 化のために書き換えない。
// issues / usage は termwidth を直接呼ぶ (以前はここと同じ関数を各自で写していた。issue 106)。

func dispWidth(s string) int                               { return termwidth.Of(s) }
func truncateDisp(s string, width int, tail string) string { return termwidth.Truncate(s, width, tail) }
func truncateDispLeft(s string, width int, head string) string {
	return termwidth.TruncateLeft(s, width, head)
}
func padSpaces(n int) string               { return termwidth.PadSpaces(n) }
func fillRight(s string, width int) string { return termwidth.FillRight(s, width) }
func fillLeft(s string, width int) string  { return termwidth.FillLeft(s, width) }

// clusterWidth は grapheme クラスタ 1 個分の表示幅を返す。引数が「分割済みの 1 クラスタ」で
// あることを呼び出し側で示すための名前で、**幅の計算そのものは dispWidth に委ねる**。
//
// ⚠️ ここを uniseg.StringWidth に戻さないこと (issue 112)。理由は「**幅の出典を 1 本にする**」
// ことであって、「x/ansi が端末の真実だから」ではない (下の v1/v2 の注記のとおり、v2 の
// 描画エンジンの既定はむしろ WcWidth で、この層とは既に食い違っている。揃える先の議論は
// issue 124 で別に追う)。
//
// 幅を 2 本のライブラリで測ると、同じ行を測り直すたびに違う答えが出る。実測 2026-08-27:
// 単一 rune クラスタ 434 件で ansi と uniseg の幅が食い違い、dropToColumn が満たすべき
// `dispWidth(dropToColumn(s,n)) == dispWidth(s) - n` の違反が **6720 件 → 757 件**へ減った
// (2-rune クラスタの全域走査)。0 にならないのは、**分割器も 2 本ある**から (uniseg=Unicode 15 /
// x/ansi=16)。そちらは issue 124。
// ASCII / CJK / 絵文字では両者が一致するので、手元の目視では絶対に出ない。
func clusterWidth(cluster string) int { return dispWidth(cluster) }

// 幅の層ごとの実測表と VS16 正規化のトレードオフは termsafe.DropEmojiVS16 のドキュメント
// コメントにある (関数ごと termsafe へ移した。main からは dropEmojiVS16 の別名で呼べる)。
