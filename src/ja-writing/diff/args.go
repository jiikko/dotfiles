package diff

import (
	_ "embed"
	"strings"

	"ja-writing/cmdenv"
	"ja-writing/internal/pycli"
	"ja-writing/internal/pystr"
)

// argparse の写し (本家の _build_parser)。使い方の誤りの文と、どの順で誤りを見つけるかを、本家の argparse (Python 3.15.0) と同じにする。
// ヘルプ (-h) は本家が出した文面をそのまま持つ (help.txt。端末の幅は 80 桁の場合)。解析そのものは lint と共有する (internal/pycli)。

const (
	prog          = "yomiyasu_diff.py"
	formatVersion = 2
)

//go:embed help.txt
var helpText string

const usageText = `使い方: yomiyasu_diff.py [--stance 立場] [--doc-type 種類] [--reader 読者] [--json] [--fail-on meaning-risk]
                        [--words フォルダ | --no-words] [--material 資料 ...] 元の文 書き直した文
       yomiyasu_diff.py --endings ファイル [--stance 立場]
       yomiyasu_diff.py --version
`

// stanceNames は --stance に使える値 (本家の STANCES の鍵の順。別名を含む)。
var stanceNames = []string{"勧め", "読み手に勧める", "決まり", "手順", "説明", "報告", "体験"}

// stances は --stance の値から立場への対応 (本家の STANCES)。立場は勧め (読み手に勧める・頼む)・決まり (決まり・手順を伝える)・説明 (事実・結果・考えを伝える)。
var stances = map[string]string{"勧め": "勧め", "読み手に勧める": "勧め", "決まり": "決まり", "手順": "決まり", "説明": "説明", "報告": "説明", "体験": "説明"}

// options は parse_args の結果。
type options struct {
	files     []string
	stance    *string
	json      bool
	failOn    []string
	docType   *string
	reader    *string
	words     *string
	noWords   bool
	materials []string
	endings   bool
	version   bool
	help      bool
}

// parseError は argparse が使い方の誤りとして断る文 (parser.error の引数)。
type parseError = pycli.Error

var spec = pycli.Spec{
	Values:         []string{"--stance", "--fail-on", "--doc-type", "--reader", "--words", "--material"},
	Flags:          []string{"--json", "--no-words", "--endings", "--version", "--help", "-h"},
	MaxPositionals: -1,
}

// parseArgs は本家の parser.parse_intermixed_args(argv)。誤りは parseError。-h は、見つけた時点で help を立てて返す (それより後の誤りは見ない)。
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
	o.files = res.Positionals
	for _, occ := range res.Options {
		value := occ.Value
		switch occ.Name {
		case "--json":
			o.json = true
		case "--no-words":
			o.noWords = true
		case "--endings":
			o.endings = true
		case "--version":
			o.version = true
		case "--stance":
			o.stance = &value
		case "--fail-on":
			o.failOn = append(o.failOn, value)
		case "--doc-type":
			o.docType = &value
		case "--reader":
			o.reader = &value
		case "--words":
			o.words = &value
		case "--material":
			o.materials = append(o.materials, value)
		}
	}
	return o, nil
}

// usageTo は parser.error の文を標準エラーに書く。
func usageTo(env cmdenv.Env, message string) (int, error) {
	return pycli.UsageTo(env, usageText, prog, message)
}

// failOnValues は本家の _fail_on。--fail-on の値を確かめ、(meaning-risk を指定したか, 誤りの文) を返す。
// 大文字と小文字、「_」と「-」は区別しない (MEANING_RISK も受ける)。
func failOnValues(values []string) (hit bool, problem string) {
	for _, value := range values {
		var pieces []string
		for _, piece := range strings.Split(value, ",") {
			if pystr.Strip(piece) != "" {
				pieces = append(pieces, pycli.FailOnWord(piece))
			}
		}
		if len(pieces) == 0 {
			// 空、空白だけ、カンマだけの値は、目に見えないので「（空）」と書く。
			return false, "--fail-on に使えない値です: （空）。diff で使えるのは meaning-risk です。"
		}
		for _, piece := range pieces {
			if piece != "meaning-risk" {
				return false, "--fail-on に使えない値です: " + piece + "。diff で使えるのは meaning-risk です。"
			}
			hit = true
		}
	}
	return hit, ""
}
