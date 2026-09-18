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
	l := fifoLocker(t, stampFile)

	// retention を確実に超えた残骸を tmp/ へ置く
	const residue = 5
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

	ch := make(chan CleanupResult, 1)
	go func() { ch <- l.CleanupTimed(true) }()
	select {
	case res := <-ch:
		if len(res.Errors) != 1 || !strings.HasPrefix(res.Errors[0], errIOTimeout.Error()) {
			t.Fatalf("掃除のタイムアウトが Errors に入っていない: %+v", res)
		}
		if !res.Partial {
			t.Errorf("期限切れなのに Partial が false (呼び出し側が「これで全部」と読む): %+v", res)
		}
		if res.Removed != residue {
			t.Errorf("期限切れ時に進捗を捨てている: removed=%d (実際に消えた %d 件を期待)",
				res.Removed, residue)
		}
		// 実際に消えていることを FS 側でも確かめる (報告だけが正しい形を防ぐ)
		left := countDirEntries(t, filepath.Join(l.metaDir, tmpDirName))
		if left != 0 {
			t.Errorf("tmp/ に %d 件残っている (報告と実態が食い違う)", left)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("CleanupTimed が 20s 以内に戻らない (包みが無い)")
	}
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
