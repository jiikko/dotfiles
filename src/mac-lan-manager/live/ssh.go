package live

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"mac-lan-manager/setup"
	"subproc"
)

// ObserveTimeout は相手を 1 台読む全体の上限 (鍵の確認と netinfo.Read の ssh を合わせて)。ssh は 1 本ごとに ConnectTimeout=5 を持つが、
// 繋がった後に相手が黙ると止まらないので、全体にも上限を置く。
const ObserveTimeout = 15 * time.Second

// Observe は本物の ssh で相手を読む (lanmgr.Options.Observe と dump --peer の口)。読むだけで、設定を変えるコマンドは打たない。
func Observe(ctx context.Context, d setup.Dest) setup.PeerNet {
	ctx, cancel := context.WithTimeout(ctx, ObserveTimeout)
	defer cancel()
	return setup.ObservePeer(ctx, SSHRun, d)
}

// SSHRun は setup.Run の本物。argv を起こして stdout を返す。stdin は /dev/null (Stdin を張らない。BatchMode の ssh が
// パスワードを聞かずに落ちる)。失敗したときの err は *RunError で、終了コードと stderr の文を持つ (setup.ObservePeer は stderr の
// Permission denied で「鍵で入れない」と「届かない」を分ける)。
func SSHRun(ctx context.Context, argv []string) (string, error) {
	if len(argv) == 0 {
		return "", errors.New("起こすコマンドが無い")
	}
	out, err := subproc.CommandContext(ctx, argv[0], argv[1:]...).Output()
	if err != nil {
		re := &RunError{Err: err}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			re.Stderr = strings.TrimSpace(string(ee.Stderr)) // Output は Stderr を張っていなければ ExitError に stderr を残す
		}
		return string(out), re
	}
	return string(out), nil
}

// RunError は SSHRun の失敗。ExitCode は終了して返ったときの終了コード (起こせなかった・signal で止まったときは -1)。
type RunError struct {
	Err    error
	Stderr string
}

func (e *RunError) Error() string {
	if e.Stderr == "" {
		return e.Err.Error()
	}
	return fmt.Sprintf("%v: %s", e.Err, e.Stderr)
}

func (e *RunError) Unwrap() error { return e.Err }

func (e *RunError) ExitCode() int {
	var ee *exec.ExitError
	if errors.As(e.Err, &ee) {
		return ee.ExitCode()
	}
	return -1
}
