package diff

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"ja-writing/internal/pyjson"
)

// 群 AD: 文体の硬化・勢い・慎重表現・動かしてはいけない値・敬語・文体の切り替えの節 (本家の _stiffness_line・_tone_line・_detector_section・_new_review_section)。

const tonePlacesShown = 5 // 1 つの候補で並べる位置の上限 (超えた分は「ほか N か所」。本家の TONE_PLACES_SHOWN)

var newReviewSections = []string{"hedges", "immutable", "politeness", "register"}

var newSectionNames = map[string]string{"hedges": "慎重な言い方", "immutable": "動かしてはいけない値", "politeness": "敬語の度合い", "register": "文体の切り替え"}

var toneLabels = map[string]string{"exclamation": "「！」", "question": "「？」", "emoji": "絵文字"}

type toneRule struct {
	subkinds []string
	middle   string // 並べるときの言い方 (「〜し」)
	last     string // 最後に置く言い方 (「〜する」)
}

var toneInfoRules = []toneRule{
	{[]string{"exclamation", "question", "intensifier"}, "「！」や強める語を減らし", "「！」や強める語を減らす"},
	{[]string{"emoji"}, "絵文字を外し", "絵文字を外す"},
	{[]string{"heading"}, "見出しを短い項目名にし", "見出しを短い項目名にする"},
}

var toneTypedRules = []toneRule{
	{[]string{"emoji"}, "絵文字を外し", "絵文字を外す"},
	{[]string{"heading"}, "見出しを短い項目名にし", "見出しを短い項目名にする"},
}

// listNote は一覧のファイルを読めなかったときの 1 行 (本家の _LIST_NOTE)。
func listNote(label, name, problem string) string {
	return fmt.Sprintf("- %s references/lists/%s.txt を読めなかったので、内蔵の一覧で検査しました（%s）。ファイルに足し引きした語は効いていません。", label, name, problem)
}

type listNoteLabel struct{ section, jsonKey, name, label string }

var listNoteLabels = []listNoteLabel{
	{"stiffness", "stiffness", "nominalization", "名詞化の言い回しの一覧"},
	{"tone", "tone", "intensifiers", "強める語の一覧"},
	{"tone", "feeling", "feelings", "気持ちの語の一覧（意味が変わる恐れの「元にない気持ちの語」に使う）"},
}

func listNotes(d *pyjson.Object, key string) []string {
	var notes []string
	for _, l := range listNoteLabels {
		if l.section != key {
			continue
		}
		if problem := field(fieldObj(d, l.jsonKey), "list_problem"); truthy(problem) {
			notes = append(notes, listNote(l.label, l.name, pyStr(problem)))
		}
	}
	return notes
}

// lineWhere は _line_where (括弧のない行の位置)。
func lineWhere(oldLine, newLine pyjson.Value) string {
	switch {
	case oldLine != nil && newLine != nil:
		if asInt(oldLine) == asInt(newLine) {
			return fmt.Sprintf("%d行目", asInt(newLine))
		}
		return fmt.Sprintf("元%d行目 → 後%d行目", asInt(oldLine), asInt(newLine))
	case newLine != nil:
		return fmt.Sprintf("後%d行目", asInt(newLine))
	case oldLine != nil:
		return fmt.Sprintf("元%d行目", asInt(oldLine))
	}
	return ""
}

func stiffnessLine(c *pyjson.Object) string {
	subkind, detail := fieldStr(c, "subkind"), fieldObj(c, "detail")
	tail := "硬い言い方に寄っていないか確かめてください。"
	f0 := func(key string) string { return fmt.Sprintf("%.0f", asFloat(field(detail, key))) }
	switch subkind {
	case "sentence":
		return fmt.Sprintf("%sの文で、漢字の割合が%s%%から%s%%に増えました（%s字→%s字）。%s", lineWhere(field(c, "old_line"), field(c, "new_line")),
			f0("old_kanji_ratio"), f0("new_kanji_ratio"), pyStr(field(detail, "old_length")), pyStr(field(detail, "new_length")), tail)
	case "sentence_nominal":
		return fmt.Sprintf("%sの文に、名詞化の言い回し（%s）が出て、漢字が%d字増えました。%s", lineWhere(field(c, "old_line"), field(c, "new_line")),
			joinList(field(detail, "added_names")), fieldInt(detail, "new_kanji")-fieldInt(detail, "old_kanji"), tail)
	case "document":
		// 平均の文の長さは、表示する値 (整数に丸めた値) で増えたかを決める (17.5 字 → 18.5 字は、どちらも 18 字と表示するので「増えました」と書かない)
		oldAvg, newAvg := f0("old_avg_length"), f0("new_avg_length")
		var text string
		switch {
		case atoiStr(newAvg) > atoiStr(oldAvg):
			text = fmt.Sprintf("推敲後、漢字の割合が%s%%から%s%%に、平均の文の長さが%s字から%s字に増えました。", f0("old_kanji_ratio"), f0("new_kanji_ratio"), oldAvg, newAvg)
		case newAvg == oldAvg:
			text = fmt.Sprintf("推敲後、漢字の割合が%s%%から%s%%に増えました（平均の文の長さは%s字のまま）。", f0("old_kanji_ratio"), f0("new_kanji_ratio"), oldAvg)
		default:
			text = fmt.Sprintf("推敲後、漢字の割合が%s%%から%s%%に増えました（平均の文の長さは%s字から%s字）。", f0("old_kanji_ratio"), f0("new_kanji_ratio"), oldAvg, newAvg)
		}
		if example := fieldObj(detail, "example"); truthy(example) {
			return text + fmt.Sprintf("硬い文体に寄っていないか確かめてください（例：%s『%s』→『%s』）。", lineWhere(field(example, "old_line"), field(example, "new_line")),
				pyStr(field(example, "old")), pyStr(field(example, "new")))
		}
		return text + "硬い文体に寄っていないか確かめてください。"
	case "nominalization":
		lines := fieldList(detail, "new_lines")
		if len(lines) > 3 {
			lines = lines[:3]
		}
		shown := strings.Join(strList(lines), "、")
		if shown == "" {
			shown = "?"
		}
		return fmt.Sprintf("名詞化の言い回し（%s）が%s個から%s個に増えました（後%s行目など）。%s", joinList(field(detail, "added_names")),
			pyStr(field(detail, "old")), pyStr(field(detail, "new")), shown, tail)
	case "no_chain":
		chains := fieldList(detail, "chains")
		return fmt.Sprintf("後%d行目に「の」が続く形（『%s』）が出ました。原文にない形です。%s", fieldInt(c, "new_line"), pyStr(chains[0]), tail)
	}
	return subkind
}

func atoiStr(s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}

// reportTonePlace は _tone_place (勢いの候補の位置 1 つ)。
func reportTonePlace(p *pyjson.Object) string {
	if field(p, "new") == nil {
		return fmt.Sprintf("%s『%s』を削除", lineWhere(field(p, "old_line"), nil), pyStr(field(p, "old")))
	}
	return fmt.Sprintf("%s『%s』→『%s』", lineWhere(field(p, "old_line"), field(p, "new_line")), pyStr(field(p, "old")), pyStr(field(p, "new")))
}

// toneMorePlaces は _tone_more_places (2 か所目からの行。tonePlacesShown か所まで。超えた分は「ほか N か所」)。
func toneMorePlaces(c *pyjson.Object) []string {
	places := fieldList(fieldObj(c, "detail"), "places")
	var rows []string
	for i := 1; i < len(places) && i < tonePlacesShown; i++ {
		rows = append(rows, "  "+reportTonePlace(asObj(places[i])))
	}
	if len(places) > tonePlacesShown {
		rows = append(rows, fmt.Sprintf("  ほか%dか所", len(places)-tonePlacesShown))
	}
	return rows
}

func reportToneLine(c *pyjson.Object) string {
	subkind, detail := fieldStr(c, "subkind"), fieldObj(c, "detail")
	places := fieldList(detail, "places")
	shown := ""
	if len(places) > 0 {
		shown = "（" + reportTonePlace(asObj(places[0])) + "）"
	}
	tail := "書き手の勢いを落としていないか確かめてください。"
	if subkind == "heading" {
		return fmt.Sprintf("見出しの字数の合計が%d字から%d字（原文の%.0f%%）になりました%s。%s", fieldInt(detail, "old"), fieldInt(detail, "new"),
			100.0*asFloat(field(detail, "new"))/asFloat(field(detail, "old")), shown, tail)
	}
	var label string
	switch {
	case subkind == "intensifier" && truthy(field(detail, "words")):
		label = "強める語（" + joinList(field(detail, "words")) + "）"
	case subkind == "intensifier":
		label = "強める語"
	default:
		var ok bool
		if label, ok = toneLabels[subkind]; !ok {
			label = subkind
		}
	}
	return fmt.Sprintf("原文の%s%s個が、推敲後は%s個になりました%s。%s", label, pyStr(field(detail, "old")), pyStr(field(detail, "new")), shown, tail)
}

// toneReference は参考の理由の文に書く、参考にした候補ごとの直し方 (rules の最後だけ言い切りの形)。
func toneReference(rules []toneRule) string {
	parts := make([]string, 0, len(rules))
	for _, r := range rules[:len(rules)-1] {
		parts = append(parts, r.middle)
	}
	return strings.Join(append(parts, rules[len(rules)-1].last), "、")
}

func infoSubkinds(candidates []pyjson.Value) []string {
	var shown []string
	for _, c := range candidates {
		if o := asObj(c); fieldStr(o, "level") == "info" {
			shown = append(shown, fieldStr(o, "subkind"))
		}
	}
	return shown
}

// detectorSection は _detector_section (「■ 文体の硬化」と「■ 勢い」の節)。key は stiffness か tone。
func detectorSection(d *pyjson.Object, key string) string {
	candidates := fieldList(fieldObj(d, key), "candidates")
	notes := listNotes(d, key)
	title := "勢い"
	if key == "stiffness" {
		title = "文体の硬化"
	}
	if len(candidates) == 0 {
		if len(notes) == 0 {
			return ""
		}
		return strings.Join(append([]string{"■ " + title + "（一覧の注意）"}, notes...), "\n")
	}
	infos := len(infoSubkinds(candidates))
	reviews := len(candidates) - infos
	docType := field(fieldObj(d, "context"), "doc_type")
	var review, reference string
	render := reportToneLine
	if key == "stiffness" {
		render = stiffnessLine
		review = "硬い言い方に寄っていないか確かめる"
		reference = fmt.Sprintf("文書の種類 %s では硬い言い方が普通なので、参考として出しています", pyStr(docType))
	} else {
		review = "書き手の勢いを落としていないか確かめる"
		shown := infoSubkinds(candidates)
		if s, ok := docType.(string); ok && slices.Contains(toneInfoDocTypes, s) {
			var rules []toneRule
			for _, r := range toneInfoRules {
				if slices.ContainsFunc(r.subkinds, func(k string) bool { return slices.Contains(shown, k) }) {
					rules = append(rules, r)
				}
			}
			if len(rules) == 0 {
				rules = toneInfoRules[:1]
			}
			reference = fmt.Sprintf("文書の種類 %s では%sのが正しい直し方のことが多いので、参考として出しています", s, toneReference(rules))
		} else {
			var rules []toneRule
			for _, r := range toneTypedRules {
				if slices.Contains(shown, r.subkinds[0]) {
					rules = append(rules, r)
				}
			}
			if len(rules) == 0 {
				rules = toneTypedRules
			}
			reference = fmt.Sprintf("文書の種類 %s では%sのが決まりなので、参考として出しています", pyStr(docType), toneReference(rules))
		}
	}
	var lines []string
	switch {
	case reviews == 0:
		lines = []string{fmt.Sprintf("■ %s（参考 %d件。%s）", title, infos, reference)}
	case infos > 0:
		lines = []string{fmt.Sprintf("■ %s（見直し候補 %d件、参考 %d件。%s）", title, reviews, infos, review)}
	default:
		lines = []string{fmt.Sprintf("■ %s（見直し候補 %d件。%s）", title, reviews, review)}
	}
	lines = append(lines, notes...)
	for _, cv := range candidates {
		c := asObj(cv)
		prefix := "- "
		if fieldStr(c, "level") == "info" {
			prefix += "（参考）"
		}
		lines = append(lines, prefix+render(c))
		if key == "stiffness" {
			lines = append(lines, itemLines(c, "old", "new")...)
		} else {
			lines = append(lines, toneMorePlaces(c)...)
		}
	}
	return strings.Join(lines, "\n")
}

// detectorReviewCount は detector_review_count (文体の硬化の検査と勢いの検査の見直し候補 level review の数。参考は数えない)。
func detectorReviewCount(d *pyjson.Object) int {
	count := 0
	for _, key := range []string{"stiffness", "tone"} {
		for _, c := range fieldList(fieldObj(d, key), "candidates") {
			if fieldStr(asObj(c), "level") == "review" {
				count++
			}
		}
	}
	return count
}

func polarity(item *pyjson.Object) (source, target string) {
	if fieldStr(item, "subkind") == "to_polite" {
		return "常体", "敬体"
	}
	return "敬体", "常体"
}

// newReviewText は慎重表現・動かしてはいけない値・敬語・文体の切り替えの候補 1 件の文。
func newReviewText(key string, item *pyjson.Object, where string) string {
	detail := fieldObj(item, "detail")
	switch key {
	case "hedges":
		return fmt.Sprintf("慎重な言い方が減りました%s: %s → %s（%s）。断定に変わっていないか確かめてください。", where,
			pyStr(field(detail, "old_count")), pyStr(field(detail, "new_count")), joinList(field(detail, "removed")))
	case "immutable":
		label := "メールアドレス"
		if fieldStr(item, "subkind") == "addressee" {
			label = "宛名"
		}
		return fmt.Sprintf("%sが変わりました%s: %s → %s", label, where, cmp.Or(joinList(field(detail, "removed")), "なし"), cmp.Or(joinList(field(detail, "added")), "なし"))
	case "register":
		source, target := polarity(item)
		oldCounts, newCounts := fieldObj(detail, "old_counts"), fieldObj(detail, "new_counts")
		var text string
		if fieldStr(detail, "pattern") == "unified" {
			text = fmt.Sprintf("原文で揺れていた文末を%sにそろえました（%s%d文 → %s。原文: 常体%d文・敬体%d文）。"+
				"多いほうにそろえたなら残してよいですが、意図した変更か確かめてください。",
				target, source, fieldInt(detail, "switched"), target, fieldInt(oldCounts, "plain"), fieldInt(oldCounts, "polite"))
		} else {
			from := "polite"
			if fieldStr(item, "subkind") == "to_polite" {
				from = "plain"
			}
			text = fmt.Sprintf("%sの文を%sに変えました（%d文中%d文。原文: 常体%d文・敬体%d文 → 推敲後: 常体%d文・敬体%d文）。"+
				"原文の文体を保つのが基本です。意図した切り替えでなければ、原文の文末に戻してください。",
				source, target, fieldInt(oldCounts, from), fieldInt(detail, "switched"),
				fieldInt(oldCounts, "plain"), fieldInt(oldCounts, "polite"), fieldInt(newCounts, "plain"), fieldInt(newCounts, "polite"))
		}
		return text + "読み手に合わせて意図して変えたなら残し、「変えたところ」に書いてください。"
	}
	label := "です・ますの割合"
	if fieldStr(item, "subkind") == "honorific_rate" {
		label = "1文あたりの敬語の数"
	}
	return fmt.Sprintf("%sが変わりました: %.1f%% → %.1f%%。相手への敬語の度合いを確かめてください。", label,
		asFloat(field(detail, "old_ratio"))*100, asFloat(field(detail, "new_ratio"))*100)
}

// newReviewSection は _new_review_section。
func newReviewSection(d *pyjson.Object, key string) string {
	section := fieldObj(d, key)
	candidates := fieldList(section, "candidates")
	var notes []string
	if problem := field(section, "list_problem"); truthy(problem) {
		notes = append(notes, listNote("慎重な言い方の一覧", "hedges", pyStr(problem)))
	}
	if len(candidates) == 0 && len(notes) == 0 {
		return ""
	}
	lines := append([]string{fmt.Sprintf("■ %s（見直し候補 %d件）", newSectionNames[key], len(candidates))}, notes...)
	for _, c := range candidates {
		item := asObj(c)
		lines = append(lines, "- "+newReviewText(key, item, whereOf(field(item, "old_line"), field(item, "new_line"))))
		lines = append(lines, itemLines(item, "old", "new")...)
		if lineList := fieldList(fieldObj(item, "detail"), "lines"); key == "register" && len(lineList) > 1 {
			parts := make([]string, 0, len(lineList)-1)
			for _, pair := range lineList[1:] {
				p, _ := pair.([]pyjson.Value)
				parts = append(parts, fmt.Sprintf("元%s行目 → 後%s行目", pyStr(p[0]), pyStr(p[1])))
			}
			lines = append(lines, "  ほか: "+strings.Join(parts, "、"))
		}
	}
	return strings.Join(lines, "\n")
}
