package lint

import (
	"strings"

	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
)

// G7 文末のコロン (trailing_colon。本家 yomiyasu_lint.py の 5737-5748)。

// g07State は G7 が文書の中で持つ状態。行ごとに決まるので、持つものは無い。
type g07State struct{}

// g07TrailingColon は見出しでも表でもない行。最後の字が「：」か「:」で、コロンだけの行 (「:::」の囲み) でなく、
// コードの伏せを除いた行が http で始まらず、最後の字が保護された範囲にないとき。
func (s *lintState) g07TrailingColon(r *rowCtx) {
	rs := pyunicode.Runes(r.stripped)
	if len(rs) == 0 || (rs[len(rs)-1] != '：' && rs[len(rs)-1] != ':') || (len(rs) >= 3 && strings.Trim(r.stripped, ":") == "") {
		return
	}
	visible, err := s.an.LineVisibleText(r.lineNo, []string{"inline_code"})
	if err != nil {
		panic(err)
	}
	last := pyunicode.RuneCount(pystr.RStrip(r.line)) - 1
	if strings.HasPrefix(pystr.Strip(visible), "http") || s.an.RangeOverlapsProtected(r.row.Start+last, r.row.Start+last+1, metricOpaqueKinds) {
		return
	}
	s.add(newFinding("trailing_colon", r.lineNo, "warn",
		"文末にコロン（：）があります。ラベルと値の対応などに必要か確認し、不要な前置きなら整理してください。", r.stripped))
}
