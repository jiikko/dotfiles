package filer

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// tile.go はプレビューのタイル (spec §0.2)。ドラクエの対戦画面のように手前へ重なる。置き場所は 4 箇所で順に使い、
// 5 枚目は 1 枚目の場所に重なる。枚数の上限は無い。

type link struct {
	line, col int    // 本文の行と、その行の中の表示桁
	text      string // 本文に書かれた文字列
	path      string // 解決した実在のファイル
}

type tile struct {
	n       *node
	src     *textSource
	slot    int
	open    tween
	closing bool
	from    rect // 育ち始める矩形 (1 枚目は木のカーソル行、2 枚目以降は選んだリンク)
	scroll  int
	sy      tween
	jump    bool
	sel     int
	links   map[int][]link // 行ごとのリンク (計算済みの行だけ)
}

// slotRect は i 番目の置き場所 (spec §0.2)。1 枚目は中央、2〜4 枚目は中央から右下へ同じ幅ずつずらす。
func slotRect(i, w, h int) rect {
	tw, th := w*60/100, h*70/100
	cx, cy := (w-tw)/2, (h-th)/2
	dx := (w - tw - 2 - cx) / 3
	dy := (h - th - 1 - cy) / 3
	return rect{cx + i*dx, cy + i*dy, tw, th}
}

func lerpRect(a, b rect, t float64) rect {
	l := func(p, q int) int { return int(float64(p) + (float64(q)-float64(p))*t + 0.5) }
	return rect{l(a.x, b.x), l(a.y, b.y), l(a.w, b.w), l(a.h, b.h)}
}

// pathDelims はリンクの候補の切れ目 (空白と、パスの前後に来やすい記号)。
func pathDelim(r rune) bool {
	if unicode.IsSpace(r) {
		return true
	}
	return strings.ContainsRune("()[]{}<>\"'`,;|=", r)
}

// linksOf は行の中から、実在するファイルへ解決できるパスだけを拾う。本文の表記は root か、そのファイルのフォルダからの相対。
// 末尾の . : と `:12` (行番号) は外して解決する。
func linksOf(line string, ln int, root, base string, cache map[string]string) []link {
	var out []link
	i := 0
	for i < len(line) {
		r, size := decodeRune(line[i:])
		if pathDelim(r) {
			i += size
			continue
		}
		j := i
		for j < len(line) {
			r2, s2 := decodeRune(line[j:])
			if pathDelim(r2) {
				break
			}
			j += s2
		}
		tok := line[i:j]
		if p := resolveLink(tok, root, base, cache); p != "" {
			out = append(out, link{line: ln, col: widthOf(line[:i]), text: trimLinkTail(tok), path: p})
		}
		i = j
	}
	return out
}

func trimLinkTail(tok string) string {
	tok = strings.TrimRight(tok, ".:")
	if k := strings.LastIndex(tok, ":"); k > 0 && isAllDigits(tok[k+1:]) {
		tok = tok[:k]
	}
	return tok
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if !isDigit(s[i]) {
			return false
		}
	}
	return true
}

func resolveLink(tok, root, base string, cache map[string]string) string {
	t := trimLinkTail(tok)
	if t == "" || t == "." || t == ".." || !strings.ContainsAny(t, "/.") {
		return ""
	}
	if p, ok := cache[t]; ok {
		return p
	}
	found := ""
	for _, dir := range []string{root, base} {
		p := t
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, t)
		}
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			found = p
			break
		}
	}
	cache[t] = found
	return found
}

// tileLinks は読めている行のうち lines 番目までのリンク (計算済みは使い回す)。
func (m *Model) tileLinks(t *tile, upto int) []link {
	if t.links == nil {
		t.links = map[int][]link{}
	}
	var out []link
	base := filepath.Dir(t.n.path())
	for ln := 0; ln < upto && ln < len(t.src.lines); ln++ {
		ls, ok := t.links[ln]
		if !ok {
			ls = linksOf(t.src.lines[ln], ln, m.root.path(), base, m.linkCache)
			t.links[ln] = ls
		}
		out = append(out, ls...)
	}
	return out
}
