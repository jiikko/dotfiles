package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jiikko/dotfiles/src/tuikit/widthenv"

	"mac-lan-manager/discover"
	"mac-lan-manager/lanmgr"
	"mac-lan-manager/ledger"
	"mac-lan-manager/netinfo"
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
	if rc := runDump(nil, &out, &errb, time.Now, nil, dumpModel(false)); rc != 0 {
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
	if rc := runDump([]string{"--json", "--wait", "200ms"}, &out, &errb, time.Now, nil, dumpModel(true)); rc != 0 {
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

// 待っている間にシグナル (stop) が来たら、待ちを打ち切って走査を止めてから戻る (止めないと dns-sd が親の外で残った。監査の指摘)。
func TestDumpStopsScanOnSignal(t *testing.T) {
	scan := &dumpScan{busy: true} // 走査が終わらない
	stop := make(chan struct{})
	close(stop)
	var out, errb bytes.Buffer
	start := time.Now()
	rc := runDump([]string{"--wait", "30s"}, &out, &errb, time.Now, stop, func() (*lanmgr.Model, error) {
		return lanmgr.New(lanmgr.Options{StartScan: func(time.Time) lanmgr.Scanner { return scan }}), nil
	})
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
	})
	if rc != 0 || !strings.Contains(out.String(), "台帳が読めない") {
		t.Errorf("rc=%d 開いたときの知らせが出力に無い:\n%s", rc, out.String())
	}
}

type brokenStore struct{}

func (brokenStore) Load() ([]ledger.Host, error)            { return nil, errors.New("壊れた台帳") }
func (brokenStore) Upsert(ledger.Host) error                { return nil }
func (brokenStore) Remove(string) error                     { return nil }
func (brokenStore) Update(string, func(*ledger.Host)) error { return nil }
