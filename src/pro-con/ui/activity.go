package ui

// 詳細の引き出しの「活動」: 作業中のカードの PG の応答の文と道具の呼び出しを時刻の順に出し、新しいものが来たら追う (issue 467)。
// backend.ActivityReader を持つ backend (本物・--view) だけ。持たない backend (模擬) は「出力」(出力の末尾) のまま。
// 🚨 読むだけ (--view でも出す)。transcript を読むので裏で呼び、同時に 1 本だけ走らせる。

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"pro-con/backend"
)

// activityMsg は裏で読んだ活動。
type activityMsg struct {
	cardID string
	items  []backend.Activity
	err    error
}

// activityView は引き出しに出している活動。card は items を読んだカード (開いているカードと違えば出さない)。
type activityView struct {
	card    string
	items   []backend.Activity
	err     error
	loading bool // 裏で読んでいる (二重に走らせない)
}

func (m *Model) activityReader() backend.ActivityReader {
	r, _ := m.be.(backend.ActivityReader)
	return r
}

// fetchActivity は引き出しを開いているカードの活動を裏で読み直す (読んでいる最中・引き出しを閉じている・読めない backend なら nil)。
func (m *Model) fetchActivity() tea.Cmd {
	r := m.activityReader()
	if r == nil || m.drawerCard == "" || m.act.loading {
		return nil
	}
	m.act.loading = true
	id := m.drawerCard
	return func() tea.Msg {
		items, err := r.Activity(id)
		return activityMsg{cardID: id, items: items, err: err}
	}
}

// trackActivity は開いているカードが変わった (開いた・J / K で送った) ら、tick を待たずに読む。
func (m *Model) trackActivity() tea.Cmd {
	if m.drawerCard == "" || m.drawerCard == m.act.card {
		return nil
	}
	return m.fetchActivity()
}

// onActivity は読んだ活動を取り込む。本文の末尾を見ていたなら (新しいものを待っていた)、足された分だけ追って末尾に留まる。
// 開いた直後の 1 回目は追わない (先頭の状態・依頼の原文から読む)。
func (m *Model) onActivity(msg activityMsg) {
	m.act.loading = false
	if msg.cardID != m.drawerCard {
		return // 読んでいる間に別のカードへ送った・閉じた (次の trackActivity が読み直す)
	}
	rows := m.drawerBodyRows()
	same := m.act.card == msg.cardID
	before := len(m.drawerBody())
	follow := same && m.pager.Offset >= before-rows
	if same && !follow { // 上限で頭の活動が捨てられた分だけ位置を上げる (読んでいる行が上へずれていかない)
		if k := slices.Index(m.act.items, firstOr(msg.items)); k > 0 {
			old := m.act.items
			m.act.items = old[k:]
			m.pager.Offset = max(m.pager.Offset-(before-len(m.drawerBody())), 0)
			m.act.items = old
		}
	}
	m.act = activityView{card: msg.cardID, items: msg.items, err: msg.err}
	if follow {
		m.pager.Stop()
		m.pager.Offset = max(len(m.drawerBody())-rows, 0)
	}
}

func firstOr(as []backend.Activity) backend.Activity {
	if len(as) == 0 {
		return backend.Activity{}
	}
	return as[0]
}

// addActivity は引き出しの本文へ活動の節を足す (add は drawerBody の折り返して足す口)。
func (m *Model) addActivity(add func(style, text string)) {
	add(sgrDim, "活動 (PG の応答の文と道具の呼び出し。新しいものは下に足す。全部は pro-con card log "+m.drawerCard+")")
	if m.act.card != m.drawerCard {
		add(sgrDim, "  読んでいる…")
		return
	}
	if m.act.err != nil {
		add(sgrYellow, "  読めないところがある: "+m.act.err.Error())
	}
	if len(m.act.items) == 0 {
		add(sgrDim, "  まだ無い (pro-con が起動した session の transcript が見つからない)")
		return
	}
	session := m.act.items[0].Session
	for _, a := range m.act.items {
		if a.Session != session { // 再開で session が入れ替わった境目
			session = a.Session
			add(sgrDim, "  ── 再開: session "+orDash(a.Session)+" ──")
		}
		line := a.Text
		if a.Tool != "" {
			line = sgrCyan + a.Tool + sgrFgReset + ": " + a.Text
		}
		add("", "  "+a.At.Local().Format("15:04")+" "+line)
	}
}
