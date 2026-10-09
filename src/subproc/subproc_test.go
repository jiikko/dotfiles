package subproc

import (
	"context"
	"testing"
)

// CommandContext が WaitDelay を張ることを固定する。
// 🚨 これが緩むと issue 105 の再発 (出力を取る実行で Wait が戻らなくなる) を誰も検知できない。
func TestCommandContextSetsWaitDelay(t *testing.T) {
	cmd := CommandContext(context.Background(), "true")
	if cmd.WaitDelay != WaitDelay {
		t.Fatalf("WaitDelay が張られていない: got %v want %v", cmd.WaitDelay, WaitDelay)
	}
}

// 猶予が 0 や負だと WaitDelay を「張っていない」のと同じになる (0 は無効化の意味)。
func TestWaitDelayIsPositive(t *testing.T) {
	if WaitDelay <= 0 {
		t.Fatalf("WaitDelay は正でなければ意味がない: %v", WaitDelay)
	}
}

// 引数がそのまま渡ること (WaitDelay を張るために包んだ結果、引数を落としていないか)。
func TestCommandContextPassesArgs(t *testing.T) {
	cmd := CommandContext(context.Background(), "echo", "a", "b")
	if len(cmd.Args) != 3 || cmd.Args[1] != "a" || cmd.Args[2] != "b" {
		t.Fatalf("引数が渡っていない: %v", cmd.Args)
	}
}

func TestGitArgsDisablesFsmonitor(t *testing.T) {
	got := GitArgs("-C", "/x", "status")
	if n := len(gitHarden); len(got) != n+3 || got[0] != "-c" || got[1] != "core.fsmonitor=false" || got[n] != "-C" || got[n+2] != "status" {
		t.Fatalf("GitArgs = %q", got)
	}
	if c := GitCommand(context.Background(), "status"); c.WaitDelay != WaitDelay || c.Args[1] != "-c" {
		t.Fatalf("GitCommand: WaitDelay=%v args=%q", c.WaitDelay, c.Args)
	}
}
