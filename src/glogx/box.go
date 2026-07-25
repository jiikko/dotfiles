package main

import (
	"strings"
)

// 枠描画のプリミティブ (browseModel の状態に依存しない純関数)。状態機械 (tui.go) から
// 描画の下請けを分離する。ここに置くのは「window/box という []string を受けて []string を返す」
// レイアウト関数だけ。m.width/m.colored 等のモデル状態を読むもの (cursorLine/bgLine/panelLines
// など) は tui.go に残す。

// centerBox は狭い幅 (最大 44) の影付きモーダル行を組む。水平センタリングと背景リストへの
// 合成は描画時に overlayCenteredBox が行う (行を塗り潰さず左右の背景を残す)。action モーダルが使う。
func centerBox(title string, rows []string, width int, colored bool) []string {
	if width <= 0 {
		width = 80
	}
	return buildShadowPanelBox(title, rows, min(44, width), colored, ansiDim)
}

// overlayBox は box をウィンドウの anchor 位置へ重ねる (リスト行を置き換える)。
// 下に収まらない場合はビューポート内へ収まる位置まで引き上げる。
func overlayBox(window, box []string, anchor, page int) []string {
	start := min(anchor, max(page-len(box), 0))
	start = max(start, 0)
	for i, p := range box {
		pos := start + i
		if pos < len(window) {
			window[pos] = p
		} else if len(window) < page {
			window = append(window, p)
		}
	}
	return window
}

// overlayCenteredBox は box を画面の水平中央に「浮かせて」重ねる。overlayBox が行を塗り潰すのに
// 対し、こちらは各行で box が占める列だけを box に差し替え、左右の背景リストは残して合成する
// (右上の usage overlay と同じ発想を中央寄せに広げたもの)。垂直は page 内で中央に置く。
// 左側は truncateKeepANSI で prefix を保持し、右側は dropToColumn で box の右端以降を復元する。
// box 行の直前/直後に reset を挟み、背景の色が box に、box の色が右背景に滲まないようにする。
func overlayCenteredBox(window, box []string, width, page int, colored bool) []string {
	if len(box) == 0 || len(window) == 0 || width <= 0 {
		return window
	}
	reset := ""
	if colored {
		reset = ansiReset
	}
	bw := 0
	for _, r := range box {
		bw = max(bw, dispWidth(r))
	}
	leftGap := max((width-bw)/2, 0)
	leftPad := strings.Repeat(" ", leftGap)
	start := min(max((page-len(box))/2, 0), max(page-len(box), 0))
	for i, boxRow := range box {
		pos := start + i
		if pos >= page {
			break
		}
		if pos >= len(window) {
			if len(window) < page {
				window = append(window, leftPad+boxRow) // 背景行が無い箇所は素の pad + box
			}
			continue
		}
		bg := window[pos]
		// 左背景: 先頭 leftGap 桁を保持し、足りなければ空白で leftGap ちょうどに詰める
		left := truncateKeepANSI(bg, leftGap)
		left += strings.Repeat(" ", max(leftGap-dispWidth(left), 0))
		// 右背景: box 行の右端 (leftGap + この行の表示幅) 以降を復元して継ぐ
		rowW := dispWidth(boxRow)
		right := dropToColumn(bg, leftGap+rowW)
		window[pos] = left + reset + boxRow + reset + right
	}
	return window
}

// panelBoxStyle は枠の見た目 (影の有無・罫線の字形・枠線の色) をまとめる。buildPanelBoxImpl の
// 引数が 7 個まで伸び、bool 2 連続 + 名前の似た glyphs/color が並んで呼び出し側から意味が
// 読めなくなったため畳んだ (issue 028 P1)。glyphs は字形、color は SGR で役割が別物。
type panelBoxStyle struct {
	shadow bool      // 右下ドロップシャドウを付けるか
	glyphs boxBorder // 罫線の字形 (borderLight / borderDouble)
	color  string    // 枠線の SGR 色 (通常 ansiDim。toast だけ種別色)
}

// buildPanelBox は枠線付きのパネルを組み立てる。行の実効幅は ANSI を除いて計算する。
func buildPanelBox(title string, rows []string, width int, colored bool) []string {
	return buildPanelBoxImpl(title, rows, width, colored, panelBoxStyle{glyphs: borderLight, color: ansiDim})
}

// buildShadowPanelBox は buildPanelBox の右下ドロップシャドウ付き版。呼び出し元は 4 系統の
// 小面積モーダル/トースト (centerBox 経由の action モーダル + toast / usage)
// と、画面最外周フレーム (wrapWindowFrame → buildPanelBoxImpl を直接呼ぶ) のみ。
//
// ⚠️ 影の適用方針: 小面積のモーダル/トーストと最外周フレームに限る。リストのテキストに重なる
// 大面積 popup (job/diff パネル) への全面シャドウは「面積が大きく影が主張しすぎる」で一度導入 →
// revert した (4fb36a2)。最外周フレームは画面端の余白セルにだけ影を落としコンテンツと重ならない
// ため、この方針と衝突しない (issue 025)。
// border は枠線 (上辺・側辺・非影下辺) の SGR 色。ドロップシャドウのブロックは中立のまま (dim)。
// 通常は ansiDim を渡す。toast だけが種別色 (緑/赤/シアン) を渡して枠ごと色付けする。
func buildShadowPanelBox(title string, rows []string, width int, colored bool, border string) []string {
	return buildPanelBoxImpl(title, rows, width, colored, panelBoxStyle{shadow: true, glyphs: borderLight, color: border})
}

// wrapWindowFrame は画面全体のコンテンツ (リスト + overlay 群を合成済みの window) を、最外周に
// 余白を残した枠 + 右下ドロップシャドウで包み「板がターミナル地色の上に浮いている」見た目にする
// (issue 025)。影の幾何は buildPanelBoxImpl(shadow=true) へ完全委譲する (影の実装は 1 箇所に保つ)。
// 返す行数 = len(content) + 4 (上余白 + 上辺 + 下辺 + 下影)。左右余白 1 桁ずつ + 影 1 桁で、
// footprint は termW に収まる (呼び出し側の contentWidth()/frameVOverhead と一致)。
// 罫線は ansiFrameBorder (scratch と同じマゼンタ) で染める。落ち影は中立 dim のまま
// (buildShadowPanelBox の方針と同じ。トーストが枠だけ種別色にして影を据え置いたのと同型)。
func wrapWindowFrame(content []string, termW int, colored bool) []string {
	// -2 = 左右余白 1 桁ずつ。二重罫線 (ユーザー要望)
	box := buildPanelBoxImpl("", content, termW-2, colored, panelBoxStyle{shadow: true, glyphs: borderDouble, color: ansiFrameBorder})
	out := make([]string, 0, len(box)+1)
	out = append(out, "") // 上余白 1 行 (端末地色)
	for _, l := range box {
		out = append(out, " "+l) // 左余白 1 桁
	}
	return out
}

// shadowBoxChrome は影付き枠 (buildShadowPanelBox) が内容幅に加える固定分
// ("│ " + " │" + 影 1 桁 = 5)。box の性質なので box.go が出典 (usage overlay 固有ではない。
// 以前は usage_overlay.go の usageBoxChrome を toast が借用しており、usage 側の都合で値を
// 変えるとトーストが無言で崩れる結合があった。issue 028 P4)。
const shadowBoxChrome = 5

// minPanelWidth は枠の最小幅 (これ未満の width は buildPanelBoxImpl がここまで押し上げる)。
const minPanelWidth = 10

// panelInnerWidth は枠幅から本文 (row) に使える表示幅を返す。内訳は左 "│ " + 右 " │" の 4 桁。
// buildPanelBoxImpl と withScrollbar が同じ式を共有し、罫線の内訳を変えたときに片方だけ
// ずれて枠が崩れないようにする。
func panelInnerWidth(frameWidth int) int { return frameWidth - 4 }

// スクロールバーのグリフ。track は枠の側辺 (│) と同じ字形にして「本文の中に走る細い溝」に見せ、
// thumb だけ █ で持ち上げる。
const (
	scrollbarTrackGlyph = "│"
	scrollbarThumbGlyph = "█"
)

// withScrollbar は buildPanelBox に渡す本文行の右端に 1 桁のスクロールバー列を足す。
// total は全行数、offset は本文先頭が全体の何行目か。全体が収まる (total <= len(rows)) ときは
// 行をそのまま返す (列を作らないので本文幅が戻る)。
//
// boxWidth は buildPanelBox に渡すのと同じ幅を受け取り、本文幅 (inner) を内部で再計算する
// (呼び出し側に枠の内訳を知らせない)。行はバー列 + 手前の空き 1 桁を除いた幅へクリップする。
func withScrollbar(rows []string, boxWidth, total, offset int, colored bool) []string {
	view := len(rows)
	if view == 0 || total <= view {
		return rows
	}
	inner := panelInnerWidth(max(boxWidth, minPanelWidth))
	contentW := max(inner-2, 1) // バー 1 桁 + 手前の空き 1 桁
	// thumb 長は表示比率、位置は offset 比率。どちらも最低 1 行を確保し、末尾 (offset=maxOffset)
	// では thumb が下端に接地する。
	thumb := min(max(view*view/total, 1), view)
	maxOffset := total - view
	start := 0
	if travel := view - thumb; travel > 0 {
		start = min((offset*travel+maxOffset/2)/maxOffset, travel)
	}
	reset := ""
	if colored {
		reset = ansiReset // 行末で色を閉じ、本文の SGR がバー列へ滲まないようにする
	}
	out := make([]string, 0, view)
	for i, row := range rows {
		glyph := paint(scrollbarTrackGlyph, ansiDim, colored)
		if i >= start && i < start+thumb {
			glyph = scrollbarThumbGlyph
		}
		content := clipToWidth(row, contentW)
		pad := strings.Repeat(" ", max(contentW-dispWidth(content), 0))
		out = append(out, content+reset+pad+" "+glyph)
	}
	return out
}

// 落ち影は前景ブロック文字で描く (bg ベタ塗りではない)。近黒 fg の █ 本体 + 一段淡い ▓ の
// 縁で、グリフの隙間から端末の地色が透けて penumbra (半影) になり、角が柔らかく浮いて見える。
// 色なし (NO_COLOR) は近黒 fg が使えず、地色に対し █ だと明るく浮くため陰影文字 ▒ / ░ で
// 代用する (現状踏襲の淡いテクスチャ表現。濃淡は body=▒ / feather=░)。
const (
	shadowGlyphFull     = "█" // 本体 (最も濃い)
	shadowGlyphFeather  = "▓" // 縁のフェザー (一段淡い)
	shadowGlyphMono     = "▒" // NO_COLOR 本体
	shadowGlyphMonoEdge = "░" // NO_COLOR フェザー
)

// shadowBottomOffset は下端の影の左端を箱の左端から右へずらす桁数 (右下方向へ落とすドロップ
// シャドウの水平オフセット)。大きいほど影が右下に寄る。既定 1 桁から調整し 2 桁 (ユーザー要望
// 2026-07-23)。
const shadowBottomOffset = 2

// shadowRun は落ち影の本体 n セル分。
func shadowRun(n int, colored bool) string {
	if n <= 0 {
		return ""
	}
	if !colored {
		return strings.Repeat(shadowGlyphMono, n)
	}
	return ansiShadowFg + strings.Repeat(shadowGlyphFull, n) + ansiReset
}

// shadowFeather は落ち影の縁 1 セル (本体より一段淡く、影が地色へ溶ける ease-in/out 用)。
func shadowFeather(colored bool) string {
	if !colored {
		return shadowGlyphMonoEdge
	}
	return ansiShadowFg + shadowGlyphFeather + ansiReset
}

// boxBorder は枠の罫線文字 (角 + 横 + 縦)。小面積モーダル/パネルは borderLight (細い単線)、
// 最外周フレーム (wrapWindowFrame) は borderDouble (二重線) を使い分ける (issue 025・ユーザー要望
// 2026-07-24: フレームの罫線を太く/二重に)。全て表示幅 1 の box-drawing 文字 (EastAsianWidth=false
// 前提) なので幅計算は border 種別に依らず不変。
type boxBorder struct{ tl, tr, bl, br, h, v string }

var (
	borderLight  = boxBorder{tl: "┌", tr: "┐", bl: "└", br: "┘", h: "─", v: "│"}
	borderDouble = boxBorder{tl: "╔", tr: "╗", bl: "╚", br: "╝", h: "═", v: "║"}
)

// buildPanelBoxImpl が本体。shadow=true では右端 1 桁 (右影) と、その下に shadowBottomOffset 桁
// 右へずらした下端影の行を足し、板が左上光源で浮いて見える 3D 風にする。footprint は width の
// まま (枠自体を fw = width-1 に狭めて右影 1 桁分を捻出。下端影は行内の左詰めオフセットで表現)。
// st.glyphs は罫線種別 (上辺・側辺・非 shadow 下辺に効く。shadow 下辺は接地ブロック ▖▁▗ 固定で
// glyphs 非依存)。
func buildPanelBoxImpl(title string, rows []string, width int, colored bool, st panelBoxStyle) []string {
	shadow, b, border := st.shadow, st.glyphs, st.color
	if width < minPanelWidth {
		width = minPanelWidth
	}
	fw := width // 枠の幅 (shadow 時は残り 1 桁が右の影)
	if shadow {
		fw = width - 1
	}
	inner := panelInnerWidth(fw)
	lines := make([]string, 0, len(rows)+3)
	// タイトルは SGR 入りの job 名や commit subject がそのまま載る。ANSI を残すと
	// 幅計算 (Truncate/StringWidth) がずれて罫線が崩れ、タイトル全体の dim 塗りも
	// 途中でリセットされるため、タイトルに限っては ANSI を落とす
	title = truncateDisp(stripANSI(title), fw-2, "…")
	top := b.tl + title + strings.Repeat(b.h, max(fw-2-dispWidth(title), 0)) + b.tr
	if shadow {
		// 最上段だけ影なし (影は右上角の 1 つ下から始まるのが自然な落ち影)
		lines = append(lines, paint(top, border, colored)+" ")
	} else {
		lines = append(lines, paint(top, border, colored))
	}
	// 行ごとに変わらない断片はループの外で 1 度だけ組む。最外周フレーム (wrapWindowFrame) は
	// 毎フレーム全可視行をここへ通すので、行数 × フレームレートぶんの alloc になっていた。
	// 右影の上端 (最初の content 行) だけ ▓ フェザーで ease-in し、以降は █ 本体にする
	// (影は最上段に無い = top で 1 行分オフセットされるので、右影の「始まり」を柔らかくする)。
	leftEdge, rightEdge := paint(b.v+" ", border, colored), paint(" "+b.v, border, colored)
	var shadeFirst, shadeRest string
	if shadow {
		shadeFirst, shadeRest = shadowFeather(colored), shadowRun(1, colored)
	}
	for i, row := range rows {
		content := clipToWidth(row, inner)
		pad := max(inner-dispWidth(content), 0)
		shade := shadeRest
		if i == 0 {
			shade = shadeFirst
		}
		lines = append(lines, leftEdge+content+strings.Repeat(" ", pad)+rightEdge+shade)
	}
	// 下辺は shadow の有無で変える:
	//   - 通常箱: 上辺 ┌─┐ と同じ中央高の細い罫線 └─┘ (標準の枠)
	//   - shadow 箱: 最下段に寄せた低い横線 ▁ + 左右の角も最下段の低ブロック ▖ ▗ 。─ 中央高だと
	//     下の落ち影との間に半セルの余白ができ、└┘ の角だけ中央高だと横線 ▁ との間に段差が出る。
	//     角・横線ともセル最下段で高さを揃え、影に接した段差のない自然なドロップシャドウにする
	//     (ユーザー指摘 2026-07-23: ─ は余白 / ▁ 一様は角が開く / └┘ 角は段差 → 低ブロックの
	//     角 ▖ ▗ で接地と角閉じを両立。▖ + ▁×n + ▗)。
	var bottom string
	if shadow {
		// ▖▁▗ は「箱の下辺の枠線」(落ち影に接地させるため低ブロックで描くだけ) なので border 色で
		// 染める。落ち影本体 (この行末の shadowRun や次のオフセット行) は下で dim のまま描く。
		bottom = paint("▖"+strings.Repeat("▁", fw-2)+"▗", border, colored)
	} else {
		bottom = paint(b.bl+strings.Repeat(b.h, fw-2)+b.br, border, colored)
	}
	if shadow {
		// 右下角の影は最も深いので █ 本体。
		lines = append(lines, bottom+shadowRun(1, colored))
		// 下端の影: 左端を shadowBottomOffset 桁だけ右へずらして右下方向に落とす (古典的な
		// ドロップシャドウ)。左端を ▓ フェザーで ease-in してから █ 本体を敷く。右端は箱の右影列と
		// 揃える (影全体の幅 = shadowBottomOffset + フェザー1 + 本体 = width)。
		lines = append(lines, strings.Repeat(" ", shadowBottomOffset)+shadowFeather(colored)+shadowRun(width-1-shadowBottomOffset, colored))
	} else {
		lines = append(lines, bottom)
	}
	return lines
}

func cursorMark(colored bool) string {
	return paint("❯ ", ansiBold, colored)
}

// カーソル溝: 全リスト行の行頭に確保する 2 桁のマージン。カーソル行だけ「→ 」が入り、
// 他の行は空白 (行ごとのガタつきを避けるため全行で幅を揃える)。
const (
	cursorGutterMark  = "→ "
	cursorGutterBlank = "  "
	cursorGutterWidth = 2
)
