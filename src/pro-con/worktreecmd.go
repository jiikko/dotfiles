package main

// pro-con worktree clean — 閉じたカードの PG の worktree を片付ける (issue 492)。既定は一覧を出すだけで、--yes で消してよいものを
// 1 個ずつ取り直して消す。worktree とブランチを消し終えたカードは、pro-con が起動した session (transcript・claude の job・起動の記録の行)
// も消す (issue 497。自動では消さない = ユーザーの決定)。判定と消し方は package wtclean。

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"pro-con/agents"
	"pro-con/card"
	"pro-con/diskuse"
	"pro-con/dispatcher"
	"pro-con/live"
	"pro-con/store"
	"pro-con/wtclean"
)

const worktreeUsage = `usage: pro-con worktree clean [--yes]
  閉じたカードの PG の worktree (<repo>/.claude/worktrees/pc-<カード>) と session を片付ける。既定は一覧を出すだけ。
  --yes で、消してよいものを 1 個ずつ取り直して消す (未 commit の変更・動いている session・master との比較を消す直前に見直す)。
  ブランチも消すのは、中身が master にあり (祖先か git cherry が全部 -)、名前が worktree-pc-<カード> のときだけ。
  worktree とブランチが消えたカードは、pro-con が起動した session の transcript・claude の job (claude rm)・起動の記録の行も消す。
  master に無い commit があるもの・記録に無いもの・session が動いているものは消さず、理由を並べる。`

// worktreeEnv は worktree clean が外から受け取るもの (テストが差し替える)。
type worktreeEnv struct {
	dir      string            // 状態の置き場 (記録・書庫・片付けの印・起動の記録を読む。書くのは受付の箱の forget だけ)
	repos    map[string]string // 設定の repo の名前 → パス
	sessions func(ctx context.Context) ([]agents.Session, error)
	procCwds func(ctx context.Context) ([]string, error) // 動いているプロセスの作業ディレクトリ (lsof)
	projects string                                      // transcript の置き場 (~/.claude/projects)
	jobsDir  string                                      // claude の job の置き場 (~/.claude/jobs。空なら job を見ない)
	// removeJob は claude の job を消す (本物は claude rm)。nil なら session を消さない (一覧には出す)
	removeJob func(ctx context.Context, id string) error
}

func runWorktree(args []string, env worktreeEnv, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "clean" {
		_, _ = fmt.Fprintln(stderr, worktreeUsage)
		return 2
	}
	fs := flag.NewFlagSet("pro-con worktree clean", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	yes := fs.Bool("yes", false, "")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, worktreeUsage)
		return 2
	}
	ctx := context.Background()
	fresh := func(ctx context.Context) (wtclean.Inputs, error) { return worktreeInputs(ctx, env, stderr) }
	in, err := fresh(ctx)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con worktree clean:", err)
		return 1
	}
	vs, scanErr := wtclean.Scan(ctx, in)
	printVerdicts(stdout, vs)
	if scanErr != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con worktree clean:", scanErr)
	}
	removing := map[string]bool{} // この一覧で worktree とブランチを消すもの (その session も消す側に並べる)
	for _, v := range vs {
		if v.Action == wtclean.RemoveAll {
			removing[v.Path] = true
		}
	}
	svs := wtclean.ScanSessions(ctx, in, removing)
	printSessionVerdicts(stdout, svs)
	if !*yes {
		if n, m := countRemovable(vs), countRemovableSessions(svs); n+m > 0 {
			_, _ = fmt.Fprintf(stdout, "\n消すには: pro-con worktree clean --yes (worktree %d 個・session %d 枚分を 1 つずつ取り直して消す)\n", n, m)
		}
		return exitOf(scanErr)
	}
	rc := exitOf(scanErr)
	_, _ = fmt.Fprintln(stdout)
	err = wtclean.Clean(ctx, vs, wtclean.Options{StateDir: env.dir, Fresh: fresh}, func(r wtclean.Result) {
		_, _ = fmt.Fprintf(stdout, "%s %s: %s\n", r.Outcome, r.Verdict.Name, r.Detail)
		if r.Outcome == wtclean.Failed {
			rc = 1
		}
	})
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con worktree clean:", err)
		return 1
	}
	if env.removeJob == nil {
		return rc
	}
	// worktree を消し終えてから材料を取り直して session を判定する (消せなかった worktree のカードの session は消さない)
	if in, err = fresh(ctx); err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con worktree clean:", err)
		return 1
	}
	opt := wtclean.SessionOptions{StateDir: env.dir, Fresh: fresh, RemoveJob: env.removeJob, Forget: func(id string, sids []string) error {
		_, err := store.Submit(env.dir, store.Request{Kind: store.KindForget, CardID: id, Sessions: sids, At: time.Now()})
		return err
	}}
	now := wtclean.ScanSessions(ctx, in, nil)
	for _, v := range now { // 一覧では消す側だったが、worktree を消せなかった等で取り直したら残すもの (黙って飛ばさない)
		if !v.Removable() && slices.ContainsFunc(svs, func(l wtclean.SessionVerdict) bool { return l.CardID == v.CardID && l.Removable() }) {
			_, _ = fmt.Fprintf(stdout, "%s %s の session: %s\n", wtclean.Skipped, v.CardID, v.Why)
		}
	}
	err = wtclean.CleanSessions(ctx, now, opt, func(v wtclean.SessionVerdict, o wtclean.Outcome, detail string) {
		_, _ = fmt.Fprintf(stdout, "%s %s の session: %s\n", o, v.CardID, detail)
		if o == wtclean.Failed {
			rc = 1
		}
	})
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con worktree clean:", err)
		return 1
	}
	return rc
}

// worktreeInputs は判定の材料を読む (記録と書庫・動いている session・cwd)。session を読めないときは失敗にする (0 本と読まない)。
func worktreeInputs(ctx context.Context, env worktreeEnv, stderr io.Writer) (wtclean.Inputs, error) {
	st, err := store.Load(env.dir)
	if err != nil {
		return wtclean.Inputs{}, fmt.Errorf("記録を読めない: %w", err)
	}
	arch, err := store.LoadArchive(env.dir)
	if err != nil { // 読めない行は飛ばされる (そのカードは記録に無い扱い = 消さない側)
		_, _ = fmt.Fprintln(stderr, "pro-con worktree clean:", err)
	}
	cards := map[string]card.Card{}
	for _, c := range arch {
		cards[c.ID] = c
	}
	for _, c := range st.Cards { // 記録にあれば記録を正とする (store.Find と同じ)
		cards[c.ID] = c
	}
	ss, err := env.sessions(ctx)
	if err != nil {
		return wtclean.Inputs{}, fmt.Errorf("動いている session を読めない (読めないまま消さない): %w", err)
	}
	procs, err := env.procCwds(ctx)
	if err != nil {
		return wtclean.Inputs{}, fmt.Errorf("プロセスの作業ディレクトリを読めない (読めないまま消さない): %w", err)
	}
	purged, err := store.LoadPurged(env.dir)
	if err != nil { // 読めない行は飛ばされる (そのカードは記録に無い扱い = 消さない側)
		_, _ = fmt.Fprintln(stderr, "pro-con worktree clean:", err)
	}
	var owned []wtclean.Owned
	regPath := filepath.Join(env.dir, live.RegistryFile)
	for _, load := range []func(string) ([]live.Owned, error){live.LoadRegistry, live.LoadRetired} {
		reg, err := load(regPath)
		if err != nil {
			return wtclean.Inputs{}, fmt.Errorf("起動の記録を読めない (読めないまま消さない): %w", err)
		}
		for _, o := range reg {
			owned = append(owned, wtclean.Owned{ID: o.ID, SessionID: o.SessionID, CardID: o.CardID})
		}
	}
	cwd, _ := os.Getwd()
	return wtclean.Inputs{Repos: env.repos, Cards: cards, Sessions: ss, Cwd: cwd, ProcCwds: procs,
		Purged: purged, Owned: owned, Projects: env.projects, JobsDir: env.jobsDir}, nil
}

// lsofCwds は自分が見えるプロセスの作業ディレクトリを lsof で読む (実測 2026-09-26: 850 本で 0.2 秒)。
// 出力の形は -F n の「n<パス>」の行。1 行も読めなければ失敗にする (0 本と読まない)。
func lsofCwds(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "lsof", "-w", "-a", "-d", "cwd", "-F", "n").Output()
	var cwds []string
	for _, l := range strings.Split(string(out), "\n") {
		if p, ok := strings.CutPrefix(l, "n"); ok && p != "" {
			cwds = append(cwds, p)
		}
	}
	if len(cwds) == 0 {
		return nil, fmt.Errorf("lsof が作業ディレクトリを 1 つも返さない: %w", err)
	}
	return cwds, nil
}

func printVerdicts(w io.Writer, vs []wtclean.Verdict) {
	var rm, keep []wtclean.Verdict
	for _, v := range vs {
		if v.Removable() {
			rm = append(rm, v)
		} else {
			keep = append(keep, v)
		}
	}
	_, _ = fmt.Fprintf(w, "消してよい (%d 個):\n", len(rm))
	for _, v := range rm {
		what := "worktree とブランチ " + v.Branch
		if v.Action == wtclean.RemoveTree {
			what = "worktree だけ (ブランチを残す: " + v.BranchWhy + ")"
		}
		_, _ = fmt.Fprintf(w, "  %s (%s / %s): %s — %s\n", v.Name, v.CardID, v.Repo, what, v.Why)
	}
	_, _ = fmt.Fprintf(w, "消さない (%d 個):\n", len(keep))
	for _, v := range keep {
		id := v.CardID
		if id == "" {
			id = "カード不明"
		}
		_, _ = fmt.Fprintf(w, "  %s (%s / %s): %s\n", v.Name, id, v.Repo, v.Why)
	}
}

// printSessionVerdicts は session の片付けの一覧 (消すものは transcript の数と大きさ・claude の job の数まで)。
func printSessionVerdicts(w io.Writer, vs []wtclean.SessionVerdict) {
	var rm, keep []wtclean.SessionVerdict
	for _, v := range vs {
		if v.Removable() {
			rm = append(rm, v)
		} else {
			keep = append(keep, v)
		}
	}
	_, _ = fmt.Fprintf(w, "\nsession を消してよいカード (%d 枚):\n", len(rm))
	for _, v := range rm {
		gone := ""
		if v.Purged {
			gone = "・記録から消したカード"
		}
		_, _ = fmt.Fprintf(w, "  %s (%s%s): session %d 本・transcript %d 個 (%s)・claude の job %d 本 — %s\n",
			v.CardID, v.Repo, gone, len(v.Sessions), len(v.Transcripts), diskuse.Human(v.Bytes), len(v.Jobs), v.Why)
		for _, p := range v.Transcripts { // transcript は消すと戻せないので、消す物を全部並べる
			_, _ = fmt.Fprintf(w, "      transcript %s\n", p)
		}
		for _, j := range v.Jobs {
			_, _ = fmt.Fprintf(w, "      claude rm %s\n", j)
		}
	}
	_, _ = fmt.Fprintf(w, "session を消さないカード (%d 枚):\n", len(keep))
	for _, v := range keep {
		_, _ = fmt.Fprintf(w, "  %s (%s): %s\n", v.CardID, orDashCLI(v.Repo), v.Why)
	}
}

func countRemovableSessions(vs []wtclean.SessionVerdict) int {
	n := 0
	for _, v := range vs {
		if v.Removable() {
			n++
		}
	}
	return n
}

func countRemovable(vs []wtclean.Verdict) int {
	n := 0
	for _, v := range vs {
		if v.Removable() {
			n++
		}
	}
	return n
}

func exitOf(err error) int {
	if err != nil {
		return 1
	}
	return 0
}

// realWorktreeEnv は本物の置き場・設定の repo・claude の実体で読む session。
func realWorktreeEnv(home string) (worktreeEnv, error) {
	repos, err := repoPaths(home)
	if err != nil {
		return worktreeEnv{}, err
	}
	cl, err := dispatcher.ResolveClaude(context.Background(), home) // 家の cwd で解決する (resolveClaude と同じ)
	if err != nil {
		return worktreeEnv{}, err
	}
	jobs := filepath.Join(home, ".claude", "jobs")
	return worktreeEnv{dir: liveDir(home), repos: repos, sessions: func(ctx context.Context) ([]agents.Session, error) {
		return agents.List(ctx, agents.ExecRunner(cl.Path))
	}, procCwds: lsofCwds, projects: filepath.Join(home, ".claude", "projects"), jobsDir: jobs, removeJob: wtclean.ClaudeRemover(cl.Path, jobs)}, nil
}
