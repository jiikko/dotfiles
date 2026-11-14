package diff

import (
	"cmp"
	"fmt"
	"strings"

	"ja-writing/internal/pyjson"
)

// 群 AD: 規則の見直し候補・足した人や担当・見出しの形・読点の増加の節 (本家の _rule_lines・_rule_section ほか)。

// showsRewrittenOnly は _shows_rewritten_only (書き直した文だけを見る候補 logic・bold・endings.flags を数え、出すか)。
func showsRewrittenOnly(d *pyjson.Object) bool {
	return fieldStr(d, "status") != "no_change" && len(fieldList(fieldObj(d, "full_diff"), "hunks")) > 0
}

// ruleCountOf は _rule_count (dict を読む形。ruleCount は型付きの値を数える)。
func ruleCountOf(d *pyjson.Object, rewrittenOnly bool) int {
	e := fieldObj(d, "endings")
	count := len(fieldList(d, "markers")) + len(fieldList(d, "structure")) + len(fieldList(e, "changes")) + len(fieldList(d, "type_changes"))
	if truthy(field(d, "new_words")) {
		count++
	}
	if truthy(field(d, "lost_words")) {
		count++
	}
	if rewrittenOnly {
		count += len(fieldList(d, "logic")) + len(fieldList(e, "flags")) + len(fieldList(d, "bold"))
	}
	return count
}

func kindHitsLine(items []pyjson.Value) string {
	parts := make([]string, len(items))
	for i, item := range items {
		o := asObj(item)
		parts[i] = fmt.Sprintf("%s（%s）", pyStr(field(o, "kind")), joinList(field(o, "hits")))
	}
	return strings.Join(parts, "、")
}

// ruleLines は _rule_lines。rewrittenOnly が偽なら、書き直した文だけを見る候補 (つながり・文末の立場・太字) を出さない。
func ruleLines(d *pyjson.Object, rewrittenOnly bool) []string {
	var lines []string
	if markers := fieldList(d, "markers"); len(markers) > 0 {
		lines = append(lines, "■ 言い回しの種類の増減（意味が変わりやすいところ）")
		for _, m := range markers {
			o := asObj(m)
			lines = append(lines, fmt.Sprintf("- %s: %s → %s（元: %s／後: %s）", pyStr(field(o, "kind")), pyStr(field(o, "orig")), pyStr(field(o, "rewrite")),
				cmp.Or(joinList(field(o, "orig_hits")), "なし"), cmp.Or(joinList(field(o, "rewrite_hits")), "なし")))
		}
	}
	if truthy(field(d, "new_words")) {
		lines = append(lines, "■ 元の文にない語: "+joinList(field(d, "new_words")))
	}
	if truthy(field(d, "lost_words")) {
		lines = append(lines, "■ 消えた語: "+joinList(field(d, "lost_words")))
	}
	for _, s := range strList(field(d, "structure")) {
		lines = append(lines, "■ "+s)
	}
	if logic := fieldList(d, "logic"); rewrittenOnly && len(logic) > 0 {
		lines = append(lines, "■ つながりを確かめる場所（書き直した文。何と何をつないでいるか言えるか）")
		for _, p := range logic {
			o := asObj(p)
			lines = append(lines, fmt.Sprintf("- %s: %s", pyStr(field(o, "kind")), shorten(fieldStr(o, "sentence"), 44)))
		}
	}
	e := fieldObj(d, "endings")
	if changes := fieldList(e, "changes"); len(changes) > 0 {
		lines = append(lines, "■ 文末の種類が変わった文（立場に合う向きか見る）")
		for _, c := range changes {
			o := asObj(c)
			lines = append(lines, fmt.Sprintf("- %s → %s: %s", pyStr(field(o, "orig_kind")), pyStr(field(o, "rewrite_kind")), shorten(fieldStr(o, "rewrite"), 44)))
		}
	}
	if changes := fieldList(d, "type_changes"); len(changes) > 0 {
		lines = append(lines, "■ 文の種類を表す語が変わった文（伝聞・推量・意見・例え・依頼が、元の文と同じか見る）")
		for _, c := range changes {
			o := asObj(c)
			lines = append(lines, "- 元: "+shorten(fieldStr(o, "orig"), 60), "  後: "+shorten(fieldStr(o, "rewrite"), 60))
			for _, side := range [][2]string{{"消えた語", "lost"}, {"増えた語", "added"}} {
				if items := fieldList(o, side[1]); len(items) > 0 {
					lines = append(lines, "  "+side[0]+": "+kindHitsLine(items))
				}
			}
		}
	}
	if flags := fieldList(e, "flags"); rewrittenOnly && len(flags) > 0 {
		head := "■ 文末の立場が混ざっている候補（立場を決めてから見る）"
		if truthy(field(e, "stance")) {
			head = fmt.Sprintf("■ 文末の立場（%sの文書として見た）", pyStr(field(e, "stance")))
		}
		lines = append(lines, head)
		for _, f := range flags {
			o := asObj(f)
			lines = append(lines, "- "+pyStr(field(o, "note")))
			for _, s := range strList(field(o, "sentences")) {
				lines = append(lines, "  ・"+shorten(s, 44))
			}
		}
	}
	if problems := fieldList(d, "bold"); rewrittenOnly && len(problems) > 0 {
		lines = append(lines, boldHead("書き直した文。"))
		for _, p := range problems {
			o := asObj(p)
			lines = append(lines, boldProblemLine(fieldInt(o, "line"), fieldStr(o, "found"), fieldStr(o, "suggest"), fieldStr(o, "how")))
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

// itemLines は候補 1 件の「元: 」「後: 」の行 (元は無いことがある)。
func itemLines(o *pyjson.Object, oldKey, newKey string) []string {
	var lines []string
	if v := field(o, oldKey); v != nil {
		lines = append(lines, "  元: "+pyStr(v))
	}
	if v := field(o, newKey); v != nil {
		lines = append(lines, "  後: "+pyStr(v))
	}
	return lines
}

// addedActorSection は _added_actor_section。
func addedActorSection(d *pyjson.Object) string {
	items := fieldList(d, "added_actors")
	if len(items) == 0 {
		return ""
	}
	lines := []string{fmt.Sprintf("■ 原文にない人や担当を足した文（%d件。原文、資料、依頼のどれにもない人や担当なら、原文の書き方に戻す）", len(items))}
	for _, it := range items {
		item := asObj(it)
		var added []string
		for _, a := range fieldList(item, "actors") {
			actor := asObj(a)
			frame := fieldStr(actor, "frame")
			name, ok := actorFrameNames[frame]
			if !ok {
				name = frame
			}
			text := fmt.Sprintf("%sに「%s」", name, pyStr(field(actor, "word")))
			if truthy(field(actor, "narrowed_from")) {
				text += fmt.Sprintf("（原文の「%s」を狭めたかもしれない）", pyStr(field(actor, "narrowed_from")))
			}
			added = append(added, text)
		}
		lines = append(lines, fmt.Sprintf("- %sを足しました%s", strings.Join(added, "、"), whereOf(field(item, "old_line"), field(item, "new_line"))))
		lines = append(lines, itemLines(item, "old", "new")...)
	}
	return strings.Join(lines, "\n")
}

// headingShapeSection は _heading_shape_section。
func headingShapeSection(d *pyjson.Object) string {
	items := fieldList(d, "heading_shapes")
	if len(items) == 0 {
		return ""
	}
	lines := []string{fmt.Sprintf("■ 見出しの形が変わった（%d件。依頼で見出しの形や長さを指定されていれば、元の形に戻す）", len(items))}
	for _, it := range items {
		item := asObj(it)
		lines = append(lines, "- 見出しを書き換えました"+whereOf(field(item, "old_line"), field(item, "new_line")),
			"  元: "+pyStr(field(item, "old")), "  後: "+pyStr(field(item, "new")))
	}
	return strings.Join(lines, "\n")
}

func rate(count, sentences int) float64 {
	if sentences == 0 {
		return 0
	}
	return float64(count) / float64(sentences)
}

// commaSection は _comma_section。
func commaSection(d *pyjson.Object) string {
	item := fieldObj(d, "comma_increase")
	if !truthy(item) {
		return ""
	}
	lines := []string{fmt.Sprintf("■ 読点が増えた（%d個 → %d個。1文あたり %.2f個 → %.2f個。足した読点が、誤読を防ぐ所、長い修飾や節の切れ目、"+
		"並べ、条件・逆接・理由の切れ目、主題のあとに別の動作主が続く所のどれかに当たるか確かめる。当たらなければ外す）",
		fieldInt(item, "old_count"), fieldInt(item, "new_count"),
		rate(fieldInt(item, "old_count"), fieldInt(item, "old_sentences")), rate(fieldInt(item, "new_count"), fieldInt(item, "new_sentences")))}
	for _, s := range fieldList(item, "sentences") {
		sentence := asObj(s)
		lines = append(lines, fmt.Sprintf("- 読点が%d個から%d個に増えました%s", fieldInt(sentence, "old_count"), fieldInt(sentence, "new_count"),
			whereOf(field(sentence, "old_line"), field(sentence, "new_line"))))
		lines = append(lines, itemLines(sentence, "old", "new")...)
	}
	return strings.Join(lines, "\n")
}
