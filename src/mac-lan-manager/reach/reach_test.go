package reach

import (
	"context"
	"errors"
	"mac-lan-manager/internal/testfile"
	"slices"
	"sync"
	"testing"
)

func readTestdata(t *testing.T, name string) string {
	t.Helper()
	return testfile.Read(t, name)
}

// dupOut は宛先の近くの別の機器も応えたときの統計 (作った値。ping のソースの形に合わせた。本物の出力は取れていない)。
const dupOut = "--- 192.168.100.255 ping statistics ---\n3 packets transmitted, 3 packets received, +2 duplicates, 0.0% packet loss\nround-trip min/avg/max/stddev = 0.300/0.400/0.500/0.100 ms\n"

// testdata は 2026-10-20 にこの Mac (macOS 26.7) で PingArgv と同じ引数の本物の ping を起こして取った stdout。
func TestParsePing(t *testing.T) {
	exit2 := errors.New("exit status 2")
	cases := []struct {
		name       string
		file       string
		err        error
		sent, recv int
		avg        float64
		unreadable bool
	}{
		{"届く (rc=0)", "ping_ok.txt", nil, 3, 3, 0.050, false},
		{"届かない (rc=2 でも統計を読む)", "ping_none.txt", exit2, 3, 0, 0, false},
		{"名前を引けない (rc=68・stdout が空)", "ping_unknown_host.txt", errors.New("exit status 68"), 0, 0, 0, true},
		{"重複の応答 (作った値: 本物は取れていない)", "", nil, 3, 3, 0.4, false},
		{"統計が無いまま rc=0", "ping_unknown_host.txt", nil, 0, 0, 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := dupOut
			if c.file != "" {
				out = readTestdata(t, c.file)
			}
			p := ParsePing(out, c.err)
			if (p.Err != nil) != c.unreadable {
				t.Fatalf("Err = %v, 読めない = %v を期待", p.Err, c.unreadable)
			}
			if p.Sent != c.sent || p.Recv != c.recv || p.AvgMs != c.avg {
				t.Fatalf("got sent=%d recv=%d avg=%v, want %d %d %v", p.Sent, p.Recv, p.AvgMs, c.sent, c.recv, c.avg)
			}
		})
	}
}

type fake struct {
	mu      sync.Mutex
	lookup  map[string][]string
	pingOut map[string]string // 無ければ届かない (統計 0/3)
	open    map[string]bool
	pinged  []string
}

func (f *fake) deps() Deps {
	return Deps{
		Lookup: func(_ context.Context, name string) ([]string, error) {
			ips, ok := f.lookup[name]
			if !ok {
				return nil, errors.New("not found")
			}
			return ips, nil
		},
		Ping: func(_ context.Context, ip string) (string, error) {
			f.mu.Lock()
			f.pinged = append(f.pinged, ip)
			f.mu.Unlock()
			if out, ok := f.pingOut[ip]; ok {
				return out, nil
			}
			return "3 packets transmitted, 0 packets received, 100.0% packet loss\n", errors.New("exit status 2")
		},
		Dial: func(_ context.Context, ip string, port int) error {
			if port != SSHPort {
				return errors.New("port")
			}
			if f.open[ip] {
				return nil
			}
			return errors.New("connection refused")
		},
	}
}

func TestCheck(t *testing.T) {
	ok := readTestdata(t, "ping_ok.txt")
	f := &fake{
		lookup: map[string][]string{
			"m2macmini.local":       {"192.168.50.7", "169.254.48.172"},
			"m2macmini.wired.local": {"192.168.100.12"}, // 広告だけ残って誰も持っていない IP (issue 847 の起点)
			"stealth.local":         {"10.0.0.5"},
			"empty.local":           {},
		},
		pingOut: map[string]string{"192.168.50.7": ok},
		open:    map[string]bool{"192.168.50.7": true, "10.0.0.5": true},
	}
	got := Check(context.Background(), []string{"m2macmini.local", "m2macmini.wired.local", "stealth.local", "empty.local", "gone.local"}, f.deps())
	want := []bool{true, false, true, false, false}
	for i, n := range got {
		if n.Reachable() != want[i] {
			t.Errorf("%s: Reachable = %v, want %v (%+v)", n.Name, n.Reachable(), want[i], n)
		}
	}
	if a := got[0].Addrs; len(a) != 2 || a[0].Ping.Recv != 3 || a[0].SSH != nil || a[1].Reachable() {
		t.Errorf("m2macmini.local の IP ごとの結果: %+v", a)
	}
	if got[2].Addrs[0].Ping.Recv != 0 || got[2].Addrs[0].SSH != nil {
		t.Errorf("ping に応えず 22 番は開く相手: %+v", got[2].Addrs[0])
	}
	if got[4].LookupErr == nil {
		t.Errorf("引けない名前に LookupErr が無い")
	}
}

// 引いた結果は外から来た値なので、IPv4 の形でないものは ping に渡さない。数も上限で切る。
func TestCheckFiltersAddrs(t *testing.T) {
	f := &fake{lookup: map[string][]string{"x.local": {"-c1", "fe80::1", "127.0.0.1", "0.0.0.0", "224.0.0.251", "255.255.255.255", "::ffff:1.1.1.1", "1.1.1.1", "1.1.1.2", "1.1.1.3", "1.1.1.4", "1.1.1.5"}}}
	got := Check(context.Background(), []string{"x.local"}, f.deps())
	slices.Sort(f.pinged)
	if want := []string{"1.1.1.1", "1.1.1.2", "1.1.1.3", "1.1.1.4"}; !slices.Equal(f.pinged, want) {
		t.Fatalf("ping した IP = %v, want %v", f.pinged, want)
	}
	if len(got[0].Addrs) != maxAddrs {
		t.Fatalf("Addrs = %d 件", len(got[0].Addrs))
	}
}
