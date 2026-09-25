package dispatcher

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Claude は dispatcher が使う claude の実体 (絶対パス) と、その版 (`--version` の最初の行)。
type Claude struct {
	Path    string
	Version string
}

// versionTimeout は `claude --version` が戻るまでの上限。
const versionTimeout = 30 * time.Second

// ResolveClaude は dispatcher の起動時に 1 回だけ claude の実体を解決する (464)。起動・再開・一覧・停止・枠・要約はすべてこの Path で呼ぶ。
// 🚨 素の名前で呼ばない: PATH の nodenv の shim は、NODENV_VERSION が無いと cmd.Dir (repo・worktree) の .node-version で node の版を選ぶので、
// repo ごとに別の版の claude が動く。版を cwd で選ぶ版管理 (nodenv / rbenv / pyenv / asdf / mise) は shim を <root>/shims に置き、
// `<名前> which <コマンド>` で今の cwd・env の実体を返す。<名前> は <root> の名前 (先頭の「.」を外す)
func ResolveClaude(ctx context.Context) (Claude, error) {
	p, err := exec.LookPath("claude")
	if err != nil {
		return Claude{}, fmt.Errorf("claude が PATH に無い: %w", err)
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return Claude{}, fmt.Errorf("claude (%s) の実体を引けない: %w", p, err)
	}
	if isShim(real) {
		manager := strings.TrimPrefix(filepath.Base(filepath.Dir(filepath.Dir(real))), ".")
		out, err := exec.CommandContext(ctx, manager, "which", "claude").Output()
		if err != nil {
			return Claude{}, fmt.Errorf("claude (%s) は版を cwd で選ぶ shim で、`%s which claude` で実体を引けない: %w", real, manager, err)
		}
		shim := real
		if real, err = filepath.EvalSymlinks(outLine(out)); err != nil {
			return Claude{}, fmt.Errorf("`%s which claude` の結果 (%q) を引けない: %w", manager, outLine(out), err)
		}
		if isShim(real) {
			return Claude{}, fmt.Errorf("claude (%s) の実体が shim (%s) のまま", shim, real)
		}
	}
	vctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	cmd := exec.CommandContext(vctx, real, "--version")
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return Claude{}, fmt.Errorf("%s --version: %w", real, err)
	}
	return Claude{Path: real, Version: outLine(out)}, nil
}

// isShim は、版を cwd で選ぶ版管理の shim の置き場 (<root>/shims) にあるか。
func isShim(path string) bool { return filepath.Base(filepath.Dir(path)) == "shims" }

// outLine はコマンドの出力の最初の行 (パスを切らない)。
func outLine(b []byte) string {
	line, _, _ := strings.Cut(strings.TrimSpace(string(b)), "\n")
	return strings.TrimSpace(line)
}
