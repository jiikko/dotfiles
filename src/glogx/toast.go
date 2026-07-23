package main

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
)

// toastDuration は自動消滅トーストの表示時間。push/pull 完了の結果を見落とさない程度に出し、
// ナビゲーションの邪魔にならない程度で自動的に引っ込める。
const toastDuration = 3 * time.Second

// toastMsg は自動消滅タイマーの発火。seq で世代管理し、新しいトーストが上書きした後に届く
// 古いタイマーは無視する (連続 push/pull で前のトーストのタイマーが後のを消さないように)。
type toastMsg struct{ seq int }

// toast は右下に数秒だけ浮かべる結果フィードバック (push/pull 完了の成功/失敗)。glogx が tmux の
// display-popup 内で動くため tmux-toast (floating pane) は popup に隠れて出せない。ゆえに glogx
// 自身の TUI 内に描く (popup 内でも直パン実行でも確実に見える。tea.Tick で自動消滅)。
type toast struct {
	text string
	ok   bool // true=成功 (✓緑) / false=失敗 (✗赤)
	seq  int  // 世代: 消滅タイマーの有効性判定 + 再表示でのリセット
}

// show は新しいトーストを立て、toastDuration 後の消滅を予約する tea.Cmd を返す (呼び出し側で
// Batch する)。既存のトーストは上書きし、世代を進めて古いタイマーを無効化する。
func (t *toast) show(text string, ok bool) tea.Cmd {
	t.seq++
	t.text, t.ok = text, ok
	seq := t.seq
	return tea.Tick(toastDuration, func(time.Time) tea.Msg { return toastMsg{seq: seq} })
}

// dismiss はタイマー発火を処理する。世代が一致するときだけ消す (上書き後の古いタイマーは無視)。
func (t *toast) dismiss(msg toastMsg) {
	if msg.seq == t.seq {
		t.text = ""
	}
}

// visible は表示中か。
func (t *toast) visible() bool { return t.text != "" }

// boxLines は右下トーストの描画行 (内容幅にフィットした影付き小箱)。非表示なら nil。
func (t *toast) boxLines(colored bool) []string {
	if t.text == "" {
		return nil
	}
	mark, color := "✓", ansiGreen
	if !t.ok {
		mark, color = "✗", ansiRed
	}
	row := paint(mark+" "+t.text, color, colored)
	boxW := runewidth.StringWidth(stripANSI(row)) + usageBoxChrome
	return buildShadowPanelBox("", []string{row}, boxW, colored)
}
