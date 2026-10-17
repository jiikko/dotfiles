// Package extcmd は外部コマンドの探索と、時間の上限付きの実行 (Chrome・ffmpeg のエンコーダ・RunQuiet)。
package extcmd

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"zundamon-kaisetsu/internal/app"
)

func FindChrome() string {
	cands := []string{os.Getenv("CHROME"),
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium"}
	for _, n := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if p, err := exec.LookPath(n); err == nil {
			cands = append(cands, p)
		}
	}
	for _, c := range cands {
		if c == "" {
			continue
		}
		if st, err := os.Stat(c); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return c
		}
	}
	return ""
}

func PngSize(path string) (int, int) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer func() { _ = f.Close() }()
	head := make([]byte, 24)
	if n, _ := f.Read(head); n < 24 || string(head[:8]) != "\x89PNG\r\n\x1a\n" {
		return 0, 0
	}
	return int(binary.BigEndian.Uint32(head[16:20])), int(binary.BigEndian.Uint32(head[20:24]))
}

// RunQuiet はコマンドを stdin なしで走らせ、rc と stdout+stderr を返す (timeout は rc=124、起動できなければ 127)。
func RunQuiet(timeout time.Duration, name string, args ...string) (int, string) {
	return RunQuietCtx(app.Ctx, timeout, name, args...)
}

// RunQuietCtx は RunQuiet の親 context を選べる版。中断の後の後始末 (エンジンの停止) は Ctx 以外から呼ぶ。
func RunQuietCtx(parent context.Context, timeout time.Duration, name string, args ...string) (int, string) {
	var out bytes.Buffer
	rc, msg := run(parent, timeout, &out, &out, name, args...)
	if msg == "" {
		msg = strings.TrimSpace(out.String())
	}
	return rc, msg
}

// RunSplit は RunQuiet の stdout と stderr を分けて返す版 (stdout をデータとして読むとき。Chrome の --dump-dom は stderr にログを混ぜる)。
// 中断・timeout・起動できないときの rc は RunQuiet と同じで、そのときの説明は stderr の側に入れる
func RunSplit(timeout time.Duration, name string, args ...string) (rc int, stdout, stderr string) {
	var o, e bytes.Buffer
	rc, msg := run(app.Ctx, timeout, &o, &e, name, args...)
	if msg == "" {
		msg = strings.TrimSpace(e.String())
	}
	return rc, o.String(), msg
}

// run はコマンドを走らせて rc を返す。中断・timeout・起動の失敗のときだけ、その説明を返す (それ以外は "")
func run(parent context.Context, timeout time.Duration, stdout, stderr io.Writer, name string, args ...string) (int, string) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	// timeout で kill した後、孫プロセス (Chrome の子など) が出力のパイプを持ったままでも待ち続けない
	cmd.WaitDelay = 5 * time.Second
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	if errors.Is(parent.Err(), context.Canceled) {
		return 130, "中断した"
	}
	if ctx.Err() != nil {
		return 124, fmt.Sprintf("%.0f 秒で応答しない", timeout.Seconds())
	}
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0, ""
	case errors.As(err, &ee):
		return ee.ExitCode(), ""
	default:
		return 127, err.Error()
	}
}

var (
	libx264Re      = regexp.MustCompile(`(?m)^\s*V\S*\s+libx264\s`)
	videotoolboxRe = regexp.MustCompile(`(?m)^\s*V\S*\s+h264_videotoolbox\s`)
)

func H264Encoder() []string {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil
	}
	rc, out := RunQuiet(60*time.Second, "ffmpeg", "-hide_banner", "-encoders")
	switch {
	case rc == 0 && libx264Re.MatchString(out):
		return []string{"-c:v", "libx264", "-preset", "medium", "-crf", "20", "-tune", "stillimage"}
	case rc == 0 && videotoolboxRe.MatchString(out):
		return []string{"-c:v", "h264_videotoolbox", "-b:v", "3M"}
	}
	return nil
}
