package main

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ボーダー付き 2 ペインを崩さず描ける最小端末サイズ (これ未満はメッセージ表示に degrade)。
const (
	minTermW = 24
	minTermH = 6
)

// paneLayout は log/changes 共通の「footer 1 行 + ボーダー付き左右 2 ペイン」レイアウトの
// 寸法計算。端末サイズから毎フレーム導出する値型 (状態は持たない)。
type paneLayout struct {
	width  int
	height int
}

func layoutFor(width, height int) paneLayout { return paneLayout{width: width, height: height} }

func (l paneLayout) paneRows() int    { return max(l.height-1-2, 1) }
func (l paneLayout) leftPaneW() int   { return max(l.width*42/100, 12) }
func (l paneLayout) leftInnerW() int  { return max(l.leftPaneW()-2, 4) }
func (l paneLayout) rightInnerW() int { return max(l.width-l.leftPaneW()-2, 4) }

// tooSmall は極小端末か (ボーダー 2 ペインがはみ出すため degrade する)。
func (l paneLayout) tooSmall() bool { return l.width < minTermW || l.height < minTermH }

// degradeView は極小端末用の単一行メッセージ。
func (l paneLayout) degradeView() string {
	return clip("git-popup: 端末が小さすぎます (最小 "+strconv.Itoa(minTermW)+"x"+strconv.Itoa(minTermH)+")", l.width)
}

// render は左右のコンテンツ行をボーダー付き 2 ペイン + footer に合成する。
// leftFocused でフォーカス側のボーダーを accent にする。
func (l paneLayout) render(leftLines, rightLines []string, leftFocused bool, footer string) string {
	leftBox := paneStyle(leftFocused, l.leftInnerW(), l.paneRows()).Render(strings.Join(leftLines, "\n"))
	rightBox := paneStyle(!leftFocused, l.rightInnerW(), l.paneRows()).Render(strings.Join(rightLines, "\n"))
	body := lipgloss.JoinHorizontal(lipgloss.Top, leftBox, rightBox)
	return body + "\n" + clip(footer, l.width)
}

// paneStyle は focused に応じてボーダー色を変えた固定サイズのペイン枠を返す。
// focused = current_accent (theme)・非 focused = cold_gray。どちらのペインに
// フォーカスがあるか (一覧 or 詳細) をボーダー色で示す。
func paneStyle(focused bool, w, h int) lipgloss.Style {
	role := "cold_gray"
	if focused {
		role = "current_accent"
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(strconv.Itoa(themeCterm[role]))).
		Width(w).Height(h)
}

// clampOffset は cursor が可視域 (offset..offset+rows-1) に収まるよう offset を返す。
// 両モデルの ensureCursorVisible が共用する純関数。
func clampOffset(cursor, offset, rows int) int {
	if cursor < offset {
		offset = cursor
	}
	if cursor >= offset+rows {
		offset = cursor - rows + 1
	}
	return max(offset, 0)
}
