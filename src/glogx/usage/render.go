package usage

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// dispWidth は文字列の端末表示幅 (ANSI は幅 0)。glogx 本体の width.go と同じ ansi.StringWidth を
// 使う: runewidth は East Asian ambiguous (罫線・ブロック) を locale 依存で 2 桁と数えるため、
// このパッケージの出力を glogx 側が dispWidth で測ると環境によって枠がずれる (issue 027)。
func dispWidth(s string) int { return ansi.StringWidth(s) }

// 自己完結のため ANSI は glogx とは独立に定義する (このパッケージ単独で色付き出力を
// 完結させ、切り出し時に glogx への依存を残さないため)。
const (
	cReset  = "\x1b[0m"
	cDim    = "\x1b[2m"
	cGreen  = "\x1b[32m"
	cYellow = "\x1b[33m"
	cRed    = "\x1b[31m"
)

// defaultOrder は Claude の枠のうち描くものと順序。5h セッションと weekly(all models) の
// 2 本。Fable 等の別週枠は Snapshot には入るが既定では描かない (spec 外)。
var defaultOrder = []string{"5h", "7d"}

// renderWindows は RenderLine / RenderTable が描く枠を返す。Claude は defaultOrder による
// 選抜、codex は取得できた枠すべて (枠構成がプラン依存で事前に列挙できないため、ラベルで
// なく Source で拾う)。順序は Claude → codex 固定。
func renderWindows(s *Snapshot) []Window {
	var ws []Window
	for _, label := range defaultOrder {
		if w, ok := s.Find(label); ok {
			ws = append(ws, w)
		}
	}
	for _, w := range s.Windows {
		if w.Source == SourceCodex {
			ws = append(ws, w)
		}
	}
	return ws
}

// RenderLine は Snapshot を 1 行のステータス文字列へ整形する。純関数 (テスト容易)。
// 単独コマンドやコンパクト表示用。複数行モーダルには RenderRows を使う。
// 例: "5h:[▱▱▱▱▱▱▱▱▱▱]2%(残:4時間39分 / 7月22日03:09) 7d:[▰▰▰▱▱▱▱▱▱▱]28%(残:2日9時間 / 7月24日07:59)"
func RenderLine(s *Snapshot, now time.Time, colored bool) string {
	if s == nil {
		return ""
	}
	ws := renderWindows(s)
	parts := make([]string, 0, len(ws))
	for _, w := range ws {
		parts = append(parts, fmt.Sprintf("%s:%s%d%%(残:%s / %s)",
			w.Label, bar(w.Percent, colored), w.Percent,
			formatRemain(w.ResetAt.Sub(now)), formatReset(w.ResetAt)))
	}
	return strings.Join(parts, " ")
}

// 表レイアウトの列定義。ヘッダーとデータ行で共有し縦の列を揃える。
const (
	tblGap    = "   " // 列間の空白 (3)
	tblLabelW = 2     // 枠ラベル列の最小幅 ("5h"/"7d"/"枠" は 2。"cx7d" 等が混ざると実幅で広がる)
	tblUsageW = 17    // 使用列 = バー "[" + barCells(10) + "]"(=12) + 空白(1) + "%3d%%"(4)
)

// RenderTable は複数行モーダル用のヘッダー行とデータ行を、列を揃えて返す。
// ⚠️ 現在 production の呼び出し元は無い (glogx 本体は罫線を挟むため RenderTableGroups を
// 直接使う)。テストの検証入口と、将来の単独コマンド切り出し用の flat な公開面として残している。
// 単独コマンド案を捨てるならテストを RenderTableGroups へ寄せてから削除してよい。
// データ行は残り時間とリセット時刻を " / " で対にし (どの残がどのリセットとペアか明示)、
// 残り時間を最大幅に揃えて "/" を縦に揃える。区切り罫線は箱幅を知る呼び出し側が引く。
// 例: header="枠   使用          残り / リセット"
//
//	row  ="5h   [▰▱▱▱▱▱▱▱▱▱]   4%   4時間26分 / 7月22日03:09"
func RenderTable(s *Snapshot, now time.Time, colored bool) (header string, rows []string) {
	header, groups := RenderTableGroups(s, now, colored)
	for _, g := range groups {
		rows = append(rows, g...)
	}
	return header, rows
}

// RenderTableGroups は RenderTable と同じデータ行を、出所 (Claude / codex) が変わる境目で
// グループに分けて返す (ユーザー要望 2026-07-31: Claude と codex の間に区切り罫線)。列幅は
// 全グループ横断で揃えるため、間に区切り行を挟んでも列は縦に揃う。区切り罫線そのものは
// 箱幅を知る呼び出し側が引く。空グループは返さない。
func RenderTableGroups(s *Snapshot, now time.Time, colored bool) (header string, groups [][]string) {
	if s == nil {
		return "", nil
	}
	ws := renderWindows(s)
	// 枠ラベル列は最長ラベルに合わせて広げる (codex の "cx7d" 等は最小幅 2 に収まらない)。
	labelW := tblLabelW
	for _, w := range ws {
		labelW = max(labelW, dispWidth(w.Label))
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
		wDay = max(wDay, dispWidth(days[i]))
		wHour = max(wHour, dispWidth(hours[i]))
		wMin = max(wMin, dispWidth(mins[i]))
		months[i], dates[i], clocks[i] = resetCols(w.ResetAt)
		wMonth = max(wMonth, dispWidth(months[i]))
		wDate = max(wDate, dispWidth(dates[i]))
	}
	// ヘッダーの「残り」列はデータの残り列と同じ幅で右寄せし、" / " 区切りをデータ行と
	// 縦に揃える (固定文字列だと列幅ぶんズレる)。リセット見出しは " / " の直後 (左詰め)。
	remainHdr := padLeft("残り", wDay+wHour+wMin)
	header = padRight("枠", labelW) + tblGap + padRight("使用", tblUsageW) + tblGap +
		remainHdr + " / " + "リセット"
	cur := make([]string, 0, len(ws))
	for i, w := range ws {
		// 出所が変わる境目 (renderWindows の順序で Claude → codex) でグループを切る。
		if i > 0 && w.Source != ws[i-1].Source {
			groups = append(groups, cur)
			cur = nil
		}
		// バーは色付き時 ANSI を含むが表示幅は常に barCells+2 なので固定列 (tblUsageW) として扱える。
		usageCell := fmt.Sprintf("%s %3d%%", bar(w.Percent, colored), w.Percent)
		remainCell := padLeft(days[i], wDay) + padLeft(hours[i], wHour) + padLeft(mins[i], wMin)
		resetCell := padLeft(months[i], wMonth) + padLeft(dates[i], wDate) + clocks[i]
		cur = append(cur, padRight(w.Label, labelW)+tblGap+usageCell+tblGap+
			remainCell+" / "+resetCell)
	}
	if len(cur) > 0 {
		groups = append(groups, cur)
	}
	return header, groups
}

// padRight は表示幅を w に右詰めパディングする (ANSI を含まないセル専用)。
func padRight(s string, w int) string {
	pad := w - dispWidth(s)
	if pad <= 0 {
		return s
	}
	return s + strings.Repeat(" ", pad)
}

// padLeft は表示幅を w に左詰めパディングする = 右寄せ (ANSI を含まないセル専用)。
func padLeft(s string, w int) string {
	pad := w - dispWidth(s)
	if pad <= 0 {
		return s
	}
	return strings.Repeat(" ", pad) + s
}

// remainCols は残り時間を (日 / 時間 / 分) のスロット文字列に分解する。時間と分は常に出し
// (5h セッションと週制限で粒度を揃え、分の列が片方だけ空いて不揃いに見えるのを防ぐ)、日は
// 1日以上のときだけ出す。空スロットは "" (呼び出し側が列幅ぶんの空白で埋める)。
// breakdown は Duration を 日/時間/分 に分解する (秒以下は切り捨て)。remainCols と
// formatRemain が同じ単位変換を共有し、境界の丸め方を 1 箇所で持つ。
func breakdown(d time.Duration) (days, hours, minutes int) {
	if d < 0 {
		d = 0
	}
	days = int(d / (24 * time.Hour))
	hours = int((d % (24 * time.Hour)) / time.Hour)
	minutes = int((d % time.Hour) / time.Minute)
	return
}

func remainCols(d time.Duration) (day, hour, minute string) {
	// 経過後 (d<=0) も「0時間0分」を返す (RenderLine の formatRemain は「リセット済み」)。
	// 意図的な非対称: table は 日/時間/分 の列に分けて縦揃えするため、経過後だけ 1 セルの
	// 「リセット済み」を返すと列分割が崩れる。ResetAt は fetch 時に固定されるので、モーダルを
	// 開いたままリセットを跨ぎ再 fetch されずに再描画したときだけ 0 表示になる (行全体が
	// stale になるレアケースで、隣にリセット時刻が並ぶため致命的誤読ではない)。
	days, hours, minutes := breakdown(d)
	if days >= 1 {
		day = fmt.Sprintf("%d日", days)
	}
	return day, fmt.Sprintf("%d時間", hours), fmt.Sprintf("%d分", minutes)
}

// resetCols はリセット時刻を (月 / 日 / 時刻) のスロットに分解する (列右寄せで整列用)。
// 時刻は HH:MM のゼロ埋め固定幅なので、月日を右寄せすれば時刻が縦に揃う。
func resetCols(t time.Time) (month, date, clock string) {
	return fmt.Sprintf("%d月", int(t.Month())), fmt.Sprintf("%d日", t.Day()), t.Format("15:04")
}

// barCells はバーのセル数 (使用率の分解能)。列幅定数 tblUsageW と連動するので変更時は両方直す。
const barCells = 10

// bar は使用率を barCells セルのバーにする。filled = round(pct/100*barCells) を整数演算で。
// 色付き時は使用率が高いほど赤へ (90%+ 赤 / 75%+ 黄 / それ以外 緑)。
func bar(pct int, colored bool) string {
	pct = max(0, min(pct, 100))
	filled := min((pct*barCells+50)/100, barCells)
	// ▰/▱ は表示幅が常に 1 (曖昧幅でない) ため塗り数に依らずバー幅が一定になる。
	// (幅計算を ansi.StringWidth に統一した現在は █ でも桁は合うが、見た目は現状維持)
	full := strings.Repeat("▰", filled)
	empty := strings.Repeat("▱", barCells-filled)
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
	days, hours, minutes := breakdown(d)
	if days < 1 {
		return fmt.Sprintf("%d時間%d分", hours, minutes)
	}
	return fmt.Sprintf("%d日%d時間", days, hours)
}

// formatReset はリセット時刻を "7月22日03:09" へ整形する (Go の参照時刻 1=月 2=日 15=時 04=分)。
// 実測値をそのまま出す (勝手な分単位の丸めはしない)。
func formatReset(t time.Time) string {
	return t.Format("1月2日15:04")
}
