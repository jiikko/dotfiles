package diff

import (
	"fmt"
	"strings"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyops"
)

// 群 AD: 規則の見直し候補・足した人や担当・見出しの形・読点の増加の節 (本家の _rule_lines・_rule_section ほか)。

// showsRewrittenOnly は _shows_rewritten_only (書き直した文だけを見る候補 logic・bold・endings.flags を数え、出すか)。
func showsRewrittenOnly(d *pyjson.Object) bool {
	return !pyops.Equal(get(d, "status"), "no_change") && pyops.Truthy(idx(idx(d, "full_diff"), "hunks"))
}

// ruleCountOf は _rule_count (dict を読む形。ruleCount は型付きの値を数える)。
func ruleCountOf(d *pyjson.Object, rewrittenOnly bool) int {
	e := getDict(d, "endings")
	count := pyops.Len(idx(d, "markers"))
	for _, key := range []string{"new_words", "lost_words"} {
		if pyops.Truthy(idx(d, key)) {
			count++
		}
	}
	count += pyops.Len(idx(d, "structure")) + pyops.Len(getList(e, "changes")) + pyops.Len(getList(d, "type_changes"))
	if rewrittenOnly {
		count += pyops.Len(getList(d, "logic")) + pyops.Len(getList(e, "flags")) + pyops.Len(getList(d, "bold"))
	}
	return count
}

// shortened は本家の s if len(s) <= n else s[:n] + "…" ("{}" で書く値。str でない短い値はそのまま書く)。
func shortened(v pyjson.Value, n int) string {
	if pyops.Len(v) <= n {
		return pyStr(v)
	}
	return pyStr(pyops.Add(pyops.Slice(v, 0, n), "…"))
}

func kindHitsLine(items pyjson.Value) string {
	var parts []string
	for _, m := range pyops.Iter(items) {
		parts = append(parts, pyStr(idx(m, "kind"))+"（"+joinList(idx(m, "hits"))+"）")
	}
	return strings.Join(parts, "、")
}

// ruleLines は _rule_lines。rewrittenOnly が偽なら、書き直した文だけを見る候補 (つながり・文末の立場・太字) を出さない。
func ruleLines(d *pyjson.Object, rewrittenOnly bool) []string {
	var lines []string
	if pyops.Truthy(idx(d, "markers")) {
		lines = append(lines, "■ 言い回しの種類の増減（意味が変わりやすいところ）")
		for _, m := range pyops.Iter(idx(d, "markers")) {
			lines = append(lines, fmt.Sprintf("- %s: %s → %s（元: %s／後: %s）", pyStr(idx(m, "kind")), pyStr(idx(m, "orig")), pyStr(idx(m, "rewrite")),
				joinOr(idx(m, "orig_hits"), "なし"), joinOr(idx(m, "rewrite_hits"), "なし")))
		}
	}
	if pyops.Truthy(idx(d, "new_words")) {
		lines = append(lines, "■ 元の文にない語: "+joinList(idx(d, "new_words")))
	}
	if pyops.Truthy(idx(d, "lost_words")) {
		lines = append(lines, "■ 消えた語: "+joinList(idx(d, "lost_words")))
	}
	for _, s := range pyops.Iter(idx(d, "structure")) {
		lines = append(lines, concat("■ ", s))
	}
	if rewrittenOnly && pyops.Truthy(get(d, "logic")) {
		lines = append(lines, "■ つながりを確かめる場所（書き直した文。何と何をつないでいるか言えるか）")
		for _, p := range pyops.Iter(idx(d, "logic")) {
			sentence := shortened(idx(p, "sentence"), 44)
			lines = append(lines, fmt.Sprintf("- %s: %s", pyStr(idx(p, "kind")), sentence))
		}
	}
	e := getDict(d, "endings")
	if pyops.Truthy(get(e, "changes")) {
		lines = append(lines, "■ 文末の種類が変わった文（立場に合う向きか見る）")
		for _, c := range pyops.Iter(idx(e, "changes")) {
			sentence := shortened(idx(c, "rewrite"), 44)
			lines = append(lines, fmt.Sprintf("- %s → %s: %s", pyStr(idx(c, "orig_kind")), pyStr(idx(c, "rewrite_kind")), sentence))
		}
	}
	if pyops.Truthy(get(d, "type_changes")) {
		lines = append(lines, "■ 文の種類を表す語が変わった文（伝聞・推量・意見・例え・依頼が、元の文と同じか見る）")
		for _, c := range pyops.Iter(idx(d, "type_changes")) {
			lines = append(lines, "- 元: "+shortened(idx(c, "orig"), 60))
			lines = append(lines, "  後: "+shortened(idx(c, "rewrite"), 60))
			for _, side := range [][2]string{{"消えた語", "lost"}, {"増えた語", "added"}} {
				if pyops.Truthy(idx(c, side[1])) {
					lines = append(lines, "  "+side[0]+": "+kindHitsLine(idx(c, side[1])))
				}
			}
		}
	}
	if rewrittenOnly && pyops.Truthy(get(e, "flags")) {
		head := "■ 文末の立場が混ざっている候補（立場を決めてから見る）"
		if pyops.Truthy(get(e, "stance")) {
			head = fmt.Sprintf("■ 文末の立場（%sの文書として見た）", pyStr(idx(e, "stance")))
		}
		lines = append(lines, head)
		for _, f := range pyops.Iter(idx(e, "flags")) {
			lines = append(lines, "- "+pyStr(idx(f, "note")))
			for _, s := range pyops.Iter(idx(f, "sentences")) {
				lines = append(lines, "  ・"+shortened(s, 44))
			}
		}
	}
	if rewrittenOnly && pyops.Truthy(get(d, "bold")) {
		lines = append(lines, boldHead("書き直した文。"))
		for _, p := range pyops.Iter(idx(d, "bold")) {
			lines = append(lines, boldProblemLine(pyStr(idx(p, "line")), pyStr(idx(p, "found")), pyStr(pyops.Or(idx(p, "suggest"), "")), pyStr(idx(p, "how"))))
		}
	}
	return lines
}

// ruleSection は _rule_section。
func ruleSection(d *pyjson.Object) string {
	rewrittenOnly := showsRewrittenOnly(d)
	lines := ruleLines(d, rewrittenOnly)
	if len(lines) == 0 {
		return ""
	}
	return fmt.Sprintf("■ 規則の見直し候補（%d件）\n", ruleCountOf(d, rewrittenOnly)) + strings.Join(lines, "\n")
}

var actorFrameNames = map[string]string{"subject": "主語", "recipient": "知らせる・頼む相手"}

// addedActorSection は _added_actor_section。
func addedActorSection(d *pyjson.Object) string {
	items := getList(d, "added_actors")
	if !pyops.Truthy(items) {
		return ""
	}
	lines := []string{fmt.Sprintf("■ 原文にない人や担当を足した文（%d件。原文、資料、依頼のどれにもない人や担当なら、原文の書き方に戻す）", pyops.Len(items))}
	for _, item := range pyops.Iter(items) {
		var added []string
		for _, actor := range pyops.Iter(idx(item, "actors")) {
			frame := idx(actor, "frame")
			text := pyStr(pyops.Lookup(actorFrameNames, frame, frame)) + "に「" + pyStr(idx(actor, "word")) + "」"
			if pyops.Truthy(get(actor, "narrowed_from")) {
				text += "（原文の「" + pyStr(idx(actor, "narrowed_from")) + "」を狭めたかもしれない）"
			}
			added = append(added, text)
		}
		lines = append(lines, fmt.Sprintf("- %sを足しました%s", strings.Join(added, "、"), whereOf(idx(item, "old_line"), idx(item, "new_line"))))
		if get(item, "old") != nil {
			lines = append(lines, concat("  元: ", idx(item, "old")))
		}
		lines = append(lines, concat("  後: ", idx(item, "new")))
	}
	return strings.Join(lines, "\n")
}

// oldNewLines は本家の lines.append("  元: " + item["old"]); lines.append("  後: " + item["new"]) (どちらも欠かせない欄)。
func oldNewLines(item pyjson.Value) []string {
	return []string{concat("  元: ", idx(item, "old")), concat("  後: ", idx(item, "new"))}
}

// headingShapeSection は _heading_shape_section。
func headingShapeSection(d *pyjson.Object) string {
	items := getList(d, "heading_shapes")
	if !pyops.Truthy(items) {
		return ""
	}
	lines := []string{fmt.Sprintf("■ 見出しの形が変わった（%d件。依頼で見出しの形や長さを指定されていれば、元の形に戻す）", pyops.Len(items))}
	for _, item := range pyops.Iter(items) {
		lines = append(lines, "- 見出しを書き換えました"+whereOf(idx(item, "old_line"), idx(item, "new_line")))
		lines = append(lines, oldNewLines(item)...)
	}
	return strings.Join(lines, "\n")
}

// rate は本家の item[count] / item[sentences] if item[sentences] else 0.0。
func rate(item pyjson.Value, count, sentences string) float64 {
	if !pyops.Truthy(idx(item, sentences)) {
		return 0
	}
	return pyops.Div(idx(item, count), idx(item, sentences))
}

// commaSection は _comma_section。
func commaSection(d *pyjson.Object) string {
	item := get(d, "comma_increase")
	if !pyops.Truthy(item) {
		return ""
	}
	oldRate, newRate := rate(item, "old_count", "old_sentences"), rate(item, "new_count", "new_sentences")
	lines := []string{fmt.Sprintf("■ 読点が増えた（%s個 → %s個。1文あたり %s個 → %s個。足した読点が、誤読を防ぐ所、長い修飾や節の切れ目、"+
		"並べ、条件・逆接・理由の切れ目、主題のあとに別の動作主が続く所のどれかに当たるか確かめる。当たらなければ外す）",
		pyStr(idx(item, "old_count")), pyStr(idx(item, "new_count")), pyops.FormatFixed(oldRate, 2), pyops.FormatFixed(newRate, 2))}
	for _, sentence := range pyops.Iter(idx(item, "sentences")) {
		lines = append(lines, fmt.Sprintf("- 読点が%s個から%s個に増えました%s", pyStr(idx(sentence, "old_count")), pyStr(idx(sentence, "new_count")),
			whereOf(idx(sentence, "old_line"), idx(sentence, "new_line"))))
		lines = append(lines, oldNewLines(sentence)...)
	}
	return strings.Join(lines, "\n")
}
