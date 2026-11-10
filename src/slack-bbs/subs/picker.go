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

func (p *Picker) visible() []int {
	var out []int
	f := strings.ToLower(p.filter)
	for i, it := range p.items {
		if f == "" || strings.Contains(strings.ToLower(it.ch.Name), f) {
			out = append(out, i)
		}
	}
	return out
}

func (p *Picker) rows() int { return max(p.h-4, 1) }

// Key はキー 1 つを処理する。
func (p *Picker) Key(k string) {
	if p.filtering {
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
		p.list.Cursor, p.list.Offset = 0, 0
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
	lines := []string{fmt.Sprintf("購読するチャンネルを選ぶ (%d 件を購読中 / 全 %d 件)", n, len(p.items))}
	switch {
	case p.filtering:
		lines = append(lines, "絞り込み: "+termsafe.PlainLine(p.filter)+"▏")
	case p.filter != "":
		lines = append(lines, "絞り込み: "+termsafe.PlainLine(p.filter)+" (/ で変える)")
	default:
		lines = append(lines, "")
	}
	end := min(p.list.Offset+p.rows(), len(vis))
	for row, i := range vis[p.list.Offset:end] {
		it := p.items[i]
		cur := "  "
		if p.list.Offset+row == p.list.Cursor {
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
	help := "Space / x: 切り替え  /: 絞り込み  Enter: 保存して終わる  q: 保存せずに終わる"
	if p.filtering {
		help = "文字: 絞り込む  Enter: 決める  Esc: 絞り込みを消す"
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
