package main

// groupExpansion は issues viewer の epic group の展開状態。3 つの集合の整合を 1 か所で持つ。
//
//   - manual: 手動で展開した GroupKey。screen に保存される (再起動を跨ぐ)
//   - auto: 番号フィルタが一時的に展開した GroupKey。refresh のたびに作り直す
//   - collapsed: 番号フィルタ中に親行を明示的に畳んだときの一時 override (auto を打ち消すためだけにある)
//
// 🚨 collapsed は番号フィルタが有効な間だけ意味を持つ。フィルタを解く経路は必ず endFilter を通す
// (以前は 3 つの map を 7 関数が直接書いていて、解く経路を 1 本足すと collapsed の消し忘れで
// 「解除後も畳んだまま・手動で展開しても開かない」になる形だった。issue 666)。
//
// GroupKey は絶対パス (issues.Issue.GroupKey)。ゼロ値は「何も展開していない」で、そのまま使える。
type groupExpansion struct {
	manual    map[string]bool
	auto      map[string]bool
	collapsed map[string]bool
}

// expanded はその group が開いているか (表示の判定の唯一の出典)。
func (g *groupExpansion) expanded(key string) bool {
	return key != "" && !g.collapsed[key] && (g.manual[key] || g.auto[key])
}

// saved は screen に保存する手動の展開 (true のものだけ。無ければ nil)。
func (g *groupExpansion) saved() map[string]bool { return copyExpandedGroups(g.manual) }

// restore は保存した手動の展開を戻す (screen の復元)。
func (g *groupExpansion) restore(saved map[string]bool) { g.manual = copyExpandedGroups(saved) }

// reveal は key を手動で開く。明示的に畳んだ状態 (collapsed) も上書きする。
//
// 🚨 manual は保存されるので、再起動を跨いで開いたままになる。カーソルの居場所を見せる方を
// 優先した意図的な選択 (anchorCursorInternal の注記)。
func (g *groupExpansion) reveal(key string) {
	if key == "" {
		return
	}
	if g.manual == nil {
		g.manual = make(map[string]bool)
	}
	g.manual[key] = true
	delete(g.collapsed, key)
}

// setAuto は番号フィルタが一時的に開く集合を差し替える (nil = フィルタ無し)。
func (g *groupExpansion) setAuto(keys map[string]bool) { g.auto = keys }

// toggle は親行の Enter / Space。開いていれば畳み、畳んでいれば開く。
// フィルタが auto で開いている group を畳むときは collapsed で打ち消す (auto 自体は refresh で作り直されるため)。
func (g *groupExpansion) toggle(key string) {
	if g.expanded(key) {
		delete(g.manual, key)
		if g.auto[key] {
			if g.collapsed == nil {
				g.collapsed = make(map[string]bool)
			}
			g.collapsed[key] = true
		} else {
			delete(g.collapsed, key)
		}
		return
	}
	delete(g.collapsed, key)
	if !g.auto[key] {
		if g.manual == nil {
			g.manual = make(map[string]bool)
		}
		g.manual[key] = true
	}
}

// endFilter は番号フィルタを解くときの後始末。collapsed を捨て、keep (解除前の現在行の group) が
// auto だけで開いていたなら manual へ引き継ぐ (解除後もその子へ着地できるように。auto 自体は
// 次の setAuto で捨てられる)。🚨 auto を読むので、setAuto(nil) より前に呼ぶこと。
func (g *groupExpansion) endFilter(keep string) {
	g.collapsed = nil
	if keep != "" && g.auto[keep] && !g.manual[keep] {
		g.reveal(keep)
	}
}

// prune は今の走査結果に無い GroupKey (alive に無いもの) を手動の展開から落とす。GroupKey は絶対
// パスなので、group の rename / 削除 / checkout の移動で死にキーが state に溜まり、同名 group を
// 作り直したとき「畳んだつもり」を無視して勝手に展開する (敵対レビュー round 2)。
func (g *groupExpansion) prune(alive map[string]bool) {
	for key := range g.manual {
		if !alive[key] {
			delete(g.manual, key)
		}
	}
}
