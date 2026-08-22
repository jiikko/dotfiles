package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func newTestLocker(t *testing.T) *Locker {
	t.Helper()
	l, err := NewLocker(t.TempDir(), 5*time.Second)
	if err != nil {
		t.Fatalf("NewLocker: %v", err)
	}
	return l
}

// 同時に取りにいったら、勝つのはちょうど 1 つ。これが壊れたら道具の意味が無い。
func TestAcquireHasExactlyOneWinner(t *testing.T) {
	l := newTestLocker(t)
	const n = 16
	var wg sync.WaitGroup
	wins := make(chan string, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if m, err := l.Acquire(time.Minute, "race"); err == nil {
				wins <- m.Token
			} else if !errors.Is(err, errBusy) {
				t.Errorf("busy 以外のエラー: %v", err)
			}
		}()
	}
	wg.Wait()
	close(wins)
	if got := len(wins); got != 1 {
		t.Fatalf("勝者が %d 人 (期待 1)", got)
	}
}

// 「成功が 1 件」だけでは引き継ぎ経路の二重取得を見逃す。実際に排他区間が
// 重なっていないことを、区間の出入りの記録で確かめる。
func TestCriticalSectionsNeverOverlap(t *testing.T) {
	l := newTestLocker(t)
	const workers = 8
	var mu sync.Mutex
	var events []string
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 5 {
				m, err := l.Acquire(time.Minute, "")
				if err != nil {
					continue // busy はスキップ (待ち行列は持たない仕様)
				}
				mu.Lock()
				events = append(events, "enter")
				mu.Unlock()
				time.Sleep(time.Millisecond)
				mu.Lock()
				events = append(events, "leave")
				mu.Unlock()
				if err := l.Release(m.Token, time.Minute); err != nil {
					t.Errorf("Release: %v", err)
				}
			}
		}()
	}
	wg.Wait()
	inside := false
	for i, e := range events {
		if e == "enter" {
			if inside {
				t.Fatalf("区間が重なった (event %d): %v", i, events)
			}
			inside = true
		} else {
			inside = false
		}
	}
}

// 期限切れの引き継ぎを同時に狙っても、引き取れるのは 1 人だけ。
// ここが二重取得の最頻出経路 (「消してから作る」にすると両方が勝つ)。
func TestStaleTakeoverHasExactlyOneWinner(t *testing.T) {
	l := newTestLocker(t)
	ttl := 50 * time.Millisecond
	if _, err := l.Acquire(ttl, "dead"); err != nil {
		t.Fatalf("下ごしらえの Acquire: %v", err)
	}
	time.Sleep(3 * ttl)

	const n = 16
	var wg sync.WaitGroup
	wins := make(chan string, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if m, err := l.Acquire(ttl, "takeover"); err == nil {
				wins <- m.Token
			}
		}()
	}
	wg.Wait()
	close(wins)
	if got := len(wins); got != 1 {
		t.Fatalf("引き継ぎの勝者が %d 人 (期待 1)", got)
	}
}

// 期限内は奪えない (自動引き継ぎが早まると、走行中の holder を追い越す)。
func TestFreshLockIsNotStolen(t *testing.T) {
	l := newTestLocker(t)
	if _, err := l.Acquire(time.Minute, ""); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if _, err := l.Acquire(time.Minute, ""); !errors.Is(err, errBusy) {
		t.Fatalf("期限内なのに奪えた (err=%v)", err)
	}
}

// 壊れた lock を「空いている」と解釈しない (fail-closed)。
func TestCorruptLockIsTreatedAsBusy(t *testing.T) {
	l := newTestLocker(t)
	if err := l.ensureDirs(); err != nil {
		t.Fatalf("ensureDirs: %v", err)
	}
	if err := os.WriteFile(l.lockPath(), []byte("{壊れた"), lockFileMode); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := l.Acquire(time.Minute, ""); !errors.Is(err, errBusy) {
		t.Fatalf("壊れた lock で busy にならない (err=%v)", err)
	}
	if _, err := l.Inspect(time.Minute); err == nil {
		t.Fatal("壊れた lock を Inspect がエラーにしない")
	}
}

// 他人の lock は解放できない。
func TestReleaseRejectsForeignToken(t *testing.T) {
	l := newTestLocker(t)
	if _, err := l.Acquire(time.Minute, ""); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := l.Release("deadbeef", time.Minute); !errors.Is(err, errNotOwner) {
		t.Fatalf("他人のトークンで解放できた (err=%v)", err)
	}
	if _, _, err := l.readLock(); err != nil {
		t.Fatalf("lock が壊された: %v", err)
	}
}

// 期限切れの自分の lock は消さない: その時点で他者が引き継いでいる可能性がある。
// 呼び出し側は exit 4 で「走行中に奪われた」と気づける。
func TestReleaseRefusesExpiredLease(t *testing.T) {
	l := newTestLocker(t)
	ttl := 50 * time.Millisecond
	m, err := l.Acquire(ttl, "")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	time.Sleep(3 * ttl)
	if err := l.Release(m.Token, ttl); !errors.Is(err, errNotOwner) {
		t.Fatalf("期限切れの lease で解放できた (err=%v)", err)
	}
}

// holder は「失った」ことを renew で知れる。
func TestRenewDetectsLostLease(t *testing.T) {
	l := newTestLocker(t)
	ttl := 50 * time.Millisecond
	m, err := l.Acquire(ttl, "")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := l.Renew(m.Token); err != nil {
		t.Fatalf("保持中の Renew が失敗: %v", err)
	}
	time.Sleep(3 * ttl)
	if _, err := l.Acquire(ttl, "thief"); err != nil {
		t.Fatalf("引き継げない: %v", err)
	}
	if err := l.Renew(m.Token); !errors.Is(err, errNotOwner) {
		t.Fatalf("奪われた後の Renew が成功した (err=%v)", err)
	}
}

// Renew は保持を延ばす (延びないと TTL 内に必ず奪われる)。
func TestRenewExtendsHold(t *testing.T) {
	l := newTestLocker(t)
	ttl := 200 * time.Millisecond
	m, err := l.Acquire(ttl, "")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	for range 3 {
		time.Sleep(ttl / 2)
		if err := l.Renew(m.Token); err != nil {
			t.Fatalf("Renew: %v", err)
		}
	}
	if _, err := l.Acquire(ttl, "thief"); !errors.Is(err, errBusy) {
		t.Fatalf("更新中なのに奪われた (err=%v)", err)
	}
}

// break は unlink ではなく graveyard への退避で行う (記録を残す)。
func TestBreakMovesLockToGraveyard(t *testing.T) {
	l := newTestLocker(t)
	m, err := l.Acquire(time.Minute, "")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := l.Break(); err != nil {
		t.Fatalf("Break: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(l.metaDir, graveyardDirName))
	if err != nil || len(entries) != 1 {
		t.Fatalf("graveyard に退避されていない (entries=%d, err=%v)", len(entries), err)
	}
	b, err := os.ReadFile(filepath.Join(l.metaDir, graveyardDirName, entries[0].Name()))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var got Meta
	if err := json.Unmarshal(b, &got); err != nil || got.Token != m.Token {
		t.Fatalf("退避された中身が違う (token=%s, err=%v)", got.Token, err)
	}
	if _, err := l.Acquire(time.Minute, "next"); err != nil {
		t.Fatalf("break 後に取れない: %v", err)
	}
}

// .lockman に sticky が付いていると引き継ぎが不可能になるので、作るときに付けない。
func TestMetaDirIsNotSticky(t *testing.T) {
	l := newTestLocker(t)
	if err := l.ensureDirs(); err != nil {
		t.Fatalf("ensureDirs: %v", err)
	}
	st, err := os.Stat(l.metaDir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if st.Mode()&os.ModeSticky != 0 {
		t.Fatal(".lockman に sticky bit が付いている (他ユーザーの lock を引き継げなくなる)")
	}
}
