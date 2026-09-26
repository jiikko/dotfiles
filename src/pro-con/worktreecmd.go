package main

// pro-con worktree clean — 閉じたカードの PG の worktree を片付ける (issue 492)。既定は一覧を出すだけで、--yes で消してよいものを
// 1 個ずつ取り直して消す。判定と消し方は package wtclean。

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"pro-con/agents"
	"pro-con/card"
	"pro-con/dispatcher"
	"pro-con/store"
	"pro-con/wtclean"
)

const worktreeUsage = `usage: pro-con worktree clean [--yes]
  閉じたカードの PG の worktree (<repo>/.claude/worktrees/pc-<カード>) を片付ける。既定は一覧を出すだけ。
  --yes で、消してよいものを 1 個ずつ取り直して消す (未 commit の変更・動いている session・master との比較を消す直前に見直す)。
  ブランチも消すのは、中身が master にあり (祖先か git cherry が全部 -)、名前が worktree-pc-<カード> のときだけ。
  master に無い commit があるもの・記録に無いもの・session が動いているものは消さず、理由を並べる。`

// worktreeEnv は worktree clean が外から受け取るもの (テストが差し替える)。
type worktreeEnv struct {
	dir      string            // 状態の置き場 (記録と書庫を読む。書かない)
	repos    map[string]string // 設定の repo の名前 → パス
	sessions func(ctx context.Context) ([]agents.Session, error)
	procCwds func(ctx context.Context) ([]string, error) // 動いているプロセスの作業ディレクトリ (lsof)
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
	if !*yes {
		if n := countRemovable(vs); n > 0 {
			_, _ = fmt.Fprintf(stdout, "\n消すには: pro-con worktree clean --yes (%d 個を 1 個ずつ取り直して消す)\n", n)
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
	cwd, _ := os.Getwd()
	return wtclean.Inputs{Repos: env.repos, Cards: cards, Sessions: ss, Cwd: cwd, ProcCwds: procs}, nil
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
	return worktreeEnv{dir: liveDir(home), repos: repos, sessions: func(ctx context.Context) ([]agents.Session, error) {
		return agents.List(ctx, agents.ExecRunner(cl.Path))
	}, procCwds: lsofCwds}, nil
}
