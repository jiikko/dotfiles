package main

import (
	"fmt"
	"strings"
	"testing"
)

// 打ち切ったときだけ、先頭に全件の件数・大きさと rg の提示を出す。提示は dupscan 自身が実行するものではない。

func manyItems(n int) map[string]string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "/// listItem%d は %d 番目の項目を返す。あとの文は表示から落ちる。\nfunc listItem%d() {}\n", i, i, i)
	}
	return map[string]string{"Items.swift": b.String()}
}

func TestIndexTruncationHeaderOnlyWhenTruncated(t *testing.T) {
	root := repoWith(t, manyItems(5))
	_, out, _ := index(t, "-name", "listItem", "-limit", "2", root)
	rows := lines(out)
	// 先頭は構文近似の 1 行、次が件数ヘッダ
	if len(rows) < 4 || !strings.HasPrefix(rows[1], "該当 5 件 (2 件表示 / 全部で約 ") || !strings.Contains(rows[1], "rg -n -i -e listItem -g '*.swift' -- ") {
		t.Fatalf("truncated output must start with the header: %q", out)
	}
	if !strings.Contains(rows[1], "コメント含む") {
		t.Errorf("the rg suggestion must say it is a text match: %q", rows[1])
	}
	if !strings.Contains(out, "… 他 3 件") {
		t.Errorf("the tail stays: %q", out)
	}
	for _, args := range [][]string{{"-limit", "5"}, {"-limit", "0"}, {"-limit", "9"}} {
		_, out, _ := index(t, append([]string{"-name", "listItem"}, append(args, root)...)...)
		if strings.Contains(out, "件表示") || strings.Contains(out, "rg -n") {
			t.Errorf("no header when nothing is cut (%v): %q", args, out)
		}
	}
}

func TestIndexRowsShowRangeAndShortDoc(t *testing.T) {
	root := repoWith(t, manyItems(1))
	_, out, _ := index(t, "-name", "listItem0", root)
	if strings.Contains(out, "あとの文") || !strings.Contains(out, "// listItem0 は 0 番目の項目を返す。\n") {
		t.Errorf("doc is cut at the first sentence by default: %q", out)
	}
	_, full, _ := index(t, "-name", "listItem0", "-doc-bytes", "0", root)
	if !strings.Contains(full, "あとの文は表示から落ちる。") {
		t.Errorf("-doc-bytes 0 keeps the whole first line: %q", full)
	}
	_, short, _ := index(t, "-name", "listItem0", "-doc-bytes", "30", root)
	if !strings.Contains(short, "…\n") {
		t.Errorf("a small -doc-bytes cuts with an ellipsis: %q", short)
	}
	// JSON は全文のまま (text の縮めは表示だけ)。終端行は単位の範囲を示す
	_, js, _ := index(t, "-name", "listItem0", "-format", "json", root)
	if !strings.Contains(js, "あとの文は表示から落ちる。") || !strings.Contains(js, `"end_line"`) {
		t.Errorf("json keeps the full doc and adds end_line: %q", js)
	}
}

func TestIndexDocFilterStillMatchesTheCutPart(t *testing.T) {
	root := repoWith(t, manyItems(1))
	// 表示では落ちる 2 文目の語でも、-doc の絞り込みは全文で当たる
	_, out, _ := index(t, "-doc", "あとの文", root)
	if !strings.Contains(out, "listItem0") {
		t.Errorf("-doc matches the whole first line, not the shortened display: %q", out)
	}
}

func TestIndexTruncationHintIsSafeForOddDirsAndNames(t *testing.T) {
	// - で始まる dir や空白を含む dir を、rg のオプション・複数の語に読ませない (-e と -- と引用符)
	root := repoWith(t, map[string]string{"-odd dir/Items.swift": manyItems(5)["Items.swift"]})
	t.Chdir(root)
	_, out, _ := index(t, "-name", "listItem", "-limit", "2", "--", "-odd dir")
	rows := lines(out)
	if len(rows) < 2 || !strings.Contains(rows[1], "rg -n -i -e listItem -g '*.swift' -- '-odd dir'") {
		t.Fatalf("the suggestion pins the pattern with -e and the paths after -- (quoted): %q", out)
	}
}

func TestIndexHeaderWithoutNameDoesNotAdviseAddingWords(t *testing.T) {
	root := repoWith(t, manyItems(5))
	_, out, _ := index(t, "-doc", "番目", "-limit", "2", root)
	rows := lines(out)
	if len(rows) < 2 || !strings.HasPrefix(rows[1], "該当 5 件 (2 件表示 / 全部で約 ") || strings.Contains(rows[1], "語を足して絞る") || strings.Contains(rows[1], "rg ") {
		t.Errorf("a query without a name has no rg suggestion and no 'add words' advice in the header: %q", out)
	}
}

func TestCallersTruncationHint(t *testing.T) {
	var b strings.Builder
	b.WriteString("func target() {}\n")
	for i := range 4 {
		fmt.Fprintf(&b, "func use%d() { target() }\n", i)
	}
	root := repoWith(t, map[string]string{"Calls.swift": b.String()})
	_, out, _ := callers(t, "target", "-limit", "2", root)
	rows := lines(out)
	if len(rows) < 3 || !strings.HasPrefix(rows[1], "呼び出し 4 件 (2 件表示 / 全部で約 ") || !strings.Contains(rows[1], `rg -n -e 'target\(' -g '*.swift' -- `) {
		t.Fatalf("callers header: %q", out)
	}
	if strings.Contains(rows[1], " -i ") {
		t.Errorf("callers match case-sensitively; the suggestion must not add -i: %q", rows[1])
	}
	if _, out, _ := callers(t, "target", root); strings.Contains(out, "件表示") {
		t.Errorf("no header when nothing is cut: %q", out)
	}
}

// 先頭の案内は `| grep` で削れるので、打ち切った事実は stderr にも出る (打ち切っていなければ出ない)。
func TestTruncationNoticeOnStderr(t *testing.T) {
	var b strings.Builder
	b.WriteString("func target() {}\n")
	for i := range 4 {
		fmt.Fprintf(&b, "func use%d() { target() }\n", i)
	}
	root := repoWith(t, map[string]string{"Calls.swift": b.String()})
	_, _, errs := callers(t, "target", "-limit", "2", root)
	if !strings.Contains(errs, "dupscan callers: 4 件中 2 件で打ち切り。全件は -limit 0") {
		t.Errorf("callers truncation notice on stderr: %q", errs)
	}
	_, _, errs = index(t, "-name", "listItem", "-limit", "2", repoWith(t, manyItems(5)))
	if !strings.Contains(errs, "dupscan index: 5 件中 2 件で打ち切り") {
		t.Errorf("index truncation notice on stderr: %q", errs)
	}
	for _, limit := range []string{"0", "9"} {
		_, _, errs = callers(t, "target", "-limit", limit, root)
		if strings.Contains(errs, "打ち切り") {
			t.Errorf("no notice when nothing is cut (limit %s): %q", limit, errs)
		}
	}
}
