package live

import (
	"context"
	"net"
	"strconv"
	"time"

	"mac-lan-manager/reach"
)

const (
	// reachLookupTimeout は名前 1 つを引く上限 (wiredLookupTimeout と同じ。広告が無いと dscacheutil は数秒待つ)
	reachLookupTimeout = 3 * time.Second
	// reachDialTimeout は 22 番に繋ぐ上限
	reachDialTimeout = 2 * time.Second
	// ReachTimeout は 1 台を確かめる全体の上限 (引く + ping の -t 3 + 終わらせる余裕)。ping は -t で自分で終わるので、これは止まったときの網
	ReachTimeout = 10 * time.Second
)

// Reach は names を本物の口で確かめる (lanmgr.Options.Reach)。
func Reach(ctx context.Context, names []string) []reach.Name {
	ctx, cancel := context.WithTimeout(ctx, ReachTimeout)
	defer cancel()
	return reach.Check(ctx, names, reach.Deps{
		Lookup: func(ctx context.Context, name string) ([]string, error) {
			ctx, cancel := context.WithTimeout(ctx, reachLookupTimeout)
			defer cancel()
			return lookupHost(ctx, name)
		},
		Ping: func(ctx context.Context, ip string) (string, error) {
			argv := reach.PingArgv(ip)
			return RunOut(ctx, argv[0], argv[1:]...) // 届かない回の rc=2 でも stdout (統計) は返る
		},
		Dial: func(ctx context.Context, ip string, port int) error {
			d := net.Dialer{Timeout: reachDialTimeout}
			c, err := d.DialContext(ctx, "tcp4", net.JoinHostPort(ip, strconv.Itoa(port)))
			if err != nil {
				return err
			}
			return c.Close()
		},
	})
}
