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

	"dupscan/internal/group"
	"dupscan/internal/norm"
	"dupscan/internal/repo"
	"dupscan/internal/report"
	"dupscan/internal/swift"
	"dupscan/internal/tree"
)

const usage = `usage: dupscan [report] [flags] [dir ...]
       dupscan gate [flags] [dir ...]

report: dir (既定は .) を起点に、git が追跡しているファイルと ignore されていない untracked のファイルを読み、
本体が同じ形の単位を段 (L1 / L2 / L3) ごとに組にして出す。
gate: base (既定は upstream との merge-base) から作業ツリー (か -head の commit) までに新しく増えた重複を出し、
止める段の違反があれば rc=1。base を決められないときは rc=2。セルフレビューと make test・CI で回す。
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
	verbose              *bool
}

func newFlags(name string, stderr io.Writer) (*flag.FlagSet, common) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	c := common{
		format:     fs.String("format", "text", "出力の形式: text / json / xcode"),
		minNodes:   fs.Int("min-nodes", 10, "これより節の少ない関数・初期化子は比べない"),
		minClosure: fs.Int("min-closure-nodes", 20, "これより節の少ないクロージャは比べない"),
		limit:      fs.Int("limit", 20, "text で出す組・違反の上限 (0 は全部)"),
		verbose:    fs.Bool("v", false, "読めない構文の単位を 1 つずつ出す"),
		levels:     fs.String("levels", "L1,L2,L3", "見る段 (カンマ区切り)"),
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

func (c common) options() (group.Options, error) {
	lv, err := parseLevels(*c.levels)
	return group.Options{MinNodes: *c.minNodes, MinClosureNodes: *c.minClosure, Levels: lv}, err
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "gate" {
		return runGate(args[1:], stdout, stderr)
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
	got, err := scanSource(repo.Worktree{Scope: sc, Exts: swift.Extensions}, true)
	if err != nil {
		fmt.Fprintln(stderr, "dupscan:", err)
		return 2
	}
	s := report.Build(sc.Root, got.files, got.units, got.errFiles, group.Build(got.entries, opt))
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

func parseLevels(s string) ([]norm.Level, error) {
	var out []norm.Level
	for _, f := range strings.Split(s, ",") {
		switch strings.TrimSpace(f) {
		case "L1":
			out = append(out, norm.L1)
		case "L2":
			out = append(out, norm.L2)
		case "L3":
			out = append(out, norm.L3)
		default:
			return nil, fmt.Errorf("知らない段 %q (L1 / L2 / L3)", f)
		}
	}
	return out, nil
}

// scanned は 1 つの読み方で読んだ結果。
type scanned struct {
	files    int
	units    []*tree.Unit
	errFiles []string
	entries  []*group.Entry
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
	units, errFiles, err := parseAll(files, contents)
	if err != nil {
		return scanned{}, err
	}
	entries := make([]*group.Entry, len(units))
	for i, u := range units {
		entries[i] = &group.Entry{Unit: u, Result: norm.Normalize(u)}
	}
	return scanned{files: len(files), units: units, errFiles: errFiles, entries: entries}, nil
}

func parseAll(files []string, contents map[string][]byte) ([]*tree.Unit, []string, error) {
	type result struct {
		units  []*tree.Unit
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
				results[i] = result{units: r.Units, hasErr: r.HasError, err: err}
			}
		}()
	}
	for i := range files {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	var units []*tree.Unit
	var errFiles []string
	for i, r := range results {
		if r.err != nil {
			return nil, nil, fmt.Errorf("%s: %w", files[i], r.err)
		}
		units = append(units, r.units...)
		if r.hasErr {
			errFiles = append(errFiles, files[i])
		}
	}
	return units, errFiles, nil
}
