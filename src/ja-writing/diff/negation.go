package diff

import (
	"ja-writing/internal/difflib"
	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pyunicode"
	"slices"
)

// 群 N: 否定 (本家の _negation_risks・_negation_flip・_negation_marks・_changed_places・_widen・_covered・_unmarked_letters・_verb_stem_change)。
// 組になった文を文字で対応させ、変わった箇所ごとに、その箇所に重なる否定を表す語の数を元と後で比べる。数が違い、変わった文字のうち否定を表す語の外の文字が
// 両側合わせて 2 字以下なら出す (句点の前の規則にも当たれば出す)。

var (
	// 否定の印。「ず」は直後が「に」「、」「。」か文末のときだけ (必ず、はず、まず は除く。読まず のように漢字に続く まず は取る)
	negationMark = pyre.NewLazy(
		`しないで|ません|なかっ|なけれ|ない|なく|禁止`+
			`|(?<!必)(?<!は)ず(?=[に、。]|$)`+
			`|ぬ(?=[。、]|$)`+
			`|未(?![来明])(?=[一-龥])`+
			`|不(?=要|可|明|正|足|在|備|具合|適|十分|一致|安定|許可|採用|使用|参加)`+
			`|非(?=公開|対応|表示|推奨|同期|互換|同意)`+
			`|無(?=効|料|視|関係|制限|断)`, 0)
	// 数える前に伏せるもの。義務の二重否定、否定の形をした別の働き、否定対比 (「AではなくB」を外すのはよくある正しい直し)
	negationMasked = pyre.NewLazy(
		`なければ(?:ならない|なりません|ならず|いけない|いけません)|なくては(?:ならない|なりません|いけない|いけません)`+
			`|ないと(?:いけない|いけません)|ざるを得(?:ない|ません)|ねば(?:ならない|なりません)`+
			`|かもしれ(?:ない|ません)|に違い(?:ない|ありません)|とは限(?:らない|りません)|わけでは(?:ない|ありません)`+
			`|に(?:すぎ|過ぎ)(?:ない|ません)|しか(?:ない|ありません)|ほか(?:ない|ありません)|問題(?:は|が|も)?(?:ない|なく|ありません)|有無`+
			`|(?:では|じゃ|でも)な(?=[いくかけ])|(?:では|じゃ)ありません`, 0)
	// 「まず」の「ず」は、直前が漢字でなければ副詞 (「まず、」) として除く
	adverbMazu   = pyre.NewLazy(`(?<![一-龥々])まず`, 0)
	sentenceTail = pyre.NewLazy(`[。！？!?]*[」』）)]*$`, 0)
)

const (
	negationLimit    = 2 // 変わった文字のうち、否定の印でない文字の数の上限 (両側の合計)
	negationEndRule  = true
	endWindow        = 5 // 句点の前の規則の範囲 (文末の直前の字数)
	placeGap         = 1 // 変わった箇所をまとめる、間の同じ文字の数の上限
	negationWidenMax = 6 // 否定の detail を右へ広げる平仮名の字数の上限
)

// 文の途中の「する → しない」「せずに ↔ して」。語幹の仮名だけが変わる形で、negationLimit に収まらなかった
var zuToTe = [2][]string{{"さずに", "せずに", "さず", "せず"}, {"して", "し"}}

func isWordChar(r rune) bool { return isKanjiChar(r) || isKatakanaChar(r) }

// negationMarks は否定の印の範囲 [(始め, 終わり)]。伏せるものを伏せてから数える。
func negationMarks(text string) [][2]int {
	masked := mustSubFunc(negationMasked, pyre.NewText(text), repeatMask)
	masked = mustSubFunc(negationWords, pyre.NewText(masked), maskLastChar)
	masked, err := adverbMazu.Get().Sub("＃＃", pyre.NewText(masked), 0)
	if err != nil {
		panic(err)
	}
	var marks [][2]int
	for _, m := range mustFinditer(negationMark, pyre.NewText(masked), 0, pyre.NoEnd) {
		marks = append(marks, [2]int{m.Start(0), m.End(0)})
	}
	return marks
}

// widen は変わった箇所 [left, right) を語の切れ目まで広げる。左へは平仮名を 4 字まで、そのあと続く漢字・片仮名の並び全部。右へは平仮名を 6 字まで。
// 句読点と空白で止める。変わった箇所の頭が漢字か片仮名なら、左の平仮名 (助詞) は取らず、右へは続く漢字・片仮名の並びを先に取る (「有効 → 無効」を「有効です → 無効です」にする)。
func widen(text []rune, left, right int) string {
	startsWithWord := left < len(text) && isWordChar(text[left])
	if !startsWithWord {
		count := 0
		for left > 0 && count < 4 && isHiraganaChar(text[left-1]) {
			left--
			count++
		}
	}
	for left > 0 && isWordChar(text[left-1]) {
		left--
	}
	for startsWithWord && right < len(text) && isWordChar(text[right]) {
		right++
	}
	count := 0
	for right < len(text) && count < negationWidenMax && isHiraganaChar(text[right]) {
		right++
		count++
	}
	return runeSlice(text, left, right)
}

func covered(left, right int, marks [][2]int) int {
	total := 0
	for _, m := range marks {
		total += max(0, min(right, m[1])-max(left, m[0]))
	}
	return total
}

// changedPlaces は 2 つの文の変わった箇所 [(i1, i2, j1, j2)]。文字の対応の equal が 1 字だけで切れた箇所は 1 つにまとめる (「不能な → できない」で、「な」だけが同じとされて
// 「い」の挿入が否定の印に見えるのを防ぐ)。どちらかが 2,000 字を超える文は、前後の同じ字を外した残りだけを対応させる。残りも 2,000 字を超えるときは、残り全体を 1 つの箇所にする
// (印でない字が多いので否定には数えない)。長い文の文字の対応は長さのおよそ 2 乗の時間がかかる。
func changedPlaces(old, new []rune) [][4]int {
	var opcodes []difflib.OpCode
	if len(old) > charAlignLimit || len(new) > charAlignLimit {
		head, tail := commonAffix(old, new)
		oldEnd, newEnd := len(old)-tail, len(new)-tail
		if oldEnd-head > charAlignLimit || newEnd-head > charAlignLimit {
			return [][4]int{{head, oldEnd, head, newEnd}}
		}
		for _, op := range difflib.New[rune](old[head:oldEnd], new[head:newEnd]).GetOpcodes() {
			opcodes = append(opcodes, difflib.OpCode{Tag: op.Tag, I1: op.I1 + head, I2: op.I2 + head, J1: op.J1 + head, J2: op.J2 + head})
		}
	} else {
		opcodes = difflib.New[rune](old, new).GetOpcodes()
	}
	var places [][4]int
	for _, op := range opcodes {
		if op.Tag == "equal" {
			continue
		}
		if n := len(places); n > 0 && op.I1-places[n-1][1] <= placeGap && op.J1-places[n-1][3] <= placeGap {
			places[n-1] = [4]int{places[n-1][0], op.I2, places[n-1][2], op.J2}
		} else {
			places = append(places, [4]int{op.I1, op.I2, op.J1, op.J2})
		}
	}
	return places
}

func inMarks(position int, marks [][2]int) bool {
	for _, m := range marks {
		if m[0] <= position && position < m[1] {
			return true
		}
	}
	return false
}

// unmarkedLetters は text[left:right] のうち、否定の印 hits に重ならない、平仮名でない字 (漢字、片仮名など) の並び。
func unmarkedLetters(text []rune, left, right int, hits [][2]int) string {
	var out []rune
	for position := left; position < right; position++ {
		if !inMarks(position, hits) && !isHiraganaChar(text[position]) {
			out = append(out, text[position])
		}
	}
	return pyunicode.FromRunes(out)
}

// verbStemChange は変わった箇所が仮名だけで、「す」で始まる側と「し」で始まる側の組 (する → しない) なら 2 を、「さずに・せずに」と「して」の組なら、印でない字の数を消す分
// (印の「ず」以外の字の数) を返す。印でない字の数から引く。それ以外は 0。
func verbStemChange(oldPart, newPart []rune) int {
	if len(oldPart) == 0 || len(newPart) == 0 {
		return 0
	}
	for _, r := range append(append([]rune(nil), oldPart...), newPart...) {
		if !isHiraganaChar(r) {
			return 0
		}
	}
	o, n := string(oldPart), string(newPart)
	for _, p := range [][2]string{{o, n}, {n, o}} {
		if slices.Contains(zuToTe[0], p[0]) && slices.Contains(zuToTe[1], p[1]) {
			return len([]rune(p[0])) - 1 + len([]rune(p[1]))
		}
	}
	if (oldPart[0] == 'す' && newPart[0] == 'し') || (oldPart[0] == 'し' && newPart[0] == 'す') {
		return 2
	}
	return 0
}

// negationFlip は組になった 2 つの文で、否定の有無が変わった最初の箇所の (元の語, 後の語) を返す。なければ ok が偽。
// 変わった箇所に重なる印だけを数える。重なりは文字の範囲が重なるときだけで、挿入した位置の前後の字は数えない。文全体の否定の印の数が元と後で同じときは、印の数が同じまま
// 言い回しだけを変えた推敲を拾わないよう、ふつうは出さない。ただし、negationLimit か句点の前の規則に当たる箇所が 2 つ以上あり、その中に印が増えた箇所と減った箇所の両方があれば出す
// (1 つの文の中で 2 つの否定を入れ替えた形)。
func negationFlip(oldText, newText string) (string, string, bool) {
	oldMarks, newMarks := negationMarks(oldText), negationMarks(newText)
	sameTotal := len(oldMarks) == len(newMarks)
	if sameTotal && len(oldMarks) == 0 {
		return "", "", false
	}
	old, new := pyunicode.Runes(oldText), pyunicode.Runes(newText)
	oldTail, newTail := mustSearchAt(sentenceTail, pyre.NewText(oldText), 0, pyre.NoEnd), mustSearchAt(sentenceTail, pyre.NewText(newText), 0, pyre.NoEnd)
	oldEnd := len(old) - (oldTail.End(0) - oldTail.Start(0))
	newEnd := len(new) - (newTail.End(0) - newTail.Start(0))
	var found [2]string
	haveFound := false
	directions := map[bool]bool{}
	for _, p := range changedPlaces(old, new) {
		i1, i2, j1, j2 := p[0], p[1], p[2], p[3]
		var oldHits, newHits [][2]int
		for _, m := range oldMarks {
			if m[0] < i2 && m[1] > i1 {
				oldHits = append(oldHits, m)
			}
		}
		for _, m := range newMarks {
			if m[0] < j2 && m[1] > j1 {
				newHits = append(newHits, m)
			}
		}
		if len(oldHits) == len(newHits) {
			continue
		}
		oldPart, newPart := runePart(old, i1, i2), runePart(new, j1, j2)
		rest := (i2 - i1 - covered(i1, i2, oldHits)) + (j2 - j1 - covered(j1, j2, newHits))
		rest -= verbStemChange(oldPart, newPart)
		// 句点の前の規則は、否定を表す語の外の字の違いが平仮名だけ (漢字・片仮名は両側で同じ) のときに限る
		nearEnd := negationEndRule && i1 >= oldEnd-endWindow && i2 <= oldEnd && j1 >= newEnd-endWindow && j2 <= newEnd &&
			unmarkedLetters(old, i1, i2, oldHits) == unmarkedLetters(new, j1, j2, newHits)
		if rest <= negationLimit || nearEnd {
			flip := [2]string{widen(old, i1, i2), widen(new, j1, j2)}
			if !sameTotal {
				return flip[0], flip[1], true
			}
			if !haveFound {
				found, haveFound = flip, true
			}
			directions[len(newHits) > len(oldHits)] = true
		}
	}
	if len(directions) == 2 {
		return found[0], found[1], true
	}
	return "", "", false
}

// runePart は Python のスライス text[a:b] (範囲の外を詰める)。
func runePart(text []rune, a, b int) []rune { return pySlice(text, a, b) }

// negationRisks は否定 (kind negation)。組になった文を文字で対応させ、変わった箇所ごとに、その箇所に重なる否定を表す語の数を元と後で比べる。
func (s *sides) negationRisks(groups []*sentenceGroup) []riskItem {
	var out []riskItem
	order := 0
	for _, group := range groups {
		for _, p := range group.pairs {
			old, new := group.old[p[0]], group.new[p[1]]
			if old.text == new.text {
				continue
			}
			if from, to, ok := negationFlip(old.text, new.text); ok {
				d := pyjson.NewObject()
				d.Set("old", from)
				d.Set("new", to)
				out = append(out, riskItem{groupIndex(group, &new, 1), order, newRisk("negation", nil, old.line, new.line, old.text, new.text, true, true, d)})
				order++
			}
		}
	}
	return out
}

// NegationRisks は元の文と書き直した文の、否定の恐れ (hunk の番号, 塊の中の順, risk の並び。JSON の値の形)。
func NegationRisks(orig, rw string) (pyjson.Value, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	return riskItemsValue(s.negationRisks(s.sentencePairs(full.hunks))), nil
}
