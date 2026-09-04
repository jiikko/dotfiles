package svc

// 表示・コピーに出す前の関門 (issue 228)。disk 側の display.go と対になる。
//
// 🚨 材料はすべて **plist ファイルの中身**と launchctl / brew の出力。plist は
// `~/Library/LaunchAgents` に誰でも置けるので、Label に端末制御シーケンスを入れた plist を
// 1 つ置くだけで注入できる。CLI (svcdoctor) は stdout へ直接書くので「表示しただけ」で発火し、
// TUI (glogx) では `Y` のコピーが pbcopy へ生で渡る。
//
// 🚨 **同一性を持つ値は書き換えず落とす** (disk の Item.Path と同じ判断)。Label / PlistPath /
// Domain は「手で叩いてください」と提示するコマンドが指す先そのもの。書き換えて表示すると、
// 攻撃者が無害化後の名前のファイル (`a<ESC>[2Jb.plist` と `ab.plist` の両方) を置くだけで、
// **doctor が「攻撃者が選んだファイルを rm しろ」と案内する**ことになる (敵対レビュー
// 2026-09-04 が実測)。ShellQuote が守るのは注入であって同一性ではない。
// 落としたことは DirErrs に件数で残す (svcExitCode が DirErrs を見て「診断できず」へ倒すので、
// CLI の終了コードにも出る = 黙って減らない)。
//
// 表示だけの自由文 (Reasons / MissingExec / エラー文) は落とさず無害化する。

import (
	"fmt"

	"termsafe"
)

// SanitizeForDisplay は Report を表示・コピーに出してよい形にする。
func SanitizeForDisplay(rep Report) Report {
	out := rep
	out.StatusErr = termsafe.PlainLine(rep.StatusErr)
	out.BrewErr = termsafe.PlainLine(rep.BrewErr)
	out.DirErrs = sanitizeDisplayLines(rep.DirErrs)
	dropped := 0
	out.Findings = make([]Finding, 0, len(rep.Findings))
	for _, f := range rep.Findings {
		if !displayableIdentity(f.Label) || !displayableIdentity(f.PlistPath) || !displayableIdentity(f.Domain) {
			dropped++
			continue
		}
		f.Reasons = sanitizeDisplayLines(f.Reasons)
		f.RestartKeys = sanitizeDisplayLines(f.RestartKeys)
		f.MissingExec = termsafe.PlainLine(f.MissingExec)
		f.BrewFormula = termsafe.PlainLine(f.BrewFormula)
		f.Commands = sanitizeDisplayLines(f.Commands)
		out.Findings = append(out.Findings, f)
	}
	out.Undiagnosed = make([]Undiagnosed, 0, len(rep.Undiagnosed))
	for _, u := range rep.Undiagnosed {
		if !displayableIdentity(u.PlistPath) {
			dropped++
			continue
		}
		u.Reason = termsafe.PlainLine(u.Reason)
		out.Undiagnosed = append(out.Undiagnosed, u)
	}
	if dropped > 0 {
		out.DirErrs = append(out.DirErrs,
			fmt.Sprintf("%d 件は名前に制御文字を含むため一覧から外しました (提示するコマンドが別のファイルを指すため)", dropped))
	}
	return out
}

// displayableIdentity は「そのまま画面とコマンド行に出してよい識別子か」。
// disk.DisplayablePath と同じ述語 (termsafe.IsPlain) を使う — 落とす基準を 2 実装しない。
//
// 🚨 **空文字は落とさない**。ここが守るのは「表示した名前と実体が一致すること」で、空である
// ことは安全性の問題ではない (実走査では Label / Domain / PlistPath は必ず埋まる)。空を
// 落とす形にすると、識別子を持たない Report を「診断できず」へ倒してしまう。
func displayableIdentity(s string) bool { return termsafe.IsPlain(s) }

// 🚨 名前は sanitize で始めること。無害化の網羅検査 (doctor/internal/displaycheck) は
// 右辺が `termsafe.*` / `sanitize*` / `Sanitize*` / `DisplayablePath` を呼んでいるかで
// 「関門を通った代入」を見分ける。別の名前にすると、無害化していても**通していない**と
// 判定される (逆に、無害化していないのにこの名前を付ければ素通りする = 名前ベースの近似)。
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
