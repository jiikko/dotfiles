package oracle

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
)

// LintCLIFile は本家 lint の CLI の単位の記録 (testdata/golden/ の中)。入力の集まりの全ファイルを、種類・読者・語の一覧・text と --json・終了コードの条件で通した
// stdout (SHA-256)・stderr・rc。Go の lint.Main の出力 (全体のバイト列) と rc の突き合わせの基準になる。
const LintCLIFile = "lint-cli.json"

// LintCLICase は CLI の 1 回の実行。Cwd は作業フォルダ (記録の root からの相対。空なら root)。Stdin は標準入力のバイト列。
// Keep は stdout の全文も記録に残す (小さい出力の実例と、ヘルプなどの 1 回きりの出力。残さない呼び出しは SHA-256 だけ)。
type LintCLICase struct {
	Name  string   `json:"name"`
	Args  []string `json:"args"`
	Cwd   string   `json:"cwd,omitempty"`
	Stdin []byte   `json:"-"`
	Keep  bool     `json:"keep,omitempty"`
}

// MarshalJSON は driver への指示の形にする (Stdin は base64)。
func (c LintCLICase) MarshalJSON() ([]byte, error) {
	type plain LintCLICase
	if c.Args == nil {
		c.Args = []string{}
	}
	return json.Marshal(struct {
		plain
		StdinB64 string `json:"stdin_b64,omitempty"`
	}{plain(c), base64.StdEncoding.EncodeToString(c.Stdin)})
}

// LintCLIRecord は 1 回の実行の記録。Out は stdout (作業フォルダの実パスを RootToken にしたもの) の SHA-256 の先頭 16 字、Len はそのバイト数。
// N は出力に出た見直し候補の数 (出さない実行は nil)、Raw は検出の生の並び (種類・allow・ダッシュの上限で外す前) の件数 (検査まで進まなかった実行は nil)。
type LintCLIRecord struct {
	Name   string `json:"name"`
	RC     int    `json:"rc"`
	Out    string `json:"out"`
	Len    int    `json:"len"`
	Err    string `json:"err"`
	N      *int   `json:"n"`
	Raw    *int   `json:"raw,omitempty"`
	Stdout string `json:"stdout,omitempty"`
}

// LintCLIGolden は lint-cli.json の中身。
type LintCLIGolden struct {
	Header Header          `json:"header"`
	Cases  []LintCLIRecord `json:"cases"`
}

// LintCLIDigestWidth は Out の字数 (SHA-256 の 16 進。16 字 = 64 bit。出力 1 つあたりの記録の大きさと、取り違えに当たる確率 2^-64 の釣り合い)。
const LintCLIDigestWidth = 16

// MarshalLintCLI は LintCLIGolden を決まったバイト列にする (case ごとに 1 行)。
func MarshalLintCLI(g LintCLIGolden) ([]byte, error) { return marshalHeaderCases(g.Header, g.Cases) }

// RecordLintCLI は cases を、files を置いた作業フォルダで本家の CLI に通して記録にする。1 つの Python のプロセスで順に走らせる。
// 終了コード 3 (内部の誤り) の実行があれば止める (本家が壊れた入力は記録にしない)。
func (r Runner) RecordLintCLI(ctx context.Context, header Header, cases []LintCLICase, files map[string]string) (LintCLIGolden, error) {
	sb, err := r.newStagedSandbox()
	if err != nil {
		return LintCLIGolden{}, err
	}
	defer sb.remove()
	if err := placeFiles(sb.cwd, files); err != nil {
		return LintCLIGolden{}, err
	}
	in := struct {
		ScriptsDir string        `json:"scripts_dir"`
		Cases      []LintCLICase `json:"cases"`
	}{sb.tree.Path(ScriptsDir), cases}
	var out []LintCLIRecord
	if err := r.runDriver(ctx, sb, "record_lint_cli.py", in, &out); err != nil {
		return LintCLIGolden{}, err
	}
	if len(out) != len(cases) {
		return LintCLIGolden{}, fmt.Errorf("CLI の実行 %d 件に対して記録が %d 件でした", len(cases), len(out))
	}
	for i, rec := range out {
		if rec.Name != cases[i].Name {
			return LintCLIGolden{}, fmt.Errorf("%d 番目の記録の名前 %q が case の名前 %q と違います", i, rec.Name, cases[i].Name)
		}
		if rec.RC == 3 {
			return LintCLIGolden{}, fmt.Errorf("case %s の本家が終了コード 3 で終わりました: %s", rec.Name, rec.Err)
		}
	}
	return LintCLIGolden{Header: header, Cases: out}, nil
}

// LintCLIFiles は CLI の記録の作業フォルダに置くファイル (作業フォルダからの相対パス → 中身)。入力の集まりは corpus/、語の一覧の見本は words/ に置く。
// Go のテストも同じ一覧を同じ場所に置いて、同じ引数で Main を呼ぶ。
func LintCLIFiles(c Corpus, wordsDir string) (map[string]string, error) {
	files := map[string]string{}
	for _, f := range c.Files {
		body, err := c.Read(f.Path)
		if err != nil {
			return nil, err
		}
		files["corpus/"+f.Path] = body
	}
	words, err := readDirFiles(wordsDir)
	if err != nil {
		return nil, err
	}
	for name, body := range words {
		files["words/"+name] = body
		files["withwords/.yomiyasu-words/"+name] = body
		files["gitroot/.yomiyasu-words/"+name] = body
	}
	for path, body := range lintCLIExtraFiles {
		files[path] = body
	}
	return files, nil
}

// lintCLIExtraFiles は誤りの入力と、語の一覧を探す場所の見本。
var lintCLIExtraFiles = map[string]string{
	".hidden.md":                     "隠しファイルです。\n",
	"badutf8.md":                     "壊れた\xff入力です。\n",
	"empty.md":                       "",
	"bom.md":                         "\xef\xbb\xbf# 見出し\n\n本文です。\r\n",
	"sub/note.md":                    "サブフォルダの文書です。\n",
	"gitroot/.git/HEAD":              "ref: refs/heads/main\n",
	"gitroot/sub/note.md":            "git の最上位の下の文書です。\n",
	"badwords/flag.txt":              "a\tb\tc\n",
	"emptywords/allow.txt":           "",
	"onlypatterns/flag-patterns.txt": "名前\t語\n",
}

// lintCLITextInput は標準入力の実例 (語と候補のある短い文書)。
const lintCLITextInput = "# まとめ\n\n本記事では設定を扱います🚀ポイントは3つです。\n\n- 項目です。\n- 項目です。\n"

// LintCLICases は CLI の記録の case の一覧 (入力の集まりの全ファイルと、条件・誤り・標準入力・作業フォルダの実例)。名前は記録と突き合わせる鍵。
func LintCLICases(c Corpus, docTypes, readers []string) []LintCLICase {
	var cases []LintCLICase
	add := func(name string, keep bool, args ...string) {
		cases = append(cases, LintCLICase{Name: name, Args: args, Keep: keep})
	}
	for i, f := range c.Files {
		file := "corpus/" + f.Path
		add(f.Path+"|json", false, "--json", file)
		for _, dt := range docTypes {
			add(f.Path+"|json|dt="+dt, false, "--json", "--doc-type", dt, file)
		}
		add(f.Path+"|json|words", false, "--json", "--words", "words", file)
		add(f.Path+"|json|no-words", false, "--json", "--no-words", file)
		if i < 3 {
			for _, rd := range readers {
				add(f.Path+"|json|reader="+rd, false, "--json", "--reader", rd, file)
			}
		}
		add(f.Path+"|text", false, file)
		add(f.Path+"|text|slides+expert", false, "--doc-type", "slides", "--reader", "expert", file)
		add(f.Path+"|text|words", false, "--words", "words", file)
		add(f.Path+"|text|strict", false, "--strict", file)
		add(f.Path+"|text|fail-on-warn", false, "--fail-on", "warn", file)
		add(f.Path+"|text|fail-on-error", false, "--fail-on", "error", file)
		add(f.Path+"|text|fail-on-mixed", false, "--fail-on=WARN,error", "--strict", file)
		add(f.Path+"|json|fail-on-warn", false, "--fail-on", "warn", "--json", file)
	}
	first := "corpus/" + c.Files[0].Path
	for _, s := range []struct {
		name string
		args []string
	}{
		{"version", []string{"--version"}},
		{"help", []string{"-h"}},
		{"help-long", []string{"--help"}},
		{"help-joined", []string{"-hx"}},
		{"help-with-json", []string{"--json", "-h"}},
		{"help-after-error", []string{"--doc-type", "-h"}},
		{"version-with-bogus", []string{"--version", "--bogus"}},
		{"version-with-bad-fail-on", []string{"--version", "--fail-on=x"}},
		{"fail-on-meaning-risk", []string{"--fail-on=meaning-risk", first}},
		{"fail-on-unknown", []string{"--fail-on=x", first}},
		{"fail-on-empty", []string{"--fail-on=", first}},
		{"fail-on-blank", []string{"--fail-on", " ", first}},
		{"fail-on-commas", []string{"--fail-on", ",,", first}},
		{"fail-on-unknown-then-meaning-risk", []string{"--fail-on=x,meaning-risk", first}},
		{"fail-on-two-unknown", []string{"--fail-on=y", "--fail-on=x", first}},
		{"fail-on-underscore-case", []string{"--fail-on=MEANING_RISK", first}},
		{"fail-on-non-ascii", []string{"--fail-on=ÀB", first}},
		{"fail-on-missing", []string{"--fail-on"}},
		{"fail-on-dash-value", []string{"--fail-on", "-", first}},
		{"fail-on-option-like-value", []string{"--fail-on", "-x", first}},
		{"fail-on-list", []string{"--fail-on", "warn,error", "--fail-on", "error", first}},
		{"strict-twice", []string{"--strict", "--strict", first}},
		{"doc-type-unknown", []string{"--doc-type", "zzz", first}},
		{"doc-type-empty", []string{"--doc-type=", first}},
		{"doc-type-blank", []string{"--doc-type", " ", first}},
		{"doc-type-case", []string{"--doc-type", "SLIDES", first}},
		{"doc-type-fullwidth", []string{"--doc-type", "ｓｌｉｄｅｓ", first}},
		{"doc-type-missing", []string{first, "--doc-type"}},
		{"reader-unknown", []string{"--reader", "zzz", first}},
		{"reader-empty", []string{"--reader=", first}},
		{"reader-case", []string{"--reader", "Expert", first}},
		{"doc-type-and-reader-both-bad", []string{"--doc-type", "zzz", "--reader", "zzz", first}},
		{"fail-on-and-doc-type-both-bad", []string{"--fail-on=x", "--doc-type", "zzz", first}},
		{"words-and-no-words", []string{"--words", "words", "--no-words", first}},
		{"words-and-no-words-with-bad-doc-type", []string{"--words", "words", "--no-words", "--doc-type", "zzz", first}},
		{"words-missing-folder", []string{"--words", "nowhere", first}},
		{"words-empty-value", []string{"--words=", first}},
		{"words-is-file", []string{"--words", "words/allow.txt", first}},
		{"words-bad-flag", []string{"--words", "badwords", first}},
		{"words-empty-allow", []string{"--words", "emptywords", first}},
		{"words-only-patterns", []string{"--words", "onlypatterns", first}},
		{"words-folder-without-lists", []string{"--words", "sub", first}},
		{"words-missing-arg", []string{"--words"}},
		{"words-then-json", []string{"--words", "words", "--json", first}},
		{"abbreviated-option", []string{"--js", first}},
		{"unknown-option", []string{"--bogus", first}},
		{"unknown-short-option", []string{"-x", first}},
		{"unknown-option-with-value", []string{"--bogus=1", first}},
		{"two-files", []string{first, first}},
		{"two-unknown", []string{"--a", "--b"}},
		{"json-with-value", []string{"--json=x", first}},
		{"strict-with-value", []string{"--strict=x", first}},
		{"help-with-value", []string{"--help=x", first}},
		{"file-after-options", []string{first, "--json"}},
		{"double-dash-file", []string{"--", first}},
		{"double-dash-option-as-file", []string{first, "--", "--json"}},
		{"negative-number-as-file", []string{"-1"}},
		{"missing-file", []string{"corpus/nowhere.md"}},
		{"missing-file-with-trailing-slash", []string{first + "/"}},
		{"directory", []string{"corpus"}},
		{"hidden-file", []string{".hidden.md"}},
		{"outside-cwd", []string{"../x.md"}},
		{"absolute-outside", []string{"/etc/hosts"}},
		{"bad-utf8-file", []string{"badutf8.md"}},
		{"empty-file", []string{"--json", "empty.md"}},
		{"empty-file-text", []string{"empty.md"}},
		{"bom-crlf-file", []string{"--json", "bom.md"}},
		{"subfolder-file", []string{"--json", "sub/note.md"}},
		{"file-with-dot-segment", []string{"./sub/note.md"}},
		{"file-with-dotdot-inside", []string{"sub/../sub/note.md"}},
		{"file-with-doc-type-and-bad-file", []string{"--doc-type", "slides", "corpus/nowhere.md"}},
	} {
		cases = append(cases, LintCLICase{Name: "scenario/" + s.name, Args: s.args, Keep: true})
	}
	stdinCases := []struct {
		name  string
		args  []string
		stdin string
		cwd   string
	}{
		{"stdin-implicit", nil, lintCLITextInput, ""},
		{"stdin-dash", []string{"-"}, lintCLITextInput, ""},
		{"stdin-json", []string{"--json"}, lintCLITextInput, ""},
		{"stdin-empty-json", []string{"--json"}, "", ""},
		{"stdin-empty-text", nil, "", ""},
		{"stdin-bad-utf8", nil, "壊れた\xff入力です。\n", ""},
		{"stdin-bom-crlf", []string{"--json"}, "\xef\xbb\xbf# 見出し\r\n\r\n本文です。\r\n", ""},
		{"stdin-strict", []string{"--strict", "--json"}, lintCLITextInput, ""},
		{"stdin-ignored-with-file", []string{"--json", first}, lintCLITextInput, ""},
		{"stdin-doc-type", []string{"--doc-type", "slides"}, lintCLITextInput, ""},
		{"cwd-withwords", []string{"--json"}, lintCLITextInput, "withwords"},
		{"cwd-withwords-text", nil, lintCLITextInput, "withwords"},
		{"cwd-withwords-no-words", []string{"--json", "--no-words"}, lintCLITextInput, "withwords"},
		{"cwd-gitroot-sub", []string{"--json"}, lintCLITextInput, "gitroot/sub"},
		{"cwd-gitroot-sub-file", []string{"--json", "note.md"}, "", "gitroot/sub"},
		{"cwd-sub-no-words-folder", []string{"--json"}, lintCLITextInput, "sub"},
		{"cwd-badwords-explicit", []string{"--words", "../badwords"}, lintCLITextInput, "sub"},
	}
	for _, s := range stdinCases {
		cases = append(cases, LintCLICase{Name: "scenario/" + s.name, Args: s.args, Stdin: []byte(s.stdin), Cwd: s.cwd, Keep: true})
	}
	return cases
}

// SortedLintCLINames はテストが記録と case の一覧を突き合わせるための、case の名前の昇順。
func SortedLintCLINames(cases []LintCLICase) []string {
	names := make([]string, len(cases))
	for i, c := range cases {
		names[i] = c.Name
	}
	sort.Strings(names)
	return names
}
