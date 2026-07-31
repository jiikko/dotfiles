package main

// issue 本文の「引き出し (drawer)」演出。issue を選ぶと本文が画面の右外から飛び出して左へ
// 滑り込み、画面の 8 割を占めて止まる。閉じるときは同じ動きの逆再生 (ユーザー要望 2026-07-31)。
//
// ⚠️ 幅を 0 から伸ばす実装にしない: それは「ページが開く」動きであって「飛び出してくる」動きに
// ならない (最初の実装がこれで、イメージと違うと指摘を受けた)。板は最終幅のまま位置だけを
// 動かし、画面外から入ってくるように見せる。
//
// 以前は本文が一覧を全画面で置き換えていたため、「今どの一覧のどこから開いたか」が画面から
// 消えていた。右寄せの drawer なら左に一覧の先頭 (番号・状態・カテゴリ) が残り、開閉が
// 位置関係として見える。
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
	// issuesDrawerExtra は比率に上乗せする桁数 (ユーザー要望 2026-07-31)。比率を上げるのでなく
	// 固定の上乗せにするのは、狭い端末でも一覧側が同じ桁数だけ残るようにするため。
	issuesDrawerExtra = 10
	// issuesDrawerMinList は左に残す一覧の最小幅 (溝 + 番号 = "→ 014 " が見える程度)。本文が
	// 画面を食い切って「どこから開いたか」が消えるのを防ぐ下限。
	issuesDrawerMinList = 8
	// issuesDrawerMaxPeek は左に残す一覧の最大幅。⚠️ 比率だけで決めると画面が広いほど一覧が
	// 場所を食う: popup は端末幅の 90% (_tmux.conf) なので、312 桁の端末では一覧に 50 桁超を
	// 割いていた (ユーザー報告 2026-07-31「まだ一覧が見えている」)。覗き見に要るのは「どの行から
	// 開いたか」が分かる幅だけなので、番号・状態・カテゴリが見える 18 桁で止める
	// ("→ 014 ○ research" = 18)。以降の幅は全部本文へ回す。
	issuesDrawerMaxPeek = 18
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

// width は今フレームで画面に見えている引き出しの幅 (区切り線を含む)。板は右外から入ってくるので
// 「見えている幅」= 画面右端から板の左辺までの距離になる。total は画面の内容幅。
//
// 開くときは easeOutCubic で終点に向けて減速し、閉じるときはその逆再生 (進捗を反転するだけ)
// にする。別のカーブを使うと「開いた動きと閉じる動きが違う」ちぐはぐさが出る。
func (d *issuesDrawer) targetWidth(total int) int {
	if total <= issuesDrawerMinList*2 {
		return total // 覗き見の余地が無い狭さでは全幅を本文に使う (中途半端に削らない)
	}
	ratioOnly := int(math.Round(float64(total) * issuesDrawerRatio))
	// 一覧の覗き見は minList..maxPeek に収める。上限があるので画面が広いほど本文が伸びる
	// (比率のままだと一覧が伸びてしまう)。
	peek := max(min(total-(ratioOnly+issuesDrawerExtra), issuesDrawerMaxPeek), issuesDrawerMinList)
	w := total - peek
	// ⚠️ 比率ぶんより狭くはしない — 覗き見の下限が効いて「広げたのに本文が狭くなる」のは本末転倒
	// (総幅 20 桁で 16 → 12 に縮む実測)。狭い端末では覗き見が minList を下回る方を選ぶ。
	return max(w, ratioOnly)
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

// composeDrawer は「一覧 (base) の右側に、幅 w だけ見えている本文 (panel) を重ねた」窓を作る。
// 板は右外から入ってくるので、w が増えるほど左辺 (total-w) が左へ動く。
//
// panel は最終幅で整形済みのものを渡し、ここでは見えている幅へ切るだけにする。位置で動かすので
// 折り返しは変わらない (板が動くのであって中身が組み直されるのではない)。幅ごとにキャッシュを
// 持つ Body を毎フレーム再整形しない効果もある。
//
// 板の左辺 1 桁は区切り線にして一覧との境界を示す (影を落とすと演出中に毎フレーム影の位置が
// 動いて目が疲れる)。幅 0 のときは base をそのまま返す。
func composeDrawer(base, panel []string, w, total int, colored bool) []string {
	if w <= 0 {
		return base
	}
	if w > total {
		w = total
	}
	left := total - w // 板の左辺 = 一覧が見えている幅
	// ⚠️ 区切りに "│" を使わない: 本文の pager はスクロールバーを右端に "│" で描くので、
	// 隣り合って "││" になり壊れて見える (実測)。細いブロックなら板の縁として区別できる。
	sep := paint("▏", ansiDim, colored)
	out := make([]string, len(base))
	for i, b := range base {
		var p string
		if i < len(panel) {
			p = panel[i]
		}
		// 左に見えている一覧。切り口で開いたままの色が板へ滲まないよう reset を挟む
		// (overlayBoxRight と同じ規律)。
		line := truncateKeepANSI(b, left)
		if colored {
			line += ansiReset
		}
		line += padSpaces(max(left-dispWidth(line), 0))
		if left < total {
			line += sep
		}
		// 板は左辺から見えていく = 本文の左側から現れる。⚠️ clipToWidth でなく truncateKeepANSI:
		// 前者は末尾に "…" を付けるので、滑り込み中の全行に "…" が並んで「切れている」ように見える。
		vis := truncateKeepANSI(p, max(w-1, 0))
		if colored {
			vis += ansiReset
		}
		// 本文が板幅より短い行 (空行・短い段落) でも板の幅ぶんは埋める。埋めないと合成後の行が
		// 画面幅より短くなり、板の右側に下地が透けたように見える。
		out[i] = line + vis + padSpaces(max(w-1-dispWidth(vis), 0))
	}
	return out
}
