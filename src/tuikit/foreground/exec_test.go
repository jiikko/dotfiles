package foreground_test

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/jiikko/dotfiles/src/tuikit/foreground"
)

// Exec の取り戻しの判断 (持っていた前面だけを取り戻し、結果を Was / Err に入れる) を、前面の確かめと取り戻しを
// 差し替えて見る (pty は要らない)。pro-con の ui/terminal_test.go も同じことを termExec 越しに見ているが、
// tuikit だけを変えたときにも tuikit のテストで止める (issue 758)。
func TestExecReclaimsOnlyWhatItHeld(t *testing.T) {
	stolen := errors.New("stolen")
	for _, held := range []bool{true, false} {
		calls := 0
		x := foreground.NewExec(exec.Command("true"))
		x.Owned = func() bool { return held }
		x.Reclaim = func() (int, error) { calls++; return 42, stolen }
		if err := x.Run(); err != nil {
			t.Fatalf("held=%v: 子の err = %v", held, err)
		}
		switch {
		case held && (calls != 1 || x.Was != 42 || !errors.Is(x.Err, stolen)):
			t.Errorf("持っていた前面を取り戻していない: calls=%d Was=%d Err=%v", calls, x.Was, x.Err)
		case !held && (calls != 0 || x.Was != 0 || x.Err != nil):
			t.Errorf("持っていなかった前面を奪った: calls=%d Was=%d Err=%v", calls, x.Was, x.Err)
		}
	}
}

// Cmd が nil なら何も走らせず、前面も確かめない (bubbletea の手放し → 入れ直しだけを通す)。
func TestExecWithoutCommandDoesNothing(t *testing.T) {
	x := &foreground.Exec{
		Owned:   func() bool { t.Error("Cmd が nil なのに前面を確かめた"); return true },
		Reclaim: func() (int, error) { t.Error("Cmd が nil なのに取り戻した"); return 0, nil },
	}
	if err := x.Run(); err != nil {
		t.Fatal(err)
	}
}

// SetStdin / SetStdout / SetStderr は、使う側が先に決めた入出力を上書きしない (bubbletea が端末を渡すのは空のときだけ)。
func TestExecSettersKeepExplicitStreams(t *testing.T) {
	var mine, theirs strings.Builder
	c := exec.Command("true")
	c.Stdin = strings.NewReader("mine")
	c.Stdout, c.Stderr = &mine, &mine
	x := foreground.NewExec(c)
	x.SetStdin(strings.NewReader("theirs"))
	x.SetStdout(&theirs)
	x.SetStderr(&theirs)
	if r, ok := c.Stdin.(*strings.Reader); !ok || r.Len() != len("mine") || c.Stdout != &mine || c.Stderr != &mine {
		t.Fatal("使う側が決めた入出力を上書きした")
	}
	empty := exec.Command("true")
	y := foreground.NewExec(empty)
	in := strings.NewReader("in")
	y.SetStdin(in)
	y.SetStdout(&theirs)
	y.SetStderr(&theirs)
	if empty.Stdin != in || empty.Stdout != &theirs || empty.Stderr != &theirs {
		t.Fatal("空の入出力に端末を入れていない")
	}
	(&foreground.Exec{}).SetStdin(in) // Cmd が nil でも panic しない
}
