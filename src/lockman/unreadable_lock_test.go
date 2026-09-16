package main

import (
	"errors"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// 中身を読めない lock を「期限切れ」と判定しないこと (issue 383)。
//
// 🚨 これが壊れると、`--ttl 2h` を宣言した保持者が生きていても **30 分で正規手順で奪える** =
// 二重実行。TTL は lock の中身にしか無いので、読めない時点で生死は判定できない。
// 判定を既定 TTL へ倒す形は「宣言 TTL − 30m」をそのまま fail-open の幅にする。
func TestUnreadableLockIsNeverTakenOver(t *testing.T) {
	// 宣言 TTL は既定 (30m) より**長い**ものを使う。既定以下だと、fail-open のままでも
	// 期限切れにならず、有無で結果が変わらない fixture になる。
	const declared = 2 * time.Hour
	l := newTestLocker(t)
	if _, err := l.Acquire(declared, "holder"); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	// 保持者は生きているが、Renew の O_TRUNC の直後で詰まった形 (= 0 バイト) を作る。
	if err := os.Truncate(l.lockPath(), 0); err != nil {
		t.Fatalf("Truncate: %v", err)
	}
	// 既定 TTL は超えたが、宣言 TTL の内側、という時刻へ mtime を戻す。
	old := time.Now().Add(-(defaultTTL + 5*time.Minute))
	if err := os.Chtimes(l.lockPath(), old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	other, err := NewLocker(l.dir, 5*time.Second)
	if err != nil {
		t.Fatalf("NewLocker(other): %v", err)
	}
	_, err = other.Acquire(declared, "thief")
	if err == nil {
		t.Fatalf("中身を読めない lock を引き継いでしまった (二重実行。宣言 TTL=%v)", declared)
	}
	if !errors.Is(err, errBusy) {
		t.Fatalf("errBusy を期待したが %v", err)
	}
	// 人が次に何をすればよいかが出ていること (fail-closed の代償は「人が break する」なので、
	// その導線がメッセージに無いと詰まったまま終わる)
	if !strings.Contains(err.Error(), "break") {
		t.Fatalf("復旧手段 (break) が案内されていない: %v", err)
	}

	// 対照: 中身が読めるなら、宣言 TTL を超えたときに限って引き継げる (fail-closed が
	// 「常に引き継げない」へ倒れていないこと = この検査が vacuous でないこと)
	l2 := newTestLocker(t)
	if _, err := l2.Acquire(time.Minute, "holder2"); err != nil {
		t.Fatalf("Acquire(l2): %v", err)
	}
	past := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(l2.lockPath(), past, past); err != nil {
		t.Fatalf("Chtimes(l2): %v", err)
	}
	other2, err := NewLocker(l2.dir, 5*time.Second)
	if err != nil {
		t.Fatalf("NewLocker(other2): %v", err)
	}
	if _, err := other2.Acquire(time.Minute, "thief2"); err != nil {
		t.Fatalf("対照: 読める & 期限切れの lock は引き継げるはず: %v", err)
	}
}

// Break は lock を読まないので、中身を読めない lock にも効く (fail-closed の逃げ道)。
// 🚨 これが壊れると、上の fail-closed は**永久 wedge**になる。
func TestBreakRemovesUnreadableLock(t *testing.T) {
	l := newTestLocker(t)
	if _, err := l.Acquire(time.Minute, "holder"); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := os.Truncate(l.lockPath(), 0); err != nil {
		t.Fatalf("Truncate: %v", err)
	}
	if _, _, err := l.readLock(); !errors.Is(err, errBusy) {
		t.Fatalf("前提: 0 バイト lock は errBusy のはず: %v", err)
	}
	if err := l.Break(); err != nil {
		t.Fatalf("Break: %v", err)
	}
	if _, err := os.Stat(l.lockPath()); !os.IsNotExist(err) {
		t.Fatalf("Break の後も lock が残っている: %v", err)
	}
	if _, err := l.Acquire(time.Minute, "next"); err != nil {
		t.Fatalf("Break の後は取れるはず: %v", err)
	}
}

// readLock が返す mtime と中身は**同じ実体**のものであること (issue 383)。
//
// 🚨 2 syscall (`os.Stat` → `os.ReadFile`) 版は、あいだに lock が差し替えられると
// 「古い mtime + 新しい中身」を返した。その組は `takeoverGeneration` の入力そのもので、
// 「gen は新しいのに mtime は古い」= 期限切れ判定と世代照合が別々の実体を見る状態を作る。
//
// 🚨 **truncate による torn read はこれでは消えない** (実測: 同じ fd でも Fstat 後の O_TRUNC で
// 「古い mtime + 0 バイト」が返る)。そちらは fail-closed 側が受け持つ。
func TestReadLockReturnsMtimeAndBodyFromSameFile(t *testing.T) {
	l := newTestLocker(t)
	first, err := l.Acquire(time.Minute, "holder")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	// 読んでいる最中に、別の中身の lock へ差し替える (temp + rename。issue 380 が提案する形)
	var once sync.Once
	old := readLockAfterStatHook
	readLockAfterStatHook = func() {
		once.Do(func() {
			tmp := l.lockPath() + ".swap"
			body := []byte(`{"token":"SWAPPED","ttl_ms":60000,"version":"lockman/1"}`)
			if err := os.WriteFile(tmp, body, 0o644); err != nil {
				t.Errorf("WriteFile: %v", err)
				return
			}
			if err := os.Rename(tmp, l.lockPath()); err != nil {
				t.Errorf("Rename: %v", err)
			}
		})
	}
	defer func() { readLockAfterStatHook = old }()

	m, _, err := l.readLock()
	if err != nil {
		t.Fatalf("readLock: %v", err)
	}
	if m == nil {
		t.Fatalf("中身を読めていない")
	}
	if m.Token != first.Token {
		t.Fatalf("mtime を取った実体と違う中身を返した (差し替え後の中身を読んでいる): got=%s want=%s",
			m.Token, first.Token)
	}
}

// 🚨 **案内が production の出口まで届くこと** (issue 383 の敵対レビュー P2-2)。
//
// fail-closed の代償は「人が `lockman break` を打つまで詰まる」ことなので、**その導線が
// 出口に出ていないと恒久 wedge が『普通に他が走っている』と見分けられない**。
// 初版は `tryTakeover` が返す error の文言しか見ておらず、その文字列は
// `cmdAcquire` が無出力で捨てていた (= production では 1 バイトも出ていなかった)。
func TestUnreadableLockGuidanceReachesCLI(t *testing.T) {
	l := newTestLocker(t)
	if _, err := l.Acquire(2*time.Hour, "holder"); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := os.Truncate(l.lockPath(), 0); err != nil {
		t.Fatalf("Truncate: %v", err)
	}
	old := time.Now().Add(-(defaultTTL + 5*time.Minute))
	if err := os.Chtimes(l.lockPath(), old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	var rc int
	out := captureStderr(t, func() { rc = run([]string{"acquire", l.dir}) })
	if rc != exitBusy {
		t.Fatalf("rc=%d (exitBusy=%d を期待)", rc, exitBusy)
	}
	for _, want := range []string{"読めない", "break"} {
		if !strings.Contains(out, want) {
			t.Fatalf("acquire の stderr に %q が無い (人が復旧手段にたどり着けない): %q", want, out)
		}
	}

	// `with` 経路も同じ (定期ジョブはこちらを使う。定型文だけだと永久 skip が見えない)
	out2 := captureStderr(t, func() { rc = run([]string{"with", l.dir, "--", "/usr/bin/true"}) })
	if rc != exitWithBusy {
		t.Fatalf("with rc=%d (exitWithBusy=%d を期待。子を走らせていないか)", rc, exitWithBusy)
	}
	if !strings.Contains(out2, "読めない") {
		t.Fatalf("with の stderr に理由が出ていない: %q", out2)
	}
}

// 🚨 **正常な busy では静かなままであること** (敵対レビュー 2 周目 P2)。
//
// 「人が動くまで解けない busy」と「正常に他者が保持中」を**機械で見分けられる差**にしてある
// (`errUnreadableLock`)。正常系まで鳴らすと `lockman acquire || exit 0` の cron が skip の
// たびにメールを飛ばし、operator が `2>/dev/null` を足す → **本当に伝えたい案内まで黙る**。
func TestNormalBusyStaysQuiet(t *testing.T) {
	l := newTestLocker(t)
	if _, err := l.Acquire(time.Hour, "holder"); err != nil { // 中身は読める = 正常な保持
		t.Fatalf("Acquire: %v", err)
	}
	var rc int
	out := captureStderr(t, func() { rc = run([]string{"acquire", l.dir}) })
	if rc != exitBusy {
		t.Fatalf("rc=%d (exitBusy=%d を期待)", rc, exitBusy)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("正常な busy で stderr を汚した (cron が 2>/dev/null を足す動機になる): %q", out)
	}
	// 機械から見分けられること (散文ではなく型で)
	_, err := l.Acquire(time.Hour, "other")
	if !errors.Is(err, errBusy) {
		t.Fatalf("errBusy を期待: %v", err)
	}
	if errors.Is(err, errUnreadableLock) {
		t.Fatalf("正常な busy が「人が動くまで解けない busy」に分類されている: %v", err)
	}
}

// 🚨 **中身を書けなかった lock を残さない** (issue 383 の敵対レビュー P1)。
//
// `tryPlace` の O_EXCL fallback (link(2) が使えない FS = smbfs の経路) は、Write / Sync / Close に
// 失敗しても自分が作った 0 バイト lock を消さずに返していた。fail-closed の下では、それは
// **保持者も居ないのに誰も引き継げない**恒久 wedge になる。
func TestTryPlaceRemovesOwnLockWhenBodyCannotBeWritten(t *testing.T) {
	l := newTestLocker(t)
	// link(2) が使えない FS (smbfs) の枝へ入れる。ローカルでは link が成功してしまい、
	// fallback が 1 行も走らない
	oldLink := tryPlaceLinkFn
	tryPlaceLinkFn = func(string, string) error { return syscall.ENOTSUP }
	defer func() { tryPlaceLinkFn = oldLink }()
	old := tryPlaceAfterCreateHook
	tryPlaceAfterCreateHook = func(f *os.File) { _ = f.Close() } // Write を必ず失敗させる
	defer func() { tryPlaceAfterCreateHook = old }()

	if _, err := l.Acquire(time.Minute, "holder"); err == nil {
		t.Fatalf("書けないのに成功した")
	}
	if _, err := os.Stat(l.lockPath()); !os.IsNotExist(err) {
		t.Fatalf("中身を書けなかった lock が残っている (誰も引き継げない): %v", err)
	}
}

// 🚨 **後始末が消してよいのは「自分が開いた実体」だけ** (敵対レビュー 2 周目 P1 / P1-b)。
//
// 初版の後始末は `os.Remove(l.lockPath())` = **名前に対する破壊的操作**で、
// 「O_EXCL が通った = 自分のもの」に乗っていた。O_EXCL が保証するのはその瞬間だけで、
// Write が失敗して戻るまでのあいだに `Break` (期限も token も見ない無条件 rename) が入れば、
// 名前は**別ホストの生きた lock** を指している。そこを消すと二重実行になる。
//
// 🚨 初版のテストは「消えたこと」しか見ておらず、**消す範囲を 1 mm も固定していなかった**
// (他人の lock を消しても緑だった)。これがその欠けていた否定ケース。
func TestTryPlaceDoesNotRemoveSomeoneElsesLock(t *testing.T) {
	l := newTestLocker(t)
	other, err := NewLocker(l.dir, 5*time.Second)
	if err != nil {
		t.Fatalf("NewLocker(other): %v", err)
	}

	oldLink := tryPlaceLinkFn
	tryPlaceLinkFn = func(string, string) error { return syscall.ENOTSUP } // smbfs の枝
	defer func() { tryPlaceLinkFn = oldLink }()

	var victim *Meta
	fired := false
	oldHook := tryPlaceAfterCreateHook
	tryPlaceAfterCreateHook = func(f *os.File) {
		// 🚨 **1 回だけ発火させる**。中で呼ぶ `other.Acquire` も同じ枝を通るので、
		// 素の hook だと**無限再帰して固まる** (実測 2026-09-16: 600 秒で timeout)。
		// 🚨 `sync.Once` は使えない: 再入した `Do` は 1 回目の完了を待つので**デッドロックする**
		// (同じく実測で固まった)。ここは同期呼び出しなので素のフラグでよい。
		if fired {
			return
		}
		fired = true
		func() {
			// 窓の中で起きること: 人が「詰まっている」と見て break を打ち、別ホストが正規に取得する
			if err := l.Break(); err != nil {
				t.Errorf("Break: %v", err)
				return
			}
			m, err := other.Acquire(2*time.Hour, "other-host-job")
			if err != nil {
				t.Errorf("other.Acquire: %v", err)
				return
			}
			victim = m
			_ = f.Close() // こちらの Write は窓の終わりに失敗する
		}()
	}
	defer func() { tryPlaceAfterCreateHook = oldHook }()

	if _, err := l.Acquire(time.Minute, "stalled-host"); err == nil {
		t.Fatalf("書けないのに成功した")
	}
	if victim == nil {
		t.Fatalf("前提: 別ホストの取得が成立していない")
	}
	m, _, err := l.readLock()
	if err != nil {
		t.Fatalf("readLock: %v", err)
	}
	if m == nil {
		t.Fatalf("別ホストの生きた lock を消した (二重実行になる)")
	}
	if m.Token != victim.Token {
		t.Fatalf("別ホストの lock が別物に置き換わっている: got=%s want=%s", m.Token, victim.Token)
	}
}

// 🚨 **待ち直す枝では鳴らないこと** (敵対レビュー 2 周目 P3)。
//
// コメントには「待ち直す枝では出さない」と書いてあったが、**テストが 1 本も守っていなかった**
// (再試行枝にも warnf を足す変異が全緑だった)。`--wait 3600` は backoff 1s→15s で数百回
// 回るので、そこで鳴らすと stderr が数百行になり、P2 と同じく「stderr を捨てる運用」を作る。
func TestWaitLoopDoesNotWarnPerRetry(t *testing.T) {
	l := newTestLocker(t)
	// 中身を読めない lock = 鳴る側の busy を置く (正常な busy だと「元々鳴らない」ので
	// 再試行枝の有無で結果が変わらない fixture になる)
	if _, err := l.Acquire(2*time.Hour, "holder"); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := os.Truncate(l.lockPath(), 0); err != nil {
		t.Fatalf("Truncate: %v", err)
	}
	old := time.Now().Add(-(defaultTTL + 5*time.Minute))
	if err := os.Chtimes(l.lockPath(), old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	var rc int
	out := captureStderr(t, func() { rc = run([]string{"acquire", l.dir, "--wait", "3s"}) })
	if rc != exitBusy {
		t.Fatalf("rc=%d (exitBusy=%d を期待)", rc, exitBusy)
	}
	// 何回再試行しても、出るのは**諦めたときの 1 行だけ**
	lines := 0
	for _, ln := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(ln) != "" {
			lines++
		}
	}
	if lines != 1 {
		t.Fatalf("待ち直す枝でも鳴っている (%d 行。1 行のはず): %q", lines, out)
	}
}
