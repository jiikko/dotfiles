package live

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mac-lan-manager/setup"
)

// fakeSSH は ssh の偽物を一時ディレクトリに置いて、その絶対パスを返す (本物の ssh は起こさない。PATH も引かない)。
func fakeSSH(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "ssh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// SSHRun は stdout を返し、失敗したら終了コードと stderr の文を err に持たせる。
func TestSSHRunExitCodeAndStderr(t *testing.T) {
	ok := fakeSSH(t, `printf 'out:%s\n' "$*"`)
	out, err := SSHRun(context.Background(), []string{ok, "-o", "BatchMode=yes", "--", "koji@192.168.50.7", "true"})
	if err != nil || out != "out:-o BatchMode=yes -- koji@192.168.50.7 true\n" {
		t.Fatalf("out=%q err=%v", out, err)
	}

	ng := fakeSSH(t, `echo partial; echo 'koji@192.168.50.7: Permission denied (publickey).' >&2; exit 255`)
	out, err = SSHRun(context.Background(), []string{ng})
	var ec interface{ ExitCode() int }
	if !errors.As(err, &ec) || ec.ExitCode() != 255 {
		t.Fatalf("終了コードが err に無い: %v", err)
	}
	if !strings.Contains(err.Error(), "Permission denied (publickey)") {
		t.Errorf("stderr の文が err に無い: %v", err)
	}
	if out != "partial\n" {
		t.Errorf("失敗しても stdout は返す: %q", out)
	}
}

// stdin は /dev/null (端末を読ませない。BatchMode の ssh がパスワードを聞かない)。偽物が stdin を読むと即 EOF になる。
func TestSSHRunStdinIsDevNull(t *testing.T) {
	cat := fakeSSH(t, `if [ -t 0 ]; then echo tty; else wc -c | tr -d ' '; fi`)
	out, err := SSHRun(context.Background(), []string{cat})
	if err != nil || strings.TrimSpace(out) != "0" {
		t.Errorf("stdin が空でない: out=%q err=%v", out, err)
	}
}

// 起こせなかったときは終了コード -1 (255 に見せない = 「鍵で入れない」にしない)。
func TestSSHRunNotStarted(t *testing.T) {
	_, err := SSHRun(context.Background(), []string{filepath.Join(t.TempDir(), "no-such-ssh")})
	var ec interface{ ExitCode() int }
	if err == nil || !errors.As(err, &ec) || ec.ExitCode() != -1 {
		t.Errorf("err=%v", err)
	}
	if _, err := SSHRun(context.Background(), nil); err == nil {
		t.Error("空の argv で err が無い")
	}
}

// setup.ObservePeer と組んだとき、Permission denied の 255 だけが「鍵で入れない」、届かない 255 は判定できない。
func TestSSHRunWithObservePeer(t *testing.T) {
	d := setup.Dest{User: "koji", IP: "192.168.50.7", HostKeyAlias: "m2macmini.local"}
	cases := []struct {
		name, body string
		unknown    bool
	}{
		{"denied", `echo 'koji@192.168.50.7: Permission denied (publickey,password).' >&2; exit 255`, false},
		{"unreachable", `echo 'ssh: connect to host 192.168.50.7 port 22: Operation timed out' >&2; exit 255`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := fakeSSH(t, c.body)
			run := func(ctx context.Context, argv []string) (string, error) {
				if len(argv) == 0 || argv[0] != "ssh" {
					t.Fatalf("ssh 以外を起こそうとした: %q", argv)
				}
				return SSHRun(ctx, append([]string{fake}, argv[1:]...))
			}
			p := setup.ObservePeer(context.Background(), run, d)
			if p.KeyOK || errors.Is(p.KeyErr, setup.ErrUnknown) != c.unknown {
				t.Errorf("PeerNet = %+v", p)
			}
		})
	}
}

// RunQuiet (lanmgr の広告を止める ssh) の失敗は、理由の 1 行と終了コードを err に持つ。lanmgr の unadvertise は終了コード
// (setup.ExitOtherName) で「相手は別の名前を広告している」を見分け、理由を画面に出す
func TestRunQuietKeepsExitCodeAndReason(t *testing.T) {
	ng := fakeSSH(t, `echo 'mac-lan-manager: 別の名前 (air6.wired.local) を広告している (止めない)' >&2; exit 4`)
	err := runQuiet(context.Background(), []string{ng})
	var ec interface{ ExitCode() int }
	if !errors.As(err, &ec) || ec.ExitCode() != setup.ExitOtherName {
		t.Fatalf("終了コードが err に無い: %v", err)
	}
	if !strings.Contains(err.Error(), "air6.wired.local") {
		t.Errorf("理由の文が err に無い: %v", err)
	}
	if err := runQuiet(context.Background(), nil); err == nil {
		t.Error("起こすコマンドが無いのに成功した")
	}
}
