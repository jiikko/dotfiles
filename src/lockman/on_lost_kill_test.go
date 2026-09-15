package main

import (
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// `--on-lost kill` (既定) の経路。着手時点でテストは **1 本も無かった** (issue 356)。
// 091:282 は「子プロセスを**止められる**ようにする」と書いているが、実装は SIGTERM を
// 1 回撃つだけで、trap / 無視する子には効かなかった。

// 🚨 子が TERM を無視しても止まること。ここが経路 2 の本体。
// 昇格が無いと runWith は子が自然に終わるまで返らず、**他者が既に引き継いでいる状態が
// 無期限に続く**ので、安全網の上限で赤にする。
func TestOnLostKillEscalatesToSigkill(t *testing.T) {
	restore := shortenOnLostGrace(t, 300*time.Millisecond)
	defer restore()

	l, err := NewLocker(t.TempDir(), testIOTimeout)
	if err != nil {
		t.Fatalf("NewLocker: %v", err)
	}
	const ttl = 600 * time.Millisecond // tick = 200ms
	go func() {
		for range 400 {
			if _, err := os.Stat(l.lockPath()); err == nil {
				_ = l.Break() // 他者が引き継いだ = lease 喪失
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// TERM を無視する子。昇格が無ければ 60 秒生き続ける
	got := boundedInt(t, "runWith (TERM を無視する子)", func() int {
		return runWith(l, ttl, "", true, []string{"sh", "-c", "trap '' TERM; sleep 60"})
	})
	if got != exitWithLost {
		t.Fatalf("exit %d (期待 %d = lease 喪失)", got, exitWithLost)
	}
}

// --on-lost warn では止めない (既定を変えたい人のための逃げ道が生きていること)。
func TestOnLostWarnDoesNotKillChild(t *testing.T) {
	l, err := NewLocker(t.TempDir(), testIOTimeout)
	if err != nil {
		t.Fatalf("NewLocker: %v", err)
	}
	const ttl = 600 * time.Millisecond
	go func() {
		for range 400 {
			if _, err := os.Stat(l.lockPath()); err == nil {
				_ = l.Break()
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	// 子は自分で 400ms 後に終わる。kill されたら 128+15 / 128+9 になるので区別できる
	got := boundedInt(t, "runWith (--on-lost warn)", func() int {
		return runWith(l, ttl, "", false, []string{"sh", "-c", "sleep 0.4"})
	})
	if got != exitWithLost {
		t.Fatalf("exit %d (期待 %d)", got, exitWithLost)
	}
}

// 🚨 pgid が 0 なら **呼び出し側のプロセスグループ**を撃つ。本番では呼び出し元シェル、
// テストでは go test 自身が死ぬので、撃つ前に弾けていることを固定する。
func TestKillGroupRefusesUnsafePgid(t *testing.T) {
	for _, pgid := range []int{0, 1, -1} {
		if err := killGroup(pgid, syscall.SIGTERM); err == nil {
			t.Errorf("pgid=%d を撃とうとした (弾くべき)", pgid)
		}
	}
}

// shortenOnLostGrace は猶予を縮める。仕様値なので production の既定は動かさない。
func shortenOnLostGrace(t *testing.T, d time.Duration) func() {
	t.Helper()
	old := onLostGracePeriod
	onLostGracePeriod = d
	return func() { onLostGracePeriod = old }
}

// 🚨 更新が詰まったとき、tick ごとに goroutine を積まないこと。
//
// `with` は lockman で唯一の長寿命モードなので、包んだ `Renew` が毎 tick ブロックすると
// `withTimeout` が見捨てた goroutine が溜まり続ける (回収できない)。issue 359 の項目 4 が
// 「357 を実装したら上限を置くか決めろ」と trigger を残していた箇所。
//
// 判定は**時間ではなく本数**で行う。上限が無い版はこの条件で 10 本以上積む
// (tick 200ms x 子の寿命 3s)。上限が在れば 1 本。
func TestRenewDoesNotPileUpGoroutinesWhenBlocked(t *testing.T) {
	l, err := NewLocker(t.TempDir(), testIOTimeout)
	if err != nil {
		t.Fatalf("NewLocker: %v", err)
	}
	const ttl = 600 * time.Millisecond // tick = 200ms
	go func() {
		for range 400 {
			if _, err := os.Stat(l.lockPath()); err == nil {
				// 詰まらせる (消してから作ると errNotOwner を拾う窓ができるので rename で被せる)
				tmp := l.lockPath() + ".fifo"
				if err := syscall.Mkfifo(tmp, 0o600); err != nil {
					return
				}
				_ = os.Rename(tmp, l.lockPath())
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	before := runtime.NumGoroutine()
	// --on-lost warn (kill しない) にして、子が生きているあいだ tick を回し続けさせる
	got := boundedInt(t, "runWith (詰まった renew)", func() int {
		return runWith(l, ttl, "", false, []string{"sh", "-c", "sleep 3"})
	})
	if got != exitWithInvalid {
		t.Fatalf("exit %d (期待 %d = 判定不能)", got, exitWithInvalid)
	}
	leaked := runtime.NumGoroutine() - before
	// 見捨てるのは詰まった Renew 1 本だけ。余裕を見て 3 本までを許す
	if leaked > 3 {
		t.Fatalf("詰まった renew が %d 本の goroutine を積んだ (期待: 1 本。上限が効いていない)", leaked)
	}
}
