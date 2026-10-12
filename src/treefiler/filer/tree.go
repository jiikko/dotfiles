package filer

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jiikko/dotfiles/src/termsafe"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

// node は木の 1 項目。フォルダの中身は開いたときに初めて読む。
//
// 🚨 symlink は辿らない (lstat)。ディレクトリへの symlink は展開できない項目として扱う
// (treebeard と同じ。spec §5.1・§10)。辿ると循環と、root の外への走査が起きる。
type node struct {
	name     string // 表示に使う前に termsafe を通した名前
	raw      string // ディスク上の名前 (パスの組み立てに使う)
	abs      string // 絶対パス。作るときに 1 回だけ組み立てる (毎フレーム熱と git を引くので、親をたどって連結すると確保が増える)
	dir      bool
	parent   *node
	kids     []*node
	loaded   bool
	readErr  error
	expanded bool
	last     *node // 最後にカーソルがあった子 (潜ったときに戻る先)
	mtime    time.Time
	size     int64
	// 表示の名前と幅の覚え (label / labelCells)。名前は作ったときに決まり、変わるのは列の最大幅 (labelMax。設定) だけなので、
	// 覚えたときの labelMax と組で持つ。配置と描画は全ノードの幅を毎 tick 引く (5 万件のフォルダの layout で 3.6 → 3.1 ms。issue 711)
	labMax int
	lab    string
	labW   int
	// 見せる子の覚えと、親の覚えの中での自分の順位 (virt.go)。kidsVer は kids を並べ直した・読み直したら増やす
	kidsVer  int
	vis      *visKids
	vidx     int
	vgen     uint64
	freshGen uint64 // 親の覚えを作り直したときに新しく現れた子なら、その gen
	pvidx    int    // 親の覚えを作り直す前の順位 (pgen の覚えでの順位。前の目標の位置を出す)
	pgen     uint64
	// 子のブロックの配置の記録 (virt.go)。geo は今の layout、geoPrev はその前の layout のもの
	geo, geoPrev blockGeo
	// geoStep は step (アニメを進める) の layout の置き方、geoStepPrev はその前の step のもの。アニメの状態はこの置き方で進んでいる
	geoStep, geoStepPrev blockGeo
	openHold             holdClock // 子のブロックが現れた / 兄弟のずれ方がばらばらになる変化があった時 (全部を動かす期間の起点)
	virtSeq              int       // 最後に仮想化した layout の通し番号
	wasCulled            bool      // 画面から遠いのでアニメを捨てた (次に配置したとき、湧かせずに前の位置から動かす)
	culledBatch          int       // 捨てたアニメの batch (戻すときに描く順を保つ)
}

func (n *node) path() string { return n.abs }

func (n *node) depth() int {
	d := 0
	for p := n.parent; p != nil; p = p.parent {
		d++
	}
	return d
}

func (n *node) hidden() bool { return hiddenName(n.raw) }

// hiddenName は dotfile の名前か (隠す設定の対象)。名前で判定する所 (裏の走査・explode・消えた項目) はここを通す。
func hiddenName(name string) bool { return strings.HasPrefix(name, ".") }

// 表示と並びの設定 (設定の板の Column width / Folders first / Natural sort)。applySettings が書き込む。
// 🚨 配色 (theme.go) と同じく UI の 1 本の goroutine だけが読み書きする。
var (
	labelMax         = 28 // 列の最大幅 (spec §3.2 の max_name 既定 28)
	sortFoldersFirst = false
	sortNatural      = true
)

func (n *node) label() string {
	n.cacheLabel()
	return n.lab
}

// labelCells は label の表示幅。
func (n *node) labelCells() int {
	n.cacheLabel()
	return n.labW
}

func (n *node) cacheLabel() {
	if n.labMax == labelMax && n.lab != "" {
		return
	}
	n.labMax, n.lab = labelMax, n.name
	if n.labW = termwidth.Of(n.name); n.labW > labelMax {
		n.lab = termwidth.Truncate(n.name, labelMax, "…")
		n.labW = termwidth.Of(n.lab)
	}
}

func newNode(raw string, info os.FileInfo, parent *node) *node {
	n := &node{raw: raw, abs: raw, name: termsafe.PlainLine(raw), parent: parent}
	if parent != nil {
		n.abs = filepath.Join(parent.abs, raw)
	}
	if info != nil {
		n.dir = info.IsDir()
		n.mtime = info.ModTime()
		n.size = info.Size()
	}
	return n
}

// newRoot は path を root にした木を作る。root は最初から開いておく。
func newRoot(path string) (*node, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	// root だけは symlink を辿る (macOS の /tmp /var /etc は symlink。辿らないと root が「ファイル」になり木が空になる)
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	n := newNode(abs, info, nil)
	n.name = termsafe.PlainLine(filepath.Base(abs))
	n.load()
	n.expanded = true
	return n, nil
}

// load はフォルダの中身を読む (1 段だけ)。失敗は readErr に残し、空のフォルダとして扱う。
func (n *node) load() {
	if !n.dir {
		return
	}
	n.loaded = true
	ents, err := os.ReadDir(n.path())
	if err != nil {
		n.readErr = err
		n.kids = nil
		return
	}
	kids := make([]*node, 0, len(ents))
	for _, e := range ents {
		// DirEntry.Info は symlink そのものを返す (lstat)
		info, err := e.Info()
		if err != nil {
			continue // 読む間に消えた
		}
		kids = append(kids, newNode(e.Name(), info, n))
	}
	sortKids(kids, sortFoldersFirst, sortNatural)
	n.kids = kids
	n.kidsVer++
}

// reload は読み込み済みのフォルダを読み直す。名前が同じ項目は古いノードをそのまま使う (開閉・last・アニメを保つ)。
// 開いていた子のフォルダも順に読み直す。
func (n *node) reload() {
	if !n.dir || !n.loaded {
		return
	}
	old := make(map[string]*node, len(n.kids))
	for _, k := range n.kids {
		old[k.raw] = k
	}
	n.load()
	for i, k := range n.kids {
		if o, ok := old[k.raw]; ok && o.dir == k.dir {
			o.mtime, o.size = k.mtime, k.size
			n.kids[i] = o
			o.reload()
		}
	}
	n.kidsVer++ // 新しい並びへ古いノードを差し戻した
	if n.last != nil && !contains(n.kids, n.last) {
		n.last = nil
	}
}

// sortKids は名前順に並べる (spec §5.6)。foldersFirst ならフォルダを上に。
func sortKids(kids []*node, foldersFirst, natural bool) {
	sort.SliceStable(kids, func(i, j int) bool {
		if foldersFirst && kids[i].dir != kids[j].dir {
			return kids[i].dir
		}
		return lessName(kids[i].raw, kids[j].raw, natural)
	})
}

// lessName は名前順 (大文字小文字を無視し、natural なら数字の連なりを数として比べる。同じなら元の名前。spec §5.6)。
func lessName(a, b string, natural bool) bool {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	c := strings.Compare(la, lb)
	if natural {
		c = naturalCmp(la, lb)
	}
	if c != 0 {
		return c < 0
	}
	return a < b
}

// naturalCmp は数字の連なりを数として比べる (file2 < file10、file2 < file02)。
func naturalCmp(a, b string) int {
	for a != "" && b != "" {
		da, db := isDigit(a[0]), isDigit(b[0])
		if da && db {
			na, ra := digitRun(a)
			nb, rb := digitRun(b)
			ta, tb := strings.TrimLeft(na, "0"), strings.TrimLeft(nb, "0")
			switch {
			case len(ta) != len(tb):
				return cmpInt(len(ta), len(tb))
			case ta != tb:
				return strings.Compare(ta, tb)
			case len(na) != len(nb):
				return cmpInt(len(na), len(nb))
			}
			a, b = ra, rb
			continue
		}
		if a[0] != b[0] {
			return cmpInt(int(a[0]), int(b[0]))
		}
		a, b = a[1:], b[1:]
	}
	return cmpInt(len(a), len(b))
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func digitRun(s string) (string, string) {
	i := 0
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	return s[:i], s[i:]
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
