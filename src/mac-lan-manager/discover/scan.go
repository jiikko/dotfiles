package discover

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"
)

// Services は探す Bonjour のサービス (ssh = リモートログイン / rfb = 画面共有 / smb = ファイル共有)。
var Services = []string{"_ssh._tcp", "_rfb._tcp", "_smb._tcp"}

// Peer は見つかった Mac 1 台 (ホスト名ごとにまとめる)。
type Peer struct {
	Host      string         // "m2macmini.local"
	Instances []string       // Bonjour のインスタンス名 ("kojiのMac mini")。名前の衝突で複数ありうる
	SSH       bool           // _ssh._tcp を広告している
	VNC       bool           // _rfb._tcp
	SMB       bool           // _smb._tcp
	Addrs     map[int]string // I/F の番号 → IPv4 (Wi-Fi 側と有線側の両方に見える)
}

// Starter はコマンドを起こして stdout を返す。ctx を取り消すとプロセスを止め、読み手は EOF を受ける (テストで差し替える)。
type Starter func(ctx context.Context, name string, args ...string) (io.ReadCloser, error)

// Scanner は Bonjour の走査。Start で裏の goroutine を起こし、Snapshot で途中の結果を読む。Close で自分が起こしたプロセスを全部止める。
// 🚨 dns-sd は終わらない。-B (一覧) は Close まで動かし、-L / -G (引き当て) は上限 lookupTimeout で止める。
type Scanner struct {
	start   Starter
	ctx     context.Context //nolint:containedctx // Close で全部の dns-sd を止めるための取り消しの根 (Scanner の寿命と同じ)
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.Mutex
	peers   map[string]*Peer
	pending int      // 引き当て中の数 (Busy の判定)
	errs    []string // 走査そのものの失敗 (dns-sd を起こせない / 取り消していないのに -B が終わった)
	settle  time.Time
	changed chan struct{}
	closing sync.Once
	seen    map[string]bool // 引き当てを起こしたサービス + インスタンス (同じものが Wi-Fi と有線の 2 つの I/F で Add されても 1 回だけ引く)
	readded map[string]bool // Rmv の後にもう一度 Add されたもの (相手の再起動など。IP が変わりうるので引き直す)
	sem     chan struct{}   // 同時に起こす引き当ての上限 (LAN 上の広告の数だけ dns-sd を起こさない)
}

// maxLookups は同時に起こす -L / -G の上限。
const maxLookups = 8

// lookupTimeout は -L / -G を待つ上限 (出なければ諦める。相手が居なくなった・応えない)。テストが短くするので var。
var lookupTimeout = 4 * time.Second

// settleWindow は起動から「一覧が出揃うまで」とみなす間 (この間は Busy。後から見つかった Mac は足していく)。
const settleWindow = 3 * time.Second

// Start は走査を起こす。now は settleWindow の起点。
func Start(start Starter, now time.Time) *Scanner {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Scanner{start: start, ctx: ctx, cancel: cancel, peers: map[string]*Peer{}, settle: now.Add(settleWindow), changed: make(chan struct{}, 1),
		seen: map[string]bool{}, readded: map[string]bool{}, sem: make(chan struct{}, maxLookups)}
	for _, svc := range Services {
		s.wg.Add(1)
		go s.browse(svc)
	}
	return s
}

// Changed は結果が変わった合図 (1 つにまとめる。呼び出し側は合図を受けたら Snapshot を読み直す)。
func (s *Scanner) Changed() <-chan struct{} { return s.changed }

func (s *Scanner) notify() {
	select {
	case s.changed <- struct{}{}:
	default:
	}
}

func (s *Scanner) fail(msg string) {
	s.mu.Lock()
	s.errs = append(s.errs, msg)
	s.mu.Unlock()
	s.notify()
}

// Err は走査そのものの失敗 (無ければ nil)。見つかった台数が 0 なのが「居ない」のか「探せていない」のかを分ける。
func (s *Scanner) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.errs) == 0 {
		return nil
	}
	return errors.New(strings.Join(s.errs, " / "))
}

// Busy は引き当て中か、起動から settleWindow の内か。
func (s *Scanner) Busy(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pending > 0 || now.Before(s.settle)
}

// Snapshot は見つかった Mac の写し (ホスト名の順)。
func (s *Scanner) Snapshot() []Peer {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Peer, 0, len(s.peers))
	for _, p := range s.peers {
		c := *p
		c.Instances = slices.Clone(p.Instances)
		c.Addrs = make(map[int]string, len(p.Addrs))
		for k, v := range p.Addrs {
			c.Addrs[k] = v
		}
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b Peer) int {
		switch {
		case a.Host < b.Host:
			return -1
		case a.Host > b.Host:
			return 1
		}
		return 0
	})
	return out
}

// Close は自分が起こした dns-sd を全部止め、裏の goroutine が終わるのを待ってから Changed を閉じる (待っている側は ok=false で抜ける。
// 閉じないと、開き直しや r のたびに古いチャネルを待つ goroutine が 1 本ずつ残る)。何度呼んでもよい。
// 🚨 notify は wg が待つ goroutine の中からしか呼ばないので、Wait の後に閉じれば閉じたチャネルへ送らない。
func (s *Scanner) Close() {
	s.closing.Do(func() {
		s.cancel()
		s.wg.Wait()
		close(s.changed)
	})
}

func (s *Scanner) browse(svc string) {
	defer s.wg.Done()
	r, err := s.start(s.ctx, "dns-sd", "-B", svc, "local")
	if err != nil {
		if s.ctx.Err() == nil { // 取り消した後の起動の失敗 (Close と Start の競り合い) は失敗にしない
			s.fail(fmt.Sprintf("dns-sd -B %s を起こせない: %v", svc, err))
		}
		return
	}
	defer func() { _ = r.Close() }()
	sc := bufio.NewScanner(r)
	// -B は終わらない。取り消していないのに終わったら失敗 (dns-sd が無い・mDNSResponder が止まっている)。黙って抜けると
	// 「0 台見つかった」と区別が付かなかった
	defer func() {
		if s.ctx.Err() == nil {
			s.fail("dns-sd -B " + svc + " が途中で終わった")
		}
	}()
	for sc.Scan() {
		b, ok := ParseBrowse(sc.Text())
		if !ok || !s.onBrowse(svc, b) {
			continue
		}
		s.wg.Add(1)
		go s.resolve(svc, b.Instance)
	}
}

// onBrowse は -B の 1 行を帳簿に付け、引き当てを起こすかを返す。Add は 1 回だけ引く (2 つの I/F で Add されても)。Rmv は帳簿から外し、
// その後にまた Add されたら引き直す: 相手が再起動すると 169.254 の IP が変わるので、外さないと r を押すまで古い IP のままになる。
// 見えなくなった Mac は一覧に残す (台帳の Mac と同じく「今は見えていない」は r で消える)。
func (s *Scanner) onBrowse(svc string, b Browse) bool {
	key := svc + "\x00" + b.Instance
	s.mu.Lock()
	defer s.mu.Unlock()
	if !b.Add {
		if s.seen[key] {
			delete(s.seen, key)
			s.readded[key] = true // 次の Add は引き直し
		}
		return false
	}
	if s.seen[key] {
		return false
	}
	s.seen[key] = true
	s.pending++
	return true
}

// resolve はインスタンスをホスト名に引き当て (-L)、初めてのホストなら IP も引く (-G)。
func (s *Scanner) resolve(svc, inst string) {
	defer s.wg.Done()
	defer func() {
		s.mu.Lock()
		s.pending--
		s.mu.Unlock()
		s.notify()
	}()
	select { // 同時に起こす数の上限
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	case <-s.ctx.Done():
		return
	}
	host := s.first(func(line string) (string, bool) {
		r, ok := ParseLookup(line)
		return r.Host, ok
	}, "dns-sd", "-L", inst, svc, "local")
	if host == "" {
		return
	}
	s.mu.Lock()
	key := svc + "\x00" + inst
	refresh := s.readded[key]
	delete(s.readded, key)
	p := s.peers[host]
	newHost := p == nil
	if newHost {
		p = &Peer{Host: host, Addrs: map[int]string{}}
		s.peers[host] = p
	}
	if !slices.Contains(p.Instances, inst) {
		p.Instances = append(p.Instances, inst)
	}
	switch svc {
	case "_ssh._tcp":
		p.SSH = true
	case "_rfb._tcp":
		p.VNC = true
	case "_smb._tcp":
		p.SMB = true
	}
	s.mu.Unlock()
	if newHost || refresh { // 同じ I/F の IP は I/F の番号で上書きされる (Addrs は I/F の番号が鍵)
		s.addrs(host)
	}
}

// addrs は -G v4 で IP を集める (lookupTimeout まで。Wi-Fi 側と有線側で別々の行が来る)。
func (s *Scanner) addrs(host string) {
	ctx, cancel := context.WithTimeout(s.ctx, lookupTimeout)
	defer cancel()
	r, err := s.start(ctx, "dns-sd", "-G", "v4", host)
	if err != nil {
		return
	}
	defer func() { _ = r.Close() }()
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		a, ok := ParseAddr(sc.Text())
		if !ok || !a.Add {
			continue
		}
		s.mu.Lock()
		if p := s.peers[host]; p != nil {
			p.Addrs[a.If] = a.IP
		}
		s.mu.Unlock()
		s.notify()
	}
}

// first はコマンドを起こし、parse が ok を返した最初の値で止める (lookupTimeout まで)。
func (s *Scanner) first(parse func(string) (string, bool), name string, args ...string) string {
	ctx, cancel := context.WithTimeout(s.ctx, lookupTimeout)
	defer cancel()
	r, err := s.start(ctx, name, args...)
	if err != nil {
		return ""
	}
	defer func() { _ = r.Close() }()
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		if v, ok := parse(sc.Text()); ok {
			return v
		}
	}
	return ""
}
