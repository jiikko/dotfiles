package filer

// kids.go は「木に見せる子」(dotfile を隠す・カーソルへの経路は見せる。spec §5.5) と、その覚え。
// 配置・描画・キーが毎フレーム・毎打鍵引くので、子の並びと見せ方が変わったときだけ作り直す (5 万件のフォルダで、絞り込みの
// スライスを毎回作るのが残りの重さだった。issue 712)。

// hasKids は n に見える子がいるか (kids と同じ判定。描画で毎フレーム呼ぶので、スライスを作らずに数える)。
func (m *Model) hasKids(n *node) bool {
	for _, k := range n.kids {
		if m.visible(k) {
			return true
		}
	}
	return false
}

// visible は k を木に見せるか (dotfile は隠す設定でも、カーソルへの経路にあれば見せる。spec §5.5)。
func (m *Model) visible(k *node) bool { return m.set.ShowHidden || !k.hidden() || m.onPath(k) }

// kids は覚え (visKids) を返す。🚨 返すスライスは書き換えない (覚えそのもの。ShowHidden なら n.kids そのもの)。
func (m *Model) kids(n *node) []*node { return m.visOf(n).kids }

func (m *Model) onPath(n *node) bool {
	for p := m.cur; p != nil; p = p.parent {
		if p == n {
			return true
		}
	}
	return false
}

// visKids は 1 つのフォルダの「木に見せる子」の覚え (kids)。子の並びと見せ方が変わったときだけ作り直す。
// 作り直すと gen が変わり、各子の vidx (この並びでの順位) と vgen を書き直す。子が今の並びに居るかは vgen == gen で分かる。
type visKids struct {
	ver     int   // node.kidsVer
	first   *node // 念のための照合 (kidsVer の更新漏れで古い並びを返さない)
	n       int
	all     bool  // ShowHidden
	pathKid *node // カーソルへの経路にある隠しファイルの子 (隠す設定でも見せる)
	labMax  int
	kids    []*node
	removed []*node // 作り直しで見せる子から外れた子 (読み直しで消えた・隠した。前の世代の順位を持ったまま。ghostCulled)
	fresh   []*node // 作り直しで新しく現れた子 (前の覚えが無い = 初めて開いたときは記さない。そのときは全部を動かす期間が覆う)
	dirs    []*node // kids のうちフォルダ (フォルダかどうかは作った後に変わらない。開いている子を探すときに全部の子を舐めない)
	maxName int     // 子の名前の幅の最大 (列の幅。全部の子を数えずに済むよう覚える)
	gen     uint64
	built   holdClock // 作り直した時 (新しく現れた子を「湧かせる」期間の起点)

	thickKey uint64 // thick を数えたときの gitGen+1 (0 = 数えていない)
	thickDim bool
	thick    bool // 子に git が無視するものでない子が 1 つでもあるか (縦棒の太さ)
}

// visOf は n の visKids (古ければ作り直す)。
func (m *Model) visOf(n *node) *visKids {
	var first *node
	if len(n.kids) > 0 {
		first = n.kids[0]
	}
	var pk *node
	if !m.set.ShowHidden {
		pk = m.hiddenPathKid(n)
	}
	v := n.vis
	if v != nil && v.ver == n.kidsVer && v.first == first && v.n == len(n.kids) && v.all == m.set.ShowHidden && v.pathKid == pk && v.labMax == labelMax {
		return v
	}
	if v != nil && sameKids(m, n, v) {
		// 並びも見せ方も変わらない作り直し (中身だけが変わったライブ更新の読み直し・labelMax): 世代を変えない。変えると全部を動かす
		// 期間に入り、書き込みの続くファイルが 1 つある巨大なフォルダで毎秒 0.6 秒ずつ全件を動かした (敵対レビュー 3 周目)
		v.ver, v.first, v.n, v.all, v.pathKid, v.labMax = n.kidsVer, first, len(n.kids), m.set.ShowHidden, pk, labelMax
		v.maxName = 0
		for _, k := range v.kids {
			v.maxName = max(v.maxName, nameCells(k))
		}
		return v
	}
	m.visGen++
	nv := &visKids{ver: n.kidsVer, first: first, n: len(n.kids), all: m.set.ShowHidden, pathKid: pk, labMax: labelMax, gen: m.visGen, built: m.startHold()}
	if m.set.ShowHidden {
		nv.kids = n.kids
	} else {
		nv.kids = make([]*node, 0, len(n.kids))
		for _, k := range n.kids {
			if m.visible(k) { // 判定は visible 1 か所 (pk は覚えの鍵に使うだけ)
				nv.kids = append(nv.kids, k)
			}
		}
	}
	for i, k := range nv.kids {
		if v != nil && k.vgen == v.gen {
			k.pvidx, k.pgen = k.vidx, v.gen
		} else {
			k.pgen = 0
		}
		if v == nil || k.vgen != v.gen {
			k.freshGen = nv.gen // 前の並びに居なかった子 (読み直しで増えた・隠しファイルを見せた)
			if v != nil {
				nv.fresh = append(nv.fresh, k)
			}
		}
		k.vidx, k.vgen = i, nv.gen
		nv.maxName = max(nv.maxName, nameCells(k))
		if k.dir {
			nv.dirs = append(nv.dirs, k)
		}
	}
	if v != nil {
		for _, k := range v.kids {
			if k.vgen == v.gen { // 新しい並びに居れば上で書き直されている
				nv.removed = append(nv.removed, k)
			}
		}
	}
	n.vis = nv
	return nv
}

// sameKids は n の今の見せる子が、覚え v の並びと同じか (どの子も v の世代で今の位置の順位を記している)。
// 🚨 v.kids と比べない: 隠しファイルを見せる設定では v.kids は n.kids そのもので、並び替え (resort) がその場で並べ替えると v.kids も
// 一緒に変わり、古い順位のまま「同じ」と判定した (乱数の操作列のテストで再現)。各子に記した順位は並べ替えで変わらない。
func sameKids(m *Model, n *node, v *visKids) bool {
	i := 0
	for _, k := range n.kids {
		if !m.set.ShowHidden && !m.visible(k) {
			continue
		}
		if k.vgen != v.gen || k.vidx != i {
			return false
		}
		i++
	}
	return i == len(v.kids)
}

// hiddenPathKid はカーソルへの経路にある n の子のうち、隠しファイルのもの (無ければ nil)。
func (m *Model) hiddenPathKid(n *node) *node {
	for p := m.cur; p != nil; p = p.parent {
		if p.parent == n {
			if p.hidden() {
				return p
			}
			return nil
		}
	}
	return nil
}

// isKidOf は k が p の今の「見せる子」か。
func (m *Model) isKidOf(p, k *node) bool {
	return k != nil && k.parent == p && k.vgen == m.visOf(p).gen
}

// blockThick は p の子に git が無視するものでない子が 1 つでもあるか (覚えを使う。git の結果と設定が変わったら数え直す)。
func (m *Model) blockThick(p *node) bool {
	v := m.visOf(p)
	if v.thickKey != m.gitGen+1 || v.thickDim != m.set.DimIgnored {
		v.thickKey, v.thickDim, v.thick = m.gitGen+1, m.set.DimIgnored, false
		for _, k := range v.kids {
			if !m.thinBranch(k) {
				v.thick = true
				break
			}
		}
	}
	return v.thick
}
