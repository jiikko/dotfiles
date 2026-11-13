package lint

import (
	"slices"
	"testing"

	"ja-writing/internal/pyre"
	"ja-writing/oracle"
)

// 絵文字の表は昇順で重ならない (inRuneRanges の二分探索の前提)。生成した表が本家の並びと違っていても、ここで落ちる。
func TestEmojiTablesAreSortedAndDisjoint(t *testing.T) {
	for name, table := range map[string][]runeRange{
		"emojiRegisteredTextBases": emojiRegisteredTextBases[:], "emojiExplicitVariationBases": emojiExplicitVariationBases[:],
		"emojiOtherBases": emojiOtherBases[:], "endVisibleRanges": endVisibleRanges,
	} {
		for i, rg := range table {
			if rg.lo > rg.hi || (i > 0 && table[i-1].hi >= rg.lo) {
				t.Errorf("%s の %d 番目 %v が昇順でも重ならない形でもない", name, i, rg)
			}
		}
	}
	if emojiDataVersion != "18.0" {
		t.Errorf("表のもとの emoji データのバージョンが %s (README の記載と、この表の作り直しの手順を見直す)", emojiDataVersion)
	}
}

// 本家が使った正規表現の文字列は、記録 (patterns.json) に同じ文字列で残っている。Go が組む文字列に書き間違いがあれば、ここで落ちる
// (照合は pyre が本家の記録と突き合わせている)。
func TestPatternSourcesAppearInPatternRecording(t *testing.T) {
	var g oracle.PatternsGolden
	if err := oracle.ReadGolden(goldenDir, oracle.PatternsFile, &g); err != nil {
		t.Fatal(err)
	}
	recorded := map[string]bool{}
	for _, p := range g.Patterns {
		if p.Flags == 0 {
			recorded[p.Pattern] = true
		}
	}
	for name, source := range map[string]string{
		"EMOJI_PATTERN": emojiPatternText(), "_END_BRACKET_AFTER_PREDICATE": endBracketAfterPredicate.pattern(), "_END_BREAK_RUN": endBreakRun.pattern(),
		"_END_BREAK_CONTINUES": endBreakContinuesPattern.pattern(), "_WH_QUESTION": whQuestion.pattern(), "_V220_QUOTES": v220Quotes.pattern(), "_V220_IDENTIFIER": v220Identifier.pattern(),
	} {
		if !recorded[source] {
			t.Errorf("%s のパターンの文字列が、本家の記録 (patterns.json) に無い: %.80s", name, source)
		}
	}
}

// 表と枝で書いた EmojiMatches は、EMOJI_PATTERN の文字列を pyre で組んだ findall と、入力の集まりの全行で同じ答えを返す
// (本家の記録との突き合わせとは別の、Go の中での独立した確かめ)。
func TestEmojiMatchesAgreesWithPatternFindall(t *testing.T) {
	re, err := pyre.Compile(emojiPatternText(), 0)
	if err != nil {
		t.Fatal(err)
	}
	corpus, err := oracle.LoadCorpus(corpusDir)
	if err != nil {
		t.Fatal(err)
	}
	inputs := append([]string{}, "🇯🇵🇺🇸🇺", "1\ufe0f\u20e3#\ufe0f\u20e3", "©\ufe0e©\ufe0f©", "❤\ufe0f❤\ufe0e")
	for _, f := range corpus.Files {
		text, err := corpus.Read(f.Path)
		if err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, text)
	}
	emojiInputs := 0
	for _, text := range inputs {
		found, err := re.Findall(pyre.NewText(text), 0, len(text)+1)
		if err != nil {
			t.Fatal(err)
		}
		want := make([]string, len(found))
		for i, groups := range found {
			want[i] = groups[0]
		}
		got := EmojiMatches(text)
		if !slices.Equal(got, want) {
			t.Errorf("EmojiMatches と findall が違う: %q と %q (入力 %.40q)", got, want, text)
		}
		if len(want) > 0 {
			emojiInputs++
		}
	}
	if emojiInputs < 10 {
		t.Errorf("絵文字を含む入力が %d 件しかない (比べた入力が足りない)", emojiInputs)
	}
}

func TestUniqueInOrderKeepsFirstOccurrence(t *testing.T) {
	got := uniqueInOrder([]string{"b", "a", "b", "c", "a", "d"})
	if want := []string{"b", "a", "c", "d"}; !slices.Equal(got, want) {
		t.Errorf("uniqueInOrder = %q、期待 %q (本家の dict.fromkeys は初めて出た順を保つ)", got, want)
	}
	if got := uniqueInOrder([]string(nil)); len(got) != 0 {
		t.Errorf("空の入力が %q になった", got)
	}
}

// stripEndDecoration の 2 つ目の戻りは、本家の `core is not sentence` (飾りを除いたか) と同じ。除いた結果が元と同じ文字列になることは無い。
func TestStripEndDecorationReportsChangeOnlyWhenRemoved(t *testing.T) {
	for text, wantChanged := range map[string]bool{"見ていきましょう": false, "見ていきましょう。": false, "見ていきましょう🚀": true, "見ていきましょう🚀。": true, "": false, "🚀": true, "。🚀": true} {
		core, changed := stripEndDecoration(text, endClosers)
		if changed != wantChanged || (changed && core == text) {
			t.Errorf("stripEndDecoration(%q) = (%q, %v)、期待 changed=%v で、changed なら中身も変わる", text, core, changed, wantChanged)
		}
	}
}
