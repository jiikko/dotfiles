package lanview

import (
	"glogx/internal/app"
	"mac-lan-manager/discover"
	"mac-lan-manager/lanmgr"
	"mac-lan-manager/live"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// main のテスト (browseModel と組み合わせるもの) と同じヘルパーの、中のテスト用の写し (issue 817 で画面を移したとき、
// 両方のテストが使っていたもの)。

// fakeLanScan は Bonjour の走査の偽物 (dns-sd を起こさない)。closed で「止めたか」を見る。
type fakeLanScan struct {
	mu      sync.Mutex
	peers   []discover.Peer
	busy    bool
	closed  bool
	changed chan struct{}
}

// lanScans はテストの中で起こした走査 (最後が今の走査)。
var (
	lanScansMu sync.Mutex
	lanScans   []*fakeLanScan
	lanPeers   = []discover.Peer{{Host: "m2macmini.local", Instances: []string{"テストの Mac mini"}, SSH: true, SMB: true, Addrs: map[int]string{14: "192.168.1.20"}}}
)

// fakeLanOptions は本物の台帳・設定・ログの置き場所 (TestMain が MAC_LAN_MANAGER_CONFIG_DIR で一時領域へ向けた) に、偽の走査と
// 固定のホスト名をつなぐ。
// 🚨 live.Options() から危ない口を 1 つずつ外す形にしない。本物から写すのは一時領域を指す 4 つだけで、残りは空 (= lanmgr は起こさない)。
// 外す形だと lanmgr に口が増えるたびに外し忘れが本物のコマンドを起こす (ReadAdvert の launchctl が CI の no-real-commands で 4 回落ちた)。
func fakeLanOptions() (lanmgr.Options, error) {
	real, err := live.Options()
	if err != nil {
		return lanmgr.Options{}, err
	}
	o := lanmgr.Options{Ledger: real.Ledger, PrefsPath: real.PrefsPath, SavePrefs: real.SavePrefs, Log: real.Log}
	o.LocalHost = func() string { return "glogx-test.local" }
	o.IfName = func(int) string { return "" }
	o.User = "tester"
	o.StartScan = func(time.Time) lanmgr.Scanner {
		s := &fakeLanScan{peers: lanPeers, busy: true, changed: make(chan struct{}, 1)}
		lanScansMu.Lock()
		lanScans = append(lanScans, s)
		lanScansMu.Unlock()
		return s
	}
	return o, nil
}

// 記憶の置き場を隔離し、時計を固定する。
func withFilerState(t *testing.T) time.Time {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.Local)
	orig := app.TimeNow
	app.TimeNow = func() time.Time { return now }
	t.Cleanup(func() { app.TimeNow = orig })
	return now
}

func (f *fakeLanScan) Snapshot() []discover.Peer {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]discover.Peer(nil), f.peers...)
}
func (f *fakeLanScan) Busy(time.Time) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.busy && !f.closed
}
func (f *fakeLanScan) Changed() <-chan struct{} { return f.changed }
func (f *fakeLanScan) Err() error               { return nil }

// Close は本物 (discover.Scanner.Close) と同じく Changed を閉じる (待っている側は ok=false で抜ける)。
func (f *fakeLanScan) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		close(f.changed)
	}
}
