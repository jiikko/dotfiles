package main

import (
	"errors"
	"os"
	"strings"
	"sync"
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
