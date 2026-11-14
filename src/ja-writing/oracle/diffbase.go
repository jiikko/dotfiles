package oracle

import (
	"os"
	"path/filepath"
	"strings"
)

// DiffBaseFile は diff の関数の記録 (testdata/golden/ の中)。形は visibility.json と同じ (case ごとの呼び出しの digest の列)。
// 群 B (9 キーの下ごしらえ)・C (文末の種類と立場)・E (太字) の公開の関数を、入力の集まりの全ファイルと diff の入力に通した結果。
const DiffBaseFile = "diff-base.json"

// DiffModule は記録の対象の本家の module。
const DiffModule = "yomiyasu_diff"

// DiffBaseGolden は diff-base.json の中身。
type DiffBaseGolden = VisibilityGolden

// DiffBaseFunctions は記録の対象の関数 (module は DiffModule)。記録の件数が 0 の名前と、一覧にない名前は、記録の検査が落とす。
var DiffBaseFunctions = []string{
	"normalize", "has_list", "sentences", "paragraphs", "logic_points",
	"bare_end", "register", "ending_kind", "ending_units", "stance_flags", "ending_changes",
	"bold_problems",
	"full_diff",
	"_build_full_diff", "_heading_structure", "_sentence_pairs", "_all_bodies",
	"type_marks", "_ExactSequenceMatcher", "_type_changes",
	"_rule_candidates",
	"_modality_changes", "_logic_changes",
}

func init() {
	DiffBaseFunctions = append(DiffBaseFunctions, DiffListFunctions...)
	DiffBaseFunctions = append(DiffBaseFunctions, DiffStiffFunctions...)
	DiffBaseFunctions = append(DiffBaseFunctions, diffStiffConstants...)
	DiffBaseFunctions = append(DiffBaseFunctions, DiffToneFunctions...)
	DiffBaseFunctions = append(DiffBaseFunctions, diffToneConstants...)
	DiffBaseFunctions = append(DiffBaseFunctions, DiffNumberFunctions...)
	DiffBaseFunctions = append(DiffBaseFunctions, diffNumberConstants...)
	DiffBaseFunctions = append(DiffBaseFunctions, DiffNegationFunctions...)
	DiffBaseFunctions = append(DiffBaseFunctions, diffNegationConstants...)
	DiffBaseFunctions = append(DiffBaseFunctions, DiffSwapFunctions...)
	DiffBaseFunctions = append(DiffBaseFunctions, diffSwapConstants...)
	DiffBaseFunctions = append(DiffBaseFunctions, DiffQualifierFunctions...)
	DiffBaseFunctions = append(DiffBaseFunctions, diffQualifierConstants...)
	DiffBaseFunctions = append(DiffBaseFunctions, DiffQuestionFunctions...)
	DiffBaseFunctions = append(DiffBaseFunctions, DiffFeelingFunctions...)
	DiffBaseFunctions = append(DiffBaseFunctions, diffQuestionConstants...)
	DiffBaseFunctions = append(DiffBaseFunctions, diffTypeConstants...)
	DiffBaseFunctions = append(DiffBaseFunctions, diffRuleConstants...)
	DiffBaseFunctions = append(DiffBaseFunctions, diffModalConstants...)
	DiffBaseFunctions = append(DiffBaseFunctions, DiffContrastFunctions...)
	DiffBaseFunctions = append(DiffBaseFunctions, diffContrastConstants...)
	DiffBaseFunctions = append(DiffBaseFunctions, DiffCertaintyFunctions...)
	DiffBaseFunctions = append(DiffBaseFunctions, diffCertaintyConstants...)
	DiffBaseFunctions = append(DiffBaseFunctions, DiffReviewFunctions...)
	DiffBaseFunctions = append(DiffBaseFunctions, diffReviewConstants...)
	DiffBaseFunctions = append(DiffBaseFunctions, DiffAdditionFunctions...)
	DiffBaseFunctions = append(DiffBaseFunctions, diffAdditionConstants...)
	DiffBaseFunctions = append(DiffBaseFunctions, DiffMeaningFunctions...)
}

// DiffEncodingNote は full_diff に渡す注記の 1 つ (本家の dict と同じ 3 つの鍵)。
type DiffEncodingNote = map[string]any

// DiffStanceArgs は stance_flags に渡す立場 (nil は指定なし)。
var DiffStanceArgs = []any{nil, "勧め", "決まり", "説明"}

func dcall(fn string, args ...any) Call {
	return Call{Module: DiffModule, Function: fn, Args: args, Digest: true}
}

// diffTextCalls は 1 つの文書に、文書の単位の関数を全部通す呼び出し (keep は結果そのものを記録に残すか)。
func diffTextCalls(text string, keep bool) []Call {
	k := func(c Call) Call { c.Keep = keep; return c }
	calls := []Call{k(dcall("normalize", text)), k(dcall("has_list", text)), k(dcall("sentences", text)), k(dcall("paragraphs", text)), k(dcall("logic_points", text))}
	for _, md := range []bool{false, true} {
		calls = append(calls, k(dcall("ending_units", text, md)))
		for _, stance := range DiffStanceArgs {
			calls = append(calls, k(dcall("stance_flags", text, stance, md)))
		}
	}
	for _, skip := range []bool{true, false} {
		calls = append(calls, k(dcall("bold_problems", text, skip)))
	}
	return calls
}

// diffLineCalls は文書の行ごとに、文の単位の関数を通す呼び出し (空の行は飛ばす)。
func diffLineCalls(text string) []Call {
	lines, _ := physicalLinesOf(text)
	var calls []Call
	for _, line := range lines {
		if line == "" {
			continue
		}
		calls = append(calls, dcall("bare_end", line), dcall("register", line), dcall("ending_kind", line))
	}
	return calls
}

// DiffBaseCases は diff の関数の記録の case の一覧 (入力の集まりの全ファイル・diff の入力の全ファイル・組ごとの ending_changes・手書きの入力)。
func DiffBaseCases(c Corpus, diffDir string, inputs []DiffInput) ([]VisCase, error) {
	var cases []VisCase
	for _, f := range c.Files {
		text, err := c.Read(f.Path)
		if err != nil {
			return nil, err
		}
		cases = append(cases, VisCase{Name: "corpus/" + f.Path, Calls: append(diffTextCalls(text, false), diffLineCalls(text)...)})
	}
	var pairs [][3]string
	for _, in := range inputs {
		var texts [2]string
		for i, name := range []string{"old.md", "new.md"} {
			body, err := os.ReadFile(filepath.Join(diffDir, in.Name, name))
			if err != nil {
				return nil, err
			}
			texts[i] = string(body)
			cases = append(cases, VisCase{Name: "input/" + in.Name + "/" + name, Calls: append(diffTextCalls(texts[i], false), diffLineCalls(texts[i])...)})
		}
		pairs = append(pairs, [3]string{"input/" + in.Name, texts[0], texts[1]})
	}
	for _, p := range DiffCorpusPairs(c) {
		o, err := c.Read(strings.TrimPrefix(p[1], "corpus/"))
		if err != nil {
			return nil, err
		}
		n, err := c.Read(strings.TrimPrefix(p[2], "corpus/"))
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, [3]string{p[0], o, n})
	}
	for _, p := range pairs {
		cases = append(cases, VisCase{Name: "pair/" + p[0], Calls: []Call{dcall("ending_changes", p[1], p[2]), dcall("full_diff", p[1], p[2])}})
	}
	for _, p := range diffEndingPairs() {
		cases = append(cases, VisCase{Name: "pair/snippet/" + p[0], Calls: []Call{{Module: DiffModule, Function: "ending_changes", Args: []any{p[1], p[2]}, Digest: true, Keep: len(p[1]) < 400}}})
	}
	for _, f := range diffFullCases() {
		args := []any{f.old, f.new}
		if f.notes != nil {
			args = append(args, f.notes)
		}
		cases = append(cases, VisCase{Name: "full/" + f.name, Calls: []Call{{Module: DiffModule, Function: "full_diff", Args: args, Digest: true, Keep: len(f.old)+len(f.new) < 2000}}})
	}
	var calls []Call
	for _, s := range diffEndingStrings {
		for _, fn := range []string{"bare_end", "register", "ending_kind"} {
			c := dcall(fn, s)
			c.Keep = true
			calls = append(calls, c)
		}
	}
	cases = append(cases, VisCase{Name: "snippet/endings", Calls: calls})
	for _, d := range diffEndingDocs {
		cases = append(cases, VisCase{Name: "snippet/doc/" + d.name, Calls: diffTextCalls(d.text, true)})
	}
	// 群 K 以降の段は、全文差分の (_Sides, hunks) を渡す。pairs は入力の集まりと diff の入力の組、あとは手書きと固定の種の組
	for _, p := range pairs {
		cases = append(cases, VisCase{Name: "heading/pair/" + p[0], Calls: diffHeadingStageCalls(p[1], p[2])})
	}
	for _, f := range diffFullCases() {
		cases = append(cases, VisCase{Name: "heading/full/" + f.name, Calls: diffHeadingStageCalls(f.old, f.new)})
	}
	for _, f := range diffHeadingCases() {
		cases = append(cases, VisCase{Name: "heading/" + f.name, Calls: diffHeadingStageCalls(f.old, f.new)})
	}
	for _, f := range diffSentenceCases() {
		cases = append(cases, VisCase{Name: "sentence/" + f.name, Calls: diffHeadingStageCalls(f.old, f.new)})
	}
	// 群 F (文の種類の印): 定数・文ごとの印・段 (_type_changes)。組は heading/ と同じ入れ物 (入力の集まりと diff の入力の組・全文差分の手書きの組・見出しと文の手書きの組) に、
	// 群 F 専用の手書きの組と固定の種の組を足したもの
	cases = append(cases, VisCase{Name: "type/constants", Calls: diffTypeConstantCalls()})
	var marks []Call
	for _, s := range diffTypeMarkSentences() {
		c := dcall("type_marks", s)
		c.Keep = true
		marks = append(marks, c)
	}
	cases = append(cases, VisCase{Name: "type/marks", Calls: marks})
	for _, f := range c.Files {
		text, err := c.Read(f.Path)
		if err != nil {
			return nil, err
		}
		cases = append(cases, VisCase{Name: "type/lines/corpus/" + f.Path, Calls: diffTypeLineCalls(text)})
	}
	for _, p := range pairs {
		cases = append(cases, VisCase{Name: "type/pair/" + p[0], Calls: diffTypeStageCalls(p[1], p[2])})
	}
	for _, f := range diffFullCases() {
		cases = append(cases, VisCase{Name: "type/full/" + f.name, Calls: diffTypeStageCalls(f.old, f.new)})
	}
	for _, f := range diffHeadingCases() {
		cases = append(cases, VisCase{Name: "type/heading/" + f.name, Calls: diffTypeStageCalls(f.old, f.new)})
	}
	for _, f := range diffSentenceCases() {
		cases = append(cases, VisCase{Name: "type/sentence/" + f.name, Calls: diffTypeStageCalls(f.old, f.new)})
	}
	for _, f := range diffTypeCases() {
		cases = append(cases, VisCase{Name: "type/" + f.name, Calls: diffTypeStageCalls(f.old, f.new)})
	}
	// 群 G (9 つのキーの組み立て): 定数と段 _rule_candidates。組は群 F と同じ入れ物に、群 G 専用の手書きの組と固定の種の組を足したもの
	cases = append(cases, VisCase{Name: "rule/constants", Calls: diffRuleConstantCalls()})
	for _, p := range pairs {
		cases = append(cases, VisCase{Name: "rule/pair/" + p[0], Calls: diffRuleCalls(p[1], p[2], false)})
	}
	for _, f := range diffFullCases() {
		cases = append(cases, VisCase{Name: "rule/full/" + f.name, Calls: diffRuleCalls(f.old, f.new, false)})
	}
	for _, f := range diffHeadingCases() {
		cases = append(cases, VisCase{Name: "rule/heading/" + f.name, Calls: diffRuleCalls(f.old, f.new, false)})
	}
	for _, f := range diffSentenceCases() {
		cases = append(cases, VisCase{Name: "rule/sentence/" + f.name, Calls: diffRuleCalls(f.old, f.new, false)})
	}
	for _, f := range diffTypeCases() {
		cases = append(cases, VisCase{Name: "rule/type/" + f.name, Calls: diffRuleCalls(f.old, f.new, false)})
	}
	for _, f := range diffRuleCases() {
		cases = append(cases, VisCase{Name: "rule/" + f.name, Calls: diffRuleCalls(f.old, f.new, true)})
	}
	// 群 H (許可・能力)・I (選ぶ範囲・因果): 定数と段 _modality_changes・_logic_changes
	cases = append(cases, VisCase{Name: "modal/constants", Calls: diffModalConstantCalls()})
	for _, p := range pairs {
		cases = append(cases, VisCase{Name: "modal/pair/" + p[0], Calls: diffModalStageCalls(p[1], p[2])})
	}
	for _, f := range diffFullCases() {
		cases = append(cases, VisCase{Name: "modal/full/" + f.name, Calls: diffModalStageCalls(f.old, f.new)})
	}
	for _, f := range diffHeadingCases() {
		cases = append(cases, VisCase{Name: "modal/heading/" + f.name, Calls: diffModalStageCalls(f.old, f.new)})
	}
	for _, f := range diffSentenceCases() {
		cases = append(cases, VisCase{Name: "modal/sentence/" + f.name, Calls: diffModalStageCalls(f.old, f.new)})
	}
	for _, f := range diffTypeCases() {
		cases = append(cases, VisCase{Name: "modal/type/" + f.name, Calls: diffModalStageCalls(f.old, f.new)})
	}
	for _, f := range diffModalCases() {
		cases = append(cases, VisCase{Name: "modal/" + f.name, Calls: diffModalStageCalls(f.old, f.new)})
	}
	// 群 R (語の一覧)
	listCases, err := diffListCases(diffDir)
	if err != nil {
		return nil, err
	}
	cases = append(cases, listCases...)
	// 群 Z (文体の硬化)
	cases = append(cases, diffStiffCases(pairs)...)
	// 群 AA (勢い)
	cases = append(cases, diffToneCases(pairs)...)
	// 群 M (数値)
	cases = append(cases, diffNumberCases(pairs)...)
	// 群 N (否定)・群 O (主語と目的語の入れ替え)
	cases = append(cases, diffNegationCases(pairs)...)
	cases = append(cases, diffSwapCases(pairs)...)
	// 群 Q (概数と範囲の語)
	cases = append(cases, diffQualifierCases(pairs)...)
	// 群 P (問い・変えない箇所・印の候補)
	cases = append(cases, diffQuestionCases(pairs)...)
	// 群 T (気持ちの創作)
	cases = append(cases, diffFeelingCases(pairs)...)
	// 群 S (否定対比の逆転)
	cases = append(cases, diffContrastCases(pairs)...)
	// 群 U (確度)
	cases = append(cases, diffCertaintyCases(pairs)...)
	// 群 X (慎重表現・動かしてはいけない値・敬語) と群 W (かぎかっこ・文体の切り替え)
	cases = append(cases, diffReviewCases(pairs)...)
	// 群 V (書き足した内容の恐れ)
	cases = append(cases, diffAdditionCases(pairs)...)
	// 群 Y (意味が変わる恐れの組み立て)
	return append(cases, diffMeaningCases(pairs)...), nil
}

// diffEndingStrings は文末の関数に通す手書きの文 (1 つが 1 つの枝を狙う)。
var diffEndingStrings = []string{
	"", " ", "。", "です。", "ます。", "ません。", "でした。", "ましょう。", "ください。", "くださいね。", "でしょう。", "確認します。",
	"確認してください", "読んではいけません。", "書いてはなりません。", "お送りしていただきます。", "ご確認いただけます。",
	"試しましょう。", "とよいです。", "といいです。", "確認をおすすめします。", "確認をお勧めします。",
	"遅れるでしょう。", "遅れるだろう。", "遅れるかもしれません。", "遅れるかもしれない。", "遅れると思います。", "遅れると考えます。", "遅れると感じます。", "遅れるはずです。", "遅れるようです。", "遅れる気がします。",
	"確認しなければなりません。", "確認しなくてはなりません。", "確認する必要があります。", "確認するべきです。",
	"これは重要です。", "これは最重要。", "これが大切でした。", "大事だ。", "不可欠である。", "肝心です", "欠かせません。", "ここが鍵です。", "カギ。", "最優先です。", "必要です。", "必要",
	"確認しました。", "確認しませんでした。", "試験でした。",
	"確認しています。", "確認しています", "使われています。", "起きます。", "あります。", "できます。", "分かります。", "増えます。", "防げます。", "書けます。", "見つけられます。", "感じられます。", "思えます。", "壊れます。", "されます。", "しれます。", "触れます。", "考えられません。",
	"送ります。", "確認しません。", "動作します。", "表示します。", "なります。",
	"これは設計です。", "これは設計だ。", "設計である。", "設計ではない。", "設計でない。", "確認する。", "確認した。", "美しい。", "読む", "最後に", "データ", "ABC", "123", "ｱｲｳ", "アルゴリズム", "ー",
	"**太字**です。", "`code`です。", "**確認します**。", "確認します（例）。", "確認します（例）（補足）。", "確認します(例)。", "確認します （例）。", "（例）", "(例)", "確認する）。", "確認します（例。", "確認します）例（。",
	"「確認します」。", "「確認します」", "『確認します』。", "確認します」』）)", "確認します.", "確認します．", "確認します!", "確認します！", "確認します?", "確認しますか？", "確認します。。。", "確認します。 ",
	"確認します　", "確認します 。", "確認します\t。",
	"確認してください。 \x1c(例)", "確認します\x1c。", "確認します\x1f。",
}

// diffEndingPairs は ending_changes に通す手書きの組。字数の積が 160,000 を超える組 (NewExact を使う) と、同じ似かたの組 (同点では元の文の順) を含む。
func diffEndingPairs() [][3]string {
	long := strings.Repeat("あいうえお", 90)
	other := strings.Repeat("あいうえお", 40) + strings.Repeat("かきくけこ", 50)
	return [][3]string{
		{"same-kind", "確認します。", "確認します。"},
		{"kind-change", "確認します。\nこれは重要です。", "確認しましょう。\nこれは大切です。"},
		{"list-orig", "- 確認します\n- 試します", "- 確認しましょう\n- 試しましょう"},
		{"similar", "設定を確認します。", "設定を確認してください。"},
		{"dissimilar", "設定を確認します。", "まったく別の話をします。"},
		{"tie", "確認します。\n確認しました。", "確認しています。"},
		{"tie-equal-score", "同じです。\nまったく別の第4段落です。", "段落でますす。"},
		{"duplicates", "確認します。\n確認します。\n確認しました。", "確認しました。\n確認します。\n確認しましょう。"},
		{"empty-old", "", "確認します。"},
		{"empty-new", "確認します。", ""},
		{"table-skipped", "| a |\n|---|\n| 確認します |", "| a |\n|---|\n| 確認しましょう |"},
		{"dash", "確認します——試します。", "確認します――試しましょう。"},
		{"exact-area", long + "です。", long + "ます。"},
		{"exact-area-low", long + "です。", other + "ます。"},
		{"exact-area-many", long + "です。\n" + other + "です。\n確認します。", long + "ます。\n" + other + "ます。\n確認しましょう。"},
		{"surrogate-free-long", strings.Repeat("あ", 500) + "です。", strings.Repeat("あ", 450) + "ます。"},
	}
}

// diffEndingDocs は文末の単位・立場の関数に通す手書きの文書 (先頭の設定・コードの囲み・表・箇条書き・ダッシュ・敬体と常体の混在)。
var diffEndingDocs = []struct{ name, text string }{
	{"frontmatter", "---\ntitle: x\n---\n\n確認します。\n"},
	{"frontmatter-unclosed", "---\ntitle: x\n\n確認します。\n"},
	{"frontmatter-second-dash-line", "---\n---\n確認します。\n---\n試します。\n"},
	{"code-fence", "確認します。\n\n```sh\nmake test する。\n```\n\n試します。\n```\n未閉じです\n"},
	{"table-separator-cell", "| 項目 |\n|---|\n| \x1c確認します |\n| \x1c説明です\x1d |\n"},
	{"table", "| 項目 | 値 |\n|---|---|\n| A | 説明です |\n| B | 説明する。確認します |\n\n|---|:--|\n"},
	{"list", "- 確認します\n* 試します\n・確認する\n1. 実行します。\n2) 実行する\n"},
	{"dash", "確認します——試します。確認する—試す。\n確認します―続けます。\n"},
	{"mixed-register", "これは設計です。\n実装は後で行う。\n確認します。\nテストも書く。\n"},
	{"mixed-register-plain-majority", "これは設計だ。\n実装は後で行う。\n確認します。\n"},
	{"stance-advise", "手順を実行します。ぜひ読んでください。読みましょう。重要です。\n"},
	{"stance-last-explain", "事実です。読んでください。まとめます。読んでください。\n"},
	{"stance-last-duplicate", "読んでください。読んでください。\n"},
	{"stance-rule", "手順を実行します。読みましょう。大切です。\n"},
	{"empty", ""},
	{"headings", "# 見出しです\n\n## 小見出しです。\n---\n本文です。\n"},
	{"bold-broken", "**「重要」**な点です。**注意。**を読みます。\n"},
}
