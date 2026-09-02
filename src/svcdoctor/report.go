package main

import (
	"fmt"
	"strings"
)

// Format は人が読むテキスト。各行に「なぜ出ているか」を必ず添える。
func Format(rep Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "サービス診断: %d 件を走査\n", rep.Scanned)
	if rep.StatusErr != "" {
		fmt.Fprintf(&b, "⚠️  診断できず (launchctl): %s\n    起動状態 (B: 失敗し続けている) は判定していません。実行ファイルの不在 (A) と Homebrew 台帳 (C) だけを出しています\n", rep.StatusErr)
	}
	if rep.BrewErr != "" {
		fmt.Fprintf(&b, "⚠️  診断できず (brew): %s\n    Homebrew 台帳との突合 (C) は判定していません\n", rep.BrewErr)
	}
	for _, e := range rep.DirErrs {
		fmt.Fprintf(&b, "⚠️  走査できず: %s\n", e)
	}
	if len(rep.Findings) == 0 && len(rep.Undiagnosed) == 0 {
		b.WriteString("壊れた登録は見つかりませんでした\n")
		return b.String()
	}
	for _, f := range rep.Findings {
		fmt.Fprintf(&b, "\n⛔ %s\n", f.Label)
		fmt.Fprintf(&b, "   %s\n", f.PlistPath)
		for _, r := range f.Reasons {
			fmt.Fprintf(&b, "   - %s\n", r)
		}
		if f.PenaltyBox {
			b.WriteString("   - launchd の penalty box 入り (失敗の繰り返しで起動間隔が延ばされています)\n")
		}
		if f.AppleLikeOut {
			b.WriteString("   - ⚠️ com.apple. を名乗っていますが Apple の管理領域 (/System/Library) の外にあります\n")
		}
		if f.BrewOrphan {
			b.WriteString("   アンインストール済みの formula の登録が残っているようです (/opt/homebrew/var 配下の残骸も確認してください)\n")
		}
		b.WriteString("   手動で実行してください (このツールは実行しません):\n")
		for _, c := range f.Commands {
			fmt.Fprintf(&b, "     %s\n", c)
		}
	}
	for _, u := range rep.Undiagnosed {
		fmt.Fprintf(&b, "\n❔ 診断できず: %s\n   %s\n", u.PlistPath, u.Reason)
	}
	return b.String()
}
