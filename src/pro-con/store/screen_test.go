package store

import (
	"testing"
	"time"

	"pro-con/card"
)

// 画面から置いた依頼は、適用で足された履歴の行にだけ画面を残す (issue 481)。前からある行・画面以外が置いた依頼の行には付けない。
// attach の指示は時刻の位置に差し込まれる (末尾の差では拾えない) が、それにも付く。
func TestAppliedHistoryRemembersScreen(t *testing.T) {
	dir := t.TempDir()
	t0 := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	submit := func(r Request) {
		t.Helper()
		if _, err := Submit(dir, r); err != nil {
			t.Fatal(err)
		}
		res, err := Apply(dir, t0.Add(time.Hour), nil)
		if err != nil || len(res) != 1 || res[0].Err != "" || res[0].Screen != r.Screen {
			t.Fatalf("%s を適用できない / 結果に画面が無い: %+v %v", r.Kind, res, err)
		}
	}
	submit(Request{Kind: "add", Title: "t", Request: "r"}) // 画面以外 (PM など) が置いた
	submit(Request{Kind: "btw", CardID: "C-001", Question: "どう?", Screen: "aaaaaa 持ち主"})
	submit(Request{Kind: "attach", CardID: "C-001", Said: []card.Event{{At: t0, Text: "こうして"}}, Screen: "bbbbbb join review"})
	st, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, e := range st.Cards[0].History {
		got[e.Text] = e.Screen
	}
	want := map[string]string{"依頼を受けた": "", "btw: どう?": "aaaaaa 持ち主", AttachPrefix + "こうして": "bbbbbb join review"}
	for text, screen := range want {
		if s, ok := got[text]; !ok || s != screen {
			t.Fatalf("%q の画面が %q (%q のはず)。履歴 %+v", text, s, screen, st.Cards[0].History)
		}
	}
	if st.Cards[0].History[0].Text != AttachPrefix+"こうして" {
		t.Fatalf("attach の指示が時刻の位置に入っていない (差し込みを試せていない): %+v", st.Cards[0].History)
	}
}
