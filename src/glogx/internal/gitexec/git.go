// Package gitexec は glogx の git の実行の共通入口 (時間の上限・失敗の理由・status viewer の write 操作の差し替え点)。
// bubbletea を持たない末端の package (データ層の gitdata が使う)。
package gitexec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"subproc"
)

// GitExitError は git コマンド自体の失敗。stderr と終了コードをそのまま伝播する。
// Err は起動の失敗・時間切れなど、git が理由を stderr に書けなかったときの原因 (無ければ nil)。
// 🚨 Error() を stderr だけにしない: git が起動できない (PATH に無い) / 時間切れで殺された ときは stderr が空で、
// 「git status に失敗しました: 」の後ろが空のトーストや、何も出さない rc=1 の終了になる。
// TimedOut は上限の時間に達して殺された (Err がその説明)。stderr に警告が出ていても、時間切れを先に出す。
type GitExitError struct {
	Stderr   string
	Code     int
	Err      error
	TimedOut bool
}

func (e *GitExitError) Error() string {
	if e.TimedOut && e.Err != nil {
		if s := strings.TrimSpace(e.Stderr); s != "" {
			return e.Err.Error() + "\n" + s
		}
		return e.Err.Error()
	}
	if s := strings.TrimSpace(e.Stderr); s != "" || e.Err == nil {
		return e.Stderr
	}
	return e.Err.Error()
}

func (e *GitExitError) Unwrap() error { return e.Err }

// gitOpTimeout は subproc.GitOpTimeout の別名 (値と理由の正本はそちら)。issues パッケージも
// 同じ上限で git を起動するため、値は共有の leaf パッケージに置いてある。
const gitOpTimeout = subproc.GitOpTimeout

// runGitTimeout は runGit の timeout 付き版。TUI の tea.Cmd (goroutine) から呼ぶ git は
// こちらを使う: ハングしても q で抜けた後に子プロセスが残らない。
func RunGitTimeout(args ...string) (string, error) { //nolint:unparam // 出力は main (gitlog.go / gitlog_watch.go) が使う。unparam はパッケージの外の呼び出しを見ない
	ctx, cancel := context.WithTimeout(context.Background(), gitOpTimeout)
	defer cancel()
	out, err := RunGitCmd(exec.CommandContext(ctx, "git", subproc.GitArgs(args...)...))
	return out, timeoutReason(ctx, err, args)
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
		return "", &GitExitError{Stderr: stderr.String(), Code: code, Err: err}
	}
	return stdout.String(), nil
}

// timeoutReason は ctx の時間切れで殺された git の失敗に、時間切れだと分かる原因を付ける。
// 🚨 exec.ExitError かどうかでは分けられない: 時間切れの SIGKILL も ExitError (Code=-1) になる。ctx.Err() で見る
func timeoutReason(ctx context.Context, err error, args []string) error {
	var ge *GitExitError
	if err == nil || !errors.Is(ctx.Err(), context.DeadlineExceeded) || !errors.As(err, &ge) {
		return err
	}
	name := "git"
	if len(args) > 0 {
		name += " " + args[0]
	}
	ge.Err = fmt.Errorf("%s が %s 以内に終わりませんでした: %w", name, gitOpTimeout, ctx.Err())
	ge.TimedOut = true
	return ge
}

// runGitReadTimeout は runGitTimeout と同じだが、表示のために作業ツリーを読む呼び出し (status・作業ツリーとの diff) 用。
// cwd の repo のローカルの設定の filter (clean / process) を止める (subproc.GitRepoArgs の doc)。add など書き込む操作には使わない。
func RunGitReadTimeout(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitOpTimeout)
	defer cancel()
	cmd, err := subproc.GitRepoCommand(ctx, "", args...)
	if err != nil {
		// 前処理 (filter を列挙する git config) の時間切れも、時間切れだと分かる形にする (下の timeoutReason は
		// GitExitError にしか付けられない)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", &GitExitError{Code: -1, TimedOut: true,
				Err: fmt.Errorf("git の設定を %s 以内に読めませんでした: %w", gitOpTimeout, errors.Join(ctx.Err(), err))}
		}
		return "", err
	}
	out, err := RunGitCmd(cmd)
	return out, timeoutReason(ctx, err, args)
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
