package main

import (
	"dupscan/internal/lang"
	"flag"
	"fmt"
	"io"

	"dupscan/internal/langs"
	"dupscan/internal/repo"
	"dupscan/internal/report"
)

// runIndex は索引を引く。語を何も渡さなければ全部の単位を 1 行 1 件で出す。
func runIndex(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("dupscan index", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var q report.Query
	fs.StringVar(&q.Name, "name", "", "名前の一部 (大文字小文字を区別しない)")
	fs.StringVar(&q.Returns, "returns", "", "戻り値の型の一部")
	fs.StringVar(&q.ParamType, "param-type", "", "どれかの引数の型の一部")
	fs.StringVar(&q.Doc, "doc", "", "doc comment の最初の行の語")
	file := fs.String("file", "", "このファイルの単位を行の順に (outline)")
	format := fs.String("format", "text", "出力の形式: text / json")
	limit := fs.Int("limit", 20, "text で出す件数の上限 (0 は全部。-file を指したときの既定は全部)")
	fs.Usage = func() {
		fmt.Fprint(stderr, usage)
		fs.PrintDefaults()
	}
	dirs, rc, ok := parseInterspersedFlags(fs, args)
	if !ok {
		return rc
	}
	fail := func(err error) int {
		fmt.Fprintln(stderr, "dupscan index:", err)
		return 2
	}
	sc, err := repo.Resolve(dirs)
	if err != nil {
		return fail(err)
	}
	if *file != "" {
		abs, err := absPath(*file)
		if err != nil {
			return fail(err)
		}
		if q.File, err = repoRel(sc.Root, abs); err != nil {
			return fail(err)
		}
	}
	src := repo.Worktree{Scope: sc, Exts: langs.Extensions()}
	cfg, err := loadConfig(src, false)
	if err != nil {
		return fail(fmt.Errorf("判定不能: %w", err))
	}
	q.Shared = cfg.InShared
	got, err := scanSource(src, true, analysisPlan{Options: lang.Options{Calls: true}})
	if err != nil {
		return fail(err)
	}
	if q.File != "" && !flagSet(fs, "limit") {
		*limit = 0 // ファイル 1 つの一覧は切らない
	}
	s := report.BuildIndex(got.units, got.calls, q)
	switch *format {
	case "json":
		err = report.WriteJSON(stdout, s)
	case "text":
		_, err = fmt.Fprintln(stdout, got.callApproximation(true))
		if err == nil {
			err = report.IndexText(stdout, s, *limit)
		}
	default:
		return fail(fmt.Errorf("知らない形式 %q (text / json)", *format))
	}
	if err != nil {
		return fail(err)
	}
	if err := writeParseWarning(stderr, "index", got.errFiles); err != nil {
		return fail(err)
	}
	return 0
}

func flagSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) { set = set || f.Name == name })
	return set
}
