package issues

// Body は issue 1 件の本文と、整形結果の幅ごとのキャッシュ。
//
// なぜキャッシュするか: 整形は「ブロック分解 → インライン解析 → 折り返し」を通るので本文
// 1 件で数 ms かかる。TUI は 1 フレームごとに描画を呼ぶため、毎フレーム整形すると popup の
// スクロールが重くなる。同じ (width, colored) なら結果は同じなので、幅が変わったときだけ
// 作り直す (リサイズ 1 回につき 1 回)。
type Body struct {
	src     string
	width   int
	colored bool
	lines   []string
	renders int // 整形した回数 (キャッシュが効いているかのテスト用)
}

// NewBody は本文から Body を作る。整形はまだ行わない (最初の Lines で遅延実行)。
func NewBody(src string) *Body { return &Body{src: src, width: -1} }

// Lines は width 桁へ整形した行を返す。前回と同じ条件ならキャッシュを返す。
func (b *Body) Lines(width int, colored bool) []string {
	if b.lines != nil && b.width == width && b.colored == colored {
		return b.lines
	}
	b.width, b.colored = width, colored
	b.lines = RenderBody(b.src, width, colored)
	b.renders++
	return b.lines
}

// Len は最後に整形した行数 (未整形なら 0)。pager のスクロール上限に使う。
func (b *Body) Len() int { return len(b.lines) }
