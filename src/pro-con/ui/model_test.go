package ui

import (
	"os/exec"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"pro-con/backend"
	"pro-con/card"
)

// spy は UI が backend に何を送ったかを記録する。状態は固定で、Poll しても変わらない。
type spy struct {
	snap     backend.Snapshot
	applied  []backend.Command
	attached []string
}

func (s *spy) Poll() backend.Snapshot     { return s.snap }
func (s *spy) Snapshot() backend.Snapshot { return s.snap }

// Apply は本物の backend と同じく空の本文を ErrEmptyText で拒否する (常に成功する spy だと UI のエラー経路が見えない)。
func (s *spy) Apply(c backend.Command) (string, error) {
	s.applied = append(s.applied, c)
	if a, ok := c.(backend.Answer); ok && a.Text == "" {
		return "", backend.ErrEmptyText
	}
	return "ok", nil
}
func (s *spy) AttachCommand(id string) (*exec.Cmd, error) {
	s.attached = append(s.attached, id)
	return exec.Command("true"), nil
}

func newSpy() *spy {
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	return &spy{snap: backend.Snapshot{Now: now, Limit: 2, DaemonTick: now, Cards: []card.Card{
		{ID: "R1", State: card.Running, Session: "s-r1", Since: now},
		{ID: "W1", State: card.Waiting, Session: "s-w1", Since: now, Wait: card.Wait{Kind: card.WaitQuestion, Question: "?"}},
		{ID: "W2", State: card.Waiting, Session: "s-w2", Since: now, Wait: card.Wait{Kind: card.WaitQuestion, Question: "?"}},
	}}}
}

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

func press(m *Model, keys ...string) tea.Cmd {
	var last tea.Cmd
	for _, k := range keys {
		_, last = m.Update(key(k))
	}
	return last
}

func typeText(m *Model, s string) {
	for _, r := range s {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// 回答の経路: 質問待ちのカードで r → 入力 → enter で、そのカード宛ての Answer が 1 つだけ backend に届く。
func TestAnswerReachesBackendForSelectedCard(t *testing.T) {
	be := newSpy()
	m := New(be)
	press(m, "right", "down") // 作業中の R1 → 質問待ちの列 (W1) → W2
	if m.selected != "W2" {
		t.Fatalf("選択が W2 に来ていない: %q", m.selected)
	}
	press(m, "r")
	typeText(m, "遅延ロードで")
	press(m, "enter")
	if len(be.applied) != 1 {
		t.Fatalf("backend に届いた操作は 1 つのはず: %d", len(be.applied))
	}
	a, ok := be.applied[0].(backend.Answer)
	if !ok || a.CardID != "W2" || a.Text != "遅延ロードで" || a.From != "人間" {
		t.Fatalf("届いた回答が違う: %#v", be.applied[0])
	}
	if m.mode != modeBoard {
		t.Fatal("送信後にボードへ戻っていない")
	}
}

func TestAnswerIsRefusedForCardThatIsNotWaiting(t *testing.T) {
	be := newSpy()
	m := New(be) // 最初の選択は作業中の R1
	press(m, "r")
	if m.mode != modeBoard {
		t.Fatal("作業中のカードで回答の入力欄が開いた")
	}
	typeText(m, "x")
	press(m, "enter")
	if len(be.applied) != 0 {
		t.Fatalf("質問待ちでないカードに操作が届いた: %#v", be.applied)
	}
}

func TestEscCancelsInputWithoutSending(t *testing.T) {
	be := newSpy()
	m := New(be)
	press(m, "o")
	typeText(m, "やっぱりやめる")
	press(m, "esc", "enter") // esc の後の enter はボードの詳細切り替えで、送信ではない
	if len(be.applied) != 0 {
		t.Fatalf("取り消した入力が送られた: %#v", be.applied)
	}
}

// 追加オーダーの種類は tab で切り替わり、選んだ種類のまま届く。
func TestOrderKindTabCycles(t *testing.T) {
	be := newSpy()
	m := New(be)
	press(m, "o")
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	typeText(m, "B 案で")
	press(m, "enter")
	o, ok := be.applied[0].(backend.AddOrder)
	if !ok || o.Kind != card.OrderRedirect || o.CardID != "R1" {
		t.Fatalf("方針変更として R1 に届くはず: %#v", be.applied[0])
	}
}

// attach は選択中のカードの session で backend に頼み、画面を明け渡すコマンドを返す。
func TestAttachUsesSelectedSession(t *testing.T) {
	be := newSpy()
	m := New(be)
	press(m, "right")
	if cmd := press(m, "a"); cmd == nil {
		t.Fatal("attach のコマンドが返らなかった")
	}
	if len(be.attached) != 1 || be.attached[0] != "s-w1" {
		t.Fatalf("W1 の session で attach するはず: %v", be.attached)
	}
}

// 選択はカード ID で持つので、カードが列を移っても同じカードを指し続ける。
func TestSelectionFollowsCardAcrossColumns(t *testing.T) {
	be := newSpy()
	m := New(be)
	press(m, "right") // W1
	be.snap.Cards[1].State = card.Review
	be.snap.Cards[1].Wait = card.Wait{}
	m.Update(tickMsg{})
	if m.selected != "W1" {
		t.Fatalf("列を移ったカードから選択が外れた: %q", m.selected)
	}
	if col, _, _ := m.position(); card.Columns[col] != card.Review {
		t.Fatalf("選択の位置がレビューの列になっていない: %s", card.Columns[col].Label())
	}
}

// ボードでのペーストはキー操作として解釈しない (貼った文字に a / r が混ざっても何も起きない)。
func TestPasteOnBoardDoesNothing(t *testing.T) {
	be := newSpy()
	m := New(be)
	m.Update(tea.PasteMsg{Content: "ara"})
	if len(be.attached) != 0 || m.mode != modeBoard {
		t.Fatalf("ボードへのペーストが操作として実行された: attached=%v mode=%v", be.attached, m.mode)
	}
	press(m, "o")
	m.Update(tea.PasteMsg{Content: "貼った本文"})
	if string(m.input) != "貼った本文" {
		t.Fatalf("入力欄へのペーストが入っていない: %q", string(m.input))
	}
}

// 空の本文で送ると backend が拒否する。そのとき入力欄は開いたままで、書き直して送れる。
func TestEmptySubmitKeepsInputOpen(t *testing.T) {
	be := newSpy()
	m := New(be)
	press(m, "right", "r", "enter")
	if m.mode != modeInput {
		t.Fatal("空の本文で拒否されたのに入力欄が閉じた")
	}
	typeText(m, "やっぱり遅延ロードで")
	press(m, "enter")
	if len(be.applied) != 2 {
		t.Fatalf("拒否された 1 回と書き直した 1 回の 2 回届くはず: %d", len(be.applied))
	}
	if a := be.applied[1].(backend.Answer); a.Text != "やっぱり遅延ロードで" || m.mode != modeBoard {
		t.Fatalf("書き直した回答が届いてボードへ戻るはず: %#v mode=%v", a, m.mode)
	}
}
