package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// レイアウト。issue 091「ロックの実体」と一致させること。
//
//	<dir>/.lockman/
//	├── lock              存在 = ロック中。中身は取得時に 1 度だけ書く
//	├── tmp/<token>.json  書きかけの置き場
//	├── probe/<token>     サーバ時刻を得る使い捨て
//	└── graveyard/<token> 引き継ぎ・break で退けた旧 lock
const (
	metaDirName      = ".lockman"
	lockName         = "lock"
	tmpDirName       = "tmp"
	probeDirName     = "probe"
	graveyardDirName = "graveyard"

	// 共有前提のモード。sticky を付けないこと: rename の可否は親ディレクトリの
	// 権限で決まるため、+t が付くと他ユーザーの lock を graveyard へ退けられず、
	// TTL が切れても永久に引き継げなくなる。
	metaDirMode  = fs.FileMode(0o777)
	lockFileMode = fs.FileMode(0o666)
)

var (
	// errBusy は他者が保持中。異常ではなくスキップの合図。
	errBusy = errors.New("locked by someone else")
	// errNotOwner は「自分は持ち主ではない」(release/renew の対象違い、lease 喪失)。
	errNotOwner = errors.New("not the lock owner")
)

// Meta は lock の中身。取得時に 1 度書いたら二度と書き換えない (識別のためだけに使う)。
// 生存判定は lock の mtime が唯一の出典で、ここには期限を持たせない
// (2 出典にすると片方だけ更新する実装が生まれ、無音で drift する)。
type Meta struct {
	Token string `json:"token"`
	Host  string `json:"host"`
	User  string `json:"user"`
	PID   int    `json:"pid"`
	Label string `json:"label,omitempty"`
	// 🚨 秒の整数で持たない: 1 秒未満が 0 に丸められ、下の fallback で「既定 30 分」に
	// 化ける (テストが短い TTL を使えないだけでなく、丸めが黙って効くのが危ない)。
	TTLMillis  int64  `json:"ttl_ms"`
	AcquiredAt string `json:"acquired_at"`
	Version    string `json:"version"`
}

// Locker は 1 つの対象ディレクトリに対する操作をまとめる。
type Locker struct {
	dir     string // 対象ディレクトリ
	metaDir string // <dir>/.lockman
	timeout time.Duration
}

func NewLocker(dir string, timeout time.Duration) (*Locker, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		// 「対象が無い」と「使用中」は別物。busy に倒さない。
		return nil, fmt.Errorf("対象ディレクトリを読めない: %w", err)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("対象がディレクトリではない: %s", abs)
	}
	return &Locker{dir: abs, metaDir: filepath.Join(abs, metaDirName), timeout: timeout}, nil
}

func (l *Locker) lockPath() string { return filepath.Join(l.metaDir, lockName) }

// ensureDirs は .lockman とその配下を作る。既にあれば何もしない (競合は EEXIST で無害)。
// sticky が付いていたら警告する — 引き継ぎが動かなくなるため。
func (l *Locker) ensureDirs() error {
	for _, d := range []string{l.metaDir,
		filepath.Join(l.metaDir, tmpDirName),
		filepath.Join(l.metaDir, probeDirName),
		filepath.Join(l.metaDir, graveyardDirName),
	} {
		if err := os.Mkdir(d, metaDirMode); err != nil && !os.IsExist(err) {
			return err
		}
		// umask に削られたモードを明示的に戻す (SMB 越しは別ユーザーが触るため)。
		// 失敗は無視する: 別ユーザーが作ったディレクトリには chmod できないが、
		// 権限さえ足りていれば動く。
		_ = os.Chmod(d, metaDirMode)
	}
	if st, err := os.Stat(l.metaDir); err == nil && st.Mode()&fs.ModeSticky != 0 {
		warnf("%s に sticky bit が付いている: 他ユーザーの lock を引き継げず、TTL 切れでも奪えない", l.metaDir)
	}
	return nil
}

// serverNow は「共有を公開しているホストの時計」を返す。
//
// 🚨 ローカルの時計を使わないこと: マシン間で数十秒ずれると、生きている lock を
// stale と誤判定して二重実行に直結する。probe ファイルを作り、それに打刻された
// mtime を読むことで、どのマシンから見ても同じ時刻の出典になる。
func (l *Locker) serverNow() (time.Time, error) {
	name := filepath.Join(l.metaDir, probeDirName, mustToken())
	f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, lockFileMode)
	if err != nil {
		return time.Time{}, fmt.Errorf("サーバ時刻を取れない (probe を作れない): %w", err)
	}
	f.Close()
	defer func() { _ = os.Remove(name) }()
	st, err := os.Stat(name)
	if err != nil {
		return time.Time{}, fmt.Errorf("サーバ時刻を取れない (probe を stat できない): %w", err)
	}
	return st.ModTime(), nil
}

// readLock は現在の lock を読む。存在しなければ (nil, zero, nil) を返す。
func (l *Locker) readLock() (*Meta, time.Time, error) {
	st, err := os.Stat(l.lockPath())
	if os.IsNotExist(err) {
		return nil, time.Time{}, nil
	}
	if err != nil {
		return nil, time.Time{}, err
	}
	if st.IsDir() {
		// 想定外の型。壊れているので busy に倒す (勝手に消さない)。
		return nil, time.Time{}, fmt.Errorf("%w: lock がディレクトリになっている", errBusy)
	}
	b, err := os.ReadFile(l.lockPath())
	if err != nil {
		return nil, st.ModTime(), err
	}
	var m Meta
	if err := json.Unmarshal(b, &m); err != nil {
		// 中身が壊れている / 書きかけ。**空いているとは解釈しない** (fail-closed)。
		return nil, st.ModTime(), fmt.Errorf("%w: lock の中身を読めない (%v)", errBusy, err)
	}
	return &m, st.ModTime(), nil
}

// expired は mtime と TTL から stale かを判定する。now は必ず serverNow の値を渡す。
func expired(now, mtime time.Time, ttl time.Duration) bool {
	return now.Sub(mtime) > ttl
}

// holderTTL は「その lock の生死を決める TTL」を返す。
//
// 🚨 判定には**保持者が宣言した TTL** (lock の中身) を使う。奪いにきた側が渡す --ttl を
// 使ってはいけない: 短い --ttl を指定するだけで、他人の生きている lease を早期に
// 奪えてしまう (実測 2026-08-22。macOS では速すぎて出ず、CI の Linux で露見した)。
// 呼び出し側の --ttl は「自分が新しく作る lock の TTL」にだけ効く。
func holderTTL(m *Meta) time.Duration {
	if m == nil || m.TTLMillis <= 0 {
		// 壊れた・古い形式で TTL を読めないときは既定へ倒す。0 にすると即座に
		// 奪える (危険)、無限にすると永久 wedge になるため。
		return defaultTTL
	}
	return time.Duration(m.TTLMillis) * time.Millisecond
}

// Acquire はロックを取る。取れなければ errBusy を返す。
//
// 勝敗は「存在すれば失敗する 1 回の原子操作」だけで決める。事前に存在チェックをしない
// (チェックしてから作ると、その隙間に割り込まれるうえ、SMB クライアントの古い
// キャッシュが判定に混入する)。
func (l *Locker) Acquire(ttl time.Duration, label string) (*Meta, error) {
	if err := l.ensureDirs(); err != nil {
		return nil, err
	}
	now, err := l.serverNow()
	if err != nil {
		return nil, err
	}
	meta := &Meta{
		Token:      mustToken(),
		Host:       hostname(),
		User:       username(),
		PID:        os.Getpid(),
		Label:      label,
		TTLMillis:  ttl.Milliseconds(),
		AcquiredAt: now.UTC().Format(time.RFC3339),
		Version:    "lockman/1",
	}
	// 1 回目: そのまま置きにいく
	err = l.tryPlace(meta)
	if err == nil {
		return meta, nil
	}
	if !errors.Is(err, errBusy) {
		return nil, err
	}
	// 2 回目: 相手が stale なら引き継ぎを試みる
	took, err := l.tryTakeover()
	if err != nil {
		return nil, err
	}
	if !took {
		return nil, errBusy
	}
	if err := l.tryPlace(meta); err != nil {
		return nil, err
	}
	return meta, nil
}

// tryPlace は lock を 1 回の原子操作で置き、置けたことを読み直して確認する。
func (l *Locker) tryPlace(meta *Meta) error {
	b, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	tmp := filepath.Join(l.metaDir, tmpDirName, meta.Token+".json")
	if err := writeFileSync(tmp, b); err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp) }()

	// link(2) が使えれば、lock は「最初から中身が入った状態」で現れる (途中経過が
	// 他者に見えない)。macOS の smbfs では ENOTSUP になりうるので O_EXCL に落とす。
	linkErr := os.Link(tmp, l.lockPath())
	switch {
	case linkErr == nil:
	case os.IsExist(linkErr):
		return errBusy
	default:
		f, err := os.OpenFile(l.lockPath(), os.O_CREATE|os.O_EXCL|os.O_WRONLY, lockFileMode)
		if os.IsExist(err) {
			return errBusy
		}
		if err != nil {
			return err
		}
		if _, err := f.Write(b); err != nil {
			f.Close()
			return err
		}
		if err := f.Sync(); err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
	// write-then-verify: 置けたつもりで負けている可能性を潰す。
	got, _, err := l.readLock()
	if err != nil {
		return err
	}
	if got == nil || got.Token != meta.Token {
		return errBusy
	}
	return nil
}

// tryTakeover は stale な lock を「存在しない名前への rename」で 1 人だけが引き取る。
//
// 🚨 ここを unlink → create に書き換えてはいけない。期限切れを見つけた 2 者が
// 「消して作り直す」と両方が勝つ。rename なら勝者は 1 人に絞られる。
func (l *Locker) tryTakeover() (bool, error) {
	now, err := l.serverNow()
	if err != nil {
		return false, err
	}
	m, mtime, err := l.readLock()
	if err != nil && !errors.Is(err, errBusy) {
		return false, err
	}
	if mtime.IsZero() {
		return true, nil // 既に誰かが退けた後。作りにいってよい
	}
	if !expired(now, mtime, holderTTL(m)) {
		return false, nil
	}
	grave := filepath.Join(l.metaDir, graveyardDirName, mustToken())
	if err := os.Rename(l.lockPath(), grave); err != nil {
		if os.IsNotExist(err) {
			return true, nil // 別の誰かが先に退けた。作りにいって、負ければ busy になる
		}
		return false, err
	}
	return true, nil
}

// Release は自分の lock を解放する。
//
// 期限切れの自分の lock は消さない: その時点で他者が引き継いでいる可能性があり、
// 消すと他者の lock を消すことになる。呼び出し側には errNotOwner を返して
// 「走行中に奪われていた」ことを知らせる。
func (l *Locker) Release(token string) error {
	m, mtime, err := l.readLock()
	if err != nil {
		return err
	}
	if m == nil || m.Token != token {
		return errNotOwner
	}
	now, err := l.serverNow()
	if err != nil {
		return err
	}
	if expired(now, mtime, holderTTL(m)) {
		return fmt.Errorf("%w: lease が切れている (走行中に引き継がれた可能性)", errNotOwner)
	}
	return os.Remove(l.lockPath())
}

// Renew は保持を更新する。utimes は使わない (クライアントの時計が混入するため)。
// 同じ内容を書き直してサーバに mtime を打刻させ、打刻したのが本当にサーバかを検算する。
func (l *Locker) Renew(token string) error {
	m, _, err := l.readLock()
	if err != nil {
		return err
	}
	if m == nil || m.Token != token {
		return errNotOwner
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(l.lockPath(), os.O_WRONLY|os.O_TRUNC, lockFileMode)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// 検算: 打刻がクライアント側だと時計ずれがそのまま TTL 判定へ入り込む。
	now, err := l.serverNow()
	if err != nil {
		return err
	}
	st, err := os.Stat(l.lockPath())
	if err != nil {
		return err
	}
	if d := now.Sub(st.ModTime()); d > clockSkewTolerance || d < -clockSkewTolerance {
		return fmt.Errorf("mtime の打刻がサーバ時刻と %v ずれている: TTL 判定が壊れるので中断する", d)
	}
	return nil
}

// State は check / status が返す観測結果。
type State struct {
	Held      bool   `json:"held"`
	Token     string `json:"token,omitempty"`
	Host      string `json:"host,omitempty"`
	User      string `json:"user,omitempty"`
	Label     string `json:"label,omitempty"`
	AgeSec    int    `json:"age_seconds,omitempty"`
	ExpiresIn int    `json:"expires_in_seconds,omitempty"`
}

// Inspect は現在の状態を返す。**排他の根拠には使えない** (読んだ次の瞬間に変わる)。
func (l *Locker) Inspect() (*State, error) {
	m, mtime, err := l.readLock()
	if err != nil {
		return nil, err
	}
	if m == nil {
		return &State{Held: false}, nil
	}
	now, err := l.serverNow()
	if err != nil {
		return nil, err
	}
	if expired(now, mtime, holderTTL(m)) {
		return &State{Held: false}, nil
	}
	age := now.Sub(mtime)
	ttl := holderTTL(m)
	return &State{
		Held: true, Token: m.Token, Host: m.Host, User: m.User, Label: m.Label,
		AgeSec:    int(age.Seconds()),
		ExpiresIn: int((ttl - age).Seconds()),
	}, nil
}

// Break は人が今すぐ剥がすための操作。unlink ではなく graveyard へ退ける
// (勝者を 1 人に保ち、誰が握っていたかの記録も残す)。
func (l *Locker) Break() error {
	if err := l.ensureDirs(); err != nil {
		return err
	}
	grave := filepath.Join(l.metaDir, graveyardDirName, mustToken())
	if err := os.Rename(l.lockPath(), grave); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return nil
}
