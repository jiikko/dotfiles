package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"pro-con/backend"
)

// sendField は送る欄の 1 つ: 開いて中身を書いたモデルと、確認に出るはずの中身 (issue 517 の欄の一覧)。
type sendField struct {
	name  string
	open  func(t *testing.T) (*Model, *spy)
	shown []string // 確認の枠に出るはずの文 (題と送る中身)
	back  mode     // 取り消して戻る画面
	kept  func(m *Model) string
	sent  func(c backend.Command) bool // 届いた操作が、この欄の宛先と中身か
}

// confirmBox は送る前の確認の枠だけを描いた行 (下に残る入力欄・案内の行・カンバンを混ぜずに、枠の中を見る)。
func confirmBox(m *Model) []string {
	region := make([]string, m.height-headerRows-2)
	for i := range region {
		region[i] = strings.Repeat(" ", m.width)
	}
	out := m.overlaySend(region)
	for i, l := range out {
		out[i] = strings.TrimSpace(ansi.Strip(l))
	}
	return out
}

func lineKept(m *Model) string { return m.line.String() }

func sendFields() []sendField {
	typed := func(keys []string, text string) func(t *testing.T) (*Model, *spy) {
		return func(t *testing.T) (*Model, *spy) {
			be := newSpy()
			m := New(be, nil)
			press(m, keys...)
			if m.mode != modeInput {
				t.Fatalf("%v で入力欄が開かない", keys)
			}
			typeText(m, text)
			return m, be
		}
	}
	return []sendField{
		{name: "新しい依頼", open: typed([]string{"n"}, "日本語の依頼"), shown: []string{"新しい依頼", "日本語の依頼"}, back: modeInput, kept: lineKept,
			sent: func(c backend.Command) bool {
				r, ok := c.(backend.NewRequest)
				return ok && r.Text == "日本語の依頼" && r.Issue == nil
			}},
		{name: "回答 1 行", open: typed([]string{"right", "down", "r"}, "遅延ロードで"), shown: []string{"W2 へ回答", "遅延ロードで"}, back: modeInput, kept: lineKept,
			sent: func(c backend.Command) bool {
				a, ok := c.(backend.Answer)
				return ok && a.CardID == "W2" && a.Text == "遅延ロードで"
			}},
		{name: "追加オーダー", open: typed([]string{"+"}, "テストも足して"), shown: []string{"R1 へ追加オーダー", "テストも足して"}, back: modeInput, kept: lineKept,
			sent: func(c backend.Command) bool {
				o, ok := c.(backend.AddOrder)
				return ok && o.CardID == "R1" && o.Text == "テストも足して"
			}},
		{name: "btw", open: typed([]string{"w"}, "今どこ?"), shown: []string{"R1 に btw", "今どこ?"}, back: modeInput, kept: lineKept,
			sent: func(c backend.Command) bool {
				b, ok := c.(backend.Btw)
				return ok && b.CardID == "R1" && b.Question == "今どこ?"
			}},
		{name: "issue の補足", open: func(t *testing.T) (*Model, *spy) {
			m, be, _ := pickerModel(t)
			press(m, "i", "enter") // 最初の行 (epic 200)
			if m.mode != modeInput || m.inputKind != inputIssue {
				t.Fatal("補足の入力欄が開かない")
			}
			typeText(m, "急ぎで")
			return m, be
		}, shown: []string{"epic 200", "急ぎで"}, back: modeInput, kept: lineKept,
			sent: func(c backend.Command) bool {
				r, ok := c.(backend.NewRequest)
				return ok && r.Text == "急ぎで" && r.Issue != nil && r.Issue.Epic == "200"
			}},
		{name: "回答フォーム", open: func(t *testing.T) (*Model, *spy) {
			be := newFormSpy()
			m := openForm(t, be)
			pressForm(m, "tab", "tab") // 補足
			typeText(m, "急がない")
			return m, be
		}, shown: []string{"W2 へ回答", "1. 形: 丸", "2. 直すもの: 色", "補足: 急がない"}, back: modeForm, kept: func(m *Model) string { return m.form.note.String() },
			sent: func(c backend.Command) bool {
				a, ok := c.(backend.Answer)
				return ok && a.CardID == "W2" && a.Text == "1. 形: 丸\n2. 直すもの: 色\n補足: 急がない"
			}},
	}
}

// 送る欄はどれも enter で送らず確認を出し、確認に送る中身を出す。y / enter 以外では元の画面へ書いた中身のまま戻り、
// もう一度 enter → y で送る (1 つだけ届いてボードへ戻る)。
func TestEverySendFieldConfirmsBeforeSending(t *testing.T) {
	for _, f := range sendFields() {
		t.Run(f.name, func(t *testing.T) {
			m, be := f.open(t)
			want := f.kept(m)
			press(m, "enter")
			if len(be.applied) != 0 || m.mode != modeConfirm {
				t.Fatalf("enter で確認を出さずに送った / 確認が出ない: %#v mode=%v", be.applied, m.mode)
			}
			out := strings.Join(confirmBox(m), "\n")
			for _, s := range f.shown {
				if !strings.Contains(out, s) {
					t.Fatalf("確認に %q が出ていない:\n%s", s, out)
				}
			}
			press(m, "n")
			if len(be.applied) != 0 || m.mode != f.back || f.kept(m) != want {
				t.Fatalf("取り消しで書いた中身のまま戻らない: %#v mode=%v %q (元 %q)", be.applied, m.mode, f.kept(m), want)
			}
			press(m, "enter", "y")
			if len(be.applied) != 1 || m.mode != modeBoard || !f.sent(be.applied[0]) {
				t.Fatalf("確認で y を押しても、この欄の宛先と中身を 1 つ送ってボードへ戻らない: %#v mode=%v", be.applied, m.mode)
			}
		})
	}
}

// 確認は y と enter で送り (confirm.IsYesStrict。人が選んだ)、それ以外 (大文字 Y・esc・文字) は取り消し。
func TestSendConfirmKeys(t *testing.T) {
	for _, tc := range []struct {
		key  tea.KeyPressMsg
		sent bool
	}{
		{tea.KeyPressMsg{Code: 'y', Text: "y"}, true},
		{tea.KeyPressMsg{Code: tea.KeyEnter}, true},
		{tea.KeyPressMsg{Code: 'Y', Text: "Y"}, false},
		{tea.KeyPressMsg{Code: tea.KeyEscape}, false},
		{tea.KeyPressMsg{Code: 'あ', Text: "あ"}, false},
	} {
		be := newSpy()
		m := New(be, nil)
		press(m, "n")
		typeText(m, "依頼")
		press(m, "enter")
		m.Update(tc.key)
		if got := len(be.applied) == 1; got != tc.sent {
			t.Fatalf("%q の後: 送られた=%v 期待=%v", tc.key.String(), got, tc.sent)
		}
		if !tc.sent && (m.mode != modeInput || m.line.String() != "依頼") {
			t.Fatalf("%q の後に書いた文のまま入力欄へ戻らない: mode=%v %q", tc.key.String(), m.mode, m.line.String())
		}
	}
}

// 確認の最中の ctrl+c は、書いた中身を消して終了の入力欄へ移らない (入力中と同じく断る)。
func TestCtrlCInSendConfirmKeepsDraft(t *testing.T) {
	be := newSpy()
	m := New(be, nil)
	press(m, "n")
	typeText(m, "書きかけ")
	press(m, "enter")
	m.Update(ctrl('c'))
	if m.mode != modeConfirm || m.line.String() != "書きかけ" || m.pending == nil {
		t.Fatalf("ctrl+c で確認と書いた中身を捨てた: mode=%v %q", m.mode, m.line.String())
	}
	press(m, "y")
	if len(be.applied) != 1 {
		t.Fatalf("ctrl+c の後も確認から送れるはず: %#v", be.applied)
	}
}

// 方針変更は確認に「PG を止める」ことを出す (追記の確認には出さない)。
func TestRedirectConfirmWarns(t *testing.T) {
	for _, redirect := range []bool{false, true} {
		m := New(newSpy(), nil)
		press(m, "+")
		if redirect {
			m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		}
		typeText(m, "B 案で")
		press(m, "enter")
		if got := strings.Contains(strings.Join(confirmBox(m), "\n"), "PG を止めて"); got != redirect {
			t.Fatalf("方針変更=%v なのに止める知らせ=%v:\n%s", redirect, got, strings.Join(confirmBox(m), "\n"))
		}
	}
}

// 長い中身は枠の中で折り返して全部出す (枠の幅で切らない)。画面に入り切らないほどなら後ろを … で切り、案内は残す。
func TestSendConfirmWrapsWholeText(t *testing.T) {
	m := New(newSpy(), nil)
	press(m, "n")
	long := strings.Repeat("日本語の長い依頼", 30) + "おわり"
	m.Update(tea.PasteMsg{Content: long})
	press(m, "enter")
	boxText := func() string { // 枠の行を罫線と空白を除いて繋ぐ (折り返しを跨いで中身を探す)
		return strings.Join(strings.Fields(strings.ReplaceAll(strings.Join(confirmBox(m), ""), "│", "")), "")
	}
	if !strings.Contains(boxText(), long) {
		t.Fatalf("長い中身が枠の中に全部出ていない:\n%s", strings.Join(confirmBox(m), "\n"))
	}
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 14}) // 枠を置ける領域は 8 行 (中身は 10 行を越える)
	m.Update(tea.PasteMsg{Content: long})              // 確認の最中の貼り付けは捨てる
	if box := confirmBox(m); strings.Contains(boxText(), "おわり") || !strings.Contains(boxText(), "…") || !strings.Contains(boxText(), "y/enter送る") ||
		!strings.HasPrefix(box[0], "╭─") || !strings.HasPrefix(box[len(box)-1], "╰") {
		t.Fatalf("入り切らない中身を … で切って、案内と枠の上下を領域に残していない:\n%s", strings.Join(box, "\n"))
	}
	if m.line.String() != long {
		t.Fatalf("確認の最中の貼り付けが入力欄に入った: %q", m.line.String())
	}
}

// 貼り付けに改行が入っていても送らない (改行は空白になり、入力欄に残る)。
func TestPasteWithNewlineDoesNotSend(t *testing.T) {
	for _, f := range sendFields() {
		t.Run(f.name, func(t *testing.T) {
			m, be := f.open(t)
			m.Update(tea.PasteMsg{Content: "一行目\n二行目\r\n"})
			if len(be.applied) != 0 || m.mode != f.back || !strings.Contains(f.kept(m), "一行目 二行目") {
				t.Fatalf("改行つきの貼り付けで送った / 入らない: %#v mode=%v %q", be.applied, m.mode, f.kept(m))
			}
		})
	}
}
