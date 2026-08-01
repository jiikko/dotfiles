// 対話ブラウズ中はキー操作 (j/k/q/b 等) が主なので、起動時に IME を英数 (ABC) へ
// 切り替え、終了時に元へ戻す。現在ソースの取得・切替・反映確認はすべて macOS の
// Text Input Source Services (TIS) を直接呼び出して行う。
package main

import "time"

// asciiInputSource は macOS 標準の英数キーボードレイアウトの入力ソース ID。
const asciiInputSource = "com.apple.keylayout.ABC"

// imeSwitch は TUI 起動前に取得した入力ソースを保持する。取得は read-only なので、対話表示に
// なる可能性がある時点で先に済ませても安全。ok=false は TIS が利用できない環境を表す。
type imeSwitch struct {
	prev string
	ok   bool
}

// テストで実マシンの入力ソースを変更しないための差し替え点。
var (
	currentInputSource = tisCurrentSourceID
	selectInputSource  = tisSelectSourceID
)

// beginIMESwitch は現在の入力ソースを TIS から取得する。
func beginIMESwitch() *imeSwitch {
	prev, ok := currentInputSource()
	return &imeSwitch{prev: prev, ok: ok && prev != ""}
}

// switchInputSource は TIS で入力ソースを選択し、現在ソース ID が実際に変わったことを確認する。
// TISSelectInputSource は要求発行後すぐ返り、OS 側の反映が数 ms 遅れうるため、確認できるまで
// 最大 50ms 待つ。TIS の選択結果と現在ソース ID だけを成功判定に使う。
func switchInputSource(id string) bool {
	if !selectInputSource(id) {
		return false
	}
	for range 50 {
		if cur, ok := currentInputSource(); ok && cur == id {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return false
}

// finish は TUI 開始前に入力ソースを英数へ切り替え、終了時に元へ戻す関数を返す。
// TIS が使えない、または切替結果を確認できない場合も glogx 本体は継続し、警告だけを返す。
func (s *imeSwitch) finish() (restore func(), warn string) {
	noop := func() {}
	if s == nil {
		// 非 TTY / --no-pager では入力ソースを触らない。
		return noop, ""
	}
	if !s.ok {
		return noop, "TIS で現在の入力ソースを取得できませんでした"
	}
	if s.prev == asciiInputSource {
		return noop, ""
	}
	if !switchInputSource(asciiInputSource) {
		return noop, "TIS で英数への切替を確認できませんでした"
	}
	return func() {
		// TUI は既に閉じていて警告を表示できないため、復元失敗は握りつぶす。
		_ = switchInputSource(s.prev)
	}, ""
}
