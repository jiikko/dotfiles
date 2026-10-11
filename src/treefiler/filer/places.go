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

// Place は開いている場所: 起動したフォルダ・開いていたフォルダ (root 以外の展開済み)・カーソル (どれも絶対パス)。
// Remember place の記録と、glogx が C-g で閉じて開き直したときの記憶 (glogx の filer_resume.go。issue 710) が同じ形を使う。
type Place struct {
	Start  string   `json:"start"`
	Opened []string `json:"opened,omitempty"`
	Cursor string   `json:"cursor,omitempty"`
}

// oneLine は 1 行に書いて同じ値で読み戻せるか。\r も外す: bufio.Scanner は行末の \r を落とすので、\r で終わるパスは
// 別のパスとして読み戻される (issue 696 の 14)。
func oneLine(s string) bool { return !strings.ContainsAny(s, "\r\n") }

// readPlaces は記録を読む。ok=false は読み切れなかった (書き戻すと読めなかった残りを消すので、呼び出し側は書かない)。
func readPlaces(path string) ([]Place, bool) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, true
	}
	if err != nil {
		return nil, false
	}
	defer f.Close()
	var ps []Place
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if len(line) < 2 {
			continue
		}
		v := line[2:]
		switch line[0] {
		case '=':
			ps = append(ps, Place{Start: v})
		case '+':
			if len(ps) > 0 {
				ps[len(ps)-1].Opened = append(ps[len(ps)-1].Opened, v)
			}
		case '@':
			if len(ps) > 0 {
				ps[len(ps)-1].Cursor = v
			}
		}
	}
	return ps, sc.Err() == nil
}

// Place は今の開いている場所を返す。
func (m *Model) Place() Place {
	p := Place{Start: m.startDir, Cursor: m.cur.path()}
	var walk func(n *node)
	walk = func(n *node) {
		if n != m.root && n.dir && n.expanded {
			p.Opened = append(p.Opened, n.path())
		}
		for _, k := range n.kids {
			walk(k)
		}
	}
	walk(m.root)
	return p
}

// RestorePlace は p の開いていたフォルダとカーソルを戻す (無くなったパス・木の外のパスは飛ばす。カーソルが戻せなければ動かさない)。
// 起動したフォルダの照合は呼び出し側が行う。
func (m *Model) RestorePlace(p Place) {
	for _, o := range p.Opened {
		if n := m.loadPath(o); n != nil && n.dir {
			n.expanded = true
		}
	}
	if n := m.loadPath(p.Cursor); n != nil && m.inTree(n) {
		for a := n.parent; a != nil; a = a.parent {
			a.expanded = true
		}
		m.setCur(n)
	}
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
	cur := m.Place()
	old, ok := readPlaces(path)
	if !ok {
		return
	}
	ps := []Place{cur}
	for _, p := range old {
		if p.Start != cur.Start && len(ps) < placesKeep {
			ps = append(ps, p)
		}
	}
	var b strings.Builder
	for _, p := range ps {
		if !oneLine(p.Start + p.Cursor) {
			continue // 改行を含むパスは保存しない (行の形式が壊れる)
		}
		b.WriteString("= " + p.Start + "\n")
		for _, o := range p.Opened {
			if oneLine(o) {
				b.WriteString("+ " + o + "\n")
			}
		}
		if p.Cursor != "" {
			b.WriteString("@ " + p.Cursor + "\n")
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
		if p.Start == m.startDir {
			m.RestorePlace(p)
			return
		}
	}
}
