package ui

// 設定画面のディスクのタブの「残した worktree」(issue 553)。主の経路 (閉じる・消すその場で片付ける = dispatcher/worktree.go) で
// 片付かなかったもの (前からの残り物・退避や削除に失敗したもの・未 commit の変更があるもの) の受け皿。一覧と理由を出し、
// 人が決められるもの (wtclean.Verdict.Ask) は D (消す。取り込み先に無い commit を残してから) / L (残す) で決める。
// 🚨 D は 2 回押して確定する (消すのは戻せない)。決める口 (backend.WorktreeDecider) は、材料を取り直して判定し直してから動かす
// (見た一覧を信じない)。読むのは backend.WorktreeReader (ディスクを測るときに裏で 1 回)。

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"pro-con/backend"
	"pro-con/wtclean"
)

// wtKeyPrefix はディスクのタブの選べる行のうち、残した worktree の行の鍵の頭 (後ろは worktree のパス)。
const wtKeyPrefix = "wt:"

type worktreesMsg struct {
	vs  []wtclean.Verdict
	err error
}

type decidedMsg struct {
	r   wtclean.Result
	err error
}

// wtState は残した worktree の一覧と、決めている途中の状態。
type wtState struct {
	vs      []wtclean.Verdict
	err     error
	read    bool   // 1 度でも読み終えた
	loading bool   // 読んでいる最中
	armed   string // D を 1 回押した worktree のパス (もう一度 D で消す)
	busy    bool   // 消す・残すの最中
}

// fetchWorktrees は残した worktree を裏で読む (git・claude agents・lsof を呼ぶので m.child に入れる)。読めない backend・読んでいる最中なら nil。
func (m *Model) fetchWorktrees() tea.Cmd {
	r, ok := m.be.(backend.WorktreeReader)
	if !ok || m.set.wt.loading {
		return nil
	}
	m.set.wt.loading = true
	return m.child(func() tea.Msg {
		vs, err := r.Worktrees()
		return worktreesMsg{vs: vs, err: err}
	})
}

func (m *Model) onWorktrees(msg worktreesMsg) {
	s := &m.set.wt
	s.vs, s.err, s.read, s.loading = msg.vs, msg.err, true, false
	s.armed = ""
	m.set.cursor = min(m.set.cursor, max(m.settingsRowCount()-1, 0))
}

// keptWorktrees は自動の片付けが消さないもの (人が決められるものを先に)。
func (m *Model) keptWorktrees() []wtclean.Verdict {
	var ask, rest []wtclean.Verdict
	for _, v := range m.set.wt.vs {
		switch {
		case v.Removable():
		case v.Ask:
			ask = append(ask, v)
		default:
			rest = append(rest, v)
		}
	}
	return append(ask, rest...)
}

// worktreeKeys は選べる行の鍵 (人が決められるものだけ)。
func (m *Model) worktreeKeys() []string {
	var out []string
	for _, v := range m.keptWorktrees() {
		if v.Ask {
			out = append(out, wtKeyPrefix+v.Path)
		}
	}
	return out
}

// selectedWorktree は選んでいる行の worktree (選んでいなければ false)。
func (m *Model) selectedWorktree() (wtclean.Verdict, bool) {
	sel := m.settingsSelectable()
	if m.set.tab != tabDisk || m.set.cursor >= len(sel) {
		return wtclean.Verdict{}, false
	}
	path, ok := strings.CutPrefix(sel[m.set.cursor], wtKeyPrefix)
	if !ok {
		return wtclean.Verdict{}, false
	}
	for _, v := range m.set.wt.vs {
		if v.Path == path {
			return v, true
		}
	}
	return wtclean.Verdict{}, false
}

// decideWorktree は D (remove) / L で選んでいる worktree を決める。D は 1 回目で構え、同じ行でもう一度押すと消す。
func (m *Model) decideWorktree(remove bool) tea.Cmd {
	s := &m.set.wt
	v, ok := m.selectedWorktree()
	if !ok {
		return nil
	}
	d, can := m.be.(backend.WorktreeDecider)
	if !can || !m.accepts(backend.OpWorktree) {
		m.refuse("この画面では worktree を消さない・残さない (見ているだけの画面 = pro-con --view)")
		return nil
	}
	if s.busy {
		m.refuse("前に決めた worktree をまだ片付けている")
		return nil
	}
	if remove && s.armed != v.Path {
		s.armed = v.Path
		return nil
	}
	s.armed, s.busy = "", true
	return m.child(func() tea.Msg {
		r, err := d.DecideWorktree(v, remove)
		return decidedMsg{r: r, err: err}
	})
}

func (m *Model) onDecided(msg decidedMsg) tea.Cmd {
	m.set.wt.busy = false
	switch {
	case msg.err != nil:
		m.refuse(msg.err.Error())
	case msg.r.Outcome == wtclean.Failed || msg.r.Outcome == wtclean.Skipped:
		m.refuse(msg.r.Verdict.Name + ": " + string(msg.r.Outcome) + ": " + msg.r.Detail)
	default:
		m.done(msg.r.Verdict.Name + ": " + string(msg.r.Outcome) + " (" + msg.r.Detail + ")")
	}
	return tea.Batch(m.fetchWorktrees(), m.measureDisk())
}

// worktreeLines はディスクのタブの下の「残した worktree」の枠。out に足し、選んでいる行ならその位置を cur に入れる。
// first は選べる行の添字の始まり (置き場の行の数)。
func (m *Model) worktreeLines(out []string, w, first int, cur *int) []string {
	border := fg(51)
	s := &m.set.wt
	kept := m.keptWorktrees()
	title := sgrBold + "残した worktree" + sgrReset + sgrDim + "  閉じる・消すその場で片付かなかったもの · r で読み直す" + sgrReset
	out = append(out, boxTop(border, title, w))
	_, readable := m.be.(backend.WorktreeReader)
	switch {
	case !readable:
		out = append(out, boxLine(border, sgrDim+" この画面では読めない"+sgrReset, w))
	case !s.read:
		out = append(out, boxLine(border, sgrDim+" 読んでいる… (worktree ごとに git を読むので数秒かかる)"+sgrReset, w))
	case s.err != nil && len(s.vs) == 0:
		out = append(out, boxLine(border, sgrYellow+" 読めない: "+s.err.Error()+sgrFgReset, w))
	case len(kept) == 0:
		out = append(out, boxLine(border, sgrDim+" 無い"+sgrReset, w))
	default:
		if s.err != nil {
			out = append(out, boxLine(border, sgrYellow+" 読めないところがある: "+s.err.Error()+sgrFgReset, w))
		}
		i := first
		for _, v := range kept {
			id := orDash(v.CardID)
			row := " " + fit(v.Name, 12) + fit(id, 8) + fit(v.Repo, 12) + v.Why
			if !v.Ask {
				out = append(out, boxLine(border, fg(239)+" "+row+sgrFgReset, w))
				continue
			}
			if i == m.set.cursor {
				*cur = len(out)
				row = sgrCyan + "▌" + sgrFgReset + sgrBold + row + sgrReset
				if s.armed == v.Path {
					row += "  " + sgrYellow + "もう一度 D で消す" + sgrFgReset
				}
			} else {
				row = " " + row
			}
			out = append(out, boxLine(border, row, w))
			i++
		}
		out = append(out, boxLine(border, sgrDim+"   明るい行: D 消す (取り込み先に無い commit を "+wtclean.RemovedRef("<名前>")+" に残してから) · L 残す。暗い行は理由を解いてから"+sgrReset, w))
	}
	if s.busy {
		out = append(out, boxLine(border, sgrYellow+" 片付けている…"+sgrFgReset, w))
	}
	return append(out, boxBottom(border, w))
}
