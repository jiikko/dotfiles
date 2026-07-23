// 対話ブラウズ中はキー操作 (j/k/q/b 等) が主なので、起動時に IME を英数 (ABC) へ
// 切り替え、終了時に元へ戻す。切り替えは macism CLI (brew: laishulu/homebrew/macism)
// に委譲し、未インストールなら何もしない (オプトイン。glogx 本体は macism に依存しない)。
package main

import (
	"os/exec"
	"strings"
)

// asciiInputSource は macOS 標準の英数キーボードレイアウトの入力ソース ID。
const asciiInputSource = "com.apple.keylayout.ABC"

// switchIMEToASCII は入力ソースを英数へ切り替え、元へ戻すための関数を返す。
// macism が無い / 現在値の取得に失敗した / 既に英数、のいずれでも安全に no-op を返す。
func switchIMEToASCII() (restore func()) {
	noop := func() {}
	cli, err := exec.LookPath("macism")
	if err != nil {
		return noop
	}
	out, err := exec.Command(cli).Output()
	prev := strings.TrimSpace(string(out))
	if err != nil || prev == "" || prev == asciiInputSource {
		return noop
	}
	if exec.Command(cli, asciiInputSource).Run() != nil {
		return noop
	}
	return func() { _ = exec.Command(cli, prev).Run() }
}
