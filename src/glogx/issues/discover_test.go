package issues

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// mkIssueDir はテスト用に dir/名前 のファイルを作る。
func mkFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("# x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFindDirsFindsRootAndOneLevelDeep(t *testing.T) {
	root := t.TempDir()
	mkFiles(t, filepath.Join(root, "issues"), "001-feat-a.md")
	mkFiles(t, filepath.Join(root, "macOS", "issues"), "002-bug-b.md")
	got := FindDirs(root)
	want := []string{filepath.Join(root, "issues"), filepath.Join(root, "macOS", "issues")}
	if len(got) != len(want) {
		t.Fatalf("見つかった数が違う: %q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%d 番目: want %q got %q", i, want[i], got[i])
		}
	}
}

func TestFindDirsIgnoresDirWithoutMarkdown(t *testing.T) {
	// 名前が issues でも issue 管理でないディレクトリが実在する
	// (ubiregi-server/script/issues/19951 は権限 fixture の .yml 置き場)
	root := t.TempDir()
	mkFiles(t, filepath.Join(root, "script", "issues", "19951"), "permissions.yml")
	if got := FindDirs(root); len(got) != 0 {
		t.Fatalf(".md の無い issues ディレクトリを拾ってしまった: %q", got)
	}
}

func TestFindDirsFindsDirWhereAllIssuesAreDone(t *testing.T) {
	// 全 issue が done/ に片付いていても issue ディレクトリとして拾う
	root := t.TempDir()
	mkFiles(t, filepath.Join(root, "issues", "done"), "001-feat-a.md")
	if got := FindDirs(root); len(got) != 1 {
		t.Fatalf("done/ だけの issues を拾えていない: %q", got)
	}
}

// issue ディレクトリ直下に md が無く、epic/<name>/ だけに issue がある repo も viewer の
// 探索対象にする。FindDirs だけでなく Scan まで通すことで、探索と 2 段走査の片方だけを
// 実装した退行を見逃さない。
func TestFindDirsFindsEpicOnlyRepoAndScanReadsIt(t *testing.T) {
	root := t.TempDir()
	groupDir := filepath.Join(root, "issues", EpicDirName, "cloud")
	mkFiles(t, groupDir, "710-feat-drive.md")

	dirs := FindDirs(root)
	if len(dirs) != 1 || dirs[0] != filepath.Join(root, "issues") {
		t.Fatalf("epic だけの issues を見つけられていない: %q", dirs)
	}
	found, _ := Scan(dirs)
	if len(found) != 1 || found[0].Rel != filepath.Join(EpicDirName, "cloud", "710-feat-drive.md") {
		t.Fatalf("FindDirs -> Scan で epic issue を拾えていない: %+v", found)
	}
}

func TestFindDirsSkipsGeneratedDirs(t *testing.T) {
	root := t.TempDir()
	mkFiles(t, filepath.Join(root, "node_modules", "issues"), "001-feat-a.md")
	mkFiles(t, filepath.Join(root, ".git", "issues"), "002-feat-b.md")
	mkFiles(t, filepath.Join(root, "tmp", "issues"), "003-feat-c.md")
	if got := FindDirs(root); len(got) != 0 {
		t.Fatalf("生成物・VCS 配下を拾ってしまった: %q", got)
	}
}

func TestFindDirsAcceptsSingularName(t *testing.T) {
	root := t.TempDir()
	mkFiles(t, filepath.Join(root, "issue"), "001-feat-a.md")
	if got := FindDirs(root); len(got) != 1 {
		t.Fatalf("issue (単数形) を拾えていない: %q", got)
	}
}

func TestRepoRootFallsBackToCwdOutsideGit(t *testing.T) {
	dir := t.TempDir()
	// t.TempDir は /var → /private/var の symlink 下に作られるため実体パスで比較する
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := RepoRoot(dir); got != dir && got != real {
		t.Fatalf("git 管理外で cwd を返していない: want %q got %q", dir, got)
	}
}

func TestRepoRootResolvesToplevelFromSubdir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git が無い")
	}
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init 失敗: %v %s", err, out)
	}
	sub := filepath.Join(root, "src", "deep")
	mkFiles(t, sub, "dummy.txt")
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := RepoRoot(sub); got != root && got != real {
		t.Fatalf("サブディレクトリから toplevel を解決できていない: want %q got %q", root, got)
	}
}
