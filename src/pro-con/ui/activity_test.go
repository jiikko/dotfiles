package ui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"pro-con/backend"
)

// activitySpy は spy に活動を読む口 (backend.ActivityReader) を足したもの。読んだカードを記録する。
type activitySpy struct {
	*spy
	items map[string][]backend.Activity
	asked []string
}

func (s *activitySpy) Activity(id string) ([]backend.Activity, error) {
	s.asked = append(s.asked, id)
	return s.items[id], nil
}

// activityModel は drawerModel と同じ盤面に、W1 の活動 n 件 (2 つの session にまたがる) を持つ backend を置く。
func activityModel(t *testing.T, n int) (*Model, *activitySpy, *clock) {
	t.Helper()
	m, sp, clk := drawerModel(t)
	be := &activitySpy{spy: sp, items: map[string][]backend.Activity{}}
	for i := range n {
		s := "s-old"
		if i >= n/2 {
			s = "s-w1"
		}
		be.items["W1"] = append(be.items["W1"], backend.Activity{At: sp.snap.Now, Session: s, Tool: "Bash", Text: fmt.Sprintf("cmd-%02d", i)})
	}
	m.be = be
	return m, be, clk
}

// deliver は開いたときに裏で走らせた読み取りの結果を届ける。
func deliver(m *Model, be *activitySpy) {
	m.Update(activityMsg{cardID: m.drawerCard, items: be.items[m.drawerCard]})
}

// fetch は裏の読み取りを 1 回走らせて取り込む (tea の実行を介さない)。
func fetch(t *testing.T, m *Model) {
	t.Helper()
	cmd := m.fetchActivity()
	if cmd == nil {
		t.Fatal("活動を読みに行かない")
	}
	m.Update(cmd())
}

// 詳細を開くと、開いたカードの活動を裏で読み、道具の呼び出しを時刻つきで出す。再開で session が入れ替わった境目を出す。
func TestDrawerShowsActivity(t *testing.T) {
	m, be, clk := activityModel(t, 4)
	open(t, m, clk)
	if !m.act.loading {
		t.Fatal("開いたのに活動を読みに行っていない")
	}
	press(m, "G")
	if !strings.Contains(screen(m), "読んでいる…") {
		t.Fatalf("読み終える前に読んでいると出さない:\n%s", screen(m))
	}
	deliver(m, be)
	fetch(t, m)
	press(m, "G")
	out := screen(m)
	for _, want := range []string{"Bash: cmd-00", "Bash: cmd-03", "── 再開: session s-w1 ──"} {
		if !strings.Contains(out, want) {
			t.Fatalf("活動に %q が無い:\n%s", want, out)
		}
	}
	if be.asked[len(be.asked)-1] != "W1" {
		t.Fatalf("開いたカードの活動を読んでいない: %v", be.asked)
	}
	if strings.Contains(out, "出力") {
		t.Fatalf("活動を読める backend で出力の末尾の節を出した:\n%s", out)
	}
}

// 本文の末尾を見ていれば、足された活動を追って末尾に留まる。途中を読んでいれば位置を動かさない。
func TestDrawerActivityFollowsTail(t *testing.T) {
	m, be, clk := activityModel(t, 40)
	open(t, m, clk)
	deliver(m, be)
	press(m, "G")
	add := func() {
		be.items["W1"] = append(be.items["W1"], backend.Activity{At: be.snap.Now.Add(time.Minute), Session: "s-w1", Tool: "Edit", Text: fmt.Sprintf("new-%d", len(be.items["W1"]))})
	}
	add()
	fetch(t, m)
	if !strings.Contains(screen(m), "Edit: new-40") {
		t.Fatalf("末尾を見ているのに足された活動を追わない:\n%s", screen(m))
	}
	press(m, "g")
	add()
	fetch(t, m)
	if m.pager.Offset != 0 {
		t.Fatalf("先頭を読んでいるのに位置を動かした: offset=%d", m.pager.Offset)
	}
}

// 読んでいる間に別のカードへ送ったら、前のカードの活動は出さない (送った先を読み直す)。
func TestDrawerActivityDropsStaleCard(t *testing.T) {
	m, be, clk := activityModel(t, 4)
	open(t, m, clk)
	stale := activityMsg{cardID: "W1", items: be.items["W1"]}
	press(m, "J") // W2 へ
	m.Update(stale)
	if m.act.card == "W1" || strings.Contains(screen(m), "cmd-00") {
		t.Fatalf("送った後に前のカードの活動を出した:\n%s", screen(m))
	}
	if !m.act.loading {
		t.Fatal("送った先の活動を読み直さない")
	}
}

// 活動を読めない backend (模擬) は、今までどおり出力の末尾を出す。
func TestDrawerWithoutActivityReaderShowsLog(t *testing.T) {
	m, _, clk := drawerModel(t)
	open(t, m, clk)
	press(m, "G")
	if m.act.loading || !strings.Contains(screen(m), "出力") {
		t.Fatalf("模擬の詳細に出力の節が無い / 読めない活動を読みに行った:\n%s", screen(m))
	}
}

// 閉じて同じカードを開き直したら、前に開いたときの活動を出さずに読み直す。
func TestDrawerReopenRefetchesActivity(t *testing.T) {
	m, be, clk := activityModel(t, 4)
	open(t, m, clk)
	deliver(m, be)
	press(m, "q")
	clk.t = clk.t.Add(drawerDuration)
	m.onFrame()
	open(t, m, clk)
	if m.act.card == "W1" || !m.act.loading {
		t.Fatalf("開き直したのに前の活動を出したまま / 読み直さない: card=%q loading=%v", m.act.card, m.act.loading)
	}
}

// 途中を読んでいる間に上限で頭の活動が捨てられても、読んでいる行は同じ位置に留まる。
func TestDrawerActivityKeepsPositionWhenHeadIsDropped(t *testing.T) {
	m, be, clk := activityModel(t, 40)
	open(t, m, clk)
	deliver(m, be)
	press(m, "G")
	for range 5 {
		press(m, "k")
	}
	want := screen(m)
	be.items["W1"] = append(be.items["W1"][5:], backend.Activity{At: be.snap.Now.Add(time.Minute), Session: "s-w1", Tool: "Edit", Text: "new"})
	fetch(t, m)
	anchor := regexp.MustCompile(`cmd-\d\d`).FindString(want) // 今見えている最初の活動
	if anchor == "" || anchor < "cmd-05" {
		t.Fatalf("前提: 捨てられない活動が見えていない (%q):\n%s", anchor, want)
	}
	if got := screen(m); strings.Index(got, anchor) != strings.Index(want, anchor) {
		t.Fatalf("頭が捨てられて読んでいる行がずれた:\n前\n%s\n後\n%s", want, got)
	}
}

// 応答の文は markdown として整形して出す (486): 改行・箇条書きが元の形で読め、コードブロックは色が付く。道具の呼び出しは 1 行のまま。
func TestDrawerRendersResponseAsMarkdown(t *testing.T) {
	m, be, clk := activityModel(t, 0)
	now := be.snap.Now
	be.items["W1"] = []backend.Activity{
		{At: now, Session: "s-w1", Text: "どれにしますか?\n\n- A: 左端に帯\n- B: 右端に印\n\n```go\nfunc main() {}\n```"},
		{At: now, Session: "s-w1", Tool: "Bash", Text: "go test ./..."},
	}
	open(t, m, clk)
	deliver(m, be)
	raw := m.drawerBody()
	var lines []string
	for _, l := range raw {
		lines = append(lines, ansi.Strip(l))
	}
	idx := func(sub string) int {
		for i, l := range lines {
			if strings.Contains(l, sub) {
				return i
			}
		}
		return -1
	}
	q, a, b, code, tool := idx("どれにしますか?"), idx("A: 左端に帯"), idx("B: 右端に印"), idx("func main() {}"), idx("Bash: go test ./...")
	if q < 0 || a <= q || b <= a || code <= b || tool <= code {
		t.Fatalf("応答の文が行に分かれて順に出ていない (q=%d a=%d b=%d code=%d tool=%d):\n%s", q, a, b, code, tool, strings.Join(lines, "\n"))
	}
	if strings.Contains(lines[q], "A: 左端に帯") {
		t.Fatalf("改行を潰して 1 行に並べた: %q", lines[q])
	}
	if !strings.Contains(raw[code], "\x1b[") {
		t.Fatalf("コードブロックに色が付いていない: %q", raw[code])
	}
}
