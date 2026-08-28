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

// firstCluster は先頭の grapheme クラスタ 1 個と、その表示幅を返す。**幅を測り直さない**
// のが要点で、理由と実測は termwidth.FirstCluster の doc が正本 (issue 124)。
func firstCluster(s string) (cluster string, width int) { return termwidth.FirstCluster(s) }

// 幅の層ごとの実測表と VS16 正規化のトレードオフは termsafe.DropEmojiVS16 のドキュメント
// コメントにある (関数ごと termsafe へ移した。main からは dropEmojiVS16 の別名で呼べる)。
