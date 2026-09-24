package ui

import (
	"math"
	"time"

	tea "charm.land/bubbletea/v2"
	"tuikit/anim"
)

// 下端の板 (詳細・PG の一覧) の開閉の演出。案内の行の裏から上へせり出し、閉じるときは下へ沈む。
// 板は下端に吸着しているので、見せる行数を板の上から k 行に絞るだけで「下から生える」に見える。
// 所要は 600ms、終わり際に減速 (memory の好み「600ms〜1s・操作のメタファー」の下限。開閉は頻繁に押すので短い側)。

type panel int

const (
	panelDetail panel = iota
	panelPG
)

type slide struct {
	start time.Time
	open  bool
}

func slideDuration() time.Duration { return 600 * time.Millisecond }

// shown は板を開いている (開く途中を含む) か。
func (m *Model) shown(p panel) bool {
	if p == panelDetail {
		return m.showDetail
	}
	return m.showSessions
}

// reserved は板の場所を画面に取っておくか (開いている・開閉の途中)。ボードの高さはこれで決める:
// 閉じる途中で先にボードが伸びると、沈んでいく板と合わせて画面の高さを超える。
func (m *Model) reserved(p panel) bool {
	_, sliding := m.slides[p]
	return m.shown(p) || sliding
}

// trackPanels は板の開閉 (キーの処理の前後の差) を見て演出を始める。途中で反転したら今の高さから向かい直す。
func (m *Model) trackPanels(before map[panel]bool) tea.Cmd {
	now := m.now()
	for p, was := range before {
		if m.shown(p) == was {
			continue
		}
		start := now
		if s, ok := m.slides[p]; ok {
			// 今の開き具合 v から続ける。EaseOutCubic は前後対称でないので、時間を裏返すと高さが飛ぶ。
			// 新しい向きで v になる進み具合 e を逆算する (ease(e) = 1-(1-e)^3 の逆関数)
			v := m.openness(s, now)
			var e float64
			if m.shown(p) {
				e = 1 - math.Cbrt(1-v) // 開く: ease(e) = v
			} else {
				e = 1 - math.Cbrt(v) // 閉じる: 1-ease(e) = v
			}
			start = now.Add(-time.Duration(e * float64(slideDuration())))
		}
		m.slides[p] = &slide{start: start, open: m.shown(p)}
	}
	return m.startFrames()
}

// panelState は画面の状態のうち、板の開閉の差を取るための写し。
func (m *Model) panelState() map[panel]bool {
	return map[panel]bool{panelDetail: m.showDetail, panelPG: m.showSessions}
}

// panelLines は板 p の今見せる行。閉じていて演出も無ければ nil。
func (m *Model) panelLines(p panel, build func() []string) []string {
	s, sliding := m.slides[p]
	if !m.shown(p) && !sliding {
		return nil
	}
	lines := build()
	if !sliding {
		return lines
	}
	return lines[:int(m.openness(s, m.now())*float64(len(lines))+0.5)]
}

// openness は板の開き具合 (0 = 閉じ切り、1 = 開き切り)。
func (m *Model) openness(s *slide, now time.Time) float64 {
	f := anim.EaseOutCubic(anim.Elapsed(s.start, now, slideDuration()))
	if !s.open {
		return 1 - f
	}
	return f
}

func (m *Model) pruneSlides(now time.Time) {
	for p, s := range m.slides {
		if anim.Elapsed(s.start, now, slideDuration()) >= 1 {
			delete(m.slides, p)
		}
	}
}
