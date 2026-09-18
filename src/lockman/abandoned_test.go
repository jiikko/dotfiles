package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// issue 362: `--io-timeout` で見捨てた goroutine が、失敗を報告した後に副作用を置いていく。
//
// 🚨 **ここは in-process なので「窓が縮んだか」は測れない**。goroutine は待てば必ず完走するし、
// プロセスは終了しない。ここが固定するのは「見捨てを見て降りる / 取り消す配線が在ること」まで。
// 実際に漏れが減るかは**バイナリの A-B** (`src/lockman/ab_abandoned.sh`) が測る。両方要る。

// stalePlacedLock は期限切れの lock を直接こしらえる (引き継ぎ経路へ入るための前提)。
func stalePlacedLock(t *testing.T, l *Locker) {
	t.Helper()
	if err := l.ensureDirs(); err != nil {
		t.Fatalf("ensureDirs: %v", err)
	}
	b, err := json.Marshal(&Meta{Token: "aaaabbbbccccdddd", TTLMillis: 1, Version: "lockman/1"})
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
	// 🚨 前提が成立したことを固定する。ここが崩れると引き継ぎ経路へ一度も入らないまま
	// 「目印が残っていない」が緑になる (何も検査していない緑)。
	if _, mtime, err := l.readLock(); err != nil || mtime.IsZero() {
		t.Fatalf("期限切れの lock を用意できていない (err=%v mtime=%v)", err, mtime)
	}
}

// tmpEntriesWithSuffix は tmp/ の中で名前が sub を含むものを列挙する。
func tmpEntriesWithSuffix(t *testing.T, l *Locker, sub string) []string {
	t.Helper()
	ents, err := os.ReadDir(filepath.Join(l.metaDir, tmpDirName))
	if err != nil {
		t.Fatalf("tmp を読めない: %v", err)
	}
	var got []string
	for _, e := range ents {
		if name := e.Name(); len(sub) == 0 || contains(name, sub) {
			got = append(got, name)
		}
	}
	return got
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ① 1 段目: 不可逆な操作の直前で見捨てられていたら、lock を**作らない**。
func TestAbandonedBeforePlaceCreatesNoLock(t *testing.T) {
	l := newTestLocker(t)
	if err := l.ensureDirs(); err != nil {
		t.Fatalf("ensureDirs: %v", err)
	}
	orig := abandonCheckBeforePlaceHook
	t.Cleanup(func() { abandonCheckBeforePlaceHook = orig })
	reached := false
	abandonCheckBeforePlaceHook = func(ab *abandon) {
		reached = true
		ab.mark()
	}
	// 🚨 **2 段目に救われていないことを固定する**。「lock が存在しない」だけを見ると、
	// 1 段目を外す変異を当てても 2 段目 (置いてから取り消す) が同じ結果を作るので緑のまま通る
	// (実測: 変異 M1 がそうだった)。**作らなかった**ことの観測点は「2 段目まで到達しないこと」。
	afterOrig := abandonCheckAfterPlaceHook
	t.Cleanup(func() { abandonCheckAfterPlaceHook = afterOrig })
	reachedAfter := false
	abandonCheckAfterPlaceHook = func(*abandon) { reachedAfter = true }

	ab := &abandon{}
	err := l.tryPlace(&Meta{Token: mustToken(), TTLMillis: time.Minute.Milliseconds()}, ab)
	if !reached {
		t.Fatal("seam に到達していない: 検査が置かれている経路を通っていない")
	}
	if reachedAfter {
		t.Fatal("1 段目で降りていない: lock を置いてから取り消している (2 段目に救われた緑)")
	}
	if !errors.Is(err, errAbandoned) {
		t.Fatalf("errAbandoned を期待したが %v", err)
	}
	if _, statErr := os.Lstat(l.lockPath()); statErr == nil {
		t.Fatal("見捨てられた後なのに lock が作られている")
	} else if !os.IsNotExist(statErr) {
		t.Fatalf("Lstat: %v", statErr)
	}
}

// ① 2 段目: 置いた後に見捨てられたら、自分が置いた lock を取り消す。
func TestAbandonedAfterPlaceUndoesItsOwnLock(t *testing.T) {
	l := newTestLocker(t)
	if err := l.ensureDirs(); err != nil {
		t.Fatalf("ensureDirs: %v", err)
	}
	orig := abandonCheckAfterPlaceHook
	t.Cleanup(func() { abandonCheckAfterPlaceHook = orig })
	reached := false
	abandonCheckAfterPlaceHook = func(ab *abandon) {
		reached = true
		// 🚨 ここへ来た時点で lock は**既に置かれている**ことを固定する。置かれる前に
		// 呼ばれていたら、この後の「消えている」は取り消しの証拠にならない。
		if _, err := os.Lstat(l.lockPath()); err != nil {
			t.Errorf("取り消し判定の時点で lock が置かれていない: %v", err)
		}
		ab.mark()
	}

	ab := &abandon{}
	err := l.tryPlace(&Meta{Token: mustToken(), TTLMillis: time.Minute.Milliseconds()}, ab)
	if !reached {
		t.Fatal("seam に到達していない")
	}
	if !errors.Is(err, errAbandoned) {
		t.Fatalf("errAbandoned を期待したが %v", err)
	}
	if _, statErr := os.Lstat(l.lockPath()); statErr == nil {
		t.Fatal("置いた lock が取り消されていない")
	} else if !os.IsNotExist(statErr) {
		t.Fatalf("Lstat: %v", statErr)
	}
}

// 🚨 再試行との干渉を固定する (issue 362 の「着手する人への注意」)。見捨てられた goroutine が
// 取り消しに走るあいだに、同じプロセスが再取得して**別 token の lock** を持っていることがある。
// 取り消しがそれを消したら、呼び出し側は「取れた」と思っているのに lock が無い = 二重実行。
func TestAbandonedUndoDoesNotRemoveALaterLock(t *testing.T) {
	l := newTestLocker(t)
	if err := l.ensureDirs(); err != nil {
		t.Fatalf("ensureDirs: %v", err)
	}
	// 後から取り直した側の lock (別 token)
	later, err := l.Acquire(time.Minute, "later")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	// 見捨てられた側が、自分の古い token で取り消しにいく
	if uerr := l.undoAbandonedPlace("00000000000000000000000000000000"); !errors.Is(uerr, errAbandoned) {
		t.Fatalf("errAbandoned を期待したが %v", uerr)
	}

	got, _, err := l.readLock()
	if err != nil {
		t.Fatalf("readLock: %v", err)
	}
	if got == nil {
		t.Fatal("後から取り直した lock が消された (二重実行に直結する)")
	}
	if got.Token != later.Token {
		t.Fatalf("lock の token が %s に変わっている (期待 %s)", got.Token, later.Token)
	}
}

// ② 引き継ぎの調停の目印: 置いた直後に見捨てられたら、置き去りにしない。
func TestAbandonedTakeoverLeavesNoClaim(t *testing.T) {
	l := newTestLocker(t)
	stalePlacedLock(t, l)

	orig := abandonCheckAfterClaimHook
	t.Cleanup(func() { abandonCheckAfterClaimHook = orig })
	reached := false
	abandonCheckAfterClaimHook = func(ab *abandon) {
		reached = true
		// 目印が既に在ることを固定する (置かれる前に降りたなら、下の「無い」は無意味)
		if got := tmpEntriesWithSuffix(t, l, takeoverClaimSuffix); len(got) != 1 {
			t.Errorf("判定の時点の目印が %v (1 件を期待)", got)
		}
		ab.mark()
	}
	// 🚨 **後段の検査に救われていないことを固定する** (1 段目 / 2 段目と同じ形)。
	// rename 直前の検査を足した時点で、「目印が残らない」だけを見るテストは**この検査を外しても
	// 緑**になった (後段が bail して defer が回収するため、最終状態が同じになる)。実測で
	// 変異 M3 が検知力を失っているのを見つけた。ここで降りたことの観測点は「後段へ進まないこと」。
	evictOrig := abandonCheckBeforeEvictHook
	t.Cleanup(func() { abandonCheckBeforeEvictHook = evictOrig })
	reachedEvict := false
	abandonCheckBeforeEvictHook = func(*abandon) { reachedEvict = true }

	took, err := l.tryTakeover(&abandon{})
	if !reached {
		t.Fatal("seam に到達していない: 目印を置く経路を通っていない")
	}
	if reachedEvict {
		t.Fatal("目印を置いた直後に降りていない: 詰まったマウントへ 2 段目の readLock を撃っている (後段に救われた緑)")
	}
	if !errors.Is(err, errAbandoned) {
		t.Fatalf("errAbandoned を期待したが took=%v err=%v", took, err)
	}
	if got := tmpEntriesWithSuffix(t, l, takeoverClaimSuffix); len(got) != 0 {
		t.Fatalf("見捨てられたのに目印が残っている: %v (その世代の引き継ぎを猶予いっぱい塞ぐ)", got)
	}
}

// ③ 回収の目印 (mark): 作った直後に見捨てられたら、置き去りにしない。
// 🚨 ③ は「②の回収機構を塞ぐ」層なので、①② を直しても残れば「引き継ぎが猶予ぶん止まる」。
func TestAbandonedReclaimLeavesNoMark(t *testing.T) {
	l := newTestLocker(t)
	stalePlacedLock(t, l)

	// 放棄された目印を用意する (猶予を大きく超えた打刻)
	m, mtime, err := l.readLock()
	if err != nil {
		t.Fatalf("readLock: %v", err)
	}
	claim := filepath.Join(l.metaDir, tmpDirName, takeoverGeneration(m, mtime)+takeoverClaimSuffix)
	if werr := os.WriteFile(claim, []byte("{}"), lockFileMode); werr != nil {
		t.Fatalf("目印を作れない: %v", werr)
	}
	old := time.Now().Add(-2 * time.Hour)
	if cerr := os.Chtimes(claim, old, old); cerr != nil {
		t.Fatalf("Chtimes: %v", cerr)
	}

	orig := abandonCheckBeforeMarkHook
	t.Cleanup(func() { abandonCheckBeforeMarkHook = orig })
	reached := false
	abandonCheckBeforeMarkHook = func(ab *abandon) {
		reached = true
		ab.mark()
	}

	took, err := l.tryTakeover(&abandon{})
	if !reached {
		t.Fatal("seam に到達していない: 回収経路 (mark を作る手前) を通っていない")
	}
	if !errors.Is(err, errAbandoned) {
		t.Fatalf("errAbandoned を期待したが took=%v err=%v", took, err)
	}
	// mark は claim の名前 + "." + nanos。claim 自身は**他人のもの**なので残ってよい。
	for _, name := range tmpEntriesWithSuffix(t, l, takeoverClaimSuffix) {
		if name != filepath.Base(claim) {
			t.Fatalf("見捨てられたのに回収の目印が残っている: %s (②の回収機構を塞ぐ)", name)
		}
	}
}

// production の配線: `withTimeout` が期限切れで実際に見捨てを立てるか。
//
// 🚨 これが無いと、上の 4 本は「seam で立てれば効く」しか言っておらず、**production の
// withTimeout が mark を呼んでいなくても全部緑**になる (fixture が退行から不可視)。
func TestWithTimeoutMarksAbandonInProduction(t *testing.T) {
	// 🚨 `t.TempDir()` を使わない。このテストは**見捨てた goroutine をわざと作る**ので、
	// テスト関数が返った後もその goroutine が dir を触りうる。`t.TempDir` の後始末は
	// 失敗をテストの失敗にするため、**実装と無関係な赤**が出る (実測: 変異 M1 で
	// 「directory not empty」が出て、変異を検知したように見えた)。後始問は best-effort にする。
	dir, err := os.MkdirTemp("", "lockman-wiring-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	l, err := NewLocker(dir, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewLocker: %v", err)
	}
	orig := abandonCheckBeforePlaceHook
	t.Cleanup(func() { abandonCheckBeforePlaceHook = orig })
	observed := make(chan bool, 1)
	abandonCheckBeforePlaceHook = func(ab *abandon) {
		// 期限が過ぎるのを**条件で**待つ (壁時計で assert しない)。上限を超えたら false を報告する。
		for range 400 {
			if ab.abandoned() {
				observed <- true
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		observed <- false
	}

	_, aerr := l.AcquireTimed(time.Minute, "wiring")
	if !errors.Is(aerr, errIOTimeout) {
		t.Fatalf("errIOTimeout を期待したが %v", aerr)
	}
	// 🚨 **errAbandoned を呼び出し側へ漏らさない** = 人が読む stderr を「判定不能」に保つ。
	// 漏れると `cmdAcquire` / `runWith` は「I/O が N 以内に返らない…判定不能」ではなく
	// `abandoned after I/O timeout` を表示する。**内部の事情**であって、人が次に何をすべきかを
	// 何も言っていない文面になる。
	//
	// 🚨 **訂正 (敵対レビュー 2 周目)**: ここには当初「exit 3 が exit 1 に化ける」と書いていたが
	// **誤り**。`errIOTimeout` も `errBusy` ではないので元から `default:` に落ちており、
	// この場面の終了コードは修正前から exit 1 (`with` なら 125)。終了コードの API は変わらない。
	// 変わるのは文面だけ。
	//
	// 構造的には漏れない (mark を立てるのは select がタイムアウト枝を選んだ後で、そのとき
	// withTimeout は errIOTimeout を返すと確定しており、goroutine の戻り値はバッファ付き chan に
	// 入ったまま誰も読まない) が、**構造の主張はテストで固定するまで主張のまま**なので pin する。
	if errors.Is(aerr, errAbandoned) {
		t.Fatal("errAbandoned が呼び出し側へ漏れている (exit 3 が exit 1 に化ける)")
	}
	select {
	case ok := <-observed:
		if !ok {
			t.Fatal("withTimeout が期限切れで見捨てを立てていない (4 秒待っても false のまま)")
		}
	case <-time.After(30 * time.Second): // 安全網。合否には使わない
		t.Fatal("seam が返らない")
	}
}

// 敵対レビュー P2 (2026-09-18): 取り消しが**判定不能**で返ったときに「TTL が切れるまで残る」と
// 断定し、`lockman break` を勧めていた。`ReleaseTimed` の期限切れは判定不能で、内側の goroutine は
// 走り続けて Remove に到達しうる。**消えるかもしれない lock** に対して「期限検査も token 照合も
// しない無条件 rename」へ人を誘導するのは、この道具で最も現実的に二重取得を作る操作。
func TestUndoDoesNotClaimTheLockRemainsWhenIndeterminate(t *testing.T) {
	l, err := NewLocker(t.TempDir(), 50*time.Millisecond)
	if err != nil {
		t.Fatalf("NewLocker: %v", err)
	}
	meta, err := l.Acquire(time.Minute, "indeterminate")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	// Release を期限切れにする (照合を通った後、Remove の手前で止める)
	orig := releaseBeforeRemoveHook
	t.Cleanup(func() { releaseBeforeRemoveHook = orig })
	released := make(chan struct{})
	releaseBeforeRemoveHook = func() {
		time.Sleep(300 * time.Millisecond) // l.timeout (50ms) を確実に超える
		close(released)
	}

	stderr := captureStderr(t, func() { _ = l.undoAbandonedPlace(meta.Token) })

	if !strings.Contains(stderr, "判定できない") {
		t.Fatalf("判定不能であることを伝えていない: %q", stderr)
	}
	if strings.Contains(stderr, "lockman break") {
		t.Fatalf("判定不能なのに break を勧めている (消えるかもしれない lock に無条件 rename を打たせる): %q", stderr)
	}
	if strings.Contains(stderr, "TTL が切れるまで残る") {
		t.Fatalf("判定不能なのに「残る」と断定している: %q", stderr)
	}
	// 実際、内側の goroutine は走り切って lock を消す = 「残る」は偽だった
	<-released
	for range 200 {
		if _, serr := os.Lstat(l.lockPath()); os.IsNotExist(serr) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("期限切れで返った後も内側の goroutine が lock を消していない (このテストの前提が崩れている)")
}

// 敵対レビュー P3 (2026-09-18): lease 切れの errNotOwner を「消すものが無い」と同じ扱いで
// 握り潰していたため、**自分が置いた lock が残っているのに stderr が完全に空**だった。
// コメントは「取り消せなかったことは黙らない」と約束している。
func TestUndoReportsWhenLeaseExpiredSoLockRemains(t *testing.T) {
	l := newTestLocker(t)
	meta, err := l.Acquire(minTTL, "expired")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	old := time.Now().Add(-time.Hour)
	if cerr := os.Chtimes(l.lockPath(), old, old); cerr != nil {
		t.Fatalf("Chtimes: %v", cerr)
	}

	stderr := captureStderr(t, func() { _ = l.undoAbandonedPlace(meta.Token) })

	if _, serr := os.Lstat(l.lockPath()); serr != nil {
		t.Fatalf("前提が崩れている: lease 切れでは Release は消さないはず (%v)", serr)
	}
	if stderr == "" {
		t.Fatal("自分が置いた lock が残っているのに何も言っていない")
	}
	if !strings.Contains(stderr, "lease 切れ") {
		t.Fatalf("残った理由 (lease 切れ) を伝えていない: %q", stderr)
	}
	if strings.Contains(stderr, "lockman break") {
		t.Fatalf("期限切れの lock は次の取得が引き継げるので break は不要: %q", stderr)
	}
}

// waitAbandoned は「見捨てられた」が立つのを条件で待つ (壁時計で assert しない)。
// 上限を超えたら false を返し、呼び出し側が明示的に FAIL させる。
func waitAbandoned(ab *abandon) bool {
	for range 400 {
		if ab.abandoned() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// newWiringLocker は「見捨てた goroutine をわざと作る」テスト用の Locker。
// 🚨 `t.TempDir()` を使わない (後始末が goroutine と競合して実装と無関係な赤が出る)。
func newWiringLocker(t *testing.T, timeout time.Duration) *Locker {
	t.Helper()
	dir, err := os.MkdirTemp("", "lockman-wiring-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	l, err := NewLocker(dir, timeout)
	if err != nil {
		t.Fatalf("NewLocker: %v", err)
	}
	return l
}

// 🚨 **配線の pin (敵対レビュー 2 周目 P1)**。`acquire` から下流へ `ab` を渡す点は 3 つあるが、
// 旧版は 1 つ (1 回目の tryPlace) しか pin しておらず、**残り 2 つを `nil` に落としても
// スイート全緑**だった。検査の中身を seam で直接叩くテストは「配線済み」を 1 mm も守らない
// (mutation-verify-new-tests.md の「計算ヘルパーの純関数テストだけで配線済みとしていないか」)。
//
// ここは `AcquireTimed` から入って**引き継ぎ経路**を通す。既存の配線テストは空 dir を使うので
// この経路へ一度も入らない。
func TestAbandonWiringReachesTakeoverPath(t *testing.T) {
	// ① tryTakeover(ab) — claim を置いた直後の検査まで ab が届くか
	t.Run("tryTakeover", func(t *testing.T) {
		l := newWiringLocker(t, 200*time.Millisecond)
		stalePlacedLock(t, l)
		orig := abandonCheckAfterClaimHook
		t.Cleanup(func() { abandonCheckAfterClaimHook = orig })
		observed := make(chan bool, 1)
		abandonCheckAfterClaimHook = func(ab *abandon) { observed <- waitAbandoned(ab) }

		if _, err := l.AcquireTimed(time.Minute, "wiring"); !errors.Is(err, errIOTimeout) {
			t.Fatalf("errIOTimeout を期待したが %v", err)
		}
		select {
		case ok := <-observed:
			if !ok {
				t.Fatal("tryTakeover へ ab が届いていない (nil が渡っている = ②③ の検査が production から死ぬ)")
			}
		case <-time.After(30 * time.Second):
			t.Fatal("seam に到達していない: 引き継ぎ経路を通っていない")
		}
	})

	// ② 引き継ぎ後の tryPlace(meta, ab) — この 1 行が A-B の「148 → 0」を作っている
	t.Run("tryPlaceAfterTakeover", func(t *testing.T) {
		l := newWiringLocker(t, 300*time.Millisecond)
		stalePlacedLock(t, l)
		orig := abandonCheckBeforePlaceHook
		t.Cleanup(func() { abandonCheckBeforePlaceHook = orig })
		calls := 0
		observed := make(chan bool, 1)
		abandonCheckBeforePlaceHook = func(ab *abandon) {
			calls++
			if calls < 2 {
				return // 1 回目は素通りさせ、errBusy → 引き継ぎへ進ませる
			}
			observed <- waitAbandoned(ab)
		}

		if _, err := l.AcquireTimed(time.Minute, "wiring"); !errors.Is(err, errIOTimeout) {
			t.Fatalf("errIOTimeout を期待したが %v", err)
		}
		select {
		case ok := <-observed:
			if !ok {
				t.Fatal("引き継ぎ後の tryPlace へ ab が届いていない (この issue の主症状が復活する)")
			}
		case <-time.After(30 * time.Second):
			t.Fatalf("2 回目の tryPlace へ到達していない (seam の到達 %d 回)", calls)
		}
	})
}

// 🚨 **不変条件: `undoAbandonedPlace` は `lockman break` を勧めない** (敵対レビュー 2 周目)。
// break は期限検査も token 照合もしない無条件 rename で、コード自身が「この道具で最も現実的に
// 二重取得を作る操作」と書いている。旧版は errNotOwner 枝に break を足しても、default 枝の
// 警告を消しても**全緑**だった (= 危険な向きが無検査だった)。全枝をテーブルで回す。
func TestUndoNeverSuggestsBreak(t *testing.T) {
	cases := []struct {
		name     string
		setup    func(t *testing.T, l *Locker) string // 返り値は undo に渡す token
		wantSay  bool                                 // stderr に何か出るべきか
		wantWord string
	}{
		{
			name: "取り消せた", wantSay: false,
			setup: func(t *testing.T, l *Locker) string {
				m, err := l.Acquire(time.Minute, "ok")
				if err != nil {
					t.Fatalf("Acquire: %v", err)
				}
				return m.Token
			},
		},
		{
			name: "自分のものではない", wantSay: false,
			setup: func(t *testing.T, l *Locker) string {
				if _, err := l.Acquire(time.Minute, "other"); err != nil {
					t.Fatalf("Acquire: %v", err)
				}
				return "00000000000000000000000000000000"
			},
		},
		{
			name: "lease 切れで残る", wantSay: true, wantWord: "lease 切れ",
			setup: func(t *testing.T, l *Locker) string {
				m, err := l.Acquire(minTTL, "expired")
				if err != nil {
					t.Fatalf("Acquire: %v", err)
				}
				old := time.Now().Add(-time.Hour)
				if cerr := os.Chtimes(l.lockPath(), old, old); cerr != nil {
					t.Fatalf("Chtimes: %v", cerr)
				}
				return m.Token
			},
		},
		{
			name: "中身を読めない (確定した失敗の枝)", wantSay: true, wantWord: "取り消せない",
			setup: func(t *testing.T, l *Locker) string {
				if err := l.ensureDirs(); err != nil {
					t.Fatalf("ensureDirs: %v", err)
				}
				if err := os.WriteFile(l.lockPath(), []byte("{broken"), lockFileMode); err != nil {
					t.Fatalf("lock を壊せない: %v", err)
				}
				return "00000000000000000000000000000000"
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := newTestLocker(t)
			token := c.setup(t, l)
			stderr := captureStderr(t, func() { _ = l.undoAbandonedPlace(token) })

			if strings.Contains(stderr, "lockman break") {
				t.Fatalf("break を勧めている (無条件 rename へ人を誘導する): %q", stderr)
			}
			if c.wantSay {
				if stderr == "" {
					t.Fatal("取り消せなかったのに黙っている")
				}
				if !strings.Contains(stderr, c.wantWord) {
					t.Fatalf("理由 %q を伝えていない: %q", c.wantWord, stderr)
				}
			} else if stderr != "" {
				t.Fatalf("言うことが無いのに出力している: %q", stderr)
			}
		})
	}
}

// 🚨 **本番の枝 (O_EXCL fallback) でも同じか** (敵対レビュー 2 周目)。`tryPlace` の link(2) は
// macOS の smbfs で ENOTSUP になり O_EXCL へ落ちる = **本番だけで走る経路**。ローカル APFS の
// テストは link(2) の枝しか通らず、O_EXCL 枝は「1 段目の検査から lock が見えるまで」「置いてから
// 2 段目まで」の syscall 数が違う。検査が両枝に効くことを固定する。
func TestAbandonChecksWorkOnExclFallbackBranch(t *testing.T) {
	origLink := tryPlaceLinkFn
	t.Cleanup(func() { tryPlaceLinkFn = origLink })
	tryPlaceLinkFn = func(string, string) error { return syscall.ENOTSUP }

	t.Run("1段目は作らない", func(t *testing.T) {
		l := newTestLocker(t)
		if err := l.ensureDirs(); err != nil {
			t.Fatalf("ensureDirs: %v", err)
		}
		orig := abandonCheckBeforePlaceHook
		t.Cleanup(func() { abandonCheckBeforePlaceHook = orig })
		abandonCheckBeforePlaceHook = func(ab *abandon) { ab.mark() }

		if err := l.tryPlace(&Meta{Token: mustToken(), TTLMillis: time.Minute.Milliseconds()}, &abandon{}); !errors.Is(err, errAbandoned) {
			t.Fatalf("errAbandoned を期待したが %v", err)
		}
		if _, err := os.Lstat(l.lockPath()); !os.IsNotExist(err) {
			t.Fatalf("O_EXCL 枝で lock が作られている (%v)", err)
		}
	})

	t.Run("2段目は取り消す", func(t *testing.T) {
		l := newTestLocker(t)
		if err := l.ensureDirs(); err != nil {
			t.Fatalf("ensureDirs: %v", err)
		}
		orig := abandonCheckAfterPlaceHook
		t.Cleanup(func() { abandonCheckAfterPlaceHook = orig })
		abandonCheckAfterPlaceHook = func(ab *abandon) {
			if _, err := os.Lstat(l.lockPath()); err != nil {
				t.Errorf("判定の時点で lock が置かれていない: %v", err)
			}
			ab.mark()
		}

		if err := l.tryPlace(&Meta{Token: mustToken(), TTLMillis: time.Minute.Milliseconds()}, &abandon{}); !errors.Is(err, errAbandoned) {
			t.Fatalf("errAbandoned を期待したが %v", err)
		}
		if _, err := os.Lstat(l.lockPath()); !os.IsNotExist(err) {
			t.Fatalf("O_EXCL 枝で置いた lock が取り消されていない (%v)", err)
		}
	})
}

// 🚨 破壊的操作 (lock を graveyard へ退ける rename) の直前でも降りること
// (敵対レビュー 3 周目)。claim の直後の検査からここまでに 2 段目の `readLock` が挟まるので、
// そこで見捨てられると「見捨てを宣言済みの goroutine が前世代の lock を退けて帰る」形になる。
func TestAbandonedTakeoverDoesNotEvict(t *testing.T) {
	l := newTestLocker(t)
	stalePlacedLock(t, l)
	before, _, err := l.readLock()
	if err != nil || before == nil {
		t.Fatalf("前提が崩れている: 期限切れ lock を読めない (%v)", err)
	}

	orig := abandonCheckBeforeEvictHook
	t.Cleanup(func() { abandonCheckBeforeEvictHook = orig })
	reached := false
	abandonCheckBeforeEvictHook = func(ab *abandon) {
		reached = true
		// ここへ来た時点で lock はまだ在る (退ける前) ことを固定する
		if _, serr := os.Lstat(l.lockPath()); serr != nil {
			t.Errorf("退ける前の検査なのに lock が無い: %v", serr)
		}
		ab.mark()
	}

	took, err := l.tryTakeover(&abandon{})
	if !reached {
		t.Fatal("seam に到達していない: 2 段目の照合を通る経路を通っていない")
	}
	if !errors.Is(err, errAbandoned) {
		t.Fatalf("errAbandoned を期待したが took=%v err=%v", took, err)
	}
	after, _, err := l.readLock()
	if err != nil {
		t.Fatalf("readLock: %v", err)
	}
	if after == nil || after.Token != before.Token {
		t.Fatal("見捨てられた goroutine が lock を退けている (不可逆な操作を実行して帰った)")
	}
	// 目印も置き去りにしない (evicted=false なので既存の defer が回収する)
	if got := tmpEntriesWithSuffix(t, l, takeoverClaimSuffix); len(got) != 0 {
		t.Fatalf("目印が残っている: %v", got)
	}
	graves, err := os.ReadDir(filepath.Join(l.metaDir, graveyardDirName))
	if err != nil {
		t.Fatalf("graveyard を読めない: %v", err)
	}
	if len(graves) != 0 {
		t.Fatalf("graveyard に退けた lock が入っている: %d 件", len(graves))
	}
}
