package view

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
	"github.com/jiikko/dotfiles/src/tuikit/tickchain"
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
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "ctrl+f", "ctrl+c", "ctrl+d":
		return tea.KeyPressMsg{Code: rune(k[5]), Mod: tea.ModCtrl}
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
	run(m, press(m, "shift+tab"))
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
	if got := m.chans[m.cur].page.Topics[0].Title; got != "new-C1-2" {
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
		if strings.ContainsAny(sgrRe.ReplaceAllString(ln, ""), "\n\x1b") { // 色 (SGR) のほかの制御文字 (画面消去など)
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
	if len(m.chans[m.cur].page.Topics) != 1 {
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

// curTab は今のタブ (水色の地の丸いタブ) の頭の SGR と name。
func curTab(name string) string { return cTeal.bg() + "\x1b[1m" + cBg.fg() + " " + name }

// 狭い端末でも、タブの行には今のチャンネルのタブが出る (タブの並びの右端を切って、どれを見ているかを消さない)。
func TestTabsKeepCurrentTab(t *testing.T) {
	var chans []subs.Channel
	for i := range 6 {
		// タブの幅は 30 (「#」+ 名前 18 + 「  未読 1」7 + 丸い両端と余白 4)。幅 80 に今のタブと両隣は入らない
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
		h := m.tabsLine()
		if !strings.Contains(h, curTab("#"+chans[cur].Name+"  未読 1")) {
			t.Errorf("cur=%d: 今のタブが無い: %q", cur, h)
		}
		if cur > 0 && !strings.HasPrefix(sgrRe.ReplaceAllString(h, ""), tabMore+tabGap) {
			t.Errorf("cur=%d: 畳んだ左のタブの印が無い: %q", cur, h)
		}
		if termwidth.Of(h) > 80 {
			t.Errorf("cur=%d: タブの行が幅を超えた: %d", cur, termwidth.Of(h))
		}
	}
	// 幅が足りれば全部のタブを出し、畳んだ印は出さない
	m.Update(tea.WindowSizeMsg{Width: 300, Height: 20})
	if h := sgrRe.ReplaceAllString(m.tabsLine(), ""); strings.Contains(h, tabMore) || strings.Count(h, "#ch-") != 6 {
		t.Errorf("広い端末で畳んだ: %q", h)
	}
	// 今のタブだけでも入らないときは、タブの名前を縮めて幅に収める
	m.chans[m.cur].ch.Name = strings.Repeat("とても長いチャンネル名", 6)
	m.Update(tea.WindowSizeMsg{Width: MinWidth, Height: 20})
	if h := m.tabsLine(); !strings.HasPrefix(h, reset(cTeal.fg()+"▐")+curTab("#とても")) || termwidth.Of(h) > MinWidth {
		t.Errorf("長い名前で今のタブが幅を超えた (または頭が無い): %q", h)
	}
}

// 狭い端末でも、上の行には今のチャンネルの状態が出る。右端の購読のキーは、状態の残りが minState に満たなければ外す。
func TestTitleKeepsState(t *testing.T) {
	l := &loader{calls: map[string]int{}}
	m := newModel(l)
	m.SetSubscriptions(&Subscriptions{})
	run(m, m.Init())
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	h := sgrRe.ReplaceAllString(m.titleLine(), "")
	if !strings.Contains(h, t0.Format("15:04")+" に読んだ") || !strings.HasSuffix(h, " s  購読するチャンネル") || termwidth.Of(h) != 120 {
		t.Errorf("広い端末で状態か購読のキーが無い: %q", h)
	}
	m.Update(tea.WindowSizeMsg{Width: MinWidth, Height: 20})
	h = sgrRe.ReplaceAllString(m.titleLine(), "")
	if !strings.Contains(h, t0.Format("15:04")+" に読んだ") || strings.Contains(h, "購読") || termwidth.Of(h) > MinWidth {
		t.Errorf("狭い端末で状態が購読のキーに押し出された: %q", h)
	}
	// 長い状態 (読み直しの失敗・読み直し中・読めなかった) でも頭は残る。購読の一覧を読んでいる間も、今のチャンネルの状態を消さない
	c := &m.chans[m.cur]
	for _, st := range []struct {
		name          string
		loading, page bool
		want          string
	}{
		{"読み直しに失敗", false, true, "🚨 読み直し"},
		{"読み直し中", true, true, "読み直し中"},
		{"読めなかった", false, false, "🚨 読めなかった"},
	} {
		saved := c.page
		c.err, c.loading = errors.New("boom"), st.loading
		if !st.page {
			c.page = nil
		}
		if h := sgrRe.ReplaceAllString(m.titleLine(), ""); !strings.Contains(h, st.want) || termwidth.Of(h) > MinWidth {
			t.Errorf("%s: 状態が無い: %q", st.name, h)
		}
		m.picking = true
		for w := MinWidth; w <= 120; w++ {
			m.Update(tea.WindowSizeMsg{Width: w, Height: 20})
			if h := sgrRe.ReplaceAllString(m.titleLine(), ""); !strings.Contains(h, st.want) || termwidth.Of(h) > w {
				t.Errorf("%s: 幅 %d で一覧を読んでいる間に今のチャンネルの状態が消えた: %q", st.name, w, h)
			}
		}
		if h := sgrRe.ReplaceAllString(m.titleLine(), ""); !strings.Contains(h, "チャンネルの一覧を読んでいます") {
			t.Errorf("%s: 広い端末で一覧を読んでいることが出ない: %q", st.name, h)
		}
		m.picking = false
		m.Update(tea.WindowSizeMsg{Width: MinWidth, Height: 20})
		c.page = saved
	}
}

// header はロゴと状態の行とタブの行 (色を外す)。
func header(m *Model) string { return sgrRe.ReplaceAllString(m.titleLine()+"\n"+m.tabsLine(), "") }

// 最初の読み込みが失敗したら、見出しの行にもそう出す (状態が空だと、読んでいるのか止まったのか分からない)。
func TestHeaderShowsFirstLoadError(t *testing.T) {
	l := &loader{calls: map[string]int{}, err: map[string]error{"C1": errors.New("boom")}}
	m := newModel(l)
	run(m, m.Init())
	if h := header(m); !strings.Contains(h, "読めなかった (r で読み直す)") {
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
	if h := header(m); !strings.Contains(h, "11/09 08:30 に読んだ板を表示中") {
		t.Errorf("読み直し中にキャッシュと分からない: %q", h)
	}
	if !strings.Contains(strings.Join(m.Lines(), "\n"), "前回-C1") {
		t.Errorf("読み直しを待たずに前回の板を出していない")
	}
	run(m, cmd)
	if h := header(m); !strings.Contains(h, " に読んだ") || strings.Contains(h, "まだ読み直していない") {
		t.Errorf("読み直した後もキャッシュと出ている: %q", h)
	}
	run(m, press(m, "tab"))
	if l.calls["C2"] != 1 {
		t.Errorf("キャッシュしか無いチャンネルを開いても読み直さない (C2 %d 回)", l.calls["C2"])
	}
	run(m, press(m, "shift+tab"))
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
	if h, want := header(m), "(話題の分け方は "+t0.Add(-3*time.Hour).Format("01/02 15:04")+" のまま)"; !strings.Contains(h, want) {
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
		Save: func(sel []subs.Channel, _ string) error { *saved = append(*saved, sel); return nil },
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
	pending := press(m, "tab")    // C2 を読み始める (結果はまだ渡さない)
	run(m, press(m, "shift+tab")) // C1 へ戻る
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
	if !strings.Contains(header(m), "#qa") {
		t.Errorf("見出しに新しいタブが無い: %q", header(m))
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
	run(m, press(m, "shift+tab"))
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
	if len(m.chans) != 0 || m.quitting || !strings.Contains(strings.Join(m.Lines(), "\n"), "s で購読するチャンネルを選ぶ") {
		t.Fatalf("全部外した後: タブ %s quitting=%v\n%s", ids(m), m.quitting, strings.Join(m.Lines(), "\n"))
	}
	if !strings.Contains(strings.Join(m.Lines(), "\n"), "購読するチャンネルを保存した (0 件)") {
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

	m.subs.Save = func([]subs.Channel, string) error { return errors.New("書けない") }
	run(m, press(m, "s"))
	for _, k := range []string{"j", "x", "enter"} {
		run(m, press(m, k))
	}
	if ids(m) != "C3" || !strings.Contains(m.LastToast(), "購読するチャンネルを保存できなかった") {
		t.Errorf("保存に失敗した後: タブ %s 知らせ %q", ids(m), m.LastToast())
	}

	m.subs.Open = func(context.Context) (*subs.Picker, error) { return nil, errors.New("slack が応答しない") }
	run(m, press(m, "s"))
	if m.picker != nil || !strings.Contains(m.LastToast(), "チャンネルの一覧を読めなかった") {
		t.Errorf("一覧の読み込みに失敗した後: 知らせ %q", m.LastToast())
	}

	bare := newModel(l)
	run(bare, bare.Init())
	run(bare, press(bare, "s"))
	if !strings.Contains(bare.LastToast(), "チャンネルを指定しないで起動したとき") {
		t.Errorf("購読の画面が無い起動で s: 知らせ %q", bare.LastToast())
	}
	bare.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	bareTop, top := header(bare), header(m)
	if strings.Contains(bareTop, "購読するチャンネル") || !strings.Contains(top, " s  購読するチャンネル") {
		t.Errorf("上の行の s の案内が、購読の画面の有無と合っていない: %q / %q", bareTop, top)
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
	m.Update(tea.WindowSizeMsg{Width: 45, Height: 40}) // 本文を折り返す幅 12: 続きの行「      abcdef」の後で折れる
	run(m, m.Init())
	s := strings.Join(m.Lines(), "\n")
	if plain := regexp.MustCompile("\x1b\\[[0-9;]*m").ReplaceAllString(s, ""); !strings.Contains(plain, "▐ ▍見出し") {
		t.Fatalf("狙った折り返しになっていない:\n%s", plain)
	}
	if strings.Contains(s, sgrHeading+"▍見出し") {
		t.Errorf("折り返しの続きを見出しにした")
	}
}

// 前に取ったチャンネルの一覧があれば、s ですぐその一覧で購読の画面を開き、取り直したら一覧を差し替える。差し替えても、
// 取り直しの間の付け外しとカーソルのチャンネルは保つ。取り直しの間に画面を閉じたら、取り直しの結果で開き直さない。
func TestPickerOpensFromCachedListAndRefreshes(t *testing.T) {
	l := &loader{calls: map[string]int{}}
	m := newModel(l)
	cur := []subs.Channel{{ID: "C1", Name: "dev"}, {ID: "C2", Name: "ops"}}
	old := []source.Channel{{ID: "C1", Name: "dev", IsMember: true}, {ID: "C2", Name: "ops", IsMember: true}}
	fresh := append(old, source.Channel{ID: "C3", Name: "qa", IsMember: true})
	var saved [][]subs.Channel
	m.SetSubscriptions(&Subscriptions{
		Open:   func(context.Context) (*subs.Picker, error) { return subs.NewPicker(fresh, cur), nil },
		Cached: func() (*subs.Picker, time.Time, bool) { return subs.NewPicker(old, cur), t0, true },
		Save:   func(sel []subs.Channel, _ string) error { saved = append(saved, sel); return nil },
	})
	run(m, m.Init())
	pending := press(m, "s")
	screen := strings.Join(m.Lines(), "\n")
	if !strings.Contains(screen, "購読するチャンネルを選ぶ") || !strings.Contains(screen, t0.Format("01/02 15:04")+" に取った一覧・取り直し中") || strings.Contains(screen, "#qa") {
		t.Fatalf("前に取った一覧ですぐ開いていない:\n%s", screen)
	}
	press(m, "j")
	press(m, "x") // ops を外す
	run(m, pending)
	screen = strings.Join(m.Lines(), "\n")
	if !strings.Contains(screen, "#qa") || strings.Contains(screen, "取り直し中") {
		t.Fatalf("取り直した一覧に差し替わっていない:\n%s", screen)
	}
	press(m, "x") // カーソルは ops のまま: ops を付け直す
	press(m, "enter")
	if len(saved) != 1 || len(saved[0]) != 2 || saved[0][1].ID != "C2" {
		t.Fatalf("差し替えでカーソルのチャンネルか付け外しを失った: %v", saved)
	}

	pending = press(m, "s")
	press(m, "q") // 取り直しの間に閉じる
	run(m, pending)
	if m.picker != nil {
		t.Error("閉じた購読の画面を、取り直しの結果で開き直した")
	}
}

// d は今のチャンネルの購読を外すか確認の板で確かめ、y / Enter で外して保存する。それ以外のキーは取り消して何も保存しない。
func TestUnsubscribeAsksFirst(t *testing.T) {
	l := &loader{calls: map[string]int{}}
	var saved [][]subs.Channel
	opened := 0
	m := subsModel(l, &saved, &opened)
	run(m, m.Init())
	press(m, "d")
	if screen := sgrRe.ReplaceAllString(strings.Join(m.Lines(), "\n"), ""); !strings.Contains(screen, "購読を外す") || !strings.Contains(screen, "#dev") {
		t.Fatalf("確認の板が出ない:\n%s", screen)
	}
	press(m, "n")
	if len(saved) != 0 || ids(m) != "C1,C2" || strings.Contains(strings.Join(m.Lines(), "\n"), "購読を外しますか") {
		t.Fatalf("取り消したのに外した・板が残った: saved=%v tabs=%s", saved, ids(m))
	}
	press(m, "d")
	run(m, press(m, "y"))
	if len(saved) != 1 || ids(m) != "C2" || saved[0][0].ID != "C2" {
		t.Fatalf("y で外していない: saved=%v tabs=%s", saved, ids(m))
	}
}

// 購読を外すか確かめている間に購読の画面が開いたら、確かめるのをやめる (隠れた確認に購読の画面の Enter・y で答えさせない)。
// 小さい端末では、問いと答え方を 1 行で出す。
func TestUnsubscribeConfirmVsPickerAndTiny(t *testing.T) {
	l := &loader{calls: map[string]int{}}
	var saved [][]subs.Channel
	opened := 0
	m := subsModel(l, &saved, &opened)
	run(m, m.Init())
	pending := press(m, "s") // 一覧を裏で読み始める (結果はまだ渡さない)
	press(m, "d")
	run(m, pending)
	press(m, "enter") // 購読の画面の Enter (保存して閉じる)
	if len(saved) != 1 || ids(m) != "C1,C2" {
		t.Fatalf("購読の画面の Enter が、隠れた確認に答えた: saved=%v tabs=%s", saved, ids(m))
	}
	m.Update(tea.WindowSizeMsg{Width: MinWidth, Height: MinHeight - 1})
	press(m, "d")
	if lines := m.Lines(); len(lines) != 1 || !strings.Contains(lines[0], "購読を外す？") || !strings.Contains(lines[0], "y/Enter") {
		t.Errorf("小さい端末の確認: %q", lines)
	}
}

// グループのメンバーが変わったら、今のタブは読み直し、前のメンバーで走っていた読み直しの結果は捨てる。
// d でグループを外すときは、定義ごと消える (既読は引き継がない) と確かめる。
func TestGroupMembersChangedReloads(t *testing.T) {
	l := &loader{calls: map[string]int{}}
	g := subs.Channel{ID: "grp0123abcd", Name: "times", Members: []subs.Channel{{ID: "C1", Name: "dev"}}}
	var saved [][]subs.Channel
	m := quiet(New(context.Background(), []subs.Channel{g}, l.load, nil, func() time.Time { return t0 }))
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 20})
	m.SetSubscriptions(&Subscriptions{Save: func(sel []subs.Channel, _ string) error { saved = append(saved, sel); return nil }})
	stale := m.Init() // 前のメンバーで読み始める (結果はまだ渡さない)
	g2 := g
	g2.Members = append(slices.Clone(g.Members), subs.Channel{ID: "C2", Name: "ops"})
	if cmd := m.setChannels([]subs.Channel{g2}); cmd != nil {
		t.Fatal("走っている読み直しを追い越して、もう 1 本読み始めた")
	}
	_, fresh := m.Update(stale())
	if m.chans[0].page != nil {
		t.Error("前のメンバーで読んだ結果を受け取った")
	}
	if fresh == nil || !m.chans[0].loading {
		t.Fatal("前のメンバーの結果を捨てた後に読み直さない")
	}
	run(m, fresh)
	if m.chans[0].page == nil || m.chans[0].loading {
		t.Fatal("読み直しの結果を受け取らない")
	}
	if cmd := m.setChannels([]subs.Channel{g}); cmd == nil {
		t.Error("読み直しが走っていないときにメンバーが変わっても、すぐ読み直さない")
	}
	press(m, "d")
	if screen := sgrRe.ReplaceAllString(strings.Join(m.Lines(), "\n"), ""); !strings.Contains(screen, "グループを消しますか") || !strings.Contains(screen, "既読は引き継がない") {
		t.Errorf("グループを消す確認の文:\n%s", screen)
	}
}

// 裏のタブのグループのメンバーが変わったら、前のメンバーで走っていた読み直し (R) の結果は捨て、開いたときに読み直す。
func TestGroupMembersChangedInBackgroundTab(t *testing.T) {
	l := &loader{calls: map[string]int{}}
	g := subs.Channel{ID: "grp0123abcd", Name: "times", Members: []subs.Channel{{ID: "C1", Name: "dev"}}}
	ch := subs.Channel{ID: "C9", Name: "news"}
	m := quiet(New(context.Background(), []subs.Channel{ch, g}, l.load, nil, func() time.Time { return t0 }))
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 20})
	run(m, m.Init())
	stale := m.reload(1) // R で裏のグループも読み始める (結果はまだ渡さない)
	g2 := g
	g2.Members = append(slices.Clone(g.Members), subs.Channel{ID: "C2", Name: "ops"})
	m.setChannels([]subs.Channel{ch, g2})
	run(m, stale)
	if m.chans[1].page != nil || m.chans[1].loading {
		t.Fatal("前のメンバーで読んだ結果を裏のタブで受け取った・裏で読み直した")
	}
	if cmd := press(m, "tab"); cmd == nil {
		t.Error("メンバーが変わった裏のタブを開いても読み直さない")
	}
}

// 前の読み直しに失敗したグループも、メンバーを直したら失敗を消して、開いたときに読み直す。
func TestGroupMembersChangedClearsError(t *testing.T) {
	l := &loader{calls: map[string]int{}}
	g := subs.Channel{ID: "grp0123abcd", Name: "times", Members: []subs.Channel{{ID: "C1", Name: "dev"}}}
	ch := subs.Channel{ID: "C9", Name: "news"}
	m := quiet(New(context.Background(), []subs.Channel{ch, g}, l.load, nil, func() time.Time { return t0 }))
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 20})
	run(m, m.Init())
	m.chans[1].err = errors.New("分類に失敗")
	g2 := g
	g2.Members = append(slices.Clone(g.Members), subs.Channel{ID: "C2", Name: "ops"})
	m.setChannels([]subs.Channel{ch, g2})
	if m.chans[1].err != nil {
		t.Error("メンバーを直しても前の失敗が残った")
	}
	if cmd := press(m, "tab"); cmd == nil {
		t.Error("メンバーを直したタブを開いても読み直さない")
	}
}

// 起動のときに前回最後に見ていたタブ (Start) を開き、最初の読み込みもそのタブにする。購読から消えたタブなら先頭のタブ。
// 新着のタブも覚えられる。Tab は今のタブ (終わるときに覚える値)。
func TestStartOpensLastTab(t *testing.T) {
	l := &loader{calls: map[string]int{}}
	m := newModel(l)
	m.Start("C2", "")
	run(m, m.Init())
	if m.Tab() != "C2" || l.calls["C2"] != 1 || l.calls["C1"] != 0 {
		t.Errorf("前回のタブ C2 を開いて読んでいない: tab=%s calls=%v", m.Tab(), l.calls)
	}
	l = &loader{calls: map[string]int{}}
	m = newModel(l)
	m.Start("C9", "")
	run(m, m.Init())
	if m.Tab() != "C1" || l.calls["C1"] != 1 {
		t.Errorf("購読に無いタブなら先頭を開く: tab=%s calls=%v", m.Tab(), l.calls)
	}
	l = &loader{calls: map[string]int{}}
	m = newModel(l)
	m.Start(StreamTab, "")
	run(m, m.Init())
	if m.Tab() != StreamTab || l.calls["C1"] != 1 || l.calls["C2"] != 1 {
		t.Errorf("新着のタブを開いて全部のタブを読んでいない: tab=%s calls=%v", m.Tab(), l.calls)
	}
	// 全部のタブに前回の板 (キャッシュ) があっても、新着のタブで起動したら全部を読み直す
	l = &loader{calls: map[string]int{}}
	m = newModel(l)
	m.Preload(0, page("C1", "前"), t0)
	m.Preload(1, page("C2", "前"), t0)
	m.Start(StreamTab, "")
	run(m, m.Init())
	if l.calls["C1"] != 1 || l.calls["C2"] != 1 {
		t.Errorf("前回の板のままのタブを、新着のタブの起動で読み直していない: calls=%v", l.calls)
	}
}

// 起動のときに前回選んでいた話題 (Start の topic) にカーソルを戻す。前回の板 (キャッシュ) があれば読み直しを待たずに、無ければ読めたときに戻す。
// 戻した話題は既読にしない。人がキーを押した後に届いた板では引き戻さない。消えた話題なら先頭の話題のまま。新着のタブでも、持ち主のタブまで合わせて戻す。
func TestStartRestoresLastTopic(t *testing.T) {
	topics := []board.Topic{topicAt("a", day0), topicAt("b", day0.Add(-time.Hour)), topicAt("c", day0.Add(-2*time.Hour))}
	pg := func(ch string) render.Page {
		return render.Page{Channel: ch, ChannelName: ch, Since: day0, Until: day0, Lookback: day0, Topics: topics}
	}
	mk := func() *Model {
		load := func(_ context.Context, ch subs.Channel) (render.Page, error) { return pg(ch.ID), nil }
		m := quiet(New(context.Background(), []subs.Channel{{ID: "C1", Name: "dev"}, {ID: "C2", Name: "ops"}}, load, nil, func() time.Time { return day0 }))
		m.Update(tea.WindowSizeMsg{Width: 90, Height: 20})
		return m
	}
	key := func(ch string, i int) string { return ch + "|" + topics[i].Units[0].ID }
	// 戻したかは selKey (カーソルの実際の位置) で見る。Topic は戻す前なら戻す予定をそのまま返すので、戻さなくても一致する

	m := mk()
	m.Start("C2", key("C2", 2))
	run(m, m.Init())
	if m.selKey() != key("C2", 2) {
		t.Errorf("読めた板で前回の話題へ戻していない: %q", m.selKey())
	}
	if m.unreadCount(1) != 3 || m.body {
		t.Errorf("戻した話題を既読にしたか本文の欄を開いた: 未読 %d body=%v", m.unreadCount(1), m.body)
	}

	m = mk()
	m.Preload(1, pg("C2"), day0)
	m.Start("C2", key("C2", 1))
	cmd := m.Init()
	if m.selKey() != key("C2", 1) {
		t.Errorf("前回の板があるのに、読み直しを待たないと戻さない: %q", m.selKey())
	}
	run(m, cmd)
	if m.selKey() != key("C2", 1) || m.body {
		t.Errorf("読み直しの後に前回の話題から外れたか本文の欄を開いた: %q body=%v", m.selKey(), m.body)
	}

	// 板が届く前に終えたら、覚えるのは前回の話題 (空や先頭の話題で覚えを上書きしない)。新着のタブで持ち主のタブが届く前も同じ
	for _, q := range []string{"q", "ctrl+c"} {
		m = mk()
		m.Start("C2", key("C2", 2))
		m.Init()
		press(m, q)
		if m.Topic() != key("C2", 2) {
			t.Errorf("板が届く前に %s で終えたら前回の話題を失った: %q", q, m.Topic())
		}
	}
	m = mk()
	m.Preload(0, pg("C1"), day0)
	m.Start(StreamTab, key("C2", 2))
	m.Init()
	press(m, "q")
	if m.Topic() != key("C2", 2) {
		t.Errorf("新着のタブで持ち主のタブが届く前に終えたら、別の話題で覚えを上書きした: %q", m.Topic())
	}

	m = mk()
	m.Start("C2", key("C2", 2))
	cmd = m.Init()
	press(m, "j")
	run(m, cmd)
	if m.Topic() != key("C2", 0) {
		t.Errorf("キーを押した後に届いた板で前回の話題へ引き戻した: %q", m.Topic())
	}

	m = mk()
	m.Start("C2", "C2|9.000000")
	m.Preload(1, pg("C2"), day0)
	cmd = m.Init()
	if m.Topic() != "C2|9.000000" {
		t.Errorf("前回の板 (キャッシュ) に無いだけで、読み直しを待たずに前回の話題を諦めた: %q", m.Topic())
	}
	run(m, cmd)
	if m.Topic() != key("C2", 0) { // 戻せない話題を覚え続けない (先頭の話題を見て終えたら、先頭の話題を覚える)
		t.Errorf("読み直した板にも無い話題を諦めず、先頭の話題を見ていても覚え直す: %q", m.Topic())
	}
	m = mk()
	m.Start("C9", "C9|1.000000")
	run(m, m.Init())
	if m.Topic() != key("C1", 0) {
		t.Errorf("購読から外したタブの話題を諦めない: %q", m.Topic())
	}

	m = mk()
	m.Preload(0, pg("C1"), day0)
	m.Preload(1, pg("C2"), day0)
	m.Start(StreamTab, key("C2", 1))
	m.Init()
	if m.selKey() != key("C2", 1) || m.unreadCount(1) != 3 || m.body {
		t.Errorf("前回の板があるのに、新着のタブで読み直しを待たないと戻さないか既読・本文の欄が変わった: %q 未読 %d body=%v", m.selKey(), m.unreadCount(1), m.body)
	}

	m = mk()
	m.Start(StreamTab, key("C2", 2))
	run(m, m.Init())
	if m.Tab() != StreamTab || m.selKey() != key("C2", 2) {
		t.Errorf("新着のタブで前回の話題 (C2 の c) へ戻していない: tab=%s topic=%q", m.Tab(), m.selKey())
	}
}

// 購読の画面を開くと参加しているチャンネルの最終投稿を裏で取り直し、届いた分から表に出す。画面を閉じたら取り直しを取り消す
// (閉じた後に Slack の呼び出しの列を使い続けない)。
func TestPickerFetchesLastPostsUntilClosed(t *testing.T) {
	m := newModel(&loader{calls: map[string]int{}})
	chs := []source.Channel{{ID: "C1", Name: "dev", IsMember: true}, {ID: "C2", Name: "ops", IsMember: true}}
	var ctxs []context.Context
	m.SetSubscriptions(&Subscriptions{
		Open:   func(context.Context) (*subs.Picker, error) { return subs.NewPicker(chs, nil), nil },
		Cached: func() (*subs.Picker, time.Time, bool) { return subs.NewPicker(chs, nil), t0, true },
		Save:   func([]subs.Channel, string) error { return nil },
		Posts: &subs.LastPosts{Get: func(ctx context.Context, _ string) (string, error) {
			ctxs = append(ctxs, ctx)
			return "1794400000.000000", ctx.Err()
		}, Now: func() time.Time { return t0 }},
	})
	// drive は Cmd を流し、Update が返した Cmd も尽きるまで流す (取り直しは 1 件ずつ次の Cmd を返す)
	var drive func(tea.Cmd)
	drive = func(c tea.Cmd) {
		if c == nil {
			return
		}
		msg := c()
		if b, ok := msg.(tea.BatchMsg); ok {
			for _, c := range b {
				drive(c)
			}
			return
		}
		_, next := m.Update(msg)
		drive(next)
	}
	pending := press(m, "s")
	drive(pending)
	if len(ctxs) != 2 || !strings.Contains(strings.Join(m.Lines(), "\n"), "2026/11") { // テストの今 (t0) は 1970 年なので年月で出る
		t.Fatalf("開いている間に最終投稿を取って表に出していない: %d 件\n%s", len(ctxs), strings.Join(m.Lines(), "\n"))
	}
	press(m, "q")
	ctxs = nil
	pending = press(m, "s") // 開き直す (前に取った値は 1 時間より新しいが、Cached の画面は値を持たないので取り直す)
	press(m, "q")
	drive(pending)
	if len(ctxs) == 0 {
		t.Fatal("開き直した画面で取り直しを始めていない (取り消しを確かめられない)")
	}
	for _, ctx := range ctxs {
		if ctx.Err() == nil {
			t.Error("閉じた購読の画面の取り直しを取り消していない")
		}
	}
	if len(ctxs) > 1 {
		t.Errorf("閉じた後も列を進めた: %d 件", len(ctxs))
	}
}

// 右下 (案内の 2 行目の右端) に Slack の呼び出しの様子を出す。購読の画面でも同じ所に出す。呼び出しがある間は、何もしないでいても
// 1 秒ごとに描き直す (直近 1 分の数が減っていくのを見せる)。
func TestAPIStatsBottomRight(t *testing.T) {
	m := newModel(&loader{calls: map[string]int{}})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	active := true
	m.SetAPIStats(func() (string, bool) { return "Slack 12回/分 間隔 2.3秒", active })
	lines := m.Lines()
	last := sgrRe.ReplaceAllString(lines[len(lines)-1], "")
	if !strings.HasSuffix(strings.TrimRight(last, " "), "Slack 12回/分 間隔 2.3秒") || termwidth.Of(last) > 120 {
		t.Errorf("右下に呼び出しの様子が無い: %q", last)
	}
	var waits []time.Duration
	m.after = func(d time.Duration, _ func(time.Time) tea.Msg) tea.Cmd { waits = append(waits, d); return nil }
	m.frameCmds()
	if !slices.Contains(waits, time.Second) {
		t.Errorf("呼び出しのある間に 1 秒ごとの描き直しを張っていない: %v", waits)
	}
	active, waits = false, nil
	m.ticks = tickchain.Chain{}
	m.frameCmds()
	if len(waits) != 0 {
		t.Errorf("呼び出しが無いのに描き直しを張った: %v", waits)
	}
	m.SetSubscriptions(&Subscriptions{Cached: func() (*subs.Picker, time.Time, bool) { return subs.NewPicker(nil, nil), t0, true },
		Open: func(context.Context) (*subs.Picker, error) { return subs.NewPicker(nil, nil), nil }, Save: func([]subs.Channel, string) error { return nil }})
	press(m, "s")
	lines = m.Lines()
	if last := sgrRe.ReplaceAllString(lines[len(lines)-1], ""); !strings.Contains(last, "Slack 12回/分") {
		t.Errorf("購読の画面の右下に呼び出しの様子が無い: %q", last)
	}
}
