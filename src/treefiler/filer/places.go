package filer

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"atomicfile"
)

// places.go は前回の場所を覚える (設定の Remember place。spec §7 の places)。
// 保存先は設定と同じ置き場の treefiler-places (configDir。treebeard は ~/.local/state/tb/places)。
// 形式は treebeard の `= <起動したフォルダ>` に続けて `+ <開いていたフォルダ>` と `@ <カーソル>` に、treefiler だけの `> <d か スクロール> <タイル>`
// (奥から手前の順) を足したもの。新しいものが先頭、最大 placesKeep 件。

const placesKeep = 50

func placesPath() string { return configFile("treefiler-places") }

// Place は開いている場所: 起動したフォルダ・開いていたフォルダ (root 以外の展開済み)・カーソル (どれも絶対パス)・プレビューのタイル。
// Remember place の記録と、glogx が C-g で閉じて開き直したときの記憶 (glogx の filer_resume.go) が同じ形を使う。
type Place struct {
	Start  string      `json:"start"`
	Opened []string    `json:"opened,omitempty"`
	Cursor string      `json:"cursor,omitempty"`
	Tiles  []TilePlace `json:"tiles,omitempty"`
}

// TilePlace は開いていたプレビューのタイル 1 枚 (奥から手前の順に並べる)。
type TilePlace struct {
	Path   string `json:"path"`
	Diff   bool   `json:"diff,omitempty"`
	Scroll int    `json:"scroll,omitempty"`
}

// oneLine は 1 行に書いて同じ値で読み戻せるか。\r も外す: bufio.Scanner は行末の \r を落とすので、\r で終わるパスは
// 別のパスとして読み戻される。
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
		case '>':
			if tp, ok := parseTileLine(v); ok && len(ps) > 0 {
				ps[len(ps)-1].Tiles = append(ps[len(ps)-1].Tiles, tp)
			}
		}
	}
	return ps, sc.Err() == nil
}

// tileLine / parseTileLine は `>` の行の値 (`d <パス>` = diff、`<スクロール> <パス>` = 本体)。
func tileLine(tp TilePlace) string {
	if tp.Diff {
		return "d " + tp.Path
	}
	return strconv.Itoa(max(0, tp.Scroll)) + " " + tp.Path
}

func parseTileLine(v string) (TilePlace, bool) {
	head, path, ok := strings.Cut(v, " ")
	if !ok || path == "" {
		return TilePlace{}, false
	}
	if head == "d" {
		return TilePlace{Path: path, Diff: true}, true
	}
	n, err := strconv.Atoi(head)
	if err != nil {
		return TilePlace{}, false
	}
	return TilePlace{Path: path, Scroll: n}, true
}

// shutTiles は q (Esc・h・←・Enter) で最後の 1 枚を閉じたときのタイルと、そのときのカーソル。
// タイルを開いている間の q は閉じるだけなので、q q で抜けると終了の時点ではタイルが無い。それでも次に開いたときにプレビューへ戻す
// (2026-10-25 ユーザー回答「最後に見たプレビューを開き直す」)。抜けたときのカーソルが閉じたときと別の項目なら使わない: 戻した画面で
// タイルとカーソルが別のものを指す (照合はパスで、離れて戻ってきた場合は使う)。新しくタイルを開いたら捨てる (openTile)。
type shutTiles struct {
	cur   string
	tiles []TilePlace
}

// openTiles は閉じかけでないタイル (奥から手前の順)。
func (m *Model) openTiles() []TilePlace {
	var ts []TilePlace
	for _, t := range m.tiles {
		if !t.closing {
			ts = append(ts, TilePlace{Path: t.n.path(), Diff: t.mode == modeDiff, Scroll: t.scroll})
		}
	}
	return ts
}

// noteShut は手前のタイルを閉じる直前に呼ぶ。それが最後の 1 枚なら、閉じる前の姿を覚える。
func (m *Model) noteShut() {
	if ts := m.openTiles(); len(ts) == 1 {
		m.shut = shutTiles{cur: m.cur.path(), tiles: ts}
	}
}

// Place は今の開いている場所を返す。タイルが無く、閉じたばかりのタイルがあってカーソルが動いていなければ、それを載せる (shutTiles)。
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
	p.Tiles = m.openTiles()
	if len(p.Tiles) == 0 && m.shut.cur == p.Cursor {
		p.Tiles = m.shut.tiles
	}
	return p
}

// RestorePlace は p の開いていたフォルダとカーソルとタイルを戻す (無くなったパス・木の外のフォルダは飛ばす。カーソルが戻せなければ
// 動かさない)。タイルは木の外のファイルでも戻す (リンクから重ねて開いたもの)。起動したフォルダの照合は呼び出し側が行う。
// 今のタイルは p のタイルに置き換える (積み増さない): glogx は New が Remember place で戻した後に、自分の記憶でもう一度戻す。
func (m *Model) RestorePlace(p Place) {
	for _, t := range m.tiles {
		t.stopDiff()
	}
	m.tiles, m.shut = nil, shutTiles{}
	for _, o := range p.Opened {
		if n := m.loadPath(o); n != nil && n.dir {
			n.expanded = true
		}
	}
	m.reveal(p.Cursor)
	m.pulseFor = m.cur // 場所の復元はカーソルを動かしたのではない (経路の光を出さない。pulse.go)
	for _, tp := range p.Tiles {
		m.restoreTile(tp)
	}
}

// savePlace は今の開いたフォルダとカーソルとタイルを、起動したフォルダの記録として先頭に書く (設定が on のときだけ)。
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
		for _, tp := range p.Tiles {
			if oneLine(tp.Path) {
				b.WriteString("> " + tileLine(tp) + "\n")
			}
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
