package dispatcher

// カードの作業の進捗を集めて store.ProgressFile に書く (issue 469)。画面と `pro-con card show` はそれを読むだけ
// (画面が 3 秒ごとに git や issue のファイルを読まない = 441)。
//
// 集めるもの (カードごと):
//   - PG の worktree の git: 取り込む先 (origin/master) より先の commit・未 commit の変更の数・最後の commit の時刻
//   - 取り込む先との差分の本文 (issue 508): store.DiffPath に書き、progress.json には数と置き場だけ入れる
//   - カードの issue の本文の「進捗」節: チェックボックスの済み / 残り (無ければ最後の項目)。worktree があれば worktree の本文 (PG が書き足した分)
//
// 🚨 git は読むだけ (--no-optional-locks。status が index を書き直さない)。取り込みの衝突は見張り (package monitor) が見て
// store.ConflictsFile に書く (同じ merge-tree を 2 か所で回さない)。

import (
	"bufio"
	"context"
	"io"
	"io/fs"
	"os"
	"os/exec"
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
	// Diff は base との merge-base から作業ツリーまでの差分 (commit 済みと未 commit の分。未追跡は入らない) を、無害化した行で
	// 先頭 limit 行まで返す。cut は limit で切ったか。
	Diff(ctx context.Context, worktree, base string, limit int) (lines []string, cut bool, err error)
}

// Branch は PG の worktree の様子。
type Branch struct {
	Name       string // HEAD のブランチ (detached なら空)
	Ahead      int
	Commits    []card.Commit // 新しい順 (上限 card.ProgressCommits)
	Dirty      int
	DirtyFiles []string // `XY パス` (上限 card.ProgressDirtyFiles)
	LastCommit time.Time
}

// ExecProgressGit は本物の git。
type ExecProgressGit struct{}

func (ExecProgressGit) Base(ctx context.Context, repo string) (string, string, error) {
	return monitor.ExecGit{}.Base(ctx, repo)
}

func (ExecProgressGit) Branch(ctx context.Context, wt, base string) (Branch, error) {
	var b Branch
	if name, err := gitOut(ctx, wt, "symbolic-ref", "--short", "-q", "HEAD"); err == nil { // detached は rc=1
		b.Name = termsafe.PlainLine(name)
	}
	n, err := gitOut(ctx, wt, "rev-list", "--count", base+"..HEAD")
	if err != nil {
		return b, err
	}
	if b.Ahead, err = strconv.Atoi(n); err != nil {
		return b, err
	}
	if b.Ahead > 0 {
		log, err := gitOut(ctx, wt, "log", "-n", strconv.Itoa(card.ProgressCommits), "--format=%h%x09%ct%x09%s", base+"..HEAD")
		if err != nil {
			return b, err
		}
		for _, l := range strings.Split(log, "\n") {
			f := strings.SplitN(l, "\t", 3)
			if len(f) != 3 {
				continue
			}
			cm := card.Commit{Hash: f[0], Subject: termsafe.PlainLine(f[2])}
			if sec, err := strconv.ParseInt(f[1], 10, 64); err == nil {
				cm.At = time.Unix(sec, 0)
			}
			b.Commits = append(b.Commits, cm)
		}
	}
	if ct, err := gitOut(ctx, wt, "log", "-1", "--format=%ct", "HEAD"); err == nil {
		if sec, err := strconv.ParseInt(ct, 10, 64); err == nil {
			b.LastCommit = time.Unix(sec, 0)
		}
	}
	st, err := gitRaw(ctx, wt, "status", "--porcelain", "-z", "--untracked-files=normal") // -z: 名前を引用符で包まない
	if err != nil {
		return b, err
	}
	b.Dirty, b.DirtyFiles = parseStatusZ(st, card.ProgressDirtyFiles)
	return b, nil
}

func (ExecProgressGit) Diff(ctx context.Context, wt, base string, limit int) ([]string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, diffTimeout)
	defer cancel()
	// 🚨 --no-ext-diff / --no-textconv: repo の設定の外の道具を走らせない (読むだけ)。-M: 名前を変えたファイルを消して足したと出さない
	// core.quotePath=false: 日本語の名前を "\346…" と引用させない (板の見出しに名前のまま出す)
	cmd := exec.CommandContext(ctx, "git", "-C", wt, "--no-optional-locks", "-c", "core.quotePath=false", "diff", "--no-color", "--no-ext-diff", "--no-textconv", "-M", "--merge-base", base)
	cmd.WaitDelay = time.Second
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, false, err
	}
	if err := cmd.Start(); err != nil {
		return nil, false, err
	}
	lines, cut, rerr := readDiffLines(out, limit)
	if cut {
		cancel() // 残りは読まない (巨大な差分で git を最後まで走らせない)
	}
	werr := cmd.Wait()
	switch {
	case rerr != nil:
		return nil, false, rerr
	case werr != nil && !cut:
		return nil, false, werr
	}
	return lines, cut, nil
}

// diffLineBytes は差分の 1 行の上限 (これより長い行は切る。生成物・minify したファイルの 1 行で詳細を埋めない)。
const diffLineBytes = 1000

// readDiffLines は r から先頭 limit 行を、1 行ずつ無害化して読む (タブは空白 4 つ)。limit を越える行があれば cut。
// 🚨 1 行は diffLineBytes までしか持たない (minify したファイルの数百 MB の 1 行を丸ごと確保しない。残りは読み捨てる)。
func readDiffLines(r io.Reader, limit int) ([]string, bool, error) {
	br := bufio.NewReaderSize(r, diffLineBytes+1)
	var lines []string
	for {
		l, long, err := readCappedLine(br)
		if l != "" || long || err == nil {
			if len(lines) >= limit {
				return lines, true, nil
			}
			if long {
				l = strings.ToValidUTF8(l, "") + " …"
			} else if strings.HasPrefix(l, "+++ ") || strings.HasPrefix(l, "--- ") {
				l = strings.TrimRight(l, "\t") // git は空白を含む名前の後ろに TAB を付ける (空白 4 つに化けて名前の一部になる)
			}
			lines = append(lines, termsafe.PlainLine(l))
		}
		if err == io.EOF {
			return lines, false, nil
		}
		if err != nil {
			return lines, false, err
		}
	}
}

// readCappedLine は改行までの先頭 diffLineBytes バイトを返す (改行は含めない)。long は切ったか。err は io.EOF で終わり。
func readCappedLine(br *bufio.Reader) (string, bool, error) {
	var b []byte
	long := false
	for {
		frag, err := br.ReadSlice('\n')
		if err == nil {
			frag = frag[:len(frag)-1]
		}
		if room := max(diffLineBytes-len(b), 0); len(frag) > room {
			b, long = append(b, frag[:room]...), true
		} else {
			b = append(b, frag...)
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		return string(b), long, err
	}
}

// parseStatusZ は `git status --porcelain -z` の出力から、変更の数と先頭 limit 個の `XY パス` を返す。
// 名前を変えた・写した変更 (X か Y が R / C) は、元の名前が NUL 区切りでもう 1 つ続く (数えない)。
func parseStatusZ(out string, limit int) (int, []string) {
	var n int
	var files []string
	recs := strings.Split(out, "\x00")
	for i := 0; i < len(recs); i++ {
		r := recs[i]
		if len(r) < 4 {
			continue
		}
		n++
		if len(files) < limit {
			files = append(files, termsafe.PlainLine(r[:2]+" "+r[3:]))
		}
		if r[0] == 'R' || r[0] == 'C' || r[1] == 'R' || r[1] == 'C' { // 作業ツリー側 (git add -N した rename) も元の名前が続く
			i++
		}
	}
	return n, files
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
		if p, diffs, err := d.gatherProgress(ctx, now); err == nil && ctx.Err() == nil {
			keep := d.saveDiffs(&p, diffs)
			if store.SaveProgress(d.Dir, p) == nil { // 書けなくても割り当ては止めない (詳細に進捗が出ないだけ)
				_ = store.PruneDiffs(d.Dir, keep) // 🚨 前の progress.json が指す本文は、新しい progress.json を書いてから消す (消せなくても次の回に消す)
			}
		}
	}()
}

// saveDiffs は集めた差分の本文を store.DiffPath に書き、書けたカードの進捗に置き場を入れる。書けたカードの集まりを返す
// (残す本文。それ以外は progress.json を書いた後に消す)。
// 🚨 本文を先に書いてから progress.json を書く (画面が置き場を読んだときに本文がまだ無い、を作らない)。
func (d *Dispatcher) saveDiffs(p *store.Progress, diffs map[string][]string) map[string]bool {
	keep := map[string]bool{}
	for id, lines := range diffs {
		cp, ok := p.Cards[id]
		if !ok || cp.Diff == nil {
			continue
		}
		if err := store.SaveDiff(d.Dir, id, []byte(strings.Join(lines, "\n"))); err != nil {
			cp.Diff, cp.Err = nil, joinErr(cp.Err, "差分を書けない: "+err.Error())
		} else {
			cp.Diff.Path = store.DiffPath(d.Dir, id)
			keep[id] = true
		}
		p.Cards[id] = cp
	}
	return keep
}

func joinErr(a, b string) string {
	if a == "" {
		return b
	}
	return a + " / " + b
}

// gatherProgress は記録を読み、作業の途中のカードの進捗と差分の本文 (カード ID → 行) を集める。
// 読むのは Dir・Repos・ProgressGit だけ (裏の goroutine から呼ぶ)。
func (d *Dispatcher) gatherProgress(ctx context.Context, now time.Time) (store.Progress, map[string][]string, error) {
	st, err := store.Load(d.Dir)
	if err != nil {
		return store.Progress{}, nil, err
	}
	diffs := map[string][]string{}
	out := store.Progress{At: now, Cards: map[string]card.Progress{}}
	type base struct{ sha, name, err string }
	bases := map[string]base{} // repo → 取り込む先 (1 回に 1 度だけ引く)
	for _, c := range st.Cards {
		repo, ok := d.Repos[c.Repo]
		if !ok || c.Archived || (!inProgress(c.State) && c.State != card.Done) {
			continue
		}
		p := card.Progress{Worktree: card.WorktreePath(repo, c)}
		var errs []string
		dir := repo
		fi, err := os.Stat(p.Worktree)
		p.NoWorktree = err != nil || !fi.IsDir()
		if c.State == card.Done { // 取り込み済み: 場所だけ出す (git は回さない。完了の列は 1 週間ぶん溜まる = 497)
			out.Cards[c.ID] = p
			continue
		}
		if !p.NoWorktree {
			dir = p.Worktree
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
			} else if br, err := d.ProgressGit.Branch(ctx, p.Worktree, b.sha); err != nil {
				errs = append(errs, "worktree の git: "+err.Error())
			} else {
				p.Branch, p.Base, p.Ahead, p.Commits, p.Dirty, p.DirtyFiles, p.LastCommit = br.Name, b.name, br.Ahead, br.Commits, br.Dirty, br.DirtyFiles, br.LastCommit
				if lines, cut, err := d.ProgressGit.Diff(ctx, p.Worktree, b.sha, card.DiffMaxLines); err != nil {
					errs = append(errs, "差分: "+err.Error())
				} else {
					p.Diff = diffSummary(lines, cut)
					diffs[c.ID] = lines
				}
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
		out.Cards[c.ID] = p // worktree の場所は常に出す (無ければ無いと出す。issue 508)
	}
	return out, diffs, nil
}

// diffSummary は差分の本文の数 (ファイル・足した行・消した行)。置き場は書いてから saveDiffs が入れる。
func diffSummary(lines []string, cut bool) *card.DiffSummary {
	ds := &card.DiffSummary{Cut: cut}
	for _, f := range card.SplitDiff(lines) {
		ds.Files++
		ds.Add += f.Add
		ds.Del += f.Del
	}
	return ds
}

// inProgress は進捗を集める列 (PG が作業を始めうる〜取り込み前)。依頼はまだ分けていない、完了は取り込み済み (worktree の場所だけ集める)。
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
