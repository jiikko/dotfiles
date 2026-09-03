package svc

import (
	"fmt"
	"strings"
)

// Annotations は Finding に添える注記。CLI (Format) と glogx の doctor 画面が
// 同じ文言を使うための単一の出典 — 片方にだけ注記を足すと、CLI を叩かないと見えない
// 判定 (AppleLikeOut など) が生まれる (issues/179)。
// 行頭の記号・インデントは呼び出し側が付ける。
func Annotations(f Finding) []string {
	var out []string
	if f.PenaltyBox {
		out = append(out, "launchd の penalty box 入り (失敗の繰り返しで起動間隔が延ばされています)")
	}
	if f.AppleLikeOut {
		out = append(out, "🚨 com.apple. を名乗っていますが Apple の管理領域 (/System/Library) の外にあります")
	}
	if f.BrewOrphan {
		out = append(out, "アンインストール済みの formula の登録が残っているようです (brew --prefix の var 配下の残骸も確認してください)")
	}
	if f.Domain == "system" && !f.HasLastExit {
		out = append(out, "起動状態は不明 (system ドメインは一般ユーザーの launchctl list に出ない)")
	}
	return out
}

// Format は人が読むテキスト。各行に「なぜ出ているか」を必ず添える。
func Format(rep Report) string {
	// 🚨 **stdout へ直接書く経路なので、ここが最後の関門**。材料は plist の中身と launchctl /
	// brew の出力で、そのまま出すと OSC52 やタイトル書き換えが「表示しただけ」で発火する
	// (issue 228)。TUI (glogx) の受け口も同じ関数を通す。
	rep = SanitizeForDisplay(rep)
	var b strings.Builder
	fmt.Fprintf(&b, "サービス診断: %d 件を走査\n", rep.Scanned)
	if rep.Interrupted {
		b.WriteString("🚨  途中で中断されました (penalty box 等の補助情報が欠けています)\n")
	}
	if rep.StatusErr != "" {
		fmt.Fprintf(&b, "🚨  診断できず (launchctl): %s\n    起動状態 (B: 失敗し続けている) は判定していません。実行ファイルの不在 (A) と Homebrew 台帳 (C) だけを出しています\n", rep.StatusErr)
	}
	if rep.BrewErr != "" {
		fmt.Fprintf(&b, "🚨  診断できず (brew): %s\n    Homebrew 台帳との突合 (C) は判定していません\n", rep.BrewErr)
	}
	for _, e := range rep.DirErrs {
		fmt.Fprintf(&b, "🚨  走査できず: %s\n", e)
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
		for _, a := range Annotations(f) {
			fmt.Fprintf(&b, "   - %s\n", a)
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
