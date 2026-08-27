// 予約できたことを知らせるトースト。glogx (src/glogx/toast.go) と同じ振る舞い —
// 右下に「にゅっと」滑り込み、少し静止して消える — を、この UI の中に持たせたもの。
//
// glogx から持ってきたのは**動き方の設計**だけで、コードは共有していない:
//   - glogx の箱は罫線 (U+2500 系) と ✓ を使う。どちらも East Asian Width が Ambiguous で、
//     CJK フォントだと 2 セルに描かれる端末がある。この UI は「幅がずれると本物のカーソル
//     (= IME の未確定文字の位置) がずれる」ため、装飾は色と反転だけに絞っている
//   - 共有パッケージ (src/toast) への切り出しは、2 つ目の利用者が出たときに行う。今 glogx を
//     触ると並行作業とぶつかるうえ、利用者 1 つでの共通化は複雑性を移すだけになる
//
// 表示のあとで終了するので、「予約した」と出てから popup が閉じる。実際に job を作るのは
// 呼び出し元のシェルで、失敗したときはシェル側が別途知らせる (scripts/tmux_schedule_keys.sh)。
package main

import (
	"math"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// toastFrames は滑り込みに使うフレーム数、toastTick は 1 フレームの間隔。
// toastHold は出切ってから閉じるまでの静止時間 (テストで待たずに済むよう var)。
const (
	toastFrames = 10
	toastTick   = 25 * time.Millisecond
)

var toastHold = 700 * time.Millisecond

type toastTickMsg struct{}
type toastDoneMsg struct{}

// toast は右下に出す 1 枚。frame が 0..toastFrames まで進むと全幅表示になる。
type toast struct {
	text   string
	frame  int
	shown  bool
	done   bool // 静止も終わった (呼び出し側が終了してよい)
	holdCh bool // hold のタイマーを張ったか
}

func (t *toast) start(text string) tea.Cmd {
	t.text, t.frame, t.shown, t.done, t.holdCh = text, 0, true, false, false
	return tea.Tick(toastTick, func(time.Time) tea.Msg { return toastTickMsg{} })
}

// advance は 1 フレーム進める。全幅に達したら静止のタイマーを返す。
func (t *toast) advance() tea.Cmd {
	if !t.shown || t.done {
		return nil
	}
	if t.frame < toastFrames {
		t.frame++
		if t.frame < toastFrames {
			return tea.Tick(toastTick, func(time.Time) tea.Msg { return toastTickMsg{} })
		}
	}
	if !t.holdCh {
		t.holdCh = true
		return tea.Tick(toastHold, func(time.Time) tea.Msg { return toastDoneMsg{} })
	}
	return nil
}

// width は今のフレームで見せる表示幅 (easeOutCubic。終わりで減速するので「すっと収まる」)。
func (t *toast) width(full int) int {
	if t.frame >= toastFrames {
		return full
	}
	if t.frame <= 0 {
		return 0
	}
	p := float64(t.frame) / float64(toastFrames)
	q := 1 - p
	return int(math.Round((1 - q*q*q) * float64(full)))
}

// overlay は画面の右下にトーストを重ねる。画面 (height 行) の最下行に置くため、足りない行は
// 空行で埋める。⚠️ 中身の最終行を上書きしない (ヘルプ行が消える) / height を超えない
// (超えると端末が流れてカーソル位置がずれる)。
func (t *toast) overlay(lines []string, width, height int) []string {
	if !t.shown {
		return lines
	}
	body := "  " + t.text + "  "
	if w := ansi.StringWidth(body); w > width {
		body = "  " + truncate(t.text, width-4) + "  "
	}
	full := ansi.StringWidth(body)
	shown := t.width(full)
	if shown <= 0 {
		return lines
	}
	// 箱の左 shown カラムだけを見せ、右端に寄せる = 右から滑り込んで見える
	visible := truncate(body, shown)
	pad := width - ansi.StringWidth(visible)
	if pad < 0 {
		pad = 0
	}
	row := strings.Repeat(" ", pad) + sgr(revOK+";"+bold, visible)
	out := make([]string, len(lines))
	copy(out, lines)
	if height > len(out) {
		for len(out) < height {
			out = append(out, "")
		}
	} else if height > 0 {
		out = out[:height]
	}
	if len(out) == 0 {
		return []string{row}
	}
	out[len(out)-1] = row
	return out
}
