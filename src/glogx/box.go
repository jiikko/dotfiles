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
	leftPad := padSpaces(leftGap)
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
		left += padSpaces(max(leftGap-dispWidth(left), 0))
		// 右背景: box 行の右端 (leftGap + この行の表示幅) 以降を復元して継ぐ
		rowW := dispWidth(boxRow)
		right := dropToColumn(bg, leftGap+rowW)
		window[pos] = left + reset + boxRow + reset + right
	}
	return window
}

// panelBoxStyle は枠の見た目 (罫線の字形・枠線の色) をまとめる (issue 028 P1 で畳んだ引数束)。
// glyphs は字形、color は SGR で役割が別物。
type panelBoxStyle struct {
	glyphs boxBorder // 罫線の字形 (borderLight / borderDouble)
	color  string    // 枠線の SGR 色 (通常 ansiDim。toast だけ種別色)
	// indent は全行の頭に付ける空白の桁数 (0 = 付けない)。
	//
	// ⚠️ 見た目の都合ではなく確保を削るために持っている。呼び出し側で
	// `" " + line` を全行に掛けると**可視行ぶんの文字列を丸ごともう 1 部作る**ことになり、
	// 実測で 8.3 KB/frame (1 フレーム 40 KB のうち約 20%) を捨てていた。ここで組み立てに
	// 織り込めば、既にある連結の一部になるので追加の確保が要らない。
	indent int
}

// buildShadowPanelBox は右下ドロップシャドウ付きの枠パネルを組み立てる (行の実効幅は ANSI を
// 除いて計算)。呼び出し元は小面積モーダル/トースト (centerBox 経由の action モーダル + toast /
// usage / PR 状態)、大面積の diff / job パネル + job 詳細、画面最外周フレーム (wrapWindowFrame →
// buildPanelBoxImpl を直接呼ぶ)。
//
// 影の適用経緯: 大面積 popup への全面シャドウは「面積が大きく影が主張しすぎる」で一度導入 →
// revert した (4fb36a2) が、その後影の描画がフェザー付き近黒に作り直され、ユーザー要望
// (2026-07-29) で diff / job / PR パネルへ再導入し全ポップアップを統一した。影なしの
// buildPanelBox 変種はこの統一で呼び出しゼロになったため削除済み (必要になったら git 履歴から
// 復活させる)。最外周フレームは画面端の余白セルにだけ影を落としコンテンツと重ならない (issue 025)。
// border は枠線 (上辺・側辺・下辺) の SGR 色。ドロップシャドウのブロックは中立のまま (dim)。
// 通常は ansiDim を渡す。toast だけが種別色 (緑/赤/シアン) を渡して枠ごと色付けする。
func buildShadowPanelBox(title string, rows []string, width int, colored bool, border string) []string {
	return buildPanelBoxImpl(title, rows, width, colored, panelBoxStyle{glyphs: borderLight, color: border})
}

// wrapWindowFrame は画面全体のコンテンツ (リスト + overlay 群を合成済みの window) を、最外周に
// 余白を残した枠 + 右下ドロップシャドウで包み「板がターミナル地色の上に浮いている」見た目にする
// (issue 025)。影の幾何は buildPanelBoxImpl(shadow=true) へ完全委譲する (影の実装は 1 箇所に保つ)。
// 返す行数 = len(content) + 4 (上余白 + 上辺 + 下辺 + 下影)。左右余白 1 桁ずつ + 影 1 桁で、
// footprint は termW に収まる (呼び出し側の contentWidth()/frameVOverhead と一致)。
// 罫線は ansiFrameBorder (scratch と同じマゼンタ) で染める。落ち影は中立 dim のまま
// (buildShadowPanelBox の方針と同じ。トーストが枠だけ種別色にして影を据え置いたのと同型)。
func wrapWindowFrame(content []string, termW int, colored bool) []string {
	// -2 = 左右余白 1 桁ずつ。二重罫線 (ユーザー要望)。左余白 1 桁は indent で組み立てに
	// 織り込む (全行を " "+l で作り直すと 8.3 KB/frame を捨てる。panelBoxStyle.indent の doc)
	box := buildPanelBoxImpl("", content, termW-2, colored,
		panelBoxStyle{glyphs: borderDouble, color: ansiFrameBorder, indent: 1})
	out := make([]string, 0, len(box)+1)
	out = append(out, "") // 上余白 1 行 (端末地色)
	return append(out, box...)
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

// withScrollbar は buildShadowPanelBox に渡す本文行の右端に 1 桁のスクロールバー列を足す。
// boxWidth は buildShadowPanelBox に渡すのと同じ幅を受け取り、本文幅 (inner) を内部で再計算
// する (呼び出し側に枠の内訳を知らせない)。影付き枠は右影 1 桁を width から捻出して枠自体が
// 1 桁狭い (buildPanelBoxImpl の fw = width-1) ため、-1 してから inner を出す — これを忘れると
// バー列が枠の clip に食われて消える。
func withScrollbar(rows []string, boxWidth, total, offset int, colored bool) []string {
	return scrollbarColumn(rows, panelInnerWidth(max(boxWidth, minPanelWidth)-1), total, offset, colored)
}

// scrollbarColumnWidth はバー列がこの関数のクリップで消費する桁数 (バー 1 桁 + 手前の空き 1 桁)。
//
// ⚠️ 内訳を変えるならここ 1 箇所。事前に幅を差し引いてから行を組む呼び出し側 (issues viewer の
// listLines / bodyLines) もこの定数を引く — 別々の数で持つと等号でずれ、小さければ全幅の行だけ
// 末尾 1 文字が "…" に化け、大きければ 1 桁ぶん本文が痩せる。どちらも「幅を超えない」ので
// テストの上限アサートを素通りする。
const scrollbarColumnWidth = 2

// scrollbarColumn は行列の右端に 1 桁のスクロールバー列を足す本体。innerWidth は行が使える
// 表示幅そのもの (枠の内訳を差し引いた後の幅)。枠付きパネルは withScrollbar 経由で、
// メインのリストビュー (viewLines) は contentWidth を直接渡してここを使う。
// total は全行数、offset は先頭行が全体の何行目か。全体が収まる (total <= len(rows)) ときは
// 行をそのまま返す (列を作らないので本文幅が戻る)。行はバー列 + 手前の空き 1 桁を除いた幅へ
// クリップする。
func scrollbarColumn(rows []string, innerWidth, total, offset int, colored bool) []string {
	view := len(rows)
	if view == 0 || total <= view {
		return rows
	}
	contentW := innerWidth - scrollbarColumnWidth
	if contentW < 1 {
		// バー列 (空白 + 記号) すら入らない極小幅ではバーを描かない。以前は contentW を 1 に
		// 床上げしており、幅 1-2 で「本文 + バー」が必ず枠を破っていた (issue 053)
		out := make([]string, len(rows))
		for i, r := range rows {
			out[i] = clipToWidth(r, innerWidth)
		}
		return out
	}
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
	// track の paint は行不変なのでループ外で 1 回だけ組む (毎フレーム全行で走る)
	track := paint(scrollbarTrackGlyph, ansiDim, colored)
	out := make([]string, 0, view)
	for i, row := range rows {
		glyph := track
		if i >= start && i < start+thumb {
			glyph = scrollbarThumbGlyph
		}
		content, cw := clipMeasure(row, contentW)
		out = append(out, content+reset+padSpaces(contentW-cw)+" "+glyph)
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

// boxBorder は枠の罫線文字 (上角 + 横 + 縦)。小面積モーダル/パネルは borderLight (細い単線)、
// 最外周フレーム (wrapWindowFrame) は borderDouble (二重線) を使い分ける (issue 025・ユーザー要望
// 2026-07-24: フレームの罫線を太く/二重に)。下辺は影に接地させる低ブロック ▖▁▗ 固定で glyphs
// 非依存のため、下角の字形は持たない。全て表示幅 1 の box-drawing 文字 (EastAsianWidth=false
// 前提) なので幅計算は border 種別に依らず不変。
type boxBorder struct{ tl, tr, h, v string }

var (
	borderLight  = boxBorder{tl: "┌", tr: "┐", h: "─", v: "│"}
	borderDouble = boxBorder{tl: "╔", tr: "╗", h: "═", v: "║"}
)

// buildPanelBoxImpl が本体。右端 1 桁 (右影) と、その下に shadowBottomOffset 桁右へずらした
// 下端影の行を足し、板が左上光源で浮いて見える 3D 風にする。footprint は width のまま
// (枠自体を fw = width-1 に狭めて右影 1 桁分を捻出。下端影は行内の左詰めオフセットで表現)。
// st.glyphs は罫線種別 (上辺・側辺に効く。下辺は接地ブロック ▖▁▗ 固定で glyphs 非依存)。
func buildPanelBoxImpl(title string, rows []string, width int, colored bool, st panelBoxStyle) []string {
	b, border := st.glyphs, st.color
	if width < minPanelWidth {
		width = minPanelWidth
	}
	fw := width - 1 // 枠の幅 (残り 1 桁が右の影)
	inner := panelInnerWidth(fw)
	lines := make([]string, 0, len(rows)+3)
	// タイトルは SGR 入りの job 名や commit subject がそのまま載る。ANSI を残すと
	// 幅計算 (Truncate/StringWidth) がずれて罫線が崩れ、タイトル全体の dim 塗りも
	// 途中でリセットされるため、タイトルに限っては ANSI を落とす
	title = truncateDisp(stripANSI(title), fw-2, "…")
	top := b.tl + title + strings.Repeat(b.h, max(fw-2-dispWidth(title), 0)) + b.tr
	// 行頭の余白は各行の連結に織り込む (panelBoxStyle.indent の doc)
	pre := padSpaces(st.indent)
	// 最上段だけ影なし (影は右上角の 1 つ下から始まるのが自然な落ち影)
	lines = append(lines, pre+paint(top, border, colored)+" ")
	// 行ごとに変わらない断片はループの外で 1 度だけ組む。最外周フレーム (wrapWindowFrame) は
	// 毎フレーム全可視行をここへ通すので、行数 × フレームレートぶんの alloc になっていた。
	// 右影の上端 (最初の content 行) だけ ▓ フェザーで ease-in し、以降は █ 本体にする
	// (影は最上段に無い = top で 1 行分オフセットされるので、右影の「始まり」を柔らかくする)。
	leftEdge, rightEdge := paint(b.v+" ", border, colored), paint(" "+b.v, border, colored)
	shadeFirst, shadeRest := shadowFeather(colored), shadowRun(1, colored)
	for i, row := range rows {
		// ⚠️ 色なしモードでは外部由来の SGR を落とす。paint も scrollbarColumn も
		// colored=false のとき reset を一切出さないため、CI ログ 1 行に閉じていない SGR が
		// あると padding・枠・スクロールバー列・後続行まで属性が続く (NO_COLOR 起動時に
		// 「以降が全部消える」形の画面破壊になる)。色を出さないモードなのだから、
		// 自分で塗らない = 外から来た色も通さない、で不変条件を揃える。
		if !colored {
			row = stripANSI(row)
		}
		content, cw := clipMeasure(row, inner)
		pad := max(inner-cw, 0)
		shade := shadeRest
		if i == 0 {
			shade = shadeFirst
		}
		lines = append(lines, pre+leftEdge+content+padSpaces(pad)+rightEdge+shade)
	}
	// 下辺は最下段に寄せた低い横線 ▁ + 左右の角も最下段の低ブロック ▖ ▗ 。─ 中央高だと
	// 下の落ち影との間に半セルの余白ができ、└┘ の角だけ中央高だと横線 ▁ との間に段差が出る。
	// 角・横線ともセル最下段で高さを揃え、影に接した段差のない自然なドロップシャドウにする
	// (ユーザー指摘 2026-07-23: ─ は余白 / ▁ 一様は角が開く / └┘ 角は段差 → 低ブロックの
	// 角 ▖ ▗ で接地と角閉じを両立。▖ + ▁×n + ▗)。
	// ▖▁▗ は「箱の下辺の枠線」(落ち影に接地させるため低ブロックで描くだけ) なので border 色で
	// 染める。落ち影本体 (この行末の shadowRun や次のオフセット行) は中立 dim のまま描く。
	bottom := paint("▖"+strings.Repeat("▁", fw-2)+"▗", border, colored)
	// 右下角の影は最も深いので █ 本体。
	lines = append(lines, pre+bottom+shadowRun(1, colored))
	// 下端の影: 左端を shadowBottomOffset 桁だけ右へずらして右下方向に落とす (古典的な
	// ドロップシャドウ)。左端を ▓ フェザーで ease-in してから █ 本体を敷く。右端は箱の右影列と
	// 揃える (影全体の幅 = shadowBottomOffset + フェザー1 + 本体 = width)。
	lines = append(lines, pre+padSpaces(shadowBottomOffset)+shadowFeather(colored)+shadowRun(width-1-shadowBottomOffset, colored))
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
