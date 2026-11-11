package anim

import "time"

// Slide は画面全体を開閉する演出の状態。zero value = 閉じて静止。状態機械は Transition が持つ。
//
// Transition と違い、途中で向きを変えても、その位置から引き返さない。開く途中で閉じたら開いた姿から
// 閉じ始め、開くときは毎回閉じた姿から始める。閉じる途中で開き直すときは、使う側が先に EndClose で
// 片付けを済ませる (済ませずに呼んだ Open は何もしない)。片付けを「閉じ切った」1 か所に寄せた画面で、
// 閉じかけの画面の中身をそのまま使い回さないため。
//
// 🚨 片付けは tick から CloseLanded を見て行う前提なので、tick を回す判定には Unsettled を使う
// (閉じる途中は、時間が過ぎても EndClose されるまで真)。
type Slide struct{ t Transition }

// Open は閉じた姿から開く演出を始める。閉じる途中なら何もしない: 片付け (EndClose) を飛ばして
// 開き直すと、閉じ切ったときの片付けが一度も走らない。
func (s *Slide) Open(now time.Time, d time.Duration) {
	if s.Closing() {
		return
	}
	s.t = Transition{}
	s.t.Open(now, d)
}

// Show は演出せずに開いて静止した状態にする。
func (s *Slide) Show() { s.t = NewOpen() }

// Close は今の状態によらず、開いた姿から閉じる演出を始める (開く途中でも、その位置から引き返さない)。
func (s *Slide) Close(now time.Time, d time.Duration) {
	s.t = NewOpen()
	s.t.Close(now, d)
}

// Closing は閉じる演出の途中か (EndClose がまだ呼ばれていない)。
func (s *Slide) Closing() bool { return s.t.Phase() == Closing }

// CloseLanded は閉じる演出が時間どおり着地したか (片付けてよい合図。状態は進めない)。
func (s *Slide) CloseLanded(now time.Time) bool { return s.Closing() && s.t.Progress(now) >= 1 }

// EndClose は閉じる演出を閉じ切った状態にする。閉じる途中でなければ何もせず false を返す。
func (s *Slide) EndClose() bool {
	if !s.Closing() {
		return false
	}
	s.t.Finish()
	return true
}

// LandOpen は開く演出を即座に着地させる (キー操作を待たせない)。閉じる途中には効かない。
func (s *Slide) LandOpen() {
	if s.t.Phase() == Opening {
		s.t.Finish()
	}
}

// Unsettled は tick を回し続けるべきか (Transition.Unsettled)。
func (s *Slide) Unsettled(now time.Time) bool { return s.t.Unsettled(now) }

// Progress は見えている割合 0..1 (緩急なし)。開く途中は 0→1、閉じる途中は 1→0。
// 演出していないときは 1 (閉じて静止していても 1。描くかどうかは使う側が別に持つ)。
func (s *Slide) Progress(now time.Time) float64 {
	p := s.t.Progress(now)
	if s.Closing() {
		return 1 - p
	}
	return p
}
