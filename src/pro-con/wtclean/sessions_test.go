package wtclean

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"pro-con/agents"
	"pro-con/card"
	"pro-con/store"
)

const (
	sid1 = "11111111-1111-4111-8111-111111111111"
	sid2 = "22222222-2222-4222-8222-222222222222"
)

// sessionRig は完了したカード C-001 の worktree と、その PG の session 2 本 (起動の記録の今の分と退いた分) の transcript・claude の job を
// sandbox の下に作る。transcript の置き場には、pro-con の外の session の transcript も置く。
type sessionRig struct {
	*fixture
	wt, projects, jobs, place string
	removed                   []string // RemoveJob に渡った短い id
	forgot                    map[string][]string
}

func newSessionRig(t *testing.T) *sessionRig {
	t.Helper()
	f := newFixture(t, sandbox)
	r := &sessionRig{fixture: f, wt: f.add("C-001"), forgot: map[string][]string{}}
	home, err := os.MkdirTemp(sandbox, "home")
	if err != nil {
		t.Fatal(err)
	}
	home, _ = filepath.EvalSymlinks(home)
	r.projects, r.jobs = filepath.Join(home, "projects"), filepath.Join(home, "jobs")
	r.place = filepath.Join(r.projects, "-x-worktrees-pc-c-001")
	cwd, _ := json.Marshal(r.wt + "/src")
	write(t, filepath.Join(r.place, sid1+".jsonl"), `{"type":"user","cwd":`+string(cwd)+"}\n")
	write(t, filepath.Join(r.place, sid1, "subagents", "agent-a.jsonl"), "{}\n")
	write(t, filepath.Join(r.place, sid2+".jsonl"), `{"type":"user","cwd":`+string(cwd)+"}\n")
	write(t, filepath.Join(r.place, "33333333-3333-4333-8333-333333333333.jsonl"), "{}\n") // 起動の記録に無い session (人が開いた)
	r.job("aaaa0001", sid1, r.wt, "")
	f.in.Owned = []Owned{{ID: "aaaa0001", SessionID: sid1, CardID: "C-001"}, {ID: "aaaa0002", SessionID: sid2, CardID: "C-001"}}
	f.in.Projects, f.in.JobsDir = r.projects, r.jobs
	return r
}

func (r *sessionRig) job(id, sid, cwd, worktreePath string) {
	data, _ := json.Marshal(map[string]string{"sessionId": sid, "cwd": cwd, "worktreePath": worktreePath, "worktreeBranch": BranchName("pc-c-001")})
	write(r.t, filepath.Join(r.jobs, id, "state.json"), string(data))
}

func (r *sessionRig) removeWorktree() {
	r.t.Helper()
	res := r.clean(r.scan(), Options{})
	if res["pc-c-001"].Outcome != Removed {
		r.t.Fatalf("worktree を消せない: %+v", res["pc-c-001"])
	}
}

func (r *sessionRig) options() SessionOptions {
	return SessionOptions{
		Fresh: func(context.Context) (Inputs, error) { return r.in, nil },
		RemoveJob: func(_ context.Context, id string) error {
			r.removed = append(r.removed, id)
			return os.RemoveAll(filepath.Join(r.jobs, id))
		},
		Forget: func(id string, sids []string) error { r.forgot[id] = sids; return nil },
	}
}

func (r *sessionRig) cleanSessions(opt SessionOptions) map[string]Outcome {
	r.t.Helper()
	out := map[string]Outcome{}
	if err := CleanSessions(context.Background(), ScanSessions(context.Background(), r.in, nil), opt, func(v SessionVerdict, o Outcome, _ string) {
		out[v.CardID] = o
	}); err != nil {
		r.t.Fatal(err)
	}
	return out
}

// worktree が残っている間は消さず (この一覧で消すなら「worktree を消した後」)、消えた後は transcript・job・起動の記録の行を消す。
// 起動の記録に無い session の transcript は同じ置き場にあっても残す。
func TestSessionsRemovedOnlyAfterWorktree(t *testing.T) {
	r := newSessionRig(t)
	ctx := context.Background()
	if v := JudgeSessions(ctx, r.in, "C-001", nil); v.Action != KeepSessions || !strings.Contains(v.Why, "残っている") {
		t.Fatalf("worktree が残っているのに消す: %+v", v)
	}
	if v := JudgeSessions(ctx, r.in, "C-001", map[string]bool{r.wt: true}); v.Action != AfterWorktree {
		t.Fatalf("この一覧で消す worktree の session を消す側に並べない: %+v", v)
	}
	if got := r.cleanSessions(r.options()); len(got) != 0 || !exists(filepath.Join(r.place, sid1+".jsonl")) {
		t.Fatalf("worktree が残っているのに消しに行った: %v", got)
	}
	r.removeWorktree()
	r.job("aaaa0002", sid2, r.repo, r.wt) // claude --bg -w の job の形: cwd は起動した repo、worktree は worktreePath (実物で実測)
	v := JudgeSessions(ctx, r.in, "C-001", nil)
	if v.Action != RemoveSessions || len(v.Transcripts) != 3 || !slices.Equal(v.Jobs, []string{"aaaa0001", "aaaa0002"}) || v.Bytes == 0 {
		t.Fatalf("判定 = %+v", v)
	}
	if got := r.cleanSessions(r.options()); got["C-001"] != Removed {
		t.Fatalf("消さない: %v", got)
	}
	for _, p := range []string{sid1 + ".jsonl", sid1, sid2 + ".jsonl"} {
		if exists(filepath.Join(r.place, p)) {
			t.Errorf("%s が残った", p)
		}
	}
	if !exists(filepath.Join(r.place, "33333333-3333-4333-8333-333333333333.jsonl")) {
		t.Error("起動の記録に無い session の transcript を消した")
	}
	if !slices.Equal(r.removed, []string{"aaaa0001", "aaaa0002"}) || !slices.Equal(r.forgot["C-001"], []string{sid1, sid2}) {
		t.Errorf("claude rm = %v / forget = %v", r.removed, r.forgot)
	}
}

// 消さない: 動いている session・完了していない・記録に無いカード・役のカード・ブランチが残る・transcript が別の worktree のもの・
// claude の job が別の worktree を持つ。
func TestSessionsKeep(t *testing.T) {
	cases := map[string]func(r *sessionRig) (string, string){
		"session が動いている": func(r *sessionRig) (string, string) {
			r.in.Sessions = []agents.Session{{ID: "aaaa0002", Name: "pc-c-001"}}
			return "C-001", "動いている"
		},
		"完了していない": func(r *sessionRig) (string, string) {
			r.in.Cards["C-001"] = card.Card{ID: "C-001", Repo: "r", State: card.Review}
			return "C-001", "完了していない"
		},
		"記録に無い": func(r *sessionRig) (string, string) {
			delete(r.in.Cards, "C-001")
			return "C-001", "記録に無い"
		},
		"役のカード": func(r *sessionRig) (string, string) { // 役の session は一覧にも出さない
			r.in.Owned = []Owned{{ID: "aaaa0001", SessionID: sid1, CardID: "PM"}}
			return "PM", ""
		},
		"ブランチが残る": func(r *sessionRig) (string, string) {
			git(r.t, r.repo, "branch", BranchName("pc-c-001"), "master")
			return "C-001", "ブランチ"
		},
		"transcript が別の worktree": func(r *sessionRig) (string, string) {
			write(r.t, filepath.Join(r.place, sid2+".jsonl"), `{"cwd":"`+r.wt+`-other"}`+"\n") // 名前の頭が同じだけの別の dir
			return "C-001", "cwd にした記録が無い"
		},
		"job が別の worktree": func(r *sessionRig) (string, string) {
			r.job("aaaa0001", sid1, r.wt, filepath.Join(r.repo, ".claude", "worktrees", "other"))
			return "C-001", "別の worktree"
		},
		"人が同じ session を別の場所で続けた": func(r *sessionRig) (string, string) {
			f, _ := os.OpenFile(filepath.Join(r.place, sid2+".jsonl"), os.O_APPEND|os.O_WRONLY, 0)
			_, _ = f.WriteString(`{"type":"user","cwd":"` + r.repo + `"}` + "\n")
			_ = f.Close()
			return "C-001", "cwd にした記録が無い"
		},
		"-w の job の cwd が repo の外": func(r *sessionRig) (string, string) {
			r.job("aaaa0001", sid1, sandbox, r.wt)
			return "C-001", "repo の外"
		},
		"job が別の session": func(r *sessionRig) (string, string) {
			r.job("aaaa0001", sid2, r.wt, "")
			return "C-001", "別の session"
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			r := newSessionRig(t)
			r.removeWorktree()
			id, why := setup(r)
			vs := ScanSessions(context.Background(), r.in, nil)
			i := slices.IndexFunc(vs, func(v SessionVerdict) bool { return v.CardID == id })
			if why == "" && len(vs) != 0 || why != "" && (i < 0 || vs[i].Action != KeepSessions || !strings.Contains(vs[i].Why, why)) {
				t.Fatalf("判定 = %+v", vs)
			}
			r.cleanSessions(r.options())
			if !exists(filepath.Join(r.place, sid1+".jsonl")) || len(r.removed) > 0 || len(r.forgot) > 0 {
				t.Fatalf("消した: removed=%v forgot=%v", r.removed, r.forgot)
			}
		})
	}
}

// 記録から消したカード (片付けの印だけがある) も、worktree と session を片付けられる (カードを 1 週間で消しても片付けの口が残る)。
func TestPurgedCardIsCleanable(t *testing.T) {
	r := newSessionRig(t)
	delete(r.in.Cards, "C-001")
	if v := r.scan()["pc-c-001"]; v.Removable() {
		t.Fatalf("印も記録も無いカードの worktree を消す: %+v", v)
	}
	r.in.Owned = nil // 起動の記録の行も消えていて、印だけが session を知っている
	r.in.Purged = map[string]store.Purged{"C-001": {CardID: "C-001", Repo: "r", Worktree: r.wt,
		Sessions: []store.PurgedSession{{ID: "aaaa0001", SessionID: sid1}, {ID: "aaaa0002", SessionID: sid2}}}}
	r.removeWorktree()
	if got := r.cleanSessions(r.options()); got["C-001"] != Removed || exists(filepath.Join(r.place, sid1+".jsonl")) {
		t.Fatalf("印のカードの session を消さない: %v", got)
	}
	if !slices.Equal(r.forgot["C-001"], []string{sid1, sid2}) {
		t.Errorf("forget = %v", r.forgot)
	}
}

// claude rm が失敗したら transcript を消さず、起動の記録の行も消させない (もう一度打てば全部やり直せる)。
func TestSessionsStopWhenJobRemovalFails(t *testing.T) {
	r := newSessionRig(t)
	r.removeWorktree()
	opt := r.options()
	opt.RemoveJob = func(context.Context, string) error { return os.ErrPermission }
	if got := r.cleanSessions(opt); got["C-001"] != Failed || !exists(filepath.Join(r.place, sid1+".jsonl")) || len(r.forgot) > 0 {
		t.Fatalf("claude rm の失敗で止まらない: %v forgot=%v", got, r.forgot)
	}
}

// 消す操作の手前の拒否: sandbox の外・置き場の直下でない・判定と違う session・symlink・状態の置き場に重なるものは消す前に断る。
func TestAllowSessionsRefusesBeforeDestroying(t *testing.T) {
	r := newSessionRig(t)
	r.removeWorktree()
	v := JudgeSessions(context.Background(), r.in, "C-001", nil)
	if err := allowSessions(r.in, v, ""); err != nil {
		t.Fatalf("消してよいものを拒否した: %v", err)
	}
	outside, err := os.MkdirTemp("", "wtclean-outside")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(outside) }()
	outside, _ = filepath.EvalSymlinks(outside)
	write(t, filepath.Join(outside, "p", sid1+".jsonl"), "{}\n")
	link := filepath.Join(r.place, sid2)
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	with := func(p string) SessionVerdict { w := v; w.Transcripts = []string{p}; return w }
	cases := map[string]struct {
		in       Inputs
		v        SessionVerdict
		stateDir string
	}{
		"sandbox の外":     {Inputs{Projects: outside}, with(filepath.Join(outside, "p", sid1+".jsonl")), ""},
		"置き場の直下でない":      {r.in, with(filepath.Join(r.place, "sub", sid1+".jsonl")), ""},
		"置き場そのもの":        {r.in, with(filepath.Join(r.projects, sid1+".jsonl")), ""},
		"判定と違う session":  {r.in, with(filepath.Join(r.place, "33333333-3333-4333-8333-333333333333.jsonl")), ""},
		"symlink":        {r.in, with(link), ""},
		"状態の置き場に重なる":     {r.in, v, r.place},
		"置き場が相対パス":       {Inputs{Projects: "projects"}, v, ""},
		"claude rm の id": {r.in, func() SessionVerdict { w := v; w.Jobs = []string{"-rf"}; return w }(), ""},
	}
	for name, c := range cases {
		if err := allowSessions(c.in, c.v, c.stateDir); err == nil {
			t.Errorf("%s: 拒否しなかった", name)
		}
	}
	if !exists(filepath.Join(outside, "p", sid1+".jsonl")) {
		t.Fatal("sandbox の外を消した")
	}
}

// テストの二進は本物の claude rm を呼ばない (本物の ~/.claude/jobs を消す)。
func TestClaudeRemoverRefusesInTests(t *testing.T) {
	if err := ClaudeRemover("/bin/echo", t.TempDir())(context.Background(), "aaaa0001"); err == nil || !strings.Contains(err.Error(), "テストの二進") {
		t.Fatalf("テストの二進で claude rm を呼んだ: %v", err)
	}
}
