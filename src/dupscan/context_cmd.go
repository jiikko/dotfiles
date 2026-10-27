package main

import (
	"dupscan/internal/lang"
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"dupscan/internal/langs"
	"dupscan/internal/repo"
	"dupscan/internal/report"
)

// runContext は def / refs / callers を 1 回の走査でまとめて出す (変更の前に定義と使われ方を一度に見る)。
//
// text だけを出す。json を足さない: 既存コマンドの json は形を固定して変えない決まりなので、ここで出すと
// 共通の machine 向けの形 (状態・省略・継続取得を持つ版) より前に、固定すべき形が 1 つ増える。
// refs / callers は名前 (と引数ラベル) だけで当てる。Type.member の Type では絞らない (型を解決しないので、絞ると 0 件になる)。
func runContext(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("dupscan context", flag.ContinueOnError)
	fs.SetOutput(stderr)
	limit := fs.Int("limit", 20, "節ごとに出す件数の上限 (0 は全部)")
	sectionsFlag := fs.String("sections", "definition,refs,callers", "definition,refs,callers のカンマ区切り、または summary")
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
		fmt.Fprintln(stderr, "dupscan context:", err)
		return 2
	}
	pos := fs.Args()
	if len(pos) == 0 || strings.TrimSpace(pos[0]) == "" {
		return fail(errors.New("定義名を渡す (例: retrying か DropboxClient.upload)"))
	}
	query := strings.TrimSpace(pos[0])
	q, err := report.ParseDefQuery(query)
	if err != nil {
		return fail(err)
	}
	if *maxLines < 1 {
		return fail(errors.New("-max-lines は 1 以上"))
	}
	if *limit < 0 {
		return fail(errors.New("-limit は 0 以上"))
	}
	selected := strings.Split(*sectionsFlag, ",")
	for i, section := range selected {
		if !slices.Contains([]string{"definition", "refs", "callers", "summary"}, section) || slices.Contains(selected[:i], section) || section == "summary" && len(selected) != 1 {
			return fail(errors.New("-sections は definition,refs,callers の重複しない組、または summary 単独"))
		}
	}
	summary := selected[0] == "summary"
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
	got, err := scanSource(src, true, analysisPlan{Options: lang.Options{Calls: true, Defs: true, Refs: true}})
	if err != nil {
		return fail(withNote(err, note))
	}
	defs := buildDefinitions(query, q, got, *maxLines, cfg.InShared)
	// 初期化子は型の名前で呼ばれる (`Foo(…)`)。名前 init で当てると self.init や別の型の .init だけが当たり、本当の呼び出しが全部落ちる
	name := q.Name
	if name == "init" && q.Type != "" {
		typeName, _, _ := strings.Cut(q.Type, "<") // Box<T>.init は Box で呼ばれる
		name = typeName[strings.LastIndex(typeName, ".")+1:]
	}
	if name == "" {
		return fail(fmt.Errorf("%q から引く名前が決まらない (Type.member の形で書く)", query))
	}
	refs := buildReferences(name, got)
	callQuery := report.CallQuery{Name: name, Labels: q.Labels}
	callers := report.BuildCallers(callQueryText(callQuery), callQuery, got.calls, got.units, langs.IsTestPath)

	ux := queryUX{Approximation: "名前・ラベル照合", ParseErrors: len(got.errFiles), Unsupported: note != ""}
	totals := map[string]int{"definition": defs.Matches, "refs": refs.Matches, "callers": callers.Matches}
	for section, total := range totals {
		if total > 0 && (!slices.Contains(selected, section) || *limit > 0 && total > *limit) {
			ux.Truncated = true
		}
	}
	definitionOmitted := defs.Matches > 0 && (summary || slices.Contains(selected, "definition") && *limit > 0 && defs.Matches > *limit)
	if slices.Contains(selected, "definition") {
		for i, e := range defs.Entries {
			if (*limit == 0 || i < *limit) && e.Truncated {
				ux.Truncated = true
				definitionOmitted = true
			}
		}
	}
	fmt.Fprintf(stdout, "dupscan context: %s — 定義 %d 件 / 参照 %d 件 / 呼び出し %d 件%s\n", query, defs.Matches, refs.Matches, callers.Matches, ux.suffix())
	fmt.Fprintf(stdout, "  参照は名前 %s、呼び出しは %s で当てた近似 (型を解決しない。同名の別の定義のものも混ざる)", name, callers.Query)
	switch {
	case name != q.Name:
		fmt.Fprintf(stdout, "。初期化子なので型の名前で引いた (参照は型の名前の全部の出現。`.init(`・`Self(…)`・`%s<…>(…)` と書いた呼び出しは数えない)", name)
	case q.Name == "init":
		fmt.Fprintf(stdout, "。初期化子は型の名前で呼ばれるので、型の呼び出しを見るなら Type.init で引く")
	case q.Type != "":
		fmt.Fprintf(stdout, "。%s では絞っていない", q.Type)
	}
	if q.Labels != nil {
		fmt.Fprint(stdout, "。ラベルの数と順で当てるので、既定値のある引数を省いた呼び出しは数えない (全部見るならラベルを外して引く)")
	}
	fmt.Fprintln(stdout)
	switch {
	case defs.Matches == 0:
		fmt.Fprintln(stdout, "  定義が 0 件。参照・呼び出しは問い合わせた定義のものとは限らない (名前の書き方か、読んでいない言語を疑う)")
	case defs.Matches > 1:
		fmt.Fprintf(stdout, "  定義が %d 件あり 1 つに決まっていない (Type.member か 名前(ラベル:) で絞る)。参照・呼び出しはどの定義のものかを分けていない\n", defs.Matches)
	}
	if note != "" {
		fmt.Fprintf(stdout, "  %s。呼び出しも Swift の分だけ\n", note)
	}
	if len(got.errFiles) > 0 {
		fmt.Fprintf(stdout, "  読めない構文を含むファイルが %d 個 (そこの定義・参照・呼び出しは欠けうる): %s\n", len(got.errFiles), headList(got.errFiles, 3))
	}
	refsOmitted := refs.Matches > 0 && (summary || slices.Contains(selected, "refs") && *limit > 0 && refs.Matches > *limit)
	if defs.Matches+callers.Matches > 0 || refsOmitted {
		action := queryNextAction(sc, "callers", callQueryText(callQuery), "名前で近似した呼び出し元を個別に調べる", "-limit", "0")
		if definitionOmitted {
			sourceLines := *maxLines
			for _, e := range defs.Entries {
				sourceLines = max(sourceLines, e.EndLine-e.Line+1)
			}
			action = queryNextAction(sc, "context", query, "定義の本体・型のメンバーを取得する (参照・呼び出しは省略)", "-sections", "definition", "-limit", "0", "-max-lines", strconv.Itoa(sourceLines))
		} else if refsOmitted {
			action = queryNextAction(sc, "refs", name, "参照の一覧を取得する (名前で当てた近似)", "-limit", "0")
		}
		if err := writeNextAction(stdout, action); err != nil {
			return fail(err)
		}
	}
	if summary {
		fmt.Fprintf(stdout, "\n## 要約 (%d 件)\n", defs.Matches)
		for i, e := range defs.Entries {
			if *limit > 0 && i == *limit {
				fmt.Fprintf(stdout, "… 他 %d 件 (-limit 0 で全部)\n", defs.Matches-*limit)
				break
			}
			doc := ""
			for _, d := range got.definitions {
				if d.File == e.File && d.Line == e.Line {
					doc = d.Doc
					break
				}
			}
			if doc == "" {
				for _, d := range got.types {
					if d.File == e.File && d.Line == e.Line {
						doc = d.Doc
						break
					}
				}
			}
			doc, _, _ = strings.Cut(doc, "\n")
			mark := ""
			if e.Test {
				mark = " [test]"
			}
			name := e.Signature
			if slices.Contains([]string{"struct", "class", "enum", "protocol", "actor", "extension"}, e.Kind) {
				name = e.Kind + " " + name
			}
			if e.Enclosing != "" {
				name = e.Enclosing + "." + name
			}
			fmt.Fprintf(stdout, "%s:%d %s%s", e.File, e.Line, name, mark)
			if doc != "" {
				fmt.Fprintf(stdout, " // %s", doc)
			}
			fmt.Fprintln(stdout)
		}
	}
	sections := []struct {
		key   string
		title string
		write func() error
	}{
		{"definition", fmt.Sprintf("定義 (%d 件)", defs.Matches), func() error {
			return report.DefinitionsTextMore(stdout, defs, *limit, "-limit 0 で全部。Type.member か 名前(ラベル:) で絞る")
		}},
		{"refs", fmt.Sprintf("参照: %s (%d 件)", name, refs.Matches), func() error { return report.ReferencesTextGrouped(stdout, refs, *limit, "-limit 0 で全部") }},
		{"callers", fmt.Sprintf("呼び出し: %s (%d 件)", callers.Query, callers.Matches), func() error {
			return report.CallersTextGrouped(stdout, callers, *limit, "-limit 0 で全部。名前(ラベル:) で絞る")
		}},
	}
	for _, sec := range sections {
		if !slices.Contains(selected, sec.key) {
			continue
		}
		fmt.Fprintf(stdout, "\n## %s\n", sec.title)
		if err := sec.write(); err != nil {
			return fail(err)
		}
	}
	if note != "" {
		fmt.Fprintln(stderr, "dupscan context:", note) // 見つかっても出す (Go の定義・参照が「無い」と読めないように)
	}
	return 0
}

// callQueryText は callers の問い合わせを `名前` か `名前(ラベル:…)` の形で書く (ParseCallQuery が読める形)。
func callQueryText(q report.CallQuery) string {
	if q.Labels == nil {
		return q.Name
	}
	var b strings.Builder
	b.WriteString(q.Name + "(")
	for _, l := range q.Labels {
		b.WriteString(l + ":")
	}
	b.WriteString(")")
	return b.String()
}

// headList は先頭 n 個を並べ、残りを件数で書く (読めないファイルが多い repo で 1 行を食い潰さない)。
func headList(items []string, n int) string {
	if len(items) <= n {
		return strings.Join(items, ", ")
	}
	return fmt.Sprintf("%s ほか %d 個", strings.Join(items[:n], ", "), len(items)-n)
}
