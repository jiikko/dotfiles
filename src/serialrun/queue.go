package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"proctree"
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
	// seqRetry は seq の flock を取り直す刻み (持っているのは札を 1 枚出す間だけなので、普段は 1 回で取れる)
	seqRetry = 20 * time.Millisecond
)

// ticketInfo は札の中身。待ちの表示と status が読む (順番と生死の判定には使わない)
type ticketInfo struct {
	Pid   int       `json:"pid"`
	Start time.Time `json:"start"` // 札を出した時刻 (待ち始め)
	Cwd   string    `json:"cwd"`
	Cmd   string    `json:"cmd"`
	// RunStart は子を起こした時刻 (未実行の札では零)。「長く持っている」の判定はこちらで見る (待って先頭に上がった札を、走り出した直後から 🚨 にしない)
	RunStart time.Time `json:"run_start,omitzero"`
	// 子の pid と開始時刻 (ps の lstart)。持ち主の serial-run だけが SIGKILL されて子が残ったとき、その子が走っている間は
	// 札を生きている扱いにする (札の flock は持ち主の死で外れるので、flock だけでは子の生死が見えない)
	ChildPid   int    `json:"child_pid,omitempty"`
	ChildStart string `json:"child_start,omitempty"`
	// ChildStartUTC は子の開始時刻を LC_ALL=C TZ=UTC で取ったもの (今の版はこちらで比べる)。
	// 🚨 child_start には持ち主の環境のままの値を書き続ける。版が混ざる間 (pull で作り直された直後に古い版がまだ待っている)、古い版は
	// child_start を自分の環境の ps と文字列で比べるので、C/UTC の値を書くと別人と読んで札を消し、2 本目を走らせる
	ChildStartUTC string `json:"child_start_utc,omitempty"`
	// Nonce は起動ごとの乱数。入れ子の素通し (SERIAL_RUN_HELD) が番号だけで照合すると、置き場が作り直されて番号が使い回されたとき、
	// 古い値を持つプロセスが別人の札を自分の祖先と読む
	Nonce string `json:"nonce,omitempty"`
	// Pgid は持ち主のプロセスグループ (子も同じグループで起こす)。開始時刻で子を確かめられない札で、使い回された pid の無関係な
	// プロセスを子と取り違えて止めないために見る
	Pgid int `json:"pgid,omitempty"`
	// V は札の書式の版 (今は ticketVersion)。読む側の判定には使っていない (子の開始時刻は ChildStartUTC の有無で見る)。書式を変えるときの目印
	V int `json:"v,omitempty"`
}

// ticketVersion は今の札の書式の版 (3: seq を外し run_start を足した。2: ChildStartUTC と Pgid を持つ)
const ticketVersion = 3

// unverifiedOrphanTTL は、子の同一性を開始時刻で確かめられない孤児の札を生かしておく上限。確かめられない札の pid を、
// 子が死んだ後に無関係な長命のプロセスが使い回すと、札は消えず、後ろは毎回待ちの上限まで待たされる
const unverifiedOrphanTTL = 6 * time.Hour

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

func (q queue) qdir() string        { return filepath.Join(q.dir, "queue") }
func (q queue) seqPath() string     { return filepath.Join(q.dir, "seq") }
func (q queue) runPath() string     { return filepath.Join(q.dir, "run.lock") }
func (q queue) historyPath() string { return filepath.Join(q.dir, "history.log") }

// hookLogPath は hook の判断の記録 (lock ごとではなく状態の置き場に 1 つ。hook はどの列に並ぶかを決めずに書く)
func hookLogPath(base string) string { return filepath.Join(base, "hook.log") }
func (q queue) ticketPath(seq int64) string {
	return filepath.Join(q.qdir(), fmt.Sprintf("%0*d%s", seqWidth, seq, ticketSuffix))
}

// openQueue は置き場を作り、symlink を解決した実パスを持つ queue を返す。
// 同じ置き場を別の綴り (symlink 越し) で指した serial-run どうしが、入れ子の照合 (SERIAL_RUN_HELD) で食い違わないようにする
func openQueue(dir string) (queue, error) {
	if err := os.MkdirAll(filepath.Join(dir, "queue"), 0o700); err != nil {
		return queue{dir: dir}, err
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return queue{dir: dir}, err
	}
	return queue{dir: real}, nil
}

// errNoQueue はその lock の置き場がまだ無い (一度も使われていない) こと
var errNoQueue = errors.New("置き場が無い")

// openExistingQueue は置き場を作らずに開く (status など見るだけの入口。typo の lock 名で空の置き場を作らない)。無ければ errNoQueue
func openExistingQueue(dir string) (queue, error) {
	real, err := filepath.EvalSymlinks(dir)
	if err == nil {
		_, err = os.Stat(filepath.Join(real, "queue"))
	}
	if os.IsNotExist(err) {
		return queue{dir: dir}, errNoQueue
	}
	if err != nil {
		return queue{dir: dir}, err
	}
	return queue{dir: real}, nil
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
// seq の flock は LOCK_NB の再試行で取る。blocking で待つと、seq を持った側が止まった (SIGSTOP・ファイルシステムの停止) とき、
// 全 session の serial-run がシグナルでも待ちの上限でも抜けられなくなる。abort が誤りを返したら諦める
func (q queue) enqueue(info ticketInfo, abort func() error) (*ticket, error) {
	sf, err := os.OpenFile(q.seqPath(), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	defer sf.Close()
	for {
		err := flock(sf, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("seq の flock: %w", err)
		}
		if aerr := abort(); aerr != nil {
			return nil, aerr
		}
		noteSeqRetry()
		time.Sleep(seqRetry)
	}
	last, err := q.lastSeq(sf)
	if err != nil {
		return nil, err
	}
	next := last + 1
	// 🚨 固定長で同じ位置に上書きする。truncate すると途中で殺されたときに空になり、tmp に書いて rename すると
	// flock が古い inode に残って、別のプロセスが新しい inode で同じ番号を取る
	if _, err := sf.WriteAt([]byte(fmt.Sprintf("%0*d\n", seqWidth, next)), 0); err != nil {
		return nil, fmt.Errorf("seq の書き込み: %w", err)
	}
	f, err := q.createTicket(next, info)
	if err != nil {
		return nil, err
	}
	return &ticket{f: f, seq: next, path: q.ticketPath(next), info: info}, nil
}

// lastSeq は最後に配った番号を返す。seq が読めない (壊れている・外から巻き戻された) ときも、
// 残っている札より小さい番号は返さない (生きている札と番号が重ならない)。
// seq も札の一覧も読めないときは誤りを返す (0 から振り直すと、生きている札と番号が重なりうる)
func (q queue) lastSeq(sf *os.File) (int64, error) {
	var n int64
	seqOK := false
	b := make([]byte, seqWidth+1)
	if k, _ := sf.ReadAt(b, 0); k == seqWidth+1 && b[seqWidth] == '\n' {
		if v, err := strconv.ParseInt(string(b[:seqWidth]), 10, 64); err == nil && v >= 0 {
			n, seqOK = v, true
		}
	}
	seqs, err := q.tickets()
	if err != nil {
		if !seqOK {
			return 0, fmt.Errorf("seq も札の一覧も読めない: %w", err)
		}
		return n, nil
	}
	if len(seqs) > 0 && seqs[len(seqs)-1] > n {
		n = seqs[len(seqs)-1]
	}
	return n, nil
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
	pauseBeforeRename()
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
// (flock を持ったまま消すので、同時に見た他の待ち手と二重には消さない)。止まったままの孤児の子は回収する (reclaimStopped)
func stateOf(path string) ticketState { return stateOfMode(path, true) }

// stateOfMode は reap=false なら札を消さず、孤児の子も止めずに状態だけを返す (lineup: status・hook の案内・入れ子の保険 heldByAncestorProcess。見るだけの入口が他人のプロセスに手を出さない)
func stateOfMode(path string, reap bool) ticketState {
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
	if info, ok := readInfoAt(path); ok && info.ChildPid > 0 {
		if st := orphanChild(info, reap); st != stateGone {
			return st
		}
	}
	if reap {
		_ = os.Remove(path)
	}
	return stateGone
}

// orphanChild は持ち主が死んだ札の子の状態を返す (stateOrphan: 生きている / stateUnknown: 判定不能 / stateGone: もう居ない)。
// 子の同一性は開始時刻で確かめる。開始時刻を書けなかった札 (pid を書いた直後に持ち主が死んだ・ps が失敗した) は、
// その pid が札を出した後に起きたプロセスなら子とみなす。札より前から居たなら pid の使い回しで、別人を待ち続けない
func orphanChild(info ticketInfo, reap bool) ticketState {
	start, known := procStart(info.ChildPid)
	if !known {
		return stateUnknown // ps が使えない: 子の生死を決められない。消すと走っている子の後ろで 2 本目が走りうる
	}
	if start == "" {
		return stateGone
	}
	// 開始時刻は時刻として比べる (ChildStartUTC)。それを持たない札 (古い版・開始時刻を書く前に持ち主が死んだ・ps が失敗した) は、
	// 「札を出した後に起きたか」だけを見る。これは pid の使い回しを見分けない (使い回したプロセスも札の後に起きる) ので、
	// 確かめられない札には寿命 (unverifiedOrphanTTL) を置く。古い版の札は Pgid も無いので回収はしない (停止した子は残り、後ろは待ちの上限で抜ける)
	verified := false
	if rec, ok := parseLstart(info.ChildStartUTC); ok {
		now, ok2 := parseLstart(start)
		if !ok2 || !now.Equal(rec) {
			return stateGone
		}
		verified = true
	} else if !startedAfter(start, info.Start) || time.Since(info.Start) > unverifiedOrphanTTL {
		return stateGone
	}
	// 止めてよいのは子だと確かめられたときだけ: 開始時刻が一致したか、開始時刻が無くても持ち主と同じプロセスグループに居るか。
	// 確かめられなければ止めずに待つ (使い回された pid の、停止中の無関係なプロセスを止めない)
	if !verified && (info.Pgid <= 0 || !samePgid(info.ChildPid, info.Pgid)) {
		return stateOrphan
	}
	if reap && reclaimStopped(info.ChildPid) {
		return stateGone
	}
	return stateOrphan
}

func samePgid(pid, pgid int) bool {
	s, known := psField(pid, "pgid")
	return known && s == strconv.Itoa(pgid)
}

// reclaimStopped は、持ち主が死んだ札の子が停止 (stat T) したままなら、子孫ごと止めて true を返す。
// 持ち主の serial-run が子孫を止める途中 (凍らせた後・TERM の前) で SIGKILL されると、子の木は停止のまま残り、
// 誰も起こさないので後ろの全員が待ちの上限まで待つ。呼ぶのは子の同一性を確かめた後だけ (orphanChild)。
// 🚨 proctree の Stop の「凍らせてから集める」順番は変えない (先に TERM を送ると、親が死んで孫が init へ移り取りこぼす)
func reclaimStopped(pid int) bool {
	st, known := procStat(pid)
	if !known || !strings.HasPrefix(st, "T") {
		return false
	}
	// 入れ子 (その子の子孫) から呼ばれたときは、自分の祖先を止めることになる。回収しない
	if ancestors()[pid] {
		return false
	}
	proctree.Target{Root: pid, Group: false}.Stop(time.Second)
	return true
}

// pred は前に並んでいる札 1 枚
type pred struct {
	seq   int64
	state ticketState
}

// predecessors は自分 (mine) より前の、消えていない札を小さい順に返す。死んだ札は消す。
// 札の一覧を読めなければ誤りを返す (「札が 0 枚」と読むと、順番を飛ばして run.lock の奪い合いに入る)
func (q queue) predecessors(mine int64) ([]pred, error) { return q.scan(mine, true) }

func (q queue) scan(mine int64, reap bool) ([]pred, error) {
	seqs, err := q.tickets()
	if err != nil {
		return nil, err
	}
	var out []pred
	for _, s := range seqs {
		if s >= mine {
			break
		}
		if st := stateOfMode(q.ticketPath(s), reap); st != stateGone {
			out = append(out, pred{seq: s, state: st})
		}
	}
	return out, nil
}

// rewriteInfo は持っている札の中身を書き直す (子の pid を足す)。読む側が途中を読んでも JSON として読めないだけで、
// そのときは子の情報が無い札として扱われる。書き直す本文は子の情報の分だけ元より長いので、WriteAt が全体を上書きする
func rewriteInfo(f *os.File, info ticketInfo) error {
	body, err := json.Marshal(info)
	if err != nil {
		return err
	}
	body = append(body, '\n')
	if _, err := f.WriteAt(body, 0); err != nil {
		return err
	}
	return f.Truncate(int64(len(body)))
}

func (q queue) readInfo(seq int64) (ticketInfo, bool) { return readInfoAt(q.ticketPath(seq)) }

// readInfoAt は札の中身を読む。書き換えの途中 (rewriteInfo) を読むと JSON として読めず false になる
func readInfoAt(path string) (ticketInfo, bool) {
	var info ticketInfo
	b, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(b, &info) != nil {
		return info, false
	}
	return info, true
}

// lineup は列に並んでいる札 (走っているもの・待っているもの) を小さい順に返す。見るだけで、死んだ札を消さず、孤児の子も止めない
// (回収は待ち手の predecessors が行う)。置き場が無ければ作らずに空を返す。
// status・hook の案内・入れ子の保険 (heldByAncestorProcess) は同じこの入口で数える (孤児・開けない札を「無い」と出さない)
func (q queue) lineup() ([]pred, error) {
	if _, err := os.Stat(q.qdir()); os.IsNotExist(err) {
		return nil, nil
	}
	return q.scan(math.MaxInt64, false)
}

// ticket は自分が出した札 1 枚 (flock を持った fd と中身)。札の寿命 (子の記録・後始末) はこの型の上で行う
type ticket struct {
	f    *os.File
	seq  int64
	path string
	info ticketInfo
}

// setChild は子を起こした時刻と pid を札に書き、続けて子の開始時刻を書く。
// 🚨 pid を先に書く。ps (開始時刻) を待ってから書くと、その間に serial-run だけが SIGKILL されたとき、子の情報が無い札として消され、
// 走り続ける子の後ろで次の 1 本が走る
func (t *ticket) setChild(pid int, runStart time.Time) error {
	t.info.ChildPid, t.info.RunStart = pid, runStart
	if err := rewriteInfo(t.f, t.info); err != nil {
		return err
	}
	// ps は負荷で時間切れになりうるので数回試す。取れなくても子は走らせる (開始時刻の無い札は、札より後に起きた pid かで同一性を見る)
	for range 3 {
		if start, known := procStart(pid); known && start != "" {
			t.info.ChildStartUTC = start
			// 古い版との互換 (ChildStartUTC の注記)。取れなくても今の版は困らない
			t.info.ChildStart, _ = psFieldEnv(pid, "lstart", false)
			return rewriteInfo(t.f, t.info)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("ps で子の開始時刻を取れない")
}

// release は札を消して閉じ、最後に run.lock (持っていれば) を放す。
// 🚨 札を消してから run.lock を放す。後ろの待ち手は札の flock で起きて run.lock を取りに来るので、逆にすると
// 札が残ったまま run.lock が空き、起きた待ち手がまだ前に札があると読んで眠り直す (遅れるだけで壊れはしない)
func (t *ticket) release(runLock *os.File) {
	_ = os.Remove(t.path)
	_ = t.f.Close()
	if runLock != nil {
		_ = runLock.Close()
	}
}

// historyEntry は history.log の 1 行。待っただけで走らなかった run (上限・シグナル・失敗) も 1 行残す (計測の母集合を偏らせない)
type historyEntry struct {
	seq     int64 // 札を出せなかったら 0
	enq     time.Time
	waited  time.Duration
	ran     time.Duration // 走らなかったら負
	rc      int           // 分からなければ負 (unqueued: 順番を待たずに exec したので結果を見ない)
	outcome string        // ran / timeout / signal / error / unqueued
	cwd     string
	cmd     string // displayCmd を通したもの (改行・タブ・制御文字を含まない)
}

func (q queue) appendHistory(e historyEntry) {
	f, err := os.OpenFile(q.historyPath(), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	seq, ran, rc := "-", "-", "-"
	if e.seq > 0 {
		seq = strconv.FormatInt(e.seq, 10)
	}
	if e.ran >= 0 {
		ran = fmt.Sprintf("%.1fs", e.ran.Seconds())
	}
	if e.rc >= 0 {
		rc = strconv.Itoa(e.rc)
	}
	// 結末は末尾に足す (古い版の 7 列の行と同じファイルに混ざっても、cwd・コマンドの列の位置が変わらない。コマンドは空白を 1 つに畳むのでタブを含まない)。
	// O_APPEND の 1 回の write で書く (並行する書き手と行が混ざらない)
	_, _ = fmt.Fprintf(f, "%s\t%s\twait=%.1fs\tran=%s\trc=%s\t%s\t%s\t%s\n",
		seq, e.enq.Format(time.RFC3339), e.waited.Seconds(), ran, rc, printable(e.cwd), e.cmd, e.outcome)
}

// blockUntilReleased は path の札に blocking の flock をかけて眠り、持ち主が lock を外したら woke を閉じる
// (取った lock はすぐ放す。死んだ札を消すのは起きた側が列挙し直すときに行う)。
// blocking の flock は止められないので goroutine で待つ。待ち手が抜けるときはプロセスごと終わり、lock はカーネルが外す
// 🚨 札を開けない・flock が失敗したときは woke で起こさず failed を閉じる。即座に起こし返すと、列挙し直して
// また同じ札で眠ろうとする空回り (CPU 100%) になる。呼び出し側は failed を見て、短い間隔の見直しに切り替える
func blockUntilReleased(path string, woke, failed chan<- struct{}) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			close(woke)
		} else {
			close(failed)
		}
		return
	}
	defer f.Close()
	if err := flock(f, syscall.LOCK_EX); err != nil {
		close(failed)
		return
	}
	close(woke)
}

// blockLock は f に blocking の flock をかけ、取れたら結果を返す
func blockLock(f *os.File, got chan<- error) {
	got <- flock(f, syscall.LOCK_EX)
}
