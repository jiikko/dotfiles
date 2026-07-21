package usage

import (
	"fmt"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"
)

// 自己完結のため ANSI は glogx とは独立に定義する (このパッケージ単独で色付き出力を
// 完結させ、切り出し時に glogx への依存を残さないため)。
const (
	cReset  = "\x1b[0m"
	cDim    = "\x1b[2m"
	cGreen  = "\x1b[32m"
	cYellow = "\x1b[33m"
	cRed    = "\x1b[31m"
)

// defaultOrder は RenderLine が描く枠と順序。5h セッションと weekly(all models) の 2 本。
// Fable 等の別週枠は Snapshot には入るが既定では描かない (spec 外)。
var defaultOrder = []string{"5h", "7d"}

// RenderLine は Snapshot を 1 行のステータス文字列へ整形する。純関数 (テスト容易)。
// 単独コマンドやコンパクト表示用。複数行モーダルには RenderRows を使う。
// 例: "5h:[░░░░]2%(残:4時間39分 / 7月22日03:09) 7d:[█░░░]28%(残:2日9時間 / 7月24日07:59)"
func RenderLine(s *Snapshot, now time.Time, colored bool) string {
	if s == nil {
		return ""
	}
	var parts []string
	for _, label := range defaultOrder {
		if w, ok := s.Find(label); ok {
			parts = append(parts, fmt.Sprintf("%s:%s%d%%(残:%s / %s)",
				w.Label, bar4(w.Percent, colored), w.Percent,
				formatRemain(w.ResetAt.Sub(now)), formatReset(w.ResetAt)))
		}
	}
	return strings.Join(parts, " ")
}

// 表レイアウトの列定義。ヘッダーとデータ行で共有し縦の列を揃える。
const (
	tblGap    = "   " // 列間の空白 (3)
	tblLabelW = 2     // 枠ラベル列 ("5h"/"7d"/"枠" いずれも表示幅 2)
	tblUsageW = 11    // 使用列 = バー "[░░░░]"(6) + 空白(1) + "%3d%%"(4)
)

// RenderTable は複数行モーダル用のヘッダー行とデータ行を、列を揃えて返す。
// データ行は残り時間とリセット時刻を " / " で対にし (どの残がどのリセットとペアか明示)、
// 残り時間を最大幅に揃えて "/" を縦に揃える。区切り罫線は箱幅を知る呼び出し側が引く。
// 例: header="枠   使用          残り / リセット"
//
//	row  ="5h   [░░░░]   4%   4時間26分 / 7月22日03:09"
func RenderTable(s *Snapshot, now time.Time, colored bool) (header string, rows []string) {
	if s == nil {
		return "", nil
	}
	var ws []Window
	for _, label := range defaultOrder {
		if w, ok := s.Find(label); ok {
			ws = append(ws, w)
		}
	}
	// 残り時間を (日 / 時間 / 分) の 3 列に分解し、列ごとに最大幅へ右寄せして単位位置を
	// 縦に揃える (例: "   4時間25分" と "2日 8時間" の "時間" が同じ桁に来る)。数字の桁数が
	// 揃わないと単位がずれるための整列で、ゼロ埋めではなく空白での右寄せにする (先頭の
	// 単位が 時間/日 で異なるためゼロ埋めでは揃わない)。
	days := make([]string, len(ws))
	hours := make([]string, len(ws))
	mins := make([]string, len(ws))
	var wDay, wHour, wMin int
	// リセット日時も同様に (月 / 日 / 時刻) の列へ分解して右寄せ整列する。月日の桁数が
	// 違っても時刻 (HH:MM) が縦に揃う (ユーザー要望 2026-07-21)。時刻はゼロ埋め固定幅。
	months := make([]string, len(ws))
	dates := make([]string, len(ws))
	clocks := make([]string, len(ws))
	var wMonth, wDate int
	for i, w := range ws {
		days[i], hours[i], mins[i] = remainCols(w.ResetAt.Sub(now))
		wDay = max(wDay, runewidth.StringWidth(days[i]))
		wHour = max(wHour, runewidth.StringWidth(hours[i]))
		wMin = max(wMin, runewidth.StringWidth(mins[i]))
		months[i], dates[i], clocks[i] = resetCols(w.ResetAt)
		wMonth = max(wMonth, runewidth.StringWidth(months[i]))
		wDate = max(wDate, runewidth.StringWidth(dates[i]))
	}
	header = padRight("枠", tblLabelW) + tblGap + padRight("使用", tblUsageW) + tblGap + "残り / リセット"
	rows = make([]string, len(ws))
	for i, w := range ws {
		// バーは色付き時 ANSI を含むが表示幅は常に 6 なので固定列 (tblUsageW) として扱える。
		usageCell := fmt.Sprintf("%s %3d%%", bar4(w.Percent, colored), w.Percent)
		remainCell := padLeft(days[i], wDay) + padLeft(hours[i], wHour) + padLeft(mins[i], wMin)
		resetCell := padLeft(months[i], wMonth) + padLeft(dates[i], wDate) + clocks[i]
		rows[i] = padRight(w.Label, tblLabelW) + tblGap + usageCell + tblGap +
			remainCell + " / " + resetCell
	}
	return header, rows
}

// padRight は表示幅を w に右詰めパディングする (ANSI を含まないセル専用)。
func padRight(s string, w int) string {
	pad := w - runewidth.StringWidth(s)
	if pad <= 0 {
		return s
	}
	return s + strings.Repeat(" ", pad)
}

// padLeft は表示幅を w に左詰めパディングする = 右寄せ (ANSI を含まないセル専用)。
func padLeft(s string, w int) string {
	pad := w - runewidth.StringWidth(s)
	if pad <= 0 {
		return s
	}
	return strings.Repeat(" ", pad) + s
}

// remainCols は残り時間を (日 / 時間 / 分) のスロット文字列に分解する。時間と分は常に出し
// (5h セッションと週制限で粒度を揃え、分の列が片方だけ空いて不揃いに見えるのを防ぐ)、日は
// 1日以上のときだけ出す。空スロットは "" (呼び出し側が列幅ぶんの空白で埋める)。
func remainCols(d time.Duration) (day, hour, minute string) {
	// 経過後 (d<=0) も「0時間0分」を返す (RenderLine の formatRemain は「リセット済み」)。
	// 意図的な非対称: table は 日/時間/分 の列に分けて縦揃えするため、経過後だけ 1 セルの
	// 「リセット済み」を返すと列分割が崩れる。ResetAt は fetch 時に固定されるので、モーダルを
	// 開いたままリセットを跨ぎ再 fetch されずに再描画したときだけ 0 表示になる (行全体が
	// stale になるレアケースで、隣にリセット時刻が並ぶため致命的誤読ではない)。
	if d < 0 {
		d = 0
	}
	if d >= 24*time.Hour {
		day = fmt.Sprintf("%d日", int(d/(24*time.Hour)))
	}
	return day,
		fmt.Sprintf("%d時間", int((d%(24*time.Hour))/time.Hour)),
		fmt.Sprintf("%d分", int((d%time.Hour)/time.Minute))
}

// resetCols はリセット時刻を (月 / 日 / 時刻) のスロットに分解する (列右寄せで整列用)。
// 時刻は HH:MM のゼロ埋め固定幅なので、月日を右寄せすれば時刻が縦に揃う。
func resetCols(t time.Time) (month, date, clock string) {
	return fmt.Sprintf("%d月", int(t.Month())), fmt.Sprintf("%d日", t.Day()), t.Format("15:04")
}

// bar4 は使用率を 4 セルのバーにする。filled = round(pct/100*4) を整数演算で。
// 色付き時は使用率が高いほど赤へ (90%+ 赤 / 75%+ 黄 / それ以外 緑)。
func bar4(pct int, colored bool) string {
	pct = max(0, min(pct, 100))
	filled := min((pct*4+50)/100, 4)
	// ▰/▱ は表示幅が常に 1 (曖昧幅でない) ため塗り数に依らずバー幅が一定になる。█ は
	// runewidth で幅 2 と判定され (端末は幅 1) 塗り数で列がずれる不具合があったため不可。
	full := strings.Repeat("▰", filled)
	empty := strings.Repeat("▱", 4-filled)
	if !colored {
		return "[" + full + empty + "]"
	}
	col := cGreen
	switch {
	case pct >= 90:
		col = cRed
	case pct >= 75:
		col = cYellow
	}
	return "[" + col + full + cDim + empty + cReset + "]"
}

// formatRemain は残り時間を "4時間39分" / "2日9時間" へ整形する。1 日未満は時間+分、
// 1 日以上は日+時間。過去 (リセット済み) は "リセット済み"。
func formatRemain(d time.Duration) string {
	if d <= 0 {
		return "リセット済み"
	}
	if d < 24*time.Hour {
		h := int(d / time.Hour)
		m := int((d % time.Hour) / time.Minute)
		return fmt.Sprintf("%d時間%d分", h, m)
	}
	days := int(d / (24 * time.Hour))
	h := int((d % (24 * time.Hour)) / time.Hour)
	return fmt.Sprintf("%d日%d時間", days, h)
}

// formatReset はリセット時刻を "7月22日03:09" へ整形する (Go の参照時刻 1=月 2=日 15=時 04=分)。
// 実測値をそのまま出す (勝手な分単位の丸めはしない)。
func formatReset(t time.Time) string {
	return t.Format("1月2日15:04")
}
