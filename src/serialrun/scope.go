package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"subproc"
	"time"
)

// scopeEnv は -l を省いたときの列の範囲 (repo: 既定。同じ git の repo のコマンドだけが 1 本ずつ / machine: マシン全体で 1 本)
const scopeEnv = "SERIAL_RUN_SCOPE"

// gitTimeout は repo を調べる git 1 回の上限 (詰まった git で待ち列に入れなくならないため。超えたらマシン全体の列に並ぶ)
const gitTimeout = 2 * time.Second

// lockFor は -l を省いたときの lock 名を、cwd (dir) と SERIAL_RUN_SCOPE から決める。
// repo の範囲では、同じ repo の worktree どうしは同じ列 (git の common dir で見る)。git の repo の外はマシン全体の列。
// 範囲の値が誤っていればマシン全体の列と、その誤りを返す (呼び出し側が警告して続ける)
func lockFor(dir string) (string, error) {
	switch s := os.Getenv(scopeEnv); s {
	case "", "repo":
	case "machine":
		return defaultLock, nil
	default:
		return defaultLock, fmt.Errorf("%s は repo か machine: %q", scopeEnv, s)
	}
	common, ok := gitCommonDir(dir)
	if !ok {
		return defaultLock, nil
	}
	return repoLockName(common), nil
}

// gitCommonDir は dir の git の common dir (worktree でも本体の .git) の実パスを返す
func gitCommonDir(dir string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", subproc.GitArgs("-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir")...)
	// 継承した repo の場所 (GIT_DIR など) を外し、cwd だけで決める (相対の GIT_DIR はサブディレクトリで外れ、大文字小文字の違う GIT_DIR は
	// 別の列になる。dotfiles の Makefile は recipe でこれらを外すので、外さないと外側と内側で列が割れる)。
	// 外すのは場所の変数だけ (GIT_CONFIG_* などの設定は残す。safe.directory を渡す環境で rev-parse が通るように)
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); !subproc.IsGitLocationEnv(name) {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	p := strings.TrimSpace(string(out))
	if p == "" {
		return "", false
	}
	if real, err := filepath.EvalSymlinks(p); err == nil {
		p = real
	}
	return p, true
}

// repoLockName は common dir から lock 名を作る: repo-<repo の dir 名>-<common dir の hash 8 桁>。
// 名前は人が status で読むため、hash は同じ dir 名の別の repo を分けるため。lock 名の規則 (lockNameRe) に収める
func repoLockName(common string) string {
	root := common
	if filepath.Base(root) == ".git" {
		root = filepath.Dir(root)
	}
	name := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		}
		return '-'
	}, filepath.Base(root))
	name = strings.TrimLeft(name, ".")
	if r := []rune(name); len(r) > 40 {
		name = string(r[:40])
	}
	sum := sha256.Sum256([]byte(common))
	return "repo-" + name + "-" + hex.EncodeToString(sum[:4])
}
