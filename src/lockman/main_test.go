package main

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
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
// ★ 掃除が走るのは状態を変える 4 コマンドちょうど。
//
// 🚨 かつて正の側は `acquire` しか見ておらず、**`with` / `break` を配線から外す変異が
// 緑で通った** (5 周目の実測)。テスト名は集合の契約を主張しているので、集合の全要素を
// 正の側でも負の側でも pin する。
func TestCleanupRunsOnlyForMutatingCommands(t *testing.T) {
	// 正の側: 4 コマンドそれぞれで打刻が現れる。
	for _, cmd := range []string{"acquire", "with", "break", "cleanup"} {
		dir := t.TempDir()
		// break / cleanup は .lockman が既にある状態が前提なので先に作る。
		if cmd == "break" || cmd == "cleanup" {
			if got := run([]string{"acquire", dir, "--token-file", tokenPath(t)}); got != exitOK {
				t.Fatalf("準備の acquire: exit %d", got)
			}
		}
		stamp := filepath.Join(dir, metaDirName, cleanupStampName)
		_ = os.Remove(stamp)
		if _, err := os.Stat(stamp); !os.IsNotExist(err) {
			t.Fatalf("前提が作れていない: %s の前に打刻が残っている (%v)", cmd, err)
		}

		var args []string
		switch cmd {
		case "acquire":
			args = []string{"acquire", dir, "--token-file", tokenPath(t)}
		case "with":
			args = []string{"with", dir, "--", "true"}
		default:
			args = []string{cmd, dir}
		}
		run(args)

		if _, err := os.Stat(stamp); err != nil {
			t.Fatalf("%s で掃除が走っていない: %v", cmd, err)
		}
	}

	// 負の側: それ以外は走らせない。release / renew は状態を変えるが、掃除は
	// acquire / with / break / cleanup の 4 つだけに配線されている (集合を pin する)。
	dir := t.TempDir()
	stamp := filepath.Join(dir, metaDirName, cleanupStampName)
	if got := run([]string{"acquire", dir, "--token-file", tokenPath(t)}); got != exitOK {
		t.Fatalf("準備の acquire: exit %d", got)
	}
	if err := os.Remove(stamp); err != nil {
		t.Fatalf("remove stamp: %v", err)
	}
	for _, args := range [][]string{
		{"check", dir}, {"status", dir},
		{"release", dir, "--token", "deadbeef"}, {"renew", dir, "--token", "deadbeef"},
	} {
		run(args)
		if _, err := os.Stat(stamp); !os.IsNotExist(err) {
			t.Fatalf("%s で掃除が走った (配線されていないはず)", args[0])
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

// ★ 掃除の失敗は verbose でなくても stderr に出る。件数も一緒に出す。
//
// 🚨 この分岐は変異で丸ごと消しても、`o.verbose` に戻しても緑だった (issue 358 の
// 敵対レビュー 4 周目が実測)。commit の見出しにしていた挙動が無検査だったので足す。
func TestDispatchWarnsOnCleanupFailureWithoutVerbose(t *testing.T) {
	l := newTestLocker(t)
	if err := l.ensureDirs(); err != nil {
		t.Fatalf("ensureDirs: %v", err)
	}
	tmpDir := filepath.Join(l.metaDir, tmpDirName)
	touchOld(t, filepath.Join(tmpDir, "old.json"), 2*time.Hour)
	if err := os.Chmod(tmpDir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(tmpDir, metaDirMode) })

	got := captureStderr(t, func() { dispatch("cleanup", l, &opts{}, nil) })

	// 🚨 部分一致で pin しない。`strings.Contains(got, "removed=")` だけだと
	// **書式から `skipped=` を落とす変異が緑で通る** (5 周目の実測)。行の構造を
	// 丸ごと固定する (可変なのはパスを含むエラー本文だけ)。
	want := regexp.MustCompile(
		`^lockman: cleanup: removed=\d+ skipped=(?:true|false) errors=\[.*permission denied.*\]\n$`)
	if !want.MatchString(got) {
		t.Fatalf("verbose 無しの警告が想定の書式で出ない:\n got=%q\nwant=%s", got, want)
	}
}

// captureStderr は fn の実行中の os.Stderr を集める。warnf が直接 os.Stderr へ書くため。
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stderr
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	os.Stderr = orig
	_ = w.Close()
	out := <-done
	_ = r.Close()
	return out
}
