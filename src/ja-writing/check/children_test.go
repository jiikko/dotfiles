package check

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"ja-writing/cmdenv"
	"ja-writing/internal/pyfs"
	"ja-writing/internal/pyunicode"
)

// 締め切りを使い切ってから動かす子は、起動せずに時間切れにする (本家の remaining <= 0 の枝)。
func TestChildrenDoNotStartAfterDeadline(t *testing.T) {
	h := newHangFixture(t)
	clock := []float64{0, 100}
	h.rt.Monotonic = func() float64 {
		now := clock[0]
		if len(clock) > 1 {
			clock = clock[1:]
		}
		return now
	}
	kids := newChildren(h.rt, 1, h.dir, h.rt.ChildEnv(cmdenv.Env{Cwd: h.dir, Environ: h.environ}))
	kids.start("lint", []string{testExecutable(t), "lint"})
	results, err := kids.wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r := results["lint"]
	if r.status != "timeout" || r.err != "時間切れ（--timeout の秒数を使い切った）" || r.code != nil {
		t.Fatalf("結果 %+v", r)
	}
	if _, err := os.Stat(h.pidFile); err == nil {
		t.Fatal("締め切りの後に子を起動した")
	}
}

// 止めた後に頼まれた子は起動しない (止める間に別の子を起こさない)。
func TestChildrenDoNotStartAfterStop(t *testing.T) {
	h := newHangFixture(t)
	kids := newChildren(h.rt, 60, h.dir, h.rt.ChildEnv(cmdenv.Env{Cwd: h.dir, Environ: h.environ}))
	kids.stop()
	kids.start("diff", []string{testExecutable(t), "diff"})
	results, err := kids.wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r := results["diff"]; r.status != "error" || r.err != "子プロセスを動かす前に止められました" {
		t.Fatalf("結果 %+v", r)
	}
	if _, err := os.Stat(h.pidFile); err == nil {
		t.Fatal("止めた後に子を起動した")
	}
}

// 上限を超える秒数は、待つ時間の上限なしにする (本家の _wait_seconds。time.Duration にすると溢れる)。
func TestWaitSecondsDropsHugeLimits(t *testing.T) {
	if got := waitSeconds(waitLimit + 1); got >= 0 {
		t.Fatalf("waitSeconds(上限+1) = %v", got)
	}
	if got := waitSeconds(waitLimit); got != waitLimit {
		t.Fatalf("waitSeconds(上限) = %v", got)
	}
}

// 親の中の仕事 (資料との照合) が panic しても、動いている子を止めてから panic を流し直す (本家の except BaseException の枝)。
func TestChildrenStopWhenParentWorkPanics(t *testing.T) {
	h := newHangFixture(t)
	kids := newChildren(h.rt, 60, h.dir, h.rt.ChildEnv(cmdenv.Env{Cwd: h.dir, Environ: h.environ}))
	pid := 0
	recovered := func() (r any) {
		defer func() { r = recover() }()
		_, _ = kids.run(context.Background(), []childCommand{{"lint", []string{testExecutable(t), "lint"}}}, func() {
			pid = h.childPID(t)
			panic("照合の途中の panic")
		})
		return nil
	}()
	if recovered != "照合の途中の panic" {
		t.Fatalf("panic が流し直されない: %v", recovered)
	}
	if alive(pid) {
		t.Fatalf("panic の後に子 %d が止まっていない", pid)
	}
}

// 本家の Popen が起動の前に断る形は、子を起動せずに本家と同じ文にする。文は python3.15 (3.15.0) の subprocess.Popen で実測した:
// NUL はどの引数でも「ValueError: embedded null byte」(作業フォルダが無いときも同じ)。argv の U+DC80..U+DCFF でない単独のサロゲートは
// 「UnicodeEncodeError: 'utf-8' codec can't encode character …」(環境変数の名前と値・作業フォルダも同じ)。断りの順は、環境変数を前から
// bytes にする (名前が値より先)、argv を前から (1 つの字の並びでは bytes にするのが NUL より先)、環境変数の NUL、作業フォルダ。
func TestChildrenRefuseWhatPopenRefuses(t *testing.T) {
	h := newHangFixture(t)
	exe := testExecutable(t)
	environ := h.rt.ChildEnv(cmdenv.Env{Cwd: h.dir, Environ: h.environ})
	const null = "ValueError: embedded null byte"
	d800 := pyunicode.FromRunes([]rune{0xD800})
	d801 := pyunicode.FromRunes([]rune{0xD801})
	encode := func(char string, position int) string {
		return fmt.Sprintf("UnicodeEncodeError: 'utf-8' codec can't encode character '\\%s' in position %d: surrogates not allowed", char, position)
	}
	for _, c := range []struct {
		name, cwd string
		argv, env []string
		want      string
	}{
		{"引数の NUL", h.dir, []string{exe, "lint", "a\x00b"}, environ, null},
		{"実行ファイルの NUL", h.dir, []string{exe + "\x00", "lint"}, environ, null},
		{"環境変数の値の NUL", h.dir, []string{exe, "lint"}, append(slices.Clone(environ), "JA_NUL=a\x00b"), null},
		{"環境変数の名前の NUL", h.dir, []string{exe, "lint"}, append(slices.Clone(environ), "JA\x00NUL=1"), null},
		{"作業フォルダの NUL", h.dir + "/no\x00where", []string{exe, "lint"}, environ, null},
		{"作業フォルダが無いときの引数の NUL", h.dir + "/nowhere", []string{exe, "lint", "a\x00b"}, environ, null},
		{"引数のサロゲート", h.dir, []string{exe, "lint", "x" + d800}, environ, encode("ud800", 1)},
		{"続いたサロゲート", h.dir, []string{exe, "lint", "ab" + d800 + d801 + "c"}, environ,
			"UnicodeEncodeError: 'utf-8' codec can't encode characters in position 2-3: surrogates not allowed"},
		{"前の引数の NUL と後の引数のサロゲート", h.dir, []string{exe, "a\x00", d800}, environ, null},
		{"前の引数のサロゲートと後の引数の NUL", h.dir, []string{exe, d800, "a\x00"}, environ, encode("ud800", 0)},
		{"同じ引数の NUL の後のサロゲート", h.dir, []string{exe, "lint", "\x00" + d800}, environ, encode("ud800", 1)},
		{"環境変数の NUL と引数のサロゲート", h.dir, []string{exe, "lint", d801}, append(slices.Clone(environ), "JA_NUL=\x00"), encode("ud801", 0)},
		{"作業フォルダの NUL と引数のサロゲート", h.dir + "/\x00", []string{exe, "lint", d801}, environ, encode("ud801", 0)},
		{"作業フォルダが無いときの引数のサロゲート", h.dir + "/nowhere", []string{exe, "lint", d801}, environ, encode("ud801", 0)},
		{"環境変数の値のサロゲートと引数のサロゲート", h.dir, []string{exe, "lint", d801}, append(slices.Clone(environ), "JA="+d800), encode("ud800", 0)},
		{"環境変数の名前のサロゲート", h.dir, []string{exe, "lint"}, append(slices.Clone(environ), "J"+d800+"=1"), encode("ud800", 1)},
		{"環境変数の名前と値のサロゲート", h.dir, []string{exe, "lint"}, append(slices.Clone(environ), "A"+d800+"="+d801), encode("ud800", 1)},
		{"前の環境変数の値と後の環境変数の名前のサロゲート", h.dir, []string{exe, "lint"}, append(slices.Clone(environ), "A="+d800, "B"+d801+"=1"), encode("ud800", 0)},
		{"前の環境変数の NUL と後の環境変数のサロゲート", h.dir, []string{exe, "lint"}, append(slices.Clone(environ), "A=\x00", "B="+d801), encode("ud801", 0)},
		{"環境変数のサロゲートと実行ファイルのサロゲート", h.dir, []string{exe + d801, "lint"}, append(slices.Clone(environ), "A="+d800), encode("ud800", 0)},
		{"作業フォルダのサロゲート", h.dir + "/a" + d800 + d801 + "b", []string{exe, "lint"}, environ,
			fmt.Sprintf("UnicodeEncodeError: 'utf-8' codec can't encode characters in position %d-%d: surrogates not allowed", pyunicode.RuneCount(h.dir)+2, pyunicode.RuneCount(h.dir)+3)},
		{"作業フォルダのサロゲートと引数の NUL", h.dir + "/" + d800, []string{exe, "lint", "a\x00"}, environ, null},
		{"作業フォルダのサロゲートと環境変数の NUL", h.dir + "/" + d800, []string{exe, "lint"}, append(slices.Clone(environ), "A=\x00"), null},
		{"作業フォルダのサロゲートと実行ファイルのサロゲート", h.dir + "/" + d800, []string{exe + d801, "lint"}, environ, encode("ud801", pyunicode.RuneCount(exe))},
		{"作業フォルダの NUL の後のサロゲート", h.dir + "/\x00" + d800, []string{exe, "lint"}, environ, encode("ud800", pyunicode.RuneCount(h.dir)+2)},
	} {
		kids := newChildren(h.rt, 60, c.cwd, c.env)
		kids.start("lint", c.argv)
		results, err := kids.wait(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		r := results["lint"]
		if r.status != "error" || r.code != nil || r.stderr != "" || r.err != "子プロセスを動かせません（"+c.want+"）" {
			t.Errorf("%s: 結果 %+v", c.name, r)
		}
		if _, err := os.Stat(h.pidFile); err == nil {
			t.Fatalf("%s: 断るはずの子を起動した", c.name)
		}
	}
}

// argv の U+DC80..U+DCFF のサロゲート (記録の JSON の "\udcff") は、本家の os.fsencode と同じく下位 8 ビットの 1 バイトで子に渡す
// (python3.15 (3.15.0) の Popen で "x\udcff" を渡した子が受け取るのは b"x\xff")。WTF-8 の 3 バイトのままでは渡さない。
func TestChildrenPassEscapedSurrogatesAsBytes(t *testing.T) {
	h := newHangFixture(t)
	out := h.dir + "/argv.bin"
	arg := "a" + pyunicode.FromRunes([]rune{0xDCFF, 0xDC80}) + "b"
	kids := newChildren(h.rt, 60, h.dir, h.rt.ChildEnv(cmdenv.Env{Cwd: h.dir, Environ: h.environ}))
	kids.start("lint", []string{"/bin/sh", "-c", `printf %s "$1" > "$2"`, "sh", arg, out})
	results, err := kids.wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r := results["lint"]; r.status != "ran" {
		t.Fatalf("結果 %+v", r)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "a\xff\x80b" {
		t.Fatalf("子が受け取った引数 % x、本家は 61 ff 80 62", got)
	}
}

// 環境変数の名前と値の U+DC80..U+DCFF のサロゲートも、argv と同じく 1 バイトで子に渡す (python3.15 (3.15.0) の Popen で env={"JA": "a\udcffb"}
// を渡した子の $JA は b"a\xffb"、env={"J\udcff": "1"} の子の env には b"J\xff=1")。
func TestChildrenPassEscapedSurrogatesInEnvironmentAsBytes(t *testing.T) {
	h := newHangFixture(t)
	out := h.dir + "/env.bin"
	dcff := pyunicode.FromRunes([]rune{0xDCFF})
	environ := append(h.rt.ChildEnv(cmdenv.Env{Cwd: h.dir, Environ: h.environ}), "JA=a"+dcff+"b", "J"+dcff+"=1", "OUT="+out)
	kids := newChildren(h.rt, 60, h.dir, environ)
	kids.start("lint", []string{"/bin/sh", "-c", `{ printf %s "$JA"; printf '\n'; /usr/bin/env; } > "$OUT"`})
	results, err := kids.wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r := results["lint"]; r.status != "ran" {
		t.Fatalf("結果 %+v", r)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(got), "\n")
	if lines[0] != "a\xffb" {
		t.Errorf("子の $JA は % x、本家は 61 ff 62", lines[0])
	}
	if !slices.Contains(lines[1:], "J\xff=1") {
		t.Errorf("子の環境に J\\xff=1 が無い: %q", lines[1:])
	}
}

// 子に渡すバイト列: 作り直した値 (記録の JSON の "\udcff" など) は os.fsencode と同じバイトにし、OS から受けた環境変数 (cmdenv.FromProcess が
// os.fsdecode と同じ規則で読んだ字) は受けたバイトに戻す。作業フォルダは OS のバイトのまま来る (UTF-8 として読めないバイトはそのまま)。
// nil の環境変数は nil のまま (exec で今の環境を引き継ぐ形。本家の env=None)。
func TestPopenEncodeKeepsOSValues(t *testing.T) {
	dcff := pyunicode.FromRunes([]rune{0xDCFF})
	fromOS := func(environ ...string) []string {
		out := make([]string, len(environ))
		for i, entry := range environ {
			out[i] = pyfs.Decode(entry)
		}
		return out
	}
	for _, c := range []struct {
		name    string
		env     []string
		cwd     string
		wantEnv []string
		wantCwd string
	}{
		{"UTF-8 の値", fromOS("LANG=ja_JP.UTF-8", "JA=日本語", "EMPTY="), "/tmp/日本語", []string{"LANG=ja_JP.UTF-8", "JA=日本語", "EMPTY="}, "/tmp/日本語"},
		{"UTF-8 として読めない生のバイト", fromOS("JA=a\xff\x80b", "J\xfe=1", "CUT=\xe6\x97"), "/tmp/\xffx", []string{"JA=a\xff\x80b", "J\xfe=1", "CUT=\xe6\x97"}, "/tmp/\xffx"},
		{"WTF-8 のサロゲートの形の生のバイト", fromOS("D800=\xed\xa0\x80", "DCFF=\xed\xb3\xbf", "J\xed\xbf\xbf=1"), "/", []string{"D800=\xed\xa0\x80", "DCFF=\xed\xb3\xbf", "J\xed\xbf\xbf=1"}, "/"},
		{"= の無い並び", []string{"NOVALUE"}, "/", []string{"NOVALUE"}, "/"},
		{"値の中の =", []string{"A=b=c"}, "/", []string{"A=b=c"}, "/"},
		{"作り直したサロゲート", []string{"JA=a" + dcff + "b"}, "/tmp/" + dcff, []string{"JA=a\xffb"}, "/tmp/\xff"},
		{"nil の環境変数", nil, "/", nil, "/"},
	} {
		got, exc := popenEncode([]string{"/bin/echo"}, c.env, c.cwd)
		if exc != nil {
			t.Errorf("%s: 断った %v", c.name, exc)
			continue
		}
		if !slices.Equal(got.env, c.wantEnv) || (got.env == nil) != (c.wantEnv == nil) || got.cwd != c.wantCwd {
			t.Errorf("%s: env %q cwd %q、期待は env %q cwd %q", c.name, got.env, got.cwd, c.wantEnv, c.wantCwd)
		}
	}
}

// OS から受けた環境変数は、本番の経路 (cmdenv.FromProcess と DefaultRuntime の ChildEnv) を通して、受けたバイトのまま子に渡る。
// python3.15 (3.15.0) の Popen(env=dict(os.environ)) で、親が受けた b"\xed\xa0\x80"・b"\xed\xb3\xbf"・b"\xff" は、子の os.environb でも同じバイト。
func TestChildrenPassOSEnvironmentBytesUnchanged(t *testing.T) {
	h := newHangFixture(t)
	values := []string{"\xed\xa0\x80", "\xed\xb3\xbf", "a\xffb", "\xed\xbf\xbfx\xed\xb2\x80"}
	script := ""
	for i, v := range values {
		name := fmt.Sprintf("JA_WRITING_OS_%d", i)
		t.Setenv(name, v)
		script += `printf '%s\n' "$` + name + `"; `
	}
	t.Setenv("JA_WRITING_OS_\xed\xa0\x80", "1") // 名前も同じ (本家の子の os.environb に b"JA_WRITING_OS_\xed\xa0\x80": b"1")
	script += "/usr/bin/env; "
	out := h.dir + "/env.bin"
	t.Setenv("JA_WRITING_OS_OUT", out)
	env, err := cmdenv.FromProcess(nil)
	if err != nil {
		t.Fatal(err)
	}
	kids := newChildren(h.rt, 60, h.dir, DefaultRuntime().ChildEnv(env))
	kids.start("lint", []string{"/bin/sh", "-c", "{ " + script + `} > "$JA_WRITING_OS_OUT"`})
	results, err := kids.wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r := results["lint"]; r.status != "ran" {
		t.Fatalf("結果 %+v", r)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(got), "\n")
	if len(lines) <= len(values) {
		t.Fatalf("子の出力 %q", got)
	}
	if want := values; !slices.Equal(lines[:len(want)], want) {
		t.Errorf("子が受けた値 %q、親が受けた値 %q", lines[:len(want)], want)
	}
	if !slices.Contains(lines[len(values):], "JA_WRITING_OS_\xed\xa0\x80=1") {
		t.Errorf("子の環境に JA_WRITING_OS_\\xed\\xa0\\x80=1 が無い: %q", lines[len(values):])
	}
}

// 起動できないときの文のパスは、bytes にする前の字で出す。作業フォルダと実行ファイルの U+DC80..U+DCFF のサロゲートは 1 バイトにして
// 起動に使うが、python3.15 (3.15.0) の Popen は cwd・実行ファイルが "/tmp/nonexist-\udcff" で無いとき
// 「FileNotFoundError: [Errno 2] No such file or directory: '/tmp/nonexist-\udcff'」を出す (両方無いときは作業フォルダ)。
func TestChildrenCannotRunShowsPathAsGiven(t *testing.T) {
	h := newHangFixture(t)
	dcff := pyunicode.FromRunes([]rune{0xDCFF})
	for _, c := range []struct {
		name, cwd, exe, want string
	}{
		{"作業フォルダが無い", h.dir + "/nowhere-" + dcff, testExecutable(t), h.dir + "/nowhere-\\udcff"},
		{"実行ファイルが無い", h.dir, h.dir + "/missing-" + dcff, h.dir + "/missing-\\udcff"},
		{"両方無い", h.dir + "/nowhere-" + dcff, h.dir + "/missing-" + dcff, h.dir + "/nowhere-\\udcff"},
	} {
		kids := newChildren(h.rt, 60, c.cwd, h.rt.ChildEnv(cmdenv.Env{Cwd: h.dir, Environ: h.environ}))
		kids.start("lint", []string{c.exe, "lint"})
		results, err := kids.wait(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		want := "子プロセスを動かせません（FileNotFoundError: [Errno 2] No such file or directory: '" + c.want + "'）"
		if r := results["lint"]; r.status != "error" || r.err != want {
			t.Errorf("%s: 結果 %+v", c.name, r)
		}
	}
}
