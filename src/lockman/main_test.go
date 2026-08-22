package main

import (
	"os"
	"path/filepath"
	"testing"
)

// tokenPath は --token-file の置き場。テスト用の使い捨て。
func tokenPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "token")
}

func TestAcquireThenSecondAcquireIsBusy(t *testing.T) {
	dir := t.TempDir()
	tok := tokenPath(t)
	if got := run([]string{"acquire", dir, "--token-file", tok}); got != exitOK {
		t.Fatalf("1 回目の acquire: exit %d (期待 %d)", got, exitOK)
	}
	if got := run([]string{"acquire", dir}); got != exitBusy {
		t.Fatalf("2 回目の acquire: exit %d (期待 %d)", got, exitBusy)
	}
	if got := run([]string{"check", dir}); got != exitBusy {
		t.Fatalf("check: exit %d (期待 %d)", got, exitBusy)
	}
	if got := run([]string{"release", dir, "--token-file", tok}); got != exitOK {
		t.Fatalf("release: exit %d (期待 %d)", got, exitOK)
	}
	if got := run([]string{"check", dir}); got != exitOK {
		t.Fatalf("解放後の check: exit %d (期待 %d)", got, exitOK)
	}
}

func TestReleaseWithForeignTokenIsNotOwner(t *testing.T) {
	dir := t.TempDir()
	if got := run([]string{"acquire", dir, "--token-file", tokenPath(t)}); got != exitOK {
		t.Fatalf("acquire: exit %d", got)
	}
	if got := run([]string{"release", dir, "--token", "deadbeef"}); got != exitNotOwner {
		t.Fatalf("他人のトークンでの release: exit %d (期待 %d)", got, exitNotOwner)
	}
}

// 対象ディレクトリが無いことを「使用中」に倒さない (別物なので区別する)。
func TestMissingDirIsErrorNotBusy(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-dir")
	if got := run([]string{"acquire", missing}); got != exitError {
		t.Fatalf("存在しない dir: exit %d (期待 %d)", got, exitError)
	}
}

// 短い TTL は SMB の属性キャッシュ遅延に埋もれて誤判定を生む。警告ではなく拒否する。
func TestTooShortTTLIsRejected(t *testing.T) {
	dir := t.TempDir()
	if got := run([]string{"acquire", dir, "--ttl", "1s"}); got != exitError {
		t.Fatalf("--ttl 1s: exit %d (期待 %d)", got, exitError)
	}
	if _, err := os.Stat(filepath.Join(dir, metaDirName, lockName)); !os.IsNotExist(err) {
		t.Fatal("拒否したのに lock を作っている")
	}
}

// with は子の終了コードを透過し、ロック側の失敗は子と衝突しない番号を使う。
func TestWithPassesThroughChildExitCode(t *testing.T) {
	dir := t.TempDir()
	// 子が 3 で終わっても「busy」に見えてはいけない (これが衝突の本体)。
	if got := run([]string{"with", dir, "--", "sh", "-c", "exit 3"}); got != 3 {
		t.Fatalf("子の exit 3: got %d", got)
	}
	if got := run([]string{"with", dir, "--", "sh", "-c", "exit 0"}); got != exitOK {
		t.Fatalf("子の exit 0: got %d", got)
	}
}

func TestWithReportsBusyWithoutCollidingWithChild(t *testing.T) {
	dir := t.TempDir()
	if got := run([]string{"acquire", dir, "--token-file", tokenPath(t)}); got != exitOK {
		t.Fatalf("acquire: exit %d", got)
	}
	if got := run([]string{"with", dir, "--", "sh", "-c", "exit 0"}); got != exitWithBusy {
		t.Fatalf("busy の with: exit %d (期待 %d)", got, exitWithBusy)
	}
}

func TestWithRequiresCommand(t *testing.T) {
	if got := run([]string{"with", t.TempDir()}); got != exitWithInvalid {
		t.Fatalf("コマンド無しの with: exit %d (期待 %d)", got, exitWithInvalid)
	}
}

// 掃除は状態を変えるコマンドのときだけ走らせる (check / status はループから呼ばれ、
// SMB の readdir が重い)。掃除済みの目印が付くかどうかで観測する。
func TestCleanupRunsOnlyForMutatingCommands(t *testing.T) {
	dir := t.TempDir()
	stamp := filepath.Join(dir, metaDirName, cleanupStampName)

	if got := run([]string{"acquire", dir, "--token-file", tokenPath(t)}); got != exitOK {
		t.Fatalf("acquire: exit %d", got)
	}
	if _, err := os.Stat(stamp); err != nil {
		t.Fatalf("acquire で掃除が走っていない: %v", err)
	}
	if err := os.Remove(stamp); err != nil {
		t.Fatalf("remove stamp: %v", err)
	}
	for _, cmd := range []string{"check", "status"} {
		run([]string{cmd, dir})
		if _, err := os.Stat(stamp); !os.IsNotExist(err) {
			t.Fatalf("%s で掃除が走った (読み取り専用では走らせない)", cmd)
		}
	}
}

func TestUnknownCommandAndHelp(t *testing.T) {
	if got := run([]string{"acquir"}); got != exitError {
		t.Errorf("未知のサブコマンド: exit %d (期待 %d)", got, exitError)
	}
	if got := run(nil); got != exitError {
		t.Errorf("引数なし: exit %d (期待 %d)", got, exitError)
	}
	for _, arg := range []string{"-h", "--help", "help"} {
		if got := run([]string{arg}); got != exitOK {
			t.Errorf("%s: exit %d (期待 %d)", arg, got, exitOK)
		}
	}
}

// 終了コードの番号が互いに衝突していないこと。with 系は子プロセスの終了コードと
// 区別できる必要があるため 120 より上に置く (issue 091 の表)。
func TestExitCodesDoNotCollide(t *testing.T) {
	seen := map[int]string{}
	for name, code := range map[string]int{
		"exitOK": exitOK, "exitError": exitError, "exitBusy": exitBusy,
		"exitNotOwner": exitNotOwner, "exitWithBusy": exitWithBusy,
		"exitWithLost": exitWithLost, "exitWithInvalid": exitWithInvalid,
	} {
		if prev, dup := seen[code]; dup {
			t.Errorf("exit code %d が %s と %s で重複している", code, prev, name)
		}
		seen[code] = name
	}
	for name, code := range map[string]int{
		"exitWithBusy": exitWithBusy, "exitWithLost": exitWithLost, "exitWithInvalid": exitWithInvalid,
	} {
		if code <= 120 {
			t.Errorf("%s = %d は子プロセスの終了コードと衝突しうる (> 120 にすること)", name, code)
		}
	}
}
