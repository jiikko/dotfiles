package lanmgr

import (
	"fmt"
	"github.com/jiikko/dotfiles/src/tuikit/listnav"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/jiikko/dotfiles/src/termsafe"
	"github.com/jiikko/dotfiles/src/tuikit/confirm"
	"github.com/jiikko/dotfiles/src/tuikit/spinner"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"

	"mac-lan-manager/netinfo"
)

// 画面の外側の余白 (端末の縁とタイルの間)。セルは横約 8px・縦約 17px (Hack 13pt) なので、約 20px = 横 3 桁・縦 1 行 (issue 747)。
const padX, padY = 3, 1

// spinStep は探索中の回る印の 1 コマ (glogx の spinnerInterval と同じ 80ms)。
const spinStep = 80 * time.Millisecond

// View は画面の行 (w × h。最下行はキーの案内)。重ねる板 (確認・コピー・別名・キー一覧) もここで重ねる。
func (m *Model) View() []string {
	if m.w < minWidth {
		return m.viewTooNarrow()
	}
	// 幅を 40 桁に固定しない: max(…, 40) にしていたころは、それより狭い端末で枠と列が折り返し、最下行の案内 (抜け方) まで画面の外へ
	// 出た (fable の敵対レビュー)。狭ければ列を落とし、どの行も幅で切る (下の安全網)
	w := m.w - 2*padX
	m.caretFY, m.caretMY = -1, -1
	var body string
	if m.sc == scSettings {
		body = lipgloss.JoinVertical(lipgloss.Left, m.header(w), gradRule(w, cAccent, cPink, cPurple, cTeal, cBg), "", m.viewSettings(w))
		if m.bandEdit {
			m.caretMX = padX + 1 + 2 + 40 + 1 + m.bandCol
			m.caretMY = padY + 3 + 1 + 1 + 3 + m.bandRow
		}
	} else {
		body = m.viewList(w)
	}
	foot := m.footer()
	h := max(m.h, 10)
	inner := m.bodyHeight()
	// 安全網: どの行も幅で切ってから枠に入れる (lipgloss は幅を超えた行を折り返し、行が増えて下のキーの案内が押し出される)
	bl := strings.Split(body, "\n")
	for i, l := range bl {
		bl[i] = termwidth.Clip(l, w)
	}
	body = lipgloss.NewStyle().Height(inner).MaxHeight(inner).Render(strings.Join(bl, "\n"))
	base := lipgloss.NewStyle().Width(m.w).Height(h).MaxHeight(h).Padding(padY, padX).Render(lipgloss.JoinVertical(lipgloss.Left, body, "", foot))
	layers := []*lipgloss.Layer{lipgloss.NewLayer(base)}
	if box, fieldRow, fieldX := m.overlayBox(); box != "" {
		box = lipgloss.NewStyle().MaxHeight(h).MaxWidth(m.w).Render(box) // 板が画面より高い・広いと View が画面からはみ出した (? の板は 22 行・66 桁)
		bw, bh := lipgloss.Width(box), lipgloss.Height(box)
		x, y := max((m.w-bw)/2, 0), max((h-bh)/2, 0)
		layers = append(layers, lipgloss.NewLayer(box).X(x).Y(y).Z(1))
		if fieldRow >= 0 {
			m.caretMX, m.caretMY = x+fieldX, y+fieldRow
		}
	}
	return strings.Split(lipgloss.NewCompositor(layers...).Render(), "\n")
}

func (m *Model) footer() string {
	pairs := [][2]string{{"j/k", "移動"}, {"h/l", "タブ"}, {"space", "台帳"}, {"c", "この Mac の IP"}, {",", "設定"}, {"?", "全部のキー"}, {"q", "閉じる"}}
	switch {
	case m.sc == scSettings:
		pairs = [][2]string{{"j/k", "項目"}, {"space", "入/切"}, {"←/→", "選ぶ"}, {"enter", "編集"}, {"q/esc/,", "閉じる"}}
	case m.filtering:
		pairs = [][2]string{{"enter", "確定"}, {"esc", "やめる"}}
	case m.keysInFooter: // 詳細カードを出す高さが無い: カーソルの Mac への動作のキーをここに出す
		pairs = [][2]string{{"j/k", "移動"}}
		if r := m.current(); !r.self && r.connectIP() != "" {
			pairs = append(pairs, [2]string{"s", "繋ぐ"}, [2]string{"Y", "コピー"})
			if r.smb {
				pairs = append(pairs, [2]string{"y", "smb"})
			}
			if r.vnc {
				pairs = append(pairs, [2]string{"o", "画面共有"})
			}
		}
		pairs = append(pairs, [2]string{"q", "閉じる"})
	}
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, lipgloss.NewStyle().Foreground(cPink).Bold(true).Render(p[0])+" "+sDim.Render(p[1]))
	}
	// 幅が足りなければ後ろから落とすが、抜ける手段 (最後の q) は必ず残す (ガイド §5)
	sep := sMuted.Render(" • ")
	for len(parts) > 1 && lipgloss.Width(strings.Join(parts, sep)) > m.w-2*padX {
		parts = slices.Delete(parts, len(parts)-2, len(parts)-1)
	}
	return strings.Join(parts, sep)
}

func (m *Model) header(w int) string {
	left := logo()
	switch {
	case m.Busy():
		what := fmt.Sprintf(" Bonjour で探しています (%d 台)", len(m.peers))
		if !m.netReady {
			what = " この Mac のネットワークを読んでいます"
		}
		left += "   " + lipgloss.NewStyle().Foreground(cPink).Render(spinner.At(m.now, spinStep)) + sDim.Render(what)
	case m.scanErr != nil:
		left += "   " + sWarn.Render("✗ Bonjour で探せない") + sDim.Render(fmt.Sprintf(" (%d 台。dump で理由を見る)", len(m.peers)))
	default:
		left += "   " + sOK.Render("✓") + sDim.Render(fmt.Sprintf(" %d 台見つかった", len(m.peers)))
	}
	ips := m.selfIPs()
	right := sKeycap.Render("c") + sDim.Render(" この Mac  メイン ") + sText.Render(or(ips[1], "-")) + sDim.Render("   内部 ") + sText.Render(or(ips[0], "-"))
	if _, note := m.selfLan(); strings.Contains(note, "未固定") {
		right += " " + pillWrn.Render("未固定")
	}
	// 狭いときは段階的に詰める (折り返させない。折り返すと行が増えて下のキーの案内が押し出される)。走査の失敗は
	// この Mac の IP より大事なので、右を先に詰め、左は消さずに短い形を残す (消すと「0 台」と見分けが付かない)
	fit := func() bool { return lipgloss.Width(left)+lipgloss.Width(right)+2 <= w }
	if !fit() && m.scanErr != nil {
		left = logo() + "   " + sWarn.Render("✗ Bonjour で探せない")
		right = sKeycap.Render("c") + sDim.Render(" 内部 ") + sText.Render(or(ips[0], "-"))
	}
	if !fit() {
		left = logo()
		if m.scanErr != nil {
			left += " " + sWarn.Render("✗ 探せない")
		}
	}
	if !fit() {
		right = sKeycap.Render("c") + sDim.Render(" メイン ") + sText.Render(or(ips[1], "-")) + sDim.Render("  内部 ") + sText.Render(or(ips[0], "-"))
	}
	if !fit() {
		right = sKeycap.Render("c") + sDim.Render(" 内部 ") + sText.Render(or(ips[0], "-"))
	}
	return left + strings.Repeat(" ", max(w-lipgloss.Width(left)-lipgloss.Width(right), 2)) + right
}

func (m *Model) viewList(w int) string {
	r := m.current()
	wide := w >= 160
	listW := w
	if wide {
		listW = w * 58 / 100
	}
	head := lipgloss.JoinVertical(lipgloss.Left, m.header(w), gradRule(w, cAccent, cPink, cPurple, cTeal, cBg), "")
	tabs := m.tabs(listW - 1)
	detailW := w
	if wide {
		detailW = w - listW - 2
	}
	detail := m.detail(r, detailW)
	// 一覧に回せる行数: 画面の高さから、見出し・タブ・枠・表の見出しと (縦に積むときは) 詳細カードを先に引く。詳細カードに動作の
	// キー (s 繋ぐ・Y コピー・o 開く) があるので、それを押し出さないよう先に取る。全部を描いて下で切っていたころは、狭い画面で
	// 詳細カードもカーソル行も画面の外に出た (監査の指摘 2026-10-16)
	frame := lipgloss.Height(shadow(card("", listW-1, surfList, tabColors[m.tab], cPurple))) - 1
	rows := m.bodyHeight() - lipgloss.Height(head) - lipgloss.Height(tabs) - frame - 2 // 2 = 表の見出しと罫線
	m.keysInFooter = false
	if !wide {
		if rows-lipgloss.Height(detail)-1 < min(3, len(m.visible())) {
			// 詳細カードを積む高さが無い: 動作のキーだけの 1 行に畳む (カードごと下で切れると、s / Y / o の案内が黙って消えた。
			// fable の敵対レビュー: 縦積みで高さ 22 以下)
			detail = m.detailCompact(r)
		}
		if rows-lipgloss.Height(detail)-1 < 1 {
			// 1 行に畳んでも入らない (高さ 13〜17): 動作のキーは最下行の案内へ回す (最下行は必ず画面に残る。fable の敵対レビュー)
			detail, m.keysInFooter = "", true
		} else {
			rows -= lipgloss.Height(detail) + 1
		}
	}
	list := lipgloss.JoinVertical(lipgloss.Left, tabs, shadow(card(m.listTable(listW-7, max(rows, 1)), listW-1, surfList, tabColors[m.tab], cPurple)))
	var main string
	switch {
	case detail == "":
		main = list
	case wide:
		main = lipgloss.JoinHorizontal(lipgloss.Top, list, "  ", lipgloss.NewStyle().MarginTop(1).Render(detail))
	default:
		main = lipgloss.JoinVertical(lipgloss.Left, list, "", detail)
	}
	return lipgloss.JoinVertical(lipgloss.Left, head, main)
}

func (m *Model) tabs(width int) string {
	labels := []string{fmt.Sprintf("管理対象  %d", m.count(true)), fmt.Sprintf("未管理  %d", m.count(false))}
	parts := make([]string, 0, len(labels))
	for i, lb := range labels {
		if i == m.tab {
			parts = append(parts, pillTab(lb, tabColors[i], cBg, true))
		} else {
			parts = append(parts, pillTab(lb, cTabIdle, cDim, false))
		}
	}
	row := strings.Join(parts, " ")
	var search string
	switch {
	case m.filtering:
		txt, col := m.filter.Window(24)
		search = sAccent.Render("/ ") + lipgloss.NewStyle().Background(cCode).Render(termwidth.FillRight(txt, 25))
		m.caretFX = padX + width - 2 - lipgloss.Width(search) + 2 + col
		m.caretFY = padY + 3
	case m.filter.String() != "":
		search = sAccent.Render("/ "+termsafe.PlainLine(m.filter.String())) + sMuted.Render("  esc で解除")
	default:
		search = sMuted.Render("/ で絞り込み")
	}
	return row + strings.Repeat(" ", max(width-lipgloss.Width(row)-lipgloss.Width(search)-2, 1)) + search
}

// col は一覧の 1 列 (設定で出し入れする)。
type col struct {
	title string
	width int // 0 なら残りの幅、fitWidth なら中身の最長に合わせる
	cell  func(r row) string
	style func(s lipgloss.Style, r row) lipgloss.Style
}

// fitWidth は中身の最長に合わせる列の印 (名前の列)。上限は maxFitWidth、幅が足りなければホスト名の列に minFlexWidth を残すまで縮める。
const (
	fitWidth     = -1
	maxFitWidth  = 34 // 中身 32 桁 + 左右の余白 (ユーザー要望 2026-10-15: 名前の列が短すぎる。固定の 12 では「kojiのMac mini」も切れた)
	minFitWidth  = 12
	minFlexWidth = 12
)

func pill(on bool, s string, st lipgloss.Style) string {
	if on {
		return st.Render(s)
	}
	return pillOff.Render(s)
}

func (m *Model) columns() []col {
	cols := []col{
		{" ", 1, func(row) string { return " " }, nil},
		{"別名", fitWidth, func(r row) string { return or(r.alias, or(r.instance, "-")) }, func(s lipgloss.Style, r row) lipgloss.Style {
			s = s.Bold(true)
			switch {
			case r.self:
				s = s.Foreground(cInfo)
			case r.alias == "":
				s = s.Bold(false).Foreground(cDim)
			}
			return s
		}},
	}
	if m.pf.ShowMain {
		cols = append(cols, col{"メイン IP", 17, func(r row) string { return or(r.mainIP, "-") }, nil})
	}
	if m.pf.ShowLan {
		cols = append(cols, col{"内部 IP", 18, func(r row) string {
			if strings.HasPrefix(r.lanIP, "169.254.") {
				return sWarn.Render(r.lanIP) // 自動割り当て (起動のたびに変わりうる。未固定)
			}
			return or(r.lanIP, "-")
		}, nil})
	}
	if m.pf.ShowOthers {
		cols = append(cols, col{"他", 6, func(r row) string {
			if len(r.others) > 0 {
				return lipgloss.NewStyle().Foreground(cPurple).Bold(true).Render(fmt.Sprintf("+%d", len(r.others)))
			}
			return sMuted.Render("-")
		}, nil})
	}
	cols = append(cols,
		col{"接続できる", 23, func(r row) string {
			return pill(r.ssh, "ssh", pillSSH) + pill(r.vnc, "画面共有", pillVNC) + pill(r.smb, "smb", pillSMB)
		}, nil},
		col{"user", 7, func(r row) string { return or(r.user, "?") }, nil},
		col{"ホスト名", 0, func(r row) string { return r.host }, func(s lipgloss.Style, r row) lipgloss.Style {
			if !r.seen && !r.self {
				return s.Foreground(cMuted) // 台帳にあるが今は見えていない
			}
			return s.Foreground(cDim)
		}},
	)
	return cols
}

// listTable は列ごとに幅を決め、セルを lipgloss のスタイルで描いて横に並べる (折り返さず … で切る)。
// detailCompact は高さが足りないときの詳細カードの代わり (動作のキーだけの 1 行)。
func (m *Model) detailCompact(r row) string {
	if r.self {
		return sDim.Render("この Mac  ") + keycap("c", "IP をコピー") + "   " + sMuted.Render("(端末を高くすると詳細)")
	}
	parts := []string{sText.Render(or(r.alias, or(r.instance, r.host)))}
	if r.connectIP() != "" {
		parts = append(parts, keycap("s", "繋ぐ"), keycap("Y", "コピー"))
		if r.smb {
			parts = append(parts, keycap("y", "smb"))
		}
		if r.vnc {
			parts = append(parts, keycap("o", "画面共有"))
		}
	}
	parts = append(parts, sMuted.Render("(端末を高くすると詳細)"))
	return strings.Join(parts, "   ")
}

// minWidth はこの画面を組む最小の幅。12 桁以下では枠を組む計算が負になって落ちた (strings.Repeat)。20 桁未満では何も読めないので、
// 抜け方だけの画面にする。
const minWidth = 20

// viewTooNarrow は幅が足りないときの画面 (抜け方だけは必ず見せる)。
func (m *Model) viewTooNarrow() []string {
	lines := make([]string, max(m.h, 1))
	lines[0] = termwidth.Clip("幅が足りない", max(m.w, 1))
	lines[len(lines)-1] = "q"
	if m.w >= termwidth.Of("q 閉じる") {
		lines[len(lines)-1] = "q 閉じる"
	}
	return lines
}

// bodyHeight は枠の内側の行数 (View の inner と同じ式)。
func (m *Model) bodyHeight() int { return max(max(m.h, 10)-2*padY-2, 1) }

// listTable は一覧の表。maxRows 行より多ければ、カーソルを含む窓だけを描く (窓の位置はタブごとに覚える)。
func (m *Model) listTable(width, maxRows int) string {
	vis := m.visible()
	cur := min(m.cur[m.tab], len(vis)-1)
	cols := m.columns()
	used, fitAt := 0, -1
	for i, c := range cols {
		if c.width == fitWidth {
			fitAt = i
			fit := termwidth.Of(c.title)
			for _, r := range vis {
				fit = max(fit, termwidth.Of(c.cell(r)))
			}
			cols[i].width = min(max(fit+2, minFitWidth), maxFitWidth)
		}
		used += cols[i].width
	}
	// 足りなければ名前の列を縮めて、ホスト名の列に minFlexWidth を残す。縮めても残らなければホスト名の列を出さず (ホスト名は
	// 詳細カードの見出しにある)、空いた幅を名前の列へ返す (縮めたまま返さないと、ホスト名も名前も両方切れる)
	desired := 0
	if fitAt >= 0 {
		desired = cols[fitAt].width
		if width-used < minFlexWidth {
			shrink := min(desired-minFitWidth, minFlexWidth-(width-used))
			cols[fitAt].width -= shrink
			used -= shrink
		}
	}
	if width-used < minFlexWidth {
		if fitAt >= 0 {
			back := min(desired-cols[fitAt].width, max(width-used, 0))
			cols[fitAt].width += back
			used += back
		}
		cols = cols[:len(cols)-1]
	}
	render := func(i int, r *row) string {
		out := make([]string, 0, len(cols))
		for ci, c := range cols {
			cw := c.width
			if cw == 0 {
				cw = max(width-used, 10)
			}
			s := lipgloss.NewStyle().Width(cw).MaxWidth(cw).Padding(0, 1).Foreground(cText)
			if ci == 0 {
				s = s.Padding(0) // 印の列 (幅 1) は余白を持たない (持たせると見出しの行で 2 行に折り返す)
			}
			txt := c.title
			if r == nil {
				s = s.Foreground(cDim)
			} else {
				txt = c.cell(*r) // 外から来た名前は rows() の入口で無害化済み (ここで色を付けたセルと区別しない)
				if c.style != nil {
					s = c.style(s, *r)
				}
				if i == cur {
					if ci == 0 {
						s = s.Background(cAccent)
					} else {
						s = s.Background(cSel)
					}
				}
			}
			out = append(out, s.Render(termwidth.Truncate(txt, cw-s.GetHorizontalPadding(), "…")))
		}
		return lipgloss.JoinHorizontal(lipgloss.Top, out...)
	}
	lines := []string{render(-1, nil), sMuted.Render(strings.Repeat("─", width))}
	from, to := 0, len(vis)
	m.listRows = maxRows // 半ページ送りの量 (端末の高さではなく、一覧の窓の行数で送る)
	if len(vis) > maxRows {
		from = listnav.WindowOffset(m.listOff[m.tab], cur, len(vis), maxRows)
		m.listOff[m.tab] = from
		to = from + maxRows
	}
	for i := from; i < to; i++ {
		lines = append(lines, render(i, &vis[i]))
	}
	return strings.Join(lines, "\n")
}

func (m *Model) detail(r row, width int) string {
	user, _ := m.userFor(r)
	name := or(r.alias, or(r.instance, r.host))
	title := sAccent.Render(name)
	if name != r.host && r.host != "" {
		title += sDim.Render("  " + r.host)
	}
	label := func(s string) string { return sDim.Width(10).Render(s) }
	link := func(url string) string {
		return lipgloss.NewStyle().Foreground(cInfo).Underline(true).Hyperlink(url).Render(url)
	}
	ip := r.connectIP()
	lines := []string{title, gradRule(width-7, cTeal, cPurple, cPink, lipgloss.Color(surfDetail)), ""}
	if !r.self {
		if ip != "" {
			lines = append(lines, label("ssh")+sCode.Render(fmt.Sprintf("ssh %s@%s", user, ip))+"  "+keycap("Y", "コピー")+"  "+keycap("s", "繋ぐ"))
		}
		if r.smb && ip != "" {
			lines = append(lines, label("smb")+link("smb://"+ip)+"   "+keycap("y", "コピー"))
		}
		if r.vnc && ip != "" {
			lines = append(lines, label("画面共有")+link("vnc://"+ip)+"   "+keycap("o", "開く"))
		}
	}
	lanState := pillOn.Render("有線・固定")
	switch {
	case r.lanIP == "":
		lanState = sMuted.Render("内部の網に居ない")
	case strings.HasPrefix(r.lanIP, "169.254."):
		lanState = pillWrn.Render("自動割り当て")
	}
	lines = append(lines, "")
	if m.pf.ShowMain {
		lines = append(lines, label("メイン")+sText.Render(or(r.mainIP, "-")))
	}
	if m.pf.ShowLan {
		lines = append(lines, label("内部")+sText.Render(or(r.lanIP, "-"))+"  "+lanState)
	}
	if m.pf.ShowOthers && len(r.others) > 0 {
		lines = append(lines, "", sDim.Render("その他の IP"))
		for _, x := range r.others {
			lines = append(lines, "  "+lipgloss.NewStyle().Width(16).Render(kindPill(x.Kind))+sMuted.Width(11).Render(x.Device)+sText.Render(x.Addr))
		}
	}
	lines = append(lines, "")
	switch {
	case r.self && !r.managed:
		lines = append(lines, sDim.Render("この Mac は台帳に無い  ")+keycap("space", "登録"))
	case r.self:
		lines = append(lines, sOK.Render("✓ 台帳に登録済み"))
	case !r.managed:
		lines = append(lines, sDim.Render("未管理: ")+keycap("space", "台帳に登録")+"   "+sMuted.Render("初期設定は次の段で入る"))
	case !r.seen:
		lines = append(lines, sWarn.Render("今は Bonjour で見えていない")+"   "+keycap("m", "別名")+"   "+keycap("space", "台帳から外す"))
	default:
		lines = append(lines, keycap("m", "別名")+"   "+keycap("space", "台帳から外す"))
	}
	return shadow(card(strings.Join(lines, "\n"), width-1, surfDetail, cTeal, cPurple, cPink))
}

func kindPill(k netinfo.Kind) string {
	switch k {
	case netinfo.KindWired:
		return pillOn.Render(k.String())
	case netinfo.KindLinkLocal:
		return pillWrn.Render(k.String())
	case netinfo.KindVirtual:
		return pillVNC.Render(k.String())
	case netinfo.KindVPN:
		return pillSSH.Render(k.String())
	case netinfo.KindInternet, netinfo.KindUnknown:
	}
	return pillOff.Render(k.String())
}

// overlayBox は重ねる板と、その中の入力欄の行・桁 (入力欄が無ければ fieldRow = -1)。
func (m *Model) overlayBox() (box string, fieldRow, fieldX int) {
	switch m.ov {
	case ovConfirm:
		if m.ask != nil {
			return modal(m.ask.title, m.ask.body, confirm.HintYesNo), -1, 0
		}
	case ovCopyIP:
		ips := m.selfIPs()
		lan, note := m.selfLan()
		_ = lan
		body := make([]string, 0, 2)
		for i, label := range []string{"内部", "メイン"} {
			extra := ""
			if i == 0 && strings.Contains(note, "未固定") {
				extra = pillWrn.Render("未固定")
			}
			row := sKeycap.Render(strconv.Itoa(i+1)) + "  " + sDim.Width(8).Render(label) + sText.Bold(true).Width(18).Render(or(ips[i], "-")) + extra
			if i == m.copyC {
				row = lipgloss.NewStyle().Background(cSel).Width(44).Render(row)
			}
			body = append(body, row)
		}
		return modal("この Mac の IP をコピー", body, "1 / 2 すぐコピー   j/k + enter 選んでコピー   esc やめる"), -1, 0
	case ovAlias:
		txt, col := m.alias.Window(24)
		prefix := sDim.Render("別名  ")
		field := lipgloss.NewStyle().Background(cCode).Render(termwidth.FillRight(txt, 25))
		// 板の中の位置: 上の辺 1 + 余白 1 + 題 1 + 空行 1 = 4 行目、左の辺 1 + 余白 2 + 前置き
		return modal("別名を付ける", []string{prefix + field}, "enter 決める   esc やめる"), 4, 3 + lipgloss.Width(prefix) + col
	case ovHelp:
		keys := [][2]string{{"j / k / g / G / ctrl-d / ctrl-u", "移動"}, {"h / l / tab", "管理対象 / 未管理のタブ"}, {"/", "絞り込み"},
			{"s", "ssh で繋ぐ (端末を渡す)"}, {"Y", "ssh のコマンドをコピー"}, {"y", "smb の接続先をコピー"}, {"o", "画面共有を開く"},
			{"c", "この Mac の IP をコピー"}, {"space", "台帳に登録 / 台帳から外す"}, {"m", "別名を付ける"}, {",", "設定"},
			{"r", "読み直す"}, {"q / esc", "閉じる"}}
		body := make([]string, 0, len(keys))
		for _, k := range keys {
			body = append(body, lipgloss.NewStyle().Foreground(cPink).Bold(true).Width(34).Render(k[0])+sText.Render(k[1]))
		}
		return modal("キーの一覧", body, "どのキーでも閉じる"), -1, 0
	case ovNone:
	}
	return "", -1, 0
}
