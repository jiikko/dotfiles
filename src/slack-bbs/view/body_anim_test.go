package view

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"slack-bbs/board"

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

// edgeOf は frame の全部の行が「列 x より左は list と同じ、右は body と同じ」になる x を返す (無ければ -1)。
// 境目をまたぐ全角の字は落として空白で詰めるので、境目の前後 2 桁は比べない。
func edgeOf(frame, list, body []string, below int) int {
	for x := 3; x < below; x++ {
		ok := true
		for i := range frame {
			if termwidth.Truncate(frame[i], x-2, "") != termwidth.Truncate(list[i], x-2, "") ||
				termwidth.SliceFrom(frame[i], x+2) != termwidth.SliceFrom(body[i], x+2) {
				ok = false
				break
			}
		}
		if ok {
			return x
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

// 本文の欄を開くときと一覧へ戻るときは、一覧との境目だけが動き、境目の右の本文は開き切った姿と同じ位置に出る
// (本文を中身ごと横へずらすと、毎フレーム全部のセルを送り直して端末が追いつかない。openingRows の doc)。
// 開くときは境目が左へ、戻るときは右へ進み、着地すると開き切った姿・一覧の姿そのものになる。
func TestBodyOpensAndClosesByMovingOnlyTheEdge(t *testing.T) {
	m := twoTopics(t)
	adv := clockAt(m, day0)
	press(m, "l")
	adv(time.Second)
	body := pane(m)
	press(m, "h")
	adv(time.Second)
	list := pane(m)
	if m.opening.Closing() {
		t.Fatal("閉じ切った後も閉じる途中のまま (tick で片付けていない)")
	}
	x0 := m.leftWidth() + 1
	step := bodyOpenDuration / 6
	edges := func(key string) (a, b int) {
		press(m, key)
		adv(step)
		a = edgeOf(pane(m), list, body, x0)
		adv(step)
		b = edgeOf(pane(m), list, body, x0)
		if a <= 0 || b <= 0 {
			t.Fatalf("%s: 途中の姿が「左は一覧・右は開き切った本文」になっていない (境目 %d → %d):\n%s", key, a, b, strings.Join(pane(m), "\n"))
		}
		adv(time.Second)
		return a, b
	}
	if a, b := edges("l"); b >= a {
		t.Errorf("開く途中で境目が左へ進まない: %d → %d", a, b)
	}
	if got := pane(m); strings.Join(got, "\n") != strings.Join(body, "\n") {
		t.Errorf("開き切った姿が前と違う:\n%s", strings.Join(got, "\n"))
	}
	if a, b := edges("h"); b <= a {
		t.Errorf("一覧へ戻る途中で境目が右へ進まない: %d → %d", a, b)
	}
	if got := pane(m); strings.Join(got, "\n") != strings.Join(list, "\n") {
		t.Errorf("戻り切った姿が一覧と違う:\n%s", strings.Join(got, "\n"))
	}
}

// 一覧へ戻る途中で開き直すと、その場で開く演出に切り替わる (戻る演出の続きを本文の上で見せない)。
func TestReopeningWhileClosingOpensAgain(t *testing.T) {
	m := twoTopics(t)
	adv := clockAt(m, day0)
	press(m, "l")
	adv(time.Second)
	body := pane(m)
	press(m, "h")
	adv(time.Second)
	list := pane(m)
	press(m, "l")
	adv(time.Second)
	press(m, "h")
	adv(bodyOpenDuration / 6)
	press(m, "l")
	step := bodyOpenDuration / 6
	adv(step)
	a := edgeOf(pane(m), list, body, m.leftWidth()+1)
	adv(step)
	b := edgeOf(pane(m), list, body, m.leftWidth()+1)
	if a <= 0 || b <= 0 || b >= a {
		t.Errorf("戻る途中で開き直したのに、境目が左へ進まない: %d → %d:\n%s", a, b, strings.Join(pane(m), "\n"))
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
