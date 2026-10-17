package extcmd

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"subproc"
)

// GitExitError は git コマンド自体の失敗。stderr と終了コードをそのまま伝播する。
type GitExitError struct {
	Stderr string
	Code   int
}

func (e *GitExitError) Error() string { return e.Stderr }

// gitOpTimeout は subproc.GitOpTimeout の別名 (値と理由の正本はそちら)。issues パッケージも
// 同じ上限で git を起動するため、値は共有の leaf パッケージに置いてある。
const gitOpTimeout = subproc.GitOpTimeout

// runGitTimeout は runGit の timeout 付き版。TUI の tea.Cmd (goroutine) から呼ぶ git は
// こちらを使う: ハングしても q で抜けた後に子プロセスが残らない。
func RunGitTimeout(args ...string) (string, error) { //nolint:unparam // 出力は main (gitlog.go / gitlog_watch.go) が使う。unparam はパッケージの外の呼び出しを見ない
	ctx, cancel := context.WithTimeout(context.Background(), gitOpTimeout)
	defer cancel()
	return RunGitCmd(exec.CommandContext(ctx, "git", subproc.GitArgs(args...)...))
}

func RunGitCmd(cmd *exec.Cmd) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// ctx の kill は直接の子にしか効かず、hook の孫が pipe を握ると Wait が戻らない
	// (理由は subproc.WaitDelay の doc)。runGit (ctx 無し) と runGitTimeout の両方が通るので、
	// subproc.CommandContext ではなくここで 1 度張る
	cmd.WaitDelay = subproc.WaitDelay
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		code := 1
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		}
		return "", &GitExitError{Stderr: stderr.String(), Code: code}
	}
	return stdout.String(), nil
}

// runGitReadTimeout は runGitTimeout と同じだが、表示のために作業ツリーを読む呼び出し (status・作業ツリーとの diff) 用。
// cwd の repo のローカルの設定の filter (clean / process) を止める (issue 732。subproc.GitRepoArgs の doc)。add など書き込む操作には使わない。
func RunGitReadTimeout(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitOpTimeout)
	defer cancel()
	cmd, err := subproc.GitRepoCommand(ctx, "", args...)
	if err != nil {
		return "", err
	}
	return RunGitCmd(cmd)
}

// maxDiffLines は diff ポップアップに保持する行数の上限。巨大コミット (自動生成物等) で
// 描画・保持行数が際限なく増えないための安全弁で、超過時は末尾に省略注記を足す。
// 🚨 これがバウンドするのは「保持する行数」だけで、ピークメモリは制限しない: runGit は
// git show の全出力を bytes.Buffer + String() で一旦フルに展開する (diff 全長 × 約2) ため、
// 数万行 diff の一時スパイクは起きる (popup クローズ後に GC 解放される一時的なもの)。
// メモリも真にバウンドしたければ git 出力を行ストリームで読み上限で打ち切る必要がある。
const MaxDiffLines = 5000

// status viewer (s キー) の write 操作。いずれもローカルの index / 作業ツリーだけを触るので
// noPromptGitCmd (remote 用) ではなく runGitTimeout を使う。テストで実 git を叩かないための
// 差し替え点として var にしてある。
//
// 🚨 呼び出しは status viewer のキー処理から同期で行う (issues viewer の MoveToSubdir と同じ作法)。
// index への 1 回の書き込みは十分速く、非同期にすると「実行中に外部編集が入る」窓が開いて
// 「確認に出した状態と実行時の状態が一致すること」(docs/status-viewer-spec.md 4 節) を守りにくい。
var (
	// runGitAdd は stage する (untracked の追加・削除の記録もこれで足りる)。
	RunGitAdd = func(paths []string) error {
		_, err := RunGitTimeout(append([]string{"add", "--"}, paths...)...)
		return err
	}
	// runGitRestoreStaged は index から降ろす (作業ツリーは触らない)。
	RunGitRestoreStaged = func(paths []string) error {
		_, err := RunGitTimeout(append([]string{"restore", "--staged", "--"}, paths...)...)
		return err
	}
	// runGitRestoreWorktree は作業ツリーの変更を捨てる (index の内容へ戻す)。復元手段は無い。
	RunGitRestoreWorktree = func(paths []string) error {
		_, err := RunGitTimeout(append([]string{"restore", "--"}, paths...)...)
		return err
	}
	// runGitCleanUntracked は untracked を削除する。-d はディレクトリ行 ("dir/" に畳まれた
	// エントリ) のために必要で、-f は clean の既定が「何もしない」ため必要。
	RunGitCleanUntracked = func(paths []string) error {
		_, err := RunGitTimeout(append([]string{"clean", "-fd", "--"}, paths...)...)
		return err
	}
)
