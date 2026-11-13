package oracle

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// LintPostFile は本家 lint_text の検出より後の処理 (種類で外す・allow・ダッシュの上限・点数・件数・context・metrics) と出力 (--json の形・render_report) の記録。
// 本家の検出の結果を作った候補に差し替えて、後の処理だけを本家のまま走らせる (py/record_lint_post.py)。検出の群が入るまで、Go の後の処理と出力を、候補のある入力で確かめる唯一の記録。
const LintPostFile = "lint-post.json"

// LintPostFinding は差し替える候補 (本家の候補の dict)。公開の 5 つの欄と、allow.txt の判定が読む内部の欄。
type LintPostFinding struct {
	Rule           string              `json:"rule"`
	Line           int                 `json:"line"`
	Severity       string              `json:"severity"`
	Message        string              `json:"message"`
	Snippet        string              `json:"snippet"`
	Words          []string            `json:"_words,omitempty"`
	PatternMatches map[string][]string `json:"_pattern_matches,omitempty"`
	// WordTypes (calque の [語, 型の番号])・FlagHits・Replacements は、allow で語の一部だけを外すとき message の作り直しが読む欄。
	WordTypes    [][]any           `json:"_word_types,omitempty"`
	FlagHits     []string          `json:"_flag_hits,omitempty"`
	Replacements map[string]string `json:"_replacements,omitempty"`
}

// LintPostCase は 1 件。Text は本家が検出で何も出さない短い文書 (natural が 0 であることを記録が確かめる)。Words は語の一覧のフォルダ (作業フォルダからの相対。空なら使わない)。
type LintPostCase struct {
	Name    string            `json:"name"`
	Text    string            `json:"text"`
	DocType *string           `json:"doc_type"`
	Reader  *string           `json:"reader"`
	Words   string            `json:"words,omitempty"`
	Inject  []LintPostFinding `json:"inject"`
}

// LintPostRecord は 1 件の記録。Result は lint_text の戻り値の正規の JSON (sort_keys・ensure_ascii・separators=(",", ":")) の SHA-256 の先頭 16 字、Report は render_report の戻りの同じ。
// JSON は --json が出す形 (indent=2。鍵は本家の dict の順のまま) の同じ。Result は鍵を並べ直すので、鍵の順の違いは JSON でしか見えない。
type LintPostRecord struct {
	Name    string `json:"name"`
	Natural int    `json:"natural"`
	Result  string `json:"result"`
	Report  string `json:"report"`
	JSON    string `json:"json"`
	Score   int    `json:"score"`
}

// LintPostGolden は lint-post.json の中身。
type LintPostGolden struct {
	Header Header           `json:"header"`
	Cases  []LintPostRecord `json:"cases"`
}

// MarshalLintPost は LintPostGolden を決まったバイト列にする。
func MarshalLintPost(g LintPostGolden) ([]byte, error) { return marshalHeaderCases(g.Header, g.Cases) }

// RecordLintPost は cases を、files を置いた作業フォルダで本家の lint_text に通して記録にする。
func (r Runner) RecordLintPost(ctx context.Context, header Header, cases []LintPostCase, files map[string]string) (LintPostGolden, error) {
	sb, err := r.newStagedSandbox()
	if err != nil {
		return LintPostGolden{}, err
	}
	defer sb.remove()
	if err := placeFiles(sb.cwd, files); err != nil {
		return LintPostGolden{}, err
	}
	in := struct {
		ScriptsDir string         `json:"scripts_dir"`
		Cases      []LintPostCase `json:"cases"`
	}{sb.tree.Path(ScriptsDir), cases}
	var out []LintPostRecord
	if err := r.runDriver(ctx, sb, "record_lint_post.py", in, &out); err != nil {
		return LintPostGolden{}, err
	}
	if len(out) != len(cases) {
		return LintPostGolden{}, fmt.Errorf("lint_text の実行 %d 件に対して記録が %d 件でした", len(cases), len(out))
	}
	for i, rec := range out {
		if rec.Name != cases[i].Name {
			return LintPostGolden{}, fmt.Errorf("%d 番目の記録の名前 %q が case の名前 %q と違います", i, rec.Name, cases[i].Name)
		}
		if rec.Natural != 0 {
			return LintPostGolden{}, fmt.Errorf("case %s の入力で本家が自分で候補を %d 件出しました (作った候補だけの結果にならない。入力を替える)", rec.Name, rec.Natural)
		}
	}
	return LintPostGolden{Header: header, Cases: out}, nil
}

func postFinding(rule string, line int, severity, message, snippet string, words ...string) LintPostFinding {
	return LintPostFinding{Rule: rule, Line: line, Severity: severity, Message: message, Snippet: snippet, Words: words}
}

// postRuleSeverity は規則の重さ (本家の実際の重さ。bold_not_rendered だけ error、info の規則と warn の規則)。
var postRuleSeverity = map[string]string{
	"bold_not_rendered": "error", "short_summary_heading": "info", "sentence_end_repetition": "warn",
	"summary_restatement": "info", "meta_intro": "info", "count_declaration": "info", "instruction_residue": "info", "short_mochiron": "info",
	"fragment_run": "info", "cleft_list_ending": "info", "negative_parallelism": "info", "negative_parallelism_density": "info", "bold_label_list": "info",
	"pause_comma": "info", "calque": "info", "metaphor_label": "info", "katakana_term": "info", "thin_evaluation": "info", "renyo_noun": "info", "ai_phrase": "info",
	"slide_comma": "info", "say_hedge": "info", "copy_comma_noun": "info", "bare_verb_chain": "info", "dialogue_period": "info", "narrative_density": "info", "project_term": "info",
}

// mailFillerMessages は mail が外す meta_filler の 5 型の message の頭 (本家の DOC_TYPE_SKIP_PATTERNS)。
var mailFillerMessages = []string{
	"チャット応答の名残「ご質問ありがとうございます」", "チャット応答の名残「必要なら次に〜します」", "定型クロージング「いかがでしたでしょうか」",
	"定型クロージング「ぜひ〜してみてください」", "定型クロージング「参考になれば幸いです」など",
}

// LintPostRules は 40 規則の ID (規則 ID の表と同じ。検出の記録 lint-raw.json が全部当たることを別に見る)。
var LintPostRules = []string{
	"excess_bold", "excess_list", "sentence_end_repetition", "bold_not_rendered", "emoji_prohibited", "dash_decoration", "dash_insertion", "dash_list_ending",
	"heading_colon", "short_summary_heading", "redundant_bracket", "trailing_colon", "slop_vocabulary", "metaphor_verb", "translated_term",
	"meta_filler", "summary_restatement", "meta_intro", "count_declaration", "instruction_residue", "short_mochiron", "fragment_run", "cleft_list_ending",
	"negative_parallelism", "negative_parallelism_density", "bold_label_list", "pause_comma", "calque", "metaphor_label", "katakana_term", "thin_evaluation",
	"renyo_noun", "ai_phrase", "slide_comma", "say_hedge", "copy_comma_noun", "bare_verb_chain", "dialogue_period", "narrative_density", "project_term",
}

func allRulesFindings() []LintPostFinding {
	var fs []LintPostFinding
	for i, rule := range LintPostRules {
		severity := postRuleSeverity[rule]
		if severity == "" {
			severity = "warn"
		}
		// 行は規則の順と逆に並べる (出力は行の順、JSON は検出の順)。同じ行に 2 件ある所も作る。
		fs = append(fs, postFinding(rule, 40-i/2, severity, "メッセージ "+rule, "スニペット\n  二行目 "+rule+"  \n\n四行目"))
	}
	for _, prefix := range mailFillerMessages {
		fs = append(fs, postFinding("meta_filler", 3, "warn", prefix+"が検出されました。", "前置きです"))
	}
	return fs
}

func dashFindings() []LintPostFinding {
	var fs []LintPostFinding
	for i, rule := range []string{"dash_decoration", "dash_insertion", "dash_list_ending", "dash_decoration", "dash_insertion", "dash_list_ending", "dash_decoration"} {
		fs = append(fs, postFinding(rule, 10+i, "warn", "ダッシュ "+rule, "—— 行"))
		if i == 1 {
			fs = append(fs, postFinding("bare_verb_chain", 2, "info", "文の形", "動詞、動詞"))
		}
	}
	return fs
}

// wordsFindings は語の一覧 (testdata/words: allow は「確かめ」と「サーバ」、flag-patterns の名前は「公開以外の鍵」「三点リーダ」「同じ行の語」) を通す候補。
// allow で外れて message の作り直しが要らない形だけを作る (作り直しは、その message を持つ群が入るときに、その群が足す)。
func wordsFindings() []LintPostFinding {
	pm := func(rule string, line int, words []string, matches map[string][]string) LintPostFinding {
		f := postFinding(rule, line, "info", "語 "+rule, "行", words...)
		f.PatternMatches = matches
		return f
	}
	return []LintPostFinding{
		postFinding("metaphor_verb", 1, "warn", "比喩動詞", "確かめる", "比喩動詞「確かめ」", "確かめ"),
		postFinding("metaphor_verb", 2, "warn", "比喩動詞 2", "別の語", "別の語"),
		postFinding("slop_vocabulary", 3, "warn", "スロップ", "サーバ", "サーバ"),
		postFinding("slop_vocabulary", 4, "warn", "スロップ 2", "土台", "土台"),
		postFinding("translated_term", 5, "warn", "直訳", "版", "版", "確かめ"),
		postFinding("translated_term", 6, "warn", "直訳 2", "版", "版"),
		postFinding("metaphor_label", 7, "info", "ラベル", "ラベル", "サーバ"),
		postFinding("calque", 8, "info", "直訳語", "サーバ", "サーバ"),
		postFinding("calque", 9, "info", "直訳語 2", "別", "別の語"),
		postFinding("katakana_term", 10, "info", "カタカナ", "サーバ", "サーバ"),
		postFinding("renyo_noun", 11, "info", "連用形", "確かめ", "確かめ"),
		postFinding("ai_phrase", 12, "info", "AI の言い回し", "サーバ", "サーバ"),
		postFinding("project_term", 13, "info", "語の一覧", "サーバ", "サーバ"),
		postFinding("project_term", 14, "info", "語の一覧 2", "キャッシュ", "キャッシュ"),
		pm("project_term", 15, []string{"三点リーダ"}, map[string][]string{"三点リーダ": {"サーバ"}}),
		pm("project_term", 16, []string{"三点リーダ"}, map[string][]string{"三点リーダ": {"..."}}),
		pm("project_term", 17, []string{"同じ行の語"}, map[string][]string{"同じ行の語": {"キャッシュ"}}),
		postFinding("emoji_prohibited", 18, "warn", "絵文字", "サーバ", "サーバ"),
		postFinding("thin_evaluation", 19, "info", "薄い評価", "x"),
	}
}

// AllowPartialPrefix は、allow.txt で語の一部だけを外して message を作り直す case の名前の頭。作り直す message は群 (G14・G15・G16・G20) の関数なので、
// その規則が Go に入る (implementedRules) までは Go の側が比べない。
const AllowPartialPrefix = "allow-partial|"

// allowPartialFindings は語の一覧 (allow は「確かめ」と「サーバ」) で、1 行の語の一部だけが外れ、message を作り直す候補。入力の集まりでは renyo_noun しか作り直しが走らない
// (calque・ai_phrase・project_term は 0 件) ので、本家に作り直させた結果を記録に置く。
func allowPartialFindings() map[string][]LintPostFinding {
	f := func(rule, msg string, words []string) LintPostFinding {
		return postFinding(rule, 1, "info", msg, "行", words...)
	}
	calque := func(words []string, types [][]any) LintPostFinding {
		c := f("calque", "直訳語", words)
		c.WordTypes = types
		return c
	}
	project := func(words []string, flagHits []string, patterns map[string][]string, replacements map[string]string) LintPostFinding {
		p := f("project_term", "語の一覧", words)
		p.FlagHits, p.PatternMatches, p.Replacements = flagHits, patterns, replacements
		return p
	}
	return map[string][]LintPostFinding{
		"calque|2-types":          {calque([]string{"サーバ", "別の語"}, [][]any{{"サーバ", 1}, {"別の語", 2}})},
		"calque|same-type":        {calque([]string{"サーバ", "別の語", "さらに別"}, [][]any{{"サーバ", 1}, {"別の語", 1}, {"さらに別", 3}})},
		"calque|same-type-rest":   {calque([]string{"サーバ", "別の語", "さらに別", "四つ目"}, [][]any{{"サーバ", 1}, {"別の語", 1}, {"さらに別", 1}, {"四つ目", 2}})},
		"ai_phrase":               {f("ai_phrase", "AI の言い回し", []string{"サーバ", "別の語", "もう一つ"})},
		"renyo_noun":              {f("renyo_noun", "連用形", []string{"確かめ", "比べ"})},
		"project_term|flag":       {project([]string{"サーバ", "キャッシュ"}, []string{"サーバ", "キャッシュ"}, nil, map[string]string{"サーバ": "サーバー", "キャッシュ": "保存"})},
		"project_term|pattern":    {project([]string{"サーバ", "三点リーダ"}, []string{"サーバ"}, map[string][]string{"三点リーダ": {"..."}}, nil)},
		"project_term|both":       {project([]string{"サーバ", "三点リーダ", "キャッシュ"}, []string{"サーバ", "キャッシュ"}, map[string][]string{"三点リーダ": {"..."}}, map[string]string{"キャッシュ": "保存"})},
		"project_term|name-allow": {project([]string{"三点リーダ", "キャッシュ"}, []string{"キャッシュ"}, map[string][]string{"三点リーダ": {"サーバ"}}, map[string]string{"キャッシュ": "保存"})},
	}
}

func repeatText(unit string, n int) string { return strings.Repeat(unit, n) + "\n" }

// LintPostCases は検出より後の処理と出力の記録の case の一覧。docTypes は種類の全部 (visibility の表)。
func LintPostCases(docTypes, readers []string) []LintPostCase {
	short := "これは文書です。\n"
	var cases []LintPostCase
	add := func(name, text string, docType, reader *string, words string, inject []LintPostFinding) {
		cases = append(cases, LintPostCase{Name: name, Text: text, DocType: docType, Reader: reader, Words: words, Inject: inject})
	}
	ptr := func(s string) *string { return &s }
	all, dash, words := allRulesFindings(), dashFindings(), wordsFindings()
	add("none|empty", short, nil, nil, "", []LintPostFinding{})
	add("none|all", short, nil, nil, "", all)
	add("none|dash", short, nil, nil, "", dash)
	for _, dt := range docTypes {
		add(dt+"|all", short, ptr(dt), nil, "", all)
		add(dt+"|dash", short, ptr(dt), nil, "", dash)
		add(dt+"|empty", short, ptr(dt), nil, "", []LintPostFinding{})
	}
	for _, rd := range readers {
		add("reader="+rd+"|all", short, nil, ptr(rd), "", all)
	}
	add("slides+expert|all", short, ptr("slides"), ptr("expert"), "", all)
	add("none|words", short, nil, nil, "words", words)
	add("none|words-empty", short, nil, nil, "words", []LintPostFinding{})
	add("mail|words", short, ptr("mail"), ptr("general"), "words", append(slices.Clone(words), all...))
	add("emptywords|words", short, nil, nil, "emptywords", words)
	partial := allowPartialFindings()
	for _, name := range slices.Sorted(maps.Keys(partial)) {
		add(AllowPartialPrefix+name, short, nil, nil, "words", partial[name])
	}
	// 点数: 地の文の字数の下限 (1000) の前後、重さの組み合わせ、文書単位の規則、数えない規則。
	for _, n := range []int{0, 5, 999, 1000, 1001, 5000} {
		text := repeatText("あ", n)
		add(fmt.Sprintf("score|chars=%d|one-warn", n), text, nil, nil, "", []LintPostFinding{postFinding("trailing_colon", 1, "warn", "m", "s")})
		add(fmt.Sprintf("score|chars=%d|mixed", n), text, nil, nil, "", []LintPostFinding{
			postFinding("bold_not_rendered", 1, "error", "m", "s"), postFinding("trailing_colon", 2, "warn", "m", "s"), postFinding("pause_comma", 3, "info", "m", "s"),
			postFinding("excess_bold", 1, "warn", "m", "s"), postFinding("bold_label_list", 4, "info", "m", "s"), postFinding("negative_parallelism_density", 5, "info", "m", "s"),
		})
		add(fmt.Sprintf("score|chars=%d|uncounted", n), text, nil, nil, "", []LintPostFinding{
			postFinding("calque", 1, "info", "m", "s"), postFinding("project_term", 2, "info", "m", "s"), postFinding("ai_phrase", 3, "info", "m", "s"),
		})
		add(fmt.Sprintf("score|chars=%d|dash", n), text, nil, nil, "", dash)
	}
	heavy := make([]LintPostFinding, 0, 200)
	for i := range 200 {
		heavy = append(heavy, postFinding("trailing_colon", i+1, "warn", "m", "s"))
	}
	add("score|heavy-short", short, nil, nil, "", heavy)
	add("score|heavy-long", repeatText("あ", 20000), nil, nil, "", heavy)
	add("score|document-weights", short, nil, nil, "", []LintPostFinding{postFinding("excess_bold", 1, "warn", "m", "s"), postFinding("excess_list", 1, "warn", "m", "s")})
	return cases
}
