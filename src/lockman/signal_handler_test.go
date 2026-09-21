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

// 🚨 **`TestMain` の guard は置かない** (敵対レビュー 363 の 2 周目 P3-2)。
//
// 1 周目は「自分へシグナルを撃つので、修正が無い版ではランナーごと死ぬ」対策として
// `TestMain` で INT / TERM を `signal.Notify` していたが、**subtest 側が `runWith` を呼ぶ前に
// 自分で登録している**ので既定処理は既に無効で、guard は**冗長**だった。
// 害の方が大きい: guard があると**このテストバイナリが Ctrl-C でも TERM でも止まらなくなり、
// 中断に SIGKILL が要る** (レビュー実測)。
// 削除しても ①全スイート緑 ②変異 A で 2 subtest とも red ③ランナーは生存 を確認済み。

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
	// 🚨 production が中継するのは INT / TERM / **HUP** の 3 つ (`notifiableSignals`)。
	// 1 周目に「主題は Ctrl-C = INT なのに TERM しか通していない」を採用したのと**同じ論拠**で
	// HUP も通す (2 周目 P3-3)。
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP} {
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

// 🚨 **`signal.Stop` は解放より"後"に走ること** (敵対レビュー 363 の 2 周目 P2-1)。
//
// 1 周目は「死を再現できないので in-process では pin できない」と記録したが、**守りたい不変条件は
// 順序であって死ではない**。`signalStop` の指標を差し替えれば、Stop が呼ばれた瞬間に
// **lock がもう消えているか**を見るだけで決定論的に固定できる。
// (Stop が先に走ると既定処理が戻り、解放中の TERM/INT がプロセスを殺して lock が残る =
// issue 363 が漏れと定義した signature そのもの。レビューが out-of-process で 3/3 再現)
func TestSignalStopRunsAfterRelease(t *testing.T) {
	l := newTestLocker(t)
	orig := signalStop
	var called, lockAliveAtStop bool
	signalStop = func(c chan<- os.Signal) {
		called = true
		_, err := os.Stat(l.lockPath())
		lockAliveAtStop = err == nil
		orig(c)
	}
	t.Cleanup(func() { signalStop = orig })

	rc := boundedInt(t, "runWith (Stop の順序)", func() int {
		return runWith(l, time.Hour, "", false, []string{"sh", "-c", "exit 0"})
	})
	if !called {
		t.Fatal("前提が作れていない: signalStop が呼ばれていない")
	}
	// rc の条件は「解放が別の理由で失敗した」ケースを弾くため (偽陽性よけ)
	if lockAliveAtStop && rc == exitOK {
		t.Fatalf("signal.Stop が解放より先に走っている (rc=%d)。既定処理が戻るので、"+
			"解放中の TERM/INT がプロセスを殺して lock が残る", rc)
	}
}

// 🚨 **全部無視されている環境では `signal.Notify` を呼ばないこと** (2 周目 P2-1)。
//
// シグナルを 1 つも渡さない `Notify` は**全シグナルを中継する**ので、意味が反転して
// SIGURG のような内部シグナルまで子へ転送しにいく。production に指標を増やさずに固定できるよう、
// 配線を `installSignalHandler` へ切り出してある。
func TestInstallSignalHandlerSkipsNotifyWhenAllIgnored(t *testing.T) {
	ch := make(chan os.Signal, 1)
	for _, tc := range []struct {
		name      string
		ignored   func(os.Signal) bool
		wantCalls [][]os.Signal
	}{
		{"全部無視 = 呼ばない", func(os.Signal) bool { return true }, nil},
		{"何も無視されていない = 3 つ渡す", func(os.Signal) bool { return false },
			[][]os.Signal{{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP}}},
		{"HUP/INT だけ無視 = TERM だけ渡す",
			func(s os.Signal) bool { return s == syscall.SIGHUP || s == syscall.SIGINT },
			[][]os.Signal{{syscall.SIGTERM}}},
	} {
		var calls [][]os.Signal
		// 🚨 本物の Notify へ委譲しない。空で呼ぶと全シグナルを中継してしまい、
		// 変異を当てた瞬間にテストプロセスが内部シグナルで溢れる
		installSignalHandler(ch, tc.ignored, func(_ chan<- os.Signal, sigs ...os.Signal) {
			calls = append(calls, sigs)
		})
		if len(calls) != len(tc.wantCalls) {
			t.Errorf("%s: 呼び出し %v (期待 %v)", tc.name, calls, tc.wantCalls)
			continue
		}
		for i := range calls {
			if len(calls[i]) != len(tc.wantCalls[i]) {
				t.Errorf("%s: %v (期待 %v)", tc.name, calls, tc.wantCalls)
				break
			}
			for j := range calls[i] {
				if calls[i][j] != tc.wantCalls[i][j] {
					t.Errorf("%s: %v (期待 %v)", tc.name, calls, tc.wantCalls)
					break
				}
			}
		}
	}
}
