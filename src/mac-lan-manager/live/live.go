// Package live は lanmgr の画面を本物 (この Mac のネットワーク・Bonjour・台帳・設定・クリップボード) につなぐ。
// 単体の bin/mac-lan-manager と glogx の L の板の両方が使う (つなぎを 2 か所に書かない)。テストは lanmgr.Options を自分で組む。
package live

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"mac-lan-manager/discover"
	"mac-lan-manager/lanmgr"
	"mac-lan-manager/ledger"
	"mac-lan-manager/netinfo"
	"mac-lan-manager/prefs"
	"mac-lan-manager/setup"
	"subproc"
	"subproc/clipboard"
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
	var log func(string)
	if p, err := DefaultLogPath(); err == nil {
		log = FileLog(p)
	}
	return lanmgr.Options{
		Log:     log, // 操作のログ (~/Library/Logs/mac-lan-manager.log)
		ReadNet: func(ctx context.Context) (netinfo.Snapshot, error) { return netinfo.Read(ctx, RunOut) },
		ReadAdvert: func(ctx context.Context) (setup.NameState, error) {
			return setup.ReadNameState(ctx, func(ctx context.Context, argv []string) (string, error) { return RunOut(ctx, argv[0], argv[1:]...) })
		},
		LocalHost:  localHost,
		StartScan:  func(now time.Time) lanmgr.Scanner { return discover.Start(discover.ExecStarter, now) },
		IfName:     ifName,
		Ledger:     ledger.FileStore{Path: ledgerPath},
		PrefsPath:  prefsPath,
		SavePrefs:  prefs.Save,
		Copy:       clipboard.Copy, // pbcopy (tmux の中では tmux バッファへもおまけ)。上限は clipboard.Timeout
		User:       os.Getenv("USER"),
		Observe:    Observe,
		SpeedTest:  SpeedTest,
		Reach:      Reach,
		LookupHost: lookupHost,
		RunQuiet: func(ctx context.Context, argv []string) error {
			_, err := RunOut(ctx, argv[0], argv[1:]...)
			var ee *exec.ExitError
			if errors.As(err, &ee) && len(ee.Stderr) > 0 { // 理由 (相手の sh が stderr に出した 1 行) を画面に届ける。終了コードは %w で残る
				return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(ee.Stderr)))
			}
			return err
		},
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

// lookupHost は名前を引く (Bonjour の .local も引ける dscacheutil を使う。Go の素の DNS の解決は mDNS を引かないことがある)。
// IPv4 だけを返す。引けなければ空 (dscacheutil は知らない名前でも 0 で抜ける)。
func lookupHost(ctx context.Context, name string) ([]string, error) {
	out, err := RunOut(ctx, "dscacheutil", "-q", "host", "-a", "name", name)
	if err != nil {
		return nil, err
	}
	var ips []string
	for _, l := range strings.Split(out, "\n") {
		if ip, ok := strings.CutPrefix(strings.TrimSpace(l), "ip_address: "); ok && net.ParseIP(ip).To4() != nil && !slices.Contains(ips, ip) {
			ips = append(ips, ip)
		}
	}
	return ips, nil
}
