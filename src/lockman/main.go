// lockman — ディレクトリ単位の排他を取る CLI (SMB 越しの複数マシン + 公開ホストの
// ローカル経路が混在する前提)。仕様の正本は issues/091-feat-lockman-directory-lease-lock.md。
//
// ⚠️ 現状は骨組みだけ。各サブコマンドは未実装で exit 125 を返す。実装に入る前に、
// issue の「実測が要る前提」(link(2) の可否 / mtime を打刻するのはホストかクライアントか /
// smbfs が fcntl ロックを送るか / キャッシュの遅れ) を macOS 実機で測ること。
package main

import (
	"fmt"
	"os"
	"sort"
)

// 終了コードは呼び出し側の shell が分岐に使う API。issue 091 の表と一致させること。
//
// acquire / release / renew / check / status:
//
//	0 成功 / 1 エラー (判定不能を含む) / 3 他者が保持中 / 4 持ち主ではない
//
// with は子プロセスの終了コードをそのまま透過するため、ロック側の失敗は
// 子と衝突しない上位の番号へ逃がす (timeout(1) / env(1) と同じ流儀)。
const (
	exitOK          = 0
	exitError       = 1
	exitBusy        = 3
	exitNotOwner    = 4
	exitWithBusy    = 121
	exitWithLost    = 122
	exitWithInvalid = 125
)

// commands はサブコマンド名とその 1 行説明。usage の出典をここ 1 つに保つ
// (説明文を別に持つと片方だけ古くなる)。
var commands = map[string]string{
	"acquire": "ロックを取る (取れなければ exit 3)",
	"release": "自分が取ったロックを解放する",
	"renew":   "保持を更新する (失っていれば exit 4)",
	"check":   "空いているかを見る (参考値。排他の根拠にはならない)",
	"status":  "誰が・いつから・あと何秒かを表示する",
	"with":    "取得 → コマンド実行 → 確実に解放",
	"break":   "人が今すぐ剥がす (graveyard へ退避する)",
	"cleanup": "残骸の掃除だけを明示的に走らせる",
}

func usage(w *os.File) {
	fmt.Fprintf(w, "usage: lockman <command> <dir> [options]\n\ncommands:\n")
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(w, "  %-8s %s\n", name, commands[name])
	}
	fmt.Fprintf(w, "\n仕様: issues/091-feat-lockman-directory-lease-lock.md\n")
}

// run は終了コードを返す。os.Exit をここに書かないのはテストから呼べるようにするため。
func run(args []string) int {
	if len(args) == 0 {
		usage(os.Stderr)
		return exitError
	}
	name := args[0]
	if name == "-h" || name == "--help" || name == "help" {
		usage(os.Stdout)
		return exitOK
	}
	if _, ok := commands[name]; !ok {
		fmt.Fprintf(os.Stderr, "lockman: 未知のサブコマンド: %s\n\n", name)
		usage(os.Stderr)
		return exitError
	}
	// ⚠️ 未実装を「成功」や「空いている」に倒さない。判定できないことを呼び出し側へ返す。
	fmt.Fprintf(os.Stderr, "lockman: %s は未実装 (issues/091 を参照)\n", name)
	return exitWithInvalid
}

func main() {
	os.Exit(run(os.Args[1:]))
}
