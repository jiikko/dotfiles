package wtclean

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"pro-con/agents"
	"pro-con/card"
	"pro-con/gitx"
)

// 🚨 本物の git worktree remove / update-ref -d を呼ぶ。fixture は全部 sandbox の下に作り、allow は testing.Testing の間
// sandbox の外を実行前に拒否する (sandbox-real-destructive-test-apis)。
var sandbox string

func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "wtclean")
	if err != nil {
		panic(err)
	}
	sandbox = root
	SetTestSandbox(root)
	code := m.Run()
	_ = os.RemoveAll(root)
	os.Exit(code)
}

// git はテストの fixture を作る git (gitx と同じく GIT_DIR などを外す)。
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Env = gitx.Env()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

type fixture struct {
	t    *testing.T
	repo string
	in   Inputs
}

// newFixture は parent の下に repo を作り、base の commit を origin/master にする。
func newFixture(t *testing.T, parent string) *fixture {
	t.Helper()
	dir, err := os.MkdirTemp(parent, "repo")
	if err != nil {
		t.Fatal(err)
	}
	repo, _ := filepath.EvalSymlinks(dir)
	git(t, repo, "init", "-q", "-b", "master")
	write(t, filepath.Join(repo, "a.txt"), "a\n")
	write(t, filepath.Join(repo, ".gitignore"), "tmp/\n") // 開発機の core.excludesFile に頼らない
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "base")
	git(t, repo, "update-ref", "refs/remotes/origin/master", "HEAD")
	return &fixture{t: t, repo: repo, in: Inputs{Repos: map[string]string{"r": repo}, Cards: map[string]card.Card{}}}
}

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// add は pro-con の PG と同じ形の worktree (<repo>/.claude/worktrees/pc-<id>・ブランチ worktree-pc-<id>) と、完了したカードを作る。
func (f *fixture) add(id string) string {
	name := "pc-" + strings.ToLower(id)
	wt := filepath.Join(f.repo, ".claude", "worktrees", name)
	git(f.t, f.repo, "worktree", "add", "-q", "-b", BranchName(name), wt, "master")
	f.in.Cards[id] = card.Card{ID: id, Repo: "r", State: card.Done}
	return wt
}

func (f *fixture) commit(wt, file string) string {
	write(f.t, filepath.Join(wt, file), file+"\n")
	git(f.t, wt, "add", file)
	git(f.t, wt, "commit", "-qm", file)
	return git(f.t, wt, "rev-parse", "HEAD")
}

// land は commit を master と origin/master へ入れる。pick なら master を 1 本進めてから cherry-pick する (祖先にならない。
// 同じ親の上へ同じ秒に写すと同じ commit になり、祖先になってしまう)。
func (f *fixture) land(sha string, pick bool) {
	if pick {
		f.commit(f.repo, "unrelated-"+sha[:7]+".txt")
		git(f.t, f.repo, "cherry-pick", sha)
	} else {
		git(f.t, f.repo, "merge", "-q", "--ff-only", sha)
	}
	git(f.t, f.repo, "update-ref", "refs/remotes/origin/master", "HEAD")
}

func (f *fixture) scan() map[string]Verdict {
	f.t.Helper()
	vs, err := Scan(context.Background(), f.in)
	if err != nil {
		f.t.Fatal(err)
	}
	out := map[string]Verdict{}
	for _, v := range vs {
		out[v.Name] = v
	}
	return out
}

func (f *fixture) clean(vs map[string]Verdict, opt Options) map[string]Result {
	f.t.Helper()
	if opt.Fresh == nil {
		opt.Fresh = func(context.Context) (Inputs, error) { return f.in, nil }
	}
	var list []Verdict
	for _, v := range vs {
		list = append(list, v)
	}
	out := map[string]Result{}
	if err := Clean(context.Background(), list, opt, func(r Result) { out[r.Verdict.Name] = r }); err != nil {
		f.t.Fatal(err)
	}
	return out
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

func (f *fixture) hasBranch(b string) bool {
	_, rc, err := gitx.Run(context.Background(), f.repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+b)
	if err != nil {
		f.t.Fatal(err)
	}
	return rc == 0
}

func TestJudge(t *testing.T) {
	f := newFixture(t, sandbox)
	anc := f.add("C-001")
	f.land(f.commit(anc, "anc.txt"), false)
	picked := f.add("C-002")
	f.land(f.commit(picked, "picked.txt"), true)
	f.add("C-003")
	f.commit(filepath.Join(f.repo, ".claude/worktrees/pc-c-003"), "only-here.txt")
	dirty := f.add("C-004")
	write(t, filepath.Join(dirty, "a.txt"), "changed\n")
	untracked := f.add("C-005")
	write(t, filepath.Join(untracked, "new.txt"), "x\n")
	tmp := f.add("C-006")
	write(t, filepath.Join(tmp, "tmp", "report.md"), "x\n")
	emptyTmp := f.add("C-007")
	if err := os.MkdirAll(filepath.Join(emptyTmp, "tmp", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	busy := f.add("C-008")
	f.in.Sessions = []agents.Session{{Name: "pc-c-008", Cwd: filepath.Join(busy, "src")}}
	f.add("C-009")
	f.in.Cards["C-009"] = card.Card{ID: "C-009", Repo: "r", State: card.Review}
	f.add("C-010")
	delete(f.in.Cards, "C-010")
	f.add("C-011")
	f.in.Cards["C-011"] = card.Card{ID: "C-011", Repo: "r", State: card.Done, StopAfterClose: true}
	f.add("C-012")
	f.in.Cards["C-012"] = card.Card{ID: "C-012", Repo: "r", State: card.Done, DeleteAt: time.Now()}
	f.add("C-013")
	f.in.Cards["C-013"] = card.Card{ID: "C-013", Repo: "other", State: card.Done}
	// merge commit のほかは全部 base に入っていても、merge commit (解決で中身を足せる) は git cherry では比べられないので消さない
	merged := f.add("C-014")
	f.land(f.commit(merged, "side.txt"), true)
	git(t, merged, "checkout", "-q", "-b", "tmp-side", BranchName("pc-c-014")+"~1")
	other := f.commit(merged, "other.txt")
	git(t, merged, "checkout", "-q", BranchName("pc-c-014"))
	git(t, merged, "merge", "-q", "--no-edit", "tmp-side")
	git(t, f.repo, "branch", "-D", "tmp-side")
	f.land(other, true)
	renamed := f.add("C-015")
	git(t, renamed, "checkout", "-q", "-b", "pc-c-015-r2")
	humanLock := f.add("C-016")
	git(t, f.repo, "worktree", "lock", "--reason", "人が見ている", humanLock)
	claudeLocked := f.add("C-017")
	git(t, f.repo, "worktree", "lock", "--reason", "claude session pc-c-017 (pid 999999 start Fri Sep 25 14:15:38 2026)", claudeLocked)
	role := filepath.Join(f.repo, ".claude", "worktrees", "pc-pm-20260926-000000")
	git(t, f.repo, "worktree", "add", "-q", "-b", "pm", role, "master")
	if err := os.MkdirAll(filepath.Join(f.repo, ".claude", "worktrees", "pc-c-099"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := f.scan()
	want := map[string]struct {
		action Action
		why    string
	}{
		"pc-c-001":              {RemoveAll, "祖先"},
		"pc-c-002":              {RemoveAll, "git cherry の 1 本が全部 -"},
		"pc-c-003":              {Keep, "origin/master に無い commit が 1 本"},
		"pc-c-004":              {Keep, "未 commit の変更"},
		"pc-c-005":              {Keep, "未 commit の変更"},
		"pc-c-006":              {Keep, "tmp/report.md"},
		"pc-c-007":              {RemoveAll, "祖先"},
		"pc-c-008":              {Keep, "session が動いている"},
		"pc-c-009":              {Keep, "完了していない"},
		"pc-c-010":              {Keep, "記録に無い"},
		"pc-c-011":              {Keep, "止め終えていない"},
		"pc-c-012":              {Keep, "削除の途中"},
		"pc-c-013":              {Keep, "カードの repo (other)"},
		"pc-c-014":              {Keep, "merge commit"},
		"pc-c-015":              {RemoveTree, "祖先"},
		"pc-c-016":              {Keep, "人が見ている"},
		"pc-c-017":              {RemoveAll, "祖先"},
		"pc-pm-20260926-000000": {Keep, "PM・取り込みの係"},
		"pc-c-099":              {Keep, "登録されていない"},
	}
	if len(got) != len(want) {
		t.Errorf("判定の数 = %d, want %d: %v", len(got), len(want), got)
	}
	for name, w := range want {
		v, ok := got[name]
		if !ok {
			t.Errorf("%s の判定が無い", name)
			continue
		}
		if v.Action != w.action || !strings.Contains(v.Why, w.why) {
			t.Errorf("%s = %s / %q, want %s / %q を含む", name, v.Action, v.Why, w.action, w.why)
		}
	}
	if v := got["pc-c-015"]; !strings.Contains(v.BranchWhy, "pro-con が作った名前") {
		t.Errorf("pc-c-015 のブランチを残す理由 = %q", v.BranchWhy)
	}
}

func TestCleanRemovesOnlyRemovable(t *testing.T) {
	f := newFixture(t, sandbox)
	anc := f.add("C-001")
	f.land(f.commit(anc, "anc.txt"), false)
	locked := f.add("C-002")
	git(t, f.repo, "worktree", "lock", "--reason", "claude session pc-c-002 (pid 999999 start Fri Sep 25 14:15:38 2026)", locked)
	kept := f.add("C-003")
	f.commit(kept, "only-here.txt")
	renamed := f.add("C-004")
	git(t, renamed, "checkout", "-q", "-b", "pc-c-004-r2")

	res := f.clean(f.scan(), Options{})
	for _, name := range []string{"pc-c-001", "pc-c-002"} {
		if r := res[name]; r.Outcome != Removed {
			t.Errorf("%s = %s (%s), want %s", name, r.Outcome, r.Detail, Removed)
		}
		if exists(filepath.Join(f.repo, ".claude", "worktrees", name)) || f.hasBranch(BranchName(name)) {
			t.Errorf("%s の worktree かブランチが残っている", name)
		}
	}
	if r := res["pc-c-004"]; r.Outcome != TreeRemoved || exists(renamed) || !f.hasBranch("pc-c-004-r2") || !f.hasBranch(BranchName("pc-c-004")) {
		t.Errorf("pc-c-004 = %s (%s): worktree だけ消し、名前の違うブランチと元のブランチは残す", r.Outcome, r.Detail)
	}
	if _, ok := res["pc-c-003"]; ok || !exists(kept) || !f.hasBranch(BranchName("pc-c-003")) {
		t.Errorf("master に無い commit がある pc-c-003 に触った: %v", res["pc-c-003"])
	}
}

// 一覧を作った後に変わったもの (未 commit の変更・session・commit) は、消す直前の取り直しで拾って消さない。
func TestCleanRejudgesEachJustBefore(t *testing.T) {
	f := newFixture(t, sandbox)
	dirty := f.add("C-001")
	busy := f.add("C-002")
	moved := f.add("C-003")
	vs := f.scan()
	write(t, filepath.Join(dirty, "late.txt"), "x\n")
	f.commit(moved, "late-commit.txt")
	calls := 0
	res := f.clean(vs, Options{Fresh: func(context.Context) (Inputs, error) {
		calls++
		in := f.in
		in.Sessions = []agents.Session{{Name: "pc-c-002", Cwd: busy}}
		return in, nil
	}})
	if calls != 3 {
		t.Errorf("取り直した回数 = %d, want 3 (1 個ごと)", calls)
	}
	for name, why := range map[string]string{"pc-c-001": "未 commit", "pc-c-002": "session", "pc-c-003": "無い commit"} {
		if r := res[name]; r.Outcome != Skipped || !strings.Contains(r.Detail, why) {
			t.Errorf("%s = %s (%s), want %s (%s)", name, r.Outcome, r.Detail, Skipped, why)
		}
	}
	for _, wt := range []string{dirty, busy, moved} {
		if !exists(wt) {
			t.Errorf("%s を消した", wt)
		}
	}
}

// 取り直しが失敗したら、残りは消さずに止まる (0 本の session と読まない)。
func TestCleanStopsWhenFreshFails(t *testing.T) {
	f := newFixture(t, sandbox)
	wt := f.add("C-001")
	err := Clean(context.Background(), []Verdict{f.scan()["pc-c-001"]}, Options{Fresh: func(context.Context) (Inputs, error) {
		return Inputs{}, os.ErrDeadlineExceeded
	}}, func(Result) { t.Error("結果が出た") })
	if err == nil || !exists(wt) {
		t.Errorf("err = %v, 残っている = %v", err, exists(wt))
	}
}

// ブランチは判定し直したときの先端のままのときだけ消す (update-ref の古い値で git が断る)。
func TestDeleteBranchRefusesMovedTip(t *testing.T) {
	f := newFixture(t, sandbox)
	f.add("C-001")
	v := f.scan()["pc-c-001"]
	if err := removeWorktree(context.Background(), v); err != nil {
		t.Fatal(err)
	}
	f.commit(f.repo, "moved.txt")
	git(t, f.repo, "branch", "-f", BranchName("pc-c-001"), "HEAD")
	if err := deleteBranch(context.Background(), f.in, v, ""); err == nil || !f.hasBranch(BranchName("pc-c-001")) {
		t.Errorf("動いたブランチを消した: err = %v", err)
	}
}

// 外した Claude の lock は、消せなかったら掛け直す (lock を外しただけで終わらない)。
func TestRemoveWorktreeRelocksOnFailure(t *testing.T) {
	f := newFixture(t, sandbox)
	wt := f.add("C-001")
	reason := "claude session pc-c-001 (pid 999999 start Fri Sep 25 14:15:38 2026)"
	git(t, f.repo, "worktree", "lock", "--reason", reason, wt)
	v := f.scan()["pc-c-001"]
	write(t, filepath.Join(wt, "late.txt"), "x\n")
	if err := removeWorktree(context.Background(), v); err == nil {
		t.Fatal("未 commit のファイルがあるのに消した")
	}
	wts, err := listWorktrees(context.Background(), f.repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range wts {
		if w.Path == wt && (!w.Locked || w.LockReason != reason) {
			t.Errorf("lock が戻っていない: %+v", w)
		}
	}
}

// 消す操作の手前の拒否: sandbox の外・設定に無い repo・置き場の外・状態の置き場に重なるものは、git を呼ぶ前に断る。
func TestAllowRefusesBeforeDestroying(t *testing.T) {
	outsideRoot, err := os.MkdirTemp("", "wtclean-outside") // sandbox の外 (本物の repo の代わり)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(outsideRoot) }()
	out := newFixture(t, outsideRoot)
	owt := out.add("C-001")
	res := out.clean(out.scan(), Options{})
	if r := res["pc-c-001"]; r.Outcome != Failed || !strings.Contains(r.Detail, "拒否") || !exists(owt) || !out.hasBranch(BranchName("pc-c-001")) {
		t.Errorf("sandbox の外 = %s (%s), 残っている = %v", r.Outcome, r.Detail, exists(owt))
	}

	f := newFixture(t, sandbox)
	wt := f.add("C-001")
	v := f.scan()["pc-c-001"]
	cases := map[string]struct {
		in       Inputs
		v        Verdict
		stateDir string
	}{
		"設定に無い repo":   {Inputs{Repos: map[string]string{"x": sandbox}}, v, ""},
		"置き場の外":        {f.in, withPath(v, filepath.Join(f.repo, "sub", "pc-c-001")), ""},
		"役の worktree":  {f.in, withPath(v, filepath.Join(f.repo, ".claude", "worktrees", "pc-pm-1")), ""},
		"判定と違う名前":      {f.in, withPath(v, filepath.Join(f.repo, ".claude", "worktrees", "pc-c-002")), ""},
		"状態の置き場の中":     {f.in, v, filepath.Join(wt, "state")},
		"状態の置き場が repo": {f.in, v, f.repo},
	}
	for name, c := range cases {
		if err := allow(c.in, c.v, c.stateDir); err == nil {
			t.Errorf("%s: 拒否しなかった", name)
		}
	}
	if err := allow(f.in, v, filepath.Join(sandbox, "state")); err != nil {
		t.Errorf("消してよいものを拒否した: %v", err)
	}
}

func withPath(v Verdict, p string) Verdict { v.Path = p; return v }

// 継承した GIT_DIR が -C の先を上書きしない (hook から起動された shell で、別の repo の worktree を読み・消さない)。
func TestGitIgnoresInheritedGitDir(t *testing.T) {
	decoy := newFixture(t, sandbox)
	decoy.add("C-001")
	f := newFixture(t, sandbox)
	f.add("C-002")
	t.Setenv("GIT_DIR", filepath.Join(decoy.repo, ".git"))
	t.Setenv("GIT_WORK_TREE", decoy.repo)
	got := f.scan()
	if _, ok := got["pc-c-002"]; !ok || len(got) != 1 {
		t.Fatalf("継承した GIT_DIR の repo を読んだ: %v", got)
	}
	f.clean(got, Options{})
	if !exists(filepath.Join(decoy.repo, ".claude", "worktrees", "pc-c-001")) || !decoy.hasBranch(BranchName("pc-c-001")) {
		t.Error("継承した GIT_DIR の repo の worktree を消した")
	}
	if exists(filepath.Join(f.repo, ".claude", "worktrees", "pc-c-002")) {
		t.Error("-C の先の worktree を消していない")
	}
}

// lock の pid に claude が居れば残す。pid が使い回されて claude でないプロセスなら、Claude の lock は妨げにしない。
func TestLockHolderAlive(t *testing.T) {
	sleeper := func(argv0 string) *exec.Cmd {
		cmd := exec.Command("/bin/sleep", "30")
		cmd.Args[0] = argv0
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
		return cmd
	}
	reason := func(pid int) string {
		return "claude session pc-c-001 (pid " + strconv.Itoa(pid) + " start Fri Sep 25 14:15:38 2026)"
	}
	if c := sleeper("claude-bg-spare"); !lockHolderAlive(reason(c.Process.Pid)) {
		t.Error("claude のプロセスが居るのに居ないと読んだ")
	}
	if c := sleeper("zsh"); lockHolderAlive(reason(c.Process.Pid)) {
		t.Error("使い回された pid (claude でない) を claude と読んだ")
	}
	if lockHolderAlive(reason(999999)) {
		t.Error("居ない pid を居ると読んだ")
	}
	if !lockHolderAlive("人が見ている") {
		t.Error("読めない理由を居ない側に倒した")
	}
}
