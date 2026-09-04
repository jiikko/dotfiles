package disk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// chromiumApp は Chromium (Electron) のプロファイルらしいディレクトリを作る。
//   - markers: 目印 ("Preferences" はファイル / "Local Storage" はディレクトリ)
//   - probe: 走行中なら開かれているファイル (`Local Storage/leveldb/LOCK`) を置くか
//
// 🚨 probe を置かないと `appIsLive` は「不在を証明できない = live」に倒れる (fail-closed)。
// 実アプリはこれを持つので、**候補になる側を試すテストでは必ず置く**。
func chromiumApp(t *testing.T, env Env, app string, markers []string, probe bool) string {
	t.Helper()
	dir := filepath.Join(env.Home, "Library", "Application Support", app)
	for _, m := range markers {
		if m == "Preferences" {
			mkfile(t, filepath.Join(dir, m), 1)
			continue
		}
		if err := os.MkdirAll(filepath.Join(dir, m), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if probe {
		// 🚨 probe は **目印を兼ねないもの**を使う (`Local Storage/leveldb/LOCK` にすると
		// `Local Storage` ごと作られ、「Preferences しか無いアプリ」を作れなくなる。
		// そのせいで「目印を 1 つに緩める」変異が素通りしていた)
		mkfile(t, filepath.Join(dir, "SingletonLock"), 1)
	}
	mkfile(t, filepath.Join(dir, "Cache", "x"), 8)
	return dir
}

func fullMarkers() []string { return []string{"Preferences", "Local Storage"} }

func chromiumEntry() Entry {
	return Entry{ID: "chromium-cache", Risk: RiskSafe, DeleteVia: "rm", Guard: GuardChromiumCache,
		Paths: []string{"~/Library/Application Support/*/Cache"}}
}

// lsofKey は fakeRunner に登録するキー (appIsLive が組む引数の先頭に合わせる)。
func lsofKey(app string) string { return lsofKeyFor(app, "SingletonLock") }

// lsofKeyFor は probe を名指しして fakeRunner のキーを組む。
func lsofKeyFor(app, probe string) string { return "lsof -t -- " + filepath.Join(app, probe) }

func itemApps(r Result) []string {
	out := make([]string, 0, len(r.Items))
	for _, it := range r.Items {
		out = append(out, filepath.Base(filepath.Dir(it.Path)))
	}
	return out
}

// 🚨 **これがこの guard の本番**: ディレクトリ名とプロセス名が違うアプリ。
// この機の `/Applications/VLC Multi-Video Player.app` は CFBundleExecutable が
// `VLCMultiVideoPlayer` で、置き場は `Multi-Video Player for Dropbox`。
// `pgrep -x` は永久に当たらないので、**プロセス名だけで判定すると走行中の実体を消す**。
func TestChromiumCacheDetectsLiveAppWhoseNameDiffersFromDir(t *testing.T) {
	env := testEnv(t)
	live := chromiumApp(t, env, "Multi-Video Player for Dropbox", fullMarkers(), true)
	quiet := chromiumApp(t, env, "Quiet", fullMarkers(), true)
	run := &fakeRunner{resp: map[string]fakeResp{
		// pgrep はどちらも「そんな名前のプロセスは無い」= 名前一致では捕まえられない
		"pgrep -x":     {rc: 1},
		lsofKey(live):  {rc: 1, out: "4242\n"}, // 開いているプロセスが居る
		lsofKey(quiet): {rc: 1, out: ""},       // 誰も開いていない
	}}
	r := scanOne(t, env, run, chromiumEntry(), okBoot)
	if got := itemApps(r); strings.Join(got, ",") != "Quiet" {
		t.Fatalf("名前が一致しない走行中アプリを候補にした: %v (status=%s reason=%s)", got, r.Status, r.Reason)
	}
}

// 目印は 2 つとも要る (片方だけでは通さない)。型も見る (symlink / 種別違いを認めない)。
func TestChromiumCacheShapeRequiresAllMarkers(t *testing.T) {
	env := testEnv(t)
	quiet := chromiumApp(t, env, "Quiet", fullMarkers(), true)
	// 🚨 probe を**置く**。置かないと liveness 側 (probe 無し = live 扱い) で落ちてしまい、
	// 「目印を 1 つに緩める」変異を形状検査が見逃しても候補に出ず、テストが素通りする
	onlyPrefs := chromiumApp(t, env, "OnlyPrefs", []string{"Preferences"}, true)
	onlyLS := chromiumApp(t, env, "OnlyLS", []string{"Local Storage"}, true)
	// Preferences が**ディレクトリ**のアプリ (種別違い)
	wrongType := chromiumApp(t, env, "WrongType", []string{"Local Storage"}, true)
	if err := os.MkdirAll(filepath.Join(wrongType, "Preferences"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 目印が symlink のアプリ (中身は別物を指しうる)
	symApp := chromiumApp(t, env, "SymMarker", []string{"Local Storage"}, true)
	if err := os.Symlink(filepath.Join(quiet, "Preferences"), filepath.Join(symApp, "Preferences")); err != nil {
		t.Fatal(err)
	}
	resp := map[string]fakeResp{}
	for _, a := range []string{quiet, onlyPrefs, onlyLS, wrongType, symApp} {
		resp[lsofKey(a)] = fakeResp{rc: 1, out: ""}
	}
	r := scanOne(t, env, &fakeRunner{resp: resp}, chromiumEntry(), okBoot)
	if got := itemApps(r); strings.Join(got, ",") != "Quiet" {
		t.Fatalf("目印が揃わないディレクトリを候補にした: %v (status=%s reason=%s)", got, r.Status, r.Reason)
	}
}

// 目印は**削除対象と重ねない**。重ねると、消した瞬間にそのアプリが検出圏外へ落ちる
// (アプリを再起動するまで「もう溜まっていない」ように見える)。
func TestChromiumMarkersAreNotDeleteTargets(t *testing.T) {
	var targets []string
	for _, e := range catalog {
		if e.Guard != GuardChromiumCache {
			continue
		}
		for _, p := range e.Paths {
			targets = append(targets, filepath.Base(p))
		}
	}
	if len(targets) == 0 {
		t.Fatal("GuardChromiumCache のエントリがカタログに無い (検査が空振りしている)")
	}
	for _, m := range chromiumProfileMarkers {
		for _, tg := range targets {
			if m.name == tg {
				t.Errorf("目印 %q が削除対象 (%v) と重なっている", m.name, targets)
			}
		}
	}
}

// 全部が起動中なら **blocked** にする (黙って候補 0 件にすると Foldable が行ごと畳み、
// 2.7GB が「候補なし = きれい」と同じ見え方になる)。
func TestChromiumCacheBlocksInsteadOfFoldingWhenAllRunning(t *testing.T) {
	env := testEnv(t)
	a := chromiumApp(t, env, "AppOne", fullMarkers(), true)
	b := chromiumApp(t, env, "AppTwo", fullMarkers(), true)
	run := &fakeRunner{resp: map[string]fakeResp{
		"pgrep -x": {rc: 1},
		lsofKey(a): {rc: 1, out: "1\n"},
		lsofKey(b): {rc: 1, out: "2\n"},
	}}
	r := scanOne(t, env, run, chromiumEntry(), okBoot)
	if r.Status != StatusBlocked {
		t.Fatalf("全部起動中なのに blocked にしていない: status=%s items=%d", r.Status, len(r.Items))
	}
	if Foldable(r) {
		t.Error("行が畳まれる (候補なしと同じ見え方になる)")
	}
	for _, name := range []string{"AppOne", "AppTwo"} {
		if !strings.Contains(r.Reason, name) {
			t.Errorf("理由に %s が出ていない: %s", name, r.Reason)
		}
	}
}

// 判定できないときは触らない側へ倒す: probe が 1 つも無ければ live 扱い。
// lsof / pgrep の実行そのものが失敗したらエントリごと failed。
func TestChromiumCacheFailsClosed(t *testing.T) {
	t.Run("probe が無ければ候補にしない", func(t *testing.T) {
		env := testEnv(t)
		chromiumApp(t, env, "NoProbe", fullMarkers(), false)
		r := scanOne(t, env, &fakeRunner{}, chromiumEntry(), okBoot)
		if len(r.Items) != 0 {
			t.Errorf("開いているか確かめられないのに候補にした: %v", itemApps(r))
		}
	})
	t.Run("lsof の実行失敗は failed", func(t *testing.T) {
		env := testEnv(t)
		app := chromiumApp(t, env, "Quiet", fullMarkers(), true)
		r := scanOne(t, env, &fakeRunner{resp: map[string]fakeResp{
			"pgrep -x":   {rc: 1},
			lsofKey(app): {err: os.ErrNotExist},
		}}, chromiumEntry(), okBoot)
		if r.Status != StatusFailed || len(r.Items) != 0 {
			t.Errorf("lsof の実行失敗を畳んだ: status=%s items=%d", r.Status, len(r.Items))
		}
	})
}

// 🚨 除外リストは**展開後のパス**に対しても効く。glob (`Application Support/*`) は
// 静的には除外ルートで始まらないので、テンプレート照合だけでは素通りする。
func TestExcludedRootIsEnforcedAfterGlobExpansion(t *testing.T) {
	env := testEnv(t)
	g := chromiumApp(t, env, "Google", fullMarkers(), true) // excludedRoots に載っている
	q := chromiumApp(t, env, "Quiet", fullMarkers(), true)
	run := &fakeRunner{resp: map[string]fakeResp{
		lsofKey(g): {rc: 1, out: ""}, lsofKey(q): {rc: 1, out: ""},
	}}
	r := scanOne(t, env, run, chromiumEntry(), okBoot)
	for _, it := range r.Items {
		if strings.Contains(it.Path, "/Google/") {
			t.Fatalf("除外ルートに踏み込んだ: %s", it.Path)
		}
	}
	var noted bool
	for _, f := range r.Failures {
		if strings.Contains(f, "除外リスト") && strings.Contains(f, "Google") {
			noted = true
		}
	}
	if !noted {
		t.Errorf("除外したことが記録に残っていない: %v", r.Failures)
	}
	if got := itemApps(r); strings.Join(got, ",") != "Quiet" {
		t.Errorf("除外が他のアプリまで落とした: %v", got)
	}
}

// excludedRootFor は展開・正規化してから比べる (テンプレートのまま比べると必ず外れる)。
func TestExcludedRootForResolvesTemplates(t *testing.T) {
	env := testEnv(t)
	for _, tc := range []struct{ path, want string }{
		{filepath.Join(env.Home, "Downloads", "x"), "~/Downloads"},
		{filepath.Join(env.Home, "src", "repo", "node_modules"), "~/src"},
		{filepath.Join(env.Home, "Library", "Application Support", "Google", "Cache"), "~/Library/Application Support/Google"},
		// 大文字小文字が違っても同じファイル (APFS の既定は case-insensitive)
		{filepath.Join(env.Home, "downloads", "x"), "~/Downloads"},
		{filepath.Join(env.Home, "Library", "Application Support", "Slack", "Cache"), ""},
		// 前方一致だけで判定しない (Downloads2 は Downloads ではない)
		{filepath.Join(env.Home, "Downloads2", "x"), ""},
	} {
		got, err := excludedRootFor(env, tc.path)
		if err != nil {
			t.Fatalf("%s: %v", tc.path, err)
		}
		if got != tc.want {
			t.Errorf("%s: excludedRootFor()=%q, want %q", tc.path, got, tc.want)
		}
	}
	if _, err := excludedRootFor(Env{}, "~/Downloads/x"); err == nil {
		t.Error("HOME が空でも判定できたことにしている (fail-closed でない)")
	}
}

// カタログ側の配線を pin する。guard を落とす / glob を広げる退行は、guard の中身を
// 試すテストでは**原理的に検出できない** (敵対レビュー 2026-09-04)。
func TestCatalogChromiumEntryIsWired(t *testing.T) {
	var found bool
	for _, e := range catalog {
		if e.ID != "chromium-cache" {
			continue
		}
		found = true
		if e.Guard != GuardChromiumCache {
			t.Errorf("guard が外れている: %q (Application Support/* に無防備な rm が張られる)", e.Guard)
		}
		// 🚨 完全一致で固定する。部分一致だと「1 段浅くする」「*/* へ広げる」を素通しする
		want := []string{
			"~/Library/Application Support/*/Cache",
			"~/Library/Application Support/*/Code Cache",
			"~/Library/Application Support/*/GPUCache",
		}
		if strings.Join(e.Paths, "\n") != strings.Join(want, "\n") {
			t.Errorf("Paths が変わっている:\n got %v\nwant %v", e.Paths, want)
		}
	}
	if !found {
		t.Fatal("chromium-cache がカタログに無い")
	}
}

// probe は**どれか 1 つでも実在すれば**それで判定する。1 本ずつ効いていることを見る
// (fixture が 1 種類しか持たないと、他の probe を消す退行を検出できない)。
func TestChromiumCacheUsesEachProbe(t *testing.T) {
	for _, probe := range []string{filepath.Join("Local Storage", "leveldb", "LOCK"), "SingletonLock"} {
		t.Run(probe, func(t *testing.T) {
			env := testEnv(t)
			dir := filepath.Join(env.Home, "Library", "Application Support", "App")
			mkfile(t, filepath.Join(dir, "Preferences"), 1)
			if err := os.MkdirAll(filepath.Join(dir, "Local Storage"), 0o755); err != nil {
				t.Fatal(err)
			}
			mkfile(t, filepath.Join(dir, "Cache", "x"), 8)
			mkfile(t, filepath.Join(dir, probe), 1)
			run := &fakeRunner{resp: map[string]fakeResp{lsofKeyFor(dir, probe): {rc: 0, out: "77\n"}}}
			if r := scanOne(t, env, run, chromiumEntry(), okBoot); len(r.Items) != 0 {
				t.Errorf("%s で起動中を検出できていない: %v", probe, itemApps(r))
			}
			run = &fakeRunner{resp: map[string]fakeResp{lsofKeyFor(dir, probe): {rc: 1, out: ""}}}
			if r := scanOne(t, env, run, chromiumEntry(), okBoot); len(r.Items) != 1 {
				t.Errorf("%s で停止中を候補にできていない: %v (status=%s)", probe, itemApps(r), r.Status)
			}
		})
	}
}

// probe は**削除対象の内側に置かない**。置くと 1 回消した時点で probe が消え、
// そのアプリが永久に live 扱い = 二度と候補に出なくなる。
func TestLivenessProbesAreNotInsideDeleteTargets(t *testing.T) {
	var targets []string
	for _, e := range catalog {
		if e.Guard == GuardChromiumCache {
			for _, p := range e.Paths {
				targets = append(targets, filepath.Base(p))
			}
		}
	}
	if len(targets) == 0 {
		t.Fatal("GuardChromiumCache のエントリがカタログに無い (検査が空振りしている)")
	}
	for _, probe := range livenessProbes {
		head := strings.Split(probe, string(filepath.Separator))[0]
		for _, tg := range targets {
			if head == tg {
				t.Errorf("probe %q が削除対象 (%v) の内側にある", probe, targets)
			}
		}
	}
}

// 形状検査で**全滅**したときも行が畳まれない (「候補なし = きれい」と同じ見え方にしない)。
// 起動中で全滅したときは guard が blocked を返し、形状で全滅したときは Unverified が受ける。
func TestChromiumCacheIsNotFoldedWhenAllDroppedByShape(t *testing.T) {
	env := testEnv(t)
	dir := filepath.Join(env.Home, "Library", "Application Support", "Mystery")
	mkfile(t, filepath.Join(dir, "Cache", "x"), 8)
	if r := scanOne(t, env, &fakeRunner{}, chromiumEntry(), okBoot); len(r.Items) != 0 {
		t.Fatalf("目印の無いディレクトリを候補にした: %v", itemApps(r))
	}
	var cat Entry
	for _, e := range catalog {
		if e.ID == "chromium-cache" {
			cat = e
		}
	}
	if cat.Unverified == "" {
		t.Fatal("chromium-cache に Unverified が付いていない (形状で全滅すると畳まれて見えなくなる)")
	}
	if Foldable(Result{Entry: cat, Status: StatusOK}) {
		t.Error("0 件のとき行が畳まれる")
	}
}
