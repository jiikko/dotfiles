package uninstall

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Tier は残骸の段。既定で動かすのは TierApp と TierCache だけで、TierData はアプリごとに選んだときだけ動かす。
// TierManual は動かさず、計画に「手で確かめるもの」として載せるだけ。
type Tier string

const (
	TierApp    Tier = "app"    // 本体 (.app / Cellar)
	TierCache  Tier = "cache"  // 作り直されるもの (キャッシュ・ログ・ウィンドウの状態・Cookie)
	TierData   Tier = "data"   // 設定とデータ (失うと戻らないことがある)
	TierManual Tier = "manual" // 共有されうる・sudo が要る・launchd に読み込まれている。動かさない
)

// cacheDirs / dataDirs は ~/Library の下で、アプリが自分の名前 (bundle ID) の項目を作る置き場。
// 🚨 apply はこの一覧の下にあるもの以外を動かさない (allowedLeftover)。足すときは、そこが「そのアプリだけのもの」を置く場所かを確かめる。
// WebKit・Cookies・HTTPStorages はログイン状態と localStorage / IndexedDB を持つので、作り直されるものではなく設定とデータに入れる。
var cacheDirs = []string{"Caches", "Logs", "Saved Application State"}

var dataDirs = []string{"Application Support", "Preferences", "Containers", "Application Scripts", "WebKit", "Cookies", "HTTPStorages"}

// Leftover は残骸の 1 つ。
type Leftover struct {
	Path   string `json:"path"`
	Tier   Tier   `json:"tier"`
	ByName bool   `json:"by_name,omitempty"` // bundle ID ではなくアプリの名前で一致した (同名の別物の可能性がある)
	Note   string `json:"note,omitempty"`
}

// leftovers は bundle ID の完全一致 (+ cask の zap の一覧 + アプリの名前の Application Support) で残骸を探す。
// 前方一致はしない: com.google.Chrome を探すときに com.google.* (別のアプリ) を拾わない。
func leftovers(home string, app App) []Leftover {
	lib := filepath.Join(home, "Library")
	bid := app.BundleID
	var out []Leftover
	seen := map[string]bool{}
	add := func(p string, t Tier, byName bool, note string) {
		p = filepath.Clean(p)
		if seen[p] {
			return
		}
		if _, err := os.Lstat(p); err != nil {
			return
		}
		seen[p] = true
		out = append(out, Leftover{Path: p, Tier: t, ByName: byName, Note: note})
	}
	for _, d := range cacheDirs {
		for _, name := range []string{bid, bid + ".savedState", bid + ".binarycookies"} {
			add(filepath.Join(lib, d, name), TierCache, false, "")
		}
	}
	for _, d := range dataDirs {
		for _, name := range []string{bid, bid + ".plist"} {
			add(filepath.Join(lib, d, name), TierData, false, "")
		}
	}
	// 名前で作るアプリが多い (Slack の Application Support/Slack)。同名の別物がありうるので印を付ける
	if app.Name != "" {
		add(filepath.Join(lib, "Application Support", app.Name), TierData, true, "アプリの名前で一致 (同名の別物でないかを確かめる)")
	}
	for _, z := range app.zap {
		for _, p := range expandZap(home, z) {
			t := classify(home, p)
			note := "cask の zap の一覧"
			if t == TierManual {
				note = manualNote(home, p)
			}
			add(p, t, false, note)
		}
	}
	// LaunchAgents は bundle ID で始まる plist を、動かさずに載せる
	if ents, err := os.ReadDir(filepath.Join(lib, "LaunchAgents")); err == nil {
		for _, e := range ents {
			if e.Name() == bid+".plist" || strings.HasPrefix(e.Name(), bid+".") && strings.HasSuffix(e.Name(), ".plist") {
				p := filepath.Join(lib, "LaunchAgents", e.Name())
				add(p, TierManual, false, manualNote(home, p))
			}
		}
	}
	slices.SortFunc(out, func(a, b Leftover) int { return strings.Compare(a.Path, b.Path) })
	return out
}

// expandZap は cask の zap の 1 行 (~ と glob を含む) を実在するパスにする。~ で始まらないもの (/Library など。sudo が要る) も
// 展開して返し、classify が TierManual に落とす。
func expandZap(home, pattern string) []string {
	if rest, ok := strings.CutPrefix(pattern, "~/"); ok {
		pattern = filepath.Join(home, rest)
	}
	if !filepath.IsAbs(pattern) {
		return nil
	}
	ms, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	return ms
}

// classify はパスの置き場から段を決める。~/Library/<置き場>/<何か> の形でないもの・知らない置き場は TierManual。
func classify(home, p string) Tier {
	root, ok := libraryRoot(home, p)
	switch {
	case !ok || appleOwned(home, p):
		return TierManual
	case slices.Contains(cacheDirs, root):
		return TierCache
	case slices.Contains(dataDirs, root):
		return TierData
	}
	return TierManual
}

// libraryRoot は p が ~/Library/<置き場>/<1 段以上> のとき、その置き場の名前を返す。置き場そのものは返さない。
func libraryRoot(home, p string) (string, bool) {
	lib := filepath.Join(home, "Library") + "/"
	clean := filepath.Clean(p)
	rest, ok := strings.CutPrefix(clean, lib)
	if !ok {
		return "", false
	}
	root, below, ok := strings.Cut(rest, "/")
	if !ok || below == "" || below == "." || strings.HasPrefix(below, "..") {
		return "", false
	}
	return root, true
}

// appleOwned は置き場の直下が Apple の管理する dir (com.apple.*・CloudDocs) の下か。cask の zap がそこを指すことがある
// (zoom の Application Support/CloudDocs/...、coteditor の Caches/com.apple.helpd/...) が、中身は OS のもの。
func appleOwned(home, p string) bool {
	lib := filepath.Join(home, "Library") + "/"
	rest, ok := strings.CutPrefix(filepath.Clean(p), lib)
	if !ok {
		return false
	}
	parts := strings.Split(rest, "/")
	if len(parts) < 2 {
		return false
	}
	first := strings.ToLower(parts[1])
	return strings.HasPrefix(first, "com.apple.") || first == "clouddocs"
}

// allowedLeftover は apply が動かしてよい残骸か: ~/Library/<cacheDirs か dataDirs>/<1 段以上> の下だけ。
func allowedLeftover(home, p string) bool {
	root, ok := libraryRoot(home, p)
	return ok && !appleOwned(home, p) && (slices.Contains(cacheDirs, root) || slices.Contains(dataDirs, root))
}

func manualNote(home, p string) string {
	root, ok := libraryRoot(home, p)
	switch {
	case ok && root == "Group Containers":
		return "同じ開発元の他のアプリと共有されうるので動かさない。他に同じ開発元のアプリが無ければ手で消す"
	case ok && root == "LaunchAgents":
		return "launchd に読み込まれたままのことがある。launchctl bootout gui/$(id -u) <この plist> の後に手で消す"
	}
	return "ホームの外 (sudo が要る) か、知らない置き場なので動かさない"
}
