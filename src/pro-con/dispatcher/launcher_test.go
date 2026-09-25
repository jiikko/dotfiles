package dispatcher

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeSettings(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// 起動・再開の引数に、ユーザーの settings.json の language だけを --settings で渡す (461。hook・許可は持ち込まない)。
func TestLauncherArgsPassOnlyLanguage(t *testing.T) {
	p := writeSettings(t, `{"language": "日本語", "hooks": {"Stop": []}, "permissions": {"allow": ["Bash"]}, "model": "opus"}`)
	settings := languageSettings(p)
	if settings != `{"language":"日本語"}` {
		t.Fatalf("--settings の中身 = %q (言語だけのはず)", settings)
	}
	l := ExecLauncher{UserSettings: p}
	start := l.startArgs("pg-1", "依頼")
	want := []string{"--bg", "-w", "pg-1", "-n", "pg-1", "--setting-sources", "project,local", "--settings", `{"language":"日本語"}`, "依頼"}
	if !slices.Equal(start, want) {
		t.Fatalf("起動の引数 = %q\nwant %q", start, want)
	}
	resume := l.resumeArgs("sid", "回答")
	want = []string{"--bg", "--resume", "sid", "--setting-sources", "project,local", "--settings", `{"language":"日本語"}`, "回答"}
	if !slices.Equal(resume, want) {
		t.Fatalf("再開の引数 = %q\nwant %q", resume, want)
	}
}

// 読めない・壊れている・language が無い / 文字列でない / 空なら --settings を付けない (起動は止めない。値を固定で補わない)。
func TestLauncherArgsOmitSettingsWithoutLanguage(t *testing.T) {
	cases := map[string]string{
		"パスが空":        "",
		"ファイルが無い":     filepath.Join(t.TempDir(), "missing.json"),
		"壊れた JSON":    writeSettings(t, `{"language": `),
		"language 無し": writeSettings(t, `{"model": "opus"}`),
		"文字列でない":      writeSettings(t, `{"language": 1}`),
		"空":           writeSettings(t, `{"language": "  "}`),
	}
	for name, p := range cases {
		if s := languageSettings(p); s != "" {
			t.Errorf("%s: languageSettings = %q (渡さないはず)", name, s)
		}
		l := ExecLauncher{UserSettings: p}
		if args := l.startArgs("n", "p"); slices.Contains(args, "--settings") {
			t.Errorf("%s: 起動の引数に --settings が付いた: %q", name, args)
		}
		if args := l.resumeArgs("sid", "t"); slices.Contains(args, "--settings") {
			t.Errorf("%s: 再開の引数に --settings が付いた: %q", name, args)
		}
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
// オプションと読んで rc=1 で落ち (`error: unknown option`)、それ以外は --bg の最初の行を返す。受けた引数は argsFile に書く。claude は偽物の絶対パス。
func fakeClaude(t *testing.T) (claude, argsFile string) {
	t.Helper()
	bin := t.TempDir()
	argsFile = filepath.Join(bin, "args")
	script := `#!/bin/sh
for a in "$@"; do printf '%s\n' "$a" >>"` + argsFile + `"; done
skip=0
for a in "$@"; do
  if [ $skip = 1 ]; then skip=0; continue; fi
  case "$a" in
    --bg) ;;
    -w|-n|--resume|--setting-sources|--settings) skip=1 ;;
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
	id, err := ExecLauncher{Claude: claude}.Resume(context.Background(), "", "sid", t.TempDir(), text)
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
// 実体は dispatcher の cwd で 1 回だけ解決する (ここでは .node-version の無い cwd = nodenv の既定の 22.11.0)。
func TestLauncherUsesOneClaudeAcrossRepos(t *testing.T) {
	repoOld, repoNew, logFile := fakeNodenv(t)
	t.Chdir(t.TempDir())
	cl, err := ResolveClaude(context.Background())
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
		if _, err := l.Resume(context.Background(), "", "sid", repo, "x"); err != nil {
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
	t.Chdir(t.TempDir())
	shims := strings.Split(os.Getenv("PATH"), string(os.PathListSeparator))[0]
	t.Setenv("PATH", shims+string(os.PathListSeparator)+"/usr/bin:/bin") // nodenv が見つからない
	if cl, err := ResolveClaude(context.Background()); err == nil {
		t.Fatalf("nodenv が無いのに解決した: %+v", cl)
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "nodenv"), []byte("#!/bin/sh\necho "+filepath.Join(shims, "claude")+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shims+string(os.PathListSeparator)+bin+string(os.PathListSeparator)+"/usr/bin:/bin")
	if cl, err := ResolveClaude(context.Background()); err == nil {
		t.Fatalf("shim を実体として受けた: %+v", cl)
	}
}
