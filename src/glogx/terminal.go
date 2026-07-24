package main

import "strings"

// 端末描画に対して外部由来の文字列を無害化する処理。GitHub API の知識は持たず、
// 「CI 由来のテキストが端末制御シーケンスを注入して枠描画を壊す/OSC52 でクリップボードを
// 触る」のを防ぐ純粋な文字列処理として github.go から分離している (結合ゼロ)。

// dropEmojiVS16 は絵文字異体字セレクタ VS16 (U+FE0F) を除去して、絵文字を text presentation
// (幅 1) へ倒す。⚠️ (U+26A0 + U+FE0F) のように「VS16 付きだと Terminal.app が幅 2 の色絵文字で
// 描くが runewidth は幅 1 と数える」文字は、glogx(1) と Terminal(2)・tmux の幅解釈が食い違い、
// glogx の毎秒再描画のたびにカーソル位置がずれて行がガタつく (ユーザー報告 2026-07-24:
// Terminal.app + tmux)。VS16 を外すと bare ⚠ (既定 text presentation・幅 1) になり glogx と
// Terminal が幅 1 で一致して揺れが止まる。commit メッセージ等の git 由来テキストに適用する。
// VS15 (U+FE0E, text 強制) は既に幅 1 なので触らない。ESC 判定と同様に高速パスを持つ。
func dropEmojiVS16(s string) string {
	const vs16 = '\ufe0f' // VS16 (emoji presentation selector)
	if !strings.ContainsRune(s, vs16) {
		return s
	}
	return strings.ReplaceAll(s, string(vs16), "")
}

// sanitizeDetailLine は CI 由来の表示文字列 (ログ・annotations・job 名) を端末描画に
// 対して無害化する。
//
//   - タブ → スペース 4: runewidth は \t を幅 0 と数えるが端末は 8 桁タブストップへ展開
//     するため、右枠の桁計算がずれて行が折り返し、インライン再描画が崩壊する (実測バグ)
//   - ANSI は SGR (ESC[…m = 色/装飾) だけを通す allowlist。それ以外の CSI (画面消去・
//     カーソル移動) や OSC/DCS 等 (OSC52 のクリップボード書き込み・タイトル変更) は、
//     CI 側の第三者 (任意の status インテグレーション等) が混入させられる端末制御
//     シーケンス注入の経路になるため、シーケンスごと落とす
//   - BOM (GitHub のログ先頭に付く U+FEFF) と \r 等の残る制御文字は落とす
func sanitizeDetailLine(s string) string {
	if !strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f || r == '\ufeff' }) {
		return s
	}
	rs := []rune(s)
	var b strings.Builder
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == '\t':
			b.WriteString("    ")
		case r == '\x1b':
			i = keepOnlySGR(&b, rs, i)
		case r < 0x20 || r == 0x7f || r == '\ufeff':
			// drop
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// keepOnlySGR は rs[i] の ESC から始まるシーケンスを解釈し、SGR (色/装飾) だけを b へ
// 書き出してそれ以外は捨てる。戻り値は消費したシーケンスの最終 index。
func keepOnlySGR(b *strings.Builder, rs []rune, i int) int {
	if i+1 >= len(rs) {
		return i // 末尾の裸 ESC は捨てる
	}
	switch rs[i+1] {
	case '[': // CSI: ESC [ <param/intermediate 0x20-0x3f>* <final 0x40-0x7e>
		j := i + 2
		for j < len(rs) && rs[j] >= 0x20 && rs[j] <= 0x3f {
			j++
		}
		if j >= len(rs) {
			return len(rs) - 1 // 途切れた CSI は捨てる
		}
		if rs[j] == 'm' && runesOnly(rs[i+2:j], "0123456789;:") {
			b.WriteString(string(rs[i : j+1])) // SGR のみ通す
		}
		return j
	case ']', 'P', '_', '^', 'X': // OSC / DCS / APC / PM / SOS: ST (ESC \) か BEL まで捨てる
		for j := i + 2; j < len(rs); j++ {
			if rs[j] == '\a' {
				return j
			}
			if rs[j] == '\x1b' && j+1 < len(rs) && rs[j+1] == '\\' {
				return j + 1
			}
		}
		return len(rs) - 1
	default:
		return i + 1 // その他の 2 文字エスケープ (ESC 7 等) は捨てる
	}
}

func runesOnly(rs []rune, allowed string) bool {
	for _, r := range rs {
		if !strings.ContainsRune(allowed, r) {
			return false
		}
	}
	return true
}
