// Package presence は開いている画面を数える (同じ状態の置き場で複数の画面を開いてよい。止めるのは最後に閉じる画面だけ)。
//
// 画面は開いている間、置き場の screens/<id>.lock を flock で持つ。画面が落ちても OS が flock を外すので、数えるときに外れている
// ものは閉じた画面として消す (後始末の仕組みを別に持たない)。dispatcher にも socket にも頼らない (dispatcher が落ちていても数えられる)。
//
// 閉じるときは quit.lock を取ってから自分の flock を外し、ほかに持たれている flock を数える。2 つの画面が同時に閉じても、
// quit.lock を後に取った方は先に閉じた方の flock がもう外れているのを見るので、ちょうど 1 つだけが 0 (最後) を受ける。
//
// 画面には持ち主 (普通の画面) と join (pro-con --join。加わるだけ) がある (issue 481)。印のファイルの中身にモードなどを書き
// (Info)、数えるときに分ける。🚨 生きているかの正本は flock (中身が古くても・壊れていても、flock が持たれていれば開いている)。
// 中身を読めない印 (前の版の画面は空のファイルを置く) は持ち主として数える。
package presence

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

// Dir は置き場の中の画面の印の置き場。
const Dir = "screens"

const quitFile = "quit.lock"

// staleTmp は作りかけの印を落ちた画面の残りとみなすまでの時間 (作ってから rename するまでは一瞬)。
const staleTmp = time.Minute

// Mode は画面のモード。
type Mode string

const (
	Owner Mode = "owner" // 持ち主 (普通の画面)。dispatcher を起こし、最後に閉じる持ち主が dispatcher と PG を止める
	Join  Mode = "join"  // 加わった画面 (pro-con --join)。読み書きするが、dispatcher を起こさず、閉じても止めない
)

// Label はモードの表示名。
func (m Mode) Label() string {
	if m == Join {
		return "join"
	}
	return "持ち主"
}

// Info は開いている画面 1 つの見分け (印のファイルの中身)。
type Info struct {
	ID     string    `json:"id"` // 画面 ID (印の名前。表示は Short)
	Mode   Mode      `json:"mode"`
	Label  string    `json:"label,omitempty"` // --as <ラベル> (任意)
	TTY    string    `json:"tty,omitempty"`   // 端末 (取れなければ空)
	PID    int       `json:"pid"`
	Opened time.Time `json:"opened"`
}

// Short は画面 ID の短い形 (画面の一覧・依頼の履歴に出す)。
func (i Info) Short() string { return ShortID(i.ID) }

// ShortID は画面 ID の短い形。
func ShortID(id string) string {
	if len(id) > 6 {
		return id[:6]
	}
	return id
}

// Name は依頼の履歴・出来事に残す画面の名前 (「a1b2c3 join review」)。
func (i Info) Name() string {
	n := i.Short() + " " + i.Mode.Label()
	if i.Label != "" {
		n += " " + i.Label
	}
	return n
}

// Tally は開いている画面の数をモードごとに数えたもの。
type Tally struct{ Owners, Joins int }

// Total は持ち主と join を合わせた数。
func (t Tally) Total() int { return t.Owners + t.Joins }

// Screen は開いている画面 1 つの印。
type Screen struct {
	dir  string // 置き場の screens/
	path string
	f    *os.File
	info Info
}

// Info はこの画面の見分け。
func (s *Screen) Info() Info { return s.info }

// Open は置き場 dir に、持ち主の画面が開いている印を置く。
func Open(dir string) (*Screen, error) { return OpenAs(dir, Info{Mode: Owner}) }

// OpenAs は置き場 dir に、この画面が開いている印を置く (ID・pid・開いた時刻は OpenAs が埋める)。
func OpenAs(dir string, info Info) (*Screen, error) {
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
	if info.Mode == "" {
		info.Mode = Owner
	}
	info.ID, info.PID, info.Opened = id, os.Getpid(), time.Now()
	// 中身は印の名前にする前に書く (数える側には書き終えた印しか見えない)
	data, err := json.Marshal(info)
	if err == nil {
		_, err = f.Write(data)
	}
	if err != nil {
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
	return &Screen{dir: d, path: p, f: f, info: info}, nil
}

// Leave はこの画面の印を外し、ほかに開いている画面の数をモードごとに返す (Owners が 0 なら最後の持ち主。呼び出し側が
// dispatcher と PG を止める)。数え直しは quit.lock の中で行う (同時に閉じた持ち主のうち、ちょうど 1 つだけが 0 を受ける)。
// 2 度目以降は数えるだけ。
func (s *Screen) Leave() (Tally, error) {
	q, err := os.OpenFile(filepath.Join(s.dir, quitFile), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return Tally{}, err
	}
	defer func() { _ = q.Close() }()
	if err := syscall.Flock(int(q.Fd()), syscall.LOCK_EX); err != nil { // 閉じる画面どうしを順に並べる (待つのは数えるあいだだけ)
		return Tally{}, err
	}
	defer func() { _ = syscall.Flock(int(q.Fd()), syscall.LOCK_UN) }()
	s.Close()
	ss, err := scan(s.dir)
	return tally(ss), err
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

// Count は置き場 dir で開いている画面の数 (持ち主と join。自分を含む)。数えられなければ誤り。
func Count(dir string) (int, error) {
	ss, err := List(dir)
	return len(ss), err
}

// Owners は置き場 dir で開いている持ち主の画面の数 (自分を含む)。数えられなければ誤り。
func Owners(dir string) (int, error) {
	ss, err := List(dir)
	return tally(ss).Owners, err
}

// List は置き場 dir で開いている画面 (開いた順)。数えられなければ誤り。
func List(dir string) ([]Info, error) { return scan(filepath.Join(dir, Dir)) }

func tally(ss []Info) Tally {
	var t Tally
	for _, i := range ss {
		if i.Mode == Join {
			t.Joins++
		} else {
			t.Owners++
		}
	}
	return t
}

func scan(d string) ([]Info, error) {
	es, err := os.ReadDir(d)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Info
	for _, e := range es {
		name := e.Name()
		if strings.HasPrefix(name, ".tmp-") { // 作ってから印の名前にするまでの間に落ちた画面の残り。数えない
			// 🚨 作っている最中のものに触らない (flock を先に取ると、その画面が印を置けなくなる)。古いものだけ片付ける
			if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > staleTmp {
				_, _, _ = heldBySomeone(filepath.Join(d, name))
			}
			continue
		}
		if !strings.HasSuffix(name, ".lock") || name == quitFile {
			continue
		}
		held, data, err := heldBySomeone(filepath.Join(d, name))
		if err != nil {
			return nil, err
		}
		if held {
			out = append(out, readInfo(data, strings.TrimSuffix(name, ".lock")))
		}
	}
	slices.SortStableFunc(out, func(a, b Info) int { return a.Opened.Compare(b.Opened) })
	return out, nil
}

// readInfo は生きている印の中身。読めなければ (前の版の空の印・壊れた中身) 持ち主として扱う
// (前の版の画面は持ち主。join を持ち主と数え違えると、最後の持ち主の quit で止めずに閉じるが、PG は画面が全部閉じてから
// dispatcher の --exit-without-screens が止める)。
func readInfo(data []byte, id string) Info {
	var i Info
	if json.Unmarshal(data, &i) != nil {
		i = Info{}
	}
	if i.Mode != Join {
		i.Mode = Owner
	}
	i.ID = id // 名前が正本
	return i
}

// heldBySomeone は印の flock が持たれているか。持たれていれば印の中身 (flock を確かめたのと同じ fd から読む) も返す。
// 持たれていなければ (画面が落ちた) 印を消す。
// 🚨 flock は開いたファイルごと (同じプロセスでも別に開けば別) なので、自分の画面の印も「持たれている」と数える
// 🚨 中身をパスで開き直して読まない: 確かめた直後に相手が閉じて印を消すと読めず、閉じた join を持ち主と数え違える
func heldBySomeone(p string) (bool, []byte, error) {
	f, err := os.OpenFile(p, os.O_RDWR, 0)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil, nil // 数えるあいだに閉じた
	}
	if err != nil {
		return false, nil, err
	}
	defer func() { _ = f.Close() }()
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		data, _ := io.ReadAll(f) // 読めなければ空 = 持ち主として数える (readInfo)
		return true, data, nil
	}
	if err != nil {
		return false, nil, err
	}
	_ = os.Remove(p) // 誰も持っていない = 落ちた画面の印 (自分が flock を持っている間に消すので、開き直した画面とは競わない。id は毎回新しい)
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false, nil, nil
}
