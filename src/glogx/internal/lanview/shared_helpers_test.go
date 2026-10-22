package lanview

import (
	"mac-lan-manager/discover"
	"mac-lan-manager/lanmgr"
	"mac-lan-manager/lanmgr/lanmgrtest"
	"mac-lan-manager/live"
	"sync"
	"time"
)

// main のテスト (browseModel と組み合わせるもの) と同じヘルパーの、中のテスト用の写し (両方のテストが使っていたもの)。

// lanScans はテストの中で起こした走査 (最後が今の走査)。
var (
	lanScansMu sync.Mutex
	lanScans   []*lanmgrtest.Scan
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
		s := lanmgrtest.NewScan(lanPeers...)
		s.SetWorking(true)
		lanScansMu.Lock()
		lanScans = append(lanScans, s)
		lanScansMu.Unlock()
		return s
	}
	return o, nil
}
