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
	"github.com/jiikko/dotfiles/src/tuikit/hintfit"
	"github.com/jiikko/dotfiles/src/tuikit/layout"
	"github.com/jiikko/dotfiles/src/tuikit/spinner"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"

	"mac-lan-manager/netinfo"
)

// 画面の外側の余白 (端末の縁とタイルの間)。セルは横約 8px・縦約 17px (Hack 13pt) なので、約 20px = 横 3 桁・縦 1 行。
const padX, padY = 3, 1

// spinStep は探索中の回る印の 1 コマ (glogx の spinnerInterval と同じ 80ms)。
const spinStep = 80 * time.Millisecond

// View は画面の行 (w × h。最下行はキーの案内)。重ねる板 (確認・コピー・別名・キー一覧) もここで重ねる。
func (m *Model) View() []string {
	if m.w < minWidth {
		return m.viewTooSmall("幅が足りない")
	}
	// 幅を 40 桁に固定しない: max(…, 40) にしていたころは、それより狭い端末で枠と列が折り返し、最下行の案内 (抜け方) まで画面の外へ
	// 出た。狭ければ列を落とし、どの行も幅で切る (下の安全網)
	w := m.w - 2*padX
	m.caretFY, m.caretMY = -1, -1
	m.small = false
	var body, tile, under string // under = 初期設定の下に残す詳細のパネル (padX, m.panelTop)
	tileX, tileY := -1, 0        // 重ねるタイルの位置 (-1 = 初期設定のタイル: 横は中央、縦は一覧のタブの下)
	switch {
	case m.sc == scSetup && m.su != nil:
		// 初期設定は一覧を背景に残し、その上に画面の中央へ重ねる (全画面に切り替えると、どの相手を設定しているかが一覧から読めなかった。
		// 一覧のカードを広げてその内側に入れる形も試したが、一覧が窓の形に引き伸ばされるので採らない)。
		// 操作は初期設定が取るので、背景の一覧は選んだ行のまま動かない
		body = m.viewList(w)
		if m.tooShort {
			return m.viewTooSmall("高さが足りない")
		}
		if m.detailOpen {
			// 詳細のパネルから開いた: パネルを下に残し、その上へ少しずらして重ねる (選んだ順に積む。esc で 1 枚ずつ戻る)
			under, tile, tileX, tileY = m.stackSetup(w)
		}
		// 見出しとタブは背景に見せ、その下から一覧の上に重ねる (中央に置くと、見出しもタブも一覧の行も全部隠れた)。左右にも一覧が覗く
		if tw := min(m.w-2*(padX+setupTileSideGap), setupTileMaxW); tile == "" && tw >= setupTileMinW {
			tile, tileX = m.viewSetup(tw, padY+m.bodyHeight()-m.listTop), -1
		}
		if tile == "" || m.tooShort {
			// 重ねて入らない (狭い・低い端末): 従来どおり全画面で出す
			tile = ""
			body = lipgloss.JoinVertical(lipgloss.Left, m.pageHead(w), m.viewSetup(w, m.bodyHeight()-4))
			if m.tooShort {
				return m.viewTooSmall("高さが足りない")
			}
		}
	case m.sc == scSettings:
		body = lipgloss.JoinVertical(lipgloss.Left, m.pageHead(w), m.viewSettings(w))
		if m.tooShort {
			return m.viewTooSmall("高さが足りない")
		}
		if m.bandEdit {
			// 板の左端 (padX) + 枠 1 + 余白 2 + 選んでいる行の左の棒 1 (selRow。帯の行は編集中は必ず選んでいる) + 名前の列 +
			// 入力欄の左の余白 1。行は 見出し 3・枠 1・余白 1・板の見出し 3 の下で、窓の分を引く。画面の中に無ければ出さない
			m.caretMX = padX + 1 + 2 + 1 + m.setLabelW + 1 + m.bandCol
			m.caretMY = padY + 3 + 1 + 1 + 3 + m.bandRow - m.setOff
			if m.caretMY >= padY+m.bodyHeight() || m.caretMX >= m.w {
				m.caretMY = -1
			}
		}
	default:
		body = m.viewList(w)
		if m.tooShort { // カーソルの行まで入らない高さ: 切れた板を出さず、抜け方だけの画面にする (高さ 12 以下でカーソルの行ごと板が切れた)
			return m.viewTooSmall("高さが足りない")
		}
		if m.detailOpen {
			// 最下行 (抜け方の案内) とその上の空行は覆わない
			tile, tileX, tileY = m.detailPanel(w, max(m.h, 10)-padY-2-m.panelTop), padX, m.panelTop
			if tile == "" {
				// 描けない (枠も入らない高さ・開いた Mac がカーソルの行に無い = 裏の読み直しで消えた): 開いたままにしない
				// (見えないパネルの案内と enter が残った)
				m.detailOpen = false
			}
		}
	}
	h := max(m.h, 10)
	inner := m.bodyHeight()
	// 安全網: どの行も幅で切ってから枠に入れる (lipgloss は幅を超えた行を折り返し、行が増えて下のキーの案内が押し出される)
	bl := strings.Split(body, "\n")
	for i, l := range bl {
		bl[i] = termwidth.Clip(l, w)
	}
	body = lipgloss.NewStyle().Height(inner).MaxHeight(inner).Render(strings.Join(bl, "\n"))
	page := body
	if !m.o.HostHints {
		page = lipgloss.JoinVertical(lipgloss.Left, body, "", m.footer())
	}
	base := lipgloss.NewStyle().Width(m.w).Height(h).MaxHeight(h).Padding(padY, padX).Render(page)
	layers := []*lipgloss.Layer{lipgloss.NewLayer(base)}
	if under != "" && tile != "" {
		layers = append(layers, lipgloss.NewLayer(under).X(padX).Y(m.panelTop).Z(1))
	}
	if tile != "" {
		if tileX < 0 {
			tileX, tileY = max((m.w-lipgloss.Width(tile))/2, 0), m.listTop
		}
		layers = append(layers, lipgloss.NewLayer(tile).X(tileX).Y(tileY).Z(2))
	}
	if box, fieldRow, fieldX := m.overlayBox(); box != "" {
		box = lipgloss.NewStyle().MaxHeight(h).MaxWidth(m.w).Render(box) // 板が画面より高い・広いと View が画面からはみ出した (? の板は 22 行・66 桁)
		bw, bh := lipgloss.Width(box), lipgloss.Height(box)
		x, y := max((m.w-bw)/2, 0), max((h-bh)/2, 0)
		layers = append(layers, lipgloss.NewLayer(box).X(x).Y(y).Z(3)) // 初期設定のタイル (Z 2) の上
		if fieldRow >= 0 {
			m.caretMX, m.caretMY = x+fieldX, y+fieldRow
		}
	}
	return strings.Split(lipgloss.NewCompositor(layers...).Render(), "\n")
}

// stackSetup は詳細のパネルと、その上へ stackDX 桁・stackDY 行ずらして重ねる初期設定のタイル (と位置)。パネルが描けない・
// 初期設定の手順のカードが入らないときは空 (呼び出し側が一覧の上に重ねる従来の形にする)。
func (m *Model) stackSetup(w int) (under, tile string, x, y int) {
	under = m.detailPanel(w, max(m.h, 10)-padY-2-m.panelTop)
	x, y = padX+stackDX, m.panelTop+stackDY
	tw := m.w - x - padX - 1 // 右端をパネルと揃える
	if under == "" || tw < setupTileMinW {
		return "", "", 0, 0
	}
	if tile = m.viewSetup(tw, max(m.h, 10)-padY-2-y); m.tooShort {
		return "", "", 0, 0
	}
	return under, tile, x, y
}

// 初期設定を詳細のパネルに重ねるときのずらし (下の層の枠が覗いて、積んだことが見える。広げると狭い端末で手順の行が切れる)。
const stackDX, stackDY = 2, 1

// 初期設定のタイルを一覧に重ねる幅の下限 (手順の行が切れずに読める。足りない端末では全画面で出す)・上限と、左右に残して一覧を覗かせる桁数。
const (
	setupTileMinW    = 60
	setupTileMaxW    = 76
	setupTileSideGap = 4
)

// rowHints は行の Mac への動作のキーの案内 (一覧と詳細のパネルが共有する。案内と効くキーを同じ述語にする)。
func (m *Model) rowHints(r row) []keyHint {
	if r.self || r.connectIP() == "" {
		return nil
	}
	var hs []keyHint
	if m.sshRefusal(r) == "" {
		hs = append(hs, keyHint{"s", "繋ぐ", 3})
	}
	if user, _ := m.userFor(r); user != "" {
		hs = append(hs, keyHint{"Y", "コピー", 4})
	}
	if r.vnc {
		hs = append(hs, keyHint{"o", "画面共有", 5})
		if user, _ := m.userFor(r); user != "" { // a が断らない条件 (ユーザー名が要る)
			hs = append(hs, keyHint{"a", "SyakeScreen", 11})
		}
	}
	if r.smb {
		hs = append(hs, keyHint{"y", "smb", 6}, keyHint{"O", "Finder で繋ぐ", 12})
	}
	switch { // askSpeed と同じ条件
	case m.o.SpeedTest == nil:
	case m.speedRun != nil:
		hs = append(hs, keyHint{"t", "測定の進捗", 9})
	case m.speedTarget(r).Refusal == "":
		hs = append(hs, keyHint{"t", "速度", 9})
	}
	if m.o.Reach != nil && m.reachTarget(r).Refusal == "" { // startReach が断らない条件 (確かめている途中なら終わったら知らせる)
		hs = append(hs, keyHint{"p", "疎通", 10})
	}
	return hs
}

// keyHint は最下行の案内の 1 つ。prio は小さいほど残す (1 = 抜ける手段。tuikit/hintfit の尺度)。
type keyHint struct {
	key, label string
	prio       int
}

// Hint は最下行の案内の 1 つ (Prio は小さいほど残す。1 = 抜ける手段。tuikit/hintfit の尺度)。
type Hint struct {
	Key, Label string
	Prio       int
}

// Hints は今の画面で効くキーの案内 (呼び出し側が自分の最下行に描くとき。Options.HostHints)。足りない画面では抜け方だけ。
func (m *Model) Hints() []Hint {
	hs := m.hints()
	out := make([]Hint, 0, len(hs))
	for _, h := range hs {
		out = append(out, Hint{Key: h.key, Label: h.label, Prio: h.prio})
	}
	return out
}

// モーダルのキーは、最下行 (hints) とモーダルの中の案内 (hintLine) の両方がここから組む (別々に書くと片方だけ直して食い違う)。
var (
	progressHints      = []keyHint{{"esc", "閉じる (確認は裏で続く)", 1}}
	speedProgressHints = []keyHint{{"esc", "閉じる (測定は裏で続く)", 1}}
	speedBoxHints      = []keyHint{{"j/k", "経路", 3}, {"y", "実行", 2}, {"n/esc", "キャンセル", 1}}
)

// hintLine はモーダルの中に出すキーの案内の 1 行。
func hintLine(hs []keyHint) string {
	parts := make([]string, 0, len(hs))
	for _, h := range hs {
		parts = append(parts, h.key+" "+h.label)
	}
	return strings.Join(parts, "   ")
}

// firstLabel は詳細カードの項目の行頭を返す関数を作る。項目名は最初の行だけに出し、2 行目からは同じ幅の空白。
func firstLabel(label func(string) string, name string) func() string {
	head := label(name)
	return func() string { h := head; head = label(""); return h }
}

// pageHead は全画面 (一覧・初期設定・設定) の見出しと罫線と空行。
func (m *Model) pageHead(w int) string {
	return lipgloss.JoinVertical(lipgloss.Left, m.header(w), gradRule(w, cAccent, cPink, cPurple, cTeal, cBg), "")
}

// hints は最下行に載せるキー。板・モーダルが開いている間はその板のキーだけにする (一覧のキーを残すと、板では効かないキーを案内する)。
// 一覧では、選んでいる Mac の動作のキー (s 繋ぐ…) を名前の長さ・配置に依らず載せる (詳細カードが 1 行に畳まれると、そこから押し出された)。
func (m *Model) hints() []keyHint {
	var hs []keyHint
	switch {
	case m.small:
		return []keyHint{{"q", "閉じる", 1}}
	case m.progressShown():
		hs = progressHints
	case m.speedShown():
		hs = speedProgressHints
	case m.ov == ovSpeed:
		hs = speedBoxHints
	case m.ov == ovConfirm:
		yes := "y/enter"
		if m.ask != nil && (m.ask.auto || m.ask.selfOnly) { // HandleInput が enter で実行しない板
			yes = "y"
		}
		hs = []keyHint{{yes, "実行", 2}, {"n/esc", "キャンセル", 1}}
		if m.ask != nil && m.ask.exec != nil {
			hs = append(hs, keyHint{"c", "コマンドをコピー", 3})
		}
	case m.ov == ovCopyIP:
		keys := "1/2"
		if len(m.selfCopyItems()) > 2 {
			keys = "1/2/3"
		}
		hs = []keyHint{{keys, "すぐコピー", 2}, {"j/k+enter", "選んでコピー", 3}, {"esc", "やめる", 1}}
	case m.ov == ovLog:
		hs = []keyHint{{"j/k", "選ぶ", 3}, {"y", "1 件コピー", 2}, {"Y", "全部コピー", 4}, {"e/esc", "閉じる", 1}}
	case m.ov == ovAlias:
		hs = []keyHint{{"enter", "決める", 2}, {"esc", "やめる", 1}}
	case m.ov == ovHelp:
		hs = []keyHint{{"どのキーでも", "閉じる", 1}}
	case m.sc == scSetup:
		hs = []keyHint{{"x", "実行", 2}, {"r", "リロード", 3}, {"e", "メッセージ", 4}, {"q/esc", "戻る", 1}} // e: 1 行に切った接続の失敗の全文
	case m.sc == scSettings:
		hs = []keyHint{{"j/k", "項目", 2}, {"space", "入/切", 3}, {"←/→", "選ぶ", 4}, {"enter", "編集", 5}, {"q/esc/,", "閉じる", 1}}
	case m.filtering:
		hs = []keyHint{{"enter", "確定", 2}, {"esc", "やめる", 1}}
	case m.detailOpen:
		hs = []keyHint{{"q/esc", "戻る", 1}}
		if r := m.current(); !r.self {
			hs = append(hs, keyHint{"enter", "初期設定", 2})
		} else if m.selfStaticRefusal() == "" {
			hs = append(hs, keyHint{"enter", "有線を固定", 2})
		}
		hs = append(hs, m.rowHints(m.current())...)
		if m.detailMax > 0 {
			hs = append(hs, keyHint{"j/k", "送る", 3})
		}
	default:
		rows := m.rowHints(m.current())
		hs = append(make([]keyHint, 0, 2+len(rows)+6), keyHint{"j/k", "移動", 7}, keyHint{"enter", "詳細", 6})
		hs = append(hs, rows...)
		hs = append(hs, keyHint{"h/l", "タブ", 10}, keyHint{"space", "台帳", 8}, keyHint{"c", "この Mac の IP", 9},
			keyHint{",", "設定", 11}, keyHint{"?", "全部のキー", 2}, keyHint{"q", "閉じる", 1})
	}
	if m.ov == ovNone && !m.speedShown() && !m.progressShown() && m.sc != scSettings && !m.filtering && !m.selfOK { // 読めるまで断るキーは案内にも出さない (出すと押しても断られた。板とモーダルのキーは網を読まない)
		hs = slices.DeleteFunc(hs, func(h keyHint) bool {
			return slices.Contains(needsNet, h.key) || (m.detailOpen && h.key == "enter") // パネルの enter (初期設定) も netRefused が断る
		})
	}
	return hs
}

// footer は最下行の案内。幅が足りなければ hintfit が優先度の低いものから項目ごと落とし、抜け方 (優先度 1) は最後まで残す。
func (m *Model) footer() string {
	hs := m.hints()
	items := make([]hintfit.Item, 0, len(hs))
	for _, h := range hs {
		items = append(items, hintfit.Item{Text: lipgloss.NewStyle().Foreground(cPink).Bold(true).Render(h.key) + " " + sDim.Render(h.label), Prio: h.prio})
	}
	w := max(m.w-2*padX, 1)
	return termwidth.Truncate(hintfit.Fit(w, sMuted.Render(" • "), items), w, "")
}

func (m *Model) header(w int) string {
	left := logo()
	switch {
	case m.Busy():
		// 何を待っているかを出す (Busy は走査の他に相手の確認・確認待ちも含む。走査が終わった後も「Bonjour で探しています」と出た)
		var what string
		switch {
		case !m.netReady:
			what = " この Mac のネットワーク設定を確認中"
		case m.scan != nil && m.scan.Busy(m.now):
			what = fmt.Sprintf(" Bonjour で探しています (%d 台)", len(m.peers))
		case m.su != nil && m.su.ch != nil:
			what = " 相手に接続して確認中"
		case m.netCh != nil:
			what = " この Mac のネットワーク設定を確認中"
		case len(m.pending) == 0 && !m.awaitingUnconf() && len(m.bg) > 0:
			what = " 有線の名前の広告を止めている"
		case len(m.pending) == 0 && !m.awaitingUnconf() && m.speedRun != nil:
			what = " 通信速度を測っている"
		case len(m.pending) == 0 && !m.awaitingUnconf() && len(m.reachRuns) > 0:
			what = " 疎通を確かめている"
		case len(m.pending) == 0 && !m.awaitingUnconf():
			what = " 有線の名前を確かめている"
		default: // 残る Busy は確認待ち (pending / awaitingUnconf)
			what = " 固定 IP の確認待ち"
		}
		left += "   " + lipgloss.NewStyle().Foreground(cPink).Render(spinner.At(m.now, spinStep)) + sDim.Render(what)
	case !m.selfOK:
		left += "   " + sWarn.Render("✗ この Mac のネットワークを読めない") + sDim.Render(" (r で読み直す)")
	case m.scanErr != nil:
		left += "   " + sWarn.Render("✗ Bonjour で探せない") + sDim.Render(fmt.Sprintf(" (%d 台。理由は ?)", len(m.peers)))
	default:
		left += "   " + sOK.Render("✓") + sDim.Render(fmt.Sprintf(" %d 台見つかった", len(m.peers)))
	}
	ips := m.selfIPs()
	right := sKeycap.Render("c") + sDim.Render(" この Mac  メイン ") + sText.Render(or(ips[1], "-")) + sDim.Render("   内部 ") + sText.Render(or(ips[0], "-"))
	if m.selfLan().Unfixed {
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
	// 詰めても 1 桁超えることがある (左右の間の 2 桁の空白)。超えると全体の行が広がり、影の列が切れた
	return termwidth.Truncate(left+strings.Repeat(" ", max(w-lipgloss.Width(left)-lipgloss.Width(right), 2))+right, w, "")
}

func (m *Model) viewList(w int) string {
	r := m.current()
	wide := w >= 160
	listW := w
	if wide {
		listW = w * 58 / 100
	}
	head := m.pageHead(w)
	tabs := m.tabs(listW - 1)
	detailW := w
	if wide {
		detailW = w - listW - 2
	}
	// 詳細カードが幅に収まらない (狭い端末) 行は、動作のキーだけの 1 行にする (広がると一覧の板の右辺まで切れた)
	detailOf := func(r row, compact bool) string {
		if !compact {
			if d := m.detail(r, detailW); lipgloss.Width(d) <= detailW {
				return d
			}
		}
		return m.detailCompact(r)
	}
	detail := detailOf(r, false)
	// 一覧に回せる行数: 画面の高さから、見出し・タブ・枠・表の見出しと (縦に積むときは) 詳細カードを先に引く。詳細カードに動作の
	// キー (s 繋ぐ・Y コピー・o 開く) があるので、それを押し出さないよう先に取る。全部を描いて下で切っていたころは、狭い画面で
	// 詳細カードもカーソル行も画面の外に出た
	frame := lipgloss.Height(shadow(card("", listW-1, surfList, tabColors[m.tab], cPurple))) - 1
	rows := m.bodyHeight() - lipgloss.Height(head) - lipgloss.Height(tabs) - frame - 2 // 2 = 表の見出しと罫線
	// 一覧の行 (カーソルの行) まで入らない高さは抜け方だけの画面にする (View)。板の下の余白・枠・影の 3 行は切れてよい
	// (行が見える方が、空の画面より役に立つ)。高さ 12 以下で、カーソルの行ごと板が切れていた
	m.tooShort = rows < -2
	if !wide {
		// 縦に積むときは、一覧に min(3, 行数) 行を残せなければ詳細カードを 1 行 (名前だけ) に畳み、それも入らなければ出さない。
		// 動作のキーは最下行の案内にいつも載せている (footer) ので、ここで何を出しても消えない。
		// 🚨 引く高さは選んだ行ではなく全行の最大で見積もる。選んだ行の分だけ引くと、詳細カードの高い行 (IP が多い相手) を選んだときだけ
		// 一覧の窓が縮み、カーソルを動かすだけで窓とスクロールバーが伸び縮みした (畳むか出さないかも全行で揃える)
		vis := m.visible()
		reserve := func(compact bool) int {
			h := lipgloss.Height(detailOf(r, compact))
			for _, v := range vis {
				h = max(h, lipgloss.Height(detailOf(v, compact)))
			}
			return h
		}
		compact, h := false, reserve(false)
		if rows-h-1 < min(3, len(vis)) {
			compact, h = true, reserve(true)
		}
		if rows-h-1 < 1 {
			detail = ""
		} else {
			detail = detailOf(r, compact)
			rows -= h + 1
		}
	}
	table := m.listTable(listW-7, max(rows, 1))
	m.listTop = padY + lipgloss.Height(head) + lipgloss.Height(tabs)
	m.panelTop = padY + lipgloss.Height(head) - 1 // 見出しの罫線の下の空行から (詳細のパネルはタブと一覧の上に重ねる)
	list := lipgloss.JoinVertical(lipgloss.Left, tabs, shadow(card(table, listW-1, surfList, tabColors[m.tab], cPurple)))
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
	pills := func(labels []string) string {
		parts := make([]string, 0, len(labels))
		for i, lb := range labels {
			if i == m.tab {
				parts = append(parts, pillTab(lb, tabColors[i], cBg, true))
			} else {
				parts = append(parts, pillTab(lb, cTabIdle, cDim, false))
			}
		}
		return strings.Join(parts, " ")
	}
	managed, unmanaged := m.count(true), m.count(false)
	row := pills([]string{fmt.Sprintf("管理対象  %d", managed), fmt.Sprintf("未管理  %d", unmanaged), "この Mac"})
	// 右の絞り込みの最小の欄 (前置き 2 + 欄 4 + 間 1 + 余白 2) が残らない幅では、タブの名前を縮める (この Mac のタブを足して 13 桁
	// 伸び、狭い端末で絞り込みの入力欄が消えた)
	if width-lipgloss.Width(row)-3 < 9 {
		row = pills([]string{fmt.Sprintf("管理 %d", managed), fmt.Sprintf("未管理 %d", unmanaged), "自分"})
	}
	// 一覧が空のとき「相手がいない」と区別できるように、設定で絞っていることを出す。ただし右の絞り込みの最小の欄
	// (前置き 2 + 欄 4 + 間 1 + 余白 2) が残るときだけ (残らない幅で足すと、入力欄とキャレットが消えた)
	if mark := sMuted.Render("  有線で見えた相手だけ"); m.pf.WiredOnly && width-lipgloss.Width(row)-lipgloss.Width(mark) >= 9 {
		row += mark
	}
	// 右の絞り込みには、タブの残りの幅だけを使う (固定の幅だと狭い端末で行が広がり、下の一覧の板の右辺まで切れた)
	room := width - lipgloss.Width(row) - 3
	var search string
	switch {
	case m.filtering:
		fw := min(25, max(room-2, 4))
		txt, col := m.filter.Window(fw - 1)
		search = sAccent.Render("/ ") + lipgloss.NewStyle().Background(cCode).Render(termwidth.FillRight(txt, fw))
		m.caretFX = 2 + col // 入力欄の中の桁。行の左端からの位置は間の空白が決まってから足す
		m.caretFY = padY + 3
	case m.filter.String() != "":
		search = sAccent.Render("/ "+termsafe.PlainLine(m.filter.String())) + sMuted.Render("  esc で解除")
	default:
		search = sMuted.Render("/ で絞り込み")
	}
	gap := max(width-lipgloss.Width(row)-lipgloss.Width(search)-2, 1)
	out := row + strings.Repeat(" ", gap) + search
	// キャレットは実際に置いた間の空白から出す (右寄せを仮定すると、間の空白が 1 桁に詰まる幅で左へずれた)
	m.caretFX += padX + lipgloss.Width(row) + gap
	if lipgloss.Width(out) > width {
		m.caretFY = -1 // 入力欄まで切れる幅ではキャレットを出さない
		out = termwidth.Truncate(out, width, "")
	}
	return out
}

// col は一覧の 1 列 (設定で出し入れする)。
type col struct {
	title string
	width int // 0 なら残りの幅、fitWidth なら中身の最長に合わせる
	cell  func(r row) string
	style func(s lipgloss.Style, r row) lipgloss.Style
}

// dropOrder は幅が足りないときに列を落とす順 (大きいほど先: 他 → user → メイン IP → 疎通 → 接続できる。別名と内部 IP は落とさない)。
var dropOrder = map[string]int{"他": 5, "user": 4, "メイン IP": 3, "疎通": 2, "接続できる": 1}

// fitWidth は中身の最長に合わせる列の印 (別名の列)。上限は maxFitWidth、幅が足りなければホスト名の列に minFlexWidth を残すまで縮める。
const (
	fitWidth     = -1
	maxFitWidth  = 34 // 中身 32 桁 + 左右の余白 (ユーザー要望 2026-10-15: 別名の列が短すぎる。固定の 12 では「kojiのMac mini」も切れた)
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
		{" ", 1, func(r row) string {
			if m.setupPending(r) {
				return "!"
			}
			return " "
		}, func(s lipgloss.Style, r row) lipgloss.Style { return s.Foreground(cWarn).Bold(true) }},
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
		cols = append(cols, col{"メイン IP", 24, func(r row) string { return ipCell(or(r.mainIP, "-"), r.mainLink) }, nil})
	}
	if m.pf.ShowLan {
		cols = append(cols, col{"内部 IP", 25, func(r row) string {
			if r.lanIP != "" && !m.lanFixed(r) {
				return sWarn.Render(ipCell(r.lanIP, r.lanLink)) // 自動割り当て (起動のたびに変わりうる。未固定)
			}
			return ipCell(or(r.lanIP, "-"), r.lanLink)
		}, nil})
	}
	if m.o.Reach != nil {
		cols = append(cols, col{"疎通", 7, m.reachCell, nil})
	}
	if m.pf.ShowOthers {
		cols = append(cols, col{"他", 6, func(r row) string {
			if n := len(m.shownOthers(r)); n > 0 {
				return lipgloss.NewStyle().Foreground(cPurple).Bold(true).Render(fmt.Sprintf("+%d", n))
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
// detailPanel は一覧で Enter を押したときに重ねる詳細のパネル (幅 width いっぱい・影なし・高さ height まで)。詳細カードと同じ中身から
// 空行を詰め、入らなければ最後の行を送りの案内にして j/k で送る (m.detailOff)。描くたびに送りの上限 (m.detailMax) を決める。
// 枠の分も入らない高さでは "" (パネルを出さない。最下行の案内で抜けられる)。
func (m *Model) detailPanel(width, height int) string {
	r := m.current()
	const frame = 4 // card の上下の枠と余白
	if height-frame < 2 || !m.detailShowsOpened() {
		m.detailMax = 0
		return ""
	}
	wrap := lipgloss.NewStyle().Width(max(width-1-6, 1)) // card(…, width-1) の内側 (左右の枠と余白 6 桁)
	var lines []string
	for i, l := range m.detailLines(r, width, true) {
		if l == "" && i != 2 { // 題と罫線の後の空行だけ残す (区切りの空行は高さを食うので詰める)
			continue
		}
		lines = append(lines, strings.Split(wrap.Render(l), "\n")...)
	}
	inner := height - frame
	m.detailMax = 0
	if len(lines) > inner {
		shown := inner - 1 // 最後の行は送りの案内
		m.detailMax = len(lines) - shown
		m.detailOff = min(max(m.detailOff, 0), m.detailMax)
		rest := len(lines) - shown - m.detailOff
		hint := "↓ j/k で続き (" + strconv.Itoa(rest) + " 行)"
		if rest == 0 {
			hint = "↑ k で戻る"
		}
		lines = append(append([]string(nil), lines[m.detailOff:m.detailOff+shown]...), sMuted.Render(hint))
	} else {
		m.detailOff = 0
		// 下まで伸ばす: 内容で高さを決めると、高い端末でパネルの下から一覧の中の詳細カードが覗き、同じ中身が 2 つ見えた
		lines = append(lines, make([]string, inner-len(lines))...)
	}
	return card(strings.Join(lines, "\n"), width-1, surfDetail, cTeal, cPurple, cPink)
}

// detailCompact は高さ・幅が足りないときの詳細カードの代わり (動作のキーだけの 1 行。枠が無いので、幅で切っても壊れない)。
func (m *Model) detailCompact(r row) string {
	return termwidth.Truncate(m.detailCompactLine(r), max(m.w-2*padX, 1), "…")
}

func (m *Model) detailCompactLine(r row) string {
	name := or(r.alias, or(r.instance, r.host))
	if r.self {
		name = "この Mac"
	}
	return sText.Render(name) + "   " + sMuted.Render("(enter で詳細。動作のキーは最下行)")
}

// ipCell は一覧の IP の列のセル。IPv4 の最長 (15 桁) まで埋めてから線の括弧を置く (括弧の位置を行の間で縦に揃える。IP の直後に
// 付けると全角の「有線」の位置が行ごとにずれる)。線が分からなければ括弧を出さない。
// 括弧に別の色を付けない: セルの中で色を分けると、選んだ行の地の色が IP と括弧の間で途切れた (描いた SGR で確かめた)。
func ipCell(ip string, l netinfo.Link) string {
	if l.Label() == "" {
		return ip
	}
	return fmt.Sprintf("%-15s (%s)", ip, l.Label())
}

// linkTag は線の括弧 ("(有線)" / "(無線)")。分からなければ ""。
func linkTag(l netinfo.Link) string {
	if l.Label() == "" {
		return ""
	}
	st := sDim
	if l == netinfo.LinkWireless {
		st = sWarn // 無線は遅くなりうる (有線で繋ぐつもりの相手が無線に居ることに気づかせる)
	}
	return st.Render("(" + l.Label() + ")")
}

// minWidth はこの画面を組む最小の幅。12 桁以下では枠を組む計算が負になって落ちた (strings.Repeat)。これより狭ければ
// 抜け方だけの画面にする。
const minWidth = 43 // 一覧の表の最小 (印 1 + 名前 4 + 内部 IP 25) + 板の枠と余白 7 + 外の余白 6 より狭いと、板の右辺が切れた

// viewTooSmall は幅・高さが足りないときの画面 (抜け方だけは必ず見せる)。
func (m *Model) viewTooSmall(why string) []string {
	m.small, m.caretFY, m.caretMY = true, -1, -1 // 何も無い画面に端末のカーソルを立てない
	lines := make([]string, max(m.h, 1))
	lines[0] = termwidth.Clip(why, max(m.w, 1))
	if m.o.HostHints { // 抜け方は呼び出し側の最下行が出す (Hints)
		return lines
	}
	lines[len(lines)-1] = "q"
	if m.w >= termwidth.Of("q 閉じる") {
		lines[len(lines)-1] = "q 閉じる"
	}
	return lines
}

// bodyHeight は枠の内側の行数 (View の inner と同じ式)。最下行の案内とその上の空行の 2 行を引く (呼び出し側が案内を描くなら引かない)。
func (m *Model) bodyHeight() int {
	foot := 2
	if m.o.HostHints {
		foot = 0
	}
	return max(max(m.h, 10)-2*padY-foot, 1)
}

// listTable は一覧の表。maxRows 行より多ければ、カーソルを含む窓だけを描く (窓の位置はタブごとに覚える)。
func (m *Model) listTable(width, maxRows int) string {
	vis := m.visible()
	cur := min(m.cur[m.tab], len(vis)-1)
	// 窓に収まらない (1 ページで出せない) ときだけ右端にスクロールバーを置く。バーの分 (layout.ScrollbarWidth) を先に引いて表を組む
	// (幅ぴったりに組むと、全幅の行だけ末尾が … に化ける)。見出しと罫線はバー列を持たないので、outer の幅へ揃える
	outer := width
	scroll := len(vis) > maxRows && outer > layout.ScrollbarWidth
	if scroll {
		width -= layout.ScrollbarWidth
	}
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
	// 足りなければ別名の列を縮めて、ホスト名の列に minFlexWidth を残す。縮めても残らなければホスト名の列を出さず (ホスト名は
	// 詳細カードの見出しにある)、空いた幅を別名の列へ返す (縮めたまま返さないと、ホスト名も名前も両方切れる)
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
	// それでも固定の列だけで幅を超えるなら、落とす順に列ごと落とす (落とさないと 1 行が枠の中で折り返し、Mac ごとに 2 行になった。
	// 幅 75〜89 の既定の列で再現)。空いた幅は別名の列へ返す
	for used > width {
		best := -1
		for i, c := range cols {
			if d := dropOrder[c.title]; d > 0 && (best < 0 || d > dropOrder[cols[best].title]) {
				best = i
			}
		}
		if best < 0 {
			break
		}
		used -= cols[best].width
		if best < fitAt {
			fitAt--
		}
		cols = slices.Delete(cols, best, best+1)
	}
	// 落とせる列が無くても超えるなら、別名の列を 12 桁より細くする (最後の手段。最低 4 桁)
	if fitAt >= 0 && used > width {
		shrink := min(cols[fitAt].width-4, used-width)
		cols[fitAt].width -= shrink
		used -= shrink
	}
	// 残りの幅を取る列 (ホスト名) が無いときだけ、空いた幅を別名の列へ返す (ある間はその列が残りを取る。両方に渡すと幅を超えた)
	if fitAt >= 0 && used < width && cols[fitAt].width < desired && !slices.ContainsFunc(cols, func(c col) bool { return c.width == 0 }) {
		grow := min(desired-cols[fitAt].width, width-used)
		cols[fitAt].width += grow
		used += grow
	}
	render := func(i int, r *row) string {
		out := make([]string, 0, len(cols))
		pending := r != nil && m.setupPending(*r)
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
				switch {
				case i == cur && ci == 0:
					s = s.Background(cAccent).Foreground(cBg) // 印 (!) を橙の上でも読めるように地の色で描く
				case i == cur:
					s = s.Background(cSel)
				case pending:
					s = s.Background(cPendBg)
				}
			}
			out = append(out, s.Render(termwidth.Truncate(txt, cw-s.GetHorizontalPadding(), "…")))
		}
		return lipgloss.JoinHorizontal(lipgloss.Top, out...)
	}
	lines := []string{render(-1, nil) + termwidth.PadSpaces(outer-width), sMuted.Render(strings.Repeat("─", outer))}
	if len(vis) == 0 {
		return strings.Join(append(lines, sDim.Render("  "+m.emptyTabNote())), "\n")
	}
	from, to := 0, len(vis)
	m.listRows = maxRows // 半ページ送りの量 (端末の高さではなく、一覧の窓の行数で送る)
	if len(vis) > maxRows {
		from = listnav.WindowOffset(m.listOff[m.tab], cur, len(vis), maxRows)
		m.listOff[m.tab] = from
		to = from + maxRows
	}
	body := make([]string, 0, to-from)
	for i := from; i < to; i++ {
		body = append(body, render(i, &vis[i]))
	}
	if scroll {
		body = layout.Scrollbar(body, outer, len(vis), from, true)
	}
	return strings.Join(append(lines, body...), "\n")
}

// emptyTabNote は相手のタブに行が無いときの 1 行。
func (m *Model) emptyTabNote() string {
	switch {
	case m.filter.String() != "":
		return "絞り込みに合う Mac が無い"
	case m.tab == tabManaged:
		return "管理対象の Mac はまだ無い (未管理のタブで space か初期設定)"
	case m.Busy():
		return "探しています…"
	}
	return "見つかった Mac は無い"
}

func (m *Model) detail(r row, width int) string {
	if !r.self && r.host == "" { // タブに行が無い
		return sDim.Render(m.emptyTabNote())
	}
	return shadow(card(strings.Join(m.detailLines(r, width, false), "\n"), width-1, surfDetail, cTeal, cPurple, cPink))
}

// detailLines は詳細の中身の行 (一覧の下のカードと、Enter で重ねるパネルが共有する)。inPanel は詳細のパネルの中か
// (一覧では enter が詳細のパネルを開き、パネルの中では初期設定を開くので、置き場で enter の案内が変わる)。
func (m *Model) detailLines(r row, width int, inPanel bool) []string {
	enterLabel := "詳細・初期設定"
	if inPanel {
		enterLabel = "初期設定"
	}
	user, _ := m.userFor(r)
	name := or(r.alias, or(r.instance, r.host))
	title := sAccent.Render(name)
	if name != r.host && r.host != "" {
		title += sDim.Render("  " + r.host)
	}
	label := func(s string) string { return sDim.Width(12).Render(s) } // 一番長い「有線の名前」(10 桁) の後に 2 桁空ける
	link := func(url string) string {
		return lipgloss.NewStyle().Foreground(cInfo).Underline(true).Hyperlink(url).Render(url)
	}
	ip := m.target(r)                 // 有線の名前が引ければ名前 (wiredname.go)
	via := tagAfter(m.connectLink(r)) // 宛先が有線の名前でも、繋ぐ IP の線を出す
	lines := []string{title, gradRule(width-7, cTeal, cPurple, cPink, lipgloss.Color(surfDetail)), ""}
	if !r.self {
		if ip != "" && user != "" {
			keys := keycap("Y", "コピー")
			if m.sshRefusal(r) == "" { // 最下行と s のキーと同じ述語 (ssh が閉じた相手に s を案内しない)
				keys += "  " + keycap("s", "繋ぐ")
			}
			lines = append(lines, label("ssh")+sCode.Render("ssh "+m.login(r, ip))+via+"  "+keys)
		}
		if r.smb && ip != "" {
			lines = append(lines, label("smb")+link("smb://"+ip)+via+"   "+keycap("y", "コピー")+"  "+keycap("O", "open をコピー"))
		}
		if r.vnc && ip != "" {
			keys := keycap("o", "開く")
			if user != "" { // 最下行の案内と a のキーと同じ条件
				keys += "  " + keycap("a", "SyakeScreen")
			}
			lines = append(lines, label("画面共有")+link("vnc://"+ip)+via+"   "+keys)
		}
	}
	lanState := pillOn.Render("有線・固定")
	switch {
	case r.lanIP == "":
		lanState = sMuted.Render("内部の網に居ない")
	case !m.lanFixed(r):
		lanState = pillWrn.Render("自動割り当て")
	}
	lines = append(lines, "")
	if m.pf.ShowMain {
		lines = append(lines, label("メイン")+sText.Render(or(r.mainIP, "-"))+tagAfter(r.mainLink))
	}
	if m.pf.ShowLan {
		lines = append(lines, label("内部")+sText.Render(or(r.lanIP, "-"))+tagAfter(r.lanLink)+"  "+lanState)
		if name, ok, note := m.wiredState(r); name != "" {
			state := sOK.Render("✓ 引ける")
			if !ok {
				state = sMuted.Render(note)
			}
			lines = append(lines, label("有線の名前")+sText.Render(name)+"  "+state)
		}
	}
	lines = append(lines, m.speedLine(r, label)...)
	lines = append(lines, m.reachLines(r, label)...)
	if others := m.shownOthers(r); m.pf.ShowOthers && len(others) > 0 {
		lines = append(lines, "", sDim.Render("その他の IP"))
		for _, x := range others {
			lines = append(lines, "  "+lipgloss.NewStyle().Width(16).Render(kindPill(x.Kind))+sMuted.Width(11).Render(x.Device)+sText.Render(x.Addr)+tagAfter(m.self.Link(x.Device)))
		}
	}
	lines = append(lines, "")
	switch {
	case r.self && !r.managed:
		lines = append(lines, sDim.Render("この Mac は台帳に無い  ")+keycap("space", "登録")+m.selfStaticKey(inPanel, enterLabel))
	case r.self:
		lines = append(lines, sOK.Render("✓ 台帳に登録済み")+m.selfStaticKey(inPanel, enterLabel))
	case !r.managed:
		lines = append(lines, sDim.Render("未管理: ")+keycap("space", "台帳に登録")+"   "+keycap("enter", enterLabel))
	case !r.seen:
		lines = append(lines, sWarn.Render("今は Bonjour で見えていない")+"   "+keycap("m", "別名")+"   "+keycap("space", "台帳から外す"))
	default:
		lines = append(lines, keycap("m", "別名")+"   "+keycap("space", "台帳から外す")+"   "+keycap("enter", enterLabel))
	}
	return lines
}

// tagAfter は詳細カードで IP の後ろに置く線の括弧 (分からなければ何も足さない)。
func tagAfter(l netinfo.Link) string {
	if t := linkTag(l); t != "" {
		return " " + t
	}
	return ""
}

func kindPill(k netinfo.Kind) string {
	switch k {
	case netinfo.KindWired:
		return pillOn.Render(k.String())
	case netinfo.KindLinkLocal, netinfo.KindWirelessNoRouter: // ルーターの無い Wi-Fi は「繋がっていない」に近い (黄)
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
	if m.progressShown() {
		return m.progressBox(), -1, 0
	}
	if m.speedShown() {
		return m.speedProgressBox(), -1, 0
	}
	switch m.ov {
	case ovConfirm:
		if m.ask != nil {
			hint := confirm.HintYesNo
			switch {
			case m.ask.exec != nil && (m.ask.auto || m.ask.selfOnly):
				hint = "y: 実行   c: コマンドをコピー   n/Esc: キャンセル"
			case m.ask.exec != nil:
				hint = "y/Enter: 実行   c: コマンドをコピー   n/Esc: キャンセル"
			case m.ask.auto: // 自動で出た板は Enter では実行しない (HandleInput)
				hint = "y: 実行   n/Esc: キャンセル"
			}
			return modal(m.ask.title, m.ask.body, hint, m.w), -1, 0
		}
	case ovLog:
		return m.logBox(), -1, 0
	case ovCopyIP:
		unfixed := m.selfLan().Unfixed
		items := m.selfCopyItems()
		body := make([]string, 0, len(items))
		width := 0
		for _, it := range items {
			width = max(width, termwidth.Of(or(it.value, it.shown)))
		}
		for i, it := range items {
			extra := ""
			switch {
			case i == 0 && unfixed:
				extra = pillWrn.Render("未固定")
			case it.value == "" && it.note != "":
				extra = sMuted.Render(it.note)
			}
			val := sText.Bold(true)
			if it.value == "" {
				val = sMuted
			}
			row := sKeycap.Render(strconv.Itoa(i+1)) + "  " + sDim.Width(12).Render(it.label) + val.Width(max(width, 15)+2).Render(or(or(it.value, it.shown), "-")) + extra
			row = termwidth.Truncate(row, max(m.w-8, 12), "…") // 板の内側 (modal は幅 w-1 から縁と余白の 6 桁を引く) に収める。狭い端末で行がはみ出した
			if i == min(m.copyC, len(items)-1) {
				row = lipgloss.NewStyle().Background(cSel).Render(row)
			}
			body = append(body, row)
		}
		keys := "1 / 2"
		if len(items) > 2 {
			keys = "1 / 2 / 3"
		}
		return modal("この Mac の IP・名前をコピー", body, keys+" すぐコピー   j/k + enter 選んでコピー   esc やめる", m.w), -1, 0
	case ovAlias:
		prefix := sDim.Render(m.edit.label + "  ")
		// 欄の幅は板に入る分だけ (板は幅 w-1 から縁と余白の 6 桁を引いた中に描く)。固定の 25 桁だと、狭い端末で前置きと欄が 2 行に割れ、
		// キャレットが欄の外に立った
		fw := min(25, max(m.w-1-6-lipgloss.Width(prefix), 4))
		txt, col := m.alias.Window(fw - 1)
		field := lipgloss.NewStyle().Background(cCode).Render(termwidth.FillRight(txt, fw))
		// 板の中の位置: 上の辺 1 + 余白 1 + 題 1 + 空行 1 = 4 行目、左の辺 1 + 余白 2 + 前置き
		return modal(m.edit.title, []string{prefix + field}, "enter 決める   esc やめる", m.w), 4, 3 + lipgloss.Width(prefix) + col
	case ovSpeed:
		return m.speedBox(), -1, 0
	case ovHelp:
		keys := [][2]string{{"j / k / g / G / ctrl-d / ctrl-u", "移動"}, {"h / l / tab", "管理対象 / 未管理 / この Mac のタブ"}, {"/", "絞り込み"},
			{"s", "ssh で繋ぐ (端末を渡す)"}, {"Y", "ssh のコマンドをコピー"}, {"y", "smb の接続先をコピー"}, {"O", "Finder で smb に繋ぐコマンド (open smb://…) をコピー"}, {"o", "画面共有を開く"}, {"a", "SyakeScreen で開く (syakescreen://connect)"},
			{"c", "この Mac の IP・有線の名前をコピー"}, {"enter", "詳細 (中でもう一度 enter で初期設定。この Mac の行では有線を固定 IP にする)"}, {"space", "台帳に登録 / 台帳から外す"}, {"m", "別名を付ける"}, {"t", "通信速度を測る (管理対象。確認してから帯域を使う。測定中は進捗を開く)"}, {"p", "疎通を確かめる (管理対象の名前ごとに ping と ssh の口)"}, {"u", "ssh のユーザー名を変える"}, {"e", "メッセージ一覧 (y でコピー)"}, {",", "設定"},
			{"r", "リロード"}, {"q / esc", "閉じる"}}
		body := make([]string, 0, len(keys)+3)
		if m.scanErr != nil { // 見出しの「✗ 探せない」の理由 (知らせは数秒で消え、glogx の中からは dump を起こせない。監査の指摘)
			body = append(body, sWarn.Render("Bonjour で探せない: ")+sText.Render(termsafe.PlainLine(m.scanErr.Error())), "")
		}
		for _, k := range keys {
			body = append(body, lipgloss.NewStyle().Foreground(cPink).Bold(true).Width(34).Render(k[0])+sText.Render(k[1]))
		}
		// 一覧の印 (キーではないが、ほかに説明する場所が無い)
		body = append(body, "", lipgloss.NewStyle().Foreground(cWarn).Bold(true).Width(34).Render("! (管理対象の行)")+sText.Render("初期設定が途中 (enter で開く)"))
		return modal("キーの一覧", body, "どのキーでも閉じる", m.w), -1, 0
	case ovNone:
	}
	return "", -1, 0
}

// selfStaticKey は「この Mac」のカードに添える enter の案内 (有線を固定できるときだけ)。詳細のパネルの中では固定を、
// 一覧の下のカードでは一覧の enter (詳細を開く) を案内する。案内の語で分けない (語を直すと案内がすり替わる)
func (m *Model) selfStaticKey(inPanel bool, enterLabel string) string {
	if m.selfStaticRefusal() != "" {
		return ""
	}
	if inPanel {
		return "   " + keycap("enter", "有線を固定")
	}
	return "   " + keycap("enter", enterLabel)
}
