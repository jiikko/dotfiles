package discover

import (
	"context"
	"io"
	"os/exec"

	"subproc"
)

// ExecStarter は本物の dns-sd を起こす Starter。読み手を Close するか ctx を取り消すと、そのプロセスを止めて待つ
// (止めるのは自分が起こした pid だけ。名前で探して止めない)。
func ExecStarter(ctx context.Context, name string, args ...string) (io.ReadCloser, error) {
	cmd := subproc.CommandContext(ctx, name, args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &cmdReader{ReadCloser: out, cmd: cmd}, nil
}

type cmdReader struct {
	io.ReadCloser
	cmd *exec.Cmd
}

func (r *cmdReader) Close() error {
	_ = r.cmd.Process.Kill() // 終わらないコマンドなので、読み終えたら止める (既に終わっていれば失敗するだけ)
	_ = r.ReadCloser.Close()
	_ = r.cmd.Wait()
	return nil
}
