package backend

import (
	"strings"
	"testing"
)

// repo のタブから出した依頼は、その repo の名前とパスと「外を触らない」を前置きし、本文は書いたまま末尾に置く。
func TestPMPromptScopesToRepo(t *testing.T) {
	p := PMPrompt(Repo{Name: "dotfiles", Path: "/Users/k/dotfiles"}, "glogx の検索を足して")
	for _, want := range []string{"dotfiles", "/Users/k/dotfiles", "外のファイルを読んだり変更したりしない"} {
		if !strings.Contains(p, want) {
			t.Fatalf("前置きに %q が無い:\n%s", want, p)
		}
	}
	if !strings.HasSuffix(p, "\n依頼:\nglogx の検索を足して") {
		t.Fatalf("本文が書いたまま末尾に無い:\n%s", p)
	}
}

// global のタブから出した依頼は repo を決めつけない (PM に判断させる)。
func TestPMPromptGlobalLeavesRepoToPM(t *testing.T) {
	p := PMPrompt(Repo{}, "どこかの repo の件")
	if !strings.Contains(p, "repo を指定していません") || strings.Contains(p, "スコープは repo") {
		t.Fatalf("global の依頼の前置きが違う:\n%s", p)
	}
}

// issue の依頼はパスと番号とタイトル、epic は親と未完了の子の一覧を入れる。補足は書いたまま末尾に置く。
func TestIssuePrompt(t *testing.T) {
	p := IssuePrompt(IssueTarget{Number: 415, Title: "設計", Path: "/r/issues/415-d.md"}, "急ぎで")
	if !strings.Contains(p, "/r/issues/415-d.md") || !strings.Contains(p, "#415 設計") || !strings.HasSuffix(p, "補足:\n急ぎで") {
		t.Fatalf("issue の指示が違う:\n%s", p)
	}
	e := IssuePrompt(IssueTarget{Number: 200, Title: "親", Path: "/r/e/200.md", Epic: "200", Children: []string{"/r/e/201.md"}}, "")
	if !strings.Contains(e, "epic 200") || !strings.Contains(e, "\n- /r/e/201.md") || strings.Contains(e, "補足") {
		t.Fatalf("epic の指示が違う:\n%s", e)
	}
}

// issue から出した依頼の原文は、補足が無くても「issue の本文を進める」の 1 文で空にしない。補足は書いたまま後ろに置く。
// 題名に番号を付けず (紐づけた issue から出る。issue 491)、issue を紐づける。epic は親 issue を紐づける (issue 511)。
func TestIssueRequest(t *testing.T) {
	title, req, issues := IssueRequest("dotfiles", IssueTarget{Number: 505, Title: "入れ替わりを直す", Path: "/r/issues/505.md"}, "")
	if title != "入れ替わりを直す" || req != "issue #505 の本文に書かれていることを進める" || len(issues) != 1 || issues[0].Repo != "dotfiles" || issues[0].Number != 505 {
		t.Fatalf("issue の依頼: %q %q %+v", title, req, issues)
	}
	if _, req, _ := IssueRequest("dotfiles", IssueTarget{Number: 505}, "急ぎで\n2 行目"); req != "issue #505 の本文に書かれていることを進める\n急ぎで\n2 行目" {
		t.Fatalf("補足を書いたまま後ろに置かない: %q", req)
	}
	title, req, issues = IssueRequest("dotfiles", IssueTarget{Number: 415, Title: "設計", Path: "/r/e/415.md", Epic: "415", Children: []string{"/r/e/416.md"}}, "")
	if title != "設計" || !strings.Contains(req, "epic 415 (親 issue #415)") || len(issues) != 1 || issues[0].Number != 415 {
		t.Fatalf("epic の依頼: %q %q %+v", title, req, issues)
	}
}
