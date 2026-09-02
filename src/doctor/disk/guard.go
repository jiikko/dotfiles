package disk

import (
	"doctor/brewledger"
	"doctor/runner"

	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"howett.net/plist"
)

// bootTime は kern.boottime。sysctl の文字列を sed で切る形 (sec と usec の取り違えで 1970 年になり
// 全件通過 / 全件棄却に化けた実例) を避け、構造体で受ける。失敗は error (呼び出し側が fail-closed)。
func bootTime() (time.Time, error) {
	tv, err := unix.SysctlTimeval("kern.boottime")
	if err != nil {
		return time.Time{}, err
	}
	if tv.Sec <= 0 {
		return time.Time{}, fmt.Errorf("kern.boottime が不正: sec=%d", tv.Sec)
	}
	return time.Unix(tv.Sec, int64(tv.Usec)), nil
}

// processRunning は完全一致でプロセスの有無を見る (pgrep -x)。部分一致は `tor` が thermalmonitord に
// 当たった実例があるので使わない。pgrep 自体が失敗したら error (fail-closed: 「起動中かもしれない」)。
func processRunning(ctx context.Context, run runner.Runner, name string) (bool, error) {
	_, stderr, rc, err := runner.WithTimeout(ctx, run, cmdTimeout, "pgrep", "-x", name)
	if err != nil {
		return false, err
	}
	switch rc {
	case 0:
		return true, nil
	case 1:
		return false, nil
	}
	return false, fmt.Errorf("pgrep: exit %d: %s", rc, strings.TrimSpace(stderr))
}

// simDeviceUDIDs は現存デバイスの UDID。既定のデバイスセットに加え、Xcode Previews のセット
// (~/Library/Developer/Xcode/UserData/Previews/Simulator Devices) も見る: Previews のデバイスは既定セットに
// 出ないので、そこだけ見ると Preview 中の生きた作業領域を孤児にする (敵対レビュー 2026-09-02 P1)。
// どちらかが失敗したら error (孤児判定を全件「孤児でない」に倒す)。
func simDeviceUDIDs(ctx context.Context, run runner.Runner, env Env) (map[string]bool, error) {
	set := map[string]bool{}
	if err := simDeviceUDIDsInto(ctx, run, nil, set); err != nil {
		return nil, err
	}
	dirs, err := simDeviceSetDirs(env)
	if err != nil {
		return nil, err
	}
	for _, dir := range dirs {
		if err := simDeviceUDIDsInto(ctx, run, []string{"--set", dir}, set); err != nil {
			return nil, fmt.Errorf("デバイスセット %s: %w", dir, err)
		}
	}
	return set, nil
}

// simDeviceSetDirs は既定セット以外の実在するデバイスセット。**列挙で持つ**: セットは増える
// (実測 2026-09-03: Xcode Previews に加え ~/Library/Developer/XCTestDevices と XCPGDevices が実在し、
// どちらも simctl --set が rc=0 で応答する)。名前を直書きすると新しいセットが増えるたびに、
// そこのデバイスの作業領域が孤児と判定される (issue 168)。
//
// 拾い方は ~/Library/Developer 直下の `*Devices` ディレクトリ + Xcode Previews の固定パス。
// 前者は「並列テストの clone (XCTestDevices)」「Playground (XCPGDevices)」を名前を知らずに拾える。
// ~/Library/Developer が読めない場合は error (fail-closed)。無視して空を返すと「セットが無い」と
// 同じ結果になり、そこに生きたデバイスがあっても孤児と判定してしまう — この関数が防いでいる
// 失敗モードそのものを、診断の痕跡を出さずに再現する (敵対レビュー 2026-09-03 で実測:
// chmod 000 で dirs が黙って空になった)。ディレクトリが無い場合だけは正常 (Xcode 未使用)。
func simDeviceSetDirs(env Env) ([]string, error) {
	var dirs []string
	dev := filepath.Join(env.Home, "Library", "Developer")
	entries, err := os.ReadDir(dev)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("デバイスセットの置き場を読めない (%s): %w", dev, err)
	}
	for _, e := range entries {
		// symlink も辿る (DirEntry.IsDir は symlink に false を返す)
		p := filepath.Join(dev, e.Name())
		if !strings.HasSuffix(e.Name(), "Devices") {
			continue
		}
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			dirs = append(dirs, p)
		}
	}
	previews := filepath.Join(dev, "Xcode", "UserData", "Previews", "Simulator Devices")
	if fi, err := os.Stat(previews); err == nil && fi.IsDir() {
		dirs = append(dirs, previews)
	}
	sort.Strings(dirs) // 呼ぶ順を決定論にする (テストが argv を固定できる)
	return dirs, nil
}

func simDeviceUDIDsInto(ctx context.Context, run runner.Runner, setArgs []string, set map[string]bool) error {
	args := append([]string{"simctl"}, setArgs...)
	args = append(args, "list", "devices", "-j")
	out, stderr, rc, err := runner.WithTimeout(ctx, run, cmdTimeout, "xcrun", args...)
	if err != nil {
		return err
	}
	if rc != 0 {
		return fmt.Errorf("simctl list devices: exit %d: %s", rc, strings.TrimSpace(stderr))
	}
	var doc struct {
		Devices map[string][]struct {
			UDID string `json:"udid"`
		} `json:"devices"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		return fmt.Errorf("simctl の JSON を解釈できない: %w", err)
	}
	for _, list := range doc.Devices {
		for _, d := range list {
			set[strings.ToUpper(d.UDID)] = true
		}
	}
	return nil
}

var simDeviceDirRe = regexp.MustCompile(`com\.apple\.CoreSimulator\.SimDevice\.([0-9A-Fa-f-]{36})`)

// simRuntime は `xcrun simctl runtime list -j` の 1 件 (走査しない。サイズは simctl の申告値)。
type simRuntime struct {
	Identifier string
	Name       string
	SizeBytes  int64
	LastUsedAt time.Time
	Path       string
}

func simRuntimes(ctx context.Context, run runner.Runner) ([]simRuntime, error) {
	out, stderr, rc, err := runner.WithTimeout(ctx, run, cmdTimeout, "xcrun", "simctl", "runtime", "list", "-j")
	if err != nil {
		return nil, err
	}
	if rc != 0 {
		return nil, fmt.Errorf("simctl runtime list: exit %d: %s", rc, strings.TrimSpace(stderr))
	}
	var doc map[string]struct {
		Identifier        string `json:"identifier"`
		RuntimeIdentifier string `json:"runtimeIdentifier"`
		Version           string `json:"version"`
		Build             string `json:"build"`
		SizeBytes         int64  `json:"sizeBytes"`
		LastUsedAt        string `json:"lastUsedAt"`
		Path              string `json:"path"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		return nil, fmt.Errorf("simctl の JSON を解釈できない: %w", err)
	}
	var rts []simRuntime
	for _, r := range doc {
		rt := simRuntime{Identifier: r.Identifier, SizeBytes: r.SizeBytes, Path: r.Path,
			Name: fmt.Sprintf("%s (%s %s)", r.RuntimeIdentifier, r.Version, r.Build)}
		if t, err := time.Parse(time.RFC3339, r.LastUsedAt); err == nil {
			rt.LastUsedAt = t
		}
		rts = append(rts, rt)
	}
	return rts, nil
}

// installedBundleIDs は AppDirs 配下を再帰して bundle (.app / .appex / .xpc / .plugin 等) の Info.plist から
// CFBundleIdentifier を集める。mdfind (Spotlight) は使わない: インデックス欠落で実在するアプリを「無い」と
// 答えた実例がある。
//
// ⚠️ 直下の .app だけを見ると偽陽性になる (敵対レビュー 2026-09-02 P1): `/Applications/Adobe Acrobat DC/
// Adobe Acrobat.app` のようにフォルダに入った app、app 内蔵の appex / XPC / LoginItems は別の bundle id で
// コンテナを持つ。走査は深さ上限つきで bundle の中にも入る。
// 走査できないディレクトリがあれば error (fail-closed: 孤児判定をしない)。存在しない dir は無視。
func installedBundleIDs(env Env) (map[string]bool, error) {
	ids := map[string]bool{}
	for _, dir := range env.AppDirs {
		if _, err := os.Stat(dir); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if err := collectBundleIDs(dir, 0, ids); err != nil {
			return nil, err
		}
	}
	if len(ids) == 0 {
		return nil, errors.New("bundle id を 1 つも取れなかった (Applications の走査が壊れている)")
	}
	return ids, nil
}

// bundleMaxDepth は bundle を探す深さの上限 (/Applications/Vendor/App.app/Contents/PlugIns/X.appex/... を拾える程度)。
const bundleMaxDepth = 8

func collectBundleIDs(dir string, depth int, ids map[string]bool) error {
	return collectBundleIDsSeen(dir, depth, ids, map[[2]uint64]struct{}{})
}

// collectVisits は collectBundleIDs が実際に降りたディレクトリ数 (テスト専用の計測点)。
// 巡回検出が効いているかを**壁時計でなく回数**で判定するため (avoid-wall-clock-assertions)。
// 走査は単一 goroutine から呼ばれる (installedBundleIDs → collectBundleIDs) ので素の int でよい。
var collectVisits int

// dirKey は (device, inode)。同じディレクトリを 2 度走査しないための鍵。
func dirKey(fi os.FileInfo) ([2]uint64, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return [2]uint64{}, false
	}
	return [2]uint64{uint64(st.Dev), st.Ino}, true
}

// collectBundleIDsSeen は seen で巡回を止める。
//
// 🚨 **symlink を辿るなら巡回検出が要る**: 旧コードの `e.IsDir()` は symlink に false を返すので
// 「symlink を辿らない」= 循環しない、を副次的に保証していた。issue 167 (c) でそれを外したとき、
// 循環防止をノーガードで捨てていた。`bundleMaxDepth` は深さしか縛らず**幅は無制限**なので、
// 同じディレクトリを指す symlink を n 個並べると n^8 に膨らむ。実測 (敵対レビュー 2026-09-03、
// ユーザー書き込み可能な ~/Applications で成立): n=2 で 67ms / n=3 で 1.32s / n=4 は 8 秒でも
// 終わらない。ここには ctx が無いのでキャンセルもできない。
func collectBundleIDsSeen(dir string, depth int, ids map[string]bool, seen map[[2]uint64]struct{}) error {
	if depth > bundleMaxDepth {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if depth == 0 {
			return err // ルートが読めないのは fail-closed
		}
		return nil // bundle 内の読めない dir は無視 (判定は続ける)
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		// e.IsDir() で絞らない: DirEntry.IsDir は symlink に false を返すので、symlink の .app が
		// 走査から落ちる (issue 167 (c))。os.Stat で解決先を見る。解決先が AppDirs の外でも
		// bundle id は集める (集めすぎても「実在するアプリ」が増えるだけで、孤児の見落とし = 安全側)
		fi, err := os.Stat(p)
		if err != nil || !fi.IsDir() {
			continue
		}
		if k, ok := dirKey(fi); ok {
			if _, dup := seen[k]; dup {
				continue // 既に走査した実体 (symlink の巡回・別名)。中身は同一なので読み直さない
			}
			seen[k] = struct{}{}
		}
		collectVisits++
		if isBundleName(e.Name()) {
			readBundleID(p, ids)
		}
		_ = collectBundleIDsSeen(p, depth+1, ids, seen)
	}
	return nil
}

// bundleInfoPlistPaths は bundle 内で Info.plist が置かれうる場所 (実在する 3 形)。
//   - Contents/Info.plist          : 通常の macOS bundle
//   - Wrapper/*/Info.plist         : iOS-on-Mac (flat 配置。X.app/Wrapper/Y.app/Info.plist)
//   - Versions/*/Resources/Info.plist : framework (実機に 1Password の Electron Framework 等)
//
// issue 167 (b): Contents/ しか読んでいなかったので、後ろ 2 つの bundle id が集まらず、
// そのアプリのコンテナが孤児と判定されていた。
func readBundleID(bundle string, ids map[string]bool) {
	// ⚠️ **bundle のパスを glob パターンに素で埋めない**。`MyApp [Beta].app` のように名前に
	// メタ文字が入ると `[Beta]` が文字クラスとして解釈され、そのアプリの bundle id が集まらない。
	// 結果は「拾いすぎ」(安全側) ではなく**拾わなすぎ**で、実在するアプリのコンテナが孤児候補に
	// 出る = issue 167 が塞ごうとした症状の再発 (敵対レビュー 2026-09-03 で実測)。
	// 固定パスは Glob を通さず、可変部があるものだけ prefix を escape して Glob する。
	esc := escapeGlobMeta(bundle)
	paths := []string{filepath.Join(bundle, "Contents", "Info.plist")}
	for _, g := range []string{
		filepath.Join(esc, "Wrapper", "*", "Info.plist"),
		filepath.Join(esc, "Versions", "*", "Resources", "Info.plist"),
	} {
		matches, err := filepath.Glob(g)
		if err != nil {
			continue
		}
		paths = append(paths, matches...)
	}
	{
		for _, m := range paths {
			data, err := os.ReadFile(m)
			if err != nil {
				continue
			}
			var info struct {
				ID string `plist:"CFBundleIdentifier"`
			}
			if _, err := plist.Unmarshal(data, &info); err == nil && info.ID != "" {
				ids[info.ID] = true
			}
		}
	}
}

func isBundleName(name string) bool {
	for _, ext := range []string{".app", ".appex", ".xpc", ".plugin", ".bundle", ".framework", ".systemextension", ".driver"} {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

// containerUUIDNameRe は bundle id ではなく UUID を名前に持つコンテナ (iOS-on-Mac アプリのデータ
// コンテナはこの形)。bundle id は `.com.apple.containermanagerd.metadata.plist` にしか無く、その
// plist は TCC で読めない (plutil が operation not permitted)。名前で突合する仕組みでは**構造的に
// 素通りする**ので、孤児判定をせず候補から外す (issue 167 (a): 実在する /Applications/やくそく帳.app の
// データコンテナが RiskConfirm trash の候補に出ていた)。
var containerUUIDNameRe = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)

// containerIsUndiagnosable は「名前から bundle id を決められないコンテナ」か。
func containerIsUndiagnosable(name string) bool { return containerUUIDNameRe.MatchString(name) }

// containerOwnedByInstalled は container の bundle id が実在するアプリ本体か、その配下 (拡張・ウィジェット。
// `<app id>.<ext>` の形) かを見る。
func containerOwnedByInstalled(id string, installed map[string]bool) bool {
	if installed[id] {
		return true
	}
	for app := range installed {
		if strings.HasPrefix(id, app+".") {
			return true
		}
	}
	return false
}

// brewFormulae は Homebrew の台帳 (doctor/brewledger。旧名・別名を含む)。
func brewFormulae(ctx context.Context, run runner.Runner) (map[string]bool, error) {
	return brewledger.Installed(ctx, run)
}

// brewPrefix は `brew --prefix` の実測値を返す。取れない / 空 / 絶対パスでない / 実在しないは
// すべて error にする (fail-closed)。ここを既定値 (/opt/homebrew) に fallback させると、
// Intel Mac や非標準 prefix の環境で「候補なし」という false green に化ける (issue 176)。
func brewPrefix(ctx context.Context, run runner.Runner) (string, error) {
	out, stderr, rc, err := runner.WithTimeout(ctx, run, cmdTimeout, "brew", "--prefix")
	if err != nil {
		return "", err
	}
	if rc != 0 {
		return "", fmt.Errorf("brew --prefix: exit %d: %s", rc, strings.TrimSpace(stderr))
	}
	p := strings.TrimSpace(out)
	if p == "" {
		return "", errors.New("brew --prefix が空を返した")
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("brew --prefix が絶対パスでない: %s", p)
	}
	// "/" を弾く: expand の TrimRight で空に潰れ、$BREW_PREFIX/var/* が /var/* に化ける
	// (深さ 3 なので validateTarget の minDepth も素通りする)。敵対レビュー 2026-09-03
	if filepath.Clean(p) == "/" {
		return "", errors.New("brew --prefix がルート (/) を返した")
	}
	fi, err := os.Stat(p)
	if err != nil {
		return "", fmt.Errorf("brew prefix を確認できない: %w", err)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("brew prefix がディレクトリでない: %s", p)
	}
	return p, nil
}

// brewSharedVarDirs は formula の状態ではない brew prefix の var 直下の共有ディレクトリ。台帳に名前が無いのは
// 当然で、孤児判定の対象外 (db は現役 redis の dump 置き場だった。敵対レビュー 2026-09-02 P1)。
var brewSharedVarDirs = map[string]bool{"homebrew": true, "log": true, "cache": true, "run": true, "db": true, "www": true,
	"lib": true, "spool": true, "mail": true, "tmp": true, "lock": true, "state": true}

var brewWouldRemoveRe = regexp.MustCompile(`^Would remove: (/\S.*?) \(`)

// brewCleanupTargets は `brew cleanup --dry-run` の stdout から `Would remove: <path> (…)` の絶対パスを取る。
// 「Would remove Library/Homebrew/vendor/…」のような相対表記は brew の prefix 配下として解決する。
func brewCleanupTargets(ctx context.Context, run runner.Runner) ([]string, error) {
	out, stderr, rc, err := runner.WithTimeout(ctx, run, cmdTimeout, "brew", "cleanup", "--dry-run")
	if err != nil {
		return nil, err
	}
	if rc != 0 {
		return nil, fmt.Errorf("brew cleanup --dry-run: exit %d: %s", rc, strings.TrimSpace(stderr))
	}
	// prefix は相対表記の行が出たときだけ要る。先に取ると brew --prefix の失敗で
	// 絶対パスの行まで巻き添えで落ちるので、遅延して解決する。
	prefix, prefixErr := "", error(nil)
	resolvePrefix := func() (string, error) {
		if prefix == "" && prefixErr == nil {
			prefix, prefixErr = brewPrefix(ctx, run)
		}
		return prefix, prefixErr
	}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if m := brewWouldRemoveRe.FindStringSubmatch(line); m != nil {
			paths = append(paths, m[1])
			continue
		}
		rest, ok := strings.CutPrefix(line, "Would remove ")
		if !ok || strings.HasPrefix(rest, "/") {
			continue
		}
		// 相対表記は prefix 配下として解決する。prefix が取れないなら**黙って捨てない**
		// (捨てると「brew cleanup が消す対象は無い」という false green になる: issue 176)
		pre, err := resolvePrefix()
		if err != nil {
			return nil, fmt.Errorf("brew cleanup の相対表記 %q を解決できず (brew --prefix): %w", strings.TrimSpace(rest), err)
		}
		paths = append(paths, filepath.Join(pre, strings.TrimSpace(rest)))
	}
	return paths, nil
}

// effectiveVMRoot は <TOOL>_ROOT の実効値。環境変数があればそれ。無ければ .zshrc の lazy loader と
// 同じ規則 (anyenv 側 ~/.anyenv/envs/<tool> があればそちら、無ければ ~/.<tool> へ fallback)。
// 「ディレクトリの存在」で判定しない: ~/.rbenv は同じ形で現役だった (issue 148 実測)。
func effectiveVMRoot(env Env, tool string) string {
	if v := env.Getenv(strings.ToUpper(tool) + "_ROOT"); v != "" {
		return canonicalPath(env, v)
	}
	anyenv := filepath.Join(env.Home, ".anyenv", "envs", tool)
	if fi, err := os.Stat(anyenv); err == nil && fi.IsDir() {
		return canonicalPath(env, anyenv)
	}
	return canonicalPath(env, filepath.Join(env.Home, "."+tool))
}

// canonicalPath は比較用の正規化 (~ 展開 / 絶対化 / symlink 解決)。⚠️ 削除対象の決定には使わない
// (EvalSymlinks はリンク先を指す。ここは「同じ場所か」の比較にだけ使う)。環境変数が相対や ~ 付きでも
// 現役 root を孤児にしない (敵対レビュー 2026-09-02 P3)。
func canonicalPath(env Env, p string) string {
	if strings.HasPrefix(p, "~/") || p == "~" {
		p = env.Home + p[1:]
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(env.Home, p)
	}
	p = filepath.Clean(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}
