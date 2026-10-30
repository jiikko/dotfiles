// Package tickchain は描き直しの tick を 1 本に束ね、今張ってある tick より早い tick が要るときだけ張り直す。
//
// 画面は「動くものがある間だけ tick を回す」形で、周期は状態で変わる (動きの間は 16〜33ms、裏の処理を待つ間は 80ms)。
// 1 本だけを張る single-flight で周期を張った時点に決めると、遅い tick が張ってある間に動きが始まったとき、
// 最初の速いフレームがその遅い tick まで待たされる (打鍵の直後に最大 80ms 止まる。treefiler で実測)。
// Chain は張ってある tick の届く時刻を覚え、それより早く要るときは新しい tick を張る。追い越された古い tick は
// Fired が false を返すので、呼び出し側は描き直しだけして張り直しの判断を Want に任せる。
//
// bubbletea に依存しない: tick の Msg と tea.Tick は呼び出し側が作り、seq を Msg に載せて Fired へ返す。
package tickchain

import "time"

// Chain の零値は tick を張っていない状態。
type Chain struct {
	due   time.Time
	armed bool
	seq   uint64
}

// Want は now から d 以内に tick が要るときに呼ぶ。新しく張るべきなら (seq, true) を返す: 呼び出し側は d 後に
// seq を載せた tick を届ける。張ってある tick が now+d までに届くなら (0, false) (束ねる)。
func (c *Chain) Want(now time.Time, d time.Duration) (seq uint64, arm bool) {
	due := now.Add(d)
	if c.armed && !c.due.After(due) {
		return 0, false
	}
	c.seq++
	c.due, c.armed = due, true
	return c.seq, true
}

// Fired は tick が届いたときに呼ぶ。最後に張った tick なら束ねを空けて true。張り直しで追い越された古い tick なら false
// (新しい tick がまだ張ってある)。
func (c *Chain) Fired(seq uint64) bool {
	if !c.armed || seq != c.seq {
		return false
	}
	c.armed = false
	return true
}

// Pending は張ってある tick の届く時刻と seq (張っていなければ ok=false)。
func (c *Chain) Pending() (due time.Time, seq uint64, ok bool) { return c.due, c.seq, c.armed }
