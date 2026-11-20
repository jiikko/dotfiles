package diff

import (
	"fmt"
	"slices"
	"strings"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyops"
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
		info, _ := d.Field(l.jsonKey).(*pyjson.Object)
		if problem := info.Field("list_problem"); pyops.Truthy(problem) {
			notes = append(notes, listNote(l.label, l.name, pyStr(problem)))
		}
	}
	return notes
}

// lineWhere は _line_where (括弧のない行の位置)。
func lineWhere(oldLine, newLine pyjson.Value) string {
	switch {
	case oldLine != nil && newLine != nil:
		if pyops.Equal(oldLine, newLine) {
			return pyStr(newLine) + "行目"
		}
		return "元" + pyStr(oldLine) + "行目 → 後" + pyStr(newLine) + "行目"
	case newLine != nil:
		return "後" + pyStr(newLine) + "行目"
	case oldLine != nil:
		return "元" + pyStr(oldLine) + "行目"
	}
	return ""
}

// stiffnessLine は _stiffness_line (先頭の「- 」と「（参考）」は付けない)。
func stiffnessLine(c pyjson.Value) string {
	subkind, detail := idx(c, "subkind"), getDict(c, "detail")
	tail := "硬い言い方に寄っていないか確かめてください。"
	f0 := func(key string) string { return pyops.FormatFixed(idx(detail, key), 0) }
	is := func(s string) bool { return pyops.Equal(subkind, s) }
	switch {
	case is("sentence"):
		return fmt.Sprintf("%sの文で、漢字の割合が%s%%から%s%%に増えました（%s字→%s字）。%s", lineWhere(idx(c, "old_line"), idx(c, "new_line")),
			f0("old_kanji_ratio"), f0("new_kanji_ratio"), pyStr(idx(detail, "old_length")), pyStr(idx(detail, "new_length")), tail)
	case is("sentence_nominal"):
		return fmt.Sprintf("%sの文に、名詞化の言い回し（%s）が出て、漢字が%s字増えました。%s", lineWhere(idx(c, "old_line"), idx(c, "new_line")),
			joinList(idx(detail, "added_names")), pyStr(pyops.Sub(idx(detail, "new_kanji"), idx(detail, "old_kanji"))), tail)
	case is("document"):
		// 平均の文の長さは、表示する値 (整数に丸めた値) で増えたかを決める (17.5 字 → 18.5 字は、どちらも 18 字と表示するので「増えました」と書かない)
		oldAvg, newAvg := f0("old_avg_length"), f0("new_avg_length")
		var text string
		switch {
		case pyops.Int(newAvg).Cmp(pyops.Int(oldAvg)) > 0:
			text = fmt.Sprintf("推敲後、漢字の割合が%s%%から%s%%に、平均の文の長さが%s字から%s字に増えました。", f0("old_kanji_ratio"), f0("new_kanji_ratio"), oldAvg, newAvg)
		case newAvg == oldAvg:
			text = fmt.Sprintf("推敲後、漢字の割合が%s%%から%s%%に増えました（平均の文の長さは%s字のまま）。", f0("old_kanji_ratio"), f0("new_kanji_ratio"), oldAvg)
		default:
			text = fmt.Sprintf("推敲後、漢字の割合が%s%%から%s%%に増えました（平均の文の長さは%s字から%s字）。", f0("old_kanji_ratio"), f0("new_kanji_ratio"), oldAvg, newAvg)
		}
		if example := get(detail, "example"); pyops.Truthy(example) {
			return text + fmt.Sprintf("硬い文体に寄っていないか確かめてください（例：%s『%s』→『%s』）。", lineWhere(idx(example, "old_line"), idx(example, "new_line")),
				pyStr(idx(example, "old")), pyStr(idx(example, "new")))
		}
		return text + "硬い文体に寄っていないか確かめてください。"
	case is("nominalization"):
		var lines []string
		for _, line := range pyops.Iter(pyops.Slice(idx(detail, "new_lines"), 0, 3)) {
			lines = append(lines, pyStr(line))
		}
		shown := strings.Join(lines, "、")
		if shown == "" {
			shown = "?"
		}
		return fmt.Sprintf("名詞化の言い回し（%s）が%s個から%s個に増えました（後%s行目など）。%s", joinList(idx(detail, "added_names")),
			pyStr(idx(detail, "old")), pyStr(idx(detail, "new")), shown, tail)
	case is("no_chain"):
		return fmt.Sprintf("後%s行目に「の」が続く形（『%s』）が出ました。原文にない形です。%s", pyStr(idx(c, "new_line")), pyStr(pyops.At(idx(detail, "chains"), 0)), tail)
	}
	// 本家は subkind をそのまま返し、呼び出し元が "- " + … でつなぐ (str でなければ TypeError)
	return pyops.Str(subkind)
}

// reportTonePlace は _tone_place (勢いの候補の位置 1 つ)。
func reportTonePlace(p pyjson.Value) string {
	if idx(p, "new") == nil {
		return lineWhere(idx(p, "old_line"), nil) + "『" + pyStr(idx(p, "old")) + "』を削除"
	}
	return lineWhere(idx(p, "old_line"), idx(p, "new_line")) + "『" + pyStr(idx(p, "old")) + "』→『" + pyStr(idx(p, "new")) + "』"
}

// toneMorePlaces は _tone_more_places (2 か所目からの行。tonePlacesShown か所まで。超えた分は「ほか N か所」)。
func toneMorePlaces(c pyjson.Value) []string {
	places := getList(getDict(c, "detail"), "places")
	var rows []string
	for _, p := range pyops.Iter(pyops.Slice(places, 1, tonePlacesShown)) {
		rows = append(rows, "  "+reportTonePlace(p))
	}
	if n := pyops.Len(places); n > tonePlacesShown {
		rows = append(rows, fmt.Sprintf("  ほか%dか所", n-tonePlacesShown))
	}
	return rows
}

func reportToneLine(c pyjson.Value) string {
	subkind, detail := idx(c, "subkind"), getDict(c, "detail")
	places := getList(detail, "places")
	shown := ""
	if pyops.Truthy(places) {
		shown = "（" + reportTonePlace(pyops.At(places, 0)) + "）"
	}
	tail := "書き手の勢いを落としていないか確かめてください。"
	if pyops.Equal(subkind, "heading") {
		ratio := pyops.Div(pyops.Mul(100.0, idx(detail, "new")), idx(detail, "old"))
		return fmt.Sprintf("見出しの字数の合計が%s字から%s字（原文の%s%%）になりました%s。%s", pyStr(idx(detail, "old")), pyStr(idx(detail, "new")),
			pyops.FormatFixed(ratio, 0), shown, tail)
	}
	var label string
	switch {
	case pyops.Equal(subkind, "intensifier") && pyops.Truthy(get(detail, "words")):
		label = "強める語（" + joinList(getList(detail, "words")) + "）"
	case pyops.Equal(subkind, "intensifier"):
		label = "強める語"
	default:
		label = pyStr(pyops.Lookup(toneLabels, subkind, subkind))
	}
	return fmt.Sprintf("原文の%s%s個が、推敲後は%s個になりました%s。%s", label, pyStr(idx(detail, "old")), pyStr(idx(detail, "new")), shown, tail)
}

// toneReference は参考の理由の文に書く、参考にした候補ごとの直し方 (rules の最後だけ言い切りの形)。
func toneReference(rules []toneRule) string {
	parts := make([]string, 0, len(rules))
	for _, r := range rules[:len(rules)-1] {
		parts = append(parts, r.middle)
	}
	return strings.Join(append(parts, rules[len(rules)-1].last), "、")
}

// infoSubkinds は本家の set(candidate.get("subkind") for candidate in candidates if candidate.get("level") == "info") の str の要素
// (str でない要素は規則の名前と等しくならない。list と dict は set に入らず TypeError)。
func infoSubkinds(candidates pyjson.Value) []string {
	var shown []string
	for _, c := range pyops.Iter(candidates) {
		if pyops.Equal(get(c, "level"), "info") {
			subkind := get(c, "subkind")
			pyops.Hashable(subkind)
			if s, ok := subkind.(string); ok {
				shown = append(shown, s)
			}
		}
	}
	return shown
}

// detectorSection は _detector_section (「■ 文体の硬化」と「■ 勢い」の節)。key は stiffness か tone。
func detectorSection(d *pyjson.Object, key string) string {
	candidates := getList(getDict(d, key), "candidates")
	notes := listNotes(d, key)
	title := "勢い"
	if key == "stiffness" {
		title = "文体の硬化"
	}
	if !pyops.Truthy(candidates) {
		if len(notes) == 0 {
			return ""
		}
		return strings.Join(append([]string{"■ " + title + "（一覧の注意）"}, notes...), "\n")
	}
	infos := 0
	for _, c := range pyops.Iter(candidates) {
		if pyops.Equal(get(c, "level"), "info") {
			infos++
		}
	}
	reviews := pyops.Len(candidates) - infos
	docType := get(getDict(d, "context"), "doc_type")
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
	for _, c := range pyops.Iter(candidates) {
		prefix := "- "
		if pyops.Equal(get(c, "level"), "info") {
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

// detectorReviewCount は detector_review_count (文体の硬化の検査と勢いの検査の見直し候補 level review の数。参考は数えない)。本家は dict と list の
// 形を確かめてから数えるので、例外にならない。
func detectorReviewCount(d *pyjson.Object) int {
	count := 0
	for _, key := range []string{"stiffness", "tone"} {
		section, _ := d.Field(key).(*pyjson.Object)
		candidates, _ := section.Field("candidates").([]pyjson.Value)
		for _, c := range candidates {
			if o, ok := c.(*pyjson.Object); ok && pyops.Equal(o.Field("level"), "review") {
				count++
			}
		}
	}
	return count
}

// newReviewText は慎重表現・動かしてはいけない値・敬語・文体の切り替えの候補 1 件の文。
func newReviewText(key string, item, detail pyjson.Value, where string) string {
	switch key {
	case "hedges":
		return fmt.Sprintf("慎重な言い方が減りました%s: %s → %s（%s）。断定に変わっていないか確かめてください。", where,
			pyStr(idx(detail, "old_count")), pyStr(idx(detail, "new_count")), joinList(idx(detail, "removed")))
	case "immutable":
		label := "メールアドレス"
		if pyops.Equal(idx(item, "subkind"), "addressee") {
			label = "宛名"
		}
		return fmt.Sprintf("%sが変わりました%s: %s → %s", label, where, joinOr(idx(detail, "removed"), "なし"), joinOr(idx(detail, "added"), "なし"))
	case "register":
		toPolite := pyops.Equal(idx(item, "subkind"), "to_polite")
		source, target, from := "敬体", "常体", "polite"
		if toPolite {
			source, target, from = "常体", "敬体", "plain"
		}
		oldCounts, newCounts := idx(detail, "old_counts"), idx(detail, "new_counts")
		var text string
		if pyops.Equal(idx(detail, "pattern"), "unified") {
			text = fmt.Sprintf("原文で揺れていた文末を%sにそろえました（%s%s文 → %s。原文: 常体%s文・敬体%s文）。"+
				"多いほうにそろえたなら残してよいですが、意図した変更か確かめてください。",
				target, source, pyStr(idx(detail, "switched")), target, pyStr(idx(oldCounts, "plain")), pyStr(idx(oldCounts, "polite")))
		} else {
			text = fmt.Sprintf("%sの文を%sに変えました（%s文中%s文。原文: 常体%s文・敬体%s文 → 推敲後: 常体%s文・敬体%s文）。"+
				"原文の文体を保つのが基本です。意図した切り替えでなければ、原文の文末に戻してください。",
				source, target, pyStr(idx(oldCounts, from)), pyStr(idx(detail, "switched")),
				pyStr(idx(oldCounts, "plain")), pyStr(idx(oldCounts, "polite")), pyStr(idx(newCounts, "plain")), pyStr(idx(newCounts, "polite")))
		}
		return text + "読み手に合わせて意図して変えたなら残し、「変えたところ」に書いてください。"
	}
	label := "です・ますの割合"
	if pyops.Equal(idx(item, "subkind"), "honorific_rate") {
		label = "1文あたりの敬語の数"
	}
	return fmt.Sprintf("%sが変わりました: %s → %s。相手への敬語の度合いを確かめてください。", label,
		pyops.FormatPercent(idx(detail, "old_ratio"), 1), pyops.FormatPercent(idx(detail, "new_ratio"), 1))
}

// newReviewSection は _new_review_section。
func newReviewSection(d *pyjson.Object, key string) string {
	section := getDict(d, key)
	candidates := getList(section, "candidates")
	var notes []string
	if pyops.Truthy(get(section, "list_problem")) {
		notes = append(notes, listNote("慎重な言い方の一覧", "hedges", pyStr(idx(section, "list_problem"))))
	}
	if !pyops.Truthy(candidates) && len(notes) == 0 {
		return ""
	}
	lines := append([]string{fmt.Sprintf("■ %s（見直し候補 %d件）", newSectionNames[key], pyops.Len(candidates))}, notes...)
	for _, item := range pyops.Iter(candidates) {
		detail := idx(item, "detail")
		lines = append(lines, "- "+newReviewText(key, item, detail, whereOf(idx(item, "old_line"), idx(item, "new_line"))))
		for _, f := range [][2]string{{"old", "元"}, {"new", "後"}} {
			if get(item, f[0]) != nil {
				lines = append(lines, "  "+f[1]+": "+pyStr(idx(item, f[0])))
			}
		}
		if key == "register" && pyops.Len(idx(detail, "lines")) > 1 {
			var parts []string
			for _, pair := range pyops.Iter(pyops.Slice(idx(detail, "lines"), 1, -1)) {
				a, b := pyops.Unpack2(pair)
				parts = append(parts, "元"+pyStr(a)+"行目 → 後"+pyStr(b)+"行目")
			}
			lines = append(lines, "  ほか: "+strings.Join(parts, "、"))
		}
	}
	return strings.Join(lines, "\n")
}
