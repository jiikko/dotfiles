package card

import "testing"

// 検索語は題名・カード ID・issue・依頼の原文を引き、空白で区切った語はすべて含むときだけ一致する (issue 532)。
func TestMatches(t *testing.T) {
	c := Card{ID: "C-085", Title: "カードを検索する", Request: "外から動かす Claude の確認",
		Issues: []IssueRef{{Repo: "dotfiles", Number: 53}}}
	for q, want := range map[string]bool{
		"":             true, // 打ち始め
		"検索":           true, // 題名
		"c-085":        true, // ID (大文字小文字を区別しない)
		"claude":       true, // 依頼の原文
		"dotfiles#053": true, // issue (String の形)
		"#53":          true, // issue (桁を揃えない形)
		"検索 claude":    true, // 語は欄をまたいで AND
		"検索 zsh":       false,
		"C-086":        false,
		"検索する外から":      false, // 欄の繋ぎ目をまたいで一致しない
	} {
		if got := c.Matches(q); got != want {
			t.Errorf("Matches(%q) = %v, want %v", q, got, want)
		}
	}
}

// HasIssue は repo が空なら番号だけで見る。
func TestHasIssue(t *testing.T) {
	c := Card{Issues: []IssueRef{{Repo: "dotfiles", Number: 532}}}
	if !c.HasIssue("", 532) || !c.HasIssue("dotfiles", 532) || c.HasIssue("obaket", 532) || c.HasIssue("", 53) {
		t.Fatal("HasIssue の判定が違う")
	}
}
