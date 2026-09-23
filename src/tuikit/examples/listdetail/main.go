// listdetail は tuikit の「一覧 → 詳細」の遷移パターンを 1 画面で見せるデモ。
// 動かし方と gif の撮り方は tuikit/README.md の「デモ」。
//
//	go run ./examples/listdetail          # 実速度
//	go run ./examples/listdetail -slow 3  # 演出を 3 倍に伸ばす (gif で動きを追うため)
//
// キー: 移動は listnav.MotionOf の語彙 (j/k・ctrl+n/p・↑↓ / Space・f・ctrl+d・b・ctrl+u 半ページ /
// g・G 端) / Enter・l 詳細を開く / 詳細では同じ語彙でスクロール、J/K 隣の項目、Esc・h 閉じる / q 終了
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"tuikit/anim"
	"tuikit/layout"
	"tuikit/listnav"
	"tuikit/sgr"
	"tuikit/termwidth"
)

// 所要とフレーム数は glogx の issues viewer と同じ値 (体感を合わせてある)。
const (
	listOpenDuration = 175 * time.Millisecond // 一覧が流れ込む演出
	listStagger      = 0.35                   // 行ごとの開始のずらし
	drawerDuration   = 112 * time.Millisecond // 詳細の引き出し
	scrollFrames     = 6                      // 詳細の半ページスクロール (× tick 16ms ≒ 100ms)
	cursorFrames     = 20                     // 一覧の半ページ移動でカーソルが滑る (≒ 320ms)
	tickInterval     = 16 * time.Millisecond
)

var drawerGeometry = layout.DrawerGeometry{Ratio: 0.8, Extra: 10, MinList: 8, MaxPeek: 18}

const sgrCursor = "\x1b[7m"

type item struct {
	num, state, category, title string
}

type tickMsg struct{}

type model struct {
	items  []item
	width  int
	height int
	slow   float64

	list      listnav.List // 一覧のカーソルと窓 (半ページでは描画カーソルだけが滑る)
	listStart time.Time    // 一覧が流れ込む演出の開始時刻

	drawer anim.Transition
	open   int // 開いている項目 (-1 = なし)。閉じる演出の間は残す
	body   []string
	pager  listnav.Pager // 詳細のスクロール (半ページだけ滑る)

	ticking bool
}

func (m *model) dur(d time.Duration) time.Duration { return time.Duration(float64(d) * m.slow) }

func (m *model) frames(n int) int { return max(int(float64(n)*m.slow), 1) }

func (m *model) Init() tea.Cmd {
	m.listStart = time.Now()
	return m.tick()
}

// tick は演出が動いている間だけ再描画を続ける (tuikit は tick を回さない。使う側の約束)。
func (m *model) tick() tea.Cmd {
	if m.ticking {
		return nil
	}
	m.ticking = true
	return tea.Tick(tickInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *model) animating(now time.Time) bool {
	return now.Sub(m.listStart) < m.dur(listOpenDuration) ||
		m.drawer.Animating(now) || m.list.Animating() || m.pager.Animating()
}

// listRows は一覧に使える行数 (ヘッダ 2 行とフッタ 1 行を除く)。
func (m *model) listRows() int { return max(m.height-3, 1) }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	now := time.Now()
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.list.Fit(len(m.items), m.listRows())
		m.buildBody()
	case tickMsg:
		m.ticking = false
		if m.drawer.Settle(now) {
			m.open, m.body = -1, nil // 閉じ切ってから中身を捨てる (逆再生の間は見えている)
		}
		m.list.Advance()
		m.pager.Advance()
	case tea.KeyPressMsg:
		// 演出中に来たキーは着地させてから効かせる (演出の終わりまで待たせない)
		if m.drawer.Finish() {
			m.open, m.body = -1, nil
		}
		m.list.Stop()
		if cmd := m.key(msg.String()); cmd != nil {
			return m, cmd
		}
	}
	if m.animating(now) {
		return m, m.tick()
	}
	return m, nil
}

func (m *model) key(k string) tea.Cmd {
	if k == "q" || k == "ctrl+c" {
		return tea.Quit
	}
	if m.drawer.Phase() == anim.Open {
		return m.bodyKey(k)
	}
	// 画面固有の動作キーを先に捌き、残りを移動の語彙へ渡す
	switch k {
	case "enter", "l":
		m.openItem(m.list.Cursor)
		m.drawer.Open(time.Now(), m.dur(drawerDuration))
		return nil
	}
	m.list.Move(listnav.MotionOf(k), len(m.items), m.listRows(), m.frames(cursorFrames))
	return nil
}

func (m *model) bodyKey(k string) tea.Cmd {
	switch k {
	case "esc", "h":
		m.pager.Stop()
		m.drawer.Close(time.Now(), m.dur(drawerDuration))
		return nil
	case "J", "K":
		// 開いたまま中身だけ差し替える (板は動かない。左に覗く一覧のカーソルが追従して見える)
		mo := listnav.Down
		if k == "K" {
			mo = listnav.Up
		}
		if m.list.Move(mo, len(m.items), m.listRows(), 0) {
			m.openItem(m.list.Cursor)
		}
		return nil
	}
	m.pager.Move(listnav.MotionOf(k), len(m.body), m.listRows(), m.frames(scrollFrames))
	return nil
}

func (m *model) openItem(i int) {
	m.open = i
	m.pager.Reset()
	m.buildBody()
}

// buildBody は詳細を**開ききった幅で**整形する。演出中は ComposeDrawer が切るだけ
// (途中の幅で整形し直すと毎フレーム折り返しが変わって文字が踊る)。
func (m *model) buildBody() {
	if m.open < 0 || m.width == 0 {
		return
	}
	it := m.items[m.open]
	w := max(drawerGeometry.Target(m.width)-1-layout.ScrollbarWidth-1, 1) // 区切り線とスクロールバーぶんを引く
	lines := []string{
		sgr.Bold + fmt.Sprintf(" %s %s: %s", it.num, it.category, it.title) + sgr.Reset,
		sgr.Dim + " 状態: " + it.state + sgr.Reset,
		"",
	}
	for p := range 6 {
		lines = append(lines, sgr.Cyan+fmt.Sprintf(" ## 節 %d", p+1)+sgr.Reset)
		lines = append(lines, wrap(fmt.Sprintf("%s の説明が続く。詳細は右から滑り込む引き出しとして一覧の上に重なり、"+
			"左には一覧の端 (番号・状態・カテゴリ) が残る。どの行から開いたかが画面から消えない。", it.title), w)...)
		lines = append(lines, "")
	}
	m.body = lines
}

// wrap は s を表示幅 w で折り返す (デモ用の素朴な実装。語の境界は見ない)。
func wrap(s string, w int) []string {
	var out []string
	line := " "
	for _, r := range s {
		if termwidth.Of(line+string(r)) > w {
			out = append(out, line)
			line = " "
		}
		line += string(r)
	}
	return append(out, line)
}

func (m *model) listLines() []string {
	rows := m.listRows()
	cur := m.list.DrawCursor(len(m.items))
	w := m.width - layout.ScrollbarWidth // バー列ぶんを先に引く (layout.ScrollbarWidth の doc)
	var out []string
	for i := m.list.Offset; i < min(m.list.Offset+rows, len(m.items)); i++ {
		it := m.items[i]
		mark := "  "
		if i == m.list.Cursor {
			mark = "→ "
		}
		ln := fmt.Sprintf("%s%s %s %-9s %s", mark, it.num, it.state, it.category, it.title)
		if i == cur {
			ln = sgrCursor + termwidth.FillRight(ln, w) + sgr.Reset
		} else if it.state == "*" {
			ln = sgr.Yellow + ln + sgr.Reset
		}
		out = append(out, ln)
	}
	return layout.Scrollbar(layout.PadTo(out, rows), m.width, len(m.items), m.list.Offset, true)
}

func (m *model) View() tea.View {
	if m.width == 0 {
		return tea.NewView("")
	}
	now := time.Now()
	body := m.listLines()
	if m.open >= 0 {
		target := drawerGeometry.Target(m.width)
		w := layout.DrawerWidth(target, m.drawer.Openness(now, anim.EaseOutCubic))
		off := m.pager.DrawOffset(len(m.body), len(body))
		panel := layout.PadTo(m.body[min(off, len(m.body)):], len(body))
		panel = layout.Scrollbar(panel, target-1, len(m.body), off, true) // 板の幅 - 区切り線
		body = layout.ComposeDrawer(body, panel, w, m.width, true)
	}
	if p := float64(now.Sub(m.listStart)) / float64(m.dur(listOpenDuration)); p < 1 {
		body = layout.SlideIn(body, p, m.width, false, listStagger)
	}
	head := []string{
		sgr.Bold + " tuikit demo — list / detail" + sgr.Reset,
		sgr.Dim + strings.Repeat("─", m.width) + sgr.Reset,
	}
	hint := " j/k 移動  Space/b 半ページ  g/G 端  Enter 開く  q 終了"
	if m.drawer.Phase() != anim.Closed {
		hint = " j/k・Space/b スクロール  J/K 隣へ  Esc 閉じる  q 終了"
	}
	all := append(head, body...)
	all = append(all, sgr.Dim+termwidth.Clip(hint, m.width)+sgr.Reset)
	v := tea.NewView(strings.Join(all, "\n"))
	v.AltScreen = true
	return v
}

func demoItems() []item {
	cats := []string{"feat", "bug", "refactor", "docs", "research", "ux"}
	titles := []string{
		"引き出しの開閉を壁時計で進める", "閉じる途中で開き直すと跳ぶ", "状態機械を 1 つにまとめる",
		"README に遷移パターンを書く", "端末の幅モデルを調べる", "半ページ移動でカーソルを滑らせる",
		"一覧の端を残して詳細を重ねる", "折り返しを最終幅で固定する", "tick を演出中だけ回す",
		"全角と半角の桁を揃える", "逆再生で中身を残す", "キー入力で演出を着地させる",
		"一覧が上から流れ込む", "ANSI を保ったまま切る", "gif を撮る手順を残す",
		"狭い端末では全幅にする", "覗き見の上限を決める", "スクロールを ease-in にする",
	}
	// 🚨 状態記号は ASCII にしておく。○ ● ✓ (East Asian Ambiguous) は vhs の xterm.js が 2 桁と
	// 数え、差分描画の位置がずれてカーソル行の反転が消える (gif でだけ起きる。tmux では正しい)。
	states := []string{"-", "*", "+"}
	// 画面に収まらない件数にする (スクロールバーと窓の送りが見えるように)
	const n = 45
	items := make([]item, n)
	for i := range items {
		items[i] = item{num: fmt.Sprintf("%03d", n-i), state: states[i%3], category: cats[i%len(cats)], title: titles[i%len(titles)]}
	}
	return items
}

func main() {
	slow := flag.Float64("slow", 1, "演出の所要時間の倍率 (gif で動きを追うときに上げる)")
	flag.Parse()
	m := &model{items: demoItems(), open: -1, slow: max(*slow, 0.1)}
	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
