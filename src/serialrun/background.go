package main

import (
	"path/filepath"
	"regexp"
	"strings"
)

// backgroundDenial は、長い検証 (longVerification) を含む単位を Bash ツール 1 回の中の素の `&` で背景に回すコマンドなら、
// 止める理由を返す。止めないなら空。
//
// なぜ止めるか: 素の `&` で起こした run は、完了の通知が来ず、ハーネスの停止 (TaskStop) も効かない。走っている間に同じ木を編集しても
// 何も言われず、古い版を検証した結果を新しい版のものと読む。規範だけでは止まらなかった。
// 規範: _claude/rules/verify-execution-not-just-exit-code.md の「非同期・background の完了も「成果物」で判定する」
//
// ## 脅威モデル
// 守るのは、Claude が 1 本の Bash の中で `make test … &` / `( make -C x lint test > log 2>&1; echo $? > rc ) &` のように
// 長い検証を素の & で背景に回す形だけ。単位は Bash ツールの shell (zsh) と同じく ; 改行 && || で切れ、| では切れない
// (zsh の `make test && echo ok &` は make test を前面で走らせる。理由は scanner の bgFrame)。
// ( … ) / { …; } / if・while・until・for・select の中にあれば、それを閉じた後ろの & も見る。
// 長い検証の語の正本は isHeavy (serial-run が直列化するもの) に、bin/skill-eval と tests/ の下の .sh を足したもの。
//
// ## 止めない形
//   - 長い検証のコマンド自身に nohup / setsid を付けた形、背景に回した & の直後の disown、zsh の `&|` / `&!`
//     (規範が認める detach。終了コードを run 固有のファイルへ書く形。disown は書かない形も通す)
//   - 後ろに wait がある形 (`make -C a test & make -C b test & wait`。同じ呼び出しの中で完了まで待つ)
//   - &&・|&・>&・&>・2>&1・引用符の中の &・長い検証を含まない単位の & (`make test; sleep 1 &`)
//
// ## 検出しない形 (読み切れないものは止めない。serial-run の hook は失敗で tool を止めない)
//   - heredoc (`<<`) と算術 (`((`) を字面に含むコマンド (引用の中にあるだけでも外れる)、scanner が読み切れない形 (関数定義・case・$'…' 等)
//   - 変数・関数・eval・スクリプトの中身・コマンド置換の中で背景に回す形、`zsh -c '… &'` の引用の中
//   - 長い検証を引用で包んだ呼び出し (`bash -c "make test" &`) と、一覧に無い前置き (caffeinate・stdbuf・sudo・xargs) を付けた形
//   - bash で走らせたときにだけ丸ごと背景に回る `make test && echo ok &` / `make test &&\n echo ok &` (Bash ツールの shell は zsh)
//   - 長い検証を Bash ツールの外 (別のスクリプト) で背景に回す形
//   - wait が待てない job (サブシェルの中で背景に回したもの) の後ろの wait も、待つ形として通す
//   - PATH 上の serial-run がこの checkout のものでない / SERIAL_RUN_HOOK_DISABLE=1 (_claude/hooks/serial-run-wrap.sh が Go の判定まで進まない)
func backgroundDenial(cmd string) string {
	if strings.Contains(cmd, "<<") || strings.Contains(cmd, "((") {
		return ""
	}
	s := scanner{src: cmd}
	if _, ok := s.scan(); !ok || s.bgHeavy <= 0 {
		return ""
	}
	return "長い検証 (make test / go test / mutate-verify 等) を、Bash ツール 1 回の中の素の & で背景に回しています。" +
		"完了の通知が来ず、TaskStop でも止められず、走っている間に同じ木を編集すると古い版を検証した結果が残ります (retro 1030 / 1049)。" +
		"Bash ツールの run_in_background: true で起こしてください (& は外す)。完了は通知で届きます。" +
		"ツールの上限を超えて生かす必要があるなら nohup で detach し、終了コードを run 固有のファイルへ書いてください。" +
		"規範: _claude/rules/verify-execution-not-just-exit-code.md の「非同期・background の完了も「成果物」で判定する」"
}

// testScriptRe は tests/ の下のシェルのテスト (`tests/claude/test_x.sh`・`./tests/…`・絶対パス)
var testScriptRe = regexp.MustCompile(`(^|/)tests/.*\.sh$`)

// longVerification は単純コマンドの語が、背景に回すのを止める長い検証か。
// 直列化の対象 (isHeavy) は全部含む。手で書いた `serial-run [オプション] -- <コマンド>` は中のコマンドで見る。
// 直列化はしないが長いもの (skill-eval・tests/ の下の .sh) もここで足す (isHeavy に足すと直列化の対象まで変わる)
// 🚨 コマンド名を足したら、hook の入口 (_claude/hooks/serial-run-wrap.sh) の手がかりの語にも足す
func longVerification(words []string) bool {
	if isSerialRun(words) {
		for i, w := range words {
			if w == "--" {
				words = words[i+1:]
				break
			}
		}
	}
	if isHeavy(words) {
		return true
	}
	w := unwrapPrefix(words)
	if len(w) == 0 {
		return false
	}
	switch filepath.Base(w[0]) {
	case "skill-eval":
		return true
	case "bash", "sh", "zsh":
		for i := 1; i < len(w); i++ {
			switch a := w[i]; {
			case a == "-o" || a == "+o" || a == "-O" || a == "+O":
				i++ // 値を取るオプション (`bash -o pipefail tests/x.sh`)
			case !strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "+"):
				return testScriptRe.MatchString(a)
			}
		}
		return false
	}
	return testScriptRe.MatchString(w[0])
}

// detachedCommand は単純コマンドが nohup / setsid で包まれているか。見るのはコマンド語と前置き (env 等) だけで、
// 引数 (`make -C ~/src/nohup test`) は見ない
func detachedCommand(words []string) bool {
	if len(words) > 0 && filepath.Base(words[0]) == "setsid" {
		return true
	}
	for _, w := range words[:len(words)-len(unwrapPrefix(words))] {
		if filepath.Base(w) == "nohup" {
			return true
		}
	}
	return false
}
