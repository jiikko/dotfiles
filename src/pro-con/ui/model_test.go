package ui

import (
	"errors"
	"os/exec"
	"slices"
	"strings"
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
	return &spy{snap: backend.Snapshot{Now: now, Limit: 2, DispatcherTick: now, Cards: []card.Card{
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
	m := New(be, nil)
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
	m := New(be, nil) // 最初の選択は作業中の R1
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
	m := New(be, nil)
	press(m, "+")
	typeText(m, "やっぱりやめる")
	press(m, "esc", "enter") // esc の後の enter はボードの詳細切り替えで、送信ではない
	if len(be.applied) != 0 {
		t.Fatalf("取り消した入力が送られた: %#v", be.applied)
	}
}

// 追加オーダーの種類は tab で切り替わり、選んだ種類のまま届く。
func TestOrderKindTabCycles(t *testing.T) {
	be := newSpy()
	m := New(be, nil)
	press(m, "+")
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	typeText(m, "B 案で")
	press(m, "enter", "y") // 方針変更は確認を挟む
	o, ok := be.applied[0].(backend.AddOrder)
	if !ok || o.Kind != card.OrderRedirect || o.CardID != "R1" {
		t.Fatalf("方針変更として R1 に届くはず: %#v", be.applied[0])
	}
}

// attach は選択中のカードの session で backend に頼み、画面を明け渡すコマンドを返す。
func TestAttachUsesSelectedSession(t *testing.T) {
	be := newSpy()
	m := New(be, nil)
	press(m, "right")
	cmd := press(m, "a")
	if cmd == nil {
		t.Fatal("attach のコマンドが返らなかった")
	}
	ready, ok := cmd().(attachReadyMsg) // 照合は裏で行い、結果が届いてから画面を明け渡す
	if !ok {
		t.Fatal("attach の準備の知らせが返らない")
	}
	if _, exec := m.Update(ready); exec == nil {
		t.Fatal("準備が済んだのに画面を明け渡すコマンドが返らない")
	}
	if len(be.attached) != 1 || be.attached[0] != "s-w1" {
		t.Fatalf("W1 の session で attach するはず: %v", be.attached)
	}
}

// 選択はカード ID で持つので、カードが列を移っても同じカードを指し続ける。
func TestSelectionFollowsCardAcrossColumns(t *testing.T) {
	be := newSpy()
	m := New(be, nil)
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
	if card.Columns[m.col] != card.Review {
		t.Fatalf("レーンのフォーカスがカードに付いていかない: %s", card.Columns[m.col].Label())
	}
}

// ボードでのペーストはキー操作として解釈しない (貼った文字に a / r が混ざっても何も起きない)。
func TestPasteOnBoardDoesNothing(t *testing.T) {
	be := newSpy()
	m := New(be, nil)
	if _, cmd := m.Update(tea.PasteMsg{Content: "ara"}); cmd != nil {
		cmd() // attach は裏で頼むので、返ったコマンドまで走らせないと「頼んでいない」を確かめられない
	}
	if len(be.attached) != 0 || m.mode != modeBoard {
		t.Fatalf("ボードへのペーストが操作として実行された: attached=%v mode=%v", be.attached, m.mode)
	}
	press(m, "+")
	m.Update(tea.PasteMsg{Content: "貼った本文"})
	if m.line.String() != "貼った本文" {
		t.Fatalf("入力欄へのペーストが入っていない: %q", m.line.String())
	}
}

// 空の本文で送ると backend が拒否する。そのとき入力欄は開いたままで、書き直して送れる。
func TestEmptySubmitKeepsInputOpen(t *testing.T) {
	be := newSpy()
	m := New(be, nil)
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

func newRepoSpy() *spy {
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	return &spy{snap: backend.Snapshot{Now: now, Limit: 2, DispatcherTick: now, Cards: []card.Card{
		{ID: "D1", Repo: "dotfiles", State: card.Running, Since: now},
		{ID: "O1", Repo: "obaket", State: card.Running, Since: now},
		{ID: "X1", Repo: "outside", State: card.Planned, Since: now},
		{ID: "D2", Repo: "dotfiles", State: card.Planned, Since: now},
	}}}
}

func visibleIDs(m *Model) []string {
	var ids []string
	for _, c := range m.visible() {
		ids = append(ids, c.ID)
	}
	return ids
}

// タブは global + 「config に在り、カードも在る repo」。config の外の repo (outside) と、
// カードの無い repo (empty) はタブにならない。
func TestTabsAreConfiguredReposWithCards(t *testing.T) {
	m := New(newRepoSpy(), repos("dotfiles", "empty", "obaket"))
	if got, want := m.tabs(), []string{"", "dotfiles", "obaket"}; !slices.Equal(got, want) {
		t.Fatalf("タブが違う: got %q want %q", got, want)
	}
}

// tab で repo のタブへ移ると、そのタブの repo のカードだけが見え、選択もその中に入る。一周すると global に戻る。
func TestTabFiltersCardsAndSelection(t *testing.T) {
	m := New(newRepoSpy(), repos("dotfiles", "obaket"))
	if len(visibleIDs(m)) != 4 {
		t.Fatalf("global は全カード (config の外も含む): %v", visibleIDs(m))
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if got := visibleIDs(m); !slices.Equal(got, []string{"D1", "D2"}) || m.tab != "dotfiles" {
		t.Fatalf("dotfiles タブのカードが違う: tab=%q %v", m.tab, got)
	}
	if m.selected != "D1" && m.selected != "D2" {
		t.Fatalf("選択が dotfiles のカードに入っていない: %q", m.selected)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.tab != "" {
		t.Fatalf("shift+tab で global に戻るはず: %q", m.tab)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.tab != "" {
		t.Fatalf("tab で一周すると global に戻るはず: %q", m.tab)
	}
}

// 選んでいる repo のカードが全部消えたらタブも消えるので、global へ戻す (空の画面に取り残さない)。
func TestTabFallsBackToGlobalWhenRepoEmpties(t *testing.T) {
	be := newRepoSpy()
	m := New(be, repos("dotfiles", "obaket"))
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab}) // obaket
	if m.tab != "obaket" {
		t.Fatalf("前提: obaket タブに居ない: %q", m.tab)
	}
	be.snap.Cards = slices.DeleteFunc(be.snap.Cards, func(c card.Card) bool { return c.Repo == "obaket" })
	m.Update(tickMsg{})
	if m.tab != "" || len(visibleIDs(m)) != 3 {
		t.Fatalf("global に戻って全カードが見えるはず: tab=%q %v", m.tab, visibleIDs(m))
	}
}

func keyTab() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyTab} }

func repos(names ...string) []backend.Repo {
	var out []backend.Repo
	for _, n := range names {
		out = append(out, backend.Repo{Name: n, Path: "/src/" + n})
	}
	return out
}

// repo のタブで n → 本文 → enter は、そのタブの repo (名前とパス) をスコープにした依頼として届く。
func TestNewRequestInRepoTabCarriesScope(t *testing.T) {
	be := newRepoSpy()
	m := New(be, repos("dotfiles", "obaket"))
	m.Update(keyTab()) // dotfiles
	press(m, "n")
	typeText(m, "検索を足して")
	press(m, "enter")
	r, ok := be.applied[len(be.applied)-1].(backend.NewRequest)
	if !ok || r.Repo != (backend.Repo{Name: "dotfiles", Path: "/src/dotfiles"}) || r.Text != "検索を足して" {
		t.Fatalf("dotfiles をスコープにした依頼として届くはず: %#v", be.applied)
	}
}

// global のタブで出した依頼は repo を持たない (PM が判断する)。
func TestNewRequestInGlobalTabHasNoScope(t *testing.T) {
	be := newRepoSpy()
	m := New(be, repos("dotfiles"))
	press(m, "n")
	typeText(m, "どこかの件")
	press(m, "enter")
	r, ok := be.applied[len(be.applied)-1].(backend.NewRequest)
	if !ok || r.Repo != (backend.Repo{}) {
		t.Fatalf("global の依頼は repo を持たないはず: %#v", be.applied)
	}
}

// 実行中のコマンドはバッジに出る (リソースを占有していればその名前も)。
func TestBadgeShowsRunningCommand(t *testing.T) {
	be := newSpy()
	now := be.snap.Now
	be.snap.Cards = []card.Card{{ID: "R1", State: card.Running, Since: now,
		Exec: card.Exec{Command: "make e2e-device", Resource: "device", Since: now.Add(-3 * time.Minute)}}}
	m := New(be, nil)
	if b := m.badge(be.snap.Cards[0]); !strings.Contains(b, "▶ device: make e2e-device 3分") {
		t.Fatalf("バッジに実行中のコマンドが無い: %q", b)
	}
}

// attach の照合を待つ間に画面が変わったら (入力欄を開いた・別のカードを選んだ)、端末を明け渡さない。照合の途中の 2 度押しは 2 回起動しない。
func TestAttachAbortsWhenScreenChanged(t *testing.T) {
	be := newSpy()
	m := New(be, nil)
	press(m, "right")
	cmd := press(m, "a")
	if again := press(m, "a"); again != nil {
		t.Fatal("照合の途中にもう一度 a を押したら、2 つ目の attach が走った")
	}
	ready := cmd().(attachReadyMsg)
	press(m, "n") // 待っている間に入力欄を開いた
	if _, exec := m.Update(ready); exec != nil {
		t.Fatal("入力欄を開いているのに端末を明け渡した")
	}
	if !strings.Contains(m.flash, "取りやめた") {
		t.Fatalf("取りやめた旨が出ない: %q", m.flash)
	}
}

// 照合を待つ間に別のカードを選んだ・終了の入力欄を開いたときも、端末を明け渡さない。
func TestAttachAbortsOnSelectionOrQuit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Model)
	}{
		{"別のカードを選んだ", func(m *Model) { press(m, "down") }},
		{"終了の入力欄を開いた", func(m *Model) { press(m, "Q") }},
	} {
		m := New(newSpy(), nil)
		press(m, "right") // 質問待ちの W1 (下に W2 がある)
		ready := press(m, "a")().(attachReadyMsg)
		tc.change(m)
		if _, exec := m.Update(ready); exec != nil {
			t.Fatalf("%s のに端末を明け渡した", tc.name)
		}
	}
}

// recorder は attach の間の指示を残す口を持つ spy (backend.AttachRecorder)。
type recorder struct {
	*spy
	card, session string
	from, to      time.Time
	n             int
	err           error
}

func (r *recorder) RecordAttach(cardID, session string, from, to time.Time) (int, error) {
	r.card, r.session, r.from, r.to = cardID, session, from, to
	return r.n, r.err
}

// 戻りの知らせは、端末を明け渡した時刻を持つ (この時刻より前に打った文は attach の間の指示ではない)。
func TestAttachDoneCarriesHandOverTime(t *testing.T) {
	m := New(newSpy(), nil)
	at := time.Date(2026, 9, 24, 10, 1, 0, 0, time.UTC)
	m.now = func() time.Time { return at }
	var back tea.ExecCallback
	m.execProcess = func(_ *exec.Cmd, f tea.ExecCallback) tea.Cmd { back = f; return func() tea.Msg { return nil } }
	press(m, "right") // W1
	m.Update(press(m, "a")())
	if back == nil {
		t.Fatal("端末を明け渡していない")
	}
	done, ok := back(nil).(attachDoneMsg)
	if !ok || done.cardID != "W1" || done.session != "s-w1" || !done.from.Equal(at) {
		t.Fatalf("戻りの知らせに明け渡した時刻とカードの session が無い: %+v", done)
	}
}

// attach から戻ったら、明け渡した時刻から戻った時刻までの指示をカードに残すよう裏で頼む。残せなければ消えない通知で出す (issue 428)。
func TestAttachDoneRecordsInstructions(t *testing.T) {
	rec := &recorder{spy: newSpy(), n: 2}
	m := New(rec, nil)
	back := time.Date(2026, 9, 24, 10, 5, 0, 0, time.UTC)
	m.now = func() time.Time { return back }
	from := back.Add(-3 * time.Minute)
	_, cmd := m.Update(attachDoneMsg{cardID: "W1", session: "s-w1", from: from, err: errors.New("exit 1")})
	if cmd == nil {
		t.Fatal("失敗で戻っても、それまでの指示を残すよう頼むはず")
	}
	m.Update(cmd())
	if rec.card != "W1" || rec.session != "s-w1" || !rec.from.Equal(from) || !rec.to.Equal(back) {
		t.Fatalf("attach の窓を渡していない: %+v", rec)
	}
	if !strings.Contains(m.flash, "2 件") {
		t.Fatalf("残した件数を知らせない: %q", m.flash)
	}
	rec.err = errors.New("読めない")
	_, cmd = m.Update(attachDoneMsg{cardID: "W1", session: "s-w1", from: from})
	m.Update(cmd())
	if !strings.Contains(m.sticky, "残せなかった") {
		t.Fatalf("残せなかったことを消えない通知で出さない: %q", m.sticky)
	}
}
