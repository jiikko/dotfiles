package main

import (
	"io"
	"io/fs"
	"strings"
	"testing"

	"pro-con/card"
	"pro-con/ui"
)

// 引数なしの pro-con help は話題の一覧を出す。知らない話題は一覧を stderr に出して rc=2 (issue 510)。
func TestHelpIndexAndUnknownTopic(t *testing.T) {
	var out strings.Builder
	if rc := runHelp(nil, &out, io.Discard); rc != 0 {
		t.Fatalf("引数なし: rc=%d", rc)
	}
	for _, tp := range helpTopics {
		if !strings.Contains(out.String(), tp.names[0]) {
			t.Errorf("一覧に %s が無い:\n%s", tp.names[0], out.String())
		}
	}
	var o, e strings.Builder
	if rc := runHelp([]string{"nope"}, &o, &e); rc != 2 || o.Len() != 0 || !strings.Contains(e.String(), helpIndex()) {
		t.Errorf("知らない話題: rc=%d stdout=%q stderr=%q", rc, o.String(), e.String())
	}
	if rc := runHelp([]string{"terms", "extra"}, io.Discard, io.Discard); rc != 2 {
		t.Errorf("余分な引数: rc=%d (期待 2)", rc)
	}
}

// どの話題もどの名前 (別名を含む) でも引け、目印の行が残らない。embed した md と話題の表は 1 対 1。
func TestHelpTopicsResolve(t *testing.T) {
	files, err := fs.Glob(helpFS, "help/*.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(helpTopics) {
		t.Errorf("help/*.md は %d 個、話題は %d 個 (%v)", len(files), len(helpTopics), files)
	}
	for _, tp := range helpTopics {
		for _, n := range tp.names {
			var out strings.Builder
			if rc := runHelp([]string{n}, &out, io.Discard); rc != 0 {
				t.Fatalf("%s: rc=%d", n, rc)
			}
			if !strings.HasPrefix(out.String(), "# ") || strings.Contains(out.String(), "{{") {
				t.Errorf("%s: 見出しが無いか目印が残っている:\n%s", n, out.String())
			}
		}
	}
}

// terms は役・レーン・人の番を正本から出す (help の md に写さない)。
func TestHelpTermsFromCanonicalSources(t *testing.T) {
	var out strings.Builder
	runHelp([]string{"terms"}, &out, io.Discard)
	got := out.String()
	for _, r := range ui.RoleMeanings() {
		if !strings.Contains(got, "**"+r[0]+"**: "+r[1]) {
			t.Errorf("役 %s の説明が無い", r[0])
		}
	}
	for _, s := range card.Columns {
		if !strings.Contains(got, "**"+s.Label()+"**: "+s.Meaning()) {
			t.Errorf("レーン %s の説明が無い", s.Label())
		}
	}
	if !strings.Contains(got, ui.HumansTurnMeaning) {
		t.Error("人の番の説明が無い")
	}
}

// usage と debug に書いた pro-con card / pro-con log のコマンドは今のパーサに通る (指示書と同じ検査)。
func TestHelpCommandsParse(t *testing.T) {
	var b strings.Builder
	for _, n := range []string{"usage", "debug"} {
		tp, _ := findHelpTopic(n)
		b.WriteString(helpText(tp))
	}
	guideCommandsParse(t, b.String())
}

// --help の 1 行から pro-con help を案内する。
func TestTopLevelHelpPointsToHelpTopics(t *testing.T) {
	var out strings.Builder
	if rc := run([]string{"--help"}, strings.NewReader(""), &out, io.Discard); rc != 0 || !strings.Contains(out.String(), "pro-con help") {
		t.Errorf("rc=%d %q", rc, out.String())
	}
}
