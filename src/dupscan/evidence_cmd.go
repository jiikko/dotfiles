package main

import (
	"bytes"
	"dupscan/internal/lang"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"dupscan/internal/langs"
	"dupscan/internal/repo"
	"dupscan/internal/report"
	"dupscan/internal/tree"
)

// runEvidence は名前の分からない既存の実装を、doc の全文・テスト・ソースの行に書いた語から探す (index の -doc は doc の最初の行しか見ない)。
//
// 語の一致を返すだけで、定義・呼び出しの照合ではない。doc の行は、その doc が付いた単位を候補として出し、
// テストとソースの行は、その行で呼んでいる名前を添える (使われ方から候補へ辿る)。text だけを出す理由は context と同じ。
func runEvidence(args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("dupscan evidence", stderr)
	limit := fs.Int("limit", 20, "節ごとに出す件数の上限 (0 は全部)")
	_, rc, ok := parseFlags(fs, args)
	if !ok {
		return rc
	}
	fail := failer(stderr, "dupscan evidence")
	pos := fs.Args()
	if len(pos) == 0 || strings.TrimSpace(pos[0]) == "" {
		return fail(errors.New("探す語を渡す (例: bucket か \"retry after\")"))
	}
	term := strings.TrimSpace(pos[0])
	if strings.ContainsAny(term, "\r\n") {
		return fail(errors.New("語は 1 行で渡す (行をまたいだ一致は探さない)"))
	}
	if *limit < 0 {
		return fail(errors.New("-limit は 0 以上"))
	}
	dirs := pos[1:]
	if len(dirs) == 0 {
		dirs = []string{"."}
	}
	sc, err := repo.Resolve(dirs)
	if err != nil {
		return fail(err)
	}
	// defs は Swift の型の名前を取るため (初期化子の呼び出しは型の名前で書く)。Go の型は取らないので、Go の型の生成は集計に出ない
	got, err := scanSource(repo.Worktree{Scope: sc, Exts: langs.Extensions()}, true, analysisPlan{Options: lang.Options{Calls: true, Defs: true}})
	if err != nil {
		return fail(err)
	}
	defined := map[string]bool{}
	for _, u := range got.units {
		defined[tree.MatchName(u.Name)] = true
	}
	for _, d := range got.types {
		defined[tree.MatchName(d.Name)] = true
	}
	s := report.BuildEvidence(term, got.contents, evidenceOwners(got), got.calls, defined, langs.IsTestPath)
	ux := queryUX{Approximation: "語の一致 (型解決なし)", ParseErrors: len(got.errFiles), Truncated: s.Truncated}
	if *limit > 0 && (len(s.Doc) > *limit || len(s.Test) > *limit || len(s.Source) > *limit || len(s.Called) > *limit) {
		ux.Truncated = true
	}
	fmt.Fprintf(stdout, "dupscan evidence: %q — doc に語がある単位 %d 個 / テスト %d 件 / その他のソース %d 件 / そこで呼ばれている名前 %d 個%s\n",
		term, len(s.Doc), len(s.Test), len(s.Source), len(s.Called), ux.suffix())
	fmt.Fprintln(stdout, "  語の一致 (大文字小文字を区別しない) で、定義・呼び出しの照合ではない。doc は doc comment の全文 (Go は関数の doc だけ)。"+
		"テスト・ソースの行はコメント・文字列も含む。一致があれば最初のnext_actionは一致行の原文確認。意味上の再利用可否は未証明。")
	if len(got.errFiles) > 0 {
		fmt.Fprintf(stdout, "  読めない構文を含むファイルが %d 個 (そこの単位は欠けうる。行は探している): %s\n", len(got.errFiles), headList(got.errFiles, 3))
	}
	for _, entries := range [][]report.EvidenceEntry{s.Doc, s.Test, s.Source} {
		if len(entries) == 0 {
			continue
		}
		e := entries[0]
		if err := writeNextAction(stdout, evidenceReadAction(sc, e, got.contents[e.File])); err != nil {
			return fail(err)
		}
		break
	}
	if len(s.Called) > 0 {
		if err := writeNextAction(stdout, queryNextAction(sc, "callers", s.Called[0].Name, "語を含む行で呼ばれている候補の使われ方を確かめる")); err != nil {
			return fail(err)
		}
	} else if len(s.Doc) > 0 && s.Doc[0].Symbol != "" {
		action := queryNextAction(sc, "index", s.Doc[0].Symbol, "doc に語がある候補の定義を索引で確かめる", "-name")
		if strings.HasSuffix(s.Doc[0].File, ".swift") {
			action = queryNextAction(sc, "context", s.Doc[0].Symbol, "doc に語がある候補の定義と使われ方を確かめる")
		}
		if err := writeNextAction(stdout, action); err != nil {
			return fail(err)
		}
	}
	if err := report.EvidenceTextGrouped(stdout, s, *limit); err != nil {
		return fail(err)
	}
	return 0
}

// The hit is the anchor even when Go test ownership or a complete definition is unavailable.
func evidenceReadAction(sc repo.Scope, e report.EvidenceEntry, source []byte) nextAction {
	lines := bytes.Count(source, []byte{'\n'})
	if len(source) > 0 && source[len(source)-1] != '\n' {
		lines++
	}
	start, end := max(1, e.Line-4), min(lines, e.Line+75)
	truncated := start > 1 || end < lines
	return nextAction{
		Kind:   "read_source",
		Reason: fmt.Sprintf("一致行の前後の原文を確認 (最大80行、source_read_truncated=%t、owner_unknown=%t)。否定条件や用途は原文で判断し、編集後は再検索する。自動実行しない", truncated, e.Unit == ""),
		Argv:   []string{"sed", "-n", fmt.Sprintf("%d,%dp", start, end), filepath.Join(sc.Root, e.File)},
	}
}

// evidenceOwners は単位に、Swift の定義 (比べる単位から外したテスト関数・protocol の要件・保存 property・enum の case) と型の宣言を足す。
// doc の行の持ち主と、行を囲む名前を引く材料にするだけで、比べる単位 (report / gate) には入れない。
// Go は定義を取らないので、Go のテスト関数・型の doc は足せない (callers と同じ限界)。
func evidenceOwners(got scanned) []*tree.Unit {
	units := append([]*tree.Unit(nil), got.units...)
	for _, d := range got.definitions {
		units = append(units, &tree.Unit{File: d.File, Line: d.Line, EndLine: d.EndLine, Name: d.Name, Signature: d.Signature,
			Enclosing: d.Enclosing, Kind: tree.UnitKind(d.Kind), Test: d.Test, DocStart: d.DocStart, DocEnd: d.DocEnd})
	}
	for _, d := range got.types {
		units = append(units, &tree.Unit{File: d.File, Line: d.Line, EndLine: d.EndLine, Name: d.Name, Signature: d.Kind + " " + d.Signature,
			Enclosing: d.Enclosing, Kind: tree.UnitKind(d.Kind), Test: d.Test, DocStart: d.DocStart, DocEnd: d.DocEnd})
	}
	return units
}
