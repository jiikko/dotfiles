package main

import (
	"dupscan/internal/lang"
	"errors"
	"fmt"
	"io"
	"strings"

	"dupscan/internal/langs"
	"dupscan/internal/repo"
	"dupscan/internal/report"
	"dupscan/internal/tree"
)

func runRefs(args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("dupscan refs", stderr)
	format := fs.String("format", "text", "出力の形式: text / json")
	limit := fs.Int("limit", 20, "text で出す参照の上限 (0 は全部)")
	_, rc, ok := parseFlags(fs, args)
	if !ok {
		return rc
	}
	fail := failer(stderr, "dupscan refs")
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

	q, qualified, err := parseQualifiedQuery(query)
	if err != nil {
		return fail(err)
	}
	if qualified {
		fmt.Fprintf(stderr, "dupscan refs: %q は非対応の問い合わせ。Type.memberでは型を解決して絞れない。単独名で探すと同名の別型も含む近似になる\n", query)
		if err := writeNextAction(stderr, queryNextAction(sc, "refs", q.Name, "型とラベルで絞らず単独名の参照を探す")); err != nil {
			return fail(err)
		}
		return 2
	}
	src, note, err := symbolSource(sc)
	if err != nil {
		return fail(err)
	}
	got, err := scanSource(src, true, analysisPlan{Options: lang.Options{Refs: true, Defs: true}})
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
	if err := got.warnParseErrors(stderr, "refs", query); err != nil {
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
	labels := isClosureProperty(name, got.definitions)
	references := make([]report.ReferenceRecord, 0, len(got.references))
	for _, r := range got.references {
		if r.Kind == "argument_label" && !labels {
			continue // ラベルは普通の語 (path: など) で雑音になる。関数型の stored property の名前を引いたときだけ出す
		}
		references = append(references, report.ReferenceRecord{
			Name: r.Name, Kind: r.Kind, File: r.File, Line: r.Line, Col: r.Col, Text: r.Text, Test: r.Test, Enclosing: r.Enclosing, TestOwner: r.TestOwner,
		})
	}
	return report.BuildReferences(name, references, got.units, langs.IsTestPath)
}

// isClosureProperty は name が、関数型の Swift の stored property として定義されているか。
// 呼び出しの引数ラベル (`Foo(onSaved: { … })`) が、その closure の実装の場所になる。
// 定義は型を解決しない名前の一致で選ぶので、同名の別の型の引数ラベルも出る (近似)。
func isClosureProperty(name string, defs []lang.Definition) bool {
	for _, d := range defs {
		if d.ClosureProperty && tree.MatchName(d.Name) == tree.MatchName(name) {
			return true
		}
	}
	return false
}
