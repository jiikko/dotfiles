package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// 猶予が切れた**後**でも、SIGKILL の直前に「もう終わったか」を見直すこと (issue 384 項目 2)。
//
// 🚨 この窓は本番では `select` の一様ランダム選択 (実測 50.1% が timer 枝) に依存していて、
// テストから作れない。`escalateBeforeKillHook` の seam で「timer 枝を取った後に子が終わった」
// 状態を決定論で作る。
//
// 実体は **SIGTERM を無視する** 子プロセスグループにする。無視しないと関数冒頭の TERM で死んで
// しまい、SIGKILL 側の判定に到達しない (= 有無で結果が変わらない fixture になる)。
func TestEscalateRechecksExitedBeforeSigkill(t *testing.T) {
	pgid, waitDone := startIgnoringTermGroup(t)

	exited := make(chan struct{})
	var once sync.Once
	old := escalateBeforeKillHook
	escalateBeforeKillHook = func() { once.Do(func() { close(exited) }) } // 猶予切れの直後に「終わった」
	defer func() { escalateBeforeKillHook = old }()

	escalateGroupKill(pgid, exited, 10*time.Millisecond)

	// 🚨 **「起きないこと」の assert なので、待つべき成立条件が無い** (この形だけは時間で待つ。
	// ポーリングにできない理由をここに書いておかないと、次の人が「消し忘れ」と読んで消す)。
	// 判定は `kill(pid, 0)` ではなく **`cmd.Wait()` の完了**で行う: SIGKILL された子は
	// wait するまでゾンビとして残り、`kill(pid, 0)` は**成功し続ける**ので、
	// シグナルの有無で結果が変わらない観測になる (実測 2026-09-16 にこの形で緑になった)。
	select {
	case <-waitDone:
		t.Fatalf("猶予切れの後に子が終わっているのに SIGKILL を撃った (pgid=%d)", pgid)
	case <-time.After(300 * time.Millisecond):
	}
}

// 対照: 終わっていなければ SIGKILL は撃つ (上の検査が「常に撃たない」へ倒れていないこと)。
func TestEscalateStillKillsWhenChildIsAlive(t *testing.T) {
	pgid, waitDone := startIgnoringTermGroup(t)

	escalateGroupKill(pgid, make(chan struct{}), 10*time.Millisecond)

	select {
	case <-waitDone:
	case <-time.After(5 * time.Second):
		t.Fatalf("生きている子へ SIGKILL が飛んでいない (pgid=%d)", pgid)
	}
}

// startIgnoringTermGroup は **SIGTERM を無視する**子を新しいプロセスグループで起こし、
// pgid と「本当に終わったか」を表す channel を返す。
//
// 🚨 TERM を無視させないと `escalateGroupKill` 冒頭の TERM で死んでしまい、SIGKILL 側の
// 判定に到達しない (= 有無で結果が変わらない fixture になる)。
func startIgnoringTermGroup(t *testing.T) (int, <-chan struct{}) {
	t.Helper()
	// 🚨 `trap "" TERM; sleep 30` では**グループごと TERM で死ぬ**: trap を張った sh は
	// 生き延びるが、同じグループに居る `sleep` が TERM を受けて死に、sh の `sleep` が返って
	// 連鎖終了する (実測 2026-09-16)。TERM を受けてもグループが生き残る形にする
	// (死ぬのは SIGKILL のときだけ)。
	// 🚨 **trap を張り終えたことを成果物で待つ**。`kill(pid, 0)` は fork 直後 (trap 設置前) でも
	// 成功するので、それを前提にすると**冒頭の TERM で死ぬ**子を「準備できた」と読む
	// (実測 2026-09-16: この形で SIGKILL 側の判定に一度も到達していなかった)。
	ready := filepath.Join(t.TempDir(), "ready")
	cmd := exec.Command("/bin/sh", "-c", `trap "" TERM; : > "$1"; while :; do sleep 0.2; done`, "sh", ready)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("Getpgid: %v", err)
	}
	waitDone := make(chan struct{})
	go func() { _ = cmd.Wait(); close(waitDone) }()
	// 前提: trap を張り終えていること (成立条件をポーリングする。壁時計で待たない)
	waitForFile(t, ready)
	t.Cleanup(func() {
		_ = killGroup(pgid, syscall.SIGKILL)
		select {
		case <-waitDone:
		case <-time.After(5 * time.Second):
			t.Errorf("後始末: 子 (pgid=%d) を回収できない", pgid)
		}
	})
	return pgid, waitDone
}

// TERM が届かないとき、警告が「子が走り続けている可能性」まで言うこと (issue 384 項目 1)。
//
// 🚨 昇格は `sync.Once` なので、ここで返ると**二度と走らない**。`kill(2)` の失敗は
// EPERM / ESRCH / EINVAL しか無く一過性が無いため再試行に意味は無く、効くのは報告だけ。
// pgid<=1 は killGroup 自身が弾くので、届かない形を決定論で作れる。
func TestEscalateReportsWhenSignalCannotReach(t *testing.T) {
	// killGroup 自身が弾く pgid を使う (届かない形を決定論で作れる。実 EPERM は
	// `setsid` した子が要るが、報告の中身を固定するのが目的なのでここでは十分)
	out := captureStderr(t, func() {
		escalateGroupKill(1, make(chan struct{}), time.Millisecond)
	})
	if strings.TrimSpace(out) == "" {
		t.Fatalf("届かなかったのに何も報告していない")
	}
	for _, want := range []string{"昇格できない", "走り続けている"} {
		if !strings.Contains(out, want) {
			t.Fatalf("報告に %q が無い (人が二重実行を疑えない): %s", want, out)
		}
	}
}

// waitForFile は子が「準備できた」と申告するのを待つ。
func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("前提: 子が準備完了を申告しない (%s)", path)
}
