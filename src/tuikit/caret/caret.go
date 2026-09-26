// Package caret は入力欄のキャレットを端末のカーソルにする (bubbletea v2 の View.Cursor)。
//
// 🚨 IME は変換中の文字を**端末のカーソルの位置**に出す。カーソルを置かないと、描画の差分を書き終えた位置
// (毎回変わる) に出て、日本語の変換中に入力欄から外れる。位置は「欄の左端 + キャレットの前の表示幅」
// (幅は lineedit.Line.Window が termwidth で数える) で、それを画面の座標にする所をここ 1 つにする (issue 517)。
//
// tuikit の中でここだけが bubbletea を import する: 置く先が tea.Cursor そのもので、形 (棒) もここで揃えるため。
package caret

import tea "charm.land/bubbletea/v2"

// At は画面の (x, y) に置く棒のカーソル。
//
// 画面の幅を越える桁は最終列 (width-1) に、負の桁は 0 に寄せる (width に置くと画面の外になり、
// そこへ IME の変換中の文字が出る。幅 9 のような極端に狭い端末で実際に起きた)。
// y が画面の外なら nil (カーソルを隠す)。height <= 0 なら縦は見ない (高さに収めるのが後の段のとき)。
func At(x, y, width, height int) *tea.Cursor {
	if y < 0 || height > 0 && y >= height {
		return nil
	}
	x = max(min(x, width-1), 0)
	c := tea.NewCursor(x, y)
	c.Shape = tea.CursorBar
	return c
}
