// Package pycli は lint と diff の CLI が共有する部品: Python の argparse の写し・使い方の誤りの文・出力先が閉じたときの扱い・入力を読む処理。
// 本家の lint・diff・check は同じ `_ArgumentParser` と `_read_input` を写しで持つ (ast が同じ)。Go は 1 つにまとめる。
package pycli

import (
	"fmt"
	"slices"
	"strings"

	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
)

// Spec は parser の定義。Values は値を 1 つ取るオプション、Flags は値を取らないオプション (-h と --help を含める)。
// MaxPositionals は位置引数の数の上限 (nargs="?" は 1、nargs="*" は負の数 = 上限なし)。上限を超えた位置引数は「unrecognized arguments」になる。
type Spec struct {
	Values         []string
	Flags          []string
	MaxPositionals int
}

// Occurrence はオプションの 1 回の指定 (指定の順に並べる)。フラグの Value は空。
type Occurrence struct {
	Name  string
	Value string
}

// Result は parse の結果。Help は -h か --help を見つけて止まったこと (それより後ろは読まない)。
type Result struct {
	Positionals []string
	Options     []Occurrence
	Help        bool
}

// Error は argparse が使い方の誤りとして断る文 (parser.error の引数)。
type Error string

func (e Error) Error() string { return string(e) }

// negativeNum は argparse の _negative_number_matcher (Python 3.15 の `-\.?\d` を match で当てる。前方一致で、\d は Unicode の十進数字)。
var negativeNum = pyre.NewLazy(`-\.?\d`, 0)

type argKind int

const (
	kindPositional argKind = iota
	kindOption
	kindUnknown
)

func (s Spec) known(name string) bool { return s.isValue(name) || s.isFlag(name) }

func (s Spec) isValue(name string) bool { return slices.Contains(s.Values, name) }

func (s Spec) isFlag(name string) bool { return slices.Contains(s.Flags, name) }

// classify は 1 つの引数 (「--」より前のもの) が、位置引数か、知っているオプション (name と、=・短い連結で付いた値) か、知らないオプションか。
// 短い名前の連結は -h だけを見る (-hx は -h)。
func (s Spec) classify(arg string) (kind argKind, name string, explicit *string) {
	switch {
	case arg == "" || arg[0] != '-' || arg == "-":
		return kindPositional, "", nil
	case s.known(arg):
		return kindOption, arg, nil
	}
	if i := strings.IndexByte(arg, '='); i >= 0 {
		if n := arg[:i]; s.known(n) {
			v := arg[i+1:]
			return kindOption, n, &v
		}
	}
	if negativeNum.MustMatchAt0(arg) || strings.Contains(arg, " ") {
		return kindPositional, "", nil
	}
	if strings.HasPrefix(arg, "-h") && !strings.HasPrefix(arg, "--") {
		return kindOption, "-h", nil
	}
	return kindUnknown, "", nil
}

// isValueToken はオプションの値として使える引数か (位置引数として分類される引数。「--」は値にならない)。
func (s Spec) isValueToken(arg string, positionalOnly bool) bool {
	if positionalOnly {
		return true
	}
	if arg == "--" {
		return false
	}
	kind, _, _ := s.classify(arg)
	return kind == kindPositional
}

// Parse は本家の parser.parse_args(argv) (位置引数とオプションを混ぜてよい parse_intermixed_args も同じ答え)。誤りは Error。
// -h は、見つけた時点で Help を立てて返す (それより後の誤りは見ない)。
func (s Spec) Parse(args []string) (Result, error) {
	var res Result
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
			kind, name, explicit = s.classify(arg)
		}
		switch kind {
		case kindPositional:
			if s.MaxPositionals < 0 || len(res.Positionals) < s.MaxPositionals {
				res.Positionals = append(res.Positionals, arg)
			} else {
				extras = append(extras, arg)
			}
		case kindUnknown:
			extras = append(extras, arg)
		case kindOption:
			if name == "-h" || name == "--help" {
				if explicit != nil {
					return res, Error("argument -h/--help: ignored explicit argument " + pystr.Repr(*explicit))
				}
				res.Help = true
				return res, nil
			}
			if s.isFlag(name) {
				if explicit != nil {
					return res, Error("argument " + name + ": ignored explicit argument " + pystr.Repr(*explicit))
				}
				res.Options = append(res.Options, Occurrence{Name: name})
				continue
			}
			value := explicit
			if value == nil {
				if i+1 >= len(args) || !s.isValueToken(args[i+1], positionalOnly) {
					return res, Error(fmt.Sprintf("argument %s: expected one argument", name))
				}
				i++
				value = &args[i]
			}
			res.Options = append(res.Options, Occurrence{Name: name, Value: *value})
		}
	}
	if len(extras) > 0 {
		return res, Error("unrecognized arguments: " + strings.Join(extras, " "))
	}
	return res, nil
}

// UsageError は parser.error(message) が標準エラーに出す文 (使い方・誤りの 1 行・案内の 1 行)。usage は末尾に改行を持つ使い方の文。
func UsageError(usage, prog, message string) string {
	return usage + prog + ": error: " + message + "\n引数の誤りです。" + prog + " -h で使い方を確かめてください。\n"
}

// FailOnWord は --fail-on の値の 1 要素の正規化 (`piece.strip().lower().replace("_", "-")`)。使える値は ASCII の語だけなので、小文字化の違いは使えない値の文に出る字だけに現れる。
// 小文字化は 1 字ごとの対応 (strings.ToLower) に İ の分解 (i と U+0307) を足したもの。語末の Σ を ς にする文脈の規則と、Python の Unicode のバージョンとの差は写さない (README の「揃わない範囲」)。
func FailOnWord(piece string) string {
	lower := strings.ToLower(strings.ReplaceAll(pystr.Strip(piece), "İ", "i̇"))
	return strings.ReplaceAll(lower, "_", "-")
}
