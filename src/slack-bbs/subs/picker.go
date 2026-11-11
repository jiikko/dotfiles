package subs

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/jiikko/dotfiles/src/termsafe"
	"github.com/jiikko/dotfiles/src/tuikit/caret"
	"github.com/jiikko/dotfiles/src/tuikit/confirm"
	"github.com/jiikko/dotfiles/src/tuikit/hintfit"
	"github.com/jiikko/dotfiles/src/tuikit/layout"
	"github.com/jiikko/dotfiles/src/tuikit/lineedit"
	"github.com/jiikko/dotfiles/src/tuikit/listnav"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
	"slack-bbs/source"
)

type item struct {
	ch  source.Channel
	on  bool
	grp *Channel // グループの行 (一覧の頭に並べる。グループは在る限り購読している)。チャンネルの行では nil
}

// Picker はチャンネルの一覧から購読を選ぶ画面の状態。キーは bubbletea の KeyPressMsg.String() の表記で受ける。
type Picker struct {
	all       []source.Channel // NewPicker に渡した一覧 (Refresh で新しい一覧に差し替える元)
	note      string           // 見出しに添える一覧の出所 (前に取った一覧を出している等)。空なら出さない
	items     []item
	list      listnav.List
	filter    lineedit.Line // 検索の語 (入力欄の編集キーは TUI ガイドの「入力欄の編集キー」)
	filtering bool
	// editing は印をグループの出し入れに使っている間、そのグループ (nil なら印は購読の付け外し)
	editing *Channel
	naming  bool          // g で作るグループの名前を打っている
	name    lineedit.Line // 作るグループの名前
	// deleting は d で消すか確かめているグループの行 (items の添字。確かめていなければ -1)
	deleting int
	w, h     int
	done     bool
	save     bool
}

// NewPicker は一覧を作る。並びは 購読中 → 参加しているチャンネル → 参加していないチャンネル (それぞれ名前順)。切り替えても並べ直さない。
// アーカイブ済みは出さない (購読していれば出して外せるようにする)。購読しているのに一覧に無いチャンネル
// (抜けた・消えた) も、外せるように先頭に出す。
func NewPicker(all []source.Channel, subscribed []Channel) *Picker {
	on := map[string]bool{}
	var groups []item
	for _, c := range subscribed {
		if c.IsGroup() {
			g := c
			g.Members = slices.Clone(c.Members) // 編集で書き換えるので、渡された購読の一覧と配列を共有しない
			groups = append(groups, item{ch: source.Channel{ID: c.ID, Name: c.Name}, on: true, grp: &g})
			continue
		}
		on[c.ID] = true
	}
	p := &Picker{all: all, w: 80, h: 24, deleting: -1}
	seen := map[string]bool{}
	for _, c := range all {
		if c.IsArchived && !on[c.ID] {
			continue
		}
		seen[c.ID] = true
		p.items = append(p.items, item{ch: c, on: on[c.ID]})
	}
	sort.SliceStable(p.items, func(i, j int) bool {
		if p.items[i].on != p.items[j].on {
			return p.items[i].on // 購読中を先に (数百のチャンネルの中から、外したいものを探さずに済む)
		}
		a, b := p.items[i].ch, p.items[j].ch
		if a.IsMember != b.IsMember {
			return a.IsMember
		}
		return a.Name < b.Name
	})
	var gone []item
	for _, c := range subscribed {
		if !seen[c.ID] && !c.IsGroup() {
			gone = append(gone, item{ch: source.Channel{ID: c.ID, Name: c.Name, IsArchived: true}, on: true})
		}
	}
	p.items = append(append(groups, gone...), p.items...)
	return p
}

// Refresh は一覧を fresh (取り直した一覧で作った画面) のチャンネルに差し替える。付け外し・検索の語・カーソルのチャンネルは
// 今の画面のものを保つ (前に取った一覧で開いた画面を、取り直しの間に操作していても失わない)。並びは差し替えた時点の購読で組み直す。
func (p *Picker) Refresh(fresh *Picker) {
	curID, editID := "", ""
	if vis := p.visible(); len(vis) > 0 {
		curID = p.items[vis[min(p.list.Cursor, len(vis)-1)]].ch.ID
	}
	if p.editing != nil {
		editID = p.editing.ID
	}
	n := NewPicker(fresh.all, p.Selected())
	p.all, p.items, p.editing, p.deleting = n.all, n.items, nil, -1
	for _, it := range p.items {
		if it.grp != nil && it.grp.ID == editID {
			p.editing = it.grp // 編集しているグループは差し替えた後の行を指し直す
		}
	}
	p.list.Cursor, p.list.Offset = 0, 0
	for i, ix := range p.visible() {
		if p.items[ix].ch.ID == curID {
			p.list.Cursor = i
			break
		}
	}
}

// SetNote は見出しに添える一覧の出所を決める (空なら出さない)。
func (p *Picker) SetNote(note string) { p.note = termsafe.PlainLine(note) }

// Resize は端末の大きさを伝える。
func (p *Picker) Resize(w, h int) { p.w, p.h = w, h }

// Done は画面を閉じたか。Saved は保存して閉じたか。
func (p *Picker) Done() bool  { return p.done }
func (p *Picker) Saved() bool { return p.save }

// Selected は購読に選ばれたチャンネル (一覧の並び順) と、その後ろにグループ (subs.Save と同じ並び)。
func (p *Picker) Selected() []Channel {
	var out, groups []Channel
	for _, it := range p.items {
		switch {
		case it.grp != nil:
			groups = append(groups, *it.grp)
		case it.on:
			out = append(out, Channel{ID: it.ch.ID, Name: it.ch.Name})
		}
	}
	return append(out, groups...)
}

// visible は検索に一致するチャンネルの添字。検索語は空白で区切った語のすべてを名前に含むもの (大文字と小文字・先頭の # は問わない)。
func (p *Picker) visible() []int {
	var words []string
	for _, w := range strings.Fields(strings.ToLower(p.filter.String())) {
		if w = strings.TrimLeft(w, "#"); w != "" {
			words = append(words, w)
		}
	}
	var out []int
	for i, it := range p.items {
		if it.grp != nil && p.editing != nil {
			continue // グループの出し入れの間は、チャンネルだけを並べる
		}
		name := strings.ToLower(it.ch.Name)
		if !slices.ContainsFunc(words, func(w string) bool { return !strings.Contains(name, w) }) {
			out = append(out, i)
		}
	}
	return out
}

// rows は 1 ページの行数 (見出し 2 行・空行・案内の 1 行を除いた残り)。
func (p *Picker) rows() int { return max(p.h-4, 1) }

// page は今のページの先頭の添字 (visible の中)。窓はページの区切りで動かす (1 行ずつ流さない): 数百のチャンネルを j で送ると、
// 画面が毎行動いて今どこにいるか追えない
func (p *Picker) page() int { return p.list.Cursor / p.rows() * p.rows() }

// pages は visible の件数 n のページの数 (0 件でも 1)。
func (p *Picker) pages(n int) int { return max((n+p.rows()-1)/p.rows(), 1) }

// Key はキー 1 つを処理する (打った文字はキーの表記から推す。Press を使えるならそちら)。
func (p *Picker) Key(k string) { p.Press(k, lineedit.KeyText(k)) }

// Paste は貼り付けた文字列を、検索の語かグループの名前を打っている間だけ欄に入れる (改行とタブは空白になる)。それ以外では何もしない
// (貼った文字をキーとして解釈しない)。
func (p *Picker) Paste(s string) {
	if p.naming {
		p.name.Insert(s)
		return
	}
	if p.filtering {
		p.filter.Insert(s)
		p.list.Cursor, p.list.Offset = 0, 0
	}
}

// Press はキー 1 つを処理する。text はその打鍵が入力する文字 (KeyPressMsg.Text。絵文字の異体字・ZWJ の並びのように 1 文字が
// 2 つ以上の rune になる打鍵は、キーの表記からは推せない)。Ctrl-C はどの状態でも保存せずに閉じる。
func (p *Picker) Press(k, text string) {
	if k == "ctrl+c" {
		p.done = true
		return
	}
	if p.deleting >= 0 {
		if confirm.IsYes(k) {
			p.items = slices.Delete(p.items, p.deleting, p.deleting+1)
			p.list.Cursor = min(p.list.Cursor, max(len(p.visible())-1, 0))
		}
		p.deleting = -1
		return
	}
	if p.naming {
		switch k {
		case "enter":
			p.naming = false
			if name := strings.TrimSpace(p.name.String()); name != "" {
				g := &Channel{ID: NewGroupID(), Name: name}
				p.items = append([]item{{ch: source.Channel{ID: g.ID, Name: name}, on: true, grp: g}}, p.items...)
				p.editing = g // 作ったらすぐ、チャンネルを入れる編集に入る
				p.list.Cursor, p.list.Offset = 0, 0
			}
			p.name.Reset()
		case "esc":
			p.naming = false
			p.name.Reset()
		default:
			p.name.Key(k, text)
		}
		return
	}
	if p.filtering {
		before := p.filter.String()
		defer func() {
			if p.filter.String() != before {
				p.list.Cursor, p.list.Offset = 0, 0 // 一致が変わったら先頭から (Enter・Esc で語が変わらなければ今のページに留まる)
			}
		}()
		// 打ったまま選べるように、編集キーでない ↑ / ↓ / Ctrl-N / Ctrl-P は一覧の移動、Tab は付け外しに使う
		// (Space は語の区切りに要るので付け外しに使えない。Tab は fzf の複数選択と同じ)
		switch k {
		case "up", "down", "ctrl+n", "ctrl+p":
			p.list.Move(listnav.MotionOf(k), len(p.visible()), p.rows(), 0)
		case "tab":
			p.toggle(p.visible())
		case "enter":
			p.filtering = false
		case "esc":
			p.filtering = false
			p.filter.Reset()
		default:
			p.filter.Key(k, text)
		}
		return
	}
	if k == "ctrl+f" {
		k = "right" // TUI ガイド §2: Ctrl-F は → の別名 (検索の欄では編集のキー)
	}
	vis := p.visible()
	if p.editing != nil && (k == "enter" || k == "esc" || k == "q") {
		p.editing = nil // グループの出し入れを終えて、購読の付け外しへ戻る (画面は閉じない)
		p.list.Cursor, p.list.Offset = 0, 0
		return
	}
	cur := -1
	if len(vis) > 0 {
		cur = vis[min(p.list.Cursor, len(vis)-1)]
	}
	switch k {
	case "space", " ", "x":
		p.toggle(vis)
		return
	case "g":
		if p.editing == nil {
			p.naming = true
		}
		return
	case "e":
		if cur >= 0 && p.items[cur].grp != nil {
			p.editing = p.items[cur].grp
			p.list.Cursor, p.list.Offset = 0, 0
		}
		return
	case "d":
		if cur >= 0 && p.items[cur].grp != nil {
			p.deleting = cur
		}
		return
	case "/":
		p.filtering = true
		return
	case "n", "right", "l":
		if len(vis) > 0 {
			p.list.Cursor = min(p.page()+p.rows(), len(vis)-1) // 次のページの先頭 (最後のページなら末尾)
		}
		return
	case "p", "left", "h":
		p.list.Cursor = max(p.page()-p.rows(), 0)
		return
	case "enter":
		p.done, p.save = true, true
		return
	case "q", "esc":
		p.done = true
		return
	}
	if m := listnav.MotionOf(k); m != listnav.None {
		p.list.Move(m, len(vis), p.rows(), 0)
	}
}

// toggle はカーソルの下のチャンネル (vis は今見えている添字) の購読を付け外す。グループを編集している間は、そのグループに
// 入れる・外す。グループの行では何もしない (グループは d で消す)。
func (p *Picker) toggle(vis []int) {
	if len(vis) == 0 {
		return
	}
	it := &p.items[vis[min(p.list.Cursor, len(vis)-1)]]
	switch {
	case it.grp != nil:
	case p.editing != nil:
		g := p.editing
		if i := slices.IndexFunc(g.Members, func(c Channel) bool { return c.ID == it.ch.ID }); i >= 0 {
			g.Members = slices.Delete(g.Members, i, i+1)
		} else {
			g.Members = append(g.Members, Channel{ID: it.ch.ID, Name: it.ch.Name})
		}
	default:
		it.on = !it.on
	}
}

// inEditing は、編集しているグループにチャンネル id が入っているか。
func (p *Picker) inEditing(id string) bool {
	return slices.ContainsFunc(p.editing.Members, func(c Channel) bool { return c.ID == id })
}

// Lines は画面の行。
func (p *Picker) Lines() []string {
	vis := p.visible()
	p.list.Fit(len(vis), p.rows())
	n := 0
	for _, it := range p.items {
		if it.on {
			n++
		}
	}
	pageNo := fmt.Sprintf("ページ %d/%d", p.page()/p.rows()+1, p.pages(len(vis)))
	head := pageNo + "  "
	if p.note != "" {
		head += p.note + "  " // 一覧が古い・取り直せなかったことは、狭い端末でも見出しの後ろより先に切らない
	}
	if p.editing != nil {
		head += fmt.Sprintf("グループ「%s」に入れるチャンネルを選ぶ (%d チャンネル)", termsafe.PlainLine(p.editing.Name), len(p.editing.Members))
	} else {
		head += fmt.Sprintf("購読するチャンネルを選ぶ (%d 件を購読中 / 全 %d 件)", n, len(p.items))
	}
	lines := []string{termwidth.Truncate(head, p.w, "…")}
	switch {
	case p.naming:
		text, _ := p.name.Window(max(p.w-termwidth.Of(nameLabel), 1))
		lines = append(lines, termwidth.Truncate(nameLabel+termsafe.PlainLine(text), p.w, "…"))
	case p.filtering:
		// 語はキャレットが欄に収まるよう前を切って出し、端末のカーソルをキャレットに置く (Cursor。日本語の変換中の文字が欄に出る)
		text, _ := p.filter.Window(p.filterRoom(len(vis)))
		lines = append(lines, termwidth.Truncate(searchLabel+termsafe.PlainLine(text)+fmt.Sprintf(searchCount, len(vis)), p.w, "…"))
	case !p.filter.Empty():
		lines = append(lines, termwidth.Truncate(fmt.Sprintf(searchLabel+"%s (%d 件が一致。/ で検索の語を変える)", termsafe.PlainLine(p.filter.String()), len(vis)), p.w, "…"))
	default:
		lines = append(lines, termwidth.Truncate("/ でチャンネルの名前を検索 (名前の一部。空白で区切ると全部を含むもの)", p.w, "…"))
	}
	off := p.page()
	end := min(off+p.rows(), len(vis))
	for row, i := range vis[min(off, len(vis)):end] {
		it := p.items[i]
		cur := "  "
		if off+row == p.list.Cursor {
			cur = "▶ "
		}
		if it.grp != nil {
			lines = append(lines, termwidth.Truncate(fmt.Sprintf("%s📂%s (%d チャンネル)", cur, termsafe.PlainLine(it.grp.Name), len(it.grp.Members)), p.w, "…"))
			continue
		}
		box := "[ ]"
		if p.editing != nil && p.inEditing(it.ch.ID) || p.editing == nil && it.on {
			box = "[x]"
		}
		var notes []string
		if it.ch.IsPrivate {
			notes = append(notes, "非公開")
		}
		switch {
		case it.ch.IsArchived:
			notes = append(notes, "一覧に無い・アーカイブ済み")
		case !it.ch.IsMember:
			notes = append(notes, "自分は未参加")
		}
		line := cur + box + " #" + termsafe.PlainLine(it.ch.Name)
		if len(notes) > 0 {
			line += "  (" + strings.Join(notes, "・") + ")"
		}
		lines = append(lines, termwidth.Truncate(line, p.w, "…"))
	}
	if len(vis) == 0 {
		lines = append(lines, "  (一致するチャンネルがありません)")
	}
	for len(lines) < p.rows()+2 {
		lines = append(lines, "")
	}
	// キーの案内の 2 行。書き方は TUI ガイドの「キー: 説明」で、語は略さない。閉じ方の 2 つ (どちらも抜ける手段 = 優先度 1) は
	// 1 行に並べると幅 40 に入らないので、1 行ずつ末尾に置く。幅に入らない項目は hintfit が優先度の低い方から落とす
	line1 := []hintfit.Item{{Prio: 2, Text: "Space/x: 購読を付け外し"}, {Prio: 2, Text: "/: 名前で検索"}, {Prio: 1, Text: "Enter: 保存して閉じる"}}
	line2 := []hintfit.Item{{Prio: 3, Text: "n/p: 次・前のページ"}, {Prio: 1, Text: "q: 保存せずに閉じる"}}
	onGroup := len(vis) > 0 && p.items[vis[min(p.list.Cursor, len(vis)-1)]].grp != nil
	switch {
	case p.naming:
		line1 = []hintfit.Item{{Prio: 2, Text: "新しいグループの名前を打つ"}, {Prio: 1, Text: "Enter: 名前を決めてチャンネルを選ぶ"}}
		line2 = []hintfit.Item{{Prio: 3, Text: "Ctrl-W: 1 語消す"}, {Prio: 1, Text: "Esc: 作るのをやめる"}}
	case p.filtering:
		toggle := "Tab: 購読を付け外し"
		if p.editing != nil {
			toggle = "Tab: グループに入れる・外す"
		}
		line1 = []hintfit.Item{{Prio: 2, Text: "↑↓: 移動"}, {Prio: 2, Text: toggle}, {Prio: 1, Text: "Enter: 検索の欄を閉じる"}}
		line2 = []hintfit.Item{{Prio: 3, Text: "Ctrl-W: 1 語消す"}, {Prio: 3, Text: "Ctrl-U: 前を全部消す"}, {Prio: 1, Text: "Esc: 検索を消す"}}
	case p.editing != nil:
		line1 = []hintfit.Item{{Prio: 2, Text: "Space/x: グループに入れる・外す"}, {Prio: 2, Text: "/: 名前で検索"}, {Prio: 1, Text: "Enter/Esc: グループの編集を終える"}}
		line2 = []hintfit.Item{{Prio: 3, Text: "n/p: 次・前のページ"}}
	case onGroup:
		line1 = []hintfit.Item{{Prio: 2, Text: "e: グループを編集"}, {Prio: 2, Text: "d: グループを消す"}, {Prio: 1, Text: "Enter: 保存して閉じる"}}
	default:
		line1 = append(line1[:2:2], hintfit.Item{Prio: 3, Text: "g: グループを作る"}, line1[2])
	}
	for _, items := range [][]hintfit.Item{line1, line2} {
		lines = append(lines, termwidth.Truncate(hintfit.Fit(p.w, "  ", items), p.w, "…"))
	}
	if p.deleting >= 0 {
		g := p.items[p.deleting].grp
		box := confirm.Dialog(" グループを消す ", []string{"📂" + termsafe.PlainLine(g.Name), "を消しますか？", "(チャンネルの購読は変わらない)"}, confirm.HintYesNo, p.w, true)
		lines = layout.OverlayCentered(lines, box, p.w, len(lines), true)
	}
	return lines
}

// nameLabel は作るグループの名前の欄の頭。
const nameLabel = "新しいグループの名前: "

// 検索の行の頭と後ろ。filterRoom と Cursor が同じ値で桁を数える
const (
	searchLabel = "名前で検索: "
	searchCount = "  (%d 件が一致)"
)

// filterRoom は検索の行で語に使える幅 (頭と件数を除いた残り。最低 1)。
func (p *Picker) filterRoom(matches int) int {
	return max(p.w-termwidth.Of(searchLabel)-termwidth.Of(fmt.Sprintf(searchCount, matches)), 1)
}

// Cursor は端末のカーソル (検索の語を打っている間だけ、検索の行のキャレットに置く。それ以外は nil で隠す)。
func (p *Picker) Cursor() *tea.Cursor {
	if p.naming {
		_, col := p.name.Window(max(p.w-termwidth.Of(nameLabel), 1))
		return caret.At(termwidth.Of(nameLabel)+col, 1, p.w, p.h)
	}
	if !p.filtering {
		return nil
	}
	_, col := p.filter.Window(p.filterRoom(len(p.visible())))
	return caret.At(termwidth.Of(searchLabel)+col, 1, p.w, p.h)
}

type program struct{ p *Picker }

func (m program) Init() tea.Cmd { return nil }

func (m program) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.p.Resize(msg.Width, msg.Height)
	case tea.PasteMsg:
		m.p.Paste(msg.Content)
	case tea.KeyPressMsg:
		m.p.Press(msg.String(), msg.Text)
		if m.p.Done() {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m program) View() tea.View {
	v := tea.NewView(strings.Join(m.p.Lines(), "\n"))
	v.AltScreen = true
	v.Cursor = m.p.Cursor()
	return v
}

// Run は画面を開き、閉じるまで待つ。
func Run(p *Picker) error {
	_, err := tea.NewProgram(program{p}).Run()
	return err
}

// Names は購読の一覧を「#名前」の列にする (一覧を出すサブコマンド用)。
func Names(chs []Channel) []string {
	out := make([]string, 0, len(chs))
	for _, c := range chs {
		line := c.Label() + "\t" + c.ID
		if c.IsGroup() {
			names := make([]string, 0, len(c.Members))
			for _, m := range c.Members {
				names = append(names, m.Label())
			}
			line += "\t" + strings.Join(names, " ")
		}
		out = append(out, line)
	}
	return slices.Clip(out)
}
