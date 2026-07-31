package main

// バックグラウンド再ビルドの決着を監視してトーストで知らせる。
//
// bin/lib/go_autobuild.zsh の --async は「ソースが新しければ裏でビルドし、旧バイナリで即
// exec する」(popup にビルド待ちを見せないため)。この結果、走っている glogx からは
//   1. 新版が入った = 次回起動で反映される
//   2. ビルドが失敗して .autobuild.failed が置かれた = ソースを直すまで旧版に固定される
// のどちらも無言だった。2 は特に危険で、気づかないまま旧版を使い続ける (ログは
// src/glogx/.autobuild.log に出るが誰も見ない)。両方をトーストにする。
//
// 監視は shim が立てる GO_AUTOBUILD_PENDING があるときだけ回し、決着を通知したら止める。
// 常時ポーリングにしないのは、glogx の tick が「動くものがある間だけ回す」設計
// (spinnerActive / maybeTick) で、通常起動に恒久 wakeup を足さないため。

import (
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
)

const (
	// autobuildPendingEnv は「裏でビルドを spawn した」ことを shim から受け取る env 名。
	// 対になる export は bin/lib/go_autobuild.zsh。名前を変えるなら両方直すこと。
	autobuildPendingEnv = "GO_AUTOBUILD_PENDING"
	// autobuildFailedStamp はビルド失敗の記録ファイル名 (go_autobuild.zsh が touch する)。
	autobuildFailedStamp = ".autobuild.failed"
	// autobuildPollInterval は決着を見に行く周期。ビルドは数秒で終わるので体感即時に寄せる。
	autobuildPollInterval = 2 * time.Second
	// autobuildWatchTimeout は監視を諦める上限。ビルダーが SIGKILL 等で死ぬと新バイナリも
	// 失敗記録も現れず永遠に決着しないため、無限ポーリングを避ける安全弁。超過時は無言で
	// 止める (嘘の通知を出すより、通知が出ないだけの縮退を選ぶ)。初回の Go toolchain 取得
	// (~90MB DL。go_autobuild.zsh のヘッダ参照) はこれを超えうるので、その場合も無言になる。
	autobuildWatchTimeout = 5 * time.Minute
)

// autobuildResult は監視の決着。
type autobuildResult int

const (
	autobuildRunning   autobuildResult = iota // まだ決着していない
	autobuildStarted                          // 裏でビルドが始まった (起動直後に伝える)
	autobuildInstalled                        // 新バイナリが入った (次回起動で反映)
	autobuildFailed                           // ビルドが失敗した (旧版のまま固定)
	autobuildStale                            // 前回の失敗が残り再挑戦もされない (起動時に判明)
)

// autobuildWatch はバックグラウンドビルドの決着監視。zero value は「監視しない」で、
// tickCmd が nil を返すため tick が 1 本も増えない。
type autobuildWatch struct {
	binPath     string    // 自バイナリのパス (差し替えを mtime で見る)
	failedPath  string    // .autobuild.failed のパス
	binMtime    time.Time // 監視開始時のバイナリ mtime
	failedMtime time.Time // 監視開始時の失敗記録 mtime (不在は zero)
	until       time.Time // この時刻を過ぎたら諦める
	active      bool      // 監視中 (tick を張り続けるか)
	// pending はまだ通知していない結果。起動直後に出す「ビルド中」を Init まで運ぶのに使う
	// (構築時に seed し、Init が handle 経由で取り出す)。
	pending autobuildResult
}

// newAutobuildWatch は exePath (自バイナリ) を監視する状態を作る。pending=false または
// exePath 空なら zero value = 監視しない (os.Executable に失敗した呼び出し側は空を渡せばよい)。
//
// バイナリは go_autobuild.zsh が src_dir 直下へ置くので、失敗記録は同じディレクトリにある。
func newAutobuildWatch(exePath string, pending bool, now time.Time) autobuildWatch {
	if !pending || exePath == "" {
		return autobuildWatch{}
	}
	failed := filepath.Join(filepath.Dir(exePath), autobuildFailedStamp)
	return autobuildWatch{
		binPath:     exePath,
		failedPath:  failed,
		binMtime:    fileMtime(exePath),
		failedMtime: fileMtime(failed),
		until:       now.Add(autobuildWatchTimeout),
		active:      true,
		// 「ビルド中」は起動直後に伝える。完成を待つと数十秒遅れ、その間ユーザーは自分が旧版を
		// 使っていることを知らないまま操作する (ユーザー要望 2026-07-31: 必要と分かった時点で出す)。
		pending: autobuildStarted,
	}
}

// selfExePath は自バイナリのパス。解決できなければ空 (= 監視しない) に落とす。go run や
// テストバイナリから動かした場合も「その実行ファイル」を返すが、監視は env で gate されるので
// 通常は shim 経由の起動でしか使われない。テストが差し替えるため var。
var selfExePath = func() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return exe
}

// autobuildStaleBinary は「前回のビルドが失敗した記録が残っており、動いている自バイナリはその
// 失敗より古い」= 書いたコードが反映されていない状態かを返す。
//
// 失敗記録は go_autobuild.zsh が成功時に消すので、残っていること自体が「最後の試行は失敗」を
// 意味する。それが自バイナリより新しければ「その試行の内容は反映されていない」が導ける。
//
// ⚠️ shim の env で判定しない: env が立つのは「backoff が再挑戦を止めている瞬間」だけなので、
// TTL 超過で再挑戦した経路・shim を経ずバイナリを直接起動した経路・別セッションが撒いた失敗を
// 取りこぼす。失敗記録という事実そのものを見れば、どの経路でも同じ結論になる。
func autobuildStaleBinary(exePath string) bool {
	if exePath == "" {
		return false
	}
	stamp := fileMtime(filepath.Join(filepath.Dir(exePath), autobuildFailedStamp))
	if stamp.IsZero() {
		return false // 失敗記録が無い = 直近の試行は成功しているか、そもそも試行がない
	}
	// 記録より新しいバイナリが置かれている = 失敗の後に (手動 build 等で) 反映済み。
	return stamp.After(fileMtime(exePath))
}

// fileMtime は mtime を返す。不在・stat 失敗は zero (「無い」と同じ扱いで比較に使える)。
func fileMtime(path string) time.Time {
	st, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return st.ModTime()
}

// classifyAutobuild は監視開始時と現在の mtime から決着を判定する純関数。
//
// 失敗を先に見る: go_autobuild.zsh は成功時に失敗記録を消すので両方が同時に「新しく」なる
// ことはないが、判定順を固定しておくと将来の実装変更で曖昧にならない。
func classifyAutobuild(startBin, curBin, startFailed, curFailed time.Time) autobuildResult {
	if curFailed.After(startFailed) {
		return autobuildFailed
	}
	if curBin.After(startBin) {
		return autobuildInstalled
	}
	return autobuildRunning
}

// autobuildMsg は 1 回分の観測結果 (tickCmd が goroutine で stat して運ぶ)。
type autobuildMsg struct{ result autobuildResult }

// tickCmd は次回の観測を予約する。監視していなければ nil (tick を増やさない)。stat は
// この Cmd の goroutine 側で行い、Update には結果だけを渡す (UI 更新経路に I/O を置かない)。
// 比較の基準 (監視開始時の mtime) は不変なので値で捕捉してよい。
func (w *autobuildWatch) tickCmd() tea.Cmd {
	if !w.active {
		return nil
	}
	bin, failed, startBin, startFailed := w.binPath, w.failedPath, w.binMtime, w.failedMtime
	return tea.Tick(autobuildPollInterval, func(time.Time) tea.Msg {
		return autobuildMsg{result: classifyAutobuild(startBin, fileMtime(bin), startFailed, fileMtime(failed))}
	})
}

// handle は観測結果を状態へ反映し、「今トーストで出すべき結果」を返す。期限切れは無言で監視を
// 終える。戻り値 keepWatching が true なら呼び出し側は tickCmd を張り直す。
//
// ⚠️ 以前はトーストが塞がっているとき (busy) 結果を保持して次の tick で出し直していた。トーストが
// 積めるようになった (toast の doc) ので、その調停は不要になり引数から落とした。pending は
// 「起動直後に出す『ビルド中』を Init まで運ぶ」役だけを担う。
func (w *autobuildWatch) handle(res autobuildResult, now time.Time) (out autobuildResult, notify, keepWatching bool) {
	if !w.active {
		return autobuildRunning, false, false
	}
	// 成功は無言で終える: 開始時に「次回起動で反映」と伝えているので、完成の報せは同じことを
	// 二度言うだけになる (トーストが 1 回の出来事で 2 回出るのはノイズ)。
	if res == autobuildInstalled {
		w.active = false
		return autobuildRunning, false, false
	}
	// 失敗は開始の通知より優先する (「ビルド中」を出す前に落ちたら、出すべきは失敗の方)。
	if res == autobuildFailed {
		w.pending = autobuildFailed
	}
	if w.pending != autobuildRunning {
		out = w.pending
		w.pending = autobuildRunning
		if out == autobuildStarted {
			return out, true, true // 開始を伝えた後も、失敗の可能性があるので監視は続ける
		}
		w.active = false
		return out, true, false
	}
	// 未決着のまま期限を過ぎたら諦める (ビルダーがシグナルで死ぬと決着しないため。tick を
	// 永久に残さない安全弁)。
	if !now.Before(w.until) {
		w.active = false
		return autobuildRunning, false, false
	}
	return autobuildRunning, false, true
}

// autobuildToast は決着に対応するトースト文面と成功色フラグを返す。
func autobuildToast(res autobuildResult) (text string, ok bool) {
	switch res {
	case autobuildStarted:
		return "新しい glogx をビルド中 (次回起動で反映)", true
	case autobuildInstalled:
		return "", false // 成功は無言 (開始時に伝えている)
	case autobuildFailed:
		return "glogx のバックグラウンドビルドが失敗 (旧版で継続。src/glogx/.autobuild.log)", false
	case autobuildStale:
		return "前回の glogx のビルドが失敗したままです (旧版で継続。src/glogx/.autobuild.log)", false
	case autobuildRunning:
		return "", false
	}
	return "", false
}
