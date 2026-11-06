package uninstall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

// world はテスト 1 本ぶんの偽の Mac。全部 t.TempDir の下に作る。
type world struct {
	t        *testing.T
	root     string
	home     string
	apps     string
	cellar   string
	running  []string          // 動いている実行ファイルの実パス (lsof の txt に出る)
	psComm   []string          // ps の comm (nil なら running と同じ。実機では symlink や素の名前が出る)
	psArgs   []string          // ps の args (nil なら comm と同じ)
	casks    string            // brew info --cask の JSON
	leaves   []string          // brew leaves
	brewFail map[string]bool   // brew uninstall を失敗させる名前
	keepApp  map[string]bool   // brew uninstall が rc=0 を返すのに消さない名前
	pkg      map[string]string // .app のパス → pkgid
	pkgs     []string          // pkgutil --pkgs
	shell    string            // dscl の UserShell
	lsofFail bool
	copies   []string // Spotlight だけが知っている、探す置き場の外の .app (開発中のビルドなど)
	noIndex  bool     // Spotlight の索引が無い
	calls    []string
}

func newWorld(t *testing.T) *world {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := &world{t: t, root: root, home: filepath.Join(root, "home"), apps: filepath.Join(root, "Applications"),
		cellar: filepath.Join(root, "Cellar"), casks: `{"casks":[]}`, shell: "/bin/zsh", brewFail: map[string]bool{}, keepApp: map[string]bool{}, pkg: map[string]string{}}
	for _, d := range []string{w.home + "/Library", w.apps, w.cellar, w.home + "/.Trash", root + "/LaunchDaemons"} {
		mkdir(t, d)
	}
	orig := systemDaemonDirs
	systemDaemonDirs = []string{root + "/LaunchDaemons"} // 実機の /Library の常駐を読まない
	t.Cleanup(func() { systemDaemonDirs = orig })
	return w
}

// dupscan:allow intentional テストの 1 行の helper。package ごとに持ち、テスト用の共有 module は作らない
func mkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o700); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, p, body string) {
	t.Helper()
	mkdir(t, filepath.Dir(p))
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

const infoPlist = `<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>%s</string><key>CFBundleShortVersionString</key><string>1.0</string></dict></plist>`

// addApp は <Applications>/<name>.app を作る。
func (w *world) addApp(name, bid string) string {
	p := filepath.Join(w.apps, name+".app")
	write(w.t, filepath.Join(p, "Contents", "Info.plist"), fmt.Sprintf(infoPlist, bid))
	write(w.t, filepath.Join(p, "Contents", "MacOS", name), "bin")
	return p
}

// lib は ~/Library/<rel> にファイルかディレクトリを作る。
func (w *world) lib(rel string) string {
	p := filepath.Join(w.home, "Library", rel)
	if strings.HasSuffix(rel, ".plist") || strings.HasSuffix(rel, ".binarycookies") {
		write(w.t, p, "x")
	} else {
		write(w.t, filepath.Join(p, "f"), "x")
	}
	return p
}

func (w *world) run(_ context.Context, name string, args ...string) (string, string, int, error) {
	cmd := filepath.Base(name) + " " + strings.Join(args, " ")
	w.calls = append(w.calls, cmd)
	switch filepath.Base(name) {
	case "ps":
		if args[len(args)-1] == "args=" && w.psArgs != nil {
			return strings.Join(w.psArgs, "\n") + "\n", "", 0, nil
		}
		if w.psComm != nil {
			return strings.Join(w.psComm, "\n") + "\n", "", 0, nil
		}
		return strings.Join(w.running, "\n") + "\n", "", 0, nil
	case "lsof":
		if w.lsofFail {
			return "", "lsof: boom", 1, nil
		}
		var b strings.Builder
		b.WriteString("p1\nn/usr/lib/dyld\n") // 何も動いていなくても、lsof は自分自身の txt を返す
		for _, r := range w.running {
			b.WriteString("n" + r + "\n")
		}
		return b.String(), "", 0, nil
	case "mdfind":
		if w.noIndex {
			return "", "", 0, nil
		}
		q := args[len(args)-1]
		var out []string
		for _, dir := range append([]string{w.apps}, w.copies...) {
			cands := []string{dir}
			if dir == w.apps {
				cands, _ = filepath.Glob(filepath.Join(w.apps, "*.app"))
			}
			for _, c := range cands {
				a, ok := readApp(c)
				// 本物の mdfind と同じく、末尾が 'c のときだけ大小を区別しない
				want, got := q, "== '"+a.BundleID+"'"
				if strings.HasSuffix(q, "'c") {
					want, got = strings.ToLower(q), strings.ToLower(got)
				}
				if ok && strings.Contains(want, got) {
					out = append(out, c)
				}
			}
		}
		return strings.Join(out, "\n") + "\n", "", 0, nil
	case "dscl":
		return "UserShell: " + w.shell + "\n", "", 0, nil
	case "du":
		return "4\t" + args[len(args)-1] + "\n", "", 0, nil
	case "mdls":
		return "(null)", "", 0, nil
	case "pkgutil":
		if args[0] == "--pkgs" {
			return strings.Join(w.pkgs, "\n") + "\n", "", 0, nil
		}
		if id, ok := w.pkg[args[len(args)-1]]; ok {
			return "volume: /\npath: x\n\npkgid: " + id + "\n", "", 0, nil
		}
		return "", "No receipt", 1, nil
	case "brew":
		switch {
		case args[0] == "--cellar":
			return w.cellar + "\n", "", 0, nil
		case args[0] == "info" && args[1] == "--cask":
			return w.casks, "", 0, nil
		case args[0] == "leaves":
			return strings.Join(w.leaves, "\n") + "\n", "", 0, nil
		case args[0] == "info":
			return `{"formulae":[]}`, "", 0, nil
		case args[0] == "uninstall":
			target := args[len(args)-1]
			if w.brewFail[target] {
				return "", "Error: refusing", 1, nil
			}
			if w.keepApp[target] {
				return "", "", 0, nil
			}
			if args[1] == "--cask" {
				_ = os.RemoveAll(filepath.Join(w.apps, strings.TrimPrefix(target, "cask-")+".app"))
			} else {
				_ = os.RemoveAll(filepath.Join(w.cellar, target))
			}
			return "", "", 0, nil
		}
	}
	return "", "", -1, errors.New("fake: 知らない呼び出し " + cmd)
}

func (w *world) opts() Options {
	return Options{Run: w.run, Home: w.home, AppDirs: []string{w.apps}, Cellar: w.cellar, CaskAppDir: w.apps, Now: func() time.Time { return now }}
}

// guard はテストの一時 dir の外への操作を実行前に拒否する (ProductionGuard はテストの中では全部拒否するので、差し替えが要る)。
func (w *world) guard(op, path string) error {
	if !strings.HasPrefix(filepath.Clean(path)+"/", w.root+"/") {
		return fmt.Errorf("テストの外への %s を拒否: %s", op, path)
	}
	return nil
}

func (w *world) applyOpts() ApplyOptions {
	return ApplyOptions{Options: w.opts(), TrashRoot: filepath.Join(w.home, ".Trash"), Guard: w.guard}
}

// dupscan:allow intentional テストの 1 行の helper。package ごとに持ち、テスト用の共有 module は作らない
func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

func pathsOf(items []Item, a Action) []string {
	var out []string
	for _, it := range items {
		if it.Action == a {
			out = append(out, it.Path)
		}
	}
	return out
}

func TestLeftoversMatchBundleIDExactlyAndSplitTiers(t *testing.T) {
	w := newWorld(t)
	w.addApp("Foo", "com.ex.foo")
	cache := w.lib("Caches/com.ex.foo")
	saved := w.lib("Saved Application State/com.ex.foo.savedState")
	data := w.lib("Application Support/com.ex.foo")
	pref := w.lib("Preferences/com.ex.foo.plist")
	byName := w.lib("Application Support/Foo")
	other := w.lib("Caches/com.ex.foobar")       // 前方一致の別アプリ
	sibling := w.lib("Caches/com.ex.foo.helper") // 同じ前置きの別の bundle ID
	group := w.lib("Group Containers/TEAM.com.ex.foo")
	agent := w.lib("LaunchAgents/com.ex.foo.helper.plist")
	w.casks = ""
	app := App{Name: "Foo", BundleID: "com.ex.foo", zap: []string{"~/Library/Group Containers/*.com.ex.foo"}}
	got := map[string]Tier{}
	for _, l := range leftovers(w.home, app) {
		got[l.Path] = l.Tier
	}
	want := map[string]Tier{cache: TierCache, saved: TierCache, data: TierData, pref: TierData, byName: TierData, group: TierManual, agent: TierManual}
	for p, tier := range want {
		if got[p] != tier {
			t.Errorf("%s: %q; want %q", p, got[p], tier)
		}
	}
	for _, p := range []string{other, sibling} {
		if _, ok := got[p]; ok {
			t.Errorf("別の bundle ID のものを拾った: %s", p)
		}
	}
}

func TestInfoPlistCannotEscapeLibrary(t *testing.T) {
	for _, id := range []string{"../../Documents", "..", ".hidden", "com.ex/../x", "com ex", ""} {
		if validBundleID(id) {
			t.Errorf("通した: %q", id)
		}
	}
	w := newWorld(t)
	w.addApp("Evil", "../../Documents")
	if inv := List(context.Background(), w.opts()); len(inv.Apps) != 0 || len(inv.Notes) == 0 {
		t.Errorf("bundle ID が不正なアプリを候補にした: %+v", inv)
	}
}

func TestBuildPlanRefusesRunningAndKeepsDataUnlessChosen(t *testing.T) {
	w := newWorld(t)
	app := w.addApp("Foo", "com.ex.foo")
	w.lib("Caches/com.ex.foo")
	data := w.lib("Application Support/com.ex.foo")
	ctx := context.Background()

	p, err := BuildPlan(ctx, w.opts(), Selection{Apps: []string{"com.ex.foo"}})
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(pathsOf(p.Items, ActionTrash), data) {
		t.Errorf("選んでいない設定とデータを計画に載せた")
	}
	p, err = BuildPlan(ctx, w.opts(), Selection{Apps: []string{"com.ex.foo"}, Data: []string{"com.ex.foo"}})
	if err != nil || !slices.Contains(pathsOf(p.Items, ActionTrash), data) {
		t.Errorf("選んだ設定とデータを載せない: %v %+v", err, p.Items)
	}
	if _, err := BuildPlan(ctx, w.opts(), Selection{Apps: []string{"com.ex.foo"}, Data: []string{"com.ex.other"}}); err == nil {
		t.Errorf("消さないアプリのデータだけを選べた")
	}
	w.running = []string{app + "/Contents/MacOS/Foo"}
	if _, err := BuildPlan(ctx, w.opts(), Selection{Apps: []string{"com.ex.foo"}}); err == nil {
		t.Errorf("動いているアプリの計画を作った")
	}
}

func TestPkgInstalledAppIsManualOnly(t *testing.T) {
	w := newWorld(t)
	app := w.addApp("Drv", "com.ex.drv")
	w.pkg[app] = "com.ex.drv.pkg"
	w.lib("Caches/com.ex.drv")
	p, err := BuildPlan(context.Background(), w.opts(), Selection{Apps: []string{"com.ex.drv"}, Data: []string{"com.ex.drv"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range p.Items {
		if it.Action != ActionNone {
			t.Errorf("pkg で入れたアプリのものを動かす計画: %+v", it)
		}
	}
}

func TestReadPlanRejectsTamperWrongIDAndStale(t *testing.T) {
	w := newWorld(t)
	w.addApp("Foo", "com.ex.foo")
	p, err := BuildPlan(context.Background(), w.opts(), Selection{Apps: []string{"com.ex.foo"}})
	if err != nil {
		t.Fatal(err)
	}
	path, err := WritePlan(w.root, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPlan(path, p.ID, now.Add(time.Minute)); err != nil {
		t.Fatalf("正しい計画を断った: %v", err)
	}
	if _, err := ReadPlan(path, "other", now); err == nil {
		t.Errorf("違う ID の承認を通した")
	}
	if _, err := ReadPlan(path, p.ID, now.Add(2*time.Hour)); err == nil {
		t.Errorf("古い計画を通した")
	}
	tampered := p
	tampered.Items = append(slices.Clone(p.Items), Item{Owner: "com.ex.foo", Tier: TierCache, Action: ActionTrash, Path: w.home + "/Documents"})
	body, _ := json.Marshal(tampered)
	write(t, path, string(body))
	if _, err := ReadPlan(path, p.ID, now); err == nil {
		t.Errorf("書き換えた計画を通した")
	}
}

// 計画を書き換えて ID を計算し直しても、作り直した候補に無いものは動かない。
func TestApplyMovesOnlyIntersectionWithFreshCandidates(t *testing.T) {
	w := newWorld(t)
	app := w.addApp("Foo", "com.ex.foo")
	cache := w.lib("Caches/com.ex.foo")
	victim := filepath.Join(w.home, "Documents", "important")
	write(t, victim, "keep")
	ctx := context.Background()
	p, err := BuildPlan(ctx, w.opts(), Selection{Apps: []string{"com.ex.foo"}})
	if err != nil {
		t.Fatal(err)
	}
	p.Items = append(p.Items, withIdentity(Item{Owner: "com.ex.foo", Tier: TierCache, Action: ActionTrash, Path: victim}))
	p.ID, _ = planID(p)
	res := Apply(ctx, w.applyOpts(), p)
	if exists(app) || exists(cache) {
		t.Errorf("計画の本体と残骸が残った: %+v", res)
	}
	if !exists(victim) {
		t.Fatalf("計画に足しただけの外のファイルを動かした")
	}
	if len(res.Done) != 2 || len(res.Failed) != 0 {
		t.Errorf("結果: %+v", res)
	}
	for _, d := range res.Done {
		if !strings.HasPrefix(d.To, filepath.Join(w.home, ".Trash", "mac-uninstall-")) {
			t.Errorf("ゴミ箱の外へ移した: %+v", d)
		}
	}
}

func TestApplySkipsReplacedAndNewEntries(t *testing.T) {
	w := newWorld(t)
	w.addApp("Foo", "com.ex.foo")
	cache := w.lib("Caches/com.ex.foo")
	ctx := context.Background()
	p, err := BuildPlan(ctx, w.opts(), Selection{Apps: []string{"com.ex.foo"}})
	if err != nil {
		t.Fatal(err)
	}
	// 計画の後に作り直された (実体が変わった) キャッシュと、計画の後に増えたログ
	if err := os.RemoveAll(cache); err != nil {
		t.Fatal(err)
	}
	w.lib("Caches/com.ex.foo")
	logs := w.lib("Logs/com.ex.foo")
	res := Apply(ctx, w.applyOpts(), p)
	if !exists(cache) || !exists(logs) {
		t.Errorf("実体が変わったもの・計画に無いものを動かした: %+v", res)
	}
	if len(res.Skipped) != 2 {
		t.Errorf("止めた理由が 2 件出ない: %+v", res.Skipped)
	}
}

func TestApplyStopsOwnerWhenBrewFailsOrLies(t *testing.T) {
	for name, setup := range map[string]func(w *world){
		"brew が失敗":          func(w *world) { w.brewFail["cask-foo"] = true },
		"brew が rc=0 で消さない": func(w *world) { w.keepApp["cask-foo"] = true },
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			app := w.addApp("foo", "com.ex.foo")
			cache := w.lib("Caches/com.ex.foo")
			w.casks = `{"casks":[{"token":"cask-foo","artifacts":[{"app":["foo.app"]}]}]}`
			setup(w)
			ctx := context.Background()
			p, err := BuildPlan(ctx, w.opts(), Selection{Apps: []string{"com.ex.foo"}})
			if err != nil || p.Items[0].Action != ActionBrewCask {
				t.Fatalf("cask の計画にならない: %v %+v", err, p.Items)
			}
			res := Apply(ctx, w.applyOpts(), p)
			if len(res.Failed) != 1 || !exists(cache) {
				t.Errorf("本体を消せなかったのに残骸を動かした / 失敗を報告しない: %+v", res)
			}
			_ = app
		})
	}
}

func TestApplyUninstallsCaskAndFormulaWithoutDangerousFlags(t *testing.T) {
	w := newWorld(t)
	w.addApp("foo", "com.ex.foo")
	w.casks = `{"casks":[{"token":"cask-foo","artifacts":[{"app":["foo.app"]}]}]}`
	mkdir(t, filepath.Join(w.cellar, "tool"))
	w.leaves = []string{"tool"}
	ctx := context.Background()
	p, err := BuildPlan(ctx, w.opts(), Selection{Apps: []string{"com.ex.foo"}, Formulae: []string{"tool"}})
	if err != nil {
		t.Fatal(err)
	}
	res := Apply(ctx, w.applyOpts(), p)
	if len(res.Done) != 2 || len(res.Failed)+len(res.Skipped) != 0 {
		t.Fatalf("結果: %+v", res)
	}
	for _, c := range w.calls {
		if strings.HasPrefix(c, "brew uninstall") && (strings.Contains(c, "--zap") || strings.Contains(c, "--force") || strings.Contains(c, "--ignore-dependencies")) {
			t.Errorf("危ない flag を付けた: %s", c)
		}
		if strings.Contains(c, "autoremove") {
			t.Errorf("autoremove を自動で走らせた: %s", c)
		}
	}
}

func TestAllowedTrashRefusesOutsideKnownPlaces(t *testing.T) {
	w := newWorld(t)
	o := w.applyOpts()
	for _, it := range []Item{
		{Tier: TierApp, Path: filepath.Join(w.home, "Documents", "x.app")},
		{Tier: TierApp, Path: filepath.Join(w.apps, "sub", "x.app")},
		{Tier: TierCache, Path: filepath.Join(w.home, "Library", "Caches")},
		{Tier: TierData, Path: filepath.Join(w.home, "Library", "Keychains", "login.keychain-db")},
		{Tier: TierData, Path: w.home + "/Library/Caches/../Keychains/x"},      // Join だと .. が消えるので文字列でつなぐ
		{Tier: TierData, Path: w.home + "/Library/Caches/x/../../Keychains/y"}, // 途中の .. は Clean でしか止まらない
		{Tier: TierManual, Path: filepath.Join(w.home, "Library", "Caches", "x")},
	} {
		if o.allowedTrash(it) {
			t.Errorf("通した: %+v", it)
		}
	}
	if !o.allowedTrash(Item{Tier: TierCache, Path: filepath.Join(w.home, "Library", "Caches", "com.ex.foo")}) {
		t.Errorf("正しい残骸を断った")
	}
}

func TestReadAppHandlesWrappedIOSApp(t *testing.T) {
	w := newWorld(t)
	p := filepath.Join(w.apps, "やくそく帳.app")
	write(t, filepath.Join(p, "Wrapper", "Promise.app", "Info.plist"), fmt.Sprintf(infoPlist, "com.ex.promise"))
	write(t, filepath.Join(p, "iTunesMetadata.plist"), "x")
	app, ok := readApp(p)
	if !ok || app.BundleID != "com.ex.promise" || app.Source != SourceMAS {
		t.Errorf("iOS のアプリを読めない: %+v %v", app, ok)
	}
}

// 動いている formula を見落とさない: comm が bin / opt の symlink や素の名前でも、lsof の txt に出る Cellar の実パスで拾う。
func TestRunningFormulaIsDetectedViaLsofAndShellsAreProtected(t *testing.T) {
	w := newWorld(t)
	for _, n := range []string{"redis", "zsh", "idle"} {
		mkdir(t, filepath.Join(w.cellar, n, "1.0", "bin"))
	}
	w.leaves = []string{"redis", "zsh", "idle"}
	w.running = []string{filepath.Join(w.cellar, "redis", "1.0", "bin", "redis-server")}
	w.psComm = []string{"redis-server"} // comm は素の名前 (tmux / nvim と同じ形)。Cellar の実パスは lsof にしか出ない
	w.shell = filepath.Join(w.cellar, "zsh", "1.0", "bin", "zsh")
	ctx := context.Background()
	if _, err := BuildPlan(ctx, w.opts(), Selection{Formulae: []string{"redis"}}); err == nil {
		t.Errorf("使われている formula の計画を作った")
	}
	if _, err := BuildPlan(ctx, w.opts(), Selection{Formulae: []string{"zsh"}}); err == nil {
		t.Errorf("ログインシェルの formula の計画を作った")
	}
	if _, err := BuildPlan(ctx, w.opts(), Selection{Formulae: []string{"idle"}}); err != nil {
		t.Errorf("使われていない formula を断った: %v", err)
	}
	w.lsofFail = true
	if _, err := BuildPlan(ctx, w.opts(), Selection{Formulae: []string{"idle"}}); err == nil {
		t.Errorf("動いているかを確かめられないのに計画を作った")
	}
}

func TestVendorTracesAndSudoCaskAreManual(t *testing.T) {
	w := newWorld(t)
	w.addApp("zoom.us", "us.zoom.xos")
	w.pkgs = []string{"us.zoom.pkg.videomeeting"}
	w.addApp("Docker", "com.docker.docker")
	w.casks = `{"casks":[{"token":"docker-desktop","artifacts":[{"app":["Docker.app"],"target":"` + w.apps + `/Docker.app"},` +
		`{"uninstall":[{"quit":"com.docker.docker","delete":["/Library/PrivilegedHelperTools/com.docker.socket"]}]}]}]}`
	ctx := context.Background()
	for _, bid := range []string{"us.zoom.xos", "com.docker.docker"} {
		p, err := BuildPlan(ctx, w.opts(), Selection{Apps: []string{bid}})
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range p.Items {
			if it.Action != ActionNone {
				t.Errorf("%s: 本体の外に何かを置くアプリを動かす計画: %+v", bid, it)
			}
		}
	}
}

func TestCaskMatchedByTargetPathNotFileName(t *testing.T) {
	w := newWorld(t)
	w.addApp("Foo", "com.ex.handmade") // 手で入れた Foo.app
	w.addApp("Bar", "com.ex.bar")
	w.casks = `{"casks":[{"token":"bar","artifacts":[{"app":["Foo.app"],"target":"` + w.apps + `/Bar.app"}]}]}`
	inv := List(context.Background(), w.opts())
	for _, a := range inv.Apps {
		switch a.BundleID {
		case "com.ex.handmade":
			if a.Source == SourceCask {
				t.Errorf("同じ元の名前の手入れの .app を cask と取り違えた: %+v", a)
			}
		case "com.ex.bar":
			if a.CaskToken != "bar" {
				t.Errorf("target の .app を cask と結び付けない: %+v", a)
			}
		}
	}
}

func TestDuplicateBundleIDAndByNameAndAppleDirs(t *testing.T) {
	w := newWorld(t)
	w.addApp("Foo", "com.ex.foo")
	ctx := context.Background()
	byName := w.lib("Application Support/Foo")
	appleCache := w.lib("Caches/com.apple.helpd/x")
	w.casks = ""
	app := App{Name: "Foo", BundleID: "com.ex.foo", zap: []string{"~/Library/Caches/com.apple.helpd/x"}}
	for _, l := range leftovers(w.home, app) {
		if l.Path == appleCache && l.Tier != TierManual {
			t.Errorf("Apple の dir の中を動かす段にした: %+v", l)
		}
	}
	p, err := BuildPlan(ctx, w.opts(), Selection{Apps: []string{"com.ex.foo"}, Data: []string{"com.ex.foo"}})
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(pathsOf(p.Items, ActionTrash), byName) {
		t.Errorf("名前で一致した dir を動かす計画にした")
	}
	w.addApp("Foo Copy", "com.ex.foo")
	if _, err := BuildPlan(ctx, w.opts(), Selection{Apps: []string{"com.ex.foo"}}); err == nil {
		t.Errorf("同じ bundle ID のアプリが 2 つあるのに計画を作った")
	}
	for _, id := range []string{"Homebrew", "ByHost", "go"} {
		if validBundleID(id) {
			t.Errorf("1 段の ID を通した: %q", id)
		}
	}
	if !isApple("COM.APPLE.Safari") || !isApple("com.apple") {
		t.Errorf("Apple の判定が大小文字・末尾で漏れる")
	}
}

// apply のときだけ cask の台帳が読めないと、本体の動かし方が変わって計画と合わない。そのときは残骸にも触らない。
func TestApplyStopsWhenAppActionChangedSincePlan(t *testing.T) {
	w := newWorld(t)
	w.addApp("foo", "com.ex.foo")
	cache := w.lib("Caches/com.ex.foo")
	w.casks = `{"casks":[{"token":"cask-foo","artifacts":[{"app":["foo.app"]}]}]}`
	ctx := context.Background()
	p, err := BuildPlan(ctx, w.opts(), Selection{Apps: []string{"com.ex.foo"}})
	if err != nil {
		t.Fatal(err)
	}
	w.casks = "not json"
	res := Apply(ctx, w.applyOpts(), p)
	if !exists(cache) || len(res.Done) != 0 {
		t.Errorf("本体が計画と合わないのに残骸を動かした: %+v", res)
	}
}

// App Store のインストールは bundle ID の名前で pkg の受領を作るので、それだけでは止めない。root の常駐があれば止める。
func TestAppStoreReceiptIsNotATraceButDaemonIs(t *testing.T) {
	w := newWorld(t)
	p := w.addApp("Cam", "com.ex.cam")
	mkdir(t, filepath.Join(p, "Contents", "_MASReceipt"))
	w.pkgs = []string{"com.ex.cam"}
	ctx := context.Background()
	plan, err := BuildPlan(ctx, w.opts(), Selection{Apps: []string{"com.ex.cam"}})
	if err != nil || plan.Items[0].Action != ActionTrash {
		t.Fatalf("App Store の受領だけで止めた: %v %+v", err, plan.Items)
	}
	write(t, filepath.Join(w.root, "LaunchDaemons", "com.ex.helper.plist"), "x")
	plan, err = BuildPlan(ctx, w.opts(), Selection{Apps: []string{"com.ex.cam"}})
	if err != nil || plan.Items[0].Action != ActionNone {
		t.Errorf("同じ開発元の root の常駐があるのに本体を動かす計画: %v %+v", err, plan.Items)
	}
}

// 探す置き場の外にある同じ bundle ID のコピー (開発中のビルド) を Spotlight で見つけたら、計画を作らない。
func TestCopiesOutsideAppDirsBlockThePlan(t *testing.T) {
	w := newWorld(t)
	w.addApp("Menu", "com.ex.menu")
	ctx := context.Background()
	if _, err := BuildPlan(ctx, w.opts(), Selection{Apps: []string{"com.ex.menu"}}); err != nil {
		t.Fatalf("コピーが無いのに断った: %v", err)
	}
	dev := filepath.Join(w.root, "src", "Debug", "Menu.app")
	write(t, filepath.Join(dev, "Contents", "Info.plist"), fmt.Sprintf(infoPlist, "COM.EX.MENU")) // 大小だけが違う
	w.copies = []string{dev}
	if _, err := BuildPlan(ctx, w.opts(), Selection{Apps: []string{"com.ex.menu"}}); err == nil || !strings.Contains(err.Error(), dev) {
		t.Errorf("置き場の外に同じ bundle ID のコピーがあるのに計画を作った / コピーの場所を出さない: %v", err)
	}
	w.copies = nil
	w.noIndex = true
	if _, err := BuildPlan(ctx, w.opts(), Selection{Apps: []string{"com.ex.menu"}}); err == nil {
		t.Errorf("Spotlight で確かめられないのに計画を作った")
	}
}

func TestFormulaUsedByShellConfigOrArgsIsKept(t *testing.T) {
	w := newWorld(t)
	for _, n := range []string{"zsh-autosuggestions", "redis", "idle"} {
		mkdir(t, filepath.Join(w.cellar, n, "1.0"))
	}
	w.leaves = []string{"zsh-autosuggestions", "redis", "idle"}
	write(t, filepath.Join(w.home, ".zshrc"), `source "$brew_prefix/share/zsh-autosuggestions/zsh-autosuggestions.zsh"`+"\n")
	prefix := filepath.Dir(w.cellar)
	// root で動く常駐: lsof に見えず、comm は setproctitle で書き換わっていて、args にだけ opt のパスが出る
	w.psComm = []string{prefix + "/opt/redis/bin/redis-server 127.0.0.1:6379"}
	// Cellar の中のスクリプトをシステムの python が動かしている: comm にも lsof の txt にも出ず、args にだけパスが出る
	mkdir(t, filepath.Join(w.cellar, "scripty", "1.0"))
	w.leaves = append(w.leaves, "scripty")
	w.psArgs = append(slices.Clone(w.psComm), "/usr/bin/python3 "+w.cellar+"/scripty/1.0/libexec/run.py")
	ctx := context.Background()
	for _, f := range []string{"zsh-autosuggestions", "redis", "scripty"} {
		if _, err := BuildPlan(ctx, w.opts(), Selection{Formulae: []string{f}}); err == nil {
			t.Errorf("%s を消す計画を作った", f)
		}
	}
	if _, err := BuildPlan(ctx, w.opts(), Selection{Formulae: []string{"idle"}}); err != nil {
		t.Errorf("使われていない formula を断った: %v", err)
	}
}

func TestRiskyCaskAndAppStoreReceiptOfAnotherApp(t *testing.T) {
	w := newWorld(t)
	w.addApp("A", "com.ex.a")
	w.addApp("B1", "com.ex.b1")
	w.addApp("B2", "com.ex.b2")
	w.casks = `{"casks":[` +
		`{"token":"a","artifacts":[{"app":["A.app"]},{"uninstall":[{"quit":"com.ex.a","trash":"~/Library/Application Support/A"}]}]},` +
		`{"token":"b","artifacts":[{"app":["B1.app"]},{"app":["B2.app"]}]}]}`
	tool := w.addApp("Tool", "com.dev.tool") // 手で入れた
	store := w.addApp("Store", "com.dev.store")
	mkdir(t, filepath.Join(store, "Contents", "_MASReceipt"))
	w.pkgs = []string{"com.dev.store"} // 同じ開発元の App Store アプリの受領
	_ = tool
	inv := List(context.Background(), w.opts())
	for _, a := range inv.Apps {
		switch a.BundleID {
		case "com.ex.a", "com.ex.b1", "com.ex.b2":
			if a.Manual == "" {
				t.Errorf("計画の外を消す cask を動かせる扱いにした: %+v", a)
			}
		case "com.dev.tool":
			if a.Manual != "" {
				t.Errorf("他のアプリの App Store の受領を開発元の痕跡に数えた: %+v", a)
			}
		}
	}
}

// シェルの設定がコマンドとして呼ぶ formula も守る (formula の名前とコマンドの名前が違っても、symlink を解いた先で判定する)。
func TestShellConfigCommandsProtectFormulae(t *testing.T) {
	w := newWorld(t)
	prefix := filepath.Dir(w.cellar)
	for name, bin := range map[string]string{"direnv": "direnv", "neovim": "nvim", "idle": "idle"} {
		real := filepath.Join(w.cellar, name, "1.0", "bin", bin)
		write(t, real, "bin")
		mkdir(t, filepath.Join(prefix, "bin"))
		if err := os.Symlink(real, filepath.Join(prefix, "bin", bin)); err != nil {
			t.Fatal(err)
		}
	}
	w.leaves = []string{"direnv", "neovim", "idle"}
	write(t, filepath.Join(w.home, ".zshrc"), "eval \"$(direnv hook zsh)\"\nexport EDITOR=\""+prefix+"/bin/nvim\"\n")
	ctx := context.Background()
	for _, f := range []string{"direnv", "neovim"} {
		if _, err := BuildPlan(ctx, w.opts(), Selection{Formulae: []string{f}}); err == nil {
			t.Errorf("設定から使われている %s を消す計画を作った", f)
		}
	}
	if _, err := BuildPlan(ctx, w.opts(), Selection{Formulae: []string{"idle"}}); err != nil {
		t.Errorf("使われていない formula を断った: %v", err)
	}
}

// Spotlight の索引の外 (ドットで始まる dir) で動いているコピーも、実行ファイルの外側の .app の bundle ID で拾う。
func TestRunningCopyOutsideSpotlightIndexBlocks(t *testing.T) {
	w := newWorld(t)
	w.addApp("Menu", "com.ex.menu")
	hidden := filepath.Join(w.root, ".local", "Menu.app")
	write(t, filepath.Join(hidden, "Contents", "Info.plist"), fmt.Sprintf(infoPlist, "com.ex.menu"))
	w.running = []string{filepath.Join(hidden, "Contents", "MacOS", "Menu")}
	if _, err := BuildPlan(context.Background(), w.opts(), Selection{Apps: []string{"com.ex.menu"}}); err == nil {
		t.Errorf("索引の外で動いている同じ bundle ID のコピーを見落とした")
	}
}

func TestUnknownCaskDirectivesAndHandmadeReceipt(t *testing.T) {
	w := newWorld(t)
	w.addApp("Pre", "com.ex.pre")
	w.addApp("Bad", "com.ex.bad")
	w.addApp("Hand", "com.v.hand") // 手で入れた。pkgutil --file-info は外れるが、同じ名前の受領が残っている
	w.pkgs = []string{"com.v.hand"}
	w.casks = `{"casks":[` +
		`{"token":"pre","artifacts":[{"app":["Pre.app"]},{"uninstall":[{"quit":"com.ex.pre","uninstall_preflight":true}]}]},` +
		`{"token":"bad","artifacts":[{"app":["Bad.app"]},{"uninstall":"not-a-list"}]}]}`
	for _, a := range List(context.Background(), w.opts()).Apps {
		if a.Manual == "" {
			t.Errorf("知らない手順・読めない手順・手入れのアプリの受領を見落とした: %+v", a)
		}
	}
}

// 実行時に呼ぶだけのスクリプト・hook・設定は lsof にも ps にも出ない。断らずに、計画に「使われているかもしれない」と出す。
func TestFormulaUsedByScriptsIsFlaggedNotBlocked(t *testing.T) {
	w := newWorld(t)
	prefix := filepath.Dir(w.cellar)
	for name, bin := range map[string]string{"jq": "jq", "ripgrep": "rg", "idle": "idle", "direnv": "direnv"} {
		real := filepath.Join(w.cellar, name, "1.0", "bin", bin)
		write(t, real, "bin")
		mkdir(t, filepath.Join(prefix, "bin"))
		if err := os.Symlink(real, filepath.Join(prefix, "bin", bin)); err != nil {
			t.Fatal(err)
		}
	}
	w.leaves = []string{"jq", "ripgrep", "idle", "direnv"}
	hook := filepath.Join(w.home, "dotfiles", "hooks", "x.sh")
	write(t, hook, "#!/bin/sh\nin=$(cat); echo \"$in\" | jq -r .tool\n")
	write(t, filepath.Join(w.home, ".claude", "settings.json"), `{"hooks":{"Stop":[{"command":"~/dotfiles/hooks/x.sh"}]}}`)
	write(t, filepath.Join(w.home, ".config", "tool", "conf"), "grep = rg --hidden\nnot-idle-like = 1\n")
	// .zshrc が source する先の $(direnv ...) は、設定から使われる formula として守る
	write(t, filepath.Join(w.home, ".zshrc"), `source "$HOME/lib/env.zsh"`+"\n")
	write(t, filepath.Join(w.home, "lib", "env.zsh"), `eval "$(direnv hook zsh)"`+"\n")
	ctx := context.Background()
	for f, where := range map[string]string{"jq": "~/dotfiles/hooks/x.sh", "ripgrep": "~/.config/tool/conf"} {
		p, err := BuildPlan(ctx, w.opts(), Selection{Formulae: []string{f}})
		if err != nil {
			t.Fatalf("%s: 断らずに計画を作る: %v", f, err)
		}
		if !strings.Contains(p.Items[0].Note, where) {
			t.Errorf("%s: 使っている場所を計画に出さない: %+v", f, p.Items[0])
		}
	}
	p, err := BuildPlan(ctx, w.opts(), Selection{Formulae: []string{"idle"}})
	if err != nil || p.Items[0].Note != "" {
		t.Errorf("別の語 (not-idle-like) に当てた: %v %+v", err, p.Items)
	}
	if _, err := BuildPlan(ctx, w.opts(), Selection{Formulae: []string{"direnv"}}); err == nil {
		t.Errorf("source の先で呼ばれる formula を消す計画を作った")
	}
}

// 見落としやすい使われ方: 大きな設定・symlink の先の設定・${VAR:-x} と末尾の . ・hook の 2 段目・拡張子の無いスクリプト・bin の無い formula・tmux の run-shell。
func TestUsageCorpusFollowsWhatTheReviewFound(t *testing.T) {
	w := newWorld(t)
	prefix := filepath.Dir(w.cellar)
	cmds := map[string]string{"shfmt": "shfmt", "jq": "jq", "neovim": "nvim", "ripgrep": "rg", "gnu-sed": "gsed", "zsh-syntax-highlighting": "", "zsh-autosuggestions": "", "tmuxfmt": "tmuxfmt"}
	for name, bin := range cmds {
		dir := filepath.Join(w.cellar, name, "1.0")
		mkdir(t, dir)
		if bin != "" {
			write(t, filepath.Join(dir, "bin", bin), "bin")
		}
		w.leaves = append(w.leaves, name)
	}
	pad := strings.Repeat("# padding\n", 8000) // 64KB を超える設定
	write(t, filepath.Join(w.home, ".zshrc"), pad+"source "+prefix+"/share/zsh-syntax-highlighting/x.zsh\nexport J=${JQ:-jq}\nalias v=nvim.\n")
	dots := filepath.Join(w.home, "dotfiles")
	write(t, filepath.Join(dots, "nvim", "init.lua"), pad+`formatters = { sh = { "shfmt" } }`+"\n")
	mkdir(t, filepath.Join(w.home, ".config"))
	if err := os.Symlink(filepath.Join(dots, "nvim"), filepath.Join(w.home, ".config", "nvim")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(w.home, ".config", "zsh", "plugins.zsh"), "source "+prefix+"/share/zsh-autosuggestions/zsh-autosuggestions.zsh\n")
	write(t, filepath.Join(dots, "hooks", "a.sh"), "#!/bin/sh\n. \"$(dirname \"$0\")/lib/l.sh\"\n")
	write(t, filepath.Join(dots, "hooks", "lib", "l.sh"), "rg -n foo\n")
	write(t, filepath.Join(w.home, "bin", "statusline"), "#!/bin/sh\ngsed -n 1p\n")
	write(t, filepath.Join(w.home, ".claude", "settings.json"), `{"hooks":{"Stop":[{"command":"~/dotfiles/hooks/a.sh input"}]},"statusLine":{"command":"~/bin/statusline"}}`)
	write(t, filepath.Join(w.home, ".tmux.conf"), `run-shell "${DOTFILES_DIR:-$HOME/dotfiles}/scripts/t.sh"`+"\n")
	write(t, filepath.Join(dots, "scripts", "t.sh"), "tmuxfmt --check\n")

	inv := List(context.Background(), w.opts())
	got := map[string]Formula{}
	for _, f := range inv.Formulae {
		got[f.Name] = f
	}
	if got["zsh-syntax-highlighting"].Protected == "" {
		t.Errorf("64KB を超える .zshrc から source する formula を守らない (退行): %+v", got["zsh-syntax-highlighting"])
	}
	for _, name := range []string{"shfmt", "jq", "neovim", "ripgrep", "gnu-sed", "zsh-autosuggestions", "tmuxfmt"} {
		if f := got[name]; f.Protected == "" && len(f.UsedBy) == 0 {
			t.Errorf("%s: 使っている場所を見落とした: %+v", name, f)
		}
	}
}

// 絶対パスで書いた source と hook は辿り、履歴と ~/Library は辿らない・
// 汎用の dir の名前 (/go/) をパスの一致に数えない。
func TestUsageFollowsAbsoluteButNotHistory(t *testing.T) {
	w := newWorld(t)
	for name, bin := range map[string]string{"gum": "gum", "pstree": "pstree", "vhs": "vhs", "go": "gofmt"} {
		write(t, filepath.Join(w.cellar, name, "1.0", "bin", bin), "bin")
		w.leaves = append(w.leaves, name)
	}
	lib := filepath.Join(w.root, "abs", "lib", "a.zsh")
	write(t, lib, "x=$(gum choose)\n")
	hook := filepath.Join(w.root, "abs", "hooks", "x.sh")
	write(t, hook, "pstree -p $$\ntail -1 ~/.zsh_history >/dev/null\nls ~/Library/Keychains/login.keychain-db\n") // スクリプトの本文の ~ は辿るが、履歴と ~/Library は読まない
	hist := filepath.Join(w.home, ".zsh_history")
	write(t, hist, "vhs demo.tape\ncat ~/Library/Keychains/login.keychain-db\n")
	write(t, filepath.Join(w.home, "Library", "Keychains", "login.keychain-db"), "vhs secret\n")
	write(t, filepath.Join(w.home, ".zshrc"), "source "+lib+"\nHISTFILE=$HOME/.zsh_history\nexport GOPATH=$HOME/go/\n")
	write(t, filepath.Join(w.home, ".claude", "settings.json"), `{"hooks":{"Stop":[{"command":"`+hook+` input"}]}}`)

	inv := List(context.Background(), w.opts())
	got := map[string]Formula{}
	for _, f := range inv.Formulae {
		got[f.Name] = f
	}
	if f := got["gum"]; f.Protected == "" && len(f.UsedBy) == 0 {
		t.Errorf("絶対パスで source した先の $(gum) を見落とした: %+v", f)
	}
	if f := got["pstree"]; len(f.UsedBy) == 0 {
		t.Errorf("絶対パスで書いた hook の中の pstree を見落とした: %+v", f)
	}
	if f := got["vhs"]; f.Protected != "" || len(f.UsedBy) != 0 {
		t.Errorf("履歴とキーチェーンを読んで vhs に印を付けた: %+v", f)
	}
	if f := got["go"]; f.Protected != "" {
		t.Errorf("$HOME/go/ を go の formula を消せない理由と読んだ: %+v", f)
	}
}

func TestCorpusReadsSameFileOnceAndStopsAtBudget(t *testing.T) {
	w := newWorld(t)
	real := filepath.Join(w.home, "big", "real.txt")
	write(t, real, strings.Repeat("x\n", 1000))
	var b strings.Builder
	for i := range 30 {
		link := filepath.Join(w.home, "big", fmt.Sprintf("l%d.txt", i))
		if err := os.Symlink(real, link); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, "source %s\n", link)
	}
	write(t, filepath.Join(w.home, ".zshrc"), b.String())
	u := buildUsage(w.home)
	n := 0
	for _, f := range u.files {
		if strings.Contains(f.text, "x\nx\n") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("同じ実体を %d 回読んだ (1 回のはず)", n)
	}
}
