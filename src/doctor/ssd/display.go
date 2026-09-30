package ssd

import "github.com/jiikko/dotfiles/src/termsafe"

// SanitizeForDisplay は表示・コピーに出す前の関門 (disk / svc / docker の display.go と対。issue 228 の規律)。
//
// 材料の Model と SMART Status の値は diskutil の出力、Note はコマンドのエラー文 = 外部由来の文字列。
// どれも同一性に使わない (提示コマンドは定数の APFSCommand / InstallHint だけ) ので、落とさず無害化する。
func SanitizeForDisplay(rep Report) Report {
	out := rep
	out.Model = termsafe.PlainLine(rep.Model)
	out.Notes = sanitizeDisplayLines(rep.Notes)
	out.Checks = make([]Check, 0, len(rep.Checks))
	for _, c := range rep.Checks {
		c.Label = termsafe.PlainLine(c.Label)
		c.Value = termsafe.PlainLine(c.Value)
		c.Note = termsafe.PlainLine(c.Note)
		out.Checks = append(out.Checks, c)
	}
	return out
}

// 🚨 名前は sanitize で始めること (doctor/internal/displaycheck が右辺の呼び出し名で関門を見分ける)。
func sanitizeDisplayLines(ss []string) []string {
	if len(ss) == 0 {
		return nil
	}
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		out = append(out, termsafe.PlainLine(s))
	}
	return out
}
