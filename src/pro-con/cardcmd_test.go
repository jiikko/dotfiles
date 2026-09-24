package main

import (
	"bytes"
	"io"
	"regexp"
	"strings"
	"testing"
	"time"

	"pro-con/card"
	"pro-con/store"
)

func card_(t *testing.T, dir string, args ...string) (rc int, out, errOut string) {
	t.Helper()
	var o, e bytes.Buffer
	rc = runCard(args, dir, &o, &e)
	return rc, o.String(), e.String()
}

// pro-con card で置いた依頼を daemon (store.Apply) が適用すると、カードが進む。置いた依頼の ID は stdout に出る。
func TestCardCommandRoundTrip(t *testing.T) {
	dir := t.TempDir()
	rc, out, errOut := card_(t, dir, "add", "--title", "色を直す", "--request", "statusline の色", "--repo", "dotfiles")
	if rc != 0 || strings.TrimSpace(out) == "" || errOut != "" {
		t.Fatalf("add: rc=%d out=%q err=%q", rc, out, errOut)
	}
	if rc, _, e := card_(t, dir, "plan", "C-001", "--issue", "dotfiles#415"); rc != 0 {
		t.Fatalf("plan: rc=%d %s", rc, e)
	}
	res, err := store.Apply(dir, time.Now())
	if err != nil || len(res) != 2 || res[0].Err != "" || res[1].Err != "" {
		t.Fatalf("適用: %+v %v", res, err)
	}
	st, _ := store.Load(dir)
	if len(st.Cards) != 1 || st.Cards[0].State != card.Planned || st.Cards[0].Repo != "dotfiles" || len(st.Cards[0].Issues) != 1 || st.Cards[0].Issues[0].Number != 415 {
		t.Fatalf("カードが進んでいない: %+v", st.Cards)
	}
}

// 使い方の誤りは箱に置く前に rc=2 で止める (daemon で除けられるより早く気づける)。
func TestCardCommandRejectsBadUsage(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"nosuch"},
		{"add"},                                  // 題名も原文も無い
		{"ask", "C-001"},                         // 質問が無い
		{"plan", "C-001", "--issue", "dotfiles"}, // 番号が無い
		{"close", "C-001", "--ending", "done"},   // 未知の終わり方
		{"review"},                               // カードが無い
	} {
		dir := t.TempDir()
		if rc, _, errOut := card_(t, dir, args...); rc != 2 || errOut == "" {
			t.Fatalf("%q: rc=%d (期待 2) err=%q", args, rc, errOut)
		}
		if res, _ := store.Apply(dir, time.Now()); len(res) != 0 {
			t.Fatalf("%q: 誤った使い方の依頼が箱に置かれた: %+v", args, res)
		}
	}
}

// 質問と回答の本文は位置引数で受ける。回答した人は --from (既定は人間)。
func TestCardCommandAskAnswer(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want store.Request
	}{
		{[]string{"ask", "C-001", "赤と青どちら?"}, store.Request{Kind: "ask", CardID: "C-001", Question: "赤と青どちら?"}},
		{[]string{"answer", "C-001", "青", "--from", "PM"}, store.Request{Kind: "answer", CardID: "C-001", Answer: "青", From: "PM"}},
		{[]string{"close", "C-001", "--ending", "answered"}, store.Request{Kind: "close", CardID: "C-001", Ending: card.EndAnswered}},
	} {
		got, err := parseCard(tc.args)
		if err != nil || got.Kind != tc.want.Kind || got.CardID != tc.want.CardID || got.Question != tc.want.Question ||
			got.Answer != tc.want.Answer || got.From != tc.want.From && tc.want.From != "" || got.Ending != tc.want.Ending {
			t.Fatalf("%q: %+v %v (期待 %+v)", tc.args, got, err, tc.want)
		}
	}
}

// PM への指示書に書いたコマンドは、今のパーサに通る (指示書だけが古くなって PM が通らないコマンドを打つ形を止める)。
func TestPMGuideCommandsParse(t *testing.T) {
	cmds := regexp.MustCompile("`(pro-con card [^`]+)`").FindAllStringSubmatch(pmGuide, -1)
	checked := 0
	for _, m := range cmds {
		line := regexp.MustCompile(`<[^>]*>#<[^>]*>`).ReplaceAllString(m[1], "dotfiles#1")
		line = regexp.MustCompile(`<[^>]*>`).ReplaceAllString(line, "x")
		args := splitQuoted(strings.TrimPrefix(line, "pro-con card "))
		if args[0] == "guide" {
			continue
		}
		if _, err := parseCard(args); err != nil {
			t.Errorf("指示書のコマンドがパーサに通らない: %s → %v", m[1], err)
		}
		checked++
	}
	if checked < 5 { // 抽出が空振りしたら緑にしない
		t.Fatalf("指示書から取り出せたコマンドが %d 本しかない", checked)
	}
	var out strings.Builder
	if rc := runCard([]string{"guide"}, t.TempDir(), &out, io.Discard); rc != 0 || out.String() != pmGuide {
		t.Fatalf("guide が指示書を出さない: rc=%d", rc)
	}
}

// splitQuoted は "..." を 1 つの引数として空白で分ける (指示書の例を引数にするだけの最小の分け方)。
func splitQuoted(s string) []string {
	var out []string
	for _, m := range regexp.MustCompile(`"([^"]*)"|(\S+)`).FindAllStringSubmatch(s, -1) {
		if m[2] != "" {
			out = append(out, m[2])
		} else {
			out = append(out, m[1])
		}
	}
	return out
}
