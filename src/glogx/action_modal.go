package main

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// actionModal は glogx 独自の「実行を伴う操作」= git push / git pull --rebase / claude update の
// 中央モーダル状態機械。確認 (y/N) → 実行中スピナー → 結果/警告ダイアログという一連の UI 状態を
// browseModel から切り出す。実行そのもの (runGitPush → pushPoll 編成 / runGitPullRebase →
// reloadAfterPull / runClaudeUpdate の結果整形) は CI・コミット状態と密結合なので browseModel に
// 残し、この型は「どのモーダルが出ているか」「キーをどう捌くか」「どう描くか」だけを持つ。
// prefixNote (tmux prefix 誤爆トースト) は tmux の関心事なので同居させない (browseModel が描く)。
type actionModal struct {
	pushConfirm  bool   // b の push 確認中 (y/N)
	pushing      bool   // git push 実行中 (終了以外のキーを無視)
	pushWarn     string // push できない理由の警告モーダル (何かキーで閉じる)
	pullConfirm  bool   // u の pull --rebase 確認中 (y/N)
	pulling      bool   // git pull --rebase 実行中 (終了以外のキーを無視)
	rerunConfirm bool   // r の CI job 再実行確認中 (y/N)
	rerunJobName string // 再実行対象の job 名 (確認モーダルの文言用)
	// rerunAction は確認 y で実行する tea.Cmd。job id / repo / SHA は browseModel 側の関心事
	// なので、askRerun 時に closure として注入する (この型は CI 状態を知らない)
	rerunAction tea.Cmd
	rerunning   bool // gh run rerun 実行中 (終了以外のキーを無視)
	updating     bool   // claude update 実行中 (終了以外のキーを無視)
	updateResult string // claude update の結果ダイアログ本文 ("" = 非表示。何かキーで閉じる)
	// cancel は走行中の push/pull を quit から中断するための cancel (deadline 無し)。running な
	// git 子プロセスが Ctrl-C 中断時に孤児化するのを防ぐ (leak 監査 2026-07-23)。stop() で呼ぶ。
	cancel context.CancelFunc
	// forceQuitArmed は push/pull 実行中に Ctrl-C が 1 回押されたか。途中終了は不整合 (特に
	// pull --rebase の mid-rebase 状態) を招くので 1 回目はブロックし、2 回目で cancel して強制
	// 終了する (stall で永久に閉じられなくなるのを防ぐ escape。ユーザー選定 2026-07-23)。
	forceQuitArmed bool
}

// active はいずれかのモーダル/トーストが表示中か (描画とセンタリングの要否判定)。
func (a *actionModal) active() bool {
	return a.pushConfirm || a.pushing || a.pullConfirm || a.pulling || a.rerunConfirm ||
		a.rerunning || a.updating || a.pushWarn != "" || a.updateResult != ""
}

// running は remote/自己更新の実行中か (spinner tick を回し、確認以外のキーを飲む)。
// rerunning も含める (実行中の誤操作防止)。ただし quit 側の Ctrl-C ブロック対象は
// push/pull のみ: rerun は fetchTimeout 付きの短い API 呼び出しで、中断しても
// 不整合 (mid-rebase のような) を残さないため即終了を許す。
func (a *actionModal) running() bool { return a.pushing || a.pulling || a.rerunning || a.updating }

// runningQuitHint は実行中モーダルに出す終了ガードの案内。1 回目の Ctrl-C で forceQuitArmed が
// 立った後は強制終了を促す (progressive disclosure)。
func (a *actionModal) runningQuitHint() string {
	if a.forceQuitArmed {
		return "もう一度 Ctrl-C で強制終了します"
	}
	return "完了まで終了できません"
}

// handleKey は最前面の action モーダルがキーを消費したら consumed=true を返す。push/pull 確認の
// 実行キー (y/Enter) は実行する tea.Cmd を action に載せる (呼び出し側が maybeTick と束ねる)。
// ⚠️ ここへ来る前に browseModel が Ctrl-C/Ctrl-G の quit 判定 (running 中のブロック) を済ませて
// いる前提。判定順 (警告/結果ダイアログ → push 確認 → pull 確認 → 実行中ガード) は footgun 回避のため厳守。
func (a *actionModal) handleKey(key string) (consumed bool, action tea.Cmd) {
	// 警告 / 結果ダイアログは何かキーで閉じる (そのキーは消費して誤操作を防ぐ)
	if a.pushWarn != "" {
		a.pushWarn = ""
		return true, nil
	}
	if a.updateResult != "" {
		a.updateResult = ""
		return true, nil
	}
	// 確認の「実行」キーは y か Enter (Enter=y はユーザー要望 2026-07-21)。それ以外はキャンセル。
	confirmYes := strings.ToLower(key) == "y" || key == "enter"
	if a.pushConfirm {
		a.pushConfirm = false
		if confirmYes {
			a.pushing = true
			ctx, cancel := a.startCancelable()
			return true, func() tea.Msg {
				defer cancel()
				return pushMsg{err: runGitPush(ctx)}
			}
		}
		return true, nil
	}
	if a.pullConfirm {
		a.pullConfirm = false
		if confirmYes {
			a.pulling = true
			ctx, cancel := a.startCancelable()
			return true, func() tea.Msg {
				defer cancel()
				return pullMsg{err: runGitPullRebase(ctx)}
			}
		}
		return true, nil
	}
	if a.rerunConfirm {
		a.rerunConfirm = false
		action := a.rerunAction
		a.rerunAction = nil
		if confirmYes {
			a.rerunning = true
			return true, action
		}
		return true, nil
	}
	if a.running() { // 実行中は (確認以外の) キーを無視する
		return true, nil
	}
	return false, nil
}

// askRerun は r で CI job 再実行の確認へ入る。action は確認 y で実行する tea.Cmd
// (rerunMsg を返す closure。browseModel 側が組む)。
func (a *actionModal) askRerun(jobName string, action tea.Cmd) {
	a.rerunConfirm = true
	a.rerunJobName = jobName
	a.rerunAction = action
}

// startCancelable は push/pull 用の deadline 無し cancel context を張り、cancel を保持する
// (quit からの中断用)。⚠️ deadline は付けない — 正当な巨大 push を timeout で切らない (K2)。
// cancel は closure の defer と stop() の双方から呼ばれうるが CancelFunc は冪等なので安全。
func (a *actionModal) startCancelable() (context.Context, context.CancelFunc) {
	a.forceQuitArmed = false // 新しい操作は「1 回目の Ctrl-C から」でやり直す
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	return ctx, cancel
}

// stop は走行中の push/pull を中断する (quit 時に呼ぶ)。走行中でなければ no-op。
func (a *actionModal) stop() {
	if a.cancel != nil {
		a.cancel()
	}
}

// askPull は u で pull --rebase の確認へ入る。
func (a *actionModal) askPull() { a.pullConfirm = true }

// startUpdate は C で claude update を確認なし即実行する (ユーザー選定 2026-07-22)。updating を
// 立て、実行結果を updateMsg で返す tea.Cmd を返す (呼び出し側が maybeTick と束ねる)。
func (a *actionModal) startUpdate() tea.Cmd {
	a.updating = true
	return func() tea.Msg {
		before, after, err := runClaudeUpdate()
		return updateMsg{before: before, after: after, err: err}
	}
}

// boxLines は action モーダルの描画行 (中央寄せの影付き枠)。どれも非アクティブなら nil。
// unpushedCount は push 確認の文言用に呼び出し側が渡す (この型はコミット状態を知らない)。
// spinner / width / colored は browseModel の状態を受け取る (usageOverlay / diffOverlay と同様)。
func (a *actionModal) boxLines(width int, colored bool, spinner string, unpushedCount int) []string {
	if !a.active() {
		return nil
	}
	title := " git push "
	var rows []string
	switch {
	case a.pushWarn != "":
		title = " ⚠ "
		rows = []string{
			"⚠ " + a.pushWarn,
			"",
			paint("何かキーを押して閉じる", ansiDim, colored),
		}
	case a.updateResult != "":
		title = " claude update "
		rows = append(strings.Split(a.updateResult, "\n"),
			"", paint("何かキーを押して閉じる", ansiDim, colored))
	case a.pushing:
		rows = []string{spinner + " pushing...", "", paint(a.runningQuitHint(), ansiDim, colored)}
	case a.pulling:
		title = " git pull --rebase "
		rows = []string{spinner + " pulling...", "", paint(a.runningQuitHint(), ansiDim, colored)}
	case a.rerunning:
		title = " CI 再実行 "
		rows = []string{spinner + " 再実行を要求中..."}
	case a.updating:
		title = " claude update "
		rows = []string{
			spinner + " updating...",
			"",
			paint("完了まで終了できません", ansiDim, colored),
		}
	case a.pullConfirm:
		title = " git pull --rebase "
		rows = []string{
			"origin から pull --rebase します",
			"",
			paint("y/Enter: 実行   n/Esc: キャンセル", ansiDim, colored),
		}
	case a.rerunConfirm:
		title = " CI 再実行 "
		rows = []string{
			"失敗した job を再実行します:",
			a.rerunJobName,
			"",
			paint("y/Enter: 実行   n/Esc: キャンセル", ansiDim, colored),
		}
	default: // pushConfirm
		rows = []string{
			fmt.Sprintf("未 push の %d コミットを push します", unpushedCount),
			"",
			paint("y/Enter: 実行   n/Esc: キャンセル", ansiDim, colored),
		}
	}
	return centerBox(title, rows, width, colored)
}
