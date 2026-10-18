package discover

import (
	"context"
	"errors"
	"fmt"
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
		// 同じインスタンスが Wi-Fi (14) と有線 (27) の 2 つの I/F で Add されても、引き当ては 1 回
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

// 同じ I/F の重複の Add は引かず、全部の I/F で Rmv された後の Add は引き直す (相手の再起動で IP が変わる)。Rmv だけでは引かない。
// 1 つの I/F だけの Rmv では外さない (もう 1 つの I/F ではまだ見えている)。
func TestOnBrowseReresolvesAfterRemove(t *testing.T) {
	s := &Scanner{seen: map[string]map[int]bool{}, readded: map[string]bool{}, hostOf: map[string]string{}, gathering: map[string]bool{}, regather: map[string]bool{}, dropped: map[string]bool{}, peers: map[string]*Peer{}}
	add := func(ifn int) Browse { return Browse{Add: true, If: ifn, Instance: "mini"} }
	rmv := func(ifn int) Browse { return Browse{Add: false, If: ifn, Instance: "mini"} }
	if act, _ := s.onBrowse("_ssh._tcp", add(14)); act != actResolve {
		t.Fatal("最初の Add で引かない")
	}
	if act, _ := s.onBrowse("_ssh._tcp", add(14)); act != actNone {
		t.Error("同じ I/F の重複の Add でもう一度引いた")
	}
	if act, _ := s.onBrowse("_ssh._tcp", add(27)); act != actNone {
		t.Error("ホストを引いている途中に別の I/F の Add で引いた (起動の直後に 2 つの I/F で Add されると引き当てが二重になる)")
	}
	if act, _ := s.onBrowse("_ssh._tcp", rmv(14)); act != actNone || s.readded["_ssh._tcp\x00mini"] {
		t.Error("1 つの I/F の Rmv で外した (もう 1 つの I/F ではまだ見えている)")
	}
	if act, _ := s.onBrowse("_ssh._tcp", rmv(27)); act != actNone {
		t.Error("Rmv で引いた")
	}
	if act, _ := s.onBrowse("_ssh._tcp", add(27)); act != actResolve {
		t.Fatal("全部の I/F の Rmv の後の Add で引き直さない (再起動で変わった IP が古いまま残る)")
	}
	if !s.readded["_ssh._tcp\x00mini"] {
		t.Error("引き直しの印が無い (resolve が -G を起こさず、IP を取り直さない)")
	}
}

// 起動の後に上がった I/F (後から挿したケーブル) で同じ Mac が Add されたら、-G を引き直してその I/F の IP を載せる。
// -L は引き直さない (ホストは分かっている)。Add が -G の終わった後に来る形と、-G の途中に来る形 (走っている -G は有線の IP を返さずに
// 終わる) の両方を見る。
func TestScannerPicksUpLateInterface(t *testing.T) {
	for _, during := range []bool{false, true} {
		t.Run(fmt.Sprintf("during=%v", during), func(t *testing.T) { scanLateInterface(t, during) })
	}
}

func scanLateInterface(t *testing.T, during bool) {
	var mu sync.Mutex
	gCalls, lCalls := 0, 0
	var s *Scanner
	gStarted := make(chan struct{})
	// state はスキャナの帳簿を条件が立つまで見る (待ちの実体は条件。上限で諦めて先へ進め、結果の assert で落とす)
	state := func(cond func() bool) {
		for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
			s.mu.Lock()
			ok := cond()
			s.mu.Unlock()
			if ok {
				return
			}
			time.Sleep(5 * time.Millisecond) // sleep-ok: tick: 条件のポーリングの刻み (待ちの実体は cond)
		}
	}
	start := func(ctx context.Context, _ string, args ...string) (io.ReadCloser, error) {
		pr, pw := io.Pipe()
		switch {
		case args[0] == "-B" && args[1] == "_ssh._tcp":
			go func() {
				_, _ = io.WriteString(pw, "21:28:02.496  Add        3  14 local.               _ssh._tcp.           mini\n")
				select {
				case <-gStarted:
				case <-ctx.Done():
				}
				if !during { // 有線が上がるのは、Wi-Fi の -G が終わった後
					state(func() bool { return len(s.gathering) == 0 && len(s.peers) == 1 })
				}
				_, _ = io.WriteString(pw, "21:28:03.000  Add        3  27 local.               _ssh._tcp.           mini\n")
				<-ctx.Done()
				_ = pw.Close()
			}()
		case args[0] == "-B":
			go func() { <-ctx.Done(); _ = pw.Close() }()
		case args[0] == "-L":
			mu.Lock()
			lCalls++
			mu.Unlock()
			go func() {
				_, _ = io.WriteString(pw, "21:28:48.953  mini._ssh._tcp.local. can be reached at m2macmini.local.:22 (interface 14)\n")
				_ = pw.Close()
			}()
		case args[0] == "-G":
			mu.Lock()
			gCalls++
			n := gCalls
			mu.Unlock()
			go func() {
				_, _ = io.WriteString(pw, "21:27:58.076  Add  40000002      14  m2macmini.local.                       192.168.50.7                                 4500\n")
				if n == 1 {
					close(gStarted)
					if during { // 有線の Add を受けてから、有線の IP を返さずに終わる
						state(func() bool { return s.seen["_ssh._tcp\x00mini"][27] })
					}
				} else { // 有線が上がった後の -G だけが有線の IP を返す
					_, _ = io.WriteString(pw, "21:27:58.076  Add  40000003      27  m2macmini.local.                       192.168.100.12                               4500\n")
				}
				_ = pw.Close()
			}()
		}
		return pr, nil
	}
	s = Start(start, time.Now())
	deadline := time.Now().Add(5 * time.Second)
	var peers []Peer
	for time.Now().Before(deadline) {
		if peers = s.Snapshot(); len(peers) == 1 && peers[0].Addrs[27] != "" {
			break
		}
		select {
		case <-s.Changed():
		case <-time.After(50 * time.Millisecond): // sleep-ok: 条件のポーリングの刻み (待ちの実体は上の条件)
		}
	}
	s.Close()
	if len(peers) != 1 || peers[0].Addrs[27] != "192.168.100.12" || peers[0].Addrs[14] != "192.168.50.7" {
		t.Fatalf("後から上がった有線の IP を拾わない: %+v", peers)
	}
	mu.Lock()
	defer mu.Unlock()
	if lCalls != 1 || gCalls != 2 {
		t.Errorf("-L %d 回 / -G %d 回, want 1 / 2", lCalls, gCalls)
	}
}

// -L / -G が上限までに答えなかったら、もう 1 度引く (1 度の無応答で、その走査の間ずっと一覧から欠ける・IP の無い行になるのを避ける)。
func TestResolveRetriesWhenLookupsTimeOut(t *testing.T) {
	var mu sync.Mutex
	calls := map[string]int{}
	start := func(ctx context.Context, _ string, args ...string) (io.ReadCloser, error) {
		pr, pw := io.Pipe()
		mu.Lock()
		calls[args[0]]++
		n := calls[args[0]]
		mu.Unlock()
		go func() {
			switch {
			case args[0] == "-B" && args[1] == "_ssh._tcp":
				_, _ = io.WriteString(pw, "21:28:02.496  Add        3  14 local.               _ssh._tcp.           mini\n")
				<-ctx.Done()
			case args[0] == "-B":
				<-ctx.Done()
			case n == 1: // 1 回目の -L / -G は答えない (上限で止まって何も出さずに終わったのと同じ)
			case args[0] == "-L":
				_, _ = io.WriteString(pw, "21:28:48.953  mini._ssh._tcp.local. can be reached at m2macmini.local.:22 (interface 14)\n")
			case args[0] == "-G":
				_, _ = io.WriteString(pw, "21:27:58.076  Add  40000002      14  m2macmini.local.                       192.168.50.7                                 4500\n")
			}
			_ = pw.Close()
		}()
		return pr, nil
	}
	s := Start(start, time.Now())
	deadline := time.Now().Add(5 * time.Second)
	var peers []Peer
	for time.Now().Before(deadline) {
		if peers = s.Snapshot(); len(peers) == 1 && peers[0].Addrs[14] != "" {
			break
		}
		select {
		case <-s.Changed():
		case <-time.After(50 * time.Millisecond): // sleep-ok: 条件のポーリングの刻み (待ちの実体は上の条件)
		}
	}
	s.Close()
	if len(peers) != 1 || peers[0].Addrs[14] != "192.168.50.7" {
		t.Fatalf("1 度答えなかった -L / -G を引き直さない: %+v (calls %v)", peers, calls)
	}
}

// 1 つの I/F だけで Rmv されて同じ I/F で Add し直されたら、Addrs に古い IP が残っていても -G を引き直す (片方の I/F の goodbye だけが
// 届いた再起動・ケーブルの挿し直しで、169.254 の番号が変わる)。
func TestOnBrowseRegathersAfterOneInterfaceRemoved(t *testing.T) {
	s := &Scanner{seen: map[string]map[int]bool{}, readded: map[string]bool{}, hostOf: map[string]string{}, gathering: map[string]bool{},
		regather: map[string]bool{}, dropped: map[string]bool{}, peers: map[string]*Peer{}}
	key := "_ssh._tcp\x00mini"
	s.seen[key] = map[int]bool{14: true, 27: true}
	s.hostOf[key] = "m2macmini.local"
	s.peers["m2macmini.local"] = &Peer{Host: "m2macmini.local", Addrs: map[int]string{14: "192.168.50.7", 27: "169.254.1.1"}}
	if act, _ := s.onBrowse("_ssh._tcp", Browse{Add: false, If: 27, Instance: "mini"}); act != actNone {
		t.Fatal("Rmv で引いた")
	}
	if act, host := s.onBrowse("_ssh._tcp", Browse{Add: true, If: 27, Instance: "mini"}); act != actAddrs || host != "m2macmini.local" {
		t.Errorf("片方の I/F の Rmv の後の Add で -G を引き直さない: act=%v host=%q", act, host)
	}
}

// 別のサービスで分かっているホストでも、この インスタンスが IP の無い I/F で見えていれば、-L の後に -G を引く
// (-L の途中に来た有線の Add は onBrowse では引かず、resolve に任せる)。
func TestResolveGathersForInterfaceWithoutIP(t *testing.T) {
	f := &fakeStarter{out: map[string]string{
		"dns-sd -L mini _ssh._tcp local": "21:28:48.953  mini._ssh._tcp.local. can be reached at m2macmini.local.:22 (interface 14)\n",
		"dns-sd -G v4 m2macmini.local":   "21:27:58.076  Add  40000003      27  m2macmini.local.                       192.168.100.12                               4500\n",
	}}
	s := Start(f.start, time.Now()) // -B の偽物は無い (起こせずにすぐ終わる)
	defer s.Close()
	s.mu.Lock()
	s.peers["m2macmini.local"] = &Peer{Host: "m2macmini.local", Addrs: map[int]string{14: "192.168.50.7"}, Instances: []string{"mini"}, SMB: true}
	s.seen["_ssh._tcp\x00mini"] = map[int]bool{14: true, 27: true}
	s.pending++
	s.mu.Unlock()
	s.wg.Add(1)
	s.resolve("_ssh._tcp", "mini")
	if got := s.Snapshot()[0].Addrs[27]; got != "192.168.100.12" {
		t.Errorf("IP の無い I/F の IP を引かない: %q", got)
	}
}

// -L の途中 (ホストがまだ分からない) に片方の I/F で Rmv → Add が来たら、引き直しの印を立てる (resolve が -G を引き直す)。
// 全部の I/F で Rmv されたら、I/F ごとの印は消す。
func TestOnBrowseMarksReaddedDuringLookup(t *testing.T) {
	s := &Scanner{seen: map[string]map[int]bool{}, readded: map[string]bool{}, hostOf: map[string]string{}, gathering: map[string]bool{},
		regather: map[string]bool{}, dropped: map[string]bool{}, peers: map[string]*Peer{}}
	key := "_ssh._tcp\x00mini"
	s.seen[key] = map[int]bool{14: true, 27: true}
	s.onBrowse("_ssh._tcp", Browse{Add: false, If: 27, Instance: "mini"})
	if act, _ := s.onBrowse("_ssh._tcp", Browse{Add: true, If: 27, Instance: "mini"}); act != actNone || !s.readded[key] {
		t.Errorf("-L の途中の片方の I/F の Rmv → Add で引き直しの印を立てない: act=%v readded=%v", act, s.readded[key])
	}
	s.onBrowse("_ssh._tcp", Browse{Add: false, If: 27, Instance: "mini"})
	s.onBrowse("_ssh._tcp", Browse{Add: false, If: 14, Instance: "mini"})
	if len(s.dropped) != 0 {
		t.Errorf("全部の I/F で Rmv された後に I/F ごとの印が残った: %v", s.dropped)
	}
}

// 引き直し (再起動の後の Add) の resolve で同じホストの -G が走っていたら、終わった後にもう 1 度引かせる
// (走っている -G は IP が変わる前に引き始めたかもしれない)。
func TestResolveRegathersWhenRefreshOverlapsGather(t *testing.T) {
	f := &fakeStarter{out: map[string]string{
		"dns-sd -L mini _ssh._tcp local": "21:28:48.953  mini._ssh._tcp.local. can be reached at m2macmini.local.:22 (interface 27)\n",
	}}
	s := Start(f.start, time.Now())
	defer s.Close()
	s.mu.Lock()
	s.peers["m2macmini.local"] = &Peer{Host: "m2macmini.local", Addrs: map[int]string{27: "169.254.1.1"}, Instances: []string{"mini"}}
	s.seen["_ssh._tcp\x00mini"] = map[int]bool{27: true}
	s.readded["_ssh._tcp\x00mini"] = true
	s.gathering["m2macmini.local"] = true // 別のサービスの -G が走っている
	s.pending++
	s.mu.Unlock()
	s.wg.Add(1)
	s.resolve("_ssh._tcp", "mini")
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.regather["m2macmini.local"] {
		t.Error("走っている -G と重なった引き直しを、終わった後に引かせない")
	}
}

// 引き直しの印のあるインスタンスは、知っているホストでも -G を起こし直し、同じ I/F の古い IP を新しい IP で上書きする。
func TestResolveRefreshesAddrsWhenReadded(t *testing.T) {
	f := &fakeStarter{out: map[string]string{
		"dns-sd -L mini _ssh._tcp local": "21:28:48.953  mini._ssh._tcp.local. can be reached at m2macmini.local.:22 (interface 27)\n",
		"dns-sd -G v4 m2macmini.local":   "21:27:58.076  Add  40000002      27  m2macmini.local.                       169.254.9.9                                  4500\n",
	}}
	s := Start(f.start, time.Now()) // -B の偽物は無い (起こせずにすぐ終わる)
	defer s.Close()
	s.mu.Lock()
	s.peers["m2macmini.local"] = &Peer{Host: "m2macmini.local", Addrs: map[int]string{27: "169.254.1.1"}, Instances: []string{"mini"}}
	s.readded["_ssh._tcp\x00mini"] = true
	s.pending++
	s.mu.Unlock()
	s.wg.Add(1)
	s.resolve("_ssh._tcp", "mini")
	if got := s.Snapshot()[0].Addrs[27]; got != "169.254.9.9" {
		t.Errorf("再起動で変わった IP を取り直していない: %q", got)
	}
}

// dns-sd を起こせない / -B が取り消していないのに終わったら Err で知らせる (黙ると「0 台見つかった」と区別できない)。
// 取り消し (Close) で終わったのは失敗にしない。
func TestScannerReportsBrowseFailure(t *testing.T) {
	f := &fakeStarter{} // _ssh._tcp は上で横取りする。残り 2 つは偽物が無い = 起こせない
	s := Start(func(ctx context.Context, name string, args ...string) (io.ReadCloser, error) {
		if len(args) > 1 && args[1] == "_ssh._tcp" {
			// -B が 1 行出してすぐ終わる (本物は終わらない)
			return io.NopCloser(strings.NewReader("Browsing for _ssh._tcp.local\n")), nil
		}
		return f.start(ctx, name, args...) // 残り 2 つは「偽物が無い」= 起こせない
	}, time.Now())
	for deadline := time.Now().Add(5 * time.Second); s.Err() == nil || strings.Count(s.Err().Error(), "dns-sd") < 3; {
		if time.Now().After(deadline) {
			t.Fatalf("走査の失敗を知らせない: %v", s.Err())
		}
		<-time.After(10 * time.Millisecond)
	}
	s.Close()
	if !strings.Contains(s.Err().Error(), "途中で終わった") || !strings.Contains(s.Err().Error(), "起こせない") {
		t.Errorf("Err = %v", s.Err())
	}

	ok := Start(func(ctx context.Context, _ string, _ ...string) (io.ReadCloser, error) {
		r, w := io.Pipe()
		go func() { <-ctx.Done(); _ = w.Close() }()
		return r, nil
	}, time.Now())
	ok.Close()
	if ok.Err() != nil {
		t.Errorf("取り消しで終わったのを失敗にした: %v", ok.Err())
	}
}

// 取り消しの後に起こそうとして失敗したもの (exec.CommandContext は取り消し済みの ctx で起動を断る) は、走査の失敗にしない
// (Close の直後に Err が「起こせない: context canceled」を返していた)。
func TestScannerCloseIsNotFailure(t *testing.T) {
	for range 20 {
		s := Start(func(ctx context.Context, _ string, _ ...string) (io.ReadCloser, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			r, w := io.Pipe()
			go func() { <-ctx.Done(); _ = w.Close() }()
			return r, nil
		}, time.Now())
		s.Close()
		if err := s.Err(); err != nil {
			t.Fatalf("取り消しを失敗にした: %v", err)
		}
	}
}

// hangStarter は -L / -G が何も出さずに止められるまで待つ dns-sd (応えない相手)。同時に生きている -L の数の最大を数える。
type hangStarter struct {
	mu        sync.Mutex
	browse    string
	live, max int
	lookups   int // 起こした -L の数
}

func (h *hangStarter) start(ctx context.Context, _ string, args ...string) (io.ReadCloser, error) {
	pr, pw := io.Pipe()
	out := ""
	if args[0] == "-B" && args[1] == "_ssh._tcp" {
		out = h.browse
	}
	lookup := args[0] == "-L"
	if lookup {
		h.mu.Lock()
		h.live++
		h.lookups++
		h.max = max(h.max, h.live)
		h.mu.Unlock()
	}
	go func() {
		_, _ = io.WriteString(pw, out)
		<-ctx.Done() // 応えない: 止められるまで閉じない
		if lookup {
			h.mu.Lock()
			h.live--
			h.mu.Unlock()
		}
		_ = pw.Close()
	}()
	return pr, nil
}

// 応えない相手が多くても、同時に起こす -L は maxLookups まで。応えない -L は lookupTimeout で諦めて、引き当て中の数を戻す
// (上限の select を外しても、待ちの上限を 4 時間にしても全部緑だった)。
func TestResolveIsBoundedAndTimesOut(t *testing.T) {
	old := lookupTimeout
	lookupTimeout = 100 * time.Millisecond
	t.Cleanup(func() { lookupTimeout = old })
	var b strings.Builder
	b.WriteString("Browsing for _ssh._tcp.local\n")
	for i := range 20 {
		fmt.Fprintf(&b, "10:57:52.588  Add        3  27 local.               _ssh._tcp.           mac%02d\n", i)
	}
	h := &hangStarter{browse: b.String()}
	s := Start(h.start, time.Now())
	defer s.Close()
	deadline := time.Now().Add(10 * time.Second)
	for {
		s.mu.Lock()
		p, seen := s.pending, len(s.seen)
		s.mu.Unlock()
		h.mu.Lock()
		n := h.lookups
		h.mu.Unlock()
		// 諦めたインスタンスは帳簿から外す (後で別の I/F で Add されたら引き直す)
		if n == 20*lookupAttempts && p == 0 && seen == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("応えない -L が諦められない (-L %d 回 seen %d pending %d)", n, seen, p)
		}
		time.Sleep(10 * time.Millisecond) // sleep-ok: tick: 条件のポーリングの刻み (待ちの実体は上の条件)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.max > maxLookups || h.max < 2 {
		t.Errorf("同時に生きていた -L の最大 = %d (上限 %d。2 未満なら並べて起こしていない)", h.max, maxLookups)
	}
}
