package usage

// ブロック文字の AA。CLI 名の大見出し (段の頭) と、盤の中央の使用率に使う。
//
// 字形は 3 行ではなく **5 段のピクセル** で持ち、半ブロック (█ ▀ ▄) で 2 段ずつ 1 行へ
// 詰めて描く。3 行 = 6 段ぶんの縦解像度が取れるので、5 段の字形がそのまま入る。
// 字形を直接 3 行の文字列で書くと段の対応が人の頭の中にしか無く、実際 0/3/6/8/9 を
// 手書きしたときに 4 つとも別々の作りになって潰れた (ユーザー指摘 2026-09-01)。
//
// ⚠️ 収録外の文字が 1 つでもあれば nil を返す。呼び出し側が普通の字へ落ちる。欠けた字を
// 空白で埋めて出すと「見出しが壊れている」ようにしか見えないため。

import (
	"strings"

	"glogx/termwidth"
)

const (
	bannerRows   = 3 // 描画後の行数 (5 段を 2 段ずつ詰めて 3 行)
	pixelRows    = 5 // 字形の段数
	bannerGlyphW = 3 // 見出しの字幅
	digitGlyphW  = 4 // 盤の中央の数字の字幅 (3 桁だと 0/6/8/9 の空きが潰れる)
)

// bannerPixels は見出し用の 3x5 字形 ('#' = 点灯)。収録は実際に使う字だけ (CLI 名は
// Snapshot の出所から決まるので増えない)。増やすときは 3 桁で他の字と見分けが付くかを
// 確かめること — M/N/W は 3 桁だと潰れる。
var bannerPixels = map[rune][pixelRows]string{
	'A': {" # ", "# #", "###", "# #", "# #"},
	'C': {" ##", "#  ", "#  ", "#  ", " ##"},
	'D': {"## ", "# #", "# #", "# #", "## "},
	'E': {"###", "#  ", "## ", "#  ", "###"},
	'L': {"#  ", "#  ", "#  ", "#  ", "###"},
	'O': {"###", "# #", "# #", "# #", "###"},
	'U': {"# #", "# #", "# #", "# #", "###"},
	'X': {"# #", "# #", " # ", "# #", "# #"},
	' ': {"   ", "   ", "   ", "   ", "   "},
}

// digitPixels は盤の中央に置く数字の 4x5 字形。見出しより 1 桁広いのは、3 桁だと
// 0 と 8、6 と 5 の区別が付かなくなるため (実測で比較して 4 桁を採用)。
var digitPixels = map[rune][pixelRows]string{
	'0': {"####", "#  #", "#  #", "#  #", "####"},
	'1': {"  # ", " ## ", "  # ", "  # ", "####"},
	'2': {"####", "   #", "####", "#   ", "####"},
	'3': {"####", "   #", " ###", "   #", "####"},
	'4': {"#  #", "#  #", "####", "   #", "   #"},
	'5': {"####", "#   ", "####", "   #", "####"},
	'6': {"####", "#   ", "####", "#  #", "####"},
	'7': {"####", "   #", "  # ", " #  ", " #  "},
	'8': {"####", "#  #", "####", "#  #", "####"},
	'9': {"####", "#  #", "####", "   #", "####"},
}

// packPixels は 5 段の字形を半ブロックで 3 行へ詰める (6 段目は空)。
// 上下とも点灯 = █ / 上だけ = ▀ / 下だけ = ▄ / どちらも消灯 = 空白。
func packPixels(px [pixelRows]string, w int) [bannerRows]string {
	rows := [pixelRows + 1]string{px[0], px[1], px[2], px[3], px[4], termwidth.PadSpaces(w)}
	var out [bannerRows]string
	for r := range bannerRows {
		top, bot := rows[r*2], rows[r*2+1]
		var b strings.Builder
		for c := range w {
			t, d := top[c] == '#', bot[c] == '#'
			switch {
			case t && d:
				b.WriteRune('█')
			case t:
				b.WriteRune('▀')
			case d:
				b.WriteRune('▄')
			default:
				b.WriteByte(' ')
			}
		}
		out[r] = b.String()
	}
	return out
}

// pixelLines は s を table の字形で bannerRows 行の AA にする。1 字でも収録外なら nil。
func pixelLines(s string, table map[rune][pixelRows]string, w int) []string {
	runes := []rune(s)
	if len(runes) == 0 {
		return nil
	}
	rows := make([]strings.Builder, bannerRows)
	for i, r := range runes {
		px, ok := table[r]
		if !ok {
			return nil
		}
		g := packPixels(px, w)
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

// pixelWidth は pixelLines が返す AA の桁数 (字数から計算できるので描かずに測れる)。
//
// ⚠️ termwidth.Of を介さず字数から計算している。幅の単一出典 (CLAUDE.md) の例外だが、
// 字形表が閉じた ASCII 集合 (A-Z の一部と数字) で全角を含まないため桁数 = 字数 x 字幅 で
// 決まる。実際の描画幅との一致は TestBannerLinesShape / TestDigitLinesShapeAndRejects が
// termwidth.Of で固定している。全角を収録するなら描いてから測る形へ変えること。
func pixelWidth(s string, w int) int {
	n := len([]rune(s))
	if n == 0 {
		return 0
	}
	return n*w + (n - 1)
}

// bannerLines は CLI 名を見出し用の AA にする (大文字化して照合)。
func bannerLines(s string) []string {
	return pixelLines(strings.ToUpper(s), bannerPixels, bannerGlyphW)
}

// bannerWidth は bannerLines が返す AA の桁数。
func bannerWidth(s string) int { return pixelWidth(s, bannerGlyphW) }

// digitLines は数字列を盤の中央用の AA にする (数字以外は nil)。
func digitLines(s string) []string { return pixelLines(s, digitPixels, digitGlyphW) }

// digitWidth は digitLines が返す AA の桁数。
func digitWidth(s string) int { return pixelWidth(s, digitGlyphW) }
