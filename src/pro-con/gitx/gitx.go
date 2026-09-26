// Package gitx は pro-con が git を呼ぶときの共通の口 (見張りの読み取り = package monitor と、worktree の片付け = package wtclean)。
//
// 🚨 `-C <dir>` を渡しても、継承した GIT_DIR / GIT_WORK_TREE などが先に効く (hook から起動された shell 等)。
// そのまま走らせると、-C の先ではなく継承した repo を読み・書く。ここでは repo の場所を決める環境変数を外してから走らせる
// (~/.claude/rules/sandbox-real-destructive-test-apis.md)。
package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"
)

// Timeout は git 1 回の上限 (大きな repo の merge-tree でも数秒の見込み。固まった git で呼び出し側を止めない)。
const Timeout = 2 * time.Minute

// scrubbed は外す環境変数 (`git rev-parse --local-env-vars` の repo の場所を決めるもの。2.43 で確認)。
var scrubbed = []string{
	"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE", "GIT_PREFIX", "GIT_CONFIG", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT",
}

// Env は git に渡す環境 (今の環境から scrubbed を外したもの)。
func Env() []string {
	var out []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if !isScrubbed(k) {
			out = append(out, kv)
		}
	}
	return out
}

func isScrubbed(k string) bool {
	return slices.Contains(scrubbed, k) || strings.HasPrefix(k, "GIT_CONFIG_KEY_") || strings.HasPrefix(k, "GIT_CONFIG_VALUE_")
}

// Run は git を dir で走らせ、stdout と rc を返す。起動できない・時間切れは err (rc で表せる失敗は err にしない)。
// rc が 2 以上は err にする (rev-parse だけは rc を呼ぶ側が見る)。
func Run(ctx context.Context, dir string, args ...string) (string, int, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = Env()
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	var exit *exec.ExitError
	name := firstCommand(args)
	switch {
	case ctx.Err() != nil:
		return "", -1, fmt.Errorf("git %s: %w", name, ctx.Err())
	case errors.As(err, &exit):
		if exit.ExitCode() > 1 && name != "rev-parse" {
			return out.String(), exit.ExitCode(), fmt.Errorf("git %s: rc=%d: %s", name, exit.ExitCode(), strings.TrimSpace(errOut.String()))
		}
		return out.String(), exit.ExitCode(), nil
	case err != nil:
		return "", -1, fmt.Errorf("git %s: %w", name, err)
	}
	return out.String(), 0, nil
}

// firstCommand は git のサブコマンドの名前 (先頭の --no-optional-locks などを飛ばす)。
func firstCommand(args []string) string {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return strings.Join(args, " ")
}

// Base は repo の取り込む先 (origin/HEAD → origin/master → origin/main の最初に在るもの) の commit と名前。
// 🚨 fetch はしない (呼び出し側は ref を動かさない)。origin/master は取り込みの係が worktree から push するたびに手元で更新される
func Base(ctx context.Context, repo string) (string, string, error) {
	for _, ref := range []string{"refs/remotes/origin/HEAD", "refs/remotes/origin/master", "refs/remotes/origin/main"} {
		out, rc, err := Run(ctx, repo, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
		if err != nil {
			return "", "", err
		}
		if rc != 0 {
			continue
		}
		name := strings.TrimPrefix(ref, "refs/remotes/")
		if ref == "refs/remotes/origin/HEAD" { // 人が読む名前は指している先 (origin/master)
			if n, rc, err := Run(ctx, repo, "rev-parse", "--abbrev-ref", ref); err == nil && rc == 0 && strings.TrimSpace(n) != "" {
				name = strings.TrimSpace(n)
			}
		}
		return strings.TrimSpace(out), name, nil
	}
	return "", "", errors.New("取り込む先 (origin/HEAD・origin/master・origin/main) が無い")
}

// IsAncestor は a が b の祖先 (か同じ) か。
func IsAncestor(ctx context.Context, repo, a, b string) (bool, error) {
	_, rc, err := Run(ctx, repo, "merge-base", "--is-ancestor", a, b)
	switch {
	case err != nil:
		return false, err
	case rc == 0:
		return true, nil
	case rc == 1:
		return false, nil
	}
	return false, fmt.Errorf("git merge-base --is-ancestor が rc=%d", rc)
}
