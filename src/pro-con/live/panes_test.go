package live

import (
	"maps"
	"testing"
)

// tmux list-panes の行を、端末 → session:window.pane の表に読む。session の名前の空白は残し、欠けた行は捨てる。
func TestParsePanes(t *testing.T) {
	got := parsePanes("/dev/ttys003 main:2.1\n/dev/ttys007 my work:0.0\n\nbroken\n")
	want := map[string]string{"/dev/ttys003": "main:2.1", "/dev/ttys007": "my work:0.0"}
	if !maps.Equal(got, want) {
		t.Fatalf("parsePanes = %v, want %v", got, want)
	}
}
