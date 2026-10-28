package main

import (
	"dupscan/internal/lang"
	"flag"
	"fmt"
	"io"
	"regexp"
	"strings"

	"dupscan/internal/langs"
	"dupscan/internal/repo"
	"dupscan/internal/report"
	"dupscan/internal/tree"
)

// runIndex は索引を引く。語を何も渡さなければ全部の単位を 1 行 1 件で出す。
func runIndex(args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("dupscan index", stderr)
	var q report.Query
	fs.StringVar(&q.Name, "name", "", "名前の一部 (大文字小文字を区別しない)")
	fs.StringVar(&q.Returns, "returns", "", "戻り値の型の一部")
	fs.StringVar(&q.ParamType, "param-type", "", "どれかの引数の型の一部")
	fs.StringVar(&q.Doc, "doc", "", "doc comment の最初の行の語")
	kinds := fs.String("kind", "", "単位の種類で絞る (カンマ区切り: func / init / property。property は computed property・subscript・deinit)。既定は全部")
	file := fs.String("file", "", "このファイルの単位を行の順に (outline)")
	format := fs.String("format", "text", "出力の形式: text / json")
	limit := fs.Int("limit", 20, "text で出す件数の上限 (0 は全部。-file を指したときの既定は全部)")
	docBytes := fs.Int("doc-bytes", 100, "text の doc を最初の文まで、この byte 数までに縮める (0 は縮めない。JSON と -doc の絞り込みは全文)")
	dirs, rc, ok := parseInterspersedFlags(fs, args)
	if !ok {
		return rc
	}
	fail := failer(stderr, "dupscan index")
	sc, err := repo.Resolve(dirs)
	if err != nil {
		return fail(err)
	}
	if q.Kinds, err = parseKinds(*kinds); err != nil {
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
			hint := report.TruncationHint{Label: "該当", Ignore: true, Globs: got.sourceGlobs(), Dirs: dirs}
			if q.Name != "" {
				hint.Pattern = regexp.QuoteMeta(q.Name)
			}
			err = report.IndexText(stdout, s, report.IndexTextOptions{Limit: *limit, DocBytes: *docBytes, Hint: hint})
			if note := report.TruncationNotice("index", *limit, s.Matches); note != "" {
				fmt.Fprintln(stderr, note)
			}
		}
	default:
		return fail(fmt.Errorf("知らない形式 %q (text / json)", *format))
	}
	if err != nil {
		return fail(err)
	}
	if err := got.warnParseErrors(stderr, "index", q.Name); err != nil {
		return fail(err)
	}
	return 0
}

func flagSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) { set = set || f.Name == name })
	return set
}

// parseKinds は -kind の値 (カンマ区切り) を単位の種類にする。知らない種類は黙って 0 件にせずエラーにする。
func parseKinds(s string) ([]tree.UnitKind, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	known := map[string]tree.UnitKind{"func": tree.KindFunc, "init": tree.KindInit, "property": tree.KindProperty}
	var out []tree.UnitKind
	for _, part := range strings.Split(s, ",") {
		k, ok := known[strings.TrimSpace(part)]
		if !ok {
			return nil, fmt.Errorf("知らない種類 %q (func / init / property)", strings.TrimSpace(part))
		}
		out = append(out, k)
	}
	return out, nil
}
