// Command tmpclean は一時領域 (Claude Code の scratchpad・$TMPDIR) を数え、承認された古いものだけをゴミ箱へ移す。
// 使い方と終了コードは tmpclean -h。本体は package tmpclean の Run。
package main

import (
	"os"

	"tmpclean"
)

func main() { os.Exit(tmpclean.Run(os.Args[1:], os.Stdout, os.Stderr)) }
