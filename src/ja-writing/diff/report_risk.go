package diff

import (
	"fmt"
	"slices"
	"strings"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyops"
)

// 群 AD: 「意味が変わる恐れ」の節 (本家の _risk_section・_risk_line・_modality_side・_where)。

var certaintyLevels = map[int64]string{1: "可能性", 2: "見込み・推量", 3: "予定", 4: "断定", 5: "確実"}

var qualifierLabels = map[string]string{"approx": "概数", "lower_in": "その数を含む下限", "lower_ex": "その数を含まない下限",
	"upper_in": "その数を含む上限", "upper_ex": "その数を含まない上限", "bound_verb": "動詞で言う範囲"}

var protectedLabels = map[string]string{"code": "コード", "inline_code": "インラインのコード", "url": "URL", "link": "リンク先",
	"frontmatter": "先頭の設定", "html": "HTML", "quote": "引用"}

var promiseLabels = map[string]string{"added": "書き手の約束", "guarantee": "保証", "prevention": "再発防止の誓い"}

// whereOf は _where (行の位置を括弧で。どちらも無ければ空)。old と new は行番号か nil。
func whereOf(oldLine, newLine pyjson.Value) string {
	switch {
	case oldLine != nil && newLine != nil:
		if pyops.Equal(oldLine, newLine) {
			return "（" + pyStr(oldLine) + "行目）"
		}
		return "（元" + pyStr(oldLine) + "行目 → 後" + pyStr(newLine) + "行目）"
	case oldLine != nil:
		return "（元" + pyStr(oldLine) + "行目）"
	case newLine != nil:
		return "（後" + pyStr(newLine) + "行目）"
	}
	return ""
}

// modalitySide は _modality_side (許可・能力の候補の行の片側)。items はその側の印 (from は lost、to は added)。
func modalitySide(names, items pyjson.Value) string {
	if pyops.Equal(names, "断定") {
		return "なし（断定）"
	}
	var parts []string
	for _, name := range strings.Split(pyops.Str(names), "・") {
		members := []string{name}
		for _, n := range modalityNames {
			if n.name == name {
				members = n.members
			}
		}
		var hits []pyjson.Value
		for _, item := range pyops.Iter(items) {
			if kind, ok := idx(item, "kind").(string); ok && slices.Contains(members, kind) {
				hits = append(hits, pyops.Iter(idx(item, "hits"))...)
			}
		}
		if len(hits) > 0 {
			parts = append(parts, name+"（"+joinList(hits)+"）")
		} else {
			parts = append(parts, name)
		}
	}
	return strings.Join(parts, "、")
}

// quoted は "、".join("「{}」".format(p) for p in items)。
func quoted(items pyjson.Value) string {
	list := pyops.Iter(items)
	parts := make([]string, len(list))
	for i, p := range list {
		parts[i] = "「" + pyStr(p) + "」"
	}
	return strings.Join(parts, "、")
}

// lookupLabel は本家の table[key] (str の鍵の表。当たらなければ KeyError)。
func lookupLabel(table map[string]string, key pyjson.Value) string {
	pyops.Hashable(key)
	if s, ok := key.(string); ok {
		if label, ok := table[s]; ok {
			return label
		}
	}
	pyops.Raise("KeyError", "%s", pyStr(key))
	return ""
}

// certaintyLevel は本家の _CERTAINTY_LEVELS[level] (鍵は 1 から 5 の int。True と 1.0 も 1 に当たる)。
func certaintyLevel(level pyjson.Value) string {
	pyops.Hashable(level)
	for n, label := range certaintyLevels {
		if _, ok := level.(string); !ok && pyops.Equal(level, n) {
			return label
		}
	}
	pyops.Raise("KeyError", "%s", pyStr(level))
	return ""
}

// riskLine は _risk_line (意味が変わる恐れの 1 行。先頭の「- 」は付けない)。
func riskLine(risk pyjson.Value) string {
	kind, subkind := idx(risk, "kind"), get(risk, "subkind")
	detail := getDict(risk, "detail")
	oldLine, newLine := get(risk, "old_line"), get(risk, "new_line")
	where := whereOf(oldLine, newLine)
	is := func(v pyjson.Value, s string) bool { return pyops.Equal(v, s) }
	k, _ := kind.(string)
	switch k {
	case "state":
		return fmt.Sprintf("状態の語が消えた%s: %s。原文は、まだ決まっていない・分かっていない・済んでいないことを伝えています。"+
			"決まった・済んだ・約束した形に変えていないか確かめ、変えていれば原文の状態に戻してください。", where, joinList(idx(detail, "words")))
	case "promise":
		if subkind == nil {
			pyops.Raise("AssertionError", "subkind is None")
		}
		label := lookupLabel(promiseLabels, subkind)
		advice := "依頼にない誓いなら外してください。"
		if !is(subkind, "prevention") {
			noun := "保証"
			if is(subkind, "added") {
				noun = "約束"
			}
			advice = "原文と依頼にない" + noun + "なら外してください。"
		}
		return fmt.Sprintf("原文にない%s%s: %s。%s", label, whereOf(nil, newLine), quoted(idx(detail, "phrases")), advice)
	case "modality", "modality_review":
		change := modalitySide(idx(detail, "from"), idx(detail, "lost")) + " → " + modalitySide(idx(detail, "to"), idx(detail, "added"))
		switch {
		case is(subkind, "permission_lost"):
			return fmt.Sprintf("許可の言い方が消えた%s: %s。原文は「〜してよい」と、してもよいことを伝えています。できることの説明や言い切りに"+
				"変えていないか確かめ、変えていれば原文の言い方に戻してください。", where, change)
		case is(subkind, "permission_added"):
			return fmt.Sprintf("原文にない許可の言い方%s: %s。原文は、してもよいとは書いていません。依頼にない許可を足していないか確かめ、"+
				"足していれば原文の言い方に戻してください。", where, change)
		}
		note := ""
		if k != "modality" {
			note = "--fail-on には数えません。"
		}
		return fmt.Sprintf("能力の言い方が消えた%s: %s。%s原文は「〜できる」と、できることを伝えています。「〜する」「必ず〜する」と"+
			"言い切っていないか確かめ、言い切っていれば原文の言い方に戻してください。", where, change, note)
	case "choice":
		change := fmt.Sprintf("%s（%s） → %s（%s）", pyStr(idx(detail, "from")), joinList(idx(detail, "old_hits")), pyStr(idx(detail, "to")), joinList(idx(detail, "new_hits")))
		if is(subkind, "one_to_all") {
			return fmt.Sprintf("選ぶ範囲が「どれか一つ」から「全部」に変わった%s: %s。原文は、どれか一つでよい・どれか一つに当てはまると"+
				"伝えています。全部が要る・全部に当てはまる意味に変えていないか確かめ、変えていれば原文の言い方に戻してください。", where, change)
		}
		return fmt.Sprintf("選ぶ範囲が「全部」から「どれか一つ」に変わった%s: %s。原文は、全部が要る・全部に当てはまると伝えています。"+
			"どれか一つでよい意味に変えていないか確かめ、変えていれば原文の言い方に戻してください。", where, change)
	case "causality":
		return fmt.Sprintf("関連の言い方が因果の言い方に変わった%s: %s（%s） → %s（%s）。原文は、関連や相関があると述べているだけで、"+
			"原因と結果とは書いていません。因果の主張に強めていないか確かめ、強めていれば原文の言い方に戻してください。", where,
			pyStr(idx(detail, "from")), joinList(idx(detail, "old_hits")), pyStr(idx(detail, "to")), joinList(idx(detail, "new_hits")))
	case "number_qualifier":
		return numberQualifierLine(where, subkind, detail)
	case "promise_review":
		return fmt.Sprintf("原文にない「〜します」%s: %s。--fail-on には数えません。説明や手順の文なら残し、"+
			"書き手の約束として足したのなら外してください。", whereOf(nil, newLine), quoted(idx(detail, "phrases")))
	case "claim":
		words := quoted(idx(detail, "words"))
		reason := idx(detail, "reason")
		pyops.Hashable(reason)
		text := "原文にない原因や効き目を足しています。"
		switch {
		case is(reason, "joined"):
			text = "原文の別々の文を因果でつないでいます。"
		case is(reason, "time_to_cause"):
			text = "原文の時を言う言い方（〜時に、〜際に）を、原因の言い方に変えています。"
		}
		return fmt.Sprintf("原文にない因果のつながり%s: %s。%s原文が事実を並べていただけなら、"+
			"つなぎの語を外して並べてください。", where, words, text)
	case "certainty", "certainty_review":
		return certaintyLine(k, subkind, where, detail)
	case "quote":
		prefix := ""
		if pyops.Truthy(idx(detail, "dialogue")) {
			prefix = "会話の"
		}
		bracket := idx(detail, "bracket")
		oldQuote := pyStr(pyops.Add(pyops.Add(pyops.At(bracket, 0), idx(detail, "old_quote")), pyops.At(bracket, 1)))
		if is(subkind, "lost") {
			return fmt.Sprintf("%sかぎかっこの中の言葉が推敲後にない%s: %s", prefix, whereOf(oldLine, nil), oldQuote)
		}
		return fmt.Sprintf("%sかぎかっこの中の言葉が変わった%s: %s → %s%s%s", prefix, where, oldQuote, pyStr(pyops.At(bracket, 0)),
			pyStr(idx(detail, "new_quote")), pyStr(pyops.At(bracket, 1)))
	case "number":
		return numberLine(risk, subkind, where, detail)
	case "citation":
		return fmt.Sprintf("引用の書き方が変わった%s: %s → %s", where, joinOr(pyops.GetOr(detail, "removed", pyops.EmptyList()), "なし"),
			joinOr(pyops.GetOr(detail, "added", pyops.EmptyList()), "なし"))
	case "negation":
		return fmt.Sprintf("否定の有無が変わった%s: %s → %s", where, pyStr(pyops.GetOr(detail, "old", "")), pyStr(pyops.GetOr(detail, "new", "")))
	case "swap":
		var items []string
		for _, pair := range pyops.Iter(pyops.GetOr(detail, "pairs", pyops.EmptyList())) {
			items = append(items, pyStr(idx(pair, "word"))+" "+pyStr(idx(pair, "old"))+"→"+pyStr(idx(pair, "new")))
		}
		return fmt.Sprintf("「が」「を」などが入れ替わった%s: %s", where, strings.Join(items, "、"))
	case "contrast_reversal":
		return fmt.Sprintf("否定対比の主張が逆になった恐れ%s: 原文で否定されていた『%s』が、推敲後は否定のない文に出ています。"+
			"主張が逆になっていないか確かめてください。", where, pyStr(pyops.GetOr(detail, "word", "")))
	case "feeling":
		return fmt.Sprintf("元にない気持ちの語%s: %s", where, joinList(pyops.GetOr(detail, "words", pyops.EmptyList())))
	case "unsupported":
		label := "資料にない名前"
		if is(subkind, "number") {
			label = "資料にない数値"
		}
		w := where
		if newLine != nil {
			w = whereOf(nil, newLine)
		}
		return fmt.Sprintf("%s%s: %s", label, w, pyStr(pyops.GetOr(detail, "text", "")))
	case "sentence_type":
		if !is(subkind, "question") {
			break
		}
		if pyops.Truthy(get(detail, "old_is_question")) {
			return "問いの文が問いでなくなった" + where
		}
		return "問いでない文が問いになった" + where
	case "protected":
		return pyStr(pyops.Lookup(protectedLabels, subkind, subkind)) + "が変わった" + where
	}
	var suffix pyjson.Value = ""
	if pyops.Truthy(subkind) {
		suffix = pyops.Add("/", subkind)
	}
	return pyStr(pyops.Add(kind, suffix)) + where
}

func numberQualifierLine(where string, subkind, detail pyjson.Value) string {
	side := func(number, names, markers pyjson.Value) string {
		var labels []string
		for _, n := range pyops.Iter(names) {
			labels = append(labels, lookupLabel(qualifierLabels, n))
		}
		label := strings.Join(labels, "・")
		if label == "" {
			label = "なし"
		}
		if !pyops.Truthy(markers) {
			return fmt.Sprintf("%s（%s）", pyStr(number), label)
		}
		return fmt.Sprintf("%s（%s: %s）", pyStr(number), label, joinList(markers))
	}
	change := side(idx(detail, "old_number"), idx(detail, "from"), idx(detail, "old_markers")) + " → " +
		side(idx(detail, "new_number"), idx(detail, "to"), idx(detail, "new_markers"))
	advice := "原文と語の種類が違います（以上と超、以下と未満は、その数を含むかどうかが違います）。数の範囲を" +
		"変えていないか確かめ、変えていれば原文の語に戻してください。"
	switch {
	case pyops.Equal(subkind, "lost"):
		advice = "原文は、数に概数か範囲を表す語を付けています。その語を落として、ちょうどその数と読める書き方に変えていないか" +
			"確かめ、変えていれば原文の語を戻してください。"
	case pyops.Equal(subkind, "added"):
		advice = "原文は、数に概数や範囲を表す語を付けていません。依頼にない語を足して、数の意味を変えていないか確かめ、" +
			"足していれば外してください。"
	}
	return fmt.Sprintf("数に付いた概数・範囲を表す語が変わった%s: %s。%s", where, change, advice)
}

func certaintyLine(kind string, subkind pyjson.Value, where string, detail pyjson.Value) string {
	label := "確度が弱まった"
	switch {
	case kind == "certainty":
		label = "確度が強まった"
	case pyops.Equal(subkind, "strengthened"):
		label = "確度が強まった恐れ（数えない変化）"
	}
	var steps []string
	for _, step := range pyops.Iter(idx(detail, "steps")) {
		target := idx(step, "to")
		if pyops.Equal(target, "断定") {
			target = "なし（断定）"
		}
		steps = append(steps, fmt.Sprintf("%s → %s（%s → %s）", pyStr(idx(step, "from")), pyStr(target),
			certaintyLevel(idx(step, "from_level")), certaintyLevel(idx(step, "to_level"))))
	}
	advice := "原文の確かさに戻すか確かめてください。"
	if pyops.Equal(subkind, "weakened") {
		advice = "原文にない慎重さを足していないか確かめてください。"
	}
	return fmt.Sprintf("%s%s: %s。%s", label, where, strings.Join(steps, "、"), advice)
}

func numberLine(risk, subkind pyjson.Value, where string, detail pyjson.Value) string {
	oldLine, newLine := get(risk, "old_line"), get(risk, "new_line")
	list := func(key string) string { return joinList(pyops.GetOr(detail, key, pyops.EmptyList())) }
	var text string
	switch {
	case pyops.Equal(subkind, "lost"):
		w := where
		if oldLine != nil {
			w = whereOf(oldLine, nil)
		}
		text = fmt.Sprintf("消えた数値%s: %s", w, list("removed"))
	case pyops.Equal(subkind, "added"):
		w := where
		if newLine != nil {
			w = whereOf(nil, newLine)
		}
		text = fmt.Sprintf("元にない数値%s: %s", w, list("added"))
	case pyops.Equal(subkind, "order"):
		text = fmt.Sprintf("数値の並びが変わった%s: %s → %s", where, list("old_order"), list("new_order"))
	default:
		text = fmt.Sprintf("数値が変わった%s: %s → %s", where, list("removed"), list("added"))
	}
	if pyops.Equal(get(risk, "basis"), "material") {
		text += "（資料にある値）"
	}
	return text
}

// riskSection は _risk_section (なければ空)。
func riskSection(d *pyjson.Object) string {
	risks := getList(d, "meaning_risks")
	if !pyops.Truthy(risks) {
		return ""
	}
	lines := []string{fmt.Sprintf("■ 意味が変わる恐れ（%d件。原文と資料に照らして確かめる）", pyops.Len(risks))}
	marks := 0
	for _, risk := range pyops.Iter(risks) {
		kind, subkind := idx(risk, "kind"), get(risk, "subkind")
		if pyops.Equal(kind, "sentence_type") && pyops.Equal(subkind, "marks") {
			marks++
			continue
		}
		lines = append(lines, "- "+riskLine(risk))
		lines = append(lines, itemLines(risk, "old", "new")...)
	}
	if marks > 0 {
		lines = append(lines, fmt.Sprintf("- 文の種類を表す語が変わった: %d件（下の「■ 文の種類を表す語が変わった文」を見る）", marks))
	}
	return strings.Join(lines, "\n")
}

// itemLines は候補 1 件の「元: 」「後: 」の行 (本家の if item.get(key) is not None: lines.append("  元: " + item[key]))。
func itemLines(item pyjson.Value, oldKey, newKey string) []string {
	var lines []string
	if get(item, oldKey) != nil {
		lines = append(lines, concat("  元: ", idx(item, oldKey)))
	}
	if get(item, newKey) != nil {
		lines = append(lines, concat("  後: ", idx(item, newKey)))
	}
	return lines
}
