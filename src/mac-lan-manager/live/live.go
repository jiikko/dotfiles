// Package live は lanmgr の画面を本物 (この Mac のネットワーク・Bonjour・台帳・設定・クリップボード) につなぐ。
// 単体の bin/mac-lan-manager と glogx の L の板の両方が使う (つなぎを 2 か所に書かない)。テストは lanmgr.Options を自分で組む。
package live

import (
	"context"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"mac-lan-manager/discover"
	"mac-lan-manager/lanmgr"
	"mac-lan-manager/ledger"
	"mac-lan-manager/netinfo"
	"mac-lan-manager/prefs"
	"subproc"
)

// Options は本物につないだ lanmgr.Options (台帳と設定の置き場所は MAC_LAN_MANAGER_CONFIG_DIR で差し替えられる)。
func Options() (lanmgr.Options, error) {
	ledgerPath, err := ledger.DefaultPath()
	if err != nil {
		return lanmgr.Options{}, err
	}
	prefsPath, err := prefs.DefaultPath()
	if err != nil {
		return lanmgr.Options{}, err
	}
	return lanmgr.Options{
		ReadNet:   func(ctx context.Context) (netinfo.Snapshot, error) { return netinfo.Read(ctx, RunOut) },
		LocalHost: localHost,
		StartScan: func(now time.Time) lanmgr.Scanner { return discover.Start(discover.ExecStarter, now) },
		IfName:    ifName,
		Ledger:    ledger.FileStore{Path: ledgerPath},
		PrefsPath: prefsPath,
		SavePrefs: prefs.Save,
		Copy:      pbcopy,
		User:      os.Getenv("USER"),
	}, nil
}

// RunOut はコマンドを起こして標準出力を返す (subproc の WaitDelay 付き)。
func RunOut(ctx context.Context, name string, args ...string) (string, error) {
	out, err := subproc.CommandContext(ctx, name, args...).Output()
	return string(out), err
}

func localHost() string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := RunOut(ctx, "scutil", "--get", "LocalHostName")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out) + ".local"
}

func ifName(index int) string {
	if i, err := net.InterfaceByIndex(index); err == nil {
		return i.Name
	}
	return ""
}

func pbcopy(s string) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(s)
	return cmd.Run()
}
