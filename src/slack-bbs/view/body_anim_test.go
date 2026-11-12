package view

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"slack-bbs/board"

	"github.com/jiikko/dotfiles/src/tuikit/anim"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
	"github.com/jiikko/dotfiles/src/tuikit/tickchain"
)

// clockAt は m の時計を start に止め、進める関数を返す。m が張った tick を拾っておき、時計を進めたときに届ける。
// テストの側で tick を作らない: 演出の間 m が tick を張り続けないと、演出が着地しないまま止まる (それもここで落とす)。
func clockAt(m *Model, start time.Time) func(time.Duration) {
	now := start
	m.now = func() time.Time { return now }
	m.ticks = tickchain.Chain{} // quiet が捨てた tick を、届くものとして待たない
	var pending []tea.Msg
	m.after = func(_ time.Duration, f func(time.Time) tea.Msg) tea.Cmd {
		if msg, ok := f(now).(tickMsg); ok {
			pending = append(pending, msg)
		}
		return nil
	}
	return func(d time.Duration) {
		now = now.Add(d)
		msgs := pending
		pending = nil
		for _, msg := range msgs {
			m.Update(msg)
		}
	}
}

// pane は画面のうち一覧と本文の欄の行 (色を落とす)。上の見出しと下のキーの案内は、一覧と本文で中身が違うので比べない。
// 欄の下の 4 行も比べない: 知らせの箱 (「#dev を読んだ」) が時刻しだいで重なる。
func pane(m *Model) []string {
	lines := m.Lines()[topRows : topRows+m.paneRows()-4]
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = sgrRe.ReplaceAllString(l, "")
	}
	return out
}

// rawPane は一覧と本文の欄の全部の行を色ごと返す (演出の始まりと着地の姿を、止まっている画面と色まで比べる)。知らせは消しておく。
func rawPane(m *Model) string {
	m.toast.Clear()
	return strings.Join(m.Lines()[topRows:topRows+m.paneRows()], "\n")
}

// drawerLeft は frame の全部の行が「列 L より左は list と同じ、L から右は body の左端から」になる L を返す (無ければ -1)。
// 境目をまたぐ全角の字は落として空白で詰め、右端は板の幅で切るので、境目の手前 2 桁と右端の 3 桁は比べない。
func drawerLeft(frame, list, body []string, total int) int {
	for l := 3; l < total-4; l++ {
		ok := true
		for i := range frame {
			room := total - l - 3
			if termwidth.Truncate(frame[i], l-2, "") != termwidth.Truncate(list[i], l-2, "") ||
				termwidth.Truncate(termwidth.SliceFrom(frame[i], l), room, "") != termwidth.Truncate(body[i], room, "") {
				ok = false
				break
			}
		}
		if ok {
			return l
		}
	}
	return -1
}

func twoTopics(t *testing.T) *Model {
	m := datedModel(t, func() []board.Topic {
		return []board.Topic{topicAt("読んでいる話題", day0), topicAt("別件", day0.Add(-time.Hour))}
	})
	m.toast.Clear() // 知らせが滑る間も tick が張られるので、演出の tick の張り忘れが隠れる
	return m
}

// 本文の欄を開くときと一覧へ戻るときは、glogx の issue viewer の引き出しと同じく、一覧の画面の上を全幅の本文の画面が板として
// 右外から滑り込み (戻るときは右外へ抜け)、板は開き切った姿のまま位置だけが動く。始まりは一覧の画面そのもの、着地すると本文の画面・
// 一覧の画面そのものになる (板の中身を途中の幅で組み直すと、着地の瞬間に折り返しが変わって跳ぶ)。
func TestBodyOpensAndClosesAsDrawer(t *testing.T) {
	m := twoTopics(t)
	adv := clockAt(m, day0)
	press(m, "l")
	adv(time.Second)
	body, bodyRaw := pane(m), rawPane(m)
	press(m, "h")
	adv(time.Second)
	list, listRaw := pane(m), rawPane(m)
	if m.drawer.Unsettled(m.now()) {
		t.Fatal("閉じ切った後も演出が片付いていない (tick で Settle していない)")
	}
	samples := func(key string, at ...time.Duration) []int {
		press(m, key)
		var got []int
		prev := time.Duration(0)
		for _, d := range at {
			adv(d - prev)
			prev = d
			l := drawerLeft(pane(m), list, body, m.w)
			if l <= 0 {
				t.Fatalf("%s の %v 後: 途中の姿が「左は一覧の画面・右は本文の板」になっていない:\n%s", key, d, strings.Join(pane(m), "\n"))
			}
			got = append(got, l)
		}
		adv(time.Second)
		return got
	}
	press(m, "l")
	if got := rawPane(m); got != listRaw {
		t.Errorf("開いた直後の姿が一覧の画面と (色を含めて) 違う (始まりで跳ぶ):\n%s", got)
	}
	press(m, "h")
	adv(time.Second)
	if l := samples("l", bodyOpenDuration/4, bodyOpenDuration/2); l[1] >= l[0] {
		t.Errorf("開く途中で板の左端が左へ進まない: %v", l)
	}
	if got := rawPane(m); got != bodyRaw {
		t.Errorf("開き切った姿が前と (色を含めて) 違う:\n%s", got)
	}
	press(m, "l")
	adv(bodyOpenDuration - time.Millisecond)
	if got := rawPane(m); got != bodyRaw {
		t.Errorf("開き終わる直前の姿が本文の画面と (色を含めて) 違う (着地で横へ跳ぶ):\n%s", got)
	}
	adv(time.Second)
	press(m, "h")
	if got := rawPane(m); got != bodyRaw {
		t.Errorf("閉じ始めの姿が本文の画面と (色を含めて) 違う (閉じ始めで横へ跳ぶ):\n%s", got)
	}
	adv(time.Second)
	press(m, "l")
	adv(time.Second)
	if l := samples("h", bodyOpenDuration/2, bodyOpenDuration*3/4); l[1] <= l[0] {
		t.Errorf("一覧へ戻る途中で板の左端が右へ進まない: %v", l)
	}
	if got := rawPane(m); got != listRaw {
		t.Errorf("戻り切った姿が一覧と (色を含めて) 違う:\n%s", got)
	}
}

// 開く途中で戻る・戻る途中で開き直すと、板はその位置から逆向きに動く (端まで跳んでから動き直さない)。
func TestDrawerReversesFromWhereItIs(t *testing.T) {
	m := twoTopics(t)
	adv := clockAt(m, day0)
	press(m, "l")
	adv(time.Second)
	body := pane(m)
	press(m, "h")
	adv(time.Second)
	list := pane(m)
	left := func() int { return drawerLeft(pane(m), list, body, m.w) }
	for _, tc := range []struct{ first, then string }{{"l", "h"}, {"h", "l"}} {
		if tc.first == "h" {
			press(m, "l")
			adv(time.Second)
		}
		press(m, tc.first)
		adv(bodyOpenDuration / 2)
		before := left()
		press(m, tc.then)
		after := left()
		if before <= 0 || after < before-1 || after > before+1 {
			t.Errorf("%s の途中で %s: 板の左端が %d から %d へ跳んだ", tc.first, tc.then, before, after)
		}
		adv(bodyOpenDuration / 4)
		moved := left()
		if (tc.then == "h" && moved <= after) || (tc.then == "l" && moved >= after) {
			t.Errorf("%s の途中で %s: 逆向きに動かない (%d → %d)", tc.first, tc.then, after, moved)
		}
		adv(time.Second)
	}
}

// 本文の半ページ送りは何コマかに分けて滑り、着地すると止まる。1 行送りは滑らせない。窓の大きさが変わったら滑るのをやめる。
func TestHalfPageGlidesAndOneLineDoesNot(t *testing.T) {
	m := datedModel(t, func() []board.Topic {
		long := topicAt("long", day0)
		long.Units[0].Msgs[0].Text = strings.Repeat("長い本文を折り返して欄からはみ出させる。", 200)
		return []board.Topic{long}
	})
	m.toast.Clear()
	adv := clockAt(m, day0)
	press(m, "l")
	adv(time.Second)
	press(m, "ctrl+d")
	seen := map[string]bool{}
	for range bodyGlideFrames + 2 {
		seen[strings.Join(bodyPlain(m), "\n")] = true
		adv(frameInterval)
	}
	if len(seen) < 3 {
		t.Errorf("半ページ送りが滑らない (見えた姿 %d 通り)", len(seen))
	}
	if m.pager.Animating() {
		t.Error("半ページ送りが着地しない")
	}
	press(m, "j")
	if m.pager.Animating() {
		t.Error("1 行送りが滑った")
	}
	press(m, "ctrl+d")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20}) // 折り返しが変わると、途中の行の番号が別の行を指す
	if m.pager.Animating() {
		t.Error("窓の大きさが変わっても半ページ送りが滑り続ける")
	}
}

// 開く途中にタブを替えると、演出をやめて一覧のまま止まる (開きかけの板に別の話題を滑り込ませ続けない)。次に開くときは閉じた姿から始める。
func TestSwitchingTabStopsDrawer(t *testing.T) {
	m := twoTopics(t)
	adv := clockAt(m, day0)
	press(m, "l")
	adv(bodyOpenDuration / 4)
	if !m.drawer.Animating(m.now()) {
		t.Fatal("前提: 開く途中でない")
	}
	press(m, "tab")
	if m.body || m.drawer.Unsettled(m.now()) {
		t.Errorf("タブを替えた後も本文の欄か演出が残る: body=%v phase=%v", m.body, m.drawer.Phase())
	}
	press(m, "l") // 新着のタブで開くと話題が既読の組へ移り一覧が変わるので、画面ではなく開き具合で見る
	if o := m.drawer.Openness(m.now(), anim.EaseOutCubic); !m.body || o != 0 {
		t.Errorf("タブを替えた後に開くと、閉じた姿から始まらない (body=%v 開き具合 %v。途中の位置から跳ぶ)", m.body, o)
	}
}
