package live

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"mac-lan-manager/setup"
	"mac-lan-manager/speedtest"
	"subproc"
)

// SpeedTimeout は測定 1 回の全体の上限 (上り・下りの流す時間 + 接続と後始末の余裕)。相手が黙ったときに書き込みで止まり続けない。
const SpeedTimeout = 2*speedtest.Duration + 30*time.Second

// SpeedTest は本物の ssh で相手との通信速度を測る (lanmgr.Options.SpeedTest)。now は時計 (lanmgr が注入された「今」を渡す)。
func SpeedTest(ctx context.Context, d setup.Dest, now func() time.Time) speedtest.Result {
	ctx, cancel := context.WithTimeout(ctx, SpeedTimeout)
	defer cancel()
	return speedtest.Measure(ctx, d, speedStart, speedtest.Clock{Now: now, After: time.After}, speedtest.Duration)
}

// speedStart は speedtest.Start の本物。ctx が終われば subproc の WaitDelay の範囲で ssh を止める。
func speedStart(ctx context.Context, argv []string, withStdin bool) (speedtest.Proc, error) {
	if len(argv) == 0 {
		return nil, errors.New("起こすコマンドが無い")
	}
	cmd := subproc.CommandContext(ctx, argv[0], argv[1:]...)
	p := &speedProc{stderr: &bytes.Buffer{}}
	cmd.Stderr = p.stderr
	if withStdin {
		in, err := cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		p.in = in
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	p.out = out
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p.wait = cmd.Wait
	return p, nil
}

type speedProc struct {
	in     io.WriteCloser
	out    io.Reader
	stderr *bytes.Buffer
	wait   func() error
}

func (p *speedProc) Stdin() io.WriteCloser { return p.in }
func (p *speedProc) Stdout() io.Reader     { return p.out }

// Wait の失敗は stderr の文を持つ (speedtest は "Permission denied (" で鍵で入れないかを分ける)。
func (p *speedProc) Wait() error {
	if err := p.wait(); err != nil {
		return &RunError{Err: err, Stderr: strings.TrimSpace(p.stderr.String())}
	}
	return nil
}
