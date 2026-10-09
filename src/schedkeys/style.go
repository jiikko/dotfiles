// 端末装飾 (SGR) の最小限のヘルパー。lipgloss は使わない: 依存を増やさずに済む量で、
// 幅計算は termwidth.Of (tuikit) に任せるため。
//
// 🚨 表示文字列に絵文字・曖昧幅の記号を混ぜない (端末と描画側の幅計算が食い違い、行ごとに
//
//	左右へずれる)。装飾は色と反転だけで表し、記号は ASCII に限る。
//	tests/tmux/test_schedule_keys.sh がこの規律を静的に検査する。
package main

import (
	sgrcode "github.com/jiikko/dotfiles/src/tuikit/sgr"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

// 色は基本 8 色 + 既定色に限る (端末のテーマに従わせる。256 色を決め打ちすると
// 明るい背景のテーマで読めなくなる)。
const (
	bold      = "1"
	fgDim     = "2"  // 見出し・補助
	fgAccent  = "36" // フォーカス中の欄 (popup の枠と同じ cyan)
	fgOK      = "32" // 発火時刻
	fgErr     = "31" // 入力エラー
	revAccent = "7;36"
	revOK     = "7;32" // トースト (成功) の反転
)

func sgr(style, s string) string {
	if s == "" {
		return ""
	}
	return "\x1b[" + style + "m" + s + sgrcode.Reset
}

// stripSGR は ESC のシーケンスを外す (読み方は termwidth.StripSGR = x/ansi のパーサ)。
func stripSGR(s string) string { return termwidth.StripSGR(s) }
