package card

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Progress はカードの作業がどこまで進んだか (issue 469)。dispatcher が集めて store.ProgressFile に書き、画面と `pro-con card show` が読む
// (記録 cards.json には入れない。Doing と同じく、画面が git やファイルを数秒ごとに重く読まない = 441)。
type Progress struct {
	Worktree   string          `json:"worktree,omitempty"`   // PG の worktree の絶対パス (issue 508。無くても置くはずの場所を入れる)
	NoWorktree bool            `json:"noWorktree,omitempty"` // Worktree がまだ無い・片付けた
	Branch     string          `json:"branch,omitempty"`     // worktree の HEAD のブランチ (detached なら空)
	Base       string          `json:"base,omitempty"`       // 突き合わせた取り込む先 (origin/master)。worktree が無ければ空
	Ahead      int             `json:"ahead"`                // 取り込む先より先の commit の本数
	Commits    []Commit        `json:"commits,omitempty"`    // 先の commit (新しい順。上限 ProgressCommits)
	Dirty      int             `json:"dirty"`                // 未 commit の変更 (未追跡を含むファイルの数)
	DirtyFiles []string        `json:"dirtyFiles,omitempty"` // 未 commit の変更の `XY パス` (git status の形。上限 ProgressDirtyFiles)
	LastCommit time.Time       `json:"lastCommit,omitzero"`  // HEAD の commit の時刻
	Diff       *DiffSummary    `json:"diff,omitempty"`       // 取り込む先との差分 (本文は DiffSummary.Path のファイル)
	Issues     []IssueProgress `json:"issues,omitempty"`     // カードの issue の「進捗」節
	Err        string          `json:"err,omitempty"`        // 読めなかったもの (読めた分は出す)
}

// DiffSummary は取り込む先との差分 (merge-base から作業ツリーまで = commit 済みと未 commit の分。issue 508) の数と、本文の置き場。
// 本文は大きいので progress.json には入れず、dispatcher が store.DiffPath に書く (画面は開いたときにだけ読む)。
type DiffSummary struct {
	Path  string `json:"path"`          // 本文 (git diff --no-color の出力を無害化したもの)
	Files int    `json:"files"`         // ファイルの数
	Add   int    `json:"add"`           // 足した行
	Del   int    `json:"del"`           // 消した行
	Cut   bool   `json:"cut,omitempty"` // DiffMaxLines で切った (本文の最後の行が知らせ)
}

// DiffMaxLines は差分の本文の上限の行数 (glogx の diff の板の maxDiffLines と同じ。色付けは開くたびに裏で回す)。
const DiffMaxLines = 5000

// ProgressCommits は詳細に並べる commit の上限 (残りは本数だけ)。
const ProgressCommits = 5

// ProgressDirtyFiles は詳細に並べる未 commit のファイルの上限 (残りは数だけ)。
const ProgressDirtyFiles = 5

type Commit struct {
	Hash    string    `json:"hash"`
	At      time.Time `json:"at,omitzero"` // commit の時刻 (committer)
	Subject string    `json:"subject"`
}

// IssueProgress は issue の本文の「進捗」節 (見出しに「進捗」を含む節)。チェックボックスがあれば済み / 残りを数え、
// 無ければ最後の項目を出す (dotfiles の issue の進捗は日付つきの箇条書きが多い)。
type IssueProgress struct {
	Ref   string   `json:"ref"`            // dotfiles#469
	Done  int      `json:"done"`           // 済み (- [x])
	Total int      `json:"total"`          // チェックボックスの数
	Left  []string `json:"left,omitempty"` // 残りの行 (- [ ] の文)
	Last  string   `json:"last,omitempty"` // チェックボックスが無いときの最後の項目
}

// RunRecord はテストの係が最後に返した結果 (dispatcher が結果を渡すときに記録へ書く。issue 469)。
type RunRecord struct {
	Command string        `json:"command"`
	Cwd     string        `json:"cwd"` // 頼んだ場所 (PG の worktree かその下)
	RC      int           `json:"rc"`
	Took    time.Duration `json:"took"`
	At      time.Time     `json:"at"`            // 終わった時刻
	Err     string        `json:"err,omitempty"` // 実行できなかった・止めた理由 (ログが無いときはこれだけが手がかり)
	Tail    []string      `json:"tail,omitempty"`
	Log     string        `json:"log,omitempty"` // 全体のログのパス
}

// RunTailLines は RunRecord に残す出力の末尾の行数。
const RunTailLines = 3

// ProgressStale を過ぎて集め直されていない進捗は古いと出す (dispatcher は progressEvery ごとに集める)。
const ProgressStale = 3 * time.Minute

// ConflictsStale を過ぎて見直されていない衝突は古いと出す (見張りは既定で 1 分ごとに見る。別のプロセスなので止まっても dispatcher は動く)。
const ConflictsStale = 5 * time.Minute

// RunDir は頼んだ場所を PG の worktree からの相対で書く (repo 全体の make test と src/pro-con の make test を見分ける)。
func RunDir(c Card, cwd string) string {
	marker := string(filepath.Separator) + filepath.Join(".claude", "worktrees", SessionName(c))
	i := strings.Index(cwd, marker)
	if i < 0 {
		return cwd
	}
	rest := strings.TrimPrefix(cwd[i+len(marker):], string(filepath.Separator))
	if rest == "" {
		return "worktree の直下"
	}
	return rest
}

// ProgressLines は詳細 (画面の引き出しと card show) に出す「進捗」の行。見出しは ProgressHead。何も無ければ nil。
// worktree の場所・commit・未 commit は WorktreeLines (別の節)。
func (c Card) ProgressLines(now time.Time, dur func(time.Duration) string) []string {
	var out []string
	if p := c.Progress; p != nil {
		for _, ip := range p.Issues {
			switch {
			case ip.Total > 0:
				out = append(out, fmt.Sprintf("issue %s の進捗: 済み %d / 残り %d", ip.Ref, ip.Done, ip.Total-ip.Done))
				for _, l := range ip.Left {
					out = append(out, "  [ ] "+l)
				}
			case ip.Last != "":
				out = append(out, "issue "+ip.Ref+" の進捗の最後の記録: "+ip.Last)
			default:
				out = append(out, "issue "+ip.Ref+" の本文に進捗の記録はまだ無い")
			}
		}
		if p.Err != "" {
			out = append(out, "読めなかったもの: "+p.Err)
		}
	}
	if r := c.LastRun; r != nil {
		where := RunDir(c, r.Cwd)
		if where == "" {
			where = "(頼んだ場所が記録に無い)"
		}
		out = append(out, fmt.Sprintf("テスト: 最後の結果 rc=%d (所要 %s・%s前) %s で `%s`", r.RC, dur(r.Took), dur(now.Sub(r.At)), where, r.Command))
		if r.Err != "" {
			out = append(out, "  "+r.Err)
		}
		for _, l := range r.Tail {
			out = append(out, "  "+l)
		}
	}
	seen := "見張りが " + dur(now.Sub(c.ConflictsAt)) + "前に見た"
	if now.Sub(c.ConflictsAt) > ConflictsStale {
		seen = "見張りが " + dur(now.Sub(c.ConflictsAt)) + "前に見たまま。見張りが止まっている?"
	}
	for _, n := range c.Conflicts {
		out = append(out, "取り込み: "+n+" ("+seen+")")
	}
	if !c.ConflictsAt.IsZero() && len(c.Conflicts) == 0 && c.Progress != nil && c.Progress.Ahead > 0 {
		out = append(out, "取り込み: 衝突は見えていない ("+seen+"。commit 済みの分だけ)")
	}
	return out
}

// ProgressHead は「進捗」の見出し (集めた時刻が古ければそう言う)。
func ProgressHead(c Card, now time.Time, dur func(time.Duration) string) string {
	return "進捗" + progressStale(c, now, dur)
}

// WorktreeHead は「worktree」の見出し (進捗と同じ回に集めるので、古さも同じに言う)。
func WorktreeHead(c Card, now time.Time, dur func(time.Duration) string) string {
	return "worktree" + progressStale(c, now, dur)
}

func progressStale(c Card, now time.Time, dur func(time.Duration) string) string {
	if !c.ProgressAt.IsZero() && now.Sub(c.ProgressAt) > ProgressStale {
		return " (" + dur(now.Sub(c.ProgressAt)) + "前に集めたまま。dispatcher が止まっている?)"
	}
	return ""
}

// Paint は WorktreeLines の色 (画面は SGR を渡し、card show は zero で素の文字にする)。
type Paint struct {
	Hash, Dim, Staged, Unstaged, Reset string
}

func (pt Paint) wrap(style, s string) string {
	if style == "" {
		return s
	}
	return style + s + pt.Reset
}

// WorktreeLines は「worktree」の節の行 (issue 508): 絶対パス (コピーしてそのまま cd できるよう 1 行に単独で置く)・ブランチ・
// 取り込む先より先の commit (新しい順・相対の時刻)・未 commit のファイル・差分の数。diffHint は差分の行の後ろに足す文 (開き方)。
// 集めていなければ nil。
func (c Card) WorktreeLines(now time.Time, dur func(time.Duration) string, pt Paint, diffHint string) []string {
	p := c.Progress
	if p == nil || p.Worktree == "" {
		return nil
	}
	out := []string{p.Worktree}
	if p.NoWorktree {
		switch {
		case c.State == Done:
			out = append(out, pt.wrap(pt.Dim, "片付けた (取り込みの後に消した)"))
		case c.State == Planned && c.Session == "":
			out = append(out, pt.wrap(pt.Dim, "まだ無い (PG をまだ起動していない)"))
		default:
			out = append(out, pt.wrap(pt.Dim, "見つからない (PG が作る前か、消された)"))
		}
		return out
	}
	if p.Base == "" { // Done (場所だけ集める) か、git を読めなかった (理由は進捗の「読めなかったもの」)
		return out
	}
	branch := p.Branch
	if branch == "" {
		branch = "(ブランチに居ない: detached HEAD)"
	}
	out = append(out, "ブランチ "+branch)
	line := fmt.Sprintf("commit: %s より %d 本先", p.Base, p.Ahead)
	if !p.LastCommit.IsZero() {
		line += " (最後の commit " + dur(now.Sub(p.LastCommit)) + "前)"
	}
	out = append(out, line)
	for _, cm := range p.Commits {
		l := "  " + pt.wrap(pt.Hash, cm.Hash)
		if !cm.At.IsZero() {
			l += " " + pt.wrap(pt.Dim, dur(now.Sub(cm.At))+"前")
		}
		out = append(out, l+" "+cm.Subject)
	}
	if n := p.Ahead - len(p.Commits); n > 0 && len(p.Commits) > 0 {
		out = append(out, pt.wrap(pt.Dim, fmt.Sprintf("  ほか %d 本", n)))
	}
	out = append(out, fmt.Sprintf("未 commit の変更: %d ファイル", p.Dirty))
	for _, f := range p.DirtyFiles {
		out = append(out, "  "+pt.statusCode(f))
	}
	if n := p.Dirty - len(p.DirtyFiles); n > 0 && len(p.DirtyFiles) > 0 {
		out = append(out, pt.wrap(pt.Dim, fmt.Sprintf("  ほか %d ファイル", n)))
	}
	if d := p.Diff; d != nil {
		l := fmt.Sprintf("差分: %d ファイル +%d -%d", d.Files, d.Add, d.Del)
		if d.Cut {
			l += fmt.Sprintf(" (%d 行で切った)", DiffMaxLines)
		}
		out = append(out, l+diffHint)
	}
	return out
}

// statusCode は `XY パス` の XY に色を付ける (index に入った変更 = Staged、作業ツリーだけ = Unstaged、未追跡 = Dim)。
func (pt Paint) statusCode(f string) string {
	if len(f) < 3 {
		return f
	}
	xy, rest := f[:2], f[2:]
	switch {
	case xy == "??":
		return pt.wrap(pt.Dim, xy) + rest
	case xy[0] != ' ':
		return pt.wrap(pt.Staged, xy) + rest
	}
	return pt.wrap(pt.Unstaged, xy) + rest
}

// DiffFile は差分の本文の 1 ファイル分 (lines[Start:End]。Start は `diff --git` の行)。
type DiffFile struct {
	Path       string
	Start, End int
	Add, Del   int
}

// SplitDiff は `git diff` の本文をファイルごとに分ける (最初の `diff --git` より前の行は、どのファイルにも入れない)。
// hunk の中の `+++` / `---` で始まる行は、ヘッダではなく足した / 消した行として数える。
func SplitDiff(lines []string) []DiffFile {
	var out []DiffFile
	inHunk := false
	for i, l := range lines {
		if strings.HasPrefix(l, "diff --git ") {
			if n := len(out); n > 0 {
				out[n-1].End = i
			}
			out = append(out, DiffFile{Path: diffPath(l), Start: i, End: len(lines)})
			inHunk = false
			continue
		}
		if len(out) == 0 {
			continue
		}
		f := &out[len(out)-1]
		switch {
		case strings.HasPrefix(l, "@@"):
			inHunk = true
		case !inHunk && strings.HasPrefix(l, "+++ ") && l != "+++ /dev/null":
			f.Path = strings.TrimPrefix(strings.TrimPrefix(l, "+++ "), "b/") // 空白を含む名前も、ここは引用されずに正しく取れる
		case !inHunk:
		case strings.HasPrefix(l, "+"):
			f.Add++
		case strings.HasPrefix(l, "-"):
			f.Del++
		}
	}
	return out
}

// diffPath は `diff --git a/x b/x` の b 側の名前 (+++ の行が無いファイル = バイナリ・mode だけの変更・消したファイル の名前)。
func diffPath(l string) string {
	rest := strings.TrimPrefix(l, "diff --git ")
	if i := strings.LastIndex(rest, " b/"); i >= 0 {
		return rest[i+3:]
	}
	return rest
}

// Label は PG の待ちの短い名前 (card list の「待ち」)。待ちが無ければ空。
func (w Wait) Label() string {
	switch w.Kind {
	case WaitQuestion:
		return "質問"
	case WaitPermission:
		return "権限の確認"
	case WaitResource:
		return fmt.Sprintf("%s の順番待ち (%d 番目)", w.Resource, w.Position)
	case WaitQuota:
		return "利用枠の回復待ち"
	case WaitCrashed:
		return "落ち続けたので止めた"
	case WaitNone:
	}
	return ""
}

// WaitingOn は今そのカードが何を待っているか (詳細の「今の待ち」。issue 469)。誰の番かは Turn (452) を読み、ここで別に判じない。
// cards は順番 (468) の前のカードを引く記録の全カード。完了・片付けたカードは空。
func (c Card) WaitingOn(cards []Card, r Roles) string {
	if c.Deleting() {
		return "削除 (PG の session を止めてから消える)"
	}
	switch t := c.Turn(r); t {
	case TurnNone:
		return ""
	case TurnHuman:
		return "人の番: " + c.humanWhy(r)
	case TurnPM:
		if c.State == Requested {
			return "PM が依頼を分ける"
		}
		return "PM が質問に答えるか人に回す"
	case TurnIntegrator:
		return "取り込みの係のレビュー"
	case TurnPG:
	}
	w := c.pgWait(cards)
	if c.Stalled && c.State == Running {
		w += " (watchdog が停滞と判定した)"
	}
	return w
}

// pgWait は PG の番のカードの待ち。
func (c Card) pgWait(cards []Card) string {
	switch {
	case c.Launching != "":
		return "dispatcher が PG の" + c.Launching + "を確かめている"
	case c.State == Planned && len(HeldBy(cards, c)) > 0:
		return strings.Join(HeldBy(cards, c), ", ") + " の後 (順番。前のカードが完了するまで起動しない)"
	case c.State == Planned && c.Resumes():
		return "PG の空き (同じ session の再開。新しい起動より先)"
	case c.State == Planned:
		return "PG の空き"
	case c.Exec.Active():
		return "テストの係の実行 (結果が届いたら PG を再開する)"
	case c.Run != "" && c.Wait.Kind == WaitResource:
		return "テストの係に頼んだコマンドの順番: " + c.Wait.Label()
	case c.Run != "":
		return "テストの係が始めるのを待っている"
	case c.Wait.Kind == WaitQuota:
		return "利用枠の回復"
	}
	return "PG の turn の途中"
}

func (c Card) humanWhy(r Roles) string {
	switch {
	case c.State == Requested:
		return "依頼を分ける (PM を起こさない設定)"
	case c.State == Review:
		if c.HandedOff() {
			return "レビュー (取り込みの係が人に回した)"
		}
		return "レビュー (取り込みの係を起こさない設定)"
	case c.Wait.Kind == WaitPermission:
		return "権限の確認に答える (a で attach して答える。r の回答では答えられない)"
	case c.Wait.Kind == WaitCrashed:
		return "落ち続けたので止めた PG (r で回答すると同じ session を再開する)"
	case c.Wait.FromPM():
		return "PM の質問に答える (r で回答すると依頼の列へ戻り、PM が続きを分ける)"
	case c.HandedOff():
		return "質問に答える (PM が人に回した)"
	case r.PMOff:
		return "質問に答える (PM を起こさない設定)"
	}
	return "回答する"
}
