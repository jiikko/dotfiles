// 対話ブラウズ中はキー操作 (j/k/q/b 等) が主なので、起動時に IME を英数 (ABC) へ
// 切り替え、終了時に元へ戻す。切り替えは macism CLI (brew: laishulu/homebrew/macism)
// に委譲する。macism は外部 CLI なので、その仕様変更・異常終了・想定外出力が glogx 本体を
// 巻き込まない (クラッシュさせない) よう、あらゆる失敗を封じ込めて no-op + 警告文に落とす。
// 警告文 (未導入の brew 案内 / 実行エラー) は呼び出し側が toast で見せる (ユーザー要望 2026-07-23)。
package main

import (
	"fmt"
	"os/exec"
	"strings"
)

// asciiInputSource は macOS 標準の英数キーボードレイアウトの入力ソース ID。
const asciiInputSource = "com.apple.keylayout.ABC"

// switchIMEToASCII は入力ソースを英数へ切り替え、元へ戻す restore を返す。第 2 戻り値 warn は
// ユーザーへ toast で見せる 1 行警告 (macism が導入済みなのにエラーになった旨)。正常時 (切替成功・
// 既に英数・取得が空) と未導入時は warn="" (未導入の案内は呼び出し側の起動時チェックに委ねる)。
//
// ⚠️ macism の失敗 (非ゼロ終了・想定外出力・panic) はすべてここで封じ込め、glogx 本体を
// クラッシュさせない。実際の IME 切替は失敗時 no-op のままで機能は壊さない。呼び出し側は warn が
// 空でなければ toast (error) で通知する。
func switchIMEToASCII() (restore func(), warn string) {
	noop := func() {}
	cli, err := exec.LookPath("macism")
	if err != nil {
		// 未導入は起動時チェック (macismInstalled) 側の brew 案内に委ね、ここでは warn を出さず
		// no-op (二重通知の回避)。IME は切り替わらないが機能は壊れない (オプトイン)。
		return noop, ""
	}
	// 想定外の panic も含めて封じ込める (仕様変更で macism の出力/挙動が変わっても glogx は
	// 落とさず、noop + 警告で継続する)。
	defer func() {
		if r := recover(); r != nil {
			restore, warn = noop, fmt.Sprintf("macism 実行で想定外のエラー: %v", r)
		}
	}()
	out, err := exec.Command(cli).Output()
	if err != nil {
		return noop, "macism の現在の入力ソース取得に失敗しました: " + firstLine(err.Error())
	}
	prev := strings.TrimSpace(string(out))
	if prev == "" || prev == asciiInputSource {
		return noop, "" // 取得が空 / 既に英数: 何もしないのが正常 (警告なし)
	}
	if err := exec.Command(cli, asciiInputSource).Run(); err != nil {
		return noop, "macism で英数への切替に失敗しました: " + firstLine(err.Error())
	}
	return func() {
		// 終了時の復元。ここでの失敗・想定外は封じて握りつぶす (TUI は既に閉じており toast も
		// 出せないため。復元漏れは次回起動時に手動で戻せる範囲の軽微な影響)。
		defer func() { _ = recover() }()
		_ = exec.Command(cli, prev).Run()
	}, ""
}
