package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRunner は launchctl / brew の fake。実際の出力形式を模す。渡された argv を記録するので、
// 「list / print 系しか呼んでいない」を実行経路で assert できる (grep では検出できない経路も含む)。
type fakeRunner struct {
	listOut  string
	listRC   int
	listErr  error
	printOut map[string]string // target → 出力
	brewOut  string
	brewErr  error
	calls    [][]string
}

func (f *fakeRunner) run(_ context.Context, name string, args ...string) (string, string, int, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	switch {
	case name == "launchctl" && len(args) == 1 && args[0] == "list":
		if f.listErr != nil {
			return "", "", -1, f.listErr
		}
		return f.listOut, "boom", f.listRC, nil
	case name == "launchctl" && len(args) == 2 && args[0] == "print":
		return f.printOut[args[1]], "", 0, nil
	case name == "brew":
		if f.brewErr != nil {
			return "", "", -1, f.brewErr
		}
		return f.brewOut, "", 0, nil
	}
	return "", "unexpected", 1, nil
}

// listWith は `launchctl list` の出力を組む (ヘッダ + 行)。
func listWith(rows ...string) string {
	return "PID\tStatus\tLabel\n" + strings.Join(rows, "\n") + "\n"
}

func writePlist(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name+".plist")
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>` + body + `</dict></plist>`
	if err := os.WriteFile(p, []byte(xml), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func kv(label string, extra string) string {
	return "<key>Label</key><string>" + label + "</string>" + extra
}

func args(paths ...string) string {
	var b strings.Builder
	b.WriteString("<key>ProgramArguments</key><array>")
	for _, p := range paths {
		b.WriteString("<string>" + p + "</string>")
	}
	b.WriteString("</array>")
	return b.String()
}

const keepAlive = "<key>KeepAlive</key><true/>"
const runAtLoad = "<key>RunAtLoad</key><true/>"

func scanDir(t *testing.T, dir string, f *fakeRunner) Report {
	t.Helper()
	return Scan(context.Background(), Options{
		Dirs: []LaunchDir{{Path: dir, Domain: "gui/501"}},
		Run:  f.run,
	})
}

func labels(rep Report) []string {
	var out []string
	for _, f := range rep.Findings {
		out = append(out, f.Label)
	}
	return out
}

// 最大の罠: -9 が 60 件 + 正の exit code 1 件で、候補が 1 件だけになる。
func TestNegativeStatusIsNoise(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "bin", "ok")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var rows []string
	for i := 0; i < 60; i++ {
		l := fmt.Sprintf("com.example.idle%d", i)
		writePlist(t, dir, l, kv(l, args(exe)+keepAlive))
		rows = append(rows, "-\t-9\t"+l)
	}
	writePlist(t, dir, "com.example.broken", kv("com.example.broken", args(exe)+keepAlive))
	rows = append(rows, "-\t78\tcom.example.broken")
	f := &fakeRunner{listOut: listWith(rows...)}
	rep := scanDir(t, dir, f)
	if got := labels(rep); len(got) != 1 || got[0] != "com.example.broken" {
		t.Fatalf("候補が 1 件 (com.example.broken) にならない: %v", got)
	}
	if rep.Findings[0].LastExit != 78 || !strings.Contains(rep.Findings[0].Reasons[0], "last exit 78") {
		t.Errorf("exit code が理由に出ない: %+v", rep.Findings[0])
	}
	if rep.StatusErr != "" || len(rep.Undiagnosed) != 0 {
		t.Errorf("全件診断できるはず: %+v", rep)
	}
}

// A: 実行ファイル不在は exit code 0 でも単独で候補になる。
func TestMissingExecutableIsPrimary(t *testing.T) {
	dir := t.TempDir()
	writePlist(t, dir, "homebrew.mxcl.mysql@8.0", kv("homebrew.mxcl.mysql@8.0",
		args("/opt/nowhere/mysqld_safe", "--x")+keepAlive+runAtLoad))
	f := &fakeRunner{
		listOut:  listWith("-\t0\thomebrew.mxcl.mysql@8.0"),
		printOut: map[string]string{"gui/501/homebrew.mxcl.mysql@8.0": "\tlast exit code = 78\n\tproperties = keepalive | runatload | penalty box\n"},
		brewOut:  "redis\nunbound\n",
	}
	rep := scanDir(t, dir, f)
	if len(rep.Findings) != 1 {
		t.Fatalf("候補 1 件のはず: %+v", rep)
	}
	got := rep.Findings[0]
	if got.MissingExec != "/opt/nowhere/mysqld_safe" {
		t.Errorf("不在の実行ファイルが記録されない: %+v", got)
	}
	if !got.PenaltyBox {
		t.Error("launchctl print の penalty box を拾っていない")
	}
	if !got.BrewOrphan || got.BrewFormula != "mysql@8.0" {
		t.Errorf("C (brew 台帳に無い) を拾っていない: %+v", got)
	}
	want := []string{"launchctl bootout gui/501/homebrew.mxcl.mysql@8.0", "rm " + filepath.Join(dir, "homebrew.mxcl.mysql@8.0.plist")}
	if strings.Join(got.Commands, "|") != strings.Join(want, "|") {
		t.Errorf("提示コマンド: got %v want %v", got.Commands, want)
	}
	out := Format(rep)
	for _, s := range []string{"実行ファイルがありません", "penalty box", "brew list に mysql@8.0 が無い", "このツールは実行しません"} {
		if !strings.Contains(out, s) {
			t.Errorf("表示に %q が無い:\n%s", s, out)
		}
	}
}

// Program と ProgramArguments[0] は別ルール: 相対名は _PATH_STDPATH で解決し、正常な登録を候補にしない。
func TestRelativeProgramArgumentsResolvedViaStdPath(t *testing.T) {
	dir := t.TempDir()
	writePlist(t, dir, "com.example.py", kv("com.example.py", args("python3", "/tmp/job.py")+keepAlive))
	writePlist(t, dir, "com.example.nope", kv("com.example.nope", args("definitely-not-a-command-xyz", "a")+keepAlive))
	writePlist(t, dir, "com.example.relprog", kv("com.example.relprog", "<key>Program</key><string>python3</string>"+keepAlive))
	f := &fakeRunner{listOut: listWith("-\t0\tcom.example.py", "-\t0\tcom.example.nope", "-\t0\tcom.example.relprog")}
	// stat は stdPath 配下の python3 だけ存在する fake (呼び出し側の $PATH に依存しない)
	stat := func(p string) error {
		if p == "/usr/bin/python3" {
			return nil
		}
		return os.ErrNotExist
	}
	rep := Scan(context.Background(), Options{Dirs: []LaunchDir{{Path: dir, Domain: "gui/501"}}, Run: f.run, Stat: stat})
	got := labels(rep)
	if len(got) != 2 || got[0] != "com.example.nope" || got[1] != "com.example.relprog" {
		t.Fatalf("相対名の解決規則が違う: %v (python3 は候補にせず、解決不能と相対 Program は候補)", got)
	}
	if !strings.Contains(rep.Findings[0].Reasons[0], "_PATH_STDPATH") {
		t.Errorf("解決失敗の理由に検索パスの話が無い: %v", rep.Findings[0].Reasons)
	}
}

// _PATH_STDPATH は launchd の実装定数。paths.h と一致することを固定する (SDK が無ければ skip)。
func TestStdPathMatchesPathsH(t *testing.T) {
	candidates := []string{"/usr/include/paths.h"}
	if out, _, rc, err := execRunner(context.Background(), "xcrun", "--show-sdk-path"); err == nil && rc == 0 {
		candidates = append(candidates, filepath.Join(strings.TrimSpace(out), "usr/include/paths.h"))
	}
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, "_PATH_STDPATH") {
				if !strings.Contains(line, `"`+stdPath+`"`) {
					t.Fatalf("paths.h の _PATH_STDPATH と不一致: %q (定数 %q)", line, stdPath)
				}
				return
			}
		}
	}
	t.Skip("paths.h が見つからない (SDK 未導入)。_PATH_STDPATH の一致は未検証")
}

// BundleProgram だけの plist は判定対象外 (「診断できず」でもない)。
func TestBundleProgramOnlyIsSkipped(t *testing.T) {
	dir := t.TempDir()
	writePlist(t, dir, "com.example.app", kv("com.example.app", "<key>BundleProgram</key><string>Contents/MacOS/Helper</string>"+keepAlive))
	writePlist(t, dir, "com.example.empty", kv("com.example.empty", "<key>ProgramArguments</key><array/>"))
	writePlist(t, dir, "com.example.none", kv("com.example.none", ""))
	f := &fakeRunner{listOut: listWith("-\t0\tcom.example.app")}
	rep := scanDir(t, dir, f)
	if len(rep.Findings) != 0 || len(rep.Undiagnosed) != 0 {
		t.Fatalf("判定対象外が候補 / 診断できずに入った: %+v", rep)
	}
	if rep.Scanned != 3 {
		t.Errorf("走査件数: %d", rep.Scanned)
	}
}

// B は再起動条件を持つものに限る: RunAtLoad のみ + 正の exit code は候補にならない。
func TestRunAtLoadOnlyIsNotRepeatedFailure(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "ok")
	if err := os.WriteFile(exe, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	writePlist(t, dir, "com.adobe.once", kv("com.adobe.once", args(exe)+runAtLoad))
	writePlist(t, dir, "com.example.keep", kv("com.example.keep", args(exe)+"<key>StartInterval</key><integer>60</integer>"))
	writePlist(t, dir, "com.example.keepfalse", kv("com.example.keepfalse", args(exe)+"<key>KeepAlive</key><false/>"))
	f := &fakeRunner{listOut: listWith("-\t110\tcom.adobe.once", "-\t1\tcom.example.keep", "-\t1\tcom.example.keepfalse")}
	rep := scanDir(t, dir, f)
	if got := labels(rep); len(got) != 1 || got[0] != "com.example.keep" {
		t.Fatalf("再起動条件を持つものだけが候補になるはず: %v", got)
	}
}

// launchctl が使えないときは「診断できず」。候補 0 件に畳まず、A は引き続き出す。
func TestLaunchctlFailureIsUndiagnosedNotZero(t *testing.T) {
	dir := t.TempDir()
	writePlist(t, dir, "com.example.gone", kv("com.example.gone", args("/nowhere/x")+keepAlive))
	for name, f := range map[string]*fakeRunner{
		"missing": {listErr: errors.New("exec: \"launchctl\": executable file not found")},
		"exit1":   {listRC: 1},
		"timeout": {listErr: context.DeadlineExceeded},
	} {
		rep := scanDir(t, dir, f)
		if rep.StatusErr == "" {
			t.Errorf("%s: 診断できずになっていない", name)
		}
		if len(rep.Findings) != 1 || rep.Findings[0].HasLastExit {
			t.Errorf("%s: A の判定が落ちた / 取れていない状態を持っている: %+v", name, rep.Findings)
		}
		if out := Format(rep); !strings.Contains(out, "診断できず (launchctl)") {
			t.Errorf("%s: 表示に診断できずが出ない:\n%s", name, out)
		}
		for _, c := range f.calls {
			if c[0] == "launchctl" && c[1] == "print" {
				t.Errorf("%s: launchctl が失敗しているのに print を呼んだ: %v", name, c)
			}
		}
	}
}

// 壊れた plist はその 1 件だけを「診断できず」にし、他を巻き込まない。
func TestBrokenPlistIsolated(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "com.example.bad.plist"), []byte("<plist><dict><key>Label"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePlist(t, dir, "com.example.gone", kv("com.example.gone", args("/nowhere/x")))
	f := &fakeRunner{listOut: listWith("-\t0\tcom.example.gone")}
	rep := scanDir(t, dir, f)
	if len(rep.Undiagnosed) != 1 || !strings.HasSuffix(rep.Undiagnosed[0].PlistPath, "com.example.bad.plist") {
		t.Fatalf("壊れた plist が診断できずに入らない: %+v", rep.Undiagnosed)
	}
	if got := labels(rep); len(got) != 1 || got[0] != "com.example.gone" {
		t.Fatalf("他の候補が巻き込まれた: %v", got)
	}
	if out := Format(rep); !strings.Contains(out, "❔ 診断できず") {
		t.Errorf("表示に出ない:\n%s", out)
	}
}

// 走査範囲はパス基準: 既定に /System/Library が無く、ラベルが com.apple.* でもユーザー領域なら候補に入る。
func TestScopeIsPathBasedNotLabelBased(t *testing.T) {
	for _, d := range defaultDirs("/Users/x", 501) {
		if strings.HasPrefix(d.Path, "/System/Library") || strings.HasPrefix(d.Path, "/usr/lib") {
			t.Errorf("Apple 管理領域を走査している: %s", d.Path)
		}
	}
	if got := defaultDirs("/Users/x", 501); got[0].Path != "/Users/x/Library/LaunchAgents" || got[2].Domain != "system" {
		t.Errorf("既定の走査先: %+v", got)
	}
	dir := t.TempDir()
	writePlist(t, dir, "com.apple.foo", kv("com.apple.foo", args("/nowhere/foo")+keepAlive))
	f := &fakeRunner{listOut: listWith("-\t0\tcom.apple.foo")}
	rep := scanDir(t, dir, f)
	if got := labels(rep); len(got) != 1 || got[0] != "com.apple.foo" {
		t.Fatalf("Apple を名乗るだけの plist が候補から外れた: %v", got)
	}
	if !rep.Findings[0].AppleLikeOut || !strings.Contains(Format(rep), "管理領域 (/System/Library) の外") {
		t.Error("Apple を名乗っているが管理領域の外、の注記が出ない")
	}
}

// 停止・削除の経路が存在しない: fake に渡った argv が list / print 系だけで、print は候補にだけ呼ぶ。
func TestOnlyReadOnlyCommandsAreExecuted(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "ok")
	if err := os.WriteFile(exe, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	writePlist(t, dir, "com.example.fine", kv("com.example.fine", args(exe)+keepAlive))
	writePlist(t, dir, "homebrew.mxcl.gone", kv("homebrew.mxcl.gone", args("/nowhere/x")+keepAlive))
	f := &fakeRunner{listOut: listWith("12\t0\tcom.example.fine", "-\t0\thomebrew.mxcl.gone"), brewOut: "redis\n"}
	rep := scanDir(t, dir, f)
	if len(rep.Findings) != 1 {
		t.Fatalf("候補: %v", labels(rep))
	}
	prints := 0
	for _, c := range f.calls {
		switch {
		case c[0] == "launchctl" && c[1] == "list":
		case c[0] == "launchctl" && c[1] == "print":
			prints++
			if c[2] != "gui/501/homebrew.mxcl.gone" {
				t.Errorf("候補でないラベルに print を呼んだ: %v", c)
			}
		case c[0] == "brew" && c[1] == "list":
		default:
			t.Errorf("読み取り以外のコマンドが実行された: %v", c)
		}
	}
	if prints != 1 {
		t.Errorf("print は候補 1 件に 1 回のはず: %d 回", prints)
	}
}

// brew が使えないときは C だけを「診断できず」にし、A / B は続ける。
func TestBrewFailureOnlyDisablesC(t *testing.T) {
	dir := t.TempDir()
	writePlist(t, dir, "homebrew.mxcl.gone", kv("homebrew.mxcl.gone", args("/nowhere/x")+keepAlive))
	f := &fakeRunner{listOut: listWith("-\t0\thomebrew.mxcl.gone"), brewErr: errors.New("brew not found")}
	rep := scanDir(t, dir, f)
	if rep.BrewErr == "" || len(rep.Findings) != 1 || rep.Findings[0].BrewOrphan {
		t.Fatalf("brew 失敗の扱い: %+v", rep)
	}
	if !strings.Contains(Format(rep), "診断できず (brew)") {
		t.Error("表示に brew の診断できずが出ない")
	}
}

func TestParseLaunchctlListFormats(t *testing.T) {
	m := parseLaunchctlList("PID\tStatus\tLabel\n1011\t0\ta\n-\t-9\tb\n-\t-\tc\n-\t78\td\n")
	if m["a"].PID != 1011 || !m["a"].HasExit || m["a"].Exit != 0 {
		t.Errorf("a: %+v", m["a"])
	}
	if m["b"].Exit != -9 || m["c"].HasExit || m["d"].Exit != 78 {
		t.Errorf("b/c/d: %+v %+v %+v", m["b"], m["c"], m["d"])
	}
	if _, ok := m["PID"]; ok {
		t.Error("ヘッダ行をラベルとして読んだ")
	}
}
