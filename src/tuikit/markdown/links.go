package markdown

import "github.com/jiikko/dotfiles/src/tuikit/termwidth"

// LinkKind はリンク候補の出どころ。開く側 (glogx issues viewer) がパスを解決する基準を
// 種類ごとに 1 つに決めるために区別する (markdown リンクはそのファイル基準、コードスパンは
// 呼び出し側の基準。基準を複数試すと別のファイルを開くので、ここで種類を潰さない)。
type LinkKind uint8

const (
	// LinkDest は `[label](dest)` の dest (ラベルが画面に出る)。
	LinkDest LinkKind = iota
	// LinkCode はインラインコード `` `x` `` の中身。パスかどうかはここでは判定しない。
	LinkCode
)

// Link は整形後の本文に現れたリンク候補 1 回ぶん。Segs は画面上の位置 (折り返しで 2 行に
// 割れたリンクは 1 つの Link に 2 つの Seg)。並びは画面の出現順 (上の行から、行内は左から)。
//
// 🚨 画面に 1 桁も出ないリンク (表のセルの切り詰めで消えた等) は載らない。したがって
// 番号 (links 内の添字) は**同じ幅で整形した結果の中でだけ**意味を持つ。幅が変わったら取り直す。
type Link struct {
	Kind LinkKind
	Dest string
	Segs []Seg
}

// Seg は 1 行の中のリンクの区間 (Line は Render が返す行の添字、Col / Width は表示桁)。
type Seg struct{ Line, Col, Width int }

// LinkMark はリンクに塗る強調。
type LinkMark uint8

const (
	LinkPlain    LinkMark = iota // 強調なし (Render と同じ見た目)
	LinkMarked                   // 選べるリンク (下線)
	LinkSelected                 // 選択中 (反転 + 太字)
)

// linkRef はリンク 1 出現の同一性 (span.link の doc)。
type linkRef struct {
	kind LinkKind
	dest string
}

// collectLinks は整形済みの行からリンクの位置を集める。index は ref → links の添字。
//
// 桁は clipToWidth (出口の切り詰め) を考慮する: 行が width を超えると末尾 1 桁が "…" に
// なるので、そこから先の区間は画面に出ない。
func collectLinks(lines []line, width int) (links []Link, index map[*linkRef]int) {
	index = map[*linkRef]int{}
	for li, l := range lines {
		limit := width
		if lineWidth(l) > width {
			limit = width - 1
		}
		col := 0
		for _, sp := range l.spans {
			w := termwidth.Of(sp.Text)
			if sp.link != nil && col < limit && w > 0 {
				// 🚨 min(w, limit-col) にしない: 残り 1 桁から始まる幅 2 の字は clipToWidth が字ごと落とすので、
				// 見える桁は「残りに収まる字だけ」で数える (0 なら画面に出ていない)
				vis := w
				if col+w > limit {
					vis = termwidth.Of(termwidth.Truncate(sp.Text, limit-col, ""))
				}
				if vis == 0 {
					col += w
					continue
				}
				i, ok := index[sp.link]
				if !ok {
					i = len(links)
					index[sp.link] = i
					links = append(links, Link{Kind: sp.link.kind, Dest: sp.link.dest})
				}
				segs := links[i].Segs
				// 同じリンクの中で style が変わってスパンが割れた (ラベル内のコードスパン) だけなら
				// 1 区間につなぐ
				if n := len(segs); n > 0 && segs[n-1].Line == li && segs[n-1].Col+segs[n-1].Width == col {
					segs[n-1].Width += vis
				} else {
					links[i].Segs = append(segs, Seg{Line: li, Col: col, Width: vis})
				}
			}
			col += w
		}
	}
	return links, index
}

// lineWidth は 1 行の表示幅 (塗る前)。
func lineWidth(l line) int {
	w := 0
	for _, sp := range l.spans {
		w += termwidth.Of(sp.Text)
	}
	return w
}
