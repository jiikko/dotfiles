package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sync"
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
	// 子を独立したプロセスグループに置き、**lease を失ったときに**まとめて止められるように
	// する (この 1 点のためだけ。091:282 の `--on-lost=kill`)。
	//
	// 🚨 **「子が孫を作ったまま残るのを防ぐ」ではない** (issue 356 でコメントを訂正した)。
	// グループへ撃つのは (a) シグナルを受けたとき (b) lease を失ったとき の 2 経路だけで、
	// **子が正常終了した経路には無い**。`sh -c 'cmd & exit 0'` のように孫を置いて親だけ
	// 終わる形では、孫が走ったままロックが解放される (実測: 解放後の孫と次の保持者が
	// 同じ資源へ交互に書いた)。091 は孫の封じ込めを約束していないので、ここは
	// **直さないと決めた** — 正常終了時にグループを薙ぐと、意図的に起こす background の子
	// (`start-server &`) まで殺すことになり、opt-out の新設が要る。
	//
	// 🚨 さらに、**`setsid()` した子孫にはどの経路でも届かない** (Darwin 24.6.0 で実測:
	// グループへ SIGTERM を撃つと非 setsid の孫は死ぬが、setsid した孫は新しい pgid へ
	// 移っており生存する)。プロセスグループで回収できる範囲が上限。
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
	// exited は「子が回収された」を昇格ゴルーチンへ知らせるためだけの合図。
	// done は select ループが 1 回だけ受け取る (受け手を 2 つにしない)。
	exited := make(chan struct{})
	go func() {
		done <- cmd.Wait()
		close(exited)
	}()
	var escalate sync.Once

	outcome := renewOK
	for {
		select {
		case sig := <-sigCh:
			// 受けたシグナルは子のプロセスグループへ転送する (自分だけ死なない)。
			_ = killGroup(pgid, sig.(syscall.Signal))
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
				//
				// 🚨 **昇格は 1 回だけ**。tick ごとに撃ち直すと猶予が毎回振り出しに戻り、
				// SIGKILL へ永久に到達しない (上限の無い再試行は上限が無いのと同じ)。
				if onLostKill {
					escalate.Do(func() { go escalateGroupKill(pgid, exited) })
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

// onLostGracePeriod は SIGTERM を撃ってから SIGKILL へ昇格するまでの猶予。
// 🚨 これは「待ち」ではなく**仕様値** — 子に後片付けの機会を与えるための窓で、
// 縮めると trap を書いた子が片付け切れない。テストが差し替えるので var。
var onLostGracePeriod = 5 * time.Second

// escalateGroupKill は lease を失ったときに子のグループを**確実に**止める。
//
// 🚨 SIGTERM を 1 回撃つだけでは足りない。091:282 は「子プロセスを止められるようにする」と
// 書いているが、TERM を trap / 無視するプログラム (ffmpeg を含め普通にある) だと子は生き続け、
// `with` は子が終わるまで返らないので**他者が既に引き継いでいる状態が無期限に続く**
// (issue 356 の経路 2)。
//
// 🚨 届く範囲はプロセスグループに残った子孫まで。**`setsid()` した子孫には届かない**
// (runWith の Setpgid の注記を参照)。
func escalateGroupKill(pgid int, exited <-chan struct{}) {
	if err := killGroup(pgid, syscall.SIGTERM); err != nil {
		warnf("%v", err)
		return
	}
	select {
	case <-exited:
		return // 猶予の内に終わった
	case <-time.After(onLostGracePeriod):
	}
	warnf("子が %v 以内に終わらないので強制終了する (lease は既に他者が持っている)", onLostGracePeriod)
	_ = killGroup(pgid, syscall.SIGKILL)
}

// killGroup はプロセスグループへシグナルを送る。
//
// 🚨 **pgid が 0 や 1 なら撃たない。** `kill(0, sig)` は「呼び出し側のプロセスグループ」を
// 撃つので、pgid の計算を誤ると本番では呼び出し元シェルを、テストでは `go test` 自身を
// 殺す。撃つ前に弾く (issue 356 の実装時の注意)。
func killGroup(pgid int, sig syscall.Signal) error {
	if pgid <= 1 {
		return fmt.Errorf("プロセスグループ %d には撃たない (自分のグループを撃つ危険)", pgid)
	}
	return syscall.Kill(-pgid, sig)
}
