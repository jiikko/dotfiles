package gitexec

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// 時間切れで殺された git は stderr が空で ExitError (signal) になるので、ctx を見て時間切れだと言う。
func TestTimeoutReasonNamesTheTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sleep", "5") // subproc: no-waitdelay — RunGitCmd が張る。ctx の時間切れで殺される
	_, err := RunGitCmd(cmd)
	err = timeoutReason(ctx, err, []string{"status"})
	if err == nil || !strings.Contains(err.Error(), "git status が") || !strings.Contains(err.Error(), "以内に終わりませんでした") {
		t.Fatalf("時間切れを言っていない: %v", err)
	}
	// 時間切れでない失敗には付けない
	if got := timeoutReason(context.Background(), &GitExitError{Stderr: "boom", Code: 1}, []string{"status"}); got.Error() != "boom" {
		t.Errorf("時間切れでない失敗の理由を変えた: %q", got.Error())
	}
}
