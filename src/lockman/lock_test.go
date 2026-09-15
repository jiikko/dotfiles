package main

import (
	"encoding/json"
	"errors"
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
	if _, err := l.Inspect(); err == nil {
		t.Fatal("壊れた lock を Inspect がエラーにしない")
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
			ok, err := l.tryTakeover()
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

	if _, err := l.tryTakeover(); err != nil {
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

	took, err := l.tryTakeover()
	if err != nil {
		t.Fatalf("tryTakeover: %v", err)
	}
	if took {
		t.Fatal("飛行中の目印があるのに退ける役を取った")
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
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(claim, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	took, err := l.tryTakeover()
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
			ok, err := l.tryTakeover()
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
		t.Fatalf("seam の到達が %d 回 (期待 %d)", got, n)
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
		TimeoutMS: (time.Hour).Milliseconds(), // 申告 1h → 猶予 3h
		At:        time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(claim, body, lockFileMode); err != nil {
		t.Fatalf("目印を書けない: %v", err)
	}
	// 自分の猶予 (30s) は超えるが、申告された猶予 (3h) には収まる古さ
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(claim, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	took, err := l.tryTakeover()
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
