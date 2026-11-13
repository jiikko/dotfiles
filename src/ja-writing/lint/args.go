package lint

import (
	_ "embed"
	"fmt"
	"regexp"
	"strings"

	"ja-writing/internal/pystr"
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
type parseError string

func (e parseError) Error() string { return string(e) }

// valueOptions は値を 1 つ取るオプション。flagOptions は値を取らないオプション。
var (
	valueOptions = map[string]bool{"--fail-on": true, "--doc-type": true, "--reader": true, "--words": true}
	flagOptions  = map[string]bool{"--json": true, "--strict": true, "--no-words": true, "--version": true, "--help": true, "-h": true}
	negativeNum  = regexp.MustCompile(`^-\d+$|^-\d*\.\d+$`)
)

// argKind は argparse の _parse_optional の分類。
type argKind int

const (
	kindPositional argKind = iota
	kindOption
	kindUnknown
)

// classify は 1 つの引数 (「--」より前のもの) が、位置引数か、知っているオプション (name と、=・短い連結で付いた値) か、知らないオプションか。
// 短い名前の連結は -h だけを見る (-hx は -h)。
func classify(arg string) (kind argKind, name string, explicit *string) {
	switch {
	case arg == "" || arg[0] != '-' || arg == "-":
		return kindPositional, "", nil
	case valueOptions[arg] || flagOptions[arg]:
		return kindOption, arg, nil
	}
	if i := strings.IndexByte(arg, '='); i >= 0 {
		if n := arg[:i]; valueOptions[n] || flagOptions[n] {
			v := arg[i+1:]
			return kindOption, n, &v
		}
	}
	if negativeNum.MatchString(arg) || strings.Contains(arg, " ") {
		return kindPositional, "", nil
	}
	if strings.HasPrefix(arg, "-h") && !strings.HasPrefix(arg, "--") {
		return kindOption, "-h", nil
	}
	return kindUnknown, "", nil
}

// parseArgs は本家の parser.parse_args(argv)。誤りは parseError。-h は、見つけた時点で help を立てて返す (それより後の誤りは見ない)。
func parseArgs(args []string) (options, error) {
	var o options
	var extras []string
	positionalOnly := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		kind, name := kindPositional, ""
		var explicit *string
		switch {
		case positionalOnly:
		case arg == "--":
			positionalOnly = true
			continue
		default:
			kind, name, explicit = classify(arg)
		}
		switch kind {
		case kindPositional:
			if o.file == nil {
				a := arg
				o.file = &a
			} else {
				extras = append(extras, arg)
			}
		case kindUnknown:
			extras = append(extras, arg)
		case kindOption:
			if name == "-h" || name == "--help" {
				if explicit != nil {
					return o, parseError(fmt.Sprintf("argument -h/--help: ignored explicit argument '%s'", *explicit))
				}
				o.help = true
				return o, nil
			}
			if flagOptions[name] {
				if explicit != nil {
					return o, parseError(fmt.Sprintf("argument %s: ignored explicit argument '%s'", name, *explicit))
				}
				switch name {
				case "--json":
					o.json = true
				case "--strict":
					o.conditions = append(o.conditions, condition{strict: true})
				case "--no-words":
					o.noWords = true
				case "--version":
					o.version = true
				}
				continue
			}
			value := explicit
			if value == nil {
				if i+1 >= len(args) || !isValueToken(args[i+1], positionalOnly) {
					return o, parseError(fmt.Sprintf("argument %s: expected one argument", name))
				}
				i++
				value = &args[i]
			}
			switch name {
			case "--fail-on":
				o.conditions = append(o.conditions, condition{value: *value})
			case "--doc-type":
				o.docType = value
			case "--reader":
				o.reader = value
			case "--words":
				o.words = value
			}
		}
	}
	if len(extras) > 0 {
		return o, parseError("unrecognized arguments: " + strings.Join(extras, " "))
	}
	return o, nil
}

// isValueToken はオプションの値として使える引数か (位置引数として分類される引数。「--」は値にならない)。
func isValueToken(arg string, positionalOnly bool) bool {
	if positionalOnly {
		return true
	}
	if arg == "--" {
		return false
	}
	kind, _, _ := classify(arg)
	return kind == kindPositional
}

// usageError は parser.error(message) が標準エラーに出す文 (使い方・誤りの 1 行・案内の 1 行)。
func usageError(message string) string {
	return usageText + prog + ": error: " + message + "\n引数の誤りです。" + prog + " -h で使い方を確かめてください。\n"
}

// failOnWord は --fail-on の値の 1 要素の正規化 (`piece.strip().lower().replace("_", "-")`)。使える値は ASCII の語だけなので、小文字化の違いは使えない値の文に出る字だけに現れる。
// 小文字化は 1 字ごとの対応 (strings.ToLower) に İ の分解 (i と U+0307) を足したもの。語末の Σ を ς にする文脈の規則と、Python の Unicode のバージョンとの差は写さない (README の「揃わない範囲」)。
func failOnWord(piece string) string {
	lower := strings.ToLower(strings.ReplaceAll(pystr.Strip(piece), "\u0130", "i\u0307"))
	return strings.ReplaceAll(lower, "_", "-")
}
