package lanmgr

import (
	"fmt"
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
	w := max(m.w-2*padX, 40)
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
	inner := max(h-2*padY-2, 1)
	// 安全網: どの行も幅で切ってから枠に入れる (lipgloss は幅を超えた行を折り返し、行が増えて下のキーの案内が押し出される)
	bl := strings.Split(body, "\n")
	for i, l := range bl {
		bl[i] = termwidth.Clip(l, w)
	}
	body = lipgloss.NewStyle().Height(inner).MaxHeight(inner).Render(strings.Join(bl, "\n"))
	base := lipgloss.NewStyle().Width(m.w).Height(h).MaxHeight(h).Padding(padY, padX).Render(lipgloss.JoinVertical(lipgloss.Left, body, "", foot))
	layers := []*lipgloss.Layer{lipgloss.NewLayer(base)}
	if box, fieldRow, fieldX := m.overlayBox(); box != "" {
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
	pairs := [][2]string{{"j/k", "移動"}, {"h/l", "タブ"}, {"space", "台帳"}, {"c", "この Mac の IP"}, {",", "設定"}, {"?", "全部のキー"}, {"q", "終了"}}
	switch {
	case m.sc == scSettings:
		pairs = [][2]string{{"j/k", "項目"}, {"space", "入/切"}, {"←/→", "選ぶ"}, {"enter", "編集"}, {"q/esc/,", "閉じる"}}
	case m.filtering:
		pairs = [][2]string{{"enter", "確定"}, {"esc", "やめる"}}
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
	if m.Busy() {
		left += "   " + lipgloss.NewStyle().Foreground(cPink).Render(spinner.At(m.now, spinStep)) + sDim.Render(fmt.Sprintf(" Bonjour で探しています (%d 台)", len(m.peers)))
	} else {
		left += "   " + sOK.Render("✓") + sDim.Render(fmt.Sprintf(" %d 台見つかった", len(m.peers)))
	}
	ips := m.selfIPs()
	right := sKeycap.Render("c") + sDim.Render(" この Mac  メイン ") + sText.Render(or(ips[1], "-")) + sDim.Render("   内部 ") + sText.Render(or(ips[0], "-"))
	if _, note := m.selfLan(); strings.Contains(note, "未固定") {
		right += " " + pillWrn.Render("未固定")
	}
	// 狭いときは段階的に詰める (折り返させない。折り返すと行が増えて下のキーの案内が押し出される)
	fit := func() bool { return lipgloss.Width(left)+lipgloss.Width(right)+2 <= w }
	if !fit() {
		left = logo()
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
	list := lipgloss.JoinVertical(lipgloss.Left, m.tabs(listW-1), shadow(card(m.listTable(listW-7), listW-1, surfList, tabColors[m.tab], cPurple)))
	var main string
	if wide {
		main = lipgloss.JoinHorizontal(lipgloss.Top, list, "  ", lipgloss.NewStyle().MarginTop(1).Render(m.detail(r, w-listW-2)))
	} else {
		main = lipgloss.JoinVertical(lipgloss.Left, list, "", m.detail(r, w))
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.header(w), gradRule(w, cAccent, cPink, cPurple, cTeal, cBg), "", main)
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
	width int // 0 なら残りの幅
	cell  func(r row) string
	style func(s lipgloss.Style, r row) lipgloss.Style
}

func pill(on bool, s string, st lipgloss.Style) string {
	if on {
		return st.Render(s)
	}
	return pillOff.Render(s)
}

func (m *Model) columns() []col {
	cols := []col{
		{" ", 1, func(row) string { return " " }, nil},
		{"別名", 12, func(r row) string { return or(r.alias, or(r.instance, "-")) }, func(s lipgloss.Style, r row) lipgloss.Style {
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
func (m *Model) listTable(width int) string {
	vis := m.visible()
	cur := min(m.cur[m.tab], len(vis)-1)
	cols := m.columns()
	used := 0
	for _, c := range cols {
		used += c.width
	}
	if width-used < 12 { // ホスト名の列に回す幅が無ければ出さない (ホスト名は詳細カードの見出しにある)
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
	for i := range vis {
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
