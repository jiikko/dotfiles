package main

import (
	"errors"
	"os"
	"testing"
	"time"
)

// 🚨 **照合してから書くまでのあいだに引き継がれても、別人の lock を上書きしないこと** (issue 380)。
//
// 旧版の `Renew` は `readLock` で照合してから **名前で開き直して** `O_TRUNC` + write していた。
// そのあいだに引き継ぎが挟まると、**別人の lock を自分のメタで上書き**し、しかも **nil (成功) を
// 返す**。被害は上書きだけではない: 上書きされた側の次の `Renew` は errNotOwner になり、
// 既定の `--on-lost=kill` が**健全な子を SIGTERM/SIGKILL する**。
//
// いまは照合と書き込みが**同じ fd** に対して行われるので、名前がすり替わっても当たるのは
// 自分が確かめた実体だけ = 窓が縮むのではなく**構造的に消える**。
func TestRenewDoesNotOverwriteLockTakenOverMidway(t *testing.T) {
	l := newTestLocker(t)
	mine, err := l.Acquire(time.Minute, "mine")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	other, err := NewLocker(l.dir, 5*time.Second)
	if err != nil {
		t.Fatalf("NewLocker(other): %v", err)
	}

	var taken *Meta
	fired := false
	old := renewBeforeWriteHook
	renewBeforeWriteHook = func() {
		if fired {
			return
		}
		fired = true
		// 照合を通った後・書く前に、引き継ぎが起きる (break + 別ホストの取得で決定論にする)
		if err := l.Break(); err != nil {
			t.Errorf("Break: %v", err)
			return
		}
		m, err := other.Acquire(time.Hour, "other-host-job")
		if err != nil {
			t.Errorf("other.Acquire: %v", err)
			return
		}
		taken = m
	}
	defer func() { renewBeforeWriteHook = old }()

	_ = l.Renew(mine.Token) // 成功しても失敗してもよい。見るのは lock の中身
	if !fired {
		t.Fatalf("前提: 照合を通っていない (seam が呼ばれていない)")
	}
	if taken == nil {
		t.Fatalf("前提: 引き継ぎが成立していない")
	}
	m, _, err := l.readLock()
	if err != nil {
		t.Fatalf("readLock: %v", err)
	}
	if m == nil {
		t.Fatalf("引き継いだ側の lock が壊れている (中身を読めない)")
	}
	if m.Token != taken.Token {
		t.Fatalf("引き継いだ側の lock を上書きした (二重実行になる): got=%s want=%s", m.Token, taken.Token)
	}
}

// 🚨 **照合してから消すまでのあいだに引き継がれても、別人の lock を消さないこと** (issue 380)。
//
// 旧版の `Release` は `os.Remove(l.lockPath())` で、照合から消すまでのあいだに引き継がれると
// **別人の lock を消し**、nil (成功) を返していた。消えた直後に第三者が acquire できるので
// **二重実行に直結する**。
//
// 🚨 ここは `Renew` と違い**構造的には閉じられない** (unlink は名前でしか撃てず、
// 「この inode を消す」原始操作は macOS に無い)。窓は「照合 → Remove」に縮むだけ。
func TestReleaseDoesNotRemoveLockTakenOverMidway(t *testing.T) {
	l := newTestLocker(t)
	mine, err := l.Acquire(time.Minute, "mine")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	other, err := NewLocker(l.dir, 5*time.Second)
	if err != nil {
		t.Fatalf("NewLocker(other): %v", err)
	}

	var taken *Meta
	fired := false
	old := releaseBeforeRemoveHook
	releaseBeforeRemoveHook = func() {
		if fired {
			return
		}
		fired = true
		if err := l.Break(); err != nil {
			t.Errorf("Break: %v", err)
			return
		}
		m, err := other.Acquire(time.Hour, "other-host-job")
		if err != nil {
			t.Errorf("other.Acquire: %v", err)
			return
		}
		taken = m
	}
	defer func() { releaseBeforeRemoveHook = old }()

	rerr := l.Release(mine.Token)
	if !fired {
		t.Fatalf("前提: 照合を通っていない (seam が呼ばれていない)")
	}
	if taken == nil {
		t.Fatalf("前提: 引き継ぎが成立していない")
	}
	// 引き継いだ側の lock が**残っている**こと
	m, _, err := l.readLock()
	if err != nil {
		t.Fatalf("readLock: %v", err)
	}
	if m == nil {
		t.Fatalf("引き継いだ側の lock を消した (直後に第三者が取れる = 二重実行)")
	}
	if m.Token != taken.Token {
		t.Fatalf("引き継いだ側の lock が別物に置き換わっている: got=%s want=%s", m.Token, taken.Token)
	}
	// 呼び出し側には「自分のものではない」と伝わること (nil = 成功で返さない)
	if !errors.Is(rerr, errNotOwner) {
		t.Fatalf("引き継がれていたのに errNotOwner を返していない: %v", rerr)
	}
}

// 対照: 誰も割り込まなければ、Renew は打刻し Release は消す (上の 2 本が
// 「常に何もしない」へ倒れていないこと)。
func TestRenewAndReleaseStillWorkWithoutInterference(t *testing.T) {
	l := newTestLocker(t)
	mine, err := l.Acquire(time.Minute, "mine")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	before, err := os.Stat(l.lockPath())
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	// mtime が動いたことを見たいので、打刻の粒度を跨ぐために過去へ打っておく
	past := time.Now().Add(-3 * time.Second)
	if err := os.Chtimes(l.lockPath(), past, past); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	if err := l.Renew(mine.Token); err != nil {
		t.Fatalf("Renew: %v", err)
	}
	after, err := os.Stat(l.lockPath())
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !after.ModTime().After(past) {
		t.Fatalf("Renew が mtime を打刻していない: %v", after.ModTime())
	}
	if after.Size() != before.Size() {
		t.Fatalf("Renew が中身の長さを変えた: %d → %d", before.Size(), after.Size())
	}
	m, _, err := l.readLock()
	if err != nil || m == nil || m.Token != mine.Token {
		t.Fatalf("Renew の後に自分の lock が読めない: m=%v err=%v", m, err)
	}
	if err := l.Release(mine.Token); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, err := os.Stat(l.lockPath()); !os.IsNotExist(err) {
		t.Fatalf("Release の後も lock が残っている: %v", err)
	}
}

// 🚨 **Renew は 0 バイトの窓を開かないこと** (issue 383 の発生源を消す)。
//
// 旧版は `O_TRUNC` で開いてから write していたので、そのあいだ lock は 0 バイトで、
// 読み手には「中身を読めない lock」として見えた (fail-closed 側で引き継ぎが止まる)。
func TestRenewNeverLeavesLockEmpty(t *testing.T) {
	l := newTestLocker(t)
	mine, err := l.Acquire(time.Minute, "mine")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	// 「照合を通った後・書く前」に読み手が居たら何が見えるか
	var sawSize int64 = -1
	fired := false
	old := renewBeforeWriteHook
	renewBeforeWriteHook = func() {
		if fired {
			return
		}
		fired = true
		if fi, err := os.Stat(l.lockPath()); err == nil {
			sawSize = fi.Size()
		}
	}
	defer func() { renewBeforeWriteHook = old }()

	if err := l.Renew(mine.Token); err != nil {
		t.Fatalf("Renew: %v", err)
	}
	if !fired {
		t.Fatalf("前提: seam が呼ばれていない")
	}
	if sawSize <= 0 {
		t.Fatalf("書く前の時点で lock が %d バイトになっている (0 バイトの窓が開いている)", sawSize)
	}
}
