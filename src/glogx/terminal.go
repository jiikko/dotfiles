package main

import "strings"

// 端末描画に対して外部由来の文字列を無害化する処理。GitHub API の知識は持たず、
// 「CI 由来のテキストが端末制御シーケンスを注入して枠描画を壊す/OSC52 でクリップボードを
// 触る」のを防ぐ純粋な文字列処理として github.go から分離している (結合ゼロ)。

// sanitizeDetailLine は CI 由来の表示文字列 (ログ・annotations・job 名) を端末描画に
// 対して無害化する。
//
//   - タブ → スペース 4: 幅計算 (dispWidth) は \t を幅 0 と数えるが端末は 8 桁タブストップへ展開
//     するため、右枠の桁計算がずれて行が折り返し、インライン再描画が崩壊する (実測バグ)
//   - ANSI は SGR (ESC[…m = 色/装飾) だけを通す allowlist。それ以外の CSI (画面消去・
//     カーソル移動) や OSC/DCS 等 (OSC52 のクリップボード書き込み・タイトル変更) は、
//     CI 側の第三者 (任意の status インテグレーション等) が混入させられる端末制御
//     シーケンス注入の経路になるため、シーケンスごと落とす
//   - BOM (GitHub のログ先頭に付く U+FEFF) と \r 等の残る制御文字は落とす
//   - VS16 付き絵文字 (⚠️ 等) は描画エンジンと端末で幅が食い違いガタつくため bare 記号へ
//     正規化する (dropEmojiVS16 参照。git 由来テキストは gitlog.go 側の入口で同じ正規化)
func sanitizeDetailLine(s string) string {
	return sanitizeLine(s, false)
}

// sanitizeLineKeepTabs \u306f sanitizeDetailLine \u306e\u30bf\u30d6\u975e\u5c55\u958b\u7248\u3002git \u7531\u6765\u30c6\u30ad\u30b9\u30c8 (commit \u306e
// subject/message/body\u30fbverbatim \u884c) \u306f\u3001\u9759\u7684\u51fa\u529b (--no-pager / \u30d1\u30a4\u30d7) \u3067 git log \u3068\u306e
// \u30d1\u30ea\u30c6\u30a3 (\u30bf\u30d6\u4fdd\u6301) \u304c\u5951\u7d04\u3055\u308c\u3066\u3044\u308b (render_test \u306e \u2026TabsInTUI \u304c pin) \u305f\u3081\u3001\u5165\u53e3\u3067\u306f
// \u5236\u5fa1\u30b7\u30fc\u30b1\u30f3\u30b9\u306e\u9664\u53bb\u3060\u3051\u884c\u3044\u3001\u30bf\u30d6\u5c55\u958b\u306f TUI \u63cf\u753b\u5074 (Width > 0 \u306e mediumLines /
// decorateVerbatim) \u304c\u62c5\u3046\u3002
func sanitizeLineKeepTabs(s string) string {
	return sanitizeLine(s, true)
}

func sanitizeLine(s string, keepTabs bool) string {
	s = dropEmojiVS16(s)
	if !strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f || r == '\ufeff' }) {
		return s
	}
	rs := []rune(s)
	var b strings.Builder
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == '\t' && keepTabs:
			b.WriteRune(r)
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
