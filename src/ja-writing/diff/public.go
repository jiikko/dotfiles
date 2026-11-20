package diff

import (
	"fmt"
	"maps"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyops"
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

// check が子の dict を渡す口は、本家が例外になる dict (欄が欠ける・型が違う) で error を返す (check はそれを「子の JSON から人が読む形を作れません」にする)。

// Report は本家の report(d)。
func Report(d *pyjson.Object) (string, error) { return report(d) }

// ShowsRewrittenOnly は本家の _shows_rewritten_only。
func ShowsRewrittenOnly(d *pyjson.Object) (ok bool, err error) {
	defer pyops.Catch(&err)
	return showsRewrittenOnly(d), nil
}

// HunkHeading は本家の _hunk_heading。
func HunkHeading(h *pyjson.Object) (heading string, err error) {
	defer pyops.Catch(&err)
	return hunkHeading(h), nil
}

// FailOnCount は本家の fail_on_count (意味が変わる恐れのうち、--fail-on=meaning-risk が数えるものの数)。dict でない要素と kind の無い要素は error。
func FailOnCount(risks []pyjson.Value) (n int, err error) {
	defer pyops.Catch(&err)
	for _, r := range risks {
		kind, _ := idx(r, "kind").(string)
		subkind, _ := get(r, "subkind").(string)
		if failOnCounted(kind, subkind) {
			n++
		}
	}
	return n, nil
}

// DetectorKeys は DetectorSummary に渡せる key (本家の docstring の並び)。
var DetectorKeys = []string{"stiffness", "tone", "hedges", "immutable", "politeness", "register"}

// DetectorSummary は本家の detector_summary (完了報告の確認の記録に写す値)。d にその検査の結果が無いか、当てはまらない (applicable が false) なら nil。
func DetectorSummary(d *pyjson.Object, key string) (summary *string, err error) {
	defer pyops.Catch(&err)
	section, ok := d.Field(key).(*pyjson.Object)
	if !ok {
		return nil, nil
	}
	if b, ok := section.Field("applicable").(bool); ok && !b {
		return nil, nil
	}
	count := pyops.Or(get(section, "candidate_count"), int64(0))
	var risky pyjson.Value = int64(0)
	if key == "immutable" {
		risky = pyops.Or(get(section, "meaning_risk_count"), int64(0))
	}
	out := func(s string) (*string, error) { return &s, nil }
	if pyops.Truthy(risky) {
		prefix := ""
		if pyops.Truthy(count) {
			prefix = "候補" + pyStr(count) + "件、"
		}
		return out(prefix + "意味が変わる恐れ " + pyStr(risky) + "件")
	}
	if !pyops.Truthy(count) {
		return out("候補なし")
	}
	candidates, ok := get(section, "candidates").([]pyjson.Value)
	if !ok {
		label := "候補"
		if pyops.Equal(get(section, "level"), "info") {
			label = "参考"
		}
		return out(label + pyStr(count) + "件")
	}
	infos := 0
	for _, c := range candidates {
		if o, ok := c.(*pyjson.Object); ok && pyops.Equal(o.Field("level"), "info") {
			infos++
		}
	}
	switch {
	case !pyops.Less(int64(infos), count):
		return out("参考" + pyStr(count) + "件")
	case infos > 0:
		return out(fmt.Sprintf("候補%s件、参考%d件", pyStr(pyops.Sub(count, int64(infos))), infos))
	}
	return out("候補" + pyStr(count) + "件")
}

// ReportParts は本家の report_parts (レポートの節の文字列。無い節は空の文字列。鍵の順は本家の辞書の順)。
func ReportParts(d *pyjson.Object) (parts *pyjson.Object, err error) {
	defer pyops.Catch(&err)
	return reportParts(d), nil
}

func reportParts(d *pyjson.Object) *pyjson.Object {
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
