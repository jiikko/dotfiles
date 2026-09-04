package docker

import (
	"fmt"

	"termsafe"
)

// 表示・コピーに出す前の関門 (disk / svc の display.go と対。issue 228 の規律)。
//
// 材料はすべて docker が持っている外部由来の文字列 — イメージ名は任意のレジストリから来るし、
// コンテナ名・ボリューム名・ビルドキャッシュの Description は誰かが書いた文字列。
//
// 🚨 **Name は書き換えず落とす** (disk.Item.Path / svc.Finding.Label と同じ判断)。
// Name は提示するコマンド (`docker volume rm <name>`) が指す先そのものなので、無害化して
// 表示すると「攻撃者が選んだ別の資源を消せ」と案内することになる。落とした件数は Dropped に残す。
// Detail / Notes / Unavailable は表示だけの自由文なので、落とさず無害化する。
func SanitizeForDisplay(rep Report) Report {
	out := rep
	out.Unavailable = termsafe.PlainLine(rep.Unavailable)
	out.SystemPrune = termsafe.PlainLine(rep.SystemPrune)
	out.SystemPruneNote = termsafe.PlainLine(rep.SystemPruneNote)
	out.Notes = sanitizeDisplayLines(rep.Notes)
	out.Groups = make([]Group, 0, len(rep.Groups))
	for _, g := range rep.Groups {
		g.Label = termsafe.PlainLine(g.Label)
		g.Command = termsafe.PlainLine(g.Command)
		g.Notes = sanitizeDisplayLines(g.Notes)
		items := make([]Item, 0, len(g.Items))
		for _, it := range g.Items {
			if !termsafe.IsPlain(it.Name) || !termsafe.IsPlain(it.Command) {
				out.Dropped++
				g.Size -= it.Size
				continue
			}
			it.Detail = termsafe.PlainLine(it.Detail)
			it.SizeText = termsafe.PlainLine(it.SizeText)
			items = append(items, it)
		}
		g.Items = items
		out.Groups = append(out.Groups, g)
	}
	if out.Dropped > 0 {
		// 🚨 **Unavailable に足さない。** あちらは「診断できなかった」の一意な印で、消費側は
		// 「非空ならエラーを出して return」と書く。1 件落としただけで全群が画面から消える
		// (敵対レビュー 2026-09-04)
		out.Notes = append(out.Notes,
			fmt.Sprintf("%d 件は名前が識別子として読めないため一覧から外しました (提示するコマンドが別の資源を指すため)", out.Dropped))
	}
	return out
}

// 🚨 名前は sanitize で始めること。無害化の網羅検査 (doctor/internal/displaycheck) は
// 右辺が `termsafe.*` / `sanitize*` / `Sanitize*` を呼んでいるかで「関門を通った代入」を
// 見分けるので、別の名前だと無害化していても「通していない」と判定される。
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
