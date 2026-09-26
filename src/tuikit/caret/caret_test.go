package caret

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// 欄の中なら言われた位置に棒で置く。幅を越える桁は最終列、負の桁は 0 に寄せる (画面の外に置くと、IME の変換中の文字がそこに出る)。
// 画面の外の行なら置かない。高さを渡さなければ縦は見ない。
func TestAt(t *testing.T) {
	for _, tc := range []struct {
		name                string
		x, y, width, height int
		wantX, wantY        int
		none                bool
	}{
		{name: "中", x: 5, y: 2, width: 10, height: 5, wantX: 5, wantY: 2},
		{name: "右端", x: 9, y: 0, width: 10, height: 5, wantX: 9},
		{name: "幅を越える", x: 10, y: 0, width: 10, height: 5, wantX: 9},
		{name: "負の桁", x: -3, y: 1, width: 10, height: 5, wantX: 0, wantY: 1},
		{name: "下の外", x: 1, y: 5, width: 10, height: 5, none: true},
		{name: "上の外", x: 1, y: -1, width: 10, height: 5, none: true},
		{name: "高さを見ない", x: 1, y: 50, width: 10, wantX: 1, wantY: 50},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := At(tc.x, tc.y, tc.width, tc.height)
			if tc.none {
				if c != nil {
					t.Fatalf("画面の外なのに置いた: %+v", c.Position)
				}
				return
			}
			if c == nil || c.X != tc.wantX || c.Y != tc.wantY || c.Shape != tea.CursorBar {
				t.Fatalf("At(%d,%d,%d,%d) = %+v、欲しいのは (%d,%d) の棒", tc.x, tc.y, tc.width, tc.height, c, tc.wantX, tc.wantY)
			}
		})
	}
}
