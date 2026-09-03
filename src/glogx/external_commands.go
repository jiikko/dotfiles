package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"glogx/usage"

	"glogx/subproc"
)

// 外部プロセス (git / tmux / claude / ブラウザ / クリップボード) を叩くラッパー群。
// browseModel の状態には一切触れない (結合ゼロ) ため、Bubble Tea の状態機械本体
// (tui.go) から分離している。多くは `var f = func(...)` の形でテストの差し替え点になる。

// noPromptGitCmd は remote に触る git (push/pull) 用のコマンドを組む。GIT_TERMINAL_PROMPT=0
// で「認証情報が要るのに helper が無い」場合に /dev/tty へ対話プロンプトを出させず即エラーに
// する: bubbletea が同じ端末を raw mode で握っているため、git が tty を奪うと表示が壊れ入力
// 挙動が未定義になる (対話認証は TUI の外でやるべき作業)。
//
// 🚨 ctx には deadline を付けない (レビュー K2: 正当な巨大 push が遅い回線で timeout 中断される
// 方が push 失敗として有害)。ただし cancel は張る: quit (Ctrl-C) 時に走行中の push/pull を
// cancel できないと、ネットワーク stall 中に抜けたとき git 子プロセスが孤児化して事実上無期限に
// 居残る (leak 監査 2026-07-23)。呼び出し側 (actionModal) が deadline 無しの cancel context を
// 渡し、quit からのみ cancel する。
func noPromptGitCmd(ctx context.Context, args ...string) *exec.Cmd {
	// quit の cancel が kill するのは直接の子だけで、hook の孫が pipe を握ると Wait が
	// 戻らない (理由は subproc.WaitDelay の doc。subproc.CommandContext が張る)
	cmd := subproc.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	return cmd
}

// runGitPush はテストで実 push しないための差し替え点。ctx は quit 中断用 (deadline 無し)。
var runGitPush = func(ctx context.Context) error {
	out, err := noPromptGitCmd(ctx, "push").CombinedOutput()
	if err != nil {
		return errors.New(strings.TrimSpace(string(out)))
	}
	return nil
}

// pullCleanup は走行中の pull (と conflict 時の rebase --abort 後始末) の完了 latch。
// bubbletea は tea.Cmd の goroutine を待たずに Run を抜けるため、quit で pull が cancel
// された直後に main が os.Exit すると、「quit で cancel された場合こそ後始末が要る」はずの
// abort (runGitPullRebase 内) が走り切る前にプロセスごと消え、repo に rebase-merge が残る。
// 登録は tea.Cmd の closure 側 (actionModal.handleKey)、看取りは main.go の waitPullCleanup。
//
// 🚨 **sync.WaitGroup ではなく cleanupLatch を使う** (issue 217 で載せ替えた)。登録が
// tea.Cmd の closure 側なので、**カウント 0 での Add が Wait と同時に走りうる**形になっている。
// WaitGroup はそれを禁じており、`race.Enabled` に囲まれていない panic
// (`sync: WaitGroup misuse: Add called concurrently with Wait`) なので **production でも落ちうる**。
// 現状は登録地点が 1 か所で pull も `a.pulling` が二重起動を塞ぐため発火経路は未確認だったが、
// 同じ形の doctor 側 (doctorCleanup) は登録地点が 3 つあり `-race` で実際に赤くなった。
// 「今は当たらない」に依存せず、構造で閉じる。
var pullCleanup cleanupLatch

// waitPullCleanup は走行中の pull 後始末を看取ってから戻る (main の終了直前用)。
// 待ちは runGitTimeout (gitOpTimeout) と WaitDelay で構造的に有限。すぐ終わらないときだけ
// 理由を出す (無言で固まったように見せない)。
func waitPullCleanup() {
	done := make(chan struct{})
	go func() { <-pullCleanup.wait(); close(done) }()
	select {
	case <-done:
		return
	case <-time.After(200 * time.Millisecond):
		fmt.Fprintln(os.Stderr, "glogx: pull の後始末 (git rebase --abort) を待っています...")
	}
	<-done
}

// runGitPullRebase はテストで実 pull しないための差し替え点。conflict で rebase が
// 途中停止したら自動で abort して pull 前の状態へ戻す (TUI 内に「rebase 進行中」の
// 壊れた状態を残さない。解決が必要な conflict はシェルでやるべき作業)。
//
// git pull --rebase は tracked の未コミット変更 (staged/unstaged) があると
// "cannot pull with rebase: You have unstaged changes" で拒否する (untracked は無害)。
// 素の git エラーは分かりにくいので、事前に検知して glogx らしい案内を返す (自動 stash は
// しない: 復元 pop の衝突で working tree に壊れた状態を残しうるため。ユーザー選定 2026-07-22)。
var runGitPullRebase = func(ctx context.Context) error {
	if st, stErr := noPromptGitCmd(ctx, "status", "--porcelain").Output(); stErr == nil && pullBlockedByDirtyTree(string(st)) {
		return errors.New("未コミットの変更があるため pull (--rebase) できません。commit か stash してから u で再度 pull してください")
	}
	out, err := noPromptGitCmd(ctx, "pull", "--rebase").CombinedOutput()
	if err == nil {
		return nil
	}
	// 🚨 以降の rev-parse / abortRebase は pull の ctx を使わない (runGitTimeout の独立 timeout):
	// quit で pull が cancel された場合こそ rebase 途中状態の後始末が要るのに、cancel 済み ctx を
	// 渡すと abort が実行されないまま repo に rebase-merge が残る。独立 timeout なら quit 後も
	// 後始末が走り、かつハング (.git ロック競合等) しても有限で終わる。
	gitDir, dirErr := runGitTimeout("rev-parse", "--git-dir")
	if dirErr == nil {
		dir := strings.TrimSpace(gitDir)
		if _, statErr := os.Stat(dir + "/rebase-merge"); statErr == nil {
			return abortRebase(out)
		}
		if _, statErr := os.Stat(dir + "/rebase-apply"); statErr == nil {
			return abortRebase(out)
		}
	}
	return errors.New(strings.TrimSpace(string(out)))
}

// abortRebase は途中停止した rebase を中断し、結果に応じたメッセージを返す。
// abort 自体が失敗したら「元に戻した」とは主張せず (壊れた状態が残っている可能性がある)、
// 手動復旧を促す。out は pull --rebase の出力 (conflict 内容の提示用)。
func abortRebase(out []byte) error {
	conflict := firstLine(strings.TrimSpace(string(out)))
	if _, err := runGitTimeout("rebase", "--abort"); err != nil {
		return fmt.Errorf("conflict のため rebase 中断を試みましたが失敗しました。手動で `git rebase --abort` してください: %s", conflict)
	}
	return fmt.Errorf("conflict のため rebase を中断して元に戻しました: %s", conflict)
}

// pullBlockedByDirtyTree は git status --porcelain の出力に rebase を阻む tracked 変更
// (staged / unstaged) が含まれるかを返す純関数。untracked (先頭 "??") は rebase を阻まない
// ため無視する。git status は内部で index を refresh するので stat-dirty の偽陽性は出ない。
func pullBlockedByDirtyTree(porcelain string) bool {
	for _, line := range strings.Split(porcelain, "\n") {
		if line == "" || strings.HasPrefix(line, "??") {
			continue
		}
		return true
	}
	return false
}

// status viewer (s キー) の write 操作。いずれもローカルの index / 作業ツリーだけを触るので
// noPromptGitCmd (remote 用) ではなく runGitTimeout を使う。テストで実 git を叩かないための
// 差し替え点として var にしてある。
//
// 🚨 呼び出しは status viewer のキー処理から同期で行う (issues viewer の MoveToSubdir と同じ作法)。
// index への 1 回の書き込みは十分速く、非同期にすると「実行中に外部編集が入る」窓が開いて
// 「確認に出した状態と実行時の状態が一致すること」(docs/status-viewer-spec.md 4 節) を守りにくい。
var (
	// runGitAdd は stage する (untracked の追加・削除の記録もこれで足りる)。
	runGitAdd = func(paths []string) error {
		_, err := runGitTimeout(append([]string{"add", "--"}, paths...)...)
		return err
	}
	// runGitRestoreStaged は index から降ろす (作業ツリーは触らない)。
	runGitRestoreStaged = func(paths []string) error {
		_, err := runGitTimeout(append([]string{"restore", "--staged", "--"}, paths...)...)
		return err
	}
	// runGitRestoreWorktree は作業ツリーの変更を捨てる (index の内容へ戻す)。復元手段は無い。
	runGitRestoreWorktree = func(paths []string) error {
		_, err := runGitTimeout(append([]string{"restore", "--"}, paths...)...)
		return err
	}
	// runGitCleanUntracked は untracked を削除する。-d はディレクトリ行 ("dir/" に畳まれた
	// エントリ) のために必要で、-f は clean の既定が「何もしない」ため必要。
	runGitCleanUntracked = func(paths []string) error {
		_, err := runGitTimeout(append([]string{"clean", "-fd", "--"}, paths...)...)
		return err
	}
)

// updateTimeout は claude update の上限。通常の自己更新 (npm/ダウンロード) はこれより十分速く
// 完了するため、到達したら更新が本当にハングしている合図。この上限が無いと updating 中は
// q/Ctrl-C を握りつぶす (handleKey の updating ガード) 設計上、無限ハング時に端末を外部から
// kill するしか脱出できず、子プロセスが孤児化し raw mode も残る。寛大な値で「更新中は中断させ
// ない」意図を保ちつつ、病的なハングだけを断ち切る。
const updateTimeout = 5 * time.Minute

// runCLIUpdate は CLI 自己更新の共通実装 (claude / codex で 1 行単位に同一だった鏡像 2 本を
// 一本化。差はコマンド名とバージョン取得関数だけ)。update 前後の CLI バージョンを挟んで取得し
// (何→何に変わったか表示するため)、CLI を自己更新する。remote に触るが git ではないので
// noPromptGitCmd は使わない (対話プロンプトは CLI 側の責務)。updateTimeout で context を張り、
// 無期限ブロックを防ぐ (超過時は updateMsg{err} 経由で updating が必ず解ける)。
func runCLIUpdate(name string, fetchVersion func(context.Context) string) (before, after, note string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()
	before = fetchVersion(ctx)
	// 🚨 WaitDelay が必須 (subproc.CommandContext が張る)。ctx の deadline が kill するのは直接の
	// 子だけで、子が残した孫が親のパイプを握っていると CombinedOutput は孫が閉じるまで戻らない
	// (理由は subproc.WaitDelay の doc)。戻らないと updateMsg が発行されず
	// actModal.updating が立ったままになり、上の updateTimeout が約束している
	// 「超過時は必ず解ける」が成立しない = updating 中は q も Ctrl-C も握り潰す設計なので
	// TUI から二度と抜けられなくなる。
	cmd := subproc.CommandContext(ctx, name, "update")
	out, e := cmd.CombinedOutput()
	if e != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return before, "", "", fmt.Errorf("%s update がタイムアウトしました (%s)", name, updateTimeout)
		}
		return before, "", "", errors.New(lastLine(strings.TrimSpace(string(out))))
	}
	after = fetchVersion(ctx)
	// 成功時も出力の末尾行を返す。🚨 捨てないこと: CLI は「更新しなかった」ときも exit 0 で
	// 成功するため (codex は自前の stale なキャッシュを見て "Codex is already up to date." を
	// 出して終わる。実測 2026-09-03 / ~/.codex/version.json が 44 日前で止まっていた)、
	// before == after だけでは「最新だった」のか「CLI が更新をサボった」のかを区別できない。
	// この行が唯一その理由を持っている。
	return before, after, lastLine(strings.TrimSpace(string(out))), nil
}

// runClaudeUpdate はテストで実 update しないための差し替え点。
var runClaudeUpdate = func() (before, after, note string, err error) {
	return runCLIUpdate("claude", usage.FetchVersion)
}

// runCodexUpdate はテストで実 update しないための差し替え点 (runClaudeUpdate の codex 版)。
// `codex update` は codex CLI の自己更新サブコマンド (0.144 で実在確認 2026-08-09)。
var runCodexUpdate = func() (before, after, note string, err error) {
	return runCLIUpdate("codex", fetchInstalledCodexVersion)
}

// runJobRerun はテストで実 rerun しないための差し替え点 (本体は jobRerun)。
var runJobRerun = func(ctx context.Context, repo Repo, jobID int64) error {
	return jobRerun(ctx, ExecRunner, repo, jobID)
}

// jobRerun は失敗 job を GitHub Actions 上で再実行する (`gh run rerun --job <id>`)。
// run 全体でなく job 単位なのは、パネルのフォーカス単位が job であり run id を
// 保持していないため (issue 019)。認証は gh へ委譲。
func jobRerun(ctx context.Context, run CommandRunner, repo Repo, jobID int64) error {
	_, stderr, err := run(ctx, "gh", "run", "rerun",
		"--job", strconv.FormatInt(jobID, 10), "-R", repo.Owner+"/"+repo.Name)
	if err != nil {
		detail := lastLine(string(stderr))
		if detail == "" {
			detail = err.Error()
		}
		return errors.New(detail)
	}
	return nil
}

// localCmdTimeout は tmux / クリップボードなどローカルの補助コマンドの実行上限。
// ハングしても機能が 1 つ欠ける (prefix ガード無効・コピー失敗) だけで済む操作なので、
// gitOpTimeout (30s) より短く切る。tea.Cmd の goroutine や Update の同期経路から呼ばれる
// ため、上限が無いと応答しないサーバ (tmux デッドロック・X 無応答) で goroutine と
// 子プロセスが glogx 終了まで残る (issue 029 P2/P3 と同じ規律)。
const localCmdTimeout = 5 * time.Second

// loadTmuxPrefix は tmux サーバの現在の prefix を bubbletea キー表記で返す
// ("" = tmux 外 / 取得失敗 / 未対応表記)。tmux.conf のパースはしない: conf は分割・
// ライブ変更されうるため、サーバの現在値だけが真実 (show-options で聞く)。
var loadTmuxPrefix = func() string {
	if os.Getenv("TMUX") == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), localCmdTimeout)
	defer cancel()
	// ctx の kill は直接の子にしか効かず、tmux サーバ側が I/O fd を握ると Wait が戻らない
	// (理由は subproc.WaitDelay の doc。以降のクリップボード系も同じ)
	cmd := subproc.CommandContext(ctx, "tmux", "show-options", "-g", "prefix")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return parseTmuxPrefix(strings.TrimSpace(string(out)))
}

// parseTmuxPrefix は `prefix C-t` 形式の出力を bubbletea 表記 ("ctrl+t") へ変換する。
// C-<英字> 以外 (M- 系や None 等) は誤爆判定できないので "" (機能オフ) に落とす。
func parseTmuxPrefix(out string) string {
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return ""
	}
	p := fields[1]
	if len(p) == 3 && strings.HasPrefix(p, "C-") {
		return "ctrl+" + strings.ToLower(p[2:])
	}
	return ""
}

// openInBrowser はテストで実ブラウザを開かないための差し替え点。
var openInBrowser = func(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Run() // open は即 detach するので timeout 不要
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

// copyToClipboard はテストで実クリップボードを触らないための差し替え点。
// OS のクリップボードコマンド (pbcopy/xclip) を真実とし、tmux 内では tmux バッファへも
// 積む (tmux paste 用のおまけ・best effort)。本家 glog は load-buffer -w の成功 (exit 0)
// を「システム側にも届いた」とみなすが、-w の実体は OSC52 転送で、外側端末が OSC52 を
// 解釈しなければ exit 0 のままクリップボードに入らない (glogx で実測 2026-07-19)。
// Update ハンドラから同期で呼ばれるため localCmdTimeout で区切る: xclip は X サーバ無応答で
// 戻らないことがある既知挙動で、上限が無いと TUI ごと固まったうえ強制終了後に子プロセスが残る
// (xdg-open へ issue 029 P3 で入れたのと同じ手当て)。
var copyToClipboard = func(text string) error {
	if os.Getenv("TMUX") != "" {
		// おまけ側は timeout を本命と分ける (tmux のハングが OS クリップボードを巻き添えにしない)
		tmuxCtx, tmuxCancel := context.WithTimeout(context.Background(), localCmdTimeout)
		// WaitDelay (subproc.CommandContext が張る) が無いと ctx の kill 後も stdin の copy goroutine
		// の write ブロックを Wait が待ち続け、timeout が効かない (tmux は stdin fd をサーバへ渡すため、
		// payload がパイプバッファを超えるとハング中のサーバが read 側を握ったままになる)
		cmd := subproc.CommandContext(tmuxCtx, "tmux", "load-buffer", "-w", "-")
		cmd.Stdin = strings.NewReader(text)
		_ = cmd.Run() // 失敗しても OS クリップボードが本命なので無視
		tmuxCancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), localCmdTimeout)
	defer cancel()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = subproc.CommandContext(ctx, "pbcopy")
	default:
		cmd = subproc.CommandContext(ctx, "xclip", "-selection", "clipboard")
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

// editorFallback は $VISUAL / $EDITOR がどちらも空のときに使うエディタ。
const editorFallback = "nvim"

// editorCommand は実ファイルを開くエディタのコマンドを組む。$VISUAL → $EDITOR → nvim の順に
// 見る (VISUAL を先に見るのは「全画面エディタは VISUAL」という POSIX の慣習)。
//
// 🚨 値は空白で語分割する。EDITOR="code -w" のように引数つきの指定が慣習的に使われるため、
// 文字列全体を実行ファイル名として扱うと起動できない。分割は quote を解釈しない意図的な
// 単純化で、**空白を含むパスの指定 (EDITOR='"/My Apps/ed" -w') は非対応** — 起動に失敗し、
// tui.go の editorClosedMsg がトーストで理由を出す。quote まで解釈するなら git の GIT_EDITOR と
// 同じく sh -c へ渡す形になり (クォートするのは git と同様ユーザー側なので glogx の責任は増えない)、
// 採らないのはシェルの解釈をエディタ起動に持ち込まないため — $EDITOR の値が展開・グロブ・
// コマンド置換を通る経路を作らない方を選ぶ。
//
// 🚨 この経路は「実ファイルを 1 つ開く」ものにだけ使う。nvim を直に呼んでいる他の 2 箇所
// (tui.go の job ログ = 標準入力 + -c の scratch バッファ / open_workspace.go の `nvim .` =
// ディレクトリを開く) は nvim 固有の機能に依存しており、任意の $EDITOR では成立しない。
//
// 🚨 **前提: 起動したプロセスの終了 = 編集の完了**。glogx は tea.ExecProcess で TUI を中断して
// 子プロセスを待ち、戻った境界で一覧と本文を取り直す (tui.go の editorClosedMsg)。GUI エディタを
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
func editorCommand(path string) *exec.Cmd {
	editor := firstNonEmptyEnv("VISUAL", "EDITOR")
	fields := strings.Fields(editor)
	if len(fields) == 0 { // 未設定・空白だけなら fallback
		return exec.Command(editorFallback, path)
	}
	args := make([]string, 0, len(fields))
	args = append(args, fields[1:]...) // EDITOR に含まれていた引数 (例: code -w)
	args = append(args, path)
	return exec.Command(fields[0], args...)
}

// firstNonEmptyEnv は最初に空でない値を持つ環境変数の値を返す。
func firstNonEmptyEnv(names ...string) string {
	for _, name := range names {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
	}
	return ""
}
