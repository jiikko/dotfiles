package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// rec はテスト用の git log レコード 1 件分 (9 フィールド + 本文)。
func rec(sha, short, subject, an, ae, ad, ar, deco, message, body string) string {
	return "\x1e" + strings.Join([]string{sha, short, subject, an, ae, ad, ar, deco, message}, "\x1f") + "\x1f" + body
}

func TestParseLogPlain(t *testing.T) {
	out := rec(strings.Repeat("a", 40), "aaaaaaa", "Fix invoice calculation", "koji", "koji@example.com",
		"Thu Jul 16 19:12:47 2026 +0900", "2 hours ago", "HEAD -> master, origin/master", "Fix invoice calculation\n\nbody\n", "") +
		rec(strings.Repeat("b", 40), "bbbbbbb", "Update README", "koji", "koji@example.com",
			"Wed Jul 15 10:00:00 2026 +0900", "1 day ago", "", "Update README\n", "")
	commits, err := ParseLog(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 {
		t.Fatalf("len = %d; want 2", len(commits))
	}
	c := commits[0]
	if c.ShortSHA != "aaaaaaa" || c.Subject != "Fix invoice calculation" || c.Author != "koji" ||
		c.AuthorEmail != "koji@example.com" || c.Date != "Thu Jul 16 19:12:47 2026 +0900" ||
		c.RelDate != "2 hours ago" || c.Decoration != "HEAD -> master, origin/master" ||
		c.Message != "Fix invoice calculation\n\nbody" || c.Body != "" {
		t.Errorf("commits[0] = %+v", c)
	}
	if commits[1].Decoration != "" {
		t.Errorf("commits[1].Decoration = %q; want empty", commits[1].Decoration)
	}
}

func TestParseLogWithBody(t *testing.T) {
	// --stat / -p の本文は最後のフィールドセパレータ以降 (issue のテスト方針: 本文を壊さない)
	body := "\n file.go | 12 ++++--\n 1 file changed\n"
	out := rec(strings.Repeat("a", 40), "aaaaaaa", "subject", "koji", "k@x", "d", "now", "", "subject", body)
	commits, err := ParseLog(out)
	if err != nil {
		t.Fatal(err)
	}
	want := " file.go | 12 ++++--\n 1 file changed"
	if commits[0].Body != want {
		t.Errorf("Body = %q; want %q", commits[0].Body, want)
	}
}

func TestParseLogEmpty(t *testing.T) {
	commits, err := ParseLog("")
	if err != nil || commits != nil {
		t.Errorf("ParseLog(\"\") = %v, %v", commits, err)
	}
}

func TestParseLogMalformed(t *testing.T) {
	if _, err := ParseLog("\x1eonly-one-field"); err == nil {
		t.Errorf("フィールド不足の出力がエラーにならない")
	}
}

func TestBuildLogArgs(t *testing.T) {
	opts := &Options{MaxCount: 5, Stat: true, Revs: []string{"main"}, Paths: []string{"src/"}}
	args := BuildLogArgs(opts, false)
	joined := strings.Join(args, " ")
	for _, want := range []string{"--max-count=5", "--stat", "--color=never", "main", "-- src/"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args に %q がありません: %v", want, args)
		}
	}
	if strings.Contains(joined, "--patch") {
		t.Errorf("指定していない --patch が入っている: %v", args)
	}
}

// ---- integration: 一時 Git リポジトリで実際の git log を解析する ----

// newTempRepo はコミット付きの一時リポジトリを作り、そのディレクトリへ chdir する。
func newTempRepo(t *testing.T, subjects []string) string {
	t.Helper()
	dir := t.TempDir()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(prev) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=tester", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=tester", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	for i, subject := range subjects {
		file := filepath.Join(dir, "file.txt")
		if err := os.WriteFile(file, []byte(strings.Repeat("line\n", i+1)), 0o644); err != nil {
			t.Fatal(err)
		}
		git("add", ".")
		git("commit", "-q", "-m", subject)
	}
	return dir
}

func TestIntegrationLoadCommitsOrderAndCount(t *testing.T) {
	newTempRepo(t, []string{"first", "second", "third"})
	commits, err := LoadCommits(&Options{MaxCount: defaultMaxCount}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 3 {
		t.Fatalf("len = %d; want 3", len(commits))
	}
	// git log は新しい順
	if commits[0].Subject != "third" || commits[2].Subject != "first" {
		t.Errorf("順序が git log と一致しない: %v, %v", commits[0].Subject, commits[2].Subject)
	}
	one, err := LoadCommits(&Options{MaxCount: 1}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(one) != 1 || one[0].Subject != "third" {
		t.Errorf("-n1 相当で %d 件 (%v)", len(one), one)
	}
}

func TestIntegrationStatBodyIntact(t *testing.T) {
	newTempRepo(t, []string{"first", "second"})
	commits, err := LoadCommits(&Options{MaxCount: defaultMaxCount, Stat: true}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range commits {
		if !strings.Contains(c.Body, "file.txt") || !strings.Contains(c.Body, "file changed") {
			t.Errorf("--stat 本文が壊れている (%s): %q", c.Subject, c.Body)
		}
	}
}

func TestIntegrationPatchBodyIntact(t *testing.T) {
	newTempRepo(t, []string{"first"})
	commits, err := LoadCommits(&Options{MaxCount: defaultMaxCount, Patch: true}, false)
	if err != nil {
		t.Fatal(err)
	}
	body := commits[0].Body
	for _, want := range []string{"diff --git", "+line"} {
		if !strings.Contains(body, want) {
			t.Errorf("-p の patch 本文に %q がありません: %q", want, body)
		}
	}
}

func TestIntegrationStagedDiff(t *testing.T) {
	dir := newTempRepo(t, []string{"first"})
	if err := os.WriteFile(filepath.Join(dir, "staged.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	diff, err := LoadStagedDiff(&Options{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "staged.txt") {
		t.Errorf("staged diff に staged.txt がありません: %q", diff)
	}
	head, err := LoadHeadCommit()
	if err != nil {
		t.Fatal(err)
	}
	if head.Subject != "first" {
		t.Errorf("HEAD subject = %q", head.Subject)
	}
}

func TestIntegrationMediumFields(t *testing.T) {
	newTempRepo(t, []string{"multi line\n\nmessage body here"})
	commits, err := LoadCommits(&Options{MaxCount: 1}, false)
	if err != nil {
		t.Fatal(err)
	}
	c := commits[0]
	if c.AuthorEmail != "t@example.com" {
		t.Errorf("AuthorEmail = %q", c.AuthorEmail)
	}
	if c.Date == "" || c.Message == "" {
		t.Errorf("Date/Message が空: %+v", c)
	}
	if !strings.Contains(c.Message, "message body here") {
		t.Errorf("フルメッセージが取れていない: %q", c.Message)
	}
}

func TestIntegrationUnpushedSHAs(t *testing.T) {
	newTempRepo(t, []string{"pushed", "local-only"})
	commits, err := LoadCommits(&Options{MaxCount: defaultMaxCount}, false)
	if err != nil {
		t.Fatal(err)
	}
	pushedSHA, localSHA := commits[1].SHA, commits[0].SHA
	// remote ref を偽装: 1 つ目のコミットまでが origin/master に到達済みという状態
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("remote", "add", "origin", "git@github.com:o/r.git")
	git("update-ref", "refs/remotes/origin/master", pushedSHA)
	unpushed := UnpushedSHAs(nil, 0, nil)
	if !unpushed[localSHA] {
		t.Errorf("ローカルのみのコミットが未 push 判定されない")
	}
	if unpushed[pushedSHA] {
		t.Errorf("remote 到達済みのコミットが未 push 判定された")
	}
	// limit 付きでも表示範囲 (rev-list 先頭) の未 push は欠けない
	limited := UnpushedSHAs(nil, 1, nil)
	if !limited[localSHA] {
		t.Errorf("limit=1 で先頭の未 push コミットが欠けた")
	}
	// planStatuses が未 push を確定させ、取得対象から外すことも確認
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	statuses, toFetch, _, hasRepo, _ := planStatuses(&Options{}, []string{localSHA, pushedSHA})
	if !hasRepo {
		t.Fatalf("github URL の remote が解決されない")
	}
	if statuses[localSHA] != StateUnpushed {
		t.Errorf("statuses[local] = %v; want unpushed", statuses[localSHA])
	}
	if len(toFetch) != 1 || toFetch[0] != pushedSHA {
		t.Errorf("toFetch = %v; want [pushed のみ]", toFetch)
	}
}

// pathspec 指定時、未 push 集合は表示側 (BuildLogArgs の `-- <paths>`) と同じ path
// フィルタで絞られる。path を触らない新しい未 push が積まれていても、path 該当の
// 未 push が集合から欠けない (C12 の回帰: 欠けると ↑ が – と誤表示される)。
func TestIntegrationUnpushedSHAsPathspec(t *testing.T) {
	dir := t.TempDir()
	prev, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(prev) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=tester", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=tester", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q", "-b", "main")
	// base (後で push 済みにする) → a を触る未 push → b を触る未 push (最新)
	write("a.txt", "base\n")
	git("add", ".")
	git("commit", "-q", "-m", "base")
	baseSHA := headSHA(t)
	write("a.txt", "a change\n")
	git("add", ".")
	git("commit", "-q", "-m", "touch a")
	aSHA := headSHA(t)
	write("b.txt", "b change\n")
	git("add", ".")
	git("commit", "-q", "-m", "touch b")
	bSHA := headSHA(t)
	// base までを origin に push 済みと偽装 (a/b は未 push)
	git("remote", "add", "origin", "git@github.com:o/r.git")
	git("update-ref", "refs/remotes/origin/master", baseSHA)

	// path 無しなら a/b 両方が未 push
	all := UnpushedSHAs(nil, defaultMaxCount, nil)
	if !all[aSHA] || !all[bSHA] {
		t.Fatalf("path 無しで a/b が未 push 判定されない: %v", all)
	}
	// a.txt 指定なら a のみ (b.txt しか触らない bSHA は path 該当せず除外される)。
	// これが崩れると `glogx -- a.txt` で aSHA が toFetch へ回り – と誤表示される
	onlyA := UnpushedSHAs(nil, defaultMaxCount, []string{"a.txt"})
	if !onlyA[aSHA] {
		t.Errorf("a.txt 指定で aSHA が未 push 判定されない: %v", onlyA)
	}
	if onlyA[bSHA] {
		t.Errorf("a.txt 指定なのに b.txt のみ触る bSHA が未 push 集合に混入: %v", onlyA)
	}
}

// headSHA は現在の HEAD の完全 SHA を返すテストヘルパー。
func headSHA(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func TestIntegrationOutsideRepo(t *testing.T) {
	dir := t.TempDir()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(prev) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CEILING_DIRECTORIES", dir)
	_, err = LoadCommits(&Options{MaxCount: 1}, false)
	if err == nil {
		t.Fatal("リポジトリ外でエラーになっていない")
	}
	var gitErr *GitExitError
	if !errors.As(err, &gitErr) {
		t.Fatalf("GitExitError ではない: %T %v", err, err)
	}
	if gitErr.Code == 0 || gitErr.Stderr == "" {
		t.Errorf("git の終了コード/stderr が伝播していない: %+v", gitErr)
	}
}

// LoadCommitDiff は実 git に対する薄い皮なので、実リポジトリで flag の妥当性ごと検証する。
func TestLoadCommitDiffRealRepo(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	run := func(args ...string) {
		t.Helper()
		if _, err := runGit(args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	if err := os.WriteFile("a.txt", []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.txt")
	run("commit", "-q", "-m", "add a")
	sha, err := runGit("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	lines, err := LoadCommitDiff(strings.TrimSpace(sha), false)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "+hello") {
		t.Errorf("patch 本文が含まれない:\n%s", joined)
	}
	if !strings.Contains(joined, "a.txt") {
		t.Errorf("stat のファイル名が含まれない:\n%s", joined)
	}
	if _, err := LoadCommitDiff("no-such-sha", false); err == nil {
		t.Error("不正 SHA でエラーが返らない")
	}
}

// verbatim 方式の統合: 実リポジトリで LoadLogDisplay + VerbatimLines の照合が成立し、
// 本文行が git log 実出力と一致すること。
func TestLoadLogDisplayVerbatimRealRepo(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	run := func(args ...string) {
		t.Helper()
		if _, err := runGit(args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	for i, msg := range []string{"first commit", "second commit\n\nbody line"} {
		if err := os.WriteFile("a.txt", []byte(strings.Repeat("x", i+1)), 0o644); err != nil {
			t.Fatal(err)
		}
		run("add", "a.txt")
		run("commit", "-q", "-m", msg)
	}
	opts := &Options{MaxCount: 20}
	commits, err := LoadCommits(opts, false)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := LoadLogDisplay(opts, false)
	if err != nil {
		t.Fatal(err)
	}
	v := VerbatimLines(raw, commits)
	if v == nil {
		t.Fatalf("実リポジトリで照合に失敗:\n%s", strings.Join(raw, "\n"))
	}
	// 本文 (非ヘッダー) 行は git log の出力とバイト一致 = 見た目の機械的一致の根拠
	for i, l := range v {
		if !l.Header && l.Text != raw[i] {
			t.Errorf("行 %d が git log 出力と不一致: %q != %q", i, l.Text, raw[i])
		}
	}
}
