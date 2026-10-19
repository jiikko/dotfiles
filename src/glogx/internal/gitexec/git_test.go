package gitexec_test

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	"glogx/internal/gitexec"
)

// git が起動できないとき (PATH に無い) は stderr が空なので、理由を起動の失敗から出す (空のトーストや無言の rc=1 にしない)。
func TestRunGitCmdKeepsTheReasonWhenGitCannotStart(t *testing.T) {
	_, err := gitexec.RunGitCmd(exec.Command("/nonexistent/git", "status")) // subproc: no-waitdelay — 起動に失敗するので子プロセスは作られない
	if err == nil {
		t.Fatal("起動できない git が成功した")
	}
	if strings.TrimSpace(err.Error()) == "" || !strings.Contains(err.Error(), "/nonexistent/git") {
		t.Errorf("理由が空 / 起動の失敗を言っていない: %q", err.Error())
	}
}

// git が stderr に理由を書いたときは、その理由をそのまま返す (起動の失敗のエラーに置き換えない)。
func TestGitExitErrorPrefersStderr(t *testing.T) {
	err := &gitexec.GitExitError{Stderr: "fatal: not a git repository\n", Code: 128, Err: exec.ErrNotFound}
	if err.Error() != "fatal: not a git repository\n" {
		t.Errorf("Error() = %q", err.Error())
	}
}

// 時間切れは stderr に警告が出ていても先に出す (警告だけが理由に見えると、時間切れに気づけない)。
func TestGitExitErrorTimedOutComesFirst(t *testing.T) {
	err := &gitexec.GitExitError{Stderr: "warning: cache stale\n", Code: -1, TimedOut: true,
		Err: errors.New("git status が 10s 以内に終わりませんでした")}
	if got := err.Error(); !strings.HasPrefix(got, "git status が 10s 以内に終わりませんでした") || !strings.Contains(got, "warning: cache stale") {
		t.Errorf("Error() = %q", got)
	}
}
