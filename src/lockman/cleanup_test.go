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
	// Refused は打刻の抑止と production の stderr 出力の両方をゲートしているので、
	// 「消さなかった」だけでなくフラグ自体を見る (これが無いと res.Refused = true を
	// 消す変異が緑で通る。issue 358 の 2 周目で実測)。
	if !res.Refused {
		t.Fatal("下限違反なのに Refused が立っていない (打刻の抑止と警告が効かなくなる)")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("下限を割る保持期間で残骸を消した: %v", err)
	}
	if len(res.Errors) == 0 {
		t.Fatal("下限違反を黙って握り潰した (エラーが 1 件も無い)")
	}
}

// ★ 下限の**絶対値**をリテラルで固定する。
//
// 🚨 ここをリテラルでなく定数どうしの比 (`minRetention >= 10 * defaultIOTimeout`) で
// 書くと、**両方を一緒に下げれば緑のまま通る**。実測 (issue 358 の敵対レビュー 2 周目):
// minRetention 10m→1m / scratchRetention 1h→2m / defaultIOTimeout 10s→6s の 3 点変異が
// 3 回とも `ok lockman` だった。しかも比で書いた版の失敗メッセージは両方のオペランドと
// 必要な比を名指しするので、**踏んだ人に「もう一方を下げれば緑になる」と教えてしまう**。
//
// リテラルなら、下げるにはこのテストを書き換えるしかなく、それは意図的な行為として
// diff に出る。
func TestMinRetentionFloorIsPinned(t *testing.T) {
	const pinned = 10 * time.Minute
	if minRetention < pinned {
		t.Fatalf("minRetention=%s が固定値 %s を割っている。"+
			"本当に下げる必要があるなら、走行中の acquire の残骸を消す確率が上がることを"+
			"理解したうえで、この pinned も一緒に直すこと (比で逃げないこと)", minRetention, pinned)
	}
	if scratchRetention < minRetention {
		t.Fatalf("scratchRetention=%s が下限 %s を割っている", scratchRetention, minRetention)
	}
	// 既定の I/O の上限との関係は「下限を見直す合図」として別に見る (これ単独は
	// 上の pin の代わりにならない — 両方下げれば通るため)。
	if minRetention < 10*defaultIOTimeout {
		t.Fatalf("defaultIOTimeout=%s が上がったので minRetention=%s を見直すこと",
			defaultIOTimeout, minRetention)
	}
}

// ★ 拒否していないときは打刻する (レート制限が進む)。
//
// 🚨 対になる「拒否したときは打刻しない」は**テストにしない**。既定の定数では拒否が
// 起きないので、書くと「production 到達不能な状態を自分で作って観測するテスト」に
// なり、この issue (358) が削除したガードと同じ形になる。そちらは変異で確認した:
// scratchRetention を下限未満にしたビルドで `lockman cleanup` を打っても
// `.cleanup_at` が作られないことを実測 (issue 358 の「実施結果」)。
func TestCleanupStampsWhenNotRefused(t *testing.T) {
	l := newTestLocker(t)
	if err := l.ensureDirs(); err != nil {
		t.Fatalf("ensureDirs: %v", err)
	}
	stamp := filepath.Join(l.metaDir, cleanupStampName)
	if _, err := os.Stat(stamp); !os.IsNotExist(err) {
		t.Fatalf("前提が崩れている: 打刻が既にある (%v)", err)
	}
	if got := l.Cleanup(true); got.Refused {
		t.Fatalf("既定の定数で拒否された (errors=%v)", got.Errors)
	}
	if _, err := os.Stat(stamp); err != nil {
		t.Fatalf("拒否していないのに打刻されていない: %v", err)
	}
}
