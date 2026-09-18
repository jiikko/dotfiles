package main

import (
	"fmt"
	"os"
	"path/filepath"
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
	sweepPauseHook.Store(&pause)
	t.Cleanup(func() { sweepPauseHook.Store(nil) })

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
		// 報告と実態が食い違っていないこと (報告だけが正しい形を防ぐ)
		left := countDirEntries(t, filepath.Join(l.metaDir, tmpDirName))
		if got := residue - left; got < res.Removed {
			t.Errorf("報告 (%d) が実際に消えた数 (%d) を上回っている", res.Removed, got)
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
	if !strings.Contains(out, "removed=判定不能") {
		t.Fatalf("0 件の期限切れを判定不能として出していない: %q", out)
	}
	for _, bad := range []string{"次回の続きから減る", "removed>=0"} {
		if strings.Contains(out, bad) {
			t.Fatalf("進捗があると断定している (%q): %q", bad, out)
		}
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
	if !strings.Contains(out, "removed>=1") {
		t.Fatalf("期限切れの件数が「少なくとも」と読める形で出ていない: %q", out)
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
