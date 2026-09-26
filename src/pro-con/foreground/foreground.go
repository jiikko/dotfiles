// Package foreground は、画面が端末の前面 (前面のプロセスグループ) を持っているかを確かめ、外れていたら取り戻す (issue 518)。
//
// 前面でないプロセスグループが端末を読むと SIGTTIN で止まる (シェルに「suspended (tty input)」と出る)。
// 画面は前面で端末を読み続けるので、何かが前面を別のグループへ移すと止まる。
// 実際に起きた経路 (2026-09-26): テストの係の make test の中の `zsh -i -c` が、job control を入れるときに前面を
// 自分のグループへ移したまま終わった。これは起こす側で制御端末を持たせない (Detached) ことで塞ぐ。Reclaim は、それでも
// 前面が外れたとき (画面が前面で渡した子が前面を移したまま終わった等) に画面が自分で戻るためのもの。
package foreground

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// reclaimEnv が立っていれば、このプロセスは Reclaim が起こした使い捨ての子: stdin (端末) の前面を自分のグループ
// (= 起こした親のグループ) へ移して抜ける。
const reclaimEnv = "PRO_CON_FOREGROUND_RECLAIM"

func init() {
	if os.Getenv(reclaimEnv) != "1" {
		return
	}
	// 前面でないグループからの tcsetpgrp は SIGTTOU で止まるので無視する。無視はこの使い捨てのプロセスにだけ効く
	signal.Ignore(syscall.SIGTTOU)
	if err := unix.IoctlSetPointerInt(0, unix.TIOCSPGRP, syscall.Getpgrp()); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

// Owner は fd (端末) の前面のプロセスグループと、自分のプロセスグループを返す。fd が端末でなければ err。
func Owner(fd int) (fg, mine int, err error) {
	fg, err = unix.IoctlGetInt(fd, unix.TIOCGPGRP)
	return fg, syscall.Getpgrp(), err
}

// Reclaim は前面が自分のグループでなければ自分へ移し、移す前の前面のグループを返す (0 = 外れていなかった)。
// 🚨 移すのは使い捨ての子 (自分のバイナリを reclaimEnv つきで起こす。同じグループにいる) にやらせる。自分で SIGTTOU を
// 無視して移すと、signal.Reset で既定の動作へ戻らず (Go の runtime は SIG_IGN を残す。実測)、以後は前面でないまま
// 端末の設定を書いても止まらなくなり、無視は exec・fork を越えて子 (エディタ・新版・supervisor) へも引き継がれる。
func Reclaim(fd int) (was int, err error) {
	fg, mine, err := Owner(fd)
	if err != nil || fg == mine {
		return 0, err
	}
	fail := func(err error) (int, error) {
		return fg, fmt.Errorf("端末の前面をグループ %d から取り戻せない: %w", fg, err)
	}
	exe, err := os.Executable()
	if err != nil {
		return fail(err)
	}
	dup, err := unix.Dup(fd) // os.NewFile に渡した fd は Close で閉じるので、複製を渡す (fd は画面が使い続ける)
	if err != nil {
		return fail(err)
	}
	tty := os.NewFile(uintptr(dup), "tty")
	defer func() { _ = tty.Close() }()
	var errOut bytes.Buffer
	c := exec.Command(exe)
	c.Env = append(os.Environ(), reclaimEnv+"=1")
	c.Stdin, c.Stderr = tty, &errOut
	if err := c.Run(); err != nil {
		return fail(fmt.Errorf("%w: %s", err, strings.TrimSpace(errOut.String())))
	}
	if now, _, err := Owner(fd); err != nil || now != mine {
		return fail(fmt.Errorf("移した後の前面が %d (err=%v)", now, err))
	}
	return fg, nil
}

// Detached は、画面の外で走り続ける子 (supervisor・テストの係の実行) を起こすときの SysProcAttr。
// 新しい session にして制御端末を持たせない (前面を移せない・/dev/tty を開けない)。グループの先頭は子自身なので、
// グループごと止める (kill(-pid)) 作法は Setpgid と変わらない。
// 🚨 Setpgid と一緒に立てない (setsid の後の setpgid は session の先頭なので、fork/exec が EPERM で失敗する。実測)。
func Detached() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }
