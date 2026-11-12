package subs

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

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
	// memberOnly はグループの編集で外すためだけに出す行 (アーカイブ済み・一覧から消えたメンバー)。購読の付け外しの画面には出さない
	// (出すと、読めないチャンネルの購読を付けられる)
	memberOnly bool
}

// joined は「参加している」タブに出す行か。グループと、購読していてアーカイブ済み・一覧から消えたチャンネル (外すために出す) もこちら。
func (it item) joined() bool { return it.grp != nil || it.ch.IsMember || it.ch.IsArchived }

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
	tab      int                 // 0: 参加している、1: 参加していない (h / l で切り替える)
	last     map[string]LastPost // チャンネル ID → 最終投稿 (前に取った値。取り直した分から書き換える)
	fetch    *fetch              // 最終投稿を裏で取り直している間の状態 (nil なら取り直していない)
	status   func() string       // 案内の 2 行目の右端に出す文字列 (SetStatus。nil なら出さない)
	base     string              // 画面を作ったときの購読のファイルの印 (SetBase。保存の SaveIf で照合する)
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
	names := map[string]string{}
	for _, c := range all {
		names[c.ID] = c.Name
	}
	member := map[string]Channel{} // どれかのグループのメンバー (アーカイブ・一覧から消えたものもグループの編集で外せるように出す)
	for _, g := range groups {
		for i, mem := range g.grp.Members {
			if n := names[mem.ID]; n != "" {
				g.grp.Members[i].Name = n // 名前を変えたチャンネルは、今の名前にする (本文の頭と分類の入力に出る)
			}
			member[mem.ID] = g.grp.Members[i]
		}
	}
	seen := map[string]bool{}
	for _, c := range all {
		_, isMember := member[c.ID]
		if c.IsArchived && !on[c.ID] && !isMember {
			continue
		}
		seen[c.ID] = true
		p.items = append(p.items, item{ch: c, on: on[c.ID], memberOnly: c.IsArchived && !on[c.ID]})
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
			seen[c.ID] = true
			gone = append(gone, item{ch: source.Channel{ID: c.ID, Name: c.Name, IsArchived: true}, on: true})
		}
	}
	for _, g := range groups {
		for _, mem := range g.grp.Members {
			if !seen[mem.ID] {
				seen[mem.ID] = true
				gone = append(gone, item{ch: source.Channel{ID: mem.ID, Name: mem.Name, IsArchived: true}, memberOnly: true})
			}
		}
	}
	p.items = append(append(groups, gone...), p.items...)
	return p
}

// Refresh は一覧を fresh (取り直した一覧で作った画面) のチャンネルに差し替える。付け外し・検索の語・カーソルのチャンネルは
// 今の画面のものを保つ (前に取った一覧で開いた画面を、取り直しの間に操作していても失わない)。並びは差し替えた時点の購読で組み直す。
func (p *Picker) Refresh(fresh *Picker) {
	curID, editID, delID := "", "", ""
	if vis := p.visible(); len(vis) > 0 {
		curID = p.items[vis[min(p.list.Cursor, len(vis)-1)]].ch.ID
	}
	if p.editing != nil {
		editID = p.editing.ID
	}
	if p.deleting >= 0 {
		delID = p.items[p.deleting].grp.ID
	}
	n := NewPicker(fresh.all, p.Selected())
	p.all, p.items, p.editing, p.deleting = n.all, n.items, nil, -1
	for i, it := range p.items {
		if it.grp != nil && it.grp.ID == editID {
			p.editing = it.grp // 編集しているグループは差し替えた後の行を指し直す
		}
		if it.grp != nil && it.grp.ID == delID {
			p.deleting = i // 消すか確かめているグループも指し直す (確認の板を知らせずに消すと、確認のつもりの Enter で画面が閉じる)
		}
	}
	p.list.Cursor, p.list.Offset = 0, 0
	for i, ix := range p.visible() {
		if p.items[ix].ch.ID == curID {
			p.list.Cursor = i
			break
		}
	}
	p.queueFetch() // 取り直した一覧で増えた参加しているチャンネルも、最終投稿を取る
}

// SetStatus は案内の 2 行目の右端に出す文字列を返す関数を決める (Slack の呼び出しの様子。nil なら出さない)。
func (p *Picker) SetStatus(f func() string) { p.status = f }

// HintLine は案内の項目を左に詰め、right を右端に置いた 1 行 (幅 w。right は style で塗る)。案内は right の分を除いた幅に
// 収まる項目だけを出す (hintfit が優先度の低い項目から落とす)。right も入らない狭い端末では案内だけを出す。
func HintLine(lead string, items []hintfit.Item, right string, w int, style func(string) string) string {
	rw := termwidth.Of(right)
	room := w - termwidth.Of(lead) - rw - 2
	if right == "" || room < 10 {
		return termwidth.Truncate(lead+hintfit.Fit(max(w-termwidth.Of(lead), 1), "  ", items), w, "…")
	}
	left := lead + hintfit.Fit(room, "  ", items)
	if termwidth.Of(left)-termwidth.Of(lead) > room { // 優先度 1 の項目が入らないと、Fit は幅を超えて返す。そのときは右端を出さない
		return termwidth.Truncate(lead+hintfit.Fit(max(w-termwidth.Of(lead), 1), "  ", items), w, "…")
	}
	return left + termwidth.PadSpaces(w-termwidth.Of(left)-rw) + style(right)
}

// SetBase は画面を作った購読の一覧の印 (LoadVersion が返したもの) を覚える。Base は保存のときに SaveIf へ渡す
// (開いている間に別の操作が書き換えた購読を、画面の保存で知らせずに消さない)。Refresh では変えない (購読はこの画面の選んだもの)。
func (p *Picker) SetBase(v string) { p.base = v }

// Base は SetBase で覚えた印。
func (p *Picker) Base() string { return p.base }

// Note は見出しに添える注記 (Refresh で差し替えるときに、取り直した画面の注記を引き継ぐ)。
func (p *Picker) Note() string { return p.note }

// SetNote は見出しに添える一覧の出所を決める (空なら出さない)。
func (p *Picker) SetNote(note string) { p.note = termsafe.PlainLine(note) }

// Resize は端末の大きさを伝える。
func (p *Picker) Resize(w, h int) { p.w, p.h = w, h }

// Done は画面を閉じたか。Saved は保存して閉じたか。
func (p *Picker) Done() bool  { return p.done }
func (p *Picker) Saved() bool { return p.save }

// Selected は購読に選ばれたチャンネル (一覧の並び順) と、その後ろにグループ (LoadVersion と同じ並び)。
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
		editing := p.editing != nil
		if it.grp != nil && editing || it.memberOnly && !editing || editing && it.ch.IsArchived && !p.inEditing(it.ch.ID) {
			// グループの出し入れの間はチャンネルだけを並べる。読めない行 (アーカイブ済み・一覧から消えた) は、編集しているグループの
			// メンバーのときだけ出す (外すため)。購読していて外すために出す行 (memberOnly でない) も、memberOnly で見ずに IsArchived で
			// 見る: 見ないと、グループに読めないチャンネルを入れられる。グループのメンバーとしてだけ残る行は購読の付け外しに出さない
			continue
		}
		if it.joined() != (p.tab == 0) {
			continue
		}
		name := strings.ToLower(it.ch.Name)
		if !slices.ContainsFunc(words, func(w string) bool { return !strings.Contains(name, w) }) {
			out = append(out, i)
		}
	}
	return out
}

// rows は 1 ページの行数 (見出し・検索・タブ・列の見出しの 4 行と、案内の 2 行を除いた残り)。
func (p *Picker) rows() int { return max(p.h-6, 1) }

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
			name := strings.TrimSpace(p.name.String())
			if slices.ContainsFunc(p.items, func(it item) bool { return it.grp != nil && it.grp.Name == name }) {
				p.SetNote("グループ「" + name + "」はもうある (別の名前にする)") // 同じ名前だと、slack-bbs group で名前で指せない
				name = ""
			}
			if name != "" {
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
	case "n":
		if len(vis) > 0 {
			p.list.Cursor = min(p.page()+p.rows(), len(vis)-1) // 次のページの先頭 (最後のページなら末尾)
		}
		return
	case "p":
		p.list.Cursor = max(p.page()-p.rows(), 0)
		return
	case "l", "right", "h", "left": // TUI ガイド §1: タブのある画面では h / l がタブの移動
		tab := 0
		if k == "l" || k == "right" {
			tab = 1
		}
		if tab != p.tab {
			p.tab, p.list.Cursor, p.list.Offset = tab, 0, 0
		}
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
	n, total := 0, 0
	for _, it := range p.items {
		if it.on {
			n++
		}
		if !it.memberOnly {
			total++
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
		head += fmt.Sprintf("購読するチャンネルを選ぶ (%d 件を購読中 / 全 %d 件)", n, total)
	}
	if f := p.fetch; f != nil { // 取り直しの進み具合は、購読の数より後ろに出す (狭い端末では先に切れてよい)
		if len(f.queue) > 0 {
			head += fmt.Sprintf("  最終投稿を取得中 (残り %d)", len(f.queue))
		}
		if len(f.failed) > 0 {
			head += fmt.Sprintf("  最終投稿を取れなかった %d 件 (err)", len(f.failed))
		}
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
	lines = append(lines, termwidth.Truncate(p.tabsLine(), p.w, "…"), p.row("  ", "   ", "チャンネル", "最終投稿", "人数"))
	off := p.page()
	end := min(off+p.rows(), len(vis))
	for row, i := range vis[min(off, len(vis)):end] {
		it := p.items[i]
		cur := "  "
		if off+row == p.list.Cursor {
			cur = "▶ "
		}
		if it.grp != nil {
			lines = append(lines, p.row(cur, "   ", fmt.Sprintf("📂%s (%d チャンネル)", termsafe.PlainLine(it.grp.Name), len(it.grp.Members)),
				p.groupPosted(it.grp), fmt.Sprintf("%dch", len(it.grp.Members))))
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
		if it.ch.IsArchived {
			notes = append(notes, "一覧に無い・アーカイブ済み")
		}
		name := "#" + termsafe.PlainLine(it.ch.Name)
		if len(notes) > 0 {
			name += "  (" + strings.Join(notes, "・") + ")"
		}
		members := ""
		if it.ch.NumMembers > 0 {
			members = strconv.Itoa(it.ch.NumMembers)
		}
		lines = append(lines, p.row(cur, box, name, p.posted(it.ch.ID), members))
	}
	if len(vis) == 0 {
		lines = append(lines, "  (一致するチャンネルがありません)")
	}
	for len(lines) < p.rows()+4 { // 見出し・検索・タブ・列の見出しの 4 行 + 1 ページ (案内の 2 行がタブの件数で上下しない)
		lines = append(lines, "")
	}
	// キーの案内の 2 行。書き方は TUI ガイドの「キー: 説明」で、語は略さない。閉じ方の 2 つ (どちらも抜ける手段 = 優先度 1) は
	// 1 行に並べると幅 40 に入らないので、1 行ずつ末尾に置く。幅に入らない項目は hintfit が優先度の低い方から落とす
	line1 := []hintfit.Item{{Prio: 2, Text: "Space/x: 購読を付け外し"}, {Prio: 2, Text: "/: 名前で検索"}, {Prio: 1, Text: "Enter: 保存して閉じる"}}
	line2 := []hintfit.Item{{Prio: 2, Text: "h/l: タブ"}, {Prio: 3, Text: "n/p: 次・前のページ"}, {Prio: 1, Text: "q: 保存せずに閉じる"}}
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
		line2 = []hintfit.Item{{Prio: 2, Text: "h/l: タブ"}, {Prio: 3, Text: "n/p: 次・前のページ"}}
	case onGroup:
		line1 = []hintfit.Item{{Prio: 2, Text: "e: グループを編集"}, {Prio: 2, Text: "d: グループを消す"}, {Prio: 1, Text: "Enter: 保存して閉じる"}}
	default:
		line1 = append(line1[:2:2], hintfit.Item{Prio: 3, Text: "g: グループを作る"}, line1[2])
	}
	status := ""
	if p.status != nil {
		status = p.status()
	}
	lines = append(lines, HintLine("", line1, "", p.w, nil), HintLine("", line2, status, p.w, func(s string) string { return s }))
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

type program struct {
	p    *Picker
	init []tea.Cmd
}

func (m program) Init() tea.Cmd { return tea.Batch(append(m.init, m.p.statusTick())...) }

// statusTickMsg は右下の Slack の呼び出しの様子を描き直す合図 (何もしないでいる間も、直近 1 分の数が減っていくのを見せる)。
type statusTickMsg struct{}

// statusTick は右下に出すものがあれば、1 秒後の描き直しを張る。
func (p *Picker) statusTick() tea.Cmd {
	if p.status == nil {
		return nil
	}
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return statusTickMsg{} })
}

// ChannelsMsg は裏で取り直したチャンネルの一覧 (Run に渡す Cmd が返す)。P は取り直した一覧で作った画面で、開いている画面を Refresh で差し替える。
type ChannelsMsg struct {
	P   *Picker
	Err error
}

func (m program) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ChannelsMsg:
		if msg.Err != nil {
			m.p.SetNote("一覧を取り直せなかった (前に取った一覧を出している): " + msg.Err.Error())
			return m, nil
		}
		m.p.Refresh(msg.P)
		m.p.SetNote(msg.P.Note()) // 取り直した画面の注記 (キャッシュに書けなかった等)。無ければ「前に取った一覧」の注記を消す
		return m, m.p.ResumeFetch()
	case LastPostMsg:
		return m, m.p.HandleLastPost(msg)
	case statusTickMsg:
		return m, m.p.statusTick()
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

// Run は画面を開き、閉じるまで待つ。opts は Program の設定 (読む画面と同じもの。view.ProgramOptions)。
// cmds は開いたときに裏で流す仕事 (一覧の取り直しは ChannelsMsg、最終投稿は StartFetch の Cmd)。
func Run(p *Picker, opts []tea.ProgramOption, cmds ...tea.Cmd) error {
	_, err := tea.NewProgram(program{p: p, init: cmds}, opts...).Run()
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

// 表の列の幅 (最終投稿は「2025/12」「11/11」と印の「none」が入る幅。人数は 4 桁と「ch」)。
const (
	postedW  = 8
	membersW = 6
)

// row は表の 1 行: カーソル・印・名前・最終投稿・人数。名前の列は残りの幅で、収まらなければ切る。
func (p *Picker) row(cur, box, name, posted, members string) string {
	nameW := max(p.w-termwidth.Of(cur)-termwidth.Of(box)-1-2-postedW-1-membersW, 4)
	name = termwidth.Truncate(name, nameW, "…")
	posted = termwidth.Truncate(posted, postedW, "…")
	line := cur + box + " " + name + termwidth.PadSpaces(nameW-termwidth.Of(name)) + "  " +
		posted + termwidth.PadSpaces(postedW-termwidth.Of(posted)) + " " +
		termwidth.PadSpaces(membersW-termwidth.Of(members)) + members
	return termwidth.Truncate(line, p.w, "…")
}

// tabsLine はタブの行 (今のタブを反転で出す。数は今の検索に一致する行の数)。
func (p *Picker) tabsLine() string {
	cur := p.tab
	var parts []string
	for t, label := range []string{"参加している", "参加していない"} {
		p.tab = t
		text := fmt.Sprintf(" %s (%d) ", label, len(p.visible()))
		if t == cur {
			text = "\x1b[7m" + text + "\x1b[27m"
		}
		parts = append(parts, text)
	}
	p.tab = cur
	return strings.Join(parts, " ")
}
