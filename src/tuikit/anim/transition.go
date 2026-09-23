// Package anim は端末 UI の演出に使う状態機械と緩急カーブ。描画フレームワークに依存しない
// (bubbletea 等の tick は使う側が回す。README の「使う側の約束」)。
package anim

import "time"

// Phase は開閉演出の状態。
type Phase uint8

const (
	Closed  Phase = iota // 閉じて静止 (zero value)
	Opening              // 開く途中
	Open                 // 開いて静止
	Closing              // 閉じる途中
)

// Transition は「開く / 閉じる」を壁時計で進める状態機械。zero value = 閉じて静止。
//
// フレーム数でなく壁時計で進めるのは、tick の周期が変わっても所要時間が変わらないため。
// 所要時間はフィールドに持たせず、動き始めるときに渡す: zero value のまま使える (構造体の
// 複合リテラルに埋めても「閉じている」以外の意味を持たない) ようにするため。
//
// 🚨 閉じる演出のあいだ、中身 (本文など) を捨ててはいけない: 逆再生で中身が見えている必要がある。
// 捨てるのは Settle / Finish が closed=true を返してから。
type Transition struct {
	phase    Phase
	started  time.Time
	duration time.Duration
}

// NewOpen は開いて静止した状態を返す (画面全体の閉じる演出のように、最初から開いているもの用)。
func NewOpen() Transition { return Transition{phase: Open} }

// Phase は現在の状態を返す。
func (t *Transition) Phase() Phase { return t.phase }

// Open は開く演出を始める。閉じる途中なら、その位置から逆再生する (見えている位置が跳ばない)。
// 既に開いている / 開く途中なら何もしない。
func (t *Transition) Open(now time.Time, d time.Duration) {
	switch t.phase {
	case Closed:
		t.phase, t.started, t.duration = Opening, now, d
	case Closing:
		t.reverse(Opening, now, d)
	case Opening, Open:
	}
}

// Close は閉じる演出を始める。開く途中なら、その位置から逆再生する。
// 既に閉じている / 閉じる途中なら何もしない。
func (t *Transition) Close(now time.Time, d time.Duration) {
	switch t.phase {
	case Open:
		t.phase, t.started, t.duration = Closing, now, d
	case Opening:
		t.reverse(Closing, now, d)
	case Closed, Closing:
	}
}

// reverse は途中の演出を反対向きへ切り替える。残りの割合だけ時計を巻き戻して始めるので、
// 切り替えた瞬間の見た目 (Openness) が連続する。
func (t *Transition) reverse(to Phase, now time.Time, d time.Duration) {
	p := t.Progress(now)
	t.phase, t.duration = to, d
	t.started = now.Add(-time.Duration((1 - p) * float64(d)))
}

// Progress は現在の演出の素の進捗 0..1。演出中でなければ 1。
func (t *Transition) Progress(now time.Time) float64 {
	if t.phase != Opening && t.phase != Closing {
		return 1
	}
	if t.duration <= 0 {
		return 1
	}
	p := float64(now.Sub(t.started)) / float64(t.duration)
	return max(min(p, 1), 0)
}

// Animating は演出の途中か (使う側が tick を回し続ける判定に使う)。
func (t *Transition) Animating(now time.Time) bool {
	return (t.phase == Opening || t.phase == Closing) && t.Progress(now) < 1
}

// Settle は演出が終わっていれば静止状態へ進める。閉じ切ったときだけ true を返す
// (中身を捨ててよい合図)。描画とキー処理の両方から呼んでよい (どちらが先でも状態が進む)。
func (t *Transition) Settle(now time.Time) (closed bool) {
	if t.Progress(now) < 1 {
		return false
	}
	return t.Finish()
}

// Finish は演出を即座に着地させる (キー操作を演出の終わりまで待たせないため)。
// 閉じ切ったときだけ true を返す。
func (t *Transition) Finish() (closed bool) {
	switch t.phase {
	case Opening:
		t.phase = Open
	case Closing:
		t.phase = Closed
		return true
	case Closed, Open:
	}
	return false
}

// Openness は開き具合 0..1 を ease で写して返す (0 = 閉じている、1 = 開ききっている)。
//
// 閉じるときは進捗を反転して同じカーブに通す = 開く動きの逆再生になる。別のカーブにすると
// 「開いた動きと閉じる動きが違う」ちぐはぐさが出る。
func (t *Transition) Openness(now time.Time, ease func(float64) float64) float64 {
	switch t.phase {
	case Opening:
		return ease(t.Progress(now))
	case Closing:
		return ease(1 - t.Progress(now))
	case Open:
		return 1
	case Closed:
	}
	return 0
}
