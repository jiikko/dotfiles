package oracle

// diffStances は --stance の値 (本家の STANCES の鍵。別名を含む)。
var diffStances = []string{"勧め", "読み手に勧める", "決まり", "手順", "説明", "報告", "体験"}

// diffHeavy は 1 回の実行が重い入力 (長い行・大きい文書)。オプションを通す組から外す (素の 4 通りだけ)。
var diffHeavy = map[string]bool{"input/full-budget": true, "input/full-long-fragment": true, "input/full-repeat": true, "input/full-long-line": true}

// diffOptionPairs は種類・立場・読者・語の一覧の全部を通す組の数 (upstream の先頭から。手で作った入力は全部通す)。
const diffOptionPairs = 6

// DiffCLICases は diff の CLI の記録の case の一覧。名前は記録と突き合わせる鍵。
//   - 組 (upstream の raw_ai と書き直した文・手で作った入力) ごとに text・--json・--fail-on meaning-risk (text と --json)
//   - オプションを通す組 (upstream の先頭 diffOptionPairs 組と手で作った入力の全部) は、種類 13 通り・読者・立場・語の一覧・資料も
//   - --endings は入力の集まりの全ファイルと、手で作った入力の全ファイル (立場なしと 3 つ)
//   - 誤りの呼び出し・標準入力・作業フォルダ (scenario/)
func DiffCLICases(c Corpus, inputs []DiffInput, docTypes, readers []string) []LintCLICase {
	var cases []LintCLICase
	add := caseAdder(&cases)
	type pair struct{ name, old, new string }
	var pairs []pair
	for _, p := range DiffCorpusPairs(c) {
		pairs = append(pairs, pair{p[0], p[1], p[2]})
	}
	upstream := len(pairs)
	for _, in := range inputs {
		o, n := DiffInputPaths(in.Name)
		pairs = append(pairs, pair{"input/" + in.Name, o, n})
	}
	for i, p := range pairs {
		add(p.name+"|text", false, p.old, p.new)
		add(p.name+"|json", false, "--json", p.old, p.new)
		add(p.name+"|json|fail", false, "--json", "--fail-on", "meaning-risk", p.old, p.new)
		add(p.name+"|text|fail", false, "--fail-on", "meaning-risk", p.old, p.new)
		if (i >= diffOptionPairs && i < upstream) || diffHeavy[p.name] {
			continue
		}
		for _, dt := range docTypes {
			add(p.name+"|json|dt="+dt, false, "--json", "--doc-type", dt, p.old, p.new)
			add(p.name+"|text|dt="+dt, false, "--doc-type", dt, p.old, p.new)
		}
		for _, st := range []string{"勧め", "決まり", "説明"} {
			add(p.name+"|json|stance="+st, false, "--json", "--stance", st, p.old, p.new)
			add(p.name+"|text|stance="+st, false, "--stance", st, p.old, p.new)
		}
		for _, rd := range readers {
			add(p.name+"|json|reader="+rd, false, "--json", "--reader", rd, p.old, p.new)
		}
		add(p.name+"|text|slides+expert", false, "--doc-type", "slides", "--reader", "expert", p.old, p.new)
		add(p.name+"|json|words", false, "--json", "--words", "words", p.old, p.new)
		add(p.name+"|text|words", false, "--words", "words", p.old, p.new)
		add(p.name+"|json|no-words", false, "--json", "--no-words", p.old, p.new)
		add(p.name+"|json|options-mixed", false, p.old, "--stance", "勧め", "--doc-type", "mail", p.new, "--json")
	}
	for _, in := range inputs {
		if len(in.Materials) == 0 {
			continue
		}
		o, n := DiffInputPaths(in.Name)
		args := []string{o, n}
		for _, m := range in.Materials {
			args = append(args, "--material", "diff/"+in.Name+"/"+m)
		}
		add("input/"+in.Name+"|json|material", false, append([]string{"--json"}, args...)...)
		add("input/"+in.Name+"|text|material", false, args...)
	}
	for _, f := range c.Files {
		file := "corpus/" + f.Path
		add(f.Path+"|endings", false, "--endings", file)
		for _, st := range []string{"勧め", "決まり", "説明"} {
			add(f.Path+"|endings|stance="+st, false, "--endings", "--stance", st, file)
		}
	}
	for _, in := range inputs {
		for _, name := range []string{"old.md", "new.md"} {
			file := "diff/" + in.Name + "/" + name
			add("input/"+in.Name+"/"+name+"|endings", false, "--endings", file)
			for _, st := range []string{"勧め", "決まり", "説明"} {
				add("input/"+in.Name+"/"+name+"|endings|stance="+st, false, "--endings", "--stance", st, file)
			}
		}
	}
	first, second := pairs[0].old, pairs[0].new
	for _, st := range diffStances {
		add("stance-name|"+st+"|json", false, "--json", "--stance", st, first, second)
		add("stance-name|"+st+"|endings", false, "--endings", "--stance", st, first)
	}
	for _, s := range diffScenarios(first, second, c.Files[0].Path) {
		cases = append(cases, LintCLICase{Name: "scenario/" + s.name, Args: s.args, Keep: true})
	}
	for _, s := range diffStdinScenarios(first, second) {
		cases = append(cases, LintCLICase{Name: "scenario/" + s.name, Args: s.args, Stdin: []byte(s.stdin), Cwd: s.cwd, Keep: true})
	}
	return cases
}

type diffScenario struct {
	name string
	args []string
}

type diffStdinScenario struct {
	name  string
	args  []string
	stdin string
	cwd   string
}

// diffScenarios は引数の誤り・--version・-h・入力の誤り (rc 2)・種類の値の正規化・オプションの組み合わせ。a と b は組 (直す前と後)、file は 1 つのファイル。
func diffScenarios(a, b, file string) []diffScenario {
	file = "corpus/" + file
	list := []diffScenario{
		{"version", []string{"--version"}},
		{"help", []string{"-h"}},
		{"help-long", []string{"--help"}},
		{"help-joined", []string{"-hx"}},
		{"help-with-json", []string{"--json", "-h"}},
		{"help-after-error", []string{"--doc-type", "-h"}},
		{"version-with-bogus", []string{"--version", "--bogus"}},
		{"version-with-bad-files", []string{"--version", a, b, a}},
		{"version-with-bad-fail-on", []string{"--version", "--fail-on=x"}},
		{"no-files", nil},
		{"one-file", []string{a}},
		{"three-files", []string{a, b, a}},
		{"endings-no-files", []string{"--endings"}},
		{"endings-two-files", []string{"--endings", a, b}},
		{"endings-json", []string{"--endings", "--json", a}},
		{"endings-fail-on", []string{"--endings", "--fail-on", "meaning-risk", a}},
		{"endings-doc-type", []string{"--endings", "--doc-type", "mail", a}},
		{"endings-reader", []string{"--endings", "--reader", "expert", a}},
		{"endings-words", []string{"--endings", "--words", "words", a}},
		{"endings-no-words", []string{"--endings", "--no-words", a}},
		{"endings-material", []string{"--endings", "--material", b, a}},
		{"endings-json-and-fail-on", []string{"--endings", "--json", "--fail-on=x", a}},
		{"endings-stance", []string{"--endings", "--stance", "手順", a}},
		{"endings-bad-stance", []string{"--endings", "--stance", "zzz", a}},
		{"words-and-no-words", []string{"--words", "words", "--no-words", a, b}},
		{"words-and-no-words-one-file", []string{"--words", "words", "--no-words", a}},
		{"stdin-twice", []string{"-", "-"}},
		{"stdin-and-material", []string{"-", b, "--material", "-"}},
		{"fail-on-unknown", []string{"--fail-on=x", a, b}},
		{"fail-on-empty", []string{"--fail-on=", a, b}},
		{"fail-on-blank", []string{"--fail-on", " ", a, b}},
		{"fail-on-commas", []string{"--fail-on", ",,", a, b}},
		{"fail-on-unknown-after-meaning-risk", []string{"--fail-on=meaning-risk,x", a, b}},
		{"fail-on-two-unknown", []string{"--fail-on=y", "--fail-on=x", a, b}},
		{"fail-on-underscore-case", []string{"--fail-on=MEANING_RISK", a, b}},
		{"fail-on-mixed", []string{"--fail-on", "a,meaning-risk", a, b}},
		{"fail-on-spaces", []string{"--fail-on", " Meaning-Risk ,", a, b}},
		{"fail-on-twice", []string{"--fail-on", "meaning-risk", "--fail-on=meaning-risk", a, b}},
		{"fail-on-non-ascii", []string{"--fail-on=ÀB", a, b}},
		{"fail-on-dotted-capital-i", []string{"--fail-on", "İ", a, b}},
		{"fail-on-warn", []string{"--fail-on", "warn", a, b}},
		{"fail-on-missing", []string{"--fail-on"}},
		{"stance-unknown", []string{"--stance", "zzz", a, b}},
		{"stance-empty", []string{"--stance=", a, b}},
		{"stance-blank", []string{"--stance", " ", a, b}},
		{"stance-separator-chars", []string{"--stance", "\x1c", a, b}},
		{"stance-ideographic-space", []string{"--stance", "\u3000", a, b}},
		{"stance-separator-endings", []string{"--endings", "--stance", "\x1f", a}},
		{"stance-case", []string{"--stance", "ＡＢ", a, b}},
		{"doc-type-unknown", []string{"--doc-type", "zzz", a, b}},
		{"doc-type-empty", []string{"--doc-type=", a, b}},
		{"doc-type-blank", []string{"--doc-type", " ", a, b}},
		{"doc-type-case", []string{"--doc-type", "SLIDES", a, b, "--json"}},
		{"doc-type-fullwidth", []string{"--doc-type", "ｓｌｉｄｅｓ", a, b, "--json"}},
		{"doc-type-spaces", []string{"--doc-type", " mail ", a, b, "--json"}},
		{"reader-unknown", []string{"--reader", "zzz", a, b}},
		{"reader-empty", []string{"--reader=", a, b}},
		{"reader-case", []string{"--reader", "Expert", a, b, "--json"}},
		{"doc-type-and-reader-both-bad", []string{"--doc-type", "zzz", "--reader", "zzz", a, b}},
		{"fail-on-and-stance-both-bad", []string{"--fail-on=x", "--stance", "zzz", a, b}},
		{"stance-and-doc-type-both-bad", []string{"--stance", "zzz", "--doc-type", "zzz", a, b}},
		{"words-missing-folder", []string{"--words", "nowhere", a, b}},
		{"words-empty-value", []string{"--words=", a, b}},
		{"words-is-file", []string{"--words", "words/allow.txt", a, b}},
		{"words-bad-flag", []string{"--words", "badwords", a, b}},
		{"words-empty-allow", []string{"--words", "emptywords", "--json", a, b}},
		{"words-only-patterns", []string{"--words", "onlypatterns", "--json", a, b}},
		{"words-folder-without-lists", []string{"--words", "sub", "--json", a, b}},
		{"words-relative", []string{"--words", "./words", "--json", a, b}},
		{"words-absolute-in-cwd", []string{"--words", "words/../words", "--json", a, b}},
		{"abbreviated-option", []string{"--js", a, b}},
		{"unknown-option", []string{"--bogus", a, b}},
		{"unknown-short-option", []string{"-x", a, b}},
		{"unknown-option-with-value", []string{"--bogus=1", a, b}},
		{"json-with-value", []string{"--json=x", a, b}},
		{"endings-with-value", []string{"--endings=x", a}},
		{"help-with-value", []string{"--help=x", a, b}},
		{"json-with-newline-value", []string{"--json=a\nb", a, b}},
		{"help-with-both-quotes", []string{"--help='\"", a, b}},
		{"stance-missing", []string{a, b, "--stance"}},
		{"material-missing", []string{a, b, "--material"}},
		{"fail-on-dash-value", []string{"--fail-on", "-", a, b}},
		{"fail-on-option-like-value", []string{"--fail-on", "-x", a, b}},
		{"fail-on-arabic-digit", []string{"--fail-on", "-١", a, b}},
		{"double-dash-files", []string{"--", a, b}},
		{"double-dash-option-as-file", []string{a, "--", "--json"}},
		{"negative-number-as-file", []string{"-1", b}},
		{"mixed-order", []string{"--json", a, "--stance", "決まり", b}},
		{"missing-file", []string{"corpus/nowhere.md", b}},
		{"missing-second-file", []string{a, "corpus/nowhere.md"}},
		{"missing-file-with-trailing-slash", []string{file + "/", b}},
		{"directory", []string{"corpus", b}},
		{"directory-second", []string{a, "corpus"}},
		{"hidden-file", []string{".hidden.md", b}},
		{"hidden-in-path", []string{a, "sub/.x/a.md"}},
		{"dotenv", []string{a, ".env"}},
		{"outside-cwd", []string{"../x.md", b}},
		{"absolute-outside", []string{a, "/etc/hosts"}},
		{"bad-utf8-first", []string{"badutf8.md", b}},
		{"bad-utf8-second", []string{a, "badutf8.md"}},
		{"bad-utf8-material", []string{a, b, "--material", "badutf8.md"}},
		{"material-missing-file", []string{a, b, "--material", "corpus/nowhere.md"}},
		{"material-hidden", []string{a, b, "--material", ".hidden.md"}},
		{"material-outside", []string{a, b, "--material", "../x.md"}},
		{"material-directory", []string{a, b, "--material", "corpus"}},
		{"missing-file-and-bad-material", []string{"corpus/nowhere.md", b, "--material", "badutf8.md"}},
		{"bad-file-and-missing-material", []string{"badutf8.md", b, "--material", "corpus/nowhere.md"}},
		{"two-bad-paths-order", []string{"corpus/nowhere.md", "../x.md"}},
		{"path-then-bad-utf8", []string{"../x.md", "badutf8.md"}},
		{"empty-files", []string{"--json", "empty.md", "empty.md"}},
		{"empty-old", []string{"empty.md", b}},
		{"bom-crlf-files", []string{"--json", "bom.md", "bom.md"}},
		{"bom-crlf-vs-lf", []string{"--json", "bom.md", "sub/note.md"}},
		{"subfolder-files", []string{"--json", "sub/note.md", "sub/note.md"}},
		{"file-with-dot-segment", []string{"./sub/note.md", "./sub/note.md"}},
		{"same-file-text", []string{a, a}},
		{"same-file-json", []string{"--json", a, a}},
		{"same-file-fail-on", []string{"--fail-on", "meaning-risk", a, a}},
		{"endings-empty", []string{"--endings", "empty.md"}},
		{"endings-bad-utf8", []string{"--endings", "badutf8.md"}},
		{"endings-missing", []string{"--endings", "corpus/nowhere.md"}},
		{"endings-hidden", []string{"--endings", ".hidden.md"}},
		{"endings-directory", []string{"--endings", "corpus"}},
		{"endings-bom-crlf", []string{"--endings", "bom.md"}},
	}
	return list
}

// diffStdinScenarios は標準入力と作業フォルダを変える呼び出し。
func diffStdinScenarios(a, b string) []diffStdinScenario {
	const text = "# まとめ\n\n本記事では設定を扱います。ポイントは3つです。\n\n- 項目です。\n- 項目です。\n"
	const text2 = "# まとめ\n\n本記事では設定を扱います。ポイントは4つです。\n\n- 項目です。\n- 項目です。\n"
	return []diffStdinScenario{
		{"stdin-old", []string{"-", b}, text, ""},
		{"stdin-new", []string{a, "-"}, text, ""},
		{"stdin-old-json", []string{"--json", "-", b}, text, ""},
		{"stdin-new-json", []string{"--json", a, "-"}, text, ""},
		{"stdin-endings", []string{"--endings", "-"}, text, ""},
		{"stdin-endings-stance", []string{"--endings", "--stance", "勧め", "-"}, text, ""},
		{"stdin-material", []string{"--json", a, b, "--material", "-"}, text2, ""},
		{"stdin-material-text", []string{a, b, "--material", "-"}, text2, ""},
		{"stdin-empty", []string{"-", b}, "", ""},
		{"stdin-empty-endings", []string{"--endings", "-"}, "", ""},
		{"stdin-bad-utf8", []string{"-", b}, "壊れた\xff入力です。\n", ""},
		{"stdin-bad-utf8-endings", []string{"--endings", "-"}, "壊れた\xff入力です。\n", ""},
		{"stdin-bad-utf8-material", []string{a, b, "--material", "-"}, "壊れた\xff入力です。\n", ""},
		{"stdin-bom-crlf", []string{"--json", "-", b}, "\xef\xbb\xbf# 見出し\r\n\r\n本文です。\r\n", ""},
		{"stdin-bom-crlf-new", []string{"--json", a, "-"}, "\xef\xbb\xbf# 見出し\r\n\r\n本文です。\r\n", ""},
		{"stdin-ignored-with-files", []string{"--json", a, b}, text, ""},
		{"stdin-doc-type", []string{"--doc-type", "slides", "-", b}, text, ""},
		{"stdin-fail-on", []string{"--fail-on", "meaning-risk", "-", b}, text, ""},
		{"cwd-withwords", []string{"--json", "a.md", "b.md"}, text, "withwords"},
		{"cwd-withwords-text", []string{"a.md", "b.md"}, text, "withwords"},
		{"cwd-withwords-no-words", []string{"--json", "--no-words", "a.md", "b.md"}, text, "withwords"},
		{"cwd-gitroot-sub", []string{"--json", "note.md", "note.md"}, text, "gitroot/sub"},
		{"cwd-gitroot-sub-text", []string{"note.md", "note.md"}, text, "gitroot/sub"},
		{"cwd-sub-no-words-folder", []string{"--json", "note.md", "note.md"}, text, "sub"},
		{"cwd-badwords-explicit", []string{"--words", "../badwords", "note.md", "note.md"}, text, "sub"},
		{"cwd-withwords-endings", []string{"--endings", "a.md"}, text, "withwords"},
		{"cwd-badwords-endings", []string{"--endings", "--words", "../badwords", "note.md"}, text, "sub"},
		{"cwd-sub-endings", []string{"--endings", "note.md"}, text, "sub"},
	}
}

// DiffDetectionKinds は diff の --json の出力に出る検出の種類 (driver が kinds に書く名前)。記録の中に、どれも「出る入力」と「出ない入力」が要る
// (どちらかが無いと、その種類の検出が空振りでも、出すべきでないときに出しても、CLI の記録は緑のまま)。
var DiffDetectionKinds = []string{
	"risk:number/replaced", "risk:number/added", "risk:number/lost", "risk:number/order", "risk:negation/None", "risk:swap/None",
	"risk:sentence_type/question", "risk:sentence_type/marks", "risk:protected/code", "risk:protected/inline_code", "risk:protected/url", "risk:protected/link",
	"risk:protected/frontmatter", "risk:protected/html", "risk:protected/quote", "risk:contrast_reversal/None", "risk:feeling/None",
	"risk:certainty/strengthened", "risk:certainty_review/strengthened", "risk:certainty_review/weakened", "risk:citation/replaced", "risk:choice/one_to_all",
	"risk:causality/relation_to_cause", "risk:modality/permission_lost", "risk:modality/permission_added", "risk:modality_review/ability_lost",
	"risk:number_qualifier/lost", "risk:number_qualifier/added", "risk:promise/guarantee", "risk:promise/prevention", "risk:promise_review/added",
	"risk:claim/causal", "risk:quote/lost", "risk:quote/changed", "risk:state/lost",
	"hedges:/decreased", "immutable:/addressee", "immutable:/email", "politeness:/honorific_rate", "politeness:/polite_ratio", "register:/to_plain", "register:/to_polite",
	"tone:tone/exclamation", "tone:tone/intensifier", "tone:tone/emoji", "tone:tone/heading",
	"stiffness:stiffness/no_chain", "stiffness:stiffness/sentence", "stiffness:stiffness/nominalization",
	"rule:bold", "rule:added_actors", "rule:heading_shapes", "comma_increase", "encoding_notes",
	"hunk:insert", "hunk:delete", "hunk:replace", "hunk:whitespace_only", "hunk:moved", "limit:long_fragment", "limit:too_large",
	"status:no_change", "status:changed_without_candidates", "status:has_candidates",
	"type_change:lost/推量", "type_change:added/伝聞", "type_change:added/推量", "type_change:added/例え",
	"endings:changes", "endings:flags", "endings:orig_flags",
}
