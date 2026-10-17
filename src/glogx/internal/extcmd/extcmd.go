// Package extcmd は glogx の画面が外部コマンド (エディタ・ブラウザ・クリップボード) を起こす口と、その結果の Msg。
// どれもテストが実コマンドを起こさないための差し替え点 (パッケージ変数) を持つ。main と画面のパッケージ
// (internal/issuesview など) が共有するので internal に置く (main は下位から import できない。issue 817)。
package extcmd

import (
	"context"
	"os/exec"
	"runtime"
	"strconv"
	"subproc/clipboard"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/jiikko/dotfiles/src/tuikit/editor"
	"github.com/jiikko/dotfiles/src/tuikit/foreground"
)

// OpenURLMsg は job 詳細ページをブラウザで開いた結果。
type OpenURLMsg struct{ Err error }

// EditorClosedMsg は job ログを開いた nvim を閉じた結果 (e キー)。reclaimErr は戻ったときに外れていた端末の前面を
// 取り戻せなかった理由。
type EditorClosedMsg struct{ Err, ReclaimErr error }

// RunEditorCmd はテストで実 nvim を起動しないための差し替え点。tea.Exec は bubbletea の描画を一旦止め、端末を nvim へ
// 明け渡し、終了後に復帰する (エディタ起動用途の標準経路)。foreground.Exec は、戻った後に前面が外れていたら
// 入れ直しの前に取り戻す (treefiler の ! の `$SHELL -ic` が前面を持ち去ったまま終わる形。issue 707)。
var RunEditorCmd = func(cmd *exec.Cmd) tea.Cmd {
	x := foreground.NewExec(cmd)
	return tea.Exec(x, func(err error) tea.Msg { return EditorClosedMsg{Err: err, ReclaimErr: x.Err} })
}

// OpenInBrowser はテストで実ブラウザを開かないための差し替え点。
var OpenInBrowser = func(url string) error {
	switch runtime.GOOS {
	case "darwin":
		// open は URL を LaunchServices へ渡して即 detach する (ブラウザの終了を待たない) ので
		// timeout も WaitDelay も要らない。Run() なので stdout/stderr は /dev/null 直結でパイプも無い。
		return exec.Command("open", url).Run() // subproc: no-waitdelay — 即 detach・パイプ無し
	default:
		// xdg-open は環境によってブラウザプロセスへ直接 exec して終了まで戻らないことが
		// ある (既知挙動)。tea.Cmd の goroutine で同期 Run するため時間で区切る (issue 029 P3)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		// subproc: no-waitdelay — Run() は stdout/stderr が /dev/null 直結でパイプを作らないため、
		// 「孫がパイプを握って Wait が戻らない」形が起きない (ctx の kill だけで足りる)。
		return exec.CommandContext(ctx, "xdg-open", url).Run() // subproc: no-waitdelay
	}
}

// CopyToClipboard はテストで実クリップボードを触らないための差し替え点。本体と失敗モード (OSC52 の exit 0・xclip のハング・
// timeout の分け方) は subproc/clipboard の doc。
var CopyToClipboard = clipboard.Copy

// EditorCommand は実ファイルを開くエディタのコマンドを組む。**解決の規則 ($VISUAL → $EDITOR → nvim、
// 値は空白で語分割、quote は解釈しない) は github.com/jiikko/dotfiles/src/tuikit/editor が持つ** (pro-con と共有。2 実装にしない)。
// 起動に失敗したときは tui.go の EditorClosedMsg がトーストで理由を出す。
//
// 🚨 この経路は「実ファイルを 1 つ開く」ものにだけ使う。nvim を直に呼んでいる他の 2 箇所
// (tui.go の job ログ = 標準入力 + -c の scratch バッファ / open_workspace.go の `nvim .` =
// ディレクトリを開く) は nvim 固有の機能に依存しており、任意の $EDITOR では成立しない。
//
// 🚨 **前提: 起動したプロセスの終了 = 編集の完了**。glogx は tea.Exec (foreground.Exec) で TUI を中断して
// 子プロセスを待ち、戻った境界で一覧と本文を取り直す (tui.go の EditorClosedMsg)。GUI エディタを
// `-w` / `--wait` なしで指定すると即座に戻るので、この前提が破れる。git の GIT_EDITOR と同じ要求で、
// 直し方も同じ (EDITOR="code -w" のように待たせる)。
//
// 破れたときの劣化は「壊れる」ではなく「反映が遅れる」に留まる — issues viewer を開いている間は
// issues_watch.go が別プロセスの編集も拾うため (閉じている間は次に開いた時点で必ず読み直す)。
//
// 🚨 遅れは「気づくまで」+「確かめるまで」の 2 段でできている。handleWatch は変化を見つけた
// 1 回目では pending に置くだけで読まず (書きかけを読まないため)、次の観測で安定を確かめてから
// 反映する。2 回目の周期は issuesWatchVerifyPoll = 300ms なので、どの経路でも +300ms 乗る:
//   - fsnotify が動く: イベント (issuesWatchDebounce = 200ms でバーストを畳む) + 300ms ≒ 0.5s。
//     取りこぼしの保険が issuesWatchIdlePoll = 30s
//   - watcher を作れない (fsnotify.NewWatcher 失敗 / 死んで閉じた後): issuesWatchBlindPoll = 1s
//     が唯一の経路 → ≒ 1.3s
//   - watcher はあるがイベントが届かない (FS が無音 / 監視ディレクトリの Add が全滅): 保険の
//     30s が唯一の経路 ≒ 30.3s = 最悪ケース。🚨 Add 失敗でも watcher は nil にならないので
//     「生きているが聾」になる。ただし 30s の観測が変化を拾えば取り直し → startWatch が Add を
//     再試行するので、露出は永久ではなく「≤30s 聾、その後回復」
//
// 🚨 検出して警告する案は採らない。「子プロセスが早く終わった」の閾値がマジックナンバーになり、
// 正当に速いケース (既に開いているウィンドウへ渡すだけ) を誤検知する。
func EditorCommand(path string) *exec.Cmd {
	// 返した Cmd は RunEditorCmd (tea.Exec + foreground.Exec) が前景で起動する。ctx が無く、端末を継承する
	// ので os/exec はパイプも copy goroutine も作らない (Wait が孫に握られる形が起きない)。
	return editor.Command(path, nil) // subproc: no-waitdelay — 前景・ctx 無し・パイプ無し
}

// ReadonlyCommand は path を読み取り専用で開く nvim (issues viewer のジャンプモードの Enter)。
// 🚨 nvim 固定: 読み取り専用は vim 系の -R に依存する (openJobLogInEditor と同じ判断)。
// line>0 ならその行へ。
// 🚨 path の前に "--" を置く: 以降を必ずファイル名として読ませる (解決済みの絶対パスなので "-" / "+" で
// 始まることは無いが、引数の解釈を path の中身に依存させない)。
func ReadonlyCommand(path string, line int) *exec.Cmd {
	args := []string{"-R"}
	if line > 0 {
		args = append(args, "+"+strconv.Itoa(line))
	}
	args = append(args, "--", path)
	return exec.Command("nvim", args...) // subproc: no-waitdelay — 前景・ctx 無し・パイプ無し
}
