package health

import (
	"testing"

	"doctor/internal/displaycheck"
)

// 表示用の構造体と、それを無害化する関門。検査の本体は `doctor/internal/displaycheck` が正本。
// 🚨 この表に載っていない型は検査されない。表示に出る構造体を足したら、ここにも足す。
var sanitizeGate = map[string]displaycheck.Gate{
	"Report": {Func: "SanitizeForDisplay", Recv: "out"},
	"Check":  {Func: "SanitizeForDisplay", Recv: "c"},
}

var sanitizeExempt = map[string]string{
	"Check.Category":    "内部生成の enum (観点の定数)",
	"Check.ID":          "内部生成の定数 (skill が項目を指す安定キー)",
	"Check.Status":      "内部生成の enum",
	"Report.Categories": "内部生成の enum の列 (ParseCategories が既知の値だけを通す)",
}

// 内訳: Check(Label / Value / Note / Fix)
const wantChecked = 4

func TestSanitizeForDisplayCoversEveryStringField(t *testing.T) {
	displaycheck.Run(t, displaycheck.Spec{
		Dir: ".", Package: "health",
		Gates: sanitizeGate, Exempt: sanitizeExempt, WantChecked: wantChecked,
		MinNamedStringTypes: 2, // Status / Category
	})
}
