package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// actionModal は glogx 独自の「実行を伴う操作」= git push / git pull --rebase / claude update の
// 中央モーダル状態機械。確認 (y/N) → 実行中スピナー → 結果/警告ダイアログという一連の UI 状態を
// browseModel から切り出す。実行そのもの (runGitPush → awaitCI 編成 / runGitPullRebase →
// reloadAfterPull / runClaudeUpdate の結果整形) は CI・コミット状態と密結合なので browseModel に
// 残し、この型は「どのモーダルが出ているか」「キーをどう捌くか」「どう描くか」だけを持つ。
// tmux prefix 誤爆のフィードバックは右下 toast で出す (browseModel が持つ)。ここには同居させない。
type actionModal struct {
	pushConfirm  bool   // b の push 確認中 (y/N)
	pushing      bool   // git push 実行中 (終了以外のキーを無視)
	pullConfirm  bool   // u の pull --rebase 確認中 (y/N)
	pulling      bool   // git pull --rebase 実行中 (終了以外のキーを無視)
	rerunConfirm bool   // r の CI job 再実行確認中 (y/N)
	rerunJobName string // 再実行対象の job 名 (確認モーダルの文言用)
	// rerunAction は確認 y で実行する tea.Cmd。job id / repo / SHA は browseModel 側の関心事
	// なので、askRerun 時に closure として注入する (この型は CI 状態を知らない)
	rerunAction tea.Cmd
	rerunning   bool // gh run rerun 実行中 (終了以外のキーを無視)
	// updating は自己更新が走行中の CLI 名の集合 ("claude" / "codex")。
	// 🚨 bool + 対象名の単数にしないこと: claude と codex は独立した外部コマンドで並走できるのに、
	// 単数だと running() が片方の実行中に C/X を飲んでしまい直列化する (ユーザー要望 2026-08-21)。
	// 空なら update なし。同じ CLI の二重起動は beginUpdate が弾く (npm の自己更新が競合する)。
	updating map[string]bool
	// cancel は走行中の push/pull を quit から中断するための cancel (deadline 無し)。running な
	// git 子プロセスが Ctrl-C 中断時に孤児化するのを防ぐ (leak 監査 2026-07-23)。stop() で呼ぶ。
	cancel context.CancelFunc
	// forceQuitArmed は push/pull 実行中に Ctrl-C が 1 回押されたか。途中終了は不整合 (特に
	// pull --rebase の mid-rebase 状態) を招くので 1 回目はブロックし、2 回目で cancel して強制
	// 終了する (stall で永久に閉じられなくなるのを防ぐ escape。ユーザー選定 2026-07-23)。
	forceQuitArmed bool
}

// updateKeyTarget は自己更新のキー (C / X) を CLI 名へ写す。ここが「キーと CLI の対応」の
// 単一の出典 (handleKey と browseModel の両方が参照する)。
func updateKeyTarget(key string) (string, bool) {
	switch key {
	case "C":
		return "claude", true
	case "X":
		return "codex", true
	}
	return "", false
}

// startUpdateFor は target の「すでに latest か」判定を始める (C / X の入口)。
func (a *actionModal) startUpdateFor(target string) tea.Cmd {
	if target == "codex" {
		return a.startCodexUpdate()
	}
	return a.startUpdate()
}

// isUpdating は指定 CLI の自己更新が走行中か。
func (a *actionModal) isUpdating(target string) bool { return a.updating[target] }

// anyUpdating はいずれかの CLI の自己更新が走行中か。
func (a *actionModal) anyUpdating() bool { return len(a.updating) > 0 }

// beginUpdate は target を走行中に加える。既に走っていれば false を返す
// (同じ CLI の自己更新を二重に走らせると npm/ダウンロードが競合するため)。
func (a *actionModal) beginUpdate(target string) bool {
	if a.updating[target] {
		return false
	}
	if a.updating == nil {
		a.updating = make(map[string]bool, 2)
	}
	a.updating[target] = true
	return true
}

// finishUpdate は target を走行中から外す。他の CLI が走っていればモーダルは残る。
//
// 🚨 target が空のときは全て外す (fail-safe)。走行中の集合が降りないと running() が真のままで
// Ctrl-C の終了ガードが解けず、モーダルを閉じられなくなる。実運用では runUpdate が必ず target を
// 入れるので通らない経路だが、「閉じられなくなる」方向の失敗は避ける。
func (a *actionModal) finishUpdate(target string) {
	if target == "" {
		a.updating = nil
		return
	}
	delete(a.updating, target)
}

// updatingTargets は走行中の CLI 名を決定論的な順で返す (map の反復順は不定なので、
// モーダルの行順が毎フレーム入れ替わらないようソートする)。
func (a *actionModal) updatingTargets() []string {
	if len(a.updating) == 0 {
		return nil
	}
	out := make([]string, 0, len(a.updating))
	for t := range a.updating {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// active はいずれかのモーダルが表示中か。「描かれる」と「キーを消費する」は同じ条件で、
// この 1 つの述語が両方を表す (boxLines の描画条件であり、handleKey が consumed を返す条件)。
//
// 🚨 2 つに分けないこと。「最前面に描かれるモーダル」と「キーを受け取るモーダル」がずれると、
// 画面の選択肢が効かない/別の操作が走る、という形の事故になる。実際に起きた例: 再起動ダイアログが
// running() だけを見ていたため push 確認 (y/N) 中に最前面へ重なり、画面の「その他のキー: 後で」に
// 従って押した y が push を実行した。他のモーダルは「自分を出してよいか」をこれで判断する。
// 一致は TestActionModalActiveMatchesHandleKey が固定している。
//
// running() との違いは確認待ち (y/N) を含むこと。running() は「中断すると壊れる処理が動いて
// いるか」= 終了ガードとスピナーの判断で、用途が別 (混同しないこと)。
func (a *actionModal) active() bool {
	return a.pushConfirm || a.pushing || a.pullConfirm || a.pulling || a.rerunConfirm ||
		a.rerunning || a.anyUpdating()
}

// running は remote/自己更新の実行中か (spinner tick を回し、確認以外のキーを飲む)。
// rerunning も含める (実行中の誤操作防止)。ただし quit 側の Ctrl-C ブロック対象は
// push/pull のみ: rerun は fetchTimeout 付きの短い API 呼び出しで、中断しても
// 不整合 (mid-rebase のような) を残さないため即終了を許す。
func (a *actionModal) running() bool {
	return a.pushing || a.pulling || a.rerunning || a.anyUpdating()
}

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
// 🚨 ここへ来る前に browseModel が Ctrl-C/Ctrl-G の quit 判定 (running 中のブロック) を済ませて
// いる前提。判定順 (push 確認 → pull 確認 → 実行中ガード) は footgun 回避のため厳守。
// 🚨 通知だけの用件 (push 対象なし・update 結果など) をここへ戻さないこと: キー待ちのモーダルは
// 次の 1 打を食べるため、no-op の通知には重すぎる。右下トーストで出す (ユーザー要望 2026-07-25)。
func (a *actionModal) handleKey(key string) (consumed bool, action tea.Cmd) {
	// 確認の「実行」キーは y か Enter (Enter=y はユーザー要望 2026-07-21)。それ以外はキャンセル。
	// 🚨 ToLower なので大文字 `Y` も受理する。これを markNextKey の厳密判定へ揃えないのは
	//    意図的 (理由は status_view.go:discardKey の注記。issue 071 / 123)。
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
			// Add は closure 側 (発行側で Add すると、Cmd を実行しない経路が latch を
			// 立てっぱなしにする)。goroutine 起動前に quit が Wait を素通りする race は
			// 理論上あるが、quit には y の後さらに 2 打鍵 (Ctrl-C×2) が要るので実害はない。
			// 看取りは main.go の waitPullCleanup
			return true, func() tea.Msg {
				pullCleanup.Add(1)
				defer pullCleanup.Done()
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
		// 例外: update だけが走っているとき、C / X は「もう片方の CLI の更新開始」として
		// **ここで消費する** (claude と codex を並走させるため。ユーザー要望 2026-08-21)。
		//
		// 🚨 consumed=false で browseModel へ素通ししないこと。素通しは全画面 viewer の
		// キー語彙に漏れる: status viewer を開いた状態で X が「変更の破棄」確認を立て、
		// update 完了後の y で git restore が着弾するのを実測した (red team 2026-08-21)。
		// この型の doc が禁じている「描かれるモーダルとキーを受け取るモーダルのずれ」そのもの。
		//
		// 🚨 同じ CLI のキーは消費して何もしない: 判定 Cmd を走らせると、その結果
		// (「すでに latest」の早期リターン) が走行中の update を降ろしてしまう
		// (終了ガードが解けて自己更新が孤児化 / 二重起動する。同 red team が実測)。
		// push / pull / rerun 中は update を重ねないので従来どおり飲む。
		if target, ok := updateKeyTarget(key); ok && a.anyUpdating() &&
			!a.pushing && !a.pulling && !a.rerunning {
			if a.isUpdating(target) {
				return true, nil // 同じ CLI は走行中 = 何もしない
			}
			return true, a.startUpdateFor(target)
		}
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
// (quit からの中断用)。🚨 deadline は付けない — 正当な巨大 push を timeout で切らない (K2)。
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

// startUpdate は C で claude update を確認なし即実行する (ユーザー選定 2026-07-22)。
// まず「既に latest か」をバックグラウンドで判定し、latest なら updateMsg{before==after}
// (=「すでに最新版です」トースト) で早期リターン、実際に更新するときだけ updateBeginMsg を
// 返す。🚨 ここでは updating を立てない: 判定前に立てると早期リターン時にも spinner
// モーダルが一瞬光る (ユーザー指摘 2026-08-12)。モーダルは updateBeginMsg を受けた
// runUpdate が立てる。
//
// 🚨 **その帰結として、C / X を押してから最大 5 秒 (claudeVersionFetchTimeout) 画面が
//
//	まったく変化しない** — 判定が `claude --version` の起動待ちだから。これは上の不変条件を
//	選んだ副作用で、既知 (issue 123 の ux 監査が指摘し、074 の不変条件 4 を根拠に却下)。
//	埋めるなら「判定が 800ms を超えたときだけ右下トーストで『確認中...』」のように、
//	spinner モーダルを光らせない形にすること。
func (a *actionModal) startUpdate() tea.Cmd {
	return func() tea.Msg {
		if v, latest := installedIsLatest(claudeVersionCacheFile, fetchInstalledClaudeVersion); latest {
			return updateMsg{target: "claude", before: v, after: v, early: true}
		}
		return updateBeginMsg{target: "claude"}
	}
}

// startCodexUpdate は X で codex update を確認なし即実行する (startUpdate の codex 版。
// モーダル表示と結果トーストは updateTarget / updateMsg.target で claude と出し分ける)。
func (a *actionModal) startCodexUpdate() tea.Cmd {
	return func() tea.Msg {
		if v, latest := installedIsLatest(codexVersionCacheFile, fetchInstalledCodexVersion); latest {
			return updateMsg{target: "codex", before: v, after: v, early: true}
		}
		return updateBeginMsg{target: "codex"}
	}
}

// runUpdate は updateBeginMsg (早期リターン判定の通過) を受けて実際の自己更新を開始する。
// ここで初めて updating (spinner モーダル + 終了ブロック) を立てる。
func (a *actionModal) runUpdate(target string) tea.Cmd {
	if !a.beginUpdate(target) {
		return nil // 同じ CLI が既に走行中 (自己更新の競合を防ぐ)
	}
	return func() tea.Msg {
		run := runClaudeUpdate
		if target == "codex" {
			run = runCodexUpdate
		}
		before, after, note, err := run()
		return updateMsg{target: target, before: before, after: after, note: note, err: err}
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
	case a.pushing:
		rows = []string{spinner + " pushing...", "", paint(a.runningQuitHint(), ansiDim, colored)}
	case a.pulling:
		title = " git pull --rebase "
		rows = []string{spinner + " pulling...", "", paint(a.runningQuitHint(), ansiDim, colored)}
	case a.rerunning:
		title = " CI 再実行 "
		rows = []string{spinner + " 再実行を要求中..."}
	case a.anyUpdating():
		// 1 つなら従来どおり CLI 名を題字に出す。並走中は題字を CLI 共通にして、
		// どちらがまだ走っているかを行で見せる (片方が終わっても閉じない)
		targets := a.updatingTargets()
		if len(targets) == 1 {
			title = " " + targets[0] + " update "
			rows = []string{spinner + " updating..."}
		} else {
			title = " CLI update "
			for _, t := range targets {
				rows = append(rows, spinner+" "+t+" updating...")
			}
		}
		rows = append(rows, "", paint("完了まで終了できません", ansiDim, colored))
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
