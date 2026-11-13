package diff

import (
	"fmt"
	"slices"
	"strings"

	"ja-writing/internal/bold"
	"ja-writing/internal/pyunicode"
)

// endingsReport は本家の _endings_report: --endings の出力 (1 つのファイルの文末だけを見る。マークダウンのコードブロックや表の区切りは飛ばす)。
func endingsReport(text, stance string) (string, error) {
	rows, flags := StanceFlags(text, stance, true)
	// Counter(...).most_common(): 数の多い順で、同じ数は初めて出た順
	var kinds []string
	counts := map[string]int{}
	for _, r := range rows {
		if r.Where == "表" {
			continue
		}
		if counts[r.Kind] == 0 {
			kinds = append(kinds, r.Kind)
		}
		counts[r.Kind]++
	}
	slices.SortStableFunc(kinds, func(a, b string) int { return counts[b] - counts[a] })
	parts := make([]string, len(kinds))
	for i, k := range kinds {
		parts[i] = fmt.Sprintf("%s %d", k, counts[k])
	}
	lines := []string{"■ 文末の種類（表を除く）: " + strings.Join(parts, "、")}
	for _, f := range flags {
		lines = append(lines, "■ "+f.Note)
		for _, r := range f.Rows {
			lines = append(lines, "  ・"+shorten(r.Sentence, 60))
		}
	}
	problems, err := bold.Problems(text, true)
	if err != nil {
		return "", err
	}
	if len(problems) > 0 {
		lines = append(lines, boldHead(""))
		items := make([]string, len(problems))
		for i, p := range problems {
			suggest := p.Suggest
			if suggest == "" {
				suggest = "（手で直す）"
			}
			items[i] = fmt.Sprintf("- %d行目: %s → %s（%s）", p.Line, p.Found, suggest, p.How)
		}
		lines = append(lines, strings.Join(items, "\n"))
	}
	return strings.Join(lines, "\n"), nil
}

// boldHead は本家の bold_head。where は見出しの「（」の直後に足す文 (--endings は空)。
func boldHead(where string) string {
	return "■ 太字にならない書き方（" + where + "GitHub などで ** がそのまま表示されることがある。表示可否と案の範囲を確認して直す）"
}

// shorten は len(s) <= n ならそのまま、超えれば先頭 n 字に「…」を足す (字は code point の数)。
func shorten(s string, n int) string {
	rs := pyunicode.Runes(s)
	if len(rs) <= n {
		return s
	}
	return pyunicode.FromRunes(rs[:n]) + "…"
}
