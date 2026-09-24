package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"pro-con/backend"
)

// docs/glogx-ui-guide.md の語彙どおりに動くことを、入口 (Update) から固定する。

// 入力欄の編集キー: ctrl+h は backspace、カーソルを動かして途中を直した結果がそのまま送られる。
func TestInputEditKeysReachSubmittedText(t *testing.T) {
	be := newSpy()
	m := New(be, nil)
	press(m, "n")
	typeText(m, "遅延ロードx")
	m.Update(ctrl('h'))                           // 遅延ロード
	m.Update(ctrl('a'))                           // 先頭へ
	typeText(m, "zsh の ")                         // zsh の 遅延ロード
	m.Update(ctrl('e'))                           // 末尾へ
	typeText(m, "にして")                            // zsh の 遅延ロードにして
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // 送信
	r, ok := be.applied[len(be.applied)-1].(backend.NewRequest)
	if !ok || r.Text != "zsh の 遅延ロードにして" {
		t.Fatalf("編集した結果が送られていない: %#v", be.applied)
	}
}

// 方針変更は y / Enter でだけ送る。知らないキーは取り消し (§4)。
func TestRedirectNeedsConfirmation(t *testing.T) {
	for _, tc := range []struct {
		key  tea.KeyPressMsg
		sent bool
	}{
		{tea.KeyPressMsg{Code: 'y', Text: "y"}, true},
		{tea.KeyPressMsg{Code: tea.KeyEnter}, true},
		{tea.KeyPressMsg{Code: 'n', Text: "n"}, false},
		{tea.KeyPressMsg{Code: 'j', Text: "j"}, false},
	} {
		be := newSpy()
		m := New(be, nil)
		press(m, "+")
		m.Update(tea.KeyPressMsg{Code: tea.KeyTab}) // 方針変更
		typeText(m, "B 案で")
		press(m, "enter")
		if len(be.applied) != 0 {
			t.Fatal("確認の前に送られた")
		}
		m.Update(tc.key)
		if got := len(be.applied) == 1; got != tc.sent {
			t.Fatalf("%q の後: 送られた=%v 期待=%v", tc.key.String(), got, tc.sent)
		}
		if m.mode != modeBoard {
			t.Fatalf("%q の後にボードへ戻っていない", tc.key.String())
		}
	}
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// q は開いている板を手前から 1 つずつ閉じ、何も開いていなければ終了する。esc は閉じるだけで終了しない (§1 / §3)。
func TestQClosesBoardsBeforeQuitting(t *testing.T) {
	m := New(newSpy(), nil)
	press(m, "enter", "s") // 詳細と session の一覧を開く
	if isQuit(press(m, "q")) || m.showSessions || !m.showDetail {
		t.Fatalf("1 回目の q は session の一覧だけを閉じるはず: sessions=%v detail=%v", m.showSessions, m.showDetail)
	}
	if isQuit(press(m, "q")) || m.showDetail {
		t.Fatal("2 回目の q は詳細を閉じるはず")
	}
	if isQuit(press(m, "esc")) {
		t.Fatal("esc で終了した")
	}
	if !isQuit(press(m, "q")) {
		t.Fatal("何も開いていないときの q で終了しない")
	}
}

// b は移動の語彙 (半ページ上) のまま効き、btw は ? で開く。
func TestBIsMotionAndQuestionMarkIsBtw(t *testing.T) {
	m := keysModel(t)
	m.selected = "P4"
	m.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	if m.selected != "P2" || m.mode != modeBoard {
		t.Fatalf("b で半ページ上へ動くはず: %s mode=%v", m.selected, m.mode)
	}
	m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	if m.mode != modeInput || m.inputKind != inputBtw {
		t.Fatal("? で btw の入力欄が開かない")
	}
}

// 入力中の案内は入力欄のキーに差し替わる (ボードの案内を残すと載せた文字が入力に化ける。§5)。
func TestHintsFollowMode(t *testing.T) {
	m := New(newSpy(), nil)
	press(m, "n")
	out := ansi.Strip(m.render())
	last := out[strings.LastIndex(out, "\n")+1:]
	if !strings.Contains(last, "ctrl+h") || !strings.Contains(last, "esc 取り消し") || strings.Contains(last, "attach") {
		t.Fatalf("入力中の案内が入力欄のキーになっていない: %q", last)
	}
}

// 案内が幅に入らないときも、最後の項目 (抜ける手段) は残す。
func TestHintLineKeepsExit(t *testing.T) {
	got := hintLine([]string{"aaaa", "bbbb", "cccc", "q 終了"}, 14)
	if !strings.HasSuffix(got, "q 終了") || strings.Contains(got, "cccc") {
		t.Fatalf("抜ける手段を残して後ろから落とすはず: %q", got)
	}
}

// Enter の詳細は画面の下端 (案内の直上) に吸着する。詳細の高さがカードで違っても、案内は常に最下行で、
// 詳細の下枠はその直上にある (ボードの直後に置くと、下端との間が選択のたびに伸び縮みする)。
func TestDetailSticksToFooter(t *testing.T) {
	m := New(newSpy(), nil)
	m.width, m.height = 120, 60
	press(m, "enter")
	if !m.showDetail {
		t.Fatal("enter で詳細が開かない")
	}
	settle(m)
	for _, key := range []string{"", "j", "l"} {
		if key != "" {
			press(m, key)
		}
		lines := strings.Split(ansi.Strip(m.render()), "\n")
		if len(lines) != m.height {
			t.Fatalf("%q: 画面の行数 %d が高さ %d と違う", key, len(lines), m.height)
		}
		if !strings.HasPrefix(strings.TrimSpace(lines[len(lines)-2]), "╰") {
			t.Fatalf("%q: 詳細の下枠が案内の直上に無い: %q", key, lines[len(lines)-2])
		}
	}
}
