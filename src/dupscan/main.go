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
	"dupscan/internal/lang"
	"dupscan/internal/langs"
	"dupscan/internal/norm"
	"dupscan/internal/repo"
	"dupscan/internal/report"
	"dupscan/internal/tree"
)

const usage = `usage: dupscan [report] [flags] [dir ...]
       dupscan gate [flags] [dir ...]
       dupscan sweep [flags] [dir ...]
       dupscan near [flags] <file>:<関数名|行> [dir ...]
       dupscan explain [flags] <file>:<関数名|行> [-compare <file>:<関数名|行>]
       dupscan search -query "terms" [-returns Type] [flags] [dir ...]
       dupscan index [-name 語] [-returns 型] [-param-type 型] [-doc 語] [-file ファイル] [dir ...]
       dupscan callers [flags] <名前|名前(ラベル:…)> [dir ...]
       dupscan def [flags] <名前|Type.member|名前(ラベル:…)> [dir ...]
       dupscan refs [flags] <名前> [dir ...]
       dupscan context [flags] <名前|Type.member|名前(ラベル:…)> [dir ...]
       dupscan evidence [flags] <語> [dir ...]
       dupscan capabilities [-format text|json]
       dupscan doctor [-repo dir] [-base ref] [-format text|json]
       dupscan agent-guide -target claude|codex [-lang all|swift|go] -print

report: dir (既定は .) を起点に、git が追跡しているファイルと ignore されていない untracked のファイルを読み、
本体が同じ形の単位を段 (L1 / L2 / L3) ごとに組にして出す。
gate: base (既定は upstream との merge-base) から作業ツリー (か -head の commit) までに新しく増えた重複を出し、
止める段の違反があれば rc=1。base を決められないときは rc=2。セルフレビューと make test・CI で回す。
sweep: 重複を掘る 5 つの見方 (既定の report / 下限 12 節と L1m / 部分木の類似 / 名前の束 / 手で展開した helper) を 1 回の走査でまとめて出す。初めて入る module を掘るときの入口。
near: 指した単位と同じ形の単位を、一致した最も細かい段で並べる (起点を書かなければそのファイルの repo 全体と比べる)。
explain: 指した単位を段ごとに潰した形・ハッシュと、比べない理由を出す (なぜ同じ / 違うと判定したか)。
index: 名前の付いた単位を 1 行 1 件で出す。書く前に、名前・戻り値の型・引数の型・doc の語で同じものが既にないかを引く。-file でそのファイルの一覧。
callers: 名前 (と引数ラベル) が当たる呼び出しの箇所と、それを囲む単位を出す (型を解決しない近似)。
def: 完全一致する定義を本体つきで出す。型は宣言とメンバー一覧、Type.member と引数ラベルで絞れる。
refs: 型・式の位置にある識別子の参照を、それを囲む単位つきで出す (コメント・文字列は除く)。
context: def・refs・callers を 1 回の走査でまとめて出す (変更の前に定義と使われ方を見る。参照・呼び出しは名前だけで当てた近似。text だけ)。
evidence: 名前の分からない実装を、doc の全文・テスト・ソースの行にある語から探す (語の一致。doc ならその単位、行ならその行の呼び出しを添える。text だけ)。
どの subcommand も、調べる repo には何も書かない。

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
	fs := commandFlags(name, stderr)
	c := common{
		format:     fs.String("format", "text", "出力の形式: text / json / xcode"),
		minNodes:   fs.Int("min-nodes", defaultMinNodes, "これより節の少ない関数・初期化子・property は比べない"),
		minClosure: fs.Int("min-closure-nodes", defaultMinClosure, "これより節の少ないクロージャは比べない"),
		limit:      fs.Int("limit", 20, "text で出す組・違反の上限 (0 は全部)"),
		verbose:    fs.Bool("v", false, "読めない構文の単位を 1 つずつ出す"),
		levels:     fs.String("levels", defaultLevels, "見る段 (カンマ区切り。L3 は値の位置まで畳む一致で、偶然の一致が多いので既定では見ない)"),
		noExclude:  fs.Bool("no-exclude", false, "repo の root の "+exclude.FileName+" の除外を外して回す (除外が正しいかを見直すため)"),
	}
	return fs, c
}

// commandFlags は stderr に出し、-h で共通の usage に続けて flag の一覧を出す FlagSet を作る。
func commandFlags(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, usage)
		fs.PrintDefaults()
	}
	return fs
}

// failer は「<command>: <err>」を stderr に出して rc=2 を返す関数を作る (入力・実行のエラーはどのサブコマンドも rc=2)。
func failer(stderr io.Writer, command string) func(error) int {
	return func(err error) int {
		fmt.Fprintln(stderr, command+":", err)
		return 2
	}
}

func parseOrderedFlags(fs *flag.FlagSet, args []string) (dirs []string, rc int, ok bool) {
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

// parseFlags は flag を名前・パスの前後どちらでも受ける。後置を受けないサブコマンドを作らないよう、位置引数を取るサブコマンドはこれを通す
// (flag.Parse は最初の名前・パスで止まり、後ろの `-v` をパスとして読んで「repo の中ではない」の誤った案内になる)。
func parseFlags(fs *flag.FlagSet, args []string) (dirs []string, rc int, ok bool) {
	flags := make([]string, 0, len(args))
	positionals := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if len(arg) > 1 && arg[0] == '-' {
			flags = append(flags, arg)
			name, _, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
			if hasValue {
				continue
			}
			if f := fs.Lookup(name); f != nil {
				boolFlag, isBool := f.Value.(interface{ IsBoolFlag() bool })
				if !isBool || !boolFlag.IsBoolFlag() {
					if i+1 == len(args) {
						return parseOrderedFlags(fs, flags)
					}
					i++
					flags = append(flags, args[i])
				}
			}
			continue
		}
		positionals = append(positionals, arg)
	}
	return parseOrderedFlags(fs, append(append(flags, "--"), positionals...))
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
	if len(args) > 0 {
		for _, spec := range commandSpecs() {
			if args[0] == spec.Name {
				return spec.Run(args[1:], stdout, stderr)
			}
		}
	}
	return runReportCommand(args, stdout, stderr)
}

func runReportCommand(args []string, stdout, stderr io.Writer) int {
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
	src := repo.Worktree{Scope: sc, Exts: langs.Extensions()}
	cfg, err := loadConfig(src, *c.noExclude)
	if err != nil {
		fmt.Fprintln(stderr, "dupscan: 判定不能:", err)
		return 2
	}
	got, err := scanSource(src, true, analysisPlan{Normalize: true})
	if err != nil {
		fmt.Fprintln(stderr, "dupscan:", err)
		return 2
	}
	entries, err := got.cloneEntries()
	if err != nil {
		fmt.Fprintln(stderr, "dupscan:", err)
		return 2
	}
	cfg, err = withComments(cfg, entries, *c.noExclude)
	if err != nil {
		fmt.Fprintln(stderr, "dupscan: 判定不能:", err)
		return 2
	}
	ex := exclude.New(cfg, entries, scopeFilter(sc))
	if c.narrowed() {
		ex.Narrowed()
	}
	ex.Scanned(opt.Levels)
	s := report.Build(sc.Root, got.files, got.units, got.errFiles, ex.ApplyGroups(group.Build(entries, opt)), ex)
	s.BelowMinNodes, s.MinNodes = group.CountBelowMin(entries, opt), opt.MinNodes
	s.BelowMinFloor = sweepDeepMinNodes
	s.BelowMinGroups = group.CountBelowMinGroups(entries, opt, s.BelowMinFloor)
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
	files       int
	units       []*tree.Unit
	errFiles    []string
	entries     []*group.Entry
	normalized  bool
	calls       []tree.Call
	definitions []lang.Definition
	types       []lang.TypeDeclaration
	references  []lang.Reference
	contents    map[string][]byte
}

// analysisPlan keeps metadata collection independent of clone normalization.
type analysisPlan struct {
	lang.Options
	Normalize bool
}

// cloneEntries rejects metadata-only scans rather than reporting false clone absence.
func (s scanned) cloneEntries() ([]*group.Entry, error) {
	if !s.normalized {
		return nil, errors.New("clone consumer requires normalization")
	}
	return s.entries, nil
}

// scanSource reads the complete scope; metadata queries retain cross-file call counts.
func scanSource(src repo.Source, requireFiles bool, plan analysisPlan) (scanned, error) {
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
	parsed, err := parseAll(files, contents, plan)
	if err != nil {
		return scanned{}, err
	}

	return scanned{
		files: len(files), units: parsed.units, errFiles: parsed.errFiles, entries: parsed.entries, normalized: plan.Normalize, calls: parsed.calls,
		definitions: parsed.definitions, types: parsed.types, references: parsed.references, contents: contents,
	}, nil
}

// parseAll keeps parsing and optional clone normalization in the same worker.
// 結果はファイルの順 (files の順) に並べる。
type parsedFiles struct {
	units       []*tree.Unit
	entries     []*group.Entry
	calls       []tree.Call
	definitions []lang.Definition
	types       []lang.TypeDeclaration
	references  []lang.Reference
	errFiles    []string
}

func parseAll(files []string, contents map[string][]byte, plan analysisPlan) (parsedFiles, error) {
	type result struct {
		units       []*tree.Unit
		entries     []*group.Entry
		calls       []tree.Call
		definitions []lang.Definition
		types       []lang.TypeDeclaration
		references  []lang.Reference
		hasErr      bool
		err         error
	}
	results := make([]result, len(files))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range runtime.NumCPU() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			options := plan.Options
			options.MetadataOnly = !plan.Normalize
			p := langs.NewParsers(options)
			defer p.Close()
			for i := range jobs {
				r, err := p.Parse(files[i], contents[files[i]])
				var entries []*group.Entry
				if plan.Normalize {
					entries = make([]*group.Entry, len(r.Units))
					for j, u := range r.Units {
						entries[j] = &group.Entry{Unit: u, Result: norm.Normalize(u)}
					}
				}
				results[i] = result{units: r.Units, entries: entries, calls: r.Calls, definitions: r.Definitions, types: r.Types, references: r.References, hasErr: r.HasError, err: err}
			}
		}()
	}
	for i := range files {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	var parsed parsedFiles
	for i, r := range results {
		if r.err != nil {
			return parsedFiles{}, fmt.Errorf("%s: %w", files[i], r.err)
		}
		parsed.units = append(parsed.units, r.units...)
		parsed.entries = append(parsed.entries, r.entries...)
		parsed.calls = append(parsed.calls, r.calls...)
		parsed.definitions = append(parsed.definitions, r.definitions...)
		parsed.types = append(parsed.types, r.types...)
		parsed.references = append(parsed.references, r.references...)
		if r.hasErr {
			parsed.errFiles = append(parsed.errFiles, files[i])
		}
	}
	return parsed, nil
}

// symbolSource は def / refs が読む走査 (定義と参照を集める言語のファイルだけ) と、読まなかった言語のファイルの説明 (無ければ空)。
// 集めない言語のファイルを黙って読み飛ばすと、そこにある定義が「無い」と読める。
func symbolSource(sc repo.Scope) (repo.Worktree, string, error) {
	exts, skipped := langs.Symbols()
	var notes []string
	for _, a := range skipped {
		files, err := repo.List(sc, a.Extensions)
		if err != nil {
			return repo.Worktree{}, "", err
		}
		if len(files) > 0 {
			notes = append(notes, fmt.Sprintf("%s のファイル %d 個", a.Name, len(files)))
		}
	}
	note := ""
	if len(notes) > 0 {
		note = strings.Join(notes, "・") + "は def / refs の対象外 (その言語の定義・参照は LSP で引く)"
	}
	return repo.Worktree{Scope: sc, Exts: exts}, note, nil
}

// withNote は err に読まなかった言語の説明を足す (Go だけの repo で def を打つと「読むファイルが無い」になる理由を添える)。
func withNote(err error, note string) error {
	if note == "" {
		return err
	}
	return fmt.Errorf("%w。%s", err, note)
}
