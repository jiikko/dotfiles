package diff

import (
	"slices"
	"strings"

	"ja-writing/internal/difflib"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
)

// 群 C: 文末の種類と、文書の立場。本家の bare_end・register・ending_kind・ending_units・stance_flags・ending_changes と、その下の _ending_changes_from_units。

// 状態や性質を表す「〜ます」の語幹 (動作ではないもの) と、可能の形。一段動詞と見分けられないので、よく出るものだけ並べる。
var (
	stative = []string{"なり", "あり", "おり", "でき", "分かり", "わかり", "つながり", "変わり", "起き", "起こり", "生じ",
		"増え", "減り", "見え", "聞こえ", "残り", "続き", "終わり", "始まり", "決まり", "進み", "遅れ",
		"下回り", "上回り", "異なり", "違い", "限り", "足り", "合い", "当たり", "向き", "似", "伝わり",
		"高まり", "下がり", "上がり", "広がり", "強まり", "弱まり", "落ち", "壊れ", "崩れ", "外れ", "漏れ",
		"そろい", "揃い", "思え", "感じられ", "止まり", "消え", "困り", "迷い", "助かり"}
	potential = []string{"防げ", "書け", "読め", "使え", "言え", "選べ", "話せ", "待て", "探せ", "直せ", "残せ", "減らせ", "増やせ",
		"守れ", "作れ", "取れ", "気づけ", "見つけられ", "続けられ", "避けられ", "変えられ", "決められ", "伝えられ"}
)

// 文末の種類と敬体・常体の正規表現。
var (
	bareMarks    = pyre.NewLazy(`\*\*|`+"`", 0)
	keitaiEnd    = pyre.NewLazy(`(?:です|ます|ません|ました|でした|ましょう|ください|でしょう)$`, 0)
	jotaiEnd     = pyre.NewLazy(`(?:だ|である|ではない|でない)$`, 0)
	plainVerbEnd = pyre.NewLazy(`[るうくすつぬぶむぐたい]$`, 0)
	nounishEnd   = pyre.NewLazy(`[ァ-ヴー一-龥A-Za-z0-9]$`, 0)
	requestEnd   = pyre.NewLazy(`ください(?:ね)?$|(?:て|で)はいけません$|(?:て|で)はなりません$|ていただきます$|ていただけます$`, 0)
	suggestEnd   = pyre.NewLazy(`ましょう$|とよいです$|といいです$|をおすすめします$|をお勧めします$`, 0)
	guessEnd     = pyre.NewLazy(`(?:でしょう|だろう|かもしれません|かもしれない|と思います|と考えます|と感じます|はずです|ようです|気がします)$`, 0)
	dutyEnd      = pyre.NewLazy(`(?:なければなりません|なくてはなりません|必要があります|べきです)$`, 0)
	evalEnd      = pyre.NewLazy(`(?:重要|最重要|大切|大事|不可欠|肝心|肝要|欠かせません|鍵|カギ|最優先|必要)(?:です|でした|だ|である)?$`, 0)
	pastEnd      = pyre.NewLazy(`(?:ました|でした|ませんでした)$`, 0)
	masuEnd      = pyre.NewLazy(`(?:ます|ません)$`, 0)
	teiruStem    = pyre.NewLazy(`(?:て|で)い$`, 0)
	passiveStem  = pyre.NewLazy(`(?:られ|[^しさ]れ)$`, 0)
	desuEnd      = pyre.NewLazy(`です$`, 0)
	unitListHead = pyre.NewLazy(`^(?:[*\-・]|\d+[.)])\s+`, 0)
	unitSplit    = pyre.NewLazy(`(?<=[。！？!?])`, 0)
	unitFullEnd  = pyre.NewLazy(`[。！？!?]$`, 0)
	unitDash     = pyre.NewLazy("[—―]{1,2}", 0)
)

// BareEnd は本家の bare_end: 文末の判定に使う形。太字や記号、文末のかっこ書き (「〜」の例など) を外す。
func BareEnd(s string) string {
	t := pystr.RStripChars(pystr.Strip(bareMarks.MustSub("", s)), "。．.！!？?")
	t = pystr.Strip(t)
	rs := pyunicode.Runes(t)
	end := len(rs)
	for end > 0 && (rs[end-1] == '）' || rs[end-1] == ')') {
		start := end - 2
		for start >= 0 && !strings.ContainsRune("（）()", rs[start]) {
			start--
		}
		if start < 0 || !strings.ContainsRune("（(", rs[start]) {
			break
		}
		end = start
		for end > 0 && pyunicode.IsSpace(rs[end-1]) {
			end--
		}
	}
	return pystr.RStripChars(pyunicode.FromRunes(rs[:end]), "」』）)")
}

// Register は本家の register: 敬体か常体か。体言止めなどは空を返す。
func Register(s string) string {
	t := BareEnd(s)
	if keitaiEnd.MustSearch(t) {
		return "敬体"
	}
	if jotaiEnd.MustSearch(t) || (plainVerbEnd.MustSearch(t) && !nounishEnd.MustSearch(t)) {
		return "常体"
	}
	return ""
}

// EndingKind は本家の ending_kind: 文末の種類。空の文は空を返す。
func EndingKind(s string) string {
	t := BareEnd(s)
	if t == "" {
		return ""
	}
	switch {
	case requestEnd.MustSearch(t):
		return "依頼"
	case suggestEnd.MustSearch(t):
		return "勧め"
	case guessEnd.MustSearch(t):
		return "推量・考え"
	case dutyEnd.MustSearch(t):
		return "義務"
	case evalEnd.MustSearch(t):
		return "評価"
	case pastEnd.MustSearch(t):
		return "過去"
	case masuEnd.MustSearch(t):
		stem := masuEnd.MustSub("", t)
		switch {
		case teiruStem.MustSearch(stem):
			return "説明（〜ています）"
		case passiveStem.MustSearch(stem) || pystr.EndsWithAny(stem, stative) || pystr.EndsWithAny(stem, potential):
			return "説明（〜ます）"
		}
		return "動作（〜します）"
	case desuEnd.MustSearch(t):
		return "断定（〜です）"
	case jotaiEnd.MustSearch(t):
		return "常体"
	case plainVerbEnd.MustSearch(t) && !nounishEnd.MustSearch(t):
		return "常体"
	}
	return "体言止めなど"
}

// EndingUnit は ending_units の 1 件 ({"where", "sentence", "kind", "full"})。Where は 地の文・箇条書き・表。
type EndingUnit struct {
	Where    string
	Sentence string
	Kind     string
	Full     bool
}

// EndingUnits は本家の ending_units: 文末を見る単位。箇条書きの 1 項目も 1 文。ダッシュの前も 1 つの区切りとして見る。
// markdown が真のときは、コードブロック・先頭の設定部分・表の区切り行を飛ばし、表のセルは「表」として扱う。
func EndingUnits(t string, markdown bool) []EndingUnit {
	var out []EndingUnit
	lines := strings.Split(t, "\n")
	if markdown && len(lines) > 0 && pystr.Strip(lines[0]) == "---" {
		if end := slices.Index(lines[1:], "---"); end >= 0 {
			lines = lines[end+2:]
		}
	}
	inCode := false
	for _, line := range lines {
		l := pystr.Strip(line)
		if markdown && strings.HasPrefix(l, "```") {
			inCode = !inCode
			continue
		}
		if inCode || l == "" || strings.HasPrefix(l, "#") || l == "---" {
			continue
		}
		var where string
		var sources []string
		switch {
		case strings.HasPrefix(l, "|"):
			if !markdown || onlyTableChars(l) {
				continue
			}
			where = "表"
			for _, cell := range strings.Split(pystr.StripChars(l, "|"), "|") {
				sources = append(sources, pystr.Strip(cell))
			}
		case unitListHead.MustMatchAt0(l):
			where, sources = "箇条書き", []string{unitListHead.MustSub("", l)}
		default:
			where, sources = "地の文", []string{l}
		}
		for _, src := range sources {
			for _, s := range unitSplit.MustSplit(src) {
				s = pystr.Strip(s)
				if s == "" {
					continue
				}
				full := unitFullEnd.MustSearch(s)
				parts := unitDash.MustSplit(s)
				for n, p := range parts {
					p = pystr.Strip(p)
					if p == "" {
						continue
					}
					k := EndingKind(p)
					last := n == len(parts)-1
					if k == "" || (!last && k == "体言止めなど") {
						continue
					}
					out = append(out, EndingUnit{Where: where, Sentence: p, Kind: k, Full: full || !last})
				}
			}
		}
	}
	return out
}

// onlyTableChars は set(l) <= set("|-: ") (表の区切り行)。
func onlyTableChars(l string) bool {
	return !strings.ContainsFunc(l, func(r rune) bool { return !strings.ContainsRune("|-: ", r) })
}

// StanceFlag は stance_flags の 1 件 ((note, rows))。
type StanceFlag struct {
	Note string
	Rows []*EndingUnit
}

// StanceFlags は本家の stance_flags: 文末の単位と、立場に合わない文末の見直しの知らせ。stance は 勧め・決まり・説明・空 (指定なし)。
// markdown が真のときは ending_units の markdown と同じ (--endings だけ)。
func StanceFlags(t, stance string, markdown bool) ([]EndingUnit, []StanceFlag) {
	rows := EndingUnits(t, markdown)
	var act, ask, rec, ev []*EndingUnit
	for i := range rows {
		r := &rows[i]
		if r.Where == "表" {
			continue
		}
		switch r.Kind {
		case "動作（〜します）":
			act = append(act, r)
		case "勧め", "依頼":
			ask = append(ask, r)
			if r.Kind == "勧め" {
				rec = append(rec, r)
			}
		case "評価":
			ev = append(ev, r)
		}
	}
	var flags []StanceFlag
	switch stance {
	case "勧め":
		if len(act) > 0 {
			flags = append(flags, StanceFlag{"勧めの文書に、動作を表す「〜します」がある。書き手の予定や手順・道具の説明か、読み手への依頼かを原文と文脈から確認する", act})
		}
	case "決まり":
		if len(rec) > 0 || len(ev) > 0 {
			flags = append(flags, StanceFlag{"決まり・手順の文書に、勧めや評価の文末がある。決まりそのものなら決まりの形（〜します）にする。決まりの理由や前提を述べる文なら残す。「〜してください」はそのままでよい", slices.Concat(rec, ev)})
		}
	case "説明":
		// r is not rows[-1]: 最後の行そのものだけを除く (中身が同じ別の行は除かない)
		body := ask
		if len(rows) > 0 {
			body = nil
			for _, r := range ask {
				if r != &rows[len(rows)-1] {
					body = append(body, r)
				}
			}
		}
		if len(body) > 0 {
			flags = append(flags, StanceFlag{"事実・結果・考えの文書に、読み手への勧めや頼みがある（最後の1文を除く）。立場が合っているか見る", body})
		}
	default:
		if len(act) > 0 && len(ask) > 0 {
			flags = append(flags, StanceFlag{"動作の「〜します」と、勧め・依頼が同じ文章にある。「〜します」が書き手の側の予定・決まった手順なのか、読み手にしてほしい行動なのかを見る", slices.Concat(act, ask)})
		} else if len(act) > 0 && len(ev) > 0 {
			flags = append(flags, StanceFlag{"動作の「〜します」と、評価（〜が重要です など）が同じ文章にある。各文の働きと原文の強さが保たれているか確認する", slices.Concat(act, ev)})
		}
	}
	// 敬体と常体。文として書かれた単位 (。で終わるもの、ダッシュの前) だけを数える。表と、。のない箇条書きは数えない
	var jotai, keitai []*EndingUnit
	for i := range rows {
		r := &rows[i]
		if r.Where == "表" || !r.Full {
			continue
		}
		switch Register(r.Sentence) {
		case "常体":
			jotai = append(jotai, r)
		case "敬体":
			keitai = append(keitai, r)
		}
	}
	if len(jotai) > 0 && len(keitai) > len(jotai) {
		flags = append(flags, StanceFlag{"敬体の文の中に、常体の文がある。そろえるときは「する → します」と形だけで変えず、立場に合う形にする", jotai})
	} else if len(keitai) > 0 && len(jotai) > len(keitai) {
		flags = append(flags, StanceFlag{"常体の文の中に、敬体の文がある", keitai})
	}
	return rows, flags
}

// EndingChange は書き直しで文末の種類が変わった文 (本家の ending_changes の 1 件)。
type EndingChange struct {
	Orig        string
	OrigKind    string
	Rewrite     string
	RewriteKind string
}

// EndingChanges は本家の ending_changes: 書き直しで文末の種類が変わった文。似ている元の文と組にして比べる。
func EndingChanges(o, r string) []EndingChange {
	return endingChangesFromUnits(EndingUnits(o, false), EndingUnits(r, false))
}

// endingExactArea は、文末の種類を比べるとき、元の文と書き直した文の字数の積がこれを超える組の ratio を NewExact で求める境 (答えは difflib と同じ)。
// difflib の ratio は長い文の組で長さの 2 乗の時間がかかる (「。」のない 2 万字の 1 文で 20 秒あまり)。短い文の組では difflib のほうが速い。
const endingExactArea = 160000

// endingMinScore は文末を比べる組にする似かたの下限。
const endingMinScore = 0.45

// endingChangesFromUnits は本家の _ending_changes_from_units: 書き直した文ごとに、元の文のうち最も似たもの (完全一致は 1) を選び、
// 似かたが下限以上で種類が違えば変化として返す。同点では元の文の順番が先。
func endingChangesFromUnits(ou, ru []EndingUnit) []EndingChange {
	// 同じ文の最初の出現を保つ。
	type original struct {
		unit  *EndingUnit
		runes []rune
	}
	bySentence := map[string]*EndingUnit{}
	var items []original
	for i := range ou {
		y := &ou[i]
		if _, seen := bySentence[y.Sentence]; seen {
			continue
		}
		bySentence[y.Sentence] = y
		items = append(items, original{y, pyunicode.Runes(y.Sentence)})
	}
	// atLeast[ch][t] は、字 ch を t+1 回以上含む元の文の番号の一覧 (番号の順)
	var atLeast map[rune][][]int
	type result struct {
		best  *EndingUnit
		score float64
	}
	matches := map[string]result{}
	var changes []EndingChange
	for k := range ru {
		x := &ru[k]
		sentence := x.Sentence
		if _, done := matches[sentence]; !done {
			best, score := (*EndingUnit)(nil), 0.0
			if y, ok := bySentence[sentence]; ok {
				best, score = y, 1.0
			} else {
				rewrite := pyunicode.Runes(sentence)
				matcher := difflib.New([]rune(nil), rewrite)
				var exact *difflib.SequenceMatcher[rune]
				if atLeast == nil {
					atLeast = map[rune][][]int{}
					for i, it := range items {
						for ch, n := range runeCounts(it.runes) {
							levels := atLeast[ch]
							for len(levels) < n {
								levels = append(levels, nil)
							}
							for t := range n {
								levels[t] = append(levels[t], i)
							}
							atLeast[ch] = levels
						}
					}
				}
				// overlaps[i] は、字ごとの min(この文の数, 元の文 i の数) の和 (字の多重集合の重なり。ratio の上限に使う)
				overlaps := map[int]int{}
				for ch, n := range runeCounts(rewrite) {
					levels := atLeast[ch]
					for t := range min(n, len(levels)) {
						for _, i := range levels[t] {
							overlaps[i]++
						}
					}
				}
				rewriteLen := len(rewrite)
				// 共通の字が 1 つもない元の文は、overlap_bound が 0 で下の条件に届かないので見なくても同じ。残りは番号の順に見る
				for _, i := range sortedKeys(overlaps) {
					it := items[i]
					length := len(it.runes)
					total := length + rewriteLen
					lengthBound := 1.0
					if total != 0 {
						lengthBound = 2.0 * float64(min(length, rewriteLen)) / float64(total)
					}
					if lengthBound < endingMinScore || lengthBound <= score {
						continue
					}
					overlapBound := 1.0
					if total != 0 {
						overlapBound = 2.0 * float64(overlaps[i]) / float64(total)
					}
					if overlapBound < endingMinScore || overlapBound <= score {
						continue
					}
					var sc float64
					if length*rewriteLen > endingExactArea {
						if exact == nil {
							exact = difflib.NewExact(nil, rewrite)
						}
						exact.SetSeq1(it.runes)
						sc = exact.Ratio()
					} else {
						matcher.SetSeq1(it.runes)
						sc = matcher.Ratio()
					}
					if sc > score {
						best, score = it.unit, sc
					}
				}
			}
			matches[sentence] = result{best, score}
		}
		m := matches[sentence]
		if m.best != nil && m.score >= endingMinScore && m.best.Kind != x.Kind {
			origKind := m.best.Kind
			if m.best.Where == "箇条書き" {
				origKind += "・箇条書き"
			}
			changes = append(changes, EndingChange{m.best.Sentence, origKind, x.Sentence, x.Kind})
		}
	}
	return changes
}

func runeCounts(rs []rune) map[rune]int {
	counts := map[rune]int{}
	for _, r := range rs {
		counts[r]++
	}
	return counts
}

func sortedKeys(m map[int]int) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
