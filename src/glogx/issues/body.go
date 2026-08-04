package issues

import (
	"regexp"
	"strings"
)

// Body は issue 1 件の本文と、整形結果の幅ごとのキャッシュ。
//
// なぜキャッシュするか: 整形は「ブロック分解 → インライン解析 → 折り返し」を通るので本文
// 1 件で数 ms かかる。TUI は 1 フレームごとに描画を呼ぶため、毎フレーム整形すると popup の
// スクロールが重くなる。同じ (width, colored) なら結果は同じなので、幅が変わったときだけ
// 作り直す (リサイズ 1 回につき 1 回)。
type Body struct {
	src      string
	width    int
	colored  bool
	lines    []string
	srcLines []int // lines と同じ並びのソース行番号 (0 = 出さない)
	renders  int   // 整形した回数 (キャッシュが効いているかのテスト用)
}

// NewBody は本文から Body を作る。整形はまだ行わない (最初の Lines で遅延実行)。
func NewBody(src string) *Body { return &Body{src: src, width: -1} }

// Lines は width 桁へ整形した行を返す。前回と同じ条件ならキャッシュを返す。
func (b *Body) Lines(width int, colored bool) []string {
	if b.lines != nil && b.width == width && b.colored == colored {
		return b.lines
	}
	b.width, b.colored = width, colored
	b.lines, b.srcLines = RenderBody(b.src, width, colored)
	b.renders++
	return b.lines
}

// SrcLines は Lines と同じ並びのソース (.md) 行番号 (0 = その行には出さない)。
// ⚠️ Lines を呼ぶ前は空。整形しないと行の対応が決まらないため。
func (b *Body) SrcLines() []int { return b.srcLines }

// SrcLineCount は本文のソース行数。行番号の溝を何桁取るかを整形前に決めるのに使う
// (溝幅が決まらないと整形幅が決まらず、整形しないと行番号が決まらない循環を切る)。
func (b *Body) SrcLineCount() int { return strings.Count(b.src, "\n") + 1 }

// Len は最後に整形した行数 (未整形なら 0)。pager のスクロール上限に使う。
func (b *Body) Len() int { return len(b.lines) }

// urlRe は本文から拾う http(s) URL。終端は空白か、URL の外側に来やすい記号で切る。
//
// なぜ記号を除くか: issue 本文の URL は markdown リンク `[text](url)`、括弧書き `(url)`、
// 文末の句点つき `url。` のいずれの形でも現れる。貪欲に拾うと `)` や `。` が URL に混ざり、
// ブラウザが 404 を開く。逆に `?` `#` `=` `&` は query / fragment の一部なので残す。
//
// ⚠️ 制御文字 (\x00-\x1f\x7f) も終端に含める。URL に生の制御文字は入らないので実用上の損失は
// 無く、含めないと `https://example.com/<ESC>]0;…<BEL>` が 1 本の URL として抽出され、URL
// ピッカーの一覧描画で端末へ素通りする (URLs は整形経路を通らず生ソースから拾うため、
// renderMarkdown 側の無害化では守れない)。\s は \t\n\r\f\v しか外さない。
var urlRe = regexp.MustCompile(`https?://[^\s)\]>"'` + "`" + `\x00-\x1f\x7f]+`)

// URLs は本文に現れる http(s) URL を出現順で返す (重複は最初の 1 つだけ)。
//
// 重複を落とすのは、同じ URL が「参考」節と本文の両方に出る issue が実在し、順に開く操作
// (viewer の u キー) で同じページを 2 回開かされるのを避けるため。整形 (Lines) とは独立に
// 生ソースから拾う: 折り返しで URL が行をまたいで割れても、元のソースには 1 本で入っている。
func (b *Body) URLs() []string {
	found := urlRe.FindAllString(b.src, -1)
	seen := make(map[string]bool, len(found))
	out := make([]string, 0, len(found))
	for _, u := range found {
		// 末尾の句読点は URL の一部でないことが多い (英文の "…/foo." と日本語の "…/foo。")。
		// 記号だけを削り、パス区切りの / は残す。
		u = strings.TrimRight(u, ".,;:。、")
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, u)
	}
	return out
}
