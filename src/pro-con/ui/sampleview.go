package ui

// 回答フォームの選択肢から v で開く見本の板 (issue 495。見た目は 2026-09-28 に人が「差分の板 D と同じ全幅の板」を選んだ)。
// 選択肢の sample (card.Option.Sample) と同じ名前の、一番新しい添付を映す。文字の添付は詳細と同じ attachText (色だけ残し、端末を
// 操作するエスケープは落とす) で読み、画像は o と同じく Preview に渡す。回答フォーム (modeForm) は下に開いたまま残るので、閉じると
// フォームへ戻る。n / p で同じ問いの別の案の見本へ移ると、フォームのカーソルもその案へ付いて行く (見比べてそのまま space で選べる)。
// 🚨 見本は開くだけで実行しない。PG が書いたコマンドをこの画面から走らせると、PG の permission の外で人の権限のコマンドを走らせる口になる
// (issue 495 の「考えること」)。その場で実行する形を足すなら、全文を見せて確かめる・PG の worktree の中・時間の上限を先に決める

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/jiikko/dotfiles/src/tuikit/layout"
	"github.com/jiikko/dotfiles/src/tuikit/listnav"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"

	"pro-con/card"
)

// sampleHScroll は h / l で横にずらす桁数 (見本は幅 160 のように端末より広く描かれることがある)。
const sampleHScroll = 8

// sampleView は見本の板の状態。
type sampleView struct {
	open   bool
	q, opt int      // 映している案 (回答フォームの問いと選択肢の添字)
	name   string   // 見本の名前 (Option.Sample)
	lines  []string // 映す行 (見つからない・開けないときは知らせの 1 行)
	wide   int      // lines の一番広い行の桁数 (横にずらせる上限)
	left   int      // 横にずらした桁
	pager  listnav.Pager
}

// latestAttachment は c の添付のうち、名前が name の一番新しいもの (PG が見本を付け直したら新しい方を映す)。
// 時刻が同じなら後ろ (後から記録に載った方) を採る。
func latestAttachment(c card.Card, name string) (card.Attachment, bool) {
	var best card.Attachment
	found := false
	for _, a := range c.Attachments {
		if a.Name == name && (!found || !a.At.Before(best.At)) {
			best, found = a, true
		}
	}
	return best, found
}

// formCard は回答フォームを開いているカード (今の記録から引く。フォームを開いた後に PG が付けた見本も開ける)。
func (m *Model) formCard() (card.Card, bool) {
	for _, c := range m.snap.Cards {
		if c.ID == m.form.cardID {
			return c, true
		}
	}
	return card.Card{}, false
}

// openSample は回答フォームの問い q の選択肢 opt の見本を開く。画像は Preview に渡す: フォームから開いたときは板を開かず、
// 板を開いたまま n / p で移ったときは板にその旨を出す (板が前の案の見本を映したまま、フォームのカーソルだけ動く形にしない)。
func (m *Model) openSample(q, opt int) tea.Cmd {
	f := &m.form
	if q >= len(f.qs) || opt >= len(f.qs[q].Options) {
		m.refuse("見本は選択肢の行で開く")
		return nil
	}
	name := f.qs[q].Options[opt].Sample
	if name == "" {
		m.refuse("この案に見本は無い")
		return nil
	}
	c, found := m.formCard()
	a, ok := latestAttachment(c, name)
	var lines []string
	var cmd tea.Cmd
	switch {
	case !found:
		lines = []string{sgrYellow + "  " + m.form.cardID + " はもう記録に無い (取り下げられたか、書庫へ移った)。見本は開けない" + sgrReset}
	case !ok:
		lines = []string{sgrYellow + "  見本「" + name + "」はまだ添付されていない (PG が card attach する前か、名前が違う)" + sgrReset}
	case a.Kind == card.AttachImage:
		open := m.openFiles
		paths := []string{a.Path}
		cmd = m.child(func() tea.Msg { return openedMsg{n: 1, err: open(paths)} })
		if !m.sample.open {
			return cmd
		}
		lines = []string{sgrDim + "  画像の見本「" + name + "」は Preview で開いた" + sgrReset}
	case a.Kind != card.AttachText:
		lines = []string{sgrDim + "  この種類の添付はここでは映せない: " + a.Path + " (pro-con card show のパスから開く)" + sgrReset}
	default:
		lines = m.attachText(a.Path)
	}
	wide := 0
	for _, l := range lines {
		wide = max(wide, termwidth.Of(l))
	}
	m.sample = sampleView{open: true, q: q, opt: opt, name: name, lines: lines, wide: wide}
	return cmd
}

// stepSample は同じ問いの d 向き (1 = 次 / -1 = 前) にある、見本を持つ案へ移る。フォームのカーソルもその案へ動かす。
func (m *Model) stepSample(d int) tea.Cmd {
	opts := m.form.qs[m.sample.q].Options
	for j := m.sample.opt + d; j >= 0 && j < len(opts); j += d {
		if opts[j].Sample == "" {
			continue
		}
		for i, r := range m.form.rows() {
			if r.q == m.sample.q && r.opt == j {
				m.form.cur = i
			}
		}
		return m.openSample(m.sample.q, j)
	}
	m.refuse("この向きに見本のある案はもう無い")
	return nil
}

// handleSampleKey は見本の板を開いている間のキー。
func (m *Model) handleSampleKey(k string) tea.Cmd {
	s := &m.sample
	switch k {
	case "esc", "q", "v":
		m.sample = sampleView{}
		return nil
	case "ctrl+c":
		m.sample = sampleView{}
		return m.requestQuit()
	case "n":
		return m.stepSample(1)
	case "p":
		return m.stepSample(-1)
	case "l", "right": // 上限 (右端でちょうど本文の幅) は描くときに overlaySample が 1 か所で当てる (端末の幅が変わっても同じ式で直る)
		s.left += sampleHScroll
		return nil
	case "h", "left":
		s.left = max(s.left-sampleHScroll, 0)
		return nil
	}
	if mo := listnav.MotionOf(k); mo != listnav.None {
		n := m.sampleBodyRows()
		s.pager.Move(mo, len(s.lines), n, glideFrames)
		s.pager.Clamp(len(s.lines), n)
		return m.startFrames()
	}
	return nil
}

func (m *Model) sampleBodyRows() int  { return max(m.drawerRegionRows()-2, 1) }
func (m *Model) sampleBodyWidth() int { return max(m.width-1-layout.ScrollbarWidth, 1) }

// overlaySample は領域 (ヘッダと下端の群のあいだ) を見本の板で置き換える (形は overlayDiff と同じ)。
func (m *Model) overlaySample(region []string) []string {
	s := &m.sample
	if !s.open {
		return region
	}
	w := m.width
	n, bw := m.sampleBodyRows(), m.sampleBodyWidth()
	s.left = min(s.left, max(s.wide-bw, 0)) // 横ずれの上限。見出しの「ずらしている」より前に当てる (広げた直後に古い桁数を出さない)
	label := m.form.qs[s.q].Options[s.opt].Label
	title := fmt.Sprintf(" %s%s%s の見本  %s%s%s  %s(%s)%s", sgrBold, m.form.cardID, sgrReset, fg(231), label, sgrReset, sgrDim, s.name, sgrReset)
	if s.left > 0 {
		title += fmt.Sprintf("  %s← %d 桁ずらしている%s", sgrYellow, s.left, sgrReset)
	}
	out := []string{fit(title, w), fg(240) + strings.Repeat("─", w) + sgrReset}
	total := len(s.lines)
	s.pager.Clamp(total, n)
	off := s.pager.DrawOffset(total, n)
	win := make([]string, n)
	for i := range win {
		if off+i < total {
			win[i] = " " + termwidth.Truncate(termwidth.DropColumns(s.lines[off+i], s.left), bw, "…") + sgrReset
		}
	}
	return layout.PadTo(append(out, layout.Scrollbar(win, w, total, off, true)...), len(region))[:len(region)]
}

// sampleHints は見本の板の案内。
func sampleHints() []string {
	return []string{"j / k 行", "space / b 半ページ", "h / l 横", "n / p 別の案の見本", "esc / v フォームへ戻る"}
}
