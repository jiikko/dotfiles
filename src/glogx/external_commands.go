package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"ratelimit/usage"

	"subproc"

	"glogx/internal/app"
	"glogx/internal/extcmd"
	"mac-lan-manager/lanmgr"
	"treefiler/filer"
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
//
// キーを押した操作なので、partial clone の手元に無い blob は取りに行く (subproc.GitArgsLazyFetch の doc)。
func noPromptGitCmd(ctx context.Context, args ...string) *exec.Cmd {
	// quit の cancel が kill するのは直接の子だけで、hook の孫が pipe を握ると Wait が
	// 戻らない (理由は subproc.WaitDelay の doc。subproc.CommandContext が張る)
	cmd := subproc.GitCommandLazyFetch(ctx, args...)
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
// 🚨 **sync.WaitGroup ではなく app.CleanupLatch を使う** (issue 217 で載せ替えた)。登録が
// tea.Cmd の closure 側なので、**カウント 0 での Add が Wait と同時に走りうる**形になっている。
// WaitGroup はそれを禁じており、`race.Enabled` に囲まれていない panic
// (`sync: WaitGroup misuse: Add called concurrently with Wait`) なので **production でも落ちうる**。
// 現状は登録地点が 1 か所で pull も `a.pulling` が二重起動を塞ぐため発火経路は未確認だったが、
// 同じ形の doctor 側 (doctorview の doctorCleanup) は登録地点が 3 つあり `-race` で実際に赤くなった。
// 「今は当たらない」に依存せず、構造で閉じる。
var pullCleanup app.CleanupLatch

// waitPullCleanup は走行中の pull 後始末を看取ってから戻る (main の終了直前用)。
// 待ちは runGitTimeout (gitOpTimeout) と WaitDelay で構造的に有限。すぐ終わらないときだけ
// 理由を出す (無言で固まったように見せない)。
func waitPullCleanup() {
	// wait() は channel を返すだけでブロックしないので、goroutine を挟まず同期に取る
	// (doctorview.WaitDoctorCleanup と同じ形。WaitGroup 時代は Wait() がブロックしたので goroutine が
	// 要ったが、載せ替えで不要になった。残すと latch を読む瞬間だけ非決定になる。issue 217)
	done := pullCleanup.Wait()
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
	// 途中状態の検査を作業ツリーの検査より先に回す。conflict で止まった rebase / merge は作業ツリーが汚れているので、
	// 逆の順だと「commit か stash して」と案内し、rebase の途中で commit を勧めてしまう (2026-10-02 の敵対的レビュー)
	op, opErr := operationInProgress()
	if opErr == nil && op != "" {
		return fmt.Errorf("%s の途中なので pull (--rebase) しません。シェルで続けるか中止して片付けてから u で再度 pull してください", op)
	}
	if st, stErr := noPromptGitCmd(ctx, "status", "--porcelain").Output(); stErr == nil && pullBlockedByDirtyTree(string(st)) {
		return errors.New("未コミットの変更があるため pull (--rebase) できません。commit か stash してから u で再度 pull してください")
	}
	// 🚨 ユーザーが自分で止めている rebase (`git rebase -i` の edit / break・conflict の途中) があるなら pull しない。
	// 作業ツリーは clean なので上の検査を通り、pull は失敗し、下の後始末が**ユーザーの rebase を abort して**進めていた
	// 成果を捨てていた (2026-10-02 の監査で前提を再現)。後始末の abort は「pull の前には rebase が無かった」と
	// 確かめられたときだけにする (確かめられないなら、その rebase が pull の起こしたものだと言えないので触らない)
	// rebase 以外の途中状態 (cherry-pick / revert / merge / 複数の pick の sequencer) も断る。空になった cherry-pick で止まった状態は
	// 作業ツリーが clean で、pull --rebase がその CHERRY_PICK_HEAD を消していた (2026-10-02 の敵対的レビュー)
	// 🚨 検査は fetch の前の 1 回だけ。fetch の最中 (ネットワークの数秒) に別の端末で rebase を始めると、その rebase を
	// pull のものと区別できず、pull が conflict すると後始末が abort しうる (2026-10-02 の敵対的レビューで指摘。同じ repo で
	// u を押した数秒のうちに手で rebase を始めた場合だけで、記録に留める)
	out, err := noPromptGitCmd(ctx, "pull", "--rebase").CombinedOutput()
	if err == nil {
		return nil
	}
	// 🚨 以降の rev-parse / abortRebase は pull の ctx を使わない (runGitTimeout の独立 timeout):
	// quit で pull が cancel された場合こそ rebase 途中状態の後始末が要るのに、cancel 済み ctx を
	// 渡すと abort が実行されないまま repo に rebase-merge が残る。独立 timeout なら quit 後も
	// 後始末が走り、かつハング (.git ロック競合等) しても有限で終わる。
	// opErr の条件が効くのは、pull の前の rev-parse だけが一時的に失敗した (ロック競合・時間切れ) ときだけで、
	// テストでは作れない (外す変異は緑のまま。2026-10-02)。前が分からないなら、その rebase を pull のものと言えない
	if opErr == nil { // ここに来るのは「pull の前は何の途中でもない」と確かめられたときだけ
		if after, afterErr := rebaseInProgress(); afterErr == nil && after {
			return abortRebase(out)
		}
	}
	return errors.New(strings.TrimSpace(string(out)))
}

// rebaseInProgress は今の repo で rebase が途中で止まっているか (git dir の下に rebase-merge / rebase-apply があるか)。
// git dir を引けないときは err を返す (呼び出し側は「分からない」を「途中ではない」に丸めない)。
func rebaseInProgress() (bool, error) {
	op, err := gitDirMarker("rebase-merge", "rebase-apply")
	return op != "", err
}

// operationInProgress は途中で止まっている git の操作の名前を返す (無ければ "")。pull の前に断る判定に使う。
func operationInProgress() (string, error) {
	names := map[string]string{
		"rebase-merge": "rebase", "rebase-apply": "rebase / am", "MERGE_HEAD": "merge",
		"CHERRY_PICK_HEAD": "cherry-pick", "REVERT_HEAD": "revert", "sequencer": "cherry-pick / revert",
		"BISECT_LOG": "bisect", // branch の上で bisect を始めた直後は pull が branch を動かす
	}
	marker, err := gitDirMarker("rebase-merge", "rebase-apply", "MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "sequencer", "BISECT_LOG")
	return names[marker], err
}

// gitDirMarker は git dir の下にある最初の印 (ファイルかディレクトリ) の名前を返す (無ければ "")。
func gitDirMarker(markers ...string) (string, error) {
	gitDir, err := extcmd.RunGitTimeout("rev-parse", "--git-dir")
	if err != nil {
		return "", err
	}
	dir := strings.TrimSpace(gitDir)
	for _, name := range markers {
		if _, statErr := os.Stat(dir + "/" + name); statErr == nil {
			return name, nil
		}
	}
	return "", nil
}

// abortRebase は途中停止した rebase を中断し、結果に応じたメッセージを返す。
// abort 自体が失敗したら「元に戻した」とは主張せず (壊れた状態が残っている可能性がある)、
// 手動復旧を促す。out は pull --rebase の出力 (conflict 内容の提示用)。
func abortRebase(out []byte) error {
	conflict := app.FirstLine(strings.TrimSpace(string(out)))
	if _, err := extcmd.RunGitTimeout("rebase", "--abort"); err != nil {
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

// localCmdTimeout は tmux などローカルの補助コマンドの実行上限 (クリップボードは subproc/clipboard の Timeout)。
// ハングしても機能が 1 つ欠ける (prefix ガード無効など) だけで済む操作なので、
// gitOpTimeout (30s) より短く切る。tea.Cmd の goroutine や Update の同期経路から呼ばれる
// ため、上限が無いと応答しないサーバ (tmux デッドロック) で goroutine と
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

// filerExecCommand は treefiler の ! / s / e が頼んだプロセス (選んだフォルダで、$f に選んだパスを入れて起こす)。
// 返した Cmd は extcmd.RunEditorCmd (tea.Exec + foreground.Exec) が前景で起動する。免除の理由は extcmd.EditorCommand と同じ
// (前景の対話プロセスで ctx が無く、端末を継承するのでパイプも copy goroutine も作らない)。
func filerExecCommand(r filer.ExecRequest) *exec.Cmd {
	cmd := exec.Command(r.Argv[0], r.Argv[1:]...) // subproc: no-waitdelay — 前景・ctx 無し・パイプ無し
	cmd.Dir = r.Dir
	cmd.Env = r.Env
	return cmd
}

// lanExecCommand は mac-lan-manager の s (ssh) / o (画面共有) が頼んだプロセス。返した Cmd は extcmd.RunEditorCmd (tea.Exec + foreground.Exec) が
// 前景で起動する。免除の理由は extcmd.EditorCommand と同じ (ssh はパスワードとホスト鍵の確認に端末が要る。ctx が無く、端末を継承するのでパイプも
// copy goroutine も作らない)。
func lanExecCommand(r lanmgr.ExecRequest) *exec.Cmd {
	argv := r.RunArgv()                       // 初期設定の手順は、端末を渡す前に板を出す
	return exec.Command(argv[0], argv[1:]...) // subproc: no-waitdelay — 前景・ctx 無し・パイプ無し
}
