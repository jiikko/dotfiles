package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
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
	// 🚨 **破壊的操作を促していないこと** (敵対レビュー 3 周目 P2-1)。同じ観測が
	// 「健全な保持者の Renew が O_TRUNC している一瞬」からも出るので、`break` を促すと
	// **生きた保持者を剥がして二重実行を作る**助言になる。誘導先は読み取り専用の `status`。
	if strings.Contains(err.Error(), "break") {
		t.Fatalf("破壊的操作 (break) を促している (一過性の窓でも同じ文が出る): %v", err)
	}
	if !strings.Contains(err.Error(), "status") {
		t.Fatalf("次に見るもの (status) が案内されていない: %v", err)
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
	if !strings.Contains(out, "読めない") {
		t.Fatalf("acquire の stderr に理由が無い (恒久 wedge が正常な保持と区別できない): %q", out)
	}
	if strings.Contains(out, "break") {
		t.Fatalf("CLI が破壊的操作を促している (一過性の窓でも同じ文が出る): %q", out)
	}

	// `with` 経路も同じ (定期ジョブはこちらを使う。定型文だけだと永久 skip が見えない)
	out2 := captureStderr(t, func() { rc = run([]string{"with", l.dir, "--", "/usr/bin/true"}) })
	if rc != exitWithBusy {
		t.Fatalf("with rc=%d (exitWithBusy=%d を期待。子を走らせていないか)", rc, exitWithBusy)
	}
	if !strings.Contains(out2, "読めない") {
		t.Fatalf("with の stderr に理由が出ていない: %q", out2)
	}
	if strings.Contains(out2, "break") {
		t.Fatalf("with が破壊的操作を促している: %q", out2)
	}

	// 🚨 **案内されたコマンドを実際に走らせる** (敵対レビュー 4 周目 P1-2 = 10 個目の fixture の嘘)。
	// 旧版は `strings.Contains(out, "status")` だけを見ており、**名指しされたコマンドがその状態で
	// 何をするかは 1 行も検査していなかった**。実際には rc=1 (道具の失敗) / stdout 空で、
	// 人はそこで行き止まっていた。1 周目 P2-2 (「文字列は production に出ていなかった」) の
	// 一段外側の版 — 文字列は出ているが、指している先が空振り。
	m := regexp.MustCompile("`lockman ([a-z]+)[^`]*`").FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("案内にコマンドが含まれていない: %q", out)
	}
	var rc2 int
	guided := captureStdout(t, func() { rc2 = run([]string{m[1], l.dir}) })
	if rc2 == exitError {
		t.Fatalf("案内された `lockman %s` が「道具の失敗」を返す (人が行き止まる): rc=%d", m[1], rc2)
	}
	if strings.TrimSpace(guided) == "" {
		t.Fatalf("案内された `lockman %s` が何も出さない (案内が空振り)", m[1])
	}
	for _, want := range []string{"size", "age"} {
		if !strings.Contains(guided, want) {
			t.Fatalf("案内先が判断材料 (%s) を出していない: %q", want, guided)
		}
	}
	// 🚨 **案内先も破壊的操作を促さない** (敵対レビュー 5 周目 P1)。
	// `errUnreadableLock` のコメントは「break を促さない」を不変条件として宣言しているのに、
	// 検査していたのは 3 hop 中 2 hop (err.Error() と acquire の stderr) だけで、
	// **その 2 つが名指ししている status** は promote したまま緑だった。
	// 不変条件は「守られている hop」ではなく「破れる hop」で pin する。
	if strings.Contains(guided, "break") {
		t.Fatalf("案内先の `lockman %s` が破壊的操作を促している (保持者が居ないことは"+
			"どのコマンドでも確認できないのに): %q", m[1], guided)
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

// 🚨 **「人が動くまで解けない busy」が分類から漏れて無音にならないこと** (3 周目 P1-1)。
//
// `lock` がディレクトリのとき、旧版は `readLock` の busy エラーを握り潰したうえで
// **エラーを根拠に `took=true`** を返し、その先の `tryPlace` が EEXIST で**素の errBusy** に
// なっていた。分類から漏れるので CLI は**無音**で、人が `break` すれば解ける wedge なのに
// 正常な保持と見分けが付かない。
func TestDirectoryLockIsClassifiedAsUnreadable(t *testing.T) {
	l := newTestLocker(t)
	if err := l.ensureDirs(); err != nil {
		t.Fatalf("ensureDirs: %v", err)
	}
	if err := os.Mkdir(l.lockPath(), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	_, err := l.Acquire(time.Hour, "x")
	if !errors.Is(err, errBusy) {
		t.Fatalf("errBusy を期待: %v", err)
	}
	if !errors.Is(err, errUnreadableLock) {
		t.Fatalf("ディレクトリの lock が「読めない busy」に分類されていない (CLI が無音になる): %v", err)
	}
	var rc int
	out := captureStderr(t, func() { rc = run([]string{"acquire", l.dir}) })
	if rc != exitBusy {
		t.Fatalf("rc=%d", rc)
	}
	if !strings.Contains(out, "読めない") {
		t.Fatalf("CLI が無音 (恒久 wedge が正常な保持と区別できない): %q", out)
	}
}

// captureStdout は fn の実行中の os.Stdout を集める (`status` は stdout に答えを書く)。
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	os.Stdout = orig
	_ = w.Close()
	out := <-done
	_ = r.Close()
	return out
}

// 🚨 **`status` は「中身を読めない lock」に答えること** (敵対レビュー 4 周目 P1-2)。
//
// 旧版は `Inspect` が readLock のエラーをそのまま返し、`status` が rc=1 (exitError) / stdout 空に
// していた。`--json` でも stdout が空 + rc=1 なので、監視からは「lockman が壊れた / dir が無い」と
// **区別できなかった**。`check` には「判定不能は busy 側へ倒す」分岐があるのに、案内はその分岐を
// **持っていない方**を名指ししていた。
func TestStatusAnswersForUnreadableLock(t *testing.T) {
	l := newTestLocker(t)
	if _, err := l.Acquire(time.Hour, "holder"); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := os.Truncate(l.lockPath(), 0); err != nil {
		t.Fatalf("Truncate: %v", err)
	}
	var rc int
	out := captureStdout(t, func() { rc = run([]string{"status", l.dir}) })
	if rc == exitError {
		t.Fatalf("status が「道具の失敗」を返した (dir が無いのと区別できない): rc=%d", rc)
	}
	if rc != exitBusy {
		t.Fatalf("status rc=%d (exitBusy=%d を期待。空いているとは言わない)", rc, exitBusy)
	}
	if !strings.Contains(out, "unreadable") {
		t.Fatalf("status が状態を答えていない: %q", out)
	}
	var rcj int
	outj := captureStdout(t, func() { rcj = run([]string{"status", "--json", l.dir}) })
	if rcj != exitBusy || !strings.Contains(outj, `"unreadable":true`) {
		t.Fatalf("status --json が状態を出していない: rc=%d out=%q", rcj, outj)
	}
	// 🚨 **canonical な unreadable lock は 0 バイト**。値型 + omitempty だと、いちばん重要な
	// 判断材料がちょうどそのときだけ JSON から消えていた (敵対レビュー 5 周目 P2-2)。
	if !strings.Contains(outj, `"size_bytes":0`) {
		t.Fatalf("status --json が 0 バイトという判断材料を落としている: %q", outj)
	}

	// 🚨 `check` の契約 (判定不能は busy 側へ倒す) を壊していないこと。
	// Inspect がエラーを返さなくなったので、`check` は `st.Held` 経由で同じ rc になる必要がある
	var rcc int
	captureStdout(t, func() { rcc = run([]string{"check", l.dir}) })
	if rcc != exitBusy {
		t.Fatalf("check rc=%d (exitBusy=%d を期待。空いているとは言わない)", rcc, exitBusy)
	}
}

// 🚨 **`with` 経路の分類も pin する** (敵対レビュー 3 周目 P2-2 / 4 周目 P2-1)。
// `TestNormalBusyStaysQuiet` は `acquire` しか通しておらず、`with` 側は「常に理由を出す」に
// 潰しても全緑だった。定期ジョブが使うのはこちらなので、静けさが壊れると 2 周目 P2 が復活する。
func TestWithStaysQuietOnNormalBusy(t *testing.T) {
	l := newTestLocker(t)
	if _, err := l.Acquire(time.Hour, "holder"); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	var rc int
	out := captureStderr(t, func() { rc = run([]string{"with", l.dir, "--", "/usr/bin/true"}) })
	if rc != exitWithBusy {
		t.Fatalf("rc=%d (exitWithBusy=%d を期待)", rc, exitWithBusy)
	}
	// 🚨 **完全一致で固定する** (部分一致だと、分類を潰して `(%v)` を常に足す変異が
	// 「読めない を含まない / 他が保持中 を含む」を満たして**緑で通る**。実測 2026-09-16)。
	if got := strings.TrimSpace(out); got != "lockman: 他が保持中のため実行しない" {
		t.Fatalf("正常な busy の stderr が定型文と一致しない (分類が効いていない): %q", got)
	}
}

// 🚨 **2 段目 (再照合) の zero-mtime も 1 段目と同じ判別であること** (敵対レビュー 4 周目 P1-1)。
//
// 3 周目は 1 段目だけを直し、commit と issue に「zero-mtime busy 全部に効く」と書いたが**偽**で、
// 2 段目は素の `mtime2.IsZero()` のままだった。そこは busy エラー + zero mtime を
// 「別の誰かが先に退けた」と読んで `took=true` を返す (fail-open) ので、その先の `tryPlace` が
// EEXIST → **素の errBusy** になり、3 周目 P1-1 が P1 と判定した「CLI が無音」がそのまま生きていた。
func TestStage2ZeroMtimeBusyIsNotTreatedAsEvicted(t *testing.T) {
	l := newTestLocker(t)
	if _, err := l.Acquire(time.Minute, "holder"); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	past := time.Now().Add(-2 * time.Minute) // 期限切れにして 1 段目を通す
	if err := os.Chtimes(l.lockPath(), past, past); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	// 1 段目の判定の直後 (2 段目より前) に、lock をディレクトリへ化けさせる
	// = readLock が (busy, zero mtime) を返す唯一の形
	fired := false
	old := takeoverObservedHook
	takeoverObservedHook = func() {
		if fired {
			return
		}
		fired = true
		if err := os.Remove(l.lockPath()); err != nil {
			t.Errorf("Remove: %v", err)
			return
		}
		if err := os.Mkdir(l.lockPath(), 0o755); err != nil {
			t.Errorf("Mkdir: %v", err)
		}
	}
	defer func() { takeoverObservedHook = old }()

	took, err := l.tryTakeover(nil)
	if !fired {
		t.Fatalf("前提: 1 段目の判定に到達していない")
	}
	if took {
		t.Fatalf("2 段目が busy + zero-mtime を「既に退けられた」と読んで took=true を返した (fail-open)")
	}
	if !errors.Is(err, errUnreadableLock) {
		t.Fatalf("2 段目が分類していない (CLI が無音になる): %v", err)
	}
}

// 🚨 **`own` を「開いた実体」で控えていること自体を pin する** (3 周目 P1-2 / 4 周目 P2-2)。
//
// `f.Stat()` を `os.Lstat(l.lockPath())` に変えても既存テストは全緑だった。照合を**する**ことは
// 守られていたが、**何を**照合するかは無検査で、パス版に戻すと 2 周目 P1 (他ホストの生きた lock を
// 消す) がそのまま再生産される。区別できなかったのは、割り込める seam が照合の**後**にしか
// 無かったため。
func TestTryPlaceCapturesIdentityFromFdNotPath(t *testing.T) {
	l := newTestLocker(t)
	other, err := NewLocker(l.dir, 5*time.Second)
	if err != nil {
		t.Fatalf("NewLocker(other): %v", err)
	}
	oldLink := tryPlaceLinkFn
	tryPlaceLinkFn = func(string, string) error { return syscall.ENOTSUP }
	defer func() { tryPlaceLinkFn = oldLink }()

	var victim *Meta
	inner := false
	oldBefore := tryPlaceBeforeIdentityHook
	tryPlaceBeforeIdentityHook = func() {
		if inner {
			return // 内側の other.Acquire では発火させない (同じ枝を通るため)
		}
		inner = true
		// 🚨 **実体を控える前**に、人が break を打ち、別ホストが正規に取得する
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
	}
	defer func() { tryPlaceBeforeIdentityHook = oldBefore }()

	oldAfter := tryPlaceAfterCreateHook
	tryPlaceAfterCreateHook = func(f *os.File) {
		if victim != nil {
			_ = f.Close() // 自分の Write を失敗させる (別ホストの取得が済んだ後で)
		}
	}
	defer func() { tryPlaceAfterCreateHook = oldAfter }()

	if _, err := l.Acquire(time.Minute, "stalled-host"); err == nil {
		t.Fatalf("書けないのに成功した")
	}
	if victim == nil {
		t.Fatalf("前提: 別ホストの取得が成立していない")
	}
	m, _, err := l.readLock()
	if err != nil || m == nil {
		t.Fatalf("別ホストの生きた lock を消した (二重実行になる): err=%v", err)
	}
	if m.Token != victim.Token {
		t.Fatalf("別ホストの lock が別物に置き換わっている: got=%s want=%s", m.Token, victim.Token)
	}
}

// 🚨 **サーバ時刻を取れないとき、age を 0 に丸めない** (敵対レビュー 5 周目 P2-1)。
//
// 旧版は `serverNow` のエラーを捨てて `AgeSec` を 0 のままにしていたので、**3 時間前の残骸が
// `age=0s` = 「いま書かれたばかり」**として出た。この状態の存在理由は「人が一過性と恒久を
// 見分けるための材料を出すこと」なので、材料が嘘をつくのは無材料より悪い。
// 発火条件 (probe を作れない / stat できない) は、権限・ENOSPC・詰まったマウントなど
// **まさに wedge を疑って status を打つ場面と相関する**。
func TestStatusDoesNotFakeAgeWhenServerTimeIsUnavailable(t *testing.T) {
	l := newTestLocker(t)
	if _, err := l.Acquire(time.Hour, "holder"); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := os.Truncate(l.lockPath(), 0); err != nil {
		t.Fatalf("Truncate: %v", err)
	}
	old := time.Now().Add(-3 * time.Hour)
	if err := os.Chtimes(l.lockPath(), old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	// probe を作れなくして serverNow を壊す (権限・ENOSPC・詰まったマウントの代理)
	probe := filepath.Join(l.metaDir, probeDirName)
	if err := os.Chmod(probe, 0o555); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(probe, 0o755) })

	var rc int
	out := captureStdout(t, func() { rc = run([]string{"status", l.dir}) })
	if rc != exitBusy {
		t.Fatalf("status rc=%d (exitBusy=%d を期待)", rc, exitBusy)
	}
	if strings.Contains(out, "age=0s") {
		t.Fatalf("取れなかった age を 0 に丸めて出している (3 時間前の残骸が「いま」に見える): %q", out)
	}
	if !strings.Contains(out, "age=不明") {
		t.Fatalf("age が取れなかったことを出していない: %q", out)
	}
	var rcj int
	outj := captureStdout(t, func() { rcj = run([]string{"status", "--json", l.dir}) })
	if rcj != exitBusy {
		t.Fatalf("status --json rc=%d", rcj)
	}
	if strings.Contains(outj, `"age_seconds"`) {
		t.Fatalf("取れなかった age を JSON に出している (機械が 0 を真に受ける): %q", outj)
	}
}

// `status` が出す判断材料 (size / age) は、**読んだ実体**から取ること
// (敵対レビュー 380 の 2 周目 P1-1)。
//
// 🚨 `readLock` の後に名前を `os.Lstat` し直すと、そのあいだ (`io.ReadAll` = SMB 1 往復) に
// 引き継がれたとき **読んでいない別のファイルの数字**を「この lock の判断材料」として出す。
// 人間向けの案内は「経過が伸び続けるなら残骸」と**その数字を使う手順を名指ししている**ので、
// 置き換えが続くかぎり age が 0 に戻り続け、人は「一過性だ、待とう」に倒れ続ける。
// これは `Release` で直したのと同じ形 (1 周目 P1-1 = identity の土台を別 syscall から取り直す)。
func TestInspectReportsSizeAndAgeOfTheFileItRead(t *testing.T) {
	l := newTestLocker(t)
	if _, err := l.Acquire(time.Hour, "holder"); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	// canonical な unreadable lock (0 バイト) を作り、はっきり古い mtime を打つ
	if err := os.Truncate(l.lockPath(), 0); err != nil {
		t.Fatalf("Truncate: %v", err)
	}
	const agedSec = 90
	old := time.Now().Add(-agedSec * time.Second)
	if err := os.Chtimes(l.lockPath(), old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	// 読んでいる最中に「健全な大きさ・いま打刻された」lock を被せる
	const swapSize = 200
	var swapped bool
	prev := readLockAfterStatHook
	readLockAfterStatHook = func() {
		if swapped {
			return
		}
		swapped = true
		tmp := l.lockPath() + ".swap"
		body := make([]byte, swapSize)
		for i := range body {
			body[i] = 'x'
		}
		if err := os.WriteFile(tmp, body, 0o644); err != nil {
			t.Errorf("WriteFile: %v", err)
			return
		}
		if err := os.Rename(tmp, l.lockPath()); err != nil {
			t.Errorf("Rename: %v", err)
		}
	}
	defer func() { readLockAfterStatHook = prev }()

	st, err := l.Inspect()
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !swapped {
		t.Fatalf("seam が発火していない (窓を再現できていないので、このテストは何も検査していない)")
	}
	if !st.Unreadable {
		t.Fatalf("中身を読めない lock として分類されていない: %+v", st)
	}
	if st.SizeBytes == nil {
		t.Fatalf("size を出していない")
	}
	if *st.SizeBytes != 0 {
		t.Fatalf("読んだ実体は 0 バイトなのに size=%dB (差し替え後のファイルを stat している)", *st.SizeBytes)
	}
	if st.AgeSec == nil {
		t.Fatalf("age を出していない")
	}
	// 実測の余裕: 読んだ実体は 90 秒前。差し替え後は 0 秒。両者は桁で離れている
	if *st.AgeSec < agedSec/2 {
		t.Fatalf("読んだ実体の mtime は %d 秒前なのに age=%ds (差し替え後のファイルを stat している)",
			agedSec, *st.AgeSec)
	}
}

// `Inspect` の**正常枝**も、中身と打刻を同じ実体から取ること
// (敵対レビュー 380 の 3 周目 P2-1)。
//
// 🚨 2 周目は unreadable 枝にしか変異を当てておらず、正常枝は無検査だった。
// こちらのほうが影響は大きい: `mtime` は `expired(now, mtime, holderTTL(m))` に入るので、
// `m` (中身) と `mtime` (打刻) が別ファイル由来になると **実在したことのないファイルについて
// 期限を評価する**ことになり、それが `check` の busy/free の終了コードと
// `status` の age / expires_in をそのまま決める。
func TestInspectUsesMtimeOfTheFileItReadForLiveLock(t *testing.T) {
	l := newTestLocker(t)
	m, err := l.Acquire(time.Hour, "holder")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	// 読む対象は「90 秒前に打刻された、生きている lock」
	const agedSec = 90
	old := time.Now().Add(-agedSec * time.Second)
	if err := os.Chtimes(l.lockPath(), old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	// 読んでいる最中に「いま打刻された、別 token の lock」を被せる
	var swapped bool
	prev := readLockAfterStatHook
	readLockAfterStatHook = func() {
		if swapped {
			return
		}
		swapped = true
		tmp := l.lockPath() + ".swap"
		body := []byte(`{"token":"SWAPPED","ttl_ms":3600000,"version":"lockman/1"}`)
		if err := os.WriteFile(tmp, body, 0o644); err != nil {
			t.Errorf("WriteFile: %v", err)
			return
		}
		if err := os.Rename(tmp, l.lockPath()); err != nil {
			t.Errorf("Rename: %v", err)
		}
	}
	defer func() { readLockAfterStatHook = prev }()

	st, err := l.Inspect()
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !swapped {
		t.Fatalf("seam が発火していない (窓を再現できていないので、このテストは何も検査していない)")
	}
	if !st.Held {
		t.Fatalf("生きている lock を保持中と答えていない: %+v", st)
	}
	// 中身は読んだ実体のもの。打刻も同じ実体のものでなければ「キメラ」になる
	if st.Token != m.Token {
		t.Fatalf("読んだ実体と違う中身を返した: got=%s want=%s", st.Token, m.Token)
	}
	if st.AgeSec == nil {
		t.Fatalf("age を出していない")
	}
	if *st.AgeSec < agedSec/2 {
		t.Fatalf("中身は読んだ実体 (%d 秒前) のものなのに age=%ds "+
			"(打刻だけ差し替え後のファイルから採っている = 実在しない組み合わせで期限を評価している)",
			agedSec, *st.AgeSec)
	}
}
