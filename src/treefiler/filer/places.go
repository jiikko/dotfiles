package filer

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"atomicfile"
)

// places.go は前回の場所を覚える (設定の Remember place。spec §7 の places)。
// 保存先は ~/.config/glogx/treefiler-places (設定と同じ置き場所。treebeard は ~/.local/state/tb/places)。
// 形式は treebeard と同じ: `= <起動したフォルダ>` に続けて `+ <開いていたフォルダ>` と `@ <カーソル>`。新しいものが先頭、最大 placesKeep 件。

const placesKeep = 50

func placesPath() string {
	d := configDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "treefiler-places")
}

type savedPlace struct {
	start  string
	opened []string
	cursor string
}

// readPlaces は記録を読む。ok=false は読み切れなかった (書き戻すと読めなかった残りを消すので、呼び出し側は書かない)。
func readPlaces(path string) ([]savedPlace, bool) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, true
	}
	if err != nil {
		return nil, false
	}
	defer f.Close()
	var ps []savedPlace
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if len(line) < 2 {
			continue
		}
		v := line[2:]
		switch line[0] {
		case '=':
			ps = append(ps, savedPlace{start: v})
		case '+':
			if len(ps) > 0 {
				ps[len(ps)-1].opened = append(ps[len(ps)-1].opened, v)
			}
		case '@':
			if len(ps) > 0 {
				ps[len(ps)-1].cursor = v
			}
		}
	}
	return ps, sc.Err() == nil
}

// savePlace は今の開いたフォルダとカーソルを、起動したフォルダの記録として先頭に書く (設定が on のときだけ)。
func (m *Model) savePlace() {
	if !m.set.Remember {
		return
	}
	path := placesPath()
	if path == "" {
		return
	}
	cur := savedPlace{start: m.startDir, cursor: m.cur.path()}
	var walk func(n *node)
	walk = func(n *node) {
		if n != m.root && n.dir && n.expanded {
			cur.opened = append(cur.opened, n.path())
		}
		for _, k := range n.kids {
			walk(k)
		}
	}
	walk(m.root)
	old, ok := readPlaces(path)
	if !ok {
		return
	}
	ps := []savedPlace{cur}
	for _, p := range old {
		if p.start != cur.start && len(ps) < placesKeep {
			ps = append(ps, p)
		}
	}
	var b strings.Builder
	for _, p := range ps {
		if strings.ContainsAny(p.start+p.cursor, "\n") {
			continue // 改行を含むパスは保存しない (行の形式が壊れる)
		}
		b.WriteString("= " + p.start + "\n")
		for _, o := range p.opened {
			if !strings.Contains(o, "\n") {
				b.WriteString("+ " + o + "\n")
			}
		}
		if p.cursor != "" {
			b.WriteString("@ " + p.cursor + "\n")
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = atomicfile.Write(path, []byte(b.String()), 0o644) // 覚えられなくても操作は続ける (次に開いたときに戻らないだけ)
}

// restorePlace は同じフォルダから開いたときの記録を戻す (設定が on のときだけ。無くなったパスは飛ばす)。
func (m *Model) restorePlace() {
	if !m.set.Remember {
		return
	}
	ps, _ := readPlaces(placesPath()) // 途中までしか読めなくても、読めた分では戻す
	for _, p := range ps {
		if p.start != m.startDir {
			continue
		}
		for _, o := range p.opened {
			if n := m.loadPath(o); n != nil && n.dir {
				n.expanded = true
			}
		}
		if n := m.loadPath(p.cursor); n != nil && m.inTree(n) {
			for a := n.parent; a != nil; a = a.parent {
				a.expanded = true
			}
			m.setCur(n)
		}
		return
	}
}
