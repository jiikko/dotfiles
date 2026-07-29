// 対話ブラウズ中はキー操作 (j/k/q/b 等) が主なので、起動時に IME を英数 (ABC) へ
// 切り替え、終了時に元へ戻す。取得と英数への切替は TIS 直接呼び出し (ime_tis_darwin.go、
// fork なし) を優先し、失敗時と終了時の復元 (CJK IM への切替 = TIS の不安定ケース) は
// macism CLI (brew: laishulu/homebrew/macism) に委譲する。macism は外部 CLI なので、その
// 仕様変更・異常終了・想定外出力が glogx 本体を巻き込まない (クラッシュさせない) よう、
// あらゆる失敗を封じ込めて no-op + 警告文に落とす。
// 警告文 (macism がエラーになった旨) は呼び出し側が toast で見せる (ユーザー要望 2026-07-23)。
package main

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// asciiInputSource は macOS 標準の英数キーボードレイアウトの入力ソース ID。
const asciiInputSource = "com.apple.keylayout.ABC"

// imeCurrent は「現在の入力ソースを問い合わせた結果」(macism fork 1 本目の産物)。
// cli が空なら macism 未導入 = 何もしない。
type imeCurrent struct {
	cli  string
	prev string
	warn string
}

// imeSwitch は英数への切替を 2 段に分けたハンドル。取得・切替とも TIS 直接呼び出し
// (ime_tis_darwin.go、fork なし) を優先し、失敗時だけ macism fork (1 本 ≈ 40-60ms、実測
// 2026-07-25/2026-07-29) へ fallback する。macism 2 fork 直列 = 80-90ms が起動の律速だった。
// macism 未導入なら TIS が使えても切り替えない (終了時の復元 = CJK IM への切替は
// TISSelectInputSource の不安定ケースで macism にしか任せられないため。復元手段がないのに
// 切り替えると英数に置き去りにする)。
//
// ⚠️ 完全非同期化はできない (切替を TUI 開始後へ回さないこと): raw mode でも IME は OS の
// 入力ソース層で効くため、切替完了前の打鍵は日本語 IME の composition に吸われてアプリに
// 届かない。「先出し + join」に留める。TIS 直接切替でも反映確認 (confirm ループ) で
// この不変条件を守る。
type imeSwitch struct{ ch <-chan imeCurrent }

// beginIMESwitch は現在の入力ソース取得を開始する。呼び出し側は TUI を開始する直前に
// finish() で join する。TIS 直読みは main goroutine 上で同期に行い (fork なし ~µs。TIS の
// スレッド安全性が保証されないため goroutine へ出さない)、goroutine 側には macism の
// LookPath と fallback の fork だけを残す。
func beginIMESwitch() *imeSwitch {
	ch := make(chan imeCurrent, 1) // バッファ 1 = 受け取り手が居なくても goroutine が詰まらない
	tisPrev, tisOK := tisCurrentSourceID()
	go func() {
		// goroutine 側の panic は main の recover では拾えないのでここで封じ込める
		defer func() {
			if r := recover(); r != nil {
				ch <- imeCurrent{warn: fmt.Sprintf("macism 実行で想定外のエラー: %v", r)}
			}
		}()
		cli, err := exec.LookPath("macism")
		if err != nil {
			// 未導入は起動時チェック (macismInstalled) 側の brew 案内に委ね、ここでは warn を
			// 出さず no-op (二重通知の回避)。IME は切り替わらないが機能は壊れない (オプトイン)。
			ch <- imeCurrent{}
			return
		}
		if tisOK && tisPrev != "" {
			ch <- imeCurrent{cli: cli, prev: tisPrev}
			return
		}
		prev, warn := macismCurrentWarn(exec.Command(cli).Output())
		ch <- imeCurrent{cli: cli, prev: prev, warn: warn}
	}()
	return &imeSwitch{ch: ch}
}

// tisSwitchASCII はテストで実 TIS を叩かない (実行マシンの IME を実際に切り替えない) ための
// 差し替え点 (loadCommitDiff と同型)。
var tisSwitchASCII = tisSwitchToASCII

// tisSwitchToASCII は TIS 直接呼び出しで英数へ切り替え、反映まで確認する。
// TISSelectInputSource は要求発行後すぐ返り OS 側の反映が数 ms 遅れうるため、現在ソースが
// 変わったことを確認して初めて成功とする (「切替完了前に TUI がキーを読まない」不変条件)。
// false なら呼び出し側が macism fork へ fallback する。
func tisSwitchToASCII() bool {
	if !tisSelectSourceID(asciiInputSource) {
		return false
	}
	for range 50 {
		if cur, ok := tisCurrentSourceID(); ok && cur == asciiInputSource {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return false
}

// finish は取得完了を待って入力ソースを英数へ切り替え、元へ戻す restore を返す。第 2 戻り値 warn
// はユーザーへ toast で見せる 1 行警告 (macism が導入済みなのにエラーになった旨)。正常時 (切替
// 成功・既に英数) と未導入時は warn="" (未導入の案内は呼び出し側の起動時チェックに委ねる)。
//
// ⚠️ macism の失敗 (非ゼロ終了・想定外出力・panic) はすべてここで封じ込め、glogx 本体を
// クラッシュさせない。実際の IME 切替は失敗時 no-op のままで機能は壊さない。呼び出し側は warn が
// 空でなければ toast (error) で通知する。
func (s *imeSwitch) finish() (restore func(), warn string) {
	noop := func() {}
	if s == nil {
		// beginIMESwitch を呼んでいない経路 (非 TTY / --no-pager では IME を触らない)。
		// 呼び出し順の前提を finish 側で受けて、nil 受け取りで panic しないようにする。
		return noop, ""
	}
	// 想定外の panic も含めて封じ込める (仕様変更で macism の出力/挙動が変わっても glogx は
	// 落とさず、noop + 警告で継続する)。
	defer func() {
		if r := recover(); r != nil {
			restore, warn = noop, fmt.Sprintf("macism 実行で想定外のエラー: %v", r)
		}
	}()
	cur := <-s.ch
	if cur.warn != "" {
		return noop, cur.warn
	}
	if cur.cli == "" {
		return noop, "" // macism 未導入 (案内は起動時チェック側)
	}
	if cur.prev == asciiInputSource {
		return noop, "" // 既に英数: 何もしないのが正常 (警告なし)
	}
	// TIS 直接切替 (fork なし) を優先し、失敗時のみ macism fork へ fallback。
	// 切替は CombinedOutput で macism の出力も拾い、失敗時の toast に理由を含める。
	if !tisSwitchASCII() {
		if out, err := exec.Command(cur.cli, asciiInputSource).CombinedOutput(); err != nil {
			detail := firstLine(err.Error())
			if s := firstLine(string(out)); s != "" {
				detail = s
			}
			return noop, "macism で英数への切替に失敗しました: " + detail
		}
	}
	return func() {
		// 終了時の復元。ここでの失敗・想定外は封じて握りつぶす (TUI は既に閉じており toast も
		// 出せないため。復元漏れは次回起動時に手動で戻せる範囲の軽微な影響)。
		defer func() { _ = recover() }()
		_ = exec.Command(cur.cli, cur.prev).Run()
	}, ""
}

// macismCurrentWarn は macism (引数なし) の出力/エラーから、現在の入力ソース prev と警告 warn を
// 決める純関数 (exec から分離しテスト可能にする)。失敗 (非ゼロ終了) は stderr を、成功したのに
// 出力が空 (仕様変更で現在ソースを返さなくなった等) も「取れなかった」として警告にする。
// 正常時は (prev, "")。warn が空でないとき prev は "" (呼び出し側は warn を優先)。
func macismCurrentWarn(out []byte, err error) (prev, warn string) {
	if err != nil {
		detail := firstLine(err.Error())
		// Output() の非ゼロ終了は *exec.ExitError に stderr が入る。"exit status N" より
		// macism 自身のエラー文の方が診断に有用なので優先して載せる。
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			detail = firstLine(string(ee.Stderr))
		}
		return "", "macism の現在の入力ソース取得に失敗しました: " + detail
	}
	prev = strings.TrimSpace(string(out))
	if prev == "" {
		return "", "macism が現在の入力ソースを返しませんでした (仕様変更の可能性)"
	}
	return prev, ""
}
