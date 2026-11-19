package diff

import (
	"fmt"
	"maps"

	"ja-writing/internal/pyjson"
)

// check が diff から使う口 (本家 check の _diff_module() が取る名前)。check は子の `diff --json` の出力を pyjson.Loads で読んだ dict を渡す。

// DiffFormatVersion は本家の DIFF_FORMAT_VERSION (--json の format_version)。
const DiffFormatVersion = formatVersion

// MaterialsFormatVersion は本家の MATERIALS_FORMAT_VERSION (資料と照らす結果の形式)。
const MaterialsFormatVersion = 1

// Stances は本家の STANCES の写し (呼び出し側が書き換えても diff の表は変わらない)。
func Stances() map[string]string { return maps.Clone(stances) }

// ListProblems は本家の _LIST_PROBLEMS (一覧のファイルがあるのに読めなかった理由)。Go は語の一覧を埋め込みで持ち、読めない場合が無いので常に空。
func ListProblems() map[string]string { return map[string]string{} }

// Report は本家の report(d)。
func Report(d *pyjson.Object) (string, error) { return report(d) }

// ShowsRewrittenOnly は本家の _shows_rewritten_only。
func ShowsRewrittenOnly(d *pyjson.Object) bool { return showsRewrittenOnly(d) }

// HunkHeading は本家の _hunk_heading。
func HunkHeading(h *pyjson.Object) string { return hunkHeading(h) }

// FailOnCount は本家の fail_on_count (dict の並びを受ける。dict でない要素は kind の無い恐れとして数えない)。
func FailOnCount(risks []pyjson.Value) int {
	objs := make([]*pyjson.Object, 0, len(risks))
	for _, r := range risks {
		if o := asObj(r); o != nil {
			objs = append(objs, o)
		}
	}
	return failOnCount(objs)
}

// DetectorKeys は DetectorSummary に渡せる key (本家の docstring の並び)。
var DetectorKeys = []string{"stiffness", "tone", "hedges", "immutable", "politeness", "register"}

// DetectorSummary は本家の detector_summary (完了報告の確認の記録に写す値)。d にその検査の結果が無いか、当てはまらない (applicable が false) なら nil。
func DetectorSummary(d *pyjson.Object, key string) *string {
	section := fieldObj(d, key)
	if section == nil {
		return nil
	}
	if b, ok := field(section, "applicable").(bool); ok && !b {
		return nil
	}
	count := fieldInt(section, "candidate_count")
	risky := 0
	if key == "immutable" {
		risky = fieldInt(section, "meaning_risk_count")
	}
	out := func(s string) *string { return &s }
	if risky != 0 {
		prefix := ""
		if count != 0 {
			prefix = fmt.Sprintf("候補%d件、", count)
		}
		return out(fmt.Sprintf("%s意味が変わる恐れ %d件", prefix, risky))
	}
	if count == 0 {
		return out("候補なし")
	}
	candidates, ok := field(section, "candidates").([]pyjson.Value)
	if !ok {
		label := "候補"
		if fieldStr(section, "level") == "info" {
			label = "参考"
		}
		return out(fmt.Sprintf("%s%d件", label, count))
	}
	infos := 0
	for _, c := range candidates {
		if o := asObj(c); o != nil && fieldStr(o, "level") == "info" {
			infos++
		}
	}
	switch {
	case infos >= count:
		return out(fmt.Sprintf("参考%d件", count))
	case infos > 0:
		return out(fmt.Sprintf("候補%d件、参考%d件", count-infos, infos))
	}
	return out(fmt.Sprintf("候補%d件", count))
}

// ReportParts は本家の report_parts (レポートの節の文字列。無い節は空の文字列。鍵の順は本家の辞書の順)。
func ReportParts(d *pyjson.Object) *pyjson.Object {
	o := pyjson.NewObject()
	o.Set("risks", riskSection(d))
	o.Set("rules", ruleSection(d))
	o.Set("added_actors", addedActorSection(d))
	o.Set("heading_shapes", headingShapeSection(d))
	o.Set("comma_increase", commaSection(d))
	o.Set("stiffness", detectorSection(d, "stiffness"))
	o.Set("tone", detectorSection(d, "tone"))
	o.Set("full_diff", fullDiffSection(d))
	for _, key := range newReviewSections {
		o.Set(key, newReviewSection(d, key))
	}
	return o
}
