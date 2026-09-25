package main

import (
	"bytes"
	"context"
	"io"
	"os/exec"
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
	rc = runCard(args, viewEnv{dir: dir, projects: dir, now: time.Now}, &o, &e)
	return rc, o.String(), e.String()
}

// pro-con card で置いた依頼を dispatcher (store.Apply) が適用すると、カードが進む。置いた依頼の ID は stdout に出る。
func TestCardCommandRoundTrip(t *testing.T) {
	dir := t.TempDir()
	rc, out, errOut := card_(t, dir, "add", "--title", "色を直す", "--request", "statusline の色", "--repo", "dotfiles", "--wait", "0")
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

// 使い方の誤りは箱に置く前に rc=2 で止める (dispatcher で除けられるより早く気づける)。
func TestCardCommandRejectsBadUsage(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"nosuch"},
		{"add"},                                  // 題名も原文も無い
		{"ask", "C-001"},                         // 質問が無い
		{"plan", "C-001", "--issue", "dotfiles"}, // 番号が無い
		{"close", "C-001", "--ending", "done"},   // 未知の終わり方
		{"review"},                               // カードが無い
		{"delete"},                               // カードが無い
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
		{[]string{"rework", "C-001", "テストを足して"}, store.Request{Kind: "rework", CardID: "C-001", Rework: "テストを足して"}},
		{[]string{"delete", "C-001"}, store.Request{Kind: "delete", CardID: "C-001", From: "人間"}},
		{[]string{"delete", "C-001", "--from", "PM"}, store.Request{Kind: "delete", CardID: "C-001", From: "PM"}},
		{[]string{"handoff", "C-001", "色の好みは人が決める"}, store.Request{Kind: "handoff", CardID: "C-001", Text: "色の好みは人が決める", From: "PM"}},
	} {
		got, _, err := parseCardWait(tc.args)
		if err != nil || got.Kind != tc.want.Kind || got.CardID != tc.want.CardID || got.Question != tc.want.Question ||
			got.Answer != tc.want.Answer || got.Rework != tc.want.Rework || got.Text != tc.want.Text || got.From != tc.want.From && tc.want.From != "" || got.Ending != tc.want.Ending {
			t.Fatalf("%q: %+v %v (期待 %+v)", tc.args, got, err, tc.want)
		}
	}
}

// PM への指示書に書いたコマンドは、今のパーサに通る (指示書だけが古くなって PM が通らないコマンドを打つ形を止める)。
func TestPMGuideCommandsParse(t *testing.T) { guideCommandsParse(t, pmGuide) }

// 取り込みの係の指示書 (487) も同じ: 書いてあるコマンドがパーサに通る。
func TestIntegratorGuideCommandsParse(t *testing.T) { guideCommandsParse(t, integratorGuide) }

func guideCommandsParse(t *testing.T, guide string) {
	t.Helper()
	cmds := regexp.MustCompile("`(pro-con card [^`]+)`").FindAllStringSubmatch(guide, -1)
	checked := 0
	for _, m := range cmds {
		line := regexp.MustCompile(`<[^>]*>#<[^>]*>`).ReplaceAllString(m[1], "dotfiles#1")
		line = regexp.MustCompile(`<[^>]*>`).ReplaceAllString(line, "x")
		args := splitQuoted(strings.TrimPrefix(line, "pro-con card "))
		if args[0] == "guide" {
			continue
		}
		if args[0] == "list" || args[0] == "show" || args[0] == "wait" { // 読む口は parseCardWait を通らない。使い方の誤り (rc=2) にならないかを見る
			if args[0] == "wait" {
				args = append(args, "--timeout", "1ms") // 指示書の例のまま待たない (カードも無いのですぐ返る)
			}
			var e strings.Builder
			if rc := runCard(args, viewEnv{dir: t.TempDir(), now: time.Now}, io.Discard, &e); rc == 2 {
				t.Errorf("指示書のコマンドが使い方の誤りになる: %s → %s", m[1], e.String())
			}
			checked++
			continue
		}
		if _, _, err := parseCardWait(args); err != nil {
			t.Errorf("指示書のコマンドがパーサに通らない: %s → %v", m[1], err)
		}
		checked++
	}
	logs := regexp.MustCompile("`(pro-con log[^`]*)`").FindAllStringSubmatch(guide, -1)
	for _, m := range logs { // 出来事の記録を読む口 (logcmd.go)。使い方の誤り (rc=2) にならないかを見る
		line := regexp.MustCompile(`<[^>]*>`).ReplaceAllString(m[1], "x")
		var e strings.Builder
		if rc := runLog(context.Background(), splitQuoted(strings.TrimPrefix(strings.TrimPrefix(line, "pro-con log"), " ")), t.TempDir(), time.Now, io.Discard, &e); rc == 2 {
			t.Errorf("指示書のコマンドが使い方の誤りになる: %s → %s", m[1], e.String())
		}
	}
	if len(logs) == 0 {
		t.Error("指示書の役目 6 に pro-con log が無い")
	}
	if checked < 5 { // 抽出が空振りしたら緑にしない
		t.Fatalf("指示書から取り出せたコマンドが %d 本しかない", checked)
	}
}

// card guide は PM の指示書を、card guide --integrator は取り込みの係の指示書を出す (487)。ほかの引数は使い方の誤り。
func TestCardGuideOutputs(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
		rc   int
	}{
		{[]string{"guide"}, pmGuide, 0},
		{[]string{"guide", "--integrator"}, integratorGuide, 0},
		{[]string{"guide", "--pm"}, "", 2},
	} {
		var out strings.Builder
		if rc := runCard(tc.args, viewEnv{dir: t.TempDir(), now: time.Now}, &out, io.Discard); rc != tc.rc || out.String() != tc.want {
			t.Errorf("%v: rc=%d (期待 %d) / 指示書が違う", tc.args, rc, tc.rc)
		}
	}
	if pmGuide == integratorGuide {
		t.Fatal("前提: PM と取り込みの係の指示書が同じ")
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

// pro-con card run <カード> -- <コマンド>... は -- の後ろをそのままコマンドにする (フラグとして読まない)。
func TestCardRunParse(t *testing.T) {
	r, _, err := parseCardWait([]string{"run", "C-001", "--", "go", "test", "-run", "TestX", "./..."})
	if err != nil || r.Kind != "run" || r.CardID != "C-001" || r.Command != "go test -run TestX ./..." {
		t.Fatalf("run を読めない: %+v %v", r, err)
	}
	// 引用つきの argv も、テストの係が bash で eval したときに同じ argv へ戻る (空白で繋ぐと 'A|B' がパイプになった = 463)
	argv := []string{"printf", `%s\n`, "A|B", "x y", "it's", "", "$HOME", "*", "a;b", `back\slash`, "改行\nの後"}
	r, _, err = parseCardWait(append([]string{"run", "C-001", "--"}, argv...))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("/bin/bash", "-c", r.Command).Output()
	if want := strings.Join(argv[2:], "\n") + "\n"; err != nil || string(out) != want {
		t.Fatalf("argv が戻らない: %q → %q (%v)、want %q", r.Command, out, err, want)
	}
	for _, bad := range [][]string{{"run", "C-001"}, {"run", "C-001", "--"}, {"run", "--", "make"}, {"run", "C-001", "make"}, {"run", "C-001", "make", "--", "test"}} {
		if _, _, err := parseCardWait(bad); err == nil {
			t.Fatalf("形の違う run を読んだ: %v", bad)
		}
	}
}
