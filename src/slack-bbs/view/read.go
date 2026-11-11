package view

import (
	"slack-bbs/board"
	"slack-bbs/render"
)

// Reads は既読の印を残す口。印はチャンネルごとに「単位の ID → 読んだときの単位の最後の発言の ts」。nil なら印は画面を閉じるまでしか残らない。
type Reads struct {
	Load func(ch string) (map[string]string, error)
	Save func(ch string, read map[string]string) error
	// LoadHidden / SaveHidden は隠した単位 (x で隠した話題の単位の ID → 隠したときの単位の最後の発言の ts)。nil なら画面を閉じるまで
	LoadHidden func(ch string) (map[string]string, error)
	SaveHidden func(ch string, hidden map[string]string) error
}

// SetReads は既読の印を読み書きする口を付け、今のチャンネルの印を読む。
func (m *Model) SetReads(r *Reads) {
	m.reads = r
	for i := range m.chans {
		m.loadRead(i)
	}
}

// loadRead はチャンネル i の印を読む (読めなければ空から始め、知らせる)。
func (m *Model) loadRead(i int) {
	c := &m.chans[i]
	c.read, c.hidden = map[string]string{}, map[string]string{}
	c.unread, c.rows = nil, nil // 印を置き換えたら未読の数と一覧の行も作り直す (読めない・口が無くて空から始める道も)
	if m.reads == nil {
		return
	}
	if m.reads.LoadHidden != nil {
		if hidden, err := m.reads.LoadHidden(c.ch.ID); err != nil {
			m.toast.Show(c.ch.Label()+" の隠した話題を読めなかった (全部出す): "+err.Error(), false)
		} else if hidden != nil {
			c.hidden = hidden
		}
	}
	read, err := m.reads.Load(c.ch.ID)
	if err != nil {
		m.toast.Show(c.ch.Label()+" の既読を読めなかった (未読から始める): "+err.Error(), false)
		return
	}
	if read != nil {
		c.read = read
	}
}

// unitUnread は単位に、印より後の他人の期間内の発言か、印より後の他人の言及 (ms。別のチャンネルでこの単位の発言のリンクを貼った発言) があるか。
// 自分の発言・自分の言及と、文脈として出した期間前の発言は数えない (自分が返信しただけのスレッドを未読にしない)。
// 言及の新しさは ts で決める: 既読の後に、既読の印より前の ts の発言を編集してリンクを足しても未読に戻らない (取得のたびに見た言及の
// 集合を印に残せば拾えるが、印の形が変わる。編集でリンクを足す形は稀とみて受け入れる)。
func unitUnread(u board.Unit, read map[string]string, ms []render.Mention) bool {
	mark, ok := read[u.ID]
	for _, msg := range u.Msgs {
		if msg.New && !msg.Mine && (!ok || board.TsBefore(mark, msg.Ts)) {
			return true
		}
	}
	for _, mn := range ms {
		if !mn.Mine && (!ok || board.TsBefore(mark, mn.Ts)) {
			return true
		}
	}
	return false
}

// unitLast は単位を読んだ印に書く ts: 単位の最後の発言か、それより後の言及。
func unitLast(u board.Unit, ms []render.Mention) string {
	last := u.Last()
	for _, mn := range ms {
		if board.TsBefore(last, mn.Ts) {
			last = mn.Ts
		}
	}
	return last
}

func topicUnread(t board.Topic, read map[string]string, mentions map[string][]render.Mention) bool {
	for _, u := range t.Units {
		if unitUnread(u, read, mentions[u.ID]) {
			return true
		}
	}
	return false
}

// unreadCount はチャンネル i の未読の話題の数 (まだ読めていなければ 0)。
func (m *Model) unreadCount(i int) int {
	c := &m.chans[i]
	if c.page == nil {
		return 0
	}
	if c.unread != nil {
		return *c.unread
	}
	n := 0
	for _, t := range c.page.Topics {
		if !topicHidden(t, c.hidden) && topicUnread(t, c.read, c.mentions) {
			n++
		}
	}
	c.unread = &n
	return n
}

// setRead は選んだ話題を既読 (read が真) か未読にして残す。変わらなければ書かない。
func (m *Model) setRead(read bool) {
	t, ok := m.selected()
	if !ok {
		return
	}
	c := &m.chans[m.cur]
	changed := false
	for _, u := range t.Units {
		if read {
			// 印は進めるだけ (前回のキャッシュの古い板の上で選んでも、後の返信まで読んだ印を戻さない)
			last := unitLast(u, c.mentions[u.ID])
			if mark, had := c.read[u.ID]; !had || board.TsBefore(mark, last) {
				c.read[u.ID] = last
				changed = true
			}
		} else if _, had := c.read[u.ID]; had {
			delete(c.read, u.ID)
			changed = true
		}
	}
	if !changed {
		return
	}
	c.unread = nil
	if m.reads == nil {
		return
	}
	if err := m.reads.Save(c.ch.ID, c.read); err != nil {
		m.toast.Show("既読を保存できなかった: "+err.Error(), false)
	}
}

// toggleRead は u キー: 未読なら既読に、既読なら未読に戻す。
func (m *Model) toggleRead() {
	t, ok := m.selected()
	if !ok {
		return
	}
	c := &m.chans[m.cur]
	if topicUnread(t, c.read, c.mentions) {
		m.setRead(true)
		m.toast.Show("この話題を既読にした", true)
		return
	}
	if !topicUnread(t, nil, c.mentions) { // 印を消しても未読にならないなら、印は消さない (後で付いた返信の判定に要る)
		m.toast.Show("この話題には期間内の他の人の発言が無いので、未読にできない", false)
		return
	}
	m.setRead(false)
	m.toast.Show("この話題を未読に戻した", true)
}

// topicHidden は話題が隠れているか: 全部の単位を x で隠していて、隠した後に他の人が自分を名指しした発言が無い
// (名指しされたら隠したままにしない: 自分への用件を見落とす)。隠した後に付いた他の返信では戻さない (読まないと決めた話題なので)。
func topicHidden(t board.Topic, hidden map[string]string) bool {
	if len(t.Units) == 0 {
		return false
	}
	for _, u := range t.Units {
		mark, ok := hidden[u.ID]
		if !ok {
			return false
		}
		for _, msg := range u.Msgs {
			if msg.ToMe && !msg.Mine && board.TsBefore(mark, msg.Ts) {
				return false
			}
		}
	}
	return true
}

// toggleHidden は x キー: 選んだ話題を隠す。隠した話題も出している (z) ときに隠れた話題で押すと、隠すのをやめる。
func (m *Model) toggleHidden() {
	t, ok := m.selected()
	if !ok {
		return
	}
	c := &m.chans[m.cur]
	hide := !topicHidden(t, c.hidden)
	for _, u := range t.Units {
		if hide {
			c.hidden[u.ID] = unitLast(u, c.mentions[u.ID])
		} else {
			delete(c.hidden, u.ID)
		}
	}
	c.rows, c.unread = nil, nil
	switch {
	case !hide:
		m.toast.Show("この話題を隠すのをやめた", true)
	case m.showHidden:
		m.toast.Show("この話題を隠した (z で隠した話題を出すのをやめる)", true)
	default:
		m.toast.Show("この話題を隠した (z で隠した話題も出し、その上で x を押すと戻す)", true)
		m.body = false  // 隠した話題の本文を開いたままにしない
		m.pager.Reset() // カーソルは次の話題へ移る。隠した話題を送った位置から読ませない
	}
	m.fitList(1)
	if m.reads == nil || m.reads.SaveHidden == nil {
		return
	}
	if err := m.reads.SaveHidden(c.ch.ID, c.hidden); err != nil {
		m.toast.Show("隠した話題を保存できなかった: "+err.Error(), false)
	}
}

// toggleShowHidden は z キー: 隠した話題も一覧に出すか (出すときは淡い色で出す)。
func (m *Model) toggleShowHidden() {
	keep := ""
	if t, ok := m.selected(); ok {
		keep = t.Units[0].ID
	}
	m.showHidden = !m.showHidden
	for i := range m.chans {
		m.chans[i].rows = nil
	}
	if len(m.chans) > 0 && !m.reselect(keep) {
		m.pager.Reset() // 選んでいた話題が隠れた: カーソルは別の話題に移る
	}
	if m.showHidden {
		m.toast.Show("隠した話題も出す (淡い色。x で隠すのをやめる)", true)
	} else {
		m.toast.Show("隠した話題を出さない", true)
	}
}

// zHint は z の案内 (押したときに起きること)。
func zHint(showing bool) string {
	if showing {
		return "隠した話題を出さない"
	}
	return "隠した話題も出す"
}
