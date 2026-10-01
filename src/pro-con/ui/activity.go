package ui

// 詳細の引き出しの「活動」: 作業中のカードの PG の応答の文と道具の呼び出しを時刻の順に出し、新しいものが来たら追う (issue 467)。
// backend.ActivityReader を持つ backend (本物・--view) だけ。持たない backend (模擬) は「出力」(出力の末尾) のまま。
// 🚨 読むだけ (--view でも出す)。transcript を読むので裏で呼び、同時に 1 本だけ走らせる。

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/jiikko/dotfiles/src/tuikit/markdown"

	"pro-con/backend"
)

// activityMsg は裏で読んだ活動。
type activityMsg struct {
	cardID string
	items  []backend.Activity
	err    error
}

// activityView は引き出しに出している活動。card は items を読んだカード (開いているカードと違えば出さない)。
//
// 🚨 整形した行を 2 段で保持する (issue 606。活動は最大 1000 件で、描くたびに markdown と折り返しを通すと 1 描画が 7〜8 ms かかっていた):
//   - section: items を全部組んだ活動の節 (sectionW の幅)。items を差し替えるときに一緒に作り直す。items だけを書き換えない
//   - lines: 活動 1 件ぶんの行 (linesW の幅)。読み直し (tick ごと) で同じ活動が届くので、差し替えをまたいで引き継ぐ
type activityView struct {
	card    string
	items   []backend.Activity
	err     error
	loading bool // 裏で読んでいる (二重に走らせない)

	section  []string
	sectionW int
	lines    map[backend.Activity][]string
	linesW   int
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
	before := m.drawerLines().Len()
	follow := same && m.pager.Offset >= before-rows
	if same && !follow { // 上限で頭の活動が捨てられた分だけ位置を上げる (読んでいる行が上へずれていかない)
		if k := slices.Index(m.act.items, firstOr(msg.items)); k > 0 {
			dropped := len(m.activitySection()) - len(m.buildSection(m.act.items[k:]))
			m.pager.Offset = max(m.pager.Offset-dropped, 0)
		}
	}
	lines, linesW := m.act.lines, m.act.linesW
	m.act = activityView{card: msg.cardID, items: msg.items, err: msg.err, lines: lines, linesW: linesW}
	m.pruneActivityLines()
	m.activitySection() // 届いたときに組んでおく (描くたびに組まない)
	if follow {
		m.pager.Stop()
		m.pager.Offset = max(m.drawerLines().Len()-rows, 0)
	}
}

// activitySection は items を組んだ活動の節。幅が変わっていなければ保持したものを返す。
func (m *Model) activitySection() []string {
	if w := m.drawerTextWidth(); m.act.section == nil || m.act.sectionW != w {
		m.act.section, m.act.sectionW = m.buildSection(m.act.items), w
	}
	return m.act.section
}

// buildSection は items を活動の節の行に組む (再開の区切り線を挟む)。活動 1 件ぶんの行は m.act.lines から引く。
// 🚨 m.act.section は書かない (位置の補正で、今の items と違う並びの行数を数えるのにも使う)。
func (m *Model) buildSection(items []backend.Activity) []string {
	if len(items) == 0 {
		return []string{} // nil にしない (activitySection が「まだ組んでいない」と読む)
	}
	w := m.drawerTextWidth()
	out := []string{}
	session := items[0].Session
	for _, a := range items {
		if a.Session != session { // 再開で session が入れ替わった境目
			session = a.Session
			out = append(out, wrapStyled(sgrDim, "  ── 再開: session "+orDash(a.Session)+" ──", w)...)
		}
		out = append(out, m.activityLines(a, w)...)
	}
	return out
}

// activityLines は活動 1 件の行 (時刻の字下げと折り返しまで済ませたもの)。幅が変わったら保持を捨てる。
func (m *Model) activityLines(a backend.Activity, w int) []string {
	if m.act.lines == nil || m.act.linesW != w {
		m.act.lines, m.act.linesW = map[backend.Activity][]string{}, w
	}
	if ls, ok := m.act.lines[a]; ok {
		return ls
	}
	// 時刻は At.Local() で組んだまま保持する。time.Local は起動時に決まり、pro-con は実行中に書き換えない (書き換えるのはテストだけ) ので
	// 保持の鍵に入れない。実行中に時間帯を切り替える機能を足すなら、時間帯を linesW と同じく鍵に入れる (issue 606 の codex レビュー)
	stamp := "  " + a.At.Local().Format("15:04") + " "
	var ls []string
	if a.Tool != "" {
		ls = wrapStyled("", stamp+sgrCyan+a.Tool+sgrFgReset+": "+a.Text, w)
	} else {
		// 応答の文は markdown として整形する (見出し・箇条書き・コードブロックのハイライト。486)。時刻の幅だけ字下げして並べる
		body, _ := markdown.Render(a.Text, max(w-len(stamp), 10), true)
		for i, l := range body {
			lead := stamp
			if i > 0 {
				lead = strings.Repeat(" ", len(stamp))
			}
			ls = append(ls, wrapStyled("", lead+l, w)...)
		}
	}
	m.act.lines[a] = ls
	return ls
}

// pruneActivityLines は今の items に無い活動の行を捨てる (引き継いだ保持を無制限に育てない)。
func (m *Model) pruneActivityLines() {
	if len(m.act.lines) <= len(m.act.items) {
		return
	}
	keep := make(map[backend.Activity]struct{}, len(m.act.items))
	for _, a := range m.act.items {
		keep[a] = struct{}{}
	}
	for a := range m.act.lines {
		if _, ok := keep[a]; !ok {
			delete(m.act.lines, a)
		}
	}
}

func firstOr(as []backend.Activity) backend.Activity {
	if len(as) == 0 {
		return backend.Activity{}
	}
	return as[0]
}

// addActivity は引き出しの本文へ活動の節の見出しを足し、活動の行 (保持したもの) を返す (add は drawerBody の折り返して足す口)。
func (m *Model) addActivity(add func(style, text string)) []string {
	add(sgrDim, "活動 (PG の応答の文と道具の呼び出し。新しいものは下に足す。全部は pro-con card log "+m.drawerCard+")")
	if m.act.card != m.drawerCard {
		add(sgrDim, "  読んでいる…")
		return nil
	}
	if m.act.err != nil {
		add(sgrYellow, "  読めないところがある: "+m.act.err.Error())
	}
	if len(m.act.items) == 0 {
		add(sgrDim, "  まだ無い (pro-con が起動した session の transcript が見つからない)")
		return nil
	}
	return m.activitySection()
}
