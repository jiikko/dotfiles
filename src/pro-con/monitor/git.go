package monitor

// 見張りが使う git の読み取り。🚨 ref・作業ツリー・index を動かすコマンドを足さない (見張りは読むだけ。issue 475)。
// merge-tree --write-tree は object database に tree を書くが、ref は動かさない (どこからも指されない object は後の gc で消える)。

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"pro-con/gitx"
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

// ExecGit は本物の git (gitx 経由。継承した GIT_DIR 等を外して走らせる)。
type ExecGit struct{}

func (ExecGit) Base(ctx context.Context, repo string) (string, string, error) {
	return gitx.Base(ctx, repo)
}

func (ExecGit) Head(ctx context.Context, worktree string) (string, error) {
	out, rc, err := gitx.Run(ctx, worktree, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", err
	}
	if rc != 0 {
		return "", fmt.Errorf("%s の HEAD を読めない", worktree)
	}
	return strings.TrimSpace(out), nil
}

func (ExecGit) IsAncestor(ctx context.Context, repo, a, b string) (bool, error) {
	return gitx.IsAncestor(ctx, repo, a, b)
}

// MergeTree は `git merge-tree --write-tree --name-only --no-messages` (git 2.38 から)。rc 0 = 衝突なし / 1 = 衝突。
// 出力の 1 行目は合わせた tree で、続く行が衝突したファイル (空行で終わる)。
func (ExecGit) MergeTree(ctx context.Context, repo, a, b string) (bool, []string, error) {
	out, rc, err := gitx.Run(ctx, repo, "merge-tree", "--write-tree", "--name-only", "--no-messages", a, b)
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
