package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"runtime"
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

// speedLedger は書き込みを断る台帳の偽物 (lanmgrtest.Store は書けるので使わない。speedtest / ping が台帳を書かないことを、書けば失敗する形で見る)。
type speedLedger struct{ hosts []ledger.Host }

func (s *speedLedger) Load() ([]ledger.Host, error)                { return s.hosts, nil }
func (s *speedLedger) Merge(ledger.Host, func(*ledger.Host)) error { return errors.New("書かない") }
func (s *speedLedger) Remove(string) error                         { return errors.New("書かない") }
func (s *speedLedger) Update(string, func(*ledger.Host)) error     { return errors.New("書かない") }

// speedModel は管理対象 3 台の画面: mini (有線で見える。台帳の内部 IP は固定) / air (Wi-Fi だけで見える) / nouser (台帳の user が使えない
// 形ではないが、$USER も空にして ssh のユーザー名が分からない)。
func speedModel(readErr error) func() (*lanmgr.Model, error) {
	return speedModelWith(readErr, false, nil)
}

// speedModelWith は speedModel に、走査が終わらない (busy) と台帳に足す行を渡せる形。
func speedModelWith(readErr error, busy bool, more []ledger.Host) func() (*lanmgr.Model, error) {
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
				return newScan(busy, nil,
					discover.Peer{Host: "m2macmini.local", Instances: []string{"mini"}, SSH: true, Addrs: map[int]string{14: "192.168.50.7", 27: "192.168.100.12"}},
					discover.Peer{Host: "air.local", Instances: []string{"air"}, SSH: true, Addrs: map[int]string{14: "192.168.50.9"}},
				)
			},
			IfName: func(i int) string { return map[int]string{14: "en0", 27: "en13"}[i] },
			Ledger: &speedLedger{hosts: append([]ledger.Host{
				{Host: "m2macmini.local", User: "koji", LAN: "192.168.100.12", Alias: "mini"},
				{Host: "air.local", User: "koji"},
				{Host: "gone.local", User: "koji"}, // 見えず内部 IP も無い: 繋ぐ IP が分からない
			}, more...)},
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

// 止める合図が来たら、測定の結果 (端末の Ctrl-C は子の ssh にも届き、ssh は失敗で終わる) より先に 130 を返す。
func TestSpeedtestStopWins(t *testing.T) {
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1)) // 合図を受ける側が走る前に test が戻る並びを毎回作る
	stop := make(chan struct{})
	test := func(context.Context, setup.Dest, func() time.Time) speedtest.Result {
		close(stop) // 合図を出してすぐ返す (ssh が SIGINT で死んだ形。ctx が切れるのを待たない)
		return speedtest.Result{Err: errors.New("上りを測れなかった: signal: interrupt")}
	}
	var out, errb bytes.Buffer
	if rc := runSpeed(nil, &out, &errb, time.Now, stop, speedModel(nil), test); rc != 130 || out.Len() != 0 {
		t.Fatalf("rc=%d stdout=%q, want 130 / 何も出さない", rc, out.String())
	}
}

// 名前が 2 台に当たる (別名と .local を落としたホスト名が別の Mac で重なる) なら、黙って片方を測らず rc=2 で断る。ホスト名にちょうど
// 一致すればそれを測る。同じ相手を 2 回渡しても 1 回だけ測る。
func TestSpeedtestAmbiguousName(t *testing.T) {
	more := []ledger.Host{{Host: "air2.local", User: "koji", LAN: "192.168.100.15", Alias: "air"}} // 別名 "air" が air.local の短い名前と重なる
	f := &fakeSpeed{res: map[string]speedtest.Result{"192.168.50.9": {UpBps: 1e8, DownBps: 1e8}, "192.168.100.15": {UpBps: 1e8, DownBps: 1e8}}}
	var out, errb bytes.Buffer
	if rc := runSpeed([]string{"air"}, &out, &errb, time.Now, nil, speedModelWith(nil, false, more), f.test); rc != 2 || len(f.dests) != 0 ||
		!strings.Contains(errb.String(), "air.local") || !strings.Contains(errb.String(), "air2.local") {
		t.Errorf("曖昧な名前: rc=%d 測った %d 回 stderr=%q", rc, len(f.dests), errb.String())
	}
	f.dests = nil
	if rc := runSpeed([]string{"air.local", "air.local"}, &out, &errb, time.Now, nil, speedModelWith(nil, false, more), f.test); rc != 0 ||
		len(f.dests) != 1 || f.dests[0].IP != "192.168.50.9" {
		t.Errorf("ホスト名で指定: rc=%d 宛先 %+v", rc, f.dests)
	}
}

// 走査が --wait までに終わらなければ、途中の一覧で測ったことを出す (JSON の scan_settled=false・文字なら stderr の注記)。
func TestSpeedtestReportsUnsettledScan(t *testing.T) {
	f := &fakeSpeed{res: map[string]speedtest.Result{"192.168.100.12": {UpBps: 9e8, DownBps: 9e8}}}
	var out, errb bytes.Buffer
	if rc := runSpeed([]string{"--json", "--wait", "100ms", "mini"}, &out, &errb, time.Now, nil, speedModelWith(nil, true, nil), f.test); rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errb.String())
	}
	var rs []speedReport
	if err := json.Unmarshal(out.Bytes(), &rs); err != nil || len(rs) != 1 || rs[0].ScanSettled {
		t.Errorf("走査が終わっていないのに scan_settled = %+v %v", rs, err)
	}
	out.Reset()
	if runSpeed([]string{"--wait", "100ms", "mini"}, &out, &errb, time.Now, nil, speedModelWith(nil, true, nil), f.test); !strings.Contains(errb.String(), "終わらなかった") {
		t.Errorf("文字の出力で走査が終わっていないことを知らせない: %q", errb.String())
	}
	out.Reset()
	if runSpeed([]string{"--json", "mini"}, &out, &errb, time.Now, nil, speedModel(nil), f.test); !strings.Contains(out.String(), `"scan_settled": true`) {
		t.Errorf("走査が終わったのに scan_settled が true でない: %s", out.String())
	}
}

// 走査を待っている間に止められたら、何も測らずに 130 で返す。
func TestSpeedtestStopWhileSettling(t *testing.T) {
	stop := make(chan struct{})
	close(stop)
	f := &fakeSpeed{}
	var out, errb bytes.Buffer
	if rc := runSpeed([]string{"--wait", "10s"}, &out, &errb, time.Now, stop, speedModelWith(nil, true, nil), f.test); rc != 130 || len(f.dests) != 0 || out.Len() != 0 {
		t.Errorf("rc=%d 測った %d 回 out=%q", rc, len(f.dests), out.String())
	}
}

// 別名が別の Mac のホスト名そのものなら、ホスト名にちょうど一致する方を測る。大文字・末尾の . は台帳の鍵の形に揃えて比べる。
func TestSpeedtestExactHostWinsAndKeyIsNormalized(t *testing.T) {
	more := []ledger.Host{{Host: "air2.local", User: "koji", LAN: "192.168.100.15", Alias: "air.local"}} // 別名が air.local のホスト名と同じ
	f := &fakeSpeed{res: map[string]speedtest.Result{"192.168.50.9": {UpBps: 1e8, DownBps: 1e8}, "192.168.100.12": {UpBps: 1e8, DownBps: 1e8}}}
	var out, errb bytes.Buffer
	if rc := runSpeed([]string{"air.local"}, &out, &errb, time.Now, nil, speedModelWith(nil, false, more), f.test); rc != 0 || len(f.dests) != 1 ||
		f.dests[0].IP != "192.168.50.9" {
		t.Errorf("ホスト名の完全一致を優先しない: rc=%d 宛先 %+v stderr=%q", rc, f.dests, errb.String())
	}
	for _, name := range []string{"M2MacMini.local.", "M2MacMini"} { // 完全一致の経路と、.local を落とした名前の経路の両方
		f.dests = nil
		if rc := runSpeed([]string{name}, &out, &errb, time.Now, nil, speedModel(nil), f.test); rc != 0 || len(f.dests) != 1 {
			t.Errorf("%s: 台帳の鍵の形に揃えない: rc=%d 測った %d 回 stderr=%q", name, rc, len(f.dests), errb.String())
		}
	}
}

// Bonjour の走査そのものが失敗したら (dns-sd を起こせない等)、scan_error と stderr で出し、rc=1 にする (走査は 3 秒で「終わった」扱いに
// なるので scan_settled は true のまま。測れた相手の値は出す)。
func TestSpeedtestReportsScanError(t *testing.T) {
	f := &fakeSpeed{res: map[string]speedtest.Result{"192.168.100.12": {UpBps: 9e8, DownBps: 9e8}}}
	failing := func() (*lanmgr.Model, error) {
		return lanmgr.New(lanmgr.Options{
			ReadNet: func(context.Context) (netinfo.Snapshot, error) {
				return netinfo.Snapshot{Services: []netinfo.Service{{Name: "Wi-Fi", Port: "Wi-Fi", Device: "en0", Active: true, IP: "192.168.50.250", Router: "192.168.50.1", Read: true}}}, nil
			},
			LocalHost: func() string { return "me.local" },
			StartScan: func(time.Time) lanmgr.Scanner {
				return newScan(false, errors.New("dns-sd -B _ssh._tcp を起こせない"))
			},
			Ledger: &speedLedger{hosts: []ledger.Host{{Host: "m2macmini.local", User: "koji", LAN: "192.168.100.12"}}},
			User:   "koji",
		}), nil
	}
	var out, errb bytes.Buffer
	rc := runSpeed([]string{"--json"}, &out, &errb, time.Now, nil, failing, f.test)
	var rs []speedReport
	if err := json.Unmarshal(out.Bytes(), &rs); err != nil || rc != 1 || len(rs) != 1 || !strings.Contains(rs[0].ScanError, "起こせない") || rs[0].UpBps != 9e8 {
		t.Errorf("走査の失敗を出さない: rc=%d %+v %v", rc, rs, err)
	}
	out.Reset()
	errb.Reset()
	if runSpeed(nil, &out, &errb, time.Now, nil, failing, f.test); !strings.Contains(errb.String(), "Bonjour で探せない") {
		t.Errorf("文字の出力で走査の失敗を知らせない: %q", errb.String())
	}
}
