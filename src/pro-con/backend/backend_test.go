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
