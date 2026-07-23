package main

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

// 枠描画のプリミティブ (browseModel の状態に依存しない純関数)。状態機械 (tui.go) から
// 描画の下請けを分離する。ここに置くのは「window/box という []string を受けて []string を返す」
// レイアウト関数だけ。m.width/m.colored 等のモデル状態を読むもの (cursorLine/bgLine/panelLines
// など) は tui.go に残す。

// centerBox は狭い幅 (最大 44) の影付きモーダル行を組む。水平センタリングと背景リストへの
// 合成は描画時に overlayCenteredBox が行う (行を塗り潰さず左右の背景を残す)。action モーダルと
// prefixNote トーストで共用する。
func centerBox(title string, rows []string, width int, colored bool) []string {
	if width <= 0 {
		width = 80
	}
	return buildShadowPanelBox(title, rows, min(44, width), colored)
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
		bw = max(bw, runewidth.StringWidth(stripANSI(r)))
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
		left += strings.Repeat(" ", max(leftGap-runewidth.StringWidth(stripANSI(left)), 0))
		// 右背景: box 行の右端 (leftGap + この行の表示幅) 以降を復元して継ぐ
		rowW := runewidth.StringWidth(stripANSI(boxRow))
		right := dropToColumn(bg, leftGap+rowW)
		window[pos] = left + reset + boxRow + reset + right
	}
	return window
}

// buildPanelBox は枠線付きのパネルを組み立てる。行の実効幅は ANSI を除いて計算する。
func buildPanelBox(title string, rows []string, width int, colored bool) []string {
	return buildPanelBoxImpl(title, rows, width, colored, false)
}

// buildShadowPanelBox は buildPanelBox の右下ドロップシャドウ付き版。confirm モーダル
// (push / pull --rebase) 専用。job/diff パネルは面積が大きく影が主張しすぎたため
// 一度全面導入 → revert (4fb36a2) した経緯があり、影は小さいモーダルに限定する。
func buildShadowPanelBox(title string, rows []string, width int, colored bool) []string {
	return buildPanelBoxImpl(title, rows, width, colored, true)
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

// buildPanelBoxImpl が本体。shadow=true では右端 1 桁・下端 1 行を「落ち影」に充て、
// 板が左上光源で浮いて見える 3D 風にする (footprint は width のまま。枠自体を
// fw = width-1 に狭めて影の余白を捻出する)。
func buildPanelBoxImpl(title string, rows []string, width int, colored bool, shadow bool) []string {
	if width < 10 {
		width = 10
	}
	fw := width // 枠の幅 (shadow 時は残り 1 桁が右の影)
	if shadow {
		fw = width - 1
	}
	inner := fw - 4 // "│ " + " │"
	lines := make([]string, 0, len(rows)+3)
	// タイトルは SGR 入りの job 名や commit subject がそのまま載る。ANSI を残すと
	// 幅計算 (Truncate/StringWidth) がずれて罫線が崩れ、タイトル全体の dim 塗りも
	// 途中でリセットされるため、タイトルに限っては ANSI を落とす
	title = runewidth.Truncate(stripANSI(title), fw-2, "…")
	top := "┌" + title + strings.Repeat("─", max(fw-2-runewidth.StringWidth(title), 0)) + "┐"
	if shadow {
		// 最上段だけ影なし (影は右上角の 1 つ下から始まるのが自然な落ち影)
		lines = append(lines, paint(top, ansiDim, colored)+" ")
	} else {
		lines = append(lines, paint(top, ansiDim, colored))
	}
	for i, row := range rows {
		content := clipToWidth(row, inner)
		pad := max(inner-runewidth.StringWidth(stripANSI(content)), 0)
		shade := ""
		if shadow {
			// 右影の上端 (最初の content 行) だけ ▓ フェザーで ease-in し、以降は █ 本体。
			// 影は最上段に無い (top で 1 行分オフセット) ので、右影の「始まり」を柔らかくする。
			if i == 0 {
				shade = shadowFeather(colored)
			} else {
				shade = shadowRun(1, colored)
			}
		}
		lines = append(lines, paint("│ ", ansiDim, colored)+content+strings.Repeat(" ", pad)+paint(" │", ansiDim, colored)+shade)
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
		bottom = paint("▖"+strings.Repeat("▁", fw-2)+"▗", ansiDim, colored)
	} else {
		bottom = paint("└"+strings.Repeat("─", fw-2)+"┘", ansiDim, colored)
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
