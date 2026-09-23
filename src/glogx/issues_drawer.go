package main

// issue 本文の「引き出し (drawer)」演出。issue を選ぶと本文が画面の右外から飛び出して左へ
// 滑り込み、画面の 8 割を占めて止まる。閉じるときは同じ動きの逆再生 (ユーザー要望 2026-07-31)。
//
// 🚨 幅を 0 から伸ばす実装にしない: それは「ページが開く」動きであって「飛び出してくる」動きに
// ならない (最初の実装がこれで、イメージと違うと指摘を受けた)。板は最終幅のまま位置だけを
// 動かし、画面外から入ってくるように見せる。
//
// 以前は本文が一覧を全画面で置き換えていたため、「今どの一覧のどこから開いたか」が画面から
// 消えていた。右寄せの drawer なら左に一覧の先頭 (番号・状態・カテゴリ) が残り、開閉が
// 位置関係として見える。
//
// 状態機械と合成は tuikit (anim.Transition / layout.ComposeDrawer) が持つ。ここに残すのは
// glogx の寸法と所要時間だけ。

import (
	"time"

	"tuikit/anim"
	"tuikit/layout"
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
	// issuesDrawerMaxPeek は左に残す一覧の最大幅。🚨 比率だけで決めると画面が広いほど一覧が
	// 場所を食う: popup は端末幅の 90% (_tmux.conf) なので、312 桁の端末では一覧に 50 桁超を
	// 割いていた (ユーザー報告 2026-07-31「まだ一覧が見えている」)。覗き見に要るのは「どの行から
	// 開いたか」が分かる幅だけなので、番号・状態・カテゴリが見える 18 桁で止める
	// ("→ 014 ○ research" = 18)。以降の幅は全部本文へ回す。
	issuesDrawerMaxPeek = 18
	// issuesDrawerDuration は開閉の所要時間。開く演出 (issuesAnimDuration) より速いのは、
	// issue を次々に見るときに開閉の往復がそのまま待ち時間になるため。🚨 変えるならここ 1 箇所。
	issuesDrawerDuration = 112 * time.Millisecond
)

// issuesDrawerGeometry は上の定数を tuikit の寸法へ束ねたもの。
var issuesDrawerGeometry = layout.DrawerGeometry{
	Ratio: issuesDrawerRatio, Extra: issuesDrawerExtra,
	MinList: issuesDrawerMinList, MaxPeek: issuesDrawerMaxPeek,
}

// issuesDrawer は本文引き出しの開閉アニメの状態。zero value = 閉じている。
//
// 🚨 閉じる演出のあいだ本文 (issuesView.open / body) を消してはいけない: 逆再生で中身が
// 見えている必要がある。実際の破棄は演出が着地してから (issuesView.settleDrawer)。
type issuesDrawer struct{ t anim.Transition }

func (d *issuesDrawer) open(now time.Time)                 { d.t.Open(now, issuesDrawerDuration) }
func (d *issuesDrawer) startClose(now time.Time)           { d.t.Close(now, issuesDrawerDuration) }
func (d *issuesDrawer) phase() anim.Phase                  { return d.t.Phase() }
func (d *issuesDrawer) animating(now time.Time) bool       { return d.t.Animating(now) }
func (d *issuesDrawer) settle(now time.Time) (closed bool) { return d.t.Settle(now) }
func (d *issuesDrawer) finish() (closed bool)              { return d.t.Finish() }

// targetWidth は開ききったときの本文の幅。
func (d *issuesDrawer) targetWidth(total int) int { return issuesDrawerGeometry.Target(total) }

// width は今フレームで画面に入っている本文の幅 (閉じていれば 0)。
func (d *issuesDrawer) width(total int, now time.Time) int {
	return layout.DrawerWidth(d.targetWidth(total), d.t.Openness(now, anim.EaseOutCubic))
}
