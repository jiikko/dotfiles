package wtclean

// session の片付け (issue 497)。worktree とブランチを消し終えた、完了したカードについて、pro-con が起動した session の
//   - transcript (~/.claude/projects/<置き場>/<session id>.jsonl と、サブエージェントの <session id>/)
//   - claude の job (~/.claude/jobs/<短い id>。claude agents --all の行。消す口は `claude rm <短い id>`)
//   - 起動の記録の行 (sessions.json / sessions-retired.json。書き手は dispatcher なので、受付の箱に forget を置いて頼む)
// を消す。自動では消さない (ユーザーの決定: 人が pro-con worktree clean --yes を打ったときだけ)。
//
// 消してよいのは、次を全部満たすカードの session だけ:
//   - カードが記録・書庫・片付けの印のどれかにあり、完了して PG を止め終えた (削除の途中でない)。役 (PM・INT) の session は扱わない
//   - 起動の記録か片付けの印にある session だけ (pro-con が起動したと示せるもの。外の session の transcript には触らない)
//   - どの session も動いていない (claude agents --json に出ていない)
//   - カードの worktree (<repo>/.claude/worktrees/pc-<カード>) が置き場にも git の worktree の一覧にも無く、ブランチ worktree-pc-<カード> も無い
//     (🚨 claude rm は worktree とブランチも自分の判断で消す = 2.1.283 で実測 2026-09-26。pro-con が消し終えた後にだけ呼ぶ)
//   - transcript は、その worktree を cwd にした記録を中に持つ (session id だけで他の置き場の同じ名前のファイルを消さない)
//   - claude の job は、その session id で、cwd がその worktree の下で、job が持つ worktree とブランチ (あれば) がカードのもの
//     (job が別の worktree を持っていると、claude rm がそれを消しうる)
//
// 消す側も worktree と同じく、1 枚ずつ材料を取り直して判定し直し、その直後に消して、消えたかを確かめる (clean.go)。

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"pro-con/card"
	"pro-con/diskuse"
	"pro-con/gitx"
	"pro-con/store"
)

// SessionAction はカード 1 枚の session をどうするか。
type SessionAction string

const (
	RemoveSessions SessionAction = "session を消す"
	AfterWorktree  SessionAction = "worktree を消した後に session を消す"
	KeepSessions   SessionAction = "session を消さない"
)

// SessionVerdict はカード 1 枚の session の判定。
type SessionVerdict struct {
	Repo        string        `json:"repo"`
	RepoPath    string        `json:"repoPath,omitempty"`
	CardID      string        `json:"cardId"`
	Worktree    string        `json:"worktree,omitempty"` // カードの worktree (消えていること)
	Sessions    []Owned       `json:"sessions"`           // 起動の記録と片付けの印にある session
	Transcripts []string      `json:"transcripts,omitempty"`
	Jobs        []string      `json:"jobs,omitempty"` // claude rm に渡す短い id (job が残っているもの)
	Bytes       int64         `json:"bytes"`          // transcript の大きさの和
	Purged      bool          `json:"purged"`         // 記録から消したカード (片付けの印で判定した)
	Action      SessionAction `json:"action"`
	Why         string        `json:"why"`
}

// Removable は消してよいか (この一覧の worktree を消した後も含む)。
func (v SessionVerdict) Removable() bool {
	return v.Action == RemoveSessions || v.Action == AfterWorktree
}

var (
	sessionIDRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	shortIDRe   = regexp.MustCompile(`^[0-9a-f]{8}$`)
)

// ScanSessions は起動の記録と片付けの印にある PG のカードの session を判定する (カード ID の順)。
// removing は、この一覧で消す worktree (RemoveAll) のパス。worktree がまだあっても、そこにあれば AfterWorktree と見る。
func ScanSessions(ctx context.Context, in Inputs, removing map[string]bool) []SessionVerdict {
	ids := map[string]bool{}
	for _, o := range in.Owned {
		if store.IsCardID(o.CardID) {
			ids[o.CardID] = true
		}
	}
	for id := range in.Purged {
		ids[id] = true
	}
	sorted := make([]string, 0, len(ids))
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Strings(sorted)
	out := make([]SessionVerdict, 0, len(sorted))
	for _, id := range sorted {
		out = append(out, JudgeSessions(ctx, in, id, removing))
	}
	return out
}

// JudgeSessions はカード 1 枚の session を今の状態で判定する (消す直前の取り直しにも使う。removing は nil)。
func JudgeSessions(ctx context.Context, in Inputs, id string, removing map[string]bool) SessionVerdict {
	v := SessionVerdict{CardID: id, Sessions: ownedOf(in, id), Action: KeepSessions}
	_, v.Purged = in.Purged[id]
	keep := func(format string, a ...any) SessionVerdict {
		v.Why = fmt.Sprintf(format, a...)
		return v
	}
	if !store.IsCardID(id) {
		return keep("PG のカードの session ではない")
	}
	c, ok := cardOf(in, id)
	if !ok {
		return keep("記録に無いカードの session (削除したカード。worktree と同じく残す)")
	}
	v.Repo = c.Repo
	switch {
	case c.State != card.Done:
		return keep("カードが完了していない (%s)", c.State.Label())
	case c.StopAfterClose:
		return keep("閉じたカードの PG をまだ止め終えていない")
	case c.Deleting():
		return keep("カードの削除の途中")
	}
	p, ok := in.Repos[c.Repo]
	if !ok {
		return keep("カードの repo (%s) が設定に無い", c.Repo)
	}
	repo, err := filepath.EvalSymlinks(p)
	if err != nil {
		return keep("repo を読めない: %v", err)
	}
	v.RepoPath = repo
	name := card.SessionName(c)
	v.Worktree = filepath.Join(worktreesDir(repo), name)
	for _, o := range v.Sessions {
		for _, s := range in.Sessions {
			if s.SessionID == o.SessionID || (o.ID != "" && s.ID == o.ID) {
				return keep("session が動いている (%s)", firstNonEmpty(s.Name, s.ID, s.SessionID))
			}
		}
	}
	if in.Cwd != "" && within(in.Cwd, v.Worktree) {
		return keep("このコマンドをその worktree の中で走らせている")
	}
	left, err := worktreeLeft(ctx, repo, v.Worktree, BranchName(name))
	if err != nil {
		return keep("worktree とブランチが残っているかを読めない: %v", err)
	}
	action := RemoveSessions
	if left != "" {
		if !removing[v.Worktree] {
			return keep("%s が残っている (先に worktree とブランチを片付ける)", left)
		}
		action = AfterWorktree
	}
	for _, o := range v.Sessions {
		if !sessionIDRe.MatchString(o.SessionID) {
			return keep("session id の形が違う (%q)", o.SessionID)
		}
		ts, n, why, err := transcriptsOf(in.Projects, o.SessionID, v.Worktree)
		if err != nil {
			return keep("transcript を読めない: %v", err)
		}
		if why != "" {
			return keep("%s", why)
		}
		v.Transcripts, v.Bytes = append(v.Transcripts, ts...), v.Bytes+n
		job, why, err := jobOf(in.JobsDir, o, repo, v.Worktree, BranchName(name))
		if err != nil {
			return keep("claude の job を読めない: %v", err)
		}
		if why != "" {
			return keep("%s", why)
		}
		if job != "" {
			v.Jobs = append(v.Jobs, job)
		}
	}
	v.Action = action
	if action == AfterWorktree {
		v.Why = "この一覧で worktree とブランチを消した後、session はどれも動いていない"
	} else {
		v.Why = "worktree とブランチは消えていて、session はどれも動いていない"
	}
	return v
}

// ownedOf は id のカードの session (起動の記録の今の分と退いた分、片付けの印。session id で重ねない)。
func ownedOf(in Inputs, id string) []Owned {
	var out []Owned
	seen := map[string]bool{}
	add := func(o Owned) {
		if o.SessionID == "" || seen[o.SessionID] {
			return
		}
		seen[o.SessionID] = true
		out = append(out, o)
	}
	for _, o := range in.Owned {
		if o.CardID == id {
			add(o)
		}
	}
	if m, ok := in.Purged[id]; ok {
		for _, s := range m.Sessions {
			add(Owned{ID: s.ID, SessionID: s.SessionID, CardID: id})
		}
	}
	return out
}

// worktreeLeft は worktree かブランチが残っていればその説明 (無ければ空)。置き場のディレクトリ・git の worktree の一覧・ブランチの全部を見る。
func worktreeLeft(ctx context.Context, repo, wt, branch string) (string, error) {
	if _, err := os.Lstat(wt); err == nil {
		return "worktree " + wt, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	wts, err := listWorktrees(ctx, repo)
	if err != nil {
		return "", err
	}
	if slices.ContainsFunc(wts, func(w worktree) bool { return w.Path == wt || strings.EqualFold(w.Path, wt) }) {
		return "git の worktree の登録 " + wt, nil
	}
	_, rc, err := gitx.Run(ctx, repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	if err != nil {
		return "", err
	}
	if rc == 0 {
		return "ブランチ " + branch, nil
	}
	return "", nil
}

// transcriptsOf は session id の transcript (<置き場>/<id>.jsonl) とサブエージェントの置き場 (<置き場>/<id>/) と、その大きさの和。
// どの transcript も wt を cwd にした記録を持たなければ、消さない理由 (why) を返す。
func transcriptsOf(projects, sid, wt string) (paths []string, bytes int64, why string, err error) {
	if projects == "" {
		return nil, 0, "", nil
	}
	files, err := filepath.Glob(filepath.Join(projects, "*", sid+".jsonl"))
	if err != nil {
		return nil, 0, "", err
	}
	dirs, err := filepath.Glob(filepath.Join(projects, "*", sid))
	if err != nil {
		return nil, 0, "", err
	}
	proven := map[string]bool{} // transcript を確かめた置き場 (サブエージェントの置き場はそこにあるものだけ消す)
	for _, f := range files {
		ok, err := ranIn(f, wt)
		if err != nil {
			return nil, 0, "", err
		}
		if !ok {
			return nil, 0, fmt.Sprintf("transcript %s に worktree (%s) を cwd にした記録が無い (pro-con の PG の session と示せない)", f, wt), nil
		}
		proven[filepath.Dir(f)] = true
		n, _, err := diskuse.Size(f)
		if err != nil {
			return nil, 0, "", err
		}
		paths, bytes = append(paths, f), bytes+n
	}
	for _, d := range dirs {
		if !proven[filepath.Dir(d)] {
			return nil, 0, d + " の隣に確かめた transcript が無い", nil
		}
		n, _, err := diskuse.Size(d)
		if err != nil {
			return nil, 0, "", err
		}
		paths, bytes = append(paths, d), bytes+n
	}
	return paths, bytes, "", nil
}

// ranIn は transcript の cwd の記録 (各行の最上位の "cwd") が 1 つ以上あり、どれも wt (かその下) か。
// 🚨 1 つ合えば通す形にしない: PG が止まった後に人が同じ session を別の場所で続けると、その後の会話まで消す (敵対的レビュー)。
// 🚨 全体を一度に読まない (transcript は数十 MB になる)。bufio.Scanner にしない (行の長さに上限がある。transcript.go と同じ)
func ranIn(path, wt string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	seen := false
	br := bufio.NewReader(f)
	for {
		line, err := br.ReadBytes('\n')
		if bytes.Contains(line, []byte(`"cwd"`)) {
			var r struct {
				Cwd string `json:"cwd"`
			}
			if json.Unmarshal(line, &r) == nil && r.Cwd != "" {
				if !within(r.Cwd, wt) {
					return false, nil
				}
				seen = true
			}
		}
		if errors.Is(err, io.EOF) {
			return seen, nil
		}
		if err != nil {
			return false, err
		}
	}
}

// jobState は ~/.claude/jobs/<短い id>/state.json のうち、確かめに使う欄 (2.1.283 で実測)。
type jobState struct {
	SessionID      string `json:"sessionId"`
	Cwd            string `json:"cwd"`
	WorktreePath   string `json:"worktreePath"`
	WorktreeBranch string `json:"worktreeBranch"`
}

// jobOf は claude rm に渡す短い id (job が残っていなければ空)。job が別の session・別の worktree を持っていれば消さない理由 (why)。
// claude --bg -w で起動した job は cwd が起動した repo で、worktree は worktreePath にある。再開 (--resume。-w なし) の job は
// worktreePath が空で、cwd が worktree (2.1.283 の実物で実測 2026-09-26)。
func jobOf(jobsDir string, o Owned, repo, wt, branch string) (id, why string, err error) {
	if jobsDir == "" || o.ID == "" {
		return "", "", nil
	}
	if !shortIDRe.MatchString(o.ID) {
		return "", fmt.Sprintf("短い id の形が違う (%q)", o.ID), nil
	}
	data, err := os.ReadFile(filepath.Join(jobsDir, o.ID, "state.json"))
	if errors.Is(err, os.ErrNotExist) {
		if _, serr := os.Lstat(filepath.Join(jobsDir, o.ID)); serr == nil {
			return "", fmt.Sprintf("claude の job %s に state.json が無い (何の job か示せない)", o.ID), nil
		}
		return "", "", nil // job はもう無い (claude rm 済み)
	}
	if err != nil {
		return "", "", err
	}
	var js jobState
	if err := json.Unmarshal(data, &js); err != nil {
		return "", fmt.Sprintf("claude の job %s の state.json を読めない: %v", o.ID, err), nil
	}
	switch {
	case js.SessionID != o.SessionID:
		return "", fmt.Sprintf("claude の job %s は別の session (%s)", o.ID, js.SessionID), nil
	case js.WorktreePath != "" && filepath.Clean(js.WorktreePath) != wt:
		return "", fmt.Sprintf("claude の job %s が別の worktree (%s) を持つ (claude rm がそれを消しうる)", o.ID, js.WorktreePath), nil
	case js.WorktreePath != "" && (js.Cwd == "" || !within(js.Cwd, repo)):
		return "", fmt.Sprintf("claude の job %s の cwd (%s) がカードの repo の外", o.ID, js.Cwd), nil
	case js.WorktreePath == "" && (js.Cwd == "" || !within(js.Cwd, wt)):
		return "", fmt.Sprintf("claude の job %s の cwd (%s) が worktree の外", o.ID, js.Cwd), nil
	case js.WorktreeBranch != "" && js.WorktreeBranch != branch:
		return "", fmt.Sprintf("claude の job %s が別のブランチ (%s) を持つ (claude rm がそれを消しうる)", o.ID, js.WorktreeBranch), nil
	}
	return o.ID, "", nil
}

// SessionOptions は session を消すときの決まり。
type SessionOptions struct {
	StateDir string                                    // pro-con の状態の置き場。ここ (とその親) は決して消さない
	Fresh    func(ctx context.Context) (Inputs, error) // 判定の材料を取り直す (1 枚ごと)
	// RemoveJob は claude の job を消す (本物は ClaudeRemover)。Forget は起動の記録の行と印を消す依頼を dispatcher に置く
	RemoveJob func(ctx context.Context, id string) error
	Forget    func(cardID string, sessionIDs []string) error
}

// CleanSessions は一覧 (ScanSessions) のうち消してよいものを 1 枚ずつ取り直して消す。
func CleanSessions(ctx context.Context, targets []SessionVerdict, opt SessionOptions, each func(SessionVerdict, Outcome, string)) error {
	if opt.Fresh == nil || opt.RemoveJob == nil || opt.Forget == nil {
		return errors.New("wtclean: Fresh / RemoveJob / Forget が無い (取り直さずに消さない)")
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
		v, out, detail := cleanSessionsOne(ctx, in, t.CardID, opt)
		each(v, out, detail)
	}
	return nil
}

func cleanSessionsOne(ctx context.Context, in Inputs, id string, opt SessionOptions) (SessionVerdict, Outcome, string) {
	v := JudgeSessions(ctx, in, id, nil)
	if v.Action != RemoveSessions {
		return v, Skipped, v.Why
	}
	if err := allowSessions(in, v, opt.StateDir); err != nil {
		return v, Failed, "消す前に拒否した: " + err.Error()
	}
	for _, j := range v.Jobs {
		if err := opt.RemoveJob(ctx, j); err != nil {
			return v, Failed, fmt.Sprintf("claude rm %s が失敗した (transcript は消していない): %v", j, err)
		}
	}
	for _, p := range v.Transcripts {
		if err := os.RemoveAll(p); err != nil {
			return v, Failed, "transcript を消せない: " + err.Error()
		}
		if _, err := os.Lstat(p); !errors.Is(err, os.ErrNotExist) {
			return v, Failed, "消した後も " + p + " が残っている"
		}
		if fi, err := os.Lstat(filepath.Dir(p)); err == nil && fi.IsDir() { // symlink の置き場は外さない (中身があっても symlink は消える)
			_ = os.Remove(filepath.Dir(p)) // 空になった置き場だけ消える (中身があれば rmdir が断る)
		}
	}
	sids := make([]string, 0, len(v.Sessions))
	for _, o := range v.Sessions {
		sids = append(sids, o.SessionID)
	}
	if err := opt.Forget(v.CardID, sids); err != nil {
		return v, Failed, "transcript と job は消したが、起動の記録の行を消す依頼を置けない (もう一度打てば置き直す): " + err.Error()
	}
	return v, Removed, fmt.Sprintf("transcript %d 個・claude の job %d 本を消し、起動の記録の行 %d 本と片付けの印を消す依頼を dispatcher に置いた",
		len(v.Transcripts), len(v.Jobs), len(sids))
}

// allowSessions は消す操作の手前の拒否。消してよいのは transcript の置き場の直下の置き場にある <判定した session id>.jsonl と
// <判定した session id>/ だけ。状態の置き場とその親には重ならない。
// 🚨 テストの二進では、SetTestSandbox で登録した置き場の外を全部拒否する (transcript の置き場も sandbox の中に作る)。
func allowSessions(in Inputs, v SessionVerdict, stateDir string) error {
	if len(v.Transcripts) > 0 {
		if in.Projects == "" || !filepath.IsAbs(in.Projects) {
			return fmt.Errorf("transcript の置き場 (%q) が絶対パスではない", in.Projects)
		}
	}
	projects := resolve(in.Projects)
	sids := map[string]bool{}
	for _, o := range v.Sessions {
		if sessionIDRe.MatchString(o.SessionID) {
			sids[o.SessionID] = true
		}
	}
	for _, p := range v.Transcripts {
		parent, err := filepath.EvalSymlinks(filepath.Dir(p))
		if err != nil {
			return err
		}
		base := filepath.Base(p)
		switch {
		case filepath.Dir(parent) != projects || parent == projects:
			return fmt.Errorf("%s は transcript の置き場 (%s/<置き場>/) の直下ではない", p, projects)
		case !sids[strings.TrimSuffix(base, ".jsonl")]:
			return fmt.Errorf("%s は判定した session の transcript ではない", p)
		}
		if fi, err := os.Lstat(p); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s は symlink (辿った先を消さない)", p)
		}
		full := filepath.Join(parent, base)
		if stateDir != "" {
			sd := resolve(stateDir)
			if within(full, sd) || within(sd, full) {
				return fmt.Errorf("%s は状態の置き場 (%s) に重なる", full, sd)
			}
		}
		if testing.Testing() {
			root := testSandboxRoot()
			if root == "" || !within(full, root) {
				return fmt.Errorf("テストの二進で、置き場 (%q) の外 (%s) を消そうとした", root, full)
			}
		}
	}
	for _, j := range v.Jobs {
		if !shortIDRe.MatchString(j) {
			return fmt.Errorf("claude rm に渡す id の形が違う (%q)", j)
		}
	}
	return nil
}

// ClaudeRemover は `claude rm <短い id>` で job を消し (agents --all の行と ~/.claude/jobs/<id> が消える。transcript は残る =
// 2.1.283 で実測 2026-09-26)、job の置き場が消えたかを確かめる。
// 🚨 テストの二進では呼ばない (本物の ~/.claude/jobs を消す)。
func ClaudeRemover(claude, jobsDir string) func(ctx context.Context, id string) error {
	return func(ctx context.Context, id string) error {
		if testing.Testing() {
			return errors.New("テストの二進で本物の claude rm を呼ばない")
		}
		if !shortIDRe.MatchString(id) {
			return fmt.Errorf("claude rm に渡す id の形が違う (%q)", id)
		}
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, claude, "rm", id)
		cmd.Dir = jobsDir // 中立な場所で走らせる (呼んだ shell の cwd の repo を claude rm に見せない)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
		}
		if _, err := os.Lstat(filepath.Join(jobsDir, id)); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("claude rm の後も job の置き場 %s が残っている", filepath.Join(jobsDir, id))
		}
		return nil
	}
}
