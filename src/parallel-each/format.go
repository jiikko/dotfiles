package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// truncate は s を max **桁** (端末の表示幅) に収める。溢れる場合は "…" を付ける。
//
// ⚠️ rune 数ではなく表示幅で切る。呼び出し側 (tui.go) は m.width から引き算した残り桁を渡すので、
// rune 数で切ると全角・絵文字が 2 倍の桁を占めて枠を越える (実測: max=6 に "あいうえおかきくけこ" を
// 渡すと 11 桁の文字列が返っていた)。幅モデルを ansi.StringWidth (GraphemeWidth) に揃える理由と
// 層ごとの実測値は glogx 本体の src/glogx/width.go 冒頭が一次情報。
//
// この関数は「max 桁に収める」ことを約束するので、**幅が文脈依存になる要素はここで均す**。
// 素の ansi.Truncate だけでは 2 つ漏れる (どちらも実測で確認):
//
//   - タブは幅 0 と数えられるが、実端末では次のタブストップまで伸びる (最大 7 桁)。
//     子プロセスの出力 (go test / make) にタブは日常的に来るので、空白へ展開してから測る。
//     展開幅を 4 にするのは lipgloss が Render で同じ幅に展開するため (測る側と描く側を揃える)
//   - 切り口で SGR が閉じない。実測: "…[32m" のように中身のない色指定が末尾に残り、
//     以降の要素や次の行まで染める。装飾が残っていたら reset を足す (reset は幅 0)
func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	s = strings.ReplaceAll(s, "\t", tabAsSpaces)
	tail := "…"
	if max == 1 {
		// ⚠️ 1 桁しかないときは "…" で潰さず中身を優先する (元の実装からの意図的な挙動。
		// 唯一の桁を省略記号に使うと情報がゼロになる)。全角は 1 桁に入らないので結果は空になる。
		tail = ""
	}
	return closeSGR(ansi.Truncate(s, max, tail))
}

// tabAsSpaces はタブの展開幅 (lipgloss の Render が使う既定と揃える)。
const tabAsSpaces = "    "

// closeSGR は装飾が開いたままの文字列に reset を足す。⚠️ reset は幅 0 なので桁の約束を壊さない。
func closeSGR(s string) string {
	if !strings.Contains(s, "\x1b") || strings.HasSuffix(s, ansiReset) {
		return s
	}
	return s + ansiReset
}

const ansiReset = "\x1b[0m"

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// formatDur renders a duration with compact units (ms, s, m).
func formatDur(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	m := int(d / time.Minute)
	s := int((d % time.Minute) / time.Second)
	return fmt.Sprintf("%dm%02ds", m, s)
}

// visibleLen は s の端末表示幅 (桁数)。ANSI エスケープは幅 0。
//
// ⚠️ 自前で走査して 1 rune = 1 桁と数えてはいけない (以前はそうしていた)。全角は 2 桁、
// VS16 付き絵文字も 2 桁なので、rune 数だと lipgloss が実際に描く幅と食い違い列が崩れる。
// truncate と同じ幅モデル (ansi.StringWidth) に揃えるのが要点 — 測る側と切る側が別のモデルだと、
// 「収めたつもりが 1 桁溢れる」がどちらの責任か分からなくなる。
func visibleLen(s string) int { return ansi.StringWidth(s) }
