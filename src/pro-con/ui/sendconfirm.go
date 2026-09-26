package ui

// 送る前の確認 (issue 517)。入力欄 (新しい依頼・issue の補足・追加オーダー・btw・回答) と回答フォームは、enter で送らずにここへ来る。
// 日本語入力の確定の enter が万一アプリに届いても、それ 1 つではここで止まる。見た目は 2026-09-26 に人が見本から選んだ形 (B): 回答フォームと同じ
// オレンジの丸い枠を画面の中央に重ね、送る中身を折り返して全部出す。y / enter で送り (confirm.IsYesStrict。これも人が選んだ)、
// 他のキーでは元の画面へ戻る。**取り消しで書いた中身を消さない** (破壊的な操作の y/N は取り消すとボードへ戻り、入力を消す)。
// 仕組みは破壊的な操作の確認と同じ modeConfirm / pending で、m.send が在るかどうかで見た目と戻り先だけが変わる。

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"tuikit/layout"

	"pro-con/backend"
)

// sendConfirm は送る前の確認に出すもの。
type sendConfirm struct {
	title string   // 枠の上辺 (どこへ何を送るか)
	warn  string   // 送る前に知らせること (方針変更は PG を止める。空なら出さない)
	body  []string // 送る中身の行 (装飾なし。幅で折り返して全部出す: 日本語は語の境が無いので、詳細の板と同じく Hardwrap。空ならそう出す)
	back  mode     // 取り消したら戻る画面 (modeInput / modeForm)
}

// askSend は cmd を送る前の確認に載せる。取り消すと今の画面 (入力欄・回答フォーム) へ、書いた中身のまま戻る。
func (m *Model) askSend(cmd backend.Command, s sendConfirm) {
	s.back = m.mode
	m.pending, m.send = cmd, &s
	m.mode = modeConfirm
}

// sendHints は送る前の確認の案内。
func (m *Model) sendHints() []string {
	back := "入力に戻る"
	if m.send.back == modeForm {
		back = "フォームに戻る"
	}
	return []string{"y / enter 送る", "他のキー " + back + " (書いた中身は残る)"}
}

// overlaySend は送る前の確認の枠を領域の中央に重ねる。入り切らなければ中身の後ろを … で切る (案内の行は残す)。
func (m *Model) overlaySend(region []string) []string {
	if m.mode != modeConfirm || m.send == nil {
		return region
	}
	s := m.send
	width, inner := formWidth(m.width)
	var lines []formLine
	add := func(text string) { lines = append(lines, formLine{text: text, caret: -1}) }
	add("")
	if s.warn != "" {
		for _, l := range strings.Split(ansi.Hardwrap(s.warn, inner, true), "\n") {
			add(sgrBold + sgrYellow + l + sgrReset)
		}
		add("")
	}
	if len(s.body) == 0 || strings.TrimSpace(strings.Join(s.body, "")) == "" {
		add(fg(244) + "(空)" + sgrReset)
	} else {
		for _, b := range s.body {
			for _, l := range strings.Split(ansi.Hardwrap(b, inner, true), "\n") {
				add(fg(231) + l + sgrReset)
			}
		}
	}
	add("")
	hint := formLine{text: fg(244) + strings.Join(m.sendHints(), "   ") + sgrReset, caret: -1}
	if room := max(len(region)-2, 3); len(lines)+1 > room { // 上下の罫線
		lines = append(lines[:room-2], formLine{text: fg(244) + "…" + sgrReset, caret: -1})
	}
	box := formBox(" "+s.title+" ", append(lines, hint), width, inner)
	out := make([]string, len(region))
	copy(out, region)
	return layout.OverlayCentered(out, box, m.width, len(out), true)
}
