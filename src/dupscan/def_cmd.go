package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"dupscan/internal/repo"
	"dupscan/internal/report"
)

func runDef(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("dupscan def", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "text", "出力の形式: text / json")
	limit := fs.Int("limit", 20, "text で出す定義の上限 (0 は全部)")
	maxLines := fs.Int("max-lines", 80, "定義ごとに出すソース行の上限")
	fs.Usage = func() {
		fmt.Fprint(stderr, usage)
		fs.PrintDefaults()
	}
	_, rc, ok := parseInterspersedFlags(fs, args)
	if !ok {
		return rc
	}
	fail := func(err error) int {
		fmt.Fprintln(stderr, "dupscan def:", err)
		return 2
	}
	pos := fs.Args()
	if len(pos) == 0 {
		return fail(errors.New("定義名を渡す (例: retrying か DropboxClient.upload)"))
	}
	q, err := report.ParseDefQuery(pos[0])
	if err != nil {
		return fail(err)
	}
	if *maxLines < 1 {
		return fail(errors.New("-max-lines は 1 以上"))
	}
	dirs := pos[1:]
	if len(dirs) == 0 {
		dirs = []string{"."}
	}
	sc, err := repo.Resolve(dirs)
	if err != nil {
		return fail(err)
	}
	src, note, err := symbolSource(sc)
	if err != nil {
		return fail(err)
	}
	cfg, err := loadConfig(src, false)
	if err != nil {
		return fail(fmt.Errorf("判定不能: %w", err))
	}
	got, err := scanSourceOptions(src, true, true, true, false)
	if err != nil {
		return fail(withNote(err, note))
	}
	summary := buildDefinitions(pos[0], q, got, *maxLines, cfg.InShared)
	switch *format {
	case "text":
		err = report.DefinitionsText(stdout, summary, *limit)
	case "json":
		err = report.WriteJSON(stdout, summary)
	default:
		return fail(fmt.Errorf("知らない形式 %q (text / json)", *format))
	}
	if err != nil {
		return fail(err)
	}
	if note != "" {
		fmt.Fprintln(stderr, "dupscan def:", note) // 見つかっても出す (同じ名前の Go の定義が「無い」と読めないように)
	}
	if summary.Matches == 0 {
		return 1
	}
	return 0
}

// buildDefinitions は走査の結果から q に当たる定義を組み立てる (def と context が同じ当て方を使う)。
func buildDefinitions(query string, q report.DefQuery, got scanned, maxLines int, shared func(string) bool) report.DefinitionsSummary {
	defs := make([]report.DefinitionRecord, 0, len(got.definitions))
	for _, d := range got.definitions {
		defs = append(defs, report.DefinitionRecord{
			Name: d.Name, Kind: d.Kind, File: d.File, Line: d.Line, EndLine: d.EndLine, Col: d.Col,
			Enclosing: d.Enclosing, Signature: d.Signature, Labels: d.Labels, Test: d.Test,
			Nested: d.Nested, Visibility: d.Visibility, Doc: d.Doc,
		})
	}
	types := make([]report.TypeRecord, 0, len(got.types))
	for _, d := range got.types {
		types = append(types, report.TypeRecord{
			Name: d.Name, Kind: d.Kind, File: d.File, Line: d.Line, EndLine: d.EndLine, Col: d.Col,
			Enclosing: d.Enclosing, Scope: d.Scope, Signature: d.Signature, Test: d.Test,
			Nested: d.Nested, Visibility: d.Visibility, Doc: d.Doc,
		})
	}
	return report.BuildDefinitions(query, q, defs, types, got.units, got.calls, got.contents, maxLines, shared)
}
