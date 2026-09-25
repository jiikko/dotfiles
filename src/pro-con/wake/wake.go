// Package wake は dispatcher を即時に起こす口 (Unix domain socket)。
//
//   - 受付の箱に依頼を置いた側 (画面・pro-con card) は Poke で「起きて」と送る。dispatcher は待ちを切り上げてすぐ回る
//
//   - 画面は Subscribe で購読し、dispatcher が記録を変えたら Broadcast で知らされて、すぐ読み直す
//
//   - 画面を開いた・閉じたときは Notify で、ほかの画面へ「読み直して」と送る (dispatcher が中継する。画面の数は package presence が数える)
//
// どれも速くするための口で、正しさは頼らない: 届かなくても (dispatcher が居ない・socket が消えた) 箱のファイルは残り、
// dispatcher と画面のポーリング (3 秒) が拾う。PG の状態・watchdog・利用枠はポーリングのまま (知らせてくれる相手がいない)。
package wake

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// File は状態の置き場に置く socket の名前。
const File = "dispatcher.sock"

// maxPath は socket のパスの長さの上限 (macOS の sun_path は 104 バイト。終端の NUL の分を引く)。
const maxPath = 103

// ioTimeout は 1 回の送受信の上限 (相手が固まっていても、依頼を置いた側・dispatcher を止めない)。
const ioTimeout = time.Second

// retryEvery は購読が切れたときに繋ぎ直す間隔。
var retryEvery = 250 * time.Millisecond // 画面は dispatcher より先に起動することが多い (画面が起こす)。居ない socket への dial はほぼ只

// Path は状態の置き場 dir の socket のパス。長すぎれば (e2e の置き場など) /tmp/pro-con-<uid>/ に dir のハッシュで置く。
// 🚨 dispatcher・画面・pro-con card (PG の中から打つ) は別のプロセスで、同じ置き場を別の書き方で持ちうる (/tmp と /private/tmp、
// 相対パス)。どれも同じ socket に着くよう、symlink を解いた絶対パスで決める。逃がす先は TMPDIR にしない (プロセスごとに違いうる)
func Path(dir string) string {
	dir = canonical(dir)
	if p := filepath.Join(dir, File); len(p) <= maxPath {
		return p
	}
	sum := sha1.Sum([]byte(dir))
	return filepath.Join(fallbackDir(), hex.EncodeToString(sum[:])[:16]+".sock")
}

func canonical(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	return filepath.Clean(dir)
}

// fallbackRoot は長い置き場の socket を置くディレクトリの親 (テストが一時ディレクトリに差し替える)。
var fallbackRoot = "/tmp"

// SetFallbackRoot は fallbackRoot を差し替え、戻す関数を返す (ほかの package のテストが本物の /tmp/pro-con-<uid> に触らないため)。
func SetFallbackRoot(root string) (restore func()) {
	old := fallbackRoot
	fallbackRoot = root
	return func() { fallbackRoot = old }
}

// RunIsolated は逃がし先の親を使い捨ての一時ディレクトリにして run を走らせる (ほかの package の TestMain から呼ぶ。
// t.TempDir の置き場は macOS では長く、socket が逃がし先へ倒れるので、差し替えないと本物の /tmp/pro-con-<uid> を作り・直し・繋ぎに行く)。
func RunIsolated(run func() int) int {
	root, err := os.MkdirTemp("/tmp", "pcfb") // 短く (逃がした socket のパスも上限に収める)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "wake: 逃がし先の親を作れない:", err)
		return 1
	}
	defer func() { _ = os.RemoveAll(root) }()
	defer SetFallbackRoot(root)()
	return run()
}

// fallbackDir は長い置き場の socket を置くディレクトリ。
func fallbackDir() string { return filepath.Join(fallbackRoot, fmt.Sprintf("pro-con-%d", os.Getuid())) }

// ErrUnsafeDir は逃がす先 (fallbackDir) を使えないとき (他のユーザーのもの・symlink・権限が緩い)。繋ぐ側はつながずに読み直しで待つ。
var ErrUnsafeDir = errors.New("socket の逃がし先を使えない")

// ensureFallbackDir は fallbackDir を自分だけのディレクトリとして用意する (Listen = dispatcher だけが呼ぶ)。
// 🚨 /tmp は誰でも書けるので、他のユーザーが先に作った (持ち主が違う・symlink) ものは使わない (dispatcher に成りすまされる)。
// 自分のもので権限だけ緩い (テストが落ちて戻し損ねた等) なら 0700 に直す。直すのは listen する側だけ (繋ぐ側 = 読む口は直さない。issue 445)
func ensureFallbackDir() error {
	d := fallbackDir()
	if err := os.Mkdir(d, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	loose, err := checkFallbackDir(d)
	if err != nil {
		return err
	}
	if loose {
		return os.Chmod(d, 0o700)
	}
	return nil
}

// checkFallbackDir は d が自分のディレクトリかを確かめ、権限が緩い (0700 より広い) かを返す。何も書かない。
func checkFallbackDir(d string) (loose bool, err error) {
	st, err := os.Lstat(d)
	if err != nil {
		return false, err
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !st.IsDir() || !ok || int(sys.Uid) != os.Getuid() {
		return false, fmt.Errorf("%w: %s が自分のディレクトリではない (他のユーザーが作った・symlink)", ErrUnsafeDir, d)
	}
	return st.Mode().Perm()&0o077 != 0, nil
}

// dialPath は繋ぐ先の socket のパス。逃がす先なら、自分のディレクトリで権限が 0700 かを先に確かめる。
// 🚨 繋ぐ側は権限を直さない (読むだけの口 = card wait・log --follow・--view の画面が、状態の置き場の外の metadata を書かない)。
// 緩ければつながずに ErrUnsafeDir を返す (呼び出し側はポーリングで待つ。次に dispatcher が Listen するときに直す)
func dialPath(dir string) (string, error) {
	p := Path(dir)
	if d := filepath.Dir(p); d == fallbackDir() {
		loose, err := checkFallbackDir(d)
		if err != nil {
			return "", err
		}
		if loose {
			return "", fmt.Errorf("%w: %s の権限が緩い (0700 でない)。dispatcher が次に起動するときに直す", ErrUnsafeDir, d)
		}
	}
	return p, nil
}

// Poke は dir の dispatcher に「起きて」と送る。居なければ誤り (呼び出し側は捨ててよい。箱のファイルはポーリングが拾う)。
func Poke(dir string) error { return send(dir, "wake") }

// Notify は dir の dispatcher を通して、購読している画面すべてへ「読み直して」と送る (画面を開いた・閉じた。ほかの画面の「画面 N」を直す)。
func Notify(dir string) error { return send(dir, "notify") }

func send(dir, cmd string) error {
	p, err := dialPath(dir)
	if err != nil {
		return err
	}
	c, err := net.DialTimeout("unix", p, ioTimeout)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	_ = c.SetDeadline(time.Now().Add(ioTimeout))
	_, err = c.Write([]byte(cmd + "\n"))
	return err
}

// Server は dispatcher の側の口。
type Server struct {
	path  string
	ln    net.Listener
	wakes chan struct{}
	mu    sync.Mutex
	subs  map[net.Conn]bool
	done  chan struct{}
}

// Listen は dir の socket を開く。🚨 dispatcher の lock (dispatcher.Lock) を取ってから呼ぶ: 前の dispatcher が残した socket の
// ファイルを消すので、lock を持たずに呼ぶと動いている dispatcher の口を奪う。socket でないファイルは消さない。
func Listen(dir string) (*Server, error) {
	p := Path(dir)
	if filepath.Dir(p) == fallbackDir() {
		if err := ensureFallbackDir(); err != nil {
			return nil, err
		}
	}
	if st, err := os.Lstat(p); err == nil {
		if st.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("%s は socket ではない (消さない)", p)
		}
		if err := os.Remove(p); err != nil {
			return nil, err
		}
	}
	ln, err := net.Listen("unix", p)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(p, 0o600); err != nil { // 自分だけが起こせる / 購読できる
		_ = ln.Close()
		return nil, err
	}
	s := &Server{path: p, ln: ln, wakes: make(chan struct{}, 1), subs: map[net.Conn]bool{}, done: make(chan struct{})}
	go s.accept()
	return s, nil
}

// Wakes は Poke を受けると値が入る (溜まった分は 1 つにまとめる。1 回回れば箱の依頼は全部適用するので)。
func (s *Server) Wakes() <-chan struct{} { return s.wakes }

func (s *Server) accept() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return // Close された
		}
		go s.handle(c)
	}
}

func (s *Server) handle(c net.Conn) {
	_ = c.SetReadDeadline(time.Now().Add(ioTimeout))
	line, err := bufio.NewReader(c).ReadString('\n')
	if err != nil {
		_ = c.Close()
		return
	}
	switch strings.TrimSpace(line) {
	case "wake":
		select {
		case s.wakes <- struct{}{}:
		default:
		}
		_ = c.Close()
	case "notify":
		_ = c.Close()
		s.Broadcast()
	case "sub":
		_ = c.SetReadDeadline(time.Time{})
		s.mu.Lock()
		select {
		case <-s.done:
			s.mu.Unlock()
			_ = c.Close()
			return
		default:
		}
		s.subs[c] = true
		s.mu.Unlock()
		_, _ = c.Read(make([]byte, 1)) // 相手が切ったら (EOF) 外す
		s.drop(c)
	default:
		_ = c.Close()
	}
}

func (s *Server) drop(c net.Conn) {
	s.mu.Lock()
	delete(s.subs, c)
	s.mu.Unlock()
	_ = c.Close()
}

// Subscribers は購読している接続の数。
func (s *Server) Subscribers() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.subs)
}

// Broadcast は購読している全員に「記録が変わった」と送る。送れなかった相手は外す (固まった画面に dispatcher を待たせない)。
func (s *Server) Broadcast() {
	s.mu.Lock()
	var conns []net.Conn
	for c := range s.subs {
		conns = append(conns, c)
	}
	s.mu.Unlock()
	for _, c := range conns {
		_ = c.SetWriteDeadline(time.Now().Add(ioTimeout))
		if _, err := c.Write([]byte("changed\n")); err != nil {
			s.drop(c)
		}
	}
}

// Close は口を閉じ、socket のファイルを消す。
func (s *Server) Close() error {
	s.mu.Lock()
	close(s.done)
	for c := range s.subs {
		_ = c.Close()
	}
	s.subs = map[net.Conn]bool{}
	s.mu.Unlock()
	err := s.ln.Close() // net の unix listener は Close でファイルを消す
	if rerr := os.Remove(s.path); rerr != nil && !errors.Is(rerr, os.ErrNotExist) && err == nil {
		err = rerr
	}
	return err
}

// Subscriber は画面の側の購読。
type Subscriber struct {
	dir      string
	onChange func()
	refused  func(error) // 逃がし先を使えないのでつながなかった (OnRefused)
	warned   bool        // refused を呼んだ (つながるまで繰り返さない)
}

// NewSubscriber は dir の dispatcher の購読を作る (Run で始める)。onChange は記録が変わったと知らされるたびに呼ぶ。
func NewSubscriber(dir string, onChange func()) *Subscriber {
	return &Subscriber{dir: dir, onChange: onChange}
}

// OnRefused は、socket の逃がし先を使えない (ErrUnsafeDir。権限が緩い・自分のものでない) のでつながなかったときに呼ぶ f をつなぐ
// (Run の前に呼ぶ)。つながるまでの間に 1 度だけ呼ぶ。つながらなくても、呼び出し側はポーリングで待てる。
func (s *Subscriber) OnRefused(f func(error)) *Subscriber {
	s.refused = f
	return s
}

// Run は購読する。切れたら retryEvery ごとに繋ぎ直す (dispatcher の起動し直し・まだ起動していない、を待つ)。ctx が終わったら戻る。
// 繋がるたびに onChange を 1 度呼ぶ。
func (s *Subscriber) Run(ctx context.Context) {
	for {
		s.once(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(retryEvery):
		}
	}
}

func (s *Subscriber) once(ctx context.Context) {
	p, err := dialPath(s.dir)
	if err != nil {
		if errors.Is(err, ErrUnsafeDir) && s.refused != nil && !s.warned {
			s.warned = true
			s.refused(err)
		}
		return
	}
	var d net.Dialer
	dctx, cancel := context.WithTimeout(ctx, ioTimeout)
	c, err := d.DialContext(dctx, "unix", p)
	cancel()
	if err != nil {
		return
	}
	stop := context.AfterFunc(ctx, func() { _ = c.Close() }) // ctx が終わったら読みを切る
	defer stop()
	defer func() { _ = c.Close() }()
	_ = c.SetWriteDeadline(time.Now().Add(ioTimeout))
	if _, err := c.Write([]byte("sub\n")); err != nil {
		return
	}
	_ = c.SetWriteDeadline(time.Time{})
	s.warned = false // また使えなくなったら、もう 1 度知らせる
	s.onChange()     // 繋がっていない間の知らせ (dispatcher の最初の Tick 等) を取り逃しているかもしれないので、繋がったら 1 度読み直させる
	r := bufio.NewReader(c)
	for {
		if _, err := r.ReadString('\n'); err != nil {
			return
		}
		s.onChange()
	}
}
