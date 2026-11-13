package lint

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
