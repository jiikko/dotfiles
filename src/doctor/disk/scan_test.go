package disk

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRunner はコマンド名 + 先頭引数で応答を返す。calls で「読み取り系しか呼んでいない」を確かめる。
// Scan はエントリを並行に走らせるので、記録は mutex で守る (-race で検出された)。
type fakeRunner struct {
	resp  map[string]fakeResp // key: "pgrep -x" / "xcrun simctl list" / "brew list" / ...
	mu    sync.Mutex
	calls [][]string
}

type fakeResp struct {
	out string
	rc  int
	err error
}

func (f *fakeRunner) run(_ context.Context, name string, args ...string) (string, string, int, error) {
	f.mu.Lock()
	f.calls = append(f.calls, append([]string{name}, args...))
	f.mu.Unlock()
	for k, r := range f.resp {
		if strings.HasPrefix(strings.Join(append([]string{name}, args...), " "), k) {
			return r.out, "stderr", r.rc, r.err
		}
	}
	return "", "unexpected: " + name, 1, nil
}

func testEnv(t *testing.T) Env {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	tmp := filepath.Join(home, "tmpdir")
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		t.Fatal(err)
	}
	return Env{Home: home, TmpDir: tmp + "/", Getenv: func(string) string { return "" }, AppDirs: []string{filepath.Join(home, "Applications")}}
}

func mkfile(t *testing.T, p string, kb int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, kb*1024), 0o644); err != nil {
		t.Fatal(err)
	}
}

func scanOne(t *testing.T, env Env, f *fakeRunner, e Entry, boot func() (time.Time, error)) Result {
	t.Helper()
	rep := Scan(context.Background(), Options{Env: env, Run: f.run, Catalog: []Entry{e}, BootTime: boot})
	return rep.Results[0]
}

func okBoot() (time.Time, error) { return time.Now().Add(time.Hour), nil }

// 表示サイズが du -sk と一致する (ハードリンク・sparse・symlink を含むツリー)。
func TestDuSizeMatchesDu(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tree")
	mkfile(t, filepath.Join(root, "a"), 64)
	mkfile(t, filepath.Join(root, "sub", "b"), 128)
	if err := os.Link(filepath.Join(root, "a"), filepath.Join(root, "hard")); err != nil {
		t.Fatal(err)
	}
	sp, err := os.Create(filepath.Join(root, "sparse"))
	if err != nil {
		t.Fatal(err)
	}
	if err := sp.Truncate(1 << 30); err != nil {
		t.Fatal(err)
	}
	_ = sp.Close()
	if err := os.Symlink(filepath.Join(root, "sub"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("du", "-sk", root).Output()
	if err != nil {
		t.Fatal(err)
	}
	duKB, _ := strconv.ParseInt(strings.Fields(string(out))[0], 10, 64)
	it, err := duSize(context.Background(), root, map[[2]uint64]struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if it.Size/1024 != duKB {
		t.Fatalf("du -sk=%dKB / duSize=%dKB (ハードリンクの二重計上か sparse の見かけサイズを数えている)", duKB, it.Size/1024)
	}
	if it.Size >= 1<<29 {
		t.Errorf("sparse を見かけのサイズで数えている: %d", it.Size)
	}
}

// 対象が symlink ならリンク先へ踏み込まない (リンク先の実データはサイズに入らない)。
func TestSymlinkTargetIsNotDescended(t *testing.T) {
	base := t.TempDir()
	mkfile(t, filepath.Join(base, "real", "big"), 4096)
	link := filepath.Join(base, "link")
	if err := os.Symlink(filepath.Join(base, "real"), link); err != nil {
		t.Fatal(err)
	}
	it, err := duSize(context.Background(), link, map[[2]uint64]struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if it.Size >= 4096*1024 || it.Files != 1 {
		t.Fatalf("symlink を辿ってリンク先を数えた: size=%d files=%d", it.Size, it.Files)
	}
}

func TestValidateTargetGuards(t *testing.T) {
	env := testEnv(t)
	home := env.Home
	if err := os.MkdirAll(filepath.Join(home, "Library", "Caches", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 経路の途中に symlink: home/linkdir -> home/Library
	if err := os.Symlink(filepath.Join(home, "Library"), filepath.Join(home, "linkdir")); err != nil {
		t.Fatal(err)
	}
	reject := []string{"/", "/Users", "/Users/someone", home, filepath.Dir(home), filepath.Join(home, "Library"),
		filepath.Join(home, "Documents"), home + "/Library/../Library/Caches", "relative/path",
		filepath.Join(home, "linkdir", "Caches", "x")}
	for _, p := range reject {
		if got, err := validateTarget(env, p); err == nil {
			t.Errorf("拒否されるべきパスが通った: %s → %s", p, got)
		}
	}
	accept := []string{filepath.Join(home, ".npm"), filepath.Join(home, "Library", "Caches", "x"), "/var/folders/xx/T/TemporaryItems"}
	for _, p := range accept {
		if _, err := validateTarget(env, p); err != nil && !strings.Contains(err.Error(), "親ディレクトリを確認できない") {
			t.Errorf("通るべきパスが拒否された: %s: %v", p, err)
		}
	}
	if got, _ := validateTarget(env, "/var/folders/a/b/c"); got != "/private/var/folders/a/b/c" && got != "" {
		t.Errorf("/var を /private/var に正規化していない: %s", got)
	}
}

// 変数が空なら展開せず失敗にする (rm -rf /foo が組み立たる入口)。0 件は空スライス。
func TestExpandEmptyVarFails(t *testing.T) {
	if _, err := expand(Env{Home: "/h", TmpDir: ""}, "$TMPDIR/.com.google.Chrome.*"); err == nil {
		t.Error("TMPDIR 空で展開が通った")
	}
	if _, err := expand(Env{Home: "", TmpDir: "/t"}, "~/.npm"); err == nil {
		t.Error("HOME 空で展開が通った")
	}
	if got, err := expand(Env{Home: t.TempDir(), TmpDir: "/t"}, "~/nothing/*"); err != nil || len(got) != 0 {
		t.Errorf("0 件: got=%v err=%v", got, err)
	}
}

// simctl が失敗したら孤児判定をせず「走査できず」にする (候補 0 件に畳まない)。
func TestSimDeviceOrphanFailsClosed(t *testing.T) {
	env := testEnv(t)
	vt := filepath.Join(env.Home, "private", "var", "tmp")
	live := "com.apple.CoreSimulator.SimDevice.11111111-1111-1111-1111-111111111111"
	dead := "com.apple.CoreSimulator.SimDevice.22222222-2222-2222-2222-222222222222"
	mkfile(t, filepath.Join(vt, live, "f"), 8)
	mkfile(t, filepath.Join(vt, dead, "f"), 8)
	e := Entry{ID: "coresimulator-orphan", Paths: []string{filepath.Join(vt, "com.apple.CoreSimulator.SimDevice.*")}, Guard: GuardSimDevice}
	for name, f := range map[string]*fakeRunner{
		"missing": {resp: map[string]fakeResp{"xcrun simctl list": {err: errors.New("xcrun not found")}}},
		"exit1":   {resp: map[string]fakeResp{"xcrun simctl list": {rc: 1}}},
		"badjson": {resp: map[string]fakeResp{"xcrun simctl list": {out: "{not json"}}},
	} {
		r := scanOne(t, env, f, e, okBoot)
		if r.Status != StatusFailed || len(r.Items) != 0 {
			t.Errorf("%s: 失敗を候補 0 件 / 候補ありに畳んだ: %+v", name, r)
		}
	}
	f := &fakeRunner{resp: map[string]fakeResp{"xcrun simctl list": {out: `{"devices":{"rt":[{"udid":"11111111-1111-1111-1111-111111111111"}]}}`}}}
	r := scanOne(t, env, f, e, okBoot)
	if r.Status != StatusOK || len(r.Items) != 1 || !strings.HasSuffix(r.Items[0].Path, dead) {
		t.Fatalf("現存 UUID を候補から外し、無い UUID だけ残すはず: %+v", r)
	}
}

// 起動時刻が取れなければ mtime ガードのエントリは候補にしない (fail-closed)。
func TestBoottimeFailsClosed(t *testing.T) {
	env := testEnv(t)
	vt := filepath.Join(env.Home, "private", "var", "tmp")
	mkfile(t, filepath.Join(vt, "old.logarchive", "f"), 8)
	e := Entry{ID: "xctest-logarchive", Paths: []string{filepath.Join(vt, "*.logarchive")}, Guard: GuardBoottime}
	f := &fakeRunner{}
	r := scanOne(t, env, f, e, func() (time.Time, error) { return time.Time{}, errors.New("sysctl failed") })
	if r.Status != StatusFailed {
		t.Fatalf("起動時刻不明なのに候補にした: %+v", r)
	}
	// 起動後に書かれたものは候補にしない / 前のものは候補
	r = scanOne(t, env, f, e, func() (time.Time, error) { return time.Now().Add(-time.Hour), nil })
	if r.Status != StatusOK || len(r.Items) != 0 {
		t.Fatalf("起動後に書かれたものが候補になった: %+v", r)
	}
	r = scanOne(t, env, f, e, okBoot)
	if r.Status != StatusOK || len(r.Items) != 1 {
		t.Fatalf("起動前のものが候補にならない: %+v", r)
	}
}

// 権限エラーは握り潰さず Failures に出し、合計に足さない。全 item が失敗ならエントリを failed に。
func TestPermissionErrorIsReported(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root では権限エラーを作れない")
	}
	env := testEnv(t)
	base := filepath.Join(env.Home, "Library", "Containers")
	mkfile(t, filepath.Join(base, "readable", "f"), 8)
	locked := filepath.Join(base, "locked")
	mkfile(t, filepath.Join(locked, "f"), 8)
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	e := Entry{ID: "x", Paths: []string{filepath.Join(base, "*")}}
	r := scanOne(t, env, &fakeRunner{}, e, okBoot)
	if r.Status != StatusOK || len(r.Items) != 1 || len(r.Failures) != 1 || !strings.Contains(r.Failures[0], "locked") {
		t.Fatalf("読めない item を Failures に出して他を続けるはず: %+v", r)
	}
	if !strings.Contains(Format(Report{Results: []Result{r}}, time.Now()), "一部走査できず") {
		t.Error("表示に走査できずが出ない")
	}
	e2 := Entry{ID: "y", Paths: []string{locked}}
	if r := scanOne(t, env, &fakeRunner{}, e2, okBoot); r.Status != StatusFailed {
		t.Fatalf("全 item 失敗なのに failed でない: %+v", r)
	}
}

// 孤児コンテナは /Applications の Info.plist 実走査で判定する (mdfind は使わない)。除外接頭辞は候補にしない。
func TestOrphanContainerUsesInfoPlist(t *testing.T) {
	env := testEnv(t)
	apps := env.AppDirs[0]
	plist := `<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>com.example.alive</string></dict></plist>`
	if err := os.MkdirAll(filepath.Join(apps, "Alive.app", "Contents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(apps, "Alive.app", "Contents", "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	cont := filepath.Join(env.Home, "Library", "Containers")
	for _, id := range []string{"com.example.alive", "com.example.gone", "com.jiikko.mine", "com.apple.CloudDocs"} {
		mkfile(t, filepath.Join(cont, id, "Data", "f"), 8)
	}
	e := Entry{ID: "orphan-container", Paths: []string{filepath.Join(cont, "*")}, Guard: GuardOrphanApp, Inspect: true}
	r := scanOne(t, env, &fakeRunner{}, e, okBoot)
	if r.Status != StatusOK || len(r.Items) != 1 || filepath.Base(r.Items[0].Path) != "com.example.gone" {
		t.Fatalf("実在アプリ / 除外接頭辞を候補から外し、孤児だけ残すはず: %+v", r)
	}
	if len(r.Contents) == 0 {
		t.Error("Inspect の中身一覧が空")
	}
	// /Applications が走査できなければ孤児判定をしない
	env2 := env
	env2.AppDirs = []string{filepath.Join(env.Home, "nowhere")}
	if r := scanOne(t, env2, &fakeRunner{}, e, okBoot); r.Status != StatusFailed {
		t.Fatalf("bundle id を 1 つも取れないのに孤児判定した: %+v", r)
	}
}

// バージョンマネージャ root は <TOOL>_ROOT の実効値で判定する (存在するだけでは候補にしない)。
func TestVersionManagerRootUsesEffectiveRoot(t *testing.T) {
	env := testEnv(t)
	for _, d := range []string{".rbenv", ".nodenv", ".goenv"} {
		mkfile(t, filepath.Join(env.Home, d, "versions", "x"), 8)
	}
	if err := os.MkdirAll(filepath.Join(env.Home, ".anyenv", "envs", "nodenv"), 0o755); err != nil {
		t.Fatal(err)
	}
	env.Getenv = func(k string) string {
		if k == "GOENV_ROOT" {
			return filepath.Join(env.Home, ".anyenv", "envs", "goenv")
		}
		return ""
	}
	e := Entry{ID: "vm", Paths: []string{filepath.Join(env.Home, ".rbenv"), filepath.Join(env.Home, ".nodenv"), filepath.Join(env.Home, ".goenv")}, Guard: GuardVMRoot}
	r := scanOne(t, env, &fakeRunner{}, e, okBoot)
	var got []string
	for _, it := range r.Items {
		got = append(got, filepath.Base(it.Path))
	}
	// rbenv: 変数なし・anyenv なし → ~/.rbenv が実効 root = 現役 (候補にしない)
	// nodenv: 変数なし・anyenv あり → 実効 root は anyenv 側 = ~/.nodenv は孤児
	// goenv: 変数が anyenv を指す → ~/.goenv は孤児
	if strings.Join(got, ",") != ".nodenv,.goenv" {
		t.Fatalf("実効 root の判定が違う: %v (status=%s reason=%s)", got, r.Status, r.Reason)
	}
}

// プロセス起動中は blocked (合計に足さない)。pgrep 自体の失敗は failed。
func TestProcessGuard(t *testing.T) {
	env := testEnv(t)
	mkfile(t, filepath.Join(env.TmpDir, ".com.google.Chrome.abc", "f"), 8)
	e := Entry{ID: "chrome-tmp", Paths: []string{"$TMPDIR/.com.google.Chrome.*"}, Guard: GuardProcessAbsent, Process: "Google Chrome"}
	cases := map[string]struct {
		resp fakeResp
		want Status
	}{
		"running": {fakeResp{rc: 0}, StatusBlocked},
		"absent":  {fakeResp{rc: 1}, StatusOK},
		"broken":  {fakeResp{err: errors.New("no pgrep")}, StatusFailed},
	}
	for name, c := range cases {
		f := &fakeRunner{resp: map[string]fakeResp{"pgrep -x Google Chrome": c.resp}}
		rep := Scan(context.Background(), Options{Env: env, Run: f.run, Catalog: []Entry{e}, BootTime: okBoot})
		r := rep.Results[0]
		if r.Status != c.want {
			t.Errorf("%s: status=%s want %s (%s)", name, r.Status, c.want, r.Reason)
		}
		if c.want != StatusOK && rep.Total != 0 {
			t.Errorf("%s: blocked / failed を合計に足した: %d", name, rep.Total)
		}
		if c.want == StatusOK && rep.Total == 0 {
			t.Errorf("%s: 候補の合計が 0", name)
		}
	}
}

// brew の台帳に無い formula の var/<name> だけを候補にする。brew 失敗は failed。
func TestBrewOrphanState(t *testing.T) {
	env := testEnv(t)
	base := filepath.Join(env.Home, "opt", "homebrew", "var")
	for _, d := range []string{"mysql", "postgresql@14", "homebrew", "log"} {
		mkfile(t, filepath.Join(base, d, "f"), 8)
	}
	e := Entry{ID: "brew-orphan-state", Paths: []string{filepath.Join(base, "*")}, Guard: GuardBrewOrphan}
	f := &fakeRunner{resp: map[string]fakeResp{"brew list": {out: "postgresql@14\nredis\n"}}}
	r := scanOne(t, env, f, e, okBoot)
	if r.Status != StatusOK || len(r.Items) != 1 || filepath.Base(r.Items[0].Path) != "mysql" {
		t.Fatalf("台帳に無い mysql だけが候補のはず: %+v", r)
	}
	f = &fakeRunner{resp: map[string]fakeResp{"brew list": {rc: 1}}}
	if r := scanOne(t, env, f, e, okBoot); r.Status != StatusFailed {
		t.Fatalf("brew 失敗を候補に畳んだ: %+v", r)
	}
}

// カタログの Paths が除外リストに踏み込んでいない。全エントリに Recover がある。
func TestCatalogRespectsExclusions(t *testing.T) {
	for _, e := range catalog {
		if e.Recover == "" {
			t.Errorf("%s: Recover (復元方法) が無い。サイズだけの行を作らない", e.ID)
		}
		if e.Guard == GuardProcessAbsent && e.Process == "" {
			t.Errorf("%s: プロセス判定の名前が無い (推測しない)", e.ID)
		}
		for _, p := range e.Paths {
			for _, ex := range excludedRoots {
				if p == ex || strings.HasPrefix(p, ex+"/") {
					t.Errorf("%s: 除外リストに踏み込んでいる: %s (%s)", e.ID, p, ex)
				}
			}
		}
	}
}

// 実行するコマンドは読み取り系だけ (削除経路はこのパッケージに無い)。
func TestOnlyReadOnlyCommands(t *testing.T) {
	env := testEnv(t)
	f := &fakeRunner{resp: map[string]fakeResp{
		"pgrep":                     {rc: 1},
		"xcrun simctl list":         {out: `{"devices":{}}`},
		"xcrun simctl runtime list": {out: `{}`},
		"brew list":                 {out: ""},
		"brew cleanup --dry-run":    {out: ""},
		"brew --prefix":             {out: "/opt/homebrew"},
	}}
	Scan(context.Background(), Options{Env: env, Run: f.run, BootTime: okBoot})
	allowed := []string{"pgrep -x", "xcrun simctl list devices -j", "xcrun simctl runtime list -j", "brew list --formula", "brew cleanup --dry-run", "brew --prefix"}
	for _, c := range f.calls {
		line := strings.Join(c, " ")
		ok := false
		for _, a := range allowed {
			if strings.HasPrefix(line, a) {
				ok = true
			}
		}
		if !ok {
			t.Errorf("読み取り以外のコマンドが実行された: %s", line)
		}
	}
}

// 合計は ok だけを数える (blocked / failed が Size を持っていても足さない)。
func TestSumDeletableSkipsBlockedAndFailed(t *testing.T) {
	got := sumDeletable([]Result{
		{Status: StatusOK, Size: 100},
		{Status: StatusBlocked, Size: 1000},
		{Status: StatusFailed, Size: 10000},
	})
	if got != 100 {
		t.Fatalf("合計 %d (blocked / failed を足している)", got)
	}
}
