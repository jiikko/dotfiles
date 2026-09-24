package listnav

import "testing"

func TestMotionOf(t *testing.T) {
	for key, want := range map[string]Motion{
		"j": Down, "down": Down, "ctrl+n": Down,
		"k": Up, "up": Up, "ctrl+p": Up,
		"ctrl+d": HalfDown, "pgdown": HalfDown, " ": HalfDown, "space": HalfDown, "f": HalfDown,
		"ctrl+u": HalfUp, "pgup": HalfUp, "b": HalfUp, "shift+space": HalfUp,
		"g": Top, "home": Top,
		"G": Bottom, "end": Bottom,
		"enter": None, "x": None, "ctrl+b": None, "l": None,
	} {
		if got := MotionOf(key); got != want {
			t.Errorf("MotionOf(%q) = %v, want %v", key, got, want)
		}
	}
}

func TestListMoveClampsAndKeepsCursorInWindow(t *testing.T) {
	const total, rows = 30, 10
	var l List
	for _, step := range []struct {
		m          Motion
		cur, off   int
		wantMoved  bool
		wantGlides bool
	}{
		{Up, 0, 0, false, false},      // 先頭で上: 動かない
		{Down, 1, 0, true, false},     // 1 行
		{HalfDown, 6, 0, true, true},  // 半ページ (5 行) は滑る
		{HalfDown, 11, 2, true, true}, // 窓はカーソルを含む最小の窓
		{Bottom, 29, 20, true, false}, // 端ジャンプは即時
		{Down, 29, 20, false, false},  // 末尾で下: 動かない
		{HalfUp, 24, 20, true, true},
		{Top, 0, 0, true, false},
	} {
		moved := l.Move(step.m, total, rows, 20)
		if l.Cursor != step.cur || l.Offset != step.off || moved != step.wantMoved || l.Animating() != step.wantGlides {
			t.Fatalf("%v: cursor=%d offset=%d moved=%v glide=%v, want %d %d %v %v",
				step.m, l.Cursor, l.Offset, moved, l.Animating(), step.cur, step.off, step.wantMoved, step.wantGlides)
		}
		if l.Cursor < l.Offset || l.Cursor >= l.Offset+rows {
			t.Fatalf("%v: カーソル %d が窓 [%d, %d) の外", step.m, l.Cursor, l.Offset, l.Offset+rows)
		}
	}
}

// 半ページ移動では論理カーソルが即着地し、描画カーソルだけが起点から着地点へ滑る。
func TestListHalfPageGlidesDrawCursorOnly(t *testing.T) {
	var l List
	l.Move(HalfDown, 30, 20, 20) // 0 → 10
	if l.Cursor != 10 {
		t.Fatalf("論理カーソルが即着地していない: %d", l.Cursor)
	}
	if got := l.DrawCursor(30); got != 0 {
		t.Fatalf("滑走の開始で描画カーソルが起点に居ない: %d", got)
	}
	for range 5 {
		l.Advance()
	}
	if got := l.DrawCursor(30); got <= 0 || got >= 10 {
		t.Fatalf("途中の描画カーソル = %d, want 0 < x < 10", got)
	}
	for l.Animating() {
		l.Advance()
	}
	if got := l.DrawCursor(30); got != 10 {
		t.Fatalf("着地しない: %d", got)
	}
	// frames = 0 は滑らせない
	var still List
	still.Move(HalfDown, 30, 20, 0)
	if still.Animating() || still.DrawCursor(30) != still.Cursor {
		t.Fatal("frames=0 で滑った")
	}
}

// 滑走中の次の移動は、滑走を捨ててから効く (積み上げない)。
func TestListMoveStopsGlide(t *testing.T) {
	var l List
	l.Move(HalfDown, 30, 20, 20)
	l.Advance()
	l.Move(Down, 30, 20, 20)
	if l.Animating() || l.DrawCursor(30) != l.Cursor || l.Cursor != 11 {
		t.Fatalf("次のキーで着地していない: cursor=%d draw=%d glide=%v", l.Cursor, l.DrawCursor(30), l.Animating())
	}
}

func TestListEmptyAndFit(t *testing.T) {
	l := List{Cursor: 5, Offset: 3}
	if l.Move(Down, 0, 10, 20) || l.Cursor != 0 || l.Offset != 0 {
		t.Fatalf("空の一覧で状態が残る: %+v", l)
	}
	l = List{Cursor: 25, Offset: 20}
	l.Fit(8, 10) // 行数が縮んだ
	if l.Cursor != 7 || l.Offset != 0 {
		t.Fatalf("縮んだ一覧へ収め直していない: cursor=%d offset=%d", l.Cursor, l.Offset)
	}
}

func TestPagerMove(t *testing.T) {
	const total, rows = 50, 10
	var p Pager
	for _, step := range []struct {
		m     Motion
		off   int
		glide bool
	}{
		{Up, 0, false},
		{Down, 1, false},
		{HalfDown, 6, true},
		{Bottom, 40, false}, // 端ジャンプは滑走を捨てて即時
		{Down, 40, false},   // 末尾で止まる
		{Top, 0, false},
	} {
		p.Move(step.m, total, rows, 6)
		if p.Offset != step.off || p.Animating() != step.glide {
			t.Fatalf("%v: offset=%d glide=%v, want %d %v", step.m, p.Offset, p.Animating(), step.off, step.glide)
		}
	}
	// 半ページは描画 offset だけが滑る
	p.Move(HalfDown, total, rows, 6)
	if p.Offset != 5 || p.DrawOffset(total, rows) != 0 {
		t.Fatalf("滑走の開始: offset=%d draw=%d", p.Offset, p.DrawOffset(total, rows))
	}
	for p.Animating() {
		p.Advance()
	}
	if p.DrawOffset(total, rows) != 5 {
		t.Fatalf("着地しない: %d", p.DrawOffset(total, rows))
	}
	// 本文が縮んでも描画 offset は範囲内
	p.Offset = 40
	if got := p.DrawOffset(12, rows); got != 2 {
		t.Fatalf("縮んだ本文の描画 offset = %d, want 2", got)
	}
	p.Reset()
	if p.Offset != 0 || p.Animating() {
		t.Fatal("Reset が先頭へ戻らない")
	}
}

// rows=1 の半ページは窓の高さ以上に動くので、起点が窓の外になる。滑らせると描画カーソルが
// 窓の外に居て強調が 1 行も描かれないので、滑らせない。
func TestListHalfPageDoesNotGlideFromOutsideWindow(t *testing.T) {
	var l List
	l.Move(HalfDown, 30, 1, 20)
	if l.Animating() {
		t.Fatal("起点が窓の外なのに滑った")
	}
	if d := l.DrawCursor(30); d < l.Offset || d >= l.Offset+1 {
		t.Fatalf("描画カーソル %d が窓 [%d, %d) の外", d, l.Offset, l.Offset+1)
	}
}

// 0 以下の表示行数でも窓は total を超えず、カーソルは窓の中に居る。
func TestNonPositiveRowsStayInRange(t *testing.T) {
	for _, rows := range []int{0, -1, -5} {
		var l List
		l.Move(Bottom, 5, rows, 0)
		if l.Cursor != 4 || l.Offset != 4 {
			t.Fatalf("rows=%d: cursor=%d offset=%d, want 4 4", rows, l.Cursor, l.Offset)
		}
		if got, _ := Scroll(Bottom, 0, rows, 5); got != 4 {
			t.Fatalf("rows=%d: Scroll(Bottom) = %d, want 4", rows, got)
		}
		var p Pager
		p.Move(Bottom, 5, rows, 0)
		if p.Offset != 4 || p.DrawOffset(5, rows) != 4 {
			t.Fatalf("rows=%d: pager offset=%d draw=%d, want 4", rows, p.Offset, p.DrawOffset(5, rows))
		}
	}
}

func TestWindowOffset(t *testing.T) {
	for _, tc := range []struct{ offset, cursor, total, rows, want int }{
		{0, 5, 100, 10, 0},    // 窓の中
		{0, 15, 100, 10, 6},   // 下へはみ出たら最下行に合わせる
		{20, 3, 100, 10, 3},   // 上へはみ出たら最上行に合わせる
		{95, 99, 100, 10, 90}, // 末尾を越えない
		{0, 0, 5, 10, 0},      // 行数が窓より少ない
	} {
		if got := WindowOffset(tc.offset, tc.cursor, tc.total, tc.rows); got != tc.want {
			t.Errorf("WindowOffset(%d, %d, %d, %d) = %d, want %d", tc.offset, tc.cursor, tc.total, tc.rows, got, tc.want)
		}
	}
}

// Clamp は行数が減った後の論理 offset を上限へ収める。収めないと Up が「超えた分」だけ空振りする。
func TestPagerClampRecoversUpAfterShrink(t *testing.T) {
	var p Pager
	p.Move(Bottom, 100, 10, 0) // offset = 90
	// 行数が 40 に減った (幅が広がって折り返しが減った等)。上限は 30
	p.Clamp(40, 10)
	if p.Offset != 30 {
		t.Fatalf("Clamp 後の offset = %d, want 30", p.Offset)
	}
	if !p.Move(Up, 40, 10, 0) || p.Offset != 29 {
		t.Errorf("Clamp 直後の Up が 1 打鍵目で効かない: offset = %d, want 29", p.Offset)
	}
}

// Clamp は描画のたびに呼ばれるので、滑走を止めてはいけない (止めると半ページ送りが毎回即時になる)。
func TestPagerClampKeepsGlide(t *testing.T) {
	var p Pager
	p.Move(HalfDown, 100, 20, 6)
	if !p.Animating() {
		t.Fatal("前提の破れ: 半ページ移動で滑走が始まらない")
	}
	p.Clamp(100, 20)
	if !p.Animating() {
		t.Error("Clamp が滑走を止めた")
	}
}
