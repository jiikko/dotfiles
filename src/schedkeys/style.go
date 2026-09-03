// 端末装飾 (SGR) の最小限のヘルパー。lipgloss は使わない: 依存を増やさずに済む量で、
// 幅計算は ansi.StringWidth に任せるため。
//
// 🚨 表示文字列に絵文字・曖昧幅の記号を混ぜない (端末と描画側の幅計算が食い違い、行ごとに
//
//	左右へずれる)。装飾は色と反転だけで表し、記号は ASCII に限る。
//	tests/tmux/test_schedule_keys.sh がこの規律を静的に検査する。
package main

import "strings"

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
	return "\x1b[" + style + "m" + s + "\x1b[0m"
}

// stripSGR は幅を測るために装飾を落とす (ansi.StringWidth は装飾を無視するが、
// 自前で桁を数える箇所では素の文字列が要る)。
func stripSGR(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
