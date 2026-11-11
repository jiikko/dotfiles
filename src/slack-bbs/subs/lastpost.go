package subs

import (
	"context"
	"maps"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"
	"slack-bbs/board"
)

// LastPost はチャンネルの最終投稿 (最新の発言の ts。発言が無ければ空) と、それを取った時刻。
type LastPost struct {
	Ts string    `json:"ts"`
	At time.Time `json:"at"`
}

// LastPosts は購読の画面が最終投稿を取り直すための口。Get は 1 チャンネルの最新の発言の ts を Slack から取る (発言が無ければ空)。
// Save は取った分を書く (取るたびに呼ぶ。途中で閉じても、次に開いたときは取った分から出す)。
type LastPosts struct {
	Get  func(ctx context.Context, id string) (string, error)
	Save func(map[string]LastPost) error
	Now  func() time.Time
}

// lastPostFresh は、取り直さずに使う最終投稿の新しさ (これより前に取ったものだけを取り直す。開くたびに 186 件を取り直さない)。
const lastPostFresh = time.Hour

// fetch は最終投稿を裏で取り直す状態 (取り終えても残り、Refresh で増えた分を ResumeFetch で取る)。結果は取り直しの実体 (このポインタ) で
// 照合する: 番号で照合すると、閉じてすぐ開き直した別の画面の取り直しと番号が重なり、前の画面の結果 (取り消しのエラー) が新しい画面に入る。
type fetch struct {
	ctx     context.Context
	lp      *LastPosts
	queue   []string        // これから取るチャンネル (先頭から 1 件ずつ)
	failed  map[string]bool // 取れなかったチャンネル (表の欄に出す。数は見出しに出す)
	running bool            // 取る Cmd を出していて、結果を待っている
}

// LastPostMsg は最終投稿を 1 件取った結果。画面を持つ側 (読む画面・slack-bbs sub) は HandleLastPost へ渡す。
type LastPostMsg struct {
	f   *fetch
	id  string
	ts  string
	err error
}

// SetLastPosts は前に取った最終投稿を出す (取り直しの前に、すぐ出す分)。
func (p *Picker) SetLastPosts(m map[string]LastPost) { p.last = maps.Clone(m) }

// StartFetch は「参加している」タブのチャンネルの最終投稿を、1 件ずつ裏で取り直し始める (購読しているもの → 一覧の順。
// lastPostFresh より新しく取ったものは取らない)。返す Cmd を画面の側で流し、届いた LastPostMsg を HandleLastPost へ渡す。
// 取るのは参加しているチャンネルだけ (ユーザーの決定 2026-11-12: 参加していない 389 件まで取ると 12 分ほど Slack の呼び出しの列を使う)。
func (p *Picker) StartFetch(ctx context.Context, lp *LastPosts) tea.Cmd {
	if lp == nil || lp.Get == nil {
		return nil
	}
	if lp.Now == nil { // 時計を渡されなければ実の時刻 (渡された LastPosts は書き換えない)
		c := *lp
		c.Now = time.Now
		lp = &c
	}
	p.fetch = &fetch{ctx: ctx, lp: lp, failed: map[string]bool{}}
	p.queueFetch()
	return p.nextFetch()
}

// queueFetch は、まだ取っていない (古い) 参加しているチャンネルを取る列の後ろに足す (一覧を差し替えたときにも呼ぶ)。
func (p *Picker) queueFetch() {
	f := p.fetch
	if f == nil {
		return
	}
	now := f.lp.Now()
	targets := p.fetchTargets()
	// 取る対象でなくなった (一覧から消えた・参加をやめた) ものは列から外す。取っている最中の先頭は、結果の照合のために残す
	// (結果が失敗でも、対象でなければ「取れなかった」に数えない。HandleLastPost)
	kept := f.queue[:0:0]
	for i, id := range f.queue {
		if targets[id] || i == 0 && f.running {
			kept = append(kept, id)
		}
	}
	f.queue = kept
	var on, rest []string
	for _, it := range p.items {
		if !targets[it.ch.ID] || it.grp != nil || slices.Contains(f.queue, it.ch.ID) {
			continue
		}
		if lp, ok := p.last[it.ch.ID]; ok && now.Sub(lp.At) < lastPostFresh {
			continue
		}
		if it.on {
			on = append(on, it.ch.ID)
		} else {
			rest = append(rest, it.ch.ID)
		}
	}
	f.queue = append(append(f.queue, on...), rest...)
	for id := range f.failed {
		if !targets[id] || slices.Contains(f.queue, id) {
			delete(f.failed, id) // 取る対象でなくなった・列に戻した (見出しの「取れなかった」に数え続けない)
		}
	}
}

// fetchTargets は最終投稿を取るチャンネル (参加していて、アーカイブ済みでない・一覧にある)。
func (p *Picker) fetchTargets() map[string]bool {
	out := map[string]bool{}
	for _, it := range p.items {
		if it.grp == nil && !it.memberOnly && it.ch.IsMember && !it.ch.IsArchived {
			out[it.ch.ID] = true
		}
	}
	return out
}

// nextFetch は列の先頭を取る Cmd (列が空なら取り直しを終える)。
func (p *Picker) nextFetch() tea.Cmd {
	f := p.fetch
	if f == nil || len(f.queue) == 0 {
		if f != nil {
			f.running = false // 取り終えても状態は残す (後の Refresh で増えたチャンネルを ResumeFetch で取る)
		}
		return nil
	}
	f.running = true
	id, ctx, get := f.queue[0], f.ctx, f.lp.Get
	return func() tea.Msg {
		ts, err := get(ctx, id)
		return LastPostMsg{f: f, id: id, ts: ts, err: err}
	}
}

// HandleLastPost は 1 件の結果を表に入れ、次の 1 件を取る Cmd を返す。閉じた後・別の取り直しの結果は捨てる (nil を返して列を止める)。
// 取れなかったチャンネルは前の値のまま残し、数だけを見出しに出す。取り消し (画面を閉じた) なら列を止める。
func (p *Picker) HandleLastPost(msg LastPostMsg) tea.Cmd {
	f := p.fetch
	if f == nil || msg.f != f || len(f.queue) == 0 || f.queue[0] != msg.id || p.done {
		return nil
	}
	f.queue = f.queue[1:]
	switch {
	case msg.err != nil && f.ctx.Err() != nil:
		f.queue, f.running = nil, false
		return nil
	case msg.err != nil:
		if p.fetchTargets()[msg.id] { // 取っている間に対象でなくなったもの (一覧から消えた) は、表に行が無いので数えない
			f.failed[msg.id] = true
		}
	default:
		delete(f.failed, msg.id)
		if p.last == nil {
			p.last = map[string]LastPost{}
		}
		p.last[msg.id] = LastPost{Ts: msg.ts, At: f.lp.Now()}
		if f.lp.Save != nil {
			if err := f.lp.Save(p.last); err != nil {
				p.SetNote("最終投稿をキャッシュに書けなかった: " + err.Error())
			}
		}
	}
	return p.nextFetch()
}

// ResumeFetch は、取り直しの列が止まっている (前の列を取り終えた後に Refresh で足した) なら、次の 1 件を取る Cmd を返す。
// 列が動いている間は nil (動いている列が次を取る。2 本にしない)。Refresh の後に呼ぶ。
func (p *Picker) ResumeFetch() tea.Cmd {
	if p.fetch == nil || p.fetch.running {
		return nil
	}
	return p.nextFetch()
}

// Fetching は最終投稿を取り直している間か (残りの数)。
func (p *Picker) Fetching() int {
	if p.fetch == nil {
		return 0
	}
	return len(p.fetch.queue)
}

// 最終投稿の欄の印。日付 (半角) と同じ列に縦に並ぶので、どれも半角で書く (全角と半角を同じ列に混ぜない)。
// 「—」(U+2014) は端末によって幅が 1 にも 2 にもなるので使わない
const (
	postedFetching = "..."  // 取り直しの列にあって、まだ値が無い
	postedNone     = "-"    // 取らないチャンネル (参加していない) で、値も無い
	postedFailed   = "err"  // 取り直しで取れなかった (前の値があっても出す)
	postedEmpty    = "none" // 発言が 1 つも無い
)

// posted はチャンネルの最終投稿の欄 (取った日付。値が無ければ、取り直し待ち・取れなかった・取らない、のどれか)。
func (p *Picker) posted(id string) string {
	// 取れなかったものは前の値を持っていても err を出す (見出しの「取れなかった n 件」がどの行かを表で分かるように)
	if p.fetch != nil && p.fetch.failed[id] {
		return postedFailed
	}
	if lp, ok := p.last[id]; ok {
		if lp.Ts == "" {
			return postedEmpty
		}
		return postedDate(board.TsTime(lp.Ts), p.nowOr())
	}
	if p.fetch != nil && slices.Contains(p.fetch.queue, id) {
		return postedFetching
	}
	return postedNone
}

// groupPosted はグループの最終投稿 (メンバーで一番新しいもの。どれも分からなければ「—」)。
func (p *Picker) groupPosted(g *Channel) string {
	newest := ""
	for _, m := range g.Members {
		if lp, ok := p.last[m.ID]; ok && board.TsBefore(newest, lp.Ts) {
			newest = lp.Ts
		}
	}
	if newest == "" {
		return postedNone
	}
	return postedDate(board.TsTime(newest), p.nowOr())
}

func (p *Picker) nowOr() time.Time {
	if p.fetch != nil {
		return p.fetch.lp.Now()
	}
	return time.Now()
}

// postedDate は最終投稿の日付 (今年なら「11/11」、それより前なら「2025/12」)。
func postedDate(t, now time.Time) string {
	if t.Year() == now.Year() {
		return t.Format("01/02")
	}
	return t.Format("2006/01")
}
