package main

// pro-con card — PM / PG が使うカードの操作の口 (issue 427 の段階 3b)。受付の箱に依頼を置くだけで、記録への適用は dispatcher (store.Apply)。
// 置いた依頼の ID を stdout に出す (add は適用を待ってカード ID を出す)。使い方の誤りは rc=2、箱に置けなかったら rc=1。
// 読むだけの口 (list / show / wait) は cardview.go、log は cardlog.go。

import (
	_ "embed"
	"encoding/json"
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

var cardUsage = `usage: pro-con card <操作> ...   (受付の箱に依頼を置く。適用は dispatcher)
  add --title <題名> [--request <依頼の原文>] [--repo <repo>] [--prompt <PM に渡した指示>] [--wait <長さ>]
                                                 適用を待ってカード ID を出す (既定 10s。待てなければ依頼 ID を出して rc=3。0 なら待たずに依頼 ID)
  plan <カード> [--issue <repo>#<番号>]... [--after <カード>]... [--points 1|2|3|5|8]
                                                 タスクに分けてキューに積んだ (--after のカードが完了するまで起動しない。--points は見積もり)
` + card.PointsMeaningText("                                                   ") + `
  ask <カード> <質問>                            PG が質問して turn を終える (AskUserQuestion は使わない)。
                                                 依頼の列のカードなら PM が人に聞く (人の番。回答で依頼の列へ戻る)
  ask <カード> [<前置き>] --json '{"questions":[...]}'
                                                 選択肢つきの質問。形は AskUserQuestion と同じ (問い 1〜4 個・選択肢 2〜4 個。
                                                 options に "recommended":true で推奨)。画面は radio / checkbox の回答フォームにする
  answer <カード> <回答> [--from <人間|PM>]      質問待ちのカードへの回答
  run <カード> -- <コマンド>...                  PG がテストの係にコマンドの実行を頼んで turn を終える (結果は再開のときに届く)
  attach <カード> <ファイル> [--note <一言>]      PG が作業の証拠 (画面の見た目・コマンドの出力) をカードに添付する
                                                 (.png などは画像、.txt / .ans は文字。1 件 20 MiB・1 枚に 50 件まで)
  review <カード>                                PG が終えた
  rework <カード> <直してほしい点>               レビュー待ちのカードを PG に差し戻す (同じ session を再開する)
  order <カード> <本文> [--redirect] [--from <人間|PM>]
                                                 追加オーダー (画面の + と同じ)。既定は追記 (PG の turn の区切りで届く)、
                                                 --redirect は方針変更 (PG を止めて届ける)。完了のカードには出せない (別件は add で新しい依頼にする)
  handoff <カード> <理由> [--from <PM|取り込みの係>]  PG の質問 / レビュー待ちを人に回したことを履歴に残す (列は変えない)
  close <カード> [--ending answered|investigated|rejected|pending-issue] [--issue <repo>#<番号>]...
  delete <カード> [--from <人間|PM>]              カードを消す (依頼の列はすぐ。それ以外は PG の session を止めてから。worktree とブランチは残す)
  move <カード> up|down [--repo <repo>]          レーンの中で 1 つ上 / 下のカードと入れ替える (上ほど優先。--repo ならその repo のカードの中の隣)
  guide                                          PM への指示書を出す (箱には何も置かない)
読むだけ (箱にも記録にも書かない):
  list [--state <列>] [--all] [--json]           カードの一覧 (--all は片付けたものも)
  show <カード> [--json]                         依頼の原文・履歴・質問・PG の出力の末尾 (画面の詳細と同じ中身)
  wait <カード> [--until <列>] [--timeout <長さ>] [--json]
                                                 列が変わる (--until ならその列に居る) まで待つ (既定 10m。時間切れは rc=1)
  log <カード> [--follow] [--json]               PG の活動 (応答の文と道具の呼び出し) を時刻の順に。再開で入れ替わった前の session から続けて出す`

// pmGuide は PM の session に渡す指示書。書いてあるコマンドは TestPMGuideCommandsParse がパーサに通して、ずれを止める。
//
//go:embed pm-guide.md
var pmGuideSrc string

// pmGuide は pm-guide.md の目印の行 ({{ポイントの目安}}) を、ポイントの意味の正本 (card.PointsMeaning) で置き換えたもの。
var pmGuide = strings.Replace(pmGuideSrc, pointsMark, card.PointsMeaningText("     - "), 1)

// pointsMark は pm-guide.md でポイントの意味を差し込む行 (インデントも含めて行ごと置き換える)。
const pointsMark = "     - {{ポイントの目安}}"

// integratorGuide は取り込みの係 (487) の session に渡す指示書。書いてあるコマンドは TestIntegratorGuideCommandsParse がパーサに通す。
//
//go:embed integrator-guide.md
var integratorGuide string

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
		switch {
		case len(args) == 1:
			_, _ = fmt.Fprint(stdout, pmGuide)
		case len(args) == 2 && args[1] == "--integrator":
			_, _ = fmt.Fprint(stdout, integratorGuide)
		default:
			_, _ = fmt.Fprintln(stderr, "usage: pro-con card guide [--integrator]")
			return 2
		}
		return 0
	case "list":
		return runCardList(args[1:], env, stdout, stderr)
	case "show":
		return runCardShow(args[1:], env, stdout, stderr)
	case "wait":
		return runCardWait(args[1:], env, stdout, stderr)
	case "log":
		return runCardLog(args[1:], env, stdout, stderr)
	}
	if args[0] == "attach" {
		return runCardAttach(args[1:], env.dir, stdout, stderr)
	}
	req, wait, err := parseCardWait(args)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "pro-con card: %v\n%s\n", err, cardUsage)
		return 2
	}
	if err := checkCardRepos(req, env.repos); err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con card:", err)
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

// checkCardRepos は add の --repo と plan の --issue の repo が設定の repo かを、受付の箱に置く前に見る (issue 511。正本は store.CheckRepo で、
// 箱に手で置かれた依頼は dispatcher の適用が同じ検査で除ける)。repos が nil なら見ない (設定を持たない呼び手)。設定は repo を書いた依頼のときだけ読む。
func checkCardRepos(r store.Request, repos func() (map[string]string, error)) error {
	var names []string
	switch r.Kind {
	case "add":
		names = []string{r.Repo}
	case "plan":
		for _, i := range r.Issues {
			names = append(names, i.Repo)
		}
	}
	if repos == nil || !slices.ContainsFunc(names, func(n string) bool { return n != "" }) {
		return nil
	}
	known, err := repos()
	if err != nil {
		return err
	}
	for _, n := range names {
		if err := store.CheckRepo(known, n); err != nil {
			return err
		}
	}
	return nil
}

// runCardAttach はファイルを受付の箱に写して添付の依頼を置く (移して記録に載せるのは dispatcher。issue 453)。
func runCardAttach(args []string, dir string, stdout, stderr io.Writer) int {
	id, file, note, err := parseAttach(args)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "pro-con card: %v\n%s\n", err, cardUsage)
		return 2
	}
	reqID, err := store.SubmitAttachment(dir, id, file, note)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con card attach: 受付の箱に置けない:", err)
		return 1
	}
	_, _ = fmt.Fprintln(stdout, reqID)
	return 0
}

func parseAttach(args []string) (id, file, note string, err error) {
	fs := flag.NewFlagSet("pro-con card attach", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&note, "note", "", "")
	var pos []string // カードとファイルはフラグの前にも後にも置ける (parseCardArgs と同じ)
	for len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		pos, args = append(pos, args[0]), args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return "", "", "", err
	}
	pos = append(pos, fs.Args()...)
	if len(pos) != 2 {
		return "", "", "", fmt.Errorf("attach は `attach <カード> <ファイル> [--note <一言>]` (位置引数は 2 個。受け取ったのは %d 個)", len(pos))
	}
	return pos[0], pos[1], note, nil
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

// afterList は --after <カード> を複数受ける (issue 468)。
type afterList []string

func (l *afterList) String() string { return strings.Join(*l, ",") }
func (l *afterList) Set(v string) error {
	if strings.TrimSpace(v) == "" {
		return errors.New("--after はカードの ID (例 C-001)")
	}
	*l = append(*l, strings.TrimSpace(v))
	return nil
}

// pointsFlag は --points <1|2|3|5|8> (見積もり。issue 490)。付けなければ 0 = 見積もり無し。0 を含むほかの値は使い方の誤り。
type pointsFlag int

func (p *pointsFlag) String() string { return strconv.Itoa(int(*p)) }
func (p *pointsFlag) Set(v string) error {
	n, err := strconv.Atoi(v)
	if err != nil || n == 0 || card.CheckPoints(n) != nil {
		return fmt.Errorf("--points は %v のどれか: %q", card.PointScale, v)
	}
	*p = pointsFlag(n)
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
	var ending, askJSON string
	var redirect bool
	switch op {
	case "add":
		fs.StringVar(&r.Title, "title", "", "")
		fs.StringVar(&r.Request, "request", "", "")
		fs.StringVar(&r.Repo, "repo", "", "")
		fs.StringVar(&r.Prompt, "prompt", "", "")
		fs.DurationVar(&wait, "wait", addWait, "")
	case "plan":
		fs.Var(&issues, "issue", "")
		fs.Var((*afterList)(&r.After), "after", "")
		fs.Var((*pointsFlag)(&r.Points), "points", "")
	case "answer", "delete":
		fs.StringVar(&r.From, "from", "人間", "")
	case "order":
		fs.BoolVar(&redirect, "redirect", false, "")
		fs.StringVar(&r.From, "from", "人間", "")
	case "handoff":
		fs.StringVar(&r.From, "from", "PM", "")
	case "close":
		fs.Var(&issues, "issue", "")
		fs.StringVar(&ending, "ending", "", "")
	case "move":
		fs.StringVar(&r.Repo, "repo", "", "")
	case "ask":
		fs.StringVar(&askJSON, "json", "", "")
	case "review", "rework":
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
	need := map[string]int{"add": 0, "plan": 1, "review": 1, "close": 1, "delete": 1, "ask": 2, "answer": 2, "rework": 2, "order": 2, "move": 2, "handoff": 2}[op]
	if op == "ask" && askJSON != "" && len(pos) == 1 { // 選択肢つきなら前置きは省ける
		pos = append(pos, "")
	}
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
		if askJSON != "" {
			qs, err := parseAskJSON(askJSON)
			if err != nil {
				return r, wait, err
			}
			r.Questions = qs
		}
	case "answer":
		r.Answer = pos[1]
	case "rework":
		r.Rework = pos[1]
	case "order": // 断る条件 (完了のカード・空の本文) は store の適用が正本。ここで弾くのは使い方の誤りだけ
		r.Text = pos[1]
		if redirect {
			r.Order = card.OrderRedirect // 付けなければ零値の追記 (card.OrderAppend)
		}
	case "move":
		d, ok := map[string]int{"up": -1, "down": 1}[pos[1]]
		if !ok {
			return r, wait, fmt.Errorf("move の向きは up か down: %q", pos[1])
		}
		r.Delta = d
	case "handoff":
		r.Text = pos[1]
		if redirect {
			r.Order = card.OrderRedirect // 付けなければ零値の追記 (card.OrderAppend)
		}
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

// parseAskJSON は ask --json の問い (AskUserQuestion の入力と同じ {"questions":[...]}) を読んで検査する。
// 知らない項目 (AskUserQuestion の annotations 等) は無視する: PG が AskUserQuestion の形を写しても通す。
func parseAskJSON(s string) ([]card.Question, error) {
	var in struct {
		Questions []card.Question `json:"questions"`
	}
	if err := json.Unmarshal([]byte(s), &in); err != nil {
		return nil, fmt.Errorf("--json は {\"questions\":[...]} の JSON: %w", err)
	}
	return card.NormalizeQuestions(in.Questions)
}

// liveDir は本物のモードの状態の置き場 (store と、pro-con が起動した session の記録の置き場)。
func liveDir(home string) string { return filepath.Join(stateDir(home), "live") }
