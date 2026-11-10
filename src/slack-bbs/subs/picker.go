package subs

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/jiikko/dotfiles/src/termsafe"
	"github.com/jiikko/dotfiles/src/tuikit/listnav"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
	"slack-bbs/source"
)

type item struct {
	ch source.Channel
	on bool
}

// Picker はチャンネルの一覧から購読を選ぶ画面の状態。キーは bubbletea の KeyPressMsg.String() の表記で受ける。
type Picker struct {
	items     []item
	list      listnav.List
	filter    string
	filtering bool
	w, h      int
	done      bool
	save      bool
}

// NewPicker は一覧を作る。並びは 購読中 → 参加しているチャンネル → 参加していないチャンネル (それぞれ名前順)。切り替えても並べ直さない。
// アーカイブ済みは出さない (購読していれば出して外せるようにする)。購読しているのに一覧に無いチャンネル
// (抜けた・消えた) も、外せるように先頭に出す。
func NewPicker(all []source.Channel, subscribed []Channel) *Picker {
	on := map[string]bool{}
	for _, c := range subscribed {
		on[c.ID] = true
	}
	p := &Picker{w: 80, h: 24}
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
		if !seen[c.ID] {
			gone = append(gone, item{ch: source.Channel{ID: c.ID, Name: c.Name, IsArchived: true}, on: true})
		}
	}
	p.items = append(gone, p.items...)
	return p
}

// Resize は端末の大きさを伝える。
func (p *Picker) Resize(w, h int) { p.w, p.h = w, h }

// Done は画面を閉じたか。Saved は保存して閉じたか。
func (p *Picker) Done() bool  { return p.done }
func (p *Picker) Saved() bool { return p.save }

// Selected は購読に選ばれたチャンネル (一覧の並び順)。
func (p *Picker) Selected() []Channel {
	var out []Channel
	for _, it := range p.items {
		if it.on {
			out = append(out, Channel{ID: it.ch.ID, Name: it.ch.Name})
		}
	}
	return out
}

// visible は検索に一致するチャンネルの添字。検索語は空白で区切った語のすべてを名前に含むもの (大文字と小文字・先頭の # は問わない)。
func (p *Picker) visible() []int {
	var words []string
	for _, w := range strings.Fields(strings.ToLower(p.filter)) {
		if w = strings.TrimLeft(w, "#"); w != "" {
			words = append(words, w)
		}
	}
	var out []int
	for i, it := range p.items {
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

// Key はキー 1 つを処理する。
func (p *Picker) Key(k string) {
	if p.filtering {
		before := p.filter
		defer func() {
			if p.filter != before {
				p.list.Cursor, p.list.Offset = 0, 0 // 一致が変わったら先頭から (Enter・Esc で語が変わらなければ今のページに留まる)
			}
		}()
		switch k {
		case "enter":
			p.filtering = false
		case "esc":
			p.filtering, p.filter = false, ""
		case "backspace":
			if r := []rune(p.filter); len(r) > 0 {
				p.filter = string(r[:len(r)-1])
			}
		case "space":
			p.filter += " "
		default:
			if len([]rune(k)) == 1 {
				p.filter += k
			}
		}
		return
	}
	vis := p.visible()
	switch k {
	case "space", " ", "x":
		if len(vis) > 0 {
			i := vis[min(p.list.Cursor, len(vis)-1)]
			p.items[i].on = !p.items[i].on
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
	case "q", "esc", "ctrl+c":
		p.done = true
		return
	}
	if m := listnav.MotionOf(k); m != listnav.None {
		p.list.Move(m, len(vis), p.rows(), 0)
	}
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
	lines := []string{termwidth.Truncate(fmt.Sprintf("%s  購読するチャンネルを選ぶ (%d 件を購読中 / 全 %d 件)", pageNo, n, len(p.items)), p.w, "…")}
	switch {
	case p.filtering:
		lines = append(lines, termwidth.Truncate(fmt.Sprintf("検索: %s▏ (%d 件)", termsafe.PlainLine(p.filter), len(vis)), p.w, "…"))
	case p.filter != "":
		lines = append(lines, termwidth.Truncate(fmt.Sprintf("検索: %s (%d 件。/ で変える)", termsafe.PlainLine(p.filter), len(vis)), p.w, "…"))
	default:
		lines = append(lines, termwidth.Truncate("/ で検索 (名前の一部。空白で区切ると全部を含むもの)", p.w, "…"))
	}
	off := p.page()
	end := min(off+p.rows(), len(vis))
	for row, i := range vis[min(off, len(vis)):end] {
		it := p.items[i]
		cur := "  "
		if off+row == p.list.Cursor {
			cur = "▶ "
		}
		box := "[ ]"
		if it.on {
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
			notes = append(notes, "未参加")
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
	help := "Space/x 切り替え  n/p ページ  / 検索  Enter 保存して終わる  q 保存せずに終わる"
	if p.filtering {
		help = "文字: 検索する  Enter: 決める  Esc: 検索を消す"
	}
	return append(lines, "", termwidth.Truncate(help, p.w, "…"))
}

type program struct{ p *Picker }

func (m program) Init() tea.Cmd { return nil }

func (m program) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.p.Resize(msg.Width, msg.Height)
	case tea.KeyPressMsg:
		m.p.Key(msg.String())
		if m.p.Done() {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m program) View() tea.View {
	v := tea.NewView(strings.Join(m.p.Lines(), "\n"))
	v.AltScreen = true
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
		out = append(out, "#"+termsafe.PlainLine(c.Name)+"\t"+c.ID)
	}
	return slices.Clip(out)
}
