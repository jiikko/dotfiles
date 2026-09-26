// Package eventlog は dispatcher の出来事の記録 (状態の置き場の events.jsonl。issue 444)。
//
// 1 行 1 出来事の JSON (時刻・種類・カード・session・理由)。書くのは dispatcher の lock を持つプロセスだけ
// (カードの記録と同じく書き手は 1 つ = 426 の決定 1。追記と回しが書き手どうしで競らない)。読む側 (pro-con log) は読むだけ。
//
// 大きさの上限 (MaxBytes) を超えそうになったら events.jsonl を events.1.jsonl へ回し (前の events.1.jsonl は捨てる)、新しく始める。
// 置き場で使うのは最大で上限の 2 倍。
package eventlog

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// File は今書いている記録 / OldFile は回した 1 つ前の記録。
const (
	File    = "events.jsonl"
	OldFile = "events.1.jsonl"
)

// MaxBytes は 1 つのファイルの大きさの上限 (テストが縮める)。
// 1 行はおよそ 150〜400 バイトで、dispatcher が書くのは何かしたときだけ (Tick ごとではない) なので、1 MiB で数千件
// = 数日ぶんの判断が残る。2 ファイルで 2 MiB に収まり、pro-con log が毎回全部読んでも待たない。
var MaxBytes int64 = 1 << 20

// Event は 1 つの出来事。Reason は人が読む 1 文 (dispatcher のログの行と同じ文。カード ID を含むことがある)。
type Event struct {
	At      time.Time `json:"at"`
	Kind    string    `json:"kind"`
	Card    string    `json:"card,omitempty"`
	Session string    `json:"session,omitempty"`
	Reason  string    `json:"reason"`
}

// 出来事の種類 (pro-con log の絞り込みと、読む側の Claude の見出し)。
const (
	KindApply    = "apply"    // 箱の依頼を適用した
	KindReject   = "reject"   // 箱の依頼を除けた
	KindRegister = "register" // PG の session を記録に取り込んだ
	KindSuspect  = "suspect"  // 取り込まなかった (別の session・外から操作された疑い)
	KindCrash    = "crash"    // PG が落ちた (自動の再開・落ち続けたので止めた)
	KindWatchdog = "watchdog" // 停滞
	KindRun      = "run"      // テストの係の実行
	KindLaunch   = "launch"   // PG を起動・再開した / 一覧で確かめた / できなかった
	KindHold     = "hold"     // 利用枠で起動・再開を待たせた / 順番の前のカードの完了を待たせた (issue 468)
	KindOrder    = "order"    // 追加オーダーを届けるため PG を再開の列へ戻した (issue 438)
	KindBtw      = "btw"      // btw に答えた (issue 438)
	KindStop     = "stop"     // PG を止めた・止め直した・止められない (終了のとき・閉じたとき)
	KindDelete   = "delete"   // カードを削除した / 削除の依頼を受けた / 削除できない (issue 451)
	KindArchive  = "archive"  // 完了のカードを自動で片付けた (issue 478) / 完了から 1 週間で記録から消した・片付けの後に起動の記録と印を消した (issue 497) / 所要の記録の 90 日より古い行を消す (issue 516)
	KindConfig   = "config"   // 設定 (PG の枠・PM の数) を変えた (issue 456)
	KindScreens  = "screens"  // 開いている画面の数で決めたこと (画面が無いので抜ける・画面が開いたので続ける)
	KindRecover  = "recover"  // 起動時の確かめ (マシンの再起動で消えた session を待たずに復旧した / 判定できない。issue 483)
	KindError    = "error"    // 一覧を取れない・書けない等
	// KindScreen は画面の側の出来事 (開いた・quit で閉じた・止めた / 止めなかった)。画面が受付の箱に置き、dispatcher が書く
	// (時刻は画面が置いた時刻。dispatcher が居ない間に置いたものは次の dispatcher が書くので、ファイルの中で時刻の順が前後しうる)
	KindScreen = "screen"
	// KindMonitor は見張り (pro-con monitor。issue 475) の知らせ (取り込みの衝突・テストの順番の長さ) と、見張りを起こした・落ちた。
	// 知らせは見張りが受付の箱に置き、dispatcher が書く (時刻は見張りが置いた時刻)
	KindMonitor = "monitor"
	// KindSupervisor は supervisor (pro-con supervise。issue 506) の知らせ (dispatcher が落ちた・起こし直す・諦めた)。
	// supervisor が受付の箱に置き、次の dispatcher が書く (時刻は supervisor が置いた時刻)
	KindSupervisor = "supervisor"
	// KindUpgrade は dispatcher の新版への入れ替え (issue 505。新版ができた・区切りを待っている・切り替えた・切り替えられない)
	KindUpgrade = "upgrade"
)

// Append は出来事を足す (1 回の write。読む側は改行で終わった行だけを読むので、書きかけを読まない)。
func Append(dir string, evs []Event) error {
	if len(evs) == 0 {
		return nil
	}
	var buf bytes.Buffer
	for _, e := range evs {
		b, err := json.Marshal(e)
		if err != nil {
			return err
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	p := filepath.Join(dir, File)
	if st, err := os.Stat(p); err == nil && st.Size() > 0 && st.Size()+int64(buf.Len()) > MaxBytes {
		if err := os.Rename(p, filepath.Join(dir, OldFile)); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	if st.Mode().Perm() != 0o600 { // 前から在ったファイルの権限が緩くても自分だけにする
		_ = f.Chmod(0o600)
	}
	data := buf.Bytes()
	if st.Size() > 0 && !endsWithNewline(p, st.Size()) { // 前の書き手が行の途中で落ちた: その行と最初の 1 件をつなげない
		data = append([]byte{'\n'}, data...)
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func endsWithNewline(p string, size int64) bool {
	f, err := os.Open(p)
	if err != nil {
		return true
	}
	defer func() { _ = f.Close() }()
	b := make([]byte, 1)
	if _, err := f.ReadAt(b, size-1); err != nil {
		return true
	}
	return b[0] == '\n'
}

// Read は回した分と今の分を古い順に読む。
func Read(dir string) ([]Event, error) { return NewFollower(dir).Next() }

// Follower は記録を読み進める (pro-con log --follow)。回されても、回される前に書かれた残りを取りこぼさない。
// 🚨 読むだけ: ファイルを開くのは読み取りだけ。
type Follower struct {
	dir     string
	started bool
	ino     uint64 // 読んでいる events.jsonl (0 ならまだ開いていない)
	off     int64  // その中の読み終えた位置 (改行の直後)
	oldIno  uint64 // events.jsonl が無かったときに読んだ events.1.jsonl (0 なら読んでいない)
}

// NewFollower は記録の最初から読む Follower を作る。
func NewFollower(dir string) *Follower { return &Follower{dir: dir} }

// Next は前に読んだ後に足された出来事を返す。壊れた行は飛ばし、改行で終わっていない最後の行は次に回す。
func (f *Follower) Next() ([]Event, error) {
	var out []Event
	cur, err := os.Open(filepath.Join(f.dir, File))
	if errors.Is(err, os.ErrNotExist) {
		if !f.started { // まだ何も書かれていない / 回した直後で新しいファイルがまだ無い
			f.started = true
			evs, ino, err := readOld(f.dir, 0, 0)
			f.oldIno = ino
			return evs, err
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = cur.Close() }()
	ino, err := inode(cur)
	if err != nil {
		return nil, err
	}
	switch {
	case !f.started: // 最初: 回した分を全部読んでから今の分を頭から
		evs, oldIno, err := readOld(f.dir, 0, 0)
		if err != nil {
			return nil, err
		}
		if oldIno != ino { // 開いた直後に回された: 回った先は今開いているファイルと同じなので 2 度読まない
			out = evs
		}
		f.off = 0
	case f.ino != 0 && f.ino != ino: // 読んでいる間に回された: 回った先の残りを読んでから、新しいファイルを頭から
		evs, _, err := readOld(f.dir, f.ino, f.off)
		if err != nil {
			return nil, err
		}
		out = evs
		f.off = 0
	case f.ino == 0: // 前は無かったファイルが現れた。その間に回っていれば、回した分 (まだ読んでいない) から
		if ino, err := oldInode(f.dir); err == nil && ino != f.oldIno {
			evs, _, err := readOld(f.dir, 0, 0)
			if err != nil {
				return nil, err
			}
			out = evs
		}
		f.off = 0
	}
	f.started, f.ino = true, ino
	evs, n, err := readFrom(cur, f.off)
	if err != nil {
		return out, err
	}
	f.off += n
	return append(out, evs...), nil
}

// readOld は OldFile を読み、その inode を返す (無ければ 0)。wantIno が 0 でなく、回った先がそのファイルなら off から
// (違えば 2 回回った: 頭から。1 つ前の回した分は失っている)。
func readOld(dir string, wantIno uint64, off int64) ([]Event, uint64, error) {
	old, err := os.Open(filepath.Join(dir, OldFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = old.Close() }()
	ino, err := inode(old)
	if err != nil || wantIno == 0 || ino != wantIno {
		off = 0
	}
	evs, _, err := readFrom(old, off)
	return evs, ino, err
}

func oldInode(dir string) (uint64, error) {
	old, err := os.Open(filepath.Join(dir, OldFile))
	if err != nil {
		return 0, err
	}
	defer func() { _ = old.Close() }()
	return inode(old)
}

// readFrom は off から改行で終わった行を読み、出来事と読み進めたバイト数を返す。
func readFrom(r io.ReadSeeker, off int64) ([]Event, int64, error) {
	if _, err := r.Seek(off, io.SeekStart); err != nil {
		return nil, 0, err
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, 0, err
	}
	end := bytes.LastIndexByte(data, '\n') + 1
	var out []Event
	for _, line := range bytes.Split(data[:end], []byte("\n")) {
		var e Event
		if len(line) == 0 || json.Unmarshal(line, &e) != nil {
			continue
		}
		out = append(out, e)
	}
	return out, int64(end), nil
}

func inode(f *os.File) (uint64, error) {
	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, errors.New("inode を読めない")
	}
	return sys.Ino, nil
}
