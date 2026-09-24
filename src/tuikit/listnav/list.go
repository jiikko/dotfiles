package listnav

import "tuikit/anim"

// List は一覧のカーソルと窓。zero value = 先頭。
//
// 半ページ移動は**描画カーソルだけ**を滑らせる (anim.CursorGlide)。論理カーソル (Cursor) は
// 即座に着地するので、決定キーは常に着地点へ効く。窓 (Offset) は論理カーソルを含む最小の窓で、
// 滑走中も動かさない (遅らせると描かれていない行が操作対象になる)。
type List struct {
	Cursor int // 論理カーソル (操作の対象)
	Offset int // 窓の先頭行
	glide  anim.CursorGlide
}

// Move は移動 m を適用する。total は行数、rows は表示行数、frames は半ページ移動で
// カーソルを滑らせるフレーム数 (0 なら滑らせない)。カーソルが動いたら true。
//
// 進行中の滑走は必ず着地させてから動かす (積み上げると「押した分だけ遅れて動く」)。
func (l *List) Move(m Motion, total, rows, frames int) bool {
	l.glide.Stop()
	rows = max(rows, 1) // 0 以下の表示行数は 1 行として扱う (窓の計算が total を超えないように)
	if total <= 0 {
		l.Cursor, l.Offset = 0, 0
		return false
	}
	from := l.Cursor
	switch m {
	case Down:
		l.Cursor++
	case Up:
		l.Cursor--
	case HalfDown:
		l.Cursor += Half(rows)
	case HalfUp:
		l.Cursor -= Half(rows)
	case Top:
		l.Cursor = 0
	case Bottom:
		l.Cursor = total - 1
	case None:
		return false
	}
	l.fit(total, rows)
	// 起点が新しい窓の外なら滑らせない (滑走の最初のフレームで描画カーソルが窓の外に居て、
	// カーソルの強調が 1 行も描かれない。半ページが窓の高さ以上になる rows=1 で起きる)
	inWindow := from >= l.Offset && from < l.Offset+rows
	if (m == HalfDown || m == HalfUp) && frames > 0 && inWindow {
		l.glide.Start(from, l.Cursor, frames)
	}
	return l.Cursor != from
}

// Fit は行数・表示行数が変わったとき (再読込・resize) にカーソルと窓を収め直す。滑走は捨てる。
func (l *List) Fit(total, rows int) {
	l.glide.Stop()
	if total <= 0 {
		l.Cursor, l.Offset = 0, 0
		return
	}
	l.fit(total, max(rows, 1))
}

// fit はカーソルを [0, total) へ、窓をカーソルを含む最小の窓へ収める (total >= 1, rows >= 1)。
func (l *List) fit(total, rows int) {
	l.Cursor = min(max(l.Cursor, 0), total-1)
	l.Offset = WindowOffset(l.Offset, l.Cursor, total, rows)
}

// DrawCursor は描画に使うカーソル行 (滑走中は途中位置)。行き過ぎても [0, total) に収める。
func (l *List) DrawCursor(total int) int {
	if total <= 0 {
		return 0
	}
	return min(max(l.glide.Cursor(l.Cursor), 0), total-1)
}

// Animating は滑走中か (使う側が tick を回し続ける判定に使う)。
func (l *List) Animating() bool { return l.glide.Active() }

// Advance は滑走を 1 フレーム進める (tick のたびに呼ぶ)。
func (l *List) Advance() {
	if l.glide.Active() {
		l.glide.Advance(l.Cursor)
	}
}

// Stop は滑走を捨てて即時表示へ倒す。
func (l *List) Stop() { l.glide.Stop() }
