// Command codex-events は、codex exec --json の出力 (events.jsonl) を読み、run が最後まで終わったかを判定して記録する。
//
//	codex-events inspect <events.jsonl> <最後の応答のファイル> <meta.json> <log>
//	codex-events is-object <ファイル>    (JSON のオブジェクトなら rc=0。codex-fanout の -S の schema の事前確認)
//	codex-events help
//
// inspect は meta.json を書き、log に応答の本文 (と、終わっていないならその旨) を追記する。rc は完了なら 0、そうでなければ 1。
// 呼び出し元は bin/codex-fanout (-J)。Python 版 (bin/lib/codex-events.py) を置き換えた (issue 670)。Python 版と意図的に変えた点は
// issue 670 の「Python 版と変えたこと」。
package main

import (
	"fmt"
	"io"
	"os"
)

const usage = `usage:
  codex-events inspect <events.jsonl> <response> <meta.json> <log>
  codex-events is-object <file>
  codex-events help
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "inspect":
		if len(args) != 5 {
			fmt.Fprint(stderr, usage)
			return 2
		}
		return inspectMain(args[1], args[2], args[3], args[4], stderr)
	case "is-object":
		if len(args) != 2 {
			fmt.Fprint(stderr, usage)
			return 2
		}
		return isObjectMain(args[1], stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	}
	fmt.Fprint(stderr, usage)
	return 2
}
