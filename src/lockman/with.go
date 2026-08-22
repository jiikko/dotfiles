package main

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

// renewDivisor は自動更新の間隔 = TTL / renewDivisor。TTL 30 分なら 10 分ごと。
const renewDivisor = 3

// runWith は「取得 → 実行 → 確実に解放」。長い処理でも lease を失わないよう自動更新する。
//
// 終了コードは呼び出し側の API なので、子プロセスの終了コードをそのまま透過し、
// ロック側の失敗は子と衝突しない上位番号 (121/122/125) へ逃がす。
func runWith(l *Locker, ttl time.Duration, label string, onLostKill bool, argv []string) int {
	meta, err := l.Acquire(ttl, label)
	if err != nil {
		if errors.Is(err, errBusy) {
			warnf("他が保持中のため実行しない")
			return exitWithBusy
		}
		warnf("%v", err)
		return exitWithInvalid
	}
	defer func() {
		if err := l.Release(meta.Token); err != nil && !errors.Is(err, errNotOwner) {
			warnf("解放に失敗: %v", err)
		}
	}()

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	// 子を独立したプロセスグループに置き、まとめて止められるようにする
	// (子が孫を作ったまま残るのを防ぐ)。
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		warnf("実行できない: %v", err)
		return exitWithInvalid
	}
	pgid := cmd.Process.Pid

	sigCh := make(chan os.Signal, 4)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigCh)

	ticker := time.NewTicker(ttl / renewDivisor)
	defer ticker.Stop()

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	lost := false
	for {
		select {
		case sig := <-sigCh:
			// 受けたシグナルは子のプロセスグループへ転送する (自分だけ死なない)。
			_ = syscall.Kill(-pgid, sig.(syscall.Signal))
		case <-ticker.C:
			if err := l.Renew(meta.Token); err != nil {
				lost = true
				warnf("lease を失った: %v", err)
				if onLostKill {
					_ = syscall.Kill(-pgid, syscall.SIGTERM)
				}
			}
		case err := <-done:
			if lost {
				return exitWithLost
			}
			return childExitCode(err)
		}
	}
}

// childExitCode は子の終了状態を終了コードへ変換する。
// シグナル死は shell の慣習に合わせて 128+signal にする。
func childExitCode(err error) int {
	if err == nil {
		return exitOK
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		return ee.ExitCode()
	}
	warnf("子プロセスの終了を取得できない: %v", err)
	return exitWithInvalid
}
