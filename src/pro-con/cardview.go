package main

// pro-con card list / show / wait — カードを画面なしで読む口 (issue 442。PM の CLI と、外の Claude が覗くため。親の設計は 441)。
//
// 🚨 読み取りだけ (441 の守ること 1)。受付の箱・記録・起動の記録に何も書かず、socket へ wake / notify を送らない
// (wait が送るのは購読の sub だけ。dispatcher は購読の数を判断に使わない)。TestViewCommandsDoNotWrite が固定する。
// `card add` の「適用を待ってカード ID を返す」も、置いた後は読むだけ。

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"pro-con/card"
	"pro-con/live"
	"pro-con/store"
	"pro-con/wake"
)

// viewPoll は wait / add の待ちで記録を読み直す間隔 (dispatcher の知らせが届かないとき。届けばすぐ読む)。
var viewPoll = time.Second

// waitFirstRead は wait が最初の列を読んだ直後に呼ぶ (テストが「読んだ後に変える」を決めるための差し替え口。本番では何もしない)。
var waitFirstRead = func() {}

// logLines は show が出す PG の出力の末尾の行数 (画面の詳細は 3 行。CLI は読む側が切れるので多めに)。
const logLines = 10

// stateNames は --state / --until に書ける列の名前 (画面の見出しと英語の名前)。
var stateNames = func() map[string]card.State {
	m := map[string]card.State{}
	en := []string{"requested", "planned", "running", "waiting", "review", "done"}
	for i, s := range card.Columns {
		m[s.Label()] = s
		m[en[i]] = s
	}
	return m
}()

func parseState(v string) (card.State, error) {
	if s, ok := stateNames[strings.ToLower(v)]; ok {
		return s, nil
	}
	return 0, fmt.Errorf("列の名前が違う: %q (依頼 / 分解済み / 作業中 / 質問待ち / レビュー / 完了、または requested / planned / running / waiting / review / done)", v)
}

// cardSummary は list の 1 行 (--json の形)。
type cardSummary struct {
	ID       string    `json:"id"`
	State    string    `json:"state"`
	Title    string    `json:"title"`
	Owner    string    `json:"owner"`
	Session  string    `json:"session,omitempty"`
	Since    time.Time `json:"since"`
	Waiting  string    `json:"waiting,omitempty"`  // 何を待っているか (質問・権限・順番待ち・利用枠・落ちた)
	Question string    `json:"question,omitempty"` // 回答の要る待ちの文
	Archived bool      `json:"archived,omitempty"`
	Deleting bool      `json:"deleting,omitempty"` // 削除の依頼を受けて、PG の session を止めてから消えるのを待っている
}

// summarize は cards (記録の全カード) から、順番で待っている前のカードも引く。
func summarize(c card.Card, cards []card.Card) cardSummary {
	return cardSummary{ID: c.ID, State: c.State.Label(), Title: c.Title, Owner: c.Owner, Session: c.Session, Since: c.Since,
		Waiting: waiting(c, cards), Question: c.Wait.Question, Archived: c.Archived, Deleting: c.Deleting()}
}

// waiting は何を待っているか。PG の待ち (waitLabel) が無ければ、順番の前のカード (issue 468)。
func waiting(c card.Card, cards []card.Card) string {
	if l := waitLabel(c.Wait); l != "" {
		return l
	}
	if c.State == card.Planned {
		if b := card.Blockers(cards, c); len(b) > 0 {
			return strings.Join(b, ", ") + " の後"
		}
	}
	return ""
}

func waitLabel(w card.Wait) string {
	switch w.Kind {
	case card.WaitQuestion:
		return "質問"
	case card.WaitPermission:
		return "権限の確認"
	case card.WaitResource:
		return fmt.Sprintf("%s の順番待ち (%d 番目)", w.Resource, w.Position)
	case card.WaitQuota:
		return "利用枠の回復待ち"
	case card.WaitCrashed:
		return "落ち続けたので止めた"
	case card.WaitNone:
	}
	return ""
}

// cardDetail は show の中身 (--json の形)。Log は PG の出力の末尾 (transcript から読む。記録には無い)。
type cardDetail struct {
	Card    card.Card `json:"card"`
	Log     []string  `json:"log"`
	Waiting string    `json:"waiting,omitempty"` // 何を待っているか (list と同じ。順番の前のカードは記録の全カードから引く)
}

// viewEnv は読む口が見る場所。
type viewEnv struct {
	dir      string // 本物のモードの状態の置き場 (store と起動の記録)
	projects string // ~/.claude/projects (PG の transcript)
	now      func() time.Time
}

func runCardList(args []string, env viewEnv, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pro-con card list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	stateArg := fs.String("state", "", "")
	all := fs.Bool("all", false, "")
	asJSON := fs.Bool("json", false, "")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, "usage: pro-con card list [--state <列>] [--all] [--json]")
		return 2
	}
	var want *card.State
	if *stateArg != "" {
		s, err := parseState(*stateArg)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "pro-con card list:", err)
			return 2
		}
		want = &s
	}
	st, err := store.Load(env.dir)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con card list:", err)
		return 1
	}
	out := []cardSummary{}
	for _, c := range st.Cards {
		if (c.Archived && !*all) || (want != nil && c.State != *want) {
			continue
		}
		out = append(out, summarize(c, st.Cards))
	}
	if *asJSON {
		return writeJSON(stdout, stderr, out)
	}
	now := env.now()
	colW := 0
	for _, s := range card.Columns {
		colW = max(colW, ansi.StringWidth(s.Label()))
	}
	for _, s := range out {
		// 列の見出しは全角なので、%-Ns (文字数) ではなく表示幅で揃える (全角は 2 セル)
		col := s.State + strings.Repeat(" ", max(colW-ansi.StringWidth(s.State), 0))
		line := fmt.Sprintf("%s  %s  %s  担当: %s  (%s)", s.ID, col, s.Title, orDashCLI(s.Owner), fmtAge(now.Sub(s.Since)))
		if s.Waiting != "" {
			line += "  待ち: " + s.Waiting
		}
		_, _ = fmt.Fprintln(stdout, line)
	}
	return 0
}

func runCardShow(args []string, env viewEnv, stdout, stderr io.Writer) int {
	id, asJSON, ok := parseIDAndJSON("show", args, stderr)
	if !ok {
		return 2
	}
	d, err := loadDetail(env, id)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con card show:", err)
		return 1
	}
	if asJSON {
		return writeJSON(stdout, stderr, d)
	}
	writeDetail(stdout, d, env.now())
	return 0
}

// loadDetail は記録からカードを、起動の記録と transcript から PG の出力の末尾を読む (どれも読むだけ)。
func loadDetail(env viewEnv, id string) (cardDetail, error) {
	st, err := store.Load(env.dir)
	if err != nil {
		return cardDetail{}, err
	}
	c, ok := findCard(st, id)
	if !ok {
		return cardDetail{}, fmt.Errorf("カード %q が無い", id)
	}
	return cardDetail{Card: c, Log: pgLog(env, c), Waiting: waiting(c, st.Cards)}, nil
}

func findCard(st store.State, id string) (card.Card, bool) {
	for _, c := range st.Cards {
		if c.ID == id {
			return c, true
		}
	}
	return card.Card{}, false
}

// pgLog はカードの PG の出力の末尾。起動の記録で短い id から session id を引き、transcript を読む。読めなければ空
// (画面の詳細と同じく、pro-con が起動した session のものだけ。記録に無い session の transcript は読まない)。
func pgLog(env viewEnv, c card.Card) []string {
	if c.Session == "" {
		return []string{}
	}
	reg, err := live.LoadRegistry(filepath.Join(env.dir, live.RegistryFile))
	if err != nil {
		return []string{}
	}
	for _, o := range reg {
		if o.ID != c.Session || o.SessionID == "" || (o.CardID != "" && o.CardID != c.ID) { // 短い id が重なっても別のカードの transcript を出さない
			continue
		}
		p, err := live.FindTranscript(env.projects, o.SessionID)
		if err != nil {
			continue // 同じ短い id の別の行 (再開で入れ替わった等) の transcript を探す
		}
		t, err := live.ReadTail(p)
		if err != nil {
			continue
		}
		out := t.Outputs
		if len(out) > logLines {
			out = out[len(out)-logLines:]
		}
		return append([]string{}, out...)
	}
	return []string{}
}

// writeDetail は画面の詳細 (ui/drawer.go の drawerBody) と同じ中身を、色も折り返しも無しで出す。
func writeDetail(w io.Writer, d cardDetail, now time.Time) {
	c := d.Card
	p := func(format string, a ...any) { _, _ = fmt.Fprintf(w, format+"\n", a...) }
	p("%s  %s", c.ID, c.Title)
	p("状態: %s (%s)  担当: %s  repo: %s  session: %s", c.State.Label(), fmtAge(now.Sub(c.Since)), orDashCLI(c.Owner), orDashCLI(c.Repo), orDashCLI(c.Session))
	var refs []string
	for _, r := range c.Issues {
		refs = append(refs, r.String()+" ("+r.Status+")")
	}
	link := strings.Join(refs, ", ")
	if link == "" {
		link = "なし"
		if c.Ending != card.EndNone {
			link += " / 終わり方: " + c.Ending.Label()
		}
	}
	p("issue: %s   親: %s", link, orDashCLI(c.ParentID))
	p("依頼の原文: 「%s」", c.Request)
	if c.Prompt != "" {
		p("PM に渡した指示: %s", c.Prompt)
	}
	if c.Deleting() {
		p("削除中: %s が %s前に依頼した (PG の session を止めてから消える)", c.DeleteBy, fmtAge(now.Sub(c.DeleteAt)))
	}
	if len(c.After) > 0 {
		p("順番: %s の後 (PM が付けた。完了するまで起動しない)", strings.Join(c.After, ", "))
	}
	if d.Waiting != "" {
		p("待ち: %s", d.Waiting)
	}
	if c.Wait.Question != "" {
		p("質問: %s", c.Wait.Question)
	}
	if c.Run != "" {
		p("テストの係に頼んだコマンド (結果待ち): %s", c.Run)
	}
	if c.Exec.Active() {
		p("実行中: %s (%s から)", c.Exec.Command, fmtAge(now.Sub(c.Exec.Since)))
	}
	for _, o := range c.Orders {
		st := "未達"
		if o.Delivered {
			st = "届いた"
		}
		p("追加オーダー (%s・%s): %s", o.Kind.Label(), st, o.Text)
	}
	p("")
	p("履歴")
	for _, e := range c.History {
		p("  %s %s", e.At.Local().Format("01-02 15:04"), e.Text) // テストの係の結果・回答・attach の指示もここに載る
	}
	p("")
	p("出力 (PG の出力の末尾)")
	for _, s := range d.Log {
		p("  %s", s)
	}
}

func runCardWait(args []string, env viewEnv, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pro-con card wait", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	until := fs.String("until", "", "")
	timeout := fs.Duration("timeout", 10*time.Minute, "")
	asJSON := fs.Bool("json", false, "")
	var pos []string
	for len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		pos, args = append(pos, args[0]), args[1:]
	}
	if err := fs.Parse(args); err != nil || len(pos)+fs.NArg() != 1 || *timeout <= 0 {
		_, _ = fmt.Fprintln(stderr, "usage: pro-con card wait <カード> [--until <列>] [--timeout <長さ (既定 10m)>] [--json]")
		return 2
	}
	id := append(pos, fs.Args()...)[0]
	var target *card.State
	if *until != "" {
		s, err := parseState(*until)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "pro-con card wait:", err)
			return 2
		}
		target = &s
	}
	st, err := store.Load(env.dir)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con card wait:", err)
		return 1
	}
	first, ok := findCard(st, id)
	if !ok {
		_, _ = fmt.Fprintf(stderr, "pro-con card wait: カード %q が無い\n", id)
		return 1
	}
	waitFirstRead()
	if target == nil && first.State == card.Done { // 完了からはどの列にも動かない (待ち続けて時間切れにしない)
		_, _ = fmt.Fprintf(stderr, "pro-con card wait: %s は完了しているので、列はもう変わらない\n", id)
		return 1
	}
	// --until が無ければ「列が変わる」、あれば「その列に居る」まで (もう居ればすぐ返す)
	done := func(c card.Card) (bool, error) {
		if target == nil {
			return c.State != first.State, nil
		}
		if c.State == *target {
			return true, nil
		}
		if c.State == card.Done { // 完了からはどの列にも戻らない (待ち続けて時間切れにしない)
			return false, fmt.Errorf("%s は完了になったので %s には来ない", c.ID, target.Label())
		}
		return false, nil
	}
	var got card.Card
	var cards []card.Card
	err = waitFor(env.dir, *timeout, stderr, func() (bool, error) {
		st, err := store.Load(env.dir)
		if err != nil {
			return false, err
		}
		c, ok := findCard(st, id)
		if !ok {
			return false, fmt.Errorf("カード %q が記録から消えた", id)
		}
		got, cards = c, st.Cards
		return done(c)
	})
	if errors.Is(err, errWaitTimeout) {
		_, _ = fmt.Fprintf(stderr, "pro-con card wait: %v 待っても %s は %s のまま\n", *timeout, id, got.State.Label())
		return 1
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con card wait:", err)
		return 1
	}
	if *asJSON {
		return writeJSON(stdout, stderr, summarize(got, cards))
	}
	_, _ = fmt.Fprintf(stdout, "%s  %s → %s  %s\n", got.ID, first.State.Label(), got.State.Label(), got.Title)
	return 0
}

var errWaitTimeout = errors.New("時間切れ")

// waitFor は cond が真になるまで待つ。上限は timeout (過ぎたら errWaitTimeout)。
func waitFor(dir string, timeout time.Duration, warn io.Writer, cond func() (bool, error)) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := watchDir(ctx, dir, warn, cond); err != nil || ctx.Err() == nil {
		return err
	}
	return errWaitTimeout
}

// watchDir は cond が真になるか ctx が終わるまで、cond を呼び続ける。dispatcher の知らせ (購読) で呼び直し、届かなくても viewPoll ごとに呼び直す。
// ctx が終わったら nil を返す (呼び出し側が ctx.Err で見分ける)。
// 🚨 購読は sub を送るだけ (wake / notify は送らない)。dispatcher が居なくてもポーリングで待てる。
// socket の逃がし先の権限が緩ければ直さずにつながず (直すのは dispatcher だけ。issue 445)、warn に 1 行知らせてポーリングで待つ
func watchDir(ctx context.Context, dir string, warn io.Writer, cond func() (bool, error)) error {
	changed := make(chan struct{}, 1)
	refused := make(chan error, 1) // 知らせは待ちのループから書く (購読の goroutine から warn へ書くと、呼び出し側の書き込みと競る)
	// 購読が終わるのを待ってから戻る (戻った後も購読が繋ぎ直しを続けると、戻った後の呼び出し側・テストの差し替えと競る)
	subCtx, stop := context.WithCancel(ctx)
	subDone := make(chan struct{})
	defer func() { stop(); <-subDone }()
	sub := wake.NewSubscriber(dir, func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	}).OnRefused(func(err error) {
		select {
		case refused <- err:
		default:
		}
	})
	go func() { defer close(subDone); sub.Run(subCtx) }()
	tick := time.NewTicker(viewPoll)
	defer tick.Stop()
	for {
		ok, err := cond()
		if err != nil || ok {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case err := <-refused:
			_, _ = fmt.Fprintf(warn, "pro-con: dispatcher の知らせを受けずに %v ごとの読み直しで待つ (%v)\n", viewPoll, err)
		case <-changed:
		case <-tick.C:
		}
	}
}

// addAndWait は add の依頼を置き、dispatcher が適用するまで待ってカード ID を返す。除けられた (記録の Rejected に載った) ら rc=1 で理由を返す。
// 🚨 待てなければ依頼 ID を返して rc=exitNotApplied (そう言う)。rc=0 にしない: `C=$(pro-con card add …)` を続けて `card plan "$C"` に
// 渡すと、plan も箱には置けて (rc=0)、dispatcher が後で「カードが無い」と除けるまで誰も気づかない (敵対的レビュー 2026-09-25 P2)
func addAndWait(dir string, req store.Request, timeout time.Duration, stdout, stderr io.Writer) int {
	reqID, err := store.Submit(dir, req)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con card: 受付の箱に置けない:", err)
		return 1
	}
	if timeout <= 0 {
		_, _ = fmt.Fprintln(stdout, reqID)
		return 0
	}
	var cardID, rejected string
	err = waitFor(dir, timeout, stderr, func() (bool, error) {
		st, err := store.Load(dir)
		if err != nil {
			return false, err
		}
		for _, c := range st.Cards {
			if c.FromRequest == reqID {
				cardID = c.ID
				return true, nil
			}
		}
		for _, r := range st.Rejected {
			if r.ID == reqID {
				rejected = r.Why
				return true, nil
			}
		}
		return false, nil
	})
	switch {
	case rejected != "":
		_, _ = fmt.Fprintf(stderr, "pro-con card add: dispatcher が依頼 %s を除けた: %s\n", reqID, rejected)
		return 1
	case cardID != "":
		_, _ = fmt.Fprintln(stdout, cardID)
		return 0
	}
	// 時間切れ・読めない: 依頼は箱に残っている (dispatcher が動けば後で適用される)。依頼 ID を返して、そう言う
	// (カードに元の依頼 ID を書かない古い dispatcher が動いている間も、適用はされるがここは当たらない)
	why := "dispatcher が動いていないか、元の依頼 ID をカードに書かない古い dispatcher が動いている (再起動すると直る)"
	if err != nil && !errors.Is(err, errWaitTimeout) {
		why = err.Error()
	}
	_, _ = fmt.Fprintf(stderr, "pro-con card add: %v 待っても適用を確かめられないので、カード ID の代わりに依頼 ID を返す (rc=%d。%s。pro-con card list で確かめる)\n", timeout, exitNotApplied, why)
	_, _ = fmt.Fprintln(stdout, reqID)
	return exitNotApplied
}

// exitNotApplied は add が適用を確かめられなかったときの rc (依頼は箱に置いた。使い方の誤り 2・失敗 1 と分ける)。
const exitNotApplied = 3

func parseIDAndJSON(op string, args []string, stderr io.Writer) (string, bool, bool) {
	var id string
	asJSON := false
	for _, a := range args {
		switch {
		case a == "--json":
			asJSON = true
		case strings.HasPrefix(a, "-") || id != "":
			_, _ = fmt.Fprintf(stderr, "usage: pro-con card %s <カード> [--json]\n", op)
			return "", false, false
		default:
			id = a
		}
	}
	if id == "" {
		_, _ = fmt.Fprintf(stderr, "usage: pro-con card %s <カード> [--json]\n", op)
		return "", false, false
	}
	return id, asJSON, true
}

func writeJSON(stdout, stderr io.Writer, v any) int {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con card:", err)
		return 1
	}
	return 0
}

func orDashCLI(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// fmtAge は経過時間を人が読む長さで (画面の fmtDur と同じ粒度: 秒 / 分 / 時間 / 日)。
func fmtAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d 秒", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d 分", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d 時間", int(d.Hours()))
	}
	return fmt.Sprintf("%d 日", int(d.Hours()/24))
}
