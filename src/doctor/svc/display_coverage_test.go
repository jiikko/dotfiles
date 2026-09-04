package svc

import (
	"testing"

	"doctor/internal/displaycheck"
)

// 表示用の構造体と、それを無害化する関門 (issue 252)。検査の本体・脅威モデル・
// 「検出しない形」は `doctor/internal/displaycheck` が正本。
//
// 🚨 **この表に載っていない型は検査されない**。新しい表示用の構造体を足したら、ここにも足すこと。
var sanitizeGate = map[string]displaycheck.Gate{
	"Report":      {Func: "SanitizeForDisplay", Recv: "out"},
	"Finding":     {Func: "SanitizeForDisplay", Recv: "f"},
	"Undiagnosed": {Func: "SanitizeForDisplay", Recv: "u"},
}

// 無害化しないと決めた文字列フィールド。**理由を必ず書く** (書けないなら無害化する側が正しい)。
var sanitizeExempt = map[string]string{
	// 🚨 同一性を持つ値は書き換えず**落とす** (display.go の 🚨 が出典)。提示する
	// `launchctl bootout` / `rm` が指す先そのものなので、無害化して出すと攻撃者が
	// 無害化後の名前の plist を置くだけで「別のファイルを消せ」と案内させられる
	"Finding.Label":         "同一性を持つ値。書き換えず displayableIdentity で落とす",
	"Finding.PlistPath":     "同上。提示するコマンドが指すファイルそのもの",
	"Finding.Domain":        "同上。launchctl のドメイン指定に載る",
	"Undiagnosed.PlistPath": "同上。手で叩く plutil / ls が指すファイル",
}

// 内訳: Report(StatusErr / BrewErr / DirErrs) + Finding(Reasons / MissingExec /
// RestartKeys / BrewFormula / Commands) + Undiagnosed(Reason)
const wantChecked = 9

func TestSanitizeForDisplayCoversEveryStringField(t *testing.T) {
	displaycheck.Run(t, displaycheck.Spec{
		Dir: ".", Package: "svc",
		Gates: sanitizeGate, Exempt: sanitizeExempt, WantChecked: wantChecked,
		// svc は named string type を持たない (0 を明示する。canary の誤検知を避ける)
		MinNamedStringTypes: 0,
	})
}
