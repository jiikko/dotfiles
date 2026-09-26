package ui

// 設定画面 (s で開閉。issue 456)。右から入ってくる全幅の板で、中を「設定 / プロセス / ディスク / ログ」のタブに分ける (tab で切り替え。
// ログのタブは settingslog.go。issue 512)。
// 変える所 (設定のタブ) は PG の枠と PM の数を ← → で 1 ずつ変え、受付の箱に置く (dispatcher の次の Tick から効く。止めずに変わる)。
// 見る所 (プロセス・ディスク) は backend.Inspector を裏で読むだけ。見ているだけの画面 (--view) は変える所 (設定のタブ) を出さない。
// 🚨 見る所は描くたびに読まない (ディスクは数秒かかる)。開いたとき・プロセスのタブへ移ったとき・r で、裏で 1 回だけ読む。

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"tuikit/anim"
	"tuikit/layout"
	"tuikit/listnav"

	"pro-con/backend"
	"pro-con/card"
	"pro-con/diskuse"
)

type settingsTab int

const (
	tabConfig settingsTab = iota // 変える所 (と見る所の要約)
	tabProcs                     // 役ごとのプロセスの一覧 (pro-con ps と同じ出どころ)
	tabDisk                      // ディスクの使用量と内訳 (pro-con du と同じ)
	tabLog                       // プロセスの出来事 (pro-con log と同じ出どころ。settingslog.go)
)

func (t settingsTab) label() string {
	switch t {
	case tabConfig:
		return "設定"
	case tabProcs:
		return "プロセス"
	case tabDisk:
		return "ディスク"
	case tabLog:
		return "ログ"
	}
	return ""
}

// settingsReverse は設定画面の選んだタブと値の反転 (設定画面だけで使うので ui の共通の定数にしない)。
const settingsReverse = "\x1b[7m"

// 設定のタブの行 (選ぶ行の順)。
var configKeys = []string{backend.ConfigLimit, backend.ConfigUsage, backend.ConfigPM}

// diskItemsShown はディスクのタブで、置き場ごとに内訳を出す数 (Enter で開いた置き場は全部)。
const diskItemsShown = 3

type settings struct {
	open   bool
	anim   anim.Transition
	tab    settingsTab
	cursor int // 今のタブの選べる行の添字
	offset int // 表示の窓の先頭 (行)

	showStopped bool   // プロセスのタブで、止まっている役の畳んだ行を開いた
	openGroup   string // ディスクのタブで、内訳を全部出している置き場 (diskuse.Group.Name。空なら閉じている)

	procs        []backend.Proc
	procsErr     error
	procsAt      time.Time // 読んだ時刻 (zero ならまだ読んでいない)
	procsLoading bool

	disk        diskuse.Usage
	diskErr     error
	diskLoading bool

	log eventLog // ログのタブ (settingslog.go)

	// want は ← → で置いた値のうち、まだ Snapshot に出ていないもの (続けて押したときに前の値から数える)。wantAt は置いた時刻
	want   map[string]int
	wantAt time.Time
}

type procsMsg struct {
	rows []backend.Proc
	err  error
	at   time.Time
}

type diskMsg struct {
	u   diskuse.Usage
	err error
}

func (m *Model) inspector() backend.Inspector {
	i, _ := m.be.(backend.Inspector)
	return i
}

// settingsTabs は出すタブ。変える所を受けない画面 (--view) は見る所だけ。
func (m *Model) settingsTabs() []settingsTab {
	if !m.accepts(backend.OpConfig) {
		return []settingsTab{tabProcs, tabDisk, tabLog}
	}
	return []settingsTab{tabConfig, tabProcs, tabDisk, tabLog}
}

// openSettings は設定画面を開き、見る所を裏で読む。
func (m *Model) openSettings() tea.Cmd {
	s := &m.set
	s.open = true
	tab := s.tab
	if tabs := m.settingsTabs(); !containsTab(tabs, tab) {
		tab = tabs[0]
	}
	enter := m.enterSettingsTab(tab)
	s.anim.Open(m.now(), drawerDuration)
	return tea.Batch(m.fetchProcs(), m.measureDisk(), enter, m.startFrames())
}

// enterSettingsTab は t のタブへ移る (選ぶ行を先頭へ。ログのタブは最新 = 一番下)。移った先で読むものがあれば裏で読む。
func (m *Model) enterSettingsTab(t settingsTab) tea.Cmd {
	s := &m.set
	s.tab, s.cursor, s.offset = t, 0, 0
	switch t {
	case tabProcs:
		return m.fetchProcs()
	case tabLog:
		s.log.follow = true
		s.cursor = max(len(s.log.rows)-1, 0)
		return m.fetchEvents()
	case tabConfig, tabDisk:
	}
	return nil
}

func (m *Model) closeSettings() tea.Cmd {
	m.set.open = false
	m.set.anim.Close(m.now(), drawerDuration)
	return m.startFrames()
}

func containsTab(ts []settingsTab, t settingsTab) bool {
	for _, x := range ts {
		if x == t {
			return true
		}
	}
	return false
}

// settingsShown は板が見えている (開いている・開閉の途中) か。
func (m *Model) settingsShown() bool {
	return m.set.open || m.set.anim.Animating(m.now())
}

// fetchProcs はプロセスを裏で読む (読んでいる最中・読めない backend なら nil)。
func (m *Model) fetchProcs() tea.Cmd {
	in := m.inspector()
	if in == nil || m.set.procsLoading {
		return nil
	}
	m.set.procsLoading = true
	now := m.now
	return m.child(func() tea.Msg {
		rows, err := in.Procs()
		return procsMsg{rows: rows, err: err, at: now()}
	})
}

// measureDisk はディスクを裏で測る (数秒かかる。測っている最中・読めない backend なら nil)。前に測った結果は、測り終えるまで残して出す。
func (m *Model) measureDisk() tea.Cmd {
	in := m.inspector()
	if in == nil || m.set.diskLoading {
		return nil
	}
	m.set.diskLoading = true
	// 🚨 m.child に入れない: 外のコマンドを起こさない (ファイルを歩くだけ) ので、端末を明け渡す前に待つ必要が無い。入れると数秒の測定の間
	// quit と ctrl+r (新版への切り替え) が待たされ、5 秒を超えると切り替えが断られる
	return func() tea.Msg {
		u, err := in.DiskUsage()
		return diskMsg{u: u, err: err}
	}
}

func (m *Model) handleSettingsKey(k string) tea.Cmd {
	s := &m.set
	switch k {
	case "ctrl+c", "Q":
		return m.requestQuit()
	case "ctrl+r": // 新版への切り替えは画面全体の操作 (どの板からも効く)
		return m.requestUpgrade()
	case "s", "q", "esc":
		return m.closeSettings()
	case "tab", "shift+tab":
		tabs := m.settingsTabs()
		i := 0
		for j, t := range tabs {
			if t == s.tab {
				i = j
			}
		}
		step := 1
		if k == "shift+tab" {
			step = len(tabs) - 1
		}
		return m.enterSettingsTab(tabs[(i+step)%len(tabs)])
	case "left", "h", "ctrl+b":
		return m.stepConfig(-1)
	case "right", "l", "ctrl+f":
		return m.stepConfig(1)
	case "enter":
		m.toggleSettingsRow()
		return nil
	case "c":
		if s.tab == tabLog {
			m.toggleCardEvents()
		}
		return nil
	case "r":
		switch s.tab {
		case tabProcs:
			return m.fetchProcs()
		case tabDisk:
			return m.measureDisk()
		case tabLog:
			return m.fetchEvents()
		case tabConfig:
		}
		return tea.Batch(m.fetchProcs(), m.measureDisk())
	}
	n := m.settingsRowCount()
	half := 0 // 半ページの送り (短いタブは端まで。ログは長いので logHalfPage ずつ)
	if s.tab == tabLog {
		half = logHalfPage
	}
	switch listnav.MotionOf(k) {
	case listnav.Down:
		s.cursor = min(s.cursor+1, max(n-1, 0))
	case listnav.Up:
		s.cursor = max(s.cursor-1, 0)
	case listnav.HalfUp:
		if half == 0 {
			s.cursor = 0
			break
		}
		s.cursor = max(s.cursor-half, 0)
	case listnav.HalfDown:
		if half == 0 {
			s.cursor = max(n-1, 0)
			break
		}
		s.cursor = min(s.cursor+half, max(n-1, 0))
	case listnav.Top:
		s.cursor = 0
	case listnav.Bottom:
		s.cursor = max(n-1, 0)
	case listnav.None:
	}
	s.log.follow = s.tab == tabLog && s.cursor >= n-1 // 一番下へ送ったら、また最新に付ける
	return nil
}

// settingsRowCount は今のタブの選べる行の数 (ログのタブは畳んだ行ごとに選べる)。
func (m *Model) settingsRowCount() int {
	if m.set.tab == tabLog {
		return len(m.set.log.rows)
	}
	return len(m.settingsSelectable())
}

// stepConfig は設定のタブで選んでいる値を delta だけ変えるよう受付の箱に置く (ほかのタブでは何もしない)。
func (m *Model) stepConfig(delta int) tea.Cmd {
	s := &m.set
	if s.tab != tabConfig || s.cursor >= len(configKeys) {
		return nil
	}
	if !m.accepts(backend.OpConfig) {
		m.refuse("この画面では設定を変えない (見ているだけの画面 = pro-con --view)")
		return nil
	}
	key := configKeys[s.cursor]
	v, _ := m.configValue(key)
	next := v + delta
	value := strconv.Itoa(next)
	if key == backend.ConfigUsage { // チェックボックス: ← → も Enter も入れ替える
		next, value = 1-v, "on"
		if next == 0 {
			value = "off"
		}
	}
	if key == backend.ConfigLimit && next < 1 {
		m.refuse("PG の枠は 1 以上 (PG を止めるなら pro-con dispatcher --stop)")
		return nil
	}
	res, err := m.be.Apply(backend.SetConfig{Key: key, Value: value})
	if err != nil { // PM の 2 以上などは backend が置く前に断る (pro-con config と同じ検査)
		m.refuse(err.Error())
		return nil
	}
	if s.want == nil {
		s.want = map[string]int{}
	}
	s.want[key], s.wantAt = next, m.now()
	m.done(res)
	m.setSnap(m.be.Snapshot())
	return nil
}

// configValue は key の今の値と、置いた値の適用を待っているか。置いた値が Snapshot に出たら (または dispatcher が除けたら) 待ちを外す。
func (m *Model) configValue(key string) (int, bool) {
	c := m.snap.Config
	v := c.Limit
	switch {
	case key == backend.ConfigUsage:
		v = 1
		if c.UsageOff {
			v = 0
		}
	case key == backend.ConfigPM:
		v = c.PMs
		if v == 0 {
			v = 1 // 設定が無ければ 1 つで動く
		}
	case v == 0:
		v = m.snap.LimitMax // 設定が無ければ dispatcher の --limit か既定
	}
	w, ok := m.set.want[key]
	if !ok {
		return v, false
	}
	// 適用を待つ設定の依頼が無く、置いた値が出た・置いた後に読んだ Snapshot なら、適用されたか除けられた (どちらも Snapshot の値が正しい)。
	// 🚨 依頼が残っている間は、値が一致しても外さない (→ ← と続けて押すと、途中の値が出てから戻るまで待ちが続く)
	if c.Pending == 0 && (w == v || m.snap.Now.After(m.set.wantAt)) {
		delete(m.set.want, key)
		return v, false
	}
	return w, true
}

// settingsSelectable は今のタブの選べる行の鍵 (設定 = 設定の名前 / プロセス = 畳んだ行だけ / ディスク = 置き場の名前。
// ログのタブは鍵を持たない = Enter で開くものが無い。選べる数は settingsRowCount)。
func (m *Model) settingsSelectable() []string {
	switch m.set.tab {
	case tabConfig:
		return configKeys
	case tabProcs:
		if _, stopped := splitProcs(m.set.procs); len(stopped) > 0 {
			return []string{"stopped"}
		}
		return nil
	case tabDisk:
		var out []string
		for _, g := range m.set.disk.Groups {
			out = append(out, g.Name)
		}
		return out
	case tabLog:
	}
	return nil
}

// toggleSettingsRow は Enter: 止まっている役の畳んだ行を開閉する / 置き場の内訳を全部出す・戻す。
func (m *Model) toggleSettingsRow() {
	s := &m.set
	sel := m.settingsSelectable()
	if s.cursor >= len(sel) {
		return
	}
	switch s.tab {
	case tabConfig:
		if sel[s.cursor] == backend.ConfigUsage {
			_ = m.stepConfig(0)
		}
	case tabProcs:
		s.showStopped = !s.showStopped
	case tabDisk:
		if s.openGroup == sel[s.cursor] {
			s.openGroup = ""
		} else {
			s.openGroup = sel[s.cursor]
		}
	case tabLog:
	}
}

// splitProcs は動いているもの (と dispatcher・supervisor・見張りの行・カードと食い違う PG) と、止まっている役 (PM・取り込み・テストの係) に分ける
// (止まっている行は 1 行に畳む)。🚨 止まった PG のうち食い違いの無いもの (終わったカードの止まった session) は出さず、数えもしない (issue 497)。
func splitProcs(rows []backend.Proc) (live, stopped []backend.Proc) {
	for _, p := range rows {
		if p.Role == "PG" && p.State == backend.ProcStopped && p.Mismatch == "" {
			continue
		}
		if p.State == backend.ProcStopped && p.Mismatch == "" && p.Role != "dispatcher" && p.Role != "supervisor" && p.Role != "見張り" {
			stopped = append(stopped, p)
			continue
		}
		live = append(live, p)
	}
	return live, stopped
}

// overlaySettings は領域 (ヘッダと下端の群のあいだ) に設定画面の板を重ねる (右から全幅で入ってくる)。
func (m *Model) overlaySettings(region []string) []string {
	if !m.settingsShown() {
		return region
	}
	w := layout.DrawerWidth(m.width, m.set.anim.Openness(m.now(), anim.EaseOutCubic))
	return layout.ComposeDrawer(region, m.settingsPanel(len(region)), w, m.width, true)
}

// settingsPanel は板の行 (開ききった幅で組む)。rows は領域の行数。選んでいる行が窓に入るよう送る。
func (m *Model) settingsPanel(rows int) []string {
	w := m.width - 1 // 左端の 1 桁は板の縁 (ComposeDrawer の区切り線)
	var tabs []string
	for _, t := range m.settingsTabs() {
		if t == m.set.tab {
			tabs = append(tabs, settingsReverse+sgrBold+" "+t.label()+" "+sgrReset)
		} else {
			tabs = append(tabs, sgrDim+" "+t.label()+" "+sgrReset)
		}
	}
	head := []string{" " + strings.Join(tabs, "  ") + sgrDim + "      tab で切り替え" + sgrReset, ""}
	shown := max(rows-len(head), 1)
	if m.set.tab == tabLog { // ログは長いので、枠の見出しと列の名前を残して行だけを送る (settingslog.go)
		return append(head, m.logLines(w, shown)...)
	}
	var body []string
	cur := 0
	switch m.set.tab {
	case tabConfig:
		body, cur = m.configLines(w)
	case tabProcs:
		body, cur = m.procLines(w)
	case tabDisk:
		body, cur = m.diskLines(w)
	case tabLog:
	}
	m.set.offset = listnav.WindowOffset(m.set.offset, cur, len(body), shown)
	end := min(len(body), m.set.offset+shown)
	return append(head, body[min(m.set.offset, end):end]...)
}

// configLines は設定のタブ (変える所と見る所の要約)。cur は選んでいる行の位置。
func (m *Model) configLines(w int) ([]string, int) {
	border := fg(51)
	out := []string{boxTop(border, sgrBold+"変える所"+sgrReset, w)}
	cur := 0
	for i, key := range configKeys {
		v, waiting := m.configValue(key)
		name, note := "PM の数", "今は 1 だけ (2 以上は 415 の論点 6 が決まってから)"
		val := fmt.Sprintf(" ‹ %d › ", v)
		switch key {
		case backend.ConfigLimit:
			name, note = "PG の枠 (同時に動かす PG の上限)", m.limitNote()
		case backend.ConfigUsage:
			name, note, val = "利用枠を見て PG を絞る", "80% で 1 本 / 95% で 0 本。外すと上限まで起動する (Enter か ← →)", " [ ] "
			if v == 1 {
				val = " [x] "
			}
		}
		mark := "  "
		if i == m.set.cursor {
			val, mark, cur = settingsReverse+val+sgrReset, sgrCyan+"▸ "+sgrFgReset, len(out)
		}
		if waiting {
			note = sgrYellow + "適用待ち (dispatcher の次の Tick で効く)" + sgrFgReset + "  " + note
		}
		out = append(out, boxLine(border, " "+mark+fit(name, 34)+val+"  "+sgrDim+note+sgrReset, w))
	}
	out = append(out, boxLine(border, "", w))
	out = append(out, boxLine(border, sgrDim+"   ← → で変えると受付の箱に置き、dispatcher の次の Tick から効く (止めずに変わる)。利用枠の絞り (80% で 1 本 / 95% で 0 本) は、チェックが入っている間この上限より優先"+sgrReset, w))
	if e := m.snap.Config.Err; e != "" {
		out = append(out, boxLine(border, sgrRed+"   設定のファイルを読めない (dispatcher は --limit で動く): "+e+sgrFgReset, w))
	}
	out = append(out, boxBottom(border, w), boxTop(border, sgrBold+"見る所 (要約。tab で開く)"+sgrReset, w))
	out = append(out, boxLine(border, "  プロセス  "+m.procsSummary(), w), boxLine(border, "  ディスク  "+m.diskSummary(), w))
	return append(out, boxBottom(border, w)), cur
}

// limitNote は PG の枠の出どころと、今の枠・使っている数・空き待ち。
func (m *Model) limitNote() string {
	c := m.snap.Config
	from := "設定"
	if c.Limit == 0 {
		from = "設定なし (dispatcher の --limit か既定)"
	}
	queued := 0
	for _, cc := range m.snap.Cards {
		if cc.State == card.Planned {
			queued++
		}
	}
	note := fmt.Sprintf("%s · 今の枠 %d · PG %d 本 · 空き待ち %d 件", from, m.snap.Limit, m.snap.SlotsUsed(), queued)
	if m.snap.LimitWhy != "" {
		note += " · " + m.snap.LimitWhy
	}
	return note
}

func (m *Model) procsSummary() string {
	switch {
	case m.inspector() == nil:
		return sgrDim + "この画面では読めない" + sgrReset
	case m.set.procsAt.IsZero():
		return sgrDim + "読んでいる…" + sgrReset
	case m.set.procsErr != nil && len(m.set.procs) == 0: // 読めなかったのを 0 本に見せない
		return sgrYellow + "読めない: " + m.set.procsErr.Error() + sgrFgReset
	}
	live, _ := splitProcs(m.set.procs)
	odd := 0
	for _, p := range live {
		if p.Mismatch != "" {
			odd++
		}
	}
	return fmt.Sprintf("動いている %d・食い違い %d", len(live)-odd, odd)
}

func (m *Model) diskSummary() string {
	d := m.set.disk
	switch {
	case m.inspector() == nil:
		return sgrDim + "この画面では測れない" + sgrReset
	case m.set.diskErr != nil:
		return sgrDim + m.set.diskErr.Error() + sgrReset
	case d.MeasuredAt.IsZero():
		return sgrDim + "測っている… (数秒かかる)" + sgrReset
	}
	return "合計 " + diskuse.Human(d.Total) + "  " + sgrDim + m.measuredNote() + sgrReset
}

func (m *Model) measuredNote() string {
	d := m.set.disk
	s := fmt.Sprintf("%s に測った (%s)", d.MeasuredAt.Local().Format("15:04"), fmtSec(d.Took))
	if m.set.diskLoading {
		s += " · 測り直している…"
	}
	return s
}

func fmtSec(d time.Duration) string { return strconv.FormatFloat(d.Seconds(), 'f', 1, 64) + " 秒" }

// procLines はプロセスのタブ。動いているものとカードと食い違う PG を出し、止まっている役は 1 行に畳んで Enter で開く。下に開いている画面の一覧 (presence の印) を添える。
func (m *Model) procLines(w int) ([]string, int) {
	border := fg(51)
	s := &m.set
	title := sgrBold + "プロセス (pro-con が起動したもの)" + sgrReset
	if !s.procsAt.IsZero() {
		title += sgrDim + "  " + s.procsAt.Local().Format("15:04:05") + " に読んだ · r で読み直す" + sgrReset
	}
	out := []string{boxTop(border, title, w)}
	cur := 0
	switch {
	case m.inspector() == nil:
		out = append(out, boxLine(border, sgrDim+" この画面では読めない"+sgrReset, w))
	case s.procsAt.IsZero():
		out = append(out, boxLine(border, sgrDim+" 読んでいる…"+sgrReset, w))
	default:
		if s.procsErr != nil {
			out = append(out, boxLine(border, sgrYellow+" 読めないところがある: "+s.procsErr.Error()+sgrFgReset, w))
		}
		out = append(out, boxLine(border, sgrDim+fit(" 役", 12)+fit("pid", 8)+fit("経過", 13)+fit("状態", procStateCells)+fit("カード", procCardCells)+fit("session", 14)+"今のコマンド"+sgrReset, w))
		live, stopped := splitProcs(s.procs)
		screens := m.screensBlock(w)
		for _, p := range live {
			if p.Role == "画面" && len(screens) > 0 { // 下に開いている画面の一覧 (モード・端末つき) を出すときは、そちらで出す
				continue
			}
			out = append(out, boxLine(border, m.procRow(p, w), w))
		}
		if len(stopped) > 0 {
			mark, verb := "▸", "enter で開く"
			if s.showStopped {
				mark, verb = "▾", "enter で畳む"
			}
			row := fmt.Sprintf(" %s 止まっている役 %d 本 (%s … %s。%s)", mark, len(stopped), procName(stopped[0]), procName(stopped[len(stopped)-1]), verb)
			cur = len(out)
			if s.cursor == 0 {
				row = sgrCyan + "▌" + sgrFgReset + sgrBold + strings.TrimPrefix(row, " ") + sgrReset
			} else {
				row = fg(239) + row + sgrFgReset
			}
			out = append(out, boxLine(border, row, w))
			if s.showStopped {
				for _, p := range stopped {
					out = append(out, boxLine(border, fg(239)+m.procRow(p, w)+sgrFgReset, w))
				}
			}
		}
	}
	out = append(out, boxBottom(border, w))
	return append(out, m.screensBlock(w)...), cur
}

func procName(p backend.Proc) string {
	if p.Card != "" {
		return p.Card
	}
	return p.Role
}

// procRow はプロセスの 1 行。カードの列は、画面が知っているカードならタイトルまで出す (前の PG の一覧と同じ cardHeading。
// タイトルの頭の issue 番号は落とす。issue 491)。
func (m *Model) procRow(p backend.Proc, w int) string {
	pid, age := "-", "-"
	if p.PID > 0 {
		pid = strconv.Itoa(p.PID)
	}
	if p.Age > 0 {
		age = fmtDur(p.Age)
	}
	col, state := "", p.State
	switch {
	case p.Mismatch != "":
		col, state = sgrYellow, "! "+p.Mismatch // 食い違い (幅の揺れる ⚠ は列を崩すので使わない)
	case p.State == backend.ProcStopped:
		col = fg(239)
	case strings.HasPrefix(p.State, "動いている"), strings.HasPrefix(p.State, "実行中"), strings.HasPrefix(p.State, "開いている"):
		col = fg(46)
	}
	// 今のコマンドは残りの幅だけ (狭い端末では前の列を崩さずに切る)
	return fit(" "+p.Role, 12) + fit(pid, 8) + fit(age, 13) + col + fit(state, procStateCells) + sgrFgReset + fit(m.procCard(p.Card), procCardCells) +
		fit(orDash(p.Session), 14) + fit(orDash(p.Command), max(w-2-procFixedCells, 0))
}

// procStateCells は状態の列の幅 (「動いている (最後の Tick 12s 前)」が入る)。procCardCells はカードの列の幅 (ID と issue 番号とタイトルの頭)。procFixedCells は今のコマンドより前の列の幅の和。
const (
	procStateCells = 34
	procCardCells  = 32
	procFixedCells = 12 + 8 + 13 + procStateCells + procCardCells + 14
)

// diskGroupNames は置き場の見出しと、中身の数の単位。
var diskGroupNames = map[string][2]string{
	diskuse.GroupWorktrees:   {"PG・役の worktree", "個"},
	diskuse.GroupTranscripts: {"transcript (pro-con の session)", "個"},
	diskuse.GroupState:       {"状態の置き場", "項目"},
	diskuse.GroupBinary:      {"pro-con のバイナリ", "本"},
}

// diskBarCells はディスクのタブの割合の棒の幅。
const diskBarCells = 24

// diskLines はディスクのタブ。置き場ごとに大きさ・割合・数と、大きい順の内訳 (Enter で開いた置き場は全部)。
func (m *Model) diskLines(w int) ([]string, int) {
	border := fg(51)
	s := &m.set
	d := s.disk
	title := sgrBold + "ディスク" + sgrReset
	if !d.MeasuredAt.IsZero() {
		title = sgrBold + "ディスク 合計 " + diskuse.Human(d.Total) + sgrReset + sgrDim + "  " + m.measuredNote() + " · r で測り直す" + sgrReset
	}
	out := []string{boxTop(border, title, w)}
	cur := 0
	switch {
	case m.inspector() == nil:
		out = append(out, boxLine(border, sgrDim+" この画面では測れない"+sgrReset, w))
	case s.diskErr != nil:
		out = append(out, boxLine(border, sgrDim+" "+s.diskErr.Error()+sgrReset, w))
	case d.MeasuredAt.IsZero():
		out = append(out, boxLine(border, sgrDim+" 測っている… (worktree を全部歩くので数秒かかる)"+sgrReset, w))
	default:
		for gi, g := range d.Groups {
			name := diskGroupNames[g.Name]
			frac := 0.0
			if d.Total > 0 {
				frac = float64(g.Bytes) / float64(d.Total)
			}
			done := 0
			for _, it := range g.Items {
				if it.Done {
					done++
				}
			}
			extra := ""
			if done > 0 {
				extra = fmt.Sprintf("  うち完了したカード %d %s", done, name[1])
			}
			k := int(frac*diskBarCells + 0.5)
			bar := fg(45) + strings.Repeat("█", k) + fg(239) + strings.Repeat("░", diskBarCells-k) + sgrFgReset
			row := " " + fit(diskuse.Human(g.Bytes), 7) + bar + " " + fit(fmt.Sprintf("%3.0f%%", frac*100), 5) + fit(orDash(name[0]), 34) +
				sgrDim + fmt.Sprintf("%d %s", g.Count, name[1]) + extra + sgrReset
			if gi == s.cursor {
				cur = len(out)
				row = sgrCyan + "▌" + sgrFgReset + strings.TrimPrefix(row, " ")
			}
			out = append(out, boxLine(border, row, w))
			items := g.Items
			if s.openGroup != g.Name && len(items) > diskItemsShown {
				items = items[:diskItemsShown]
			}
			for _, it := range items {
				note := it.Card
				if it.Done {
					note += " 完了"
				}
				out = append(out, boxLine(border, sgrDim+"      "+fit(diskuse.Human(it.Bytes), 7)+fit(clipLeft(it.Name, 44), 46)+note+sgrReset, w))
			}
			if rest := len(g.Items) - len(items); rest > 0 {
				out = append(out, boxLine(border, sgrDim+fmt.Sprintf("      … ほか %d %s (enter で全部)", rest, name[1])+sgrReset, w))
			}
		}
		if n := len(d.Warnings); n > 0 {
			out = append(out, boxLine(border, sgrYellow+fmt.Sprintf(" 読めない所 %d 件 (その分は数えていない): %s", n, d.Warnings[0])+sgrFgReset, w))
		}
	}
	return append(out, boxBottom(border, w)), cur
}

// clipLeft は長い名前を頭から切る (transcript の置き場の名前は末尾に worktree の名前がある)。
func clipLeft(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return "…" + string(r[len(r)-n+1:])
}

// settingsHints は設定画面の案内。
func (m *Model) settingsHints() []string {
	h := []string{"tab 次のタブ", "j / k 選ぶ"}
	switch m.set.tab {
	case tabConfig:
		h = append(h, "← → 値を変える", "r 測り直す")
	case tabProcs:
		h = append(h, avail("enter 止まっている役を開く / 畳む", len(m.settingsSelectable()) > 0), "r 読み直す")
	case tabDisk:
		h = append(h, "enter 内訳", "r 測り直す")
	case tabLog:
		verb := "c カードの出来事も出す"
		if m.set.log.showCards {
			verb = "c プロセスの出来事だけ"
		}
		h = append(h, verb, "G 最新へ")
	}
	return append(h, "s / q / esc 閉じる")
}

// procCard はカードの列の中身 (画面の知らないカード = 片付けた・別の repo のものは ID だけ)。
func (m *Model) procCard(id string) string {
	if id == "" {
		return "-"
	}
	for _, c := range m.snap.Cards {
		if c.ID == id {
			return cardHeading(c)
		}
	}
	return id
}
