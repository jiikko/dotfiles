package ui

import (
	"image/color"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"tuikit/anim"
	"tuikit/layout"

	"pro-con/backend"
)

// 新版への切り替え (ctrl+r) の演出 (issue 509。2026-09-26 にユーザーが見本から選んだ: 色を背景へ寄せる・400ms・中央の枠)。
//
//   - 旧版: 画面全体の色を端末の背景色へ寄せて暗くし、中央の枠に「新版へ切り替え中…」を浮かべる。暗くなりきったら終了して
//     main に exec を任せる。暗くなった最後の 1 枚は alt screen に残したまま (upgrade.Screen) 新版へ渡す
//   - 新版: 旧版の最後の 1 枚を受け取り (uiState.Frame)、最初の読み込みが届くまでそのまま出す (出ている画面と同じなので跳ばない)。
//     届いたら (か arriveWait を過ぎたら) 自分のボードを暗いところから明るく戻す
//
// 暗くするのは SGR の色の書き換え (端末の dim (SGR 2) は段が 1 つしか無くアニメにならない)。既定の色 (色を指定していない文字と地) は
// 端末に問い合わせた色 (OSC 10 / 11) を使い、答えが無ければ暗い背景を前提にした値を使う。

const (
	switchDuration = 400 * time.Millisecond
	switchFloor    = 0.30 // 暗くなりきったときの明るさ (元の色の 30%)
	// arriveWait は新版が最初の読み込みを待つ上限 (本物の backend は最初の読み直しに claude agents の一覧を待ち、最大 10 秒かかる。
	// 長く暗いままにしない: 過ぎたら読み込みの前のボードで明るく戻し、後から中身が入る)
	arriveWait   = time.Second
	switchLabel  = "↻ 新版へ切り替え中…"
	arrivedLabel = "↻ 新版に切り替えた"
	switchAccent = 202 // 現在地の色 (style.go の冒頭の合意)
)

type rgb struct{ r, g, b int }

// 端末に色を問い合わせられないときの既定の色 (暗い背景の端末)。
var (
	defaultTermFg = rgb{208, 208, 208}
	defaultTermBg = rgb{0, 0, 0}
)

// switchFade は切り替えの演出の状態。
type switchFade struct {
	leaving  time.Time // 旧版として暗くし始めた時刻 (zero = 暗くしていない)
	lastView string    // 最後に描いた画面 (暗くしている間だけ持つ。新版へ渡す)
	arriving string    // 新版として受け取った旧版の最後の 1 枚 ("" = 受け取っていない / 明るく戻し始めた)
	arrived  time.Time // 新版として明るく戻し始めた時刻
	fg, bg   *rgb      // 端末の既定の色 (問い合わせの答え。nil なら defaultTerm*)
}

type arriveGoMsg struct{}

func toRGB(c color.Color) *rgb {
	if c == nil {
		return nil
	}
	r, g, b, _ := c.RGBA()
	return &rgb{int(r >> 8), int(g >> 8), int(b >> 8)}
}

func (s *switchFade) termColors() (rgb, rgb) {
	fg, bg := defaultTermFg, defaultTermBg
	if s.fg != nil {
		fg = *s.fg
	}
	if s.bg != nil {
		bg = *s.bg
	}
	return fg, bg
}

// OnFirstView は最初の View で 1 度だけ呼ぶ関数を置く。
func (m *Model) OnFirstView(f func()) { m.firstView = f }

// startLeaving は旧版の暗転を始める (requestUpgrade)。
func (m *Model) startLeaving() tea.Cmd {
	m.fade.leaving = m.now()
	return m.startFrames()
}

// leavingProgress は旧版の暗転の進み 0..1 (暗転していなければ -1)。
func (m *Model) leavingProgress(now time.Time) float64 {
	if m.fade.leaving.IsZero() {
		return -1
	}
	return anim.Elapsed(m.fade.leaving, now, switchDuration)
}

// arrivingProgress は新版の明転の進み 0..1 (明転していなければ -1。受け取った 1 枚を出して待っている間も -1)。
func (m *Model) arrivingProgress(now time.Time) float64 {
	if m.fade.arrived.IsZero() {
		return -1
	}
	return anim.Elapsed(m.fade.arrived, now, switchDuration)
}

func (m *Model) switchAnimating(now time.Time) bool {
	if p := m.leavingProgress(now); p >= 0 {
		return true // 暗くなりきった後も、終了 (onFrame の leaveDone) までコマを回す
	}
	p := m.arrivingProgress(now)
	return p >= 0 && p < 1
}

// leaveDone は暗くなりきったか。なりきったら終了して main に切り替えを任せる (onFrame が呼ぶ)。
// 暗くなりきった 1 枚は、この Update の後の View で描かれてから終わる (bubbletea は終了のときに最後の View を書き出す)。
func (m *Model) leaveDone() tea.Cmd {
	if m.leavingProgress(m.now()) < 1 || m.up == nil || m.up.requested {
		return nil
	}
	m.up.requested = true
	if m.up.keepScreen != nil {
		m.up.keepScreen()
	}
	return tea.Quit
}

// cancelLeaving は暗転を取り消す (切り替えに失敗して旧版のまま続ける)。明るい画面へ戻す (暗いままにしない)。
func (m *Model) cancelLeaving() { m.fade.leaving, m.fade.lastView = time.Time{}, "" }

// startArriving は受け取った旧版の最後の 1 枚 (frame) を出して待つ (ImportState)。
func (m *Model) startArriving(frame string) { m.fade.arriving = frame }

// arriveCmd は新版の明転を始める合図を用意する (Init)。変化を知らせる backend なら最初の読み込み (changedMsg) を待ち、
// arriveWait を上限にする。知らせない backend (模擬) は最初から中身があるので、すぐ始める。
func (m *Model) arriveCmd() tea.Cmd {
	if m.fade.arriving == "" {
		return nil
	}
	if _, ok := m.be.(backend.Notifier); !ok {
		return func() tea.Msg { return arriveGoMsg{} }
	}
	return tea.Tick(arriveWait, func(time.Time) tea.Msg { return arriveGoMsg{} })
}

// beginArrive は明転を始める (最初の読み込みが届いた / 待ちの上限を過ぎた)。2 度目以降は何もしない。
func (m *Model) beginArrive() tea.Cmd {
	if m.fade.arriving == "" {
		return nil
	}
	m.fade.arriving, m.fade.arrived = "", m.now()
	return m.startFrames()
}

// applySwitchFade は描いた画面 r に切り替えの演出を掛ける (View)。
func (m *Model) applySwitchFade(r string) string {
	if m.fade.arriving != "" {
		return m.fade.arriving
	}
	now := m.now()
	fg, bg := m.fade.termColors()
	if p := m.leavingProgress(now); p >= 0 {
		t := anim.EaseOutCubic(p)
		out := m.overlaySwitchLabel(dimANSI(r, 1-(1-switchFloor)*t, fg, bg), switchLabel, t, bg)
		m.fade.lastView = out
		return out
	}
	if p := m.arrivingProgress(now); p >= 0 && p < 1 {
		t := 1 - anim.EaseOutCubic(p) // 暗さ (1 = 暗くなりきり)
		return m.overlaySwitchLabel(dimANSI(r, 1-(1-switchFloor)*t, fg, bg), arrivedLabel, t, bg)
	}
	return r
}

// overlaySwitchLabel は中央の枠に文言を重ねる。alpha (0..1) で背景から浮かび上がる (暗転と同じ速さで)。
func (m *Model) overlaySwitchLabel(screen, label string, alpha float64, bg rgb) string {
	if alpha <= 0 {
		return screen
	}
	r, g, b := rgb256(switchAccent)
	c := mixRGB(bg, rgb{r, g, b}, alpha)
	col := "\x1b[0;1;38;2;" + strconv.Itoa(c.r) + ";" + strconv.Itoa(c.g) + ";" + strconv.Itoa(c.b) + "m"
	body := " " + label + " "
	w := ansi.StringWidth(body)
	box := []string{
		col + "╭" + strings.Repeat("─", w) + "╮" + sgrReset,
		col + "│" + body + "│" + sgrReset,
		col + "╰" + strings.Repeat("─", w) + "╯" + sgrReset,
	}
	lines := strings.Split(screen, "\n")
	return strings.Join(layout.OverlayCentered(lines, box, m.width, len(lines), true), "\n")
}

func mixRGB(a, b rgb, t float64) rgb {
	lerp := func(x, y int) int { return x + int(float64(y-x)*t+0.5) }
	return rgb{lerp(a.r, b.r), lerp(a.g, b.g), lerp(a.b, b.b)}
}

// basic16 は基本 16 色 (SGR 30〜37 / 90〜97) の RGB。端末ごとに違うので xterm の値で近似する。
var basic16 = [16]rgb{{0, 0, 0}, {205, 0, 0}, {0, 205, 0}, {205, 205, 0}, {0, 0, 238}, {205, 0, 205}, {0, 205, 205}, {229, 229, 229},
	{127, 127, 127}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0}, {92, 92, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255}}

func color256(n int) rgb {
	if n < 16 {
		return basic16[n]
	}
	r, g, b := rgb256(n)
	return rgb{r, g, b}
}

// sgrState は SGR を読み進めたときの前景と地 (nil = 端末の既定)。
type sgrState struct{ fg, bg *rgb }

// dimANSI は s の色を、端末の地 bg へ寄せて明るさを k (1 = 元のまま、0 = 地と同じ) にする。
// 色を指定していない文字は端末の既定の前景 fg として暗くする (明示の色に直さないと暗くならない)。既定の地は bg そのものなので触らない。
// 色以外の SGR (太字・下線など) と SGR 以外の制御列はそのまま残す。
func dimANSI(s string, k float64, fg, bg rgb) string {
	if k >= 1 {
		return s
	}
	var st sgrState
	dim := func(c rgb) rgb { return mixRGB(bg, c, k) }
	colors := func() string {
		f := fg
		if st.fg != nil {
			f = *st.fg
		}
		out := ";38;2;" + rgbParams(dim(f))
		if st.bg == nil {
			return out + ";49"
		}
		return out + ";48;2;" + rgbParams(dim(*st.bg))
	}
	var b strings.Builder
	b.Grow(len(s) * 2)
	lineHead := func() { b.WriteString("\x1b[" + colors()[1:] + "m") }
	lineHead()
	for i := 0; i < len(s); {
		switch {
		case s[i] == '\n':
			b.WriteByte('\n')
			lineHead() // 行ごとに色を置き直す (行をまたいで色が続くかは描く側の読み方に依る)
			i++
		case strings.HasPrefix(s[i:], "\x1b["):
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j >= len(s) {
				b.WriteString(s[i:])
				return b.String()
			}
			if s[j] != 'm' {
				b.WriteString(s[i : j+1])
			} else {
				// 色だけの SGR (keep が空) で先頭の ; を残すと、空の引数 = 0 (reset) になって太字などが消える
				b.WriteString("\x1b[" + strings.TrimPrefix(st.read(s[i+2:j])+colors(), ";") + "m")
			}
			i = j + 1
		default:
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String()
}

func rgbParams(c rgb) string {
	return strconv.Itoa(c.r) + ";" + strconv.Itoa(c.g) + ";" + strconv.Itoa(c.b)
}

// read は SGR の引数 params を読んで色を st に取り込み、色以外の引数を返す (空の SGR は 0 = reset として読む)。
// 読めない形 (: 区切りの色・引数の足りない色) は捨てる (その後ろの引数も。区切りが読めないので)。
func (st *sgrState) read(params string) string {
	if params == "" {
		params = "0"
	}
	ps := strings.Split(params, ";")
	keep := []string{}
	num := func(i int) (int, bool) {
		if i >= len(ps) {
			return 0, false
		}
		if ps[i] == "" {
			return 0, true
		}
		n, err := strconv.Atoi(ps[i])
		return n, err == nil
	}
	for i := 0; i < len(ps); i++ {
		p, ok := num(i)
		if !ok { // : 区切り (38:2::r:g:b・4:3 など) は読めないので捨てる (色なら後ろに足す暗い既定の色になる)
			continue
		}
		switch {
		case p == 0:
			st.fg, st.bg = nil, nil
			keep = append(keep, "0")
		case p == 38 || p == 48:
			c, n, ok := extColor(i, num)
			if !ok { // 読めない色 (引数の足りない 38;5 など) は残さない: 残すと後ろに足す暗い色の引数と繋がって別の色に読まれる
				i = len(ps)
				continue
			}
			if p == 38 {
				st.fg = &c
			} else {
				st.bg = &c
			}
			i += n
		case p == 39:
			st.fg = nil
		case p == 49:
			st.bg = nil
		case p >= 30 && p <= 37:
			c := basic16[p-30]
			st.fg = &c
		case p >= 90 && p <= 97:
			c := basic16[p-90+8]
			st.fg = &c
		case p >= 40 && p <= 47:
			c := basic16[p-40]
			st.bg = &c
		case p >= 100 && p <= 107:
			c := basic16[p-100+8]
			st.bg = &c
		default:
			keep = append(keep, ps[i])
		}
	}
	return strings.Join(keep, ";")
}

// extColor は i 番目の引数 (38 / 48) に続く色 (5;n か 2;r;g;b) を読む。読んだ引数の数 (38 / 48 を除く) を返す。
func extColor(i int, num func(int) (int, bool)) (rgb, int, bool) {
	kind, ok := num(i + 1)
	if !ok {
		return rgb{}, 0, false
	}
	switch kind {
	case 5:
		n, ok := num(i + 2)
		if !ok || n < 0 || n > 255 {
			return rgb{}, 0, false
		}
		return color256(n), 2, true
	case 2:
		r, ok1 := num(i + 2)
		g, ok2 := num(i + 3)
		b, ok3 := num(i + 4)
		if !ok1 || !ok2 || !ok3 {
			return rgb{}, 0, false
		}
		return rgb{r, g, b}, 4, true
	}
	return rgb{}, 0, false
}

// execOnTerminal は tea.ExecProcess の前に、子の Stdout を端末 (os.Stdout) に固定する。
// 🚨 bubbletea は子の Stdout が空なら画面の出力を渡す。main は画面の出力を upgrade.Screen で包むので (*os.File でない)、
// そのまま渡ると os/exec がパイプを挟み、attach の claude やエディタの stdout が端末でなくなる。
func execOnTerminal(c *exec.Cmd, fn tea.ExecCallback) tea.Cmd {
	if c.Stdout == nil {
		c.Stdout = os.Stdout
	}
	return tea.ExecProcess(c, fn)
}
