package lint

// 本家の規則 40 の一覧。TestRawFindingsMatchPythonRecording・TestEveryRuleHasHitsInRawRecording が、記録に出る規則 ID の集合と同じかを見る。本家の規則は 40 (`"rule": "…"` の字面 31 + dash 3 + 形の規則 3 + 文の型 3)。群 G1〜G20 は README の「lint の骨組みと群の足し方」の表の番号。

// ruleGroup は規則 ID → 群の名前。
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
