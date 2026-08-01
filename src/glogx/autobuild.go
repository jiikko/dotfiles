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
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

const (
	// autobuildPendingEnv は「裏でビルドを spawn した」ことを shim から受け取る env 名。
	// 対になる export は bin/lib/go_autobuild.zsh。名前を変えるなら両方直すこと。
	autobuildPendingEnv = "GO_AUTOBUILD_PENDING"
	// autobuildFailedStamp はビルド失敗の記録ファイル名 (go_autobuild.zsh が書く)。
	// ⚠️ 中身 (失敗した入力の指紋) はここでは読まない。使うのは存在と mtime だけ。
	autobuildFailedStamp = ".autobuild.failed"
	// autobuildRevStamp はビルド元の tree hash の記録 (go_autobuild.zsh が書く。診断用)。
	// これがあると「古い版で動いています」に「どの版が動いているか」を添えられる。
	// ⚠️ stale の判定には使わない (判定は shim 側の指紋。理由は go_autobuild.zsh の doc)。
	autobuildRevStamp = ".autobuild.rev"
	// autobuildLockDir はビルド中を示す lock ディレクトリ名 (go_autobuild.zsh が mkdir する)。
	// autobuildFailedStamp と同じく shim との取り決めなので、名前を変えるなら両方直すこと。
	autobuildLockDir = ".autobuild.lock"
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
	autobuildStale                            // 自バイナリが今のソースを反映していない (起動時に判明)
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

// autobuildStaleBinary は「動いている自バイナリが今のソースを反映していない」かを返す。
//
// 根拠は 2 つで、どちらも同じ事実 (書いたコードが動いていない) を立証する:
//
//  1. ビルド失敗の記録が自バイナリより新しい。失敗記録は go_autobuild.zsh が成功時に消すので、
//     残っていること自体が「最後の試行は失敗」を意味する
//  2. ソースが自バイナリより新しい (誰もビルドしていない)
//
// 2 を見るのは、shim が再ビルドを spawn しない経路が複数あるため: lock 残留で「他がビルド中」と
// 誤認する / 同期ツール (rsync -a 等) でソースの mtime が巻き戻り shim の -nt が偽になる /
// shim を経ずバイナリを直接起動する。原因を 1 つずつ塞ぐのではなく「ソースの方が新しい」という
// 1 つの事実へ還元する (issue 033)。
//
// ⚠️ shim の env で判定しない: env が立つのは「backoff が再挑戦を止めている瞬間」だけなので、
// TTL 超過で再挑戦した経路・shim を経ずバイナリを直接起動した経路・別セッションが撒いた失敗を
// 取りこぼす。ファイルという事実そのものを見れば、どの経路でも同じ結論になる。
func autobuildStaleBinary(exePath string) bool {
	if exePath == "" {
		return false
	}
	dir := filepath.Dir(exePath)
	// ビルド中は黙る: lock がある = shim が「他がビルド中」と判断しているのと同じ状態で、
	// ここで警告すると走っているビルドを「していない」と嘘をつく (連続起動で実際に起きる:
	// 1 本目が spawn したビルドの最中に 2 本目を起動すると、shim は lock を取れず env も立てない)。
	// 死んだ lock の始末は shim 側が持つ (pid 生存 + timeout)。
	if _, err := os.Stat(filepath.Join(dir, autobuildLockDir)); err == nil {
		return false
	}
	binAt := fileMtime(exePath)
	if binAt.IsZero() {
		return false // 自バイナリが読めない = 比較の基準が無い
	}
	// 記録より新しいバイナリが置かれている = 失敗の後に (手動 build 等で) 反映済み。
	if fileMtime(filepath.Join(dir, autobuildFailedStamp)).After(binAt) {
		return true
	}
	return autobuildSourcesNewer(dir, binAt)
}

// autobuildSourcesNewer は dir 配下のソースが binAt (自バイナリ) より新しいかを返す。
//
// ⚠️ 「ソース」の定義は go_autobuild.zsh の再帰 glob と揃える (**/*.go から _test.go を除く +
// 直下の go.mod / go.sum)。食い違うと「shim は再ビルドしないのに glogx は古いと言う」矛盾、
// あるいはその逆 (黙って旧版に固定) が出る。片方を変えたら両方直すこと。
func autobuildSourcesNewer(dir string, binAt time.Time) bool {
	// ソース木の外では判定しない (配布・コピーされたバイナリ、テストバイナリの一時ディレクトリ)。
	// go_autobuild.zsh はバイナリを src_dir 直下へ置くので、隣に go.mod があるのがソース木の印。
	// shim はこのゲートを持たないが、go.mod が無ければ go build 自体が通らないので実質の
	// 食い違いは起きない (不一致が起きるとしても「shim は再ビルドするが glogx は黙る」= 安全側)。
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		return false
	}
	newer := false
	// 起動パスに乗るが fork は無い。実測 169µs/回 (2026-07-31 の src/glogx = 90 ファイル /
	// 6 ディレクトリ)。外部プロセス起動と比べても十分小さく、Bench の分解能以下。
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return nil // 読めないものは無視する (判定は best-effort。止める理由にはしない)
		case d.IsDir():
			if path != dir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir // zsh の ** も dot ディレクトリを辿らない
			}
			return nil
		case !autobuildSourceFile(dir, path, d.Name()):
			return nil
		case fileMtime(path).After(binAt):
			newer = true
			return filepath.SkipAll // 1 つ見つかれば結論は出る
		}
		return nil
	})
	return newer
}

// autobuildSourceFile は path が再ビルドの契機になるファイルか (go_autobuild.zsh と同じ定義)。
func autobuildSourceFile(dir, path, name string) bool {
	if strings.HasSuffix(name, ".go") {
		return !strings.HasSuffix(name, "_test.go") // テストは go build の入力ではない
	}
	// go.mod / go.sum は直下だけ (shim も $src_dir 直下しか見ない。サブモジュールは対象外)
	return (name == "go.mod" || name == "go.sum") && filepath.Dir(path) == dir
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
	// 完成は伝える。以前は無言だった (開始時に「次回起動で反映」と伝えているため二度言うことに
	// なる) が、その場で再起動できるようになったので意味が変わった: 「次回起動で反映」は待ちの
	// 案内、完成は行動できる合図。⚠️ 出し方はトーストでなく再起動を促すダイアログ (呼び出し側)。
	// 数秒で消えるトーストだと、目を離している間に行動の機会だけが消える。
	if res == autobuildInstalled {
		w.active = false
		return autobuildInstalled, true, false
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

// autobuildRunningRev は「動いているバイナリがどこから作られたか」を短く返す (" (tree abc1234)")。
// 記録が無ければ空文字 — shim を経ずに置かれたバイナリや、この記録が入る前のビルドで起きる。
//
// ⚠️ 判定には使わない。tree hash はコミット済みの内容しか見ないので「今より新しいか」は
// 言えない (未コミットの編集が見えない)。ここで言えるのは「どの版か」だけで、それで十分:
// 追う人が git show でその tree を辿れる。
func autobuildRunningRev(exePath string) string {
	if exePath == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(exePath), autobuildRevStamp))
	if err != nil {
		return ""
	}
	rev := strings.TrimSpace(string(b))
	if rev == "" {
		return ""
	}
	short, dirty, _ := strings.Cut(rev, " ")
	if len(short) > 12 {
		short = short[:12]
	}
	if dirty != "" {
		short += " " + dirty
	}
	return " (動いているのは tree " + short + ")"
}

// autobuildToast は決着に対応するトースト文面と成功色フラグを返す。
func autobuildToast(res autobuildResult) (text string, ok bool) {
	switch res {
	case autobuildStarted:
		return "新しい glogx をビルド中 (次回起動で反映)", true
	case autobuildInstalled:
		return "", false // 完成はトーストにしない (再起動ダイアログで出す。handle の doc)
	case autobuildFailed:
		return "glogx のバックグラウンドビルドが失敗 (旧版で継続。src/glogx/.autobuild.log)", false
	case autobuildStale:
		// 原因 (失敗記録 / 誰もビルドしていない) ではなく次の行動を出す: どちらでも復旧手順は
		// 同じで、理由はログにある。動いている版が分かるなら添える (どれだけ古いかの手がかり)。
		return "glogx が古い版で動いています" + autobuildRunningRev(selfExePath()) +
			" (GO_AUTOBUILD_SYNC=1 glogx で再ビルド。src/glogx/.autobuild.log)", false
	case autobuildRunning:
		return "", false
	}
	return "", false
}
