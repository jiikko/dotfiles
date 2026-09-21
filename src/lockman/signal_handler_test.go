package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// 🚨 **テスト全体で INT / TERM を握っておく** (issue 363)。
//
// このファイルのテストは**自分のプロセスへシグナルを撃つ**。`runWith` が `signal.Notify` を
// 立てる前に撃つと、修正が無い版では**既定処理でテストランナーごと死ぬ** (変異を当てた瞬間に
// スイートが消える = 変異検証ができない)。
//
// Go の `os/signal` は **登録された全チャネルへ配送**し、**1 つでも登録があれば既定処理が
// 無効になる**。テスト側でも登録しておけば ①変異を当ててもランナーは死なない
// ②`runWith` が登録していればそちらにも同じシグナルが届く = **「runWith が受け取れたか」
// だけが差として観測できる**。
//
// 🚨 **このため「即死しないこと」自体はここでは検査できない**。363 が (B) で変えないのは
// 取得中の窓だけで、ここで見るのは「**子が存在し始める瞬間から、シグナルを runWith が
// 受け取って転送するか**」。
//
// 🚨 `os.Exit` は defer を走らせないので `defer signal.Stop(guard)` は書かない
// (敵対レビュー P3-1。「後始末に見えて何もしない行」を置かない)。プロセスの寿命と同じ
// ライフタイムで持つのが意図。
func TestMain(m *testing.M) {
	guard := make(chan os.Signal, 8)
	signal.Notify(guard, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		for range guard { // 捨てるだけ。既定処理を無効にするのが目的
		}
	}()
	os.Exit(m.Run())
}

// 子が存在し始める瞬間から、シグナルを `runWith` が受け取って子へ転送すること (issue 363)。
//
// 旧版は `signal.Notify` が `cmd.Start()` の**後**にあったので、その窓のシグナルは既定処理で
// lockman を即死させ、**子は別プロセスグループで孤児として走り続けた** (出典の実測: 漏れ
// 20/120 のうち 9 件がこの形。全件 stdout 0B / stderr 0B の無言)。
//
// 🚨 **窓は `cmd.Start()` の直前で作る** (敵対レビュー P1-1)。seam を Start の**後ろ**に置くと
// 「Start と Notify のあいだ」に Notify を戻す変異 (= 同じ退行) が判別の外に落ち、**緑で通る**
// (実測)。Start の前なら判別境界が「子が存在し始める瞬間」に一致する。
func TestSignalIsHandledFromTheMomentChildExists(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		t.Run(sig.String(), func(t *testing.T) {
			l := newTestLocker(t)
			dir := t.TempDir()
			finished := filepath.Join(dir, "finished")

			// 🚨 **撃つだけでは足りない。配送されたことを確かめてから seam を抜ける**
			// (敵対レビュー前の実測)。`Kill` からランタイムがチャネルへ配送するまでは非同期なので、
			// 「撃って即 return」だと変異の窓が数 ns しかなく、配送が変異後の登録に間に合う =
			// 現行と変異がどちらも同じ結果になって区別できない。
			observed := make(chan os.Signal, 4)
			signal.Notify(observed, sig)
			t.Cleanup(func() { signal.Stop(observed) })

			var fired bool
			orig := beforeChildStartHook
			beforeChildStartHook = func() {
				if fired {
					return
				}
				fired = true
				_ = syscall.Kill(os.Getpid(), sig)
				select {
				case <-observed: // 配送済み = ここから後に登録しても受け取れない
				case <-time.After(10 * time.Second):
					t.Error("前提が作れていない: シグナルが配送されない")
				}
			}
			t.Cleanup(func() { beforeChildStartHook = orig })

			// 子はシグナルで死ぬ (trap しない)。転送されなければ 5 秒走り切って "finished" を書く
			rc := boundedInt(t, "runWith (Start 直前の "+sig.String()+")", func() int {
				return runWith(l, time.Hour, "", false,
					[]string{"sh", "-c", fmt.Sprintf("sleep 5; : > %q", finished)})
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
			if want := 128 + int(sig); rc != want {
				t.Errorf("rc=%d (期待 %d = 子がシグナルで死んだ)", rc, want)
			}
		})
	}
}

// 🚨 **既に無視されているシグナルは `Notify` に渡さない** (敵対レビュー 363 の P1-2)。
//
// 渡すと Go が自前ハンドラを入れ、`exec` で子の disposition が SIG_IGN → SIG_DFL に変わる
// (`nohup` / cron で端末が切れたとき、以前は生き延びたジョブが死ぬ)。
// 🚨 **空になったら `Notify` を呼んではいけない** — `signal.Notify(c)` は**シグナルを 1 つも
// 渡さないと全シグナルを中継する**ので、意味が反転する。
func TestNotifiableSignalsRespectsInheritedIgnore(t *testing.T) {
	all := func(os.Signal) bool { return true }
	none := func(os.Signal) bool { return false }
	onlyHUPandINT := func(s os.Signal) bool { return s == syscall.SIGHUP || s == syscall.SIGINT }

	for _, tc := range []struct {
		name    string
		ignored func(os.Signal) bool
		want    []os.Signal
	}{
		{"何も無視されていない = 3 つとも渡す", none,
			[]os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP}},
		// `nohup` / cron で端末が切れる形。TERM だけ渡す (Go は TERM の SIG_IGN を尊重しない)
		{"HUP と INT が継承 SIG_IGN = TERM だけ渡す", onlyHUPandINT,
			[]os.Signal{syscall.SIGTERM}},
		// 🚨 ここが空になったとき `signal.Notify` を呼ぶと**全シグナルを中継する**ので、
		// 呼び出し側の `len(sigs) > 0` ガードと対で意味を持つ
		{"全部無視されている = 1 つも渡さない", all, nil},
	} {
		got := notifiableSignals(tc.ignored)
		if len(got) != len(tc.want) {
			t.Errorf("%s: %v (期待 %v)", tc.name, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s: %v (期待 %v)", tc.name, got, tc.want)
				break
			}
		}
	}
}
