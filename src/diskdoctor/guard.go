package main

import (
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
func processRunning(ctx context.Context, run Runner, name string) (bool, error) {
	_, stderr, rc, err := runWithTimeout(ctx, run, "pgrep", "-x", name)
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

// simDeviceUDIDs は `xcrun simctl list devices -j` の現存デバイス UDID。失敗は error (孤児判定を
// 全件「孤児でない」に倒す)。
func simDeviceUDIDs(ctx context.Context, run Runner) (map[string]bool, error) {
	out, stderr, rc, err := runWithTimeout(ctx, run, "xcrun", "simctl", "list", "devices", "-j")
	if err != nil {
		return nil, err
	}
	if rc != 0 {
		return nil, fmt.Errorf("simctl list devices: exit %d: %s", rc, strings.TrimSpace(stderr))
	}
	var doc struct {
		Devices map[string][]struct {
			UDID string `json:"udid"`
		} `json:"devices"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		return nil, fmt.Errorf("simctl の JSON を解釈できない: %w", err)
	}
	set := map[string]bool{}
	for _, list := range doc.Devices {
		for _, d := range list {
			set[strings.ToUpper(d.UDID)] = true
		}
	}
	return set, nil
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

func simRuntimes(ctx context.Context, run Runner) ([]simRuntime, error) {
	out, stderr, rc, err := runWithTimeout(ctx, run, "xcrun", "simctl", "runtime", "list", "-j")
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

// installedBundleIDs は /Applications と ~/Applications の .app を実走査して CFBundleIdentifier を集める。
// mdfind (Spotlight) は使わない: インデックス欠落で実在するアプリを「無い」と答えた実例がある。
// 走査できないディレクトリがあれば error (fail-closed: 孤児判定をしない)。存在しない ~/Applications は無視。
func installedBundleIDs(env Env) (map[string]bool, error) {
	ids := map[string]bool{}
	for _, dir := range env.AppDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".app") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, e.Name(), "Contents", "Info.plist"))
			if err != nil {
				continue // Info.plist の無い .app (壊れた bundle) は無視。読めないだけで孤児判定は続ける
			}
			var info struct {
				ID string `plist:"CFBundleIdentifier"`
			}
			if _, err := plist.Unmarshal(data, &info); err == nil && info.ID != "" {
				ids[info.ID] = true
			}
		}
	}
	if len(ids) == 0 {
		return nil, errors.New("/Applications から bundle id を 1 つも取れなかった (走査が壊れている)")
	}
	return ids, nil
}

// brewFormulae は brew list --formula の台帳。
func brewFormulae(ctx context.Context, run Runner) (map[string]bool, error) {
	out, stderr, rc, err := runWithTimeout(ctx, run, "brew", "list", "--formula")
	if err != nil {
		return nil, err
	}
	if rc != 0 {
		return nil, fmt.Errorf("brew list: exit %d: %s", rc, strings.TrimSpace(stderr))
	}
	set := map[string]bool{}
	for _, f := range strings.Fields(out) {
		set[f] = true
		// mysql@8.4 → var/mysql のように @version を落とした名前で状態ディレクトリが作られる
		if i := strings.IndexByte(f, '@'); i > 0 {
			set[f[:i]] = true
		}
	}
	return set, nil
}

var brewWouldRemoveRe = regexp.MustCompile(`^Would remove: (/\S.*?) \(`)

// brewCleanupTargets は `brew cleanup --dry-run` の stdout から `Would remove: <path> (…)` の絶対パスを取る。
// 「Would remove Library/Homebrew/vendor/…」のような相対表記は brew の prefix 配下として解決する。
func brewCleanupTargets(ctx context.Context, run Runner) ([]string, error) {
	out, stderr, rc, err := runWithTimeout(ctx, run, "brew", "cleanup", "--dry-run")
	if err != nil {
		return nil, err
	}
	if rc != 0 {
		return nil, fmt.Errorf("brew cleanup --dry-run: exit %d: %s", rc, strings.TrimSpace(stderr))
	}
	prefixOut, _, prc, perr := runWithTimeout(ctx, run, "brew", "--prefix")
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
		return filepath.Clean(v)
	}
	anyenv := filepath.Join(env.Home, ".anyenv", "envs", tool)
	if fi, err := os.Stat(anyenv); err == nil && fi.IsDir() {
		return anyenv
	}
	return filepath.Join(env.Home, "."+tool)
}
