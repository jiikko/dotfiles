package disk

import (
	"testing"

	"doctor/internal/displaycheck"
)

// 表示用の構造体と、それを無害化する関門。ネストした型 (Item / EntryOutcome / ItemOutcome) は
// 親の関門がループの中で処理するので、親の関門を担当として書く。
//
// 🚨 **この表に載っていない型は検査されない**。新しい表示用の構造体を足したら、ここにも足すこと。
// 検査の本体・脅威モデル・「検出しない形」は `doctor/internal/displaycheck` が正本
// (disk / svc / docker で 1 実装を共有する。issue 252)。
var sanitizeGate = map[string]displaycheck.Gate{
	"Report":        {Func: "SanitizeForDisplay", Recv: "out"},
	"Result":        {Func: "SanitizeResultForDisplay", Recv: "r"},
	"Entry":         {Func: "SanitizeEntryForDisplay", Recv: "e"},
	"Item":          {Func: "SanitizeResultForDisplay", Recv: "it"},
	"DeleteReport":  {Func: "SanitizeDeleteReportForDisplay", Recv: "out"},
	"EntryOutcome":  {Func: "SanitizeDeleteReportForDisplay", Recv: "e"},
	"ItemOutcome":   {Func: "SanitizeDeleteReportForDisplay", Recv: "it"},
	"CommandRecord": {Func: "SanitizeCommandRecordForDisplay", Recv: "c"},
}

// 無害化しないと決めた文字列フィールド。**理由を必ず書く** (書けないなら無害化する側が正しい)。
var sanitizeExempt = map[string]string{
	// 🚨 この 2 つは「外部由来ではない」ではなく **呼び出し側の不変条件に依存**している。
	// snapshot 復元経路では Entry / Status も保存ファイルの中身になるが、glogx が
	// doctorSnapshotInCatalog (doctor_cache.go) でカタログへ束ね直し、knownDiskStatus で
	// 未知の Status を落としている。**依存先が変わったら無害化する側へ倒すこと**
	// (Entry.ID は doctor_view.go の y のコピー本文に [%s] として出る)
	"Result.Status":        "glogx の knownDiskStatus (doctor_cache.go) が未知値を落とす前提",
	"Item.Path":            "同一性を持つ値。書き換えず DisplayablePath で落とす (display.go の 🚨)",
	"ItemOutcome.Path":     "同上。削除の照合 (planDelete の itemKey) から外れるので書き換えない",
	"Entry.ID":             "glogx の doctorSnapshotInCatalog がカタログへ束ね直す前提 (復元経路)",
	"Entry.Paths":          "カタログのリテラル (catalog.go)。glob の元で、展開後は Item.Path 側の規律に載る",
	"Entry.Processes":      "カタログのリテラル (catalog.go)。guard の突合に使う固定のプロセス名",
	"Entry.Guard":          "Guard 型の enum (boottime / sim-device / process-absent)",
	"EntryOutcome.ID":      "カタログの固定 ID。DeleteReport は復元経路を持たない (engine が毎回作る)",
	"EntryOutcome.Outcome": "内部生成の enum (planned / deleted / …)",
	"EntryOutcome.Method":  "カタログの DeleteVia から導く内部語 (rm / trash / cli / propose)",
	"ItemOutcome.Outcome":  "内部生成の enum",
	"ItemOutcome.Staged":   "内部生成の予測不能名 (.glogx-delete-<hex>)。外部由来の文字は入らない",
	// 🚨 Remaining は「触ったが残った」パスの記録。**同一性を持つ値なので書き換えない**
	// (Item.Path と同じ規律)。今は Reason の件数算出にしか使っておらず表示に出ないが、
	// 表示に出すなら DisplayablePath で落とす側へ倒すこと (書き換えると照合から外れる)
	"EntryOutcome.Remaining": "同一性を持つパスの記録。件数の算出のみに使い、表示には出さない",
}

// wantChecked は「非 exempt の文字列フィールド」の期待件数 (射程が縮んだことを赤にする錨)。
const wantChecked = 23

func TestSanitizeForDisplayCoversEveryStringField(t *testing.T) {
	displaycheck.Run(t, displaycheck.Spec{
		Dir: ".", Package: "disk",
		Gates: sanitizeGate, Exempt: sanitizeExempt, WantChecked: wantChecked,
		// disk は Status / Risk / Guard / Outcome / Method を named string type で持つ
		MinNamedStringTypes: 4,
	})
}
