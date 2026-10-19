package statusview

import (
	"glogx/internal/gitexec"
	"glogx/internal/testenv"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// 開いた repo の .git/config の filter (clean / process) を、状態ビューの status とプレビューの diff で走らせない。
// 対照として、filter を止めない runGitTimeout では同じ罠が走ることを見る (罠が作動しない fixture を緑にしない)。
func TestGlogxGitDoesNotRunRepoFilters(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		testenv.SkipEnv(t, "git が無い")
	}
	for _, kind := range []string{"clean", "process"} {
		t.Run(kind, func(t *testing.T) {
			dir, marks := t.TempDir(), filepath.Join(t.TempDir(), "marks")
			t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
			t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
			git := func(args ...string) {
				if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v\n%s", args, err, out)
				}
			}
			git("init", "-q", "-b", "main")
			if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			git("add", ".")
			git("-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "x")
			trap := filepath.Join(t.TempDir(), "trap.sh")
			if err := os.WriteFile(trap, []byte("#!/bin/sh\ntouch "+marks+"\ncat\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			git("config", "filter.x.clean", trap)
			if kind == "process" {
				git("config", "--unset", "filter.x.clean")
				git("config", "filter.x.process", trap)
			}
			if err := os.WriteFile(filepath.Join(dir, ".git", "info", "attributes"), []byte("* filter=x\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("b\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			// index と別の秒の過去に打つ: stat が食い違うので、status / diff は毎回 filter を通して内容を比べる
			old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
			if err := os.Chtimes(filepath.Join(dir, "f.txt"), old, old); err != nil {
				t.Fatal(err)
			}
			t.Chdir(dir)
			if _, err := gitexec.RunGitTimeout("status", "--porcelain"); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(marks); err != nil {
				t.Fatalf("対照: filter を止めない status で罠が走らない: %v", err)
			}
			if err := os.Remove(marks); err != nil {
				t.Fatal(err)
			}
			st, err := loadWorktreeStatus()
			if err != nil {
				t.Fatal(err)
			}
			if len(st.rows) == 0 {
				t.Fatal("status に変更が出ない")
			}
			if _, err := loadWorktreeDiff([]string{"f.txt"}, false, false); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(marks); err == nil {
				t.Fatal("repo の filter が走った")
			}
		})
	}
}

// submodule の中の設定 (.git/modules/<名前>/config) の filter を、状態ビューの status とプレビューの diff で走らせない。
// 子の git は submodule 自身の設定を読むので、親の設定の列挙と -c の上書きは届かない。--ignore-submodules=dirty で子を起こさない
func TestGlogxGitDoesNotRunSubmoduleFilters(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		testenv.SkipEnv(t, "git が無い")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	src, dir, marks := t.TempDir(), t.TempDir(), filepath.Join(t.TempDir(), "marks")
	git := func(in string, args ...string) {
		if out, err := exec.Command("git", append([]string{"-C", in}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(path, body string) {
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(in string) {
		git(in, "add", "-A")
		git(in, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "x")
	}
	for _, d := range []string{src, dir} {
		git(d, "init", "-q", "-b", "main")
		write(filepath.Join(d, "f.txt"), "a\n")
		commit(d)
	}
	git(dir, "-c", "protocol.file.allow=always", "submodule", "add", "-q", src, "sub")
	commit(dir)
	trap := filepath.Join(t.TempDir(), "trap.sh")
	write(trap, "#!/bin/sh\ntouch "+marks+"\ncat\n")
	git(filepath.Join(dir, "sub"), "config", "filter.evil.clean", trap) // .git/modules/sub/config に入る
	write(filepath.Join(dir, "sub", ".gitattributes"), "* filter=evil\n")
	write(filepath.Join(dir, "sub", "f.txt"), "b\n")
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(dir, "sub", "f.txt"), old, old); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if _, err := gitexec.RunGitTimeout("status", "--porcelain"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marks); err != nil {
		t.Fatalf("対照: submodule の中を見る status で罠が走らない: %v", err)
	}
	if err := os.Remove(marks); err != nil {
		t.Fatal(err)
	}
	if _, err := loadWorktreeStatus(); err != nil {
		t.Fatal(err)
	}
	if _, err := loadWorktreeDiff([]string{"sub"}, false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marks); err == nil {
		t.Fatal("submodule の filter が走った")
	}
}
