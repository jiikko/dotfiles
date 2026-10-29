package main

import (
	"dupscan/internal/lang"
	"errors"
	"fmt"
	"io"
	"regexp"

	"dupscan/internal/langs"
	"dupscan/internal/repo"
	"dupscan/internal/report"
)

// runCallers は名前 (と引数ラベル) で呼び出しの箇所を引く。型を解決しないので、同名の別のメソッドの呼び出しも当たる (近似)。
func runCallers(args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("dupscan callers", stderr)
	format := fs.String("format", "text", "出力の形式: text / json")
	selector := fs.String("selector", "", "Goのみ: sourceに書いた単純selector chainをAND (例: strings.Join。型/importは解決しない)")
	limit := fs.Int("limit", 20, "text で出す件数の上限 (0 は全部)")
	rest, rc, ok := parseFlags(fs, args)
	if !ok {
		return rc
	}
	fail := failer(stderr, "dupscan callers")
	if len(fs.Args()) == 0 {
		return fail(errors.New("呼ばれる名前を渡す (例: make か make(x:_:))"))
	}
	q, err := report.ParseCallQuery(rest[0])
	if err != nil {
		return fail(err)
	}

	exts := langs.Extensions()
	if *selector != "" {
		q.Selector, err = report.ParseCallSelector(*selector)
		if err != nil {
			return fail(err)
		}
		exts = nil
		for _, adapter := range langs.All {
			if adapter.Name == "go" {
				exts = adapter.Extensions
			}
		}
	}
	dirs := rest[1:]
	if len(dirs) == 0 {
		dirs = []string{"."}
	}
	sc, err := repo.Resolve(dirs)
	if err != nil {
		return fail(err)
	}
	got, err := scanSource(repo.Worktree{Scope: sc, Exts: exts}, true, analysisPlan{Options: lang.Options{Calls: true}})
	if err != nil {
		if q.Selector != "" {
			return fail(fmt.Errorf("go selector検索 (Swiftは対象外): %w", err))
		}
		return fail(err)
	}
	s := report.BuildCallers(rest[0], q, got.calls, got.units, langs.IsTestPath)
	switch *format {
	case "json":
		err = report.WriteJSON(stdout, s)
	case "text":
		_, err = fmt.Fprintln(stdout, got.callApproximation(q.Labels != nil))
		if err == nil {
			hint := report.TruncationHint{Label: "呼び出し", Globs: got.sourceGlobs(), Dirs: dirs}
			if q.Selector == "" {
				hint.Pattern = regexp.QuoteMeta(q.Name) + `\(`
			}
			err = report.CallersTextHint(stdout, s, *limit, "-limit 0 か -format json で全部。ラベルを書いて絞る", hint)
			if note := report.TruncationNotice("callers", *limit, s.Matches); note != "" {
				fmt.Fprintln(stderr, note)
			}
		}
	default:
		return fail(fmt.Errorf("知らない形式 %q (text / json)", *format))
	}
	if err != nil {
		return fail(err)
	}
	if err := got.warnParseErrors(stderr, "callers", q.Name); err != nil {
		return fail(err)
	}
	return 0
}
