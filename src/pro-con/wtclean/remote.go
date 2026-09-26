package wtclean

// remote (origin) に残った PG のブランチ (worktree-pc-<カード>) の片付け (issue 533)。PG は push しない (521) が、521 より前の PG と
// 例外で push したブランチが残る。
//
// 判定はローカルの worktree と同じ材料 (cardOf・trunk・inBase) を使う (同じ判定を 2 つ作らない)。消してよいのは:
//   - 名前が worktree-pc-<カード> (か worktree-pc-<カード>-<後ろ>。PG が作り直した版 -r1・-rebased など) で、役 (pc-pm-* / pc-int-*) でない
//   - そのカードが同じ repo のカードで、完了して PG を止め終えたもの (削除の途中でない)
//   - remote の先端の中身が取り込む先 (origin/master) にある (inBase)
//
// 🚨 remote のブランチを消すのは外に出る破壊的な操作。1 本ずつ、git fetch --prune で取り直して判定し直し、その直後に
// 判定した先端を lease にして消す (`git push --force-with-lease=refs/heads/<b>:<sha> origin :refs/heads/<b>`)。
// 判定の後に remote で動いたブランチは git が断る。消した後は ls-remote で消えたかを確かめる。

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"pro-con/card"
	"pro-con/gitx"
)

// Remote は片付ける remote (取り込む先 trunk と同じく origin だけを見る)。
const Remote = "origin"

// remoteBranchPrefix は PG のブランチの名前の頭 (BranchName("pc-"))。
var remoteBranchPrefix = BranchName("pc-")

// RemoteVerdict は remote のブランチ 1 本の判定。
type RemoteVerdict struct {
	Repo     string `json:"repo"`
	RepoPath string `json:"repoPath"`
	Branch   string `json:"branch"` // remote での短い名前 (worktree-pc-c-004)
	Head     string `json:"head"`   // 判定した remote の先端 (消すときの lease)
	CardID   string `json:"cardId,omitempty"`
	Remove   bool   `json:"remove"`
	Why      string `json:"why"`
}

// RemoteResult は remote のブランチ 1 本を消しに行った結果。
type RemoteResult struct {
	Verdict RemoteVerdict
	Outcome Outcome
	Detail  string
}

// Fetch は repo の origin を取り直し、remote で消えたブランチの追跡も消す (git fetch --prune)。
// 🚨 動かすのは refs/remotes/origin/* だけ (ローカルのブランチ・作業ツリーは動かさない)。判定の読み取り (git.go) には置かない
func Fetch(ctx context.Context, repo string) error {
	if err := guardTestRemote(ctx, repo); err != nil {
		return err
	}
	return run0(ctx, repo, "fetch", "--prune", "--quiet", Remote)
}

// ScanRemote は設定の全 repo の origin を取り直して、PG のブランチを判定する (repo の名前・ブランチの名前の順)。
// repo 1 個が取り直せなくても、ほかの repo は判定する (取り直せなかった repo は判定せず、エラーにまとめる)。
func ScanRemote(ctx context.Context, in Inputs) ([]RemoteVerdict, error) {
	var out []RemoteVerdict
	var errs []string
	for _, n := range sortedRepos(in) {
		vs, err := scanRemoteRepo(ctx, in, n)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", n, err))
		}
		out = append(out, vs...)
	}
	if len(errs) > 0 {
		return out, fmt.Errorf("remote を読めない repo がある: %s", strings.Join(errs, " / "))
	}
	return out, nil
}

func scanRemoteRepo(ctx context.Context, in Inputs, repoName string) ([]RemoteVerdict, error) {
	repo, err := filepath.EvalSymlinks(in.Repos[repoName])
	if err != nil {
		return nil, err
	}
	if _, rc, err := gitx.Run(ctx, repo, "config", "--get", "remote."+Remote+".url"); err != nil || rc != 0 {
		return nil, err // origin の無い repo (手元だけの repo) は片付ける remote のブランチが無い
	}
	if err := Fetch(ctx, repo); err != nil {
		return nil, err
	}
	refs, err := remoteBranches(ctx, repo)
	if err != nil {
		return nil, err
	}
	out := make([]RemoteVerdict, 0, len(refs))
	for _, r := range refs {
		out = append(out, judgeRemote(ctx, in, repoName, repo, r.name, r.head))
	}
	return out, nil
}

type remoteRef struct{ name, head string }

// remoteBranches は fetch した origin の PG のブランチ (refs/remotes/origin/worktree-pc-*) を名前の順に返す。
func remoteBranches(ctx context.Context, repo string) ([]remoteRef, error) {
	prefix := "refs/remotes/" + Remote + "/"
	out, _, err := gitx.Run(ctx, repo, "for-each-ref", "--format=%(objectname) %(refname)", prefix+remoteBranchPrefix+"*")
	if err != nil {
		return nil, err
	}
	var refs []remoteRef
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		sha, ref, ok := strings.Cut(l, " ")
		if !ok {
			continue
		}
		refs = append(refs, remoteRef{name: strings.TrimPrefix(ref, prefix), head: sha})
	}
	return refs, nil // for-each-ref は refname の順に返す
}

// judgeRemote は remote のブランチ 1 本の判定の本体 (head は fetch した直後の remote の先端)。
func judgeRemote(ctx context.Context, in Inputs, repoName, repo, branch, head string) RemoteVerdict {
	v := RemoteVerdict{Repo: repoName, RepoPath: repo, Branch: branch, Head: head}
	keep := func(format string, a ...any) RemoteVerdict {
		v.Why = fmt.Sprintf(format, a...)
		return v
	}
	name, ok := strings.CutPrefix(branch, "worktree-")
	if !ok || !strings.HasPrefix(name, "pc-") {
		return keep("PG のブランチの名前 (%s<カード>) ではない", remoteBranchPrefix)
	}
	if isRole(name) {
		return keep("PM・取り込みの係のブランチ")
	}
	c, ok := cardOfBranch(in, name)
	if !ok {
		return keep("記録に無いカードのブランチ")
	}
	v.CardID = c.ID
	if p, ok := in.Repos[c.Repo]; !ok || !samePath(p, repo) {
		return keep("カードの repo (%s) のブランチではない", c.Repo)
	}
	switch {
	case c.State != card.Done:
		return keep("カードが完了していない (%s)", c.State.Label())
	case c.StopAfterClose:
		return keep("閉じたカードの PG をまだ止め終えていない")
	case c.Deleting():
		return keep("カードの削除の途中")
	}
	base, baseName, err := trunk(ctx, repo)
	if err != nil {
		return keep("取り込む先を読めない: %v", err)
	}
	merged, why, err := inBase(ctx, repo, base, baseName, head)
	if err != nil {
		return keep("%s との比較に失敗した: %v", baseName, err)
	}
	v.Why, v.Remove = why, merged
	return v
}

// cardOfBranch はブランチの名前 (worktree- を外した pc-c-025 / pc-c-025-r1) のカード。名前そのもののカードが無ければ、
// 後ろの -<語> を 1 つずつ外して探す (PG が作り直した版の -r1・-rebased は元のカードの作業。中身は inBase が見る)。
func cardOfBranch(in Inputs, name string) (card.Card, bool) {
	for n := name; strings.Count(n, "-") >= 2; n = n[:strings.LastIndex(n, "-")] {
		id := strings.ToUpper(strings.TrimPrefix(n, "pc-"))
		if c, ok := cardOf(in, id); ok && card.SessionName(c) == n {
			return c, true
		}
	}
	return card.Card{}, false
}

// JudgeRemote は remote のブランチ 1 本を、origin を取り直してから判定し直す (消す直前の取り直し)。remote に無ければ ok が false。
func JudgeRemote(ctx context.Context, in Inputs, repoName, branch string) (RemoteVerdict, bool, error) {
	repo, err := filepath.EvalSymlinks(in.Repos[repoName])
	if err != nil {
		return RemoteVerdict{}, false, err
	}
	if err := Fetch(ctx, repo); err != nil {
		return RemoteVerdict{}, false, err
	}
	out, rc, err := gitx.Run(ctx, repo, "rev-parse", "--verify", "--quiet", "refs/remotes/"+Remote+"/"+branch+"^{commit}")
	if err != nil {
		return RemoteVerdict{}, false, err
	}
	if rc != 0 {
		return RemoteVerdict{Repo: repoName, RepoPath: repo, Branch: branch}, false, nil
	}
	return judgeRemote(ctx, in, repoName, repo, branch, strings.TrimSpace(out)), true, nil
}

// CleanRemote は一覧 (ScanRemote) のうち消してよいものを 1 本ずつ、材料と origin を取り直して判定し直してから消す。
func CleanRemote(ctx context.Context, targets []RemoteVerdict, opt Options, each func(RemoteResult)) error {
	if opt.Fresh == nil {
		return errors.New("wtclean: Fresh が無い (取り直さずに消さない)")
	}
	for _, t := range targets {
		if !t.Remove {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		in, err := opt.Fresh(ctx)
		if err != nil {
			return fmt.Errorf("判定の材料を取り直せないので、残りは消さない: %w", err)
		}
		each(cleanRemoteOne(ctx, in, t, opt))
	}
	return nil
}

func cleanRemoteOne(ctx context.Context, in Inputs, t RemoteVerdict, opt Options) RemoteResult {
	v, ok, err := JudgeRemote(ctx, in, t.Repo, t.Branch)
	switch {
	case err != nil:
		return RemoteResult{Verdict: t, Outcome: Failed, Detail: "取り直して判定し直せない: " + err.Error()}
	case !ok:
		return RemoteResult{Verdict: t, Outcome: Skipped, Detail: "remote にもう無い"}
	case !v.Remove:
		return RemoteResult{Verdict: v, Outcome: Skipped, Detail: v.Why}
	}
	if err := allowRemote(ctx, in, v, opt.StateDir); err != nil {
		return RemoteResult{Verdict: v, Outcome: Failed, Detail: "消す前に拒否した: " + err.Error()}
	}
	if err := deleteRemote(ctx, v); err != nil {
		return RemoteResult{Verdict: v, Outcome: Failed, Detail: err.Error()}
	}
	return RemoteResult{Verdict: v, Outcome: Removed, Detail: v.Why}
}

// deleteRemote は remote のブランチを、判定した先端 (v.Head) のままのときだけ消し、remote から消えたかを確かめる。
func deleteRemote(ctx context.Context, v RemoteVerdict) error {
	ref := "refs/heads/" + v.Branch
	if v.Head == "" {
		return errors.New("判定した先端が無い (lease を掛けずに消さない)")
	}
	if err := keepRemoteHead(ctx, v); err != nil {
		return fmt.Errorf("取り込む先から辿れない先端を残せないので消さない: %w", err)
	}
	if err := run0(ctx, v.RepoPath, "push", "--quiet", "--force-with-lease="+ref+":"+v.Head, Remote, ":"+ref); err != nil {
		return fmt.Errorf("git push が断った (判定の後に remote で動いたかもしれない): %w", err)
	}
	urls, err := pushURLs(ctx, v.RepoPath) // 🚨 送り先 (pushurl) を見る。ls-remote origin は取得先を読む
	if err != nil {
		return fmt.Errorf("消えたかを確かめられない: %w", err)
	}
	for _, u := range urls {
		out, _, err := gitx.Run(ctx, v.RepoPath, "ls-remote", u, ref)
		if err != nil {
			return fmt.Errorf("消えたかを確かめられない: %w", err)
		}
		if strings.TrimSpace(out) != "" {
			return fmt.Errorf("git push の後も remote (%s) に %s が残っている", u, ref)
		}
	}
	return nil
}

// keepRemoteHead は消す先端が取り込む先の祖先でなければ (rebase / cherry-pick で入った)、refs/pro-con/removed/origin/<ブランチ>/<sha> に残す
// (ローカルの keepReflog と同じ置き場。消した後の fetch --prune で追跡も消え、commit の作者・日付・文を手元で辿れなくなるため)。
func keepRemoteHead(ctx context.Context, v RemoteVerdict) error {
	base, _, err := trunk(ctx, v.RepoPath)
	if err != nil {
		return err
	}
	anc, err := gitx.IsAncestor(ctx, v.RepoPath, v.Head, base)
	if err != nil || anc {
		return err
	}
	return run0(ctx, v.RepoPath, "update-ref", "refs/pro-con/removed/"+Remote+"/"+v.Branch+"/"+v.Head, v.Head)
}

// allowRemote は remote のブランチを消す手前の拒否。消してよいのは設定の repo の origin の PG のブランチ (役を除く) だけ。
// 🚨 テストの二進 (testing.Testing) では、origin の取得先・送り先が SetTestSandbox で登録した置き場の下の道でなければ拒否する
// (本物の remote に触らない。fixture の正しさに頼らない)。
func allowRemote(ctx context.Context, in Inputs, v RemoteVerdict, stateDir string) error {
	repo, err := filepath.EvalSymlinks(v.RepoPath)
	if err != nil {
		return err
	}
	configured := false
	for _, p := range in.Repos {
		if samePath(p, repo) {
			configured = true
		}
	}
	name := strings.TrimPrefix(v.Branch, "worktree-")
	switch {
	case !configured:
		return fmt.Errorf("%s は設定の repo ではない", repo)
	case !strings.HasPrefix(v.Branch, remoteBranchPrefix) || isRole(name):
		return fmt.Errorf("%s は PG のブランチ (%s<カード>) ではない", v.Branch, remoteBranchPrefix)
	}
	if stateDir != "" && within(repo, resolve(stateDir)) {
		return fmt.Errorf("%s は状態の置き場 (%s) に重なる", repo, stateDir)
	}
	return guardTestRemote(ctx, repo)
}

// guardTestRemote は、テストの二進 (testing.Testing) で origin の取得先・送り先が SetTestSandbox で登録した置き場の下の道でなければ拒否する
// (消す push だけでなく、読む fetch / ls-remote の前にも掛ける)。テストの二進でなければ何もしない。
func guardTestRemote(ctx context.Context, repo string) error {
	if !testing.Testing() {
		return nil
	}
	root := testSandboxRoot()
	get, err := urlsOf(ctx, repo, false)
	if err != nil {
		return err
	}
	push, err := urlsOf(ctx, repo, true)
	if err != nil {
		return err
	}
	for _, u := range append(get, push...) {
		if root == "" || !filepath.IsAbs(u) || !within(u, root) {
			return fmt.Errorf("テストの二進で、置き場 (%q) の外の remote (%s) に触ろうとした", root, u)
		}
	}
	return nil
}

// pushURLs は origin の送り先 (pushurl・pushInsteadOf を解いた後) の全部。
func pushURLs(ctx context.Context, repo string) ([]string, error) { return urlsOf(ctx, repo, true) }

// urlsOf は origin の取得先 (push なら送り先) の全部 (insteadOf を解いた後。file:// は道にする)。
func urlsOf(ctx context.Context, repo string, push bool) ([]string, error) {
	args := []string{"remote", "get-url", "--all", Remote}
	if push {
		args = []string{"remote", "get-url", "--push", "--all", Remote}
	}
	out, rc, err := gitx.Run(ctx, repo, args...)
	if err == nil && rc != 0 {
		err = fmt.Errorf("git %s: rc=%d", strings.Join(args, " "), rc)
	}
	if err != nil {
		return nil, err
	}
	urls := strings.Fields(out)
	if len(urls) == 0 {
		return nil, errors.New("origin の URL が無い")
	}
	for i, u := range urls {
		if p, ok := strings.CutPrefix(u, "file://"); ok {
			urls[i] = p
		}
	}
	return urls, nil
}

// sortedRepos は設定の repo の名前を順に並べる (Scan と ScanRemote の順を揃える)。
func sortedRepos(in Inputs) []string {
	names := make([]string, 0, len(in.Repos))
	for n := range in.Repos {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
