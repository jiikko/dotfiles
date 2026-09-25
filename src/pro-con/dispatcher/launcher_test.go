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

// 起動・再開の引数に、ユーザーの settings.json の language と auto memory の無効化だけを --settings で渡す (461 / 431。hook・許可は持ち込まない)。
func TestLauncherArgsPassLanguageAndNoAutoMemory(t *testing.T) {
	p := writeSettings(t, `{"language": "日本語", "hooks": {"Stop": []}, "permissions": {"allow": ["Bash"]}, "model": "opus"}`)
	const settings = `{"autoMemoryEnabled":false,"language":"日本語"}`
	l := ExecLauncher{UserSettings: p}
	start := l.startArgs("pg-1", "依頼")
	want := []string{"--bg", "-w", "pg-1", "-n", "pg-1", "--setting-sources", "project,local", "--settings", settings, "依頼"}
	if !slices.Equal(start, want) {
		t.Fatalf("起動の引数 = %q\nwant %q", start, want)
	}
	resume := l.resumeArgs("sid", "pg-1", "回答")
	want = []string{"--bg", "--resume", "sid", "-n", "pg-1", "--setting-sources", "project,local", "--settings", settings, "回答"}
	if !slices.Equal(resume, want) {
		t.Fatalf("再開の引数 = %q\nwant %q", resume, want)
	}
}

// 読めない・壊れている・language が無い / 文字列でない / 空なら language を渡さない (起動は止めない。値を固定で補わない)。
// auto memory の無効化は language と関係なく渡す。~/.claude/CLAUDE.md は PG・PM には残す (431)。
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
		for kind, args := range map[string][]string{"起動": l.startArgs("n", "p"), "再開": l.resumeArgs("sid", "n", "t")} {
			i := slices.Index(args, "--settings")
			if i < 0 || i+1 >= len(args) || args[i+1] != `{"autoMemoryEnabled":false}` {
				t.Errorf("%s: %sの引数 = %q (--settings は auto memory の無効化だけのはず)", name, kind, args)
			}
		}
	}
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

// haiku は --setting-sources project,local と --settings を付けて claude -p を呼ぶ (prompt は標準入力)。
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
	want := []string{"-p", "--model", "haiku", "--setting-sources", "project,local", "--settings", `{"x":1}`}
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
