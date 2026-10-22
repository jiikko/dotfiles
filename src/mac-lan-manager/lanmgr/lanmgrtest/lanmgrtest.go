// Package lanmgrtest は lanmgr の Scanner と ledger.Store の偽物を、本物の規約のまま 1 つずつ持つ (テスト専用。本番からは使わない)。
// lanmgr・root の dump / speedtest・glogx のテストに写しがあり、Changed を返さない (nil) 写しでは、Changed を待つ経路
// (dump.go の settle の <-m.Changed()・main.go の watch) がテストから通らなかった。glogx からも使うので internal には置かない。
package lanmgrtest

import (
	"errors"
	"slices"
	"sync"
	"time"

	"mac-lan-manager/discover"
	"mac-lan-manager/ledger"
)

// Scan は Bonjour の走査の偽物 (dns-sd を起こさない)。本物 (discover.Scanner) と同じく、Close は何度呼んでもよく、閉じるときに
// Changed を閉じ、閉じている間は Busy が偽になる。値はテストの goroutine と Model の goroutine の両方から触るので、Set* を通す。
// 本物は走査ごとに別の値だが、テストは同じ値を起こし直しに使い回すので、StartScan からは Reopen してから返す。
type Scan struct {
	mu      sync.Mutex
	peers   []discover.Peer
	working bool
	err     error
	closed  bool
	closes  int
	changed chan struct{}
}

// Reopen は閉じた走査を新しい走査として開き直す (Changed は新しいチャネル。閉じた回数は残す)。開いていれば何もしない。
func (s *Scan) Reopen() *Scan {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		s.closed, s.changed = false, make(chan struct{}, 1)
	}
	return s
}

// NewScan は peers が見えている走査 (終わっている。Busy は偽)。
func NewScan(peers ...discover.Peer) *Scan {
	return &Scan{peers: peers, changed: make(chan struct{}, 1)}
}

// SetPeers は見えている Mac を差し替える (Poke は呼ばない)。
func (s *Scan) SetPeers(peers ...discover.Peer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.peers = peers
}

// SetWorking は走査が続いているか (Busy) を決める。
func (s *Scan) SetWorking(w bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.working = w
}

// SetErr は走査そのものの失敗 (dns-sd を起こせない等)。
func (s *Scan) SetErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

// Poke は結果が変わった合図を 1 つ送る (溜まっていれば捨てる。閉じた後は何もしない)。
func (s *Scan) Poke() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.changed <- struct{}{}:
	default:
	}
}

// Closes は Close を呼ばれた回数 (止めたか・何度止めたかを見る)。
func (s *Scan) Closes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closes
}

func (s *Scan) Snapshot() []discover.Peer {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.peers)
}

func (s *Scan) Busy(time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.working && !s.closed
}

func (s *Scan) Changed() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.changed
}

func (s *Scan) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *Scan) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.changed)
	}
	s.closes++
}

// Store は台帳の偽物 (メモリの中だけ。本物のファイルを書かない)。本物 (ledger.FileStore) と同じく、鍵は ledger.Key に揃え、Update は
// 鍵を変えさせず、無い行は ErrNotFound、Merge は空のホスト名を足さない。Hosts は 1 つの goroutine から触る前提。
type Store struct {
	Hosts   []ledger.Host
	LoadErr error // Load だけを失敗させる (壊れた台帳)
}

func (s *Store) Load() ([]ledger.Host, error) {
	if s.LoadErr != nil {
		return nil, s.LoadErr
	}
	return slices.Clone(s.Hosts), nil
}

func (s *Store) Update(host string, f func(*ledger.Host)) error {
	i := slices.IndexFunc(s.Hosts, func(x ledger.Host) bool { return x.Host == ledger.Key(host) })
	if i < 0 {
		return ledger.ErrNotFound
	}
	f(&s.Hosts[i])
	s.Hosts[i].Host = ledger.Key(host)
	return nil
}

func (s *Store) Merge(h ledger.Host, f func(*ledger.Host)) error {
	if err := s.Update(h.Host, f); !errors.Is(err, ledger.ErrNotFound) {
		return err
	}
	if h.Host = ledger.Key(h.Host); h.Host == "" {
		return errors.New("ホスト名が空")
	}
	s.Hosts = append(s.Hosts, h)
	return nil
}

func (s *Store) Remove(host string) error {
	s.Hosts = slices.DeleteFunc(s.Hosts, func(x ledger.Host) bool { return x.Host == ledger.Key(host) })
	return nil
}
