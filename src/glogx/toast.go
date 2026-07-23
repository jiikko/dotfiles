package main

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
)

// toastHold は「にゅっと出た」あと引っ込むまでの静止時間。push/pull 完了の結果を見落とさない程度。
const toastHold = 3 * time.Second

// toastMsg は静止 (holding) が終わって退場アニメを始める合図。seq で世代管理し、新しいトーストが
// 上書きした後に届く古いタイマーは無視する (連続 push/pull で前の退場が後のを消さないように)。
type toastMsg struct{ seq int }

type toastPhase int

const (
	toastHidden   toastPhase = iota // 非表示
	toastEntering                   // 下端から せり上がり中 (revealed 0→full)
	toastHolding                    // 全表示で静止 (toastHold 後に leaving へ)
	toastLeaving                    // 下端へ 引っ込み中 (revealed full→0)
)

// toast は右下に出す結果フィードバック (push/pull 完了)。下端から数行ぶん「にゅっと」せり上がって
// 現れ、数秒静止し、また下端へ「にゅっと」引っ込んで消える (revealed = 下端から見せている box 行数
// を tick で増減させる縦スライド)。glogx は tmux の display-popup 内で動くため tmux-toast
// (floating pane) は popup に隠れて出せず、glogx 自身の TUI 内に描く。
type toast struct {
	text     string
	ok       bool // true=成功 (✓緑) / false=失敗 (✗赤)
	seq      int  // 世代: 退場タイマーの有効性判定 + 再表示リセット
	phase    toastPhase
	revealed int // 現在下端から見せている box 行数 (0=非表示相当 / full=全表示)
}

// show は新しいトーストを下端せり上がりで出し始める。呼び出し側で maybeTick を Batch して
// tick を回すこと (アニメは tickMsg で進む)。既存トーストは上書きし世代を進める。
func (t *toast) show(text string, ok bool) {
	t.seq++
	t.text, t.ok = text, ok
	t.phase = toastEntering
	t.revealed = 0
}

// animating は入場/退場アニメ中か (tick を回す必要がある + spinnerActive に含める)。holding は
// 全表示のまま静止 (tea.Tick の toastMsg 待ち) なので tick 不要。
func (t *toast) animating() bool { return t.phase == toastEntering || t.phase == toastLeaving }

// visible は表示中か (holding 含む)。
func (t *toast) visible() bool { return t.phase != toastHidden }

// advance はアニメを 1 フレーム進める。full は現在の box 全行数 (呼び出し側が fullBox で算出)。
// 入場が完了したら holding へ移り、toastHold 後に退場を始める toastMsg を予約して返す。
func (t *toast) advance(full int) (holdCmd tea.Cmd) {
	switch t.phase {
	case toastEntering:
		t.revealed++
		if t.revealed >= full {
			t.revealed = full
			t.phase = toastHolding
			seq := t.seq
			return tea.Tick(toastHold, func(time.Time) tea.Msg { return toastMsg{seq: seq} })
		}
	case toastLeaving:
		t.revealed--
		if t.revealed <= 0 {
			t.revealed = 0
			t.phase = toastHidden
			t.text = ""
		}
	}
	return nil
}

// startLeaving は holding の静止時間が明けたら (toastMsg) 退場アニメへ移す。世代一致時のみ。
func (t *toast) startLeaving(msg toastMsg) {
	if msg.seq == t.seq && t.phase == toastHolding {
		t.phase = toastLeaving
	}
}

// fullBox は内容幅にフィットした影付き小箱 (全行)。アニメの基準になる全行数の算出にも使う。
func (t *toast) fullBox(colored bool) []string {
	mark, color := "✓", ansiGreen
	if !t.ok {
		mark, color = "✗", ansiRed
	}
	row := paint(mark+" "+t.text, color, colored)
	boxW := runewidth.StringWidth(stripANSI(row)) + usageBoxChrome
	return buildShadowPanelBox("", []string{row}, boxW, colored)
}

// boxLines は現フレームで見せる box 行 (下端から revealed 行) を返す。非表示なら nil。
// 入場中は下端の border/shadow から せり上がり、holding で全行、退場中は縮んで消える。
func (t *toast) boxLines(colored bool) []string {
	if t.phase == toastHidden {
		return nil
	}
	full := t.fullBox(colored)
	n := min(max(t.revealed, 0), len(full))
	if n == 0 {
		return nil
	}
	return full[len(full)-n:] // 下端 n 行 (overlayBoxBottomRight が画面下端へ載せる)
}
