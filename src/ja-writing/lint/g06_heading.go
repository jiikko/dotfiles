package lint

import "strings"

// G6 見出し (heading_colon・short_summary_heading・redundant_bracket。本家 yomiyasu_lint.py の 5699-5724)。

// summaryHeading は本家の _SUMMARY_HEADING (「まとめ」「おわりに」だけの見出し)。
var summaryHeading = newLazy(`^#{1,6}\s*(まとめ|おわりに)\s*$`, 0)

// summaryHeadingMaxChars は短い文書の境目 (地の文の字数がこれ未満)。
const summaryHeadingMaxChars = 2000

var redundantBrackets = []string{"（素の出力）", "（いわゆる）", "（概要）", "（詳細）", "（感謝と設計への反映）"}

// g06Heading は見出しの行 (kind が heading) の最初。この後に骨組みが g08Vocabulary を呼ぶ。
func (s *lintState) g06Heading(r *rowCtx) {
	if strings.Contains(r.scanText, "とは：") || strings.Contains(r.scanText, "とは:") {
		s.add(newFinding("heading_colon", r.lineNo, "warn",
			"見出しが「Xとは：Y」の形になっています。コロンの後の語句を落とさずに、「Xの仕組みと使い方」のような名詞句にしてください。", r.stripped))
	}
	core, _ := stripEndDecoration(r.stripped, "")
	if (mustMatchAt0(summaryHeading, r.stripped) || mustMatchAt0(summaryHeading, core)) && s.metrics.charCount < summaryHeadingMaxChars {
		s.add(newFinding("short_summary_heading", r.lineNo, "info",
			"短い文書に「まとめ」「おわりに」の見出しがあります。本文の繰り返しになっていないか確かめてください。新しい情報や結論の絞り込み、数値を担う場合は残してください。", r.stripped))
	}
	for _, bracket := range redundantBrackets {
		if strings.Contains(r.scanText, bracket) {
			s.add(newFinding("redundant_bracket", r.lineNo, "warn",
				"見出しに補足カッコが含まれています。役割を確認し、同じ情報の繰り返しや不要な補足であれば整理してください。", r.stripped))
			break
		}
	}
}
