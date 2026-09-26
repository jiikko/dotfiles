package wtclean

// 消す側。🚨 「全部判定してから全部消す」にしない: 1 個ずつ、材料 (記録・session・git) を取り直して判定し直し、
// その直後に消して、消えたかを確かめる (検査と実行の距離を 1 個の処理時間に縮める。sandbox-real-destructive-test-apis)。
// 窓は 0 にできないので、git の側にも止め具を残す:
//   - worktree は `git worktree remove` を --force なしで呼ぶ (未 commit の変更・追跡していないファイルがあれば git が断る)。
//     Claude Code が残した lock は、判定し直して外してよいと見た直後に外し、消せなければ同じ理由で掛け直す
//   - ブランチは `git update-ref -d <ref> <確かめた先端>` で消す (取り直してから先端が動いていれば git が断る)
// 消すと worktree の HEAD とブランチの reflog も消える。reflog にしか無い commit (PG が rebase・amend・reset で外した前の版) は、
// 取り込む先から辿れなければ refs/pro-con/removed/<名前>/<sha> に残してから消す (2026-09-26 の実物では 50 個中 29 個にあった。
// 残さないと、取り込んでいない版を辿る手段がなくなって gc で消える)。見るのは `git for-each-ref refs/pro-con/removed`

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"pro-con/gitx"
)

// Outcome は 1 個を消しに行った結果。
type Outcome string

const (
	Removed     Outcome = "消した"
	TreeRemoved Outcome = "worktree だけ消した"
	Skipped     Outcome = "取り直したら消さない"
	Failed      Outcome = "失敗"
)

// Result は 1 個の結果。
type Result struct {
	Verdict Verdict // 取り直した判定 (取り直せなければ一覧の判定)
	Outcome Outcome
	Detail  string
}

// Options は消すときの決まり。
type Options struct {
	// StateDir は pro-con の状態の置き場。ここ (とその親) は決して消さない
	StateDir string
	// Fresh は判定の材料を取り直す (1 個ごとに呼ぶ)。失敗したら、残りは消さずに止まる
	Fresh func(ctx context.Context) (Inputs, error)
}

// Clean は一覧 (Scan) のうち消してよいものを 1 個ずつ消す。each は 1 個ごとに結果を受け取る (進み具合を出す)。
func Clean(ctx context.Context, targets []Verdict, opt Options, each func(Result)) error {
	if opt.Fresh == nil {
		return errors.New("wtclean: Fresh が無い (取り直さずに消さない)")
	}
	for _, t := range targets {
		if !t.Removable() {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		in, err := opt.Fresh(ctx)
		if err != nil {
			return fmt.Errorf("判定の材料を取り直せないので、残りは消さない: %w", err)
		}
		each(cleanOne(ctx, in, t, opt))
	}
	return nil
}

func cleanOne(ctx context.Context, in Inputs, t Verdict, opt Options) Result {
	v, err := Judge(ctx, in, t.Repo, t.Path)
	if err != nil {
		return Result{Verdict: t, Outcome: Failed, Detail: "判定し直せない: " + err.Error()}
	}
	if !v.Removable() {
		return Result{Verdict: v, Outcome: Skipped, Detail: v.Why}
	}
	if err := allow(in, v, opt.StateDir); err != nil {
		return Result{Verdict: v, Outcome: Failed, Detail: "消す前に拒否した: " + err.Error()}
	}
	if err := keepReflog(ctx, v); err != nil {
		return Result{Verdict: v, Outcome: Failed, Detail: "reflog にしか無い commit を残せないので消さない: " + err.Error()}
	}
	if err := removeWorktree(ctx, v); err != nil {
		return Result{Verdict: v, Outcome: Failed, Detail: err.Error()}
	}
	if v.Action != RemoveAll {
		return Result{Verdict: v, Outcome: TreeRemoved, Detail: v.BranchWhy}
	}
	if err := deleteBranch(ctx, in, v, opt.StateDir); err != nil {
		return Result{Verdict: v, Outcome: TreeRemoved, Detail: "ブランチは消せなかった: " + err.Error()}
	}
	return Result{Verdict: v, Outcome: Removed, Detail: v.Why}
}

// run0 は消す側の git。rc が 0 でなければ失敗 (gitx.Run は rc 1 を結果の意味として返すので、そのまま使うと失敗を見落とす)。
func run0(ctx context.Context, dir string, args ...string) (string, error) {
	out, rc, err := gitx.Run(ctx, dir, args...)
	if err == nil && rc != 0 {
		err = fmt.Errorf("git %s: rc=%d", strings.Join(args, " "), rc)
	}
	return out, err
}

// keepReflog は worktree の HEAD とブランチの reflog にある commit のうち、取り込む先から辿れないものを
// refs/pro-con/removed/<名前>/<sha> に残す (ref は増えるが、中身は取り込む先とほぼ同じ object なので重くない)。
func keepReflog(ctx context.Context, v Verdict) error {
	base, _, err := trunk(ctx, v.RepoPath)
	if err != nil {
		return err
	}
	shas, err := reflogCommits(ctx, v.Path, "HEAD")
	if err != nil {
		return err
	}
	if v.Branch != "" {
		more, err := reflogCommits(ctx, v.RepoPath, "refs/heads/"+v.Branch)
		if err != nil {
			return err
		}
		shas = append(shas, more...)
	}
	seen := map[string]bool{}
	for _, sha := range shas {
		if seen[sha] {
			continue
		}
		seen[sha] = true
		anc, err := gitx.IsAncestor(ctx, v.RepoPath, sha, base)
		if err != nil {
			return err
		}
		if anc {
			continue
		}
		if _, err := run0(ctx, v.RepoPath, "update-ref", "refs/pro-con/removed/"+v.Name+"/"+sha, sha, ""); err != nil {
			if _, rc, _ := gitx.Run(ctx, v.RepoPath, "rev-parse", "--verify", "--quiet", "refs/pro-con/removed/"+v.Name+"/"+sha); rc != 0 {
				return err // 既に残してある (前の実行が途中で止まった) なら続ける
			}
		}
	}
	return nil
}

// reflogCommits は ref の reflog にある commit (無い reflog は空)。
func reflogCommits(ctx context.Context, dir, ref string) ([]string, error) {
	out, rc, err := gitx.Run(ctx, dir, "reflog", "show", "--format=%H", ref, "--")
	if err != nil {
		return nil, err
	}
	if rc != 0 {
		return nil, nil
	}
	return strings.Fields(out), nil
}

// removeWorktree は --force なしで worktree を外し、置き場が消えたかを確かめる。
func removeWorktree(ctx context.Context, v Verdict) error {
	if v.Lock != "" {
		if _, err := run0(ctx, v.RepoPath, "worktree", "unlock", v.Path); err != nil {
			return fmt.Errorf("Claude Code の lock を外せない: %w", err)
		}
	}
	if _, err := run0(ctx, v.RepoPath, "worktree", "remove", v.Path); err != nil {
		if v.Lock != "" {
			if _, lerr := run0(ctx, v.RepoPath, "worktree", "lock", "--reason", v.Lock, v.Path); lerr != nil {
				return fmt.Errorf("git worktree remove が断った: %w (外した lock を掛け直せない: %v)", err, lerr)
			}
		}
		return fmt.Errorf("git worktree remove が断った: %w", err)
	}
	if _, err := os.Lstat(v.Path); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("git worktree remove の後も %s が残っている", v.Path)
	}
	return nil
}

// deleteBranch はブランチを、判定し直したときの先端のままのときだけ消し、消えたかを確かめる。
func deleteBranch(ctx context.Context, in Inputs, v Verdict, stateDir string) error {
	wts, err := listWorktrees(ctx, v.RepoPath)
	if err != nil {
		return err
	}
	if usedElsewhere(wts, worktree{Path: v.Path, Branch: v.Branch}) {
		return errors.New("ほかの worktree が同じブランチを使い始めた")
	}
	if err := allow(in, v, stateDir); err != nil {
		return fmt.Errorf("消す前に拒否した: %w", err)
	}
	ref := "refs/heads/" + v.Branch
	if _, err := run0(ctx, v.RepoPath, "update-ref", "-d", ref, v.Head); err != nil {
		return err
	}
	if _, rc, err := gitx.Run(ctx, v.RepoPath, "rev-parse", "--verify", "--quiet", ref); err != nil || rc == 0 {
		return fmt.Errorf("git update-ref -d の後も %s が残っている", ref)
	}
	return nil
}

// allow は消す操作の手前の拒否 (事後の確かめでは消えた後にしか気づけない)。消してよいのは
// 設定の repo の <repo>/.claude/worktrees/pc-<カード> (役の worktree を除く) だけで、状態の置き場とその親は決して消さない。
// 🚨 テストの二進 (testing.Testing) では、SetTestSandbox で登録した置き場の外を全部拒否する (fixture の正しさに頼らない)。
func allow(in Inputs, v Verdict, stateDir string) error {
	repo, err := filepath.EvalSymlinks(v.RepoPath)
	if err != nil {
		return err
	}
	wt, err := filepath.EvalSymlinks(v.Path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if errors.Is(err, os.ErrNotExist) { // ブランチを消すときは worktree はもう無い。親を解いて名前を足す
		parent, perr := filepath.EvalSymlinks(filepath.Dir(v.Path))
		if perr != nil {
			return perr
		}
		wt = filepath.Join(parent, filepath.Base(v.Path))
	}
	configured := false
	for _, p := range in.Repos {
		if samePath(p, repo) {
			configured = true
		}
	}
	name := filepath.Base(wt)
	switch {
	case !configured:
		return fmt.Errorf("%s は設定の repo ではない", repo)
	case filepath.Dir(wt) != worktreesDir(repo) || !strings.HasPrefix(name, "pc-") || isRole(name):
		return fmt.Errorf("%s は pro-con の PG の worktree (%s/pc-<カード>) ではない", wt, worktreesDir(repo))
	case name != v.Name:
		return fmt.Errorf("%s は判定した worktree (%s) ではない", wt, v.Name)
	}
	if stateDir != "" {
		sd := resolve(stateDir)
		if within(wt, sd) || within(sd, wt) || within(repo, sd) {
			return fmt.Errorf("%s は状態の置き場 (%s) に重なる", wt, sd)
		}
	}
	if testing.Testing() {
		root := testSandboxRoot()
		if root == "" || !within(repo, root) || !within(wt, root) {
			return fmt.Errorf("テストの二進で、置き場 (%q) の外 (%s) を消そうとした", root, wt)
		}
	}
	return nil
}

var (
	sandboxMu   sync.Mutex
	sandboxRoot string
)

// SetTestSandbox はテストの二進で消してよい置き場を登録する (TestMain から 1 回。空なら全部拒否する)。
func SetTestSandbox(dir string) {
	sandboxMu.Lock()
	defer sandboxMu.Unlock()
	sandboxRoot = resolve(dir)
}

func testSandboxRoot() string {
	sandboxMu.Lock()
	defer sandboxMu.Unlock()
	return sandboxRoot
}
