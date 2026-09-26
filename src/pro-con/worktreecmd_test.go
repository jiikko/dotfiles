package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"pro-con/agents"
	"pro-con/card"
	"pro-con/gitx"
	"pro-con/live"
	"pro-con/store"
	"pro-con/wtclean"
)

// worktreeFixture は sandbox (TestMain が wtclean に登録した置き場) の下に、完了したカード C-001 の PG の worktree を持つ repo と
// 状態の置き場を作る。worktree の先端は origin/master の祖先 (消してよい)。
func worktreeFixture(t *testing.T) (env worktreeEnv, wt string) {
	t.Helper()
	root, err := os.MkdirTemp(worktreeSandbox, "cmd")
	if err != nil {
		t.Fatal(err)
	}
	root, _ = filepath.EvalSymlinks(root)
	repo, dir := filepath.Join(root, "repo"), filepath.Join(root, "state")
	for _, d := range []string{repo, dir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Env = gitx.Env()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "master")
	run("commit", "-q", "--allow-empty", "-m", "base")
	run("update-ref", "refs/remotes/origin/master", "HEAD")
	wt = filepath.Join(repo, ".claude", "worktrees", "pc-c-001")
	run("worktree", "add", "-q", "-b", wtclean.BranchName("pc-c-001"), wt, "master")
	data, err := json.Marshal(store.State{Cards: []card.Card{{ID: "C-001", Repo: "r", State: card.Done}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, store.StateFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return worktreeEnv{dir: dir, repos: map[string]string{"r": repo}, sessions: func(context.Context) ([]agents.Session, error) { return nil, nil },
		procCwds: func(context.Context) ([]string, error) { return []string{"/"}, nil }}, wt
}

func TestWorktreeCleanListsWithoutYes(t *testing.T) {
	env, wt := worktreeFixture(t)
	var out, errOut bytes.Buffer
	if rc := runWorktree([]string{"clean"}, env, &out, &errOut); rc != 0 {
		t.Fatalf("rc = %d: %s", rc, errOut.String())
	}
	if !strings.Contains(out.String(), "消してよい (1 個)") || !strings.Contains(out.String(), "pro-con worktree clean --yes") {
		t.Errorf("一覧 = %q", out.String())
	}
	if _, err := os.Stat(wt); err != nil {
		t.Errorf("--yes なしで消した: %v", err)
	}
}

func TestWorktreeCleanYesRemoves(t *testing.T) {
	env, wt := worktreeFixture(t)
	var out, errOut bytes.Buffer
	if rc := runWorktree([]string{"clean", "--yes"}, env, &out, &errOut); rc != 0 {
		t.Fatalf("rc = %d: %s / %s", rc, out.String(), errOut.String())
	}
	if _, err := os.Stat(wt); !errors.Is(err, os.ErrNotExist) || !strings.Contains(out.String(), "消した pc-c-001") {
		t.Errorf("消していない: %v / %q", err, out.String())
	}
}

// session を読めないときは 0 本と読まずに止まる (動いている PG の worktree を消さない)。
func TestWorktreeCleanStopsWhenSessionsUnreadable(t *testing.T) {
	env, wt := worktreeFixture(t)
	env.sessions = func(context.Context) ([]agents.Session, error) { return nil, agents.ErrEmptyOutput }
	var out, errOut bytes.Buffer
	if rc := runWorktree([]string{"clean", "--yes"}, env, &out, &errOut); rc != 1 || !strings.Contains(errOut.String(), "session を読めない") {
		t.Errorf("rc = %d, stderr = %q", rc, errOut.String())
	}
	if _, err := os.Stat(wt); err != nil {
		t.Errorf("消した: %v", err)
	}
}

// 1 週間で記録から消したカード (片付けの印だけがある) を --yes で片付ける: worktree とブランチを消した後、pro-con が起動した
// session の transcript と claude の job を消し、起動の記録の行と印を消す依頼 (forget) を受付の箱に置く。--yes なしでは全部並べるだけ。
func TestWorktreeCleanRemovesSessionsOfPurgedCard(t *testing.T) {
	env, wt := worktreeFixture(t)
	const sid = "11111111-1111-4111-8111-111111111111"
	if err := store.Update(env.dir, func(s *store.State) error { s.Cards, s.NextID = nil, 2; return nil }); err != nil {
		t.Fatal(err)
	}
	mark, _ := json.Marshal(store.Purged{CardID: "C-001", Repo: "r", Worktree: wt, Sessions: []store.PurgedSession{{ID: "aaaa0001", SessionID: sid}}})
	if err := os.WriteFile(filepath.Join(env.dir, store.PurgeFile), append(mark, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := live.Register(filepath.Join(env.dir, live.RegistryFile), live.Owned{ID: "aaaa0001", SessionID: sid, PID: 1, CardID: "C-001"}); err != nil {
		t.Fatal(err)
	}
	home := filepath.Dir(env.dir)
	env.projects, env.jobsDir = filepath.Join(home, "projects"), filepath.Join(home, "jobs")
	tr := filepath.Join(env.projects, "-x-pc-c-001", sid+".jsonl")
	cwd, _ := json.Marshal(wt)
	for p, s := range map[string]string{tr: `{"cwd":` + string(cwd) + "}\n", filepath.Join(env.jobsDir, "aaaa0001", "state.json"): `{"sessionId":"` + sid + `","cwd":` + string(cwd) + "}"} {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var removed []string
	env.removeJob = func(_ context.Context, id string) error {
		removed = append(removed, id)
		return os.RemoveAll(filepath.Join(env.jobsDir, id))
	}
	var out, errOut bytes.Buffer
	if rc := runWorktree([]string{"clean"}, env, &out, &errOut); rc != 0 || !strings.Contains(out.String(), "C-001 (r・記録から消したカード): session 1 本・transcript 1 個") {
		t.Fatalf("一覧 (rc=%d) = %s / %s", rc, out.String(), errOut.String())
	}
	if _, err := os.Stat(tr); err != nil || len(removed) > 0 {
		t.Fatalf("--yes なしで消した: %v %v", err, removed)
	}
	out.Reset()
	if rc := runWorktree([]string{"clean", "--yes"}, env, &out, &errOut); rc != 0 {
		t.Fatalf("rc = %d: %s / %s", rc, out.String(), errOut.String())
	}
	if _, err := os.Stat(wt); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("worktree を消していない: %s", out.String())
	}
	if _, err := os.Stat(tr); !errors.Is(err, os.ErrNotExist) || len(removed) != 1 {
		t.Fatalf("transcript / job を消していない: %v %v\n%s", err, removed, out.String())
	}
	reqs := store.PendingRequests(env.dir)
	if len(reqs) != 1 || reqs[0].Kind != store.KindForget || reqs[0].CardID != "C-001" || len(reqs[0].Sessions) != 1 || reqs[0].Sessions[0] != sid {
		t.Fatalf("forget の依頼 = %+v", reqs)
	}
}

func TestWorktreeUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"prune"}, {"clean", "--force"}, {"clean", "x"}} {
		var out, errOut bytes.Buffer
		if rc := runWorktree(args, worktreeEnv{}, &out, &errOut); rc != 2 || !strings.Contains(errOut.String(), "usage") {
			t.Errorf("%v: rc = %d", args, rc)
		}
	}
}
