package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	seqWidth     = 12
	ticketSuffix = ".ticket"
	tmpPrefix    = ".tmp-"
	// staleTmp は作りかけの札 (.tmp-<id>) を落ちた待ち手の残りとみなすまでの時間 (作ってから rename するまでは一瞬)
	staleTmp = time.Minute
)

// ticketInfo は札の中身。待ちの表示と status が読む (順番と生死の判定には使わない)
type ticketInfo struct {
	Seq   int64     `json:"seq"`
	Pid   int       `json:"pid"`
	Start time.Time `json:"start"`
	Cwd   string    `json:"cwd"`
	Cmd   string    `json:"cmd"`
	// 子の pid と開始時刻 (ps の lstart)。持ち主の serial-run だけが SIGKILL されて子が残ったとき、その子が走っている間は
	// 札を生きている扱いにする (札の flock は持ち主の死で外れるので、flock だけでは子の生死が見えない)
	ChildPid   int    `json:"child_pid,omitempty"`
	ChildStart string `json:"child_start,omitempty"`
}

// ticketState は札の持ち主の状態
type ticketState int

const (
	stateGone    ticketState = iota // 札が無い (消えた)
	stateAlive                      // 持ち主が flock を持っている
	stateOrphan                     // 持ち主は死んだが、子がまだ走っている
	stateUnknown                    // 札を開けない (権限・fd の枯渇)。生死を決められない
)

// queue は 1 つの lock の置き場 (<状態の置き場>/<lock 名>/)。
// 🚨 置き場は消さない・作り直さない。走っている持ち主は今の run.lock の inode を flock しているので、作り直すと
// 新しく来た人が新しい run.lock を取れて同時に 2 本走り、番号も 0 に戻って札の名前が使い回される。
type queue struct{ dir string }

func (q queue) qdir() string    { return filepath.Join(q.dir, "queue") }
func (q queue) seqPath() string { return filepath.Join(q.dir, "seq") }
func (q queue) runPath() string { return filepath.Join(q.dir, "run.lock") }
func (q queue) ticketPath(seq int64) string {
	return filepath.Join(q.qdir(), fmt.Sprintf("%0*d%s", seqWidth, seq, ticketSuffix))
}

func openQueue(dir string) (queue, error) {
	q := queue{dir: dir}
	if err := os.MkdirAll(q.qdir(), 0o700); err != nil {
		return q, err
	}
	return q, nil
}

// flock は EINTR で取り直す (待っている間に Notify したシグナルが届くと、blocking の flock は EINTR で返る)
func flock(f *os.File, how int) error {
	for {
		err := syscall.Flock(int(f.Fd()), how)
		if !errors.Is(err, syscall.EINTR) {
			return err
		}
	}
}

// enqueue は番号を取って札を出し、flock を持った札を返す。
// 🚨 番号の採番から札の rename までを seq の flock の中で行う。採番の後に flock を離すと、小さい番号を取った人の札が
// まだ無い間に、大きい番号の人が先に列挙して先頭だと読み、順番を抜かす。
func (q queue) enqueue(info ticketInfo) (*os.File, int64, error) {
	sf, err := os.OpenFile(q.seqPath(), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, 0, err
	}
	defer sf.Close()
	if err := flock(sf, syscall.LOCK_EX); err != nil {
		return nil, 0, fmt.Errorf("seq の flock: %w", err)
	}
	next := q.lastSeq(sf) + 1
	// 🚨 固定長で同じ位置に上書きする。truncate すると途中で殺されたときに空になり、tmp に書いて rename すると
	// flock が古い inode に残って、別のプロセスが新しい inode で同じ番号を取る
	if _, err := sf.WriteAt([]byte(fmt.Sprintf("%0*d\n", seqWidth, next)), 0); err != nil {
		return nil, 0, fmt.Errorf("seq の書き込み: %w", err)
	}
	info.Seq = next
	f, err := q.createTicket(next, info)
	if err != nil {
		return nil, 0, err
	}
	return f, next, nil
}

// lastSeq は最後に配った番号を返す。seq が読めない (壊れている・外から巻き戻された) ときも、
// 残っている札より小さい番号は返さない (生きている札と番号が重ならない)
func (q queue) lastSeq(sf *os.File) int64 {
	var n int64
	b := make([]byte, seqWidth+1)
	if k, _ := sf.ReadAt(b, 0); k == seqWidth+1 && b[seqWidth] == '\n' {
		if v, err := strconv.ParseInt(string(b[:seqWidth]), 10, 64); err == nil && v >= 0 {
			n = v
		}
	}
	if m := q.maxTicket(); m > n {
		n = m
	}
	return n
}

func (q queue) maxTicket() int64 {
	seqs, _ := q.tickets()
	if len(seqs) == 0 {
		return 0
	}
	return seqs[len(seqs)-1]
}

// createTicket は flock (LOCK_EX) を持った札 <seq>.ticket を置く。
// 🚨 数える対象にならない名前 (.tmp-<id>) で作って flock してから、札の名前へ rename する。札の名前で作ってから flock すると、
// その間は他の待ち手に「持ち主のいない札」に見え、消される (src/pro-con/flockmark の Create と同じ理由。pro-con の module の
// 中なので import しない)
func (q queue) createTicket(seq int64, info ticketInfo) (*os.File, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	tmp := filepath.Join(q.qdir(), tmpPrefix+hex.EncodeToString(b))
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*os.File, error) {
		_ = os.Remove(tmp)
		_ = f.Close()
		return nil, err
	}
	if err := flock(f, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fail(fmt.Errorf("札の flock: %w", err))
	}
	body, err := json.Marshal(info)
	if err != nil {
		return fail(err)
	}
	if _, err := f.Write(append(body, '\n')); err != nil {
		return fail(err)
	}
	if p := os.Getenv("SERIAL_RUN_TEST_PAUSE_BEFORE_RENAME"); p != "" {
		// テストの差し込み口: 札を公開する直前で止まる (この間に来た人が順番を抜かさないことを見る)。fifo に書かれるまで待つ
		if g, err := os.Open(p); err == nil {
			_, _ = g.Read(make([]byte, 1))
			_ = g.Close()
		}
	}
	if err := os.Rename(tmp, q.ticketPath(seq)); err != nil {
		return fail(err)
	}
	return f, nil
}

// tickets は札の番号を小さい順に返す (生死は見ない)
func (q queue) tickets() ([]int64, error) {
	ents, err := os.ReadDir(q.qdir())
	if err != nil {
		return nil, err
	}
	var seqs []int64
	for _, e := range ents {
		name := e.Name()
		if !strings.HasSuffix(name, ticketSuffix) {
			continue
		}
		v, err := strconv.ParseInt(strings.TrimSuffix(name, ticketSuffix), 10, 64)
		if err != nil {
			continue
		}
		seqs = append(seqs, v)
	}
	sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })
	return seqs, nil
}

// sweepStaleTmp は作りかけの札の残り (持ち主が rename の前に死んだもの) を消す
func (q queue) sweepStaleTmp() {
	ents, err := os.ReadDir(q.qdir())
	if err != nil {
		return
	}
	for _, e := range ents {
		if !strings.HasPrefix(e.Name(), tmpPrefix) {
			continue
		}
		fi, err := e.Info()
		if err != nil || time.Since(fi.ModTime()) < staleTmp {
			continue
		}
		p := filepath.Join(q.qdir(), e.Name())
		if dead, f := probe(p); dead {
			_ = os.Remove(p)
			_ = f.Close()
		}
	}
}

// probe は札の持ち主が死んでいるかを見る。死んでいれば、flock を持ったままの fd を返す (呼び出し側が消してから閉じる)。
// 持ち主の生死は flock だけで決め、pid は見ない (pid は使い回される。flock はプロセスが死ねばカーネルが外す)。
func probe(path string) (bool, *os.File) {
	f, err := os.Open(path)
	if err != nil {
		return false, nil // 無い = もう消えている (死んだ札ではない)
	}
	if err := flock(f, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return false, nil
	}
	// flock を取るまでの間に別の待ち手が消していたら (unlink 済みの inode を掴んだ)、死んだ札としては扱わない
	var a, b syscall.Stat_t
	if syscall.Fstat(int(f.Fd()), &a) != nil || syscall.Stat(path, &b) != nil || a.Ino != b.Ino || a.Dev != b.Dev {
		_ = f.Close()
		return false, nil
	}
	return true, f
}

// stateOf は札の状態を返す。持ち主も子も死んでいれば札を消して stateGone を返す
// (flock を持ったまま消すので、同時に見た他の待ち手と二重には消さない)
func stateOf(path string) ticketState {
	dead, f := probe(path)
	if !dead {
		// probe は開けない札も「死んでいない」と答えるので、開けるかで生きている・無い・判定不能を見分ける
		g, err := os.Open(path)
		switch {
		case err == nil:
			_ = g.Close()
			return stateAlive
		case os.IsNotExist(err):
			return stateGone
		default:
			return stateUnknown
		}
	}
	defer f.Close()
	var info ticketInfo
	if b, err := os.ReadFile(path); err == nil && json.Unmarshal(b, &info) == nil && info.ChildPid > 0 &&
		info.ChildStart != "" && procStart(info.ChildPid) == info.ChildStart {
		return stateOrphan
	}
	_ = os.Remove(path)
	return stateGone
}

// pred は前に並んでいる札 1 枚
type pred struct {
	seq   int64
	state ticketState
}

// predecessors は自分 (mine) より前の、消えていない札を小さい順に返す。死んだ札は消す
func (q queue) predecessors(mine int64) []pred {
	seqs, _ := q.tickets()
	var out []pred
	for _, s := range seqs {
		if s >= mine {
			break
		}
		if st := stateOf(q.ticketPath(s)); st != stateGone {
			out = append(out, pred{seq: s, state: st})
		}
	}
	return out
}

// rewriteInfo は持っている札の中身を書き直す (子の pid を足す)。読む側が途中を読んでも JSON として読めないだけで、
// そのときは子の情報が無い札として扱われる
func rewriteInfo(f *os.File, info ticketInfo) {
	body, err := json.Marshal(info)
	if err != nil {
		return
	}
	body = append(body, '\n')
	if _, err := f.WriteAt(body, 0); err == nil {
		_ = f.Truncate(int64(len(body)))
	}
}

// procStart は pid の開始時刻 (ps の lstart) を返す。居なければ空。pid の使い回しと見分けるために使う
func procStart(pid int) string {
	out, err := exec.Command("/bin/ps", "-o", "lstart=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (q queue) readInfo(seq int64) (ticketInfo, bool) {
	var info ticketInfo
	b, err := os.ReadFile(q.ticketPath(seq))
	if err != nil || json.Unmarshal(b, &info) != nil {
		return info, false
	}
	return info, true
}

// blockUntilReleased は path の札に blocking の flock をかけて眠り、持ち主が lock を外したら woke を閉じる
// (取った lock はすぐ放す。死んだ札を消すのは起きた側が列挙し直すときに行う)。
// blocking の flock は止められないので goroutine で待つ。待ち手が抜けるときはプロセスごと終わり、lock はカーネルが外す
// 🚨 札を開けないときは、札が無いときだけ起こす。権限・fd の枯渇で開けない札に即座に起こし返すと、列挙し直して
// また同じ札で眠ろうとする空回り (CPU 100%) になる。起こさなければ、呼び出し側の短い見直しが拾う
func blockUntilReleased(path string, woke chan<- struct{}) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			close(woke)
		}
		return
	}
	_ = flock(f, syscall.LOCK_EX)
	_ = f.Close()
	close(woke)
}

// blockLock は f に blocking の flock をかけ、取れたら結果を返す
func blockLock(f *os.File, got chan<- error) {
	got <- flock(f, syscall.LOCK_EX)
}
