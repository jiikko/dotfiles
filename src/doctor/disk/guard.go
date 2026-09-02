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
	"strings"
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
	previews := filepath.Join(env.Home, "Library", "Developer", "Xcode", "UserData", "Previews", "Simulator Devices")
	if fi, err := os.Stat(previews); err == nil && fi.IsDir() {
		if err := simDeviceUDIDsInto(ctx, run, []string{"--set", previews}, set); err != nil {
			return nil, fmt.Errorf("previews のデバイスセット (Xcode Previews): %w", err)
		}
	}
	return set, nil
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
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if isBundleName(e.Name()) {
			if data, err := os.ReadFile(filepath.Join(p, "Contents", "Info.plist")); err == nil {
				var info struct {
					ID string `plist:"CFBundleIdentifier"`
				}
				if _, err := plist.Unmarshal(data, &info); err == nil && info.ID != "" {
					ids[info.ID] = true
				}
			}
		}
		_ = collectBundleIDs(p, depth+1, ids)
	}
	return nil
}

func isBundleName(name string) bool {
	for _, ext := range []string{".app", ".appex", ".xpc", ".plugin", ".bundle", ".framework", ".systemextension", ".driver"} {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

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

// brewSharedVarDirs は formula の状態ではない /opt/homebrew/var 直下の共有ディレクトリ。台帳に名前が無いのは
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
	prefixOut, _, prc, perr := runner.WithTimeout(ctx, run, cmdTimeout, "brew", "--prefix")
	prefix := strings.TrimSpace(prefixOut)
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if m := brewWouldRemoveRe.FindStringSubmatch(line); m != nil {
			paths = append(paths, m[1])
			continue
		}
		if rest, ok := strings.CutPrefix(line, "Would remove "); ok && !strings.HasPrefix(rest, "/") && perr == nil && prc == 0 && prefix != "" {
			paths = append(paths, filepath.Join(prefix, strings.TrimSpace(rest)))
		}
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
