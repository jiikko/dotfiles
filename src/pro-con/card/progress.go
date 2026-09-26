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
	Base       string          `json:"base,omitempty"`      // 突き合わせた取り込む先 (origin/master)。worktree が無ければ空
	Ahead      int             `json:"ahead"`               // 取り込む先より先の commit の本数
	Commits    []Commit        `json:"commits,omitempty"`   // 先の commit (新しい順。上限 ProgressCommits)
	Dirty      int             `json:"dirty"`               // 未 commit の変更 (未追跡を含むファイルの数)
	LastCommit time.Time       `json:"lastCommit,omitzero"` // HEAD の commit の時刻
	Issues     []IssueProgress `json:"issues,omitempty"`    // カードの issue の「進捗」節
	Err        string          `json:"err,omitempty"`       // 読めなかったもの (読めた分は出す)
}

// ProgressCommits は詳細に並べる commit の上限 (残りは本数だけ)。
const ProgressCommits = 5

type Commit struct {
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
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
func (c Card) ProgressLines(now time.Time, dur func(time.Duration) string) []string {
	var out []string
	if p := c.Progress; p != nil {
		if p.Base != "" {
			line := fmt.Sprintf("commit: %s より %d 本先", p.Base, p.Ahead)
			if !p.LastCommit.IsZero() {
				line += " (最後の commit " + dur(now.Sub(p.LastCommit)) + "前)"
			}
			out = append(out, line)
			for _, cm := range p.Commits {
				out = append(out, "  "+cm.Hash+" "+cm.Subject)
			}
			if n := p.Ahead - len(p.Commits); n > 0 && len(p.Commits) > 0 {
				out = append(out, fmt.Sprintf("  ほか %d 本", n))
			}
			out = append(out, fmt.Sprintf("未 commit の変更: %d ファイル", p.Dirty))
		}
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
	if !c.ProgressAt.IsZero() && now.Sub(c.ProgressAt) > ProgressStale {
		return "進捗 (" + dur(now.Sub(c.ProgressAt)) + "前に集めたまま。dispatcher が止まっている?)"
	}
	return "進捗"
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
	case c.HandedOff():
		return "質問に答える (PM が人に回した)"
	case r.PMOff:
		return "質問に答える (PM を起こさない設定)"
	}
	return "回答する"
}
