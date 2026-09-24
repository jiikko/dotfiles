package daemon

// ExecLauncher は本物の claude で PG を起動・再開する。
//
// 🚨 **まだ一度も本物の claude で走らせていない** (週の利用枠のため。issue 427 の 3f で確かめる)。引数の形は 425 の実測と `claude --help` から組んだ:
//   - 起動は `claude --bg -w <name> -n <name> --setting-sources project,local <prompt>`。-w で Claude Code に worktree を作らせる
//     (trust 済みの repo の下でないと --bg が起動しない。415 論点 2)。--setting-sources でユーザーの hook を外す (431 の計測)
//   - TMUX / TMUX_PANE を落とす (PG が起動元の pane の状態のバッジを上書きしないように。415 論点 5)
//   - 再開は stop してから `claude --bg --resume <session-id> <text>` (実行中の session に --resume するとコピーが起動する。415 論点 11)
// 431 の PG 用の設定ディレクトリ (規約を絞る) はログイン待ち (433) なので、まだ渡していない。

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ExecLauncher は Launcher の本物。
type ExecLauncher struct{}

// launchTimeout は claude --bg / stop が戻るまでの上限 (--bg は起動したらすぐ戻る。実測は 1 秒未満)。
const launchTimeout = 30 * time.Second

func (ExecLauncher) Start(ctx context.Context, repoPath, name, prompt string) (string, error) {
	out, err := runClaude(ctx, repoPath, "--bg", "-w", name, "-n", name, "--setting-sources", "project,local", prompt)
	if err != nil {
		return "", err
	}
	return parseBackgrounded(out)
}

func (ExecLauncher) Resume(ctx context.Context, id, sessionID, text string) (string, error) {
	if _, err := runClaude(ctx, "", "stop", id); err != nil {
		return "", fmt.Errorf("claude stop %s: %w", id, err)
	}
	out, err := runClaude(ctx, "", "--bg", "--resume", sessionID, "--setting-sources", "project,local", text)
	if err != nil {
		return "", err
	}
	return parseBackgrounded(out)
}

func runClaude(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, launchTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = dir
	cmd.Env = withoutTmux(os.Environ())
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("claude %s: %w: %s", args[0], err, strings.TrimSpace(errOut.String()))
	}
	return out.String(), nil
}

// parseBackgrounded は claude --bg の最初の行「backgrounded · <id> · <name>」から id を取る (425 で実測した形)。
func parseBackgrounded(out string) (string, error) {
	line, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	parts := strings.Split(line, " · ")
	if len(parts) < 2 || strings.TrimSpace(parts[0]) != "backgrounded" || strings.TrimSpace(parts[1]) == "" {
		return "", fmt.Errorf("claude --bg の出力を読めない (版が変わった?): %q", line)
	}
	return strings.TrimSpace(parts[1]), nil
}

func withoutTmux(env []string) []string {
	out := env[:0:0]
	for _, e := range env {
		if strings.HasPrefix(e, "TMUX=") || strings.HasPrefix(e, "TMUX_PANE=") {
			continue
		}
		out = append(out, e)
	}
	return out
}
