// Package wtclean は閉じたカードの PG の worktree を片付ける (issue 492)。447 の決まりでカードを閉じても PG の worktree と
// ブランチは残すので、消す仕組みが無いと溜まる一方になる。
//
// 判定 (Judge) は 1 か所だけに置く: `pro-con worktree clean` の一覧と、消す直前の取り直しと、後で設定画面 (456) の内訳が同じ関数を呼ぶ。
//
// 消してよいのは、次を全部満たす worktree だけ:
//   - <repo>/.claude/worktrees/pc-<カード> で、git の worktree として登録されている (PM・取り込みの係の pc-pm-* / pc-int-* は除く)
//   - そのカードが記録か書庫にあり、同じ repo のカードで、完了して PG を止め終えたもの (削除の途中でない)。
//     完了から 1 週間で記録から消したカードは、片付けの印 (store.Purged。issue 497) にあれば完了したカードと見る
//   - その中 (かその下) を cwd にした session もプロセス (人の shell・エディタ・テスト) も居ない。このコマンドもその中で走っていない
//   - git worktree lock が無いか、Claude Code が session に掛けた lock (claude session pc-<カード> (pid N …)) で、その pid の claude が居ない。
//     🚨 Claude Code はこの lock を session が終わっても外さず、pid も生きている session のものと一致しない (2.1.282 で実測 2026-09-26:
//     動いている pc-c-053 の lock の pid が既に居なかった)。session が動いているかは lock ではなく claude agents の cwd で見る
//   - 未 commit の変更・追跡していないファイル・skip-worktree / assume-unchanged の印が無く、その下に別の worktree も無い
//   - 無視されたファイルは、中身の無いディレクトリか、作り直せる go_autobuild の産物 (.autobuild.* とその隣の実行ファイル) だけ
//     (🚨 git worktree remove は --force なしでも無視されたファイルを黙って消す。tmp/ のレポート・.env・settings.local.json・入れ子の repo を失わない)
//   - 先端が取り込む先 (origin/master。無ければ origin/main) の祖先か、`git cherry` が全部 - で、かつ空白まで同じ patch
//     (git patch-id --verbatim) が取り込む先にある (rebase / cherry-pick で入った。🚨 git cherry の patch-id は空白の違いを無視する)
//
// ブランチを消すのはそのうえで、pro-con が作った名前 (worktree-pc-<カード>) で、ほかの worktree が使っていないときだけ。
// master に無い commit があるものは worktree もブランチも消さない (人が見る)。
//
// worktree とブランチを消した後のカードは、pro-con が起動した session (起動の記録の行・transcript・claude の job) も消す (sessions.go)。
package wtclean

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"pro-con/agents"
	"pro-con/card"
	"pro-con/gitx"
	"pro-con/store"
)

// Action は 1 個の worktree をどうするか。
type Action string

const (
	RemoveAll  Action = "worktree とブランチを消す"
	RemoveTree Action = "worktree だけ消す (ブランチは残す)"
	Keep       Action = "消さない"
)

// Verdict は worktree 1 個の判定。
type Verdict struct {
	Repo     string `json:"repo"`     // repo の名前 (設定)
	RepoPath string `json:"repoPath"` // repo の絶対パス
	Path     string `json:"path"`     // worktree の絶対パス
	Name     string `json:"name"`     // pc-c-004
	CardID   string `json:"cardId,omitempty"`
	Head     string `json:"head,omitempty"`
	Branch   string `json:"branch,omitempty"` // 短い名前 (worktree-pc-c-004)。detached なら空
	Action   Action `json:"action"`
	// Why は判定の根拠。消すなら中身が master にあると示せた理由、消さないなら消さない理由
	Why string `json:"why"`
	// BranchWhy は RemoveTree のとき、ブランチを残す理由
	BranchWhy string `json:"branchWhy,omitempty"`
	// Lock は Claude Code が掛けたまま残した lock の理由 (消すときに外す。外した後に消せなければ掛け直す)
	Lock string `json:"lock,omitempty"`
}

// Removable は worktree を消してよいか。
func (v Verdict) Removable() bool { return v.Action == RemoveAll || v.Action == RemoveTree }

// Inputs は判定の材料 (git 以外)。消す直前には毎回取り直す (Clean の fresh)。
type Inputs struct {
	Repos    map[string]string    // repo の名前 → 絶対パス (設定)
	Cards    map[string]card.Card // カードの ID → カード (記録と書庫。記録を正とする)
	Sessions []agents.Session     // 動いている session (claude agents --json。対話の session も含む)
	Cwd      string               // このコマンドを走らせた作業ディレクトリ
	ProcCwds []string             // 今動いているプロセスの作業ディレクトリ (lsof。claude agents に出ない人の shell・エディタ・テスト)
	// Purged は完了から 1 週間で記録から消したカードの片付けの印 (カード ID → 印)
	Purged map[string]store.Purged
	// Owned は pro-con が起動した session (起動の記録の今の分と退いた分)。session の片付けはここにある session だけを扱う
	Owned []Owned
	// Projects は transcript の置き場 (~/.claude/projects)。JobsDir は claude の job の置き場 (~/.claude/jobs。空なら job を見ない)
	Projects string
	JobsDir  string
}

// Owned は起動の記録の 1 行のうち、片付けに要る欄 (live.Owned から作る。この package は live を知らない)。
type Owned struct {
	ID        string // claude の短い id (claude rm に渡す)
	SessionID string
	CardID    string
}

// cardOf は判定に使うカード。記録か書庫にあればそれ (記録を正とする)、無ければ片付けの印から完了したカードとして組む。
func cardOf(in Inputs, id string) (card.Card, bool) {
	if c, ok := in.Cards[id]; ok {
		return c, true
	}
	if m, ok := in.Purged[id]; ok && m.CardID == id {
		return card.Card{ID: m.CardID, Repo: m.Repo, State: card.Done}, true
	}
	return card.Card{}, false
}

// worktreesDir は pro-con の PG の worktree を置く所 (claude --bg -w pc-<カード> が作る。card.WorktreePath)。
func worktreesDir(repo string) string { return filepath.Join(repo, ".claude", "worktrees") }

// rolePrefixes は役 (PM・取り込みの係) の worktree の名前の頭。役は次の知らせをそこで再開するので消さない。
var rolePrefixes = []string{"pc-pm-", "pc-int-"}

func isRole(name string) bool {
	return slices.ContainsFunc(rolePrefixes, func(p string) bool { return strings.HasPrefix(name, p) })
}

// Scan は設定の全 repo の pro-con の worktree を判定する (repo の名前・worktree の名前の順)。
// repo 1 個が読めなくても、ほかの repo は判定する (読めなかった repo はエラーにまとめる)。
func Scan(ctx context.Context, in Inputs) ([]Verdict, error) {
	names := make([]string, 0, len(in.Repos))
	for n := range in.Repos {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []Verdict
	var errs []string
	for _, n := range names {
		vs, err := scanRepo(ctx, in, n)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", n, err))
		}
		out = append(out, vs...)
	}
	if len(errs) > 0 {
		return out, fmt.Errorf("読めない repo がある: %s", strings.Join(errs, " / "))
	}
	return out, nil
}

func scanRepo(ctx context.Context, in Inputs, repoName string) ([]Verdict, error) {
	repo, err := filepath.EvalSymlinks(in.Repos[repoName])
	if err != nil {
		return nil, err
	}
	wts, err := listWorktrees(ctx, repo)
	if err != nil {
		return nil, err
	}
	dir := worktreesDir(repo)
	var out []Verdict
	seen := map[string]bool{}
	for _, w := range wts {
		if filepath.Dir(w.Path) != dir || !strings.HasPrefix(filepath.Base(w.Path), "pc-") {
			continue
		}
		seen[filepath.Base(w.Path)] = true
		out = append(out, judge(ctx, in, repoName, repo, w, wts))
	}
	// 置き場にあって git の worktree として登録されていないもの (消えかけ・手で作った) は、判定できないので残す
	ents, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return out, err
	}
	for _, e := range ents {
		if e.IsDir() && strings.HasPrefix(e.Name(), "pc-") && !seen[e.Name()] {
			out = append(out, Verdict{Repo: repoName, RepoPath: repo, Path: filepath.Join(dir, e.Name()), Name: e.Name(),
				Action: Keep, Why: "git の worktree として登録されていない (git worktree list に無い)"})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Judge は repo の中の worktree 1 個を今の状態で判定し直す (消す直前の取り直し。一覧を読み直してから判定する)。
func Judge(ctx context.Context, in Inputs, repoName, wtPath string) (Verdict, error) {
	repo, err := filepath.EvalSymlinks(in.Repos[repoName])
	if err != nil {
		return Verdict{}, err
	}
	wts, err := listWorktrees(ctx, repo)
	if err != nil {
		return Verdict{}, err
	}
	for _, w := range wts {
		if w.Path == wtPath {
			return judge(ctx, in, repoName, repo, w, wts), nil
		}
	}
	return Verdict{Repo: repoName, RepoPath: repo, Path: wtPath, Name: filepath.Base(wtPath), Action: Keep,
		Why: "git の worktree として登録されていない (git worktree list に無い)"}, nil
}

// judge は判定の本体。all は同じ repo の worktree の全部 (ブランチをほかの worktree が使っていないかを見る)。
func judge(ctx context.Context, in Inputs, repoName, repo string, w worktree, all []worktree) Verdict {
	name := filepath.Base(w.Path)
	v := Verdict{Repo: repoName, RepoPath: repo, Path: w.Path, Name: name, Head: w.Head, Branch: w.Branch, Lock: w.LockReason, Action: Keep}
	keep := func(format string, a ...any) Verdict {
		v.Why = fmt.Sprintf(format, a...)
		return v
	}
	switch {
	case isRole(name):
		return keep("PM・取り込みの係の worktree (役が次の知らせをそこで再開する)")
	case w.Locked && !claudeLock(w.LockReason, name):
		return keep("git worktree lock されている (%s)", firstNonEmpty(w.LockReason, "理由なし"))
	case w.Locked && lockHolderAlive(w.LockReason):
		return keep("lock を掛けた claude のプロセスが居る (%s)", w.LockReason)
	case w.Prunable:
		return keep("git が消えかけの worktree と見ている (git worktree prune の対象)")
	case w.Head == "":
		return keep("HEAD を読めない")
	}
	id := strings.ToUpper(strings.TrimPrefix(name, "pc-"))
	c, ok := cardOf(in, id)
	if !ok || card.SessionName(c) != name {
		return keep("記録に無いカードの worktree")
	}
	v.CardID = c.ID
	if p, ok := in.Repos[c.Repo]; !ok || !samePath(p, repo) {
		return keep("カードの repo (%s) の worktree ではない", c.Repo)
	}
	switch {
	case c.State != card.Done:
		return keep("カードが完了していない (%s)", c.State.Label())
	case c.StopAfterClose:
		return keep("閉じたカードの PG をまだ止め終えていない")
	case c.Deleting():
		return keep("カードの削除の途中")
	}
	for _, s := range in.Sessions {
		if s.Cwd != "" && within(s.Cwd, w.Path) {
			return keep("session が動いている (%s)", firstNonEmpty(s.Name, s.ID, s.SessionID))
		}
	}
	if in.Cwd != "" && within(in.Cwd, w.Path) {
		return keep("このコマンドをその中で走らせている")
	}
	for _, p := range in.ProcCwds {
		if within(p, w.Path) {
			return keep("その中を作業ディレクトリにしたプロセスが居る (%s)", p)
		}
	}
	for _, o := range all {
		if o.Path != w.Path && within(o.Path, w.Path) {
			return keep("その下に別の worktree がある (%s)", o.Path)
		}
	}
	st, err := readStatus(ctx, w.Path)
	if err != nil {
		return keep("git status を読めない: %v", err)
	}
	switch {
	case len(st.changes) > 0:
		return keep("未 commit の変更・追跡していないファイルが %d 件 (%s)", len(st.changes), clip(st.changes))
	case len(st.hidden) > 0:
		return keep("skip-worktree / assume-unchanged の印が付いたファイルがある (git status に変更が出ない: %s)", clip(st.hidden))
	case len(st.ignored) > 0:
		return keep("消すと戻せない無視されたファイルがある (%s)", clip(st.ignored))
	}
	base, baseName, err := trunk(ctx, repo)
	if err != nil {
		return keep("取り込む先を読めない: %v", err)
	}
	merged, why, err := inBase(ctx, repo, base, baseName, w.Head)
	if err != nil {
		return keep("%s との比較に失敗した: %v", baseName, err)
	}
	if !merged {
		return keep("%s", why)
	}
	v.Why = why
	switch {
	case w.Branch == "":
		v.Action, v.BranchWhy = RemoveTree, "ブランチが無い (detached HEAD)"
	case w.Branch != BranchName(name):
		v.Action, v.BranchWhy = RemoveTree, fmt.Sprintf("pro-con が作った名前 (%s) ではない", BranchName(name))
	case usedElsewhere(all, w):
		v.Action, v.BranchWhy = RemoveTree, "ほかの worktree が同じブランチを使っている"
	default:
		v.Action = RemoveAll
	}
	return v
}

// BranchName は claude -w <name> が worktree に作るブランチの名前 (427 の 3f で実測: worktree-pc-c-053)。
func BranchName(name string) string { return "worktree-" + name }

// trunk は取り込む先 (origin/master、無ければ origin/main) の commit と名前。
// 🚨 origin/HEAD は見ない: remote の既定のブランチが作業用のブランチを指していると、master に無いものを「取り込み済み」と読む
// (gitx.Base は見張りの衝突の相手を決めるもので、消してよいかの根拠には使わない)
func trunk(ctx context.Context, repo string) (string, string, error) {
	for _, name := range []string{"origin/master", "origin/main"} {
		out, rc, err := gitx.Run(ctx, repo, "rev-parse", "--verify", "--quiet", "refs/remotes/"+name+"^{commit}")
		if err != nil {
			return "", "", err
		}
		if rc == 0 {
			return strings.TrimSpace(out), name, nil
		}
	}
	return "", "", errors.New("取り込む先 (origin/master・origin/main) が無い")
}

// inBase は head の中身が取り込む先 base にあるか。祖先なら確か。祖先でなければ git cherry の patch の同一性で見て
// (rebase / cherry-pick で入ったものは祖先にならない)、さらに空白まで同じ patch が base にあるかを確かめる (verbatimMissing)。
// 🚨 head を base へ merge した tree と base の tree を比べる形にはしない: 取り込んだ後に master が同じ行をさらに変えていると衝突し、
// 正しく取り込まれたものまで残す (2026-09-26 の実物で 36 個中 17 個)。🚨 git cherry は merge commit を比べないので、base に無い merge commit が 1 本でもあれば
// 「示せない」側に倒す (merge の解決で足した中身を見落とさない)。
func inBase(ctx context.Context, repo, base, baseName, head string) (bool, string, error) {
	anc, err := gitx.IsAncestor(ctx, repo, head, base)
	if err != nil {
		return false, "", err
	}
	if anc {
		return true, fmt.Sprintf("先端が %s の祖先", baseName), nil
	}
	merges, err := countMerges(ctx, repo, base, head)
	if err != nil {
		return false, "", err
	}
	if merges > 0 {
		return false, fmt.Sprintf("%s に無い merge commit が %d 本ある (git cherry で比べられない)", baseName, merges), nil
	}
	plus, minus, err := cherry(ctx, repo, base, head)
	if err != nil {
		return false, "", err
	}
	if plus > 0 {
		return false, fmt.Sprintf("%s に無い commit が %d 本ある", baseName, plus), nil
	}
	if minus == 0 { // 祖先でないのに比べる commit が 0 本 (起きないはず)。示せていないので消さない
		return false, baseName + " の祖先ではないが、git cherry が commit を返さない", nil
	}
	missing, err := verbatimMissing(ctx, repo, base, head)
	if err != nil {
		return false, "", err
	}
	if missing > 0 {
		return false, fmt.Sprintf("git cherry は全部 - だが、空白まで比べると %s に無い変更が %d 本ある", baseName, missing), nil
	}
	return true, fmt.Sprintf("中身は全部 %s にある (git cherry の %d 本が全部 - で、空白まで同じ)", baseName, minus), nil
}

func usedElsewhere(all []worktree, w worktree) bool {
	return slices.ContainsFunc(all, func(o worktree) bool { return o.Path != w.Path && o.Branch == w.Branch })
}

// within は p が dir そのものかその下か。symlink を解いてから、大文字小文字を無視して比べる
// (🚨 APFS は既定で大文字小文字を区別しないので、session の cwd が PC-C-001 と書かれていても同じ dir。区別する volume では
// 別の dir も「その下」と読むが、消さない側に倒れるだけ)。
func within(p, dir string) bool {
	p, dir = strings.ToLower(resolve(p)), strings.ToLower(resolve(dir))
	return p == dir || strings.HasPrefix(p, dir+string(filepath.Separator))
}

func samePath(a, b string) bool {
	ai, aerr := os.Stat(a)
	bi, berr := os.Stat(b)
	if aerr == nil && berr == nil {
		return os.SameFile(ai, bi)
	}
	return resolve(a) == resolve(b)
}

func resolve(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return "名前なし"
}

// clip は理由に並べるファイルを 3 個までにする。
func clip(xs []string) string {
	if len(xs) <= 3 {
		return strings.Join(xs, ", ")
	}
	return strings.Join(xs[:3], ", ") + fmt.Sprintf(" ほか %d 件", len(xs)-3)
}
