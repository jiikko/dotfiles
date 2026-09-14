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

// renewOutcome は自動更新の結果。**「lease を失った」と「判定できない」は別物**で、
// 091:398-399 が終了コードを分けている (122 = 走行中に lease を失った / 125 = 判定不能)。
// 単一の bool に畳むと、一過性の I/O ヒカップが「引き継がれた」として報告される。
type renewOutcome int

const (
	renewOK            renewOutcome = iota
	renewLost                       // errNotOwner = 本当に持ち主でなくなった
	renewIndeterminate              // I/O タイムアウト・probe 失敗・打刻ずれ
)

// classifyRenewErr は Renew の失敗を終了コードの分類へ落とす。
// 🚨 errNotOwner は %w でラップされて返る経路がある (lock.go の「lease が切れている」)
// ので、== ではなく errors.Is で見る。
func classifyRenewErr(err error) renewOutcome {
	if errors.Is(err, errNotOwner) {
		return renewLost
	}
	return renewIndeterminate
}

// runWith は「取得 → 実行 → 確実に解放」。長い処理でも lease を失わないよう自動更新する。
//
// 終了コードは呼び出し側の API なので、子プロセスの終了コードをそのまま透過し、
// ロック側の失敗は子と衝突しない上位番号 (121/122/125) へ逃がす。
func runWith(l *Locker, ttl time.Duration, label string, onLostKill bool, argv []string) int {
	meta, err := l.AcquireTimed(ttl, label)
	if err != nil {
		if errors.Is(err, errBusy) {
			warnf("他が保持中のため実行しない")
			return exitWithBusy
		}
		// I/O タイムアウトもここへ落ちる (判定不能 = 125。091:418)。
		warnf("%v", err)
		return exitWithInvalid
	}
	defer func() {
		// 🚨 解放のタイムアウトは**終了コードを上書きしない**。091:418 の「超えたら 125」は
		// 取得・更新の経路に当てる規律で、子の終了コードは呼び出し側の API だから
		// (子が成功したのに 125 を返すと、透過の契約が壊れる)。固まった事実は warn で出す。
		if err := l.ReleaseTimed(meta.Token); err != nil && !errors.Is(err, errNotOwner) {
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

	outcome := renewOK
	for {
		select {
		case sig := <-sigCh:
			// 受けたシグナルは子のプロセスグループへ転送する (自分だけ死なない)。
			_ = syscall.Kill(-pgid, sig.(syscall.Signal))
		case <-ticker.C:
			if err := l.RenewTimed(meta.Token); err != nil {
				// 🚨 **「lease を失った」と「確認できない」を混ぜない。** Renew は
				// errNotOwner 以外でも失敗する (I/O タイムアウト / probe の失敗 /
				// clockSkewTolerance を超える打刻ずれ)。一過性の I/O ヒカップを 122 と
				// 報告すると「走行中に引き継がれた」と読まれるが、実際には lease は
				// 失われていない。091:398-399 ではそれは 125 (判定不能)。
				if outcome == renewOK {
					outcome = classifyRenewErr(err)
				}
				if outcome == renewLost {
					warnf("lease を失った: %v", err)
				} else {
					warnf("lease を確認できない (判定不能): %v", err)
				}
				// どちらも fail-closed で子を止める。**次の tick を待たない** —
				// TTL を超えれば他者が引き継ぐので、待つほど二重実行に近づく。
				if onLostKill {
					_ = syscall.Kill(-pgid, syscall.SIGTERM)
				}
			}
		case err := <-done:
			switch outcome {
			case renewLost:
				return exitWithLost
			case renewIndeterminate:
				return exitWithInvalid
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
