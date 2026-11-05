package health

import (
	"fmt"
	"strings"

	"github.com/jiikko/dotfiles/src/termsafe"
)

// SanitizeForDisplay は表示・保存の前の関門 (disk / svc / docker / ssd の display.go と対)。
// 値は外部コマンドの出力・プロセスのパス = 外部由来の文字列なので、制御文字を落とす。ID は定数だけなので触らない。
func SanitizeForDisplay(rep Report) Report {
	out := rep
	out.Checks = make([]Check, len(rep.Checks))
	for i, c := range rep.Checks {
		c.Label = termsafe.PlainLine(c.Label)
		c.Value = termsafe.PlainLine(c.Value)
		c.Note = termsafe.PlainLine(c.Note)
		c.Fix = termsafe.PlainLine(c.Fix)
		out.Checks[i] = c
	}
	return out
}

var statusWord = map[Status]string{
	StatusOK: "ok", StatusWarn: "注意", StatusBad: "異常", StatusUnknown: "判定不能", StatusInfo: "情報",
}

var categoryTitle = map[Category]string{
	CatHW: "ハードウェアと寿命", CatProc: "プロセス・メモリ", CatNet: "ネットワーク", CatDisk: "ディスク",
	CatTmux: "tmux", CatNvim: "nvim", CatChrome: "Chrome", CatApps: "その他のアプリ",
}

// Counts は判定ごとの件数。
func (r Report) Counts() map[Status]int {
	m := map[Status]int{}
	for _, c := range r.Checks {
		m[c.Status]++
	}
	return m
}

// Format は Markdown のレポートにする (表示の前に SanitizeForDisplay を通しておく)。
func Format(rep Report) string {
	var b strings.Builder
	n := rep.Counts()
	fmt.Fprintf(&b, "# mac-healthcheck のレポート\n\n%s に診断。異常 %d / 注意 %d / 判定不能 %d / ok %d / 情報 %d\n",
		rep.ScannedAt.Format("2006-01-02 15:04"), n[StatusBad], n[StatusWarn], n[StatusUnknown], n[StatusOK], n[StatusInfo])
	for _, cat := range AllCategories {
		var rows []Check
		for _, c := range rep.Checks {
			if c.Category == cat {
				rows = append(rows, c)
			}
		}
		if len(rows) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n## %s\n\n| 判定 | 項目 | 値 | メモ | 直し方 |\n|---|---|---|---|---|\n", categoryTitle[cat])
		for _, c := range rows {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", statusWord[c.Status], cell(c.Label), cell(c.Value), cell(c.Note), cell(c.Fix))
		}
	}
	return b.String()
}

// cell は Markdown の表のセルを壊す | を逃がす。
func cell(s string) string { return strings.ReplaceAll(s, "|", `\|`) }
