package dispatcher

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Tool は dispatcher が使うコマンドの実体 (絶対パス) と、その版 (`--version` の最初の行)。
type Tool struct {
	Path    string
	Version string
}

// Claude は claude の実体 (ResolveClaude)。
type Claude = Tool

// resolveTimeout は実体の解決 (`<版管理> which claude` と `claude --version`) が戻るまでの上限。
const resolveTimeout = 30 * time.Second

// ResolveClaude は dispatcher の起動時に 1 回だけ claude の実体を解決する (464)。起動・再開・一覧・停止・枠・要約はすべてこの Path で呼ぶ。
// 🚨 素の名前で呼ばない: PATH の nodenv の shim は、NODENV_VERSION が無いと cmd.Dir (repo・worktree) の .node-version で node の版を選ぶので、
// repo ごとに別の版の claude が動く。版を cwd で選ぶ版管理 (nodenv / rbenv / pyenv / asdf / mise) は shim を <root>/shims に置き
// (普通のファイル / 版管理の本体への symlink)、`<名前> which <コマンド>` で今の cwd・env の実体を返す。<名前> は <root> の名前 (先頭の「.」を外す)。
// dir は解決に使う cwd。🚨 画面・人が dispatcher を起こした cwd にしない (その repo の .node-version が claude の入っていない版を指すと解決できず、
// 古い版を指すと全 repo をその版に固定する)。
// 🚨 解決した symlink の先 (版つきのパス) には固定しない: native の自動更新が古い版を消すと、動き続けている dispatcher の起動がすべて失敗する
func ResolveClaude(ctx context.Context, dir string) (Claude, error) {
	return resolveTool(ctx, dir, "claude")
}

// ResolveCodex は codex の実体を ResolveClaude と同じ形で解決する (issue 514。敵対的レビューを codex に回す設定のとき PG に渡す)。
// 🚨 対話シェルの codex は zsh の関数 (zshlib/_codex.zsh) で、PG の bash からは見えない。素の名前ではなくこの Path を渡す
func ResolveCodex(ctx context.Context, dir string) (Tool, error) {
	return resolveTool(ctx, dir, "codex")
}

// resolveTool は name の実体を PATH と版管理の shim から解き、`--version` が通ることまで確かめる。
func resolveTool(ctx context.Context, dir, name string) (Tool, error) {
	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()
	p, err := exec.LookPath(name)
	if err != nil {
		return Tool{}, fmt.Errorf("%s が PATH に無い: %w", name, err)
	}
	if p, err = filepath.Abs(p); err != nil {
		return Tool{}, err
	}
	if shim, ok := shimOf(p); ok {
		manager := strings.TrimPrefix(filepath.Base(filepath.Dir(filepath.Dir(shim))), ".")
		cmd := exec.CommandContext(ctx, manager, "which", name)
		cmd.Dir = dir
		out, err := cmd.Output()
		if err != nil {
			return Tool{}, fmt.Errorf("%s (%s) は版を cwd で選ぶ shim で、%s で `%s which %s` が実体を返さない: %w", name, p, dir, manager, name, err)
		}
		if p = outLine(out); !filepath.IsAbs(p) {
			return Tool{}, fmt.Errorf("`%s which %s` の結果が絶対パスでない: %q", manager, name, p)
		}
		if _, ok := shimOf(p); ok {
			return Tool{}, fmt.Errorf("%s (%s) の実体が shim (%s) のまま", name, shim, p)
		}
	}
	cmd := exec.CommandContext(ctx, p, "--version")
	cmd.Dir = dir
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return Tool{}, fmt.Errorf("%s --version: %w", p, err)
	}
	return Tool{Path: p, Version: outLine(out)}, nil
}

// shimOf は path が版を cwd で選ぶ版管理の shim (<root>/shims の下) なら、その shim のパスを返す。symlink は 1 段ずつ辿って各段を見る
// (EvalSymlinks は最後まで解くので、mise の shim = 本体への symlink を指す symlink では shims を通り過ぎる)。
func shimOf(path string) (string, bool) {
	for range 40 {
		if isShim(path) {
			return path, true
		}
		next, err := os.Readlink(path)
		if err != nil {
			return "", false // symlink でない (実体)
		}
		if !filepath.IsAbs(next) {
			next = filepath.Join(filepath.Dir(path), next)
		}
		path = next
	}
	return "", false
}

func isShim(path string) bool { return filepath.Base(filepath.Dir(path)) == "shims" }

// outLine はコマンドの出力の最初の行 (パスを切らない)。
func outLine(b []byte) string {
	line, _, _ := strings.Cut(strings.TrimSpace(string(b)), "\n")
	return strings.TrimSpace(line)
}
