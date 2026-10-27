package main

import (
	"dupscan/internal/lang"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"dupscan/internal/langs"
	"dupscan/internal/repo"
	"dupscan/internal/report"
)

func runRefs(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("dupscan refs", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "text", "出力の形式: text / json")
	limit := fs.Int("limit", 20, "text で出す参照の上限 (0 は全部)")
	fs.Usage = func() {
		fmt.Fprint(stderr, usage)
		fs.PrintDefaults()
	}
	_, rc, ok := parseInterspersedFlags(fs, args)
	if !ok {
		return rc
	}
	fail := func(err error) int {
		fmt.Fprintln(stderr, "dupscan refs:", err)
		return 2
	}
	pos := fs.Args()
	if len(pos) == 0 || strings.TrimSpace(pos[0]) == "" {
		return fail(errors.New("参照を探す名前を渡す (例: StorageOperationLockRegistry)"))
	}
	query := strings.TrimSpace(pos[0])
	dirs := pos[1:]
	if len(dirs) == 0 {
		dirs = []string{"."}
	}
	sc, err := repo.Resolve(dirs)
	if err != nil {
		return fail(err)
	}

	// A dot operator is a valid bare name; only diagnose an identifier-led qualifier.
	first, _ := utf8.DecodeRuneInString(query)
	if strings.Contains(query, ".") && (unicode.IsLetter(first) || first == '_' || first == '`') {
		q, err := report.ParseDefQuery(query)
		if err != nil {
			return fail(err)
		}
		if q.Type != "" {
			fmt.Fprintf(stderr, "dupscan refs: %q は非対応の問い合わせ。Type.memberでは型を解決して絞れない。単独名で探すと同名の別型も含む近似になる\n", query)
			if err := writeNextAction(stderr, queryNextAction(sc, "refs", q.Name, "型とラベルで絞らず単独名の参照を探す")); err != nil {
				return fail(err)
			}
			return 2
		}
	}
	src, note, err := symbolSource(sc)
	if err != nil {
		return fail(err)
	}
	got, err := scanSource(src, true, analysisPlan{Options: lang.Options{Refs: true}})
	if err != nil {
		return fail(withNote(err, note))
	}
	summary := buildReferences(query, got)
	switch *format {
	case "text":
		err = report.ReferencesText(stdout, summary, *limit)
	case "json":
		err = report.WriteJSON(stdout, summary)
	default:
		return fail(fmt.Errorf("知らない形式 %q (text / json)", *format))
	}
	if err != nil {
		return fail(err)
	}
	if err := writeParseWarning(stderr, "refs", got.errFiles); err != nil {
		return fail(err)
	}
	if note != "" {
		fmt.Fprintln(stderr, "dupscan refs:", note) // 見つかっても出す (同じ名前の Go の定義が「無い」と読めないように)
	}
	if summary.Matches == 0 {
		return 1
	}
	return 0
}

// buildReferences は走査の結果から name の参照を組み立てる (refs と context が同じ当て方を使う)。
func buildReferences(name string, got scanned) report.ReferencesSummary {
	references := make([]report.ReferenceRecord, 0, len(got.references))
	for _, r := range got.references {
		references = append(references, report.ReferenceRecord{
			Name: r.Name, Kind: r.Kind, File: r.File, Line: r.Line, Col: r.Col, Text: r.Text, Test: r.Test, Enclosing: r.Enclosing, TestOwner: r.TestOwner,
		})
	}
	return report.BuildReferences(name, references, got.units, langs.IsTestPath)
}
