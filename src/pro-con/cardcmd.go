package main

// pro-con card — PM / PG が使うカードの操作の口 (issue 427 の段階 3b)。受付の箱に依頼を置くだけで、記録への適用は daemon (store.Apply)。
// 置いた依頼の ID を stdout に出す。使い方の誤りは rc=2、箱に置けなかったら rc=1。

import (
	_ "embed"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"pro-con/card"
	"pro-con/store"
)

const cardUsage = `usage: pro-con card <操作> ...   (受付の箱に依頼を置く。適用は daemon)
  add --title <題名> [--request <依頼の原文>] [--repo <repo>] [--prompt <PM に渡した指示>]
  plan <カード> [--issue <repo>#<番号>]...      タスクに分けてキューに積んだ
  ask <カード> <質問>                            PG が質問して turn を終える (AskUserQuestion は使わない)
  answer <カード> <回答> [--from <人間|PM>]      質問待ちのカードへの回答
  review <カード>                                PG が終えた
  close <カード> [--ending answered|investigated|rejected|pending-issue] [--issue <repo>#<番号>]...
  guide                                          PM への指示書を出す (箱には何も置かない)`

// pmGuide は PM の session に渡す指示書。書いてあるコマンドは TestPMGuideCommandsParse がパーサに通して、ずれを止める。
//
//go:embed pm-guide.md
var pmGuide string

// runCard は pro-con card の本体。dir は本物のモードの状態の置き場 (store の dir)。
func runCard(args []string, dir string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, cardUsage)
		return 2
	}
	if args[0] == "guide" {
		_, _ = fmt.Fprint(stdout, pmGuide)
		return 0
	}
	req, err := parseCard(args)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "pro-con card: %v\n%s\n", err, cardUsage)
		return 2
	}
	id, err := store.Submit(dir, req)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con card: 受付の箱に置けない:", err)
		return 1
	}
	_, _ = fmt.Fprintln(stdout, id)
	return 0
}

// issueList は --issue <repo>#<番号> を複数受ける。
type issueList []card.IssueRef

func (l *issueList) String() string { return fmt.Sprint(*l) }
func (l *issueList) Set(v string) error {
	repo, num, ok := strings.Cut(v, "#")
	n, err := strconv.Atoi(num)
	if !ok || repo == "" || err != nil || n <= 0 {
		return fmt.Errorf("--issue は <repo>#<番号> (例 dotfiles#415): %q", v)
	}
	*l = append(*l, card.IssueRef{Repo: repo, Number: n, Status: "open"})
	return nil
}

var endings = map[string]card.Ending{
	"answered": card.EndAnswered, "investigated": card.EndResearchOnly, "rejected": card.EndRejected, "pending-issue": card.EndPendingIssue,
}

// parseCard は引数を依頼にする。操作ごとに要る引数が無ければエラー (箱に置く前に止める。daemon で除けられるより早く気づける)。
func parseCard(args []string) (store.Request, error) {
	op, rest := args[0], args[1:]
	fs := flag.NewFlagSet("pro-con card "+op, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var r store.Request
	r.Kind = op
	var issues issueList
	var ending string
	switch op {
	case "add":
		fs.StringVar(&r.Title, "title", "", "")
		fs.StringVar(&r.Request, "request", "", "")
		fs.StringVar(&r.Repo, "repo", "", "")
		fs.StringVar(&r.Prompt, "prompt", "", "")
	case "plan":
		fs.Var(&issues, "issue", "")
	case "answer":
		fs.StringVar(&r.From, "from", "人間", "")
	case "close":
		fs.Var(&issues, "issue", "")
		fs.StringVar(&ending, "ending", "", "")
	case "ask", "review":
	default:
		return r, fmt.Errorf("未知の操作 %q", op)
	}
	// カードの ID と本文 (ask / answer) はフラグの前に置く (pro-con card ask C-001 "質問")。flag はフラグの後の位置引数しか残さないので先に取る
	var pos []string
	for len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		pos, rest = append(pos, rest[0]), rest[1:]
	}
	if err := fs.Parse(rest); err != nil {
		return r, err
	}
	pos = append(pos, fs.Args()...)
	need := map[string]int{"add": 0, "plan": 1, "review": 1, "close": 1, "ask": 2, "answer": 2}[op]
	if len(pos) != need {
		return r, fmt.Errorf("%s は位置引数が %d 個 (受け取ったのは %d 個)", op, need, len(pos))
	}
	if need >= 1 {
		r.CardID = pos[0]
	}
	switch op {
	case "add":
		if strings.TrimSpace(r.Title) == "" && strings.TrimSpace(r.Request) == "" {
			return r, fmt.Errorf("add には --title か --request が要る")
		}
	case "ask":
		r.Question = pos[1]
	case "answer":
		r.Answer = pos[1]
	case "close":
		if ending != "" {
			e, ok := endings[ending]
			if !ok {
				return r, fmt.Errorf("--ending は answered / investigated / rejected / pending-issue のどれか: %q", ending)
			}
			r.Ending = e
		}
	}
	r.Issues = issues
	return r, nil
}

// liveDir は本物のモードの状態の置き場 (store と、pro-con が起動した session の記録の置き場)。
func liveDir(home string) string { return filepath.Join(stateDir(home), "live") }
