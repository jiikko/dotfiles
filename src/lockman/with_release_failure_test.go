package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// issue 364 の 1: `with` は「確実に解放」を謳うが、解放に失敗しても子の rc を透過していたため
// **rc だけを見る呼び出し側からは成功に見えた** (lock は TTL 切れまで残る)。
//
// 🚨 rc と warn の**両方**を同じケースで見ること。rc だけを見ると「warn は出るが rc は
// 上書きしない実装」が緑で通り、warn だけを見ると「rc を上書きしない実装」が緑で通る。
//
// 対照を 2 本置く (どちらが欠けても「常に 125 を返す」実装が緑で通る):
//   - 解放が成功する正常系で rc=0 のまま (上書きが常時発火していない)
//   - 子が非 0 のとき rc は子のまま (透過が壊れていない)
func TestWithReportsReleaseFailureInExitCode(t *testing.T) {
	t.Run("child_ok_release_fails", func(t *testing.T) {
		l := newTestLocker(t)
		var rc int
		stderr := captureStderr(t, func() {
			// 子が走っているあいだに probe を壊す。`sh -c` の中で自分で壊すのが
			// いちばん確実 (解放は子の終了後なので、この順序で必ず窓に入る)。
			probe := filepath.Join(l.metaDir, probeDirName)
			rc = runWith(l, time.Minute, "", false,
				[]string{"sh", "-c", "chmod 500 '" + probe + "'; exit 0"})
		})
		t.Cleanup(func() { _ = os.Chmod(filepath.Join(l.metaDir, probeDirName), 0o700) })

		// 🚨 前提: 解放が実際に失敗したこと。失敗していなければ以降の assert は
		// 何も守らない (「上書きを外す」変異が緑で通る)。
		if !strings.Contains(stderr, "解放に失敗:") {
			t.Fatalf("前提崩れ: 解放が失敗していない。stderr=%q", stderr)
		}
		// lock が実際に残っていること (これが実害そのもの)
		if _, err := os.Stat(filepath.Join(l.metaDir, lockName)); err != nil {
			t.Fatalf("前提崩れ: lock が残っていない (解放に失敗したのに): %v", err)
		}

		if rc != exitWithInvalid {
			t.Fatalf("rc=%d (期待 %d = 解放に失敗したので成功を返さない)", rc, exitWithInvalid)
		}
		// 呼び出し側が次に何を見ればよいかまで出す。文面は完全一致で pin しない代わりに、
		// **偽の断定を足す変異**を捕まえるために「何が起きたか」の語を両方見る。
		if !strings.Contains(stderr, "子は成功したがロックを解放できていない") {
			t.Fatalf("解放できていない事実を伝えていない: %q", stderr)
		}
		if !strings.Contains(stderr, "TTL が切れるまで待たされる") {
			t.Fatalf("次の実行への影響を伝えていない: %q", stderr)
		}
	})

	t.Run("control_release_ok_keeps_zero", func(t *testing.T) {
		l := newTestLocker(t)
		var rc int
		stderr := captureStderr(t, func() {
			rc = runWith(l, time.Minute, "", false, []string{"sh", "-c", "exit 0"})
		})
		if rc != exitOK {
			t.Fatalf("rc=%d (期待 %d = 正常系は透過のまま)", rc, exitOK)
		}
		if strings.Contains(stderr, "解放に失敗") {
			t.Fatalf("正常系で解放に失敗している: %q", stderr)
		}
	})

	t.Run("control_child_failure_is_transparent", func(t *testing.T) {
		l := newTestLocker(t)
		var rc int
		stderr := captureStderr(t, func() {
			probe := filepath.Join(l.metaDir, probeDirName)
			// 解放は失敗させたうえで、子は 7 で終わる。**子の rc を透過する**ことを見る
			// (ここが 125 になると「非 0 も塗り潰す」実装になっており、情報が減る)。
			rc = runWith(l, time.Minute, "", false,
				[]string{"sh", "-c", "chmod 500 '" + probe + "'; exit 7"})
		})
		t.Cleanup(func() { _ = os.Chmod(filepath.Join(l.metaDir, probeDirName), 0o700) })

		if !strings.Contains(stderr, "解放に失敗:") {
			t.Fatalf("前提崩れ: 解放が失敗していない。stderr=%q", stderr)
		}
		if rc != 7 {
			t.Fatalf("rc=%d (期待 7 = 子の終了コードを透過する)", rc)
		}
	})

	t.Run("control_not_owner_is_not_an_error", func(t *testing.T) {
		// 既に他者が引き継いでいる (= 解放すべきものが無い) ときは、②の契約は破れて
		// いないので rc を上げない。ここを広げると、正常な引き継ぎで 125 が出る。
		l := newTestLocker(t)
		var rc int
		stderr := captureStderr(t, func() {
			lockPath := filepath.Join(l.metaDir, lockName)
			rc = runWith(l, time.Minute, "", false,
				[]string{"sh", "-c", "rm -f '" + lockPath + "'; exit 0"})
		})
		if rc != exitOK {
			t.Fatalf("rc=%d (期待 %d = 持ち主でないだけなら上書きしない)。stderr=%q", rc, exitOK, stderr)
		}
	})
}
