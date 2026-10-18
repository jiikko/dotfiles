package ui

import (
	"hash/fnv"
	"strconv"
	"strings"

	"github.com/jiikko/dotfiles/src/tuikit/sgr"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"

	"pro-con/card"
)

// 見た目 (2026-09-24 に B「枠」で合意): 列と詳細を角丸の罫線で囲み、選択中のカードがある列の枠だけを現在地色にする。
// 色の意味は theme/colors.yml に揃える。番号は下の col* だけに書き、TestThemeColorsMatchYML が yml と突き合わせる
// (Go の生成器は無い。glogx の ansiFrameBorder と同じ事情)。

// テーマの色 (theme/colors.yml の role の cterm)。🚨 ここ以外に番号を書かない (yml を変えたときに黙ってずれる)。
const (
	colCurrent   = 202 // current_accent: 現在地
	colAttention = 214 // quantity_yellow の色を「要対応」に使う (yml に要対応の role は無い。issue 770)
	colMarker    = 208 // marker_orange: 未 push (まだ master に載っていない)
	colInfo      = 51  // info_cyan: 情報
	colActive    = 46  // active_green: 正常
	colCold      = 240 // cold_gray: 消灯
	colError     = 196 // error_red: 危険
)

func fg(n int) string { return "\x1b[38;5;" + strconv.Itoa(n) + "m" }
func bg(n int) string { return "\x1b[48;5;" + strconv.Itoa(n) + "m" }

// waitDim は待っているカードの地の明度の倍率 (×0.7。2026-09-27 にユーザーが見本の ×0.55 / ×0.7 / ×0.8 から選んだ。×0.55 はほぼ黒で「色なし」に見えた)。
const waitDim = 0.7

// bgDim は 256 色の n を、色相を保ったまま明度を k 倍にした地 (RGB で出す)。cardPalette は cube の暗い段なので、
// 256 色のままでは色相を保って暗くできない。truecolor が通らない端末では近い 256 色に落ちる (ユーザー了承済み)。
// k = 1 はカードの普段の地 (256 色の番号で塗ると端末の設定しだいで色が変わるので RGB で塗る。2026-09-27 のユーザーの決定)。
func bgDim(n int, k float64) string {
	r, g, b := sgr.XtermRGB(n)
	scale := func(v int) string { return strconv.Itoa(int(float64(v)*k + 0.5)) }
	return "\x1b[48;2;" + scale(r) + ";" + scale(g) + ";" + scale(b) + "m"
}

var sgrSelected = bg(colCurrent) + "\x1b[38;5;16m\x1b[1m" // 現在地 (蛍光オレンジ地に黒字)

const sgrFgReset = "\x1b[39m\x1b[22m" // 前景と太字だけを戻す (帯の背景色を消さない)

// cardPalette はカード固有の地の色。明るい地は文字が溶けるので暗い地だけ (docs/theme-colors.md の「選択中テキスト」の実例)。
// 状態の色 (stateColor) や現在地色 (202) と紛れない暗さにしてある。
// cube の最も暗い段 (#5f) でなく 1 段明るい段 (#87) にした (#5f は待ちで暗くするとほぼ黒の「色なし」に見えた。
// 2026-09-27 にユーザーが見本で選んだ。数が同じでも色が変わるので、既存のカードの色は全部変わる — ユーザー了承済み)。
// 茶の 94 だけは据え置き (1 段上げると現在地 202 / 要対応 214 / 未 push 208 の橙に寄る)。
var cardPalette = []int{88, 18, 28, 90, 100, 30, 91, 94, 25, 125}

// cardColor はカード ID から固有の地の色を決める (同じカードは列を移っても・起動し直しても同じ色)。
func cardColor(id string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	return cardPalette[h.Sum32()%uint32(len(cardPalette))]
}

// stateColor は列 (状態) の色。意味は theme/colors.yml に揃える。
func stateColor(st card.State) int {
	switch st {
	case card.Requested:
		return colInfo // 情報: 来たばかり
	case card.Planned:
		return 250 // 待機 (中立)
	case card.Running:
		return colActive // 正常 / アクティブ
	case card.Waiting:
		return colAttention // 要対応 (人間の番)
	case card.Review:
		return colMarker // 未 push マーカー (まだ master に載っていない)
	case card.Done:
		return colCold // 消灯
	}
	return 250
}

// paint は s を表示幅 w に合わせ、背景色 b で行全体を塗る。s の中の全リセットの後も背景色を塗り直す。
func paint(b, s string, w int) string {
	s = fit(s, w)
	return b + strings.ReplaceAll(s, sgrReset, sgrReset+b) + sgrReset
}

// boxLine は罫線の枠の中の 1 行 (│ + 中身 + │)。
func boxLine(border, s string, w int) string {
	return border + "│" + sgrReset + fit(s, w-2) + border + "│" + sgrReset
}

func boxTop(border, title string, w int) string {
	inner := w - 2
	t := ""
	if title != "" {
		// 見出しは枠の内側に収める。はみ出すと、その行の右にある列がすべてずれる (狭い端末の「▶ 3 作業中 (2)」等)
		title = termwidth.Truncate(title, max(inner-3, 0), "…")
		t = "─ " + title + border + " "
	}
	rest := max(0, inner-termwidth.Of(t))
	return border + "╭" + t + strings.Repeat("─", rest) + "╮" + sgrReset
}

func boxBottom(border string, w int) string {
	return border + "╰" + strings.Repeat("─", max(0, w-2)) + "╯" + sgrReset
}
