// Package reach は相手の Mac に名前ごとに届くかを確かめる: 名前を引き、引けた IP ごとに ping と ssh の口 (22 番) を見る。
// 名前を引く口・ping を起こす口・TCP で繋ぐ口は呼び出し側が渡す (本物は live、テストは偽物)。
//
// 名前が引けることと届くことは別 (有線の名前は、その IP を持つ I/F が無くなった後も広告が残りうる。issue 847)。
package reach

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"sync"
)

// PingArgv は ip へ打つ ping (3 回・0.2 秒おき・全体で 3 秒まで)。絶対パスで起こす (PATH の先に同名のものを置かれても打たない)。
func PingArgv(ip string) []string {
	return []string{"/sbin/ping", "-n", "-q", "-c", "3", "-i", "0.2", "-t", "3", ip}
}

// SSHPort は口を見る番号。
const SSHPort = 22

// Deps は外とのやりとりの口。
type Deps struct {
	Lookup func(ctx context.Context, name string) ([]string, error) // IPv4 だけを返す
	// Ping は PingArgv(ip) を起こして stdout を返す。err は終了の失敗 (届かないときも rc=2 で err になる。判定は stdout の統計で行う)
	Ping func(ctx context.Context, ip string) (stdout string, err error)
	Dial func(ctx context.Context, ip string, port int) error // 繋がれば nil
}

// Ping は ping 1 回分の結果。Err があれば統計を読めなかった (届かないのとは別。起こせない・統計が無いまま終わった)。
type Ping struct {
	Sent, Recv int
	AvgMs      float64 // Recv > 0 のときだけ
	Err        error
}

// Addr は名前から引けた IP 1 つの結果。
type Addr struct {
	IP   string
	Ping Ping
	SSH  error // nil なら 22 番が開いている
}

// Reachable は届くか: ping が 1 つでも返る、か ssh の口が開く (ステルスモードの Mac は ping に応えない)。
func (a Addr) Reachable() bool { return a.Ping.Recv > 0 || a.SSH == nil }

// Name は名前 1 つの結果。LookupErr か Addrs が空なら引けていない。
type Name struct {
	Name      string
	LookupErr error
	Addrs     []Addr
}

// Reachable は引けた IP のどれかに届くか。
func (n Name) Reachable() bool {
	for _, a := range n.Addrs {
		if a.Reachable() {
			return true
		}
	}
	return false
}

// Elsewhere は ip と違う IP に引けたか (有線の名前が内部 IP と違う所を指す。lanmgr の wiredCheck の wiredMissing と同じ見方)。ip が空なら比べない。
func (n Name) Elsewhere(ip string) bool {
	return ip != "" && slices.ContainsFunc(n.Addrs, func(a Addr) bool { return a.IP != ip })
}

// maxAddrs は名前 1 つについて確かめる IP の数の上限 (引けた IP を全部確かめると、壊れた広告で数が増えたときに ping が並びすぎる)。
const maxAddrs = 4

// Check は names を並行に確かめる。結果は names と同じ順。
func Check(ctx context.Context, names []string, d Deps) []Name {
	out := make([]Name, len(names))
	var wg sync.WaitGroup
	for i, n := range names {
		wg.Go(func() { out[i] = checkName(ctx, n, d) })
	}
	wg.Wait()
	return out
}

func checkName(ctx context.Context, name string, d Deps) Name {
	r := Name{Name: name}
	ips, err := d.Lookup(ctx, name)
	if err != nil {
		r.LookupErr = err
		return r
	}
	for _, s := range ips {
		ip, ok := peerIPv4(s)
		if ok && len(r.Addrs) < maxAddrs && !slices.ContainsFunc(r.Addrs, func(x Addr) bool { return x.IP == ip }) { // 揃えた後で同じ IP は 1 つ
			r.Addrs = append(r.Addrs, Addr{IP: ip})
		}
	}
	var wg sync.WaitGroup
	for i := range r.Addrs {
		a := &r.Addrs[i]
		wg.Go(func() {
			out, err := d.Ping(ctx, a.IP)
			a.Ping = ParsePing(out, err)
		})
		wg.Go(func() { a.SSH = d.Dial(ctx, a.IP, SSHPort) })
	}
	wg.Wait()
	return r
}

// peerIPv4 は引いた結果 (外から来た値) を相手の IPv4 の文字列に揃える。ping の引数にするので、IPv4 の形のものだけを通し
// (先頭が - の値をオプションとして読ませない)、::ffff:a.b.c.d は a.b.c.d に直す (そのまま渡すと ping は引けずに終わり、Dial は通って食い違う)。
// 自分 (127/8・0.0.0.0) とマルチキャスト・ブロードキャストは相手ではないので外す (127 は自分の sshd が開いて「届く」になる)。
func peerIPv4(s string) (string, bool) {
	a, err := netip.ParseAddr(s)
	if err != nil {
		return "", false
	}
	a = a.Unmap()
	if !a.Is4() || a.IsLoopback() || a.IsUnspecified() || a.IsMulticast() || a == netip.AddrFrom4([4]byte{255, 255, 255, 255}) {
		return "", false
	}
	return a.String(), true
}

var (
	reStats = regexp.MustCompile(`(?m)^(\d+) packets transmitted, (\d+) packets received`)
	reRTT   = regexp.MustCompile(`(?m)^round-trip min/avg/max/stddev = [0-9.]+/([0-9.]+)/`)
)

// ParsePing は ping の stdout の統計行を読む。届かない回も ping は rc=2 で統計を出す (実測 2026-10-20 macOS 26.7。testdata/ping_none.txt)
// ので、rc ではなく統計で決める。統計が無ければ「読めない」で、runErr を理由に付ける (名前を引けない rc=68 は stdout が空)。
func ParsePing(stdout string, runErr error) Ping {
	m := reStats.FindStringSubmatch(stdout)
	if m == nil {
		if runErr == nil {
			runErr = errors.New("統計が無い")
		}
		return Ping{Err: fmt.Errorf("ping の結果を読めない: %w", runErr)}
	}
	sent, _ := strconv.Atoi(m[1])
	recv, _ := strconv.Atoi(m[2])
	p := Ping{Sent: sent, Recv: recv}
	if recv > 0 {
		if r := reRTT.FindStringSubmatch(stdout); r != nil {
			p.AvgMs, _ = strconv.ParseFloat(r[1], 64)
		}
	}
	return p
}
