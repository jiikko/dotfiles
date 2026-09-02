package disk

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
	// 最長一致 ("pgrep -x Google Chrome" が "pgrep -x Google Chrome Canary" を横取りしない)
	line := strings.Join(append([]string{name}, args...), " ")
	best := ""
	for k := range f.resp {
		if strings.HasPrefix(line, k) && len(k) > len(best) {
			best = k
		}
	}
	if best != "" {
		r := f.resp[best]
		return r.out, "stderr", r.rc, r.err
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
	// Xcode Previews のデバイスセットにいるものも現存 (既定セットだけ見ると Preview 中の作業領域を孤児にする)
	previews := filepath.Join(env.Home, "Library", "Developer", "Xcode", "UserData", "Previews", "Simulator Devices")
	if err := os.MkdirAll(previews, 0o755); err != nil {
		t.Fatal(err)
	}
	f = &fakeRunner{resp: map[string]fakeResp{
		"xcrun simctl list":  {out: `{"devices":{"rt":[{"udid":"11111111-1111-1111-1111-111111111111"}]}}`},
		"xcrun simctl --set": {out: `{"devices":{"rt":[{"udid":"22222222-2222-2222-2222-222222222222"}]}}`},
	}}
	r = scanOne(t, env, f, e, okBoot)
	if r.Status != StatusOK || len(r.Items) != 0 {
		t.Fatalf("Previews セットのデバイスを孤児にした: %+v", r)
	}
	// argv を丸ごと突き合わせる。fakeRunner の応答は先頭一致で選ぶので、`--set` の後ろ (パスと
	// list devices -j の順序) を検証しないと、そこを壊す変更をテストが通す (issues/170)。
	// パスに空白が含まれる ("Simulator Devices") ので、1 引数として渡っていることも同時に見る。
	wantSet := []string{"xcrun", "simctl", "--set", previews, "list", "devices", "-j"}
	wantDefault := []string{"xcrun", "simctl", "list", "devices", "-j"}
	var sawSet, sawDefault bool
	f.mu.Lock()
	for _, c := range f.calls {
		if slices.Equal(c, wantSet) {
			sawSet = true
		}
		if slices.Equal(c, wantDefault) {
			sawDefault = true
		}
	}
	got := append([][]string(nil), f.calls...)
	f.mu.Unlock()
	if !sawSet || !sawDefault {
		t.Fatalf("simctl の argv が想定と違う (既定=%v Previews=%v):\n want %v\n want %v\n got  %v",
			sawDefault, sawSet, wantDefault, wantSet, got)
	}
	f = &fakeRunner{resp: map[string]fakeResp{
		"xcrun simctl list":  {out: `{"devices":{}}`},
		"xcrun simctl --set": {rc: 1},
	}}
	if r = scanOne(t, env, f, e, okBoot); r.Status != StatusFailed {
		t.Fatalf("Previews セットの取得失敗を fail-closed にしていない: %+v", r)
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

// 権限エラーは握り潰さず Failures に出し、合計に足さない。全 Item が失敗ならエントリを failed に。
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
		t.Fatalf("読めない Item を Failures に出して他を続けるはず: %+v", r)
	}
	if !strings.Contains(Format(Report{Results: []Result{r}}, time.Now()), "一部走査できず") {
		t.Error("表示に走査できずが出ない")
	}
	e2 := Entry{ID: "y", Paths: []string{locked}}
	if r := scanOne(t, env, &fakeRunner{}, e2, okBoot); r.Status != StatusFailed {
		t.Fatalf("全 Item 失敗なのに failed でない: %+v", r)
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
	// フォルダに入った app (Adobe Acrobat DC/Adobe Acrobat.app の形) と、app 内蔵の appex
	nested := `<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>com.vendor.nested</string></dict></plist>`
	if err := os.MkdirAll(filepath.Join(apps, "Vendor Suite", "Nested.app", "Contents", "PlugIns", "Widget.appex", "Contents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(apps, "Vendor Suite", "Nested.app", "Contents", "Info.plist"), []byte(nested), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(apps, "Vendor Suite", "Nested.app", "Contents", "PlugIns", "Widget.appex", "Contents", "Info.plist"),
		[]byte(strings.ReplaceAll(nested, "com.vendor.nested", "com.vendor.nested.widget")), 0o644); err != nil {
		t.Fatal(err)
	}
	cont := filepath.Join(env.Home, "Library", "Containers")
	for _, id := range []string{"com.example.alive", "com.example.gone", "com.jiikko.mine", "com.apple.CloudDocs",
		"com.vendor.nested", "com.vendor.nested.widget", "com.example.alive.ShareExtension"} {
		mkfile(t, filepath.Join(cont, id, "Data", "f"), 8)
	}
	e := Entry{ID: "orphan-container", Paths: []string{filepath.Join(cont, "*")}, Guard: GuardOrphanApp, Inspect: true}
	r := scanOne(t, env, &fakeRunner{}, e, okBoot)
	if r.Status != StatusOK || len(r.Items) != 1 || filepath.Base(r.Items[0].Path) != "com.example.gone" {
		t.Fatalf("実在アプリ (入れ子 / 内蔵 appex / 拡張の <app id>.<ext>) と除外接頭辞を外し、孤児だけ残すはず: %+v", r)
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
	// 環境変数が ~ 付き / 末尾スラッシュ / 相対でも現役を孤児にしない
	for _, val := range []string{"~/.rbenv", env.Home + "/.rbenv/", ".rbenv"} {
		e2 := Entry{ID: "vm", Paths: []string{filepath.Join(env.Home, ".rbenv")}, Guard: GuardVMRoot}
		env2 := env
		env2.Getenv = func(k string) string {
			if k == "RBENV_ROOT" {
				return val
			}
			return ""
		}
		if r := scanOne(t, env2, &fakeRunner{}, e2, okBoot); len(r.Items) != 0 {
			t.Errorf("RBENV_ROOT=%q で現役 root を孤児にした", val)
		}
	}
}

// プロセス起動中は blocked (合計に足さない)。pgrep 自体の失敗は failed。
func TestProcessGuard(t *testing.T) {
	env := testEnv(t)
	mkfile(t, filepath.Join(env.TmpDir, ".com.google.Chrome.abc", "f"), 8)
	e := Entry{ID: "chrome-tmp", Paths: []string{"$TMPDIR/.com.google.Chrome.*"}, Guard: GuardProcessAbsent, Processes: []string{"Google Chrome"}}
	cases := map[string]struct {
		resp fakeResp
		want Status
	}{
		"running": {fakeResp{rc: 0}, StatusBlocked},
		"absent":  {fakeResp{rc: 1}, StatusOK},
		"broken":  {fakeResp{err: errors.New("no pgrep")}, StatusFailed},
	}
	e.Processes = []string{"Google Chrome", "Google Chrome Canary"}
	for name, c := range cases {
		f := &fakeRunner{resp: map[string]fakeResp{"pgrep -x Google Chrome Canary": c.resp, "pgrep -x Google Chrome": {rc: 1}}}
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
	for _, d := range []string{"mysql", "postgresql@14", "homebrew", "log", "db", "www"} {
		mkfile(t, filepath.Join(base, d, "f"), 8)
	}
	e := Entry{ID: "brew-orphan-state", Paths: []string{filepath.Join(base, "*")}, Guard: GuardBrewOrphan}
	f := &fakeRunner{resp: map[string]fakeResp{"brew info": {out: `{"formulae":[{"name":"postgresql@14"},{"name":"redis"}]}`}}}
	r := scanOne(t, env, f, e, okBoot)
	if r.Status != StatusOK || len(r.Items) != 1 || filepath.Base(r.Items[0].Path) != "mysql" {
		t.Fatalf("台帳に無い mysql だけが候補のはず (db / www は共有 dir): %+v", r)
	}
	// rename 済み formula: 旧名 postgresql の状態 dir は oldnames で現役扱い
	mkfile(t, filepath.Join(base, "postgresql", "f"), 8)
	f2 := &fakeRunner{resp: map[string]fakeResp{"brew info": {out: `{"formulae":[{"name":"postgresql@14","oldnames":["postgresql"]},{"name":"redis"}]}`}}}
	r = scanOne(t, env, f2, e, okBoot)
	for _, it := range r.Items {
		if filepath.Base(it.Path) == "postgresql" {
			t.Fatal("旧名 (oldnames) の状態 dir を孤児にした")
		}
	}
	f = &fakeRunner{resp: map[string]fakeResp{"brew info": {rc: 1}}}
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
		if e.Guard == GuardProcessAbsent && len(e.Processes) == 0 {
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
		"brew --prefix":             {out: env.Home}, // 実機の /opt/homebrew に依存させない
	}}
	Scan(context.Background(), Options{Env: env, Run: f.run, BootTime: okBoot})
	allowed := []string{"pgrep -x", "xcrun simctl list devices -j", "xcrun simctl --set", "xcrun simctl runtime list -j", "brew info --json=v2 --installed", "brew cleanup --dry-run", "brew --prefix"}
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
	got := SumDeletable([]Result{
		{Status: StatusOK, Size: 100},
		{Status: StatusBlocked, Size: 1000},
		{Status: StatusFailed, Size: 10000},
	})
	if got != 100 {
		t.Fatalf("合計 %d (blocked / failed を足している)", got)
	}
}

// Options.Reuse が前回結果を返したエントリは走査しない (パスが存在しなくても前回の値が Reused で載る)。
// ID が違う結果は混ぜない。
func TestReuseSkipsScan(t *testing.T) {
	env := testEnv(t)
	prev := Result{Entry: Entry{ID: "heavy"}, Status: StatusOK, Size: 12345, Elapsed: 3 * time.Second,
		MeasuredAt: time.Now().Add(-10 * time.Minute), Items: []Item{{Path: "/gone", Size: 12345}}}
	cat := []Entry{
		{ID: "heavy", Label: "Heavy", Recover: "x", Paths: []string{filepath.Join(env.Home, "nowhere-heavy")}},
		{ID: "light", Label: "Light", Recover: "y", Paths: []string{filepath.Join(env.Home, "nowhere-light")}},
	}
	rep := Scan(context.Background(), Options{Env: env, Run: (&fakeRunner{}).run, Catalog: cat, BootTime: okBoot,
		Reuse: func(e Entry) *Result {
			if e.ID == "heavy" {
				return &prev
			}
			if e.ID == "light" {
				wrong := prev
				wrong.Entry.ID = "heavy" // 別エントリの結果を返す誤り → 使わない
				return &wrong
			}
			return nil
		}})
	var heavy, light Result
	for _, r := range rep.Results {
		switch r.Entry.ID {
		case "heavy":
			heavy = r
		case "light":
			light = r
		}
	}
	if !heavy.Reused || heavy.Size != 12345 || heavy.Elapsed != 3*time.Second || heavy.MeasuredAt.IsZero() {
		t.Fatalf("前回結果が再利用されない / 計測情報が保たれない: %+v", heavy)
	}
	if light.Reused || len(light.Items) != 0 {
		t.Fatalf("ID の違う結果を混ぜた: %+v", light)
	}
	if rep.Total != 12345 {
		t.Errorf("再利用分が合計に入らない: %d", rep.Total)
	}
}

// issue 175: HOME に glob メタ文字が入っていても、実在するパスは候補として拾えること。
// 素朴に filepath.Glob へ渡すと `[1]` が文字クラスとして解釈されて 0 件になり、実在するのに
// 「候補なし」に化ける (false green)。
func TestExpandLiteralizesGlobMetaInEnv(t *testing.T) {
	base := t.TempDir()
	// 各メタ文字名に「escape しなければ余分にマッチする」おとりの兄弟を必ず作る。
	// おとりが無いと ? のように「escape しなくても同じ結果になる」ケースが混ざり、変異させても
	// green のまま残る (敵対レビュー指摘)。
	decoys := map[string]string{"h[1]": "h1", "h*": "hZZ", "h?x": "hax", `h\z`: "hXz", "h 1": "hY1"}
	for _, name := range []string{"h[1]", "h*", "h?x", `h\z`, "h 1"} {
		home := filepath.Join(base, name)
		if err := os.MkdirAll(filepath.Join(home, ".npm", "_cacache"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(base, decoys[name], ".npm", "_cacache"), 0o755); err != nil {
			t.Fatal(err)
		}
		got, err := expand(Env{Home: home, TmpDir: "/t"}, "~/.npm/_cacache")
		if err != nil {
			t.Errorf("HOME=%q: err=%v", name, err)
			continue
		}
		if len(got) != 1 || got[0] != filepath.Join(home, ".npm", "_cacache") {
			t.Errorf("HOME=%q: 実在するのに拾えていない: got=%v", name, got)
		}
	}
	// テンプレート側のメタ文字は glob として効いたまま (literal 化しすぎていない)
	home := filepath.Join(base, "h[1]")
	if err := os.MkdirAll(filepath.Join(home, "go", "1.24", "pkg", "mod"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := expand(Env{Home: home, TmpDir: "/t"}, "~/go/*/pkg/mod")
	if err != nil || len(got) != 1 {
		t.Errorf("テンプレートの * が効いていない: got=%v err=%v", got, err)
	}
}

// issue 175: 展開結果が相対パスなら「診断できず」にする (glob が 0 件になって無音で消えるのを防ぐ)。
// 相対パスは cwd に偶然一致すると validateTarget が failed にし、一致しなければ無音の 0 件になる
// 非対称があった。expand の時点で常に error に倒す。
func TestExpandRelativeFailsClosed(t *testing.T) {
	if _, err := expand(Env{Home: "/h", TmpDir: "tmp"}, "$TMPDIR/TemporaryItems/NSIRD_Finder_*"); err == nil {
		t.Error("相対 TMPDIR が無音の 0 件になった (診断できずに倒れていない)")
	}
	if _, err := expand(Env{Home: "relhome", TmpDir: "/t"}, "~/.npm"); err == nil {
		t.Error("相対 HOME が無音の 0 件になった")
	}
	// 絶対パスなら従来どおり通る
	if _, err := expand(Env{Home: "/h", TmpDir: t.TempDir()}, "$TMPDIR/nothing/*"); err != nil {
		t.Errorf("絶対 TMPDIR が拒否された: %v", err)
	}
}

// issue 176: brew prefix は `brew --prefix` から解決する (Apple Silicon /opt/homebrew と
// Intel /usr/local で違うので直書きしない)。取れないときは fail-closed。
func TestBrewPrefixIsResolvedNotHardcoded(t *testing.T) {
	env := testEnv(t)
	prefix := filepath.Join(env.Home, "usr", "local") // Intel 相当。/opt/homebrew ではない
	mkfile(t, filepath.Join(prefix, "var", "mysql", "f"), 8)
	e := Entry{ID: "brew-orphan-state", Paths: []string{"$BREW_PREFIX/var/*"}, Guard: GuardBrewOrphan}

	f := &fakeRunner{resp: map[string]fakeResp{
		"brew info":     {out: `{"formulae":[{"name":"redis"}]}`},
		"brew --prefix": {out: prefix + "\n"},
	}}
	r := scanOne(t, env, f, e, okBoot)
	if r.Status != StatusOK || len(r.Items) != 1 || filepath.Base(r.Items[0].Path) != "mysql" {
		t.Fatalf("prefix が動的に解決されていない (Intel 相当の prefix で候補 0 件): %+v", r)
	}

	// 取れない / 空 / 相対 / 実在しない → すべて failed (候補 0 件に畳まない)。
	// ⚠️ fixture は「その guard だけを踏む」形にする。どれも空文字や存在しないパスにすると、
	// 実際には Stat の guard 1 つだけが全部を弾いていて、他の guard を消しても green のまま残る
	// (敵対レビュー 2026-09-03 で実測)。各ケースは error 文言で「どの guard が弾いたか」まで固定する。
	for _, tc := range []struct {
		name, out, wantMsg string
		rc                 int
	}{
		// rc≠0: out は正当な prefix にして、rc だけで弾かれることを固定する
		{name: "rc!=0", out: prefix, rc: 1, wantMsg: "exit 1"},
		{name: "空", out: "\n", wantMsg: "空を返した"},
		// 相対: cwd から見て実在する "." にして、Stat ではなく IsAbs で弾かれることを固定する
		{name: "相対", out: ".", wantMsg: "絶対パスでない"},
		{name: "実在しない", out: filepath.Join(env.Home, "no-such-prefix"), wantMsg: "確認できない"},
		// "/" は expand の TrimRight で空に潰れ、$BREW_PREFIX/var/* が /var/* に化ける
		{name: "ルート", out: "/", wantMsg: "ルート (/) を返した"},
	} {
		f := &fakeRunner{resp: map[string]fakeResp{
			"brew info":     {out: `{"formulae":[{"name":"redis"}]}`},
			"brew --prefix": {out: tc.out, rc: tc.rc},
		}}
		r := scanOne(t, env, f, e, okBoot)
		if r.Status != StatusFailed {
			t.Errorf("%s: 診断できずに倒れていない: status=%s items=%d", tc.name, r.Status, len(r.Items))
			continue
		}
		// 文言だけでなく「brewPrefix が弾いたこと」まで固定する。下流 (expand の絶対パス検査 /
		// validateTarget) にも二重の防御があるので、prefix 側の guard を消しても別の層が弾いて
		// green のまま残る (敵対レビュー 2026-09-03 で "." の相対ケースが実際にそうなっていた)。
		const by = "brew --prefix を取得できず"
		if !strings.HasPrefix(r.Reason, by) || !strings.Contains(r.Reason, tc.wantMsg) {
			t.Errorf("%s: brewPrefix 以外の層が弾いている (このケースは対象の guard を検査できていない): reason=%q want~=%q", tc.name, r.Reason, tc.wantMsg)
		}
	}
	// ディレクトリでない prefix (ファイル) も弾く
	fileAsPrefix := filepath.Join(env.Home, "not-a-dir")
	mkfile(t, fileAsPrefix, 1)
	f = &fakeRunner{resp: map[string]fakeResp{
		"brew info":     {out: `{"formulae":[{"name":"redis"}]}`},
		"brew --prefix": {out: fileAsPrefix},
	}}
	if r := scanOne(t, env, f, e, okBoot); r.Status != StatusFailed || !strings.Contains(r.Reason, "ディレクトリでない") {
		t.Errorf("ファイルを prefix として受け入れた: %+v", r)
	}
}

// カタログが brew prefix を直書きしていないこと (issue 176 の再発防止)。
func TestCatalogHasNoHardcodedBrewPrefix(t *testing.T) {
	for _, e := range catalog {
		for _, p := range e.Paths {
			if strings.HasPrefix(p, "/opt/homebrew") || strings.HasPrefix(p, "/usr/local") {
				t.Errorf("%s: brew prefix を直書きしている (%s)。$BREW_PREFIX を使う", e.ID, p)
			}
		}
	}
}

// issue 176: brew cleanup の相対表記 (Would remove Library/Homebrew/vendor/...) は prefix 配下として
// 解決する。prefix が取れないときに**黙って捨てる**と「消す対象は無い」という false green になる。
func TestBrewCleanupRelativeLineFailsClosed(t *testing.T) {
	env := testEnv(t)
	prefix := filepath.Join(env.Home, "usr", "local")
	mkfile(t, filepath.Join(prefix, "Library", "Homebrew", "vendor", "portable-ruby", "f"), 8)
	e := Entry{ID: "brew-cleanup-residue", Guard: GuardBrewCleanup}
	const out = "Would remove Library/Homebrew/vendor/portable-ruby\n"

	f := &fakeRunner{resp: map[string]fakeResp{
		"brew cleanup --dry-run": {out: out},
		"brew --prefix":          {out: prefix},
	}}
	r := scanOne(t, env, f, e, okBoot)
	if r.Status != StatusOK || len(r.Items) != 1 {
		t.Fatalf("相対表記が prefix 配下として解決されていない: %+v", r)
	}

	// prefix が取れないなら候補 0 件に畳まず failed
	f = &fakeRunner{resp: map[string]fakeResp{
		"brew cleanup --dry-run": {out: out},
		"brew --prefix":          {rc: 1},
	}}
	if r := scanOne(t, env, f, e, okBoot); r.Status != StatusFailed {
		t.Errorf("相対表記を黙って捨てた (false green): status=%s items=%d", r.Status, len(r.Items))
	}

	// 絶対パスの行しか無いなら brew --prefix の失敗に巻き込まれない
	f = &fakeRunner{resp: map[string]fakeResp{
		"brew cleanup --dry-run": {out: "Would remove: " + filepath.Join(prefix, "Library", "Homebrew", "vendor", "portable-ruby") + " (1 file, 8KB)\n"},
		"brew --prefix":          {rc: 1},
	}}
	if r := scanOne(t, env, f, e, okBoot); r.Status != StatusOK || len(r.Items) != 1 {
		t.Errorf("絶対パスの行が brew --prefix の失敗に巻き込まれた: %+v", r)
	}
}
