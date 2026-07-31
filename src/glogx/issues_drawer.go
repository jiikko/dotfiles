package main

// issue 本文の「引き出し (drawer)」演出。Notion の peek のように、issue を選ぶと本文が左から
// にゅっと開いて画面の大半を占め、閉じるときは同じ動きを逆再生する (ユーザー要望 2026-07-31)。
//
// 以前は本文が一覧を全画面で置き換えていたため、「今どの一覧のどこから開いたか」が画面から
// 消えていた。drawer なら右側に一覧が残り、開閉が位置関係として見える。
//
// 演出はフレーム数でなく壁時計で進める (viewer の開く演出 animStart と同じ方式)。tick の周期が
// 変わっても所要時間が変わらないため。

import (
	"math"
	"time"
)

const (
	// issuesDrawerRatio は開ききったときに本文が占める幅の割合 (残りに一覧が見える)。
	issuesDrawerRatio = 0.8
	// issuesDrawerDuration は開閉の所要時間。開く演出 (issuesAnimDuration = 700ms) より速いのは、
	// issue を次々に見るときに往復 1.4s は待たされる感じになるため。⚠️ 変えるならここ 1 箇所。
	issuesDrawerDuration = 450 * time.Millisecond
)

// drawerPhase は引き出しの状態。
type drawerPhase uint8

const (
	drawerClosed  drawerPhase = iota // 本文なし (一覧だけ)
	drawerOpening                    // 開く途中
	drawerOpen                       // 開ききって静止
	drawerClosing                    // 閉じる途中 (本文はまだ生きている)
)

// issuesDrawer は本文引き出しの開閉アニメの状態。zero value = 閉じている。
//
// ⚠️ 閉じる演出のあいだ本文 (issuesView.open / body) を消してはいけない: 逆再生で中身が
// 見えている必要がある。実際の破棄は演出が着地してから (issuesView.settleDrawer)。
type issuesDrawer struct {
	phase   drawerPhase
	started time.Time // 現在の演出の開始時刻
}

// open は開く演出を始める。既に開いている (静止中) なら何もしない。
func (d *issuesDrawer) open(now time.Time) {
	d.phase, d.started = drawerOpening, now
}

// startClose は閉じる演出を始める。開ききる前に閉じた場合は、今見えている幅から折り返すよう
// 開始時刻をずらす (幅が飛ばずに連続する)。
func (d *issuesDrawer) startClose(now time.Time) {
	if d.phase == drawerClosed || d.phase == drawerClosing {
		return
	}
	elapsed := issuesDrawerDuration
	if d.phase == drawerOpening {
		// 開きかけの進捗 p で閉じ始めるなら、閉じ演出は残り p ぶんから始める
		if p := d.rawProgress(now); p < 1 {
			elapsed = time.Duration(p * float64(issuesDrawerDuration))
		}
	}
	d.phase, d.started = drawerClosing, now.Add(-(issuesDrawerDuration - elapsed))
}

// rawProgress は現在の演出の素の進捗 0..1 (演出中でなければ 1)。
func (d *issuesDrawer) rawProgress(now time.Time) float64 {
	if d.phase != drawerOpening && d.phase != drawerClosing {
		return 1
	}
	p := float64(now.Sub(d.started)) / float64(issuesDrawerDuration)
	return max(min(p, 1), 0)
}

// animating は演出の途中か (tick を回し続ける判定に使う)。
func (d *issuesDrawer) animating(now time.Time) bool {
	switch d.phase {
	case drawerOpening, drawerClosing:
		return d.rawProgress(now) < 1
	case drawerClosed, drawerOpen:
		return false
	}
	return false
}

// settle は演出が終わっていれば静止状態へ遷移し、閉じ切ったなら true を返す
// (呼び出し側が本文を破棄する合図)。
func (d *issuesDrawer) settle(now time.Time) (closed bool) {
	if d.rawProgress(now) < 1 {
		return false
	}
	switch d.phase {
	case drawerOpening:
		d.phase = drawerOpen
	case drawerClosing:
		d.phase = drawerClosed
		return true
	case drawerClosed, drawerOpen:
	}
	return false
}

// finish は演出を即座に着地させる (キー操作は待たせない)。閉じ切ったなら true。
func (d *issuesDrawer) finish() (closed bool) {
	switch d.phase {
	case drawerOpening:
		d.phase = drawerOpen
	case drawerClosing:
		d.phase = drawerClosed
		return true
	case drawerClosed, drawerOpen:
	}
	return false
}

// width は今フレームの引き出しの幅 (区切り線を含む)。total は画面の内容幅。
//
// 開くときは easeOutCubic で終点に向けて減速し、閉じるときはその逆再生 (進捗を反転するだけ)
// にする。別のカーブを使うと「開いた動きと閉じる動きが違う」ちぐはぐさが出る。
func (d *issuesDrawer) targetWidth(total int) int {
	return int(math.Round(float64(total) * issuesDrawerRatio))
}

func (d *issuesDrawer) width(total int, now time.Time) int {
	target := d.targetWidth(total)
	switch d.phase {
	case drawerClosed:
		return 0
	case drawerOpen:
		return target
	case drawerOpening:
		return int(math.Round(float64(target) * easeOutCubicFloat(d.rawProgress(now))))
	case drawerClosing:
		return int(math.Round(float64(target) * easeOutCubicFloat(1-d.rawProgress(now))))
	}
	return 0
}

// composeDrawer は「一覧 (base) の上に、左から幅 w の本文 (panel) を重ねた」窓を作る。
//
// panel は最終幅で整形済みのものを渡し、ここでは幅 w に切るだけにする。演出の途中幅で整形し直すと
// (a) 毎フレーム本文が折り返し直されて文字が踊り、(b) 幅ごとにキャッシュを持つ Body が毎フレーム
// 再整形される (1 回数 ms × 開閉のフレーム数)。切るだけなら「板が開くと中身が見えてくる」動きになる。
//
// 右端 1 桁は区切り線にして、本文と一覧の境界を示す (影を落とすと 1 桁ぶん本文が痩せるうえ、
// 演出中に毎フレーム影の位置が動いて目が疲れる)。幅 0 のときは base をそのまま返す。
func composeDrawer(base, panel []string, w, total int, colored bool) []string {
	if w <= 0 {
		return base
	}
	if w > total {
		w = total
	}
	// ⚠️ 区切りに "│" を使わない: 本文の pager はスクロールバーを右端に "│" で描くので、
	// 隣り合って "││" になり壊れて見える (実測)。細いブロックなら板の縁として区別できる。
	sep := paint("▏", ansiDim, colored)
	out := make([]string, len(base))
	for i, b := range base {
		var p string
		if i < len(panel) {
			p = panel[i]
		}
		// 本文は w-1 桁へ切る。⚠️ clipToWidth でなく truncateKeepANSI を使う: 前者は末尾に "…" を
		// 付けるので、開く途中の全行に "…" が並んで「切れている」ように見える (板が開いて中身が
		// 現れる動きにしたい)。どちらも SGR は保つ。
		line := truncateKeepANSI(p, max(w-1, 0))
		pad := max(w-1-dispWidth(line), 0)
		// 切り口で開いたままの色が右の一覧へ滲まないよう reset を挟む (overlayBoxRight と同じ規律)。
		if colored {
			line += ansiReset
		}
		line += padSpaces(pad)
		if w >= 1 {
			line += sep
		}
		out[i] = line + dropToColumn(b, w)
	}
	return out
}
