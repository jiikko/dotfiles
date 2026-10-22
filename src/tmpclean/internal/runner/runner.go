// Package runner は tmpclean の外部コマンドの実行口。stdout / stderr / exit code を分けて返す
// (lsof は「該当なし」も「失敗」も rc=1 を返すので、3 つを揃えて見ないと判定できない)。
//
// os/exec を import してよいのはこの package だけ (.golangci.local.yml の depguard)。src/doctor/runner と同じ形だが、
// doctor を import すると plist などまで引き込むので写している。3 つ目の利用者が出たら共有 module への切り出しを検討する。
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"syscall"

	"subproc"
)

// Runner は実行口の型。err は「起動できなかった / ctx の期限切れ」。exit code が非 0 なだけなら
// err == nil で rc に入る。テストは fake を差す。
type Runner func(ctx context.Context, name string, args ...string) (stdout, stderr string, rc int, err error)

// Exec は実際に exec する Runner。子を自分のプロセスグループにし、ctx の終了ではグループごと止める。
func Exec(ctx context.Context, name string, args ...string) (string, string, int, error) {
	cmd := subproc.CommandContext(ctx, name, args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	err := cmd.Run()
	if err == nil {
		return out.String(), errb.String(), 0, nil
	}
	if ctx.Err() != nil {
		return out.String(), errb.String(), -1, fmt.Errorf("%s: %w", name, ctx.Err())
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return out.String(), errb.String(), ee.ExitCode(), nil
	}
	return out.String(), errb.String(), -1, err
}
