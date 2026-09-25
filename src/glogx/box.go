package main

import (
	"tuikit/layout"
)

// 枠描画のプリミティブ (browseModel の状態に依存しない純関数)。状態機械 (tui.go) から
// 描画の下請けを分離する。ここに置くのは「window/box という []string を受けて []string を返す」
// レイアウト関数だけ。m.width/m.colored 等のモデル状態を読むもの (cursorLine/bgLine/panelLines
// など) は tui.go に残す。

// overlayBox / overlayCenteredBox は tuikit layout.Overlay / layout.OverlayCentered の別名
// (行ごと置き換える / 中央に浮かせて左右の背景を残す)。
func overlayBox(window, box []string, anchor, page int) []string {
	return layout.Overlay(window, box, anchor, page)
}

func overlayCenteredBox(window, box []string, width, page int, colored bool) []string {
	return layout.OverlayCentered(window, box, width, page, colored)
}

// buildShadowPanelBox は右下ドロップシャドウ付きの枠パネルを組み立てる (行の実効幅は ANSI を
// 除いて計算)。呼び出し元は小面積モーダル (confirm.Box 経由の action モーダル /
// usage / PR 状態)、大面積の diff / job パネル + job 詳細、画面最外周フレーム (wrapWindowFrame →
// buildPanelBoxImpl を直接呼ぶ)。
//
// 影の適用経緯: 大面積 popup への全面シャドウは「面積が大きく影が主張しすぎる」で一度導入 →
// revert した (4fb36a2) が、その後影の描画がフェザー付き近黒に作り直され、ユーザー要望
// (2026-07-29) で diff / job / PR パネルへ再導入し全ポップアップを統一した。影なしの
// buildPanelBox 変種はこの統一で呼び出しゼロになったため削除済み (必要になったら git 履歴から
// 復活させる)。最外周フレームは画面端の余白セルにだけ影を落としコンテンツと重ならない (issue 025)。
// 枠線は ansiDim (種別色で枠を染める通知の箱は tuikit/toast が自分で組む)。
func buildShadowPanelBox(title string, rows []string, width int, colored bool) []string {
	return buildPanelBoxImpl(title, rows, width, colored, layout.PanelStyle{Border: layout.BorderLight, Color: ansiDim})
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
	// 織り込む (全行を " "+l で作り直すと 8.3 KB/frame を捨てる。layout.PanelStyle.Indent の doc)
	box := buildPanelBoxImpl("", content, termW-2, colored,
		layout.PanelStyle{Border: layout.BorderDouble, Color: ansiFrameBorder, Indent: 1})
	out := make([]string, 0, len(box)+1)
	out = append(out, "") // 上余白 1 行 (端末地色)
	return append(out, box...)
}

// withScrollbar は buildShadowPanelBox に渡す本文行の右端に 1 桁のスクロールバー列を足す。
// boxWidth は buildShadowPanelBox に渡すのと同じ幅を受け取り、本文幅 (inner) を内部で再計算
// する (呼び出し側に枠の内訳を知らせない)。影付き枠は右影 1 桁を width から捻出して枠自体が
// 1 桁狭い (buildPanelBoxImpl の fw = width-1) ため、-1 してから inner を出す — これを忘れると
// バー列が枠の clip に食われて消える。
func withScrollbar(rows []string, boxWidth, total, offset int, colored bool) []string {
	return layout.Scrollbar(rows, layout.PanelInnerWidth(max(boxWidth, layout.PanelMinWidth)-1), total, offset, colored)
}

// buildPanelBoxImpl は tuikit layout.Panel に glogx の影の色 (テーマの近黒) を渡して板を組む。
// 呼び出し元は小面積モーダル (confirm.Box 経由の action モーダル / usage / PR 状態)、
// 大面積の diff / job パネル + job 詳細、画面最外周フレーム (wrapWindowFrame)、zoom の演出枠。
func buildPanelBoxImpl(title string, rows []string, width int, colored bool, st layout.PanelStyle) []string {
	st.Shadow = ansiShadowFg
	return layout.Panel(title, rows, width, colored, st)
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
