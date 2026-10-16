package lanmgr

import (
	"errors"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
	"mac-lan-manager/setup"
)

// viewSetup は初期設定の画面 (2 枚のカード: 手順 (進捗バー付き) と、この手順で実行するもの)。issue 747 の「charm 版」節の配置。
func (m *Model) viewSetup(w int) string {
	su := m.su
	inner := max(w-7, 10)
	steps := m.setupSteps()
	done := 0
	for _, s := range steps {
		if s.Done {
			done++
		}
	}
	next, hasNext := nextStep(steps)

	var b strings.Builder
	b.WriteString(sAccent.Render("初期設定  ") + sText.Bold(true).Render(su.label) + sDim.Render("  "+su.dest.User+"@"+su.dest.IP) + "\n")
	b.WriteString(progressBar(done, len(steps), min(30, max(inner-14, 4))) + sDim.Render(fmt.Sprintf("  %d / %d 済", done, len(steps))) + "\n")
	switch {
	case su.ch != nil:
		b.WriteString(sDim.Render("相手を読んでいる…") + "\n")
	case m.unconfOverdue():
		// 期限の後の観測が読めずに印が残ると tick が止まる (読み直しは r だけ)。断りの文言を読んだ人にしか届かなかった (敵対レビュー)
		b.WriteString(sWarn.Render("固定 IP にした後の確認が済んでいない: 相手が元に戻った頃なので r で読み直す") + "\n")
	case errors.Is(su.obs.KeyErr, setup.ErrUnknown):
		b.WriteString(sWarn.Render("相手を読めない (届かない・判定できない): "+su.obs.KeyErr.Error()) + "\n")
	case su.seen && !su.obs.KeyOK:
		b.WriteString(sDim.Render("鍵ではまだ入れない (公開鍵を置く手順で入れるようになる)") + "\n")
	default:
		b.WriteString("\n")
	}
	b.WriteString("\n")
	for i, s := range steps {
		mark, title := sMuted.Render("·"), sDim.Render(s.Title)
		switch {
		case s.Done && s.ID != stepConfirm:
			mark, title = sOK.Render("✓"), sText.Render(s.Title)
		case hasNext && s.ID == next.ID:
			mark, title = sAccent.Render("▶"), sText.Bold(true).Render(s.Title)
		}
		b.WriteString(fmt.Sprintf("%s %d  %s\n", mark, i+1, title))
		if hasNext && s.ID == next.ID && s.Blocked != "" {
			b.WriteString(sWarn.Render("     止まっている: "+s.Blocked) + "\n")
		}
	}
	stepsCard := shadow(card(strings.TrimRight(b.String(), "\n"), w-1, surfPanel, cAccent, cYellow, cOK))

	var c strings.Builder
	c.WriteString(sAccent.Render("この手順で実行するもの") + "\n" + gradRule(inner, cTeal, cPurple, lipgloss.Color(surfDetail)) + "\n\n")
	switch {
	case !hasNext:
		c.WriteString(sOK.Render("全部済んでいる") + "\n")
	case next.Blocked != "":
		c.WriteString(sDim.Render("先に上の理由を片付ける (r で読み直す)") + "\n")
	default:
		if a, why := m.setupAction(next); a != nil {
			c.WriteString(sText.Bold(true).Render(a.title) + "\n")
			for _, l := range a.body {
				if strings.HasPrefix(l, "$ ") {
					c.WriteString(sCode.Render(termwidth.Truncate(l, max(inner-2, 4), "…")) + "\n")
				} else {
					c.WriteString(sDim.Render(termwidth.Truncate(l, inner, "…")) + "\n")
				}
			}
			c.WriteString("\n" + keycap("x", "実行 (確認の後)"))
		} else {
			c.WriteString(sWarn.Render(why) + "\n")
		}
	}
	codeCard := shadow(card(strings.TrimRight(c.String(), "\n"), w-1, surfDetail, cTeal, cPurple))
	// 見出し 3 行 + 余白 1 行の下に置く。2 枚が入らなければ「実行するもの」を省く (同じ中身は x の確認の板に出る)。
	// 手順のカードも入らなければ抜け方だけの画面にする
	out := lipgloss.JoinVertical(lipgloss.Left, stepsCard, "", codeCard)
	if lipgloss.Height(out)+4 > m.bodyHeight() {
		out = stepsCard
	}
	m.tooShort = lipgloss.Height(out)+4 > m.bodyHeight()
	return out
}

// progressBar は済んだ手順の割合のバー (オレンジ → 緑のグラデーション)。
func progressBar(done, total, width int) string {
	if total <= 0 {
		return ""
	}
	fill := done * width / total
	var b strings.Builder
	for i, col := range lipgloss.Blend1D(max(width, 1), cAccent, cYellow, cOK) {
		if i < fill {
			b.WriteString(lipgloss.NewStyle().Foreground(col).Render("█"))
		} else {
			b.WriteString(sMuted.Render("░"))
		}
	}
	return b.String()
}
