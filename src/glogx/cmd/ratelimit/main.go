// ratelimit — Claude Code / codex の利用枠 (5h / weekly) を表示・判定する単独コマンド。
//
// 取得と整形は glogx/usage をそのまま使う (usage.go の doc が想定していた「FetchAll + RenderLine
// を呼ぶだけの main」の切り出し)。glogx 本体の TUI とは独立に、zsh・hook・skill から呼ぶ。
//
// 使い方:
//
//	ratelimit [-source claude|codex|all] [-json] [-check] [-cached] [-warn-5h N] [-warn-7d N]
//
//	-source   見る出所 (既定 all)。hook は claude だけ、codex 系 skill は codex だけを見る
//	-check    閾値を超えた枠だけを 1 行ずつ出す。rc: 0 = 超過なし / 1 = 超過あり / 3 = 判定不能
//	-cached   取得で待たない。キャッシュが古ければ裏で更新を起こし、手元の値で答える (hook 用)
//	-json     枠を JSON で出す
//
// キャッシュは出所ごとに ~/.cache/glog/ratelimit-<source>.json。glogx の claude-usage.json とは
// 分けている: あちらは「Claude 枠必須・codex 枠も揃っていること」を鮮度の契約にしているため、
// 片方の出所だけを取るこのコマンドが書き込むと、その契約を壊すか、古い codex 枠を新しい時刻で
// 上書きしてしまう。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"doctor/cachedir"
	"glogx/atomicfile"
	"glogx/subproc"
	"glogx/usage"
	"termsafe"

	"golang.org/x/term"
)

type source string

const (
	srcClaude source = "claude"
	srcCodex  source = "codex"
)

const (
	// cacheTTL: hook は毎プロンプト読むが、使用率は分単位でしか動かない。
	cacheTTL = 5 * time.Minute
	// refreshInterval: 裏の更新を起こす間隔の下限。取得が失敗し続ける環境 (未ログイン等) で、
	// プロンプトのたびに claude / codex を起こさないため。
	refreshInterval = time.Minute
	fetchTimeout    = 30 * time.Second
	// maxStale: 取得せずに / 取得に失敗して、古いキャッシュで答えてよい上限。裏の取得が失敗し続ける (未ログイン・
	// /usage の書式変更) と古い % で判定し続け、実際は超過していても黙るため、超えたら判定不能にする。
	maxStale = 30 * time.Minute
	// envRefreshing は裏の更新プロセスとその子 (claude -p /usage) に付ける印。`claude -p` の中でも
	// UserPromptSubmit hook が走りうるので、印があるときは更新を起こさない (再帰の連鎖を止める)。
	envRefreshing = "RATELIMIT_REFRESHING"
)

const (
	rcOK      = 0
	rcOver    = 1
	rcUsage   = 2
	rcUnknown = 3
)

type cacheEntry struct {
	Windows   []usage.Window `json:"windows"`
	FetchedAt time.Time      `json:"fetchedAt"`
}

// fresh / usable は未来の時刻を真にしない (時計の巻き戻しで古い値を使い続けないため。issue 201)。
func (e cacheEntry) fresh(now time.Time) bool { return e.ageWithin(now, cacheTTL) }

// usable は取得せずに (-cached) / 取得に失敗して、手元の値で答えてよいか。
func (e cacheEntry) usable(now time.Time) bool { return e.ageWithin(now, maxStale) }

func (e cacheEntry) ageWithin(now time.Time, limit time.Duration) bool {
	age := now.Sub(e.FetchedAt)
	return age >= 0 && age < limit
}

// env はテストで差し替える外界。
type env struct {
	now       time.Time
	cacheDir  string
	fetch     func(ctx context.Context, s source) ([]usage.Window, error)
	spawn     func(s source) error
	stdout    io.Writer
	stderr    io.Writer
	colored   bool
	getenv    func(string) string
	lookupBin func(string) bool
}

type sourceResult struct {
	src       source
	windows   []usage.Window
	fetchedAt time.Time
	err       error // 取得に失敗した (古い値で答えている / 値が無い)
}

func main() {
	os.Exit(run(os.Args[1:], env{
		now:       time.Now(),
		fetch:     fetchSource,
		spawn:     spawnRefresh,
		stdout:    os.Stdout,
		stderr:    os.Stderr,
		colored:   term.IsTerminal(int(os.Stdout.Fd())),
		getenv:    os.Getenv,
		lookupBin: hasBin,
	}))
}

func run(args []string, e env) int {
	fs := flag.NewFlagSet("ratelimit", flag.ContinueOnError)
	fs.SetOutput(e.stderr)
	srcFlag := fs.String("source", "all", "claude | codex | all")
	jsonOut := fs.Bool("json", false, "枠を JSON で出す")
	check := fs.Bool("check", false, "閾値を超えた枠だけを出す (rc 0=なし 1=あり 3=判定不能)")
	cached := fs.Bool("cached", false, "取得で待たない (古ければ裏で更新)")
	warn5h := fs.Int("warn-5h", 80, "5h 枠の警告閾値 (%)")
	warn7d := fs.Int("warn-7d", 85, "weekly 枠の警告閾値 (%)")
	refresh := fs.Bool("refresh", false, "(内部用) 取得してキャッシュへ書くだけ")
	if err := fs.Parse(args); err != nil {
		return rcUsage
	}
	srcs, err := parseSources(*srcFlag)
	if err != nil {
		fmt.Fprintln(e.stderr, "ratelimit:", err)
		return rcUsage
	}
	if e.cacheDir == "" {
		d, err := cachedir.Base()
		if err != nil {
			fmt.Fprintln(e.stderr, "ratelimit: cache dir:", err)
			return rcUnknown
		}
		e.cacheDir = d
	}

	if *refresh {
		rc := rcOK
		for _, s := range srcs {
			if _, err := fetchAndSave(e, s); err != nil {
				fmt.Fprintf(e.stderr, "ratelimit: %s: %v\n", s, err)
				rc = rcUnknown
			}
		}
		return rc
	}

	// -source all でも、入っていない CLI の出所は黙って外す (codex 未導入の環境で毎回失敗を出さない)。
	// 名指しされた出所は外さない (見に来た人に「無い」を伝える)。
	if *srcFlag == "all" {
		kept := srcs[:0]
		for _, s := range srcs {
			if e.lookupBin(string(s)) {
				kept = append(kept, s)
			}
		}
		srcs = kept
	}

	results := make([]sourceResult, 0, len(srcs))
	for _, s := range srcs {
		results = append(results, resolve(e, s, *cached))
	}

	switch {
	case *jsonOut:
		return printJSON(e, results)
	case *check:
		return printCheck(e, results, *warn5h, *warn7d)
	default:
		return printLine(e, results)
	}
}

func parseSources(v string) ([]source, error) {
	switch v {
	case "claude":
		return []source{srcClaude}, nil
	case "codex":
		return []source{srcCodex}, nil
	case "all":
		return []source{srcClaude, srcCodex}, nil
	}
	return nil, fmt.Errorf("-source は claude / codex / all のいずれか: %q", v)
}

// resolve はキャッシュを優先して 1 出所ぶんの枠を返す。
func resolve(e env, s source, cached bool) sourceResult {
	entry, ok := loadCache(cachePath(e.cacheDir, s))
	if ok && entry.fresh(e.now) {
		return sourceResult{src: s, windows: entry.Windows, fetchedAt: entry.FetchedAt}
	}
	if cached {
		if err := maybeSpawn(e, s); err != nil {
			fmt.Fprintf(e.stderr, "ratelimit: %s: 裏の更新を起こせない: %v\n", s, err)
		}
		if ok && entry.usable(e.now) {
			return sourceResult{src: s, windows: entry.Windows, fetchedAt: entry.FetchedAt}
		}
		return sourceResult{src: s, err: errors.New("新しいキャッシュが無い (裏で取得中)")}
	}
	got, err := fetchAndSave(e, s)
	if err == nil {
		return sourceResult{src: s, windows: got.Windows, fetchedAt: got.FetchedAt}
	}
	if ok && entry.usable(e.now) {
		return sourceResult{src: s, windows: entry.Windows, fetchedAt: entry.FetchedAt, err: err}
	}
	return sourceResult{src: s, err: err}
}

func fetchAndSave(e env, s source) (cacheEntry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	ws, err := e.fetch(ctx, s)
	if err != nil {
		return cacheEntry{}, err
	}
	for i := range ws {
		ws[i].Label = termsafe.PlainLine(ws[i].Label)
	}
	entry := cacheEntry{Windows: ws, FetchedAt: e.now}
	// 保存の失敗は取得結果を捨てる理由にしない (次回が取り直しになるだけ)。
	if err := saveCache(e.cacheDir, s, entry); err != nil {
		fmt.Fprintf(e.stderr, "ratelimit: %s: キャッシュを書けない: %v\n", s, err)
	}
	return entry, nil
}

func saveCache(dir string, s source, entry cacheEntry) error {
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return atomicfile.Write(cachePath(dir, s), data, 0o600)
}

func cachePath(dir string, s source) string {
	return filepath.Join(dir, "ratelimit-"+string(s)+".json")
}

func stampPath(dir string, s source) string {
	return filepath.Join(dir, "ratelimit-"+string(s)+".refresh")
}

func loadCache(path string) (cacheEntry, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return cacheEntry{}, false
	}
	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil || len(entry.Windows) == 0 {
		return cacheEntry{}, false
	}
	// 表示に載る文字列は入口で 1 回 termsafe を通す (src/glogx/CLAUDE.md。キャッシュは書き換えうる)。
	for i := range entry.Windows {
		entry.Windows[i].Label = termsafe.PlainLine(entry.Windows[i].Label)
	}
	return entry, true
}

// maybeSpawn は refreshInterval に 1 回だけ裏の更新を起こす。複数セッションが同時に起こす
// 競合は許す (claude / codex が 2 回起きるだけで、キャッシュは atomic に置き換わる)。
func maybeSpawn(e env, s source) error {
	if e.getenv(envRefreshing) != "" {
		return nil
	}
	stamp := stampPath(e.cacheDir, s)
	if fi, err := os.Stat(stamp); err == nil {
		if age := e.now.Sub(fi.ModTime()); age >= 0 && age < refreshInterval {
			return nil
		}
	}
	if err := os.MkdirAll(e.cacheDir, 0o700); err != nil {
		return err
	}
	if err := atomicfile.Write(stamp, nil, 0o600); err != nil {
		return err
	}
	// atomicfile は temp + rename なので、mtime は書いた瞬間になる。テストの時刻と揃える。
	_ = os.Chtimes(stamp, e.now, e.now)
	return e.spawn(s)
}

func spawnRefresh(s source) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	// Wait しない切り離しの起動。stdio は nil (= /dev/null) なので、呼び出し元 (hook) の
	// パイプを握らず、WaitDelay の出番は無い (subproc.go の doc: パイプを持たない実行は事情が違う)。
	cmd := subproc.CommandContext(context.Background(), self, "-refresh", "-source", string(s))
	cmd.Env = refreshEnv(os.Environ()) // この配線自体はテストに無い (実プロセスを起こすため)。消すと再帰止めが外れる
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// refreshEnv は裏の更新プロセスの環境。印を付けないと、その子の claude -p が走らせる hook から
// また更新が起き、連鎖する (読む側は maybeSpawn)。
func refreshEnv(base []string) []string {
	return append(base[:len(base):len(base)], envRefreshing+"=1")
}

func fetchSource(ctx context.Context, s source) ([]usage.Window, error) {
	if s == srcCodex {
		return usage.FetchCodex(ctx)
	}
	snap, err := usage.Fetch(ctx)
	if err != nil {
		return nil, err
	}
	return snap.Windows, nil
}

func hasBin(name string) bool {
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if fi, err := os.Stat(filepath.Join(dir, name)); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return true
		}
	}
	return false
}

// overLimit は閾値を超えた枠と、判定できた枠の数を返す。リセット済み (キャッシュ後に境界を
// 跨いだ) の枠・未消費の枠・長さの分からない枠は判定しない。
func overLimit(r sourceResult, now time.Time, warn5h, warn7d int) (out []usage.Window, judged int) {
	for _, w := range r.windows {
		if w.Unused || !w.ResetAt.After(now) {
			continue
		}
		var limit int
		switch w.Span() {
		case 5 * time.Hour:
			limit = warn5h
		case 7 * 24 * time.Hour:
			limit = warn7d
		default:
			continue
		}
		judged++
		if w.Percent >= limit {
			out = append(out, w)
		}
	}
	return out, judged
}

func printCheck(e env, results []sourceResult, warn5h, warn7d int) int {
	over, known := false, false
	for _, r := range results {
		if r.err != nil {
			fmt.Fprintf(e.stderr, "ratelimit: %s: %v\n", r.src, r.err)
		}
		over1, judged := overLimit(r, e.now, warn5h, warn7d)
		if judged > 0 {
			known = true
		}
		for _, w := range over1 {
			over = true
			fmt.Fprintf(e.stdout, "%s %s %d%% (%s にリセット)\n",
				r.src, w.Label, w.Percent, formatReset(w.ResetAt, e.now))
		}
	}
	// 🚨 出所をまたいで OR を取るので、-source all では片方の判定不能がもう片方の「超過なし」に
	// 隠れる。呼び出し側 (hook = claude だけ / codex 系 skill = codex だけ) は出所を 1 つに絞って呼ぶ。
	switch {
	case over:
		return rcOver
	case known:
		return rcOK
	}
	return rcUnknown
}

func printLine(e env, results []sourceResult) int {
	if len(results) == 0 {
		fmt.Fprintln(e.stderr, "ratelimit: claude / codex のどちらの CLI も見つからない")
		return rcUnknown
	}
	rc := rcOK
	for _, r := range results {
		if r.err != nil {
			fmt.Fprintf(e.stderr, "ratelimit: %s: %v\n", r.src, r.err)
		}
		if len(r.windows) == 0 {
			rc = rcUnknown
			continue
		}
		snap := &usage.Snapshot{Windows: r.windows}
		line := usage.RenderLine(snap, e.now, e.colored)
		if r.src == srcClaude {
			// RenderLine は Claude を 5h / 7d だけ選ぶ。モデル別の週枠 (7d(Fable) 等) は足す。
			var extra []string
			for _, w := range r.windows {
				if w.Source == "" && strings.HasPrefix(w.Label, "7d(") && !w.Unused {
					extra = append(extra, fmt.Sprintf("%s:%d%%", w.Label, w.Percent))
				}
			}
			if len(extra) > 0 {
				line += " " + strings.Join(extra, " ")
			}
		}
		fmt.Fprintf(e.stdout, "%-6s %s  [%s]\n", r.src, line, formatAge(e.now.Sub(r.fetchedAt)))
	}
	return rc
}

type jsonWindow struct {
	Source     string    `json:"source"`
	Label      string    `json:"label"`
	Percent    int       `json:"percent"`
	ResetAt    time.Time `json:"resetAt,omitzero"`
	WindowMins int64     `json:"windowMins,omitempty"`
	Unused     bool      `json:"unused,omitempty"`
	FetchedAt  time.Time `json:"fetchedAt"`
}

func printJSON(e env, results []sourceResult) int {
	out := struct {
		Windows []jsonWindow `json:"windows"`
		Errors  []string     `json:"errors,omitempty"`
	}{Windows: []jsonWindow{}}
	rc := rcOK
	if len(results) == 0 {
		out.Errors = append(out.Errors, "claude / codex のどちらの CLI も見つからない")
		rc = rcUnknown
	}
	for _, r := range results {
		if r.err != nil {
			out.Errors = append(out.Errors, fmt.Sprintf("%s: %v", r.src, r.err))
		}
		if len(r.windows) == 0 {
			rc = rcUnknown
		}
		for _, w := range r.windows {
			out.Windows = append(out.Windows, jsonWindow{
				Source: string(r.src), Label: w.Label, Percent: w.Percent, ResetAt: w.ResetAt,
				WindowMins: w.WindowMins, Unused: w.Unused, FetchedAt: r.fetchedAt,
			})
		}
	}
	enc := json.NewEncoder(e.stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return rcUnknown
	}
	return rc
}

func formatReset(t, now time.Time) string {
	t = t.Local()
	if y1, m1, d1 := t.Date(); y1 == now.Year() && m1 == now.Month() && d1 == now.Day() {
		return t.Format("15:04")
	}
	return t.Format("1/2 15:04")
}

func formatAge(d time.Duration) string {
	switch {
	case d < 0:
		return "取得時刻が未来"
	case d < time.Minute:
		return "今取得"
	case d < time.Hour:
		return fmt.Sprintf("%d分前に取得", int(d/time.Minute))
	}
	return fmt.Sprintf("%d時間前に取得", int(d/time.Hour))
}
