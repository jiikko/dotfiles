package listnav

// ClampOffset はスクロール offset を 0..max(total-rows, 0) へ収める。
//
// 「offset は独立した状態ではなく (カーソル・行数・表示行数) からの導出値」という規律を
// 1 箇所に置くための関数。論理 offset の収束と glide の途中位置の両方に通す。手書きすると
// 同じ画面の中でも散り、上限の決め方を変えたとき片方だけ直して「末尾に届かない」形で壊れる。
func ClampOffset(offset, total, rows int) int {
	return max(min(offset, max(total-rows, 0)), 0)
}

// WindowOffset は「カーソルを含む窓」へ offset を収束させる (キー処理と描画で行数が食い違っても、
// カーソルが画面外に出ない)。
func WindowOffset(offset, cursor, total, rows int) int {
	if cursor < offset {
		offset = cursor
	}
	if cursor >= offset+rows {
		offset = cursor - rows + 1
	}
	return ClampOffset(offset, total, rows)
}

// Cycle は 0..n-1 を輪として、i から delta 進めた位置を返す (末尾の次は先頭、先頭の前は末尾)。
// n は 1 以上 (0 のときは整数の 0 除算で panic する。呼ぶ側が空を先に弾く)。
func Cycle(i, delta, n int) int {
	return ((i+delta)%n + n) % n
}
