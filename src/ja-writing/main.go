// Command ja-writing は、日本語の文章の読みやすさを点検する本家 yomiyasu (Python) の Go 実装。
//
//	ja-writing lint  [オプション] [ファイル]
//	ja-writing diff  [オプション] 元の文 書き直した文
//	ja-writing check [run|verify|start|export|end] [オプション]
//	ja-writing hook  --host claude-code|codex
//
// 各 subcommand は <名前>.Main(ctx, env) の形で、本家の yomiyasu_<名前>.py の main と同じ終了コードを返す。
// subcommand の表とシグナルの受け方は entry にある (check のテストの子も同じ入口を通る)。
package main

import (
	"os"

	"ja-writing/entry"
)

func main() {
	os.Exit(entry.Run(os.Args[1:], entry.Main))
}
