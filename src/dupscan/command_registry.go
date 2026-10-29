package main

import "io"

type commandSpec struct {
	Name        string
	Run         func([]string, io.Writer, io.Writer) int
	Formats     []string
	SymbolsOnly bool
	Query       string
	Summary     string
	// Watched は、作ったときの用途どおりに使われているかを見ている入口の、その用途。空でなければ起動のたびに stderr へ 1 行出す (watchNotice)。
	// 消さずに残しているのは、ほかの入口では埋まらない用途があるから (search は index の AND の絞り込みでは引けない用途の語、
	// evidence は grep の行を単位へ写す、context は def・refs・callers を 1 回の走査で)。足りた・足りなかった例が集まったら、広げるか消すかを決める。
	Watched string
}

// Dispatch and discovery share this list so an advertised command always has an entry point.
func commandSpecs() []commandSpec {
	return []commandSpec{
		{Name: "report", Run: runReportCommand, Formats: []string{"text", "json", "xcode"}, Summary: "Find structurally duplicate units"},
		{Name: "gate", Run: runGate, Formats: []string{"text", "json", "xcode"}, Summary: "Check new duplicates against a base"},
		{Name: "sweep", Run: runSweep, Formats: []string{"text"}, Summary: "Dig duplicates from four angles in one scan"},
		{Name: "near", Run: runNear, Formats: []string{"text", "json"}, Query: "file:unit-or-line", Summary: "Find units resembling a selected unit"},
		{Name: "explain", Run: runExplain, Formats: []string{"text", "json"}, Query: "file:unit-or-line", Summary: "Explain normalization and matching"},
		{Name: "search", Run: runSearch, Formats: []string{"text"}, Query: "-query terms with optional type filters", Summary: "Find helper candidates across name, signature and doc", Watched: "名前の分からない既存の helper を、用途の語・署名の型から引く (index の絞り込みでは引けない探し方)"},
		{Name: "index", Run: runIndex, Formats: []string{"text", "json"}, Query: "optional metadata filters", Summary: "Find existing units before writing"},
		{Name: "callers", Run: runCallers, Formats: []string{"text", "json"}, Query: "name-or-signature", Summary: "Find approximate call sites"},
		{Name: "def", Run: runDef, Formats: []string{"text", "json"}, SymbolsOnly: true, Query: "name-or-Type.member-or-signature", Summary: "Read definitions and type declarations"},
		{Name: "refs", Run: runRefs, Formats: []string{"text", "json"}, SymbolsOnly: true, Query: "name", Summary: "Find approximate identifier references"},
		{Name: "context", Run: runContext, Formats: []string{"text"}, SymbolsOnly: true, Query: "name-or-Type.member-or-signature", Summary: "Read definitions, references and callers together", Watched: "変更の前に、定義・参照・呼び出し元を 1 回の走査でまとめて読む"},
		{Name: "evidence", Run: runEvidence, Formats: []string{"text"}, Query: "term", Summary: "Find lexical evidence in docs, tests and source", Watched: "索引に載らない doc の後半・テストの使用例・ソースの行の語から、名前の分からない実装の候補へ辿る"},
		{Name: "capabilities", Run: runCapabilities, Formats: []string{"text", "json"}, Summary: "Discover compiled commands and language support"},
	}
}

// watchNotice は Watched の入口を起動したときに stderr へ出す 1 行。stdout と rc は変えない (結果を読む hook・スクリプトの判定を壊さないため)。
func watchNotice(spec commandSpec) string {
	return "dupscan " + spec.Name + ": 使われ方を見ている入口です (作った用途: " + spec.Watched + ")。" +
		"この用途で使って足りた・足りなかったことがあれば ~/dotfiles/issues/epic/932/ に issue を書いてください。使われる形へ広げる材料にします\n"
}

func runNear(args []string, stdout, stderr io.Writer) int {
	return runPoint("near", args, stdout, stderr)
}

func runExplain(args []string, stdout, stderr io.Writer) int {
	return runPoint("explain", args, stdout, stderr)
}
