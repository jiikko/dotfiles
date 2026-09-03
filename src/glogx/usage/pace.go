package usage

// ペースゲージと助言。窓を等分したスロットを「番号 + 空白」の 2 カラムで並べ、その背景で
// 消化量を描く。読み方は _claude/statusline-command.sh の pace_row と同じ:
//
//	背景緑 = 想定内の消化 / 背景赤 = 前借り / 青 = 使えるのに使っていない過去 /
//	暗灰 = まだ来ていない未来 / 下線 = いま居るスロット
//
// 「残り 1 日で 50% 余っている」= 余らせ過ぎ、「残り 5 日で 80% 使った」= 超過を、
// 残量% だけでは分からない「窓のどこにいるか」と併せて読むための表示。
//
// 🚨 カラム単位に塗るので 0.5 スロット (半日 / 半時間) の端数まで見える。前景色では
// 「番号」と「空白」で塗り分けができない (色の付いた空白は見えない) ため背景色を使う。

import (
	"fmt"
	"strings"
	"time"

	"glogx/sgr"
)

// paceGaugeMaxCells は番号付きゲージを出す最大スロット数。番号は半角 1 桁前提なので
// 2 桁になる窓 (10 等分以上) では出さず、呼び出し側が素のバーへ落ちる。
const paceGaugeMaxCells = 9

// paceGauge は cells 個のスロットを番号付きで並べたゲージを返す。
//
//	usedPct  … 消費した割合 (塗る量)
//	elapsed  … 経過した割合 (想定線の位置)
//	atCell   … いま居るスロット (0 起点。負なら下線を引かない)
//
// 🚨 塗りも想定線も「少しでも掛かったカラムは掛かっている扱い」(切り上げ) にする。
// 四捨五入にすると窓の終端がカラムの手前に落ちたときに今いるカラムが塗られない。
func paceGauge(cells int, usedPct, elapsed float64, atCell int, colored bool) string {
	if cells <= 0 || cells > paceGaugeMaxCells {
		return ""
	}
	cols := cells * 2
	fill := ceilCols(usedPct, cols)
	mark := ceilCols(elapsed, cols)
	var b strings.Builder
	b.WriteByte('[')
	for c := range cols {
		col := sgr.BrightBlack // まだ来ていない未来
		switch {
		case c < fill && c < mark:
			col = sgr.BgGreenOnBlack // 想定内の消化
		case c < fill:
			col = sgr.BgRedOnBlack // 前借り
		case c < mark:
			col = sgr.BrightBlue // 使えるのに使っていない過去
		}
		cell := " " // 奇数カラムは空白 (塗りの半スロット分解能をここが担う)
		if c%2 == 0 {
			digit := string(rune('1' + c/2))
			if colored && c == atCell*2 {
				digit = sgr.UnderlineBold + digit // 下線は番号だけに掛ける
			}
			cell = digit
		}
		if c == 0 {
			cell = " " + cell // 左端の余白も 1 カラム目と同じ塗りにする (端に穴を空けない)
		}
		if colored {
			b.WriteString(col + cell + sgr.Reset)
		} else {
			b.WriteString(cell)
		}
	}
	b.WriteString("]") // 右端の余白は最終スロットの空白カラムが担う (左右の余白が揃う)
	return b.String()
}

// ceilCols は割合 (0-100) を cols カラムへ切り上げで写す。
func ceilCols(pct float64, cols int) int {
	if pct <= 0 {
		return 0
	}
	n := int((pct*float64(cols) + 99) / 100)
	return min(n, cols)
}

// paceAdvice は状態語で言えないことだけを返す ("1.2時間分の前借り" / "0.8日分の余り")。
// 帯の中 (適正) は空 — 語が既に言っているので足さない。上限だけは行動が決まるので明示する。
func paceAdvice(used int, elapsed float64, span time.Duration, cells int) string {
	if used >= 100 {
		return "リセット待ち"
	}
	band := paceBand(span)
	d := float64(used) - elapsed
	if d <= band && d >= -band {
		return ""
	}
	// 乖離 pt を「何スロット分か」に換算する (1 スロット = 100/cells pt)。
	amt := absFloat(d) * float64(cells) / 100
	unit := paceAdviceUnit(span)
	if d > 0 {
		return fmt.Sprintf("%.1f%s分の前借り", amt, unit)
	}
	return fmt.Sprintf("%.1f%s分の余り", amt, unit)
}

// paceBudget は 1 スロットあたりに使える残枠 ("7.6%/時" / "12.3%/日")。残りが 1 スロット未満の
// ときは出さない: 「残 12 時間で 110.0%/日」はその 1 日が来ないので実行不能な数字になる。
// その場合は残枠そのものを返す。
func paceBudget(used int, remain, span time.Duration, cells int) string {
	if cells <= 0 || span <= 0 {
		return ""
	}
	left := max(100-used, 0)
	cell := span / time.Duration(cells)
	if remain < cell {
		return fmt.Sprintf("残枠%d%%", left)
	}
	return fmt.Sprintf("%.1f%%/%s", float64(left)*float64(cell)/float64(remain), paceCellUnit(span))
}

// paceAdviceUnit は助言に使う単位語 ("時間" / "日")。予算 (paceCellUnit) と語が違うのは
// statusline に揃えたため ("1.2時間分の前借り" と "7.6%/時" が自然に読めるように)。
func paceAdviceUnit(span time.Duration) string {
	if span >= 24*time.Hour {
		return "日"
	}
	return "時間"
}

// paceCellUnit は 1 スロットあたりの予算に使う単位語 ("時" / "日")。
func paceCellUnit(span time.Duration) string {
	if span >= 24*time.Hour {
		return "日"
	}
	return "時"
}

func absFloat(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
