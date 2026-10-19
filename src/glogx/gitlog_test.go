package main

import (
	"glogx/internal/gitdata"
	"glogx/internal/gitexec"
	"glogx/internal/testenv"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ---- integration: 一時 Git リポジトリで実際の git log を解析する ----

func TestIntegrationLoadCommitsOrderAndCount(t *testing.T) {
	testenv.NewTempRepo(t, []string{"first", "second", "third"})
	commits, err := gitdata.LoadCommits(gitdata.LogQuery{MaxCount: defaultMaxCount}, false)
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
	one, err := gitdata.LoadCommits(gitdata.LogQuery{MaxCount: 1}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(one) != 1 || one[0].Subject != "third" {
		t.Errorf("-n1 相当で %d 件 (%v)", len(one), one)
	}
}

func TestIntegrationStatBodyIntact(t *testing.T) {
	testenv.NewTempRepo(t, []string{"first", "second"})
	commits, err := gitdata.LoadCommits(gitdata.LogQuery{MaxCount: defaultMaxCount, Stat: true}, false)
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
	testenv.NewTempRepo(t, []string{"first"})
	commits, err := gitdata.LoadCommits(gitdata.LogQuery{MaxCount: defaultMaxCount, Patch: true}, false)
	if err != nil {
		t.Fatal(err)
	}
	body := commits[0].Body
	testenv.ContainsAll(t, "-p の patch 本文", body, "diff --git", "+line")
}

func TestIntegrationUnpushedSHAs(t *testing.T) {
	testenv.NewTempRepo(t, []string{"pushed", "local-only"})
	commits, err := gitdata.LoadCommits(gitdata.LogQuery{MaxCount: defaultMaxCount}, false)
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
	unpushed := gitdata.UnpushedSHAs(nil, 0, nil)
	if !unpushed[localSHA] {
		t.Errorf("ローカルのみのコミットが未 push 判定されない")
	}
	if unpushed[pushedSHA] {
		t.Errorf("remote 到達済みのコミットが未 push 判定された")
	}
	// limit 付きでも表示範囲 (rev-list 先頭) の未 push は欠けない
	limited := gitdata.UnpushedSHAs(nil, 1, nil)
	if !limited[localSHA] {
		t.Errorf("limit=1 で先頭の未 push コミットが欠けた")
	}
	// planStatuses が未 push を確定させ、取得対象から外すことも確認
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	statuses, toFetch, _, hasRepo, _ := planStatuses(&Options{}, []string{localSHA, pushedSHA})
	if !hasRepo {
		t.Fatalf("github URL の remote が解決されない")
	}
	if statuses[localSHA] != gitdata.StateUnpushed {
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
	t.Chdir(dir)
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
	baseSHA := testenv.HeadSHA(t)
	write("a.txt", "a change\n")
	git("add", ".")
	git("commit", "-q", "-m", "touch a")
	aSHA := testenv.HeadSHA(t)
	write("b.txt", "b change\n")
	git("add", ".")
	git("commit", "-q", "-m", "touch b")
	bSHA := testenv.HeadSHA(t)
	// base までを origin に push 済みと偽装 (a/b は未 push)
	git("remote", "add", "origin", "git@github.com:o/r.git")
	git("update-ref", "refs/remotes/origin/master", baseSHA)

	// path 無しなら a/b 両方が未 push
	all := gitdata.UnpushedSHAs(nil, defaultMaxCount, nil)
	if !all[aSHA] || !all[bSHA] {
		t.Fatalf("path 無しで a/b が未 push 判定されない: %v", all)
	}
	// a.txt 指定なら a のみ (b.txt しか触らない bSHA は path 該当せず除外される)。
	// これが崩れると `glogx -- a.txt` で aSHA が toFetch へ回り – と誤表示される
	onlyA := gitdata.UnpushedSHAs(nil, defaultMaxCount, []string{"a.txt"})
	if !onlyA[aSHA] {
		t.Errorf("a.txt 指定で aSHA が未 push 判定されない: %v", onlyA)
	}
	if onlyA[bSHA] {
		t.Errorf("a.txt 指定なのに b.txt のみ触る bSHA が未 push 集合に混入: %v", onlyA)
	}
}

// verbatim 方式の統合: 実リポジトリで LoadLogDisplay + VerbatimLines の照合が成立し、
// 本文行が git log 実出力と一致すること。
func TestLoadLogDisplayVerbatimRealRepo(t *testing.T) {
	// global / system の git 設定を読まない: log.decorate が設定された環境では --decorate を外す退行が見えなくなる
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := t.TempDir()
	t.Chdir(dir)
	run := func(args ...string) {
		t.Helper()
		if _, err := gitexec.RunGitTimeout(args...); err != nil {
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
	commits, err := gitdata.LoadCommits(opts.LogQuery(), false)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := gitdata.LoadLogDisplay(opts.LogQuery(), false)
	if err != nil {
		t.Fatal(err)
	}
	v := VerbatimLines(raw, commits)
	if v == nil {
		t.Fatalf("実リポジトリで照合に失敗:\n%s", strings.Join(raw, "\n"))
	}
	// ヘッダー行は端末の git log と同じくブランチ名を持つ (パイプでは log.decorate=auto が消す)
	if !strings.Contains(v[0].Text, "(HEAD -> ") {
		t.Errorf("先頭コミットのヘッダーにブランチ名が無い: %q", v[0].Text)
	}
	// 本文 (非ヘッダー) 行は git log の出力とバイト一致 = 見た目の機械的一致の根拠
	for i, l := range v {
		if !l.Header && l.Text != raw[i] {
			t.Errorf("行 %d が git log 出力と不一致: %q != %q", i, l.Text, raw[i])
		}
	}
}

// GitHub の repo を解決できない repo でも、未 push は手元で判定して 🔼 にする (⬜ = push 済みで Check が無い、と同じ絵にしない)。
func TestMergePlanMarksUnpushedWithoutRepo(t *testing.T) {
	statuses, toFetch, _, hasRepo, _ := mergePlan(repoPlan{hasRepo: false, unpushed: map[string]bool{"a": true}}, []string{"a", "b"})
	if hasRepo || len(toFetch) != 0 {
		t.Fatalf("hasRepo=%v toFetch=%v", hasRepo, toFetch)
	}
	if statuses["a"] != gitdata.StateUnpushed || statuses["b"] != gitdata.StateNone {
		t.Errorf("statuses = %v (a は未 push、b は Check なし)", statuses)
	}
}
