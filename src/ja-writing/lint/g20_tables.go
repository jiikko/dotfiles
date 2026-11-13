// G20 の message と正規表現 (本家 yomiyasu_lint.py の 5314-5448)。組んだ後の文字列のまま写した (手で写して書き間違えないため)。
// 本家の値と同じかは TestLintBaseMatchesPythonRecording が lint-base.json の定数と比べる (tables_oracle_test.go の tableConstantValue)。

package lint

// project_term の message の 3 つの形 (本家の _PROJECT_TERM_MESSAGE・_PROJECT_TERM_PATTERN_MESSAGE・_PROJECT_TERM_BOTH_MESSAGE。{} に語を並べる)。
const (
	projectTermMessageHead        = "語の一覧（flag.txt）の語（"
	projectTermPatternMessageHead = "語の一覧（flag-patterns.txt）の語（"
	projectTermBothMessageHead    = "語の一覧（flag.txt、flag-patterns.txt）の語（"
)

// 英数字で始まるか終わる語は識別子の一部では数えず、漢字と仮名を両方含む語の漢字の端は、前後に漢字が続く所では数えない。
const (
	wordBeforePattern      = `(?<![A-Za-z0-9_./\\-])`
	wordAfterPattern       = `(?![A-Za-z0-9_])(?![./\\-][A-Za-z0-9_])`
	wordKanjiBeforePattern = `(?<![一-龥々〆ヵヶ])`
	wordKanjiAfterPattern  = `(?![一-龥々〆ヵヶ])`
)

var (
	wordASCII         = newLazy(`[A-Za-z0-9_]`, 0)
	replacementBefore = newLazy(wordBeforePattern, 0)
	replacementAfter  = newLazy(wordAfterPattern, 0)
)
