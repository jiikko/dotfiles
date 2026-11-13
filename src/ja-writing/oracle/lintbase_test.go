package oracle

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

func loadLintBase(t *testing.T) LintBaseGolden {
	t.Helper()
	var g LintBaseGolden
	if err := ReadGolden(goldenDir, LintBaseFile, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

// 生成した絵文字の表は、記録 (lint-base.json) から今の生成の規則で作り直したものと同じ。記録だけ作り直して表を作り忘れた・表を手で書き換えた、のどちらでも落ちる。
func TestLintEmojiTablesMatchGolden(t *testing.T) {
	want, err := GenerateLintEmojiTables(loadLintBase(t))
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../" + LintEmojiTablesPath)
	if err != nil {
		t.Fatalf("表がありません。make golden で作る: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s が lint-base.json から作り直したものと違う (%d バイトと %d バイト)。make golden で作り直す", LintEmojiTablesPath, len(got), len(want))
	}
}

// 記録の入力にする文字列 (本家へ渡す closers の引数) は、記録した本家の定数と同じ字。
func TestLintCaseInputsMatchRecordedConstants(t *testing.T) {
	g := loadLintBase(t)
	constant := func(name string) json.RawMessage {
		t.Helper()
		raw, err := LintBaseConstant(g, name)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	var closers string
	if err := json.Unmarshal(constant("_END_CLOSERS"), &closers); err != nil || closers != lintEndClosers {
		t.Errorf("lintEndClosers %q が本家の _END_CLOSERS %q と違う (%v)", lintEndClosers, closers, err)
	}
	var brackets map[string]string
	if err := json.Unmarshal(constant("_END_BRACKETS"), &brackets); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for k := range brackets {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	want := strings.Split(lintBracketClosers, "")
	slices.Sort(want)
	if !slices.Equal(keys, want) {
		t.Errorf("lintBracketClosers %q が本家の _END_BRACKETS の鍵 %q と違う", lintBracketClosers, keys)
	}
}
