package dispatcher

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"pro-con/store"
)

// 既定のモデルと effort (store.Models[0] / store.DefaultEffort から作らない: 作ると既定を変える変更をテストが追いかける)。
const (
	defaultModel       = "claude-opus-5-5"
	defaultEffort      = "medium"
	defaultModelEffort = defaultModel + " " + defaultEffort
)

func writeSettings(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// 起動・再開の引数に、ユーザーの settings.json の language と auto memory・SendFeedback の無効化だけを --settings で渡す
// (461 / 431 / 525。hook・許可は持ち込まない。起動と再開で同じ値 = tools の並びが揃う)。
// --settings を完全一致で固定するのはこのテストと TestLauncherArgsWithoutLanguageStillDisableAutoMemory で、時刻や PID のような揮発値を入れる変更は
// この 2 本が止める (TestPersistentSessionProfile は同じ時刻の中でしか比べない)。名前は本番と同じ形 (pc-…) にしておく。
func TestLauncherArgsPassLanguageAndNoAutoMemory(t *testing.T) {
	p := writeSettings(t, `{"language": "日本語", "hooks": {"Stop": []}, "permissions": {"allow": ["Bash"]}, "model": "opus"}`)
	const settings = `{"autoMemoryEnabled":false,"feedbackDrafts":"off","language":"日本語"}`
	l := ExecLauncher{UserSettings: p}
	start := l.startArgs("pc-c-001", "依頼")
	want := []string{"--bg", "-w", "pc-c-001", "-n", "pc-c-001", "--model", defaultModel, "--effort", defaultEffort, "--setting-sources", "project,local", "--settings", settings, "依頼"}
	if !slices.Equal(start, want) {
		t.Fatalf("起動の引数 = %q\nwant %q", start, want)
	}
	resume := l.resumeArgs("sid", "pc-c-001", "回答")
	want = []string{"--bg", "--resume", "sid", "-n", "pc-c-001", "--model", defaultModel, "--effort", defaultEffort, "--setting-sources", "project,local", "--settings", settings, "回答"}
	if !slices.Equal(resume, want) {
		t.Fatalf("再開の引数 = %q\nwant %q", resume, want)
	}
	restart := l.restartArgs("pc-pm-x", "指示")
	want = []string{"--bg", "-n", "pc-pm-x", "--model", defaultModel, "--effort", defaultEffort, "--setting-sources", "project,local", "--settings", settings, "指示"}
	if !slices.Equal(restart, want) {
		t.Fatalf("起動し直しの引数 = %q\nwant %q", restart, want)
	}
}

// 読めない・壊れている・language が無い / 文字列でない / 空なら language を渡さない (起動は止めない。値を固定で補わない)。
// auto memory・SendFeedback の無効化は language と関係なく渡す。~/.claude/CLAUDE.md は PG・PM・取り込みの係には残す (431)。
func TestLauncherArgsWithoutLanguageStillDisableAutoMemory(t *testing.T) {
	cases := map[string]string{
		"パスが空":        "",
		"ファイルが無い":     filepath.Join(t.TempDir(), "missing.json"),
		"壊れた JSON":    writeSettings(t, `{"language": `),
		"language 無し": writeSettings(t, `{"model": "opus"}`),
		"文字列でない":      writeSettings(t, `{"language": 1}`),
		"空":           writeSettings(t, `{"language": "  "}`),
	}
	for name, p := range cases {
		l := ExecLauncher{UserSettings: p}
		for kind, args := range map[string][]string{"起動": l.startArgs("n", "p"), "再開": l.resumeArgs("sid", "n", "t"), "起動し直し": l.restartArgs("n", "p")} {
			i := slices.Index(args, "--settings")
			if i < 0 || i+1 >= len(args) || args[i+1] != `{"autoMemoryEnabled":false,"feedbackDrafts":"off"}` {
				t.Errorf("%s: %sの引数 = %q (--settings は auto memory・SendFeedback の無効化だけのはず)", name, kind, args)
			}
		}
	}
}

// PG・PM・取り込みの係の session の形 (431。理由は launcher.go 冒頭)。止めるのは、pro-con のコードの変更で次が崩れる形:
//   - 起動・再開・起動し直し (581)・役 (PG / PM / 取り込みの係)・session の名前をまたいで、違うのは -w <name> / --resume <id> / -n <name> と位置引数だけ。
//     Start / Resume / Restart は組んだ引数をそのまま claude に渡し (止めてから再開・起動し直すときも)、環境変数も起動・再開・起動し直しで同じ
//   - ユーザーの settings.json から写すのは language だけ (model・effort・hook・許可・plugin・env を写すと、人の普段の設定の変更で role の形が変わる)。
//     引数の全体で比べるので、--settings に写す変更も CLI の flag に写す変更も止まる
//   - --setting-sources は project,local (user を入れると hook・許可・~/.claude/rules が戻る)。--exclude-dynamic-system-prompt-sections は付けない
//     (-p 用で --bg の session には効かない。upstream が対応したら 449 の測り方で A-B してから)
//
// 検出しないもの: fixture に無い鍵・値の形を写す変更 / 同じ時刻の中で変わらない揮発値 (完全一致の TestLauncherArgs… 2 本が止める) / 故意の迂回 /
// Claude Code の側が起動と再開で変える部分 (546)
func TestPersistentSessionProfile(t *testing.T) {
	user := writeSettings(t, `{"language": "日本語", "model": "opus", "effortLevel": "max", "outputStyle": "x", "alwaysThinkingEnabled": true,
		"hooks": {"Stop": [{"hooks": [{"type": "command", "command": "stop.sh"}]}], "SessionStart": [{"hooks": [{"type": "command", "command": "start.sh"}]}],
			"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "pre.sh"}]}]},
		"permissions": {"allow": ["Bash"], "deny": ["Read(./.env)"], "ask": ["Bash(git push:*)"], "defaultMode": "auto"},
		"enabledPlugins": {"gopls-lsp@claude-plugins-official": true}, "mcpServers": {"x": {"command": "x"}}, "statusLine": {"type": "command", "command": "x"},
		"env": {"ENABLE_CLAUDEAI_MCP_SERVERS": "false", "CLAUDE_CODE_X": "1", "DISABLE_X": "1", "A": "1"},
		"agentPushNotifEnabled": true, "skipAutoPermissionPrompt": true, "preferredNotifChannel": "x", "skipWorkflowUsageWarning": true,
		"workflowSizeGuideline": "small", "autoMemoryEnabled": true, "feedbackDrafts": "on", "claudeMdExcludes": ["/x"]}`)
	l := ExecLauncher{UserSettings: user}
	const prompt, text = "依頼 C-001\n\n本文", "- 回答" // 改行を含む指示と、「-」で始まる本文 (前置きが付く) も通す
	start := l.startArgs("pc-c-001", prompt)
	resume := l.resumeArgs("sid-1", "pc-c-001", text)
	shape := sessionShape(t, start)
	for _, args := range [][]string{resume,
		l.startArgs("pc-pm-20260929-120000", "PM の指示"), l.resumeArgs("sid-2", "pc-pm-20260929-120000", "通知"),
		l.startArgs("pc-int-20260929-120000", "取り込みの指示"), l.resumeArgs("sid-3", "pc-int-20260929-120000", "- 取り込んで"),
		l.restartArgs("pc-pm-20260929-120000", "PM の指示"), l.restartArgs("pc-int-20260929-120000", "- 取り込みの指示")} {
		if s := sessionShape(t, args); !slices.Equal(s, shape) {
			t.Fatalf("起動と再開・役・名前で session の形が違う (キャッシュの先頭が揃わない):\n%q\nwant %q", s, shape)
		}
	}
	run := func(f func(ExecLauncher) error) ([]string, string) {
		return execArgs(t, func(claude string) error { return f(ExecLauncher{Claude: claude, UserSettings: user}) })
	}
	execStart, envStart := run(func(x ExecLauncher) error {
		_, err := x.Start(context.Background(), t.TempDir(), "pc-c-001", prompt)
		return err
	})
	execResume, envResume := run(func(x ExecLauncher) error {
		_, err := x.Resume(context.Background(), "", "sid-1", t.TempDir(), "pc-c-001", text)
		return err
	})
	execStopResume, _ := run(func(x ExecLauncher) error {
		_, err := x.Resume(context.Background(), "old-1", "sid-1", t.TempDir(), "pc-c-001", text)
		return err
	})
	restart := l.restartArgs("pc-pm-20260929-120000", prompt)
	execStopRestart, envRestart := run(func(x ExecLauncher) error {
		_, err := x.Restart(context.Background(), "old-2", t.TempDir(), "pc-pm-20260929-120000", prompt)
		return err
	})
	if !slices.Equal(execStart, start) || !slices.Equal(execResume, resume) || !slices.Equal(execStopResume, append([]string{"stop", "old-1"}, resume...)) ||
		!slices.Equal(execStopRestart, append([]string{"stop", "old-2"}, restart...)) {
		t.Fatalf("claude が受け取った引数が、組んだ引数と違う (組んだ後に足した・組まずに渡した・止めずに再開した):\n起動 %q\nwant %q\n再開 %q\nwant %q\n止めて再開 %q\n止めて起動し直し %q",
			execStart, start, execResume, resume, execStopResume, execStopRestart)
	}
	if envStart != envResume || envStart != envRestart || strings.Contains("\n"+envStart, "\nTMUX") {
		t.Fatalf("起動・再開・起動し直しで claude の環境変数が違う / TMUX が残っている (415 論点 5):\n起動 %q\n再開 %q\n起動し直し %q", envStart, envResume, envRestart)
	}
	langOnly := ExecLauncher{UserSettings: writeSettings(t, `{"language": "日本語"}`)}
	for _, c := range []struct{ got, want []string }{
		{start, langOnly.startArgs("pc-c-001", prompt)},
		{resume, langOnly.resumeArgs("sid-1", "pc-c-001", text)},
		{restart, langOnly.restartArgs("pc-pm-20260929-120000", prompt)},
	} {
		if !slices.Equal(c.got, c.want) {
			t.Fatalf("ユーザーの settings.json から language 以外を写している:\n%q\nwant %q", c.got, c.want)
		}
	}
	for _, args := range [][]string{start, resume, restart} {
		if v := flagValue(t, args, "--setting-sources"); v != "project,local" {
			t.Fatalf("--setting-sources = %q (user を入れると hook・許可・~/.claude/rules が戻る): %q", v, args)
		}
		if slices.Contains(args, "--exclude-dynamic-system-prompt-sections") {
			t.Fatalf("--exclude-dynamic-system-prompt-sections を付けた (-p 用で --bg の session には効かない): %q", args)
		}
	}
}

// sessionShape は起動・再開の引数から、起動だけ・再開だけに要る -w <name> / --resume <id> と最後の位置引数を除き、-n の値を伏せた残り。
func sessionShape(t *testing.T, args []string) []string {
	t.Helper()
	if len(args) == 0 {
		t.Fatal("引数が空")
	}
	var out []string
	for i := 0; i < len(args)-1; i++ {
		switch args[i] {
		case "-w", "--resume":
			i++
		case "-n":
			out = append(out, "-n", "<name>")
			i++
		default:
			out = append(out, args[i])
		}
	}
	return out
}

// execArgs は偽の claude (fakeClaude) で f (Start / Resume) を走らせ、claude が受け取った引数 (呼んだ順につないだもの) と、
// 最後の呼び出しの環境変数のうち claude の形を変えうるもの (fakeClaude の envFile) を返す。
func execArgs(t *testing.T, f func(claude string) error) ([]string, string) {
	t.Helper()
	claude, argsFile := fakeClaude(t)
	if err := f(claude); err != nil {
		t.Fatalf("偽の claude が受け付けなかった: %v", err)
	}
	b, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	env, err := os.ReadFile(argsFile + ".env")
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\x00"), "\x00"), string(env)
}

// flagValue は args の flag の次の値。flag がちょうど 1 回だけ在るのでなければ落とす (2 つめの同じ flag で上書きする形を見逃さない)。
func flagValue(t *testing.T, args []string, flag string) string {
	t.Helper()
	i := slices.Index(args, flag)
	if i < 0 || i+1 >= len(args) || slices.Index(args[i+1:], flag) >= 0 {
		t.Fatalf("%s がちょうど 1 回ではない: %q", flag, args)
	}
	return args[i+1]
}

// haiku (要約役・btw) は家の .claude/CLAUDE.md も外す。家が分からなければ auto memory だけ外す (431)。
func TestHaikuSettings(t *testing.T) {
	if got, want := HaikuSettings("/h"), `{"autoMemoryEnabled":false,"claudeMdExcludes":["/h/.claude/CLAUDE.md"]}`; got != want {
		t.Fatalf("HaikuSettings(/h) = %s\nwant %s", got, want)
	}
	if got, want := HaikuSettings(""), `{"autoMemoryEnabled":false}`; got != want {
		t.Fatalf(`HaikuSettings("") = %s\nwant %s`, got, want)
	}
}

// haiku は --no-session-persistence (transcript を残さない。460 の P3) と --setting-sources project,local と --settings を付けて claude -p を呼ぶ (prompt は標準入力)。
func TestHaikuPassesSettings(t *testing.T) {
	bin := t.TempDir()
	argsFile := filepath.Join(bin, "args")
	claude := filepath.Join(bin, "claude")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\" >>\"" + argsFile + "\"; done\necho 要約\n"
	if err := os.WriteFile(claude, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	out, err := HaikuAsk(claude, t.TempDir(), `{"x":1}`)(context.Background(), "問い")
	if err != nil || strings.TrimSpace(out) != "要約" {
		t.Fatalf("out = %q, err = %v", out, err)
	}
	b, _ := os.ReadFile(argsFile)
	want := []string{"-p", "--model", "haiku", "--no-session-persistence", "--setting-sources", "project,local", "--settings", `{"x":1}`}
	if got := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n"); !slices.Equal(got, want) {
		t.Fatalf("引数 = %q\nwant %q", got, want)
	}
}

// CLAUDE_CONFIG_DIR があれば claude と同じくその下の settings.json を読む。
func TestUserSettingsPath(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if got := UserSettingsPath("/h"); got != "/h/.claude/settings.json" {
		t.Fatalf("既定 = %q", got)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "/c")
	if got := UserSettingsPath("/h"); got != "/c/settings.json" {
		t.Fatalf("CLAUDE_CONFIG_DIR = %q", got)
	}
}

// fakeClaude は PATH の先頭に偽の claude を置く。本物 (commander) と同じく、「-」で始まる引数のうち知らないものを
// オプションと読んで rc=1 で落ち (`error: unknown option`)、それ以外は --bg の最初の行を返す。claude は偽物の絶対パス。
// 受けた引数は argsFile に NUL で区切って足していき (引数に改行が入る)、claude の形を変えうる環境変数は argsFile.env に呼ぶたびに書き直す。
func fakeClaude(t *testing.T) (claude, argsFile string) {
	t.Helper()
	bin := t.TempDir()
	argsFile = filepath.Join(bin, "args")
	script := `#!/bin/sh
for a in "$@"; do printf '%s\000' "$a" >>"` + argsFile + `"; done
env | grep -E '^(DISABLE_|ENABLE_|CLAUDE|ANTHROPIC|TMUX)' | sort >"` + argsFile + `.env"
skip=0
for a in "$@"; do
  if [ $skip = 1 ]; then skip=0; continue; fi
  case "$a" in
    --bg) ;;
    -w|-n|--resume|--model|--effort|--setting-sources|--settings) skip=1 ;;
    -*) echo "error: unknown option '$a'" >&2; exit 1 ;;
  esac
done
echo "backgrounded · ab12 · pc-c-001"
`
	claude = filepath.Join(bin, "claude")
	if err := os.WriteFile(claude, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return claude, argsFile
}

// 回答・追加オーダーの本文が「-」で始まっても (箇条書き)、claude がオプションと読まずに再開できる (462)。本文は削らずに届く。
func TestResumeTextStartingWithDashIsNotReadAsOption(t *testing.T) {
	claude, argsFile := fakeClaude(t)
	text := "- A にする\n- B はやめる"
	id, err := ExecLauncher{Claude: claude}.Resume(context.Background(), "", "sid", t.TempDir(), "pc-c-001", text)
	if err != nil {
		t.Fatalf("「-」で始まる本文で再開が失敗した: %v", err)
	}
	if id != "ab12" {
		t.Fatalf("id = %q", id)
	}
	if b, _ := os.ReadFile(argsFile); !strings.Contains(string(b), text) {
		t.Fatalf("本文がそのまま届いていない: %q", b)
	}
	if _, err := (ExecLauncher{Claude: claude}).Start(context.Background(), t.TempDir(), "pc-c-001", "-x で始まる指示"); err != nil {
		t.Fatalf("「-」で始まる指示で起動が失敗した: %v", err)
	}
}

// claude が rc≠0 で返した / 起動できなかった失敗は ErrRejected (何も立っていない)。--bg の出力が読めないだけのものは違う (立っているかもしれない)。
func TestLauncherClassifiesRejected(t *testing.T) {
	claude, _ := fakeClaude(t)
	if _, err := runClaude(context.Background(), claude, "", "--bogus"); !errors.Is(err, ErrRejected) {
		t.Fatalf("rc=1 の失敗が ErrRejected でない: %v", err)
	}
	if _, err := runClaude(context.Background(), claude, filepath.Join(t.TempDir(), "gone"), "--bg"); !errors.Is(err, ErrRejected) {
		t.Fatalf("消えた cwd (chdir の失敗) が ErrRejected でない: %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runClaude(cancelled, claude, "", "--bg"); err == nil || errors.Is(err, ErrRejected) {
		t.Fatalf("取り消し (dispatcher の終了) を ErrRejected にした: %v", err)
	}
	if _, err := parseBackgrounded("何か別の出力"); errors.Is(err, ErrRejected) {
		t.Fatalf("出力が読めないだけの失敗を ErrRejected にした: %v", err)
	}
}

// fakeNodenv は nodenv の形を偽物で作る: PATH の先頭の shims/claude は cwd から上へ .node-version を探して node の版を選び
// (NODENV_VERSION があればそれ)、versions/<版>/bin/claude を exec する。偽の claude は自分の版と最初の引数を log に書き、--bg の最初の行を返す。
// 返すのは、別の版を選ぶ 2 つの repo と log (464 の発火条件: PATH に shim があり NODENV_VERSION が無い)。
func fakeNodenv(t *testing.T) (repoOld, repoNew, logFile string) {
	t.Helper()
	top := t.TempDir()
	root := filepath.Join(top, ".nodenv")
	logFile = filepath.Join(top, "log")
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for v, cc := range map[string]string{"19.3.0": "2.1.17", "22.11.0": "2.1.273"} {
		write(filepath.Join(root, "versions", v, "bin", "claude"), `#!/bin/sh
if [ "$1" = --version ]; then echo "`+cc+` (Claude Code)"; exit 0; fi
echo "`+cc+` $1" >>"`+logFile+`"
echo "backgrounded · ab12 · pc-c-001"
`)
	}
	write(filepath.Join(root, "version"), "22.11.0\n")
	nodenv := filepath.Join(top, "bin", "nodenv")
	write(nodenv, `#!/bin/sh
v="$NODENV_VERSION"
d="$PWD"
while [ -z "$v" ]; do
  [ -f "$d/.node-version" ] && v=$(cat "$d/.node-version")
  [ "$d" = / ] && break
  d=$(dirname "$d")
done
[ -n "$v" ] || v=$(cat "`+root+`/version")
p="`+root+`/versions/$v/bin/$2"
case "$1" in
  which) echo "$p" ;;
  exec) shift 2; exec "$p" "$@" ;;
esac
`)
	write(filepath.Join(root, "shims", "claude"), "#!/bin/sh\nexec \""+nodenv+"\" exec claude \"$@\"\n")
	repoOld, repoNew = filepath.Join(top, "gx-navi"), filepath.Join(top, "ubiregi-server")
	write(filepath.Join(repoOld, ".node-version"), "19.3.0\n")
	write(filepath.Join(repoNew, ".node-version"), "22.11.0\n")
	t.Setenv("NODENV_VERSION", "")
	t.Setenv("PATH", filepath.Join(root, "shims")+string(os.PathListSeparator)+filepath.Dir(nodenv)+string(os.PathListSeparator)+"/usr/bin:/bin")
	return repoOld, repoNew, logFile
}

// PATH に nodenv の shim があり NODENV_VERSION が無くても、repo ごとに別の版の claude で起動・再開しない (464)。
// 実体は渡した cwd で 1 回だけ解決する。dispatcher を起こした cwd (ここでは古い版を指す repo) には左右されない
// (.node-version の無い cwd = nodenv の既定の 22.11.0)。
func TestLauncherUsesOneClaudeAcrossRepos(t *testing.T) {
	repoOld, repoNew, logFile := fakeNodenv(t)
	t.Chdir(repoOld)
	cl, err := ResolveClaude(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cl.Version != "2.1.273 (Claude Code)" || isShim(cl.Path) {
		t.Fatalf("解決した claude = %+v (nodenv の既定の版の実体のはず)", cl)
	}
	l := ExecLauncher{Claude: cl.Path}
	for _, repo := range []string{repoOld, repoNew} {
		if _, err := l.Start(context.Background(), repo, "pc-c-001", "x"); err != nil {
			t.Fatalf("%s で起動できない: %v", repo, err)
		}
		if _, err := l.Resume(context.Background(), "", "sid", repo, "pc-c-001", "x"); err != nil {
			t.Fatalf("%s で再開できない: %v", repo, err)
		}
	}
	b, _ := os.ReadFile(logFile)
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if !strings.HasPrefix(line, "2.1.273 ") {
			t.Fatalf("解決した版と別の claude で起動・再開した:\n%s", b)
		}
	}
}

// 解決できない形は dispatcher を起動させない (素の名前に倒さない)。shim を置いた版管理が実体を返せない / 返したものがまた shim。
func TestResolveClaudeRefusesUnresolvableShim(t *testing.T) {
	fakeNodenv(t)
	shims := strings.Split(os.Getenv("PATH"), string(os.PathListSeparator))[0]
	t.Setenv("PATH", shims+string(os.PathListSeparator)+"/usr/bin:/bin") // nodenv が見つからない
	if cl, err := ResolveClaude(context.Background(), t.TempDir()); err == nil {
		t.Fatalf("nodenv が無いのに解決した: %+v", cl)
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "nodenv"), []byte("#!/bin/sh\necho "+filepath.Join(shims, "claude")+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shims+string(os.PathListSeparator)+bin+string(os.PathListSeparator)+"/usr/bin:/bin")
	if cl, err := ResolveClaude(context.Background(), t.TempDir()); err == nil {
		t.Fatalf("shim を実体として受けた: %+v", cl)
	}
}

// 版管理の本体への symlink を shim に置く形 (mise) も shim として辿る。shim でない symlink (native の ~/.local/bin/claude → versions/<版>) は
// 版つきの先に固定しない (自動更新が古い版を消すと、動き続けている dispatcher の起動がすべて失敗する)。
func TestResolveClaudeSymlinks(t *testing.T) {
	top := t.TempDir()
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	link := func(target, path string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
	}
	real := filepath.Join(top, "real", "claude")
	write(real, "#!/bin/sh\necho '2.1.300 (Claude Code)'\n")
	mise := filepath.Join(top, "bin", "mise")
	write(mise, "#!/bin/sh\n[ \"$1\" = which ] && echo "+real+"\n")
	shims := filepath.Join(top, "mise", "shims")
	link(mise, filepath.Join(shims, "claude"))
	t.Setenv("PATH", shims+string(os.PathListSeparator)+filepath.Dir(mise)+string(os.PathListSeparator)+"/usr/bin:/bin")
	if cl, err := ResolveClaude(context.Background(), t.TempDir()); err != nil || cl.Path != real {
		t.Fatalf("mise の形の shim を辿らない: %+v %v", cl, err)
	}
	other := filepath.Join(top, "other", "bin") // shims の外から shim を指す symlink (~/bin/claude → shim)
	link(filepath.Join(shims, "claude"), filepath.Join(other, "claude"))
	t.Setenv("PATH", other+string(os.PathListSeparator)+filepath.Dir(mise)+string(os.PathListSeparator)+"/usr/bin:/bin")
	if cl, err := ResolveClaude(context.Background(), t.TempDir()); err != nil || cl.Path != real {
		t.Fatalf("shim を指す symlink を辿らない: %+v %v", cl, err)
	}
	local := filepath.Join(top, "local", "bin")
	link(real, filepath.Join(local, "claude"))
	t.Setenv("PATH", local+string(os.PathListSeparator)+"/usr/bin:/bin")
	if cl, err := ResolveClaude(context.Background(), t.TempDir()); err != nil || cl.Path != filepath.Join(local, "claude") || cl.Version != "2.1.300 (Claude Code)" {
		t.Fatalf("shim でない symlink の先に固定した: %+v %v", cl, err)
	}
}

// 本物の stopThenRun が、前の session を止める段の拒否にだけ errStopFailed を付ける (581。role.go は errStopFailed の拒否を
// 起動し直しの拒否と数えない。fakeLauncher は自分で付けて返すので、この配線は dispatcher のテストからは見えない)。
func TestRestartMarksOnlyStopFailure(t *testing.T) {
	for name, c := range map[string]struct {
		script   string
		stopFail bool
	}{
		"止める段で拒否":    {script: "#!/bin/sh\nexit 1\n", stopFail: true},
		"止めた後の起動で拒否": {script: "#!/bin/sh\n[ \"$1\" = stop ] && exit 0\nexit 1\n", stopFail: false},
	} {
		t.Run(name, func(t *testing.T) {
			claude := filepath.Join(t.TempDir(), "claude")
			if err := os.WriteFile(claude, []byte(c.script), 0o700); err != nil {
				t.Fatal(err)
			}
			_, err := ExecLauncher{Claude: claude}.Restart(context.Background(), "old-1", t.TempDir(), "pc-pm-x", "指示")
			if !errors.Is(err, ErrRejected) || errors.Is(err, errStopFailed) != c.stopFail {
				t.Fatalf("err = %v (ErrRejected: %v / errStopFailed: %v、期待 %v)", err, errors.Is(err, ErrRejected), errors.Is(err, errStopFailed), c.stopFail)
			}
		})
	}
}

// PG・PM・取り込みの係の --model / --effort は pro-con の設定 (状態の置き場の settings.json) から起動・再開・起動し直しのたびに読む。
// 設定が無い・壊れているなら既定 (Opus 5.5 / medium) で起動する (Claude Code の既定やユーザーの settings.json の model に任せない)。
func TestLauncherModelAndEffortFromSettings(t *testing.T) {
	user := writeSettings(t, `{"model": "sonnet", "effortLevel": "max"}`) // ユーザーの settings.json の model / effortLevel は写さない
	dir := t.TempDir()
	l := ExecLauncher{UserSettings: user, StateDir: dir}
	lifecycles := []struct {
		kind string
		args func() []string
	}{
		{"起動", func() []string { return l.startArgs("n", "p") }},
		{"再開", func() []string { return l.resumeArgs("sid", "n", "t") }},
		{"起動し直し", func() []string { return l.restartArgs("n", "p") }},
	}
	for _, c := range []struct {
		name, settings, want string // settings が空なら settings.json を置かない
	}{
		{"設定なし", "", defaultModelEffort},
		{"設定の値", `{"model": "claude-fable-5-1", "effort": "high"}`, "claude-fable-5-1 high"},
		{"選べない値は既定に倒す", `{"model": "opus", "effort": "hgih"}`, defaultModelEffort}, // claude が引数で弾くと、止めた後の起動に失敗する
		{"壊れた設定は既定", `{"model": `, defaultModelEffort},
	} {
		t.Run(c.name, func(t *testing.T) {
			writeProConSettings(t, dir, c.settings)
			for _, lc := range lifecycles {
				args := lc.args()
				if got := flagValue(t, args, "--model") + " " + flagValue(t, args, "--effort"); got != c.want {
					t.Errorf("%sの --model / --effort = %q, want %q", lc.kind, got, c.want)
				}
			}
		})
	}
}

// writeProConSettings は pro-con の状態の置き場 dir に settings.json を書く (body が空なら消す)。
func writeProConSettings(t *testing.T, dir, body string) {
	t.Helper()
	p := filepath.Join(dir, store.SettingsFile)
	if body == "" {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		return
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
