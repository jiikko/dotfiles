package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// --io-timeout (091:418) のテスト。着手時点で **0 件**だった (issue 357)。
//
// 詰まりは FIFO で作る。open がブロックする位置を指定できるので、「応答しないマウント」の
// 詰まる場所だけを本番と揃えられる。
// 🚨 **本番に FIFO は置かれない。** ここが証明するのは「包みが在り、その位置で詰まっても
// 期限内に戻る」という機構で、smbfs が実際にそこでブロックするかは別に実測が要る
// (`_claude/rules/verify-execution-not-just-exit-code.md`「隔離環境での失敗も本番の失敗ではない」)。

const testIOTimeout = 200 * time.Millisecond

// fifoLocker は .lockman を本物の経路で用意し、指定したファイルを FIFO へ差し替える。
func fifoLocker(t *testing.T, blockAt func(*Locker) string) *Locker {
	t.Helper()
	l, err := NewLocker(t.TempDir(), testIOTimeout)
	if err != nil {
		t.Fatalf("NewLocker: %v", err)
	}
	if err := l.ensureDirs(); err != nil {
		t.Fatalf("ensureDirs: %v", err)
	}
	path := blockAt(l)
	_ = os.Remove(path)
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("mkfifo %s: %v", path, err)
	}
	return l
}

func lockFile(l *Locker) string  { return l.lockPath() }
func stampFile(l *Locker) string { return filepath.Join(l.metaDir, cleanupStampName) }

// 🚨 待つのは合否の基準ではなく**安全網**。包みが無いと戻らないので、放置すると
// スイート全体が固まる (「赤」ではなく「終わらない」になり、変異検証も読めない)。
func boundedInt(t *testing.T, what string, fn func() int) int {
	t.Helper()
	ch := make(chan int, 1)
	go func() { ch <- fn() }()
	select {
	case got := <-ch:
		return got
	case <-time.After(20 * time.Second):
		t.Fatalf("%s が 20s 以内に戻らない (--io-timeout の包みが無い)", what)
		return -1
	}
}

// 包みは Locker の入口すべてに要る。1 つでも素通りすると --io-timeout の目的
// (スクリプトが無言で固まらない) が達成されない。
func TestIOTimeoutWrapsLockerEntries(t *testing.T) {
	for _, c := range []struct {
		name string
		call func(*Locker) error
	}{
		{"AcquireTimed", func(l *Locker) error { _, err := l.AcquireTimed(time.Minute, ""); return err }},
		{"RenewTimed", func(l *Locker) error { return l.RenewTimed("tok") }},
		{"ReleaseTimed", func(l *Locker) error { return l.ReleaseTimed("tok") }},
		{"InspectTimed", func(l *Locker) error { _, err := l.InspectTimed(); return err }},
	} {
		t.Run(c.name, func(t *testing.T) {
			l := fifoLocker(t, lockFile)
			ch := make(chan error, 1)
			go func() { ch <- c.call(l) }()
			select {
			case err := <-ch:
				if !errors.Is(err, errIOTimeout) {
					t.Fatalf("詰まっているのに判定不能を返さない: %v", err)
				}
			case <-time.After(20 * time.Second):
				t.Fatalf("%s が 20s 以内に戻らない (包みが無い)", c.name)
			}
		})
	}
}

// deferred Cleanup も包む (issue 357 の「未包み ②」)。
// 🚨 **タイムアウトは致命にしない** — 掃除は正しさに関与しない設計なので、
// 件数と一緒に Errors へ入れて呼び出し側に見せるだけ。
//
// 詰まる位置は `.cleanup_at`: stampCleanup が **write-only で open** するので、
// FIFO だと読み手が来るまで返らない (sweep の ReadDir / Remove は FIFO では詰まらない)。
func TestIOTimeoutWrapsDeferredCleanup(t *testing.T) {
	l := fifoLocker(t, stampFile)
	type out struct{ res CleanupResult }
	ch := make(chan out, 1)
	go func() { ch <- out{l.CleanupTimed(true)} }()
	select {
	case o := <-ch:
		if len(o.res.Errors) != 1 || !strings.HasPrefix(o.res.Errors[0], errIOTimeout.Error()) {
			t.Fatalf("掃除のタイムアウトが Errors に入っていない: %+v", o.res)
		}
		// 🚨 **「Removed は必ず 0」は消えた不変条件** (issue 393 / 敵対レビュー P3-1)。
		// 期限切れでも「どこまで進んだか」を報告する仕様になったので、この fixture で 0 なのは
		// **残骸を 1 件も置いていないから**であって、「進捗を捨てているから」ではない。
		// 旧 assert のまま fixture を現実的にすると、正しい挙動に**偽の赤**が出る。
		if !o.res.Partial {
			t.Errorf("期限切れなのに Partial が立っていない: %+v", o.res)
		}
		if o.res.Removed != 0 {
			t.Errorf("残骸を置いていないのに消したことになっている: %+v", o.res)
		}
		if o.res.Skipped {
			t.Errorf("force なのに skip された: %+v", o.res)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("CleanupTimed が 20s 以内に戻らない (包みが無い)")
	}
}

// 091:496 の受け入れ条件そのもの。**同じ --io-timeout で check も with も倒れること**、
// かつ「空いている」に倒れないこと。
// 着手前は check だけが 3.03s で倒れ、with は 10s 経っても戻らなかった (issue 357 の再現 A)。
func TestIOTimeoutFallsOverForCheckAndWith(t *testing.T) {
	l := fifoLocker(t, lockFile)
	dir := l.dir

	// check: 判定不能は busy 側へ倒す (「空いている」= exitOK と言わない)
	got := boundedInt(t, "check", func() int { return run([]string{"check", dir, "--io-timeout", "200ms"}) })
	if got != exitBusy {
		t.Errorf("check: exit %d (期待 %d = 判定不能は busy 側)", got, exitBusy)
	}

	// with: 091:418 の「超えたら with は 125」
	got = boundedInt(t, "with", func() int {
		return run([]string{"with", dir, "--io-timeout", "200ms", "--ttl", "30s", "--", "true"})
	})
	if got != exitWithInvalid {
		t.Errorf("with: exit %d (期待 %d = 判定不能)", got, exitWithInvalid)
	}
}

// Renew の失敗を「lease 喪失 (122)」と「判定不能 (125)」に分ける (091:398-399)。
// 単一の bool に畳むと、一過性の I/O ヒカップが「走行中に引き継がれた」と報告される。
func TestWithSeparatesLostLeaseFromIndeterminate(t *testing.T) {
	for _, c := range []struct {
		name string
		// breakIt は取得済みの lock に「その壊れ方」を起こす
		breakIt func(t *testing.T, l *Locker)
		want    int
	}{
		{
			name:    "lease を失った (他者が引き継いだ) は 122",
			breakIt: func(t *testing.T, l *Locker) { _ = l.Break() },
			want:    exitWithLost,
		},
		{
			name: "I/O が詰まっただけなら 125 (lease は失っていない)",
			breakIt: func(t *testing.T, l *Locker) {
				// 🚨 消してから作ると「lock が無い」= errNotOwner を拾う窓ができる。
				// 別名で作って rename で被せる (窓を作らない)。
				tmp := l.lockPath() + ".fifo"
				if err := syscall.Mkfifo(tmp, 0o600); err != nil {
					t.Fatalf("mkfifo: %v", err)
				}
				if err := os.Rename(tmp, l.lockPath()); err != nil {
					t.Fatalf("rename: %v", err)
				}
			},
			want: exitWithInvalid,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			l, err := NewLocker(t.TempDir(), testIOTimeout)
			if err != nil {
				t.Fatalf("NewLocker: %v", err)
			}
			// 🚨 ttl は runWith へ直接渡す。run() は minTTL (30s) を強制するので、
			// コマンド経由では renew の tick を待てない
			const ttl = 600 * time.Millisecond // tick = ttl/renewDivisor = 200ms
			go func() {
				// lock が置かれるのを待ってから壊す (壁時計で待たない)
				for range 400 {
					if _, err := os.Stat(l.lockPath()); err == nil {
						c.breakIt(t, l)
						return
					}
					time.Sleep(10 * time.Millisecond)
				}
			}()
			got := boundedInt(t, "runWith", func() int {
				return runWith(l, ttl, "", true, []string{"sh", "-c", "sleep 5"})
			})
			if got != c.want {
				t.Fatalf("exit %d (期待 %d)", got, c.want)
			}
		})
	}
}

// classifyRenewErr は上のテストが見ている分類そのもの。errNotOwner は %w でラップされて
// 返る経路があるので、== ではなく errors.Is で見ていることを固定する。
func TestClassifyRenewErr(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
		want renewOutcome
	}{
		{"errNotOwner はそのまま lease 喪失", errNotOwner, renewLost},
		// 🚨 文字列を連結しただけでは errors.Is が効かない = 判定不能側。ここを lease 喪失に
		// してしまうと「文面で判定する」形になり、メッセージを変えた瞬間に壊れる
		{"文字列連結しただけの errNotOwner は判定不能", errors.New("x: " + errNotOwner.Error()), renewIndeterminate},
		{"%w でラップされた errNotOwner は lease 喪失", wrapNotOwner(), renewLost},
		{"I/O タイムアウトは判定不能", errIOTimeout, renewIndeterminate},
		{"その他の I/O エラーも判定不能", os.ErrPermission, renewIndeterminate},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := classifyRenewErr(c.err); got != c.want {
				t.Fatalf("classifyRenewErr(%v) = %d, want %d", c.err, got, c.want)
			}
		})
	}
}

// wrapNotOwner は lock.go が実際に返す形 (%w でラップした errNotOwner) を作る。
func wrapNotOwner() error {
	return fmt.Errorf("%w: lease が切れている (走行中に引き継がれた可能性)", errNotOwner)
}

// --io-timeout の値検証。0 / 負値は**全操作を即「判定不能」**にするので受け付けない
// (0 を「無制限」と読む CLI が多く、黙って受けると意図と逆に倒れる)。
// 上限も要る: 大きすぎると cleanup の minRetention が前提にしている
// 「--io-timeout は十分小さい」が崩れる。
func TestIOTimeoutValueIsValidated(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		arg  string
		want int
	}{
		{"0", exitError},
		{"-1s", exitError},
		{"1ns", exitError}, // 下限未満。全部が判定不能になる
		{"10h", exitError}, // 上限超え
		{"200ms", exitOK},  // 受理される (下限ちょうど上)
		{"5m", exitOK},     // 上限ちょうど
	} {
		t.Run(c.arg, func(t *testing.T) {
			got := run([]string{"check", dir, "--io-timeout", c.arg})
			if got != c.want {
				t.Fatalf("check --io-timeout %s: exit %d (期待 %d)", c.arg, got, c.want)
			}
		})
	}
	// with は番号空間が違う (091:398-399)
	if got := run([]string{"with", dir, "--io-timeout", "0", "--ttl", "30s", "--", "true"}); got != exitWithInvalid {
		t.Errorf("with --io-timeout 0: exit %d (期待 %d)", got, exitWithInvalid)
	}
}
