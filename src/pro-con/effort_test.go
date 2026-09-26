package main

import (
	"encoding/json"
	"strings"
	"testing"

	"pro-con/store"
)

// plan --points は 1 / 2 / 3 / 5 / 8 だけを受ける。0 を含むほかの値・値の無い --points は使い方の誤り (issue 490)。
func TestCardPlanPointsParse(t *testing.T) {
	r, _, err := parseCardWait([]string{"plan", "C-003", "--issue", "dotfiles#1", "--points", "8"})
	if err != nil || r.Points != 8 {
		t.Fatalf("plan --points を読めない: %+v %v", r, err)
	}
	if r, _, err := parseCardWait([]string{"plan", "C-003"}); err != nil || r.Points != 0 {
		t.Fatalf("--points を付けないと見積もり無し: %+v %v", r, err)
	}
	for _, v := range []string{"0", "4", "13", "-1", "x", "3pt"} {
		if _, _, err := parseCardWait([]string{"plan", "C-003", "--points", v}); err == nil {
			t.Errorf("--points %s を受けた", v)
		}
	}
	if _, _, err := parseCardWait([]string{"plan", "C-003", "--points"}); err == nil {
		t.Error("値の無い --points を受けた")
	}
}

// card show に見積もりと作業中だった時間が出る (--json は今の分を含めた秒)。見積もりも時間も無いカードには行を出さない。
func TestCardShowEffort(t *testing.T) {
	env := viewFixture(t) // C-001 は質問待ち、C-002 は依頼
	if _, out, _ := viewCmd(t, env, "show", "C-002"); strings.Contains(out, "重さ:") {
		t.Fatalf("見積もりも時間も無いのに重さの行が出た: %q", out)
	}
	mustSubmit(t, env.dir, store.Request{Kind: "plan", CardID: "C-002", Points: 3})
	mustApply(t, env.dir)
	if _, out, _ := viewCmd(t, env, "show", "C-002"); !strings.Contains(out, "重さ: 見積もり 3pt / 作業中だった時間 0 秒") {
		t.Fatalf("詳細に見積もりが無い: %q", out)
	}
	_, out, _ := viewCmd(t, env, "show", "C-002", "--json")
	var d cardDetail
	if json.Unmarshal([]byte(out), &d) != nil || d.Card.Points != 3 || d.WorkedSeconds != 0 {
		t.Fatalf("--json に見積もりが無い: %q", out)
	}
}
