package main

import (
	"github.com/jiikko/dotfiles/src/tuikit/confirm"

	"glogx/internal/app"

	tea "charm.land/bubbletea/v2"
)

// updateBeginMsg は「早期リターン判定 (installedIsLatest) を通過した = 実際に自己更新を
// 走らせる」合図。C/X 押下 → startUpdate (判定。モーダルなし) → updateBeginMsg →
// actModal.runUpdate (spinner モーダルあり) の 2 段構え。
type updateBeginMsg struct{ target string }

// updateMsg は `claude update` の実行結果 (C キー、確認なし即実行)。glogx の独自機能。
// before/after は update 前後の CLI バージョン (取得失敗時は空)。両方取れて差があれば
// "vX → vY"、同じなら「変更なし」を notice に出す。
type updateMsg struct {
	target string // "claude" / "codex" (結果トーストの出し分け用)
	before string
	after  string
	// note は update 成功時の CLI 出力末尾行 (early / 失敗時は空)。before == after のとき
	// 「なぜ変わらなかったか」を語る唯一の材料 (runCLIUpdate の note を参照)。
	note string
	err  error // 失敗時のみ。Error() は CLI 出力の末尾行を含む
	// early は「すでに latest」の判定による早期リターン (runUpdate の実行結果ではない)。
	// 実行結果と区別できないと、C 連打で並走した判定の結果が走行中の update を降ろす。
	early bool
}

// updateNoteSuffix は CLI 出力の末尾行をトーストへ添える接尾辞にする ("" ならなし)。
// before == after のときだけ使う: 「更新しなかった理由」は CLI 自身の言葉が一次情報で、
// glogx が要約すると (「最新です」がまさにそうだった) 誤訳になる。
func updateNoteSuffix(note string) string {
	if note == "" {
		return ""
	}
	return ": " + note
}

// releaseStartupChecks は、起動時の CI 取得が終わっていれば預けた検査を 1 回だけ放す。
// 取得が終わる経路は ciResultMsg の最後のチャンクだけでなく、pull の後に取得を始めない分岐
// (pendingFetches を直接 0 にする) もあるので、出来事ではなく状態 (fetching) で見る。
func (m *browseModel) releaseStartupChecks() tea.Cmd {
	if m.deferredStartup == nil || m.fetching() || m.done {
		return nil
	}
	cmd := m.deferredStartup
	m.deferredStartup = nil
	return cmd
}

// showClaudeUpdate は「新バージョンあり」の通知を出す。
//
// 先行トーストを潰さないための専用タイマーでの遅延再送は要らない。
// トーストが積めるので調停は不要 (toast の doc 参照)。
func (m *browseModel) showClaudeUpdate(latest string) tea.Cmd {
	m.toast.Show("Claude Code v"+latest+" が公開されています (C で更新)", true)
	return m.maybeTick()
}

func (m *browseModel) onAutobuild(msg autobuildMsg) (tea.Model, tea.Cmd) {
	// 裏のビルドが決着したらトーストで知らせる。
	//
	// 🚨 notify と keep は同時に立つ (handle は「開始を伝えた後も失敗を拾うため監視を続ける」を
	// この組み合わせで表す)。通知したときに tickCmd を張り直さないと監視チェーンが切れ、
	// その後のビルド失敗が二度と通知されない (監視は失敗を検出する唯一の経路)。
	res, notify, keep := m.autobuild.handle(msg.result, app.TimeNow())
	var watch tea.Cmd
	if keep {
		watch = m.autobuild.tickCmd()
	}
	if notify && res == autobuildInstalled {
		// 完成はその場で再起動できる合図なので、消えるトーストでなくダイアログで出す。
		// 🚨 ここでは「出したい」を立てるだけ。実際に出るのは中断できない処理 (claude update /
		// push / pull) が走っていないときで、判断は restartPromptVisible() が持つ
		m.restartPending = true
		return m, tea.Batch(m.maybeTick(), watch)
	}
	if notify {
		text, ok := autobuildToast(res)
		m.toast.Show(text, ok)
		return m, tea.Batch(m.maybeTick(), watch) // トーストのスライドは tick で進む
	}
	return m, watch
}

func (m *browseModel) onAutobuildSpawn(msg autobuildSpawnMsg) (tea.Model, tea.Cmd) {
	// pull 後の裏ビルドが始まった。🚨 起動時とまったく同じ形で監視を張る (newAutobuildWatch →
	// handle で「ビルド中」を即出す → tickCmd)。完成すれば通常の autobuildMsg 経路が
	// 再起動ダイアログを出すので、ここで完成側の面倒を見る必要はない。
	if !msg.spawned || m.autobuild.active {
		return m, nil
	}
	m.autobuild = newAutobuildWatch(selfExePath(), true, app.TimeNow())
	if res, notify, _ := m.autobuild.handle(autobuildRunning, app.TimeNow()); notify {
		text, ok := autobuildToast(res)
		m.toast.Show(text, ok)
	}
	return m, tea.Batch(m.autobuild.tickCmd(), m.maybeTick())
}

func (m *browseModel) onUpdateBegin(msg updateBeginMsg) (tea.Model, tea.Cmd) {
	// C / X 連打で「同じ CLI」の update が走行中なら二重実行しない (自己更新が競合する)。
	// 🚨 別の CLI は弾かない: claude と codex は独立に走らせる (ユーザー要望 2026-08-21)。
	// ここを anyUpdating() に戻すと片方の実行中にもう片方が始められず直列に戻る。
	if m.actModal.isUpdating(msg.target) {
		return m, m.maybeTick()
	}
	// 🚨 判定 Cmd の走行中 (実測 40-80ms) は update モーダルが出ていないため、その窓で
	// b / u / r を押すと確認モーダルが立つ。そこへ update を重ねると、描かれるのは update
	// (boxLines の switch 順) なのにキーを受け取るのは確認 (handleKey の判定順) になり、
	// 「完了まで終了できません」の画面で Enter が git push を起動する (audit 2026-08-20 で
	// runGitPush 1 回を実測)。確認・git 実行中は update を譲り、理由をトーストで伝える。
	// 🚨 ここに anyUpdating() を足さないこと: 別 CLI の update とは並走させる。
	// 本来の直し方は「描画とキー判定を同一の状態値から導出する」(未対応)。
	if m.actModal.pushConfirm || m.actModal.pullConfirm || m.actModal.rerunConfirm ||
		m.actModal.pushing || m.actModal.pulling || m.actModal.rerunning {
		m.toast.Show(msg.target+" update は確認/実行が終わってから実行してください", false) // 断り (✗。ガイド §9)
		return m, m.maybeTick()
	}
	return m, tea.Batch(m.actModal.runUpdate(msg.target), m.maybeTick())
}

func (m *browseModel) onUpdate(msg updateMsg) (tea.Model, tea.Cmd) {
	// 🚨 早期リターン (「すでに latest」の判定結果) で走行中の update を降ろさないこと。
	// C の判定 Cmd が並走したとき、その結果が走っている自己更新の追跡を消し、モーダルが
	// 閉じて終了ガードが解ける (自己更新が孤児化 / 二重起動する。2026-08-21 に
	// npm 2 本同時と Ctrl-C 脱出を実測)。走行中なら判定結果は捨てる。
	if msg.early && m.actModal.isUpdating(msg.target) {
		return m, m.maybeTick()
	}
	// 該当 CLI だけ走行中から外す。もう片方が走っていればモーダルは残る
	// (両方終わるまで「完了まで終了できません」を維持する)
	m.actModal.finishUpdate(msg.target)
	// 結果は右下トーストで出す (旧: 何かキーで閉じるダイアログ。ユーザー要望 2026-07-25)。
	// バージョンが上がったのか latest だったのかは 1 行に畳んで一目で分かる形にする。
	// 「新バージョンあり」の通知 (showClaudeUpdate) と違って調停は挟まない:
	// こちらは C / X を押した本人への結果なので、先行トーストを上書きする後勝ちが正しい。
	// どちらの CLI の結果かが一目で分かるよう、主語として CLI 名を常に前置する
	// (ユーザー要望 2026-08-12。旧: codex のみ前置)。
	name := msg.target + " "
	switch {
	case msg.err != nil:
		m.showWarning(name + "更新に失敗: " + app.FirstLine(msg.err.Error()))
	case msg.before != "" && msg.after != "" && msg.before != msg.after:
		m.toast.Show(name+"v"+msg.before+" → v"+msg.after+" に更新しました", true)
	case msg.before != "" && msg.before == msg.after && msg.early:
		// glogx 自身が「installed >= キャッシュ済み latest」を証明した早期リターン。
		// ここだけが「最新です」と言ってよい (update は走っていない)。
		m.toast.Show(name+"すでに最新版です (v"+msg.before+")", true)
	case msg.before != "" && msg.before == msg.after:
		// update を走らせたのにバージョンが動かなかった。🚨 これを「最新です」と言わないこと:
		// CLI は自前の stale なキャッシュを見て「更新不要」と判断しても exit 0 で成功する
		// (実測 2026-09-03: 起動時トーストが codex の新版を告げた直後に X を押しても
		// ~/.codex/version.json が 44 日前で止まっていて "already up to date" が返り、
		// glogx がそれを「すでに最新版です」と翻訳していた)。glogx は registry 直取りの
		// latest を握っているので、それより古いままなら警告として出す。
		if latest := cachedLatestVersion(versionCacheFileFor(msg.target)); versionLess(msg.before, latest) {
			m.showWarning(name + "update を実行しましたが v" + msg.before + " のままです (公開は v" + latest + ")" + updateNoteSuffix(msg.note))
			return m, m.maybeTick()
		}
		m.toast.Show(name+"update を実行しましたが変化なし (v"+msg.before+")"+updateNoteSuffix(msg.note), true)
	case msg.after != "":
		m.toast.Show(name+"現在のバージョン: v"+msg.after, true) // before 不明で比較できず
	default:
		m.toast.Show(name+"update を実行しました", true) // 前後とも取得できず
	}
	return m, m.maybeTick()
}

// cancelAll は走行中の全非同期リソース (CI fetch の ctx / usage fetch / push・pull の git /
// issues と git log の fsnotify watcher) を止める後始末の単一ファネル。冪等。quit() (キー操作の終了) と
// main.go の defer の両方が呼ぶ: bubbletea v2 は SIGINT/SIGTERM を InterruptMsg/QuitMsg に
// 変換するが、この 2 つだけは model.Update を経由せず eventLoop が直接 return するため
// (tea.go の handleSignals/eventLoop)、シグナル終了では quit() が走らない。defer 側が無いと
// push/pull 中の SIGTERM (tmux kill-window 等) で deadline なしの git 子プロセスが孤児化する。
func (m *browseModel) cancelAll() {
	m.cancel()
	m.usageOv.Stop()  // 走行中の usage fetch subprocess を中断 (オーファン化防止)
	m.actModal.stop() // 走行中の push/pull git subprocess を中断 (stall 中の孤児化防止)
	// issues viewer の fsnotify watcher を閉じる。通常終了ではプロセス終了が fd を回収するが、
	// 再起動 (restartSelf の syscall.Exec) は fd テーブルを引き継ぐため明示的に閉じないと漏れる:
	// fsnotify (go.mod の pin は v1.10.1) の darwin backend は監視対象 fd に O_CLOEXEC を付ける一方、kqueue fd 本体
	// (backend_kqueue.go newKqueue) には CloseOnExec を呼ばず、viewer を開いたまま r で再起動する
	// たびに kqueue fd が新プロセスへ 1 本ずつ継承され続ける。
	m.issuesOv.StopWatch()
	m.stopGitLogWatch() // git log の見張りも同じ理由で閉じる (gitlog_watch.go)
	m.doctorOv.Stop()   // doctor の走査 goroutine / 子プロセス (brew / simctl) を止める (popup の開閉で残さない)
	m.lanV.Stop()       // mac-lan-manager の Bonjour の走査 (終わらない dns-sd) を止める
}

// handleQuitKey は Ctrl-C / Ctrl-G の終了要求を捌く (走っているものによっては飲む・2 回目まで待つ)。
// エディタの <C-g> (extcmd.IsQuitRequest) も ctrl+g としてここを通す: 迂回すると push / pull / 自己更新 / doctor の削除の
// 途中でもプロセスが落ちる。
func (m *browseModel) handleQuitKey(key string) (tea.Model, tea.Cmd) {
	switch {
	// 🚨 push / pull と共存したときは下の 2 段ガードへ落とす。ここで常時ブロックすると
	// stall した push の唯一の脱出口 (Ctrl-C 2 回) が消える (現状は updateBeginMsg の
	// 譲りで共存しないが、if 1 つに依存させない)。
	case m.actModal.anyUpdating() && !m.actModal.pushing && !m.actModal.pulling:
		// 自己バイナリ更新の中断は CLI を壊しうるので常にブロック (ユーザー選定 2026-07-22)。
		// 並走中はどちらか 1 つでも走っていればブロックする (片方だけ生き残った状態で
		// 終了すると、残った自己更新が孤児のまま進む)。
		// escape は updateTimeout のみ。モーダルに「完了まで終了できません」を出す。
		return m, nil
	case m.doctorOv.Deleting():
		// doctor の削除も push / pull と同じ 2 段ガード。🚨 ここに case が無いと
		// **1 回目の Ctrl-C でプロセスごと落ちる**。それは削除の中断を ctx で伝える経路を
		// 使わないので、記録が executing のまま残り、cli: の子プロセスも孤児化する
		// (doctor_delete.go 冒頭の不変条件を、UI の配線が破る形。2026-09-03 に実測)。
		// 中断の意味づけ (1 回目は武装 / 2 回目で cancel) は doctorview.DoctorView が持つので、
		// 判定を 2 箇所に書かず InterruptDelete へ渡す。**どちらの回でもここで飲む**
		// (2 回目の cancel は相を落とさない = 相を落とすのは receiveDelete なので、
		// 回数で戻り値を分けても同じ値になる)
		m.doctorOv.InterruptDelete()
		return m, m.maybeTick()
	case m.actModal.pushing || m.actModal.pulling:
		// 途中終了は不整合 (特に pull --rebase の mid-rebase 状態) を招くので 1 回目はブロック。
		// ただし stall で永久に閉じられなくならないよう、2 回目の Ctrl-C で強制終了する
		// (quit() の actModal.stop() が走行中 git を cancel。ユーザー選定 2026-07-23)。
		if m.actModal.forceQuitArmed {
			return m.quit()
		}
		m.actModal.forceQuitArmed = true
		return m, nil
	case key == "ctrl+c":
		return m.quitNow() // 緊急脱出は演出なしで最短に抜ける
	default:
		return m.quit()
	}
}

// quit はアプリ全体を終了する (取得中断分は unknown へ落とす)。
func (m *browseModel) quit() (tea.Model, tea.Cmd) { return m.quitWith(true) }

// quitNow は演出なしで即終了する (Ctrl-C の緊急脱出。待たされないことが価値)。
func (m *browseModel) quitNow() (tea.Model, tea.Cmd) { return m.quitWith(false) }

// quitWith は後始末をしてから終了する。animate なら「中央へ吸い込まれる」演出を挟み、
// 着地してから抜ける (tickMsg の settle が tea.Quit を出す)。
//
// 🚨 後始末 (走行中 subprocess の cancel・issues の画面記憶) は演出の前に済ませる: 演出中に
// 端末を閉じられても、止めるべきものは止まっている状態にしておく。
func (m *browseModel) quitWith(animate bool) (tea.Model, tea.Cmd) {
	m.issuesOv.RememberScreen(app.TimeNow()) // 出したまま終了したら次の起動で開き直す (出していなければ消す)
	m.doctorOv.RememberScreen(app.TimeNow()) // 出したまま終了したら次の起動で開き直す (出していなければ消す)
	m.rlDash.RememberScreen(app.TimeNow())   // 出したまま終了したら次の起動で開き直す (出していなければ消す)
	m.filerV.RememberScreen(app.TimeNow())
	m.lanV.RememberScreen(app.TimeNow())
	m.cancelAll()
	// 預けた起動時の検査は捨てる: 閉じる演出の間は done が立たないので、取り消された CI 取得の結果が届くと
	// releaseStartupChecks を通り、終了直前に claude / codex を起こして孤児にする
	m.deferredStartup = nil
	if m.fetching() {
		m.fillUnknown()
	}
	if animate && m.zoom.startClose(app.TimeNow()) {
		return m, m.maybeTick()
	}
	m.done = true
	return m, tea.Quit
}

// restartForNewBinary は「新しいバイナリで自分を置き換える」ことを予約して終了する。
//
// exec を Update の中でやらない: bubbletea が端末を raw mode + Alt Screen で握っている最中に
// プロセスを置き換えると、復元されないまま次のプロセスが同じ端末を触ることになる。tea.Quit で
// 抜けて bubbletea に端末を戻させてから、main.go が exec する。
//
// 後始末 (走行中 subprocess の cancel・issues の画面記憶) は quit と共有する。画面記憶が乗るので、
// issues viewer を開いたまま再起動すると同じ画面から再開する (issues_state.go の TTL 内)。
func (m *browseModel) restartForNewBinary() (tea.Model, tea.Cmd) {
	m.restartRequested = true
	return m.quit()
}

// autobuildAfterPull は pull で自分のソースが更新されていたら、その場で裏ビルドを起動する。
//
// 狙いは「glogx を手で起動し直す手間をなくす」: pull で新しい
// glogx が降ってきても、popup を閉じて開き直すまで再ビルドが始まらないのを避ける。
//
// 🚨 「pull が glogx のソースを含んでいたか」は自分で判定しない。glogx はどの repo でも動くので
// 「今 pull した repo が dotfiles か」の判定が要るように見えるが、shim の指紋比較は自分のソース
// ディレクトリだけを見るので、無関係な repo を pull しても stale にならない = 何も起きない。
// 判定を写経せず shim へ委ねることで、この分岐そのものが不要になる。
//
// 監視中 (起動時に spawn された分がまだ決着していない) なら何もしない: 同じビルドを二重に
// 数えないため。shim の lock があるので二重起動そのものは起きないが、トーストは重複しうる。
func (m *browseModel) autobuildAfterPull() tea.Cmd {
	if m.autobuild.active {
		return nil
	}
	return spawnAutobuildCmd()
}

// restartPromptVisible は再起動ダイアログを今出してよいか。表示 (restartPromptLines) と
// 入力 (handleKey) の両方がこの 1 つの述語を見る。
//
// 🚨 順序を 2 箇所に書かないための関数。「キーは actModal が先に飲む」「描画は
// restartPrompt を後に重ねる」と別々に書くと、claude update 中に裏ビルドが完成すると
// 「最前面のダイアログにどのキーも届かない」状態になる (実測: r/j/q/esc/enter/ctrl+g の
// すべてが無反応。しかも更新中モーダルの『完了まで終了できません』をダイアログが覆うので、
// 効かない理由すら画面から消える)。
//
// actModal が出ている間は出さない (running() ではなく active())。active() は「描かれる」と
// 「キーを消費する」を兼ねる 1 つの述語で、running() は実行中だけを指すので足りない。
// 理由は 2 つあり、どちらか片方だけ見ると事故る:
//
//   - 実行中 (running): このダイアログの r は restartForNewBinary → cancelAll で走行中の
//     claude update / git を殺す。Ctrl-C をブロックしてまで防いでいる当のものなので、
//     押させてはいけない選択肢を提示しない
//   - 確認待ち (y/N): キーは actModal が持っているのに最前面はこちらになる。画面の
//     「その他のキー: 後で」に従って押した y が push を実行した (実測。無反応より危険)
//
// 完成の事実は restartPending が保持しているので、actModal が手を離せば自然に出る。
func (m *browseModel) restartPromptVisible() bool {
	// 🚨 doctor の削除中も出さない。このダイアログの r は cancelAll で走行中の処理を殺すし、
	// 出ている間はどのキーもダイアログに吸われて doctor に届かない (削除の確認が裏に残る)。
	// actModal を除外しているのと同じ理由 (2026-09-03 に実測)
	return m.restartPending && !m.actModal.active() && (!m.doctorOv.Visible() || !m.doctorOv.OwnsKeys())
}

// restartPromptLines は完成ダイアログの箱 (push/pull 確認と同じ見た目)。
func (m *browseModel) restartPromptLines() []string {
	if !m.restartPromptVisible() {
		return nil
	}
	return confirm.Dialog(" 新しい glogx ", []string{"新しいバージョンが利用可能です"},
		"r: 今すぐ再起動   その他: 後で", m.contentWidth(), m.colored)
}
