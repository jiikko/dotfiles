package main

import (
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
	updating     bool   // claude update 実行中 (終了以外のキーを無視)
	updateResult string // claude update の結果ダイアログ本文 ("" = 非表示。何かキーで閉じる)
}

// active はいずれかのモーダル/トーストが表示中か (描画とセンタリングの要否判定)。
func (a *actionModal) active() bool {
	return a.pushConfirm || a.pushing || a.pullConfirm || a.pulling || a.updating ||
		a.pushWarn != "" || a.updateResult != ""
}

// running は remote/自己更新の実行中か (spinner tick を回し、確認以外のキーを飲む)。
func (a *actionModal) running() bool { return a.pushing || a.pulling || a.updating }

// blocksQuit は Ctrl-C/Ctrl-G による終了を握りつぶすべきか。claude update だけ自己バイナリ更新の
// 中断が CLI を壊しうるため完了まで待たせる (push/pull は中断可)。
func (a *actionModal) blocksQuit() bool { return a.updating }

// handleKey は最前面の action モーダルがキーを消費したら consumed=true を返す。push/pull 確認の
// 実行キー (y/Enter) は実行する tea.Cmd を action に載せる (呼び出し側が maybeTick と束ねる)。
// ⚠️ ここへ来る前に browseModel が Ctrl-C/Ctrl-G の quit 判定 (blocksQuit) を済ませている前提。
// 判定順 (警告/結果ダイアログ → push 確認 → pull 確認 → 実行中ガード) は footgun 回避のため厳守。
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
			return true, func() tea.Msg { return pushMsg{err: runGitPush()} }
		}
		return true, nil
	}
	if a.pullConfirm {
		a.pullConfirm = false
		if confirmYes {
			a.pulling = true
			return true, func() tea.Msg { return pullMsg{err: runGitPullRebase()} }
		}
		return true, nil
	}
	if a.running() { // 実行中は (確認以外の) キーを無視する
		return true, nil
	}
	return false, nil
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
		rows = []string{spinner + " pushing..."}
	case a.pulling:
		title = " git pull --rebase "
		rows = []string{spinner + " pulling..."}
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
	default: // pushConfirm
		rows = []string{
			fmt.Sprintf("未 push の %d コミットを push します", unpushedCount),
			"",
			paint("y/Enter: 実行   n/Esc: キャンセル", ansiDim, colored),
		}
	}
	return centerBox(title, rows, width, colored)
}
