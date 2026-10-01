package ui

import (
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"pro-con/backend"
)

// mixedActivity は応答 : 道具 = 1 : 2、応答の一部に code block を含む活動 n 件 (後半で session が入れ替わる)。
// 実データの形 (issue 606: 直近 30 transcript の応答のうち code block を含むのは 4%) に寄せ、code block は 8 件に 1 件にする。
func mixedActivity(now time.Time, n int) []backend.Activity {
	items := make([]backend.Activity, n)
	for i := range items {
		s := "s-old"
		if i >= n/2 {
			s = "s-w1"
		}
		a := backend.Activity{At: now.Add(time.Duration(i) * time.Second), Session: s}
		switch {
		case i%3 != 0:
			a.Tool, a.Text = "Bash", fmt.Sprintf("go test ./... -run Test%04d", i)
		case i%24 == 0:
			a.Text = fmt.Sprintf("応答-%04d\n\n```go\nfunc f(x int) int {\n\treturn x * 2\n}\n```", i)
		default:
			a.Text = fmt.Sprintf("応答-%04d: 端末の幅の計算を **共通の層** へ寄せる。\n\n- 1 つ目\n- 2 つ目", i)
		}
		items[i] = a
	}
	return items
}

// openedActivity は W1 の詳細を開き、活動 n 件を届けた画面。
func openedActivity(t *testing.T, n int) (*Model, *activitySpy) {
	t.Helper()
	m, be, clk := activityModel(t, 0)
	be.items["W1"] = mixedActivity(be.snap.Now, n)
	open(t, m, clk)
	deliver(m, be)
	if n > 0 {
		last := fmt.Sprintf("%04d", n-1) // 最後の活動の番号 (応答は markdown で整形されるので本文そのものでは探さない)
		if !strings.Contains(ansi.Strip(strings.Join(m.drawerBody(), "\n")), last) {
			t.Fatalf("前提: 活動の節が描かれていない (最後の活動 %s が本文に無い)", last)
		}
	}
	return m, be
}

// oneViewBytes は View 1 回の確保バイト (届いた直後の 1 回目を測る。保持を温めてから測ると、描く側で遅れて組む形を見逃す)。
func oneViewBytes(m *Model) uint64 {
	var a, b runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&a)
	_ = m.View()
	runtime.ReadMemStats(&b)
	return b.TotalAlloc - a.TotalAlloc
}

// 詳細を開いた画面の 1 描画の確保量は、活動の件数で増えない (issue 606)。描くたびに全件を markdown と折り返しに通していた
// (1000 件で 1 描画 7〜8 ms)。整形は届いたときに 1 回だけ。時間ではなく確保量の伸び率で見る。
// 見ていないもの: 確保せずに件数に比例して走査する形 (行の数え直し等)。届いたとき (onActivity) の重さ。
func TestDrawerActivityViewAllocDoesNotGrowWithItems(t *testing.T) {
	small, _ := openedActivity(t, 30)
	large, _ := openedActivity(t, 1000)
	s1, l1 := oneViewBytes(small), oneViewBytes(large)
	if r := float64(l1) / float64(s1); r > 1.3 {
		t.Fatalf("活動 30 → 1000 件で、届いた直後の 1 描画の確保量が %.2f 倍 (%d → %d バイト)。件数に比例する整形が描く経路に入った", r, s1, l1)
	}
	s2, l2 := viewBytes(small), viewBytes(large)
	if r := float64(l2) / float64(s2); r > 1.3 {
		t.Fatalf("活動 30 → 1000 件で 1 描画の確保量が %.2f 倍 (%d → %d バイト)。件数に比例する整形が描く経路に入った", r, s2, l2)
	}
}

// 保持した活動の節は、保持を捨てて組み直したものと同じ (issue 606)。届き直し・頭の切り捨て・幅の変更・読めない印のどれでも古い行を出さない。
func TestDrawerActivitySectionMatchesRebuild(t *testing.T) {
	m, be := openedActivity(t, 60)
	check := func(step string) {
		t.Helper()
		got := m.drawerBody()
		sec, lines, lw := m.act.section, m.act.lines, m.act.linesW
		m.act.section, m.act.lines = nil, nil
		want := m.drawerBody()
		m.act.section, m.act.lines, m.act.linesW = sec, lines, lw
		if !slices.Equal(got, want) {
			t.Fatalf("%s: 保持した本文が組み直したものと違う\n保持:\n%s\n組み直し:\n%s", step, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}
	check("届いた直後")

	be.items["W1"] = append(be.items["W1"][7:], backend.Activity{At: be.snap.Now.Add(time.Hour), Session: "s-new", Text: "新しい応答\n\n- 項目"})
	deliver(m, be)
	check("頭を捨てて 1 件足した")
	if !strings.Contains(strings.Join(m.drawerBody(), "\n"), "再開: session s-new") {
		t.Fatal("前提: 足した活動の session の区切りが出ていない")
	}
	if len(m.act.lines) > len(m.act.items) {
		t.Fatalf("今の活動に無い行を保持し続けている (%d 件の活動に %d 件)", len(m.act.items), len(m.act.lines))
	}

	// 幅で折り返しが変わる長い活動を足す (短い行だけだと、幅を変えても行が同じで古い幅の保持を見分けられない)
	long := strings.Repeat("長い道具の呼び出しの引数 ", 12)
	be.items["W1"] = append(be.items["W1"], backend.Activity{At: be.snap.Now.Add(2 * time.Hour), Session: "s-new", Tool: "Bash", Text: long})
	deliver(m, be)
	before := len(m.drawerBody())
	m.width = 70
	check("幅を変えた")
	if len(m.drawerBody()) == before {
		t.Fatal("前提: 幅を変えても本文の行数が変わらない (折り返しが幅に依存する活動が無い)")
	}
	m.width = 120
	check("幅を戻した")

	m.Update(activityMsg{cardID: m.drawerCard, items: be.items["W1"], err: errors.New("壊れた行")})
	check("読めない印が付いた")
}

// deliverBytes は活動を届けたとき (onActivity) の確保バイト。
func deliverBytes(m *Model, be *activitySpy) uint64 {
	var a, b runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&a)
	deliver(m, be)
	runtime.ReadMemStats(&b)
	return b.TotalAlloc - a.TotalAlloc
}

// 読み直し (tick ごと) で同じ活動が届いたら、活動 1 件ずつの整形は作り直さない (issue 606)。作り直すと詳細を開いているあいだ毎秒全件を整形する。
// 初めて届いたときの確保量と比べる (同じ 1000 件)。
func TestDrawerActivityRedeliverReusesLines(t *testing.T) {
	m, be, clk := activityModel(t, 0)
	be.items["W1"] = mixedActivity(be.snap.Now, 1000)
	open(t, m, clk)
	first := deliverBytes(m, be)
	be.items["W1"] = slices.Clone(be.items["W1"]) // 読み直しは別の slice で届く
	again := deliverBytes(m, be)
	if r := float64(again) / float64(first); r > 0.3 {
		t.Fatalf("同じ活動の読み直しで、初めて届いたときの %.2f 倍を確保した (%d → %d バイト)。活動 1 件ずつの整形を作り直している", r, first, again)
	}
}
