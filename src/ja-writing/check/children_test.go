package check

import (
	"context"
	"fmt"
	"os"
	"slices"
	"testing"

	"ja-writing/cmdenv"
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
// 「UnicodeEncodeError: 'utf-8' codec can't encode character …」。断りの順は argv を前から (1 つの字の並びでは bytes にするのが NUL より先)、
// そのあと環境変数と作業フォルダの NUL。
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
