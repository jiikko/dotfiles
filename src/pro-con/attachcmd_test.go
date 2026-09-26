package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"pro-con/card"
	"pro-con/dispatcher"
	"pro-con/store"
)

// PG が card attach で置いた添付を dispatcher (store.Apply) が載せると、card show に種類・一言・パスが出る (--json にもパス)。
// ファイルが消えた (書庫へ移した・削除した) 添付は、そう書く。
func TestCardAttachRoundTrip(t *testing.T) {
	dir, work := viewDir(t), t.TempDir()
	mustSubmit(t, dir, store.Request{Kind: "add", Title: "見た目を直す"})
	mustApply(t, dir)
	if err := store.Update(dir, func(st *store.State) error { st.Cards[0].State = card.Running; return nil }); err != nil {
		t.Fatal(err)
	}
	shot := filepath.Join(work, "drawer.png")
	if err := os.WriteFile(shot, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rc, out, e := card_(t, dir, "attach", "C-001", shot, "--note", "詳細の引き出し"); rc != 0 || strings.TrimSpace(out) == "" {
		t.Fatalf("attach: rc=%d out=%q err=%q", rc, out, e)
	}
	mustApply(t, dir)
	env := viewEnv{dir: dir, projects: t.TempDir(), now: time.Now}
	rc, out, e := viewCmd(t, env, "show", "C-001")
	st, _ := store.Load(dir)
	path := st.Cards[0].Attachments[0].Path
	for _, want := range []string{"添付", "画像  詳細の引き出し  " + path} {
		if rc != 0 || !strings.Contains(out, want) {
			t.Fatalf("show に %q が無い: rc=%d out=%q err=%q", want, rc, out, e)
		}
	}
	if strings.Contains(out, "ファイルが無い") {
		t.Fatalf("在るファイルを無いと書いた: %q", out)
	}
	_, js, _ := viewCmd(t, env, "show", "C-001", "--json")
	var d cardDetail
	if json.Unmarshal([]byte(js), &d) != nil || len(d.Card.Attachments) != 1 || d.Card.Attachments[0].Path != path {
		t.Fatalf("show --json に添付のパスが無い: %q", js)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, out, _ := viewCmd(t, env, "show", "C-001"); !strings.Contains(out, "ファイルが無い") {
		t.Fatalf("消えた添付をそう書かない: %q", out)
	}
}

// attach の使い方の誤りは rc=2、ファイルが無いなどで箱に置けなければ rc=1。どちらも箱に依頼を置かない。
func TestCardAttachRejects(t *testing.T) {
	for _, tc := range []struct {
		args []string
		rc   int
	}{
		{[]string{"attach"}, 2},
		{[]string{"attach", "C-001"}, 2},
		{[]string{"attach", "C-001", "a.png", "b.png"}, 2},
		{[]string{"attach", "C-001", "a.png", "--nosuch"}, 2},
		{[]string{"attach", "C-001", "/nonexistent/a.png"}, 1},
	} {
		dir := t.TempDir()
		if rc, _, e := card_(t, dir, tc.args...); rc != tc.rc || e == "" {
			t.Errorf("%q: rc=%d (期待 %d) err=%q", tc.args, rc, tc.rc, e)
		}
		if n := store.Pending(dir); n != 0 {
			t.Errorf("%q: 箱に依頼を置いた (%d 件)", tc.args, n)
		}
	}
}

// PG への指示 (dispatcher.Prompt) の attach のコマンドは、今のパーサに通る (指示だけが古くなる形を止める)。
// 敵対的レビューを codex に回す設定 (514) で足す attach (証拠・代わりに回した記録) も同じ。
func TestPromptAttachCommandParses(t *testing.T) {
	for _, rv := range []dispatcher.Review{
		{},
		{Mode: store.ReviewCodex, Codex: "/opt/codex"},
		{Mode: store.ReviewCodex, CodexErr: "codex が PATH に無い"},
	} {
		p := dispatcher.Prompt(card.Card{ID: "C-007", Title: "t"}, rv)
		ms := regexp.MustCompile("`(pro-con card attach [^`]+)`").FindAllStringSubmatch(p, -1)
		if len(ms) == 0 {
			t.Fatalf("PG への指示に attach のコマンドが無い:\n%s", p)
		}
		for _, m := range ms {
			line := regexp.MustCompile(`<[^>]*>`).ReplaceAllString(m[1], "x")
			id, file, note, err := parseAttach(splitQuoted(strings.TrimPrefix(line, "pro-con card attach ")))
			if err != nil || id != "C-007" || file != "x" || note == "" {
				t.Fatalf("%+v: %s → id=%q file=%q note=%q err=%v", rv, m[1], id, file, note, err)
			}
		}
	}
}
