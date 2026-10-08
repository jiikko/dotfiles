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
	dir      bool
	parent   *node
	kids     []*node
	loaded   bool
	readErr  error
	expanded bool
	last     *node // 最後にカーソルがあった子 (潜ったときに戻る先)
	mtime    time.Time
	size     int64
	git      byte // 0 / '?' / '+' / 'M' / '!' (spec §5.3)
}

func (n *node) path() string {
	if n.parent == nil {
		return n.raw
	}
	return filepath.Join(n.parent.path(), n.raw)
}

func (n *node) depth() int {
	d := 0
	for p := n.parent; p != nil; p = p.parent {
		d++
	}
	return d
}

func (n *node) hidden() bool { return strings.HasPrefix(n.raw, ".") }

// maxName は列の最大幅 (spec §3.2 の max_name 既定 28)。
const maxName = 28

func (n *node) label() string {
	if termwidth.Of(n.name) <= maxName {
		return n.name
	}
	return termwidth.Truncate(n.name, maxName, "…")
}

func newNode(raw string, info os.FileInfo, parent *node) *node {
	n := &node{raw: raw, name: termsafe.PlainLine(raw), parent: parent}
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
	sort.SliceStable(kids, func(i, j int) bool { return lessName(kids[i].raw, kids[j].raw) })
	n.kids = kids
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
	if n.last != nil && !contains(n.kids, n.last) {
		n.last = nil
	}
}

// lessName は名前順 (大文字小文字を無視した自然順。同じなら元の名前。spec §5.6)。
func lessName(a, b string) bool {
	if c := naturalCmp(strings.ToLower(a), strings.ToLower(b)); c != 0 {
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
