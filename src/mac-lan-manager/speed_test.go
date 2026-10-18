package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"mac-lan-manager/discover"
	"mac-lan-manager/lanmgr"
	"mac-lan-manager/ledger"
	"mac-lan-manager/netinfo"
	"mac-lan-manager/setup"
	"mac-lan-manager/speedtest"
)

// speedLedger は台帳の偽物 (読むだけ)。
type speedLedger struct{ hosts []ledger.Host }

func (s *speedLedger) Load() ([]ledger.Host, error)            { return s.hosts, nil }
func (s *speedLedger) Upsert(ledger.Host) error                { return errors.New("書かない") }
func (s *speedLedger) Remove(string) error                     { return errors.New("書かない") }
func (s *speedLedger) Update(string, func(*ledger.Host)) error { return errors.New("書かない") }

// speedModel は管理対象 3 台の画面: mini (有線で見える。台帳の内部 IP は固定) / air (Wi-Fi だけで見える) / nouser (台帳の user が使えない
// 形ではないが、$USER も空にして ssh のユーザー名が分からない)。
func speedModel(readErr error) func() (*lanmgr.Model, error) {
	return func() (*lanmgr.Model, error) {
		return lanmgr.New(lanmgr.Options{
			ReadNet: func(context.Context) (netinfo.Snapshot, error) {
				if readErr != nil {
					return netinfo.Snapshot{}, readErr
				}
				return netinfo.Snapshot{Services: []netinfo.Service{
					{Name: "Thunderbolt Ethernet", Port: "Thunderbolt Ethernet", Device: "en13", Active: true, IP: "192.168.100.11", Method: "manual", Read: true},
					{Name: "Wi-Fi", Port: "Wi-Fi", Device: "en0", Active: true, IP: "192.168.50.250", Router: "192.168.50.1", Read: true},
				}}, nil
			},
			LocalHost: func() string { return "me.local" },
			StartScan: func(time.Time) lanmgr.Scanner {
				return &dumpScan{peers: []discover.Peer{
					{Host: "m2macmini.local", Instances: []string{"mini"}, SSH: true, Addrs: map[int]string{14: "192.168.50.7", 27: "192.168.100.12"}},
					{Host: "air.local", Instances: []string{"air"}, SSH: true, Addrs: map[int]string{14: "192.168.50.9"}},
				}}
			},
			IfName: func(i int) string { return map[int]string{14: "en0", 27: "en13"}[i] },
			Ledger: &speedLedger{hosts: []ledger.Host{
				{Host: "m2macmini.local", User: "koji", LAN: "192.168.100.12", Alias: "mini"},
				{Host: "air.local", User: "koji"},
				{Host: "gone.local", User: "koji"}, // 見えず内部 IP も無い: 繋ぐ IP が分からない
			}},
			User: "koji",
		}), nil
	}
}

// fakeSpeed は測定の偽物 (宛先を記録し、宛先ごとの値を返す)。
type fakeSpeed struct {
	mu    sync.Mutex
	dests []setup.Dest
	res   map[string]speedtest.Result
}

func (f *fakeSpeed) test(_ context.Context, d setup.Dest, _ func() time.Time) speedtest.Result {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dests = append(f.dests, d)
	return f.res[d.IP]
}

// speedtest は管理対象を 1 台ずつ、画面の t と同じ宛先 (内部 IP を優先 = 有線) で測る。測れない相手は測らずに理由を出し、rc=1。
// 有線で 100 Mbps 付近なら頭打ちの疑いを出す。
func TestSpeedtestMeasuresManagedPeers(t *testing.T) {
	f := &fakeSpeed{res: map[string]speedtest.Result{
		"192.168.100.12": {UpBps: 88e6, DownBps: 80e6},
		"192.168.50.9":   {UpBps: 300e6, DownBps: 250e6},
	}}
	var out, errb bytes.Buffer
	rc := runSpeed([]string{"--json"}, &out, &errb, time.Now, nil, speedModel(nil), f.test)
	if rc != 1 {
		t.Errorf("rc=%d, want 1 (gone.local を測れない) stderr=%s", rc, errb.String())
	}
	var rs []speedReport
	if err := json.Unmarshal(out.Bytes(), &rs); err != nil {
		t.Fatalf("JSON が読めない: %v\n%s", err, out.String())
	}
	got := map[string]speedReport{}
	for _, r := range rs {
		got[r.Host] = r
	}
	if r := got["m2macmini.local"]; r.IP != "192.168.100.12" || r.Link != "有線" || r.UpBps != 88e6 || !r.Capped100M || r.Name != "mini" {
		t.Errorf("mini = %+v (有線の内部 IP で測り、100M の頭打ちを出す)", r)
	}
	if r := got["air.local"]; r.IP != "192.168.50.9" || r.Link != "無線" || r.Capped100M || r.Error != "" {
		t.Errorf("air = %+v", r)
	}
	if r := got["gone.local"]; r.Skipped == "" || r.UpBps != 0 {
		t.Errorf("gone = %+v (繋ぐ IP が分からないのに測った / 理由が無い)", r)
	}
	if len(rs) != 3 || len(f.dests) != 2 {
		t.Errorf("報告 %d 件 / 測った %d 回, want 3 / 2", len(rs), len(f.dests))
	}
	for _, d := range f.dests {
		if d.User != "koji" || d.HostKeyAlias == "" {
			t.Errorf("宛先 = %+v", d)
		}
	}
}

// 引数で相手を選べる (ホスト名・.local を落とした名前・別名)。管理対象に無い名前は rc=2 で何も測らない。
func TestSpeedtestPicksNamedPeers(t *testing.T) {
	f := &fakeSpeed{res: map[string]speedtest.Result{"192.168.100.12": {UpBps: 900e6, DownBps: 900e6}}}
	for _, name := range []string{"m2macmini.local", "m2macmini", "mini"} {
		f.dests = nil
		var out, errb bytes.Buffer
		if rc := runSpeed([]string{name}, &out, &errb, time.Now, nil, speedModel(nil), f.test); rc != 0 || len(f.dests) != 1 {
			t.Errorf("%s: rc=%d 測った %d 回 stderr=%s", name, rc, len(f.dests), errb.String())
		}
		if !strings.Contains(out.String(), "↑ 900 Mbps  ↓ 900 Mbps  [192.168.100.12 有線]") || strings.Contains(out.String(), "頭打ち") {
			t.Errorf("%s: 出力 = %q", name, out.String())
		}
	}
	f.dests = nil
	var out, errb bytes.Buffer
	if rc := runSpeed([]string{"mini", "nope"}, &out, &errb, time.Now, nil, speedModel(nil), f.test); rc != 2 || len(f.dests) != 0 {
		t.Errorf("知らない相手: rc=%d 測った %d 回", rc, len(f.dests))
	}
}

// 測れなかった (ssh の失敗) は rc=1 で理由を出す。この Mac のネットワークを読めなければ測らない (宛先を分類できず、Wi-Fi の IP を
// 有線として測りうる)。
func TestSpeedtestFailures(t *testing.T) {
	f := &fakeSpeed{res: map[string]speedtest.Result{"192.168.100.12": {Err: speedtest.ErrNoKey}}}
	var out, errb bytes.Buffer
	if rc := runSpeed([]string{"mini"}, &out, &errb, time.Now, nil, speedModel(nil), f.test); rc != 1 || !strings.Contains(out.String(), "✗ 鍵で入れない") {
		t.Errorf("測れなかった: rc=%d out=%q", rc, out.String())
	}
	f.dests = nil
	out.Reset()
	errb.Reset()
	if rc := runSpeed(nil, &out, &errb, time.Now, nil, speedModel(errors.New("networksetup が失敗した")), f.test); rc != 1 || len(f.dests) != 0 ||
		!strings.Contains(errb.String(), "ネットワークを読めない") {
		t.Errorf("網が読めない: rc=%d 測った %d 回 stderr=%q", rc, len(f.dests), errb.String())
	}
}

// 測定の途中で止められたら (SIGINT 等)、測定を取り消して 130 で返す (残りの相手は測らない)。
func TestSpeedtestStopCancelsMeasurement(t *testing.T) {
	stop := make(chan struct{})
	var calls int
	test := func(ctx context.Context, _ setup.Dest, _ func() time.Time) speedtest.Result {
		calls++
		close(stop)
		<-ctx.Done() // 止められるまで流し続ける
		return speedtest.Result{Err: ctx.Err()}
	}
	var out, errb bytes.Buffer
	if rc := runSpeed(nil, &out, &errb, time.Now, stop, speedModel(nil), test); rc != 130 || calls != 1 {
		t.Errorf("rc=%d 測った %d 回, want 130 / 1", rc, calls)
	}
}
