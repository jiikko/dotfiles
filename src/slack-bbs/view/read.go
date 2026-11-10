package view

import (
	"slack-bbs/board"
)

// Reads は既読の印を残す口。印はチャンネルごとに「単位の ID → 読んだときの単位の最後の発言の ts」。nil なら印は画面を閉じるまでしか残らない。
type Reads struct {
	Load func(ch string) (map[string]string, error)
	Save func(ch string, read map[string]string) error
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
	c.read = map[string]string{}
	c.unread = nil // 印を置き換えたら未読の数も数え直す (読めない・口が無くて空から始める道も)
	if m.reads == nil {
		return
	}
	read, err := m.reads.Load(c.ch.ID)
	if err != nil {
		m.toast.Show("#"+c.ch.Name+" の既読を読めなかった (未読から始める): "+err.Error(), false)
		return
	}
	if read != nil {
		c.read = read
	}
}

// unitUnread は単位に、印より後の他人の期間内の発言があるか。自分の発言と、文脈として出した期間前の発言は数えない
// (自分が返信しただけのスレッドを未読にしない)。
func unitUnread(u board.Unit, read map[string]string) bool {
	mark, ok := read[u.ID]
	for _, msg := range u.Msgs {
		if msg.New && !msg.Mine && (!ok || board.TsBefore(mark, msg.Ts)) {
			return true
		}
	}
	return false
}

func topicUnread(t board.Topic, read map[string]string) bool {
	for _, u := range t.Units {
		if unitUnread(u, read) {
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
		if topicUnread(t, c.read) {
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
			if mark, had := c.read[u.ID]; !had || board.TsBefore(mark, u.Last()) {
				c.read[u.ID] = u.Last()
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
	if topicUnread(t, c.read) {
		m.setRead(true)
		m.toast.Show("この話題を既読にした", true)
		return
	}
	if !topicUnread(t, nil) { // 印を消しても未読にならないなら、印は消さない (後で付いた返信の判定に要る)
		m.toast.Show("この話題には期間内の他の人の発言が無いので、未読にできない", false)
		return
	}
	m.setRead(false)
	m.toast.Show("この話題を未読に戻した", true)
}
