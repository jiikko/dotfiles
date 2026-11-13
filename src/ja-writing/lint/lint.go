// Package lint は ja-writing lint (表現と書式の検査) の Main を持つ。
//
// 本家 yomiyasu_lint.py の規則は群ごとに 1 ファイルずつ足す。土台 (候補を出さない部品) は
// emoji.go (絵文字。公開は EmojiMatches)・endmark.go (文末の飾り)・prose.go (文字の下ごしらえと、1 つの文書の解析と覚えを持つ document)。
// 本家の名前との対応は各ファイルの冒頭のコメントにある。
//
// 群は lint が文書ごとに作る document (解析 an と地の文の覚え) を受け取る形にする。
// bold_not_rendered は internal/bold の Problems(text, analysis) を呼ぶ lint/bold_rule.go が受け持ち、土台はそれに依らない。
package lint

import (
	"context"

	"ja-writing/cmdenv"
)

// Main は subcommand の入口。終了コードの意味は本家の argparse の定義に合わせる。
func Main(_ context.Context, env cmdenv.Env) int {
	return cmdenv.Unimplemented(env, "lint")
}
