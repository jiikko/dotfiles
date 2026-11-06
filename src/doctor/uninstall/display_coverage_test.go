package uninstall

import (
	"testing"

	"doctor/internal/displaycheck"
)

// 表示用の構造体と、それを無害化する関門。検査の本体は `doctor/internal/displaycheck` が正本。
// 🚨 この表に載っていない型は検査されない。表示に出る構造体を足したら、ここにも足す。
// Leftover は Item へ写してから出る (直接は表示しない) ので載せない。Result は apply の結果で、パスは計画 (=作り直した候補) から来る。
var sanitizeGate = map[string]displaycheck.Gate{
	"Inventory": {Func: "SanitizeInventory", Recv: "out"},
	"App":       {Func: "SanitizeInventory", Recv: "a"},
	"Formula":   {Func: "SanitizeInventory", Recv: "f"},
	"Item":      {Func: "SanitizePlan", Recv: "it"},
	"Result":    {Func: "SanitizeResult", Recv: "out"},
	"Done":      {Func: "SanitizeResult", Recv: "d"},
	"Skip":      {Func: "SanitizeResult", Recv: "s"},
}

var sanitizeExempt = map[string]string{
	"App.Source":    "内部生成の enum",
	"Item.Tier":     "内部生成の enum",
	"Item.Action":   "内部生成の enum",
	"Done.Action":   "内部生成の enum",
	"Result.PlanID": "計画の ID (hex。ReadPlan が中身の hash と照合済み)",
}

// 内訳: Inventory(Notes) + App(Name BundleID Version Path CaskToken PkgID Manual) + Formula(Name Version Path Protected UsedBy)
// + Item(Owner Path Token Note) + Result(Trash) + Done(Owner Path To) + Skip(Owner Path Reason)
const wantChecked = 24

func TestSanitizeCoversEveryStringField(t *testing.T) {
	displaycheck.Run(t, displaycheck.Spec{
		Dir: ".", Package: "uninstall",
		Gates: sanitizeGate, Exempt: sanitizeExempt, WantChecked: wantChecked,
		MinNamedStringTypes: 3, // Source / Tier / Action
	})
}
