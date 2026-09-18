package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// 期限切れした掃除は「どこまで進んだか」を報告すること (issue 393)。
//
// 🚨 旧版は `withTimeout` が返したエラーだけを見て `CleanupResult{Errors: ...}` を返しており、
// **実際には数千件を消しているのに `removed=0`** と報告していた (実測 2026-09-18: 残骸 10,000 件で
// 3 回連続 `removed=0`、実数は毎回 2,400〜2,500 件)。`dispatch` のコメントは
// 「部分的に進んでいるのか何も進んでいないのかは**失敗している場面でこそ知りたい**」と
// 約束しているのに、その唯一の場面で数字が嘘になっていた。
//
// 詰まる位置は `.cleanup_at` (FIFO)。sweep は**その前に**終わるので、
// 「消した件数 > 0 なのに期限切れ」という状態を決定論的に作れる。
func TestCleanupTimedReportsPartialProgress(t *testing.T) {
	l, err := NewLocker(t.TempDir(), testIOTimeout)
	if err != nil {
		t.Fatalf("NewLocker: %v", err)
	}
	if err := l.ensureDirs(); err != nil {
		t.Fatalf("ensureDirs: %v", err)
	}

	// retention を確実に超えた残骸を tmp/ へ置く
	const residue = 20
	old := time.Now().Add(-(scratchRetention + time.Hour))
	for i := range residue {
		p := filepath.Join(l.metaDir, tmpDirName, fmt.Sprintf("stale-%d", i))
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatalf("Chtimes: %v", err)
		}
	}

	// 🚨 **窓の内側で止める**。`.cleanup_at` を FIFO にする形 (sweep の**後**で詰まる) だと、
	// 「sweep の途中で期限切れ」を構造的に再現できず、**カウンタを一括で足す変異
	// (= issue 393 の症状そのもの) が全スイート緑で通る** (敵対レビュー P1-1 が実測)。
	// ここでは 1 件消すごとに待たせ、期限内に一部しか進めない状態を作る。
	pause := func() { time.Sleep(testIOTimeout / 4) }
	hook := &sweepPauseHook
	hook.Store(&pause)
	t.Cleanup(func() { hook.Store(nil) })

	ch := make(chan CleanupResult, 1)
	go func() { ch <- l.CleanupTimed(true) }()
	select {
	case res := <-ch:
		if !hasTimeoutError(res) {
			t.Fatalf("掃除のタイムアウトが Errors に入っていない: %+v", res)
		}
		if !res.Partial {
			t.Errorf("期限切れなのに Partial が false (呼び出し側が「これで全部」と読む): %+v", res)
		}
		// 🚨 **「0 でない」と「全件でない」の両方**を見る。前者だけだと部分結果を捨てる旧挙動しか
		// 検出できず、後者だけだと「常に 0」でも通る。両方で「途中経過を報告している」を固定する。
		if res.Removed == 0 {
			t.Errorf("期限切れ時に進捗を捨てている: removed=0 (途中まで消しているのに 0 と報告)")
		}
		if res.Removed >= residue {
			t.Errorf("途中で止まっていない (removed=%d / 全 %d 件)。窓を再現できていないので"+
				"このテストは一括カウントの変異を検出できない", res.Removed, residue)
		}
		// 🚨 **見捨てた goroutine が終わるのを待ってからテストを終える** (2 周目 P1-2)。
		// 待たないと、`t.Cleanup` の `TempDir` 削除と、掃除を続けている goroutine の
		// **`stampCleanup` による `.cleanup_at` の再作成**が競合し、
		// `TempDir RemoveAll cleanup: ... directory not empty` で**無関係な赤**が出る
		// (レビュー実測: 並行負荷の高い環境で `go test -race ./...` 8 回中 2 回。
		//  こちらの環境では 16 回中 0 回で再現しなかったが、機構は成立している)。
		// 待つのは時間ではなく**成立条件** (`avoid-wall-clock-assertions.md`)。
		hook.Store(nil) // 残りは全速で消させる
		waitFor(t, "掃除が終わる", func() bool {
			_, err := os.Stat(filepath.Join(l.metaDir, cleanupStampName))
			return err == nil
		})

		// 報告と実態が食い違っていないこと。**待った後**なので実態は確定している
		// (待つ前に数えると、見捨てた goroutine が消し続けるぶん assert が
		//  成立する方向へ動き続け、ほとんど何も検査しない。2 周目 P3-2)
		if left := countDirEntries(t, filepath.Join(l.metaDir, tmpDirName)); left != 0 {
			t.Errorf("掃除が終わったのに %d 件残っている", left)
		}
		if res.Removed >= residue {
			t.Errorf("報告 (%d) が「途中経過」になっていない (全 %d 件)", res.Removed, residue)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("CleanupTimed が 20s 以内に戻らない (包みが無い)")
	}
}

// 期限切れで 0 件のときは「進んでいる」と言わないこと (敵対レビュー 393 の P2-2)。
//
// 🚨 「期限切れ + 0 件」は**判定不能**で、「排水中だから待てばよい」でも
// 「1 件も進まない」でもない。旧文面は「掃除は途中で、次回の続きから減る」と
// **偽の肯定**を足しており、issue 393 が動機に挙げた誤診 (「掃除が進んでいない」→ `rm -rf`) を
// 逆向きに作っていた (「進んでいるから待とう」)。
func TestCleanupCLISaysIndeterminateWhenNothingRemoved(t *testing.T) {
	l := fifoLocker(t, stampFile) // 残骸ゼロ。打刻で詰まる
	var out string
	done := make(chan struct{})
	go func() {
		out = captureStderr(t, func() {
			run([]string{"cleanup", l.dir, "--io-timeout", testIOTimeout.String()})
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("cleanup が 20s 以内に戻らない")
	}
	// 🚨 **行の構造を丸ごと固定する** (2 周目 P2-1)。部分一致 + 「悪い語」の否定リストでは、
	// ①同じ偽の肯定を**別の言い回し**で書けば素通りし ②否定リストの語は
	// production から消えているので**一字一句戻したときしか鳴らない**。
	// `main_test.go` の `TestDispatchWarnsOnCleanupFailureWithoutVerbose` が
	// 既に同じ規律 (「部分一致で pin しない」) を書いており、新設した 2 つの書式だけが
	// その外に置かれていた。
	want := regexp.MustCompile(
		`^lockman: cleanup: removed=判定不能 \(期限切れ。1 件も消せていないのか、消している途中なのかは分からない\) ` +
			`skipped=(?:true|false) errors=\[.*I/O timeout.*\]\n$`)
	if !want.MatchString(out) {
		t.Fatalf("0 件の期限切れの書式が違う:\n got=%q\nwant=%s", out, want)
	}
}

// sweep 中に起きたエラーも期限切れで捨てないこと (敵対レビュー 393 の P2-3)。
//
// 🚨 エラーは「排水中」と「権限ドリフトで恒久的に詰まっている」を分ける情報で、
// issue 393 の動機 2 に対しては件数より効く。旧版は期限切れ時に `CleanupResult` を
// 組み立て直してタイムアウト 1 本だけを入れており、sweep 中のエラーを捨てていた。
func TestCleanupTimedKeepsSweepErrorsOnTimeout(t *testing.T) {
	l, err := NewLocker(t.TempDir(), testIOTimeout)
	if err != nil {
		t.Fatalf("NewLocker: %v", err)
	}
	if err := l.ensureDirs(); err != nil {
		t.Fatalf("ensureDirs: %v", err)
	}
	old := time.Now().Add(-(graveyardRetention + time.Hour))
	stale := func(dir, name string) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatalf("Chtimes: %v", err)
		}
	}

	// sweep の順は tmp → probe → graveyard。
	// **tmp で消せない残骸** (エラーを作る) → **graveyard で消せる残骸** (そこで期限切れにする)
	tmpDir := filepath.Join(l.metaDir, tmpDirName)
	stale(tmpDir, "unremovable")
	graveDir := filepath.Join(l.metaDir, graveyardDirName)
	for i := range 20 {
		stale(graveDir, fmt.Sprintf("old-%d", i))
	}
	if err := os.Chmod(tmpDir, 0o555); err != nil { // 親の書き込みを落として remove を失敗させる
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(tmpDir, metaDirMode) })

	pause := func() { time.Sleep(testIOTimeout / 4) }
	sweepPauseHook.Store(&pause)
	t.Cleanup(func() { sweepPauseHook.Store(nil) })

	ch := make(chan CleanupResult, 1)
	go func() { ch <- l.CleanupTimed(true) }()
	select {
	case res := <-ch:
		if !res.Partial || !hasTimeoutError(res) {
			t.Fatalf("期限切れとして返っていない: %+v", res)
		}
		var sweepErrs int
		for _, e := range res.Errors {
			if !strings.HasPrefix(e, errIOTimeout.Error()) {
				sweepErrs++
			}
		}
		if sweepErrs == 0 {
			t.Fatalf("sweep 中のエラーを捨てている (タイムアウト 1 本だけ): %+v", res.Errors)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("CleanupTimed が 20s 以内に戻らない")
	}
}

func hasTimeoutError(res CleanupResult) bool {
	for _, e := range res.Errors {
		if strings.HasPrefix(e, errIOTimeout.Error()) {
			return true
		}
	}
	return false
}

// 対照: 期限切れしていない掃除は Partial を立てない (「常に部分的」へ倒れていないこと)。
func TestCleanupMarksCompleteRunAsNotPartial(t *testing.T) {
	l := newTestLocker(t)
	if err := l.ensureDirs(); err != nil {
		t.Fatalf("ensureDirs: %v", err)
	}
	p := filepath.Join(l.metaDir, tmpDirName, "stale")
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	old := time.Now().Add(-(scratchRetention + time.Hour))
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	res := l.CleanupTimed(true)
	if len(res.Errors) != 0 {
		t.Fatalf("CleanupTimed: %v", res.Errors)
	}
	if res.Partial {
		t.Errorf("完走した掃除に Partial が立っている (「少なくとも N 件」と読ませてしまう): %+v", res)
	}
	if res.Removed != 1 {
		t.Errorf("removed=%d (1 件を期待)", res.Removed)
	}
}

// CLI の出口でも意味の違いが出ること。`removed=N` と `removed>=N` は
// 「打ち止め」と「途中」で読み方が変わるので、同じ書式にしない。
func TestCleanupCLIDistinguishesPartialFromComplete(t *testing.T) {
	l := fifoLocker(t, stampFile)
	p := filepath.Join(l.metaDir, tmpDirName, "stale")
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	old := time.Now().Add(-(scratchRetention + time.Hour))
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	var out string
	done := make(chan struct{})
	go func() {
		out = captureStderr(t, func() {
			run([]string{"cleanup", l.dir, "--io-timeout", testIOTimeout.String()})
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("cleanup が 20s 以内に戻らない")
	}
	// 🚨 こちらも**完全一致**。部分一致 (`Contains(out, "removed>=1")`) だと、
	// 括弧の中身に「掃除は途中で、次回の続きから減る」のような**偽の断定**を戻す変異が
	// 素通りする (2 周目 P2-1 が実測: その変異は全スイート緑だった)。
	want := regexp.MustCompile(
		`^lockman: cleanup: removed>=\d+ \(期限切れ。ここまでは確認済み\) ` +
			`skipped=(?:true|false) errors=\[.*I/O timeout.*\]\n$`)
	if !want.MatchString(out) {
		t.Fatalf("期限切れ (1 件以上) の書式が違う:\n got=%q\nwant=%s", out, want)
	}
}

func countDirEntries(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	return len(entries)
}

// waitFor は成立条件を上限つきでポーリングする (壁時計で待たない)。
func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%s のを 10 秒待っても成立しない", what)
}
