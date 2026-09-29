package ssd

import (
	"testing"

	"doctor/internal/displaycheck"
)

// 表示用の構造体と、それを無害化する関門 (issue 252)。検査の本体・脅威モデルは
// `doctor/internal/displaycheck` が正本。
//
// 🚨 **この表に載っていない型は検査されない**。新しい表示用の構造体を足したら、ここにも足すこと。
// smartctl の JSON を読む smartJSON は表示に出ない (そこから組んだ Check だけが出る) ので載せていない。
var sanitizeGate = map[string]displaycheck.Gate{
	"Report": {Func: "SanitizeForDisplay", Recv: "out"},
	"Check":  {Func: "SanitizeForDisplay", Recv: "c"},
}

var sanitizeExempt = map[string]string{
	"Check.Status": "内部生成の enum (外部の出力から作らない)",
	"Report.SMART": "内部生成の enum (overall が Checks から導く)",
	"Report.APFS":  "内部生成の enum (常に StatusUnchecked)",
}

// 内訳: Report(Model / Notes) + Check(Label / Value / Note)
const wantChecked = 5

func TestSanitizeForDisplayCoversEveryStringField(t *testing.T) {
	displaycheck.Run(t, displaycheck.Spec{
		Dir: ".", Package: "ssd",
		Gates: sanitizeGate, Exempt: sanitizeExempt, WantChecked: wantChecked,
		MinNamedStringTypes: 1, // Status は named string type
	})
}
