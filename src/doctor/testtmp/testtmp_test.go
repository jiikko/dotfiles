package testtmp

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// withRoot は $TMPDIR を使い捨てに差し替えて root を返す。
// 🚨 ケース間で状態を共有しない (前のケースの残骸が次に効くと A-B が同じ結果を返して誤診する)。
func withRoot(t *testing.T) string {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())
	return filepath.Join(os.TempDir(), rootName)
}

// 死んだ pid の dir は回収し、生きている pid / 自分の dir は残す。
// 🚨 これが機構の本体。正常系 (自分の dir が作れる) だけ試しても何も確かめたことにならない。
func TestSweepRemovesOnlyDeadPidDirs(t *testing.T) {
	root := withRoot(t)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	// 死んだ pid: 自分の子を 1 つ起こして待ってから、その pid を使う (存在しない pid を
	// でっち上げると「たまたま生きている」を踏む。実際に終了した pid なら確実に死んでいる)
	dead := spawnAndReap(t)
	dirs := map[string]bool{ // path → 消えてほしいか
		filepath.Join(root, "a."+itoa(dead)+".xxxx"):         true,
		filepath.Join(root, "b."+itoa(os.Getpid())+".yyyy"):  false, // 自分
		filepath.Join(root, "c."+itoa(os.Getppid())+".zzzz"): false, // 生きている親
		filepath.Join(root, "d.notanumber.wwww"):             false, // pid が読めない = 触らない
		filepath.Join(root, "noprefix"):                      false, // 形が違う = 触らない
	}
	for d := range dirs {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// 前提: 5 件そろっている (早期 return で素通りしていないことを固定する)
	if got := countEntries(t, root); got != len(dirs) {
		t.Fatalf("前提が崩れた: root に %d 件 (want %d)", got, len(dirs))
	}

	n := sweep(root)
	if n != 1 {
		t.Errorf("回収した件数 = %d (want 1)", n)
	}
	for d, wantGone := range dirs {
		_, err := os.Lstat(d)
		gone := os.IsNotExist(err)
		if gone != wantGone {
			t.Errorf("%s: 消えた=%v (want %v)", filepath.Base(d), gone, wantGone)
		}
	}
}

// symlink は辿らない。辿ると root の外を消せてしまう。
func TestSweepDoesNotFollowSymlinks(t *testing.T) {
	root := withRoot(t)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "precious")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(outside, "keep.txt")
	if err := os.WriteFile(victim, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	dead := spawnAndReap(t)
	link := filepath.Join(root, "a."+itoa(dead)+".link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	if n := sweep(root); n != 0 {
		t.Errorf("symlink を回収した (n=%d)。root の外を消しうる", n)
	}
	if _, err := os.Lstat(victim); err != nil {
		t.Fatalf("symlink の先が消えた: %v", err)
	}
}

// Setup は root の中にだけ dir を作り、cleanup で消す。
func TestSetupCreatesInsideRootAndCleansUp(t *testing.T) {
	root := withRoot(t)
	dir, _, cleanup, err := Setup("mypkg")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(dir, root+string(os.PathSeparator)) {
		t.Fatalf("root の外に作った: %s (root=%s)", dir, root)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("作られていない: %v", err)
	}
	cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("cleanup で消えていない: %v", err)
	}
}

// prefix にパス区切りを渡したら拒否する (root の外へ出る形を作らせない)。
func TestSetupRejectsBadPrefix(t *testing.T) {
	withRoot(t)
	for _, bad := range []string{"", "../escape", "a/b", "a.b"} {
		if _, _, _, err := Setup(bad); err == nil {
			t.Errorf("prefix %q を通した", bad)
		}
	}
}

// 「回収した件数」を Setup が返すこと (0 件を成功の証拠にしないため、呼び出し元が報告できる)。
func TestSetupReportsSweptCount(t *testing.T) {
	root := withRoot(t)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	dead := spawnAndReap(t)
	for _, n := range []string{"a", "b"} {
		if err := os.MkdirAll(filepath.Join(root, n+"."+itoa(dead)+".x"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	_, swept, cleanup, err := Setup("mypkg")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if swept != 2 {
		t.Errorf("swept = %d (want 2)", swept)
	}
}

// --- ヘルパー -------------------------------------------------------------------------------

// spawnAndReap は確実に死んでいる pid を返す (子を起こして wait する)。
func spawnAndReap(t *testing.T) int {
	t.Helper()
	// /usr/bin/true を起こして即 wait する。終了済みかつ reap 済みの pid は生きていない
	pid, err := syscall.ForkExec("/usr/bin/true", []string{"true"}, &syscall.ProcAttr{})
	if err != nil {
		t.Skipf("子プロセスを起こせない: %v", err)
	}
	var ws syscall.WaitStatus
	if _, err := syscall.Wait4(pid, &ws, 0, nil); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if alive(pid) {
		t.Skipf("pid %d が回収後も生きていると判定された (pid 再利用?)", pid)
	}
	return pid
}

func countEntries(t *testing.T, dir string) int {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return len(es)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
