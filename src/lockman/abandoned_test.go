package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// issue 362: `--io-timeout` で見捨てた goroutine が、失敗を報告した後に副作用を置いていく。
//
// 🚨 **ここは in-process なので「窓が縮んだか」は測れない**。goroutine は待てば必ず完走するし、
// プロセスは終了しない。ここが固定するのは「見捨てを見て降りる / 取り消す配線が在ること」まで。
// 実際に漏れが減るかは**バイナリの A-B** (tests/lockman/) が測る。両方要る。

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

	took, err := l.tryTakeover(&abandon{})
	if !reached {
		t.Fatal("seam に到達していない: 目印を置く経路を通っていない")
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
	// 🚨 **errAbandoned を呼び出し側へ漏らさない**。漏れると `cmdAcquire` の `--wait` ループが
	// `errors.Is(err, errBusy)` に当たらず `default:` へ落ち、**exit 3 (他者が保持中) のはずが
	// exit 1 (エラー) になる**。`_av1ify_lock.zsh` は rc=3 を SKIP、rc≠0 を
	// 「排他を用意できない = 中止」に分けているので、終了コードの API (issue 091 の表) が変わる。
	// 構造的には起きない (mark を立てるのは select がタイムアウト枝を選んだ後で、そのとき
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
