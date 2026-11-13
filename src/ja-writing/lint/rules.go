package lint

import "slices"

// 規則 ID と群。本家の規則は 40 (`"rule": "…"` の字面 31 + dash 3 + 形の規則 3 + 文の型 3)。群 G1〜G20 は README の「lint の骨組みと群の足し方」の表の番号。

// ruleGroup は規則 ID → 群の名前。出力の鍵の順や処理の順には使わない (検出の順は lintText の骨組みが決める)。
var ruleGroup = map[string]string{
	"excess_bold": "G1", "excess_list": "G1",
	"sentence_end_repetition": "G2",
	"bold_not_rendered":       "G3",
	"emoji_prohibited":        "G4",
	"dash_decoration":         "G5", "dash_insertion": "G5", "dash_list_ending": "G5",
	"heading_colon": "G6", "short_summary_heading": "G6", "redundant_bracket": "G6",
	"trailing_colon":  "G7",
	"slop_vocabulary": "G8", "metaphor_verb": "G8", "translated_term": "G8",
	"meta_filler": "G9", "summary_restatement": "G9", "meta_intro": "G9", "count_declaration": "G9",
	"instruction_residue": "G10",
	"short_mochiron":      "G11", "fragment_run": "G11", "cleft_list_ending": "G11",
	"negative_parallelism": "G12", "negative_parallelism_density": "G12", "bold_label_list": "G12",
	"pause_comma": "G13",
	"calque":      "G14", "metaphor_label": "G14", "katakana_term": "G14", "thin_evaluation": "G14",
	"renyo_noun":  "G15",
	"ai_phrase":   "G16",
	"slide_comma": "G17",
	"say_hedge":   "G18", "copy_comma_noun": "G18", "bare_verb_chain": "G18",
	"dialogue_period": "G19", "narrative_density": "G19",
	"project_term": "G20",
}

// groupDependencies は、群の検出が別の群の検出の結果を読む組 (地図 2.2 節の太字の 3 つ)。依存先の規則が全部入るまで、依存元の規則は入れられない
// (TestImplementedRulesRespectGroupDependencies)。
//   - G11 (cleft_list_ending) は G9 の filler_patterns に当たる文を外す
//   - G14 (calque) は G8 の metaphor_verb・translated_term と同じ行に出さない。_KIKU_RE と _PUBLICATION_WORDS も使う
//   - G17 (slide_comma) は G13 の pause_comma の候補の _heads と _objects を読む
var groupDependencies = map[string][]string{
	"G11": {"G9"},
	"G14": {"G8"},
	"G17": {"G13"},
}

// implementedRules は、Go に入った規則の一覧 (1 か所だけ)。群を入れる人は、入れた規則の値を true にして、その規則の検出を本家の検出の生の並び
// (testdata/golden/lint-raw.json) と比べるテストを通す。規則を足す・外すのはこの表だけで、骨組み (lintText) は書き換えない。
// 群の間は 1 行空けてある (別の群を同時に入れる人の変更が、同じ行に重ならないようにするため)。
// 全部が true になると、CLI の単位の記録 (出力の全体と rc) を字まで比べる段に切り替わる (TestCLIMatchesPythonRecording)。
var implementedRules = map[string]bool{
	"excess_bold": true,
	"excess_list": true,

	"sentence_end_repetition": true,

	"bold_not_rendered": true,

	"emoji_prohibited": true,

	"dash_decoration":  true,
	"dash_insertion":   true,
	"dash_list_ending": true,

	"heading_colon":         true,
	"short_summary_heading": true,
	"redundant_bracket":     true,

	"trailing_colon": true,

	"slop_vocabulary": true,
	"metaphor_verb":   true,
	"translated_term": true,

	"meta_filler":         true,
	"summary_restatement": true,
	"meta_intro":          true,
	"count_declaration":   true,

	"instruction_residue": true,

	"short_mochiron":    true,
	"fragment_run":      true,
	"cleft_list_ending": true,

	"negative_parallelism":         true,
	"negative_parallelism_density": true,
	"bold_label_list":              true,

	"pause_comma": true,

	"calque":          true,
	"metaphor_label":  true,
	"katakana_term":   false,
	"thin_evaluation": false,

	"renyo_noun": true,

	"ai_phrase": true,

	"slide_comma": true,

	"say_hedge":       true,
	"copy_comma_noun": true,
	"bare_verb_chain": true,

	"dialogue_period":   true,
	"narrative_density": true,

	"project_term": true,
}

// allRulesImplemented は 40 規則が全部入ったか。
func allRulesImplemented() bool {
	for rule := range ruleGroup {
		if !implementedRules[rule] {
			return false
		}
	}
	return true
}

// sortedRules は規則 ID を昇順に返す。
func sortedRules() []string {
	rules := make([]string, 0, len(ruleGroup))
	for rule := range ruleGroup {
		rules = append(rules, rule)
	}
	slices.Sort(rules)
	return rules
}

// 文書の種類ごとの扱い (本家の DOC_TYPE_SKIP_RULES・DOC_TYPE_SKIP_PATTERNS・DOC_TYPE_ONLY_RULES)。並びは本家のまま (skipped_by_doc_type の鍵の順になる)。

type docTypeSkip struct {
	docType string
	rules   []string
}

var docTypeSkipRules = []docTypeSkip{
	{"pr", []string{"excess_list"}},
	{"article", nil},
	{"incident", []string{"excess_list"}},
	{"proposal", nil},
	{"minutes", []string{"bold_label_list", "excess_list", "sentence_end_repetition"}},
	{"manual", []string{"excess_list"}},
	{"slides", []string{"bare_verb_chain", "bold_label_list", "cleft_list_ending", "count_declaration", "excess_list", "short_summary_heading"}},
	{"essay", []string{"bare_verb_chain", "metaphor_label"}},
	{"formal", nil},
	{"promo", []string{"bare_verb_chain", "emoji_prohibited", "fragment_run", "metaphor_label"}},
	{"mail", []string{"emoji_prohibited"}},
	{"paper", nil},
	{"script", []string{"bare_verb_chain", "fragment_run", "metaphor_label", "sentence_end_repetition"}},
}

// docTypeSkipPatterns は、規則の一部の候補だけを種類で外すもの (mail の meta_filler の 5 型)。外すのは message が、タプルのどれかで始まる候補。
var docTypeSkipPatterns = map[string]struct {
	rule     string
	prefixes []string
}{
	"mail": {"meta_filler", []string{
		"チャット応答の名残「ご質問ありがとうございます」",
		"チャット応答の名残「必要なら次に〜します」",
		"定型クロージング「いかがでしたでしょうか」",
		"定型クロージング「ぜひ〜してみてください」",
		"定型クロージング「参考になれば幸いです」など",
	}},
}

// docTypeOnlyRules は、種類を指定したときだけ当てる規則 (規則 ID → 当てる種類)。種類を指定しないときと、ほかの種類では除く。
var docTypeOnlyRules = map[string][]string{
	"pause_comma":       {"pr", "article", "incident", "proposal", "minutes", "manual", "slides", "essay", "formal", "mail", "paper"},
	"dialogue_period":   {"script"},
	"narrative_density": {"essay"},
	"slide_comma":       {"slides"},
}

func skipRulesFor(docType string) []string {
	for _, row := range docTypeSkipRules {
		if row.docType == docType {
			return row.rules
		}
	}
	return nil
}

// 点数 (rule_penalty_score) の定数。

var penaltyWeights = map[string]int{"error": 3, "warn": 2, "info": 1}

const (
	penaltyFloor = 1000
	penaltyHalf  = 40
)

var documentRuleWeights = map[string]int{"excess_bold": 2, "excess_list": 2, "bold_label_list": 1}

// penaltyUncounted は点数を計算する前に除く規則 (2.2.0 から 2.5.0 で足した 13 規則。どれも info で、この規則の候補だけなら 100 のまま)。
var penaltyUncounted = []string{"calque", "metaphor_label", "say_hedge", "thin_evaluation", "katakana_term", "dialogue_period", "narrative_density",
	"copy_comma_noun", "bare_verb_chain", "project_term", "renyo_noun", "slide_comma", "ai_phrase"}

var dashRules = []string{"dash_decoration", "dash_insertion", "dash_list_ending"}

// dashFindingLimit は 1 文書に出すダッシュの候補の上限 (DASH_FINDING_LIMIT)。点数には全部を数える。
const dashFindingLimit = 3

// allowRules は allow.txt の語で外す規則 (規則 ID の順。context.words.allow_removed の並び)。
var allowRules = []string{"ai_phrase", "calque", "katakana_term", "metaphor_label", "metaphor_verb", "project_term", "renyo_noun", "slop_vocabulary", "translated_term"}

// allowPartialRules は 1 行に 1 件で語を並べ、allow.txt の語を語ごとに外す規則。
var allowPartialRules = []string{"ai_phrase", "calque", "katakana_term", "project_term", "renyo_noun"}
