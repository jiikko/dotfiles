package docker

import (
	"testing"

	"doctor/internal/displaycheck"
)

// 表示用の構造体と、それを無害化する関門 (issue 252)。検査の本体・脅威モデル・
// 「検出しない形」は `doctor/internal/displaycheck` が正本。
//
// 🚨 **この表に載っていない型は検査されない**。新しい表示用の構造体を足したら、ここにも足すこと。
// `docker system df` の JSON を読むための内部構造体 (dfImage / dfContainer / …) は
// 表示に出ない (そこから組んだ Item / Group だけが出る) ので載せていない。
var sanitizeGate = map[string]displaycheck.Gate{
	"Report": {Func: "SanitizeForDisplay", Recv: "out"},
	"Group":  {Func: "SanitizeForDisplay", Recv: "g"},
	"Item":   {Func: "SanitizeForDisplay", Recv: "it"},
}

// 無害化しないと決めた文字列フィールド。**理由を必ず書く** (書けないなら無害化する側が正しい)。
var sanitizeExempt = map[string]string{
	// 🚨 同一性を持つ値は書き換えず**落とす** (display.go の 🚨 が出典)。提示する
	// `docker rmi` / `docker volume rm` が指す資源そのものなので、無害化して出すと、
	// 攻撃者が無害化後の名前の資源を別に置くだけで「別のものを消せ」と案内させられる
	"Item.Name":    "同一性を持つ値。書き換えず termsafe.IsPlain で落とす",
	"Item.Command": "同上。提示コマンドの文字列そのもの",
	// Kind は内部生成の enum (containers / images / build-cache / volumes)
	"Group.Kind": "内部生成の enum (docker の出力から作らない)",
}

// 内訳: Report(Unavailable / SystemPrune / SystemPruneNote / Notes) +
// Group(Label / Command / Notes) + Item(Detail / SizeText)
const wantChecked = 9

func TestSanitizeForDisplayCoversEveryStringField(t *testing.T) {
	displaycheck.Run(t, displaycheck.Spec{
		Dir: ".", Package: "docker",
		Gates: sanitizeGate, Exempt: sanitizeExempt, WantChecked: wantChecked,
		// docker は Kind を named string type で持つ
		MinNamedStringTypes: 1,
	})
}
