package store

import (
	"strings"
	"testing"
	"time"

	"pro-con/card"
)

// 設定に無い repo (パスで書いた・知らない名前) のカードを作る依頼は、箱に手で置かれても記録に入れない (issue 511)。
// plan が issue の repo をカードの repo にするときも同じ。前から設定に無い repo のカードへの削除は受ける。
func TestApplyRejectsUnknownRepo(t *testing.T) {
	repos := map[string]string{"dotfiles": "/Users/k/dotfiles"}
	dir := t.TempDir()
	submit(t, dir, Request{Kind: "add", Title: "パス", Repo: "/Users/k/dotfiles", At: t0})
	submit(t, dir, Request{Kind: "add", Title: "名前", Repo: "dotfiles", At: t0.Add(time.Second)})
	submit(t, dir, Request{Kind: "add", Title: "global", At: t0.Add(2 * time.Second)})
	res, err := Apply(dir, t0, repos)
	if err != nil || len(res) != 3 || !strings.Contains(res[0].Err, `repo "/Users/k/dotfiles" は設定に無い`) || res[1].Err != "" || res[2].Err != "" {
		t.Fatalf("設定に無い repo の add を除けない / 設定の repo・repo 無しを除けた: %+v %v", res, err)
	}
	if st, _ := Load(dir); len(st.Cards) != 2 {
		t.Fatalf("除けた依頼がカードになった: %+v", st.Cards)
	}
	submit(t, dir, Request{Kind: "plan", CardID: "C-002", Issues: []card.IssueRef{{Repo: "glogx", Number: 3, Status: "open"}}})
	if res, _ := Apply(dir, t0, repos); len(res) != 1 || !strings.Contains(res[0].Err, `repo "glogx" は設定に無い`) {
		t.Fatalf("plan が設定に無い repo をカードに付けた: %+v", res)
	}
	if c := cardOf(t, dir, "C-002"); c.Repo != "" || c.State != card.Requested {
		t.Fatalf("除けた plan がカードを変えた: %+v", c)
	}
	// 検査の前に入った設定に無い repo のカード (検査を持たない呼び手が置いた) も、repo を変えない依頼は受ける
	submit(t, dir, Request{Kind: "add", Title: "古い", Repo: "gone", At: t0})
	applyAll(t, dir)
	submit(t, dir, Request{Kind: "plan", CardID: "C-003", Issues: []card.IssueRef{{Repo: "dotfiles", Number: 1, Status: "open"}}})
	if res, _ := Apply(dir, t0, repos); len(res) != 1 || res[0].Err != "" {
		t.Fatalf("設定に無い repo のカードへの repo を変えない依頼を除けた: %+v", res)
	}
}

// issue を付けて足した依頼は、足した時点でカードに issue が紐づく。PM が plan で同じ issue を付け直しても重ねない (issue 511)。
func TestAddLinksIssues(t *testing.T) {
	dir := t.TempDir()
	ref := card.IssueRef{Repo: "dotfiles", Number: 505, Status: "open"}
	submit(t, dir, Request{Kind: "add", Title: "t", Request: "issue #505 の本文に書かれていることを進める", Repo: "dotfiles", Issues: []card.IssueRef{ref}, At: t0})
	applyAll(t, dir)
	if c := cardOf(t, dir, "C-001"); len(c.Issues) != 1 || c.Issues[0] != ref {
		t.Fatalf("add の issue がカードに紐づかない: %+v", c.Issues)
	}
	submit(t, dir, Request{Kind: "plan", CardID: "C-001", Issues: []card.IssueRef{ref, {Repo: "dotfiles", Number: 506, Status: "open"}}})
	applyAll(t, dir)
	if c := cardOf(t, dir, "C-001"); len(c.Issues) != 2 || c.Issues[1].Number != 506 {
		t.Fatalf("plan で同じ issue を重ねた / 新しい issue を付けない: %+v", c.Issues)
	}
}
