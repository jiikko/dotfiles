package view

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
	"slack-bbs/board"
	"slack-bbs/render"
	"slack-bbs/source"
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
	m := quiet(New(context.Background(), []subs.Channel{{ID: "C1", Name: "dev"}, {ID: "C2", Name: "ops"}}, l.load, nil, func() time.Time { return t0 }))
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 20})
	return m
}

// quiet は toast の時計を張らない画面にする (run が tea.Tick を実時間で待たないように)。toast を動かすテストは tickMsg を自分で送る。
func quiet(m *Model) *Model {
	m.after = func(time.Duration, func(time.Time) tea.Msg) tea.Cmd { return nil }
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
	run(m, press(m, "r"))
	if l.calls["C1"] != 0 {
		t.Errorf("読み込み中の r で読み直しを頼んだ: %v", l.calls)
	}
	if !strings.Contains(m.LastToast(), "#dev は読み込み中なので、読み直せない") {
		t.Errorf("読み込み中の r の知らせ = %q", m.LastToast())
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

// 狭い端末でも、見出しの行には今のチャンネルのタブと状態が出る (タブの並びの右端を切って、どれを見ているかを消さない)。
func TestHeaderKeepsCurrentTabAndState(t *testing.T) {
	var chans []subs.Channel
	for i := range 6 {
		// タブの幅 22 (「#」+ 21)。幅 80 から状態の 13 桁を引くと 3 本は入らず、引かなければ 3 本入って状態が切れる
		chans = append(chans, subs.Channel{ID: fmt.Sprintf("C%d", i), Name: fmt.Sprintf("ch-%d-あいうえおかきく", i)})
	}
	l := &loader{calls: map[string]int{}}
	m := quiet(New(context.Background(), chans, l.load, nil, func() time.Time { return t0 }))
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	run(m, m.Init())
	for _, cur := range []int{0, 3, 5} {
		for m.cur != cur {
			run(m, press(m, "tab"))
		}
		h := m.header()
		if want := "\x1b[7m#" + chans[cur].Name + "(1)\x1b[27m"; !strings.Contains(h, want) { // (1) は未読の話題の数
			t.Errorf("cur=%d: 今のタブが無い: %q", cur, h)
		}
		if !strings.HasSuffix(h, "   最終 "+t0.Format("15:04")) {
			t.Errorf("cur=%d: 状態が無い: %q", cur, h)
		}
		if cur > 0 && !strings.HasPrefix(h, tabMore+tabGap) {
			t.Errorf("cur=%d: 畳んだ左のタブの印が無い: %q", cur, h)
		}
	}
	// 幅が足りれば全部のタブを出し、畳んだ印は出さない
	m.Update(tea.WindowSizeMsg{Width: 300, Height: 20})
	if h := m.header(); strings.Contains(h, tabMore) || strings.Count(h, "#ch-") != 6 {
		t.Errorf("広い端末で畳んだ: %q", h)
	}
	// 今のタブだけでも状態と並べて入らないときは、タブの名前を縮めて状態を丸ごと残す
	m.chans[m.cur].ch.Name = strings.Repeat("とても長いチャンネル名", 6)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	h := m.header()
	if !strings.HasSuffix(h, "\x1b[27m   最終 "+t0.Format("15:04")) || !strings.HasPrefix(h, "\x1b[7m#とても") || termwidth.Of(h) > 60 {
		t.Errorf("長い名前で状態が切れた (または反転が閉じていない): %q", h)
	}
	// 名前だけなら入るが、両側の畳んだ印を足すと入らない今のタブ (中ほど) でも、状態は丸ごと残る
	for m.cur != 3 {
		run(m, press(m, "tab"))
	}
	m.chans[m.cur].ch.Name = "ch-あいうえおかきくけ" // タブの幅 22。幅 40 から状態の 13 桁を引いた 27 に、印 6 桁を足すと入らない
	m.Update(tea.WindowSizeMsg{Width: MinWidth, Height: 20})
	if h := m.header(); !strings.HasSuffix(h, "   最終 "+t0.Format("15:04")) || !strings.Contains(h, "\x1b[7m#ch-あ") {
		t.Errorf("印の分だけ押し出されて状態が切れた: %q", h)
	}
	// 状態の文が長くて今のタブの残りが minTab に満たなくても (残りが負・0〜11)、今のタブは頭の数文字を読める幅で残し、状態の方を詰める
	c := &m.chans[m.cur]
	for _, st := range []struct {
		name          string
		loading, page bool
		want          string
	}{
		{"読み直しに失敗 (残り -1)", false, true, "🚨 読み直し"},
		{"読み直し中 (残り 5)", true, true, "読み直し中"},
		{"読めなかった (残り 7)", false, false, "🚨 読めなかった"},
	} {
		saved := c.page
		c.err, c.loading = errors.New("boom"), st.loading
		if !st.page {
			c.page = nil
		}
		if h := m.header(); !strings.HasPrefix(h, "\x1b[7m#ch-あいう") || !strings.Contains(h, st.want) || termwidth.Of(h) > MinWidth {
			t.Errorf("%s: 今のタブが読めない: %q", st.name, h)
		}
		c.page = saved
	}
}

// 最初の読み込みが失敗したら、見出しの行にもそう出す (状態が空だと、読んでいるのか止まったのか分からない)。
func TestHeaderShowsFirstLoadError(t *testing.T) {
	l := &loader{calls: map[string]int{}, err: map[string]error{"C1": errors.New("boom")}}
	m := newModel(l)
	run(m, m.Init())
	if h := m.header(); !strings.Contains(h, "読めなかった (r で読み直す)") {
		t.Errorf("見出しに失敗が出ていない: %q", h)
	}
}

// キャッシュの前回の板は、読み直せるまで出して「キャッシュ」と分かるようにし、開いたチャンネルは読み直す (前回の板のまま止めない)。
func TestPreloadedBoardIsShownThenReloaded(t *testing.T) {
	l := &loader{calls: map[string]int{}}
	m := newModel(l)
	at := time.Date(2026, 11, 9, 8, 30, 0, 0, time.Local)
	for i, ch := range []string{"C1", "C2"} {
		m.Preload(i, page(ch, "前回-"+ch), at)
	}
	cmd := m.Init()
	if h := m.header(); !strings.Contains(h, "11/09 08:30 のキャッシュを出している") {
		t.Errorf("読み直し中にキャッシュと分からない: %q", h)
	}
	if !strings.Contains(strings.Join(m.Lines(), "\n"), "前回-C1") {
		t.Errorf("読み直しを待たずに前回の板を出していない")
	}
	run(m, cmd)
	if h := m.header(); !strings.Contains(h, "最終 ") || strings.Contains(h, "キャッシュ") {
		t.Errorf("読み直した後もキャッシュと出ている: %q", h)
	}
	run(m, press(m, "tab"))
	if l.calls["C2"] != 1 {
		t.Errorf("キャッシュしか無いチャンネルを開いても読み直さない (C2 %d 回)", l.calls["C2"])
	}
	run(m, press(m, "tab"))
	run(m, press(m, "tab"))
	if l.calls["C2"] != 1 {
		t.Errorf("読み直した後のチャンネルを、開くたびに読み直した (C2 %d 回)", l.calls["C2"])
	}
}

// 前回の分類を使い回した板は、分類した時刻を見出しに添える (取得は新しくても、見出しやラベルは古い)。
func TestHeaderShowsReusedClassificationTime(t *testing.T) {
	l := &loader{calls: map[string]int{}}
	m := New(context.Background(), []subs.Channel{{ID: "C1", Name: "dev"}}, func(ctx context.Context, ch subs.Channel) (render.Page, error) {
		p, err := l.load(ctx, ch)
		p.ClassifiedAt = t0.Add(-3 * time.Hour)
		return p, err
	}, nil, func() time.Time { return t0 })
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	run(m, m.Init())
	if h, want := m.header(), "(分類は "+t0.Add(-3*time.Hour).Format("01/02 15:04")+")"; !strings.Contains(h, want) {
		t.Errorf("見出し %q に %q が無い", h, want)
	}
}

// 本文の行は色付けしてから出す (見出しの行に色が付く)。
func TestBodyLinesAreHighlighted(t *testing.T) {
	l := &loader{calls: map[string]int{}}
	m := newModel(l)
	run(m, m.Init())
	if s := strings.Join(m.Lines(), "\n"); !strings.Contains(s, sgrHeading+render.HeadingMark+"原文") {
		t.Errorf("本文の見出しに色が付いていない:\n%q", s)
	}
}

// subsModel は C1 (dev)・C2 (ops) を読む画面に、C1〜C3 から選べる購読の画面を付ける。saved に保存した購読を入れる。
func subsModel(l *loader, saved *[][]subs.Channel, opened *int) *Model {
	m := newModel(l)
	all := []source.Channel{{ID: "C1", Name: "dev", IsMember: true}, {ID: "C2", Name: "ops", IsMember: true}, {ID: "C3", Name: "qa", IsMember: true}}
	m.SetSubscriptions(&Subscriptions{
		Open: func(context.Context) (*subs.Picker, error) {
			*opened++
			cur := []subs.Channel{{ID: "C1", Name: "dev"}, {ID: "C2", Name: "ops"}}
			if len(*saved) > 0 {
				cur = (*saved)[len(*saved)-1]
			}
			return subs.NewPicker(all, cur), nil
		},
		Save: func(sel []subs.Channel) error { *saved = append(*saved, sel); return nil },
	})
	return m
}

func ids(m *Model) string {
	var out []string
	for _, c := range m.chans {
		out = append(out, c.ch.ID)
	}
	return strings.Join(out, ",")
}

// s で購読の画面を開き、保存したら閉じずにタブを入れ替える。残るチャンネルは読んだ板をそのまま持ち (読み直さない)、
// 外したチャンネルの読み込み中の結果は捨てる。
func TestSubscriptionsPickerReplacesTabs(t *testing.T) {
	l := &loader{calls: map[string]int{}}
	var saved [][]subs.Channel
	opened := 0
	m := subsModel(l, &saved, &opened)
	run(m, m.Init())
	pending := press(m, "tab") // C2 を読み始める (結果はまだ渡さない)
	run(m, press(m, "tab"))    // C1 へ戻る
	run(m, press(m, "s"))
	if !strings.Contains(strings.Join(m.Lines(), "\n"), "購読するチャンネルを選ぶ") {
		t.Fatalf("購読の画面が開いていない:\n%s", strings.Join(m.Lines(), "\n"))
	}
	// 並びは dev・ops・qa。ops を外して qa を付ける
	for _, k := range []string{"j", "x", "j", "x", "enter"} {
		run(m, press(m, k))
	}
	if len(saved) != 1 || ids(m) != "C1,C3" {
		t.Fatalf("保存 %v / タブ %s (want C1,C3)", saved, ids(m))
	}
	if m.chans[m.cur].ch.ID != "C1" || m.chans[m.cur].page == nil || l.calls["C1"] != 1 {
		t.Errorf("見ていた C1 の板を持ち続けていない (cur=%s calls=%d)", m.chans[m.cur].ch.ID, l.calls["C1"])
	}
	run(m, pending) // 外した C2 の結果
	if ids(m) != "C1,C3" || m.chans[1].page != nil {
		t.Errorf("外したチャンネルの結果を受け取った: %s", ids(m))
	}
	if !strings.Contains(m.header(), "#qa") {
		t.Errorf("見出しに新しいタブが無い: %q", m.header())
	}
}

// 外して付け直したチャンネルでは、外す前に頼んだ読み直しの結果を、付け直した後の読み直しの結果と取り違えない。
func TestResubscribedChannelDropsOldResult(t *testing.T) {
	l := &loader{calls: map[string]int{}}
	var saved [][]subs.Channel
	opened := 0
	m := subsModel(l, &saved, &opened)
	run(m, m.Init())
	old := press(m, "tab") // C2 の 1 回目 (渡さない)
	run(m, press(m, "tab"))
	run(m, press(m, "s"))
	for _, k := range []string{"j", "x", "enter"} { // ops を外す
		run(m, press(m, k))
	}
	run(m, press(m, "s"))
	for _, k := range []string{"j", "x", "enter"} { // ops を付け直す
		run(m, press(m, k))
	}
	fresh := press(m, "tab") // 付け直した C2 を読む
	run(m, old)
	if c := m.chans[m.cur]; c.ch.ID != "C2" || c.page != nil || !c.loading {
		t.Fatalf("外す前の読み直しの結果を受け取った: %+v", c)
	}
	run(m, fresh)
	if c := m.chans[m.cur]; c.page == nil || c.loading {
		t.Errorf("付け直した後の読み直しの結果を受け取っていない")
	}
}

// 購読を全部外しても終わらず、s で選び直せる。
func TestUnsubscribeAllThenPickAgain(t *testing.T) {
	l := &loader{calls: map[string]int{}}
	var saved [][]subs.Channel
	opened := 0
	m := subsModel(l, &saved, &opened)
	run(m, m.Init())
	run(m, press(m, "s"))
	for _, k := range []string{"x", "j", "x", "enter"} {
		run(m, press(m, k))
	}
	if len(m.chans) != 0 || m.quitting || !strings.Contains(strings.Join(m.Lines(), "\n"), "s で購読を選ぶ") {
		t.Fatalf("全部外した後: タブ %s quitting=%v\n%s", ids(m), m.quitting, strings.Join(m.Lines(), "\n"))
	}
	if !strings.Contains(strings.Join(m.Lines(), "\n"), "購読を保存した (0 件)") {
		t.Errorf("全部外した画面に保存の知らせが出ない (箱を重ねられない画面でも黙らない)")
	}
	run(m, press(m, "j")) // 何も無いところでキーを押しても落ちない・終わらない
	run(m, press(m, "s"))
	if opened != 2 || m.picker == nil {
		t.Errorf("選び直せない (開いた回数 %d)", opened)
	}
}

// 見ていたチャンネルを外したら先頭のチャンネルへ移り、まだ読んでいなければ読む。保存・一覧の読み込みに失敗したら、
// タブを変えずに知らせる。購読の画面が付いていない起動 (チャンネルを引数で渡した) では、s で理由を知らせる。
func TestSubscriptionsEdgeCases(t *testing.T) {
	l := &loader{calls: map[string]int{}}
	var saved [][]subs.Channel
	opened := 0
	m := subsModel(l, &saved, &opened)
	run(m, m.Init())
	run(m, press(m, "tab")) // C2 を見る (読む)
	run(m, press(m, "s"))
	for _, k := range []string{"x", "j", "x", "j", "x", "enter"} { // dev を外し ops を外し qa を付ける
		run(m, press(m, k))
	}
	if ids(m) != "C3" || m.cur != 0 || l.calls["C3"] != 1 {
		t.Errorf("見ていたチャンネルを外した後: タブ %s cur=%d C3 を読んだ回数 %d", ids(m), m.cur, l.calls["C3"])
	}

	m.subs.Save = func([]subs.Channel) error { return errors.New("書けない") }
	run(m, press(m, "s"))
	for _, k := range []string{"j", "x", "enter"} {
		run(m, press(m, k))
	}
	if ids(m) != "C3" || !strings.Contains(m.LastToast(), "購読を保存できなかった") {
		t.Errorf("保存に失敗した後: タブ %s 知らせ %q", ids(m), m.LastToast())
	}

	m.subs.Open = func(context.Context) (*subs.Picker, error) { return nil, errors.New("slack が応答しない") }
	run(m, press(m, "s"))
	if m.picker != nil || !strings.Contains(m.LastToast(), "購読の一覧を読めなかった") {
		t.Errorf("一覧の読み込みに失敗した後: 知らせ %q", m.LastToast())
	}

	bare := newModel(l)
	run(bare, bare.Init())
	run(bare, press(bare, "s"))
	if !strings.Contains(bare.LastToast(), "チャンネルを指定せずに起動したとき") {
		t.Errorf("購読の画面が無い起動で s: 知らせ %q", bare.LastToast())
	}
	if strings.Contains(bare.help(), "s 購読") || !strings.Contains(m.help(), "s 購読") {
		t.Errorf("キーの案内の s 購読が、購読の画面の有無と合っていない: %q / %q", bare.help(), m.help())
	}
}

// 狭い端末で原文の続きの行が字下げ無しで折り返され、続きが「▍」で始まっても、見出しの色を付けない。
func TestNarrowWrapDoesNotFakeHeading(t *testing.T) {
	u := board.Unit{ID: "1.000000", Msgs: []board.Msg{{Ts: "1.000000", Name: "tanaka", Text: "本文\nabcdef▍見出しのふり", New: true}}}
	p := render.Page{Channel: "C1", ChannelName: "dev", Since: t0, Until: t0, Lookback: t0,
		Topics: []board.Topic{{Title: "t", State: board.StateInfo, Units: []board.Unit{u}}}}
	m := New(context.Background(), []subs.Channel{{ID: "C1", Name: "dev"}}, func(context.Context, subs.Channel) (render.Page, error) {
		return p, nil
	}, nil, func() time.Time { return t0 })
	m.Update(tea.WindowSizeMsg{Width: MinWidth, Height: 40}) // 本文の欄の幅 12: 続きの行「      abcdef」の後で折れる
	run(m, m.Init())
	s := strings.Join(m.Lines(), "\n")
	if plain := regexp.MustCompile("\x1b\\[[0-9;]*m").ReplaceAllString(s, ""); !strings.Contains(plain, "│▍見出し") {
		t.Fatalf("狙った折り返しになっていない:\n%s", plain)
	}
	if strings.Contains(s, sgrHeading+"▍見出し") {
		t.Errorf("折り返しの続きを見出しにした")
	}
}
