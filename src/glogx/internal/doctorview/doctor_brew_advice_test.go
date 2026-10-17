package doctorview

import (
	"strings"
	"testing"
)

// 未知のパターンは英語のまま出す (無理に訳して誤った誘導をしない)。
func TestBrewAdviceUnknownStaysRaw(t *testing.T) {
	a := BrewAdviceFor("Warning: Something we have never seen.\nblah")
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
		a := BrewAdviceFor(w)
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

// cask 版は marker も削除コマンドも formula 版と違う (--cask が要る)。
// 🚨 marker を formula 固定にすると、cask では名前が 1 つも取れず「日本語の見出しになったのに
// 手が 1 つも出ない」形になる (実際に既存 fixture で起きた)。
func TestBrewAdviceCaskVariant(t *testing.T) {
	w := "Warning: Some installed casks are deprecated or disabled.\n" +
		"You should find replacements:\n  foo\n\n" +
		"Uninstall them with `brew uninstall --cask`\n\nSee also: brew help uninstall\n"
	a := BrewAdviceFor(w)
	if !a.Known || a.Title != "非推奨 / 無効になった cask があります" {
		t.Fatalf("cask 版を認識していない: %+v", a)
	}
	if len(a.Actions) != 2 || a.Actions[1].Cmd != "brew uninstall --cask foo" {
		t.Fatalf("cask の削除コマンドに --cask が無い: %+v", a.Actions)
	}
	// 🚨 名前の列の後ろの散文 (Uninstall them with... / See also:) を名前として拾わない
	for _, act := range a.Actions {
		for _, bad := range []string{"Uninstall", "them", "with", "See", "also", "help"} {
			if strings.Contains(act.Cmd, " "+bad) {
				t.Errorf("散文の語を名前として拾った: %q", act.Cmd)
			}
		}
	}
}

// 名前の列は「インデントされた連続する行」に限る。
//
// 🚨 語の allowlist だけでは足りない: brew の散文には **語が全部「名前として通る」もの**が
// ある (`These kegs were installed earlier`)。バッククォートや記号が無いので語の検査では
// 止まらず、インデントと連続性の規則だけが止める。
func TestBrewAdviceNameListIsIndentedAndContiguous(t *testing.T) {
	t.Run("非インデントの散文で打ち切る", func(t *testing.T) {
		w := "Warning: You have unlinked kegs in your Cellar.\n" +
			"Run `brew link` on these:\n  node\n" +
			"These kegs were installed earlier\n"
		a := BrewAdviceFor(w)
		if len(a.Actions) == 0 {
			t.Fatal("手が出ていない")
		}
		if a.Actions[0].Cmd != "brew link node" {
			t.Errorf("散文の語を名前として拾った: %q", a.Actions[0].Cmd)
		}
	})
	t.Run("空行で打ち切る", func(t *testing.T) {
		w := "Warning: You have unlinked kegs in your Cellar.\n" +
			"Run `brew link` on these:\n  node\n\n  ruby extra note\n"
		a := BrewAdviceFor(w)
		if len(a.Actions) == 0 {
			t.Fatal("手が出ていない")
		}
		if a.Actions[0].Cmd != "brew link node" {
			t.Errorf("空行の先を名前として拾った: %q", a.Actions[0].Cmd)
		}
	})
}
