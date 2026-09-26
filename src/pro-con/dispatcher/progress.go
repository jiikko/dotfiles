package dispatcher

// カードの作業の進捗を集めて store.ProgressFile に書く (issue 469)。画面と `pro-con card show` はそれを読むだけ
// (画面が 3 秒ごとに git や issue のファイルを読まない = 441)。
//
// 集めるもの (カードごと):
//   - PG の worktree の git: 取り込む先 (origin/master) より先の commit・未 commit の変更の数・最後の commit の時刻
//   - カードの issue の本文の「進捗」節: チェックボックスの済み / 残り (無ければ最後の項目)。worktree があれば worktree の本文 (PG が書き足した分)
//
// 🚨 git は読むだけ (--no-optional-locks。status が index を書き直さない)。取り込みの衝突は見張り (package monitor) が見て
// store.ConflictsFile に書く (同じ merge-tree を 2 か所で回さない)。

import (
	"bufio"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"termsafe"

	"pro-con/card"
	"pro-con/monitor"
	"pro-con/store"
)

// progressEvery は集め直す間隔 (カードごとに git を数回走らせるので、Doing の 10 秒より長くする)。
const progressEvery = 30 * time.Second

// ProgressGit は進捗が使う git の読み取り (テストが差し替える)。
type ProgressGit interface {
	// Base は repo の取り込む先の commit と名前 (見張りと同じ選び方。monitor.ExecGit)。
	Base(ctx context.Context, repo string) (sha, name string, err error)
	// Branch は worktree の HEAD の、base より先の commit と未 commit の変更。
	Branch(ctx context.Context, worktree, base string) (Branch, error)
}

// Branch は PG の worktree の様子。
type Branch struct {
	Ahead      int
	Commits    []card.Commit // 新しい順 (上限 card.ProgressCommits)
	Dirty      int
	LastCommit time.Time
}

// ExecProgressGit は本物の git。
type ExecProgressGit struct{}

func (ExecProgressGit) Base(ctx context.Context, repo string) (string, string, error) {
	return monitor.ExecGit{}.Base(ctx, repo)
}

func (ExecProgressGit) Branch(ctx context.Context, wt, base string) (Branch, error) {
	var b Branch
	n, err := gitOut(ctx, wt, "rev-list", "--count", base+"..HEAD")
	if err != nil {
		return b, err
	}
	if b.Ahead, err = strconv.Atoi(n); err != nil {
		return b, err
	}
	if b.Ahead > 0 {
		log, err := gitOut(ctx, wt, "log", "-n", strconv.Itoa(card.ProgressCommits), "--format=%h%x09%s", base+"..HEAD")
		if err != nil {
			return b, err
		}
		for _, l := range strings.Split(log, "\n") {
			if h, s, ok := strings.Cut(l, "\t"); ok {
				b.Commits = append(b.Commits, card.Commit{Hash: h, Subject: termsafe.PlainLine(s)})
			}
		}
	}
	if ct, err := gitOut(ctx, wt, "log", "-1", "--format=%ct", "HEAD"); err == nil {
		if sec, err := strconv.ParseInt(ct, 10, 64); err == nil {
			b.LastCommit = time.Unix(sec, 0)
		}
	}
	st, err := gitOut(ctx, wt, "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return b, err
	}
	for _, l := range strings.Split(st, "\n") {
		if l != "" {
			b.Dirty++
		}
	}
	return b, nil
}

// collectProgress は progressEvery ごとに、裏で進捗を集めて書く (git で Tick の割り当てを待たせない)。前の回が終わっていなければ飛ばす。
func (d *Dispatcher) collectProgress(ctx context.Context, now time.Time) {
	if d.ProgressGit == nil || (!d.progressAt.IsZero() && now.Sub(d.progressAt) < progressEvery) {
		return
	}
	if !d.progressBusy.CompareAndSwap(false, true) {
		return
	}
	d.progressAt = now
	go func() {
		defer d.progressBusy.Store(false)
		if p, err := d.gatherProgress(ctx, now); err == nil && ctx.Err() == nil {
			_ = store.SaveProgress(d.Dir, p) // 書けなくても割り当ては止めない (詳細に進捗が出ないだけ)
		}
	}()
}

// gatherProgress は記録を読み、作業の途中のカードの進捗を集める。読むのは Dir・Repos・ProgressGit だけ (裏の goroutine から呼ぶ)。
func (d *Dispatcher) gatherProgress(ctx context.Context, now time.Time) (store.Progress, error) {
	st, err := store.Load(d.Dir)
	if err != nil {
		return store.Progress{}, err
	}
	out := store.Progress{At: now, Cards: map[string]card.Progress{}}
	type base struct{ sha, name, err string }
	bases := map[string]base{} // repo → 取り込む先 (1 回に 1 度だけ引く)
	for _, c := range st.Cards {
		repo, ok := d.Repos[c.Repo]
		if !ok || c.Archived || !inProgress(c.State) {
			continue
		}
		var p card.Progress
		var errs []string
		wt := card.WorktreePath(repo, c)
		dir := repo
		if fi, err := os.Stat(wt); err == nil && fi.IsDir() {
			dir = wt
			b, ok := bases[c.Repo]
			if !ok {
				sha, name, err := d.ProgressGit.Base(ctx, repo)
				b = base{sha: sha, name: name}
				if err != nil {
					b.err = err.Error()
				}
				bases[c.Repo] = b
			}
			if b.err != "" {
				errs = append(errs, "取り込む先: "+b.err)
			} else if br, err := d.ProgressGit.Branch(ctx, wt, b.sha); err != nil {
				errs = append(errs, "worktree の git: "+err.Error())
			} else {
				p.Base, p.Ahead, p.Commits, p.Dirty, p.LastCommit = b.name, br.Ahead, br.Commits, br.Dirty, br.LastCommit
			}
		}
		for _, r := range c.Issues {
			if r.Repo != c.Repo { // 別の repo の issue は、その repo の本文を読む場所が worktree に無い
				continue
			}
			ip, err := issueProgress(dir, r)
			if err != nil {
				errs = append(errs, r.String()+": "+err.Error())
			}
			if ip.Total > 0 || ip.Last != "" || err == nil { // 途中で読めなくなった本文 (長すぎる行) も、読めた分は出す
				p.Issues = append(p.Issues, ip)
			}
		}
		p.Err = strings.Join(errs, " / ")
		if p.Base != "" || len(p.Issues) > 0 || p.Err != "" {
			out.Cards[c.ID] = p
		}
	}
	return out, nil
}

// inProgress は進捗を集める列 (PG が作業を始めうる〜取り込み前)。依頼はまだ分けていない、完了は取り込み済み。
func inProgress(s card.State) bool {
	switch s {
	case card.Planned, card.Running, card.Waiting, card.Review:
		return true
	case card.Requested, card.Done:
	}
	return false
}

// issueProgress は dir の issues/ から r の本文を探し、「進捗」節を読む。
func issueProgress(dir string, r card.IssueRef) (card.IssueProgress, error) {
	ip := card.IssueProgress{Ref: r.String()}
	path, err := findIssue(filepath.Join(dir, "issues"), r.Number)
	if err != nil {
		return ip, err
	}
	f, err := os.Open(path)
	if err != nil {
		return ip, err
	}
	defer func() { _ = f.Close() }()
	return parseProgress(ip, bufio.NewScanner(f))
}

// findIssue は issues/ の下 (open / next / done / epic のどこでも) で「<番号>-」から始まる .md を探す。
func findIssue(root string, n int) (string, error) {
	prefix := strconv.Itoa(n) + "-"
	if n < 100 {
		prefix = strconv.Itoa(1000 + n)[1:] + "-" // 3 桁に揃えた名前 (042-…)
	}
	var found string
	err := filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !e.IsDir() && strings.HasPrefix(e.Name(), prefix) && strings.HasSuffix(e.Name(), ".md") {
			found = p
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", os.ErrNotExist
	}
	return found, nil
}

var (
	headingRe  = regexp.MustCompile(`^(#+)\s+(.*)$`)
	checkboxRe = regexp.MustCompile(`^\s*[-*]\s+\[([ xX])\]\s+(.*)$`)
)

// progressLeftMax は残りの行を並べる上限 / progressLastRunes は最後の項目を切る長さ (詳細を 1 枚の issue で埋めない)。
const (
	progressLeftMax   = 8
	progressLastRunes = 120
)

// parseProgress は見出しに「進捗」を含む節 (同じか浅い見出しまで。深い見出しは節の中) を読む。節が複数あれば全部を足す。
func parseProgress(ip card.IssueProgress, sc *bufio.Scanner) (card.IssueProgress, error) {
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	level := 0 // 0 = 節の外
	fence := false
	for sc.Scan() {
		line := termsafe.PlainLine(sc.Text()) // 本文は詳細にそのまま出す (色と制御文字を落とす)
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fence = !fence
			continue
		}
		if fence {
			continue
		}
		if m := headingRe.FindStringSubmatch(line); m != nil {
			// H1 は issue の題 (「…進捗…を確かめられる」のように題に入ることがある) なので節にしない。節の中の深い見出しは
			// 「進捗」を含んでも節を縮めない (### 進捗メモ の後の ### 次 で ## 進捗 を閉じない)
			switch l := len(m[1]); {
			case level > 0 && l > level:
			case l >= 2 && strings.Contains(m[2], "進捗"):
				level = l
			case level > 0:
				level = 0
			}
			continue
		}
		if level == 0 {
			continue
		}
		if m := checkboxRe.FindStringSubmatch(line); m != nil {
			ip.Total++
			if m[1] == " " {
				if len(ip.Left) < progressLeftMax {
					ip.Left = append(ip.Left, card.ClipRunes(m[2], progressLastRunes))
				}
			} else {
				ip.Done++
			}
			continue
		}
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") { // 字下げの無い項目 (続きの行と子の項目は数えない)
			ip.Last = card.ClipRunes(strings.TrimSpace(line[2:]), progressLastRunes)
		}
	}
	if ip.Total > 0 {
		ip.Last = ""
	}
	return ip, sc.Err()
}
