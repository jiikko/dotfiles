package lint

import (
	_ "embed"

	"ja-writing/internal/pycli"
)

// argparse の写し (本家の _build_parser)。使い方の誤りの文と、どの順で誤りを見つけるかを、本家の argparse (Python 3.15.0) と同じにする。
// 揃えない所: ヘルプ (-h) は本家が出した文面をそのまま持つ (help.txt。端末の幅は 80 桁の場合)。ほかの誤りの書き方 (別の名前の接頭辞の省略・--opt=値 の短い形の連結など) は
// 本家の記録 (testdata/golden/lint-cli.json) にあるものだけを確かめている。

const prog = "yomiyasu_lint.py"

//go:embed help.txt
var helpText string

const usageText = `使い方: yomiyasu_lint.py [-h] [--json] [--strict] [--fail-on {warn,error}]
                      [--doc-type 種類] [--reader 読者] [--words フォルダ]
                      [--no-words] [--version]
                      [file]
`

// condition は --strict か --fail-on の 1 回の指定 (指定の順に並べる)。
type condition struct {
	strict bool
	value  string
}

// options は parse_args の結果。
type options struct {
	file       *string
	json       bool
	conditions []condition
	docType    *string
	reader     *string
	words      *string
	noWords    bool
	version    bool
	help       bool
}

// parseError は argparse が使い方の誤りとして断る文 (parser.error の引数)。
type parseError = pycli.Error

var spec = pycli.Spec{
	Values:         []string{"--fail-on", "--doc-type", "--reader", "--words"},
	Flags:          []string{"--json", "--strict", "--no-words", "--version", "--help", "-h"},
	MaxPositionals: 1,
}

// parseArgs は本家の parser.parse_args(argv)。誤りは parseError。-h は、見つけた時点で help を立てて返す (それより後の誤りは見ない)。
func parseArgs(args []string) (options, error) {
	var o options
	res, err := spec.Parse(args)
	if err != nil {
		return o, err
	}
	if res.Help {
		o.help = true
		return o, nil
	}
	if len(res.Positionals) > 0 {
		o.file = &res.Positionals[0]
	}
	for _, occ := range res.Options {
		value := occ.Value
		switch occ.Name {
		case "--json":
			o.json = true
		case "--strict":
			o.conditions = append(o.conditions, condition{strict: true})
		case "--no-words":
			o.noWords = true
		case "--version":
			o.version = true
		case "--fail-on":
			o.conditions = append(o.conditions, condition{value: value})
		case "--doc-type":
			o.docType = &value
		case "--reader":
			o.reader = &value
		case "--words":
			o.words = &value
		}
	}
	return o, nil
}

// usageError は parser.error(message) が標準エラーに出す文 (使い方・誤りの 1 行・案内の 1 行)。
func usageError(message string) string { return pycli.UsageError(usageText, prog, message) }
