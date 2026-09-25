package dispatcher

// ExecLauncher は本物の claude で PG を起動・再開する。
//
// 🚨 **まだ一度も本物の claude で走らせていない** (週の利用枠のため。issue 427 の 3f で確かめる)。引数の形は 425 の実測と `claude --help` から組んだ:
//   - 起動は `claude --bg -w <name> -n <name> --setting-sources project,local <prompt>`。-w で Claude Code に worktree を作らせる
//     (trust 済みの repo の下でないと --bg が起動しない。415 論点 2)。--setting-sources でユーザーの hook を外す (431 の計測)
//   - TMUX / TMUX_PANE を落とす (PG が起動元の pane の状態のバッジを上書きしないように。415 論点 5)
//   - 再開は stop してから `claude --bg --resume <session-id> <text>` (実行中の session に --resume するとコピーが起動する。415 論点 11)
//   - 起動・再開ともユーザーの settings.json の language だけを --settings で渡す (461。-p では --setting-sources に user を入れても
//     language が効かず、--settings で渡したときだけ効いた。440 の 7d)。🚨 中身は言語だけ (hook・許可を足すと --setting-sources で外した意味が崩れる)
// 431 の PG 用の設定ディレクトリ (規約を絞る) はログイン待ち (433) なので、まだ渡していない。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ExecLauncher は Launcher の本物。Claude は claude の実体の絶対パス (ResolveClaude。素の名前にしない = 464)。
// UserSettings はユーザーの settings.json のパスで、起動・再開のたびに language を読む (空なら渡さない)。
type ExecLauncher struct{ Claude, UserSettings string }

// ErrRejected は claude が起動・再開を受け付けなかった失敗 (rc≠0 で返った / プロセスを起動できなかった)。何も立っていないので、
// 同じ失敗を繰り返さずに数えられる (462。時間切れ・--bg の出力が読めないものは、立っているかもしれないので含めない)
var ErrRejected = errors.New("claude が受け付けなかった")

// launchTimeout は claude --bg / stop が戻るまでの上限 (--bg は起動したらすぐ戻る。実測は 1 秒未満)。
const launchTimeout = 30 * time.Second

func (l ExecLauncher) Start(ctx context.Context, repoPath, name, prompt string) (string, error) {
	out, err := runClaude(ctx, l.Claude, repoPath, l.startArgs(name, prompt)...)
	if err != nil {
		return "", err
	}
	return parseBackgrounded(out)
}

func (l ExecLauncher) Resume(ctx context.Context, stopID, sessionID, cwd, text string) (string, error) {
	if stopID != "" {
		if _, err := runClaude(ctx, l.Claude, "", "stop", stopID); err != nil {
			return "", fmt.Errorf("claude stop %s: %w", stopID, err)
		}
	}
	out, err := runClaude(ctx, l.Claude, cwd, l.resumeArgs(sessionID, text)...)
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
	return withSettings([]string{"--bg", "-w", name, "-n", name, "--setting-sources", "project,local"}, languageSettings(l.UserSettings), prompt)
}

func (l ExecLauncher) resumeArgs(sessionID, text string) []string {
	return withSettings([]string{"--bg", "--resume", sessionID, "--setting-sources", "project,local"}, languageSettings(l.UserSettings), text)
}

// withSettings は --settings を位置引数 (prompt) の前に挟む。settings が空なら付けない。
func withSettings(args []string, settings, positional string) []string {
	if settings != "" {
		args = append(args, "--settings", settings)
	}
	return append(args, positionalArg(positional))
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

// languageSettings は path の settings.json から language だけを抜いた --settings の JSON を返す。
// 読めない・壊れている・language が無い / 文字列でない / 空なら "" (渡さない。起動は止めない)。
func languageSettings(path string) string {
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var s struct {
		Language any `json:"language"`
	}
	if json.Unmarshal(b, &s) != nil {
		return ""
	}
	lang, ok := s.Language.(string)
	if !ok || strings.TrimSpace(lang) == "" {
		return ""
	}
	out, err := json.Marshal(map[string]string{"language": lang})
	if err != nil {
		return ""
	}
	return string(out)
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
