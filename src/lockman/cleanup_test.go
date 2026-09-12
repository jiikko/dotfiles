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
// 🚨 効く**軸**が違うことに注意 (5 周目の実測)。`minRetention` を下げる変異では
// リテラル側が先に Fatal するので比の行は走らない。比が単独で効く唯一の軸は
// **`defaultIOTimeout` を上げた**とき (2m にすると比の行だけが発火する)。
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
// 🚨 前提が作れなかったら **Fatal で落とす** (Skip にしない)。Skip にすると
// 「環境が失敗を作れなかった」と「production が失敗を記録しなくなった」が同じ緑に
// 畳まれ、`os.Remove` のエラー記録を消す変異が rc=0 で素通りする (4 周目が実測。
// CI は `-v` なしなので skip はログからも読めない)。
// `adversarial-review-own-safeguards.md` §2「判定不能を緑に畳まない」。
func TestCleanupDoesNotStampWhenRemoveFails(t *testing.T) {
	l := newTestLocker(t)
	if err := l.ensureDirs(); err != nil {
		t.Fatalf("ensureDirs: %v", err)
	}
	tmpDir := filepath.Join(l.metaDir, tmpDirName)
	touchOld(t, filepath.Join(tmpDir, "old.json"), 2*time.Hour)
	if err := os.Chmod(tmpDir, 0o500); err != nil { // 読めるが消せない
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(tmpDir, metaDirMode) })

	res := l.Cleanup(true)
	if len(res.Errors) == 0 {
		t.Fatal("前提が作れていない: 削除が拒否されなかった (root 実行なら、このテストは" +
			"守りとして成立していないので環境を変えること)")
	}
	assertNoStamp(t, l, res)
}

// ★ 上と別の枝: ReadDir 自体が失敗する場合 (`chmod 0000`)。
// 実装では `os.Remove` の失敗とは違う分岐なので、片方のテストでは守れない
// (4 周目が「ReadDir のエラー記録を消す変異が緑」で実測)。
func TestCleanupDoesNotStampWhenReadDirFails(t *testing.T) {
	l := newTestLocker(t)
	if err := l.ensureDirs(); err != nil {
		t.Fatalf("ensureDirs: %v", err)
	}
	tmpDir := filepath.Join(l.metaDir, tmpDirName)
	if err := os.Chmod(tmpDir, 0o000); err != nil { // 読むこともできない
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(tmpDir, metaDirMode) })

	res := l.Cleanup(true)
	if len(res.Errors) == 0 {
		t.Fatal("前提が作れていない: ReadDir が拒否されなかった (root 実行なら、この" +
			"テストは守りとして成立していないので環境を変えること)")
	}
	assertNoStamp(t, l, res)
}

// ★ 3 つ目の枝: ReadDir は通るが `e.Info()` (lstat) が落ちる場合。
// dir を 0400 にすると「読めるが辿れない」ので readdir だけが成功する。
// 4 周目がこの枝にも良性 ENOENT の基準を当てたのに、テストは 1 本も無かった
// (5 周目の実測: エラー記録を落とす変異が緑で通った)。
func TestCleanupDoesNotStampWhenInfoFails(t *testing.T) {
	l := newTestLocker(t)
	if err := l.ensureDirs(); err != nil {
		t.Fatalf("ensureDirs: %v", err)
	}
	tmpDir := filepath.Join(l.metaDir, tmpDirName)
	touchOld(t, filepath.Join(tmpDir, "old.json"), 2*time.Hour)
	if err := os.Chmod(tmpDir, 0o400); err != nil { // 読めるが辿れない
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(tmpDir, metaDirMode) })

	res := l.Cleanup(true)
	if len(res.Errors) == 0 {
		t.Fatal("前提が作れていない: e.Info() が拒否されなかった (root 実行なら、この" +
			"テストは守りとして成立していないので環境を変えること)")
	}
	assertNoStamp(t, l, res)
}

// ★ 逆に「サブ dir がまるごと消えている」は良性 (掃除の目的が達成された状態)。
// 失敗に数えると打刻が飛び、正常系で警告が鳴る (4 周目 P2-1 と同型)。
//
// 🚨 issue 358 の脅威モデル表は当初「ENOENT フィルタは seam が無いので決定論的に
// 書けない」と書いていたが、**それが当てはまるのは `os.Remove` と `e.Info()` だけ**。
// ReadDir 側は dir を消すだけで決定論的に作れる (5 周目の実測)。
func TestCleanupQuietWhenSubdirMissing(t *testing.T) {
	l := newTestLocker(t)
	if err := l.ensureDirs(); err != nil {
		t.Fatalf("ensureDirs: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(l.metaDir, tmpDirName)); err != nil {
		t.Fatalf("remove tmp dir: %v", err)
	}

	res := l.Cleanup(true)
	if len(res.Errors) != 0 {
		t.Fatalf("消えている dir を失敗に数えた: %v", res.Errors)
	}
	if _, err := os.Stat(filepath.Join(l.metaDir, cleanupStampName)); err != nil {
		t.Fatalf("良性なのに打刻されていない: %v", err)
	}
}

// ★ `serverNow()` が落ちる枝 (probe/ が在るのに書けない)。打刻せずエラーを持ち帰る。
// この枝は `Cleanup` の冒頭にあり、3 つの sweep の枝とは別の分岐なので個別に要る。
func TestCleanupDoesNotStampWhenServerNowFails(t *testing.T) {
	l := newTestLocker(t)
	if err := l.ensureDirs(); err != nil {
		t.Fatalf("ensureDirs: %v", err)
	}
	probeDir := filepath.Join(l.metaDir, probeDirName)
	if err := os.Chmod(probeDir, 0o500); err != nil { // 読めるが作れない
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(probeDir, metaDirMode) })

	res := l.Cleanup(true)
	if len(res.Errors) == 0 {
		t.Fatal("前提が作れていない: probe を作れてしまった (root 実行なら、この" +
			"テストは守りとして成立していないので環境を変えること)")
	}
	assertNoStamp(t, l, res)
}

// ★ 打刻そのものが落ちる枝。sweep は成功しているので `res.Errors` は空のままになり、
// **3 周目が作った「失敗したら打刻しない」ゲートからは構造的に見えなかった**
// (5 周目の実測: .lockman を 0500 にすると tmp/ の中身は消せるが .cleanup_at は
// 作れず、rc=0 / stderr 0B で毎回フル sweep する状態が無音で続く)。
func TestCleanupReportsStampFailure(t *testing.T) {
	l := newTestLocker(t)
	if err := l.ensureDirs(); err != nil {
		t.Fatalf("ensureDirs: %v", err)
	}
	touchOld(t, filepath.Join(l.metaDir, tmpDirName, "old.json"), 2*time.Hour)
	if err := os.Chmod(l.metaDir, 0o500); err != nil { // 辿れるが作れない
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(l.metaDir, metaDirMode) })

	res := l.Cleanup(true)
	// 🚨 前提: sweep 自体は成功していること。ここが 0 だと「sweep が落ちたから
	// エラーが在る」になり、打刻の失敗を 1 mm も検査していないテストになる。
	if res.Removed != 1 {
		t.Fatalf("前提が作れていない: sweep が成功していない (removed=%d errors=%v。"+
			"root 実行ならこのテストは守りとして成立していない)", res.Removed, res.Errors)
	}
	if len(res.Errors) == 0 {
		t.Fatal("打刻の失敗を握り潰した (掃除は成功しているのにレート制限が永久に進まない)")
	}
	assertNoStamp(t, l, res)
}

// ★ .lockman がまだ無い dir への cleanup は良性 (一度も使われていない = 掃除の
// 目的が達成済み)。失敗に数えると正常系で警告が鳴る。
//
// 併せて **cleanup が .lockman を作らない**ことも pin する。「ensureDirs を呼べば
// serverNow は落ちない」は採らなかった案で、掃除が掃除の対象を作る形になるため
// (`pending-issue-rationale-in-code.md`: 却下した案の理由をコード側にも残す)。
func TestCleanupOnNeverLockedDirIsQuiet(t *testing.T) {
	l := newTestLocker(t) // ensureDirs を呼ばない

	res := l.Cleanup(true)
	if len(res.Errors) != 0 {
		t.Fatalf(".lockman が無いだけで失敗に数えている: %v", res.Errors)
	}
	if _, err := os.Stat(l.metaDir); !os.IsNotExist(err) {
		t.Fatalf("cleanup が .lockman を作った (掃除が掃除の対象を作らない): %v", err)
	}
}

func assertNoStamp(t *testing.T, l *Locker, res CleanupResult) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(l.metaDir, cleanupStampName)); !os.IsNotExist(err) {
		t.Fatalf("掃除が失敗 (%v) したのに打刻された: 以後 %s は skip される (%v)",
			res.Errors, cleanupInterval, err)
	}
}

// ★ 逆向き: 失敗していないときは打刻する (レート制限が進む)。
//
// 🚨 かつてここには「これが無いと『常に打刻しない』変異が緑で通る」と書いてあったが、
// **実測で偽だった** (5 周目)。その変異は `TestCleanupIsRateLimited` と
// `TestCleanupRunsOnlyForMutatingCommands` が拾うので、本テストの検出力の増分は
// 上記 11 変異に対してゼロ。それでも残すのは「掃除が成功したら打刻する」を
// 名指しで読める場所が他に無いため (`mutation-verify-new-tests.md` が戒めるのは
// **偽の検証記録**であって、増分ゼロのテストの存在そのものではない)。
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
