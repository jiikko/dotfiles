package lanmgr

// 設定の画面 (`,` で開閉)。何の IP をどう出すかを、環境の推測に頼らず設定で決める (issue 747 の「IP の出し方を設定で決める」)。

import (
	"github.com/jiikko/dotfiles/src/tuikit/listnav"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"

	"mac-lan-manager/netinfo"
	"mac-lan-manager/prefs"
)

// otherKinds は「その他の IP」に含めるかを切り替える種類 (表示の順)。
var otherKinds = []netinfo.Kind{netinfo.KindWired, netinfo.KindLinkLocal, netinfo.KindVirtual, netinfo.KindVPN, netinfo.KindUnknown}

const (
	setMain = iota
	setLan
	setOthers
	setMode
	setBand
	setIF
	setKind0 // ここから otherKinds の順
)

func (m *Model) setCount() int { return setKind0 + len(otherKinds) }

// lanIFs は I/F で決めるときの候補 (ルーターの無いサービス)。
func (m *Model) lanIFs() []netinfo.Service {
	var out []netinfo.Service
	for _, s := range m.self.Services {
		if !s.Internet() {
			out = append(out, s)
		}
	}
	return out
}

func (m *Model) savePrefs() {
	if m.o.SavePrefs == nil || m.o.PrefsPath == "" {
		return
	}
	if err := m.o.SavePrefs(m.o.PrefsPath, m.pf); err != nil {
		m.note("設定を保存できない: "+err.Error(), false)
	}
}

func (m *Model) keySettings(key, text string) Action {
	if m.bandEdit { // 入力中は入力欄が先にキーを取る (§7)
		switch key {
		case "esc":
			m.bandEdit = false
		case "enter":
			v := strings.TrimSpace(m.band.String())
			if (prefs.Prefs{Band: v}).BandPrefix() == "" {
				m.note("帯は 192.168.100.0/24 の形 (/24) で書く", false)
				return None
			}
			m.bandEdit = false
			m.pf.Band = v
			m.savePrefs()
		default:
			m.band.Key(key, text)
		}
		return None
	}
	// space は移動の語彙では半ページだが、この画面では入 / 切。移動より先に捌く (一覧の space と同じ)
	if key != "space" && move(&m.setC, m.setCount(), key, m.setCount()) {
		return None
	}
	switch key {
	case "q", "esc", ",":
		m.sc = scList
	case "space":
		switch {
		case m.setC == setMain:
			m.pf.ShowMain = !m.pf.ShowMain
		case m.setC == setLan:
			m.pf.ShowLan = !m.pf.ShowLan
		case m.setC == setOthers:
			m.pf.ShowOthers = !m.pf.ShowOthers
		case m.setC >= setKind0:
			k := otherKinds[m.setC-setKind0].Key()
			if i := slices.Index(m.pf.Kinds, k); i >= 0 {
				m.pf.Kinds = slices.Delete(m.pf.Kinds, i, i+1)
			} else {
				m.pf.Kinds = append(m.pf.Kinds, k)
			}
		default:
			m.note("この項目は ← → で変える", false)
			return None
		}
		m.savePrefs()
	case "left", "right", "h", "l":
		switch m.setC {
		case setMode:
			m.pf.LanByBand = !m.pf.LanByBand
		case setIF:
			ifs := m.lanIFs()
			if len(ifs) == 0 {
				m.note("ルーターの無い I/F が無い", false)
				return None
			}
			i := slices.IndexFunc(ifs, func(s netinfo.Service) bool { return s.Device == m.pf.LanIF })
			d := 1
			if key == "left" || key == "h" {
				d = len(ifs) - 1
			}
			m.pf.LanIF = ifs[(max(i, 0)+d)%len(ifs)].Device
			if i < 0 {
				m.pf.LanIF = ifs[0].Device
			}
		default:
			m.note("この項目は space で入 / 切", false)
			return None
		}
		m.savePrefs()
	case "enter":
		if m.setC != setBand {
			m.note("この項目は space か ← → で変える", false)
			return None
		}
		m.band.Reset()
		m.band.Insert(m.pf.Band)
		m.bandEdit = true
	}
	return None
}

func onOffPill(on bool) string {
	if on {
		return lipgloss.NewStyle().Foreground(cBg).Background(cTeal).Bold(true).Padding(0, 1).Render("オン")
	}
	return lipgloss.NewStyle().Foreground(cMuted).Background(cTabIdle).Padding(0, 1).Render("オフ")
}

func choice(opts []string, cur int) string {
	parts := make([]string, 0, len(opts))
	for i, o := range opts {
		if i == cur {
			parts = append(parts, lipgloss.NewStyle().Foreground(cBg).Background(cPink).Bold(true).Padding(0, 1).Render(o))
		} else {
			parts = append(parts, sMuted.Padding(0, 1).Render(o))
		}
	}
	return sDim.Render("◂ ") + strings.Join(parts, " ") + sDim.Render(" ▸")
}

func (m *Model) viewSettings(w int) string {
	labels := []string{"メイン IP (インターネット側)", "内部 IP (この Mac 同士の通信)", "その他の IP (+N の札・詳細カード)", "決め方", "帯", "I/F"}
	sections := map[int]string{setMain: "一覧に出す IP", setMode: "内部 IP の決め方", setKind0: "その他の IP に含める種類"}
	explain := map[netinfo.Kind]string{netinfo.KindWired: "ルーターの無い有線の I/F", netinfo.KindLinkLocal: "169.254 (固定していない I/F)",
		netinfo.KindVirtual: "bridge / vmnet (VM・Docker のブリッジ)", netinfo.KindVPN: "utun (VPN・Tailscale)", netinfo.KindUnknown: "設定に出てこない I/F"}
	inner := max(w-7, 10) // 板の中身の幅 (card の Width(w-1-2) と左右の余白 2)
	m.setLabelW = min(40, max(inner/2, 8))
	label := func(s string) string {
		return lipgloss.NewStyle().Width(m.setLabelW).Render(termwidth.Truncate("  "+s, m.setLabelW, "…"))
	}
	rows := make([]string, 0, m.setCount()+8)
	selAt := 0 // 選んでいる項目の rows の中の位置 (窓をここへ合わせる)
	for i := range m.setCount() {
		if sec, ok := sections[i]; ok {
			if i > 0 {
				rows = append(rows, "")
			}
			rows = append(rows, sAccent.Render(sec))
		}
		var name string
		if i < setKind0 {
			name = labels[i]
		} else {
			name = otherKinds[i-setKind0].String()
		}
		var val string
		dim := ""
		switch i {
		case setMain:
			val = onOffPill(m.pf.ShowMain)
		case setLan:
			val = onOffPill(m.pf.ShowLan)
		case setOthers:
			val = onOffPill(m.pf.ShowOthers)
		case setMode:
			cur := 1
			if m.pf.LanByBand {
				cur = 0
			}
			val = choice([]string{"帯で決める", "I/F で決める"}, cur)
		case setBand:
			if !m.pf.LanByBand {
				dim = "(この決め方では使わない)"
			}
			if m.bandEdit {
				txt, col := m.band.Window(20)
				val = lipgloss.NewStyle().Background(cCode).Padding(0, 1).Render(termwidth.FillRight(txt, 21)) + sMuted.Render("   enter で確定 / esc でやめる")
				m.bandCol, m.bandRow = col, len(rows) // カーソルの位置は View が板の位置から決める (窓で切った後の行に直す)
			} else {
				val = sText.Render(m.pf.Band) + sMuted.Render("   enter で編集")
			}
		case setIF:
			if m.pf.LanByBand {
				dim = "(この決め方では使わない)"
			}
			ifs := m.lanIFs()
			names := make([]string, 0, len(ifs))
			cur := 0
			for j, s := range ifs {
				names = append(names, s.Device+" "+s.Name)
				if s.Device == m.pf.LanIF {
					cur = j
				}
			}
			if len(names) > 0 {
				val = choice(names, cur)
			} else {
				val = sMuted.Render("ルーターの無い I/F が無い")
			}
		default:
			k := otherKinds[i-setKind0]
			if !m.pf.ShowOthers {
				dim = "(その他の IP がオフ)"
			}
			val = onOffPill(slices.Contains(m.pf.Kinds, k.Key())) + sMuted.Render("   "+explain[k])
		}
		row := label(name) + val
		if dim != "" {
			row = sMuted.Render(label(name)) + sMuted.Render(dim)
		}
		if i == m.setC {
			row = selRow(row, w-8)
			selAt = len(rows)
		}
		rows = append(rows, row)
	}
	sl := m.selfLan()
	rows = append(rows, "", sDim.Render("今の設定でのこの Mac の内部 IP: ")+sText.Bold(true).Render(or(sl.IP, "-"))+sMuted.Render("  ("+sl.Note+")"))
	rows = append(rows, sMuted.Render("保存先 "+or(m.o.PrefsPath, "(保存しない)")))
	// どの行も板の中の幅で切る (折り返すと行が増え、帯の入力欄のキャレットが別の行に出た。監査の指摘)
	for i, r := range rows {
		rows[i] = termwidth.Truncate(r, inner, "…")
	}
	// 窓: 画面に入る行だけを、選んでいる項目を含むように描く (24 行以下で「その他の IP に含める種類」が画面の外のまま
	// カーソルだけ進んだ)。見出し 3・板の枠と余白 4・板の見出し 3 を引いた行数
	avail := max(m.bodyHeight()-3-4-3, 3)
	m.setOff = 0
	if len(rows) > avail {
		m.setOff = listnav.WindowOffset(m.setOffPrev, selAt, len(rows), avail)
		m.setOffPrev = m.setOff
		rows = rows[m.setOff : m.setOff+avail]
	}
	head := sAccent.Render("設定") + "\n" + gradRule(w-7, cPink, cPurple, cTeal, lipgloss.Color(surfPanel))
	return shadow(card(head+"\n\n"+strings.Join(rows, "\n"), w-1, surfPanel, cPink, cPurple, cTeal))
}

// selRow は選んだ行 (左端にオレンジの棒、行は選択の地)。
func selRow(row string, width int) string {
	return lipgloss.NewStyle().Background(cAccent).Render(" ") + lipgloss.NewStyle().Background(cSel).Width(width).Render(row)
}
