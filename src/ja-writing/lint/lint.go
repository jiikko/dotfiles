// Package lint は ja-writing lint (表現と書式の検査) の Main を持つ。本家 yomiyasu_lint.py の Go 実装。
//
// 構成:
//   - lint.go (Main・run)・args.go (argparse の写し): 引数と終了コード (本家の _run・argparse)。出力は result.go (text と --json)
//   - lint_text.go: lint_text の骨組み (群を本家の順に呼ぶ)。groups は gNN_*.go (G1〜G20) に 1 ファイルずつ。群の足し方は README の「lint の群の足し方」
//   - post.go: 検出の後の処理 (種類で外す・allow・ダッシュの上限・点数)。rules.go: 規則の表と実装済みの規則の表
//   - 土台 (候補を出さない部品): emoji.go (絵文字。公開は EmojiMatches)・endmark.go (文末の飾り)・prose.go (文字の下ごしらえと、1 つの文書の解析と覚えを持つ document)
//
// 本家の名前との対応は各ファイルの冒頭のコメントにある。
// bold_not_rendered (G3) は internal/bold の Problems を呼ぶ lint/bold_rule.go が受け持ち、骨組みは g03Bold で呼ぶ。
package lint

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"ja-writing/cmdenv"
	"ja-writing/visibility"
)

// 終了コード (本家の argparse の定義): 0 は検査を最後まで実行し条件に当たらない・1 は --strict か --fail-on の条件に当たった・2 は使い方か入力の誤り・3 は内部の誤りか出力先が閉じた。
const (
	exitHit      = 1
	exitInternal = 3
)

// Main は subcommand の入口 (本家の main)。
func Main(_ context.Context, env cmdenv.Env) int {
	code, err := run(env)
	if err == nil {
		return code
	}
	// 本家の BrokenPipeError と、ほかの例外 (rc 3)。文面の細部 (トレースバック) は揃えない。
	var internal *visibility.Error
	if errors.As(err, &internal) {
		eprintln(env, fmt.Sprintf("%s: 内部エラー: %s: %s", prog, internal.Type, internal.Message))
	} else if errors.Is(err, errOutputClosed) {
		eprintln(env, prog+": 出力先が閉じられたため、結果を最後まで書けませんでした。")
	} else {
		eprintln(env, fmt.Sprintf("%s: 内部エラー: %v", prog, err))
	}
	return exitInternal
}

var errOutputClosed = errors.New("output closed")

// eprintln は標準エラーに 1 行書く (書けなくても何もしない。本家の _error)。
func eprintln(env cmdenv.Env, message string) { _, _ = io.WriteString(env.Stderr, message+"\n") }

// printTo は標準出力に書く。書けなかったら errOutputClosed (本家は BrokenPipeError を rc 3 にする)。
func printTo(env cmdenv.Env, text string) error {
	if _, err := io.WriteString(env.Stdout, text); err != nil {
		return errOutputClosed
	}
	return nil
}

// run は本家の _run。確かめる順は「--version → 条件 → 種類と読者 → words の同時指定 → 語の一覧 → 入力のパス → 入力を読む」で、誤りが重なったときにどの文が出るかを決める。
func run(env cmdenv.Env) (int, error) {
	o, err := parseArgs(env.Args)
	var perr parseError
	if errors.As(err, &perr) {
		_, _ = io.WriteString(env.Stderr, usageError(string(perr)))
		return cmdenv.ExitUsage, nil
	}
	if o.help {
		return 0, printTo(env, helpText)
	}
	if o.version {
		return 0, printTo(env, fmt.Sprintf("%s %s (JSON format %d)\n", prog, toolVersion, formatVersion))
	}
	names, problem := failConditions(o.conditions)
	if problem != "" {
		eprintln(env, problem)
		return cmdenv.ExitUsage, nil
	}
	docType, err := visibility.NormalizeDocType(o.docType)
	if err != nil {
		eprintln(env, err.Error())
		return cmdenv.ExitUsage, nil
	}
	reader, err := visibility.NormalizeReader(o.reader)
	if err != nil {
		eprintln(env, err.Error())
		return cmdenv.ExitUsage, nil
	}
	if o.words != nil && o.noWords {
		_, _ = io.WriteString(env.Stderr, usageError("--words と --no-words は一緒に使えません"))
		return cmdenv.ExitUsage, nil
	}
	var cwd *string
	if env.Cwd != "" {
		cwd = &env.Cwd
	}
	words, _, err := visibility.ResolveWords(cwd, o.words, o.noWords)
	if err != nil {
		var werr *visibility.Error
		if errors.As(err, &werr) && werr.Type == visibility.TypeWordsError {
			eprintln(env, werr.Message)
			return cmdenv.ExitUsage, nil
		}
		return 0, err
	}
	if o.file != nil {
		if reason, ok := visibility.InputPathProblem(*o.file); !ok {
			eprintln(env, fmt.Sprintf("Error opening file %s: %s。このファイルを検査するときは、標準入力から渡してください（ファイルの名前の代わりに - を渡すか、省きます）", *o.file, reason))
			return cmdenv.ExitUsage, nil
		}
	}
	content, problem := readInput(env, o.file)
	if problem != "" {
		eprintln(env, problem)
		return cmdenv.ExitUsage, nil
	}

	res, err := lintText(content, lintContext{docType: docType, reader: reader, words: words})
	if err != nil {
		return 0, err
	}
	hit := hitConditions(names, res.counts)
	if o.json {
		out, err := dumpResult(res)
		if err != nil {
			return 0, err
		}
		if err := printTo(env, out+"\n"); err != nil {
			return 0, err
		}
	} else {
		text := renderReport(res) + "\n" + exitLine(hit, res.counts)
		if err := printTo(env, text); err != nil {
			return 0, err
		}
	}
	if len(hit) > 0 {
		return exitHit, nil
	}
	return 0, nil
}

// hitConditions は当たった条件の文 (指定の順。同じ文は 1 回)。strict と warn は重さが同じだが文が違うので、両方指定すると両方並ぶ。
func hitConditions(names []string, counts [3]int) []string {
	var hit []string
	for _, name := range names {
		c := failConditionTable[name]
		if !slices.Contains(hit, c.text) && slices.ContainsFunc(c.severities, func(s int) bool { return counts[s] > 0 }) {
			hit = append(hit, c.text)
		}
	}
	return hit
}

// exitLine は条件に当たったときの text の出力の末尾 (空行と「終了コード1: …」の行)。当たらなければ空。--json には付けない。
func exitLine(hit []string, counts [3]int) string {
	if len(hit) == 0 {
		return ""
	}
	return fmt.Sprintf("\n終了コード1: %sに当たりました（error %d件、warn %d件）。\n", strings.Join(hit, "、"), counts[0], counts[1])
}

// failCondition は --strict と --fail-on の条件。text は終了コード 1 の行に書く文、severities は当たったかを決める重さ (counts の添字。0 が error・1 が warn)。
type failCondition struct {
	text       string
	severities []int
}

var failConditionTable = map[string]failCondition{
	"strict": {"--strict の条件（warn か error の候補が1件以上）", []int{0, 1}},
	"warn":   {"--fail-on=warn の条件（warn か error の候補が1件以上）", []int{0, 1}},
	"error":  {"--fail-on=error の条件（error の候補が1件以上）", []int{0}},
}

const meaningRiskMessage = "--fail-on=meaning-risk は lint では使えません。lint は1つの文章だけを見るので、意味の変化は判定できません。" +
	"原文と比べる yomiyasu_diff.py か yomiyasu_check.py に付けてください。"

// failConditions は本家の _conditions。--strict と --fail-on の並びを、条件の名前の並びにする。使えない値があれば誤りの文を返す
// (meaning-risk は、ほかの値と混ぜても、ほかの誤りより先に断る)。
func failConditions(raw []condition) (names []string, problem string) {
	var unknown []string
	for _, c := range raw {
		if c.strict {
			names = append(names, "strict")
			continue
		}
		pieces := 0
		for _, piece := range strings.Split(c.value, ",") {
			piece = failOnWord(piece)
			if piece == "" {
				continue
			}
			pieces++
			switch piece {
			case "warn", "error":
				names = append(names, piece)
			case "meaning-risk":
				return nil, meaningRiskMessage
			default:
				unknown = append(unknown, piece)
			}
		}
		if pieces == 0 {
			// 空、空白だけ、カンマだけの値は、目に見えないので「（空）」と書く。
			unknown = append(unknown, "（空）")
		}
	}
	if len(unknown) > 0 {
		return nil, fmt.Sprintf("--fail-on に使えない値です: %s。lint で使えるのは warn と error です。", unknown[0])
	}
	return names, ""
}

// readInput は本家の _read_input。ファイルか標準入力を読み、(文字列, "") か ("", 誤りの文) を返す。
func readInput(env cmdenv.Env, file *string) (string, string) {
	if file != nil && *file != "-" {
		data, err := os.ReadFile(*file)
		if err != nil {
			return "", fmt.Sprintf("Error opening file %s: ファイルを開けません（%s）", *file, visibility.OSErrorReason(*file, err))
		}
		text, _, derr := visibility.DecodeSourceBytes(data)
		if derr != nil {
			return "", fmt.Sprintf("Error opening file %s: UTF-8 として読めません", *file)
		}
		return text, ""
	}
	data, err := io.ReadAll(env.Stdin)
	if err != nil {
		return "", fmt.Sprintf("Error reading stdin: 標準入力を読めません（%s）", visibility.OSErrorReason("", err))
	}
	text, _, derr := visibility.DecodeSourceBytes(data)
	if derr != nil {
		return "", "Error reading stdin: UTF-8 として読めません"
	}
	return text, ""
}
