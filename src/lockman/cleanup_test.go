package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// touchOld はテスト用に「十分古い」残骸を作る。
func touchOld(t *testing.T, path string, age time.Duration) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), lockFileMode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	old := time.Now().Add(-age)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

// ★ cleanup は生きている lock を絶対に消さない。
// 「期限切れを消してから作る」は二重取得が最も出る経路で、掃除という別経路から
// それを持ち込むと、rename 引き継ぎで勝者を 1 人に絞った意味が消える。
func TestCleanupNeverRemovesLock(t *testing.T) {
	l := newTestLocker(t)
	m, err := l.Acquire(time.Minute, "live")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	for range 3 {
		l.Cleanup(true)
	}
	got, _, err := l.readLock()
	if err != nil || got == nil || got.Token != m.Token {
		t.Fatalf("生きている lock が消された (got=%v err=%v)", got, err)
	}

	// 期限切れの lock も cleanup は消さない (回収は Acquire の引き継ぎだけが行う)。
	l2 := newTestLocker(t)
	if _, err := l2.Acquire(30*time.Millisecond, "dead"); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	l2.Cleanup(true)
	if _, err := os.Stat(l2.lockPath()); err != nil {
		t.Fatalf("期限切れの lock を cleanup が消した: %v", err)
	}
}

func TestCleanupRemovesOldScratchOnly(t *testing.T) {
	l := newTestLocker(t)
	if err := l.ensureDirs(); err != nil {
		t.Fatalf("ensureDirs: %v", err)
	}
	oldTmp := filepath.Join(l.metaDir, tmpDirName, "old.json")
	freshTmp := filepath.Join(l.metaDir, tmpDirName, "fresh.json")
	// probe/ も対象。ここに fixture を置かないと「probe の sweep を丸ごと消す」変異が
	// 緑で通る (issue 358 の敵対レビューが実測)。
	oldProbe := filepath.Join(l.metaDir, probeDirName, "old")
	freshProbe := filepath.Join(l.metaDir, probeDirName, "fresh")
	oldGrave := filepath.Join(l.metaDir, graveyardDirName, "old")
	freshGrave := filepath.Join(l.metaDir, graveyardDirName, "fresh")
	touchOld(t, oldTmp, 2*time.Hour)
	touchOld(t, freshTmp, time.Minute)
	touchOld(t, oldProbe, 2*time.Hour)
	touchOld(t, freshProbe, time.Minute)
	touchOld(t, oldGrave, 8*24*time.Hour)
	touchOld(t, freshGrave, 24*time.Hour)

	res := l.Cleanup(true)
	if res.Removed != 3 {
		t.Fatalf("removed=%d (期待 3): errors=%v", res.Removed, res.Errors)
	}
	for _, p := range []string{freshTmp, freshProbe, freshGrave} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("新しい残骸を消した: %s", p)
		}
	}
	for _, p := range []string{oldTmp, oldProbe, oldGrave} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("古い残骸が残っている: %s", p)
		}
	}
}

// レート制限が効く (毎回 readdir すると SMB では重い)。
func TestCleanupIsRateLimited(t *testing.T) {
	l := newTestLocker(t)
	if err := l.ensureDirs(); err != nil {
		t.Fatalf("ensureDirs: %v", err)
	}
	if res := l.Cleanup(false); res.Skipped {
		t.Fatal("初回は掃除するはず")
	}
	if res := l.Cleanup(false); !res.Skipped {
		t.Fatal("直後の 2 回目が skip されていない")
	}
	if res := l.Cleanup(true); res.Skipped {
		t.Fatal("--force が skip された")
	}
}

// 掃除の失敗は致命にしない (掃除は正しさに関与しない)。
func TestCleanupFailureDoesNotBreakAcquire(t *testing.T) {
	l := newTestLocker(t)
	if err := l.ensureDirs(); err != nil {
		t.Fatalf("ensureDirs: %v", err)
	}
	tmpDir := filepath.Join(l.metaDir, tmpDirName)
	touchOld(t, filepath.Join(tmpDir, "old.json"), 2*time.Hour)
	if err := os.Chmod(tmpDir, 0o500); err != nil { // 削除できない権限にする
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(tmpDir, metaDirMode) })

	res := l.Cleanup(true)
	if len(res.Errors) == 0 {
		t.Log("この環境では削除が拒否されなかった (root 実行など)。以降の検査のみ行う")
	}
	if _, err := l.Acquire(time.Minute, ""); err != nil {
		t.Fatalf("掃除の失敗が acquire を壊した: %v", err)
	}
}

// ★ 保持期間には下限があり、割った値で呼ばれたら **何も消さずに** エラーを返す。
//
// 縛っているのは定数ではなく sweepDir に渡る値。定数に対する検査は「別の定数を作って
// 渡す」「下限も一緒に下げる」で静かに迂回されることを issue 358 の敵対レビューが
// 実測しており、そのどちらもここでは通らない。
func TestSweepRefusesRetentionBelowFloor(t *testing.T) {
	l := newTestLocker(t)
	if err := l.ensureDirs(); err != nil {
		t.Fatalf("ensureDirs: %v", err)
	}
	victim := filepath.Join(l.metaDir, tmpDirName, "inflight.json")
	touchOld(t, victim, 24*time.Hour) // 下限を割っていなければ確実に消える古さ

	var res CleanupResult
	l.sweepDir(tmpDirName, minRetention-time.Nanosecond, time.Now(), &res)

	if res.Removed != 0 {
		t.Fatalf("下限を割る保持期間で %d 件消した (期待 0)", res.Removed)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("下限を割る保持期間で残骸を消した: %v", err)
	}
	if len(res.Errors) == 0 {
		t.Fatal("下限違反を黙って握り潰した (エラーが 1 件も無い)")
	}
}

// ★ 下限を 2 通りで固定する。**どちらも本体で、片方は補助ではない** (3 周目の実測)。
//
//   - リテラルの `pinned`: 定数どうしの比だけで書くと、両方を一緒に下げれば緑で通る
//     (2 周目の実測: minRetention 10m→1m / scratchRetention 1h→2m / defaultIOTimeout
//     10s→6s の 3 点変異が 3 回とも `ok lockman`)
//   - `defaultIOTimeout` との比: リテラルが**単独で**効くのは `minRetention` が
//     [100s, 600s) にあるときだけで、**本当に危険な 100s 未満の帯を押さえているのは
//     こちら** (3 周目の実測)
//
// 🚨 どちらも「壁」ではない。production の 3 定数とこのテストの `pinned` を合わせて
// 4 行直せば通る (3 周目が実測)。上げているのは**敷居**であって不可能性ではなく、
// 4 行の diff が「意図してやった」ことを示す、という設計。
func TestMinRetentionFloorIsPinned(t *testing.T) {
	const pinned = 10 * time.Minute
	if minRetention < pinned {
		t.Fatalf("minRetention=%s が固定値 %s を割っている: "+
			"走行中の acquire の残骸を消す確率が上がる", minRetention, pinned)
	}
	if scratchRetention < minRetention {
		t.Fatalf("scratchRetention=%s が下限 %s を割っている", scratchRetention, minRetention)
	}
	// 既定の I/O の上限との関係。上のリテラルと役割が違う (帯が違う) ので両方要る。
	if minRetention < 10*defaultIOTimeout {
		t.Fatalf("minRetention=%s が defaultIOTimeout=%s の 10 倍未満: "+
			"走行中の I/O より短い保持期間になる", minRetention, defaultIOTimeout)
	}
}

// ★ 掃除が失敗したら打刻しない。打刻するとレート制限が進み、以後 10 分は skip されて
// 「掃除が黙って止まっている」状態が観測できなくなる。
//
// 🚨 検査に使うのは下限違反ではなく **実際に起きうる失敗** (権限ドリフト)。下限違反は
// 壊れたビルドでしか起きないので、それで検査すると production 到達不能な状態を自分で
// 作って観測するテストになり、この issue (358) が削除したガードと同じ形になる。
func TestCleanupDoesNotStampWhenSweepFails(t *testing.T) {
	l := newTestLocker(t)
	if err := l.ensureDirs(); err != nil {
		t.Fatalf("ensureDirs: %v", err)
	}
	tmpDir := filepath.Join(l.metaDir, tmpDirName)
	touchOld(t, filepath.Join(tmpDir, "old.json"), 2*time.Hour)
	if err := os.Chmod(tmpDir, 0o500); err != nil { // 削除できない権限にする
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(tmpDir, metaDirMode) })

	res := l.Cleanup(true)
	if len(res.Errors) == 0 {
		t.Skip("この環境では削除が拒否されなかった (root 実行など)")
	}
	stamp := filepath.Join(l.metaDir, cleanupStampName)
	if _, err := os.Stat(stamp); !os.IsNotExist(err) {
		t.Fatalf("掃除が失敗したのに打刻された (以後 %s は skip される): %v", cleanupInterval, err)
	}
}

// ★ 逆向き: 失敗していないときは打刻する (レート制限が進む)。
// これが無いと「常に打刻しない」変異が緑で通る。
func TestCleanupStampsWhenClean(t *testing.T) {
	l := newTestLocker(t)
	if err := l.ensureDirs(); err != nil {
		t.Fatalf("ensureDirs: %v", err)
	}
	stamp := filepath.Join(l.metaDir, cleanupStampName)
	if _, err := os.Stat(stamp); !os.IsNotExist(err) {
		t.Fatalf("前提が崩れている: 打刻が既にある (%v)", err)
	}
	if got := l.Cleanup(true); len(got.Errors) != 0 {
		t.Fatalf("既定の定数で失敗した: %v", got.Errors)
	}
	if _, err := os.Stat(stamp); err != nil {
		t.Fatalf("失敗していないのに打刻されていない: %v", err)
	}
}
