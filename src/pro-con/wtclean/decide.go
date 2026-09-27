package wtclean

// 人が決める側 (issue 553)。自動の片付け (Clean) が残すもののうち、理由が「取り込む先に無い commit がある」か「記録に無いカード」だけの
// worktree (Verdict.Ask) を、人が設定画面のディスクのタブで「消す」(Discard) か「残す」(Hold) に決める。
// 🚨 どちらも Clean と同じく、材料を取り直して Judge で判定し直してから動かす (人が見た一覧の判定をそのまま信じない)。
// 判定し直して Ask でなくなった (session が動き出した・未 commit の変更ができた・自動で消せるようになった) か、先端が人が見たものから
// 動いていれば何もしない (人は見たものについて決めた)。

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pro-con/gitx"
)

// holdPrefix は人が「残す」と決めた worktree に掛ける git worktree lock の理由の頭。判定はこの lock を見て「人が残した」と出し、
// 自動の片付けも人の決め直しも触らない (やめるのは git worktree unlock)。
const holdPrefix = "pro-con: 人が残すと決めた"

// orphanWhy は記録に無いカードの worktree の理由の頭。
const orphanWhy = "記録に無いカードの worktree"

// RemovedRef は消した worktree の取り込んでいない commit を残す ref の置き場 (refs/pro-con/removed/<名前>/<sha>)。
func RemovedRef(name string) string { return "refs/pro-con/removed/" + name + "/" }

// Discard は人が「消す」と決めた worktree 1 個を消す。取り込む先に無い commit (先端と reflog) を RemovedRef に残してから、
// worktree を --force なしで外し、ブランチは pro-con が作った名前で先端が動いていないときだけ消す。
func Discard(ctx context.Context, seen Verdict, opt Options) Result {
	v, res, ok := rejudge(ctx, seen, opt)
	if !ok {
		return res
	}
	if err := keepReflog(ctx, v, v.Head); err != nil {
		return Result{Verdict: v, Outcome: Failed, Detail: "取り込んでいない commit を残せないので消さない: " + err.Error()}
	}
	dest, err := removeTree(ctx, v, opt.StateDir)
	if err != nil {
		return Result{Verdict: v, Outcome: Failed, Detail: err.Error()}
	}
	kept := "取り込んでいない commit は git for-each-ref " + RemovedRef(v.Name) + " で見られる" + stashedNote(len(v.Tmp), dest)
	if v.Branch != BranchName(v.Name) {
		return Result{Verdict: v, Outcome: TreeRemoved, Detail: fmt.Sprintf("ブランチは残した (pro-con が作った名前 %s ではない)。%s", BranchName(v.Name), kept)}
	}
	in, err := opt.Fresh(ctx)
	if err != nil {
		return Result{Verdict: v, Outcome: TreeRemoved, Detail: "ブランチは消せなかった (材料を取り直せない): " + err.Error() + "。" + kept}
	}
	if err := deleteBranch(ctx, in, v, opt.StateDir); err != nil {
		return Result{Verdict: v, Outcome: TreeRemoved, Detail: "ブランチは消せなかった: " + err.Error() + "。" + kept}
	}
	return Result{Verdict: v, Outcome: Removed, Detail: kept}
}

// Hold は人が「残す」と決めた worktree に git worktree lock (理由は holdPrefix と時刻) を掛ける。Claude Code が残した lock
// (持ち主の claude はもう居ない) があれば、掛け替える。
func Hold(ctx context.Context, seen Verdict, opt Options, now time.Time) Result {
	v, res, ok := rejudge(ctx, seen, opt)
	if !ok {
		return res
	}
	if v.Lock != "" {
		if err := run0(ctx, v.RepoPath, "worktree", "unlock", v.Path); err != nil {
			return Result{Verdict: v, Outcome: Failed, Detail: "claude の lock を外せない: " + err.Error()}
		}
	}
	reason := holdPrefix + " (" + now.Local().Format("2006-01-02 15:04") + ")"
	if err := run0(ctx, v.RepoPath, "worktree", "lock", "--reason", reason, v.Path); err != nil {
		return Result{Verdict: v, Outcome: Failed, Detail: "lock を掛けられない: " + err.Error()}
	}
	v.Ask, v.Why = false, reason
	return Result{Verdict: v, Outcome: Held, Detail: reason + " (やめるなら git worktree unlock " + v.Path + ")"}
}

// rejudge は材料を取り直して判定し直し、人が決めてよいまま (Ask で、先端が人が見たもの) かを見る。ok でなければ res を返す。
func rejudge(ctx context.Context, seen Verdict, opt Options) (Verdict, Result, bool) {
	if opt.Fresh == nil {
		return seen, Result{Verdict: seen, Outcome: Failed, Detail: "wtclean: Fresh が無い (取り直さずに動かさない)"}, false
	}
	in, err := opt.Fresh(ctx)
	if err != nil {
		return seen, Result{Verdict: seen, Outcome: Failed, Detail: "判定の材料を取り直せない: " + err.Error()}, false
	}
	v, err := Judge(ctx, in, seen.Repo, seen.Path)
	switch {
	case err != nil:
		return seen, Result{Verdict: seen, Outcome: Failed, Detail: "判定し直せない: " + err.Error()}, false
	case !v.Ask:
		return v, Result{Verdict: v, Outcome: Skipped, Detail: "判定し直したら人が決めるものではなくなった: " + v.Why}, false
	case v.Head != seen.Head:
		return v, Result{Verdict: v, Outcome: Skipped, Detail: fmt.Sprintf("見た後に先端が動いた (%.7s → %.7s)。見直してから決める", seen.Head, v.Head)}, false
	}
	if err := allow(in, v, opt.StateDir); err != nil {
		return v, Result{Verdict: v, Outcome: Failed, Detail: "動かす前に拒否した: " + err.Error()}, false
	}
	return v, Result{}, true
}

// Unlanded は PG の worktree name (pc-<カード>) の先端とブランチ worktree-<name> のうち、取り込み先 (origin/master。無ければ origin/main) に
// 中身が無いものがあれば、その理由を返す (無ければ空)。判定は片付けと同じ inBase (近似を別に書かない。issue 553 の close の検査)。
// worktree もブランチも無ければ空 (PG が作業していない・片付け済み)。
func Unlanded(ctx context.Context, repo, name string) (string, error) {
	repo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		return "", err
	}
	wts, err := listWorktrees(ctx, repo)
	if err != nil {
		return "", err
	}
	var heads []string
	for _, w := range wts {
		if filepath.Base(w.Path) == name && filepath.Dir(w.Path) == worktreesDir(repo) && w.Head != "" {
			heads = append(heads, w.Head)
		}
	}
	out, rc, err := gitx.Run(ctx, repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+BranchName(name)+"^{commit}")
	if err != nil {
		return "", err
	}
	if rc == 0 {
		heads = append(heads, strings.TrimSpace(out))
	}
	if len(heads) == 0 {
		return "", nil
	}
	base, baseName, err := trunk(ctx, repo)
	if err != nil {
		return "", err
	}
	for _, h := range heads {
		merged, why, err := inBase(ctx, repo, base, baseName, h)
		if err != nil {
			return "", err
		}
		if !merged {
			return why, nil
		}
	}
	return "", nil
}

// NoWorktree は Settle が片付ける worktree が無かったときの Detail (PG が作業していない・片付け済み)。
const NoWorktree = "worktree が無い"

// Settle はカードを閉じた (取り込まない終わり方)・削除したその場で、PG の worktree を片付ける (issue 553)。材料を取り直して判定し、
// 自動で消せるもの (取り込み済み) は Clean と同じく、人が決めるもの (取り込み先に無い commit・記録に無いカード) は Discard と同じく
// 取り込み先に無い commit を RemovedRef に残してから消す。それ以外の理由で残すもの (未 commit の変更・session・lock 等) は消さずに
// Skipped と理由を返す (設定画面のディスクのタブに残る)。worktree が無ければ Skipped。
func Settle(ctx context.Context, repoName, wtPath string, opt Options) Result {
	seen := Verdict{Repo: repoName, Path: wtPath, Name: filepath.Base(wtPath)}
	if _, err := os.Lstat(wtPath); errors.Is(err, os.ErrNotExist) {
		return Result{Verdict: seen, Outcome: Skipped, Detail: NoWorktree}
	}
	if opt.Fresh == nil {
		return Result{Verdict: seen, Outcome: Failed, Detail: "wtclean: Fresh が無い (取り直さずに消さない)"}
	}
	in, err := opt.Fresh(ctx)
	if err != nil {
		return Result{Verdict: seen, Outcome: Failed, Detail: "判定の材料を取り直せない: " + err.Error()}
	}
	v, err := Judge(ctx, in, repoName, wtPath)
	switch {
	case err != nil:
		return Result{Verdict: seen, Outcome: Failed, Detail: "判定できない: " + err.Error()}
	case v.Removable():
		return cleanOne(ctx, in, v, opt)
	case v.Ask:
		return Discard(ctx, v, opt)
	}
	return Result{Verdict: v, Outcome: Skipped, Detail: v.Why}
}
