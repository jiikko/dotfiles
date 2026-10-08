package filer

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jiikko/dotfiles/src/tuikit/listnav"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

// Options は Model の外から決めるもの。
type Options struct {
	// Now は「今」。熱の色が今に依存するので、テストと撮影で固定するために注入する。nil なら time.Now
	Now func() time.Time
}

// Result はキーを処理した結果、呼び出し側にしてほしいこと。
type Result int

const (
	// None は何もしない。
	None Result = iota
	// Quit は終わりたい (単体ならプログラムを、glogx なら glogx ごと終える。spec §0.1)。
	Quit
	// Exec はプロセスを起こしてほしい (中身は TakeExec。`!` と `s`)。終わったら Refresh を呼ぶ。
	Exec
)

// Notice は呼び出し側に toast で出してほしい知らせ。OK=false は失敗 (✗ 赤。glogx-ui-guide §9)。
type Notice struct {
	Text string
	OK   bool
}

type anim struct {
	x, y, alpha tween
	ghost       bool
	w           int
}

// Model は treefiler の画面 1 枚分の状態。
type Model struct {
	root, cur  *node
	w, h       int
	now        func() time.Time
	anims      map[*node]*anim
	order      []*node // 描画順 (生まれた順)
	camX, camY tween
	tiles      []*tile
	showHidden bool
	moving     bool
	lastAdv    time.Time
	notices    []Notice
	linkCache  map[string]string
	cv         *canvas // 使い回す格子 (大きさが変わったら作り直す)
	startDir   string  // 起動したフォルダ (前回の場所を覚える鍵)
	walker     *walker
	recs       map[string]walkResult // walker の結果の写し (Advance で取り込む)
	recsVer    int
	git        gitWatch
	set        Settings
	setErrs    []string
	saveErr    string
	panel      panelState
	gitSnap    gitSnapshot
	gitVer     int
	watch      *watcher
	ripples    map[*node]ripple
	bead       tween
	beadFor    *node // ビーズが走っている先 (カーソルが変わったら走り直す)
	beadVel    float64
	beadOn     bool
	search     searchState
	lastSearch string
	exploding  *explodeJob
	help       bool
	prompt     promptState
	exec       *ExecRequest
}

// ripple はライブ更新で光った項目 (spec §4.4)。start から pulse で光り、rippleLife で消える。
type ripple struct {
	start    time.Time
	strength float64
}

const (
	rippleStep = 90 * time.Millisecond   // 1 段上の祖先へ光が昇る間隔
	rippleLife = 2200 * time.Millisecond // 光が消えるまで
)

// New は dir を root にした Model を作る。カーソルは root の最初の子 (無ければ root)。
func New(dir string, opts Options) (*Model, error) {
	root, err := newRoot(dir)
	if err != nil {
		return nil, err
	}
	// moving は最初から true: 起動時のフェードインがある。false で始めると、呼び出し側が「動いていないので tick を回さない」と
	// 判断し、透明のまま次のキーまで何も見えない (glogx の TestFilerAnimationAdvancesOnTickAndStops が捕まえた)
	m := &Model{root: root, cur: root, startDir: root.abs, now: opts.Now, anims: map[*node]*anim{}, linkCache: map[string]string{}, moving: true,
		walker: newWalker(), recs: map[string]walkResult{}, watch: newWatcher(), ripples: map[*node]ripple{}}
	if m.now == nil {
		m.now = time.Now
	}
	if root.readErr != nil {
		m.fail("読めません: " + root.name)
	}
	if ks := m.kids(root); len(ks) > 0 {
		m.cur = ks[0]
		root.last = ks[0]
	}
	m.set, m.setErrs = loadSettings(configPath())
	m.watch.pause(!m.set.Live)
	applyTheme(&m.set)
	labelMax, sortFoldersFirst, sortNatural = m.set.MaxName, m.set.FoldersFirst, m.set.NaturalSort
	m.showHidden = m.set.ShowHidden
	m.resort(root)
	if ks := m.kids(root); len(ks) > 0 && m.cur == root {
		m.cur, root.last = ks[0], ks[0]
	}
	m.restorePlace()
	m.startGit(true)
	return m, nil
}

// Refresh はディスクを読み直す (開いていたフォルダの中身。開閉とカーソルの位置は保つ)。
// カーソルの項目が消えていたら、残っているいちばん近い祖先へ移る。
func (m *Model) Refresh() {
	m.walker.forget(m.root.path())
	m.startGit(true)
	m.root.reload()
	for !m.inTree(m.cur) {
		m.cur = m.cur.parent
	}
	m.moving = true
}

// inTree は n が今の木に繋がっているか (読み直しで消えた項目は親の kids から外れる)。
func (m *Model) inTree(n *node) bool {
	for ; n.parent != nil; n = n.parent {
		if !contains(n.parent.kids, n) {
			return false
		}
	}
	return n == m.root
}

// Changed はライブ更新の合図のチャネル (開いているフォルダが変わると 1 つ届く)。呼ぶとポーリングが始まる。
// 呼び出し側は合図を受けたら Advance を呼んで取り込み、また Changed を待つ。Close で閉じられる (待っている側が終わる)。
func (m *Model) Changed() <-chan struct{} {
	return m.watch.start() // Live が off でもチャネルは返す (止めると、on に戻したときに待つ者がいない。watcher が読み比べを休むだけ)
}

// Close はライブ更新のポーリングを止める (glogx でファイラーを閉じたとき)。
func (m *Model) Close() {
	m.watch.close()
	m.savePlace()
}

// Resize は画面の大きさを伝える (最下行はステータスバー)。
func (m *Model) Resize(w, h int) {
	if w != m.w || h != m.h {
		m.moving = true // カメラの目標 (画面の 38% と縦の中央) が変わる
	}
	m.w, m.h = w, h
}

// Busy は裏で走査・git の取得が走っているか (呼び出し側はこの間、遅い周期で Advance を呼んで結果を取り込む)。
// 取り込んでいない結果がある間も true (最後の結果を取り込む前に呼び出し側が tick を止める窓を塞ぐ。レビューの指摘 2026-10-08)。
func (m *Model) Busy() bool {
	return m.walker.busy() || m.git.busy() || m.walker.pending(m.recsVer) || m.git.pending(m.gitVer) || m.exploding != nil
}

// Animating は動いている途中か (呼び出し側はこの間だけ高い周期で Advance を呼ぶ)。
func (m *Model) Animating() bool { return m.moving }

// OwnsKeys は入力モード中か (glogx の横断キーを譲る判定に使う。spec §0.3)。今は入力欄を持たない。
func (m *Model) OwnsKeys() bool { return m.search.active || m.prompt.active || m.panel.open }

// SearchQuery は検索欄の今の入力 (検索していなければ "")。
func (m *Model) SearchQuery() string {
	if !m.search.active {
		return ""
	}
	return m.search.line.String()
}

// CaretPos は入力欄のキャレットの位置 (画面の桁と行)。入力欄が無ければ ok=false。
// 呼び出し側は端末のカーソルをここに置く (IME の変換中の文字が入力欄に出るように。glogx-ui-guide §7 の caret)。
func (m *Model) CaretPos() (x, y int, ok bool) {
	switch {
	case m.h <= 0:
		return 0, 0, false
	case m.search.active:
		_, col := m.search.line.Window(max(m.w-searchPrefixW-1, 1))
		return searchPrefixW + col, m.h - 1, true
	case m.prompt.active:
		pw := widthOf(m.promptPrefix())
		_, col := m.prompt.line.Window(max(m.w-pw-1, 1))
		return pw + col, m.h - 1, true
	}
	return 0, 0, false
}

// promptPrefix は `!` の入力欄の頭 (どのフォルダで走るか)。
func (m *Model) promptPrefix() string {
	return " " + termwidth.Truncate(baseName(m.workDir()), 24, "…") + " $ "
}

const searchPrefix = " / "

var searchPrefixW = len(searchPrefix)

// TakeNotices は溜まった知らせを取り出す。
func (m *Model) TakeNotices() []Notice {
	n := m.notices
	m.notices = nil
	return n
}

// fail は失敗の知らせ (押したキーが効かなかった理由。glogx-ui-guide §9 の ✗ 赤)。
func (m *Model) fail(text string) { m.notices = append(m.notices, Notice{Text: text}) }

func (m *Model) canvasH() int { return max(m.h-1, 1) }

// ---------- 時間 ----------

// Advance は now まで動きを進める。まだ動いているかを返す。
func (m *Model) Advance(now time.Time) bool {
	dt := 0.0
	if !m.lastAdv.IsZero() {
		dt = math.Min(now.Sub(m.lastAdv).Seconds(), 0.05) * speedFactor[m.set.Speed] // 動きの速さは dt に掛ける (spec §4.1)
	}
	m.lastAdv = now
	m.takeBackground()
	m.takeExplode()
	m.moving = m.step(dt)
	return m.moving
}

// startGit は git の取得を頼む (設定の Git が off なら何もしない。取得を始める場所はここだけにする)。
func (m *Model) startGit(force bool) {
	if m.set.Git {
		m.git.start(m.root.path(), m.now(), force)
	}
}

// takeBackground は裏の走査と git の結果を取り込み、見えているフォルダの走査を頼む。
func (m *Model) takeBackground() {
	if recs, v, ok := m.walker.snapshot(m.recsVer); ok {
		m.recs, m.recsVer = recs, v
	}
	if s, v, ok := m.git.take(m.gitVer); ok {
		m.gitVer = v   // off でも版は進める (取り込まずに残すと Busy が続き、呼び出し側の tick が止まらない)
		if m.set.Git { // off にする前に始めた取得の結果は捨てる
			m.gitSnap = s
		}
	}
	var open []string
	for _, n := range m.order {
		if n.dir {
			m.walker.request(n.path())
			if n.expanded && n.loaded {
				open = append(open, n.path())
			}
		}
	}
	m.watch.setDirs(open, m.loadedSig)
	for _, c := range m.watch.take() {
		m.applyChange(c)
	}
}

// loadedSig は木が読み込んだときのフォルダの中身 (ライブ更新の基準)。
func (m *Model) loadedSig(dir string) map[string]entrySig {
	n := m.findNode(dir)
	if n == nil {
		return nil
	}
	sig := make(map[string]entrySig, len(n.kids))
	for _, k := range n.kids {
		sig[k.raw] = entrySig{k.mtime, k.size, k.dir}
	}
	return sig
}

// WatchingChan は今のライブ更新の合図のチャネル (止まっていれば nil。始めはしない)。
// 届いた合図が今のチャネルからのものかを見分けるのに使う (閉じる直前の合図で待ちが 2 本に増えないように)。
func (m *Model) WatchingChan() <-chan struct{} { return m.watch.current() }

// applyChange はライブ更新の 1 件を取り込む: そのフォルダを読み直し、変わった項目から root へ光を昇らせる。
func (m *Model) applyChange(c dirChange) {
	n := m.findNode(c.dir)
	if n == nil {
		return
	}
	n.reload()
	m.walker.forget(c.dir)
	// 強制にしない: 書き込みの続くフォルダ (ログ) で毎秒 git status が走るのを、3 秒の間隔制限で止める
	m.startGit(false)
	visible := func(name string) bool { return m.showHidden || !strings.HasPrefix(name, ".") }
	lit := false
	for _, name := range c.changed {
		for _, k := range n.kids {
			if k.raw == name && visible(name) {
				m.lightUp(k)
				lit = true
			}
		}
	}
	if !lit && c.listing && m.removedVisible(c) {
		m.lightUp(n) // 見える項目が消えたときはフォルダ自身を光らせる
	}
	for !m.inTree(m.cur) {
		m.cur = m.cur.parent
	}
	m.moving = true
}

// removedVisible は変化に「見える項目が消えた」が含まれるか (隠した dotfile の出入りでフォルダを光らせない)。
func (m *Model) removedVisible(c dirChange) bool {
	for _, name := range c.removed {
		if m.showHidden || !strings.HasPrefix(name, ".") {
			return true
		}
	}
	return false
}

// findNode は絶対パスの項目 (木に読み込まれていれば)。
func (m *Model) findNode(abs string) *node {
	var walk func(n *node) *node
	walk = func(n *node) *node {
		if n.abs == abs {
			return n
		}
		if !strings.HasPrefix(abs, n.abs+string(os.PathSeparator)) {
			return nil
		}
		for _, k := range n.kids {
			if f := walk(k); f != nil {
				return f
			}
		}
		return nil
	}
	return walk(m.root)
}

var speedFactor = map[string]float64{"slow": 0.5, "normal": 1, "fast": 2, "instant": 1000}

// lightUp は n から root へ、1 段ごとに rippleStep 遅れて光を昇らせる (spec §4.4)。
func (m *Model) lightUp(n *node) {
	if !m.set.Ripples {
		return
	}
	now := m.now()
	k := 0
	for p := n; p != nil; p = p.parent {
		start := now.Add(time.Duration(k) * rippleStep)
		strength := math.Max(math.Pow(0.8, float64(k)), 0.35)
		if old, ok := m.ripples[p]; !ok || old.start.Add(4*rippleStep).Before(now) || start.Before(old.start) {
			m.ripples[p] = ripple{start, math.Max(strength, old.strength)}
		}
		k++
	}
}

// glow は n の今の光り方 (0〜1)。立ち上がり 0.07 秒は線形、その後 0.45 秒の指数で消える (spec §4.4 の pulse)。
func (m *Model) glow(n *node) float64 {
	r, ok := m.ripples[n]
	if !ok {
		return 0
	}
	t := m.now().Sub(r.start).Seconds()
	switch {
	case t < 0:
		return 0
	case t < 0.07:
		return t / 0.07 * r.strength
	}
	return math.Exp(-(t-0.07)/0.45) * r.strength
}

// heatAge は n の色を決める経過秒 (spec §5.1 の Tree::heat)。フォルダは配下の最新、dotfile を隠している間は
// dotfile でないファイルだけの最新。まだ数えていなければ自分の mtime。
func (m *Model) heatAge(n *node) float64 {
	t := n.mtime
	if n.dir {
		if r, ok := m.recs[n.path()]; ok {
			switch {
			case m.showHidden && r.newest.After(t):
				t = r.newest
			case !m.showHidden && !r.vis.IsZero():
				t = r.vis
			}
		}
	}
	return m.now().Sub(t).Seconds()
}

func (m *Model) gitState(n *node) byte { return m.gitSnap.state(n.path(), n.dir) }

func (m *Model) sync(pos map[*node]place) {
	for n := range pos {
		if a, ok := m.anims[n]; ok {
			a.ghost = false
			a.w = m.labelWidth(n)
		}
	}
	// 生まれる順は親が先 (列順) になるよう、深さの浅い順に足す
	var born []*node
	for n := range pos {
		if _, ok := m.anims[n]; !ok {
			born = append(born, n)
		}
	}
	sortByDepth(born)
	for _, n := range born {
		p := pos[n]
		a := &anim{w: m.labelWidth(n)}
		a.x.v, a.y.v = float64(p.x), float64(p.y)
		// 新しい項目は、いちばん近いアニメ中の祖先の右端から湧く (spec §4.2)
		for anc := n.parent; anc != nil; anc = anc.parent {
			if pa, ok := m.anims[anc]; ok && !pa.ghost {
				a.x.v, a.y.v = pa.x.v+float64(pa.w), pa.y.v
				break
			}
		}
		if n.parent == nil {
			a.alpha.v = 1
		}
		m.anims[n] = a
		m.order = append(m.order, n)
	}
	for n, a := range m.anims {
		if _, ok := pos[n]; !ok {
			a.ghost = true
		}
	}
}

// sortByDepth は浅い順、同じ深さはパス順 (map の反復順に依らず、描く順を毎回同じにする)。
func sortByDepth(ns []*node) {
	sort.Slice(ns, func(i, j int) bool {
		if di, dj := ns[i].depth(), ns[j].depth(); di != dj {
			return di < dj
		}
		return ns[i].path() < ns[j].path()
	})
}

// ghostTarget は消える項目が縮んでいく先 (いちばん近い生きた祖先の右端)。
func (m *Model) ghostTarget(n *node) (float64, float64) {
	for anc := n.parent; anc != nil; anc = anc.parent {
		if pa, ok := m.anims[anc]; ok && !pa.ghost {
			return pa.x.v + float64(pa.w), pa.y.v
		}
	}
	return 0, 0
}

func (m *Model) step(dt float64) bool {
	pos := m.layout()
	m.sync(pos)
	moving := false
	kept := m.order[:0]
	for _, n := range m.order {
		a := m.anims[n]
		if a == nil {
			continue
		}
		if a.ghost {
			tx, ty := m.ghostTarget(n)
			moving = a.alpha.step(0, fadeOutDur, dt) || moving
			moving = a.x.step(tx, moveDur, dt) || moving
			moving = a.y.step(ty, moveDur, dt) || moving
			if a.alpha.v <= 0.02 {
				delete(m.anims, n)
				continue
			}
		} else {
			p := pos[n]
			moving = a.alpha.step(1, fadeInDur, dt) || moving
			moving = a.x.step(float64(p.x), moveDur, dt) || moving
			moving = a.y.step(float64(p.y), moveDur, dt) || moving
		}
		kept = append(kept, n)
	}
	m.order = kept
	now := m.now()
	for n, r := range m.ripples {
		if now.Sub(r.start) > rippleLife {
			delete(m.ripples, n)
		}
	}
	moving = moving || len(m.ripples) > 0
	moving = m.stepBead(pos, dt) || moving
	// カメラ: カーソルの名前の左端を画面の 38% に、選択線を画面の縦の中央に (spec §4.5)
	cp := pos[m.cur]
	moving = m.camX.step(float64(cp.x)-float64(m.w)*0.38, camDur, dt) || moving
	moving = m.camY.step(float64(cp.y)-float64(m.canvasH()/2), camDur, dt) || moving
	kt := m.tiles[:0]
	for _, t := range m.tiles {
		target := 1.0
		if t.closing {
			target = 0
		}
		moving = t.open.step(target, popupDur, dt) || moving
		moving = t.sy.step(float64(t.scroll), scrollDur, dt) || moving
		if t.closing && t.open.v <= 0.001 {
			continue
		}
		kt = append(kt, t)
	}
	m.tiles = kt
	return moving
}

// stepBead はカーソルが変わったら、線に沿って光の粒 (ビーズ) を前のカーソルの位置から走らせる (spec §4.3)。
func (m *Model) stepBead(pos map[*node]place, dt float64) bool {
	cx := float64(pos[m.cur].x - 2)
	if m.beadFor != m.cur {
		start := cx - 10
		if a := m.anims[m.beadFor]; a != nil && m.beadFor != nil && math.Round(a.x.v)-2 != cx {
			start = a.x.v - 2
		}
		m.beadFor, m.beadOn = m.cur, true
		m.bead = tween{v: start}
	}
	if !m.beadOn {
		return false
	}
	prev := m.bead.v
	moving := m.bead.step(cx, beadDur, dt)
	if dt > 0 {
		m.beadVel = (m.bead.v - prev) / dt
	}
	if !moving {
		m.beadOn = false
	}
	return moving
}

// snapAll は動いている途中のものを全部終点へ飛ばす。消えかけの項目と閉じかけのタイルはその場で捨てる。
// キー入力のたびに呼ぶ (初動は速く・終わりはゆっくり・キーが来たら終わりを打ち切って次へ。spec §0.1)。
func (m *Model) snapAll() {
	kept := m.order[:0]
	for _, n := range m.order {
		a := m.anims[n]
		if a == nil {
			continue
		}
		if a.ghost {
			delete(m.anims, n)
			continue
		}
		a.x.snap()
		a.y.snap()
		a.alpha.snap()
		kept = append(kept, n)
	}
	m.order = kept
	m.camX.snap()
	m.camY.snap()
	m.beadOn = false
	kt := m.tiles[:0]
	for _, t := range m.tiles {
		if t.closing {
			continue
		}
		t.open.snap()
		t.sy.snap()
		kt = append(kt, t)
	}
	m.tiles = kt
}

// ---------- キー ----------

// HandleKey は 1 打鍵を処理する (キーの表記は bubbletea の KeyPressMsg.String())。入力する文字はキーから推す
// (1 文字のキーはその文字、space は空白)。入力欄に文字を入れる呼び出し側は HandleInput で KeyPressMsg.Text を渡す。
func (m *Model) HandleKey(k string) Result {
	text := ""
	switch {
	case k == "space":
		text = " "
	case len([]rune(k)) == 1:
		text = k
	}
	return m.HandleInput(k, text)
}

// HandleInput は 1 打鍵を処理する。text はその打鍵が入力する文字 (KeyPressMsg.Text)。
func (m *Model) HandleInput(k, text string) Result {
	m.snapAll()
	defer func() { m.moving = true }() // 目標が変わったので次の Advance で動き出す
	m.startGit(false)                  // 前回から 3 秒以上たっていれば取り直す (周期のタイマーは張らない)
	if k == "ctrl+c" && !m.search.active && !m.prompt.active {
		return Quit // どこからでも即終了 (glogx-ui-guide §1)。検索中は検索の取り消し (spec §6.2)
	}
	if m.help {
		m.help = false // キー一覧は ? 以外のキーで閉じるだけ (spec §6.6)。閉じたキーは他の意味を持たない
		return None
	}
	if m.panel.open {
		m.panelKey(k)
		return None
	}
	if m.search.active {
		m.searchKey(k, text)
		return None
	}
	if m.prompt.active {
		m.promptKey(k, text)
		if m.exec != nil {
			return Exec
		}
		return None
	}
	if m.exploding != nil && k == "esc" {
		m.exploding.cancel()
		m.fail("explode を中断しました")
		return None
	}
	if m.frontTile() != nil {
		m.tileKey(k)
		return None
	}
	return m.treeKey(k)
}

func (m *Model) siblings() []*node {
	if m.cur.parent == nil {
		return []*node{m.cur}
	}
	return m.kids(m.cur.parent)
}

// moveSibling は同じ階層 (兄弟) の中で delta だけ動く。端では止まる (spec §0.1: j を押しっぱなしでも潜らない)。
func (m *Model) moveSibling(delta int) {
	s := m.siblings()
	for i, n := range s {
		if n == m.cur {
			m.setCur(s[max(0, min(len(s)-1, i+delta))])
			return
		}
	}
}

func (m *Model) setCur(n *node) {
	m.cur = n
	if n.parent != nil {
		n.parent.last = n
	}
}

func (m *Model) halfPage() int { return max(1, m.canvasH()/4) }

func (m *Model) treeKey(k string) Result {
	// 画面固有の動作キーを先に捌き、残りを listnav.MotionOf へ渡す (glogx-ui-guide §2)。
	// Space / Tab は treebeard の開閉 (spec §0.1 の「o 以外は treebeard で上書き」)
	switch k {
	case "q", "esc", "ctrl+c":
		return Quit
	case "l", "right", "ctrl+f", "enter":
		m.descend()
	case "h", "left", "ctrl+b":
		// ctrl+b を ← の別名にするのは glogx-ui-guide §2 の例外 (pro-con のボードと同じ「← が左の列へ動くだけ」の画面)
		if m.cur.parent != nil {
			m.setCur(m.cur.parent)
		}
	case "space", " ", "tab":
		if m.cur.dir {
			m.ensureLoaded(m.cur)
			m.cur.expanded = !m.cur.expanded
		}
	case "c":
		if m.cur.dir {
			m.cur.expanded = false
		}
	case "C":
		m.collapseOthers()
	case "J":
		m.moveSibling(10)
	case "K":
		m.moveSibling(-10)
	case "-", "backspace":
		m.rerootUp()
	case "/":
		m.startSearch()
	case "n":
		m.repeatSearch(1)
	case "N":
		m.repeatSearch(-1)
	case "e":
		m.explode()
	case "?":
		m.help = true
	case "!":
		m.startPrompt()
	case "s":
		m.requestShell()
		return Exec
	case ".":
		m.set.ShowHidden = !m.set.ShowHidden // 設定の Dotfiles と同じ (保存もする)
		m.applySettings("show_hidden")
	case ",":
		m.panel.open, m.help = true, false
	default:
		switch listnav.MotionOf(k) {
		case listnav.Down:
			m.moveSibling(1)
		case listnav.Up:
			m.moveSibling(-1)
		case listnav.HalfDown:
			m.moveSibling(m.halfPage())
		case listnav.HalfUp:
			m.moveSibling(-m.halfPage())
		case listnav.Top:
			m.setCur(m.siblings()[0])
		case listnav.Bottom:
			s := m.siblings()
			m.setCur(s[len(s)-1])
		case listnav.None:
		}
	}
	return None
}

func (m *Model) ensureLoaded(n *node) {
	if n.dir && !n.loaded {
		n.load()
		if n.readErr != nil {
			m.fail("読めません: " + n.name)
		}
	}
}

// descend はフォルダなら開いて潜り (最後にいた子か先頭へ)、ファイルならタイルを開く。
func (m *Model) descend() {
	if !m.cur.dir {
		m.openTile(m.cur, m.cursorRect())
		return
	}
	m.ensureLoaded(m.cur)
	ks := m.kids(m.cur)
	if len(ks) == 0 {
		return
	}
	m.cur.expanded = true
	next := m.cur.last
	if next == nil || !contains(ks, next) {
		next = ks[0]
	}
	m.setCur(next)
}

// collapseOthers はカーソルの祖先以外を全部畳む (カーソル自身も畳む。treebeard の C。spec §6.1・§10)。
func (m *Model) collapseOthers() {
	var walk func(n *node)
	walk = func(n *node) {
		if n.dir && (!m.onPath(n) || n == m.cur) {
			n.expanded = false
		}
		for _, k := range n.kids {
			walk(k)
		}
	}
	walk(m.root)
	m.root.expanded = true
}

// rerootUp は root の親を新しい root にする (- / Backspace。spec §5.7)。
func (m *Model) rerootUp() {
	old := m.root
	parentPath := old.path()
	if i := strings.LastIndexByte(parentPath, '/'); i > 0 {
		parentPath = parentPath[:i]
	} else {
		return // もう / まで来ている
	}
	nr, err := newRoot(parentPath)
	if err != nil {
		m.fail("1 段上へ行けません: " + err.Error())
		return
	}
	// 新しい root の子のうち旧 root と同じ名前の項目を旧 root で置き換える (開いた状態とアニメを保つ)
	found := false
	for i, k := range nr.kids {
		if k.raw == baseName(old.raw) {
			old.raw, old.parent = k.raw, nr // abs は変わらない (同じフォルダ)
			nr.kids[i] = old
			found = true
		}
	}
	if !found {
		// 親の中に見つからない (消えた / 読めない) なら付け替えない。付け替えると旧 root が孤児になり、カーソルが木の外へ出る
		m.fail("1 段上へ行けません: " + old.name + " が親の中に見つかりません")
		return
	}
	nr.last = old
	m.root = nr
}

func baseName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// bottomLoadLines は G で 1 回に読み足す行数の上限。
const bottomLoadLines = 20000

// ---------- 描画 ----------

// View は今の画面 (w×h、最下行はステータスバー) を行ごとに返す。
func (m *Model) View() []string {
	if m.w <= 0 || m.h <= 0 {
		return nil
	}
	return m.draw().lines()
}

func (m *Model) draw() *canvas {
	if m.cv == nil || m.cv.w != m.w || m.cv.h != m.h {
		m.cv = newCanvas(m.w, m.h)
	} else {
		m.cv.reset()
	}
	c := m.cv
	ch := m.canvasH()
	ox, oy := int(math.Round(m.camX.v)), int(math.Round(m.camY.v))
	pos := m.layout()
	now := map[*node][2]int{}
	for _, n := range m.order {
		if a := m.anims[n]; a != nil && !a.ghost && a.alpha.v > 0.05 {
			now[n] = [2]int{int(math.Round(a.x.v)), int(math.Round(a.y.v))}
		}
	}
	// カーソル: 名前の背景を glogx のカーソル色で塗るだけ (spec §0.1)
	if p, ok := now[m.cur]; ok && p[1]-oy < ch {
		c.fillBg(p[0]-1-ox, p[1]-oy, p[0]+widthOf(m.cur.label())-ox, p[1]-oy, cCursorBg)
	}
	m.drawLines(c, now, ox, oy, ch)
	m.drawNames(c, pos, ox, oy, ch)
	m.drawBead(c, ox, oy, ch)
	for i, t := range m.tiles {
		front := i == len(m.tiles)-1
		p := math.Max(0, math.Min(1, t.open.v))
		r := lerpRect(t.from, slotRect(t.slot, m.w, ch), p)
		if front {
			c.dim(r, 0.55*p)
		}
		m.drawTile(c, t, r, front, p)
	}
	m.statusBar(c)
	if m.search.active {
		m.searchBar(c)
	}
	if m.prompt.active {
		m.promptBar(c)
	}
	if m.help {
		m.drawHelp(c)
	}
	if m.panel.open {
		m.drawPanel(c)
	}
	return c
}

// drawBead はカーソルの行を走る光の粒。頭が明るく、尾は速さに応じて伸びる。文字の上だけを光らせる (spec §4.3)。
func (m *Model) drawBead(c *canvas, ox, oy, ch int) {
	a := m.anims[m.cur]
	if !m.beadOn || a == nil {
		return
	}
	y := int(math.Round(a.y.v)) - oy
	if y < 0 || y >= ch {
		return
	}
	head := int(math.Round(m.bead.v)) - ox
	dir := 1
	if m.beadVel >= 0 {
		dir = -1 // 尾は進む向きの後ろ
	}
	tail := int(math.Max(1, math.Min(12, math.Abs(m.beadVel)*0.05)))
	for i := 0; i <= tail; i++ {
		p := c.at(head+dir*i, y)
		if p == nil || p.s == " " || p.cont {
			continue
		}
		p.fg = mix(cAccRoute, cFlash, 1-float64(i)/float64(tail+1))
		if i == 0 {
			p.bg = mix(cBg, cAccRoute, 0.35)
		}
	}
}

func (m *Model) drawLines(c *canvas, now map[*node][2]int, ox, oy, ch int) {
	for k, lc := range m.lines(now) {
		x, y := k[0]-ox, k[1]-oy
		if y >= ch {
			continue
		}
		p := c.at(x, y)
		if p == nil {
			continue
		}
		sap := heat(m.heatAge(lc.owner))
		var col rgb
		switch lc.emph {
		case emRoute:
			col = mix(cAccRoute, sap, 0.18)
		case emActive:
			col = mix(cAccAct, sap, 0.42)
		case emDim:
			col = mix(cAccDim, mix(sap, cBg, 0.55), 0.35)
		}
		p.s, p.fg = glyph(m.set.Lines, lc), col
	}
}

func (m *Model) drawNames(c *canvas, pos map[*node]place, ox, oy, ch int) {
	q := "" // 検索の一致を色付けする語。検索中だけ (String は毎回確保するので、項目ごとに呼ばない)
	if m.search.active {
		q = m.search.line.String()
	}
	for pass := range 2 { // 消えかけの項目を先に、生きている項目を後に描く
		for _, n := range m.order {
			a := m.anims[n]
			if a == nil || (pass == 0) != a.ghost {
				continue
			}
			x, y := int(math.Round(a.x.v))-ox, int(math.Round(a.y.v))-oy
			if y < 0 || y >= ch {
				continue
			}
			base := heat(m.heatAge(n))
			st := m.gitState(n)
			switch {
			case m.onPath(n):
				base = cRouteTxt
			case st == gitIgn && m.set.DimIgnored:
				// ignored は灰色に沈める (spec §1.4 の ignored_shade)
				base = mix(mix(cBg, cIgnored, 0.1+0.08*float64(m.set.DimFloor)), base, 0.12)
			case pos[n].active:
			default:
				base = mix(base, cBg, 0.08*float64(10-m.set.FocusDim)) // 選択線から外れた枝 (spec §1.4 の off_line)
			}
			fg := mix(cBg, base, a.alpha.v)
			if g := m.glow(n); g > 0.02 {
				fg = mix(fg, cRipple, g)
				if n != m.cur {
					c.fillBg(x, y, x+a.w-1, y, mix(cBg, cRippleBg, g*a.alpha.v))
				}
			}
			nx := c.put(x, y, n.label(), fg, n.dir || m.onPath(n))
			if d := m.details(n); d != "" {
				c.put(nx+2, y, d, mix(cBg, base, a.alpha.v*0.5), false) // 名前の後ろの詳細は薄く (spec §3.2)
			}
			if q != "" {
				if r, spans := fuzzy(n.name, q); r < rankNone {
					lw := widthOf(n.label())
					for _, sp := range spans {
						for k := widthOf(n.name[:sp[0]]); k < widthOf(n.name[:sp[1]]) && k < lw; k++ {
							if p := c.at(x+k, y); p != nil {
								p.fg, p.bg, p.bold = cFlash, cMatchBg, true
							}
						}
					}
				}
			}
			if a.ghost {
				continue
			}
			bx := x + a.w + 1
			switch {
			case n.dir && !n.expanded && (!n.loaded || len(m.kids(n)) > 0):
				// 閉じたフォルダの芽は、配下にいちばん重い git の変更があればその色 (spec §2.2)
				bud := mix(cBg, base, a.alpha.v*0.55)
				if gitRank(st) > 0 {
					bud = mix(cBg, gitColor[st], a.alpha.v)
				}
				c.put(bx, y, "›", bud, false)
			case !n.dir && gitRank(st) > 0:
				c.put(bx, y, string(st), mix(cBg, gitColor[st], a.alpha.v), true)
			}
		}
	}
}

func human(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1<<20:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	case n < 1<<30:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
}

func ago(s float64) string {
	switch {
	case s < 60:
		return fmt.Sprintf("%ds ago", int(s))
	case s < 3600:
		return fmt.Sprintf("%dm ago", int(s/60))
	case s < 86400:
		return fmt.Sprintf("%dh ago", int(s/3600))
	case s < 365*86400:
		return fmt.Sprintf("%dd ago", int(s/86400))
	}
	return fmt.Sprintf("%.1fy ago", s/(365*86400))
}

type seg struct {
	s    string
	c    rgb
	bold bool
}

// statusBar は最下行 (spec §3.4)。左にパンくず、右に件数・大きさ・熱の点と経過時間・凡例・キーの案内。
func (m *Model) statusBar(c *canvas) {
	y := m.h - 1
	c.clear(0, y, m.w-1, y, cBar)
	var chain []*node
	for n := m.cur; n != nil; n = n.parent {
		chain = append([]*node{n}, chain...)
	}
	age := m.heatAge(m.cur)
	meta := human(m.cur.size)
	partial := ""
	if m.cur.dir {
		meta = "folder"
		if m.cur.loaded {
			meta = fmt.Sprintf("%d items", len(m.kids(m.cur)))
		}
		if r, ok := m.recs[m.cur.path()]; ok {
			meta += " · " + human(r.bytes)
			if !r.complete {
				meta += "+" // 走査の上限で打ち切った (spec §3.4)
				partial = " (partial)"
			}
		}
	}
	var right []seg
	if m.gitSnap.top != "" && m.gitSnap.branch != "" {
		right = append(right, seg{"⎇ ", cAccRoute, false}, seg{m.gitSnap.branch + " · ", cText, false})
		if st := m.gitState(m.cur); st != gitNone {
			col := cMuted
			if c, ok := gitColor[st]; ok {
				col = c
			}
			right = append(right, seg{gitWord(st) + " · ", col, false})
		}
	}
	right = append(right, seg{meta + " · ", cMuted, false}, seg{"● ", heat(age), false}, seg{ago(age) + partial, cText, false}, seg{"   ", cMuted, false})
	legend := []seg{{"now ", cMuted, false}}
	for _, st := range ember {
		legend = append(legend, seg{"▮", st.c, false})
	}
	legend = append(legend, seg{" old   ", cMuted, false})
	hints := []seg{{"!", cAccRoute, true}, {" cmd  ", cMuted, false}, {"s", cAccRoute, true}, {" shell  ", cMuted, false},
		{"/", cAccRoute, true}, {" find  ", cMuted, false}, {"?", cAccRoute, true}, {" keys  ", cMuted, false}, {"q", cAccRoute, true}, {" quit ", cMuted, false}}
	if m.frontTile() != nil {
		hints = []seg{{"tab", cAccRoute, true}, {" link  ", cMuted, false}, {"q", cAccRoute, true}, {" close ", cMuted, false}}
	}
	segW := func(ss []seg) int {
		w := 0
		for _, s := range ss {
			w += widthOf(s.s)
		}
		return w
	}
	crumbW := 1
	for i, n := range chain {
		if i > 0 {
			crumbW += 3
		}
		crumbW += widthOf(n.name)
	}
	// 幅が足りなければ凡例から落とす。抜ける手段 (q) は残す (glogx-ui-guide §5)
	if m.exploding != nil {
		right = append([]seg{{spinnerFrame(m.now()) + " ", cAccRoute, false}, {"exploding · " + itoa(m.exploding.count()) + " folders · ", cText, false},
			{"esc", cAccRoute, true}, {" stops · ", cMuted, false}}, right...)
	}
	if !m.set.Legend {
		legend = nil
	}
	all := append(append(append([]seg{}, right...), legend...), hints...)
	if crumbW+2+segW(all) > m.w {
		all = append(append([]seg{}, right...), hints...)
	}
	if crumbW+2+segW(all) > m.w {
		all = hints
	}
	x := c.put(0, y, " ", cMuted, false)
	room := m.w - segW(all) - 2
	for i, n := range chain {
		if x >= room {
			break
		}
		if i > 0 {
			x = c.put(x, y, " › ", cAccAct, false)
		}
		col, bold := cMuted, false
		if i == len(chain)-1 {
			col, bold = cText, true
		}
		x = c.put(x, y, termwidth.Truncate(n.name, max(room-x, 1), "…"), col, bold)
	}
	rx := m.w - segW(all)
	for _, s := range all {
		rx = c.put(rx, y, s.s, s.c, s.bold)
	}
}

func widthOf(s string) int { return termwidth.Of(s) }

func decodeRune(s string) (rune, int) { return utf8.DecodeRuneInString(s) }

func itoa(n int) string { return strconv.Itoa(n) }

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// spinnerFrame は 80ms ごとに進む点字のスピナー (spec §2.2)。
func spinnerFrame(now time.Time) string {
	return spinnerFrames[int(now.UnixMilli()/80)%len(spinnerFrames)]
}

// searchBar は検索中の最下行 (spec §3.4 の / 検索行)。
func (m *Model) searchBar(c *canvas) {
	y := m.h - 1
	c.clear(0, y, m.w-1, y, cBar)
	x := c.put(0, y, searchPrefix, cAccRoute, true)
	text, _ := m.search.line.Window(max(m.w-searchPrefixW-1, 1))
	x = c.put(x, y, text, cText, false)
	q := m.search.line.String()
	switch {
	case q == "":
		c.put(x+1, y, "  tab ↑↓ 移動 · enter 留まる · esc 戻る", cMuted, false)
	case len(m.search.matches) == 0:
		x = c.put(x+1, y, "  no match", cDot, false)
		c.put(x, y, "  esc 戻る", cMuted, false)
	default:
		x = c.put(x+1, y, "  "+itoa(m.search.at+1)+"/"+itoa(len(m.search.matches)), cAccRoute, false)
		c.put(x, y, "  tab ↑↓ 移動 · enter 留まる · esc 戻る", cMuted, false)
	}
}

// helpKeys はキー一覧 (? の板。treebeard の KEYS から、写さないもの (マウス・音声・ソート・画像) を除いて日本語にした)。
var helpKeys = [][2]string{
	{"j k ↓ ↑ ^N ^P", "同じ階層の中で移動"},
	{"J K / ^D ^U", "10 件 / 半ページ"},
	{"g G", "兄弟の先頭 / 末尾"},
	{"l → ^F enter", "フォルダへ潜る · ファイルを開く"},
	{"h ← ^B", "親へ"},
	{"space tab", "フォルダの開閉"},
	{"c C", "このフォルダを畳む · 経路以外を畳む"},
	{"e", "配下を全部開く (esc で中断)"},
	{"/ n N", "列の中を検索 · 次 · 前"},
	{"- backspace", "root を 1 段上へ"},
	{".", "dotfile の表示を切り替え"},
	{",", "設定"},
	{"タイルの中", "j k ^D ^U g G · tab でパスを選ぶ · J K で隣"},
	{"q esc", "終了 (タイルの上では 1 枚閉じる)"},
	{"!", "ここでコマンドを 1 行 ($f = 選んだパス)"},
	{"s", "ここでシェルを開く (抜けると戻る)"},
	{"?", "このキー一覧"},
}

// drawHelp はキー一覧の板 (中央。spec §8.1 の幅 64)。
func (m *Model) drawHelp(c *canvas) {
	w := min(64, m.w-2)
	h := min(len(helpKeys)+4, m.h-1)
	if w < 20 || h < 4 {
		return
	}
	x0, y0 := (m.w-w)/2, max((m.h-1-h)/2, 0)
	c.clear(x0, y0, x0+w-1, y0+h-1, cPop)
	c.put(x0, y0, "╭"+strings.Repeat("─", w-2)+"╮", cAccRoute, false)
	for y := y0 + 1; y < y0+h-1; y++ {
		c.put(x0, y, "│", cAccRoute, false)
		c.put(x0+w-1, y, "│", cAccRoute, false)
	}
	c.put(x0, y0+h-1, "╰"+strings.Repeat("─", w-2)+"╯", cAccRoute, false)
	c.put(x0+2, y0, " treefiler ", cText, true)
	for i, kv := range helpKeys {
		y := y0 + 2 + i
		if y >= y0+h-1 {
			break
		}
		kx := c.put(x0+2, y, termwidth.FillLeft(kv[0], 16), cAccRoute, true)
		c.put(kx+2, y, termwidth.Truncate(kv[1], max(w-22, 1), "…"), cText, false)
	}
}

// promptBar は `!` の入力中の最下行 (spec §3.4 の ! プロンプト行)。
func (m *Model) promptBar(c *canvas) {
	y := m.h - 1
	c.clear(0, y, m.w-1, y, cBar)
	pre := m.promptPrefix()
	x := c.put(0, y, pre, cAccRoute, true)
	text, _ := m.prompt.line.Window(max(m.w-widthOf(pre)-1, 1))
	x = c.put(x, y, text, cText, false)
	c.put(x+1, y, "  enter 実行 · esc 取り消し · $f = 選んだパス", cMuted, false)
}

// details は名前の後ろに出す詳細 (設定の Name details。spec §3.2)。出さないなら ""。
func (m *Model) details(n *node) string {
	if m.set.Details == "off" || m.set.Details == "" {
		return ""
	}
	age := shortAge(m.now().Sub(n.mtime).Seconds())
	size := ""
	if n.dir {
		if r, ok := m.recs[n.path()]; ok {
			size = shortSize(r.bytes)
		}
	} else {
		size = shortSize(n.size)
	}
	switch m.set.Details {
	case "age":
		return age
	case "size":
		return size
	}
	if size == "" {
		return age
	}
	return age + " · " + size
}

func (m *Model) detailsWidth() int {
	switch m.set.Details {
	case "age", "size":
		return 2 + 4
	case "both":
		return 2 + 11
	}
	return 0
}

// shortAge は now 5m 3h 2d 4w 8mo 3y (spec §3.2)。
func shortAge(s float64) string {
	switch {
	case s < 60:
		return "now"
	case s < 3600:
		return itoa(int(s/60)) + "m"
	case s < 86400:
		return itoa(int(s/3600)) + "h"
	case s < 7*86400:
		return itoa(int(s/86400)) + "d"
	case s < 30*86400:
		return itoa(int(s/(7*86400))) + "w"
	case s < 365*86400:
		return itoa(int(s/(30*86400))) + "mo"
	}
	return itoa(int(s/(365*86400))) + "y"
}

// shortSize は 980B 4.2K 12M 1.3G (4 桁以内。spec §3.2)。
func shortSize(n int64) string {
	units := []string{"B", "K", "M", "G", "T"}
	v := float64(n)
	i := 0
	for v >= 1000 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 || v >= 10 {
		return itoa(int(v+0.5)) + units[i]
	}
	return strconv.FormatFloat(v, 'f', 1, 64) + units[i]
}
