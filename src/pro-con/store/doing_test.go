package store

import (
	"os"
	"testing"
	"time"

	"pro-con/card"
)

// 完了したカードには前の様子を足さない (完了してから dispatcher が次に集めるまでの間も、完了と「走っている」を並べない)。
func TestAttachSkipsDoneCard(t *testing.T) {
	d := Doing{At: time.Now(), Cards: map[string][]card.Doing{"C-001": {{Kind: card.DoingProcess, Text: "make test"}}}}
	running := card.Card{ID: "C-001", State: card.Running}
	d.Attach(&running)
	if len(running.Doing) != 1 {
		t.Fatalf("作業中のカードに足さない: %+v", running)
	}
	done := card.Card{ID: "C-001", State: card.Done}
	d.Attach(&done)
	if len(done.Doing) != 0 || !done.DoingAt.IsZero() {
		t.Fatalf("完了したカードに足した: %+v", done)
	}
}

// 完了したカードには worktree の場所だけ足す (issue 508。取り込み済みの branch の commit・差分は古い)。
func TestDerivedAttachDoneCardLocationOnly(t *testing.T) {
	now := time.Now()
	d := Derived{Progress: Progress{At: now, Cards: map[string]card.Progress{"C-001": {Worktree: "/r/.claude/worktrees/pc-c-001", NoWorktree: true,
		Ahead: 3, Commits: []card.Commit{{Hash: "a1"}}, Diff: &card.DiffSummary{Files: 1}}}}}
	done := card.Card{ID: "C-001", State: card.Done}
	d.Attach(&done)
	if p := done.Progress; p == nil || p.Worktree != "/r/.claude/worktrees/pc-c-001" || !p.NoWorktree || p.Ahead != 0 || p.Commits != nil || p.Diff != nil {
		t.Fatalf("完了のカードに場所だけを足さない: %+v", p)
	}
	running := card.Card{ID: "C-001", State: card.Running}
	d.Attach(&running)
	if p := running.Progress; p == nil || p.Ahead != 3 || p.Diff == nil {
		t.Fatalf("作業中のカードに全部を足さない: %+v", p)
	}
}

// 差分の本文: 前と同じなら書き直さない・keep に無いカードの本文は消す。
func TestSaveAndPruneDiffs(t *testing.T) {
	dir := t.TempDir()
	if err := SaveDiff(dir, "C-001", []byte("a")); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(DiffPath(dir, "C-001"), old, old); err != nil {
		t.Fatal(err)
	}
	if err := SaveDiff(dir, "C-001", []byte("a")); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(DiffPath(dir, "C-001")); err != nil || !fi.ModTime().Equal(old) {
		t.Fatalf("同じ中身を書き直した: %v %v", fi.ModTime(), err)
	}
	if err := SaveDiff(dir, "C-002", []byte("b")); err != nil {
		t.Fatal(err)
	}
	if err := PruneDiffs(dir, map[string]bool{"C-002": true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(DiffPath(dir, "C-001")); !os.IsNotExist(err) {
		t.Fatalf("keep に無い本文を消さない: %v", err)
	}
	if _, err := os.Stat(DiffPath(dir, "C-002")); err != nil {
		t.Fatalf("keep の本文を消した: %v", err)
	}
	if err := PruneDiffs(t.TempDir(), nil); err != nil {
		t.Fatalf("置き場が無いときに失敗した: %v", err)
	}
}
