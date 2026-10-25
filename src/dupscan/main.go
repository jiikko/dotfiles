// Command dupscan は、repo の中で本体が同じ形のメソッド (関数・初期化子・クロージャ) を構文木で見つけて組にする。
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"

	"dupscan/internal/exclude"
	"dupscan/internal/group"
	"dupscan/internal/norm"
	"dupscan/internal/repo"
	"dupscan/internal/report"
	"dupscan/internal/swift"
	"dupscan/internal/tree"
)

const usage = `usage: dupscan [report] [flags] [dir ...]
       dupscan gate [flags] [dir ...]
       dupscan near [flags] <file>:<関数名|行> [dir ...]
       dupscan explain [flags] <file>:<関数名|行>
       dupscan index [-name 語] [-returns 型] [-param-type 型] [-doc 語] [-file ファイル] [dir ...]
       dupscan callers [flags] <名前|名前(ラベル:…)> [dir ...]

report: dir (既定は .) を起点に、git が追跡しているファイルと ignore されていない untracked のファイルを読み、
本体が同じ形の単位を段 (L1 / L2 / L3) ごとに組にして出す。
gate: base (既定は upstream との merge-base) から作業ツリー (か -head の commit) までに新しく増えた重複を出し、
止める段の違反があれば rc=1。base を決められないときは rc=2。セルフレビューと make test・CI で回す。
near: 指した単位と同じ形の単位を、一致した最も細かい段で並べる (起点を書かなければそのファイルの repo 全体と比べる)。
explain: 指した単位を段ごとに潰した形・ハッシュと、比べない理由を出す (なぜ同じ / 違うと判定したか)。
index: 名前の付いた単位を 1 行 1 件で出す。書く前に、名前・戻り値の型・引数の型・doc の語で同じものが既にないかを引く。-file でそのファイルの一覧。
callers: 名前 (と引数ラベル) が当たる呼び出しの箇所と、それを囲む単位を出す (型を解決しない近似)。
どちらも調べる repo には何も書かない。

flags:
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// common は report と gate で共通の flag。
type common struct {
	format, levels       *string
	minNodes, minClosure *int
	limit                *int
	verbose, noExclude   *bool
}

func newFlags(name string, stderr io.Writer) (*flag.FlagSet, common) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	c := common{
		format:     fs.String("format", "text", "出力の形式: text / json / xcode"),
		minNodes:   fs.Int("min-nodes", defaultMinNodes, "これより節の少ない関数・初期化子・property は比べない"),
		minClosure: fs.Int("min-closure-nodes", defaultMinClosure, "これより節の少ないクロージャは比べない"),
		limit:      fs.Int("limit", 20, "text で出す組・違反の上限 (0 は全部)"),
		verbose:    fs.Bool("v", false, "読めない構文の単位を 1 つずつ出す"),
		levels:     fs.String("levels", defaultLevels, "見る段 (カンマ区切り。L3 は値の位置まで畳む一致で、偶然の一致が多いので既定では見ない)"),
		noExclude:  fs.Bool("no-exclude", false, "repo の root の "+exclude.FileName+" の除外を外して回す (除外が正しいかを見直すため)"),
	}
	fs.Usage = func() {
		fmt.Fprint(stderr, usage)
		fs.PrintDefaults()
	}
	return fs, c
}

func parseFlags(fs *flag.FlagSet, args []string) (dirs []string, rc int, ok bool) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, 0, false
		}
		return nil, 2, false
	}
	dirs = fs.Args()
	if len(dirs) == 0 {
		dirs = []string{"."}
	}
	return dirs, 0, true
}

// narrowed は、段か下限を既定から変えた走査か (組が作られないだけの除外を「効いていない」と言えない)。
// 既定の下限と段。下限を 20 節より下げると、1 行の委譲などの雑音の組が増える。L3 は値の位置まで畳むので偶然の一致が多い。
const (
	defaultMinNodes   = 20
	defaultMinClosure = 20
	defaultLevels     = "L1,L2"
)

// narrowed は、既定より狭い走査 (既定の段を見ない・下限を既定より上げた) か (組が作られないだけの除外を「効いていない」と言えない)。
// 既定より広い走査 (L3 も見る・下限を下げる) は狭くない。
func (c common) narrowed() bool {
	lv, err := parseLevels(*c.levels)
	if err != nil {
		return true
	}
	set := map[norm.Level]bool{}
	for _, l := range lv {
		set[l] = true
	}
	def, _ := parseLevels(defaultLevels)
	for _, l := range def {
		if !set[l] {
			return true
		}
	}
	return *c.minNodes > defaultMinNodes || *c.minClosure > defaultMinClosure
}

func (c common) options() (group.Options, error) {
	lv, err := parseLevels(*c.levels)
	return group.Options{MinNodes: *c.minNodes, MinClosureNodes: *c.minClosure, Levels: lv}, err
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "gate" {
		return runGate(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "callers" {
		return runCallers(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "index" {
		return runIndex(args[1:], stdout, stderr)
	}
	if len(args) > 0 && (args[0] == "near" || args[0] == "explain") {
		return runPoint(args[0], args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "report" {
		args = args[1:]
	}
	fs, c := newFlags("dupscan", stderr)
	dirs, rc, ok := parseFlags(fs, args)
	if !ok {
		return rc
	}
	opt, err := c.options()
	if err != nil {
		fmt.Fprintln(stderr, "dupscan:", err)
		return 2
	}
	sc, err := repo.Resolve(dirs)
	if err != nil {
		fmt.Fprintln(stderr, "dupscan:", err)
		return 2
	}
	src := repo.Worktree{Scope: sc, Exts: swift.Extensions}
	cfg, err := loadConfig(src, *c.noExclude)
	if err != nil {
		fmt.Fprintln(stderr, "dupscan: 判定不能:", err)
		return 2
	}
	got, err := scanSource(src, true)
	if err != nil {
		fmt.Fprintln(stderr, "dupscan:", err)
		return 2
	}
	cfg, err = withComments(cfg, got.entries, *c.noExclude)
	if err != nil {
		fmt.Fprintln(stderr, "dupscan: 判定不能:", err)
		return 2
	}
	ex := exclude.New(cfg, got.entries, scopeFilter(sc))
	if c.narrowed() {
		ex.Narrowed()
	}
	ex.Scanned(opt.Levels)
	s := report.Build(sc.Root, got.files, got.units, got.errFiles, ex.ApplyGroups(group.Build(got.entries, opt)), ex)
	switch *c.format {
	case "json":
		err = report.JSON(stdout, s)
	case "xcode":
		err = report.Xcode(stdout, s)
	case "text":
		err = report.Text(stdout, s, report.TextOptions{Limit: *c.limit, Verbose: *c.verbose})
	default:
		fmt.Fprintf(stderr, "dupscan: 知らない形式 %q\n", *c.format)
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, "dupscan:", err)
		return 2
	}
	return 0
}

// loadConfig は repo の root の除外の設定を読む。無ければ nil (除外無し)。読めない・形が違うときはエラー (黙って除外無しで回さない)。
// noExclude でも形は確かめる (誤りを -no-exclude で隠さない)。
func loadConfig(src repo.Source, noExclude bool) (*exclude.Config, error) {
	b, ok, err := src.ReadRoot(exclude.FileName)
	if err != nil || !ok {
		return nil, err
	}
	cfg, err := exclude.Parse(b)
	if err != nil || noExclude {
		return nil, err
	}
	return cfg, nil
}

// withComments は cfg に単位の直前のコメントの許可を足す。種別か理由の無いコメントはエラー (設定ファイルの誤りと同じく黙って外さない)。
// noExclude ならコメントも効かせない (形は確かめる)。
func withComments(cfg *exclude.Config, entries []*group.Entry, noExclude bool) (*exclude.Config, error) {
	rules, errs := exclude.CommentRules(entries)
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	if noExclude {
		return cfg, nil
	}
	return exclude.WithComments(cfg, rules), nil
}

// scopeFilter は、起点が repo 全体でなければ起点の中かを返す関数 (起点の外を指す除外を効いていないに数えないため)。
func scopeFilter(sc repo.Scope) func(string) bool {
	if sc.Whole() {
		return nil
	}
	return sc.Contains
}

func parseLevels(s string) ([]norm.Level, error) {
	var out []norm.Level
	for _, f := range strings.Split(s, ",") {
		l, err := norm.ParseLevel(f)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, nil
}

// scanned は 1 つの読み方で読んだ結果。
type scanned struct {
	files    int
	units    []*tree.Unit
	errFiles []string
	entries  []*group.Entry
	calls    []tree.Call
}

// scanSource は src のファイルを読んで単位を正規化する。requireFiles なら 0 件を判定不能にする
// (起点の取り違えで黙って「重複なし」にしない。base の側は 0 件でもよい: まだ Swift が無かった commit)。
func scanSource(src repo.Source, requireFiles bool) (scanned, error) {
	files, err := src.Files()
	if err != nil {
		return scanned{}, err
	}
	if requireFiles && len(files) == 0 {
		return scanned{}, fmt.Errorf("%s に読むファイルが 1 つも無い (判定不能)", src.Name())
	}
	contents, err := src.ReadAll(files)
	if err != nil {
		return scanned{}, err
	}
	units, calls, errFiles, err := parseAll(files, contents)
	if err != nil {
		return scanned{}, err
	}
	entries := make([]*group.Entry, len(units))
	for i, u := range units {
		entries[i] = &group.Entry{Unit: u, Result: norm.Normalize(u)}
	}
	return scanned{files: len(files), units: units, errFiles: errFiles, entries: entries, calls: calls}, nil
}

func parseAll(files []string, contents map[string][]byte) ([]*tree.Unit, []tree.Call, []string, error) {
	type result struct {
		units  []*tree.Unit
		calls  []tree.Call
		hasErr bool
		err    error
	}
	results := make([]result, len(files))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range runtime.NumCPU() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := swift.NewParser()
			if err != nil {
				for i := range jobs {
					results[i].err = err
				}
				return
			}
			defer p.Close()
			for i := range jobs {
				r, err := p.Parse(files[i], contents[files[i]])
				results[i] = result{units: r.Units, calls: r.Calls, hasErr: r.HasError, err: err}
			}
		}()
	}
	for i := range files {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	var units []*tree.Unit
	var calls []tree.Call
	var errFiles []string
	for i, r := range results {
		if r.err != nil {
			return nil, nil, nil, fmt.Errorf("%s: %w", files[i], r.err)
		}
		units = append(units, r.units...)
		calls = append(calls, r.calls...)
		if r.hasErr {
			errFiles = append(errFiles, files[i])
		}
	}
	return units, calls, errFiles, nil
}
