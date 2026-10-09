package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 開いた repo の .git/config の fsmonitor・textconv を、glogx の git (一覧・d の diff・staged の diff) で走らせない (issue 692)。
func TestGlogxGitDoesNotRunRepoConfigCommands(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git が無い")
	}
	dir := t.TempDir()
	marks := t.TempDir()
	fsmon, textconv := filepath.Join(marks, "fsmon"), filepath.Join(marks, "textconv")
	extdiff, gpg, hook := filepath.Join(marks, "extdiff"), filepath.Join(marks, "gpg"), filepath.Join(marks, "hook")
	git := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q", "-b", "main")
	write("f.txt", "a\n")
	write(".gitattributes", "*.txt diff=x\n")
	git("add", ".")
	git("-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "x")
	write("f.txt", "b\n")
	git("add", "f.txt")
	git("-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "y")
	// 署名の見出しを持つ commit を作る (log.showSignature=true と gpg.program で、署名の確かめに gpg.program が呼ばれる形)
	raw := git("cat-file", "commit", "HEAD")
	head, body, _ := strings.Cut(raw, "\n\n")
	signed := head + "\ngpgsig -----BEGIN PGP SIGNATURE-----\n \n -----END PGP SIGNATURE-----\n\n" + body + "\n"
	hash := exec.Command("git", "-C", dir, "hash-object", "-t", "commit", "-w", "--stdin")
	hash.Stdin = strings.NewReader(signed)
	out, err := hash.Output()
	if err != nil {
		t.Fatal(err)
	}
	git("update-ref", "refs/heads/main", strings.TrimSpace(string(out)))
	sha := git("rev-parse", "HEAD")
	write("f.txt", "c\n")
	git("add", "f.txt")
	git("config", "core.fsmonitor", "touch "+fsmon)
	git("config", "diff.x.textconv", "touch "+textconv+"; cat")
	// gpg.program と diff.external はシェルを通さずに直接起動されるので、罠は実行できるスクリプトにする
	// (`touch x;false` の文字列のままだと起動に失敗するだけで、修正を外しても何も起きない。変異で確かめた 2026-10-09)
	script := func(name, marker string) string {
		p := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\ntouch "+marker+"\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	git("config", "diff.external", script("extdiff.sh", extdiff))
	git("config", "log.showSignature", "true")
	git("config", "gpg.program", script("gpg.sh", gpg))
	hooks := t.TempDir()
	if err := os.WriteFile(filepath.Join(hooks, "post-index-change"), []byte("#!/bin/sh\ntouch "+hook+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	git("config", "core.hooksPath", hooks)
	// 追跡しているファイルの時刻だけを変える (status が index を書き直す条件。書き直すと post-index-change が走る)
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(dir, ".gitattributes"), future, future); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	t.Chdir(dir)

	if _, err := LoadCommitDiff(sha, false); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadStagedDiff(&Options{Patch: true}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLogDisplay(&Options{Patch: true, MaxCount: 5}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := loadWorktreeStatus(); err != nil {
		t.Fatal(err)
	}
	// 状態ビューのプレビュー (カーソル行の diff。開くと debounce の後に自動で取る。2 周目のレビューで再現した経路)
	write("f.txt", "d\n")
	for _, staged := range []bool{false, true} {
		if _, err := loadWorktreeDiff([]string{"f.txt"}, staged, false); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{fsmon, textconv, extdiff, gpg, hook} {
		if _, err := os.Stat(p); err == nil {
			t.Fatalf("repo の設定のコマンドが走った: %s", filepath.Base(p))
		}
	}
}
