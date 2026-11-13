package oracle

import (
	"context"
	"fmt"
	"strings"
)

// LintRawFile は本家 lint の検出の生の並び (種類・allow・ダッシュの上限で外す前の候補。内部の欄 _words などを含む) の記録 (testdata/golden/ の中)。
// 群ごとの検出を、規則 ID で絞って突き合わせるための基準 (CLI の単位の記録は、40 規則が全部入るまで比べられない)。
const LintRawFile = "lint-raw.json"

// LintRawContext は検出の生の並びを取る文脈。DocType が nil は種類なし。Words は語の一覧 (words/) を使う。
// 種類は、検出そのものが変わるものだけを選んである: pause_comma を当てる種類か (種類なし・promo・script は当てない)・essay (pause_comma の目的語を数えない)・
// slides (slide_comma)・script (dialogue_period)・minutes と promo と script と essay と slides (ai_phrase の型)・promo (copy_comma_noun の数え方)。
// article は「ほかの種類」の代表 (種類で検出が変わらない)。
type LintRawContext struct {
	Name    string
	DocType *string
	Words   bool
}

// LintRawContexts は検出の生の並びを取る文脈の一覧。
func LintRawContexts() []LintRawContext {
	dt := func(s string) *string { return &s }
	return []LintRawContext{
		{Name: "none"}, {Name: "essay", DocType: dt("essay")}, {Name: "slides", DocType: dt("slides")}, {Name: "promo", DocType: dt("promo")},
		{Name: "script", DocType: dt("script")}, {Name: "minutes", DocType: dt("minutes")}, {Name: "article", DocType: dt("article")}, {Name: "words", Words: true},
	}
}

// LintRawCase は driver への指示 1 件 (入力のファイルは LintCLIFiles の corpus/ の下)。
type LintRawCase struct {
	Name    string  `json:"name"`
	Path    string  `json:"path"`
	DocType *string `json:"doc_type"`
	Words   string  `json:"words,omitempty"`
}

// LintRawRecord は 1 件の記録。Metrics は結果の metrics の欄 (文書の指標。検出が空でも出力に出る) の digest。Findings は「規則:行:digest」を空白でつないだもの (検出の順)。digest は内部の欄を含む候補の dict の正規の JSON の SHA-256 の先頭 8 字。
type LintRawRecord struct {
	Name     string `json:"name"`
	N        int    `json:"n"`
	Metrics  string `json:"metrics"`
	Findings string `json:"findings"`
}

// CheckCount は候補の数 N と Findings の並びの長さが合うか (片方だけ欠けた・壊れた記録を読まない)。
func (rec LintRawRecord) CheckCount() error {
	if n := len(strings.Fields(rec.Findings)); n != rec.N {
		return fmt.Errorf("記録 %s の候補の数 n=%d と並びの長さ %d が違います", rec.Name, rec.N, n)
	}
	return nil
}

// LintRawGolden は lint-raw.json の中身。
type LintRawGolden struct {
	Header Header          `json:"header"`
	Cases  []LintRawRecord `json:"cases"`
}

// MarshalLintRaw は LintRawGolden を決まったバイト列にする。
func MarshalLintRaw(g LintRawGolden) ([]byte, error) { return marshalHeaderCases(g.Header, g.Cases) }

// LintRawCases は入力の集まりの全ファイルと、入力の集まりに足りない形の入力 (lintRawGapInputs。rawgap/ の下) を、LintRawContexts の全部の文脈で通す case の一覧。名前は「パス|文脈の名前」。
func LintRawCases(c Corpus) []LintRawCase {
	var cases []LintRawCase
	add := func(name, path string) {
		for _, ctx := range LintRawContexts() {
			rc := LintRawCase{Name: name + "|" + ctx.Name, Path: path, DocType: ctx.DocType}
			if ctx.Words {
				rc.Words = "words"
			}
			cases = append(cases, rc)
		}
	}
	for _, f := range c.Files {
		add(f.Path, "corpus/"+f.Path)
	}
	for _, name := range lintRawGapNames() {
		add(lintRawGapDir+name+".md", lintRawGapDir+name+".md")
	}
	return cases
}

// RecordLintRaw は cases を、files を置いた作業フォルダで本家の lint_text に通して記録にする。
func (r Runner) RecordLintRaw(ctx context.Context, header Header, cases []LintRawCase, files map[string]string) (LintRawGolden, error) {
	sb, err := r.newStagedSandbox()
	if err != nil {
		return LintRawGolden{}, err
	}
	defer sb.remove()
	if err := placeFiles(sb.cwd, files); err != nil {
		return LintRawGolden{}, err
	}
	in := struct {
		ScriptsDir string        `json:"scripts_dir"`
		Cases      []LintRawCase `json:"cases"`
	}{sb.tree.Path(ScriptsDir), cases}
	var out []LintRawRecord
	if err := r.runDriver(ctx, sb, "record_lint_raw.py", in, &out); err != nil {
		return LintRawGolden{}, err
	}
	if len(out) != len(cases) {
		return LintRawGolden{}, fmt.Errorf("lint_text の実行 %d 件に対して記録が %d 件でした", len(cases), len(out))
	}
	for i, rec := range out {
		if rec.Name != cases[i].Name {
			return LintRawGolden{}, fmt.Errorf("%d 番目の記録の名前 %q が case の名前 %q と違います", i, rec.Name, cases[i].Name)
		}
		if err := rec.CheckCount(); err != nil {
			return LintRawGolden{}, err
		}
	}
	return LintRawGolden{Header: header, Cases: out}, nil
}

// marshalHeaderCases は header と cases だけを持つ記録 (lint-cli・lint-raw・lint-post) を決まったバイト列にする (case ごとに 1 行)。
func marshalHeaderCases[T any](h Header, cases []T) ([]byte, error) {
	header, err := marshalField("header", h)
	if err != nil {
		return nil, err
	}
	lines, err := marshalLines(cases)
	if err != nil {
		return nil, err
	}
	return []byte("{\n" + header + ",\n\"cases\": " + lines + "\n}\n"), nil
}
