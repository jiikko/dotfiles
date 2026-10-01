package dispatcher

// ExecLauncher は本物の claude で PG・PM・取り込みの係 (persistent role) の session を起動・再開する。引数の形は 425 の実測と `claude --help` から組んだ:
//   - 起動は `claude --bg -w <name> <profile> <prompt>`。-w で Claude Code に worktree を作らせる
//     (trust 済みの repo の下でないと --bg が起動しない。415 論点 2)
//   - 再開は stop してから `claude --bg --resume <session-id> <profile> <text>` (実行中の session に --resume するとコピーが起動する。415 論点 11)
//   - 起動し直しは stop してから、前の worktree を cwd に `claude --bg <profile> <prompt>` (-w を付けない。付けると worktree の中に worktree を作る。
//     -w 無しの --bg が role の worktree で起動して idle になることは 581 で実測 = claude 2.1.286)
//   - <profile> (persistentSessionArgs) は起動と再開で同じにする (431)。prompt cache は request の先頭 (tools・system) が同じなら
//     session をまたいで当たる。形は launcher_test.go の TestPersistentSessionProfile が固定する
//   - TMUX / TMUX_PANE を落とす (PG が起動元の pane の状態のバッジを上書きしないように。415 論点 5)

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ExecLauncher は Launcher の本物。Claude は claude の実体の絶対パス (ResolveClaude。素の名前にしない = 464)。
// UserSettings はユーザーの settings.json のパスで、起動・再開のたびに language を読む (空なら language は渡さない)。
type ExecLauncher struct{ Claude, UserSettings string }

// ErrRejected は claude が起動・再開を受け付けなかった失敗 (rc≠0 で返った / プロセスを起動できなかった)。何も立っていないので、
// 同じ失敗を繰り返さずに数えられる (462。時間切れ・--bg の出力が読めないものは、立っているかもしれないので含めない)
var ErrRejected = errors.New("claude が受け付けなかった")

// errStopFailed は再開・起動し直しの前に前の session を止められなかった失敗 (ErrRejected も併せ持ちうる)。起動し直しが受け付けられなかったのと分ける (581)
var errStopFailed = errors.New("前の session を止められなかった")

// launchTimeout は claude --bg / stop が戻るまでの上限 (--bg は起動したらすぐ戻る。実測は 1 秒未満)。
const launchTimeout = 30 * time.Second

func (l ExecLauncher) Start(ctx context.Context, repoPath, name, prompt string) (string, error) {
	out, err := runClaude(ctx, l.Claude, repoPath, l.startArgs(name, prompt)...)
	if err != nil {
		return "", err
	}
	return parseBackgrounded(out)
}

func (l ExecLauncher) Resume(ctx context.Context, stopID, sessionID, cwd, name, text string) (string, error) {
	return l.stopThenRun(ctx, stopID, cwd, l.resumeArgs(sessionID, name, text))
}

func (l ExecLauncher) Restart(ctx context.Context, stopID, cwd, name, prompt string) (string, error) {
	return l.stopThenRun(ctx, stopID, cwd, l.restartArgs(name, prompt))
}

// stopThenRun は stopID の session を止めてから (空なら止めない) cwd で claude を args で走らせ、--bg が返す短い id を返す (再開と起動し直しの共通)。
func (l ExecLauncher) stopThenRun(ctx context.Context, stopID, cwd string, args []string) (string, error) {
	if stopID != "" {
		if _, err := runClaude(ctx, l.Claude, "", "stop", stopID); err != nil {
			return "", fmt.Errorf("%w: claude stop %s: %w", errStopFailed, stopID, err)
		}
	}
	out, err := runClaude(ctx, l.Claude, cwd, args...)
	if err != nil {
		return "", err
	}
	return parseBackgrounded(out)
}

func (l ExecLauncher) Stop(ctx context.Context, id string) error {
	if _, err := runClaude(ctx, l.Claude, "", "stop", id); err != nil {
		return fmt.Errorf("claude stop %s: %w", id, err)
	}
	return nil
}

func (l ExecLauncher) startArgs(name, prompt string) []string {
	return l.args([]string{"--bg", "-w", name}, name, prompt)
}

func (l ExecLauncher) resumeArgs(sessionID, name, text string) []string {
	return l.args([]string{"--bg", "--resume", sessionID}, name, text)
}

func (l ExecLauncher) restartArgs(name, prompt string) []string {
	return l.args([]string{"--bg"}, name, prompt)
}

func (l ExecLauncher) args(lifecycle []string, name, positional string) []string {
	return append(append(lifecycle, persistentSessionArgs(name, l.UserSettings)...), positionalArg(positional))
}

// persistentSessionArgs は起動と再開で共通の session の形: -n <name> (再開でも付け直す。付けないと、再開の後の名前は AI の付けた題になる。
// 488 で -n の有無の A-B を実測) と、--setting-sources project,local + --settings (persistentSessionSettings。中身と外すものは rolesettings.go)。
func persistentSessionArgs(name, userSettings string) []string {
	return []string{"-n", name, "--setting-sources", "project,local", "--settings", persistentSessionSettings(userSettings)}
}

// dashGuard は「-」で始まる位置引数の前に付ける前置き。
const dashGuard = "pro-con から:\n"

// positionalArg は位置引数が「-」で始まらないようにする (462)。claude は「-」で始まる引数をオプションと読み、
// 箇条書きの回答・追加オーダーで `error: unknown option` の rc=1 になる。
// 🚨 `--` で区切る形は claude が受けるかを実測していないので使わない。本文は削らない
func positionalArg(s string) string {
	if strings.HasPrefix(s, "-") {
		return dashGuard + s
	}
	return s
}

// UserSettingsPath は claude が読むユーザーの settings.json (CLAUDE_CONFIG_DIR があればその下)。
func UserSettingsPath(home string) string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "settings.json")
	}
	return filepath.Join(home, ".claude", "settings.json")
}

func runClaude(ctx context.Context, claude, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, launchTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, claude, args...)
	cmd.Dir = dir
	cmd.Env = withoutTmux(os.Environ())
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		// 起動できなかった (cmd.Process が無い: chdir の失敗・claude が無い) か、時間内に自分で rc≠0 を返したものだけが「立っていない」。
		// 取り消し・時間切れ (ctx) は claude のせいではないので数えない
		if ctx.Err() == nil && (cmd.Process == nil || (errors.As(err, &exit) && exit.Exited())) {
			err = fmt.Errorf("%w: %w", ErrRejected, err)
		}
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
