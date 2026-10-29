package main

import (
	"dupscan/internal/lang"
	"errors"
	"fmt"
	"io"
	"regexp"

	"time"

	"dupscan/internal/langs"
	"dupscan/internal/repo"
	"dupscan/internal/report"
	"dupscan/internal/typed"
)

// runCallers は名前 (と引数ラベル) で呼び出しの箇所を引く。型を解決しないので、同名の別のメソッドの呼び出しも当たる (近似)。
func runCallers(args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("dupscan callers", stderr)
	format := fs.String("format", "text", "出力の形式: text / json")
	selector := fs.String("selector", "", "Goのみ: sourceに書いた単純selector chainをAND (例: strings.Join。型/importは解決しない)")
	limit := fs.Int("limit", 20, "text で出す件数の上限 (0 は全部)")
	typedFlag := fs.Bool("typed", false, "Goのみ: go/packages で型を解決し、呼ばれる関数を package のパス・受け手の型・名前の鍵で確定する (例: dupscan/internal/repo.Scope.Whole)。構文の近似より遅い")
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

	if *typedFlag {
		if *selector != "" {
			return fail(errors.New("-typed と -selector は一緒に使えない (-typed は型で鍵を確定するので、selector の絞り込みは要らない)"))
		}
		return runTypedCallers(rest[0], rest[1:], *format, *limit, stdout, fail)
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

// runTypedCallers は callers -typed: 起点の module を go/packages で読み、型で確定した呼び出し元・確定できない呼び出し・
// 確定できないパッケージ・読んでいないファイルを分けて出す。
func runTypedCallers(query string, dirs []string, format string, limit int, stdout io.Writer, fail func(error) int) int {
	if len(dirs) == 0 {
		dirs = []string{"."}
	}
	sc, err := repo.Resolve(dirs)
	if err != nil {
		return fail(err)
	}
	var goExts []string
	for _, adapter := range langs.All {
		if adapter.Name == "go" {
			goExts = adapter.Extensions
		}
	}
	files, err := repo.List(sc, goExts)
	if err != nil {
		return fail(err)
	}
	modules, err := typed.Modules(sc.Root, sc.Prefixes)
	if err != nil {
		return fail(fmt.Errorf("module を探せない: %w", err))
	}
	if len(modules) == 0 {
		return fail(errors.New("起点の中にも上にも go.mod が無い (-typed は Go の module を読む)"))
	}
	start := time.Now()
	res, err := typed.Callers(sc.Root, modules, query, files)
	if err != nil {
		return fail(err)
	}
	switch format {
	case "json":
		err = report.WriteJSON(stdout, res)
	case "text":
		err = writeTypedCallers(stdout, query, res, limit, time.Since(start))
	default:
		return fail(fmt.Errorf("知らない形式 %q (text / json)", format))
	}
	if err != nil {
		return fail(err)
	}
	return 0
}

func writeTypedCallers(w io.Writer, query string, res typed.Result, limit int, took time.Duration) error {
	ew := &lineWriter{w: w}
	ew.printf("型で解決 (go/packages。module %d・パッケージ %d・%.1f 秒)。呼ばれる関数は package のパス・受け手の型・名前の鍵で示す\n", len(res.Modules), res.Packages, took.Seconds())
	if len(res.Keys) == 0 {
		ew.printf("%q に当たる関数の呼び出し・参照は無い (型で確定できたパッケージの中では)\n", query)
	}
	byKey := map[string][]typed.Call{}
	for _, c := range res.Confirmed {
		byKey[c.Key] = append(byKey[c.Key], c)
	}
	for _, k := range res.Keys {
		cs := byKey[k]
		ew.printf("%s: 呼び出し %d 件\n", k, len(cs))
		for i, c := range cs {
			if limit > 0 && i >= limit {
				ew.printf("  ... 他 %d 件 (-limit 0 か -format json で全部)\n", len(cs)-limit)
				break
			}
			ew.printf("  %s:%d:%d %s\n", c.File, c.Line, c.Column, c.Caller)
		}
	}
	if len(res.Unknown) > 0 {
		ew.printf("呼び出し先を確定できない %d 件 (interface を通した呼び出し・関数値として渡した所。確定に数えない):\n", len(res.Unknown))
		for i, c := range res.Unknown {
			if limit > 0 && i >= limit {
				ew.printf("  ... 他 %d 件\n", len(res.Unknown)-limit)
				break
			}
			ew.printf("  %s:%d:%d %s (%s・%s)\n", c.File, c.Line, c.Column, c.Caller, c.Kind, c.Key)
		}
	}
	var untyped, partial []typed.Package
	for _, p := range res.Untyped {
		if p.Partial {
			partial = append(partial, p)
		} else {
			untyped = append(untyped, p)
		}
	}
	if len(untyped) > 0 {
		ew.printf("🚨 型で確定できないパッケージ %d 個 (この中の呼び出しは上に数えていない。構文の近似の callers で見る):\n", len(untyped))
		for _, p := range untyped {
			ew.printf("  %s: %s\n", p.Path, p.Reason)
		}
	}
	if len(partial) > 0 {
		ew.printf("依存のパッケージの誤りで結果が欠けうるパッケージ %d 個 (この中の呼び出しは上に数えたが、型が取れずに落ちたものがありうる):\n", len(partial))
		for _, p := range partial {
			ew.printf("  %s\n", p.Path)
		}
	}
	if len(res.Unread) > 0 {
		ew.printf("この構成 (今の GOOS / GOARCH・build tag 無し) で読んでいない Go のファイル %d 本 (この中の呼び出しは上に数えていない):\n", len(res.Unread))
		for i, f := range res.Unread {
			if i >= 5 {
				ew.printf("  ... 他 %d 本\n", len(res.Unread)-5)
				break
			}
			ew.printf("  %s\n", f)
		}
	}
	return ew.err
}
