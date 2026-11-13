package oracle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf8"
)

// BoldFile は internal/bold の正解の記録 (testdata/golden/ の中)。本家の yomiyasu_lint.bold_problems の結果。
const BoldFile = "bold.json"

// BoldProblem は bold_problems の dict 1 つ。
type BoldProblem struct {
	Line    int    `json:"line"`
	Found   WStr   `json:"found"`
	Suggest WStr   `json:"suggest"`
	How     string `json:"how"`
}

// BoldCase は 1 つの入力と、本家の答え。入力は File (testdata/corpus/ からの相対パス。中身は記録に書かない) か Text のどちらか。
type BoldCase struct {
	Name            string        `json:"name"`
	File            string        `json:"file,omitempty"`
	SHA256          string        `json:"sha256,omitempty"`
	Text            WStr          `json:"text,omitempty"`
	SkipFrontmatter bool          `json:"skip_frontmatter"`
	Problems        []BoldProblem `json:"problems"`
}

// BoldGolden は bold.json の中身。
type BoldGolden struct {
	Header Header     `json:"header"`
	Cases  []BoldCase `json:"cases"`
}

// Input は case の入力の文字列 (File なら testdata/corpus/ から読み、SHA-256 が記録と合うかを見る)。
func (c BoldCase) Input(corpusDir string) (WStr, error) {
	if c.File == "" {
		return c.Text, nil
	}
	body, err := os.ReadFile(filepath.Join(corpusDir, filepath.FromSlash(c.File)))
	if err != nil {
		return "", err
	}
	if got := SHA256Hex(body); got != c.SHA256 {
		return "", fmt.Errorf("%s の SHA-256 が記録と合いません (記録 %s、今 %s)。make golden で取り直す", c.File, c.SHA256, got)
	}
	return WStr(body), nil
}

// RecordBold は入力の集まりの全ファイルと BoldExtraTexts を、本家の lint と diff の bold_problems に skip_frontmatter の 2 通りで通す。
// 本家の diff は lint と同じ太字のコードを自分で持つ (yomiyasu_diff.py 1923-2685 行)。Go は 1 つ (internal/bold) にまとめるので、
// その根拠として lint と diff の答えが全部の入力で同じことを、記録を作るときに確かめ、違えば止まる。
func (r Runner) RecordBold(ctx context.Context, header Header, corpus Corpus) (BoldGolden, error) {
	var cases []BoldCase
	for _, f := range corpus.Files {
		body, err := os.ReadFile(filepath.Join(corpus.Root, filepath.FromSlash(f.Path)))
		if err != nil {
			return BoldGolden{}, err
		}
		if !utf8.Valid(body) {
			continue // bold_problems は str を取る。UTF-8 でない入力は lint の decode_source で止まり、ここまで来ない
		}
		for _, sf := range []bool{true, false} {
			cases = append(cases, BoldCase{Name: f.Path, File: f.Path, SHA256: f.SHA256, Text: WStr(body), SkipFrontmatter: sf})
		}
	}
	for _, t := range BoldExtraTexts() {
		for _, sf := range []bool{true, false} {
			cases = append(cases, BoldCase{Name: t.Name, Text: WStr(t.Text), SkipFrontmatter: sf})
		}
	}
	var calls []Call
	for _, c := range cases {
		for _, module := range []string{"yomiyasu_lint", "yomiyasu_diff"} {
			calls = append(calls, Call{Module: module, Function: "bold_problems", Args: []any{c.Text, c.SkipFrontmatter}})
		}
	}
	results, err := r.CallFunctions(ctx, calls)
	if err != nil {
		return BoldGolden{}, err
	}
	if len(results) != len(calls) {
		return BoldGolden{}, fmt.Errorf("bold_problems の呼び出し %d 件に対して結果が %d 件でした", len(calls), len(results))
	}
	for k := range cases {
		lint, diff := results[2*k], results[2*k+1]
		if lint.Exception != nil || diff.Exception != nil {
			return BoldGolden{}, fmt.Errorf("%s (skip_frontmatter=%v): bold_problems が例外を投げました (lint %v / diff %v)", cases[k].Name, cases[k].SkipFrontmatter, lint.Exception, diff.Exception)
		}
		if !bytes.Equal(lint.Result, diff.Result) {
			return BoldGolden{}, fmt.Errorf("%s (skip_frontmatter=%v): 本家の lint と diff の bold_problems の答えが違います", cases[k].Name, cases[k].SkipFrontmatter)
		}
		if err := json.Unmarshal(lint.Result, &cases[k].Problems); err != nil {
			return BoldGolden{}, fmt.Errorf("%s: bold_problems の結果を読めません: %w", cases[k].Name, err)
		}
		if cases[k].Problems == nil {
			cases[k].Problems = []BoldProblem{}
		}
		if cases[k].File != "" {
			cases[k].Text = ""
		}
	}
	return BoldGolden{Header: header, Cases: cases}, nil
}

// MarshalBold は記録を書き出す形にする。
func MarshalBold(g BoldGolden) ([]byte, error) {
	b, err := json.MarshalIndent(g, "", " ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// ReadBoldGolden は bold.json を読む (試験から使う)。
func ReadBoldGolden(goldenDir string) (BoldGolden, error) {
	var g BoldGolden
	err := ReadGolden(goldenDir, BoldFile, &g)
	return g, err
}
