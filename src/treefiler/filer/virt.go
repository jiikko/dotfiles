package filer

import (
	"math"
	"slices"
	"strings"
)

// virt.go は巨大なフォルダの子を、画面の近くのものだけ配置しアニメを持たせる仕組み。
//
// 子の位置は「列の x」「ブロックの先頭の行 y0」「行の間隔 stride」と自分の順位 (vidx) で決まるので、全部を map に入れなくても
// 誰がどこへ行くかは計算で出せる。画面の近く (画面 + 上下の windowMargin) の子だけを pos に入れてアニメを持たせ、外へ出た子の
// アニメは捨てる。捨てた子が要るとき (画面の近くへ入ってきた・ブロックが消えて縮む・読み直しで外れた) は、アニメを持つ兄弟の状態を
// 前の順位の差だけずらして作り直す (reviveAnim / ghostCulled)。これが全部を動かしていた場合と同じになるのは、仮想化のまま扱う変化を
// 「子が全員同じだけずれる変化」(カーソルの移動・ブロックの寄せ直し・カメラ) に限っているから (noteGeo / anyRef の doc)。
//
// 遠くても配置する子: カーソルへの経路 (spine)・ブロックの先頭と末尾 (線の端)・開いているフォルダ・新しく現れて湧いている子・
// ライブ更新で光っている子。縮んで消える子を持つ子は step の中で捨てずに置く (cullFar)。
//
// 全部を動かす (仮想化しない) のは次のとき。意味が変わらない側へ倒す:
//   - 子の数が virtMin 以下 (小さなフォルダの見た目・動きは今までと同じ)
//   - ブロックが現れた・見せる子の覚えが作り直された (読み直し・隠しファイル・並び替え・ライブ更新)・行の間隔が変わったときから
//     spawnHold の間 (noteGeo)。湧く・縮む・並びがばらばらに動く間は全部を動かす
//
// 🚨 仮想化で変わること (全部を動かした場合との差。等価性のテスト virt_test.go が、仮想化しない Model を正解役に残りを固定する):
//   - 縮んで消える子の、消えかけ (透明度 1 割未満) の名前が、別の名前や線と重なるセルで上に来る方が違うことがある
//     (乱数の操作列 300 本中 3 本。sameButFaint が許すのはこの形だけ)
//   - 裏の走査 (熱・大きさ) は、アニメを持っているフォルダにだけ頼む (画面へ入ってから数え始める)
//   - 描画の確保・時間は窓の大きさに比例し、全部の子の数に依らない。ただし全部を動かす期間 (0.6 秒) の間は全件に比例する
//     (712 より前は常にそうだった)

// virtBlock は 1 回の layout で仮想化したブロック (親 1 つの子)。
type virtBlock struct {
	gen        uint64 // 配置したときの visKids の gen
	y0, stride int    // 子 i の目標の行は (y0+i)*stride
	nx         int
	real       []*node // 配置の時点でアニメを持っていた子 (今の目標からのずれで、配置する範囲を広げる)
	placed     []*node // 配置した子
	active     bool    // 子の place.active (親が選択線の上)
	drawn      []*node // 描くときにアニメを持つ子 (線を引く子。collectDrawn)
}

func (b *virtBlock) targetY(k *node) int { return (b.y0 + k.vidx) * b.stride }

// virtMin は仮想化する子の数の下限 (これ以下は全部を動かす)。🚨 画面の高さに依らない定数にする: 高さで変わると、窓の大きさ
// (glogx は View の度に Resize する) だけで仮想化する・しないが切り替わり、切り替わりのたびに戻ってくる子の扱いが要る。
// 一度仮想化したブロックは、子が半分を下回るまで仮想化を続ける (閾値の前後を行き来しない)。
const virtMinDefault = 1024

func (m *Model) virtMin() int {
	if m.virtOverride != 0 {
		return m.virtOverride // テストが小さなフォルダで仮想化を試す / 負なら仮想化しない (等価性のテストの正解役)
	}
	return virtMinDefault
}

// virtualizes は親 p の n 個の子のブロックを仮想化するか (全部を動かす期間かどうかは見ない)。
func (m *Model) virtualizes(p *node, n int) bool {
	o := m.virtMin()
	switch {
	case o <= 0:
		return false
	case p.virtSeq >= m.layoutSeq-1:
		return n > o/2
	}
	return n > o
}

// windowMargin は画面の上下に足す余白の行数 (画面の高さの 4 倍)。当時の実測で決めた: 乱数の操作列のテスト 300 本で、
// 1 倍だと 3 本 (種 118 / 214 / 241) で画面を高くした直後に薄く描かれる子が仮想化した側に無かった (原因は未特定)。
// 🚨 2026-10-12 の HEAD では 1 倍でも 300 本が全部通り、倍率を固定するテストが無い (実測。後の直しで原因が消えたかは未確認)。
// 縮めるなら、倍率で結果が変わる場面をテストで作ってから。
func windowMargin(ch int) float64 { return 4 * float64(ch) }

// spawnHold は全部の子を動かしておく時間 (秒。速さの設定を掛ける前)。湧く動き (moveDur) とフェードイン (fadeInDur) が終わるまで。
const spawnHold = moveDur * 1.2

// holdClock は spawnHold の起点。🚨 起点は「記した後の最初の Advance」: glogx はキー → View → (後の tick で) Advance の順に呼ぶので、
// 記した時点はまだ動きが始まっていない。かつ、問い合わせた時点でもない (問い合わせは子が生まれるときにしか起きないので、
// 何十フレームも後に初めて問われて期間が始まり直した。乱数の操作列のテストで再現)。
// 🚨 経過は壁時計ではなく animClock (Advance の dt の和) で測る: 動きは 1 回の Advance の進みを 0.05 秒で頭打ちにするので、Advance の
// 間隔が 50ms を超えると壁時計の 0.6 秒では動きが終わっておらず、兄弟のずれがばらばらなまま仮想化に戻った。
type holdClock struct {
	ok  bool
	adv int // 記した時点の Advance の回数
}

func (m *Model) startHold() holdClock { return holdClock{ok: true, adv: m.advN} }

// inHold は h から spawnHold の内か。記してからまだ Advance が来ていなければ内。起点の Advance が覚えている範囲より古ければ外
// (advClock の長さ以上の Advance が過ぎた: 1 回の進みは正なので、それだけ過ぎれば期間より長い)。
func (m *Model) inHold(h *holdClock) bool {
	switch {
	case !h.ok:
		return false
	case h.adv == m.advN:
		return true
	case m.advN-h.adv > len(m.advClock):
		return false
	}
	return m.animClock-m.advClock[(h.adv+1)%len(m.advClock)] < spawnHold
}

// blockGeo は 1 回の layout での子のブロックの置き方 (子 i の目標は x=nx, y=(y0+i)*stride。i は gen の覚えでの順位)。
type blockGeo struct {
	y0, stride, nx int
	gen            uint64
	seq            int // layout の通し番号 (0 = 記していない)
	step           int // step の通し番号 (step の layout で記したときだけ。0 = それ以外)
}

// noteGeo は p の子のブロックの置き方を記し、全部を動かす期間を始めるべきかを決める: ブロックが現れた (前の layout で置いて
// いない) とき、順位の付け方 (見せる子の覚え) か行の間隔が変わったとき。仮想化のまま扱うのは、子が全員同じだけずれる変化
// (カーソルの移動・ブロックの寄せ直し・カメラ) だけ: そのときだけ「近い兄弟の動きを写す」が厳密に成り立つ。
// 🚨 「ばらばらかどうか」を細かい条件 (順位の差の変化の回数・大きな跳躍・動いている途中か) で見分けて仮想化を続ける形は採らない:
// 条件を足すたびに別の組み合わせで破られた (別の入力での破れ 2 回と、乱数の操作列のテストの 300 本中 7 本)。読み直し・隠しファイル・
// 並び替え・ライブ更新の直後は spawnHold の間だけ全部を動かす。
func (m *Model) noteGeo(p *node, v *visKids, y0, stride, nx int) {
	if p.geo.seq == m.layoutSeq {
		return
	}
	p.geoPrev = p.geo
	p.geo = blockGeo{y0, stride, nx, v.gen, m.layoutSeq, 0}
	if m.inStep {
		p.geo.step = m.stepN
		p.geoStepPrev, p.geoStep = p.geoStep, p.geo
	}
	prev := p.geoPrev
	switch {
	case prev.seq != m.layoutSeq-1:
		p.openHold = m.startHold() // 現れた
	case prev.stride != stride || prev.gen != v.gen:
		p.openHold = m.startHold()
	}
}

// indexRange は目標の行が [lo, hi] に入る子の順位の範囲 [a, b] (子 i の行は (y0+i)*stride。n 個。空なら a > b)。
func indexRange(y0, stride, n int, lo, hi float64) (int, int) {
	a := int(math.Ceil(lo/float64(stride))) - y0
	b := int(math.Floor(hi/float64(stride))) - y0
	return max(a, 0), min(b, n-1)
}

// placeVirtual は巨大なブロック (親 p の子 ks) のうち画面の近くの子だけを pos に入れる。開いているフォルダの子は遠くても入れる
// (その子のブロックを次の列で配置する・線を引くのにアニメが要る)。次の列の親を next に足して返す。
func (m *Model) placeVirtual(pos map[*node]place, p *node, v *visKids, y0, stride, nx int, active bool, spKid *node, next []colEntry) []colEntry {
	b := m.takeVirt(p)
	b.gen, b.y0, b.stride, b.nx, b.active = v.gen, y0, stride, nx, active
	for _, k := range m.realBuf {
		if k.parent == p && k.vgen == v.gen {
			b.real = append(b.real, k)
		}
	}
	ks := v.kids
	put := func(k *node) {
		if k.vgen != v.gen {
			return // 今の見せる子ではない (検索がカーソルを、ライブ更新で消えた項目へ戻した。乱数の操作列のテストで再現)
		}
		if _, ok := pos[k]; !ok {
			pos[k] = place{nx, b.targetY(k), active}
			b.placed = append(b.placed, k)
		}
	}
	putRange := func(lo, hi float64) {
		a, z := indexRange(y0, stride, len(ks), lo, hi)
		for i := a; i <= z; i++ {
			put(ks[i])
		}
	}
	ch := m.canvasH()
	mg := windowMargin(ch)
	// カメラが向かう先 (カーソルの行 = 選択線の y=0 を画面の縦の中央へ。step のカメラと同じ式)
	top := -float64(ch / 2)
	putRange(top-mg, top+float64(ch)+mg)
	if len(b.real) > 0 {
		// 今の画面に「今の位置」が入る子。目標からのずれ (off) は兄弟ごとに同じか、挿入で上下に分かれても数通りなので、
		// ずれの最小と最大で範囲を広げれば足りる
		offMin, offMax := math.Inf(1), math.Inf(-1)
		for _, k := range b.real {
			off := m.anims[k].y.v - float64(b.targetY(k))
			offMin, offMax = min(offMin, off), max(offMax, off)
		}
		putRange(m.camY.v-mg-offMax, m.camY.v+float64(ch)+mg-offMin)
	}
	if spKid != nil {
		put(spKid)
	}
	// 先頭と末尾の子は遠くても置く: 縦棒の端と、親からブロックへ降りる肘 (lines) はブロックの両端の位置で決まる。ブロックが丸ごと
	// 画面の外でも、肘は画面の中の親から画面を縦に貫く
	put(ks[0])
	put(ks[len(ks)-1])
	for k := range m.ripples {
		if m.isKidOf(p, k) {
			put(k) // 光っている子は遠くても置く (親からの肘を光らせる drawRippleElbows が今の位置を引く)
		}
	}
	if m.inHold(&v.built) {
		// 新しく現れた子は遠くても湧かせる (湧いている間は親の右端に居て、ブロックの線の出方を決める: linesInto の「まだ展開の途中」)
		for _, k := range v.fresh {
			put(k)
		}
	}
	for _, k := range v.dirs {
		if k.expanded {
			put(k)
			if len(m.kids(k)) > 0 {
				next = append(next, colEntry{k, b.targetY(k)})
			}
		}
	}
	return next
}

// takeVirt は p の virtBlock を使い回しの入れ物から出す (layout の度に作り直さない)。
func (m *Model) takeVirt(p *node) *virtBlock {
	if m.virtN == len(m.virtPool) {
		m.virtPool = append(m.virtPool, &virtBlock{})
	}
	b := m.virtPool[m.virtN]
	m.virtN++
	b.real, b.placed, b.drawn = b.real[:0], b.placed[:0], b.drawn[:0] // 中身は resetVirt が空にしている
	m.virt[p] = b
	return b
}

// resetVirt は layout の始めに、前回の仮想化の記録を捨て、アニメを持つ子を集め直す。
func (m *Model) resetVirt() {
	if m.virt == nil {
		m.virt = map[*node]*virtBlock{}
	}
	clear(m.virt)
	for _, b := range m.virtPool[:m.virtN] { // 使わなかったブロックに前の子を残さない (消えた部分木を掴む。issue 726)
		b.real, b.placed, b.drawn = compactTail(b.real, nil), compactTail(b.placed, nil), compactTail(b.drawn, nil)
	}
	m.virtN = 0
	clear(m.refs) // 覚えは layout ごと (anyRef)。次に仮想化するまで消えた親のキーを残さない
	m.realBuf = compactTail(m.realBuf, nil)
	if m.virtMin() <= 0 {
		return
	}
	for _, n := range m.order {
		if a := m.anims[n]; a != nil && !a.ghost && n.parent != nil && (n.parent.virtSeq > 0 || m.virtualizes(n.parent, len(n.parent.kids))) {
			m.realBuf = append(m.realBuf, n)
		}
	}
}

// compactAnims は anims が最近の最多の 1/4 を切ったら map を作り直す。🚨 Go の map は要素を消しても容量が残り、反復
// (sync が毎 tick 舐める) は容量に比例する: 巨大なフォルダを開いた直後に全部の子へアニメを持たせ、仮想化で捨てた後も 5 万件ぶんの
// 空の map を舐め続けた (j 1 回 170 ms)。
func (m *Model) compactAnims() {
	n := len(m.anims)
	m.animsPeak = max(m.animsPeak, n)
	if m.animsPeak < 64 || n*4 >= m.animsPeak {
		return
	}
	fresh := make(map[*node]*anim, n)
	for k, a := range m.anims {
		fresh[k] = a
	}
	m.anims, m.animsPeak = fresh, n
}

// cullFar は sync の終わりに、画面から遠いので配置しなかった子のアニメを捨てる (消えていくのではない。order からは step が外す)。
// 縮んで消える子を持つ子は捨てずに、その場の目標で置く: 縮む子の行き先 (ghostTarget) はいちばん近いアニメを持つ祖先の右端で、
// 捨てると別の祖先へ向かって縮む (C で閉じた遠くのフォルダの中身。乱数の操作列のテストで再現)。この step で縮み始めた子の親は
// layout の時点では分からないので、縮む子を決めた後のここで拾う。描く側の layout では置かないが、画面に見えている子は窓の中なので
// もともと置かれ、見えていない子は描画に出ない。
func (m *Model) cullFar(pos map[*node]place) {
	if len(m.virt) == 0 {
		return
	}
	keep := map[*node]bool{}
	for n, a := range m.anims {
		if a.ghost && n.parent != nil {
			keep[n.parent] = true
		}
	}
	for n, a := range m.anims {
		if _, ok := pos[n]; ok || !m.culled(n) {
			continue
		}
		if keep[n] {
			b := m.virt[n.parent]
			pos[n] = place{b.nx, b.targetY(n), b.active}
			continue
		}
		delete(m.anims, n)
		n.wasCulled, n.culledBatch = true, a.batch
	}
}

// culled は n が仮想化したブロックの子で、画面から遠いので配置しなかったか (消えていくのではない。アニメを捨てる)。
func (m *Model) culled(n *node) bool {
	b, ok := m.virt[n.parent]
	return ok && n.vgen == b.gen
}

// isFresh は n が親の覚えの作り直しで新しく現れた子で、まだ湧いている期間か。
func (m *Model) isFresh(n *node) bool {
	if n.parent == nil || n.freshGen != n.vgen {
		return false
	}
	v := m.visOf(n.parent)
	return n.vgen == v.gen && m.inHold(&v.built) // 今の覚えの子だけ (外れた子は世代が古いまま。敵対レビュー 4 周目の直しで再現)
}

// oldIdx は n の、置き方 g のときの順位 (分からなければ -1)。覚えを作り直していなければ今の順位、作り直したならその前の順位。
func oldIdx(n *node, g blockGeo) int {
	switch g.gen {
	case n.vgen:
		return n.vidx
	case n.pgen:
		return n.pvidx
	}
	return -1
}

// continuous は p の子のブロックを前の layout から続けて置いているか。
func (m *Model) continuous(p *node) bool { return p.geoPrev.seq == m.layoutSeq-1 }

// reviveAnim は仮想化で画面から遠いので捨てていた子 n を、配置し直すときに作り直す。写せなければ nil (呼び出し側は湧かせる)。
//
// 仮想化している間、アニメを持たない子の状態は「アニメを持つ兄弟の状態を、アニメが進んだ置き方 (直前の step の配置) での行の差
// だけずらしたもの」と定める。ブロックの子は全員が同じ瞬間に同じだけ目標が変わるので、全部を動かしていた場合の状態と一致する
// (anyRef の doc)。作り直した後の目標の変化 (並び替え・行の間隔・飛ぶ動き) は、次の step で全部を動かした場合と同じく今の位置から
// 始まる。🚨行の差は前の置き方の順位 (oldIdx) で取る: 今の順位で取ると、並び替えの後は前の位置と食い違う。
func (m *Model) reviveAnim(n *node) *anim {
	p := n.parent
	if p == nil || p.virtSeq == 0 || m.isFresh(n) {
		return nil // 仮想化したことのないブロック (全部を動かしている) と新しく現れた子は、今までどおり湧かせる
	}
	// アニメの状態が進んだ置き方: step の中なら前の step のもの、描く直前ならこのフレームの step のもの。🚨 直前の layout ではない
	// (step と step の間に描くための layout が挟まり、そこで新しい行の間隔を記すと、古い位置のアニメに新しい間隔でずらし幅を
	// 掛けてしまう)
	g, want := p.geoStep, m.stepN
	if m.inStep {
		g, want = p.geoStepPrev, m.stepN-1
	}
	if g.step == 0 || g.step != want || !m.continuous(p) {
		return nil // 続けて置いていない (畳んで開き直した) = 現れた
	}
	oi := oldIdx(n, g)
	if oi < 0 {
		return nil
	}
	ref, ri := m.anyRef(p, g)
	if ref == nil {
		if !n.wasCulled {
			return nil
		}
		// アニメを持つ兄弟が居ない: 捨てたときに止まっていたとみなし、前の目標の位置に置く
		a := &anim{w: m.labelWidth(n), batch: n.culledBatch}
		a.x.v, a.y.v, a.alpha.v = float64(g.nx), float64((g.y0+oi)*g.stride), 1
		return a
	}
	a := *m.anims[ref]
	a.ghost = false
	a.w = m.labelWidth(n)
	a.y.shift(float64((oi - ri) * g.stride))
	if n.wasCulled {
		a.batch = n.culledBatch // 描く順は捨てる前のもの
	}
	return &a
}

// anyRef は p の子のうちアニメを持ち、置き方 g での順位が分かる子とその順位 (無ければ nil)。1 回の layout の間は親ごとに覚える。
// どの兄弟でもよい: 作り直しが起きるのは、仮想化している step (窓が動いた) と、仮想化から全部を動かす期間に入る step と、それらの
// 描く直前だけで、どれも直前の step は仮想化している。仮想化している間は、ブロックの子は全員が同じ瞬間に同じだけ目標が変わる
// (変わる変化だけを仮想化のまま扱う。noteGeo) ので、アニメを持つ兄弟は全員が同じずれで動いている (いちばん近い兄弟を探しても、
// 変異で確かめたとおり答えは同じ)。湧いている子もここには居ない (湧くのは全部を動かす期間だけ)。
func (m *Model) anyRef(p *node, g blockGeo) (*node, int) {
	if m.refsSeq != m.layoutSeq {
		clear(m.refs)
		m.refsSeq = m.layoutSeq
	}
	if m.refs == nil {
		m.refs = map[*node]*node{}
	}
	ref, ok := m.refs[p]
	if !ok {
		for _, k := range m.realBuf {
			if k.parent == p && oldIdx(k, g) >= 0 {
				ref = k
				break
			}
		}
		m.refs[p] = ref
	}
	if ref == nil {
		return nil, 0
	}
	return ref, oldIdx(ref, g)
}

// noteStepVirt は step の layout で仮想化したブロックの親を覚える (次の step で、そのブロックが消えたかを見る)。
func (m *Model) noteStepVirt() {
	m.stepVirt = compactTail(m.stepVirt, nil)
	for p := range m.virt {
		m.stepVirt = append(m.stepVirt, p)
	}
	// 外れた子 (覚えの removed) を読むのは、次の step で stepVirt に居る親だけ (ghostCulled・realizeVirtual)。この step で仮想化しなかった親の
	// removed はもう誰も読まないので捨てる (残すと、消えた部分木をその親の覚えを次に作り直すまで掴む)
	held := m.remHolders[:0]
	for _, p := range m.remHolders {
		if p.vis == nil || len(p.vis.removed) == 0 {
			continue
		}
		if _, ok := m.virt[p]; ok {
			held = append(held, p)
			continue
		}
		p.vis.removed = nil
	}
	m.remHolders = compactTail(m.remHolders, held)
}

// compactTail は s を詰めた結果 kept (s の先頭を使い回した slice。nil なら空) を返し、s の後ろに残った古いポインタを nil で埋める
// (len を縮めるだけでは cap の先に消えた node・閉じたタイルが残り、GC に回収されない)。
func compactTail[T any](s, kept []T) []T {
	clear(s[len(kept):])
	if kept == nil {
		return s[:0]
	}
	return kept
}

// ghostCulled は、直前の step で仮想化していたブロックの子が見せる子から外れた (ライブ更新で消えた・隠した) とき、またはブロックが
// この step で置かれなくなった (畳んだ・祖先を畳んだ・root を変えた) とき、
// 画面から遠いので捨てていた子にもアニメを作り直し、縮んで消える側に回す。全部を動かす側では全部の子が縮んで消え、遠くの子も
// 縮む途中で画面を横切る (畳んだ後に画面を高くすると、透明度 25% で届いた。乱数の操作列のテストで再現)。
// 全部の子にアニメを持たせるのは消える動きの間 (fadeOutDur) だけ。
func (m *Model) ghostCulled() {
	added := false
	// 覚えの作り直しで外れた子 (ライブ更新で消えた・隠した) も同じ: 全部を動かす側では縮んで消える
	for _, p := range m.stepVirt {
		v, g := p.vis, p.geoStep
		if p.geo.seq == m.layoutSeq {
			g = p.geoStepPrev // この step でも置いた: geoStep はこの step のもの。アニメが進んだのは 1 つ前の step の置き方
		}
		if v == nil || len(v.removed) == 0 || g.step != m.stepN-1 || g.gen == v.gen {
			continue
		}
		ref, ri := m.anyRef(p, g)
		for _, k := range v.removed {
			if _, ok := m.anims[k]; ok || ref == nil {
				continue
			}
			oi := oldIdx(k, g)
			if oi < 0 {
				continue
			}
			a := *m.anims[ref]
			a.w = m.labelWidth(k)
			a.y.shift(float64((oi - ri) * g.stride))
			if k.wasCulled {
				a.batch = k.culledBatch
			}
			k.wasCulled = false
			m.anims[k] = &a
			m.order = append(m.order, k)
			added = true
		}
		v.removed = nil // 1 回だけ (次の作り直しまで同じ子を作り直さない)
	}
	for _, p := range m.stepVirt {
		if p.geo.seq == m.layoutSeq {
			continue // 今も置いている
		}
		g, v := p.geoStep, p.vis
		if v == nil || g.step != m.stepN-1 || v.gen != g.gen {
			continue
		}
		ref, ri := m.anyRef(p, g)
		if ref == nil {
			continue
		}
		ra := m.anims[ref]
		for _, k := range v.kids {
			if _, ok := m.anims[k]; ok {
				continue
			}
			a := *ra
			a.w = m.labelWidth(k)
			a.y.shift(float64((k.vidx - ri) * g.stride))
			if k.wasCulled {
				a.batch = k.culledBatch
			}
			k.wasCulled = false
			m.anims[k] = &a
			m.order = append(m.order, k)
			added = true
		}
	}
	if added {
		m.sortOrder()
	}
}

// realizeVirtual は描く直前に、配置したがアニメを持たない子 (仮想化で捨てていた子) を作り直す (reviveAnim)。step の後で
// 画面へ入ってきた子 (大きく飛ぶ動きの途中) と、キーの直後で Advance の前に新しく選択線に乗った遠くの子 (選択線の縦棒が画面を
// 貫く) を、そのフレームから描くため。全部を動かす期間でも同じ。新しく現れた子は step の sync に任せる (湧かせる)。
func (m *Model) realizeVirtual(pos map[*node]place) {
	added := false
	for k := range pos {
		if k.parent == nil || k.parent.virtSeq == 0 {
			continue
		}
		if _, ok := m.anims[k]; ok {
			continue
		}
		if a := m.reviveAnim(k); a != nil {
			m.anims[k] = a
			m.order = append(m.order, k)
			added = true
		}
	}
	// 読み直しで外れた子も、次の step まではまだ縮んでいない (全部を動かす側では前の位置に描かれる)。
	// 🚨 前提: 覚えの鍵 (kidsVer / ShowHidden / 経路上の隠しファイル / labelMax) を変えた後に覚えを引くのは layout (View か step) だけで、
	// キーの処理の中で覚えを引く所 (siblings / moveSibling / descend / nextFile / isKidOf) は鍵を変える前に引く。だから Advance を挟まずに
	// 2 回作り直しても、その間の View でここを通る (外れた子は 1 回目の View で作り直され、前の順位も 1 回目の世代のもので足りる)。
	// キーの処理の中で鍵を変えた後に覚えを引くようになったら、2 回目の作り直しが前の順位 (pvidx) を中間の世代で上書きし、遠くで捨てた子が
	// 滑らずに湧く (今は到達しない)
	for _, p := range m.stepVirt {
		if p.vis == nil {
			continue
		}
		if p.geoStep.gen == p.vis.gen {
			p.vis.removed = nil // 外れた後の世代で既に step を通った (縮ませる機会は過ぎた)。残すと毎フレーム舐め続ける (敵対レビュー 5 周目)
			continue
		}
		for _, k := range p.vis.removed {
			if _, ok := m.anims[k]; ok {
				continue
			}
			if a := m.reviveAnim(k); a != nil {
				m.anims[k] = a
				m.order = append(m.order, k)
				added = true
			}
		}
	}
	if added {
		m.sortOrder()
	}
}

// collectDrawn は仮想化したブロックごとに、アニメを持つ子を集める (線は今の位置を持つ子から引く。全部を動かす側も lines が
// now に居る子だけを使う。配置した子 (placed) で代えると、キーの直後のようにアニメがまだ前の位置にある間に食い違う)。
func (m *Model) collectDrawn(now map[*node][2]int) {
	if m.virtN == 0 {
		return
	}
	for _, n := range m.order {
		if b, ok := m.virt[n.parent]; ok { // 読み直しで消えた子は覚えの作り直しを伴うので、そのブロックは全部を動かす期間 (仮想化しない)
			if _, ok := now[n]; ok {
				b.drawn = append(b.drawn, n)
			}
		}
	}
}

// sortOrder は order を「生まれた回 → 深さ → パス」の順に並べ直す。全部を動かしていれば order はこの順のまま (回ごとに
// sortByDepth で並べて末尾へ足す) なので、写した・戻した子 (古い回) を足したときだけ呼ぶ。重なったラベルの前後を同じにする。
func (m *Model) sortOrder() {
	slices.SortStableFunc(m.order, func(x, y *node) int {
		ax, ay := m.anims[x], m.anims[y]
		bx, by := 0, 0
		if ax != nil {
			bx = ax.batch
		}
		if ay != nil {
			by = ay.batch
		}
		if bx != by {
			return bx - by
		}
		if dx, dy := x.depth(), y.depth(); dx != dy {
			return dx - dy
		}
		return strings.Compare(x.path(), y.path())
	})
}
