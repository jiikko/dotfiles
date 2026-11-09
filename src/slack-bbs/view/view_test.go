package view

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
	"slack-bbs/board"
	"slack-bbs/render"
	"slack-bbs/subs"
)

var t0 = time.Unix(1_000_000, 0)

func page(ch, title string) render.Page {
	u := board.Unit{ID: "1.000000", Thread: true, Msgs: []board.Msg{
		{Ts: "1.000000", Name: "tanaka", Text: "親の発言", New: true},
		{Ts: "2.000000", Name: "suzuki", Text: strings.Repeat("とても長い返信の本文です。", 20), New: true},
	}}
	return render.Page{Channel: ch, ChannelName: ch, Base: "https://ws.slack.com/", Since: t0, Until: t0, Lookback: t0,
		Topics: []board.Topic{{Title: title, State: board.StateOpen, Units: []board.Unit{u},
			Labels: []board.Label{{Kind: board.KindAction, Source: board.SourceMention, Reason: "名指し", Refs: []string{"2.000000"}}}}}}
}

// loader は呼ばれた回数を数え、チャンネルごとに決まった板 (または誤り) を返す。
type loader struct {
	calls map[string]int
	err   map[string]error
	title string
}

func (l *loader) load(_ context.Context, ch subs.Channel) (render.Page, error) {
	l.calls[ch.ID]++
	if err := l.err[ch.ID]; err != nil {
		return render.Page{}, err
	}
	return page(ch.ID, fmt.Sprintf("%s-%s-%d", l.title, ch.ID, l.calls[ch.ID])), nil
}

func newModel(l *loader) *Model {
	m := New(context.Background(), []subs.Channel{{ID: "C1", Name: "dev"}, {ID: "C2", Name: "ops"}}, l.load, nil, func() time.Time { return t0 })
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 20})
	return m
}

// run は Cmd を同期で実行して結果を画面へ渡す (bubbletea の代わり)。
func run(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		for _, c := range b {
			run(m, c)
		}
		return
	}
	m.Update(msg)
}

func press(m *Model, k string) tea.Cmd {
	_, cmd := m.Update(keyMsg(k))
	return cmd
}

func keyMsg(k string) tea.KeyPressMsg {
	switch k {
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	}
	return tea.KeyPressMsg{Code: rune(k[0]), Text: k}
}

// 起動時は最初のチャンネルだけを読み、他は開いたときに 1 回だけ読む。r は今のチャンネルだけ、R は全部を読み直す。
func TestLoadsLazilyAndReloadScope(t *testing.T) {
	l := &loader{calls: map[string]int{}}
	m := newModel(l)
	run(m, m.Init())
	if l.calls["C1"] != 1 || l.calls["C2"] != 0 {
		t.Fatalf("起動時の読み込み: %v", l.calls)
	}
	run(m, press(m, "tab"))
	run(m, press(m, "tab"))
	if l.calls["C1"] != 1 || l.calls["C2"] != 1 {
		t.Fatalf("Tab での読み込み: %v", l.calls)
	}
	run(m, press(m, "r"))
	if l.calls["C1"] != 2 || l.calls["C2"] != 1 {
		t.Fatalf("r は今のチャンネルだけ: %v", l.calls)
	}
	run(m, press(m, "R"))
	if l.calls["C1"] != 3 || l.calls["C2"] != 2 {
		t.Fatalf("R は全部: %v", l.calls)
	}
}

// 後から頼んだ読み直しの結果が先に届いたら、遅れて届いた古い結果で上書きしない。
func TestStaleReloadResultIsDropped(t *testing.T) {
	l := &loader{calls: map[string]int{}}
	m := newModel(l)
	l.title = "old"
	oldMsg := m.reload(0)() // 古い読み直しは先に走り終え、結果の到着だけが遅れる
	l.title = "new"
	run(m, m.reload(0))
	m.Update(oldMsg)
	if got := m.topics()[0].Title; got != "new-C1-2" {
		t.Errorf("古い結果で上書きされた: %q (want new-C1-2)", got)
	}
}

// R は読み込み中のチャンネルを飛ばす (同じチャンネルの claude を 2 本走らせない)。
func TestReloadAllSkipsLoading(t *testing.T) {
	l := &loader{calls: map[string]int{}}
	m := newModel(l)
	pending := m.Init() // C1 は読み込み中のまま
	run(m, press(m, "R"))
	if l.calls["C1"] != 0 || l.calls["C2"] != 1 {
		t.Errorf("R が読み込み中の C1 も読んだ: %v", l.calls)
	}
	run(m, pending)
}

// 初回の読み込みの失敗の文 (slack-cli の stderr を含む) は、改行・制御文字を落として枠の中に収める。
func TestFirstLoadErrorIsSanitized(t *testing.T) {
	l := &loader{calls: map[string]int{}, err: map[string]error{"C1": errors.New("slack: rc=1\n\x1b[2J行 2")}}
	m := newModel(l)
	run(m, m.Init())
	lines := m.Lines()
	if len(lines) != 20 {
		t.Errorf("行の数 = %d, want 20", len(lines))
	}
	for _, ln := range lines {
		if strings.ContainsAny(ln, "\n\x1b") && !strings.Contains(ln, "\x1b[7m") && !strings.Contains(ln, "\x1b[27m") {
			t.Errorf("制御文字・改行が出た: %q", ln)
		}
	}
	if !strings.Contains(strings.Join(lines, "\n"), "読めなかった") {
		t.Errorf("失敗が出ていない")
	}
}

// 小さい端末では枠を描かず、幅・高さを超えない。
func TestTinyTerminal(t *testing.T) {
	l := &loader{calls: map[string]int{}}
	m := newModel(l)
	run(m, m.Init())
	for _, sz := range [][2]int{{0, 0}, {10, 3}, {MinWidth - 1, 30}, {100, MinHeight - 1}} {
		m.Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		lines := m.Lines()
		if len(lines) > max(sz[1], 1) {
			t.Errorf("%v: 行の数 %d", sz, len(lines))
		}
		for _, ln := range lines {
			if termwidth.Of(ln) > max(sz[0], 1) {
				t.Errorf("%v: 幅 %d", sz, termwidth.Of(ln))
			}
		}
	}
}

// 読み直しで話題が並び替わっても、見ていた話題にカーソルを戻す。
func TestReloadKeepsSelectedTopic(t *testing.T) {
	order := [][]string{{"a", "b"}, {"b", "a"}}
	n := 0
	load := func(context.Context, subs.Channel) (render.Page, error) {
		var ts []board.Topic
		for _, id := range order[min(n, 1)] {
			ts = append(ts, board.Topic{Title: id, State: board.StateInfo, Units: []board.Unit{{ID: id, Msgs: []board.Msg{{Ts: id}}}}})
		}
		n++
		return render.Page{Topics: ts}, nil
	}
	m := New(context.Background(), []subs.Channel{{ID: "C1"}}, load, nil, func() time.Time { return t0 })
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 20})
	run(m, m.Init())
	press(m, "j") // b を選ぶ
	run(m, press(m, "r"))
	if tp, _ := m.selected(); tp.Title != "b" {
		t.Errorf("読み直しの後の選択 = %q, want b", tp.Title)
	}
}

// 読み直しに失敗しても前の板は残し、失敗を画面に出す。
func TestFailedReloadKeepsPreviousBoard(t *testing.T) {
	l := &loader{calls: map[string]int{}, err: map[string]error{}}
	m := newModel(l)
	run(m, m.Init())
	l.err["C1"] = errors.New("claude が落ちた")
	run(m, press(m, "r"))
	if len(m.topics()) != 1 {
		t.Fatalf("前の板が消えた")
	}
	screen := strings.Join(m.Lines(), "\n")
	if !strings.Contains(screen, "読み直しに失敗") || !strings.Contains(screen, "claude が落ちた") {
		t.Errorf("失敗が画面に出ていない:\n%s", screen)
	}
}

// 走っている読み直しがあるあいだの r は、新しく claude を起こさない。
func TestReloadWhileLoadingIsIgnored(t *testing.T) {
	l := &loader{calls: map[string]int{}}
	m := newModel(l)
	pending := m.Init()
	if cmd := press(m, "r"); cmd != nil {
		t.Error("読み込み中の r で読み直しを頼んだ")
	}
	run(m, pending)
}

// 画面の行は端末の幅を超えず、行の数は端末の高さに合う (全角・絵文字・長い本文があっても)。
func TestLinesFitTerminal(t *testing.T) {
	l := &loader{calls: map[string]int{}}
	m := newModel(l)
	run(m, m.Init())
	for _, k := range []string{"", "enter"} {
		if k != "" {
			press(m, k)
		}
		lines := m.Lines()
		if len(lines) != 20 {
			t.Errorf("行の数 = %d, want 20", len(lines))
		}
		for i, ln := range lines {
			if w := termwidth.Of(ln); w > 90 {
				t.Errorf("%d 行目の幅 %d > 90: %q", i, w, ln)
			}
		}
	}
}

// 端末の制御文字は画面に出さない (見出し・本文・チャンネル名・誤りの文)。
func TestLinesStripControls(t *testing.T) {
	esc := "\x1b]0;x\x07"
	l := &loader{calls: map[string]int{}, title: esc}
	m := New(context.Background(), []subs.Channel{{ID: "C1", Name: "dev" + esc}}, l.load, nil, func() time.Time { return t0 })
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 20})
	run(m, m.Init())
	if s := strings.Join(m.Lines(), "\n"); strings.Contains(s, "\x07") || strings.Contains(s, "\x1b]") {
		t.Errorf("制御文字が出た: %q", s)
	}
}

// o は選んだ話題の最後の発言を開く (スレッドの返信は thread_ts 付きの permalink)。
func TestOpenSelectedOpensLatestMessage(t *testing.T) {
	// 話題に単位が 2 つあり、最後の発言は先頭でない単位 (スレッド) の返信にある
	load := func(context.Context, subs.Channel) (render.Page, error) {
		thread := board.Unit{ID: "1.000000", Thread: true, Msgs: []board.Msg{{Ts: "1.000000"}, {Ts: "9.000000"}}}
		single := board.Unit{ID: "5.000000", Msgs: []board.Msg{{Ts: "5.000000"}}}
		return render.Page{Channel: "C1", Base: "https://ws.slack.com/",
			Topics: []board.Topic{{Title: "t", State: board.StateInfo, Units: []board.Unit{single, thread}}}}, nil
	}
	var opened string
	m := New(context.Background(), []subs.Channel{{ID: "C1", Name: "dev"}}, load,
		func(u string) error { opened = u; return nil }, func() time.Time { return t0 })
	run(m, m.Init())
	press(m, "o")
	if want := "https://ws.slack.com/archives/C1/p9000000?thread_ts=1.000000&cid=C1"; opened != want {
		t.Errorf("開いた URL = %q, want %q", opened, want)
	}
}
