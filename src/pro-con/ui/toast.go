package ui

// 操作の結果の通知は tuikit/toast (glogx と同じ右下のトースト。右から滑り込み、数秒止まって、また右へ引っ込む)。2026-09-25 のユーザーの依頼。
// 成功 (✓ 緑)・失敗と断り (✗ 赤)・中立の知らせ (… シアン。次の通知が来たら退く) の 3 つで積む。消すまで残す通知 (Notify / sticky) は下端の行のまま。

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"tuikit/toast"
)

// done は操作が済んだ通知 (✓)。
func (m *Model) done(s string) { m.toasts.Show(s, true) }

// fail は操作が失敗した通知 (✗)。
func (m *Model) fail(s string) { m.toasts.Show(s, false) }

// refuse は操作を断った理由の通知 (✗ 赤。fail と同じ見た目)。「できない・使えない・選ばれていない」はこちら
// (glogx の showWarning と同じ決まり)。🚨 info (シアン) で出すと、押したキーが効かなかったことが進行中の知らせに見える
// (2026-09-25 のユーザーの指摘: --view で n を押して青い通知が出た)。
func (m *Model) refuse(s string) { m.toasts.Show(s, false) }

// info は中立の知らせ (… シアン。次の通知が来たら退く): 取り消した・レーンの端・まだ無い・受け付け済み。
func (m *Model) info(s string) { m.toasts.ShowInfo(s) }

// toastTimers は toast が頼んだ「静止の後に引っ込む」合図を tea.Tick で戻す (glogx の toastTimers と同じ)。
func toastTimers(ts []toast.Timer) tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(ts))
	for _, t := range ts {
		msg := t.Msg
		cmds = append(cmds, tea.Tick(t.After, func(time.Time) tea.Msg { return msg }))
	}
	return tea.Batch(cmds...)
}

// overlayToast は region (ボードの領域) の右下に toast を重ねる。入力中にボードを暗くした後に重ねるので、toast は暗くならない。
func (m *Model) overlayToast(region []string) []string {
	box := m.toasts.BoxLines(true, len(region), m.width)
	if len(box) == 0 {
		return region
	}
	out := append([]string(nil), region...)
	base := max(len(out)-len(box), 0)
	for i, row := range box {
		pos := base + i
		if pos >= len(out) {
			break
		}
		rw := ansi.StringWidth(row)
		keep := max(m.width-rw, 0)
		left := ansi.Cut(out[pos], 0, keep)
		pad := strings.Repeat(" ", max(keep-ansi.StringWidth(left), 0))
		out[pos] = left + sgrReset + pad + row
	}
	return out
}
