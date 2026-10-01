package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jiikko/dotfiles/src/restartable/internal/control"
)

func TestSIGPIPEMainHelper(t *testing.T) {
	if os.Getenv("RESTARTABLE_SIGPIPE_HELPER") != "1" {
		return
	}
	code := runMain([]string{"--control", "/tmp/" + strings.Repeat("x", 120), "--", "/bin/true"})
	os.Exit(code)
}

func TestSIGPIPERemainsHandledAfterRunnerReturns(t *testing.T) {
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := readEnd.Close(); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestSIGPIPEMainHelper$")
	cmd.Env = append(os.Environ(), "RESTARTABLE_SIGPIPE_HELPER=1")
	cmd.Stdout = io.Discard
	cmd.Stderr = writeEnd
	if err := cmd.Start(); err != nil {
		_ = writeEnd.Close()
		t.Fatal(err)
	}
	_ = writeEnd.Close()
	err = cmd.Wait()
	if err == nil {
		t.Fatal("helper succeeded, want the runner setup error code 1")
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("helper exit = %v, want exit code 1 (not SIGPIPE)", err)
	}
}

// CLI の終了コードは obaket の dev-restart が依存する契約 (status の rc 2 = ループが動いていない)。
// 本物の CLI 入口 (runMain) を子プロセスで起こして rc と stdout / stderr を別々に見る。
func TestControlCLIMainHelper(t *testing.T) {
	raw := os.Getenv("RESTARTABLE_CLI_ARGS")
	if raw == "" {
		return
	}
	var args []string
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		t.Fatal(err)
	}
	os.Exit(runMain(args))
}

func runCLI(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestControlCLIMainHelper$")
	cmd.Env = append(os.Environ(), "RESTARTABLE_CLI_ARGS="+string(encoded))
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		code = 0
	case errors.As(err, &exit):
		code = exit.ExitCode()
	default:
		t.Fatal(err)
	}
	return code, out.String(), errOut.String()
}

// 短い socket path (MaxSocketPath = 103) を確保する。
func shortSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "rs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "c.sock")
}

// serve は control.Listen の本物のサーバを立て、受けた要求に respond で答える。
// respond が nil なら要求を受けても答えない。
func serve(t *testing.T, path string, respond func(*control.Request) control.Response) {
	t.Helper()
	server, err := control.Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	t.Cleanup(func() {
		close(done)
		_ = server.Close()
	})
	go func() {
		for {
			select {
			case req := <-server.Requests():
				if respond != nil {
					req.Respond(respond(req))
				}
			case <-done:
				return
			}
		}
	}()
}

func TestControlCLIExitCodes(t *testing.T) {
	missing := shortSocketPath(t)

	refused := shortSocketPath(t)
	listener, err := net.Listen("unix", refused)
	if err != nil {
		t.Fatal(err)
	}
	listener.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = listener.Close() // socket ファイルだけが残る (listener 無し)

	okSock := shortSocketPath(t)
	serve(t, okSock, func(req *control.Request) control.Response {
		return control.Response{OK: true, State: string(req.Command), Ready: true}
	})
	failSock := shortSocketPath(t)
	serve(t, failSock, func(req *control.Request) control.Response {
		return control.Response{OK: false, Reason: "refused-" + string(req.Command)}
	})
	silentSock := shortSocketPath(t)
	serve(t, silentSock, nil)

	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantResp   *control.Response // stdout の JSON と一致すべき応答 (nil なら stdout が空であること)
		wantStderr bool              // stderr に診断が出ること (false なら空であること)
	}{
		{"status: socket が無い", []string{"status", "--control", missing}, 2, nil, true},
		{"restart: socket が無い", []string{"restart", "--control", missing}, 2, nil, true},
		{"status: socket ファイルだけ残っていて listener が無い", []string{"status", "--control", refused}, 2, nil, true},
		{"status: ok:true", []string{"status", "--control", okSock}, 0, &control.Response{OK: true, State: "status", Ready: true}, false},
		{"restart: ok:true", []string{"restart", "--control", okSock}, 0, &control.Response{OK: true, State: "restart", Ready: true}, false},
		{"status: ok:false", []string{"status", "--control", failSock}, 1, &control.Response{Reason: "refused-status"}, false},
		{"restart: ok:false", []string{"restart", "--control", failSock}, 1, &control.Response{Reason: "refused-restart"}, false},
		{"status: 時間切れ", []string{"status", "--control", silentSock, "--timeout", "200ms"}, 124, nil, true},
		{"status: 不正な flag", []string{"status", "--bogus"}, 2, nil, true},
		{"status: 余分な引数", []string{"status", "--control", okSock, "extra"}, 2, nil, true},
		{"status: timeout が 0", []string{"status", "--control", okSock, "--timeout", "0s"}, 2, nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := runCLI(t, tc.args...)
			if code != tc.wantCode {
				t.Fatalf("rc = %d, want %d\nstdout=%q\nstderr=%q", code, tc.wantCode, stdout, stderr)
			}
			if tc.wantResp == nil {
				if stdout != "" {
					t.Fatalf("stdout = %q, want empty", stdout)
				}
			} else {
				var got control.Response
				if err := json.Unmarshal([]byte(stdout), &got); err != nil {
					t.Fatalf("stdout %q is not a response JSON: %v", stdout, err)
				}
				if got != *tc.wantResp {
					t.Fatalf("stdout response = %+v, want %+v", got, *tc.wantResp)
				}
			}
			if (stderr != "") != tc.wantStderr {
				t.Fatalf("stderr = %q, want diagnostics: %v", stderr, tc.wantStderr)
			}
		})
	}
}

// 時間切れはどちらの形 (context の期限切れ / 接続の I/O の期限切れ) で返っても 124。CLI を通すテストでは、どちらが先に返るかが
// 競合で決まり、片方の分岐を外しても緑になる回があるので、写しそのものを決定的に固定する。
func TestCallErrorCode(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
		want int
	}{
		{"socket が無い", fmt.Errorf("dial unix /x: %w", os.ErrNotExist), 2},
		{"listener が無い", errors.New("dial unix /x: connect: connection refused"), 2},
		{"context の期限切れ", fmt.Errorf("call: %w", context.DeadlineExceeded), 124},
		{"I/O の期限切れ", &net.OpError{Op: "read", Net: "unix", Err: os.ErrDeadlineExceeded}, 124},
		{"それ以外", errors.New("decode: unexpected EOF"), 1},
	} {
		if got := callErrorCode(c.err); got != c.want {
			t.Errorf("%s: callErrorCode = %d, want %d", c.name, got, c.want)
		}
	}
}
