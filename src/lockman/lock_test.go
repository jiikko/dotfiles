package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
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
				if err := l.Release(m.Token); err != nil {
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

	// 🚨 引き継ぐ側は長い TTL で取る。短い TTL のまま競わせると、勝者の新しい lock も
	// すぐ期限切れになり、後続が「正当に」引き継いで勝者が増える (仕様どおりの挙動)。
	// それでは「同じ 1 回の引き継ぎ競争で勝者は 1 人」という主張を測れない。
	const n = 16
	var wg sync.WaitGroup
	wins := make(chan string, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if m, err := l.Acquire(time.Minute, "takeover"); err == nil {
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

// ★ 回帰テスト: 生きている他人の lock を「短い TTL を渡す」だけで早期に奪えないこと。
//
// 生死の判定に**保持者が宣言した TTL** ではなく**奪いに来た側の --ttl** を使っていると、
// `lockman acquire "$dir" --ttl 30s` と打つだけで、30 分の lease を持つ相手を 30 秒で
// 追い出せてしまう。macOS では速すぎて出ず、CI (Linux) が「勝者が 4 人」で露見させた。
func TestShortTTLCannotStealLiveLock(t *testing.T) {
	l := newTestLocker(t)
	if _, err := l.Acquire(time.Hour, "long-lease"); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, err := l.Acquire(time.Millisecond, "thief"); !errors.Is(err, errBusy) {
		t.Fatalf("短い TTL を渡して他人の lease を奪えた (err=%v)", err)
	}
	// 解放も同じ: 奪いに来た側の TTL では「期限切れ」と判定させない
	if err := l.Release("deadbeef"); !errors.Is(err, errNotOwner) {
		t.Fatalf("Release: %v (期待 errNotOwner)", err)
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
	// 🚨 **手段ではなく意図を pin する** (issue 383 の敵対レビュー 4 周目 P1-2)。
	// 旧版は `Inspect` が**エラーを返すこと**を assert していたが、それは当時の手段で、
	// 意図は上のコメントどおり「空いていると解釈しない」。エラーに倒していたせいで
	// `status` が rc=1 (道具の失敗) / stdout 空になり、**案内の行き先が空振り**していた
	// (`acquire` が `lockman status` を名指ししているのに、人はそこで行き止まる)。
	// いまは「保持中 かつ 中身を読めない」という**状態**として答える。
	st, err := l.Inspect()
	if err != nil {
		t.Fatalf("壊れた lock で Inspect がエラーになった (状態として答えるべき): %v", err)
	}
	if !st.Held {
		t.Fatal("壊れた lock を「空いている」と答えた (fail-closed が壊れている)")
	}
	if !st.Unreadable {
		t.Fatal("壊れた lock が「中身を読めない」と分類されていない (CLI が案内できない)")
	}
}

// 他人の lock は解放できない。
func TestReleaseRejectsForeignToken(t *testing.T) {
	l := newTestLocker(t)
	if _, err := l.Acquire(time.Minute, ""); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := l.Release("deadbeef"); !errors.Is(err, errNotOwner) {
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
	if err := l.Release(m.Token); !errors.Is(err, errNotOwner) {
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

// ★ 上の対照: 誰も引き継いでいなくても、TTL を超えていれば Renew は失敗する。
//
// 上のテストは「token が別人のものに変わった」ことしか見ておらず、token 照合だけの
// 実装でも緑になる。期限切れのまま lock が残っている (= これから誰かが引き継げる)
// 状態で Renew が通ってしまうと、readLock と O_TRUNC の隙間に引き継ぎが挟まったとき
// 他者の lock を truncate して自分のメタで上書きする。
func TestRenewRefusesExpiredLease(t *testing.T) {
	l := newTestLocker(t)
	ttl := 50 * time.Millisecond
	m, err := l.Acquire(ttl, "")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	time.Sleep(3 * ttl)
	// 前提: 誰も引き継いでいない (lock は自分の token のまま残っている)。
	// ここが崩れると token 照合だけで落ちてしまい、期限検査を何も守らなくなる。
	got, _, err := l.readLock()
	if err != nil {
		t.Fatalf("readLock: %v", err)
	}
	if got == nil || got.Token != m.Token {
		t.Fatalf("前提が崩れた: lock が自分のものでなくなっている (%+v)", got)
	}
	if err := l.Renew(m.Token); !errors.Is(err, errNotOwner) {
		t.Fatalf("期限切れの lease を Renew で延長できた (err=%v)", err)
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

// takeoverSeam は「期限切れと判定した直後」の seam をテストから握る。戻り値は
// (最初の 1 人が判定を終えたら閉じる channel, その 1 人を再開させる関数,
//
//	seam が呼ばれた回数を返す関数)。
//
// 最初の 1 人だけを止め、2 人目以降は素通りさせる。これで「B が期限切れと判定した直後に
// A の引き継ぎを丸ごと通す」という、実測で二重取得を作った順序を決定論で再現できる。
func takeoverSeam(t *testing.T) (observed <-chan struct{}, resume func(), calls func() int) {
	t.Helper()
	obs := make(chan struct{})
	proceed := make(chan struct{})
	var mu sync.Mutex
	n := 0
	orig := takeoverObservedHook
	t.Cleanup(func() { takeoverObservedHook = orig })
	takeoverObservedHook = func() {
		mu.Lock()
		n++
		first := n == 1
		mu.Unlock()
		if first {
			close(obs)
			<-proceed
		}
	}
	return obs, func() { close(proceed) }, func() int {
		mu.Lock()
		defer mu.Unlock()
		return n
	}
}

// graveyardTokens は退けられた lock の token を読み出す。
func graveyardTokens(t *testing.T, l *Locker) []string {
	t.Helper()
	dir := filepath.Join(l.metaDir, graveyardDirName)
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("graveyard を読めない: %v", err)
	}
	var out []string
	for _, e := range ents {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("graveyard の %s を読めない: %v", e.Name(), err)
		}
		var m Meta
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("graveyard の %s が JSON でない: %v", e.Name(), err)
		}
		out = append(out, m.Token)
	}
	return out
}

// removeTakeoverClaims は引き継ぎの調停の目印を消す (掃除に浚われた状況を作る)。
// 消した件数を返す — 0 なら「前提が作れていない」ので呼び出し側が落とすため。
func removeTakeoverClaims(t *testing.T, l *Locker) int {
	t.Helper()
	dir := filepath.Join(l.metaDir, tmpDirName)
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("tmp を読めない: %v", err)
	}
	n := 0
	for _, e := range ents {
		if !strings.HasSuffix(e.Name(), takeoverClaimSuffix) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			t.Fatalf("目印を消せない: %v", err)
		}
		n++
	}
	return n
}

// ★ 回帰テスト (issue 366): 期限切れと判定した後に他者が引き継ぎを完走しても、
// 遅れてきた観測者は「その後に置かれた新しい lock」を退けない。
//
// 症状 (勝者の数) だけでなく機構を固定する — graveyard に入ってよいのは死んだ lock だけ。
// 実測 2026-09-15: この防御が無いと、勝者の新しい lock が graveyard に入っていた。
func TestStaleTakeoverDoesNotEvictFreshLock(t *testing.T) {
	l := newTestLocker(t)
	ttl := 50 * time.Millisecond
	dead, err := l.Acquire(ttl, "dead")
	if err != nil {
		t.Fatalf("下ごしらえの Acquire: %v", err)
	}
	time.Sleep(3 * ttl)

	observed, resume, calls := takeoverSeam(t)
	type result struct {
		m   *Meta
		err error
	}
	late := make(chan result, 1)
	go func() {
		m, err := l.Acquire(time.Minute, "late")
		late <- result{m, err}
	}()

	// 遅れてくる側が「期限切れ」と判定し終えるまで待つ。上限は安全網 (合否ではない)。
	select {
	case <-observed:
	case <-time.After(10 * time.Second):
		t.Fatal("seam が呼ばれない: 期限切れの判定に到達していないので、このテストは何も守っていない")
	}

	// その隙に引き継ぎを完走させる
	winner, err := l.Acquire(time.Minute, "winner")
	if err != nil {
		t.Fatalf("引き継ぎ側の Acquire: %v", err)
	}
	resume()
	got := <-late

	if got.err == nil {
		t.Fatalf("遅れてきた観測者も勝った (二重取得): winner=%s late=%s", winner.Token, got.m.Token)
	}
	if !errors.Is(got.err, errBusy) {
		t.Fatalf("busy 以外のエラー: %v", got.err)
	}
	for _, tok := range graveyardTokens(t, l) {
		if tok != dead.Token {
			t.Fatalf("死んだ lock 以外が退けられた: token=%s (死=%s 勝者=%s)", tok, dead.Token, winner.Token)
		}
	}
	cur, _, err := l.readLock()
	if err != nil {
		t.Fatalf("readLock: %v", err)
	}
	if cur == nil || cur.Token != winner.Token {
		t.Fatalf("勝者の lock が残っていない: %+v (期待 %s)", cur, winner.Token)
	}
	if n := calls(); n != 2 {
		t.Fatalf("seam の呼び出しが %d 回 (期待 2): 想定した順序になっていない", n)
	}
}

// ★ 回帰テスト (issue 366 / 2 段目の単独検査): 調停の目印が掃除に浚われていても、
// 破壊的操作の直前の再照合が「世代が変わった」ことを見て退去を止める。
//
// 1 段目 (調停) と 2 段目 (再照合) はどちらか一方でも上のテストを緑にしてしまうので、
// 段ごとに単独で red を見られる形にしてある (adversarial-review-own-safeguards.md §1.5)。
func TestStaleTakeoverRefusesWhenGenerationChangedAfterClaimSwept(t *testing.T) {
	l := newTestLocker(t)
	ttl := 50 * time.Millisecond
	dead, err := l.Acquire(ttl, "dead")
	if err != nil {
		t.Fatalf("下ごしらえの Acquire: %v", err)
	}
	time.Sleep(3 * ttl)

	observed, resume, _ := takeoverSeam(t)
	late := make(chan error, 1)
	go func() {
		_, err := l.Acquire(time.Minute, "late")
		late <- err
	}()
	select {
	case <-observed:
	case <-time.After(10 * time.Second):
		t.Fatal("seam が呼ばれない: 期限切れの判定に到達していない")
	}

	winner, err := l.Acquire(time.Minute, "winner")
	if err != nil {
		t.Fatalf("引き継ぎ側の Acquire: %v", err)
	}
	// 1 段目を無効化する: 掃除が目印を浚った状況を作る
	if n := removeTakeoverClaims(t, l); n == 0 {
		t.Fatal("前提が作れていない: 調停の目印が 1 つも無い (1 段目が動いていない)")
	}
	resume()

	if err := <-late; !errors.Is(err, errBusy) {
		t.Fatalf("目印が無いと遅れてきた観測者が勝つ (err=%v)", err)
	}
	for _, tok := range graveyardTokens(t, l) {
		if tok != dead.Token {
			t.Fatalf("死んだ lock 以外が退けられた: token=%s (死=%s 勝者=%s)", tok, dead.Token, winner.Token)
		}
	}
	cur, _, err := l.readLock()
	if err != nil {
		t.Fatalf("readLock: %v", err)
	}
	if cur == nil || cur.Token != winner.Token {
		t.Fatalf("勝者の lock が残っていない: %+v (期待 %s)", cur, winner.Token)
	}
}

// ★ 回帰テスト (issue 366 / 1 段目の単独検査): 同じ世代を同時に見た者が何人いても、
// 実際に退けるのは 1 人だけ。
//
// 全員が「期限切れ」と判定し終えてから一斉に進む形にする (最悪の順序)。調停が無いと
// 全員が true を返す — 先頭が退けた後の者は ENOENT / lock 不在で「作りにいってよい」に
// 落ちるため。ここを 2 段目は守らない (世代は変わっていないので再照合は通る)。
func TestConcurrentTakeoverElectsExactlyOneEvictor(t *testing.T) {
	l := newTestLocker(t)
	ttl := 50 * time.Millisecond
	if _, err := l.Acquire(ttl, "dead"); err != nil {
		t.Fatalf("下ごしらえの Acquire: %v", err)
	}
	time.Sleep(3 * ttl)

	const n = 8
	var mu sync.Mutex
	arrived := 0
	release := make(chan struct{})
	orig := takeoverObservedHook
	t.Cleanup(func() { takeoverObservedHook = orig })
	takeoverObservedHook = func() {
		mu.Lock()
		arrived++
		if arrived == n {
			close(release)
		}
		mu.Unlock()
		select {
		case <-release:
		case <-time.After(10 * time.Second): // 安全網。合否には使わない
		}
	}

	var wg sync.WaitGroup
	took := make(chan bool, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := l.tryTakeover(nil)
			if err != nil {
				t.Errorf("tryTakeover: %v", err)
				return
			}
			took <- ok
		}()
	}
	wg.Wait()
	close(took)
	mu.Lock()
	got := arrived
	mu.Unlock()
	if got != n {
		t.Fatalf("seam の到達が %d 回 (期待 %d): 全員が判定に到達していない", got, n)
	}
	wins := 0
	for ok := range took {
		if ok {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("退ける役が %d 人 (期待 1)", wins)
	}
}

// ★ 回帰テスト (issue 366 / 2 段目の mtime 照合): 判定した後にその lock が延長されて
// いたら、token が同じでも退けない。
//
// 期限の判定と保持者の Renew は別々の瞬間に serverNow を取るので、境界では
// 「観測側は期限切れ・保持者は期限内」が両立する。世代 (token) だけを見ていると、
// 延長で生き返った lock をそのまま退けてしまう。
func TestStaleTakeoverRefusesRenewedLock(t *testing.T) {
	l := newTestLocker(t)
	ttl := 50 * time.Millisecond
	held, err := l.Acquire(ttl, "holder")
	if err != nil {
		t.Fatalf("下ごしらえの Acquire: %v", err)
	}
	time.Sleep(3 * ttl)

	observed, resume, _ := takeoverSeam(t)
	late := make(chan error, 1)
	go func() {
		_, err := l.Acquire(time.Minute, "late")
		late <- err
	}()
	select {
	case <-observed:
	case <-time.After(10 * time.Second):
		t.Fatal("seam が呼ばれない: 期限切れの判定に到達していない")
	}

	// 保持者が滑り込みで延長した状態を作る (token は同じ / mtime だけ進む)。
	// 期限切れの lease は Renew が受けないので、打刻を直接作る。
	now := time.Now()
	if err := os.Chtimes(l.lockPath(), now, now); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	resume()

	if err := <-late; !errors.Is(err, errBusy) {
		t.Fatalf("延長された lock を退けてしまった (err=%v)", err)
	}
	// 🚨 退けずに帰る経路では目印を外す。残すと、一過性の失敗がその世代の引き継ぎを
	// 猶予いっぱい塞ぐ (誰も飛行していないのに待たされる)。
	if _, err := os.Stat(takeoverClaimPathFor(l, held.Token)); !os.IsNotExist(err) {
		t.Fatalf("退けずに帰ったのに目印が残っている (err=%v)", err)
	}
	if got := graveyardTokens(t, l); len(got) != 0 {
		t.Fatalf("生きている lock が退けられた: %v", got)
	}
	cur, _, err := l.readLock()
	if err != nil {
		t.Fatalf("readLock: %v", err)
	}
	if cur == nil || cur.Token != held.Token {
		t.Fatalf("保持者の lock が残っていない: %+v (期待 %s)", cur, held.Token)
	}
}

// ★ 回帰テスト (issue 366): lock の中身は他ホストが書いた値なので、token をそのまま
// パスの構成要素にしない。調停の目印が tmp/ の外に出ると、掃除の射程からも外れる。
func TestTakeoverClaimStaysInsideTmpForHostileToken(t *testing.T) {
	l := newTestLocker(t)
	if err := l.ensureDirs(); err != nil {
		t.Fatalf("ensureDirs: %v", err)
	}
	// 期限切れの lock を直接こしらえる (token にパス区切りを含める)
	b, err := json.Marshal(&Meta{Token: "../escape", TTLMillis: 1, Version: "lockman/1"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(l.lockPath(), b, lockFileMode); err != nil {
		t.Fatalf("lock を作れない: %v", err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(l.lockPath(), old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	if _, err := l.tryTakeover(nil); err != nil {
		t.Fatalf("tryTakeover: %v", err)
	}
	escaped := filepath.Join(l.metaDir, "escape"+takeoverClaimSuffix)
	if _, err := os.Stat(escaped); err == nil {
		t.Fatalf("目印が tmp/ の外に作られた: %s", escaped)
	}
	ents, err := os.ReadDir(filepath.Join(l.metaDir, tmpDirName))
	if err != nil {
		t.Fatalf("tmp を読めない: %v", err)
	}
	found := false
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), takeoverClaimSuffix) {
			found = true
			if !strings.HasPrefix(e.Name(), "notoken-") {
				t.Fatalf("危険な token がそのまま目印の名前になった: %s", e.Name())
			}
		}
	}
	if !found {
		t.Fatal("前提が作れていない: 調停の目印が 1 つも作られていない")
	}
}

// takeoverClaimPathFor はその世代の調停の目印のパスを返す。
func takeoverClaimPathFor(l *Locker, gen string) string {
	return filepath.Join(l.metaDir, tmpDirName, gen+takeoverClaimSuffix)
}

// staleLockWithClaim は「期限切れの lock + その世代の目印」という下ごしらえを作る。
func staleLockWithClaim(t *testing.T, l *Locker) (*Meta, string) {
	t.Helper()
	ttl := 50 * time.Millisecond
	dead, err := l.Acquire(ttl, "dead")
	if err != nil {
		t.Fatalf("下ごしらえの Acquire: %v", err)
	}
	time.Sleep(3 * ttl)
	claim := takeoverClaimPathFor(l, dead.Token)
	if err := l.placeTakeoverClaim(claim); err != nil {
		t.Fatalf("目印を作れない: %v", err)
	}
	return dead, claim
}

// ★ 回帰テスト (issue 366): 飛行中の目印は踏まない。踏むと役が 2 人になる (= 二重取得)。
func TestTakeoverYieldsToLiveClaim(t *testing.T) {
	l := newTestLocker(t)
	dead, claim := staleLockWithClaim(t, l)
	before, err := os.Stat(claim)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	var took bool
	// 🚨 診断が出ることも固定する。check は期限切れを free と答えるので、理由を出さないと
	// 「free なのに acquire できない」という追跡不能の矛盾になる。
	stderr := captureStderr(t, func() { took, err = l.tryTakeover(nil) })
	if err != nil {
		t.Fatalf("tryTakeover: %v", err)
	}
	if took {
		t.Fatal("飛行中の目印があるのに退ける役を取った")
	}
	if !strings.Contains(stderr, "調停中") {
		t.Fatalf("譲った理由が stderr に出ない: %q", stderr)
	}
	after, err := os.Stat(claim)
	if err != nil {
		t.Fatalf("目印が消えた: %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("飛行中の目印が回収 (作り直し) された")
	}
	cur, _, err := l.readLock()
	if err != nil {
		t.Fatalf("readLock: %v", err)
	}
	if cur == nil || cur.Token != dead.Token {
		t.Fatalf("lock が退けられた: %+v", cur)
	}
}

// ★ 回帰テスト (issue 366): 作成者が死んで残った目印は、猶予を過ぎたら回収する。
//
// 回収が無いと、目印を取った直後に死んだプロセス (Ctrl-C / --io-timeout の発火。どちらも
// defer を飛ばす既定経路) がその世代の引き継ぎを掃除まで塞ぎ、**scratchRetention = 1h が
// 既定 TTL 30m を超える**ので「TTL を過ぎれば誰かが引き継げる」という契約が割れる。
func TestTakeoverReclaimsAbandonedClaim(t *testing.T) {
	l := newTestLocker(t)
	dead, claim := staleLockWithClaim(t, l)
	// 目印は「別プロセスが置いて死んだもの」にする (回収後に中身が書き直されたかを見るため)
	foreign, err := json.Marshal(&takeoverClaimBody{
		Host: "other", User: "other", PID: 1,
		TimeoutMS: (5 * time.Second).Milliseconds(),
		At:        time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(claim, foreign, lockFileMode); err != nil {
		t.Fatalf("目印を書けない: %v", err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(claim, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	took, err := l.tryTakeover(nil)
	if err != nil {
		t.Fatalf("tryTakeover: %v", err)
	}
	if !took {
		t.Fatal("放棄された目印を回収できない (その世代は掃除まで引き継げない)")
	}
	st, err := os.Stat(claim)
	if err != nil {
		t.Fatalf("回収後の目印が無い: %v", err)
	}
	if st.ModTime().Before(old.Add(time.Hour)) {
		t.Fatalf("目印が作り直されていない (mtime=%v)", st.ModTime())
	}
	// 🚨 打刻はサーバに付けさせる (書き直す)。クライアントの時計で Chtimes すると
	// 時計ずれがそのまま猶予の判定へ入る — cleanup.go の stampCleanup と同じ罠。
	// 「中身が自分のものになっているか」がその唯一の観測点。
	if got := readTakeoverClaimBody(claim); got == nil || got.PID != os.Getpid() {
		t.Fatalf("回収後の目印が書き直されていない: %+v (期待 PID %d)", got, os.Getpid())
	}
	// 🚨 mark にも自分の身元と飛行上限を書く。書かないと「回収が止まっている」の判定が
	// 猶予の fallback に落ち、人へ出す名前も「作成者不明」になる (回収者を名指しできない)。
	marks := takeoverMarks(t, l)
	if len(marks) != 1 {
		t.Fatalf("mark が %d 件 (期待 1): %v", len(marks), marks)
	}
	mbody := readTakeoverClaimBody(filepath.Join(l.metaDir, tmpDirName, marks[0]))
	if mbody == nil || mbody.PID != os.Getpid() {
		t.Fatalf("mark に自分の body が書かれていない: %+v", mbody)
	}
	got := graveyardTokens(t, l)
	if len(got) != 1 || got[0] != dead.Token {
		t.Fatalf("死んだ lock が退けられていない: %v (期待 [%s])", got, dead.Token)
	}
}

// ★ 回帰テスト (issue 366): 放棄された目印を同時に回収しても、役を持つのは 1 人だけ。
//
// 素朴な remove → create だと、先に回収した者が作り直した**新しい**目印を次の者が消して
// しまい、役が 2 人になる。「存在しない名前へ rename できた 1 人だけが作り直す」形を固定する。
func TestConcurrentReclaimElectsExactlyOneEvictor(t *testing.T) {
	l := newTestLocker(t)
	_, claim := staleLockWithClaim(t, l)
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(claim, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	// 🚨 barrier は**回収経路**に置く。判定直後 (takeoverObservedHook) に置くと、先に回収した
	// 者が打刻を戻した後の Stat は「飛行中」の枝へ落ち、mark が 1 度も作られないまま
	// 「役は 1 人」が成立しうる (調停が働いた証拠にならない緑)。
	const n = 8
	var mu sync.Mutex
	arrived := 0
	release := make(chan struct{})
	orig := takeoverReclaimHook
	t.Cleanup(func() { takeoverReclaimHook = orig })
	takeoverReclaimHook = func() {
		mu.Lock()
		arrived++
		if arrived == n {
			close(release)
		}
		mu.Unlock()
		select {
		case <-release:
		case <-time.After(10 * time.Second): // 安全網。合否には使わない
		}
	}

	var wg sync.WaitGroup
	took := make(chan bool, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := l.tryTakeover(nil)
			if err != nil {
				t.Errorf("tryTakeover: %v", err)
				return
			}
			took <- ok
		}()
	}
	wg.Wait()
	close(took)
	mu.Lock()
	got := arrived
	mu.Unlock()
	// canary: 全員が「放棄された」と判定するところまで来ていないと、この緑は
	// 調停ではなく直列化の結果でしかない
	if got != n {
		t.Fatalf("回収経路への到達が %d 回 (期待 %d): 調停が争われていない", got, n)
	}
	wins := 0
	for ok := range took {
		if ok {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("回収で役を取った者が %d 人 (期待 1)", wins)
	}
}

// ★ 回帰テスト (issue 366): 猶予は**目印の作成者が申告した --io-timeout** から決める。
//
// 自分の値だけで決めると、長い --io-timeout で走っている作成者がまだ飛行しているうちに
// その目印を回収してしまい、役が 2 人になる。
func TestTakeoverGraceHonorsClaimDeclaredTimeout(t *testing.T) {
	l := newTestLocker(t) // 自分の io-timeout は 5s → 申告を読まなければ猶予は 30s
	dead, claim := staleLockWithClaim(t, l)
	body, err := json.Marshal(&takeoverClaimBody{
		Host: "other", User: "other", PID: 1,
		// --io-timeout の上限 (main.go の maxIOTimeout = 5m) いっぱいで申告 → 猶予 15m。
		// これより大きい申告は壊れた値として頭打ちにされるので、範囲内の値で測る。
		TimeoutMS: maxIOTimeout.Milliseconds(),
		At:        time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(claim, body, lockFileMode); err != nil {
		t.Fatalf("目印を書けない: %v", err)
	}
	// 自分の猶予 (30s) は超えるが、申告された猶予 (15m) には収まる古さ
	old := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(claim, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	took, err := l.tryTakeover(nil)
	if err != nil {
		t.Fatalf("tryTakeover: %v", err)
	}
	if took {
		t.Fatal("まだ飛行しうる作成者の目印を回収した (申告された --io-timeout を見ていない)")
	}
	cur, _, err := l.readLock()
	if err != nil {
		t.Fatalf("readLock: %v", err)
	}
	if cur == nil || cur.Token != dead.Token {
		t.Fatalf("lock が退けられた: %+v", cur)
	}
}

// ★ 回帰テスト (issue 366): 中身を読めない目印でも、猶予を待たずに回収しない。
//
// `placeTakeoverClaim` は中身の書き込み失敗を握り潰す (目印としては成立するため)。
// その受け皿が `takeoverClaimGrace` の fallback で、ここが 0 に倒れると**作られたばかりの
// 生きた目印をその場で回収**して役が 2 人になる。到達経路は 3 つ: refresh の truncate と
// write のあいだに読む / SMB の write 失敗 / 別形式で書かれた目印。
func TestTakeoverYieldsToClaimWithUnreadableBody(t *testing.T) {
	l := newTestLocker(t)
	dead, claim := staleLockWithClaim(t, l)
	if err := os.WriteFile(claim, nil, lockFileMode); err != nil { // 0 バイト = 中身を読めない
		t.Fatalf("目印を空にできない: %v", err)
	}
	if got := readTakeoverClaimBody(claim); got != nil {
		t.Fatalf("前提が作れていない: 中身が読めてしまう (%+v)", got)
	}
	// 猶予が 0 なら回収され、fallback (自分の io-timeout と既定値の大きいほう × 倍率) が
	// 効いていれば譲る、という古さ
	old := time.Now().Add(-5 * time.Second)
	if err := os.Chtimes(claim, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	took, err := l.tryTakeover(nil)
	if err != nil {
		t.Fatalf("tryTakeover: %v", err)
	}
	if took {
		t.Fatal("中身を読めない目印を、猶予を待たずに回収した (役が 2 人になる)")
	}
	cur, _, err := l.readLock()
	if err != nil {
		t.Fatalf("readLock: %v", err)
	}
	if cur == nil || cur.Token != dead.Token {
		t.Fatalf("lock が退けられた: %+v", cur)
	}
}

// ★ 回帰テスト (issue 366): 世代 id は mtime で変わらない / 危険な token は検疫する。
//
// 1 段目の調停は「同じ世代を見た者が必ず同じ id を得る」ことだけで成立している。
// mtime を混ぜると、延長で mtime が動いたときに同じ世代が別の id になり調停をすり抜ける。
func TestTakeoverGenerationAndTokenQuarantine(t *testing.T) {
	m := &Meta{Token: "0123456789abcdef"}
	t1, t2 := time.Unix(1000, 0), time.Unix(2000, 0)
	if a, b := takeoverGeneration(m, t1), takeoverGeneration(m, t2); a != b {
		t.Fatalf("世代 id が mtime で変わる: %q != %q", a, b)
	}
	if got := takeoverGeneration(m, t1); got != m.Token {
		t.Fatalf("世代 id が token と違う: %q", got)
	}
	if a, b := takeoverGeneration(nil, t1), takeoverGeneration(nil, t2); a == b {
		t.Fatalf("中身を読めない lock の id が mtime で変わらない: %q", a)
	}

	for _, bad := range []string{"", "../escape", "a/b", "ABCDEF", "abc.def", "xyz",
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0"} {
		if isHexToken(bad) {
			t.Errorf("パスの構成要素にできない token を通した: %q", bad)
		}
	}
	for _, ok := range []string{"a", "0", "0123456789abcdef",
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"} {
		if !isHexToken(ok) {
			t.Errorf("mustToken が作る形を弾いた: %q", ok)
		}
	}
}

// ★ 回帰テスト (issue 366): 猶予は既定 TTL を超えない / 申告値で溢れない。
//
// 猶予が既定 TTL を超えると「TTL を過ぎれば誰かが引き継げる」という道具の契約が割れる。
// 申告値 (他ホストが書いた値) で int64 を溢れさせると猶予が負になり、生きた目印を
// その場で回収する fail-open になる。
func TestTakeoverClaimGraceBounds(t *testing.T) {
	l := &Locker{timeout: defaultIOTimeout}
	fallback := defaultIOTimeout * takeoverClaimGraceFactor
	maxGrace := maxTakeoverClaimTimeout * takeoverClaimGraceFactor

	if got := l.takeoverClaimGrace(nil); got != fallback {
		t.Fatalf("中身を読めない目印の猶予が %v (期待 %v)", got, fallback)
	}
	if fallback > defaultTTL {
		t.Fatalf("既定の猶予 %v が既定 TTL %v を超える: TTL を過ぎても引き継げない窓ができる", fallback, defaultTTL)
	}

	// 🚨 申告値は他ホストが書いた値。**ms のまま頭打ちにしてから変換**しないと、桁を間違えた
	// 値で乗算が wrap し、最長のはずの猶予が最短 (= 回収する側) に化ける。
	// 実測 2026-09-16: 変換を先にすると MaxInt64 は -1ms になり猶予が fallback に落ちた。
	for _, c := range []struct {
		ms   int64
		want time.Duration
	}{
		{math.MaxInt64, maxGrace},
		{math.MaxInt64 / 2, maxGrace},
		{5_000_000_000_000, maxGrace},
		{maxTakeoverClaimTimeout.Milliseconds(), maxGrace},
		{maxTakeoverClaimTimeout.Milliseconds() + 1, maxGrace},
		{-1, fallback},
		{0, fallback},
		{time.Second.Milliseconds(), fallback}, // 申告が自分より短ければ fallback のまま
	} {
		if got := l.takeoverClaimGrace(&takeoverClaimBody{TimeoutMS: c.ms}); got != c.want {
			t.Errorf("申告 %d ms: 猶予 %v (期待 %v)", c.ms, got, c.want)
		}
	}

	// 🚨 申告値の上限は**フラグの UI 制約とは別の定数**で持つ。同じ値だが、UI 側を下げたときに
	// 猶予まで黙って縮み「まだ飛行している作成者の目印を回収する」側へ倒れるのを防ぐため。
	if maxTakeoverClaimTimeout != maxIOTimeout {
		t.Fatalf("申告値の上限 %v と --io-timeout の上限 %v がずれている: "+
			"どちらかを動かすときは猶予への影響を確かめること", maxTakeoverClaimTimeout, maxIOTimeout)
	}
	// 直接 Locker を組む経路 (フラグ検証を通らない) でも溢れない。
	for _, to := range []time.Duration{time.Duration(math.MaxInt64), 10 * time.Hour} {
		big := &Locker{timeout: to}
		if got := big.takeoverClaimGrace(nil); got != maxGrace {
			t.Errorf("l.timeout=%v: 猶予 %v (期待 %v)", to, got, maxGrace)
		}
	}

	// フラグが許す --io-timeout の全域で、猶予は maxGrace を超えない (溢れない)。
	for _, to := range []time.Duration{minIOTimeout, defaultIOTimeout, maxIOTimeout} {
		l := &Locker{timeout: to}
		for _, ms := range []int64{0, maxIOTimeout.Milliseconds(), math.MaxInt64} {
			if got := l.takeoverClaimGrace(&takeoverClaimBody{TimeoutMS: ms}); got <= 0 || got > maxGrace {
				t.Errorf("--io-timeout %v / 申告 %d ms: 猶予 %v が範囲外 (0, %v]", to, ms, got, maxGrace)
			}
		}
	}
}

// ★ 回帰テスト (issue 366): 回収の途中で目印が置き直されていたら、上書きせずに譲る。
//
// 打刻を戻す O_TRUNC は「今そこにあるもの」への無条件操作なので、掃除が目印を浚った後に
// 別の観測者が置き直していると、その人の目印を自分の中身で上書きして役が 2 人になる。
func TestRefreshTakeoverClaimYieldsWhenClaimWasReplaced(t *testing.T) {
	l := newTestLocker(t)
	if err := l.ensureDirs(); err != nil {
		t.Fatalf("ensureDirs: %v", err)
	}
	claim := takeoverClaimPathFor(l, "deadbeef")
	if err := l.placeTakeoverClaim(claim); err != nil {
		t.Fatalf("目印を作れない: %v", err)
	}
	before, err := os.ReadFile(claim)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// 🚨 fixture は**実際に起きる近接ケース**にする。1970 年のような桁違いの値だと、
	// 照合を「秒で丸める」「1 時間以上ずれたときだけ弾く」のような粗い判定へ変異させても
	// 緑のまま通り、照合の粒度を何も守らない。
	st, err := os.Stat(claim)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	for _, skew := range []time.Duration{-time.Nanosecond, time.Nanosecond, -time.Second, -time.Minute} {
		if err := l.refreshTakeoverClaim(claim, st.ModTime().Add(skew)); !errors.Is(err, errClaimReplaced) {
			t.Fatalf("打刻が %v ずれた目印を上書きした (err=%v)", skew, err)
		}
	}
	after, err := os.ReadFile(claim)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("目印の中身が書き換わった: %q -> %q", before, after)
	}
}

// ★ 回帰テスト (issue 366): 回収の mark が在るときは譲る。**理由を出すのは「止まっている」
// ときだけ**で、良性の競合では黙る。
//
// 回収を始めた者が途中で死ぬと目印の打刻が凍り、以後の観測者は全員この枝へ落ちて掃除まで
// 塞がる — そこは診断が要る。一方、同じ古さを見た者どうしの競合はミリ秒で解消するので、
// そこで `lockman break` を勧めると**最も現実的に二重取得を作る操作**へ人を誘導することになる
// (実測 2026-09-16: 8 本同時の正常な回収で 7 行の break 案内が出ていた)。
func TestTakeoverYieldsToReclaimMarkAndWarnsOnlyWhenStuck(t *testing.T) {
	for _, tc := range []struct {
		name     string
		markAge  time.Duration
		wantWarn bool
	}{
		{"良性の競合 (mark が新しい)", 0, false},
		{"回収が止まっている (mark が猶予より古い)", 2 * time.Hour, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := newTestLocker(t)
			dead, claim := staleLockWithClaim(t, l)
			old := time.Now().Add(-2 * time.Hour)
			if err := os.Chtimes(claim, old, old); err != nil {
				t.Fatalf("Chtimes: %v", err)
			}
			st, err := os.Stat(claim)
			if err != nil {
				t.Fatalf("Stat: %v", err)
			}
			mark := fmt.Sprintf("%s.%d", claim, st.ModTime().UnixNano())
			if err := os.WriteFile(mark, nil, lockFileMode); err != nil {
				t.Fatalf("mark を作れない: %v", err)
			}
			if tc.markAge > 0 {
				at := time.Now().Add(-tc.markAge)
				if err := os.Chtimes(mark, at, at); err != nil {
					t.Fatalf("Chtimes(mark): %v", err)
				}
			}

			var took bool
			stderr := captureStderr(t, func() { took, err = l.tryTakeover(nil) })
			if err != nil {
				t.Fatalf("tryTakeover: %v", err)
			}
			if took {
				t.Fatal("回収の mark が在るのに役を取った (役が 2 人になる)")
			}
			if got := strings.Contains(stderr, "lockman break"); got != tc.wantWarn {
				t.Fatalf("break の案内が %v (期待 %v): %q", got, tc.wantWarn, stderr)
			}
			cur, _, err := l.readLock()
			if err != nil {
				t.Fatalf("readLock: %v", err)
			}
			if cur == nil || cur.Token != dead.Token {
				t.Fatalf("lock が退けられた: %+v", cur)
			}
		})
	}
}

// ★ 回帰テスト (issue 366): 掃除が目印を浚った後でも、回収した者は目印を置き直して役を保つ。
//
// 🚨 これは**配線のテスト**。照合のために open の手前へ Stat を足すと、ENOENT の配線が
// Stat 側にだけ残って open 側から抜け落ちる。そうなると「掃除が浚った」という正常系が
// errBusy ではない hard error になり、`acquire --wait` のリトライループにすら入らない。
func TestRefreshTakeoverClaimRecreatesSweptClaim(t *testing.T) {
	l := newTestLocker(t)
	if err := l.ensureDirs(); err != nil {
		t.Fatalf("ensureDirs: %v", err)
	}
	claim := takeoverClaimPathFor(l, "deadbeef")

	// 掃除に浚われた後の状態 (目印が無い)
	if err := l.refreshTakeoverClaim(claim, time.Unix(1, 0)); err != nil {
		t.Fatalf("浚われた目印を置き直せない: %v", err)
	}
	body := readTakeoverClaimBody(claim)
	if body == nil || body.PID != os.Getpid() {
		t.Fatalf("置き直した目印が自分のものになっていない: %+v", body)
	}
}

// ★ 回帰テスト (issue 366): 回収の途中で失敗したら、回収の目印 (mark) を残さない。
//
// mark を残すと、目印の打刻は観測した値のままなので以後の観測者は同じ mark 名を計算して
// EEXIST で弾かれ続け、**プロセスが落ちていなくても**その世代が掃除まで引き継げなくなる。
// これは回収機構が防ぐはずの状態そのもの (掃除は 1h で既定 TTL 30m を超える)。
func TestReclaimLeavesNoMarkWhenRefreshFails(t *testing.T) {
	l := newTestLocker(t)
	_, claim := staleLockWithClaim(t, l)
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(claim, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	// 打刻を戻す書き込みだけを落とす (読むのは通る)
	if err := os.Chmod(claim, 0o444); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(claim, lockFileMode) })

	took, err := l.tryTakeover(nil)
	if err == nil {
		// 🚨 前提が作れていないと「実装が直っている」に見える緑になる (root 実行など)。
		t.Fatalf("前提が作れていない: 打刻を戻す書き込みが失敗しなかった (took=%v)", took)
	}
	if took {
		t.Fatal("回収に失敗したのに役を取ったと答えた")
	}
	if marks := takeoverMarks(t, l); len(marks) != 0 {
		t.Fatalf("回収に失敗したのに mark が残っている: %v (その世代は掃除まで引き継げない)", marks)
	}
}

// takeoverMarks は回収の調停に使った目印 (<gen>.takeover.<nanos>) を列挙する。
func takeoverMarks(t *testing.T, l *Locker) []string {
	t.Helper()
	ents, err := os.ReadDir(filepath.Join(l.metaDir, tmpDirName))
	if err != nil {
		t.Fatalf("tmp を読めない: %v", err)
	}
	var out []string
	for _, e := range ents {
		if i := strings.Index(e.Name(), takeoverClaimSuffix+"."); i >= 0 {
			out = append(out, e.Name())
		}
	}
	return out
}

// ★ 回帰テスト (issue 366): 照合を通った後に目印が置き直されても、**相手の目印を壊さない**。
//
// 🚨 これが「照合を fd に対して行っていること」の唯一の検査。パスに対する照合
// (os.Stat で見てから O_TRUNC で開く) へ戻す変異は、seam が無いと全テスト緑で通る —
// 単一プロセスのテストには「照合と書き込みのあいだに置き直す第三者」が居ないため。
//
// なお fd 版はこの順序で **成功を返す** (旧 inode の打刻は observed のまま)。役の一意性は
// 2 段目の再照合と tryPlace の O_EXCL が担うので、ここで固定するのは「壊さない」ことだけ。
func TestRefreshTakeoverClaimDoesNotClobberReplacedClaim(t *testing.T) {
	l := newTestLocker(t)
	if err := l.ensureDirs(); err != nil {
		t.Fatalf("ensureDirs: %v", err)
	}
	claim := takeoverClaimPathFor(l, "deadbeef")
	if err := l.placeTakeoverClaim(claim); err != nil {
		t.Fatalf("目印を作れない: %v", err)
	}
	st, err := os.Stat(claim)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	// 照合を通った直後に「掃除が浚って別の観測者が置き直す」を差し込む
	other, err := json.Marshal(&takeoverClaimBody{Host: "other", User: "other", PID: 4242})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	orig := takeoverRefreshHook
	t.Cleanup(func() { takeoverRefreshHook = orig })
	fired := false
	takeoverRefreshHook = func() {
		if fired {
			return
		}
		fired = true
		if err := os.Remove(claim); err != nil {
			t.Errorf("目印を消せない: %v", err)
		}
		if err := os.WriteFile(claim, other, lockFileMode); err != nil {
			t.Errorf("置き直せない: %v", err)
		}
	}

	_ = l.refreshTakeoverClaim(claim, st.ModTime())
	if !fired {
		t.Fatal("前提が作れていない: seam に到達していない")
	}
	got := readTakeoverClaimBody(claim)
	if got == nil || got.PID != 4242 {
		t.Fatalf("置き直された目印を壊した: %+v (期待 PID 4242)", got)
	}
}

// ★ 回帰テスト (issue 366): 回収で打刻を戻すとき、**自分より長い旧 body を切り詰める**。
//
// 回収の本命は「別ホストが置いた目印」なので、旧 body のほうが長い組み合わせ (長い hostname /
// 長い username) は普通に起きる。切り詰めないと末尾に旧 body の破片が残って JSON が壊れ、
// 次の観測者は申告値を失って短い fallback の猶予で判定する = 早すぎる回収へ倒れる。
func TestReclaimTruncatesLongerPreviousBody(t *testing.T) {
	l := newTestLocker(t)
	_, claim := staleLockWithClaim(t, l)
	long, err := json.Marshal(&takeoverClaimBody{
		Host: strings.Repeat("h", 200), User: strings.Repeat("u", 200), PID: 1,
		TimeoutMS: (5 * time.Second).Milliseconds(), At: time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	mine, err := l.marshalClaimBody()
	if err != nil {
		t.Fatalf("marshalClaimBody: %v", err)
	}
	if len(long) <= len(mine) {
		t.Fatalf("前提が作れていない: 旧 body (%d) が自分の body (%d) より長くない", len(long), len(mine))
	}
	if err := os.WriteFile(claim, long, lockFileMode); err != nil {
		t.Fatalf("目印を書けない: %v", err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(claim, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	took, err := l.tryTakeover(nil)
	if err != nil {
		t.Fatalf("tryTakeover: %v", err)
	}
	if !took {
		t.Fatal("放棄された目印を回収できない")
	}
	if got := readTakeoverClaimBody(claim); got == nil || got.PID != os.Getpid() {
		t.Fatalf("回収後の目印を読めない (旧 body の破片が残っている): %+v", got)
	}
}

// ★ 回帰テスト (issue 366): 回収の途中で目印が置き直されたら、**busy として譲る** (hard error にしない)。
//
// hard error にすると `errBusy` に当たらないので `acquire --wait` のリトライループに入らず、
// 良性のレース 1 回で待機が丸ごと諦める。
func TestReclaimYieldsAsBusyWhenClaimReplacedMidway(t *testing.T) {
	l := newTestLocker(t)
	_, claim := staleLockWithClaim(t, l)
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(claim, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	orig := takeoverReclaimHook
	t.Cleanup(func() { takeoverReclaimHook = orig })
	fired := false
	takeoverReclaimHook = func() {
		if fired {
			return
		}
		fired = true
		now := time.Now() // 別の観測者が置き直した = 打刻が進んだ状態
		if err := os.Chtimes(claim, now, now); err != nil {
			t.Errorf("Chtimes: %v", err)
		}
	}

	took, err := l.tryTakeover(nil)
	if !fired {
		t.Fatal("前提が作れていない: 回収経路に到達していない")
	}
	if err != nil {
		t.Fatalf("置き直しを hard error にした (--wait が諦める): %v", err)
	}
	if took {
		t.Fatal("置き直された目印を回収して役を取った")
	}
	if marks := takeoverMarks(t, l); len(marks) != 0 {
		t.Fatalf("譲ったのに mark が残っている: %v", marks)
	}
}

// ★ 回帰テスト (issue 366): 「回収が止まっている」の診断は **mark を置いた回収者**を名指しする。
//
// 目印 (claim) の body は**とうに死んだ作成者**のものなので、それで閾値を作ると
// 「回収者はまだ飛行中なのに止まっていると診断する」ことになり、しかも人へ無関係な pid を見せる。
func TestStuckReclaimWarnNamesTheReclaimerNotTheDeadCreator(t *testing.T) {
	l := newTestLocker(t)
	_, claim := staleLockWithClaim(t, l)
	creator, err := json.Marshal(&takeoverClaimBody{Host: "creator", User: "creator", PID: 1111})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(claim, creator, lockFileMode); err != nil {
		t.Fatalf("目印を書けない: %v", err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(claim, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	st, err := os.Stat(claim)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	reclaimer, err := json.Marshal(&takeoverClaimBody{Host: "reclaimer", User: "reclaimer", PID: 2222})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	mark := fmt.Sprintf("%s.%d", claim, st.ModTime().UnixNano())
	if err := os.WriteFile(mark, reclaimer, lockFileMode); err != nil {
		t.Fatalf("mark を作れない: %v", err)
	}
	stuck := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(mark, stuck, stuck); err != nil {
		t.Fatalf("Chtimes(mark): %v", err)
	}

	var took bool
	stderr := captureStderr(t, func() { took, err = l.tryTakeover(nil) })
	if err != nil {
		t.Fatalf("tryTakeover: %v", err)
	}
	if took {
		t.Fatal("mark が在るのに役を取った")
	}
	if !strings.Contains(stderr, "pid=2222") {
		t.Fatalf("回収者を名指ししていない: %q", stderr)
	}
	if strings.Contains(stderr, "pid=1111") {
		t.Fatalf("とうに死んだ作成者を名指しした (人が無関係な pid を撃つ): %q", stderr)
	}
}
