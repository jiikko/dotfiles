package framebench

import (
	"bytes"
	"strings"
	"testing"
)

// 色を捨てずに端末へ出す (NoTTY のまま測ると SGR が全部消え、差分と出るバイトが本物より軽く出る)。
func TestFrameKeepsColors(t *testing.T) {
	var out bytes.Buffer
	r := newRenderer(20, 3, &out)
	r.Frame("\x1b[38;2;255;0;0mred\x1b[m plain", nil)
	if !strings.Contains(out.String(), "red") {
		t.Fatalf("前提: 描いた文字が出ていない: %q", out.String())
	}
	if !strings.Contains(out.String(), "38;2;255;0;0") {
		t.Fatalf("色が捨てられた (TrueColor で出していない): %q", out.String())
	}
	if r.Written() != int64(out.Len()) {
		t.Fatalf("Written が出たバイトと合わない: %d / %d", r.Written(), out.Len())
	}
}

// 前のコマと content も属性も同じなら描かない (bubbletea の viewEquals)。属性 (カーソルの位置) だけ変わったコマは描く。
func TestFrameSkipsOnlyIdenticalFrames(t *testing.T) {
	r := NewRenderer(20, 3)
	type cursor struct{ X, Y int }
	if !r.Frame("a", &cursor{1, 0}) {
		t.Fatal("1 コマ目を描かなかった")
	}
	n := r.Written()
	if r.Frame("a", &cursor{1, 0}) || r.Written() != n {
		t.Fatal("同じコマを描き直した")
	}
	if !r.Frame("a", &cursor{2, 0}) {
		t.Fatal("カーソルだけ動いたコマを描かなかった (bubbletea は描く)")
	}
	if !r.Frame("b", &cursor{2, 0}) || r.Written() == n {
		t.Fatal("中身の変わったコマが端末へ出ていない")
	}
}
