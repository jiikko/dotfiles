package setup

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWiredName(t *testing.T) {
	for in, want := range map[string]string{"air5.local": "air5.wired.local", "M2MacMini.local": "m2macmini.wired.local"} {
		if got, err := WiredName(in); err != nil || got != want {
			t.Errorf("WiredName(%q) = %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "air5", "a b.local", "-x.local", "a.b.local", "x'y.local", "air5.local.evil"} {
		if got, err := WiredName(in); err == nil {
			t.Errorf("WiredName(%q) = %q (拒むはず)", in, got)
		}
	}
	for _, a := range []NameAdvert{{"air5.wired.local", "fe80::1"}, {"air5.local", "192.168.100.11"}, {"a$(id).wired.local", "192.168.100.11"}} {
		if a.Validate() == nil {
			t.Errorf("使えない広告を通した: %+v", a)
		}
	}
}

// wiredSandbox は偽の launchctl だけが見える PATH と、使い捨ての HOME を作る。偽の launchctl は引数を log に書き、print は dir/state の
// 中身を返す (無ければ本物と同じく失敗する)。bootout は dir/bootout_rc の値で抜ける (無ければ 0)。
// 🚨 本物の launchctl は PATH に入れない (/bin にある)。走らせる前に sandbox に解決することを確かめる
// (_claude/rules/sandbox-real-destructive-test-apis.md: 拒否は実行の前)。plutil は本物 (/usr/bin。ファイルを読むだけ)。
func wiredSandbox(t *testing.T) (env []string, home, dir string) {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bin, home := filepath.Join(d, "bin"), filepath.Join(d, "home")
	for _, p := range []string{bin, home} {
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	fake := "#!/bin/sh\necho \"$@\" >> '" + d + "/log'\n" +
		"case \"$1\" in\nprint) [ -f '" + d + "/state' ] || exit 113; cat '" + d + "/state';;\n" +
		"bootout) [ -f '" + d + "/bootout_rc' ] && exit $(cat '" + d + "/bootout_rc');;\nesac\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "launchctl"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"mkdir", "mv", "rm", "cat"} { // /bin は launchctl があるので PATH に入れず、使う道具だけを並べる
		if err := os.Symlink("/bin/"+c, filepath.Join(bin, c)); err != nil {
			t.Fatal(err)
		}
	}
	env = []string{"PATH=" + bin + ":/usr/bin", "HOME=" + home}
	cmd := exec.Command("/bin/sh", "-c", "command -v launchctl")
	cmd.Env = env
	if out, err := cmd.Output(); err != nil || strings.TrimSpace(string(out)) != filepath.Join(bin, "launchctl") {
		t.Fatalf("launchctl が sandbox の外に解決した (走らせない): %q %v", out, err)
	}
	return env, home, d
}

// printOut は本物の launchctl print の形 (実測): 1 段目の state と arguments。1 段深い所にも state = active が出る。
func printOut(state string, a NameAdvert) string {
	var b strings.Builder
	b.WriteString(WiredNameLabel + " = {\n\tactive count = 1\n\tstate = " + state + "\n\n\targuments = {\n")
	for _, x := range a.args() {
		b.WriteString("\t\t" + x + "\n")
	}
	b.WriteString("\t}\n\n\tendpoints = {\n\t\t\"x\" = {\n\t\t\tstate = active\n\t\t}\n\t}\n}\n")
	return b.String()
}

func runScript(t *testing.T, env []string, script string) (string, error) {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// 入れる → 状態を読む → 消す を、偽の launchctl と本物の plutil で通す。状態は走っている job の引数から読む。
func TestNameAdvertScripts(t *testing.T) {
	env, home, dir := wiredSandbox(t)
	a := NameAdvert{Name: "air5.wired.local", IP: "192.168.100.11"}
	read := func() NameState {
		t.Helper()
		st, err := ReadNameState(context.Background(), func(_ context.Context, argv []string) (string, error) {
			return runScript(t, env, argv[2])
		})
		if err != nil {
			t.Fatal(err)
		}
		return st
	}
	setPrint := func(s string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "state"), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if st := read(); st != (NameState{}) {
		t.Fatalf("入れる前の状態 = %+v (none のはず)", st)
	}
	if out, err := runScript(t, env, a.InstallScript()); err != nil {
		t.Fatalf("入れられない: %v %s", err, out)
	}
	plist := filepath.Join(home, "Library", "LaunchAgents", WiredNameLabel+".plist")
	if out, err := exec.Command("/usr/bin/plutil", "-lint", plist).CombinedOutput(); err != nil {
		t.Fatalf("plist が壊れている: %v %s", err, out)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "log"))
	if !strings.Contains(string(b), "bootstrap gui/") || !strings.Contains(string(b), plist) {
		t.Errorf("launchctl bootstrap を打っていない: %q", b)
	}
	if st := read(); st.Running || st.Name != "" {
		t.Errorf("job が無いのに = %+v (plist だけで走っていると読まない)", st)
	}
	setPrint(printOut("not running", a))
	if st := read(); st.Advertises(a) {
		t.Errorf("止まった job を広告中と読んだ: %+v", st)
	}
	setPrint(printOut("running", a))
	if st := read(); !st.Advertises(a) {
		t.Errorf("走っている広告を読めない: %+v", st)
	}
	// 入れ直しの bootstrap が失敗し、新しい plist の横で古い引数の job が走っている: 新しい IP で済みと読まない
	newer := NameAdvert{Name: a.Name, IP: "192.168.100.22"}
	if out, err := runScript(t, env, newer.InstallScript()); err != nil {
		t.Fatalf("入れ直せない: %v %s", err, out)
	}
	if st := read(); st.Advertises(newer) || !st.Advertises(a) {
		t.Errorf("走っている job の引数でなく plist を読んだ: %+v", st)
	}
	// 別の名前を広告している相手は消さない (走っている job で見る / job が無ければ plist で見る)
	for _, jobLoaded := range []bool{true, false} {
		if !jobLoaded {
			if err := os.Remove(filepath.Join(dir, "state")); err != nil {
				t.Fatal(err)
			}
		}
		out, err := runScript(t, env, RemoveScript("other.wired.local"))
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != ExitOtherName || !strings.Contains(out, "別の名前") {
			t.Errorf("job=%v: 別の名前の広告を消しにいった: %v %s", jobLoaded, err, out)
		}
		if _, err := os.Stat(plist); err != nil {
			t.Errorf("job=%v: 別の名前のとき plist を消した: %v", jobLoaded, err)
		}
	}
	setPrint(printOut("running", a))
	// bootout が「走っていない (3)」以外で落ちたら、止めたことにしない
	if err := os.WriteFile(filepath.Join(dir, "bootout_rc"), []byte("125"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := runScript(t, env, RemoveScript(a.Name)); err == nil || !strings.Contains(out, "止められない") {
		t.Errorf("bootout の失敗を成功にした: %v %s", err, out)
	}
	if _, err := os.Stat(plist); err != nil {
		t.Errorf("止められなかったのに plist を消した (次のログインまで走り続け、その後は戻らない): %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bootout_rc"), []byte("3"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := runScript(t, env, RemoveScript(a.Name)); err != nil {
		t.Fatalf("消せない: %v %s", err, out)
	}
	if _, err := os.Stat(plist); !os.IsNotExist(err) {
		t.Errorf("plist が残った: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "state")); err != nil {
		t.Fatal(err)
	}
	if st := read(); st != (NameState{}) {
		t.Errorf("消した後の状態 = %+v", st)
	}
}

// 読み込まれていない (GUI のドメインが無い・止まった後) ときは bootout を打たずに plist を消す (打つと 3 以外で落ち、plist が残った)。
// 読み込まれているのに読めない形の print は「読めない」(済みにも止まっているにもしない)。1 段深い state が前にあっても 1 段目を読む。
func TestNameAdvertScriptsEdges(t *testing.T) {
	env, home, dir := wiredSandbox(t)
	a := NameAdvert{Name: "air5.wired.local", IP: "192.168.100.11"}
	if out, err := runScript(t, env, a.InstallScript()); err != nil {
		t.Fatalf("入れられない: %v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, "bootout_rc"), []byte("125"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := runScript(t, env, RemoveScript(a.Name)); err != nil {
		t.Errorf("読み込まれていないのに bootout で落ちた: %v %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(home, "Library", "LaunchAgents", WiredNameLabel+".plist")); !os.IsNotExist(err) {
		t.Errorf("plist が残った: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "log")); strings.Contains(string(b), "bootout") {
		t.Errorf("読み込まれていないのに bootout を打った: %q", b)
	}
	read := func(print string) (NameState, error) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "state"), []byte(print), 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := runScript(t, env, StatusScript())
		if err != nil {
			t.Fatal(err)
		}
		return ParseStatus(out)
	}
	if st, err := read("weird\n"); err == nil {
		t.Errorf("読めない形の print を %+v と読んだ", st)
	}
	nested := strings.Replace(printOut("not running", a), "\tactive count = 1\n", "\tactive count = 1\n\tsockets = {\n\t\tstate = running\n\t}\n", 1)
	if st, err := read(nested); err != nil || st.Running {
		t.Errorf("1 段深い state を読んだ: %+v %v", st, err)
	}
}

func TestParseStatus(t *testing.T) {
	for _, bad := range []string{"", "running\nx", "weird\na\nb", "none\nx"} {
		if _, err := ParseStatus(bad); err == nil {
			t.Errorf("読めない形 %q を読めたことにした", bad)
		}
	}
	if st, err := ParseStatus("stopped\n\n\n"); err != nil || st != (NameState{}) {
		t.Errorf("走っていない形 = %+v, %v", st, err)
	}
}
