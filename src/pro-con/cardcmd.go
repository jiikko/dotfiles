package main

// pro-con card — PM / PG が使うカードの操作の口 (issue 427 の段階 3b)。受付の箱に依頼を置くだけで、記録への適用は dispatcher (store.Apply)。
// 置いた依頼の ID を stdout に出す (add は適用を待ってカード ID を出す)。使い方の誤りは rc=2、箱に置けなかったら rc=1。
// 読むだけの口 (list / show / wait) は cardview.go。

import (
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"pro-con/card"
	"pro-con/store"
)

const cardUsage = `usage: pro-con card <操作> ...   (受付の箱に依頼を置く。適用は dispatcher)
  add --title <題名> [--request <依頼の原文>] [--repo <repo>] [--prompt <PM に渡した指示>] [--wait <長さ>]
                                                 適用を待ってカード ID を出す (既定 10s。待てなければ依頼 ID を出して rc=3。0 なら待たずに依頼 ID)
  plan <カード> [--issue <repo>#<番号>]...      タスクに分けてキューに積んだ
  ask <カード> <質問>                            PG が質問して turn を終える (AskUserQuestion は使わない)
  answer <カード> <回答> [--from <人間|PM>]      質問待ちのカードへの回答
  run <カード> -- <コマンド>...                  PG がテストの係にコマンドの実行を頼んで turn を終える (結果は再開のときに届く)
  review <カード>                                PG が終えた
  rework <カード> <直してほしい点>               レビュー待ちのカードを PG に差し戻す (同じ session を再開する)
  handoff <カード> <理由> [--from <PM|人間>]      PG の質問を人に回したことを履歴に残す (列は変えない)
  close <カード> [--ending answered|investigated|rejected|pending-issue] [--issue <repo>#<番号>]...
  delete <カード> [--from <人間|PM>]              カードを消す (依頼の列はすぐ。それ以外は PG の session を止めてから。worktree とブランチは残す)
  guide                                          PM への指示書を出す (箱には何も置かない)
読むだけ (箱にも記録にも書かない):
  list [--state <列>] [--all] [--json]           カードの一覧 (--all は片付けたものも)
  show <カード> [--json]                         依頼の原文・履歴・質問・PG の出力の末尾 (画面の詳細と同じ中身)
  wait <カード> [--until <列>] [--timeout <長さ>] [--json]
                                                 列が変わる (--until ならその列に居る) まで待つ (既定 10m。時間切れは rc=1)`

// pmGuide は PM の session に渡す指示書。書いてあるコマンドは TestPMGuideCommandsParse がパーサに通して、ずれを止める。
//
//go:embed pm-guide.md
var pmGuide string

// addWait は add が適用を待つ既定の長さ (dispatcher が動いていれば 1 秒かからない。427 の実測で約 130 ms)。
const addWait = 10 * time.Second

// runCard は pro-con card の本体。env.dir は本物のモードの状態の置き場 (store の dir)。
func runCard(args []string, env viewEnv, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, cardUsage)
		return 2
	}
	switch args[0] {
	case "guide":
		_, _ = fmt.Fprint(stdout, pmGuide)
		return 0
	case "list":
		return runCardList(args[1:], env, stdout, stderr)
	case "show":
		return runCardShow(args[1:], env, stdout, stderr)
	case "wait":
		return runCardWait(args[1:], env, stdout, stderr)
	}
	req, wait, err := parseCardWait(args)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "pro-con card: %v\n%s\n", err, cardUsage)
		return 2
	}
	if req.Kind == "add" {
		return addAndWait(env.dir, req, wait, stdout, stderr)
	}
	dir := env.dir
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

// parseCardWait は引数を依頼と、add の --wait (適用を待つ長さ。既定 addWait、0 なら待たない) にする。操作ごとに要る引数が無ければエラー
// (箱に置く前に止める。dispatcher で除けられるより早く気づける)。
func parseCardWait(args []string) (store.Request, time.Duration, error) {
	r, wait, err := parseCardArgs(args)
	if err == nil && wait < 0 {
		err = fmt.Errorf("--wait は 0 以上の長さ: %v", wait)
	}
	return r, wait, err
}

func parseCardArgs(args []string) (store.Request, time.Duration, error) {
	wait := addWait
	op, rest := args[0], args[1:]
	if op == "run" { // pro-con card run C-001 -- make test (-- の後ろは argv。フラグとして読まない)
		i := slices.Index(rest, "--")
		if i != 1 || len(rest) < 3 {
			return store.Request{}, wait, errors.New("run は `run <カード> -- <コマンド>...`")
		}
		cwd, _ := os.Getwd() // dispatcher が、頼んだのがそのカードの PG の worktree かを照らす
		return store.Request{Kind: "run", CardID: rest[0], Command: shellJoin(rest[2:]), Cwd: cwd}, wait, nil
	}
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
		fs.DurationVar(&wait, "wait", addWait, "")
	case "plan":
		fs.Var(&issues, "issue", "")
	case "answer", "delete":
		fs.StringVar(&r.From, "from", "人間", "")
	case "handoff":
		fs.StringVar(&r.From, "from", "PM", "")
	case "close":
		fs.Var(&issues, "issue", "")
		fs.StringVar(&ending, "ending", "", "")
	case "ask", "review", "rework":
	default:
		return r, wait, fmt.Errorf("未知の操作 %q", op)
	}
	// カードの ID と本文 (ask / answer / rework) はフラグの前に置く (pro-con card ask C-001 "質問")。flag はフラグの後の位置引数しか残さないので先に取る
	var pos []string
	for len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		pos, rest = append(pos, rest[0]), rest[1:]
	}
	if err := fs.Parse(rest); err != nil {
		return r, wait, err
	}
	pos = append(pos, fs.Args()...)
	need := map[string]int{"add": 0, "plan": 1, "review": 1, "close": 1, "delete": 1, "ask": 2, "answer": 2, "rework": 2, "handoff": 2}[op]
	if len(pos) != need {
		return r, wait, fmt.Errorf("%s は位置引数が %d 個 (受け取ったのは %d 個)", op, need, len(pos))
	}
	if need >= 1 {
		r.CardID = pos[0]
	}
	switch op {
	case "add":
		if strings.TrimSpace(r.Title) == "" && strings.TrimSpace(r.Request) == "" {
			return r, wait, errors.New("add には --title か --request が要る")
		}
	case "ask":
		r.Question = pos[1]
	case "answer":
		r.Answer = pos[1]
	case "rework":
		r.Rework = pos[1]
	case "handoff":
		r.Text = pos[1]
	case "close":
		if ending != "" {
			e, ok := endings[ending]
			if !ok {
				return r, wait, fmt.Errorf("--ending は answered / investigated / rejected / pending-issue のどれか: %q", ending)
			}
			r.Ending = e
		}
	}
	r.Issues = issues
	return r, wait, nil
}

// liveDir は本物のモードの状態の置き場 (store と、pro-con が起動した session の記録の置き場)。
func liveDir(home string) string { return filepath.Join(stateDir(home), "live") }
