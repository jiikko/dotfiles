package main

import (
	"context"
	"glogx/internal/doctorview"
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
			a := doctorview.BrewAdviceFor(tc.warning)
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

// 画面: 見出しが日本語になり、手の行が選べて y でコマンドがコピーできる。
func TestBrewSectionShowsAdviceRows(t *testing.T) {
	v := &doctorview.DoctorView{Shown: true, Tab: doctorview.TabBrew, Expanded: map[doctorview.RowKey]bool{}}
	v.Brew = &doctorview.BrewDoctorResult{Warnings: []string{brewWarnUnlinked}}
	o := doctorTestOpts(60)
	o.Width = 96
	_ = v.Lines(o)
	var key doctorview.RowKey
	for _, r := range v.Rows {
		if strings.HasPrefix(string(r.Key), "brew:") {
			key = r.Key
			if !strings.Contains(r.Text, "リンクされていない keg があります") {
				t.Errorf("見出しが日本語になっていない: %q", r.Text)
			}
		}
	}
	if key == "" {
		t.Fatal("brew の行が無い")
	}
	v.Expanded[key] = true
	_ = v.Lines(o)
	var acts []doctorview.DoctorRow
	for _, r := range v.Rows {
		if strings.HasPrefix(string(r.Key), "brewact:") {
			acts = append(acts, r)
		}
	}
	if len(acts) != 2 {
		t.Fatalf("手の行が選べる形で出ていない: %d 件", len(acts))
	}
	if !acts[0].Selectable || acts[0].CopyPath != "brew link node ruby" {
		t.Errorf("1 つ目の手 = selectable:%v copyPath:%q", acts[0].Selectable, acts[0].CopyPath)
	}
	out := doctorText(v, 60)
	if !strings.Contains(out, "原文 (brew doctor)") {
		t.Error("原文が残っていない (英語のまま LLM へ投げる導線が消える)")
	}
}

// brew の手を Space で選び、x で確認、y で実行する。
//
// 🚨 disk の削除と**同じ二段** (印を付ける → 実行) にしてある。同じ Space が片方は
// 「印」で片方は「即実行」だと、身についた期待のまま副作用を起こす形になる。
func TestBrewActionSelectRunAndReport(t *testing.T) {
	var ran [][]string
	v := &doctorview.DoctorView{Shown: true, Tab: doctorview.TabBrew, Expanded: map[doctorview.RowKey]bool{}}
	v.BrewRun = func(_ context.Context, name string, args ...string) (string, string, int, error) {
		ran = append(ran, append([]string{name}, args...))
		if name == "brew" && len(args) > 0 && args[0] == "info" {
			return "", "not found", 1, nil // 1 件だけ失敗させる
		}
		return "linked\n", "", 0, nil
	}
	v.Brew = &doctorview.BrewDoctorResult{Warnings: []string{brewWarnUnlinked}}
	o := doctorTestOpts(40)
	o.Width = 96
	_ = v.Lines(o)

	// 手の行まで降りて Space で選ぶ
	var actKey doctorview.RowKey
	for _, r := range v.Rows {
		if strings.HasPrefix(string(r.Key), "brew:") {
			v.Expanded[r.Key] = true
		}
	}
	_ = v.Lines(o)
	for _, r := range v.Rows {
		if strings.HasPrefix(string(r.Key), "brewact:") {
			actKey = r.Key
			break
		}
	}
	if actKey == "" {
		t.Fatal("手の行が無い")
	}
	v.Cur.JumpTo(v.Rows, actKey)
	if act := v.HandleKey(" ", 40); act != doctorview.DoctorSwallow {
		t.Fatalf("Space で選べない: %v (%s)", act, v.PendingToast)
	}
	if len(v.SelectedActions) != 1 {
		t.Fatalf("選択が入っていない: %v", v.SelectedActions)
	}
	// 選んだ印が行に出る
	_ = v.Lines(o)
	var marked bool
	for _, r := range v.Rows {
		if r.Key == actKey && strings.Contains(r.Text, "*") {
			marked = true
		}
	}
	if !marked {
		t.Error("選んだ手に印が出ていない")
	}
	if h := v.Hint(120); !strings.Contains(h, "x: 1 件を実行") {
		t.Errorf("hint に実行の導線が出ていない: %q", h)
	}

	// x で確認画面へ。**実行するコマンドがそのまま出る**
	if act := v.HandleKey("x", 40); act != doctorview.DoctorSwallow {
		t.Fatalf("x が効かない: %v (%s)", act, v.PendingToast)
	}
	out := doctorPanelText(v, 30)
	if !strings.Contains(out, "これを実行しますか?") || !strings.Contains(out, "$ brew link node ruby") {
		t.Fatalf("確認画面に実行するコマンドが出ていない:\n%s", out)
	}
	if len(ran) != 0 {
		t.Fatalf("確認の前に実行してしまった: %v", ran)
	}

	// y で実行 → 結果
	if act := v.HandleKey("y", 40); act != doctorview.DoctorRunDelete {
		t.Fatalf("y で実行に入らない: %v", act)
	}
	_ = runDeleteCmds(t, v, v.TakeDeleteCmd())
	if len(ran) != 1 || strings.Join(ran[0], " ") != "brew link node ruby" {
		t.Fatalf("実行したコマンド = %v", ran)
	}
	if v.Del.CmdRep == nil {
		t.Fatalf("結果が出ていない: %+v", v.Del)
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
	v := &doctorview.DoctorView{Shown: true, Tab: doctorview.TabBrew, Expanded: map[doctorview.RowKey]bool{}}
	v.Brew = &doctorview.BrewDoctorResult{Warnings: []string{brewWarnUnlinked}}
	_ = v.Lines(doctorTestOpts(40))
	if act := v.HandleKey("x", 40); act != doctorview.DoctorToast {
		t.Fatalf("何も選んでいないのに実行へ入った: %v", act)
	}
	if !strings.Contains(v.PendingToast, "Space") {
		t.Errorf("選び方を案内していない: %q", v.PendingToast)
	}
	if v.Del.Active() {
		t.Error("パネルが立ってしまった")
	}
	// 🚨 選んでいないときは hint に x を出さない。常設すると幅の予算を食い、
	// r: 再スキャン のような常に使える手を押し出す
	if h := v.Hint(120); strings.Contains(h, "x:") {
		t.Errorf("選んでいないのに x を案内した: %q", h)
	}
}

// 🚨 タブが違えば実行キーは効かない。**「印を付けた後にやること」をタブごとに 1 つに保つ**のが
// タブ分割の目的なので、ディスクのタブで x を押しても brew は走らない (逆も同じ)。
func TestExecuteKeysAreTabScoped(t *testing.T) {
	v := &doctorview.DoctorView{Shown: true, Expanded: map[doctorview.RowKey]bool{}}
	v.Brew = &doctorview.BrewDoctorResult{Warnings: []string{brewWarnUnlinked}}
	v.SelectedActions = map[doctorview.CmdKey]bool{"brew link node ruby": true}
	_ = v.Lines(doctorTestOpts(40))

	v.Tab = doctorview.TabDisk
	if act := v.HandleKey("x", 40); act != doctorview.DoctorToast || !strings.Contains(v.PendingToast, "タブ") {
		t.Errorf("ディスクのタブで x が走った: %v (%q)", act, v.PendingToast)
	}
	if v.Del.Active() {
		t.Fatal("ディスクのタブで brew の実行パネルが立った")
	}
	v.Tab = doctorview.TabBrew
	// 🚨 「ディスク」だけを見ない: ゲートを外しても beginDelete が
	// 「ディスクのスキャンが終わるまで待ってください」を返すので素通りする (変異で実測)
	if act := v.HandleKey("d", 40); act != doctorview.DoctorToast || !strings.Contains(v.PendingToast, "タブ") {
		t.Errorf("Homebrew のタブで d が削除へ入った: %v (%q)", act, v.PendingToast)
	}
	// hint も押せないキーを案内しない
	v.Tab = doctorview.TabBrew
	if h := v.Hint(120); strings.Contains(h, "d: 削除") {
		t.Errorf("Homebrew のタブで削除を案内した: %q", h)
	}
	v.Tab = doctorview.TabDisk
	if h := v.Hint(120); strings.Contains(h, "x:") {
		t.Errorf("ディスクのタブで brew の実行を案内した: %q", h)
	}
}
