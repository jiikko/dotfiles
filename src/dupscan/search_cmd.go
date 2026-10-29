package main

import (
	"bytes"
	"dupscan/internal/jsonout"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"dupscan/internal/langs"
	"dupscan/internal/repo"
	"dupscan/internal/report"
)

func runSearch(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("dupscan search", flag.ContinueOnError)
	fs.SetOutput(stderr)
	query := fs.String("query", "", "用途語 (1〜8語。名前/署名/doc先頭行の字句一致)")
	returns := fs.String("returns", "", "戻り値の型の一部 (字面・大文字小文字を区別)")
	param := fs.String("param-type", "", "引数型の一部 (字面・大文字小文字を区別)")
	file := fs.String("file", "", "このファイルだけ (index -fileと同じパス)")
	language := fs.String("lang", "auto", "auto または対象言語")
	scope := fs.String("scope", "all", "all / prod / test")
	minimum := fs.Int("min-matched-terms", 1, "必要な一致語数")
	limit := fs.Int("limit", 5, "候補数の上限 (1〜50)")
	maxBytes := fs.Int("max-bytes", 8192, "textの全出力予算 (1024〜1048576 bytes)")
	dirs, rc, ok := parseFlags(fs, args)
	if !ok {
		return rc
	}
	fail := failer(stderr, "dupscan search")
	terms, err := report.SearchTerms(*query)
	if err != nil {
		return fail(err)
	}
	if *minimum < 1 || *minimum > len(terms) {
		return fail(errors.New("-min-matched-terms は1〜重複を除いた語数"))
	}
	if *limit < 1 || *limit > 50 {
		return fail(errors.New("-limit は1〜50"))
	}
	if *maxBytes < 1024 || *maxBytes > 1048576 {
		return fail(errors.New("-max-bytes は1024〜1048576"))
	}
	if *scope != "all" && *scope != "prod" && *scope != "test" {
		return fail(errors.New("-scope はall / prod / test"))
	}
	exts := []string{}
	for _, adapter := range langs.All {
		if *language == "auto" || adapter.Name == *language {
			exts = append(exts, adapter.Extensions...)
		}
	}
	if len(exts) == 0 {
		return fail(fmt.Errorf("未対応の-lang %q", *language))
	}
	sc, err := repo.Resolve(dirs)
	if err != nil {
		return fail(err)
	}
	src := repo.Worktree{Scope: sc, Exts: exts}
	cfg, err := loadConfig(src, false)
	if err != nil {
		return fail(fmt.Errorf("判定不能: %w", err))
	}
	filters := report.Query{Returns: *returns, ParamType: *param, Shared: cfg.InShared}
	if *file != "" {
		abs, err := absPath(*file)
		if err != nil {
			return fail(err)
		}
		filters.File, err = repoRel(sc.Root, abs)
		if err != nil {
			return fail(err)
		}
	}
	got, err := scanSource(src, true, analysisPlan{})
	if err != nil {
		return fail(err)
	}
	summary := report.BuildSearch(got.units, report.SearchQuery{Terms: terms, Filters: filters, MinMatched: *minimum, Scope: *scope})
	header := func(returned int, snippetTruncated bool) string {
		ux := queryUX{Approximation: "名前/署名/doc先頭行の字句一致", ParseErrors: len(got.errFiles), Truncated: returned < summary.Matches || snippetTruncated}
		return fmt.Sprintf("dupscan search: %q — 候補 %d 件 / 表示 %d 件 / 単位 %d%s\n  lang=%s scope=%s returns=%q param-type=%q file=%q min-matched-terms=%d\n", *query, summary.Matches, returned, summary.Units, ux.suffix(), *language, *scope, *returns, *param, filters.File, *minimum)
	}
	footer := "字句による候補。scoreは順位用で確率ではない。型/所属/再利用可否は未証明。Goテスト関数は索引対象外。0件は不存在の証明ではない。\n"
	hardDiagnostic := ""
	if summary.Matches == 0 {
		if summary.FilteredUnits == 0 {
			hardDiagnostic = "解析済み索引の型/file/scope条件に一致する単位0件。語数を緩めても候補は増えない。型の不存在やalias解決を示すものではない。\n"
		}
		fileArg := *file
		if filters.File != "" {
			fileArg = filepath.Join(sc.Root, filters.File)
		}
		values := map[string]string{"returns": *returns, "param-type": *param, "file": fileArg, "scope": *scope, "lang": *language}
		reason := ""
		retryMinimum := *minimum
		if retryMinimum > 1 && summary.FilteredUnits > 0 {
			retryMinimum = 1
			reason = "一致0件の確認案: min-matched-termsを1へ下げる。他の条件は維持 (自動実行しない)"
		}
		for _, key := range []string{"returns", "param-type", "file", "scope", "lang"} {
			if reason != "" {
				break
			}
			value := values[key]
			if value == "" || key == "scope" && value == "all" || key == "lang" && value == "auto" {
				continue
			}
			switch key {
			case "scope":
				values[key] = "all"
			case "lang":
				values[key] = "auto"
			default:
				values[key] = ""
			}
			change := key + " 条件を外す"
			if key == "scope" {
				change = "scopeをallへ広げる"
			}
			if key == "lang" {
				change = "langをautoへ広げる"
			}
			reason = "一致0件の確認案: " + change + "。他の条件は維持 (自動実行しない)"
			break
		}
		if reason != "" {
			argv := []string{"-query", *query, "-min-matched-terms", strconv.Itoa(retryMinimum), "-limit", strconv.Itoa(*limit), "-max-bytes", strconv.Itoa(*maxBytes)}
			for _, key := range []string{"returns", "param-type", "file", "scope", "lang"} {
				if values[key] != "" {
					argv = append(argv, "-"+key, values[key])
				}
			}
			argv = append(argv, "--")
			var action bytes.Buffer
			if err := writeNextAction(&action, scopedNextAction(sc, "search", reason, argv...)); err != nil {
				return fail(err)
			}
			if len(header(0, false))+len(footer)+action.Len() <= *maxBytes {
				footer += action.String()
			}
		} else {
			var action bytes.Buffer
			reason := "メタデータでは一致0件。最初の語でdoc全文/テスト/ソースを確認する (ディレクトリscopeは維持、定義一致ではない)"
			if err := writeNextAction(&action, queryNextAction(sc, "evidence", terms[0], reason)); err != nil {
				return fail(err)
			}
			if len(header(0, false))+len(footer)+action.Len() <= *maxBytes {
				footer += action.String()
			}
		}
	}
	if hardDiagnostic != "" && len(header(0, false))+len(footer)+len(hardDiagnostic) <= *maxBytes {
		footer += hardDiagnostic
	}
	var body strings.Builder
	returned := 0
	snippetTruncated := false
	for _, candidate := range summary.Candidates {
		if returned == *limit {
			break
		}
		card, truncated := report.SearchCandidateText(candidate)
		readArgv := []string{"sed", "-n", fmt.Sprintf("%d,%dp", candidate.Entry.Line, max(candidate.Entry.Line, min(candidate.Entry.EndLine, candidate.Entry.Line+79))), filepath.Join(sc.Root, candidate.Entry.File)}
		readJSON, err := jsonout.Line(readArgv)
		if err != nil {
			return fail(err)
		}
		card += fmt.Sprintf("  read_argv: %s\n  source_read_truncated=%t\n", readJSON, candidate.Entry.EndLine > candidate.Entry.Line+79)
		if len(header(returned+1, snippetTruncated || truncated))+body.Len()+len(card)+len(footer) > *maxBytes {
			break
		}
		body.WriteString(card)
		returned++
		snippetTruncated = snippetTruncated || truncated
	}
	output := header(returned, snippetTruncated) + body.String() + footer
	if len(output) > *maxBytes {
		return fail(errors.New("要約を予算内に出せない。-max-bytes を増やす"))
	}
	if _, err := io.WriteString(stdout, output); err != nil {
		return fail(err)
	}
	return 0
}
