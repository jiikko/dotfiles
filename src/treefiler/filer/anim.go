package filer

import "math"

// tween は時間で進む補間。初動を速く、終わりをゆっくりにする (ease-out quint。spec §0.1)。
// 目標が変わったらその時点の値から始め直す。キー入力では snap で終点へ飛ばす。
//
// treebeard のばね (spec §4.1) から替えた理由: 終わりの長さを時間で決めたい・キーで即座に終点へ飛ばしたい
// (ユーザー要望 2026-10-08)。ばねへ戻すなら「止まったら目標へちょうど合わせる」が要る (spec §0.2 の 🚨)。
type tween struct {
	v, from, to, el, dur float64
	init                 bool
}

func easeOut(t float64) float64 { return 1 - math.Pow(1-t, 5) }

// step は dt 秒進め、まだ動いているかを返す。
func (s *tween) step(target, dur, dt float64) bool {
	if !s.init || target != s.to {
		s.from, s.to, s.el, s.dur, s.init = s.v, target, 0, dur, true
	}
	if s.from == s.to {
		s.v = s.to
		return false
	}
	s.el += dt
	t := math.Min(1, s.el/s.dur)
	s.v = s.from + (s.to-s.from)*easeOut(t)
	return t < 1
}

// snap は動きの途中を終点へ飛ばす。
func (s *tween) snap() {
	if s.init {
		s.v, s.el = s.to, s.dur
	}
}

// 長さ (秒)。「終わりの減速が見える時間」で決めた: quint は最後の数 % が 1 桁未満で画面に出ないので、
// 0.30 秒では減速が 90ms しか見えなかった (モックを tmux で実測。spec §0.1 の表)。
const (
	moveDur    = 0.50
	camDur     = 0.55
	popupDur   = 0.50
	scrollDur  = 0.30
	fadeInDur  = 0.25
	fadeOutDur = 0.35
)
