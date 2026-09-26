package ui

// ボードの / の検索 (issue 532。見た目は 2026-09-27 にユーザーが見本の案 A を選んだ: 欄は最下段・一致しないカードは隠す)。
//
// 一致の判定は card.Matches (`card list --grep` と同じ)。絞るのはレーンの並び (lanes) だけで、タブの範囲 (visible) は変えない:
// ゲージの件数・x の片付けの対象は今のタブの全部のまま。絞っている間は件数の行の頭に「/ <語> n/N 枚」、レーンの見出しに (一致/全部) を出す。
//
// 入力の作法は glogx の issues の番号の絞り込み (issues_number_filter.go) に揃える: 打つたびに絞る、enter で確定して絞り込みは残す
// (確定後はボードのキーが絞った結果に効く)、esc で絞り込みをやめる。空で確定したら絞り込みごとやめる (印だけ残ると嘘になる)。
// ほかの入力欄 (modeInput) と違い、打っている間もボードを暗くしない (絞った結果を見ながら打つため)。
//
// 🚨 絞っている間は K / J (優先度) と x (完了を片付け) を断る。K / J の隣は backend が絞る前の並びで決めるので、見えている隣と
// 入れ替わらない。x は見えていないカードまで片付ける。どちらも esc で絞り込みをやめてから押す。

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"tuikit/lineedit"
	"tuikit/termwidth"

	"pro-con/card"
)

type search struct {
	active bool // 絞り込みが効いている (確定した後も残る)
	typing bool // 検索語を打っている (キーは欄に取られる)
	line   lineedit.Line
}

// query は今の検索語。
func (s *search) query() string { return s.line.String() }

// keeps はカードが絞り込みに残るか (絞っていなければ全部)。
func (s *search) keeps(c *card.Card) bool { return !s.active || c.Matches(s.query()) }

// startSearch は / で入力を始める。絞っている最中なら検索語の続きから打てる (1 文字直したいだけで全部消えないように)。
func (m *Model) startSearch() {
	if m.showDetail { // 詳細を開いたまま絞ると、引き出しのカードが隠れることがある。閉じてから打つ
		m.closeDrawer()
	}
	m.search.active, m.search.typing = true, true
}

// clearSearch は絞り込みをやめて、隠していたカードを戻す。選んでいたカードはそのまま選ぶ (ID で持っているので)。
func (m *Model) clearSearch() {
	m.search = search{}
	m.afterSearchChange()
}

// afterSearchChange は検索語が変わった後に選択とレーンの位置を合わせる (選んでいたカードが隠れたら、同じレーンの見えているカードへ。
// ensureSelection は絞った後の並び = columns を見る)。
func (m *Model) afterSearchChange() {
	m.resetSlots() // 絞り込みでカードが出入りしても「動いた」の演出はしない (タブの切り替えと同じ。スクロールも戻す)
	m.ensureSelection()
}

// handleSearchKey は打っている間のキー。enter / esc / ctrl+c 以外は編集キーとして lineedit に渡す (glogx-ui-guide §7)。
func (m *Model) handleSearchKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		m.clearSearch()
	case "enter":
		if m.search.line.Empty() {
			m.clearSearch()
			return nil
		}
		m.search.typing = false
	case "ctrl+c":
		return m.requestQuit()
	default:
		m.editSearch(func() { m.search.line.Key(k.String(), k.Text) })
	}
	return nil
}

// pasteSearch はペーストを検索語に入れる (改行とタブは lineedit が空白にする)。
func (m *Model) pasteSearch(s string) {
	m.editSearch(func() { m.search.line.Insert(s) })
}

// editSearch は検索語を edit で変え、変わったら選択とレーンを合わせる。
// 🚨 変える前に今の選択の行を selRow に控える: ensureSelection は選んでいたカードが消えたら selRow の 1 つ上を選ぶが、
// selRow を更新するのは setSnap だけなので、控えないと最後のスナップショットの頃の行で無関係なカードへ飛ぶ
func (m *Model) editSearch(edit func()) {
	before := m.search.query()
	if _, row, ok := m.position(); ok {
		m.selRow = row
	}
	edit()
	if m.search.query() != before {
		m.afterSearchChange()
	}
}

// searchRefusal は絞っている間に断る操作の理由 (断らないなら "")。
func (m *Model) searchRefusal(key string) string {
	if !m.search.active {
		return ""
	}
	switch key {
	case "K", "J":
		return "絞り込み中は優先度を動かさない (隠れたカードと入れ替わる)。esc で絞り込みをやめてから"
	case "x":
		return "絞り込み中は片付けない (隠れた完了のカードも片付く)。esc で絞り込みをやめてから"
	}
	return ""
}

// searchMark は件数の行の頭に出す絞り込みの印 (確定した後だけ。打っている間は最下段の欄が見えている)。枚数は今のタブの中で数える。
func (m *Model) searchMark() string {
	if !m.search.active || m.search.typing {
		return ""
	}
	all, hit := 0, 0
	for c := range m.visible() {
		all++
		if m.search.keeps(c) {
			hit++
		}
	}
	return " " + sgrYellow + sgrBold + "/ " + m.search.query() + sgrFgReset + sgrYellow + fmt.Sprintf(" %d/%d 枚", hit, all) + sgrReset
}

// searchHead は最下段の検索の欄の見出し。
func searchHead() string {
	return " " + sgrBold + fg(202) + "/ 検索 (題名・カード ID・issue 番号・依頼の原文): " + sgrReset + fg(231)
}

// searchField は検索の欄の見出しと、欄に出す文字列・その中のキャレットの桁 (inputField と同じ形)。
func (m *Model) searchField() (head, text string, col int) {
	head = searchHead()
	text, col = m.search.line.Window(m.width - termwidth.Of(head))
	return head, text, col
}

// searchHints は打っている間の案内 (ボードのキーは欄に取られるので出さない。glogx-ui-guide §5)。
func searchHints() []string {
	return []string{"enter 確定", "ctrl+h 1 文字消す", "ctrl+w 1 語消す", "ctrl+u 前を消す", "esc 絞り込みをやめる"}
}

// sgrHit は一致した語の色 (要対応の 214 を太字で。確認の印の桃色と紛れない)。
const sgrHit = "\x1b[38;5;214m\x1b[1m"

// markHits は SGR を含む行 s の、SGR の外の文字列にある words を色で浮かせる (大文字小文字は区別しない)。
// after は浮かせた語の後に戻す SGR (選択中の太字・下線や、完了の dim)。
func markHits(s string, words []string, after string) string {
	if len(words) == 0 {
		return s
	}
	var b strings.Builder
	for len(s) > 0 {
		if s[0] == '\x1b' { // SGR は丸ごと写す (語が色の番号に一致しないように)
			end := strings.IndexByte(s, 'm')
			if end < 0 {
				end = len(s) - 1
			}
			b.WriteString(s[:end+1])
			s = s[end+1:]
			continue
		}
		seg := s
		if i := strings.IndexByte(s, '\x1b'); i >= 0 {
			seg = s[:i]
		}
		b.WriteString(markHitsPlain(seg, words, after))
		s = s[len(seg):]
	}
	return b.String()
}

// markHitsPlain は SGR を含まない seg の words を浮かせる。小文字にするとバイト長が変わる文字を含むなら浮かせない (位置がずれるため)。
func markHitsPlain(seg string, words []string, after string) string {
	low := strings.ToLower(seg)
	if len(low) != len(seg) {
		return seg
	}
	hit := make([]bool, len(seg))
	for _, w := range words {
		for from := 0; ; {
			i := strings.Index(low[from:], w)
			if i < 0 {
				break
			}
			for k := from + i; k < from+i+len(w); k++ {
				hit[k] = true
			}
			from += i + len(w)
		}
	}
	var b strings.Builder
	for i := 0; i < len(seg); {
		j := i
		for j < len(seg) && hit[j] == hit[i] {
			j++
		}
		if hit[i] {
			b.WriteString(sgrHit + seg[i:j] + sgrFgReset + after)
		} else {
			b.WriteString(seg[i:j])
		}
		i = j
	}
	return b.String()
}

// laneTotal は絞る前のレーン s の枚数 (絞っている間、レーンの見出しに (一致/全部) と出す)。
func (m *Model) laneTotal(s card.State) int {
	n := 0
	for c := range m.visible() {
		if c.State == s {
			n++
		}
	}
	return n
}

// searchWords はカードの題名で浮かせる語 (絞っていなければ無し)。
func (m *Model) searchWords() []string {
	if !m.search.active {
		return nil
	}
	return strings.Fields(strings.ToLower(m.search.query()))
}
