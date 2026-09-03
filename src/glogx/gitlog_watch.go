package main

// git log を表示している間、別プロセスによる変化 (別ターミナルの commit / rebase、
// Claude Code、git pull / fetch) を見張ってその場で反映する (ユーザー要望 2026-09-01)。
//
// 反映の機構は既にある (reloadLog)。ここが足すのは「変わったと気づく」ことだけ。
//
// 方式は issues viewer の見張り (issues_watch.go) と同じ「イベントで起こし、指紋で判定する」:
//
//   - fsnotify で .git のイベントを待つ。commit / rebase / fetch は必ず .git 配下を書くので
//     体感即時で気づける
//   - 🚨 イベントは真偽の正本にしない。git は 1 操作で index / logs / refs / *.lock を続けて
//     書き、rebase では HEAD が何度も動く。起こされたら必ず指紋を測り直し、本当に表示が
//     変わったときだけ読む
//   - 保険として 1 分ポーリングも回す。イベントを取りこぼしても、watcher を作れない環境
//     (fd 上限) でも必ず追いつく (その場合は最悪 1 分遅れる)
//
// 🚨 フレーム tick (spinnerActive) には混ぜない: 混ぜると起動中ずっと 12.5fps で起きることに
// なり、「動くものがある間だけ tick を回す」という glogx の設計を崩す。usageRefreshTick や
// issues の見張りと同じく、自分の周期で自己再アームする独立チェーンにする。

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

const (
	// gitLogWatchPoll はイベントの取りこぼしに備えた保険の周期 (ユーザー指定: 1 分)。
	gitLogWatchPoll = time.Minute
	// gitLogWatchDebounce はイベントのバーストを畳む静穏時間。1 操作で複数ファイルが書かれる
	// だけでなく、rebase / cherry-pick は数百 ms 間隔で HEAD を動かすので、issues の見張り
	// (200ms) より長く取って 1 回の反映へ畳む (反映 1 回ごとに git log と CI の再取得が走る)。
	// 🚨 drainWatchEvents の静穏は無制限にリセットされるので、`.git` を書き続けるプロセス
	// (gc / repack / 大量 fetch) がいる間はイベント経路が返らない。その間は保険のポーリングが
	// 受け持つ (縮退するのは即時性だけ)。
	gitLogWatchDebounce = time.Second
)

// gitLogProbeMsg は「何かあった / 周期が来た」の合図 (指紋はまだ測っていない)。
//
// 測定 (git fork) を Update 側の判断の後に回すため、tick の中では測らない: ポップアップを
// 開いている間は反映を見送るので、その間の fork をまるごと省ける。
type gitLogProbeMsg struct {
	// fromEvent は発行元がイベント経路か (false = 保険のポーリング)。🚨 受け取り側は
	// 「届けたチェーンの札だけ」を降ろすのにこれを使う。両方降ろすと、まだ w.Events で
	// ブロックしている goroutine が居るのに evArmed が false になり、single-flight を
	// すり抜けて 2 本目が張られる (観測 1 回につき goroutine が 1 本ずつ積み上がる)。
	fromEvent bool
	// closed は「イベントの経路が閉じた」(watcher が死んだ)。
	closed bool
	gen    int // 発行時点の世代 (watcher を作り直すと増える)
}

// gitLogFPMsg は 1 回分の測定結果。ok=false は「測れなかった」(git の失敗 / timeout) で、
// 変化なしとも変化ありとも解釈しない第 3 の結果 = 基準を汚さず次の周期で測り直す。
type gitLogFPMsg struct {
	fp  string
	ok  bool
	gen int
}

// gitLogDirsMsg は見張り対象ディレクトリの解決結果 (起動時に 1 回)。
type gitLogDirsMsg struct{ dirs []string }

// gitLogReloadMsg は外部変更の反映のために Update の外で読み直した git log (reflectGitLogChange)。
type gitLogReloadMsg struct {
	data logData
	err  error
	gen  int // logWatch.gen (見張りの世代)
	seq  int // logWatch.reloadSeq (読み直しの世代。届くまでに別の読み直しが入っていたら捨てる)
}

// gitLogWatch は見張りの状態。zero value は「まだ何も張っていない」。
type gitLogWatch struct {
	w    dirWatcher
	dirs []string // 見張るディレクトリ (空 = 解決できなかった → ポーリングのみ)
	gen  int      // 世代 (watcher を閉じるたびに増える。古いチェーンの観測を弾く)
	seen string   // 反映済みの指紋
	// hasSeen は seen が有効か。🚨 空文字列を「基準なし」のセンチネルにしないこと: コミット 0 件
	// (revs が空範囲) の指紋は正当に "" になるため、区別できないと変化の判定が狂う。
	hasSeen bool
	// チェーンは 2 本 (イベント待ち / 保険のポーリング)。それぞれ二重に張らない。
	evArmed   bool
	pollArmed bool
	// measuring は指紋の測定が in-flight か (観測が来るたびに git を重ねない)。
	// 🚨 厳密な排他ではない: 自分で読み直したとき (reloadLog) は飛んでいる測定を捨てるために
	// 札を降ろすので、その結果が届く前に次の測定が始まりうる (最大 fork 1 本の重複)。
	measuring bool
	// reloading は外部変更の反映のための読み直し (reflectGitLogChange) が in-flight か。
	// 立っている間は測定を見送る (読み直し中に測っても、結果は読み直しが基準を降ろした後に
	// 捨てられるだけ)。
	reloading bool
	// reloadSeq は読み直しの世代。applyLogData が進める。飛んでいる非同期の読み直しが、その後に
	// 入った pull の読み直しを古い状態で上書きしないための札 (gen は見張りの世代で、pull では進まない)。
	reloadSeq int
}

// gitLogWatchDirsCmd は見張るディレクトリを解決する (git を叩くので Init の同期経路に乗せない)。
func gitLogWatchDirsCmd() tea.Cmd {
	return func() tea.Msg { return gitLogDirsMsg{dirs: gitLogWatchDirs()} }
}

// gitLogWatchDirs は .git のうちイベントを取りたいディレクトリ (fsnotify は再帰しない)。
//
// worktree では HEAD / index が worktree 専用ディレクトリ、refs / packed-refs / logs が
// 共有ディレクトリにあるので両方見る (実測 2026-09-01: --absolute-git-dir は
// .git/worktrees/<name>、--git-common-dir は .git を返す)。
func gitLogWatchDirs() []string {
	out, err := runGitTimeout("rev-parse", "--absolute-git-dir")
	if err != nil {
		return nil // repo の外 / git が無い: 見張らない (ポーリングも指紋が取れないので空振りする)
	}
	gitDir := strings.TrimSpace(out)
	if gitDir == "" {
		return nil
	}
	// --path-format は git 2.31+。失敗したら共有側を諦めて worktree 側だけ見る
	// (HEAD の動きは拾えるので無音にはならず、refs の変化は 1 分ポーリングが拾う)。
	common := gitDir
	if cOut, cErr := runGitTimeout("rev-parse", "--path-format=absolute", "--git-common-dir"); cErr == nil {
		if p := strings.TrimSpace(cOut); p != "" {
			common = p
		}
	}
	dirs := []string{gitDir}
	if common != gitDir {
		dirs = append(dirs, common)
	}
	// refs / logs は入れ子で、fsnotify は再帰しない。🚨 親だけを見張ると、スラッシュ入りの
	// ブランチ名 (refs/heads/feature/x) や remote ごとの ref (refs/remotes/origin/…)、
	// reflog (logs/refs/…) の更新がイベントにならない = そこだけポーリング待ちになる
	// (敵対レビューで指摘 2026-09-01)。ディレクトリを辿って全部登録する。
	for _, root := range []string{filepath.Join(common, "refs"), filepath.Join(common, "logs")} {
		dirs = appendSubdirs(dirs, root)
	}
	return dirs
}

// gitLogWatchMaxDirs は見張るディレクトリ数の上限。kqueue (macOS) は 1 ディレクトリにつき fd を
// 1 本使うので、ref を大量に持つ repo で fd を食い潰さないよう頭を止める。溢れた分はイベントが
// 来ないだけで、1 分ポーリングが受け持つ (無音にはならない)。
const gitLogWatchMaxDirs = 64

// appendSubdirs は root とその配下のディレクトリを dirs へ足す (上限まで)。
func appendSubdirs(dirs []string, root string) []string {
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 読めない枝は飛ばす (権限・競合で消えた)
		}
		if !d.IsDir() {
			return nil
		}
		if len(dirs) >= gitLogWatchMaxDirs {
			return filepath.SkipAll
		}
		dirs = append(dirs, path)
		return nil
	})
	return dirs
}

// startGitLogWatch は watcher を用意して対象ディレクトリを登録する。
//
// 🚨 「Add 済み」を自前で覚えて skip しないこと。fsnotify の watch はディレクトリが消えると
// 黙って失われる (git は refs/heads を packed-refs へ畳んだり rebase 用ディレクトリを作り消し
// したりする)。Add は冪等なので毎回無条件に呼び、消えて戻った先を取り戻す (issues_watch.go の
// 同じ注記が起源)。watcher を作れない環境では黙ってポーリングだけに縮退する。
func (m *browseModel) startGitLogWatch() {
	if len(m.logWatch.dirs) == 0 {
		return
	}
	if m.logWatch.w == nil {
		w, err := newDirWatcher()
		if err != nil {
			return
		}
		m.logWatch.w = w
	}
	for _, dir := range m.logWatch.dirs {
		_ = m.logWatch.w.Add(dir) // 失敗は次の周期で再挑戦 (存在しない refs/tags 等)
	}
}

// stopGitLogWatch は watcher を閉じて世代を進める (cancelAll から呼ぶ後始末)。
//
// 🚨 通常終了ではプロセス終了が fd を回収するが、再起動 (restartSelf の syscall.Exec) は fd
// テーブルを引き継ぐため明示的に閉じないと漏れる (issues viewer と同じ形。理由の正本は
// cancelAll の doc)。この見張りは起動から終了まで開き続けるので、閉じ忘れると r で再起動する
// たびに kqueue fd が 1 本ずつ新プロセスへ継承される。
func (m *browseModel) stopGitLogWatch() {
	if m.logWatch.w != nil {
		_ = m.logWatch.w.Close()
	}
	// dirs は残す (解決し直す必要がない)。世代を進めて古いチェーンの観測を弾く。
	m.logWatch = gitLogWatch{gen: m.logWatch.gen + 1, dirs: m.logWatch.dirs}
}

// gitLogWatchCmd は次の観測を予約する (イベント待ち + 保険のポーリング)。
func (m *browseModel) gitLogWatchCmd() tea.Cmd {
	return tea.Batch(m.gitLogEventCmd(), m.gitLogPollCmd())
}

// gitLogEventCmd は fsnotify のイベントを 1 回待ち、バーストを畳んでから合図を返す。
// ブロックする Cmd にするのは、外から Msg を送る口を持たずに済むため (bubbletea の定石)。
func (m *browseModel) gitLogEventCmd() tea.Cmd {
	if m.logWatch.evArmed || m.logWatch.w == nil {
		return nil
	}
	m.logWatch.evArmed = true
	w, gen := m.logWatch.w, m.logWatch.gen
	return func() tea.Msg {
		select {
		case _, ok := <-w.Events():
			if !ok {
				return gitLogProbeMsg{fromEvent: true, closed: true, gen: gen}
			}
		case _, ok := <-w.Errors():
			if !ok {
				return gitLogProbeMsg{fromEvent: true, closed: true, gen: gen}
			}
			// エラーは握って測定へ倒す (指紋が正本なので、測り直せば辻褄は合う)
		}
		drainWatchEvents(w, gitLogWatchDebounce)
		return gitLogProbeMsg{fromEvent: true, gen: gen}
	}
}

// gitLogPollCmd は保険のポーリング。イベントを取りこぼしても必ず追いつく。
func (m *browseModel) gitLogPollCmd() tea.Cmd {
	if m.logWatch.pollArmed {
		return nil
	}
	m.logWatch.pollArmed = true
	gen := m.logWatch.gen
	return tea.Tick(gitLogWatchPoll, func(time.Time) tea.Msg {
		return gitLogProbeMsg{gen: gen}
	})
}

// gitLogMeasureCmd は表示中の git log の指紋を測る (git fork 1 本)。
func (m *browseModel) gitLogMeasureCmd() tea.Cmd {
	opts, gen := m.opts, m.logWatch.gen
	return func() tea.Msg {
		out, err := runGitTimeout(BuildFingerprintArgs(opts)...)
		if err != nil {
			return gitLogFPMsg{gen: gen}
		}
		return gitLogFPMsg{fp: out, ok: true, gen: gen}
	}
}

// handleGitLogDirs は対象ディレクトリの解決結果を受けて見張りを開始する。
func (m *browseModel) handleGitLogDirs(msg gitLogDirsMsg) tea.Cmd {
	m.logWatch.dirs = msg.dirs
	m.startGitLogWatch()
	return m.gitLogWatchCmd()
}

// handleGitLogProbe は合図を受けて、測るかどうかを決める (常に次の観測を予約する)。
func (m *browseModel) handleGitLogProbe(msg gitLogProbeMsg) tea.Cmd {
	if msg.gen != m.logWatch.gen {
		return nil // 世代が進む前に張った古いチェーン (札も触らない)
	}
	if msg.closed {
		// watcher が死んだ (fd 回収等)。閉じてポーリングだけで続ける (無音にはしない)
		m.logWatch.evArmed = false
		if m.logWatch.w != nil {
			_ = m.logWatch.w.Close()
			m.logWatch.w = nil
		}
		m.logWatch.gen++
		// 🚨 飛んでいる測定の札もここで降ろす: 世代を進めた後に届く結果は gen 違いで捨てられるので、
		// 降ろさないと measuring が永久に true のまま = 以降ひとつも測らなくなる (静かに機能停止)。
		m.logWatch.measuring = false
		m.logWatch.reloading = false // 同じ理由 (gen 違いで捨てられる読み直しを待ち続けない)
		m.logWatch.pollArmed = false // 旧世代の tick は捨てられるので張り直す
		return m.gitLogWatchCmd()
	}
	if msg.fromEvent {
		m.logWatch.evArmed = false
	} else {
		m.logWatch.pollArmed = false
		m.startGitLogWatch() // 消えて戻ったディレクトリを取り戻す (Add は冪等)
	}
	if m.logWatch.measuring || m.logWatch.reloading || m.gitLogReloadDeferred() {
		// 見送る = 基準 (seen) を触らないので、反映できるようになった次の観測で気づく
		return m.gitLogWatchCmd()
	}
	m.logWatch.measuring = true
	return tea.Batch(m.gitLogMeasureCmd(), m.gitLogWatchCmd())
}

// handleGitLogFP は測定結果を受けて、変わっていれば反映する。
func (m *browseModel) handleGitLogFP(msg gitLogFPMsg) tea.Cmd {
	if msg.gen != m.logWatch.gen {
		return nil
	}
	if !m.logWatch.measuring {
		// 誰も待っていない測定結果 = 測っている間に自分で読み直した (reloadLog / refetchAfterPush が
		// 札と基準を降ろす)。古い測定値を基準にすると、次の観測で必ず不一致になって無駄な再読込と
		// トーストが 2 回走る。捨てて測り直す。
		return nil
	}
	m.logWatch.measuring = false
	if !msg.ok {
		return nil // 測れなかった: 基準を汚さない (チェーンは予約済みなので次の周期で測り直す)
	}
	// 🚨 見送り判定は「測る前」と「反映する前」の両方で見る: 測定 (git fork) の最中にポップアップを
	// 開かれると、開始時のチェックだけでは開いている内容のキャッシュを消してしまう (reloadLog が
	// diffOv / prStatusOv / detailOv を reset する)。基準を触らないので、閉じた後の観測で反映される。
	if m.gitLogReloadDeferred() {
		return nil
	}
	if !m.logWatch.hasSeen {
		// 基準がまだ無い = 起動直後か、自分で読み直した直後 (reloadLog が基準を降ろす)。
		// 🚨 測定値を無条件に基準にすると、読み込み (LoadCommits) 〜 測定の窓に入った変化を
		// 「元からそう」と飲み込んでしまうので、手元のコミット列と突き合わせてその窓を閉じる。
		if gitLogFPMatchesCommits(msg.fp, m.commits) {
			m.logWatch.seen, m.logWatch.hasSeen = msg.fp, true
			return nil
		}
		return m.reflectGitLogChange()
	}
	if msg.fp == m.logWatch.seen {
		return nil
	}
	// 反映すると reloadLog が基準を降ろす (hasSeen = false)。🚨 ここで測定値を基準に置き直さない:
	// reloadLog の読み直しは測定より新しい状態を読むことがあり (連続 commit・rebase 中・
	// -p で読み直し自体に 100ms 級かかる)、古い測定値を基準にすると次の観測で必ず不一致になって
	// 「表示は何も変わらないのにトーストだけ出る」再読込が 1 回増える (敵対レビューで実測)。
	// 降りたままにすると、次の測定が手元のコミット列と突き合わせて基準を作る。
	// 反映に失敗したときは基準が元のまま残る = 次の周期で再挑戦する。
	return m.reflectGitLogChange()
}

// gitLogReloadDeferred は「変化に気づいたが今は反映しない」状態か。
//
//   - SHA を握っているポップアップ (diff / PR 状態 / job 詳細ログ) は、下から差し替えると
//     開いている内容ごとキャッシュが消える (reloadLog が reset する)
//   - 実行中・確認待ちのモーダル (push / pull / rerun / update) の下でコミット列が入れ替わると、
//     確認した対象と実行する対象がずれる
//   - job パネルも SHA を握る UI で、reloadLog は closePanel() で黙って閉じる
//   - 演出中 (pull / push アニメ) は offset がアニメの進行度なので、錨の画面行を測れない
//   - 全画面の viewer (issues / status / 残量ダッシュボード) を見ている間は、そもそも git log が
//     見えていない。反映するとトーストだけが viewer の上に出て、裏でカーソルのリセットと CI の
//     再取得 (GitHub API) と見えないアニメの tick が走る (敵対レビューで実測 2026-09-01)
//
// いずれも見送るだけで基準は動かさないので、閉じた後の観測で反映される (最悪 1 分後)。
func (m *browseModel) gitLogReloadDeferred() bool {
	return m.actModal.active() || m.diffOv.visible() || m.prStatusOv.visible() ||
		m.detailOv.visible() || m.panelSHA != "" || m.pullAnimating || m.pushAnimating ||
		m.issuesOv.visible() || m.statusOv.visible() || m.rlDash.visible()
}

// reflectGitLogChange は外部の変更を画面へ反映する (読み直しを Cmd へ出し、結果は
// handleGitLogReload が受ける)。
//
// 読み直しは git を 5-6 本 fork する (loadLogData)。合成 repo (60 コミット × 各 4000 行 patch) での
// 実測 2026-09-01 は既定 22ms / --stat 45ms / -p 139ms で、Update の中で同期にやると rebase 中は
// 1 秒ごとにその停止が無操作で入る (issue 146)。pull の全面リロードは利用者の操作なので同期のまま。
//
// 🚨 LoadCommits は timeout なしの runGit を使う (起動時の同期経路と同じ関数)。git が stall しても
// Update は止まらないが、reloading の札が立ったままになり自動追従だけが止まる (次の観測は全部
// 見送られる)。起動もできない repo なので追従だけを timeout で救う形は採らない。
func (m *browseModel) reflectGitLogChange() tea.Cmd {
	m.logWatch.reloading = true
	opts, colored, oneline := m.opts, m.colored, m.oneline
	gen, seq := m.logWatch.gen, m.logWatch.reloadSeq
	return func() tea.Msg {
		d, err := loadLogData(opts, colored, oneline)
		return gitLogReloadMsg{data: d, err: err, gen: gen, seq: seq}
	}
}

// handleGitLogReload は読み直しの結果をモデルへ入れる。
//
// 捨てる条件はいずれも「基準 (seen) を触らない」ので、次の観測で変化に気づいて再挑戦する:
//
//   - 見張りの世代が進んだ (watcher の閉じ直し) / 読み直しの世代が進んだ (この間に pull が入った。
//     古い logData を入れると pull の結果より古い状態へ戻る)
//   - 読み直しに失敗した (警告だけ出す)
//   - 見送り状態になった (読み直している間にポップアップが開かれた等。applyLogData は開いている
//     内容のキャッシュを消す)
//
// カーソルが先頭にいるときだけ pull と同じ演出 (新規行が上から降る) に倒し、途中を読んで
// いるときは見ているコミットを同じ画面行に留める (ユーザー選定 2026-09-01: 読んでいる行を
// ずらさない)。「先頭を見ている」の判定と錨は**ここ**で (= 行集合を入れ替える直前に) 測る:
// Cmd を出してから届くまでに利用者はスクロールしている。
func (m *browseModel) handleGitLogReload(msg gitLogReloadMsg) tea.Cmd {
	if msg.gen != m.logWatch.gen || msg.seq != m.logWatch.reloadSeq {
		return nil
	}
	m.logWatch.reloading = false
	if msg.err != nil {
		m.showWarning("git log の再読込に失敗しました: " + firstLine(msg.err.Error()))
		return nil
	}
	if m.gitLogReloadDeferred() {
		return nil
	}
	// 🚨 「先頭を見ている」は cursor だけでは決まらない: ctrl+d / pgdown はカーソルを動かさず
	// ビューポートだけ下げるので、cursor == 0 のまま下の方を読んでいる状態がある。offset も見ないと
	// その状態で画面が先頭へ飛ぶ (この機能の主旨に反する)。
	keepView := m.cursor > 0 || m.offset > 0
	added, cmd := m.applyLogData(msg.data, keepView)
	m.toast.show(gitLogChangeToast(added), true)
	return tea.Batch(cmd, m.maybeTick())
}

// gitLogChangeToast は反映を伝える文言。先頭に増えた分が主役なので件数を出し、
// 増えていない変化 (amend / rebase / decoration の移動) は件数を出さない。
func gitLogChangeToast(added int) string {
	if added > 0 {
		return fmt.Sprintf("新しいコミット %d 件", added)
	}
	return "git log を更新しました"
}

// gitLogFPMatchesCommits は指紋の SHA 列が今表示しているコミット列と一致するか。
//
// decoration は比べない: 表示用の読み込みは --color=always なので %D に ANSI が入りうる
// 一方、指紋は --color=never で測る (BuildFingerprintArgs) ため、文字列として比べると
// 色付き表示では常に不一致になる。SHA だけなら両者で同じ表現になる。
func gitLogFPMatchesCommits(fp string, commits []Commit) bool {
	shas := fingerprintSHAs(fp)
	if len(shas) != len(commits) {
		return false
	}
	for i, c := range commits {
		if shas[i] != c.SHA {
			return false
		}
	}
	return true
}

// fingerprintSHAs は指紋 (1 行 1 コミット、SHA \x1f decoration) から SHA 列を取り出す。
func fingerprintSHAs(fp string) []string {
	fp = strings.TrimRight(fp, "\n")
	if fp == "" {
		return nil
	}
	lines := strings.Split(fp, "\n")
	shas := make([]string, 0, len(lines))
	for _, line := range lines {
		sha, _, _ := strings.Cut(line, fieldSep)
		shas = append(shas, sha)
	}
	return shas
}
