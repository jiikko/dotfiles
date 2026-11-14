package diff

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyunicode"
)

// 群 AD: 「意味が変わる恐れ」の節 (本家の _risk_section・_risk_line・_modality_side・_where)。

var certaintyLevels = map[int]string{1: "可能性", 2: "見込み・推量", 3: "予定", 4: "断定", 5: "確実"}

var qualifierLabels = map[string]string{"approx": "概数", "lower_in": "その数を含む下限", "lower_ex": "その数を含まない下限",
	"upper_in": "その数を含む上限", "upper_ex": "その数を含まない上限", "bound_verb": "動詞で言う範囲"}

var protectedLabels = map[string]string{"code": "コード", "inline_code": "インラインのコード", "url": "URL", "link": "リンク先",
	"frontmatter": "先頭の設定", "html": "HTML", "quote": "引用"}

// whereOf は _where (行の位置を括弧で。どちらも無ければ空)。old と new は行番号か nil。
func whereOf(oldLine, newLine pyjson.Value) string {
	switch {
	case oldLine != nil && newLine != nil:
		if asInt(oldLine) == asInt(newLine) {
			return fmt.Sprintf("（%d行目）", asInt(oldLine))
		}
		return fmt.Sprintf("（元%d行目 → 後%d行目）", asInt(oldLine), asInt(newLine))
	case oldLine != nil:
		return fmt.Sprintf("（元%d行目）", asInt(oldLine))
	case newLine != nil:
		return fmt.Sprintf("（後%d行目）", asInt(newLine))
	}
	return ""
}

// modalitySide は _modality_side (許可・能力の候補の行の片側)。items はその側の印 (from は lost、to は added)。
func modalitySide(names string, items []pyjson.Value) string {
	if names == "断定" {
		return "なし（断定）"
	}
	var parts []string
	for _, name := range strings.Split(names, "・") {
		members := []string{name}
		for _, n := range modalityNames {
			if n.name == name {
				members = n.members
			}
		}
		var hits []string
		for _, item := range items {
			o := asObj(item)
			if slices.Contains(members, fieldStr(o, "kind")) {
				hits = append(hits, strList(field(o, "hits"))...)
			}
		}
		if len(hits) > 0 {
			parts = append(parts, name+"（"+strings.Join(hits, "、")+"）")
		} else {
			parts = append(parts, name)
		}
	}
	return strings.Join(parts, "、")
}

func asObj(v pyjson.Value) *pyjson.Object {
	o, _ := v.(*pyjson.Object)
	return o
}

func quoted(items pyjson.Value) string {
	list := strList(items)
	for i, s := range list {
		list[i] = "「" + s + "」"
	}
	return strings.Join(list, "、")
}

// riskLine は _risk_line (意味が変わる恐れの 1 行。先頭の「- 」は付けない)。
func riskLine(risk *pyjson.Object) string {
	kind, subkind := fieldStr(risk, "kind"), fieldStr(risk, "subkind")
	detail := fieldObj(risk, "detail")
	oldLine, newLine := field(risk, "old_line"), field(risk, "new_line")
	where := whereOf(oldLine, newLine)
	switch kind {
	case "state":
		return fmt.Sprintf("状態の語が消えた%s: %s。原文は、まだ決まっていない・分かっていない・済んでいないことを伝えています。"+
			"決まった・済んだ・約束した形に変えていないか確かめ、変えていれば原文の状態に戻してください。", where, joinList(field(detail, "words")))
	case "promise":
		label := map[string]string{"added": "書き手の約束", "guarantee": "保証", "prevention": "再発防止の誓い"}[subkind]
		advice := "依頼にない誓いなら外してください。"
		if subkind != "prevention" {
			noun := "保証"
			if subkind == "added" {
				noun = "約束"
			}
			advice = "原文と依頼にない" + noun + "なら外してください。"
		}
		return fmt.Sprintf("原文にない%s%s: %s。%s", label, whereOf(nil, newLine), quoted(field(detail, "phrases")), advice)
	case "modality", "modality_review":
		change := modalitySide(fieldStr(detail, "from"), fieldList(detail, "lost")) + " → " + modalitySide(fieldStr(detail, "to"), fieldList(detail, "added"))
		switch subkind {
		case "permission_lost":
			return fmt.Sprintf("許可の言い方が消えた%s: %s。原文は「〜してよい」と、してもよいことを伝えています。できることの説明や言い切りに"+
				"変えていないか確かめ、変えていれば原文の言い方に戻してください。", where, change)
		case "permission_added":
			return fmt.Sprintf("原文にない許可の言い方%s: %s。原文は、してもよいとは書いていません。依頼にない許可を足していないか確かめ、"+
				"足していれば原文の言い方に戻してください。", where, change)
		}
		note := ""
		if kind != "modality" {
			note = "--fail-on には数えません。"
		}
		return fmt.Sprintf("能力の言い方が消えた%s: %s。%s原文は「〜できる」と、できることを伝えています。「〜する」「必ず〜する」と"+
			"言い切っていないか確かめ、言い切っていれば原文の言い方に戻してください。", where, change, note)
	case "choice":
		change := fmt.Sprintf("%s（%s） → %s（%s）", pyStr(field(detail, "from")), joinList(field(detail, "old_hits")), pyStr(field(detail, "to")), joinList(field(detail, "new_hits")))
		if subkind == "one_to_all" {
			return fmt.Sprintf("選ぶ範囲が「どれか一つ」から「全部」に変わった%s: %s。原文は、どれか一つでよい・どれか一つに当てはまると"+
				"伝えています。全部が要る・全部に当てはまる意味に変えていないか確かめ、変えていれば原文の言い方に戻してください。", where, change)
		}
		return fmt.Sprintf("選ぶ範囲が「全部」から「どれか一つ」に変わった%s: %s。原文は、全部が要る・全部に当てはまると伝えています。"+
			"どれか一つでよい意味に変えていないか確かめ、変えていれば原文の言い方に戻してください。", where, change)
	case "causality":
		return fmt.Sprintf("関連の言い方が因果の言い方に変わった%s: %s（%s） → %s（%s）。原文は、関連や相関があると述べているだけで、"+
			"原因と結果とは書いていません。因果の主張に強めていないか確かめ、強めていれば原文の言い方に戻してください。", where,
			pyStr(field(detail, "from")), joinList(field(detail, "old_hits")), pyStr(field(detail, "to")), joinList(field(detail, "new_hits")))
	case "number_qualifier":
		return numberQualifierLine(where, subkind, detail)
	case "promise_review":
		return fmt.Sprintf("原文にない「〜します」%s: %s。--fail-on には数えません。説明や手順の文なら残し、"+
			"書き手の約束として足したのなら外してください。", whereOf(nil, newLine), quoted(field(detail, "phrases")))
	case "claim":
		reason := "原文にない原因や効き目を足しています。"
		switch fieldStr(detail, "reason") {
		case "joined":
			reason = "原文の別々の文を因果でつないでいます。"
		case "time_to_cause":
			reason = "原文の時を言う言い方（〜時に、〜際に）を、原因の言い方に変えています。"
		}
		return fmt.Sprintf("原文にない因果のつながり%s: %s。%s原文が事実を並べていただけなら、"+
			"つなぎの語を外して並べてください。", where, quoted(field(detail, "words")), reason)
	case "certainty", "certainty_review":
		return certaintyLine(kind, subkind, where, detail)
	case "quote":
		prefix := ""
		if truthy(field(detail, "dialogue")) {
			prefix = "会話の"
		}
		bracket := pyunicode.Runes(fieldStr(detail, "bracket"))
		open, closing := string(bracket[0]), string(bracket[1])
		oldQuote := open + fieldStr(detail, "old_quote") + closing
		if subkind == "lost" {
			return fmt.Sprintf("%sかぎかっこの中の言葉が推敲後にない%s: %s", prefix, whereOf(oldLine, nil), oldQuote)
		}
		return fmt.Sprintf("%sかぎかっこの中の言葉が変わった%s: %s → %s%s%s", prefix, where, oldQuote, open, fieldStr(detail, "new_quote"), closing)
	case "number":
		return numberLine(risk, subkind, where, detail)
	case "citation":
		return fmt.Sprintf("引用の書き方が変わった%s: %s → %s", where, cmp.Or(joinList(field(detail, "removed")), "なし"), cmp.Or(joinList(field(detail, "added")), "なし"))
	case "negation":
		return fmt.Sprintf("否定の有無が変わった%s: %s → %s", where, fieldStr(detail, "old"), fieldStr(detail, "new"))
	case "swap":
		pairs := fieldList(detail, "pairs")
		items := make([]string, len(pairs))
		for i, p := range pairs {
			o := asObj(p)
			items[i] = fmt.Sprintf("%s %s→%s", pyStr(field(o, "word")), pyStr(field(o, "old")), pyStr(field(o, "new")))
		}
		return fmt.Sprintf("「が」「を」などが入れ替わった%s: %s", where, strings.Join(items, "、"))
	case "contrast_reversal":
		return fmt.Sprintf("否定対比の主張が逆になった恐れ%s: 原文で否定されていた『%s』が、推敲後は否定のない文に出ています。"+
			"主張が逆になっていないか確かめてください。", where, fieldStr(detail, "word"))
	case "feeling":
		return fmt.Sprintf("元にない気持ちの語%s: %s", where, joinList(field(detail, "words")))
	case "unsupported":
		label := "資料にない名前"
		if subkind == "number" {
			label = "資料にない数値"
		}
		w := where
		if newLine != nil {
			w = whereOf(nil, newLine)
		}
		return fmt.Sprintf("%s%s: %s", label, w, fieldStr(detail, "text"))
	}
	if kind == "sentence_type" && subkind == "question" {
		if truthy(field(detail, "old_is_question")) {
			return "問いの文が問いでなくなった" + where
		}
		return "問いでない文が問いになった" + where
	}
	if kind == "protected" {
		label, ok := protectedLabels[subkind]
		if !ok {
			label = pyStr(field(risk, "subkind"))
		}
		return label + "が変わった" + where
	}
	if subkind != "" {
		kind += "/" + subkind
	}
	return kind + where
}

func numberQualifierLine(where, subkind string, detail *pyjson.Object) string {
	side := func(number pyjson.Value, names, markers pyjson.Value) string {
		var labels []string
		for _, n := range strList(names) {
			labels = append(labels, qualifierLabels[n])
		}
		label := strings.Join(labels, "・")
		if label == "" {
			label = "なし"
		}
		if !truthy(markers) {
			return fmt.Sprintf("%s（%s）", pyStr(number), label)
		}
		return fmt.Sprintf("%s（%s: %s）", pyStr(number), label, joinList(markers))
	}
	change := side(field(detail, "old_number"), field(detail, "from"), field(detail, "old_markers")) + " → " +
		side(field(detail, "new_number"), field(detail, "to"), field(detail, "new_markers"))
	advice := "原文と語の種類が違います（以上と超、以下と未満は、その数を含むかどうかが違います）。数の範囲を" +
		"変えていないか確かめ、変えていれば原文の語に戻してください。"
	switch subkind {
	case "lost":
		advice = "原文は、数に概数か範囲を表す語を付けています。その語を落として、ちょうどその数と読める書き方に変えていないか" +
			"確かめ、変えていれば原文の語を戻してください。"
	case "added":
		advice = "原文は、数に概数や範囲を表す語を付けていません。依頼にない語を足して、数の意味を変えていないか確かめ、" +
			"足していれば外してください。"
	}
	return fmt.Sprintf("数に付いた概数・範囲を表す語が変わった%s: %s。%s", where, change, advice)
}

func certaintyLine(kind, subkind, where string, detail *pyjson.Object) string {
	label := "確度が弱まった"
	switch {
	case kind == "certainty":
		label = "確度が強まった"
	case subkind == "strengthened":
		label = "確度が強まった恐れ（数えない変化）"
	}
	var steps []string
	for _, s := range fieldList(detail, "steps") {
		step := asObj(s)
		target := pyStr(field(step, "to"))
		if target == "断定" {
			target = "なし（断定）"
		}
		steps = append(steps, fmt.Sprintf("%s → %s（%s → %s）", pyStr(field(step, "from")), target,
			certaintyLevels[fieldInt(step, "from_level")], certaintyLevels[fieldInt(step, "to_level")]))
	}
	advice := "原文の確かさに戻すか確かめてください。"
	if subkind == "weakened" {
		advice = "原文にない慎重さを足していないか確かめてください。"
	}
	return fmt.Sprintf("%s%s: %s。%s", label, where, strings.Join(steps, "、"), advice)
}

func numberLine(risk *pyjson.Object, subkind, where string, detail *pyjson.Object) string {
	oldLine, newLine := field(risk, "old_line"), field(risk, "new_line")
	var text string
	switch subkind {
	case "lost":
		w := where
		if oldLine != nil {
			w = whereOf(oldLine, nil)
		}
		text = fmt.Sprintf("消えた数値%s: %s", w, joinList(field(detail, "removed")))
	case "added":
		w := where
		if newLine != nil {
			w = whereOf(nil, newLine)
		}
		text = fmt.Sprintf("元にない数値%s: %s", w, joinList(field(detail, "added")))
	case "order":
		text = fmt.Sprintf("数値の並びが変わった%s: %s → %s", where, joinList(field(detail, "old_order")), joinList(field(detail, "new_order")))
	default:
		text = fmt.Sprintf("数値が変わった%s: %s → %s", where, joinList(field(detail, "removed")), joinList(field(detail, "added")))
	}
	if fieldStr(risk, "basis") == "material" {
		text += "（資料にある値）"
	}
	return text
}

// riskSection は _risk_section (なければ空)。
func riskSection(d *pyjson.Object) string {
	risks := fieldList(d, "meaning_risks")
	if len(risks) == 0 {
		return ""
	}
	lines := []string{fmt.Sprintf("■ 意味が変わる恐れ（%d件。原文と資料に照らして確かめる）", len(risks))}
	marks := 0
	for _, r := range risks {
		risk := asObj(r)
		if fieldStr(risk, "kind") == "sentence_type" && fieldStr(risk, "subkind") == "marks" {
			marks++
			continue
		}
		lines = append(lines, "- "+riskLine(risk))
		if v := field(risk, "old"); v != nil {
			lines = append(lines, "  元: "+pyStr(v))
		}
		if v := field(risk, "new"); v != nil {
			lines = append(lines, "  後: "+pyStr(v))
		}
	}
	if marks > 0 {
		lines = append(lines, fmt.Sprintf("- 文の種類を表す語が変わった: %d件（下の「■ 文の種類を表す語が変わった文」を見る）", marks))
	}
	return strings.Join(lines, "\n")
}
