package chromecookie

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// testApp はテストが使う作業領域の名前（本物のツールと重ならない名前にする）。
const testApp = "chromecookie-test"

// testWS はテストが共有する Workspace。isolateTemp が登録簿を毎回空に戻す。
var testWS = NewWorkspace(testApp)

// isolateTemp は作業領域をテスト専用にする（本物の ~/Library/Caches を触らない）。
// 作業領域は HOME 基準（os.UserCacheDir）なので、HOME を差し替えれば隔離できる。
// 戻り値は作業領域の親（= tempRoot の 1 つ上）。
func isolateTemp(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	testWS.mu.Lock()
	testWS.paths = map[string]struct{}{}
	testWS.closing = false
	testWS.mu.Unlock()
	// 「終了中」を立てるテストの後に、別のテストが一時ディレクトリを作れなくならないように戻す。
	t.Cleanup(func() {
		testWS.mu.Lock()
		testWS.closing = false
		testWS.mu.Unlock()
	})

	parent, err := testWS.tempRootParent()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	return parent
}

// registerCleanup は後始末の登録簿へ直接書き込む（登録経路が壊れた状態を模すテスト用）。
// 本番の登録は newTempDirWith がロックの下で作成と同時に行う。
func registerCleanup(path string) {
	testWS.mu.Lock()
	defer testWS.mu.Unlock()
	testWS.paths[path] = struct{}{}
}

// deadPID は確実に終了済みのプロセス ID を返す。
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("/usr/bin/true")
	if err := cmd.Run(); err != nil {
		t.Skipf("/usr/bin/true を実行できない: %v", err)
	}
	pid := cmd.Process.Pid
	if processAlive(pid) {
		t.Skipf("pid %d がまだ生きている（再利用の可能性）", pid)
	}
	return pid
}

// 一時ディレクトリは①（defer）でも②（シグナル経路の RunAllCleanups）でも消えること。
// 成功パスのあとに残骸が 0 件であることまで見る。
func TestTempDirIsRemovedByBothPaths(t *testing.T) {
	isolateTemp(t)

	// ①: cleanup 関数で消える
	dir, cleanup, err := testWS.NewTempDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("作られていない: %v", err)
	}
	if !strings.HasPrefix(filepath.Base(dir), strconv.Itoa(os.Getpid())+"-") {
		t.Errorf("ディレクトリ名に pid が入っていない（③の掃除が効かなくなる）: %s", filepath.Base(dir))
	}
	cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("①で消えていない: %v", err)
	}

	// ②: RunAllCleanups（シグナル経路）で消える
	dir2, _, err := testWS.NewTempDir()
	if err != nil {
		t.Fatal(err)
	}
	testWS.RunAllCleanups()
	if _, err := os.Stat(dir2); !os.IsNotExist(err) {
		t.Errorf("②で消えていない: %v", err)
	}

	// 残骸ゼロ（成功パスの後に一時領域が空であること）
	entries, err := os.ReadDir(testWS.tempRoot())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("一時領域に残骸がある: %d 件", len(entries))
	}
}

// ③: 起動時の掃除は「自分が作った親の直下 / <pid>- 形式 / その pid が死んでいる」の
// 3 条件を満たすものだけを消すこと。1 つでも欠けたら触らない。
func TestSweepRemovesOnlyDeadOwnDirs(t *testing.T) {
	isolateTemp(t)
	root, err := testWS.ensureTempRoot()
	if err != nil {
		t.Fatal(err)
	}

	dead := filepath.Join(root, strconv.Itoa(deadPID(t))+"-abc")
	mine := filepath.Join(root, strconv.Itoa(os.Getpid())+"-abc")
	alive := filepath.Join(root, "1-abc") // pid 1 (launchd) は生きている
	weird := filepath.Join(root, "notapid-abc")
	// 🚨 「死んだ pid」の名前にすること。適当な数字（1234 等）だと、その pid が
	// たまたま生きている機械では processAlive に守られてしまい、
	// 「ディレクトリ以外は対象外」の検査が効いているかを確かめられない。
	plainFile := filepath.Join(root, strconv.Itoa(deadPID(t))+"-file")
	for _, d := range []string{dead, mine, alive, weird} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(plainFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	testWS.SweepStaleTempDirs()

	if _, err := os.Stat(dead); !os.IsNotExist(err) {
		t.Errorf("死んだプロセスの残骸が消えていない: %v", err)
	}
	for _, keep := range []string{mine, alive, weird, plainFile} {
		if _, err := os.Stat(keep); err != nil {
			t.Errorf("消してはいけないものを消した: %s (%v)", keep, err)
		}
	}
}

// 一時領域の名前は他ツール（esa-cli の esa-cookie 等）と共有しないこと。
//
// 🚨 共有すると③の掃除の母集合に別ツールの実行中ディレクトリが入る。
func TestTempRootIsToolSpecific(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := testWS.tempRoot()
	if !strings.Contains(root, string(filepath.Separator)+testApp+string(filepath.Separator)) {
		t.Errorf("ツールごとの名前（app）を含むパスにすべき: %q", root)
	}
	if other := NewWorkspace("other-tool").tempRoot(); other == root {
		t.Errorf("別の app と作業領域が同じ: %q", root)
	}
	// 🚨 $TMPDIR 配下に置かないこと（呼び出し側が場所を動かせるうえ、
	// 未設定時の /tmp は誰でも書けるので別ユーザーに居座られる）。
	if !strings.HasPrefix(root, home) {
		t.Errorf("HOME 基準の固定パスにすべき: %q (HOME=%q)", root, home)
	}
	t.Setenv("TMPDIR", filepath.Join(home, "elsewhere"))
	if got := testWS.tempRoot(); got != root {
		t.Errorf("TMPDIR で作業領域が動いている: %q -> %q", root, got)
	}
}

// 🚨 ③（破壊的な掃除）が、検証を通らない一時領域に一切触れないこと。
//
// 発火条件: $TMPDIR/<app>-extract を別ディレクトリへのシンボリックリンクにし、
// その先に「死んだ pid の名前」のディレクトリを置く。以前の実装は検証（ensureTempRoot）を
// 通らず os.ReadDir → os.RemoveAll していたため、**リンク先を再帰削除した**。
func TestSweepRefusesUnverifiedRoot(t *testing.T) {
	parent := isolateTemp(t)

	victim := filepath.Join(parent, "victim")
	stale := filepath.Join(victim, strconv.Itoa(deadPID(t))+"-abc")
	if err := os.MkdirAll(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	important := filepath.Join(stale, "important.txt")
	if err := os.WriteFile(important, []byte("消えてはいけない"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, testWS.tempRoot()); err != nil {
		t.Skipf("シンボリックリンクを作れない: %v", err)
	}

	testWS.SweepStaleTempDirs()

	if _, err := os.Stat(important); err != nil {
		t.Errorf("作業領域の外のファイルが削除された: %v", err)
	}
	if _, err := os.Stat(stale); err != nil {
		t.Errorf("作業領域の外のディレクトリが削除された: %v", err)
	}
}

// 🚨 **相対**シンボリックリンクでも拒否すること。
//
// os.Root が拒否するのは「ルートの外へ出る」リンクだけで、**ルート内に留まる相対リンクは追う**
// （実測で確認）。つまり絶対リンクだけを試すテストは、検証本体を一度も通らずに緑になる。
// 1 周目のテストがまさにその形で、検証を全部消しても緑のままだった。
func TestSweepRefusesRelativeSymlinkRoot(t *testing.T) {
	parent := isolateTemp(t)

	victim := filepath.Join(parent, "victim")
	stale := filepath.Join(victim, strconv.Itoa(deadPID(t))+"-abc")
	if err := os.MkdirAll(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	important := filepath.Join(stale, "important.txt")
	if err := os.WriteFile(important, []byte("消えてはいけない"), 0o600); err != nil {
		t.Fatal(err)
	}
	// 相対リンク（親ディレクトリの中に留まるので os.Root は追ってしまう）
	if err := os.Symlink("victim", testWS.tempRoot()); err != nil {
		t.Skipf("シンボリックリンクを作れない: %v", err)
	}

	testWS.SweepStaleTempDirs()

	if _, err := os.Stat(important); err != nil {
		t.Errorf("相対シンボリックリンクの先が削除された: %v", err)
	}
}

// 🚨 ①（NewTempDir が返す cleanup。defer で呼ぶ）も③と同じ検証を通ること。
//
// ①の窓は「一時ディレクトリを作ってから defer が走るまで」＝作業中ずっとなので、
// ②③より構造的に広い。ここだけパス文字列の os.RemoveAll に戻すと穴が復活する。
func TestDeferCleanupRefusesUnverifiedRoot(t *testing.T) {
	parent := isolateTemp(t)

	dir, cleanup, err := testWS.NewTempDir()
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(dir)

	victim := filepath.Join(parent, "victim")
	if err := os.MkdirAll(filepath.Join(victim, name), 0o700); err != nil {
		t.Fatal(err)
	}
	important := filepath.Join(victim, name, "important.txt")
	if err := os.WriteFile(important, []byte("消えてはいけない"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(testWS.tempRoot()); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, testWS.tempRoot()); err != nil {
		t.Skipf("シンボリックリンクを作れない: %v", err)
	}

	cleanup()

	if _, err := os.Stat(important); err != nil {
		t.Errorf("①がシンボリックリンクの先を削除した: %v", err)
	}
}

// 🚨 検証は最後の 1 コンポーネントだけでは足りない。
// 途中のディレクトリ（~/Library/Caches/<app>）を差し替えれば、
// 検証済みの root ごと任意の場所へ移せる。
func TestParentComponentIsVerified(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	testWS.mu.Lock()
	testWS.paths = map[string]struct{}{}
	testWS.mu.Unlock()

	caches := filepath.Join(home, "Library", "Caches")
	if err := os.MkdirAll(caches, 0o700); err != nil {
		t.Fatal(err)
	}
	// 攻撃者が用意した先。中身は「掃除の条件に合う」形にしておく。
	attacker := filepath.Join(home, "attacker")
	stale := filepath.Join(attacker, "extract", strconv.Itoa(deadPID(t))+"-abc")
	if err := os.MkdirAll(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	important := filepath.Join(stale, "important.txt")
	if err := os.WriteFile(important, []byte("消えてはいけない"), 0o600); err != nil {
		t.Fatal(err)
	}
	// 親（<app>）自体を symlink にする。
	if err := os.Symlink(attacker, filepath.Join(caches, testApp)); err != nil {
		t.Skipf("シンボリックリンクを作れない: %v", err)
	}

	if _, err := testWS.openVerifiedTempRoot(); err == nil {
		t.Error("親がシンボリックリンクでも検証を通ってしまう")
	}

	testWS.SweepStaleTempDirs()
	if _, err := os.Stat(important); err != nil {
		t.Errorf("作業領域の外が削除された: %v", err)
	}
}

// 登録経路が壊れて作業領域の外のパスが登録されても、触らないこと。
func TestRunAllCleanupsIgnoresPathsOutsideRoot(t *testing.T) {
	parent := isolateTemp(t)
	if _, err := testWS.ensureTempRoot(); err != nil {
		t.Fatal(err)
	}

	outside := filepath.Join(parent, "outside")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(outside, "keep.txt")
	if err := os.WriteFile(keep, []byte("消えてはいけない"), 0o600); err != nil {
		t.Fatal(err)
	}
	registerCleanup(outside)

	testWS.RunAllCleanups()

	if _, err := os.Stat(keep); err != nil {
		t.Errorf("作業領域の外に登録されたパスを削除した: %v", err)
	}
}

// 🚨 pid の生死だけで判定しない。番号が再利用されると永久に消えなくなるため、
// 十分に古いものは生存判定に関わらず消すこと。
func TestSweepRemovesStaleEntriesEvenIfPidAlive(t *testing.T) {
	isolateTemp(t)
	root, err := testWS.ensureTempRoot()
	if err != nil {
		t.Fatal(err)
	}

	// pid 1 (launchd) は常に生きている = 生存判定だけなら永久に残る名前。
	old := filepath.Join(root, "1-ancient")
	if err := os.MkdirAll(old, 0o700); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-staleAge - time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	// 同じく生きている pid だが、新しいもの（並行実行中かもしれない）。
	fresh := filepath.Join(root, "1-fresh")
	if err := os.MkdirAll(fresh, 0o700); err != nil {
		t.Fatal(err)
	}

	testWS.SweepStaleTempDirs()

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("古い残骸が消えていない（pid 再利用で永久に残る）: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("生きている pid の新しいディレクトリを消した: %v", err)
	}
}

// 作業領域のパーミッションは自分だけ（0700）であること。
func TestTempRootPermissions(t *testing.T) {
	isolateTemp(t)
	root, err := testWS.ensureTempRoot()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("作業領域のパーミッション: got %o, want 700", perm)
	}
}

// HOME が相対パスだと作業領域が cwd 依存になるので拒否すること。
func TestRelativeHomeIsRejected(t *testing.T) {
	t.Setenv("HOME", "relative/home")
	if _, err := testWS.tempRootParent(); err == nil {
		t.Error("相対パスの HOME を受け入れてはいけない（cwd 依存になる）")
	}
}

// 🚨 ①②（defer / シグナル経路）も③と同じ検証を通ること。
//
// ③だけを塞いで①②に os.RemoveAll(パス文字列) を残すと、同じ差し替えで
// リンク先を消せる。しかも①②は「資格情報を読むたび」に走るので窓はむしろ広い。
func TestRunAllCleanupsRefusesUnverifiedRoot(t *testing.T) {
	parent := isolateTemp(t)

	// 正規の手順で作業領域と一時ディレクトリを作り、後始末に登録する。
	dir, _, err := testWS.NewTempDir()
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(dir)

	// 作業領域を victim へ差し替える（同名のディレクトリを用意しておく）。
	victim := filepath.Join(parent, "victim")
	if err := os.MkdirAll(filepath.Join(victim, name), 0o700); err != nil {
		t.Fatal(err)
	}
	important := filepath.Join(victim, name, "important.txt")
	if err := os.WriteFile(important, []byte("消えてはいけない"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(testWS.tempRoot()); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, testWS.tempRoot()); err != nil {
		t.Skipf("シンボリックリンクを作れない: %v", err)
	}

	testWS.RunAllCleanups()

	if _, err := os.Stat(important); err != nil {
		t.Errorf("後始末がシンボリックリンクの先を削除した: %v", err)
	}
}

// エントリ側がシンボリックリンクでも、その先を消さないこと。
func TestSweepIgnoresSymlinkEntries(t *testing.T) {
	parent := isolateTemp(t)
	root, err := testWS.ensureTempRoot()
	if err != nil {
		t.Fatal(err)
	}

	victim := filepath.Join(parent, "victim")
	if err := os.MkdirAll(victim, 0o700); err != nil {
		t.Fatal(err)
	}
	important := filepath.Join(victim, "important.txt")
	if err := os.WriteFile(important, []byte("消えてはいけない"), 0o600); err != nil {
		t.Fatal(err)
	}
	// 死んだ pid の名前でリンクを張る（掃除の条件に合致する名前にする）。
	link := filepath.Join(root, strconv.Itoa(deadPID(t))+"-link")
	if err := os.Symlink(victim, link); err != nil {
		t.Skipf("シンボリックリンクを作れない: %v", err)
	}

	testWS.SweepStaleTempDirs()

	if _, err := os.Stat(important); err != nil {
		t.Errorf("リンク先のファイルが削除された: %v", err)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Errorf("ディレクトリ以外は対象外のはずが、リンク自身が削除された: %v", err)
	}
}

// 親ディレクトリが自分のものでない（シンボリックリンク等）なら使わないこと。
func TestEnsureTempRootRejectsSymlink(t *testing.T) {
	parent := isolateTemp(t)
	other := filepath.Join(parent, "elsewhere")
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, testWS.tempRoot()); err != nil {
		t.Skipf("シンボリックリンクを作れない: %v", err)
	}
	if _, err := testWS.ensureTempRoot(); err == nil {
		t.Error("シンボリックリンクの一時領域を受け入れてはいけない")
	}
}

// pid の形式判定（③の母集合を決める）。
func TestPidFromTempDirName(t *testing.T) {
	cases := map[string]int{
		"123-abc": 123,
		"1-x":     1,
		"abc-1":   0,
		"-1-x":    0,
		"0-x":     0,
		"123":     0,
		"":        0,
		// 🚨 Atoi は受けるが MkdirTemp が作らない形。掃除の母集合を広げないよう拒否する。
		"+1-x":               0,
		"007-x":              0,
		"0000000000000001-x": 0,
		" 1-x":               0,
	}
	for name, want := range cases {
		got, ok := pidFromTempDirName(name)
		if want == 0 {
			if ok {
				t.Errorf("%q: 対象外にすべき（got %d）", name, got)
			}
			continue
		}
		if !ok || got != want {
			t.Errorf("%q: got %d (ok=%v), want %d", name, got, ok, want)
		}
	}
}

// pid<=0 を kill(2) に渡さないこと（0 はプロセスグループ全体を意味する）。
func TestProcessAliveGuardsNonPositivePID(t *testing.T) {
	// 🚨 {0, -1, -1234} だけだと、ガードが無くても kill(2) が成功して true になるため
	// 検出力がゼロ（しかも -1234 の結果はその機械にプロセスグループ 1234 が在るかに依存する）。
	// 確実に存在しないプロセスグループを混ぜる。
	for _, pid := range []int{0, -1, -1234, -99999} {
		if !processAlive(pid) {
			t.Errorf("pid=%d は「判定不能 = 消さない」に倒すべき", pid)
		}
	}
	if !processAlive(os.Getpid()) {
		t.Error("自分自身は生きている判定になるべき")
	}
}

// 🚨 所有者が自分でない作業領域は使わないこと。
//
// 実環境では他ユーザー所有のディレクトリを作れないため、uid の取得を seam にして検査する。
// seam が無いとこの検査は永久に無検査のまま残る（3 周の敵対的レビューで指摘され続けた）。
func TestForeignOwnerIsRejected(t *testing.T) {
	isolateTemp(t)
	root, err := testWS.ensureTempRoot()
	if err != nil {
		t.Fatal(err)
	}
	// 掃除の条件に合う残骸を置く（検査が効いていなければ消えるはず）。
	stale := filepath.Join(root, strconv.Itoa(deadPID(t))+"-abc")
	if err := os.MkdirAll(stale, 0o700); err != nil {
		t.Fatal(err)
	}

	foreign := os.Getuid() + 1 // 自分以外の所有として扱う

	if _, err := testWS.openVerifiedTempRootWith(foreign, nil); err == nil {
		t.Error("所有者が自分でない作業領域を受け入れてはいけない")
	}
	testWS.sweepStaleTempDirs(foreign, nil)
	if _, err := os.Stat(stale); err != nil {
		t.Errorf("所有者を確認できない領域の中身を削除した: %v", err)
	}
}

// 🚨 Lstat した実体と、実際に開いた実体が違うなら拒否すること。
//
// 「名前を 2 回解決する」構造には必ず窓がある。実時間のレースは再現できないので、
// 窓の内側（Lstat と OpenRoot の間）に seam を置いて決定論的に差し替える。
func TestSwapDuringVerificationIsDetected(t *testing.T) {
	parent := isolateTemp(t)
	if _, err := testWS.ensureTempRoot(); err != nil {
		t.Fatal(err)
	}
	// すり替え先（別 inode のディレクトリ）。中身は掃除の条件に合う形にしておく。
	victim := filepath.Join(parent, "victim")
	stale := filepath.Join(victim, strconv.Itoa(deadPID(t))+"-abc")
	if err := os.MkdirAll(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	important := filepath.Join(stale, "important.txt")
	if err := os.WriteFile(important, []byte("消えてはいけない"), 0o600); err != nil {
		t.Fatal(err)
	}

	swapped := false
	hook := func(name string) {
		if name != tempRootName || swapped {
			return
		}
		swapped = true
		// 検証の途中で、同じ名前に別のディレクトリを置く。
		if err := os.Rename(testWS.tempRoot(), filepath.Join(parent, "moved-away")); err != nil {
			t.Error(err)
			return
		}
		if err := os.Rename(victim, testWS.tempRoot()); err != nil {
			t.Error(err)
		}
	}
	_, err := testWS.openVerifiedTempRootWith(os.Getuid(), hook)
	if !swapped {
		t.Fatal("seam が呼ばれていない（窓の内側に無い）")
	}
	if err == nil {
		t.Fatal("検証中に差し替えられたのに受け入れた")
	}
	if !strings.Contains(err.Error(), "差し替え") {
		t.Errorf("差し替えだと分かるメッセージにすべき: %v", err)
	}
}

// 🚨 親ホップ（<app>）の差し替えも検出すること。
//
// 検証は 2 ホップあるのに、テストは子（extract）しか駆動していなかった。
// 同じコードを通るとはいえ、「検証済み」と言えるのは駆動した側だけ。
func TestSwapOfParentHopIsDetected(t *testing.T) {
	parent := isolateTemp(t)
	if _, err := testWS.ensureTempRoot(); err != nil {
		t.Fatal(err)
	}
	// 親（<app>）と同じ形の別ディレクトリを用意しておく。
	decoy := filepath.Join(filepath.Dir(parent), "decoy")
	if err := os.MkdirAll(filepath.Join(decoy, tempRootName), 0o700); err != nil {
		t.Fatal(err)
	}

	swapped := false
	hook := func(name string) {
		if name != testWS.app || swapped {
			return
		}
		swapped = true
		if err := os.Rename(parent, filepath.Join(filepath.Dir(parent), "moved-away")); err != nil {
			t.Error(err)
			return
		}
		if err := os.Rename(decoy, parent); err != nil {
			t.Error(err)
		}
	}

	_, err := testWS.openVerifiedTempRootWith(os.Getuid(), hook)
	if !swapped {
		t.Fatal("親ホップで seam が呼ばれていない")
	}
	if err == nil {
		t.Fatal("親ディレクトリが差し替えられたのに受け入れた")
	}
	if !strings.Contains(err.Error(), "差し替え") {
		t.Errorf("差し替えだと分かるメッセージにすべき: %v", err)
	}
	// エラーメッセージが実在するパスを指すこと（display をホップごとに進める）。
	if !strings.Contains(err.Error(), parent) {
		t.Errorf("エラーが実在しないパスを指している: %v（%s を含むべき）", err, parent)
	}
}

// 🚨 シグナル受信後の順序: 既定へ戻す → 後始末 → 終了。
//
// 逆順だと、後始末が長引いている間どのシグナルでも止められなくなる。
// 実シグナルでは「後始末が長い」状況を壁時計なしに作れないので、順序そのものを検査する。
func TestSignalHandlerResetsBeforeCleanup(t *testing.T) {
	var order []string
	exited := 0
	handleCleanupSignal(
		syscall.SIGINT,
		resetFunc(func() { order = append(order, "reset") }),
		cleanupFunc(func() { order = append(order, "cleanup") }),
		func(code int) { order = append(order, "exit"); exited = code },
	)
	if strings.Join(order, ",") != "reset,cleanup,exit" {
		t.Errorf("順序が違う: %v（reset,cleanup,exit であるべき）", order)
	}
	if exited != 128+int(syscall.SIGINT) {
		t.Errorf("終了コード: got %d, want %d", exited, 128+int(syscall.SIGINT))
	}
	// exit は 1 回だけ（os.Exit は戻らないが、戻る実装で二重に呼ばない）。
	if n := strings.Count(strings.Join(order, ","), "exit"); n != 1 {
		t.Errorf("exit の呼び出しが %d 回", n)
	}
}

// 🚨 シグナルハンドラへ渡す reset が、本当に signal.Reset を呼ぶこと。
//
// 型（resetFunc / cleanupFunc）は**引数の取り違え**をコンパイルエラーにするが、
// 「reset のスロットに no-op を渡す」形は型では止まらず、既存のテストも全部緑になる（実測）。
// 実挙動で検査するには「後始末が長引いている間に 2 発目のシグナル」を作る必要があり、
// それは壁時計依存かつ新しいグローバル seam を要求する（消したばかりのもの）。
//
// # このゲートの脅威モデル
//
// 止めるもの: **うっかり**（リファクタ・整理・マージ事故で signal.Reset が落ちる）。
// 止めないもの: 意図的な迂回（別名の関数に包む / 動的に組み立てる / 別 API で同じことをする）。
// 意味論の正しさ（Reset が実際に効くか）は OS の仕様であり、ここでは検査しない。
func TestSignalHandlerPassesRealReset(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "workspace.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		id, ok := call.Fun.(*ast.Ident)
		if !ok || id.Name != "handleCleanupSignal" || len(call.Args) < 2 {
			return true
		}
		found = true
		// 第 2 引数（reset）の中に signal.Reset の呼び出しがあること。
		hasReset := false
		ast.Inspect(call.Args[1], func(m ast.Node) bool {
			sel, ok := m.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Reset" {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "signal" {
				hasReset = true
			}
			return true
		})
		if !hasReset {
			t.Error("handleCleanupSignal の reset 引数が signal.Reset を呼んでいない" +
				"（後始末が長引いている間、2 発目のシグナルで止められなくなる）")
		}
		return true
	})
	if !found {
		t.Fatal("handleCleanupSignal の呼び出しが見つからない（走査が壊れている）")
	}
}

// 🚨 ②の後始末が終わってから os.Exit までの間に main 側が NewTempDir を進めても、
// 新しいコピーが作られない（= 誰にも消されない残骸が生まれない）こと。
//
// 実シグナルでは「後始末の直後・終了の直前」を壁時計なしに作れないので、
// handleCleanupSignal の exit に「main がまだ走っている」を演じさせて窓を決定論的に開く。
func TestSignalShutdownRefusesNewTempDirs(t *testing.T) {
	isolateTemp(t)
	before, _, err := testWS.NewTempDir()
	if err != nil {
		t.Fatal(err)
	}

	var lateDir string
	var lateErr error
	handleCleanupSignal(
		syscall.SIGINT,
		resetFunc(func() {}),
		cleanupFunc(testWS.shutdownCleanups),
		func(int) { lateDir, _, lateErr = testWS.NewTempDir() }, // 終了直前に main が進んだ
	)

	if _, err := os.Stat(before); !os.IsNotExist(err) {
		t.Errorf("後始末の前に作られたコピーが消えていない: %v", err)
	}
	if lateErr == nil {
		t.Errorf("終了処理中なのに一時ディレクトリを作った（誰にも消されない）: %s", lateDir)
	}
	entries, err := os.ReadDir(testWS.tempRoot())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("終了処理の後に残骸がある: %d 件", len(entries))
	}
}

// 🚨 作成（MkdirTemp）と登録は同じロックの下で行うこと。
//
// ロックの外で作ってから登録すると、その間に②が後始末を終えて終了へ進んだとき、
// 作ったディレクトリは登録簿に無いので消されない。窓の内側（作成の直後）に割り込み、
// そこでロックが保持されている = ②が割り込めないことを確かめる。
func TestTempDirCreationAndRegistrationAreAtomic(t *testing.T) {
	isolateTemp(t)
	reached := false
	dir, cleanup, err := testWS.newTempDirWith(func() {
		reached = true
		if testWS.mu.TryLock() {
			testWS.mu.Unlock()
			t.Error("作成と登録の間でロックが外れている（②がこの窓に割り込むと残骸が残る）")
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if !reached {
		t.Fatal("作成直後の窓に到達していない（検査が空振りしている）")
	}
	testWS.mu.Lock()
	_, registered := testWS.paths[dir]
	testWS.mu.Unlock()
	if !registered {
		t.Error("作ったディレクトリが後始末に登録されていない")
	}
}

// ②の配線: シグナルハンドラへ渡す後始末は「終了中」を立てる shutdownCleanups であること。
// RunAllCleanups を渡すと、上の窓（後始末の後に NewTempDir が進む）が開いたままになる。
//
// 脅威モデルは TestSignalHandlerPassesRealReset と同じ（うっかりの差し戻しを止める）。
func TestSignalHandlerPassesShutdownCleanups(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "workspace.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		id, ok := call.Fun.(*ast.Ident)
		if !ok || id.Name != "handleCleanupSignal" || len(call.Args) < 3 {
			return true
		}
		found = true
		conv, ok := call.Args[2].(*ast.CallExpr)
		if !ok || len(conv.Args) != 1 {
			t.Errorf("cleanup 引数が cleanupFunc(...) の形ではない")
			return true
		}
		// メソッド値（w.shutdownCleanups）で渡す。
		if arg, ok := conv.Args[0].(*ast.SelectorExpr); !ok || arg.Sel.Name != "shutdownCleanups" {
			t.Errorf("シグナルハンドラの後始末が shutdownCleanups ではない（終了中を立てない）")
		}
		return true
	})
	if !found {
		t.Fatal("handleCleanupSignal の呼び出しが見つからない（走査が壊れている）")
	}
}

// 🚨 削除に失敗したパスは登録簿に残り、同じプロセスの中で再試行されること。
//
// 削除の前に登録簿から消すと、1 回目（① の defer 経路など）で失敗したコピーは
// 以後の RunAllCleanups（エラー経路・main の defer・シグナル経路）から二度と消されない。
func TestFailedCleanupIsRetried(t *testing.T) {
	parent := isolateTemp(t)
	dir, _, err := testWS.NewTempDir()
	if err != nil {
		t.Fatal(err)
	}

	// 作業領域を一時的に検証不能にする（symlink に差し替える）→ 1 回目の削除は失敗する。
	root := testWS.tempRoot()
	moved := filepath.Join(parent, "moved")
	if err := os.Rename(root, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, root); err != nil {
		t.Skipf("シンボリックリンクを作れない: %v", err)
	}
	testWS.RunAllCleanups()
	if _, err := os.Stat(filepath.Join(moved, filepath.Base(dir))); err != nil {
		t.Fatalf("前提が崩れている: 検証不能な作業領域で削除が起きた: %v", err)
	}

	// 作業領域を元に戻してから再試行 → 今度は消えること。
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(moved, root); err != nil {
		t.Fatal(err)
	}
	testWS.RunAllCleanups()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("1 回目に失敗したコピーが再試行で消えていない: %v", err)
	}
	testWS.mu.Lock()
	n := len(testWS.paths)
	testWS.mu.Unlock()
	if n != 0 {
		t.Errorf("削除に成功したのに登録簿に残っている: %d 件", n)
	}
}

// removeVerified は削除に成功した名前だけを removed に入れること。
// 失敗した名前まで入れると、RunAllCleanups が登録簿から消して二度と再試行しない。
func TestRemoveVerifiedReportsOnlySuccesses(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root では権限拒否を作れない")
	}
	isolateTemp(t)
	root, err := testWS.ensureTempRoot()
	if err != nil {
		t.Fatal(err)
	}
	// "stuck" は中身を列挙できない（RemoveAll が失敗する）。"ok" は普通に消える。
	locked := filepath.Join(root, "stuck", "locked")
	if err := os.MkdirAll(filepath.Join(locked, "inner"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "ok"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

	removed, err := testWS.removeVerified("stuck", "ok")
	if err == nil {
		t.Fatal("前提: stuck の削除は失敗するはず")
	}
	if strings.Join(removed, ",") != "ok" {
		t.Errorf("removed は成功したものだけであるべき: %q", removed)
	}
	if _, err := os.Stat(filepath.Join(root, "ok")); !os.IsNotExist(err) {
		t.Errorf("1 件の失敗で残りの削除が止まった: %v", err)
	}
}

// 🚨 作業領域を用意できない失敗（シンボリックリンク・終了処理中など）は EnvError で返すこと。
// 作業領域は全プロファイル共通なので、プロファイルを変えても直らない。
func TestTempRootFailureIsEnvError(t *testing.T) {
	parent := isolateTemp(t)
	if err := os.RemoveAll(parent); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), parent); err != nil {
		t.Skipf("シンボリックリンクを作れない: %v", err)
	}
	_, _, err := testWS.NewTempDir()
	if !IsEnvError(err) || !strings.Contains(err.Error(), "作業領域") {
		t.Errorf("作業領域の異常: EnvError であるべき: %v", err)
	}

	isolateTemp(t)
	testWS.mu.Lock()
	testWS.closing = true
	testWS.mu.Unlock()
	_, _, err = testWS.NewTempDir()
	if !IsEnvError(err) || !errors.Is(err, errCleanupClosing) {
		t.Errorf("終了処理中: EnvError（元は errCleanupClosing）であるべき: %v", err)
	}
}

// 🚨 既存の作業領域（または親）が group / other に開かれていたら使わないこと。
// 作成時の 0700 は既存のディレクトリには効かないので、ここで拒否しないと他人が書ける場所へ
// 資格情報のコピーを作る（<pid>-* を symlink に差し替えられる）。
func TestPermissiveExistingRootIsRejected(t *testing.T) {
	for _, hop := range []string{"parent", "extract"} {
		t.Run(hop, func(t *testing.T) {
			parent := isolateTemp(t)
			if _, err := testWS.ensureTempRoot(); err != nil {
				t.Fatal(err)
			}
			target := parent
			if hop == "extract" {
				target = filepath.Join(parent, tempRootName)
			}
			if err := os.Chmod(target, 0o777); err != nil {
				t.Fatal(err)
			}
			if _, _, err := testWS.NewTempDir(); err == nil || !IsEnvError(err) {
				t.Errorf("0777 の %s を受け入れた（EnvError で止めるべき）: %v", hop, err)
			}
		})
	}
}
