package ui

// tmux の外の attach の前の案内 (issue 527)。tmux の外では端末を丸ごと claude attach に渡すので、attach の間は pro-con が何も描けない。
// そこで渡す前に、戻り方 (Ctrl+Z) を中央の枠で出し、enter / y で attach へ進む (他のキーではやめる)。
// Ctrl+Z で claude attach が終わる (止まらない) ことは、隔離 tmux で本物の claude attach を動かして確かめた (rc=0 で終わる)。
// ← は Claude Code の agent の一覧へ行くだけで pro-con には戻らないので、そう書く。
// tmux の中は popup の枠の見出しに戻る操作を出すので (attachpopup.go)、この案内は出さない。

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"tuikit/confirm"
	"tuikit/layout"
)

// attachGuideOnce は案内をこの画面で初回だけ出すか (false = 毎回。2026-09-26 にユーザーが毎回を選んだ)。
const attachGuideOnce = false

// attachGuideLines は案内の枠の中身 (装飾なし)。
var attachGuideLines = []string{
	"pro-con に戻るには Ctrl+Z",
	"(← は Claude Code の一覧へ行くだけで、pro-con には戻らない)",
	"attach を抜けても PG は動き続ける",
}

// attachReturnHelp は ? の表 (流れのタブ) に出す戻り方。help/usage.md の「attach から戻る」と揃える。
var attachReturnHelp = []string{
	"tmux の中: attach は画面の上の窓で開く。窓の枠の見出しにあるキー (tmux の prefix に続けて d か Ctrl+Z) で閉じて戻る",
	"tmux の外: attach の前の案内のとおり Ctrl+Z で戻る (← は Claude Code の一覧へ行くだけ)",
	"戻れなくなったら、別の端末で pro-con attach --leave (attach の接続だけを終わらせる)。どの戻り方でも PG は動き続ける",
}

// askAttachGuide は端末を渡す前の案内を出す (enter / y で msg の attach へ進む)。
func (m *Model) askAttachGuide(msg attachReadyMsg) { m.guide = &msg }

// attachGuideHints は案内の間の案内の行。
func attachGuideHints() []string {
	return []string{"y / enter attach する", "他のキー やめる"}
}

// handleAttachGuideKey は案内の間のキー。enter / y で端末を渡し、他のキーではやめる。
func (m *Model) handleAttachGuideKey(k tea.KeyPressMsg) tea.Cmd {
	g := m.guide
	m.guide = nil
	if !confirm.IsYesStrict(k.String()) {
		m.info("attach をやめた")
		return nil
	}
	m.guideSeen = true
	return m.execAttach(*g)
}

// overlayAttachGuide は案内の枠を領域の中央に重ねる。
func (m *Model) overlayAttachGuide(region []string) []string {
	g := m.guide
	if g == nil {
		return region
	}
	width, inner := formWidth(m.width)
	lines := []formLine{{caret: -1}}
	for i, s := range attachGuideLines {
		col := fg(231)
		if i == 0 {
			col = sgrBold + sgrYellow
		}
		for _, l := range strings.Split(ansi.Hardwrap(s, inner, true), "\n") {
			lines = append(lines, formLine{text: col + l + sgrReset, caret: -1})
		}
	}
	lines = append(lines, formLine{caret: -1}, formLine{text: fg(244) + strings.Join(attachGuideHints(), "   ") + sgrReset, caret: -1})
	box := formBox(" "+g.cardID+" の PG に attach する ", lines, width, inner)
	out := make([]string, len(region))
	copy(out, region)
	return layout.OverlayCentered(out, box, m.width, len(out), true)
}
