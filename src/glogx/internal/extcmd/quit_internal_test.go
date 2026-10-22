package extcmd

import (
	"errors"
	"os/exec"
	"slices"
	"strconv"
	"testing"
)

// 終了コードを渡すのはエディタにだけ。F の `!` のシェルや L の ssh に渡すと、その中で開いた nvim まで <C-g> で抜ける。
func TestQuitCodeEnvOnlyForEditor(t *testing.T) {
	want := quitCodeEnv + "=" + strconv.Itoa(QuitExitCode)
	for _, tc := range []struct {
		origin ExecOrigin
		has    bool
	}{{OriginEditor, true}, {OriginFiler, false}, {OriginLan, false}} {
		cmd := exec.Command("true") // subproc: no-waitdelay — 起動しない (Env を組むだけ)
		withQuitCode(tc.origin, cmd)
		if got := slices.Contains(cmd.Env, want); got != tc.has {
			t.Errorf("origin %d: %s を渡したか = %v, want %v (Env=%v)", tc.origin, want, got, tc.has, cmd.Env)
		}
	}
}

// 終了の依頼は QuitExitCode の終了だけ。ただの異常終了 (nvim の :cq の既定 = 1) や起動失敗で glogx を閉じない。
func TestIsQuitRequest(t *testing.T) {
	exit := func(code int) error {
		return exec.Command("sh", "-c", "exit "+strconv.Itoa(code)).Run() // subproc: no-waitdelay — 即終了・パイプ無し
	}
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"QuitExitCode", exit(QuitExitCode), true},
		{"exit 1", exit(1), false},
		{"正常終了", nil, false},
		{"起動失敗", errors.New(`exec: "zz": executable file not found in $PATH`), false},
	} {
		if got := IsQuitRequest(tc.err); got != tc.want {
			t.Errorf("%s: IsQuitRequest = %v, want %v (err=%v)", tc.name, got, tc.want, tc.err)
		}
	}
}
