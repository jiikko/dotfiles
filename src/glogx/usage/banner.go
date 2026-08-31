package usage

// CLI 名の大見出し (ブロック文字の AA)。カードごとに小さく "Claude Code" を繰り返すより、
// 段の頭に 1 回大きく出す方が「どちらの CLI の枠か」が一目で分かる (ユーザー要望 2026-09-01)。
//
// ⚠️ 対応していない文字が 1 つでもあれば nil を返す。呼び出し側が素のテキスト見出しへ落ちる。
// 欠けた字を空白で埋めて出すと「見出しが壊れている」ようにしか見えないため。

import "strings"

// bannerRows はグリフの行数。bannerGlyphW は 1 文字の桁数 (字間 1 桁は組むときに足す)。
const (
	bannerRows   = 3
	bannerGlyphW = 3
)

// bannerGlyphs は 3 行 x 3 桁のブロック文字。下段が ▀ (上半分) なのは、3 行で
// 「上・中・下端」を表すこの手の字形の作法 (下端の線を半分の高さで描く)。
//
// 収録は実際に使う字だけ (CLI 名は Snapshot の出所から決まるので増えない)。増やすときは
// 3 桁で他の字と見分けが付くかを確かめること — M/N/W は 3 桁だと潰れる。
var bannerGlyphs = map[rune][bannerRows]string{
	'A': {"▄▀▄", "█▀█", "▀ ▀"},
	'C': {"▄▀▀", "█  ", "▀▀▀"},
	'D': {"█▀▄", "█ █", "▀▀ "},
	'E': {"█▀▀", "█▀▀", "▀▀▀"},
	'L': {"█  ", "█  ", "▀▀▀"},
	'O': {"▄▀▄", "█ █", "▀▀▀"},
	'U': {"█ █", "█ █", "▀▀▀"},
	'X': {"█ █", " █ ", "▀ ▀"},
	' ': {"   ", "   ", "   "},
}

// bannerLines は s (大文字化して照合) を bannerRows 行の AA にする。1 字でも収録外なら nil。
func bannerLines(s string) []string {
	runes := []rune(strings.ToUpper(s))
	if len(runes) == 0 {
		return nil
	}
	rows := make([]strings.Builder, bannerRows)
	for i, r := range runes {
		g, ok := bannerGlyphs[r]
		if !ok {
			return nil
		}
		for k := range bannerRows {
			if i > 0 {
				rows[k].WriteByte(' ') // 字間
			}
			rows[k].WriteString(g[k])
		}
	}
	out := make([]string, bannerRows)
	for k := range bannerRows {
		out[k] = rows[k].String()
	}
	return out
}

// bannerWidth は bannerLines が返す AA の桁数 (字数から計算できるので描かずに測れる)。
func bannerWidth(s string) int {
	n := len([]rune(s))
	if n == 0 {
		return 0
	}
	return n*bannerGlyphW + (n - 1)
}
