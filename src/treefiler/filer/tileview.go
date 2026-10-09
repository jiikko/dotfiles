package filer

import (
	"path/filepath"
	"strings"

	"github.com/jiikko/dotfiles/src/tuikit/highlight"
	"github.com/jiikko/dotfiles/src/tuikit/markdown"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

// tileview.go はタイルに出す「表示の行」(spec §8.3)。元の行 (textSource) から、モード (本体 / diff)・Markdown の整形・
// コードの色付け・折り返しを経て作る。スクロール・位置表示・スクロールバー・リンク (Tab) はこの行の上で数える。
// 外部コマンド (glow / bat) は呼ばない (spec §0.1): Markdown は tuikit/markdown、コードは tuikit/highlight (chroma)。

type viewLine struct {
	styled string // SGR つき (canvas.putSGR で描く)
	plain  string // 同じ文字列から SGR を外したもの (リンクを探す・幅を数える)
}

type tileMode int

const (
	modeFile tileMode = iota
	modeDiff
)

// viewKey が変わったときだけ表示の行を作り直す。
type viewKey struct {
	w        int
	wrap     bool
	mode     tileMode
	n        int // 元の行の数 (diff なら diff の行の数)
	eof      bool
	diffDone bool
}

// markdownCap は Markdown を整形するために読む上限 (整形は全体を見るので、先頭からここまでを読む)。
// 🚨 spec §5.5 の head (2 MiB) より小さくしている: 整形は UI の goroutine で同期に走る。上限 + 1 MiB のファイルを開いて G を押す
// テスト (TestMarkdownReadsUpToCap。ファイルの書き出しを含む) の所要が、上限 2 MiB で 1.83 秒、256 KiB で 0.06 秒
// (2026-10-09。ファイルの大きさも違い、整形だけの時間は分けて測っていない)。
const markdownCap = 256 << 10

func isMarkdown(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".md" || ext == ".markdown"
}

// setSource はタイルの中身を n に差し替え、表示の状態を初めに戻す (開いたとき・J K で隣へ送ったとき)。
func (t *tile) setSource(n *node, src *textSource) {
	t.n, t.src = n, src
	t.links, t.scroll, t.sy, t.jump, t.sel = nil, 0, tween{}, false, 0
	t.stopDiff()
	t.mode, t.diff = modeFile, nil
	t.vlines, t.vkey, t.vsrc = nil, viewKey{}, 0
	t.hl = nil
	if isMarkdown(n.raw) {
		// G などで先へ読んでも、ここを超えて読まない (超えるたびに全体を整形し直すことになる)。
		// 🚨 描く前のキーでも読み進めるので、中身を決めたここで付ける
		src.limit = markdownCap
	} else {
		t.hl = highlight.ForPath(n.raw)
	}
}

func (m *Model) tileTextWidth(t *tile) int { return max(slotRect(t.slot, m.w, m.canvasH()).w-4, 1) }

// view はタイルの表示の行 (作ってあれば使い回す)。
func (m *Model) view(t *tile) []viewLine {
	w := m.tileTextWidth(t)
	if t.mode == modeDiff {
		lines, done := t.diff.result()
		k := viewKey{w: w, mode: modeDiff, n: len(lines), diffDone: done}
		if k != t.vkey || t.vlines == nil {
			styled := highlight.Diff(lines)
			t.vlines = make([]viewLine, len(lines))
			for i, l := range lines {
				t.vlines[i] = viewLine{styled[i], l}
			}
			t.vkey, t.links = k, nil
		}
		return t.vlines
	}
	md := isMarkdown(t.n.raw)
	if md {
		t.src.ensureBytes(markdownCap)
	}
	k := viewKey{w: w, wrap: m.set.Wrap, n: len(t.src.lines), eof: t.src.eof}
	if k == t.vkey && t.vlines != nil {
		return t.vlines
	}
	// 幅・折り返し・モードが同じで行が増えただけなら、増えた行だけを足す (先へ読み進めるたびに全部を色付けし直さない)
	grow := !md && t.vlines != nil && k.w == t.vkey.w && k.wrap == t.vkey.wrap && t.vkey.mode == modeFile
	if !grow {
		t.vlines, t.vsrc, t.links = t.vlines[:0], 0, nil
	}
	if md {
		out, _ := markdown.Render(strings.Join(t.src.lines, "\n"), w, true)
		for _, o := range out {
			t.vlines = append(t.vlines, viewLine{o, stripSGR(o)})
		}
	} else {
		for _, l := range t.src.lines[t.vsrc:] {
			pieces := []string{l}
			if k.wrap {
				pieces = wrapCells(l, w)
			}
			for _, p := range pieces {
				st := p
				if t.hl != nil {
					st = t.hl(p)
				}
				t.vlines = append(t.vlines, viewLine{st, p})
			}
		}
		t.vsrc = len(t.src.lines)
	}
	if t.vlines == nil {
		t.vlines = []viewLine{}
	}
	t.vkey = k
	return t.vlines
}

// wrapCells は s を w 桁ごとに切る (全角を途中で割らない)。空の行は空の 1 行。
func wrapCells(s string, w int) []string {
	if termwidth.Of(s) <= w {
		return []string{s}
	}
	var out []string
	start, col := 0, 0
	for i := 0; i < len(s); {
		cl, cw := termwidth.FirstCluster(s[i:])
		if col+cw > w && col > 0 {
			out = append(out, s[start:i])
			start, col = i, 0
		}
		col += cw
		i += len(cl)
	}
	return append(out, s[start:])
}

// stripSGR は SGR (ESC [ ... m) とその他の CSI を外す。
func stripSGR(s string) string {
	if !strings.Contains(s, "\x1b") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b {
			b.WriteByte(s[i])
			continue
		}
		if i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && (s[i] < 0x40 || s[i] > 0x7e) {
				i++
			}
		}
	}
	return b.String()
}

// ensureBytes は末尾か、先頭から max バイトまで読み進める。
func (s *textSource) ensureBytes(maxBytes int64) {
	for !s.eof && s.off < maxBytes {
		before := s.off
		s.ensure(len(s.lines) + 1000)
		if s.off == before && !s.eof {
			return // 読み進められない (行の数だけが足りていた)
		}
	}
}

// diffBusy は diff を取りに行っているタイルがあるか。
func (m *Model) diffBusy() bool {
	for _, t := range m.tiles {
		if t.diff != nil {
			if _, done := t.diff.result(); !done {
				return true
			}
		}
	}
	return false
}
