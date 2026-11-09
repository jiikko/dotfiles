// slack-bbs は購読した Slack のチャンネルの会話を、話題ごとに組み直して掲示板風に読むコマンド。
//
//	slack-bbs [-since 2d] [-lookback 30d] [-model sonnet] [-max 500] [-dry-run] [-json] [<チャンネルID>...]
//	slack-bbs sub     購読するチャンネルを一覧から選ぶ
//	slack-bbs subs    購読しているチャンネルを出す
//	slack-bbs prepare [オプション] [-out <dir>] [<チャンネルID>...]   分類の依頼を書く (2 段の 1 段目)
//	slack-bbs show -key <鍵> [-json] [-keep] <dir>                  依頼と応答から板を出す (2 段の 2 段目)
//
// 取得は slack-cli (読み取り専用)、分類は claude -p か、2 段 (prepare → 外で分類 → show) なら呼び出した側 (GUI の Claude のセッション)。LLM に任せるのは話題への分け方・見出し・要点・ラベルだけで、
// 本文は原文を並べる。スレッドは親が古くても期間内に返信が付いていれば「続いている」話題として出す。
// 1 段の実行は会話の本文をどこにも保存しない (設定に残すのは購読するチャンネルの ID と名前だけ)。2 段の実行は prepare のディレクトリに
// 本文を書き、show が出し切ったら消す。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jiikko/dotfiles/src/termsafe"
	"slack-bbs/board"
	"slack-bbs/job"
	"slack-bbs/llm"
	"slack-bbs/render"
	"slack-bbs/source"
	"slack-bbs/subs"
	"slack-bbs/view"
	"subproc"
)

const usage = `slack-bbs - Slack のチャンネルの会話を話題ごとに組み直して読む

使い方:
  slack-bbs [オプション] [<チャンネルID>...]   購読しているチャンネル (引数があればそのチャンネル) を読む
  slack-bbs sub                               購読するチャンネルを一覧から選ぶ
  slack-bbs subs                              購読しているチャンネルを出す
  slack-bbs prepare [オプション] [-out <dir>] [<チャンネルID>...]
                                              分類せず、分類の依頼をディレクトリに書く (2 段の 1 段目。出力は JSON)
  slack-bbs show -key <鍵> [-json] [-keep] <dir>
                                              依頼と分類の応答を検査して板を出し、全部出せたら依頼と応答を消す (2 段目)。
                                              鍵は prepare の出力の key (分類する側には渡さない)

オプション (引数より前に置く):
  -since <期間>     読む期間。2d / 12h / 2026-11-01 (既定 2d)
  -lookback <期間>  期間より前に始まったスレッドを、どこまで遡って探すか (既定 30d)
  -model <名前>     分類に使う claude のモデル (既定 sonnet)
  -max <数>         1 チャンネルで LLM に送る発言の上限。超えたら送らずに止める (既定 500)
  -dry-run          LLM を呼ばず、送る件数と文字数だけを出す
  -json             話題に組んだ結果 (中間表現) を JSON で出す
  -md               端末でも読む画面を開かず、Markdown を出す (パイプの先では既定で Markdown)

読む画面 (端末で打ったとき): 左に話題の一覧、右に中身。r で今のチャンネル、R で全部を読み直す (claude -p で分類し直す)。
  j/k: 移動  l/Enter: 本文へ  h/Esc: 一覧へ  Tab: チャンネル  o: Slack で開く  q: 終わる

環境変数:
  SLACK_BBS_SLACK   slack-cli のパス (既定 slack)
  SLACK_BBS_CLAUDE  claude のパス (既定 claude)

🚨 分類のために、期間内の発言と文脈の発言の本文が claude -p (Anthropic) へ送られる。送る前に -dry-run で量を見られる。
   2 段の実行では、本文は prepare が書くディレクトリ (既定は一時ディレクトリ) を経て、分類する側のセッションへ渡る。
`

type deps struct {
	slack    source.Runner
	claude   llm.Runner
	now      func() time.Time
	subsPath func() (string, error)
	pick     func(*subs.Picker) error
	tty      func() bool // stdout が端末か (端末なら読む画面を開く)
	browse   func(*view.Model) error
	openURL  func(string) error
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	rc := run(ctx, os.Args[1:], os.Stdout, os.Stderr, deps{
		slack:    source.ExecRunner{Bin: envOr("SLACK_BBS_SLACK", "slack"), Tee: os.Stderr},
		claude:   llm.ExecRunner{Bin: envOr("SLACK_BBS_CLAUDE", "claude")},
		now:      time.Now,
		subsPath: subs.Path,
		pick:     subs.Run,
		tty:      func() bool { return isTerminal(os.Stdout) },
		browse:   view.Run,
		openURL: func(url string) error {
			return subproc.Command("open", url).Run()
		},
	})
	stop()
	os.Exit(rc)
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, d deps) int {
	if len(args) > 0 {
		switch args[0] {
		case "sub":
			return report(stderr, cmdSub(ctx, d, stdout))
		case "subs":
			return report(stderr, cmdSubs(d, stdout))
		case "prepare":
			o, err := parseFlags(args[1:], d.now(), stderr)
			if err != nil {
				return flagRC(stderr, err)
			}
			return report(stderr, prepare(ctx, o, o.out, d, stdout, stderr))
		case "show":
			return cmdShow(args[1:], stdout, stderr)
		case "help", "-h", "--help":
			fmt.Fprint(stdout, usage)
			return 0
		}
	}
	o, err := parseFlags(args, d.now(), stderr)
	if err != nil {
		return flagRC(stderr, err)
	}
	if o.out != "" {
		fmt.Fprintln(stderr, "slack-bbs: -out は prepare のオプション")
		return 2
	}
	return report(stderr, read(ctx, o, d, stdout, stderr))
}

// flagRC は引数の読み取りの誤りを出して終了コードを返す (-h なら使い方を出して 0)。
func flagRC(stderr io.Writer, err error) int {
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(stderr, usage)
		return 0
	}
	fmt.Fprintln(stderr, "slack-bbs:", clean(err.Error()), "(使い方は slack-bbs -h)")
	return 2
}

// newFlagSet はフラグの集まり。flag 自身の出力 (説明の空なフラグを並べるだけで読めない) は捨て、誤りと -h は flagRC が出す。
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func cmdShow(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("slack-bbs show")
	asJSON := fs.Bool("json", false, "")
	keep := fs.Bool("keep", false, "")
	key := fs.String("key", "", "")
	if err := fs.Parse(args); err != nil {
		return flagRC(stderr, err)
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "slack-bbs: show には prepare の出力したディレクトリを 1 つ渡す (オプションはその前に置く)")
		return 2
	}
	return report(stderr, show(fs.Arg(0), *key, *asJSON, *keep, stdout, stderr))
}

// clean は外から来た文字列 (チャンネル名・パス・誤りの文) を stderr へ出す前に端末の制御文字を落とす。
func clean(s string) string { return termsafe.PlainLine(s) }

func report(stderr io.Writer, err error) int {
	if err != nil {
		fmt.Fprintln(stderr, "slack-bbs:", clean(err.Error()))
		return 1
	}
	return 0
}

type options struct {
	since, lookback time.Time
	until           time.Time
	model           string
	max             int
	dryRun, json    bool
	md              bool   // 端末でも読む画面を開かず Markdown を出す
	sinceFixed      bool   // -since を日付で渡した (読み直しでずらさない)
	out             string // prepare の書き先 (空なら一時ディレクトリを作る)
	channels        []string
}

func parseFlags(args []string, now time.Time, stderr io.Writer) (options, error) {
	fs := newFlagSet("slack-bbs")
	since := fs.String("since", "2d", "")
	lookback := fs.String("lookback", "30d", "")
	o := options{until: now}
	fs.StringVar(&o.model, "model", "sonnet", "")
	fs.IntVar(&o.max, "max", 500, "")
	fs.BoolVar(&o.dryRun, "dry-run", false, "")
	fs.BoolVar(&o.json, "json", false, "")
	fs.StringVar(&o.out, "out", "", "")
	fs.BoolVar(&o.md, "md", false, "")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	o.channels = fs.Args()
	for _, a := range o.channels {
		if strings.HasPrefix(a, "-") {
			return o, fmt.Errorf("オプション %s はチャンネルより前に置く", a)
		}
	}
	var err error
	if o.since, o.sinceFixed, err = parseSince(*since, now); err != nil {
		return o, err
	}
	back, err := parseSpan(*lookback)
	if err != nil {
		return o, fmt.Errorf("-lookback: %w", err)
	}
	o.lookback = o.since.Add(-back)
	if o.max <= 0 {
		return o, errors.New("-max は 1 以上")
	}
	return o, nil
}

// parseSince は -since を時刻にする。期間 (2d / 12h) なら now から遡り、日付 (2026-11-01) ならその日の 0 時 (手元の時刻帯)。
// fixed は日付で渡したか (読み直しで期間をずらさない)。
func parseSince(s string, now time.Time) (t time.Time, fixed bool, err error) {
	if t, err := time.ParseInLocation("2006-01-02", s, now.Location()); err == nil {
		if !t.Before(now) {
			return time.Time{}, false, fmt.Errorf("-since %s は今より後", s)
		}
		return t, true, nil
	}
	d, err := parseSpan(s)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("-since: %w", err)
	}
	return now.Add(-d), false, nil
}

// parseSpan は 2d / 12h / 90m のような長さ。d (日) は time.ParseDuration に無いので先に捌く。
func parseSpan(s string) (time.Duration, error) {
	var d time.Duration
	if n, ok := strings.CutSuffix(s, "d"); ok {
		days, err := strconv.Atoi(n)
		if err != nil {
			return 0, fmt.Errorf("期間 %q を読めない (例: 2d / 12h)", s)
		}
		d = time.Duration(days) * 24 * time.Hour
	} else {
		var err error
		if d, err = time.ParseDuration(s); err != nil {
			return 0, fmt.Errorf("期間 %q を読めない (例: 2d / 12h)", s)
		}
	}
	if d <= 0 {
		return 0, fmt.Errorf("期間 %q は正の長さにする", s)
	}
	return d, nil
}

func cmdSub(ctx context.Context, d deps, stdout io.Writer) error {
	path, err := d.subsPath()
	if err != nil {
		return err
	}
	cur, err := subs.Load(path)
	if err != nil {
		return err
	}
	all, err := source.Client{R: d.slack}.Channels(ctx)
	if err != nil {
		return err
	}
	p := subs.NewPicker(all, cur)
	if err := d.pick(p); err != nil {
		return err
	}
	if !p.Saved() {
		fmt.Fprintln(stdout, "保存せずに終わった")
		return nil
	}
	sel := p.Selected()
	if err := subs.Save(path, sel); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%d 件のチャンネルを購読する (%s)\n", len(sel), path)
	return nil
}

func cmdSubs(d deps, stdout io.Writer) error {
	path, err := d.subsPath()
	if err != nil {
		return err
	}
	cur, err := subs.Load(path)
	if err != nil {
		return err
	}
	if len(cur) == 0 {
		fmt.Fprintln(stdout, "購読しているチャンネルはまだ無い (slack-bbs sub で選ぶ)")
		return nil
	}
	for _, l := range subs.Names(cur) {
		fmt.Fprintln(stdout, l)
	}
	return nil
}

// collected は 1 チャンネルから集めて単位にまとめ直したもの。
type collected struct {
	page    render.Page
	units   []board.Unit
	sent    int // LLM へ送る発言の数 (文脈の発言を含む)
	chars   int
	threads int // スレッドを取り直した回数
}

// eachChannel は読むチャンネルごとに集めて fn へ渡す。1 チャンネルの失敗では止めず、最後に失敗したチャンネルを返す。
func eachChannel(ctx context.Context, o options, d deps, stderr io.Writer, fn func(c collected) error) error {
	targets, err := channelsToRead(o, d)
	if err != nil {
		return err
	}
	cli := source.Client{R: d.slack}
	me, err := cli.Whoami(ctx)
	if err != nil {
		return err
	}
	names, err := cli.Names(ctx)
	if err != nil {
		return err
	}
	var failed []string
	for _, ch := range targets {
		err := func() error {
			c, err := collect(ctx, o, cli, me, names, ch)
			if err != nil {
				return err
			}
			return fn(c)
		}()
		if err != nil {
			if ctx.Err() != nil {
				return err
			}
			fmt.Fprintf(stderr, "slack-bbs: #%s: %s\n", clean(ch.Name), clean(err.Error()))
			failed = append(failed, "#"+ch.Name)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("読めなかったチャンネル: %s", strings.Join(failed, " "))
	}
	return nil
}

// read は 1 段で読む: 集めて claude -p で分類し、組んで出す。
func read(ctx context.Context, o options, d deps, stdout, stderr io.Writer) error {
	if !o.json && !o.dryRun && !o.md && d.tty() {
		return browse(ctx, o, d)
	}
	var pages []render.Page
	err := eachChannel(ctx, o, d, stderr, func(c collected) error {
		if o.dryRun {
			fmt.Fprintf(stdout, "#%s: 単位 %d / 送る発言 %d (スレッドの取得 %d 回) / 本文 %d 文字\n",
				c.page.ChannelName, len(c.units), c.sent, c.threads, c.chars)
			return nil
		}
		if err := checkMax(c, o.max); err != nil {
			return err
		}
		p, dropped, use, err := classify(ctx, o, d, c)
		if err != nil {
			return err
		}
		if use != nil {
			fmt.Fprintf(stderr, "slack-bbs: #%s: claude へ発言 %d 件 (約 %d トークン) を送った ($%.4f)\n", clean(p.ChannelName), c.sent, use.InputTokens, use.CostUSD)
		}
		warnDropped(stderr, p.ChannelName, dropped)
		if o.json {
			pages = append(pages, p)
			return nil
		}
		return writeMarkdown(stdout, p)
	})
	if o.json && !o.dryRun {
		if jerr := writeJSON(stdout, pages); jerr != nil {
			return jerr
		}
	}
	return err
}

func checkMax(c collected, limit int) error {
	if c.sent > limit {
		return fmt.Errorf("送る発言が %d 件で上限 %d を超えた。-since を短くするか -max を上げる (-dry-run で量を見られる)", c.sent, limit)
	}
	return nil
}

// classify は集めた単位を claude -p で分類させ、検査して話題に組んだ板を返す。単位が無ければ claude を呼ばない (use は nil)。
func classify(ctx context.Context, o options, d deps, c collected) (render.Page, board.Dropped, *llm.Usage, error) {
	p := c.page
	if len(c.units) == 0 {
		return p, board.Dropped{}, nil, nil
	}
	resp, use, err := llm.Classify(ctx, d.claude, o.model, p.ChannelName, c.units)
	if err != nil {
		return p, board.Dropped{}, nil, err
	}
	var dropped board.Dropped
	p.Topics, dropped = board.Assemble(c.units, resp)
	return p, dropped, &use, nil
}

// assemble は分類を検査して話題に組み、捨てたものがあれば warn で出す。
func assemble(stderr io.Writer, name string, units []board.Unit, resp board.Response) []board.Topic {
	topics, dropped := board.Assemble(units, resp)
	warnDropped(stderr, name, dropped)
	return topics
}

func warnDropped(stderr io.Writer, name string, dropped board.Dropped) {
	if dropped.Any() {
		fmt.Fprintf(stderr, "slack-bbs: #%s: 🚨 LLM の分類から捨てたもの: %s\n", clean(name), dropped)
	}
}

// browse は読む画面を開く。読み直しのたびに取得から分類までをやり直す (分類は claude -p)。画面の中では stderr が見えないので、
// slack-cli の警告と分類から捨てたものは板の注記に載せる。
func browse(ctx context.Context, o options, d deps) error {
	targets, err := channelsToRead(o, d)
	if err != nil {
		return err
	}
	cli := source.Client{R: source.Quiet(d.slack)}
	me, err := cli.Whoami(ctx)
	if err != nil {
		return err
	}
	names, err := cli.Names(ctx)
	if err != nil {
		return err
	}
	// 画面を閉じたら、走っている読み直し (slack-cli と claude -p) を止め、終わるのを待ってから戻る。
	// 待たないと、戻った直後の os.Exit で子を止める前にこのプロセスが終わり、claude が走り続けて費用がかかる。
	ctx, cancel := context.WithCancel(ctx)
	var (
		mu       sync.Mutex
		closed   bool // 閉じた後に走り出した読み直しは始めない (Wait と並んで Add しない)
		inflight sync.WaitGroup
	)
	defer func() {
		mu.Lock()
		closed = true
		mu.Unlock()
		cancel()
		inflight.Wait()
	}()
	load := func(ctx context.Context, ch subs.Channel) (render.Page, error) {
		mu.Lock()
		if closed {
			mu.Unlock()
			return render.Page{}, context.Canceled
		}
		inflight.Add(1)
		mu.Unlock()
		defer inflight.Done()
		o := o
		if now := d.now(); !o.sinceFixed && o.until.Before(now) {
			// 読み直しは押した時点までを読む (期間の長さは保つ)。日付で渡した -since はずらさない
			shift := now.Sub(o.until)
			o.since, o.lookback, o.until = o.since.Add(shift), o.lookback.Add(shift), now
		} else {
			o.until = now
		}
		c, err := collect(ctx, o, cli, me, names, ch)
		if err != nil {
			return render.Page{}, err
		}
		if err := checkMax(c, o.max); err != nil {
			return render.Page{}, err
		}
		p, dropped, _, err := classify(ctx, o, d, c)
		if err != nil {
			return render.Page{}, err
		}
		if dropped.Any() {
			p.Notes = append(p.Notes, "LLM の分類から捨てたもの: "+dropped.String())
		}
		return p, nil
	}
	return d.browse(view.New(ctx, targets, load, d.openURL, d.now))
}

func writeMarkdown(w io.Writer, p render.Page) error {
	if err := render.Markdown(w, p); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w)
	return err
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// prepared は prepare の -json の出力 (分類する側が読む)。
type prepared struct {
	Dir      string            `json:"dir"`
	Key      string            `json:"key"` // show -key に渡す。分類する側には渡さない (依頼の書き換えを見破る鍵)
	Channels []preparedChannel `json:"channels"`
}

type preparedChannel struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Units    int    `json:"units"`
	Messages int    `json:"messages"`
	Prompt   string `json:"prompt"`   // 分類する側が読む文面
	Response string `json:"response"` // 分類する側が書く応答
}

// prepare は 2 段の 1 段目: 集めて、分類の依頼をディレクトリに書く (分類はしない)。
// 単位の無いチャンネルも依頼を書く (show が「期間内の発言はありません」のページを出す。応答は要らない)。
func prepare(ctx context.Context, o options, dir string, d deps, stdout, stderr io.Writer) error {
	m, dir, err := job.Open(dir)
	if err != nil {
		return err
	}
	out := prepared{Dir: dir, Key: m.Key()}
	err = eachChannel(ctx, o, d, stderr, func(c collected) error {
		if err := checkMax(c, o.max); err != nil {
			return err
		}
		id := c.page.Channel
		prompt, err := llm.PromptFile(c.page.ChannelName, c.units, job.ResponsePath(dir, id))
		if err != nil {
			return err
		}
		req := job.Request{Channel: id, ChannelName: c.page.ChannelName, Since: c.page.Since, Until: c.page.Until,
			Lookback: c.page.Lookback, Base: c.page.Base, Notes: c.page.Notes, Units: c.units}
		if err := job.Add(dir, &m, req, prompt); err != nil {
			return err
		}
		pc := preparedChannel{ID: id, Name: c.page.ChannelName, Units: len(c.units), Messages: c.sent, Response: job.ResponsePath(dir, id)}
		if len(c.units) > 0 {
			pc.Prompt = job.PromptPath(dir, id)
		}
		out.Channels = append(out.Channels, pc)
		return nil
	})
	if len(out.Channels) == 0 {
		job.Discard(dir, m)
		out.Dir, out.Key = "", ""
		out.Channels = []preparedChannel{}
	}
	if jerr := writeJSON(stdout, out); jerr != nil {
		return jerr
	}
	return err
}

// show は 2 段の 2 段目: 依頼と応答を読み、検査して組んで出す。全チャンネルを出せたら、prepare と分類する側が書いたファイルを消す。
func show(dir, key string, asJSON, keep bool, stdout, stderr io.Writer) error {
	m, err := job.ReadManifest(dir, key)
	if err != nil {
		return err
	}
	var pages []render.Page
	var failed []string
	for _, ch := range m.Channels {
		r, resp, err := job.Read(dir, m, ch)
		if errors.Is(err, job.ErrNoResponse) && len(r.Units) == 0 && r.Channel == ch {
			err = nil // 単位が無いチャンネルは分類しないので応答が無くてよい
		}
		if err != nil {
			fmt.Fprintf(stderr, "slack-bbs: %s: %s\n", ch, clean(err.Error()))
			failed = append(failed, ch)
			continue
		}
		p := render.Page{Channel: r.Channel, ChannelName: r.ChannelName, Since: r.Since, Until: r.Until, Lookback: r.Lookback,
			Base: r.Base, Notes: r.Notes}
		if len(r.Units) > 0 {
			p.Topics = assemble(stderr, r.ChannelName, r.Units, resp)
		}
		if asJSON {
			pages = append(pages, p)
			continue
		}
		if err := writeMarkdown(stdout, p); err != nil {
			return err
		}
	}
	if asJSON {
		if pages == nil {
			pages = []render.Page{}
		}
		if err := writeJSON(stdout, pages); err != nil {
			return err
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("出せなかったチャンネル: %s (%s は消さずに残した)", strings.Join(failed, " "), dir)
	}
	if keep {
		return nil
	}
	left, err := job.Clean(dir, m)
	if left {
		fmt.Fprintf(stderr, "slack-bbs: 依頼と応答は消した。%s には他のファイルがあるので残した\n", clean(dir))
	}
	return err
}

func channelsToRead(o options, d deps) ([]subs.Channel, error) {
	path, err := d.subsPath()
	if err != nil {
		return nil, err
	}
	cur, err := subs.Load(path)
	if err != nil {
		return nil, err
	}
	if len(o.channels) == 0 {
		if len(cur) == 0 {
			return nil, errors.New("購読しているチャンネルが無い。slack-bbs sub で選ぶか、チャンネル ID を引数に渡す")
		}
		return cur, nil
	}
	known := map[string]string{}
	for _, c := range cur {
		known[c.ID] = c.Name
	}
	var out []subs.Channel
	for _, id := range o.channels {
		if strings.HasPrefix(id, "#") {
			return nil, fmt.Errorf("%s: チャンネルは名前でなく ID (C…) で渡す (名前で渡すと slack-cli がチャンネル一覧を保存する)", id)
		}
		name := known[id]
		if name == "" {
			name = id
		}
		out = append(out, subs.Channel{ID: id, Name: name})
	}
	return out, nil
}

// maxThreadErrNotes は、読めなかったスレッドを 1 本ずつ注記に出す上限。
const maxThreadErrNotes = 3

func collect(ctx context.Context, o options, cli source.Client, me source.Me, names map[string]string, ch subs.Channel) (collected, error) {
	sinceTs := board.TsOf(o.since.Unix())
	hist, err := cli.History(ctx, ch.ID, board.TsOf(o.lookback.Unix()))
	if err != nil {
		return collected{}, err
	}
	var notes []string
	if hist.MaybeOld {
		notes = append(notes, source.OldSlackCLINote)
	}
	if hist.Warning != "" {
		notes = append(notes, "slack-cli の警告 (history): "+hist.Warning)
	}
	threads := map[string][]source.Message{}
	failed := 0
	for _, ts := range board.NeedThreads(hist.Msgs, sinceTs) {
		th, err := cli.Thread(ctx, ch.ID, ts)
		if err != nil {
			if ctx.Err() != nil {
				return collected{}, err
			}
			// 1 本のスレッドが読めない (消えた等) だけでチャンネルごと止めない。そのスレッドは「読めなかった」印で出る。
			// 注記は最初の数本だけ (認証切れ等で全部が落ちると、同じ文が本数だけ並ぶ)
			if failed++; failed <= maxThreadErrNotes {
				notes = append(notes, "スレッド "+ts+" を読めなかった: "+termsafe.ClipRunes(termsafe.PlainLine(err.Error()), 200))
			}
			continue
		}
		threads[ts] = th.Msgs
		if th.Warning != "" {
			notes = append(notes, "slack-cli の警告 (スレッド "+ts+"): "+th.Warning)
		}
	}
	if failed > maxThreadErrNotes {
		notes = append(notes, fmt.Sprintf("ほか %d 本のスレッドも読めなかった", failed-maxThreadErrNotes))
	}
	units, err := board.Build(board.Input{History: hist.Msgs, Threads: threads, Since: sinceTs, Me: me.UserID, Names: names})
	if err != nil {
		return collected{}, err
	}
	c := collected{units: units, threads: len(threads), page: render.Page{
		Channel: ch.ID, ChannelName: ch.Name, Since: o.since, Until: o.until, Lookback: o.lookback, Base: me.URL,
	}}
	for _, u := range units {
		for _, m := range u.Msgs {
			c.sent++
			c.chars += len([]rune(m.Text))
		}
	}
	if hist.Truncated {
		c.page.Notes = append(c.page.Notes, fmt.Sprintf("history が上限 %d 件で切れた。-lookback を短くする", source.HistoryLimit))
	}
	c.page.Notes = append(c.page.Notes, notes...)
	return c, nil
}
