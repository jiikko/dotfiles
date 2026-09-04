package main

import (
	"strings"
	"testing"
)

// 実際に brew doctor が出した 3 パターン (ユーザー環境 2026-09-04) をそのまま入力にする。
// 🚨 fixture を自分で書き直さない: 見出しの語や marker の文言が実物と 1 文字違うだけで
// 解析は空振りするので、実物を写しておかないと「通ったのに現場では効かない」形になる
const (
	brewWarnUnlinked = "Warning: You have unlinked kegs in your Cellar.\n" +
		"Leaving kegs unlinked can lead to build-trouble and cause formulae that depend on\n" +
		"those kegs to fail to run properly once built.\n\n" +
		"Run `brew link` on these:\n  node\n  ruby"
	brewWarnDeprecated = "Warning: Some installed formulae are deprecated or disabled.\n" +
		"You should find replacements for the following formulae:\n    tree-sitter@0.25"
	brewWarnMissing = "Warning: Some installed formulae or casks are missing dependencies.\n" +
		"Run `brew missing` for more details.\n\n" +
		"You should `brew install` the missing dependencies:\n" +
		"  brew install ada-url c-ares fmt hdrhistogram_c icu4c@78 llhttp simdjson uvwasi"
)

func TestBrewAdviceKnownPatterns(t *testing.T) {
	for _, tc := range []struct {
		name    string
		warning string
		title   string
		cmds    []string
	}{
		{"unlinked", brewWarnUnlinked, "リンクされていない keg があります",
			[]string{"brew link node ruby", "brew uninstall node ruby"}},
		{"deprecated", brewWarnDeprecated, "非推奨 / 無効になった formula があります",
			[]string{"brew info tree-sitter@0.25", "brew uninstall tree-sitter@0.25"}},
		{"missing", brewWarnMissing, "依存が欠けている formula / cask があります",
			[]string{"brew install ada-url c-ares fmt hdrhistogram_c icu4c@78 llhttp simdjson uvwasi", "brew missing"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := brewAdviceFor(tc.warning)
			if !a.Known {
				t.Fatalf("パターンを認識していない")
			}
			if a.Title != tc.title {
				t.Errorf("見出し = %q", a.Title)
			}
			var got []string
			for _, act := range a.Actions {
				got = append(got, act.Cmd)
			}
			if strings.Join(got, " / ") != strings.Join(tc.cmds, " / ") {
				t.Errorf("コマンド = %v\n期待 = %v", got, tc.cmds)
			}
			if a.Dropped != 0 {
				t.Errorf("正常な入力で名前を落とした: %d", a.Dropped)
			}
		})
	}
}

// 未知のパターンは英語のまま出す (無理に訳して誤った誘導をしない)。
func TestBrewAdviceUnknownStaysRaw(t *testing.T) {
	a := brewAdviceFor("Warning: Something we have never seen.\nblah")
	if a.Known || a.Title != "" || len(a.Actions) != 0 {
		t.Errorf("未知のパターンを既知として扱った: %+v", a)
	}
}

// 🚨 brew の出力は外部由来。細工された名前をコマンドへ通さない (コピーして貼る経路に載る)。
func TestBrewAdviceRejectsUnsafeNames(t *testing.T) {
	for _, bad := range []string{
		"node; rm -rf /",
		"node$(id)",
		"node`id`",
		"../../etc/passwd",
		"node\x1b]52;c;cHduZWQ=\x07",
	} {
		w := "Warning: You have unlinked kegs in your Cellar.\nRun `brew link` on these:\n  " + bad + "\n  ruby"
		a := brewAdviceFor(w)
		for _, act := range a.Actions {
			for _, tok := range []string{";", "$(", "`", "..", "\x1b"} {
				if strings.Contains(act.Cmd, tok) {
					t.Errorf("危険な語がコマンドへ通った (%q): %q", bad, act.Cmd)
				}
			}
		}
		if a.Dropped == 0 && len(a.Actions) > 0 && strings.Contains(a.Actions[0].Cmd, strings.Fields(bad)[0]) {
			// 先頭トークンだけが通るケース (node; rm -rf /) は「落とした」と数えていること
			t.Errorf("落とした名前を数えていない: %q → %+v", bad, a)
		}
	}
}

// 画面: 見出しが日本語になり、手の行が選べて y でコマンドがコピーできる。
func TestBrewSectionShowsAdviceRows(t *testing.T) {
	v := &doctorView{shown: true, expanded: map[string]bool{}}
	v.brew = &brewDoctorResult{Warnings: []string{brewWarnUnlinked}}
	o := doctorTestOpts(60)
	o.width = 96
	_ = v.lines(o)
	var key string
	for _, r := range v.rows {
		if strings.HasPrefix(r.key, "brew:") {
			key = r.key
			if !strings.Contains(r.text, "リンクされていない keg があります") {
				t.Errorf("見出しが日本語になっていない: %q", r.text)
			}
		}
	}
	if key == "" {
		t.Fatal("brew の行が無い")
	}
	v.expanded[key] = true
	_ = v.lines(o)
	var acts []doctorRow
	for _, r := range v.rows {
		if strings.HasPrefix(r.key, "brewact:") {
			acts = append(acts, r)
		}
	}
	if len(acts) != 2 {
		t.Fatalf("手の行が選べる形で出ていない: %d 件", len(acts))
	}
	if !acts[0].selectable || acts[0].copyPath != "brew link node ruby" {
		t.Errorf("1 つ目の手 = selectable:%v copyPath:%q", acts[0].selectable, acts[0].copyPath)
	}
	out := doctorText(v, 60)
	if !strings.Contains(out, "原文 (brew doctor)") {
		t.Error("原文が残っていない (英語のまま LLM へ投げる導線が消える)")
	}
}
