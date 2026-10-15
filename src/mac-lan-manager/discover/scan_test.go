package discover

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeStarter は dns-sd の偽物。-B は行を出した後、止められる (ctx の取り消し・Close) まで閉じない (本物と同じく終わらない)。
// -L / -G は行を出したら閉じる。起こした回数と、止められずに残っている数を数える。
type fakeStarter struct {
	mu      sync.Mutex
	out     map[string]string // コマンドの引数 → 出力
	started int
	live    int
}

func (f *fakeStarter) start(ctx context.Context, name string, args ...string) (io.ReadCloser, error) {
	key := name + " " + strings.Join(args, " ")
	f.mu.Lock()
	out, ok := f.out[key]
	f.started++
	f.mu.Unlock()
	if !ok {
		return nil, errors.New("no fake: " + key)
	}
	pr, pw := io.Pipe()
	f.mu.Lock()
	f.live++
	f.mu.Unlock()
	browse := len(args) > 0 && args[0] == "-B"
	done := make(chan struct{})
	go func() {
		_, _ = io.WriteString(pw, out)
		if browse {
			select { // 本物の -B は終わらない
			case <-ctx.Done():
			case <-done:
			}
		}
		_ = pw.Close()
	}()
	return &fakeReader{PipeReader: pr, done: done, f: f}, nil
}

type fakeReader struct {
	*io.PipeReader
	done chan struct{}
	once sync.Once
	f    *fakeStarter
}

func (r *fakeReader) Close() error {
	r.once.Do(func() {
		close(r.done)
		r.f.mu.Lock()
		r.f.live--
		r.f.mu.Unlock()
	})
	return r.PipeReader.Close()
}

func TestScannerFindsPeersAndClosesAll(t *testing.T) {
	f := &fakeStarter{out: map[string]string{
		"dns-sd -B _ssh._tcp local": "Browsing for _ssh._tcp.local\n" +
			"21:28:02.496  Add        3  27 local.               _ssh._tcp.           kojiのMac mini\n" +
			"21:28:02.496  Add        2  14 local.               _ssh._tcp.           kojiのMac meeini (2)\n",
		"dns-sd -B _rfb._tcp local": "21:28:02.496  Add        3  27 local.               _rfb._tcp.           kojiのMac mini\n",
		// 同じインスタンスが Wi-Fi (14) と有線 (27) の 2 つの I/F で Add されても、引き当ては 1 回 (red team の P3-6)
		"dns-sd -B _smb._tcp local": "21:28:02.496  Add        3  27 local.               _smb._tcp.           kojiのMac mini\n" +
			"21:28:02.496  Add        3  14 local.               _smb._tcp.           kojiのMac mini\n",
		"dns-sd -L kojiのMac mini _smb._tcp local": "21:28:48.953  kojiのMac\\032mini._smb._tcp.local. can be reached at m2macmini.local.:445 (interface 27)\n",
		"dns-sd -L kojiのMac mini _ssh._tcp local": "21:28:48.953  kojiのMac\\032mini._ssh._tcp.local. can be reached at m2macmini.local.:22 (interface 27)\n",
		"dns-sd -L kojiのMac mini _rfb._tcp local": "21:28:48.953  kojiのMac\\032mini._rfb._tcp.local. can be reached at m2macmini.local.:5900 (interface 27)\n",
		// 名前の衝突で (2) が付いた同じ Mac は同じホストに引き当たる (ホスト名でまとめる)
		"dns-sd -L kojiのMac meeini (2) _ssh._tcp local": "21:28:48.953  x._ssh._tcp.local. can be reached at m2macmini.local.:22 (interface 14)\n",
		"dns-sd -G v4 m2macmini.local": "21:27:58.076  Add  40000003      14  m2macmini.local.                       192.168.50.7                                 4500\n" +
			"21:27:58.076  Add  40000002      27  m2macmini.local.                       169.254.217.100                              4500\n",
	}}
	s := Start(f.start, time.Now())
	deadline := time.Now().Add(5 * time.Second)
	var peers []Peer
	for time.Now().Before(deadline) {
		peers = s.Snapshot()
		if len(peers) == 1 && len(peers[0].Addrs) == 2 && peers[0].VNC && peers[0].SMB && len(peers[0].Instances) == 2 && !s.Busy(time.Now().Add(time.Hour)) {
			break
		}
		select {
		case <-s.Changed():
		case <-time.After(50 * time.Millisecond): // sleep-ok: 条件のポーリングの刻み (待ちの実体は上の条件)
		}
	}
	s.Close()
	if len(peers) != 1 {
		t.Fatalf("peers = %+v", peers)
	}
	p := peers[0]
	if p.Host != "m2macmini.local" || !p.SSH || !p.VNC || !p.SMB || p.Addrs[14] != "192.168.50.7" || p.Addrs[27] != "169.254.217.100" || len(p.Instances) != 2 {
		t.Errorf("peer = %+v", p)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.live != 0 {
		t.Errorf("Close の後に止まっていない dns-sd が %d 本残っている", f.live)
	}
	if f.started != 3+4+1 { // -B ×3 / -L ×4 (smb の 2 つの I/F の Add は 1 回にまとめる) / -G は新しいホストで 1 回だけ
		t.Errorf("起こした数 = %d, want 8", f.started)
	}
}

// Close は Changed を閉じる (待っている側を ok=false で抜けさせる。閉じないと開き直しのたびに待ちの goroutine が残る)。2 度呼んでもよい。
func TestCloseClosesChanged(t *testing.T) {
	s := Start(func(ctx context.Context, _ string, _ ...string) (io.ReadCloser, error) {
		r, w := io.Pipe()
		go func() { <-ctx.Done(); _ = w.Close() }()
		return r, nil
	}, time.Now())
	ch := s.Changed()
	s.Close()
	s.Close()
	for { // 閉じる前に積まれた合図が 1 つ残っていることがあるので、閉じたと分かるまで読む
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Close の後も Changed が閉じない")
		}
	}
}
