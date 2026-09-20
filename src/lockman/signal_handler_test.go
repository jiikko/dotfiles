package main

import (
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// 🚨 **テスト全体で SIGTERM を握っておく** (issue 363)。
//
// このファイルのテストは**自分のプロセスへ TERM を撃つ**。`runWith` が `signal.Notify` を
// 立てる前に撃つと、修正が無い版では**既定処理でテストランナーごと死ぬ** (変異を当てた瞬間に
// スイートが消える = 変異検証ができない)。
//
// Go の `os/signal` は **登録された全チャネルへ配送**し、**1 つでも登録があれば既定処理が
// 無効になる**。そこでテスト側でも登録しておけば:
//   - 変異 (`signal.Notify` を `cmd.Start()` の後ろへ戻す) を当ててもランナーは死なない
//   - `runWith` が登録していれば、そちらの `sigCh` にも同じシグナルが届く
//
// = **「runWith が受け取れたか」だけが差として観測できる**。
//
// 🚨 **このため「即死しないこと」自体はここでは検査できない** (テスト側の登録が既定処理を
// 潰すので、修正の有無で死ぬ / 死なないの差が出ない)。363 が (B) で変えないのは取得中の窓
// だけで、ここで見るのは「**子を起こした後のシグナルを runWith が受け取って転送するか**」。
func TestMain(m *testing.M) {
	guard := make(chan os.Signal, 8)
	signal.Notify(guard, syscall.SIGTERM)
	defer signal.Stop(guard)
	go func() {
		for range guard { // 捨てるだけ。既定処理を無効にするのが目的
		}
	}()
	os.Exit(m.Run())
}

// 子を起こした直後に届いたシグナルを、`runWith` が受け取って子へ転送すること (issue 363)。
//
// 旧版は `signal.Notify` が `cmd.Start()` の**後**にあったので、この窓のシグナルは
// 既定処理で lockman を即死させ、**子は別プロセスグループで孤児として走り続けた**
// (出典の実測: 漏れ 20/120 のうち 9 件がこの形。全件 stdout 0B / stderr 0B の無言)。
//
// 窓は決定論で作る: `afterChildStartHook` (Start の直後・select ループの前) で自分へ TERM を撃つ。
func TestSignalIsHandledFromTheMomentChildExists(t *testing.T) {
	l := newTestLocker(t)
	dir := t.TempDir()
	started := filepath.Join(dir, "started")
	finished := filepath.Join(dir, "finished")

	// 🚨 **撃つだけでは足りない。配送されたことを確かめてから seam を抜ける**。
	// `signal.Kill` から Go ランタイムがチャネルへ配送するまでは非同期なので、
	// 「撃って即 return」だと**変異 (Notify を seam の直後へ置く) の窓が数 ns しかなく**、
	// 配送が変異後の登録に間に合ってしまう = 変異が緑で通る (実測 2026-09-21: 現行と変異が
	// どちらも rc=143 / 子が死ぬ で区別できなかった)。
	// ここで「テスト側のチャネルが受け取った」まで待てば、**seam を抜けた時点で配送は完了**して
	// おり、その後に登録する実装 (= 旧版) は原理的に受け取れない。
	observed := make(chan os.Signal, 4)
	signal.Notify(observed, syscall.SIGTERM)
	t.Cleanup(func() { signal.Stop(observed) })

	orig := afterChildStartHook
	var fired bool
	afterChildStartHook = func() {
		if fired {
			return
		}
		fired = true
		// 子が起動して "started" を書くまで待ってから撃つ (撃つ前に子が居ないと、
		// 何を転送したのかが観測できない)
		if !waitForCondition(t, 10*time.Second, func() bool {
			_, err := os.Stat(started)
			return err == nil
		}) {
			t.Error("前提が作れていない: 子が起動しない")
			return
		}
		_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
		select {
		case <-observed: // 配送済み = ここから後に登録しても受け取れない
		case <-time.After(10 * time.Second):
			t.Error("前提が作れていない: シグナルが配送されない")
		}
	}
	t.Cleanup(func() { afterChildStartHook = orig })

	// 子は TERM で死ぬ (trap しない)。転送されなければ 5 秒走り切って "finished" を書く
	rc := boundedInt(t, "runWith (Start 直後の TERM)", func() int {
		return runWith(l, time.Hour, "", false,
			[]string{"sh", "-c", ": > " + started + "; sleep 5; : > " + finished})
	})

	if !fired {
		t.Fatal("前提が作れていない: seam が発火していない")
	}
	if _, err := os.Stat(finished); err == nil {
		t.Fatalf("シグナルが子へ転送されていない (子が最後まで走り切った。rc=%d)。"+
			"`signal.Notify` が子を起こした後だと、この窓のシグナルは既定処理へ流れる", rc)
	}
	// 🚨 lock が残らないこと。旧版の漏れの本体はこちら (孤児の子 + lock 残存)
	if _, err := os.Stat(l.lockPath()); err == nil {
		t.Fatalf("lock が残っている (rc=%d)", rc)
	}
	// 子はシグナルで死ぬので 128+15
	if want := 128 + int(syscall.SIGTERM); rc != want {
		t.Errorf("rc=%d (期待 %d = 子が SIGTERM で死んだ)", rc, want)
	}
}
