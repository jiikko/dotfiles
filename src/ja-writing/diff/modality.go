package diff

import (
	"slices"
	"strings"

	"ja-writing/internal/pyre"
	"ja-writing/internal/pyunicode"
)

// 群 H: 許可・能力の言い方。「してよい」(許可) と「できる」(能力)、「必ず〜する」(保証) の違いは仕様の違いになる。
// 文の種類の印 (群 F) とは別の表に置き、群 F と同じ範囲 (typeRanges) で、種類があるかないかを比べる (同じ種類の中の言い換えは変化にしない)。
// 能力の弱い印 (消えた強い印を吸うだけ) は、語の頭が多いので前もって絞らずに当てる (literals が空の文字列 1 つ)。

// modalityKinds は本家の MODALITY_MARKERS の順。
var modalityKinds = []typeKind{
	{name: "許可", literals: []string{"てよ", "でよ", "て良", "で良", "もよ", "も良", "いい", "構", "かま", "支え", "結構", "問題", "大丈夫", "許可"},
		pattern: pyre.NewLazy(`(?:て(?<!とて)(?<!とって)(?<!言って)(?<!いって)(?<!全て)(?<!すべて)(?<!極めて)(?<!きわめて)(?<!初めて)(?<!改めて)(?<!あえて)(?<!かつて)(?<!せめて)(?<!決して)(?<!総じて)(?<!概して)(?<!として)(?<!によって)(?<!について)(?<!くて)(?!(?:(?<=いただいて)|(?<=頂いて)|(?<=もらって))も(?:よろし|いい))|で(?<!ないで)(?<!らいで)(?:(?<=[んい]で)|(?<=[^。、！？!?\n「『（(\s]で)(?<!どうで)(?=も)))(?:も?(?:(?:よい|良い)(?![一-龥々〆ヵヶァ-ヴーA-Za-z0-9])|よろし|(?:よ|良)く(?=、)|いい(?=[。、]|です|でしょう|だろう|と|が|し|か|けど|よ|ね|の|$)|構(?:わな|いませ)|かま(?:わな|いませ)|差し?支え(?:な|あり|ござ))|も(?:問題(?:は)?(?:な|あり|ござ)|大丈夫|結構(?=です|だ|でござ|[。、！!]|$)))|(?:れ|け)ば(?:結構(?=です|だ|でござ|[。、！!]|$)|差し?支え(?:な|あり|ござ))|で結構(?=です|だ|でござ|[。、！!]|$)|形で差し?支え(?:な|あり|ござ)|許可(?:する|される|します|されます)(?=[。！!]|$)`, pyre.MULTILINE)},
	{name: "能力", literals: []string{"でき", "出来", "可能"},
		pattern: pyre.NewLazy(`(?:でき|出来)(?:る(?!だけ|限り|かぎり)|ます|ない|ません|なく(?=て|な)|ず|て(?:い|おり|お)?(?:な|ませ|らず)|(?:(?<!ができ)(?<!が出来)|(?<=ことができ)|(?<=ことが出来))(?:て(?!(?:い|おり)(?:た|ました))|(?=、)))|不?可能(?:です|だ|である|な(?!限|かぎ)|に|(?=[。、]|$|と|で))`, pyre.MULTILINE)},
	{name: "能力（弱い印）", literals: []string{""},
		pattern: pyre.NewLazy(`(?:でき|出来)(?:なかった|て(?:い|おり)(?:た|ました)|(?:(?<!ができ)(?<!が出来)|(?<=ことができ)|(?<=ことが出来))(?:た|ました))|られ(?:(?<=[一-龥][めけえせてねべれげじみきちにひびりいぎ]られ)|(?<=[見得来寝着居]られ)|(?<=せられ))(?:る|ます|ない|ません|た|ました|なかった)|(?:いただけ|頂け)(?:る|ます|ない|ません|た|ました|なかった)|得(?<=[しりきちびみいにぎじ]得)(?:る|ます|ない|ません|た|ました|なかった)|うる(?<=[しりきちびみいにぎじ]うる)|え(?:(?<=[使言払買会扱担補賄笑洗歌行]え)|(?<=[しりきちびみいにぎじ]え))(?:る|ます|ない|ません|た|ました|なかった)|け(?<=[書聞行置解動引築描歩磨裁省着]け)(?:る|ます|ない|ません|た|ました|なかった)|げ(?<=[防稼急泳脱研]げ)(?:る|ます|ない|ません|た|ました|なかった)|せ(?:(?<=[出話直消残戻外試探示移返回流隠渡通壊倒許貸押]せ)|(?<=減らせ|増やせ|動かせ|生かせ|活かせ|果たせ|満たせ|冷やせ|延ばせ|伸ばせ|飛ばせ|起こせ))(?:る|ます|ない|ません|た|ました|なかった)|て(?<=[待持保勝打放]て)(?:る|ます|ない|ません|た|ました|なかった)|べ(?<=[選運呼学飛遊結喜]べ)(?:る|ます|ない|ません|た|ました|なかった)|め(?:(?<=[読飲頼望掴組住]め)|(?<=楽しめ))(?:る|ます|ない|ません|た|ました|なかった)|れ(?<=[守作取送乗帰撮登通走語測計図]れ)(?:る|ます|ない|ません|た|ました|なかった)`, pyre.MULTILINE)},
	{name: "不要", literals: []string{"なくて", "ないで", "不要", "不必要", "必要"},
		pattern: pyre.NewLazy(`(?:なくて|ないで)(?:も?(?:(?:よい|良い)(?![一-龥々〆ヵヶァ-ヴーA-Za-z0-9])|よろし|(?:よ|良)く(?=、)|いい(?=[。、]|です|でしょう|だろう|と|が|し|か|けど|よ|ね|の|$)|構(?:わな|いませ)|かま(?:わな|いませ)|差し?支え(?:な|あり|ござ))|も(?:問題(?:は)?(?:な|あり|ござ)|大丈夫|結構(?=です|だ|でござ|[。、！!]|$)))|不必?要|必要(?:は|が|も)?(?:な(?:い|く|し)|ありません|ございません)`, pyre.MULTILINE)},
	{name: "義務", literals: []string{"なければ", "なくては", "ないといけ", "ねばなら", "必要", "べき", "ものと", "必須"},
		pattern: pyre.NewLazy(`なければ(?:ならない|なりません|いけない|いけません)|なくては(?:ならない|なりません|いけない|いけません)|ないといけ(?:ない|ません)|ねばなら(?:ない|ず)|必要(?<!不必要)(?:があ(?:る|ります)|(?:に|と)(?:なる|なります)|(?:です|だ)(?=[。！!]|$))|べき(?<!しかるべき)(?<!然るべき)(?<!驚くべき)(?<!恐るべき)(?<!来るべき)(?<!特筆すべき)(?<!注目すべき)(?!乗)|ものと(?:する|します)|必須`, pyre.MULTILINE)},
	{name: "禁止", literals: []string{"はいけ", "はなら", "禁止"},
		pattern: pyre.NewLazy(`(?:て|で)は(?:いけ|なら)(?:ない|ません)|禁止`, pyre.MULTILINE)},
	{name: "保証", literals: []string{"保証", "担保", "必ず", "確実に", "ことは"},
		pattern: pyre.NewLazy(`保証(?:する|します|される|されます|いたします|致します|(?:して|されて)(?:いる|います))(?=[。、]|$)|担保(?:する|します|される|されます)(?=[。、]|$)|必ず(?!しも)|確実に|ことは(?:ない|ありません|ございません)(?=[。]|$)`, pyre.MULTILINE)},
}

var modalityKindNames = kindNames(modalityKinds)

// abilityNegative は能力の強い印のうち、打ち消しの形 (できない、できません、できず、できていない、不可能だ)。
// 打ち消しの印だけが消えた範囲 (判別できません → 分かりません) は、能力の消えにしない。
var abilityNegative = pyre.NewLazy(`(?:でき|出来)(?:な|ませ|ず|て(?:い|おり|お)?(?:な|ませ|らず))|不可能`, 0)

// modalityNames は変化の相手の名前に使う種類と、その並び。能力の弱い印は「能力」と書く。
type modalityName struct {
	name    string
	members []string
}

var modalityNames = []modalityName{{"能力", []string{"能力", "能力（弱い印）"}}, {"不要", []string{"不要"}}, {"義務", []string{"義務"}}, {"禁止", []string{"禁止"}}, {"保証", []string{"保証"}}}

func kindNames(kinds []typeKind) []string {
	names := make([]string, len(kinds))
	for i, k := range kinds {
		names[i] = k.name
	}
	return names
}

// modalityNameOf は種類の並び (印の種類の名前) を、変化の相手の名前 (能力・不要・義務・禁止・保証の順、「・」でつなぐ) にする。
func modalityNameOf(kinds []string, fallback string) string {
	var names []string
	for _, n := range modalityNames {
		for _, member := range n.members {
			if slices.Contains(kinds, member) {
				names = append(names, n.name)
				break
			}
		}
	}
	if len(names) == 0 {
		return fallback
	}
	return strings.Join(names, "・")
}

func lostKinds(before, after kindMarks) []string {
	var out []string
	for _, e := range before {
		if !after.has(e.Kind) {
			out = append(out, e.Kind)
		}
	}
	return out
}

// ModalityChange は許可・能力の言い方が変わった範囲 1 つ。Subkind は permission_lost・permission_added・ability_lost。
type ModalityChange struct {
	Orig, Rewrite, Subkind, From, To string
	Lost, Added                      []KindHits
}

// ModalityChanges は本家の _modality_changes: 許可・能力の言い方が変わった範囲。o と r は正規化した元と後の文章。
// 範囲は、文の種類の印を比べる範囲と同じ。cache に同じ文章で作った範囲があれば使い回す。
// 「不要」の種類の変化だけの範囲は出さない。同じ範囲で依頼・決まりを表す語が消えたか増えたときは、文の種類の候補に任せて出さない。
func ModalityChanges(o, r string, cache *typeCache) []ModalityChange {
	if o == r {
		return nil
	}
	oPos, rPos := sortMarks(findMarks(modalityKinds, o)), sortMarks(findMarks(modalityKinds, r))
	hasOld := false
	for _, m := range oPos {
		hasOld = hasOld || m.kind == "許可" || m.kind == "能力"
	}
	hasNew := false
	for _, m := range rPos {
		hasNew = hasNew || m.kind == "許可"
	}
	if !hasOld && !hasNew {
		return nil
	}
	ranges, oMarks, rMarks := cache.shared(o, r)
	oKeys, rKeys := markStarts(oPos), markStarts(rPos)
	oTypeKeys, rTypeKeys := markStarts(oMarks), markStarts(rMarks)
	rRunes := pyunicode.Runes(r)
	var out []ModalityChange
	for _, item := range ranges {
		before := marksIn(oPos, item.spans, modalityKindNames, oKeys)
		after := marksIn(rPos, [][2]int{{item.a, item.b}}, modalityKindNames, rKeys)
		lost, added := lostKinds(before, after), lostKinds(after, before)
		type found struct{ subkind, from, to string }
		var founds []found
		if slices.Contains(lost, "許可") {
			founds = append(founds, found{"permission_lost", "許可", modalityNameOf(added, "断定")})
		}
		if slices.Contains(added, "許可") {
			founds = append(founds, found{"permission_added", modalityNameOf(lost, "断定"), "許可"})
		}
		// 打ち消しの能力 (できない、できません) だけが消えた範囲は出さない (言い換えた「分からない」「失敗したとき」は、できないことの意味を残していることが多い)
		if slices.Contains(lost, "能力") && !slices.Contains(added, "能力（弱い印）") && !slices.Contains(added, "許可") {
			positive := false
			for _, hit := range before.hits("能力") {
				if m, err := abilityNegative.Get().Match(pyre.NewText(hit), 0, pyre.NoEnd); err != nil {
					panic(err)
				} else if m == nil {
					positive = true
				}
			}
			if positive {
				founds = append(founds, found{"ability_lost", "能力", modalityNameOf(added, "断定")})
			}
		}
		if len(founds) == 0 {
			continue
		}
		ruleBefore := marksIn(oMarks, item.spans, typeKindNames, oTypeKeys).has("依頼・決まり")
		if ruleBefore != marksIn(rMarks, [][2]int{{item.a, item.b}}, typeKindNames, rTypeKeys).has("依頼・決まり") {
			continue
		}
		orig, rewrite := strings.Join(item.texts, ""), rangeSentences(rRunes, item.a, item.b)
		for _, f := range founds {
			out = append(out, ModalityChange{orig, rewrite, f.subkind, f.from, f.to, kindList(before, lost), kindList(after, added)})
		}
	}
	return out
}

// kindList は kinds に挙げた種類の印を (before / after の並びのまま) {kind, hits} の一覧にする。
func kindList(marks kindMarks, kinds []string) []KindHits {
	var out []KindHits
	for _, k := range kinds {
		out = append(out, KindHits{k, marks.hits(k)})
	}
	return out
}
