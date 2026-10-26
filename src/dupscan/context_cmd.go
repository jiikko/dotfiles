package main

import (
	"dupscan/internal/lang"
	"errors"
	"flag"
	"fmt"
	"io"
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

	fmt.Fprintf(stdout, "dupscan context: %s — 定義 %d 件 / 参照 %d 件 / 呼び出し %d 件\n", query, defs.Matches, refs.Matches, callers.Matches)
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
	sections := []struct {
		title string
		write func() error
	}{
		{fmt.Sprintf("定義 (%d 件)", defs.Matches), func() error {
			return report.DefinitionsTextMore(stdout, defs, *limit, "-limit 0 で全部。Type.member か 名前(ラベル:) で絞る")
		}},
		{fmt.Sprintf("参照: %s (%d 件)", name, refs.Matches), func() error { return report.ReferencesTextMore(stdout, refs, *limit, "-limit 0 で全部") }},
		{fmt.Sprintf("呼び出し: %s (%d 件)", callers.Query, callers.Matches), func() error {
			return report.CallersTextMore(stdout, callers, *limit, "-limit 0 で全部。名前(ラベル:) で絞る")
		}},
	}
	for _, sec := range sections {
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
