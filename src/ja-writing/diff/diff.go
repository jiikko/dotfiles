// Package diff は ja-writing diff (元の文と書き直した文の差分の検査) の Main を持つ。本家 yomiyasu_diff.py の Go 実装。
//
// 構成と、本家の群 (A〜AE) との対応:
//   - diff.go (Main・run): 引数と終了コード (本家の _run・main。群 A)。引数の定義は args.go (argparse の解析は lint と共有する internal/pycli)
//   - base.go (群 B): 9 キーの下ごしらえ。ending.go (群 C): 文末の種類と立場。endings.go: --endings の出力 (群 E の太字は lint と共有する internal/bold を呼ぶだけで、写さない)
//
// 本家の名前との対応は各ファイルの冒頭のコメントにある。diff 本体 (差分と検出) はまだ無く、--endings 以外の検査まで進む呼び出しは「まだ実装していません」で終わる。
package diff

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"ja-writing/cmdenv"
	"ja-writing/internal/pycli"
	"ja-writing/internal/pystr"
	"ja-writing/visibility"
)

// Main は subcommand の入口 (本家の main)。
func Main(_ context.Context, env cmdenv.Env) int {
	code, err := run(env)
	if err == nil {
		return code
	}
	return pycli.Finish(env, prog, err)
}

// fail は標準エラーに 1 行書いて rc 2 にする (本家の _error + return 2)。
func fail(env cmdenv.Env, message string) (int, error) {
	pycli.Eprintln(env, message)
	return cmdenv.ExitUsage, nil
}

// request は引数の検査を済ませた 1 回の呼び出し (diff 本体が受け取るもの)。
type request struct {
	stance    string
	docType   *string
	reader    *string
	words     *visibility.Words
	failOn    bool
	json      bool
	old, new  string
	oldInfo   visibility.SourceInfo
	newInfo   visibility.SourceInfo
	materials []string
}

// run は本家の _run。確かめる順は本家と同じ (誤りが重なったときにどの文が出るかを決める)。
func run(env cmdenv.Env) (int, error) {
	o, err := parseArgs(env.Args)
	var perr parseError
	if errors.As(err, &perr) {
		return usageTo(env, string(perr))
	}
	if o.help {
		return 0, pycli.AtExit("stdout", pycli.PrintTo(env, helpText))
	}
	if o.version {
		return 0, pycli.AtExit("stdout", pycli.PrintTo(env, fmt.Sprintf("%s %s (JSON format %d)\n", prog, visibility.Version, formatVersion)))
	}
	if code, handled, err := checkShape(env, o); handled {
		return code, err
	}
	failOn, problem := failOnValues(o.failOn)
	if problem != "" {
		return fail(env, problem)
	}
	var req request
	req.failOn, req.json = failOn, o.json
	if o.stance != nil {
		stance, ok := stances[*o.stance]
		if !ok {
			shown := *o.stance
			if pystr.Strip(shown) == "" {
				shown = "（空）"
			}
			return fail(env, fmt.Sprintf("--stance に使えない値です: %s。使えるのは %s です。", shown, strings.Join(stanceNames, "、")))
		}
		req.stance = stance
	}
	if req.docType, err = visibility.NormalizeDocType(o.docType); err != nil {
		return fail(env, err.Error())
	}
	if req.reader, err = visibility.NormalizeReader(o.reader); err != nil {
		return fail(env, err.Error())
	}
	if !o.endings {
		var cwd *string
		if env.Cwd != "" {
			cwd = &env.Cwd
		}
		if req.words, _, err = visibility.ResolveWords(cwd, o.words, o.noWords); err != nil {
			var werr *visibility.Error
			if errors.As(err, &werr) && werr.Type == visibility.TypeWordsError {
				return fail(env, werr.Message)
			}
			return 0, err
		}
	}
	for _, path := range append(append([]string{}, o.files...), o.materials...) {
		if reason, ok := visibility.InputPathProblem(path); !ok {
			return fail(env, fmt.Sprintf("Error opening file %s: %s。このファイルを渡すときは、標準入力から渡してください（ファイルの名前の代わりに - を渡します。1か所だけ）", path, reason))
		}
	}
	texts := make([]string, len(o.files))
	infos := make([]visibility.SourceInfo, len(o.files))
	for i := range o.files {
		var problem string
		if texts[i], infos[i], problem = pycli.ReadInput(env, &o.files[i]); problem != "" {
			return fail(env, problem)
		}
	}
	for _, path := range o.materials {
		text, _, problem := pycli.ReadInput(env, &path)
		if problem != "" {
			return fail(env, problem)
		}
		req.materials = append(req.materials, text)
	}
	if o.endings {
		report, err := endingsReport(texts[0], req.stance)
		if err != nil {
			return 0, err
		}
		return 0, pycli.PrintTo(env, report+"\n")
	}
	req.old, req.new, req.oldInfo, req.newInfo = texts[0], texts[1], infos[0], infos[1]
	return compare(env, req)
}

// checkShape は、ファイルの数・--endings と一緒に使えないオプション・--words と --no-words・標準入力の数の誤り (本家の parser.error。どれも rc 2)。
// handled が真なら (code, err) を返して終わる。
func checkShape(env cmdenv.Env, o options) (code int, handled bool, err error) {
	reject := func(message string) (int, bool, error) {
		code, err := usageTo(env, message)
		return code, true, err
	}
	if o.endings {
		switch {
		case o.json:
			return reject("--endings は --json に対応していません")
		case len(o.failOn) > 0:
			return reject("--endings は --fail-on に対応していません")
		case o.docType != nil || o.reader != nil:
			return reject("--endings は --doc-type と --reader に対応していません")
		case o.words != nil || o.noWords:
			return reject("--endings は --words と --no-words に対応していません")
		case len(o.materials) > 0:
			return reject("--endings は --material に対応していません")
		case len(o.files) != 1:
			return reject(fmt.Sprintf("--endings のときは、ファイルを1つだけ渡してください（渡されたのは%dつ）", len(o.files)))
		}
	} else if len(o.files) != 2 {
		return reject(fmt.Sprintf("元の文と書き直した文の2つのファイルを渡してください（渡されたのは%dつ）", len(o.files)))
	}
	if o.words != nil && o.noWords {
		return reject("--words と --no-words は一緒に使えません")
	}
	stdin := 0
	for _, f := range append(append([]string{}, o.files...), o.materials...) {
		if f == "-" {
			stdin++
		}
	}
	if stdin > 1 {
		return reject("標準入力（-）は1か所だけに使えます")
	}
	return 0, false, nil
}

// compare は差分の検査の本体 (本家の diff() と出力。まだ無い。引数の検査と入力の読みは済んでいる)。
func compare(env cmdenv.Env, _ request) (int, error) {
	return cmdenv.Unimplemented(env, "diff"), nil
}
