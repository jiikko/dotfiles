package subproc

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// gitFilterRepo は、作業ツリーに stat の古い変更が 1 つある repo を作る (status が clean filter を通す状態)。
// global の設定は t の一時ファイルに向け、ユーザーの ~/.gitconfig を読まない。
func gitFilterRepo(t *testing.T) (repo, marks string) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(tmp, "global.cfg"))
	t.Setenv("GIT_CONFIG_PARAMETERS", "")
	repo, marks = filepath.Join(tmp, "r"), filepath.Join(tmp, "marks")
	if err := os.WriteFile(filepath.Join(tmp, "global.cfg"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	run("init", "-q")
	writeFile(t, filepath.Join(repo, "f"), "a\n")
	run("add", "f")
	run("-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "i")
	writeFile(t, filepath.Join(repo, "f"), "b\n")
	// mtime を index と別の秒の過去に打つ: stat が食い違うので、status は毎回 clean filter を通して内容を比べる
	// (同じ秒だと racy の扱いでたまにしか通らず、罠が走らないのを修正の効果と読み違える)
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(repo, "f"), old, old); err != nil {
		t.Fatal(err)
	}
	return repo, marks
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// trapScript は呼ばれたら marks に tag を書き、stdin をそのまま返す filter を作る (git はシェルを通して起動する)。
func trapScript(t *testing.T, marks, tag string) string {
	t.Helper()
	p := filepath.Join(filepath.Dir(marks), "trap-"+tag+".sh")
	writeFile(t, p, "#!/bin/sh\necho "+tag+" >> '"+marks+"'\ncat\n")
	return p
}

func gitConfig(t *testing.T, args ...string) {
	t.Helper()
	if args[0] == "-C" { // -C は config の前に置く
		args = append([]string{"-C", args[1], "config"}, args[2:]...)
	} else {
		args = append([]string{"config"}, args...)
	}
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		t.Fatalf("git config %v: %v\n%s", args, err, out)
	}
}

func readMarks(t *testing.T, marks string) string {
	t.Helper()
	b, err := os.ReadFile(marks)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

func runStatus(t *testing.T, repo string, guarded bool) string {
	t.Helper()
	var cmd *exec.Cmd
	if guarded {
		var err error
		if cmd, err = GitRepoCommand(context.Background(), repo, "status", "--porcelain=v1"); err != nil {
			t.Fatal(err)
		}
	} else {
		cmd = GitCommand(context.Background(), "-C", repo, "status", "--porcelain=v1")
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("status: %v\n%s", err, out)
	}
	return string(out)
}

// repo のローカルの設定の filter (clean / process / include したファイル) が、表示のための status で走らない (issue 732)。
// 対照として、素の GitCommand では同じ罠が走ることも見る (罠が作動していない fixture を緑にしない)。
func TestGitRepoArgsStopsLocalFilters(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, repo, marks string)
	}{
		{"clean", func(t *testing.T, repo, marks string) {
			gitConfig(t, "-C", repo, "filter.x.clean", trapScript(t, marks, "clean"))
		}},
		{"required の clean", func(t *testing.T, repo, marks string) {
			gitConfig(t, "-C", repo, "filter.x.clean", trapScript(t, marks, "clean"))
			gitConfig(t, "-C", repo, "filter.x.required", "true")
		}},
		{"process", func(t *testing.T, repo, marks string) {
			gitConfig(t, "-C", repo, "filter.x.process", trapScript(t, marks, "process"))
		}},
		{"include したファイル", func(t *testing.T, repo, marks string) {
			inc := filepath.Join(filepath.Dir(marks), "inc.cfg")
			gitConfig(t, "-f", inc, "filter.x.clean", trapScript(t, marks, "clean"))
			gitConfig(t, "-C", repo, "include.path", inc)
		}},
		{"空の名前", func(t *testing.T, repo, marks string) {
			gitConfig(t, "-f", filepath.Join(repo, ".git", "config"), "filter..clean", trapScript(t, marks, "clean"))
			writeFile(t, filepath.Join(repo, ".gitattributes"), "* filter=\n")
		}},
		{"名前に点を含む", func(t *testing.T, repo, marks string) {
			gitConfig(t, "-C", repo, "filter.a.b.clean", trapScript(t, marks, "clean"))
			writeFile(t, filepath.Join(repo, ".gitattributes"), "* filter=a.b\n")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, marks := gitFilterRepo(t)
			writeFile(t, filepath.Join(repo, ".gitattributes"), "* filter=x\n")
			tc.setup(t, repo, marks)
			runStatus(t, repo, false)
			if readMarks(t, marks) == "" {
				t.Fatal("対照: 素の GitCommand で罠が走らない (fixture が status に filter を通させていない)")
			}
			removeMarks(t, marks)
			if out := runStatus(t, repo, true); !strings.Contains(out, " M f") {
				t.Errorf("status に変更が出ない: %q", out)
			}
			if got := readMarks(t, marks); got != "" {
				t.Errorf("GitRepoCommand でも repo の filter が走った: %q", got)
			}
		})
	}
}

// global に置いた filter (git-lfs の形) は止めない。ローカルが同じ名前を上書きしても、global の値で走る。
func TestGitRepoArgsKeepsGlobalFilters(t *testing.T) {
	repo, marks := gitFilterRepo(t)
	writeFile(t, filepath.Join(repo, ".gitattributes"), "* filter=lfs\n")
	gitConfig(t, "--global", "filter.lfs.clean", trapScript(t, marks, "global"))
	gitConfig(t, "--global", "filter.lfs.required", "true")
	runStatus(t, repo, true)
	if got := readMarks(t, marks); !onlyTag(got, "global") {
		t.Fatalf("global の filter が走らない: %q", got)
	}
	removeMarks(t, marks)
	gitConfig(t, "-C", repo, "filter.lfs.clean", trapScript(t, marks, "local"))
	runStatus(t, repo, true)
	if got := readMarks(t, marks); !onlyTag(got, "global") {
		t.Fatalf("ローカルの上書きでなく global の値で走るはず: %q", got)
	}
}

// -c で上書きできない名前 (= を含む) は、status を起こさずに落とす。
func TestGitRepoArgsRejectsUnoverridableName(t *testing.T) {
	repo, marks := gitFilterRepo(t)
	gitConfig(t, "-C", repo, "filter.a=b.clean", trapScript(t, marks, "clean"))
	if _, err := GitRepoArgs(context.Background(), repo, "status"); err == nil {
		t.Fatal("= を含む名前で err が返らない")
	}
}

// filter が無い repo・repo でない場所でも動き、引数は GitArgs と同じ並び (-C dir の後に args) になる。
func TestGitRepoArgsWithoutFilters(t *testing.T) {
	repo, _ := gitFilterRepo(t)
	got, err := GitRepoArgs(context.Background(), repo, "status")
	if err != nil {
		t.Fatal(err)
	}
	want := GitArgs("-C", repo, "status")
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("got %q want %q", got, want)
	}
	if _, err := GitRepoArgs(context.Background(), t.TempDir(), "status"); err != nil {
		t.Fatalf("repo でない場所: %v", err)
	}
	if _, err := GitRepoArgs(context.Background(), filepath.Join(t.TempDir(), "none"), "status"); err == nil {
		t.Fatal("無い dir で err が返らない")
	}
}

func TestFilterOverrides(t *testing.T) {
	out := "global\x00filter.lfs.clean\nG\x00local\x00filter.lfs.clean\nL\x00local\x00filter.x.process\nP\x00global\x00filter.clean\nX\x00"
	got, err := filterOverrides([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"filter.lfs.clean=G",
		"filter.x.process=", "filter.x.required=false",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// onlyTag は marks の行が 1 行以上あり、全部 tag であるか (status は 1 回で filter を何度か呼ぶ)。
func onlyTag(marks, tag string) bool {
	if marks == "" {
		return false
	}
	for _, l := range strings.Split(marks, "\n") {
		if l != tag {
			return false
		}
	}
	return true
}

func removeMarks(t *testing.T, marks string) {
	t.Helper()
	if err := os.Remove(marks); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
