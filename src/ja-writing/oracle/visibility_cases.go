package oracle

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"subproc"
)

// VisModule は記録の対象の本家の module。
const VisModule = "markdown_visibility"

// VisCase は関数の呼び出しの並び 1 つ。上から順に、同じ Python のプロセスの中で走る (Save した値と作業フォルダの fixture が、後の呼び出しに見える)。
// 作業フォルダを使う case は、先頭で chdir し、最後に restoreenv で環境を戻す (case どうしが cwd と環境変数を持ち越さない)。
type VisCase struct {
	Name  string
	Calls []Call
}

// VisFunction は記録する本家の名前。Constant は関数でなく定数 (Get で読む)。Raises は、記録の中に例外になる呼び出しと成功する呼び出しの両方が要る関数。
type VisFunction struct {
	Name     string
	Constant bool
	Raises   bool
}

// VisFunctions は記録の対象の公開面 (lint・diff・check が import する名前と、その下の find_words_folder・load_words・check_pattern)。
// 記録の件数が 0 の名前・例外か成功のどちらかが無い Raises の名前は、記録の検査が落とす (空振りを緑にしない)。
var VisFunctions = []VisFunction{
	{"analyze_markdown", false, true},
	{"line_visible_text", false, true},
	{"range_overlaps_protected", false, false},
	{"inline_protected_spans", false, false},
	{"reference_definition_end", false, false},
	{"unicode_whitespace", false, false},
	{"decode_source", false, true},
	{"normalize_doc_type", false, true},
	{"normalize_reader", false, true},
	{"context_label", false, true},
	{"find_words_folder", false, true},
	{"load_words", false, true},
	{"resolve_words", false, true},
	{"current_directory", false, false},
	{"words_context", false, false},
	{"words_label", false, false},
	{"check_pattern", false, true},
	{"input_path_problem", false, false},
	{"absolute_path", false, false},
	{"DOC_TYPES", true, false},
	{"READER_LEVELS", true, false},
	{"WORDS_FOLDER", true, false},
	{"WORDS_FILES", true, false},
	{"WORDS_MAX_LINES", true, false},
	{"FLAG_REPLACEMENT_MAX", true, false},
	{"PATTERNS_FILE", true, false},
	{"PATTERNS_MAX", true, false},
	{"PATTERN_NAME_MAX", true, false},
	{"PATTERN_MAX_LENGTH", true, false},
	{"PATTERN_MAX_REPEAT", true, false},
	{"PATTERN_MAX_COST", true, false},
	{"PATTERNS_MAX_COST", true, false},
	{"INPUT_PATH_OUTSIDE", true, false},
	{"INPUT_PATH_HIDDEN", true, false},
	{"INPUT_PATH_NO_CWD", true, false},
	{"__version__", true, false},
}

func vcall(fn string, args ...any) Call {
	return Call{Module: VisModule, Function: fn, Args: args, Digest: true}
}

func vkeep(c Call) Call { c.Keep = true; return c }

func vsave(c Call, name string) Call { c.Save = name; return c }

func vget(name string) Call {
	return Call{Module: VisModule, Function: name, Get: true, Digest: true, Keep: true}
}

func vop(op Op) Call { return Call{Op: &op, Digest: true} }

// physicalLinesOf は文字列を \r\n・\r・\n で行に割る (本家の _physical_lines と同じ割り方を、Go で独立に書いたもの)。starts は各行の始まりのコードポイントの添字。
func physicalLinesOf(text string) (lines []string, starts []int) {
	runes := []rune(text)
	start := 0
	flush := func(end, next int) {
		lines = append(lines, string(runes[start:end]))
		starts = append(starts, start)
		start = next
	}
	for i := 0; i < len(runes); i++ {
		switch runes[i] {
		case '\r':
			if i+1 < len(runes) && runes[i+1] == '\n' {
				flush(i, i+2)
				i++
			} else {
				flush(i, i+1)
			}
		case '\n':
			flush(i, i+1)
		}
	}
	flush(len(runes), len(runes))
	return lines, starts
}

var (
	kindsText  = []any{"inline_code", "link_destination", "bare_url", "escape"}
	kindsBlock = []any{"code", "html_block", "frontmatter", "reference_definition"}
)

func runeLen(s string) int { return len([]rune(s)) }

// corpusCase は入力の集まりの 1 ファイルに、解析と行ごとの問い合わせを通す。
func corpusCase(path string, raw []byte) VisCase {
	text := string(raw)
	calls := []Call{
		vsave(vcall("analyze_markdown", text), "a"),
		vsave(vcall("analyze_markdown", text, false), "b"),
		vcall("decode_source", Bytes(raw)),
		vcall("decode_source", text),
	}
	lines, starts := physicalLinesOf(text)
	for i, line := range lines {
		n, start, size := i+1, starts[i], runeLen(line)
		calls = append(calls,
			vcall("line_visible_text", Ref("a"), n),
			vcall("line_visible_text", Ref("a"), n, kindsText),
			vcall("range_overlaps_protected", Ref("a"), start, start+size),
			vcall("range_overlaps_protected", Ref("a"), start+size/2, start+size+2, kindsBlock),
			vcall("inline_protected_spans", line),
		)
	}
	calls = append(calls,
		vcall("range_overlaps_protected", Ref("a"), 0, runeLen(text)),
		vcall("range_overlaps_protected", Ref("b"), 0, runeLen(text), []any{}),
		vcall("inline_protected_spans", text),
	)
	return VisCase{Name: "corpus/" + path, Calls: calls}
}

// snippetCase は手書きの入力に解析を通し、結果そのものも記録に残す。
func snippetCase(name, text string) VisCase {
	calls := []Call{
		vkeep(vsave(vcall("analyze_markdown", text), "a")),
		vsave(vcall("analyze_markdown", text, false), "b"),
		vkeep(vcall("inline_protected_spans", text)),
		vkeep(vcall("range_overlaps_protected", Ref("a"), 0, runeLen(text))),
	}
	lines, _ := physicalLinesOf(text)
	for i := range lines {
		calls = append(calls, vkeep(vcall("line_visible_text", Ref("a"), i+1)), vcall("line_visible_text", Ref("b"), i+1, kindsBlock))
	}
	return VisCase{Name: "snippet/" + name, Calls: calls}
}

// refDefInputs は reference_definition_end に渡す行の並び。
var refDefInputs = [][]string{
	// 行の中に改行の字がある (最後の行でも char が '\n' を返し、本家は IndexError)
	{"[a]: b \"t", "\n"},
	{"[a]: b \"t\n"},
	{"[a]: b \"t\n", ""},
	{"[a]: b (t\n"},
	{"[a\n"},
	{"[a\n", "b]: /u"},
	{"[a\n]: /u"},
	{"[a]: /u\n"},
	{"[a]: /u \"t\" \n"},
	{"[a]: /u", "\"t\n"},
	{"[a]: /url"},
	{"[a]: /url", "[b]: /u2"},
	{"[a]:", "/url"},
	{"[a]:", "/url", "\"title\""},
	{"[a]: /url", "\"title\""},
	{"[a]: /url \"title\""},
	{"[a]: /url \"title\" extra"},
	{"[a]: /url", "'title", "", "rest"},
	{"[a]: /url (t)", ""},
	{"[a]: /url (t(", ""},
	{"[a]: <b c>"},
	{"[a]: <b", "c>"},
	{"[a]: <b\\>c>"},
	{"[a]: <b<c>"},
	{"[a]: b\\ c"},
	{"[a]: b(c)d"},
	{"[a]: b(c"},
	{"[a]: b)c"},
	{"[a]: " + repeatStr("(", 33) + "x" + repeatStr(")", 33)},
	{"[a]: " + repeatStr("(", 32) + "x" + repeatStr(")", 32)},
	{"   [a]: /url"},
	{"    [a]: /url"},
	{"[a b", "c]: /url"},
	{"[a b", "", "c]: /url"},
	{"[]: /url"},
	{"[ ]: /url"},
	{"[\\]]: /url"},
	{"[a[]: /url"},
	{"[" + repeatStr("a", 999) + "]: /url"},
	{"[" + repeatStr("a", 1000) + "]: /url"},
	{"[" + repeatStr("\\a", 499) + "a]: /url"},
	{"[" + repeatStr("\\a", 500) + "]: /url"},
	{"[a]:/url"},
	{"[a]: ", "  ", "/url"},
	{"[a]: /url\t\"t\"\t"},
	{"[a]: /url\"t\""},
	{"[a]: /url \"t\\\"x\""},
	{"[a]: /url", "  \"t", "t2\"", "x"},
	{"[a]: /url", "  \"t", "", "t2\""},
	{"[a]: /\x01x"},
	{"[a]: /\x7fx"},
	{"[a]:"},
	{""},
	{},
}

// kelvin などの正規化の入力。
var docTypeInputs = []any{
	nil, "pr", "PR", " SLIDES ", "Slides", "\U0000ff33\U0000ff2c\U0000ff29\U0000ff24\U0000ff25\U0000ff33", "slides\U00003000", "\U000000a0article\U000000a0",
	"", "   ", "\t", "unknown", "p r", "pr\x00", "a\U0000212artic", "\U00000130", "script", "formal", "promo", "mail", "paper", "essay", "minutes",
	"incident", "proposal", "manual", "article", "\U0000ff50\U0000ff52", "pr\n", "PR ",
}

var readerInputs = []any{
	nil, "expert", "EXPERT", " informed ", "\U0000ff47\U0000ff45\U0000ff4e\U0000ff45\U0000ff52\U0000ff41\U0000ff4c", "", "  ", "novice", "gene ral", "general\U00003000",
}

func normalizeCases() []VisCase {
	var calls []Call
	for _, v := range docTypeInputs {
		calls = append(calls, vkeep(vcall("normalize_doc_type", v)))
	}
	for _, v := range readerInputs {
		calls = append(calls, vkeep(vcall("normalize_reader", v)))
	}
	for _, d := range []any{nil, "pr", "SLIDES", "", "bad"} {
		for _, r := range []any{nil, "expert", "General", "", "bad"} {
			calls = append(calls, vkeep(vcall("context_label", d, r)))
		}
	}
	return []VisCase{{Name: "doc-types/normalize", Calls: calls}}
}

func constantCases() []VisCase {
	var calls []Call
	for _, f := range VisFunctions {
		if f.Constant {
			calls = append(calls, vget(f.Name))
		}
	}
	return []VisCase{{Name: "constants", Calls: calls}}
}

func whitespaceCases() []VisCase {
	var calls []Call
	for cp := rune(0); cp <= 0x3100; cp++ {
		if cp >= 0xD800 && cp <= 0xDFFF {
			calls = append(calls, vcall("unicode_whitespace", Cps{cp}))
			continue
		}
		calls = append(calls, vcall("unicode_whitespace", string(cp)))
	}
	for _, cp := range []rune{0xFE00, 0xFEFF, 0x1680, 0x180E, 0x200B, 0x2028, 0x2029, 0x202F, 0x205F, 0xE0001, 0x1F600, 0x10FFFF, 0x1D7CE, 0xD7FF, 0xE000} {
		calls = append(calls, vcall("unicode_whitespace", string(cp)))
	}
	for _, s := range []string{"", "  ", " \t\n", "a ", " a", "\U00003000\U000000a0", "\U00003000\U000000a0 ", "ab", "\r\n", "\x0b", "\x1c", "\x85"} {
		calls = append(calls, vcall("unicode_whitespace", s))
	}
	calls = append(calls, vcall("unicode_whitespace", Cps{0xD800, 0x20}), vcall("unicode_whitespace", Cps{0x20, 0xDFFF}))
	return []VisCase{{Name: "unicode-whitespace", Calls: calls}}
}

func decodeCases() []VisCase {
	var calls []Call
	byteInputs := [][]byte{
		nil, []byte("a"), []byte("abc\n"), []byte("\xef\xbb\xbfabc"), []byte("\xef\xbb\xbf\xef\xbb\xbfabc"), []byte("a\r\nb\r\n"), []byte("a\rb\r"), []byte("a\r\nb\nc"), []byte("a\r\rb"),
		[]byte("\r"), []byte("\r\n"), []byte("\n"), []byte("あいう\r\nえお"), []byte("\xef\xbb\xbf"), []byte("a\U00002028b\u0085c"),
	}
	for _, b := range byteInputs {
		calls = append(calls, vkeep(vcall("decode_source", Bytes(b))), vkeep(vcall("decode_source", string(b))))
	}
	for _, s := range []Cps{{0xFEFF, 0x61}, {0xD800}, {0x61, 0xDC00, 0x62}, {0xFEFF, 0xFEFF}} {
		calls = append(calls, vcall("decode_source", s))
	}
	// UTF-8 の誤り: 1 バイト・先頭と継続の全組み合わせの代表・途中で終わる形・長さ違いの継続。
	for b := range 256 {
		calls = append(calls, vcall("decode_source", Bytes{byte(b)}), vcall("decode_source", Bytes{byte(b), 0x80}), vcall("decode_source", Bytes{'a', byte(b), 'b'}))
	}
	firsts := []byte{0xC1, 0xC2, 0xDF, 0xE0, 0xE1, 0xEC, 0xED, 0xEE, 0xEF, 0xF0, 0xF1, 0xF3, 0xF4, 0xF5}
	seconds := []byte{0x00, 0x41, 0x7F, 0x80, 0x8F, 0x90, 0x9F, 0xA0, 0xBF, 0xC0, 0xFF}
	for _, f := range firsts {
		for _, s := range seconds {
			calls = append(calls, vcall("decode_source", Bytes{f, s}), vcall("decode_source", Bytes{f, s, 0x80}), vcall("decode_source", Bytes{f, s, 0x80, 0x80}),
				vcall("decode_source", Bytes{f, s, 0x80, 0x41}), vcall("decode_source", Bytes{f, s, 0x41, 0x80}), vcall("decode_source", Bytes{'x', f, s, 0x80, 'y'}))
		}
	}
	for _, b := range [][]byte{{0xE3, 0x81}, {0xE3}, {0xF0, 0x9F, 0x98}, {0xF0, 0x9F}, {0xF0}, {0xE3, 0x81, 0x82, 0xE3}, {0xED, 0xA0, 0x80}, {0xED, 0xBF, 0xBF}, {0xF4, 0x90, 0x80, 0x80}, {0xF4, 0x8F, 0xBF, 0xBF}, {0xC0, 0x80}, {0xE0, 0x80, 0x80}, {0xF0, 0x80, 0x80, 0x80}, {0xFF, 0xFE}} {
		calls = append(calls, vkeep(vcall("decode_source", Bytes(b))))
	}
	return []VisCase{{Name: "decode-source", Calls: calls}}
}

// surrogateCases は単独のサロゲートを含む str (Python の str は持てる)。JSON の文字列では書けないので Cps で渡す。
func surrogateCases() []VisCase {
	hi, lo := rune(0xD800), rune(0xDC00)
	texts := []Cps{
		cps("a", hi, "b"), cps("`", lo, "`"), cps("[", hi, "](x)"), cps(rune(0xD83D), rune(0xDE00)), cps("> ", hi, "\n- ", rune(0xDFFF)),
		cps("<a ", hi, "=b>"), cps("http://x", hi), cps(hi), cps("# ", hi, lo),
	}
	var calls []Call
	for i, t := range texts {
		name := "s" + itoa3(i)
		calls = append(calls, vkeep(vsave(vcall("analyze_markdown", t), name)), vkeep(vcall("inline_protected_spans", t)),
			vkeep(vcall("line_visible_text", Ref(name), 1)), vkeep(vcall("decode_source", t)), vkeep(vcall("range_overlaps_protected", Ref(name), 0, len(t))))
	}
	// 単独のサロゲートを含むパターン。必須の字の列 (Required) と誤りの文は、サロゲートを U+FFFD に変えずに持ち続ける。
	for _, p := range []Cps{
		cps("a", hi, "b"), cps(hi, "{2}x"), cps("x", hi, "{1,2}y"), cps("a", hi, lo, "b"), cps("\\", hi), cps("(?:", hi, ")x"), cps("[", hi, "-", lo, "]x"),
	} {
		calls = append(calls, vkeep(vcall("check_pattern", p)))
	}
	return []VisCase{{Name: "surrogates", Calls: calls}}
}

func indexErrorCases() []VisCase {
	text := "a\nb\nc\n"
	calls := []Call{vsave(vcall("analyze_markdown", text), "a")}
	for _, n := range []int{1, 2, 3, 4, 5, 0, -1, -2, -3, -4, 100, -100} {
		calls = append(calls, vkeep(vcall("line_visible_text", Ref("a"), n)), vkeep(vcall("line_visible_text", Ref("a"), n, []any{"escape"})))
	}
	calls = append(calls,
		vkeep(vcall("range_overlaps_protected", Ref("a"), 3, 3)), vkeep(vcall("range_overlaps_protected", Ref("a"), 5, 2)),
		vkeep(vcall("range_overlaps_protected", Ref("a"), -5, 100)), vkeep(vcall("range_overlaps_protected", Ref("a"), 0, 1, []any{})))
	// 保護された範囲のある文書で、種類の指定ごとの結果と、同じ問い合わせの 2 回目 (結果の覚え) を見る。
	doc := "`a` \\* [l](u) text\n"
	calls = append(calls, vsave(vcall("analyze_markdown", doc), "p"))
	for _, kinds := range []any{nil, []any{}, []any{"inline_code"}, []any{"inline_code", "escape"}, []any{"escape", "inline_code"}, []any{"nope"}, []any{"link_destination"}} {
		for range 2 {
			calls = append(calls, vkeep(vcall("line_visible_text", Ref("p"), 1, kinds)), vkeep(vcall("range_overlaps_protected", Ref("p"), 0, 3, kinds)), vkeep(vcall("range_overlaps_protected", Ref("p"), 3, 4, kinds)))
		}
	}
	// 種類の集合は覚えの鍵になる。名前に NUL を含む 1 つの種類 ("escape\x00inline_code") は、2 つの種類の集合と別物。どちらを先に問うても同じ答えになる。
	both, joined := []any{"escape", "inline_code"}, []any{"escape\x00inline_code"}
	for i, order := range [][][]any{{both, joined}, {joined, both}} {
		name := "nul" + itoa3(i)
		calls = append(calls, vsave(vcall("analyze_markdown", "`x`"), name))
		for _, kinds := range order {
			calls = append(calls, vkeep(vcall("line_visible_text", Ref(name), 1, kinds)), vkeep(vcall("range_overlaps_protected", Ref(name), 0, 3, kinds)))
		}
	}
	return []VisCase{{Name: "analysis-queries", Calls: calls}}
}

// VisibilityCases は記録する呼び出しの全部 (入力の集まりと手書きの入力と、作業フォルダの fixture)。
func VisibilityCases(c Corpus) ([]VisCase, error) {
	var cases []VisCase
	for _, f := range c.Files {
		raw, err := os.ReadFile(filepath.Join(c.Root, filepath.FromSlash(f.Path)))
		if err != nil {
			return nil, fmt.Errorf("入力 %s を読めません: %w", f.Path, err)
		}
		cases = append(cases, corpusCase(f.Path, raw))
	}
	for _, s := range markdownSnippets {
		cases = append(cases, snippetCase(s.name, s.text))
	}
	refCalls := []Call{}
	for _, lines := range refDefInputs {
		args := make([]any, len(lines))
		for i, l := range lines {
			args[i] = l
		}
		for start := -1; start <= len(lines); start++ {
			refCalls = append(refCalls, vkeep(vcall("reference_definition_end", args, start)))
		}
		refCalls = append(refCalls, vkeep(vcall("reference_definition_end", args, 0, true)))
	}
	cases = append(cases, VisCase{Name: "reference-definition-end", Calls: refCalls})
	cases = append(cases, normalizeCases()...)
	cases = append(cases, constantCases()...)
	cases = append(cases, whitespaceCases()...)
	cases = append(cases, decodeCases()...)
	cases = append(cases, indexErrorCases()...)
	cases = append(cases, surrogateCases()...)
	cases = append(cases, fuzzCases()...)
	cases = append(cases, patternCases()...)
	words, err := wordsCases()
	if err != nil {
		return nil, err
	}
	cases = append(cases, words...)
	cases = append(cases, pathCases()...)
	for i := range cases {
		scopeRefs(&cases[i])
	}
	return cases, nil
}

// scopeRefs は case の中の Save と Ref の名前に case の名前を付ける。Python のプロセスは全部の case で 1 つで、保存した値は残るので、
// 解析が例外で終わった case の後ろの Ref が、別の case の同じ名前の値を黙って読む (case どうしが結果を持ち越す)。名前を分ければ、保存できなかった Ref は KeyError になる。
func scopeRefs(c *VisCase) {
	var scope func(v any) any
	scope = func(v any) any {
		switch x := v.(type) {
		case Ref:
			return Ref(string(x) + "@" + c.Name)
		case []any:
			out := make([]any, len(x))
			for i, e := range x {
				out[i] = scope(e)
			}
			return out
		}
		return v
	}
	for i := range c.Calls {
		call := &c.Calls[i]
		if call.Save != "" {
			call.Save += "@" + c.Name
		}
		if call.Args != nil {
			call.Args = scope(call.Args).([]any)
		}
	}
}

// fixtureCase は作業フォルダの fixture を使う case の組み立て。fixture は <<ROOT>>/<name>/ の下に作る (case どうしで同じフォルダを使わない)。
type fixtureCase struct {
	name  string
	calls []Call
}

func newFixtureCase(name string) *fixtureCase {
	f := &fixtureCase{name: name}
	f.op(Op{Kind: "chdir", Path: RootToken})
	f.op(Op{Kind: "mkdir", Path: f.dir()})
	return f
}

func (f *fixtureCase) dir(rel ...string) string {
	return strings.Join(append([]string{RootToken, f.name}, rel...), "/")
}

// op は fixture の操作を足す。不正な UTF-8 を含む write の中身は、JSON の文字列を通ると U+FFFD になるので BytesB64 に直す。
func (f *fixtureCase) op(o Op) *fixtureCase {
	if o.Kind == "write" && o.BytesB64 == "" && !utf8.ValidString(o.Content) {
		o.BytesB64, o.Content = base64.StdEncoding.EncodeToString([]byte(o.Content)), ""
	}
	f.calls = append(f.calls, vop(o))
	return f
}

func (f *fixtureCase) write(rel, content string) *fixtureCase {
	return f.op(Op{Kind: "write", Path: f.dir(rel), Content: content})
}

func (f *fixtureCase) writeBytes(rel string, b []byte) *fixtureCase {
	return f.op(Op{Kind: "write", Path: f.dir(rel), BytesB64: base64.StdEncoding.EncodeToString(b)})
}

func (f *fixtureCase) mkdir(rel string) *fixtureCase {
	return f.op(Op{Kind: "mkdir", Path: f.dir(rel)})
}

func (f *fixtureCase) git(cwdRel string, args ...string) *fixtureCase {
	return f.op(Op{Kind: "git", Path: f.dir(cwdRel), Args: subproc.GitArgs(args...)})
}

func (f *fixtureCase) call(c Call) *fixtureCase {
	f.calls = append(f.calls, c)
	return f
}

func (f *fixtureCase) done() VisCase {
	f.op(Op{Kind: "restoreenv"})
	return VisCase{Name: "fixture/" + f.name, Calls: f.calls}
}

// cps は文字列とサロゲート (rune) を並べて Cps にする。
func cps(parts ...any) Cps {
	var out Cps
	for _, p := range parts {
		switch v := p.(type) {
		case string:
			out = append(out, []rune(v)...)
		case rune:
			out = append(out, v)
		}
	}
	return out
}
