// Command dupscan は、repo の中で本体が同じ形のメソッド (関数・初期化子・クロージャ) を構文木で見つけて組にする。
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

dir (既定は .) を起点に、git が追跡しているファイルと ignore されていない untracked のファイルを読み、
本体が同じ形の単位を段 (L1 / L2 / L3) ごとに組にして出す。調べる repo には何も書かない。

flags:
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "report" {
		args = args[1:]
	}
	fs := flag.NewFlagSet("dupscan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "text", "出力の形式: text / json / xcode")
	minNodes := fs.Int("min-nodes", 10, "これより節の少ない関数・初期化子は比べない")
	minClosure := fs.Int("min-closure-nodes", 20, "これより節の少ないクロージャは比べない")
	limit := fs.Int("limit", 20, "text で出す組の上限 (0 は全部)")
	verbose := fs.Bool("v", false, "読めない構文の単位を 1 つずつ出す")
	levels := fs.String("levels", "L1,L2,L3", "組を出す段 (カンマ区切り)")
	fs.Usage = func() {
		fmt.Fprint(stderr, usage)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	lv, err := parseLevels(*levels)
	if err != nil {
		fmt.Fprintln(stderr, "dupscan:", err)
		return 2
	}
	dirs := fs.Args()
	if len(dirs) == 0 {
		dirs = []string{"."}
	}
	s, err := scan(dirs, group.Options{MinNodes: *minNodes, MinClosureNodes: *minClosure, Levels: lv})
	if err != nil {
		fmt.Fprintln(stderr, "dupscan:", err)
		return 2
	}
	switch *format {
	case "json":
		err = report.JSON(stdout, s)
	case "xcode":
		err = report.Xcode(stdout, s)
	case "text":
		err = report.Text(stdout, s, report.TextOptions{Limit: *limit, Verbose: *verbose})
	default:
		fmt.Fprintf(stderr, "dupscan: 知らない形式 %q\n", *format)
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

func scan(dirs []string, opt group.Options) (report.Summary, error) {
	sc, err := repo.Resolve(dirs)
	if err != nil {
		return report.Summary{}, err
	}
	files, err := repo.List(sc, swift.Extensions)
	if err != nil {
		return report.Summary{}, err
	}
	// 0 件は「重複なし」ではなく判定不能 (起点の取り違えで黙って緑にしない)。
	if len(files) == 0 {
		return report.Summary{}, fmt.Errorf("起点 %v に読むファイルが 1 つも無い (判定不能)", dirs)
	}
	units, errFiles, err := parseAll(sc.Root, files)
	if err != nil {
		return report.Summary{}, err
	}
	entries := make([]*group.Entry, len(units))
	for i, u := range units {
		entries[i] = &group.Entry{Unit: u, Result: norm.Normalize(u)}
	}
	return report.Build(sc.Root, len(files), units, errFiles, group.Build(entries, opt)), nil
}

func parseAll(root string, files []string) ([]*tree.Unit, []string, error) {
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
				src, err := os.ReadFile(filepath.Join(root, files[i]))
				if err != nil {
					results[i].err = err
					continue
				}
				r, err := p.Parse(files[i], src)
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
