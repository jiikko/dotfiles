// Package presence は開いている画面を数える (同じ状態の置き場で複数の画面を開いてよい。止めるのは最後に閉じる画面だけ)。
//
// 画面は開いている間、置き場の screens/<id>.lock を flock で持つ。画面が落ちても OS が flock を外すので、数えるときに外れている
// ものは閉じた画面として消す (後始末の仕組みを別に持たない)。dispatcher にも socket にも頼らない (dispatcher が落ちていても数えられる)。
//
// 閉じるときは quit.lock を取ってから自分の flock を外し、ほかに持たれている flock を数える。2 つの画面が同時に閉じても、
// quit.lock を後に取った方は先に閉じた方の flock がもう外れているのを見るので、ちょうど 1 つだけが 0 (最後) を受ける。
package presence

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Dir は置き場の中の画面の印の置き場。
const Dir = "screens"

const quitFile = "quit.lock"

// staleTmp は作りかけの印を落ちた画面の残りとみなすまでの時間 (作ってから rename するまでは一瞬)。
const staleTmp = time.Minute

// Screen は開いている画面 1 つの印。
type Screen struct {
	dir  string // 置き場の screens/
	path string
	f    *os.File
}

// Open は置き場 dir に、この画面が開いている印を置く。
func Open(dir string) (*Screen, error) {
	d := filepath.Join(dir, Dir)
	if err := os.MkdirAll(d, 0o700); err != nil {
		return nil, err
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, err
	}
	id := hex.EncodeToString(b[:])
	// 🚨 数える対象にならない名前で作って flock してから、印の名前へ rename する。作ってから flock すると、その間はほかの画面の
	// Count から「誰も持っていない = 落ちた画面の印」に見え、消されて数えられない画面になる (敵対的レビューで 20000 回中 146 回再現。
	// O_EXLOCK でも open の中で作成と lock の間に割り込まれて再現した)
	tmp := filepath.Join(d, ".tmp-"+id)
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return nil, err
	}
	p := filepath.Join(d, id+".lock")
	if err := os.Rename(tmp, p); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return nil, err
	}
	return &Screen{dir: d, path: p, f: f}, nil
}

// Leave はこの画面の印を外し、ほかに開いている画面の数を返す (0 なら最後の画面。呼び出し側が dispatcher と PG を止める)。
// 2 度目以降は数えるだけ。
func (s *Screen) Leave() (int, error) {
	q, err := os.OpenFile(filepath.Join(s.dir, quitFile), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return 0, err
	}
	defer func() { _ = q.Close() }()
	if err := syscall.Flock(int(q.Fd()), syscall.LOCK_EX); err != nil { // 閉じる画面どうしを順に並べる (待つのは数えるあいだだけ)
		return 0, err
	}
	defer func() { _ = syscall.Flock(int(q.Fd()), syscall.LOCK_UN) }()
	s.Close()
	return count(s.dir)
}

// Close は数えずに印を外す (Leave しないで抜ける経路。落ちたのと同じ扱い)。
func (s *Screen) Close() {
	if s.f == nil {
		return
	}
	_ = os.Remove(s.path) // 先に消す (外した flock の印を、ほかの画面の Count が一瞬「閉じた画面」として消しに来るのと競わない)
	_ = syscall.Flock(int(s.f.Fd()), syscall.LOCK_UN)
	_ = s.f.Close()
	s.f = nil
}

// Count は置き場 dir で開いている画面の数 (自分を含む)。数えられなければ誤り。
func Count(dir string) (int, error) { return count(filepath.Join(dir, Dir)) }

func count(d string) (int, error) {
	es, err := os.ReadDir(d)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range es {
		name := e.Name()
		if strings.HasPrefix(name, ".tmp-") { // 作ってから印の名前にするまでの間に落ちた画面の残り。数えない
			// 🚨 作っている最中のものに触らない (flock を先に取ると、その画面が印を置けなくなる)。古いものだけ片付ける
			if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > staleTmp {
				_, _ = heldBySomeone(filepath.Join(d, name))
			}
			continue
		}
		if !strings.HasSuffix(name, ".lock") || name == quitFile {
			continue
		}
		held, err := heldBySomeone(filepath.Join(d, name))
		if err != nil {
			return 0, err
		}
		if held {
			n++
		}
	}
	return n, nil
}

// heldBySomeone は印の flock が持たれているか。持たれていなければ (画面が落ちた) 印を消す。
// 🚨 flock は開いたファイルごと (同じプロセスでも別に開けば別) なので、自分の画面の印も「持たれている」と数える
func heldBySomeone(p string) (bool, error) {
	f, err := os.OpenFile(p, os.O_RDWR, 0)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil // 数えるあいだに閉じた
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	_ = os.Remove(p) // 誰も持っていない = 落ちた画面の印 (自分が flock を持っている間に消すので、開き直した画面とは競わない。id は毎回新しい)
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false, nil
}
