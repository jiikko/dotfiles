package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jiikko/dotfiles/src/tuikit/widthenv"

	"mac-lan-manager/discover"
	"mac-lan-manager/lanmgr"
	"mac-lan-manager/ledger"
	"mac-lan-manager/netinfo"
	"mac-lan-manager/setup"
)

func TestMain(m *testing.M) { widthenv.ExitIfUnsupported(); m.Run() }

type dumpScan struct {
	peers  []discover.Peer
	busy   bool
	closed bool
	err    error
}

func (s *dumpScan) Snapshot() []discover.Peer { return s.peers }
func (s *dumpScan) Busy(time.Time) bool       { return s.busy }
func (s *dumpScan) Changed() <-chan struct{}  { return nil }
func (s *dumpScan) Close()                    { s.closed = true }
func (s *dumpScan) Err() error                { return s.err }

func dumpModel(busy bool) func() (*lanmgr.Model, error) {
	return func() (*lanmgr.Model, error) {
		return lanmgr.New(lanmgr.Options{
			ReadNet: func(context.Context) (netinfo.Snapshot, error) {
				return netinfo.Snapshot{Services: []netinfo.Service{
					{Name: "Thunderbolt Ethernet", Device: "en13", Active: true, IP: "169.254.13.42", Read: true},
					{Name: "Wi-Fi", Device: "en0", Active: true, IP: "192.168.50.250", Router: "192.168.50.1", Read: true},
				}}, nil
			},
			LocalHost: func() string { return "me.local" },
			StartScan: func(time.Time) lanmgr.Scanner {
				return &dumpScan{busy: busy, peers: []discover.Peer{{Host: "m2macmini.local", Instances: []string{"kojiのMac mini"}, SSH: true,
					Addrs: map[int]string{14: "192.168.50.7", 27: "169.254.217.100"}}}}
			},
			IfName: func(i int) string { return map[int]string{14: "en0", 27: "en13"}[i] },
			User:   "koji",
		}), nil
	}
}

// dump は画面と同じ判定の一覧を文字で出す (この Mac・見えた Mac・その IP の分類と繋ぐ先・Bonjour の生の結果)。
func TestDumpText(t *testing.T) {
	var out, errb bytes.Buffer
	if rc := runDump(nil, &out, &errb, time.Now, nil, dumpModel(false), nil); rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errb.String())
	}
	for _, want := range []string{"host: me.local", "net_ready: true", "m2macmini.local", "lan=169.254.217.100", "connect=169.254.217.100", "27:169.254.217.100", "# 時間", "ネットワーク読了="} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("出力に %q が無い:\n%s", want, out.String())
		}
	}
}

// --json は同じ中身を JSON で出す。走査が終わらなければ --wait の上限で打ち切り、busy=true で出す (待ち続けない)。
func TestDumpJSONAndWaitCap(t *testing.T) {
	var out, errb bytes.Buffer
	start := time.Now()
	if rc := runDump([]string{"--json", "--wait", "200ms"}, &out, &errb, time.Now, nil, dumpModel(true), nil); rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errb.String())
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("--wait の上限で打ち切らない (%v)", time.Since(start))
	}
	var r lanmgr.Report
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatalf("JSON が読めない: %v\n%s", err, out.String())
	}
	if !r.Busy || len(r.Rows) != 2 || r.Rows[1].Host != "m2macmini.local" || r.Rows[1].ConnectIP != "169.254.217.100" {
		t.Errorf("report = %+v", r)
	}
}

// 待っている間にシグナル (stop) が来たら、待ちを打ち切って走査を止めてから戻る (止めないと dns-sd が親の外で残った)。
func TestDumpStopsScanOnSignal(t *testing.T) {
	scan := &dumpScan{busy: true} // 走査が終わらない
	stop := make(chan struct{})
	close(stop)
	var out, errb bytes.Buffer
	start := time.Now()
	rc := runDump([]string{"--wait", "30s"}, &out, &errb, time.Now, stop, func() (*lanmgr.Model, error) {
		return lanmgr.New(lanmgr.Options{StartScan: func(time.Time) lanmgr.Scanner { return scan }}), nil
	}, nil)
	if rc != 130 || time.Since(start) > 5*time.Second {
		t.Errorf("シグナルで打ち切らない: rc=%d (%v)", rc, time.Since(start))
	}
	if !scan.closed {
		t.Error("シグナルで戻るときに走査を止めていない (dns-sd が残る)")
	}
}

// 開いたときに積んだ知らせ (台帳が読めない) も、待ちの後の出力に残る (Report は知らせを取り出さない)。
func TestDumpKeepsEarlyNotices(t *testing.T) {
	var out, errb bytes.Buffer
	// 走査が終わらない (busy) ので、待ちの中で Report が何度も呼ばれる (知らせを取り出していたころは、ここで消えた)
	rc := runDump([]string{"--wait", "200ms"}, &out, &errb, time.Now, nil, func() (*lanmgr.Model, error) {
		return lanmgr.New(lanmgr.Options{Ledger: brokenStore{}, StartScan: func(time.Time) lanmgr.Scanner { return &dumpScan{busy: true} }}), nil
	}, nil)
	if rc != 0 || !strings.Contains(out.String(), "台帳が読めない") {
		t.Errorf("rc=%d 開いたときの知らせが出力に無い:\n%s", rc, out.String())
	}
}

type brokenStore struct{}

func (brokenStore) Load() ([]ledger.Host, error)                { return nil, errors.New("壊れた台帳") }
func (brokenStore) Merge(ledger.Host, func(*ledger.Host)) error { return nil }
func (brokenStore) Remove(string) error                         { return nil }
func (brokenStore) Update(string, func(*ledger.Host)) error     { return nil }

// noModel は --peer のときに呼ばれてはいけない newModel (相手を読むときは、この Mac の網も Bonjour も起こさない)。
func noModel(t *testing.T) func() (*lanmgr.Model, error) {
	return func() (*lanmgr.Model, error) {
		t.Error("--peer なのに newModel を呼んだ")
		return lanmgr.New(lanmgr.Options{}), nil
	}
}

// --peer はその相手を observe で読み、鍵の判定と有線のサービスを出す。--user の既定は $USER、--host の既定は IP。
func TestDumpPeerText(t *testing.T) {
	t.Setenv("USER", "alice")
	var got setup.Dest
	observe := func(_ context.Context, d setup.Dest) setup.PeerNet {
		got = d
		return setup.PeerNet{KeyOK: true, NetKnown: true, Service: "Thunderbolt Ethernet", IP: "169.254.13.42", Method: "DHCP", Mask: "255.255.0.0"}
	}
	var out, errb bytes.Buffer
	if rc := runDump([]string{"--peer", "192.168.50.7"}, &out, &errb, time.Now, nil, noModel(t), observe); rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errb.String())
	}
	if got != (setup.Dest{User: "alice", IP: "192.168.50.7", HostKeyAlias: "192.168.50.7"}) {
		t.Errorf("Dest = %+v", got)
	}
	for _, want := range []string{"peer: alice@192.168.50.7", "key: ok", "net_known: true", "service=Thunderbolt Ethernet", "ip=169.254.13.42", "method=DHCP", "mask=255.255.0.0", "router=-"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("出力に %q が無い:\n%s", want, out.String())
		}
	}
}

// 相手を読む途中で止められたら (SIGINT / SIGTERM) 130 で返す。読めなかった結果 (key: unknown) として 0 を返すと、スクリプトから
// 中断を区別できない (走査の dump は同じ止め方で 130)。
func TestDumpPeerStoppedExits130(t *testing.T) {
	stop := make(chan struct{})
	close(stop)
	observe := func(ctx context.Context, _ setup.Dest) setup.PeerNet {
		<-ctx.Done() // 止められるまで ssh が返らない
		return setup.PeerNet{KeyErr: fmt.Errorf("%w: %w", setup.ErrUnknown, ctx.Err())}
	}
	var out, errb bytes.Buffer
	if rc := runDump([]string{"--peer", "192.168.50.7"}, &out, &errb, time.Now, stop, noModel(t), observe); rc != 130 {
		t.Errorf("止めた dump --peer の rc = %d, want 130 (stdout=%q)", rc, out.String())
	}
}

// 鍵の 3 通り (入れる / 入れない / 判定できない) を JSON の key で分ける。--user と --host は Dest へそのまま渡る。
func TestDumpPeerJSONKeyOutcomes(t *testing.T) {
	unknown := fmt.Errorf("%w: ssh: connect to host 192.168.50.7 port 22: Operation timed out", setup.ErrUnknown)
	cases := []struct {
		name string
		p    setup.PeerNet
		key  string
	}{
		{"ok", setup.PeerNet{KeyOK: true}, "ok"},
		{"denied", setup.PeerNet{}, "denied"},
		{"host_key_unknown", setup.PeerNet{HostKeyUnknown: true}, "host_key_unknown"},
		{"unknown", setup.PeerNet{KeyErr: unknown}, "unknown"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got setup.Dest
			observe := func(_ context.Context, d setup.Dest) setup.PeerNet { got = d; return c.p }
			var out, errb bytes.Buffer
			args := []string{"--peer", "192.168.50.7", "--user", "koji", "--host", "m2macmini.local", "--json"}
			if rc := runDump(args, &out, &errb, time.Now, nil, noModel(t), observe); rc != 0 {
				t.Fatalf("rc=%d stderr=%s", rc, errb.String())
			}
			if got != (setup.Dest{User: "koji", IP: "192.168.50.7", HostKeyAlias: "m2macmini.local"}) {
				t.Errorf("Dest = %+v", got)
			}
			var r peerReport
			if err := json.Unmarshal(out.Bytes(), &r); err != nil {
				t.Fatalf("JSON が読めない: %v\n%s", err, out.String())
			}
			if r.Key != c.key || r.KeyOK != c.p.KeyOK || (c.p.KeyErr != nil) != strings.Contains(r.KeyErr, "timed out") {
				t.Errorf("report = %+v", r)
			}
		})
	}
}

// 相手の IP・ユーザーが ssh に渡せない形なら、ssh を起こさずに使い方の誤り (rc=2) で返す。
func TestDumpPeerRejectsBadDest(t *testing.T) {
	observe := func(context.Context, setup.Dest) setup.PeerNet {
		t.Error("渡せない宛先で observe を呼んだ")
		return setup.PeerNet{}
	}
	for _, args := range [][]string{{"--peer", "-oProxyCommand=x"}, {"--peer", "192.168.50.7", "--user", "-x"}} {
		var out, errb bytes.Buffer
		if rc := runDump(args, &out, &errb, time.Now, nil, noModel(t), observe); rc != 2 {
			t.Errorf("%q: rc=%d", args, rc)
		}
	}
}

// 止める合図を受けた goroutine が cancel するより先に run が戻っても、止められたと答える (ping / speedtest の 130)。
// GOMAXPROCS(1) で、合図を受ける側が走る前に run が戻る並びを毎回作る (ctx.Err() で見る形はここで必ず false になる)。
func TestUntilStopSeesStopBeforeCancel(t *testing.T) {
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	for i := range 100 {
		stop := make(chan struct{})
		if !untilStop(stop, func(context.Context) { close(stop) }) {
			t.Fatalf("%d 回目: 合図の後に戻った run を止められていないと読んだ", i)
		}
	}
	if untilStop(make(chan struct{}), func(context.Context) {}) {
		t.Error("合図が無いのに止められたと読んだ")
	}
	var canceled bool
	stop := make(chan struct{})
	untilStop(stop, func(ctx context.Context) { close(stop); <-ctx.Done(); canceled = true })
	if !canceled {
		t.Error("合図で ctx を取り消していない")
	}
}
