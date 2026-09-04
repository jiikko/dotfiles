package main

import (
	"context"
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
	v := &doctorView{shown: true, tab: tabBrew, expanded: map[string]bool{}}
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

// cask 版は marker も削除コマンドも formula 版と違う (--cask が要る)。
// 🚨 marker を formula 固定にすると、cask では名前が 1 つも取れず「日本語の見出しになったのに
// 手が 1 つも出ない」形になる (実際に既存 fixture で起きた)。
func TestBrewAdviceCaskVariant(t *testing.T) {
	w := "Warning: Some installed casks are deprecated or disabled.\n" +
		"You should find replacements:\n  foo\n\n" +
		"Uninstall them with `brew uninstall --cask`\n\nSee also: brew help uninstall\n"
	a := brewAdviceFor(w)
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
		a := brewAdviceFor(w)
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
		a := brewAdviceFor(w)
		if len(a.Actions) == 0 {
			t.Fatal("手が出ていない")
		}
		if a.Actions[0].Cmd != "brew link node" {
			t.Errorf("空行の先を名前として拾った: %q", a.Actions[0].Cmd)
		}
	})
}

// brew の手を Space で選び、x で確認、y で実行する。
//
// 🚨 disk の削除と**同じ二段** (印を付ける → 実行) にしてある。同じ Space が片方は
// 「印」で片方は「即実行」だと、身についた期待のまま副作用を起こす形になる。
func TestBrewActionSelectRunAndReport(t *testing.T) {
	var ran [][]string
	v := &doctorView{shown: true, tab: tabBrew, expanded: map[string]bool{}}
	v.brewRun = func(_ context.Context, name string, args ...string) (string, string, int, error) {
		ran = append(ran, append([]string{name}, args...))
		if name == "brew" && len(args) > 0 && args[0] == "info" {
			return "", "not found", 1, nil // 1 件だけ失敗させる
		}
		return "linked\n", "", 0, nil
	}
	v.brew = &brewDoctorResult{Warnings: []string{brewWarnUnlinked}}
	o := doctorTestOpts(40)
	o.width = 96
	_ = v.lines(o)

	// 手の行まで降りて Space で選ぶ
	var actKey string
	for _, r := range v.rows {
		if strings.HasPrefix(r.key, "brew:") {
			v.expanded[r.key] = true
		}
	}
	_ = v.lines(o)
	for _, r := range v.rows {
		if strings.HasPrefix(r.key, "brewact:") {
			actKey = r.key
			break
		}
	}
	if actKey == "" {
		t.Fatal("手の行が無い")
	}
	v.cur.jumpTo(v.rows, actKey)
	if act := v.handleKey(" ", 40); act != doctorSwallow {
		t.Fatalf("Space で選べない: %v (%s)", act, v.pendingToast)
	}
	if len(v.selectedActions) != 1 {
		t.Fatalf("選択が入っていない: %v", v.selectedActions)
	}
	// 選んだ印が行に出る
	_ = v.lines(o)
	var marked bool
	for _, r := range v.rows {
		if r.key == actKey && strings.Contains(r.text, "*") {
			marked = true
		}
	}
	if !marked {
		t.Error("選んだ手に印が出ていない")
	}
	if h := v.hint(120); !strings.Contains(h, "x: 1 件を実行") {
		t.Errorf("hint に実行の導線が出ていない: %q", h)
	}

	// x で確認画面へ。**実行するコマンドがそのまま出る**
	if act := v.handleKey("x", 40); act != doctorSwallow {
		t.Fatalf("x が効かない: %v (%s)", act, v.pendingToast)
	}
	out := doctorPanelText(v, 30)
	if !strings.Contains(out, "これを実行しますか?") || !strings.Contains(out, "$ brew link node ruby") {
		t.Fatalf("確認画面に実行するコマンドが出ていない:\n%s", out)
	}
	if len(ran) != 0 {
		t.Fatalf("確認の前に実行してしまった: %v", ran)
	}

	// y で実行 → 結果
	if act := v.handleKey("y", 40); act != doctorRunDelete {
		t.Fatalf("y で実行に入らない: %v", act)
	}
	_ = runDeleteCmds(t, v, v.takeDeleteCmd())
	if len(ran) != 1 || strings.Join(ran[0], " ") != "brew link node ruby" {
		t.Fatalf("実行したコマンド = %v", ran)
	}
	if v.del.cmdRep == nil {
		t.Fatalf("結果が出ていない: %+v", v.del)
	}
	out = doctorPanelText(v, 30)
	for _, want := range []string{"実行の結果", "1 件すべて成功しました", "$ brew link node ruby", "exit 0"} {
		if !strings.Contains(out, want) {
			t.Errorf("結果に %q が無い:\n%s", want, out)
		}
	}
}

// x は何も選んでいないと実行しない (押し間違いで走らせない)。
func TestBrewRunNeedsSelection(t *testing.T) {
	v := &doctorView{shown: true, tab: tabBrew, expanded: map[string]bool{}}
	v.brew = &brewDoctorResult{Warnings: []string{brewWarnUnlinked}}
	_ = v.lines(doctorTestOpts(40))
	if act := v.handleKey("x", 40); act != doctorToast {
		t.Fatalf("何も選んでいないのに実行へ入った: %v", act)
	}
	if !strings.Contains(v.pendingToast, "Space") {
		t.Errorf("選び方を案内していない: %q", v.pendingToast)
	}
	if v.del.active() {
		t.Error("パネルが立ってしまった")
	}
	// 🚨 選んでいないときは hint に x を出さない。常設すると幅の予算を食い、
	// r: 再スキャン のような常に使える手を押し出す
	if h := v.hint(120); strings.Contains(h, "x:") {
		t.Errorf("選んでいないのに x を案内した: %q", h)
	}
}

// 🚨 タブが違えば実行キーは効かない。**「印を付けた後にやること」をタブごとに 1 つに保つ**のが
// タブ分割の目的なので、ディスクのタブで x を押しても brew は走らない (逆も同じ)。
func TestExecuteKeysAreTabScoped(t *testing.T) {
	v := &doctorView{shown: true, expanded: map[string]bool{}}
	v.brew = &brewDoctorResult{Warnings: []string{brewWarnUnlinked}}
	v.selectedActions = map[string]bool{"brew link node ruby": true}
	_ = v.lines(doctorTestOpts(40))

	v.tab = tabDisk
	if act := v.handleKey("x", 40); act != doctorToast || !strings.Contains(v.pendingToast, "タブ") {
		t.Errorf("ディスクのタブで x が走った: %v (%q)", act, v.pendingToast)
	}
	if v.del.active() {
		t.Fatal("ディスクのタブで brew の実行パネルが立った")
	}
	v.tab = tabBrew
	// 🚨 「ディスク」だけを見ない: ゲートを外しても beginDelete が
	// 「ディスクのスキャンが終わるまで待ってください」を返すので素通りする (変異で実測)
	if act := v.handleKey("d", 40); act != doctorToast || !strings.Contains(v.pendingToast, "タブ") {
		t.Errorf("Homebrew のタブで d が削除へ入った: %v (%q)", act, v.pendingToast)
	}
	// hint も押せないキーを案内しない
	v.tab = tabBrew
	if h := v.hint(120); strings.Contains(h, "d: 削除") {
		t.Errorf("Homebrew のタブで削除を案内した: %q", h)
	}
	v.tab = tabDisk
	if h := v.hint(120); strings.Contains(h, "x:") {
		t.Errorf("ディスクのタブで brew の実行を案内した: %q", h)
	}
}
