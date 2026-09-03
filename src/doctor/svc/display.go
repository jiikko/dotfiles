package svc

// 表示・コピーに出す前の関門 (issue 228)。disk 側の display.go と対になる。
//
// 🚨 材料はすべて **plist ファイルの中身**と launchctl / brew の出力。plist は
// `~/Library/LaunchAgents` に誰でも置けるので、Label に端末制御シーケンスを入れた plist を
// 1 つ置くだけで注入できる。CLI (svcdoctor) は stdout へ直接書くので「表示しただけ」で発火し、
// TUI (glogx) では `Y` のコピーが pbcopy へ生で渡る。
//
// 🚨 復元経路 (SanitizeRestored) との違い: あちらは**保存ファイルを信用しない**ので形が
// 崩れた Finding は丸ごと落とす (コマンドを組み立てる材料になるため)。こちらは実物の走査結果
// なので**落とさずに無害化するだけ**にする — 名前が変な plist ほど壊れた登録である可能性が
// 高く、診断から消すのは目的と逆。提示コマンドは実行しないし、ShellQuote も通っている。

import "termsafe"

// SanitizeForDisplay は Report を表示・コピーに出してよい形にする。
func SanitizeForDisplay(rep Report) Report {
	out := rep
	out.StatusErr = termsafe.PlainLine(rep.StatusErr)
	out.BrewErr = termsafe.PlainLine(rep.BrewErr)
	out.DirErrs = cleanDisplayLines(rep.DirErrs)
	out.Findings = make([]Finding, 0, len(rep.Findings))
	for _, f := range rep.Findings {
		f.Label = termsafe.PlainLine(f.Label)
		f.PlistPath = termsafe.PlainLine(f.PlistPath)
		f.Domain = termsafe.PlainLine(f.Domain)
		f.Reasons = cleanDisplayLines(f.Reasons)
		f.RestartKeys = cleanDisplayLines(f.RestartKeys)
		f.MissingExec = termsafe.PlainLine(f.MissingExec)
		f.BrewFormula = termsafe.PlainLine(f.BrewFormula)
		f.Commands = cleanDisplayLines(f.Commands)
		out.Findings = append(out.Findings, f)
	}
	out.Undiagnosed = make([]Undiagnosed, 0, len(rep.Undiagnosed))
	for _, u := range rep.Undiagnosed {
		u.PlistPath = termsafe.PlainLine(u.PlistPath)
		u.Reason = termsafe.PlainLine(u.Reason)
		out.Undiagnosed = append(out.Undiagnosed, u)
	}
	return out
}

func cleanDisplayLines(ss []string) []string {
	if len(ss) == 0 {
		return nil
	}
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		out = append(out, termsafe.PlainLine(s))
	}
	return out
}
