package card

import (
	"fmt"
	"strings"
)

// Matches はカードが検索語 q に一致するか (画面の / と `card list --grep` が同じこれを使う。issue 532)。
// q を空白で区切った語が**すべて**、題名・カード ID・issue (dotfiles#532 / #532)・依頼の原文のどれかに含まれれば一致。
// 大文字と小文字は区別しない (c-085 でも C-085 を引ける)。空の q はすべてに一致する (打ち始めでボードを空にしない)。
func (c Card) Matches(q string) bool {
	hay := strings.ToLower(c.searchText())
	for _, w := range strings.Fields(strings.ToLower(q)) {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}

// searchText は Matches が引く欄を改行で繋いだもの (語が欄をまたいで一致しないよう、語に入らない改行で区切る)。
func (c Card) searchText() string {
	parts := []string{c.ID, c.Title, c.Request}
	for _, r := range c.Issues {
		// String は番号を 3 桁に揃える (dotfiles#053)。#53 と打っても引けるよう、揃えない形も持つ
		parts = append(parts, r.String(), fmt.Sprintf("%s#%d", r.Repo, r.Number))
	}
	return strings.Join(parts, "\n")
}

// HasIssue はカードが issue を指しているか。repo が空なら番号だけで見る (`card list --issue 532`)。
func (c Card) HasIssue(repo string, number int) bool {
	for _, r := range c.Issues {
		if r.Number == number && (repo == "" || r.Repo == repo) {
			return true
		}
	}
	return false
}
