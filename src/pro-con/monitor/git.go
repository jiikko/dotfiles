package monitor

// 見張りが使う git の読み取り。🚨 ref・作業ツリー・index を動かすコマンドを足さない (見張りは読むだけ。issue 475)。
// merge-tree --write-tree は object database に tree を書くが、ref は動かさない (どこからも指されない object は後の gc で消える)。

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"time"
)

// Git は見張りが使う git の読み取り (テストが差し替えられる)。
type Git interface {
	// Base は repo の取り込む先 (origin/HEAD → origin/master → origin/main の最初に在るもの) の commit と名前。
	Base(ctx context.Context, repo string) (sha, name string, err error)
	// Head は worktree の HEAD の commit。
	Head(ctx context.Context, worktree string) (string, error)
	// IsAncestor は a が b の祖先 (か同じ) か。
	IsAncestor(ctx context.Context, repo, a, b string) (bool, error)
	// MergeTree は a と b を合わせたときに衝突するか、衝突したファイル。
	MergeTree(ctx context.Context, repo, a, b string) (conflict bool, files []string, err error)
}

// gitTimeout は git 1 回の上限 (大きな repo の merge-tree でも数秒の見込み。固まった git で見張りを止めない)。
const gitTimeout = 2 * time.Minute

// ExecGit は本物の git。
type ExecGit struct{}

func (ExecGit) Base(ctx context.Context, repo string) (string, string, error) {
	for _, ref := range []string{"refs/remotes/origin/HEAD", "refs/remotes/origin/master", "refs/remotes/origin/main"} {
		out, rc, err := gitRun(ctx, repo, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
		if err != nil {
			return "", "", err
		}
		if rc != 0 {
			continue
		}
		name := strings.TrimPrefix(ref, "refs/remotes/")
		if ref == "refs/remotes/origin/HEAD" { // 人が読む名前は指している先 (origin/master)
			if n, rc, err := gitRun(ctx, repo, "rev-parse", "--abbrev-ref", ref); err == nil && rc == 0 && strings.TrimSpace(n) != "" {
				name = strings.TrimSpace(n)
			}
		}
		return strings.TrimSpace(out), name, nil
	}
	return "", "", errors.New("取り込む先 (origin/HEAD・origin/master・origin/main) が無い")
}

func (ExecGit) Head(ctx context.Context, worktree string) (string, error) {
	out, rc, err := gitRun(ctx, worktree, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", err
	}
	if rc != 0 {
		return "", fmt.Errorf("%s の HEAD を読めない", worktree)
	}
	return strings.TrimSpace(out), nil
}

func (ExecGit) IsAncestor(ctx context.Context, repo, a, b string) (bool, error) {
	_, rc, err := gitRun(ctx, repo, "merge-base", "--is-ancestor", a, b)
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

// MergeTree は `git merge-tree --write-tree --name-only --no-messages` (git 2.38 から)。rc 0 = 衝突なし / 1 = 衝突。
// 出力の 1 行目は合わせた tree で、続く行が衝突したファイル (空行で終わる)。
func (ExecGit) MergeTree(ctx context.Context, repo, a, b string) (bool, []string, error) {
	out, rc, err := gitRun(ctx, repo, "merge-tree", "--write-tree", "--name-only", "--no-messages", a, b)
	if err != nil {
		return false, nil, err
	}
	switch rc {
	case 0:
		return false, nil, nil
	case 1:
		lines := strings.Split(out, "\n")
		var files []string
		for _, l := range lines[1:] {
			if l == "" {
				break
			}
			if !slices.Contains(files, l) {
				files = append(files, l)
			}
		}
		return true, files, nil
	}
	return false, nil, fmt.Errorf("git merge-tree が rc=%d", rc)
}

// gitRun は git を dir で走らせ、stdout と rc を返す。起動できない・時間切れは err (rc で表せる失敗は err にしない)。
func gitRun(ctx context.Context, dir string, args ...string) (string, int, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case ctx.Err() != nil:
		return "", -1, fmt.Errorf("git %s: %w", args[0], ctx.Err())
	case errors.As(err, &exit):
		if exit.ExitCode() > 1 && args[0] != "rev-parse" { // rev-parse の rc は呼ぶ側が見る
			return out.String(), exit.ExitCode(), fmt.Errorf("git %s: rc=%d: %s", args[0], exit.ExitCode(), strings.TrimSpace(errOut.String()))
		}
		return out.String(), exit.ExitCode(), nil
	case err != nil:
		return "", -1, fmt.Errorf("git %s: %w", args[0], err)
	}
	return out.String(), 0, nil
}
