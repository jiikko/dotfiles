package main

import (
	"fmt"
	"os"
	"time"
)

// doctorCleanup は走行中の doctor 走査 (disk / svc / brew) の完了 latch。
//
// なぜ要るか (issue 211): `doctorView.stop()` は ctx を cancel するだけで、**子プロセスの死を
// 待たない**。`runner.Exec` の kill は `exec.CommandContext` が起こす watchdog goroutine が
// `Kill(-pgid)` を打つ形なので、cancel が戻った時点では brew の子孫 (bash → ruby → git) は
// まだ生きている (red team 実測 3/3: cancel 直後 alive=true / 2 秒後 alive=false)。
//
// 再起動 (`r`) は `syscall.Exec` でプロセス像を置き換えるため、**その watchdog goroutine ごと
// 消える**。子は Setpgid で別プロセスグループなので端末の SIGINT も届かず、走り続ける
// (pid は保たれるので厳密には「孤児」ではなく、誰も wait しないゾンビになる)。
//
// 形は pullCleanup (external_commands.go) と同じで、**同じ cleanupLatch を使う**
// (issue 217 で pull 側も WaitGroup から載せ替えた)。登録は走査を起こす側
// (doctorView.start)、看取りは main.go。**pull 用とは別のインスタンスを持つ**: 待ちの上限も
// 出すメッセージも別で、片方の遅れをもう片方の理由として表示すると診断を誤らせる。
var doctorCleanup cleanupLatch

// doctorTrack は走査 1 本を latch に登録して走らせる。goroutine は呼び出し側が作る
// (tea.Cmd の closure からも使うため)。
//
// 🚨 **add は f を呼ぶ前に済ませる**。子プロセスを起こすのは f の中なので、この順序が
// 「子が生まれる前に登録されている」を保証する。
func doctorTrack(f func()) {
	doctorCleanup.add()
	defer doctorCleanup.done()
	f()
}

// waitDoctorCleanup は走行中の doctor 走査の帰還を看取ってから戻る (終了・再起動の直前用)。
// 待ちは runner.Exec の ctx cancel + WaitDelay (2 秒) で構造的に有限。すぐ終わらないときだけ
// 理由を出す (無言で固まったように見せない。waitPullCleanup と同じ作法)。
func waitDoctorCleanup() {
	done := doctorCleanup.wait()
	select {
	case <-done:
		return
	case <-time.After(200 * time.Millisecond):
		fmt.Fprintln(os.Stderr, "glogx: doctor の走査 (brew / du) の終了を待っています...")
	}
	<-done
}
