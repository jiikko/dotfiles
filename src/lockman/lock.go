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
//	├── lock               存在 = ロック中。中身は取得時に 1 度だけ書く
//	├── tmp/<token>.json   書きかけの置き場
//	├── tmp/<gen>.takeover 引き継ぎの調停 (その世代を退ける役を 1 人に絞る目印)
//	├── probe/<token>      サーバ時刻を得る使い捨て
//	└── graveyard/<token>  引き継ぎ・break で退けた旧 lock
const (
	metaDirName      = ".lockman"
	lockName         = "lock"
	tmpDirName       = "tmp"
	probeDirName     = "probe"
	graveyardDirName = "graveyard"

	// 引き継ぎの調停に使う目印の接尾辞。置き場は tmp/ (掃除のバックストップが要るため)。
	//
	// 🚨 掃除を回収の主手段にしてはいけない。目印を取った直後にプロセスが死ぬ経路は
	// **既定の利用で踏む**ので (下の takeoverClaimGrace)、掃除 (scratchRetention = 1h) を
	// 待たせると既定 TTL 30m を超えて引き継げなくなり、「TTL を過ぎれば誰かが引き継げる」
	// という道具の契約を割る。回収は tryTakeover 自身が猶予つきで行い、掃除は取りこぼしの
	// 受け皿に留める。
	takeoverClaimSuffix = ".takeover"

	// 目印の作成者が「もう飛行していない」と見なすまでの猶予の倍率。
	//
	// 作成者が目印を作った後に残るのは readLock + rename の 2 つだけで、それが固まれば
	// 作成者自身の --io-timeout が発火してプロセスが終わる。終わるまでに要るのは
	// Acquire の 1 回 + dispatch の deferred Cleanup の 1 回で、合わせて io-timeout の
	// 約 2 倍。3 倍はその余裕。
	takeoverClaimGraceFactor = 3

	// 目印の申告値として受け入れる上限。**フラグの UI 制約 (main.go の maxIOTimeout) とは
	// 別に持つ** — 同じ値だが、UI 側を下げたときに猶予まで黙って縮み、「まだ飛行している
	// 作成者の目印を回収する」= 二重取得の側へ倒れるのを防ぐため
	// (2 つが一致することは TestTakeoverClaimGraceBounds が固定する)。
	//
	// 🚨 これを超える申告をする host と共有すると、その host がまだ飛行しているうちに
	// 目印を回収しうる。該当するのは `--io-timeout` の範囲検証が入る前のビルド (issue 357 以前)
	// と、将来この上限を広げたビルド。再評価の trigger: 異なる版の lockman が同じ共有を
	// 触る運用が出たとき。
	maxTakeoverClaimTimeout = 5 * time.Minute

	// 共有前提のモード。sticky を付けないこと: rename の可否は親ディレクトリの
	// 権限で決まるため、+t が付くと他ユーザーの lock を graveyard へ退けられず、
	// TTL が切れても永久に引き継げなくなる。
	//
	// 🚨 想定する敵: **いない**。この 0o777 + no-sticky が守るのは「協調する
	// ホスト・ユーザーどうしが誤って同時に走ること」だけで、**非信頼の同一ホスト
	// ユーザーは射程外**。no-sticky は「他ユーザーが自分の lock を rename できる」
	// ことを設計として要求しているので、権限で敵を締め出す方向とは両立しない。
	// 射程に入れるなら (lock を差し替える悪意あるローカルユーザーを想定するなら)
	// このモードだけでは足りず、readLock / Renew を O_NOFOLLOW + fd ベースの
	// Fstat へ作り替える必要がある。射程を広げるときに再評価する。
	metaDirMode  = fs.FileMode(0o777)
	lockFileMode = fs.FileMode(0o666)
)

var (
	// errBusy は他者が保持中。異常ではなくスキップの合図。
	errBusy = errors.New("locked by someone else")
	// errNotOwner は「自分は持ち主ではない」(release/renew の対象違い、lease 喪失)。
	errNotOwner = errors.New("not the lock owner")
	// errClaimReplaced は「回収しようとした目印が、途中で別の観測者に置き直された」。
	// errBusy を流用しないこと: 流用すると「譲ってよい失敗」の集合が、将来 errBusy を
	// 返す実装が増えたときに黙って広がる。
	errClaimReplaced = errors.New("takeover claim was replaced")
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
	// 🚨 ここも --io-timeout で包む。対象ディレクトリは共有そのものなので、応答しない
	// マウントではこの Stat が返らず、Locker ができる前に無言で固まる。
	st, err := statDirTimed(abs, timeout)
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

// takeoverObservedHook は「期限切れと判定した直後」に呼ばれる seam。production では
// 何もしない。
//
// 🚨 テストのためだけの 1 行だが、外すと回帰テストが成立しない。判定から破壊的操作
// までの窓はミリ秒しかないので、seam が無いと「16 本で競わせて勝者を数える」統計テスト
// にしかならず、それは負荷次第で緑になる assert (avoid-wall-clock-assertions.md)。
// 実測 2026-09-15: 素の競争では -count=200 に 1 回しか落ちない。
var takeoverObservedHook = func() {}

// takeoverReclaimHook は「目印が放棄されたと判定した直後」に呼ばれる seam。production では
// 何もしない。
//
// 🚨 これが無いと、回収の調停 (mark の O_EXCL) を検査するテストが**統計的**になる。
// 回収した者が目印の打刻を戻した後に別の者が Stat すると「飛行中」の枝へ落ちるので、
// 直列化した環境では mark が 1 度も作られないまま「役は 1 人」が成立してしまう
// (= 調停が働いた証拠にならない緑)。
var takeoverReclaimHook = func() {}

// takeoverRefreshHook は「回収した目印の打刻を戻す直前 (照合を通った後)」に呼ばれる seam。
// production では何もしない。
//
// 🚨 これが無いと、**照合を fd に対して行っていること自体が無検査**になる。パスに対する
// 照合 (os.Stat で見てから O_TRUNC で開く) へ戻す変異は、seam が無いと全テスト緑で通る —
// 単一プロセスのテストには「照合と書き込みのあいだに目印を置き直す第三者」が居ないため。
var takeoverRefreshHook = func() {}

// tryTakeover は stale な lock を 1 人だけが引き取る。
//
// 🚨 「rename は原子操作だから勝者は 1 人に絞られる」は**偽**。原子なのは操作であって、
// 「自分が期限切れと判定したあの lock を動かす」ことは保証しない — rename は名前に対する
// 操作で、名前の指す先は判定してから rename するまでに入れ替わる。実測 2026-09-15
// (issue 366): 判定と rename のあいだを 60ms 広げると 10/10 で勝者が 2〜5 人になり、
// graveyard には「先に勝った者の新しい lock」が入っていた (2 人目が 1 人目の lock を
// 退けてから自分の lock を置いていた)。
//
// 閉じ方は 2 段。段ごとに変異を当てて red を確認してある (issue 366):
//
//  1. **調停**: 観測した世代 (lock の token) から決まる名前を tmp/ へ O_EXCL で取る。
//     同じ世代を見た者は同じ名前を狙うので、退ける役が 1 人に絞られる。目印を取った者が
//     死んだ場合は、猶予を過ぎた目印を**回収**する (reclaimTakeoverClaim)。
//  2. **直前の再照合**: 破壊的操作の直前に lock を読み直し、まだ同じ世代・同じ mtime かを
//     確かめる。1 だけでは、目印が回収・掃除された後に現れる遅延観測者と、Break / Release の
//     割り込みが残る。
//
// どちらも「判定できないなら退けない」へ倒す: 取り逃した引き継ぎは次の acquire で済むが、
// 誤った引き継ぎはそのまま二重実行になる。
//
// 🚨 残る窓: 2 の再照合から rename までのあいだに `lock` という**名前の指す先**が
// 差し替わると、置かれたばかりの lock を退けうる。差し替えられる主体は 3 つあり、
// **危険度が同じではない**:
//
//   - `Break`: **何も要らない**。期限検査も token 照合も目印の取得もしない無条件の rename
//     なので、人が break を打った直後に別者が acquire すると成立する。しかも break は
//     「詰まって見える」場面 = 引き継ぎが飛び交う場面で打たれる。**ここが最も現実的**
//   - `Release`: 時計の逆行が要る。`Release` は readLock の**後**に serverNow を取り、
//     `tryTakeover` は**前**に取るので、serverNow が単調なら takeover が期限切れと判定した
//     後の Release は必ず期限切れ側に落ちて errNotOwner で帰る
//   - 別の `tryTakeover`: 目印で止まる (1 段目)
//
// 窓の幅も「syscall 2 つぶん」とは限らない。**rename 自身が固まれば、退けられるのは
// その syscall が返る瞬間の `lock` が指す先**なので、窓は stall の長さそのもので上限が無い。
// 目印も再照合も rename より手前にあるので、この形だけはどちらでも防げない。
// 0 にするには rename ではなく「inode を指定した削除」が要り、POSIX にその原始操作が無い。
// 再開の trigger: 実 lock を使った並行実験でこの経路を再現できたとき (Break の経路は
// seam で順序を作れば再現できるはず。未実施)。
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
	takeoverObservedHook()

	// 1 段目: この世代を退ける役を 1 人に絞る。
	gen := takeoverGeneration(m, mtime)
	claim := filepath.Join(l.metaDir, tmpDirName, gen+takeoverClaimSuffix)
	switch err := l.placeTakeoverClaim(claim); {
	case err == nil:
	case os.IsExist(err):
		// 役は誰かが取っている。飛行中なら譲り、放棄されていれば回収する。
		took, rerr := l.reclaimTakeoverClaim(claim, now)
		if rerr != nil {
			return false, rerr
		}
		if !took {
			return false, nil
		}
	default:
		return false, err
	}
	// 退けずに帰る経路では目印を外す。外さないと、一過性の I/O エラーがその世代の
	// 引き継ぎを猶予 (takeoverClaimGrace) いっぱい塞ぐ。**退けたときは外さない**:
	// 同じ世代を古い state で見ている遅延観測者を、2 段目に頼らずもう 1 段止められる。
	evicted := false
	defer func() {
		if !evicted {
			_ = os.Remove(claim)
		}
	}()

	// 2 段目: 破壊的操作の直前に取り直して照合する。ここで初めて対象が確定する。
	m2, mtime2, err := l.readLock()
	if err != nil && !errors.Is(err, errBusy) {
		return false, err
	}
	if mtime2.IsZero() {
		return true, nil // 別の誰かが先に退けた。作りにいって、負ければ busy になる
	}
	if !mtime2.Equal(mtime) || takeoverGeneration(m2, mtime2) != gen {
		// 判定してから中身が変わった (引き継がれた / 延長された)。退けない。
		return false, nil
	}

	grave := filepath.Join(l.metaDir, graveyardDirName, mustToken())
	if err := os.Rename(l.lockPath(), grave); err != nil {
		if os.IsNotExist(err) {
			return true, nil // 別の誰かが先に退けた。作りにいって、負ければ busy になる
		}
		return false, err
	}
	evicted = true
	return true, nil
}

// takeoverGeneration は引き継ぎの調停に使う「世代 id」を返す。要件は 1 つだけ:
// **同じ lock 世代を見た 2 者が必ず同じ id を得ること**。token は世代ごとに新しく引くので、
// それがそのまま世代 id になる。
//
// 🚨 mtime を id に混ぜないこと。延長 (Renew) で mtime が動くと、同じ世代を違う mtime で
// 見た 2 者が別々の id を得て調停をすり抜ける。二重取得までは 2 段目が止めるが、1 段目の
// 主張 (「退ける役は 1 人」) は黙って消える。
//
// 🚨 token はパスの構成要素になるので、他ホストが書いた値をそのまま使わない。小文字 hex
// 以外は mtime 由来の id へ倒す (中身を読めない lock には token が無いので、その経路にも
// この id を使う)。mtime 由来の id は token より弱い — 別世代が同じ mtime を持つと衝突する
// — が、衝突は「引き継がない」側へ倒れるだけで危険側には倒れない。
//
// 🚨 この関数は「lock の関数」ではなく「lock × その観測者が中身を parse できたか」の
// 関数なので、1 段目の調停は **「同じ lock は誰が読んでも同じ parse 結果になる」という
// 仮定**に乗っている (readLock は mtime と中身を独立した 2 つの syscall で採るので、
// 対は原子的に観測されていない)。到達可能な非決定性は潰れている — tryPlace の O_EXCL
// fallback が作る「中身が空の lock」も Renew の O_TRUNC も mtime を現在へ動かすため、
// gen の計算に届く前に expired で弾かれる (「Renew が触らないから mtime は動かない」では
// ない。動くが、動いた lock は期限切れにならない) — が、SMB の属性キャッシュが
// 「古い mtime + 新しい中身」を返す環境は**未確認リスク**として残る。
func takeoverGeneration(m *Meta, mtime time.Time) string {
	if m != nil && isHexToken(m.Token) {
		return m.Token
	}
	return fmt.Sprintf("notoken-%d", mtime.UnixNano())
}

// isHexToken は mustToken が作る形 (空でない小文字 hex) かを見る。
func isHexToken(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
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
//
// 期限切れの lease は延長せず errNotOwner を返す。token が一致していても、TTL を
// 超えていればその lock は既に他者が引き継げる状態にあり、書き直すと
// 「引き継いだ側の lock を truncate して自分のメタで上書きする」形になるため。
// 判定は Release と同じ expired / holderTTL を使う (2 つ目の判定を作らない)。
//
// 🚨 **readLock と下の OpenFile のあいだの窓は 0 になっていない。直さないと決めた**
// (issue 340 項目 1 の残り)。
// 期限検査 (issue 312) で「期限切れ lease の復活」は塞いだが、「照合した直後に他者へ
// 引き継がれた lock を O_TRUNC で上書きする」経路は窓が縮んだだけで残る。
// 0 にするには取得と同じ「存在しない名前への rename で勝者を 1 人に絞る」形を Renew にも
// 持ち込む必要があり、renew のたびに rename が増える。
// 再開の trigger: 実 lock を使った並行実験でこの上書きを再現できたとき。
// 影響の範囲: av1ify は finalize の直前に renew の rc で保持を判定する
// (__av1ify_lock_still_held)。rc=0 は「token 一致 かつ 期限内」までを意味する。
func (l *Locker) Renew(token string) error {
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
	// 上の期限検査で取った now は書き込み前の時刻なので、ここで取り直す。
	now, err = l.serverNow()
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

// takeoverClaimBody は調停の目印の中身。**回収の可否を判断するために作成者が自分の
// 飛行時間の上限 (--io-timeout) を申告する**のが本体で、host / user / pid は
// 「誰が握ったまま死んだか」を人が追うための情報。
type takeoverClaimBody struct {
	Host      string `json:"host"`
	User      string `json:"user"`
	PID       int    `json:"pid"`
	TimeoutMS int64  `json:"io_timeout_ms"`
	At        string `json:"at"`
}

// marshalClaimBody は目印 / mark に書く身元と飛行上限を作る。
func (l *Locker) marshalClaimBody() ([]byte, error) {
	return json.Marshal(&takeoverClaimBody{
		Host: hostname(), User: username(), PID: os.Getpid(),
		TimeoutMS: l.timeout.Milliseconds(), At: time.Now().UTC().Format(time.RFC3339),
	})
}

// placeTakeoverClaim は目印を O_EXCL で作る。既にあれば os.IsExist が真の error を返す。
func (l *Locker) placeTakeoverClaim(claim string) error {
	f, err := os.OpenFile(claim, os.O_CREATE|os.O_EXCL|os.O_WRONLY, lockFileMode)
	if err != nil {
		return err
	}
	b, err := l.marshalClaimBody()
	if err == nil {
		// 中身は診断と猶予の申告だけなので、書けなくても目印としては成立する
		// (読めない目印は takeoverClaimGrace の fallback で扱う)。取った役を手放すほうが害が大きい。
		_, _ = f.Write(b)
		err = f.Close()
	} else {
		f.Close()
	}
	if err != nil {
		// 🚨 目印だけを残して帰らない。Close の失敗は SMB の write-behind の flush 失敗で
		// 実在する経路で、残すとその世代が猶予いっぱい塞がる (プロセスは生きているので
		// 誰も飛行していない状態を猶予で待つことになる)。取った役を明示的に手放す。
		_ = os.Remove(claim)
		return err
	}
	return nil
}

// takeoverClaimGrace は「目印の作成者はもう飛行していない」と見なすまでの猶予を返す。
//
// 🚨 短すぎると**生きた作成者の目印を回収して役が 2 人になる** (= 二重取得) ので、
// 判定できないときは長い側へ倒す。作成者が申告した --io-timeout を使い、読めなければ
// 自分の値と既定値の大きいほうで代用する。
//
// 🚨 **ms のまま頭打ちにしてから time.Duration へ変換する**。先に変換すると、桁を間違えた
// 申告値で乗算が wrap し、**最長のはずの猶予が最短 = 回収する側に化ける**
// (実測 2026-09-16: TimeoutMS = MaxInt64 は -1ms になり、猶予が既定の 30s に落ちた。
// 「溢れたら無限の猶予へ倒す」と書いたガードは、その手前の wrap で素通りされていた)。
// 頭打ちの値が maxTakeoverClaimTimeout なのは、--io-timeout が 100ms〜5m に検証済み (main.go) で、
// 正しい作成者の飛行時間はその範囲で覆えるから。結果として猶予は必ずその 3 倍以下になり、
// d * takeoverClaimGraceFactor は溢れない。
//
// 🚨 猶予は **--ttl より長くなりうる** (例: `--ttl 1m --io-timeout 5m` で猶予 15m)。
// 目印を置いた直後に死んだプロセスがあると、その世代は TTL を過ぎても猶予のあいだ
// 引き継げない。猶予を TTL で縛る手は採らない — 縛ると「まだ飛行しうる作成者の目印を
// 回収する」side へ倒れ、二重取得に直結する。人の脱出口は `lockman break`。
func (l *Locker) takeoverClaimGrace(c *takeoverClaimBody) time.Duration {
	d := l.timeout
	if defaultIOTimeout > d {
		d = defaultIOTimeout
	}
	if c != nil && c.TimeoutMS > 0 {
		ms := c.TimeoutMS
		if max := maxTakeoverClaimTimeout.Milliseconds(); ms > max {
			ms = max // 受け入れる上限を超える申告は壊れた値として扱う
		}
		if got := time.Duration(ms) * time.Millisecond; got > d {
			d = got
		}
	}
	// 🚨 l.timeout 自体の頭打ち。**production の入口 (main.go) は範囲検証を通るので、ここは
	// 直接 Locker を組む経路 (テスト・将来の埋め込み) のためだけの溢れガード**。
	// 「検証されていない入口が production にある」という意味ではない。
	if d > maxTakeoverClaimTimeout {
		d = maxTakeoverClaimTimeout
	}
	return d * takeoverClaimGraceFactor
}

// readTakeoverClaimBody は目印の中身を読む。読めなければ nil (猶予は fallback になる)。
func readTakeoverClaimBody(claim string) *takeoverClaimBody {
	b, err := os.ReadFile(claim)
	if err != nil {
		return nil
	}
	var c takeoverClaimBody
	if err := json.Unmarshal(b, &c); err != nil {
		return nil
	}
	return &c
}

// reclaimTakeoverClaim は「作成者が死んで残った目印」を回収する。回収できたら true
// (= 呼び出し側が退ける役を引き継ぐ)。
//
// 🚨 **目印を消したり動かしたりして回収してはいけない**。remove も rename も「今そこに
// あるもの」に効く無条件の操作なので、先に回収した者が作り直した**新しい**目印を次の者が
// 奪う形になる — 直そうとしている bug と同じ TOCTOU を 1 段下で作り直すだけ
// (実測 2026-09-16: rename + 作り直しの版は 8 本同時で役が 5 人になった)。
//
// 代わりに「**観測した古さ**から決まる名前」を O_EXCL で取る。同じ古さを見た者は同じ名前を
// 狙うので、回収する役も 1 人に絞られる。目印そのものには触らない。
//
// 回収した者は最後に目印を「生きている」状態へ戻す (refreshTakeoverClaim)。戻さないと、
// 自分が落ちたとき後続は同じ古さしか観測できず、回収の名前が埋まったまま次の回収ができない。
//
// 🚨 診断は**最初の 1 猶予ぶん無言**になる。mark の打刻は「目印が凍った時刻 + 猶予」あたりに
// 付くので、警告が出るのは目印が凍ってから約 2 猶予後 (既定で ~60s、5m 申告なら ~30 分)。
// その帯は check が free・acquire が busy・stderr が空になる。恒久化はしない。
//
// 🚨 残る窓 (**この修正が新設した wedge**。trade-off を明示しておく): mark を取ってから
// 打刻を戻すまでのあいだに**プロセスが死ぬ**と、目印の打刻は観測値のまま凍り、以後の観測者は
// 全員同じ mark 名を計算して EEXIST で落ちる。復帰は掃除 (scratchRetention 1h +
// レート制限 10m) だけなので **最悪 ~1h10m**、既定 TTL 30m を超える。
//
// 窓は「syscall 1 つ」とは限らない: 打刻を戻すのは**停止したマウントで止まる操作**なので、
// この道具が想定する主要シナリオでは窓は stall の長さそのもの (--io-timeout が発火して
// プロセスが終わる = 死ぬ側へ倒れる)。
//
// 366 以前はこの状態が無かった (目印も mark も無いので、死んだプロセスは誰も塞がなかった)。
// **「退ける役が 2 人」を「退ける役が 0 人、最長 ~1h10m」と交換している**。排他の道具として
// 正しさを優先した判断で、人の脱出口は `lockman break` (上の warnf が案内する)。
// 根治は 362 / 363 の側 (見捨てられた goroutine / 遅い signal.Notify が副作用を残さなくなれば、
// この回収機構は取りこぼしの受け皿へ格下げできる)。
func (l *Locker) reclaimTakeoverClaim(claim string, now time.Time) (bool, error) {
	st, err := os.Stat(claim)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil // 既に消えた。今回は譲って次の acquire に任せる
		}
		return false, err
	}
	body := readTakeoverClaimBody(claim)
	if grace := l.takeoverClaimGrace(body); now.Sub(st.ModTime()) <= grace {
		// 飛行中。**ここを黙って busy にしない** — check は期限切れを free と答えるので、
		// 理由を出さないと「free なのに acquire できない」という診断不能の矛盾になる。
		warnf("引き継ぎの調停中のため取れない (%s。猶予 %v)", takeoverClaimWho(body), grace)
		return false, nil
	}
	takeoverReclaimHook()
	mark := fmt.Sprintf("%s.%d", claim, st.ModTime().UnixNano())
	f, err := os.OpenFile(mark, os.O_CREATE|os.O_EXCL|os.O_WRONLY, lockFileMode)
	if err != nil {
		if os.IsExist(err) {
			// 判定にも名前にも **mark の body** を使う。目印 (claim) の body は
			// **とうに死んだ作成者**のものなので、それで閾値を作ると「回収者はまだ飛行中
			// なのに止まっていると診断する」ことになり、しかも人へ無関係な pid を見せる。
			// 🚨 **良性の競合では黙る**。同じ古さを見た者どうしの競合はミリ秒で解消するのに、
			// そこで `lockman break` を勧めると、この道具で**最も現実的に二重取得を作る操作**
			// (期限検査も token 照合もしない無条件 rename) へ人を誘導することになる。
			// 実測 2026-09-16: 8 本同時の正常な回収で 7 行の break 案内が出ていた。
			//
			// 鳴らすのは **mark 自身が猶予を超えて古い**とき = 回収を始めた者が死んで、
			// 掃除まで無言で塞がる状態に入っているときだけ。
			if mst, serr := os.Stat(mark); serr == nil {
				mbody := readTakeoverClaimBody(mark)
				if now.Sub(mst.ModTime()) > l.takeoverClaimGrace(mbody) {
					warnf("引き継ぎの回収が止まっている (%s)。解消しなければ lockman break", takeoverClaimWho(mbody))
				}
			}
			return false, nil
		}
		return false, err
	}
	if b, merr := l.marshalClaimBody(); merr == nil {
		_, _ = f.Write(b) // 書けなくても mark としては成立する (猶予は fallback になる)
	}
	f.Close()
	if err := l.refreshTakeoverClaim(claim, st.ModTime()); err != nil {
		// 🚨 mark を残したまま帰らない。目印の打刻は観測した値のままなので、以後の観測者は
		// 同じ mark 名を計算して EEXIST で弾かれ続け、**プロセスが落ちていなくても**その世代が
		// 掃除まで引き継げなくなる (回収機構が防ぐはずの状態そのもの)。権限ドリフトの EACCES /
		// SMB の EIO で到達する。
		_ = os.Remove(mark)
		if os.IsExist(err) || errors.Is(err, errClaimReplaced) {
			return false, nil // 入れ違いに別の観測者が役を取った。譲る
		}
		return false, err
	}
	warnf("放棄された引き継ぎの目印を回収した (%s)", takeoverClaimWho(body))
	return true, nil
}

// refreshTakeoverClaim は回収した目印の打刻をサーバに更新させる。
//
// 🚨 照合は **fd に対して**行う。「Stat で照合してから別の syscall で開く」形は、そのあいだに
// 掃除が目印を浚い、別の観測者が O_EXCL で置き直すと**その人の目印を開いて壊す** (= 役が 2 人)。
// 先に開いてから fstat すれば、置き換えが open より前なら新しい inode を掴んで不一致で弾け、
// open より後なら unlink 済みの旧 inode を掴むので相手には 1 バイトも書かない。
//
// 🚨 **保証しているのは「相手のファイルを壊さない」までで、「役は 1 人」ではない**。
// 置き換えが open の後だと `f.Stat()` は旧 inode の打刻を返すので照合は通り、この関数は
// 成功を返す — 置き直した側と 2 人が役を持つ。
//
// 🚨 **そうなった後は、2 段目の再照合も `tryPlace` の O_EXCL も一意性を担保しない**
// (2026-09-16 の敵対レビュー 6 周目が反証。ここに「担保する」と書いていたのは誤り)。
// 2 人とも同じ (token, mtime) を観測しているので 2 段目は両方通り、先に退けた側が lock を
// 置いた後、もう一方の `os.Rename` が**その置いたばかりの lock** を退けて自分のを置ける —
// issue 366 の元の signature (graveyard に「現に誰かが保持している token の lock」が入る)
// がそのまま出る。2 段目が守るのは**古い state で来た観測者**であって、正当に役を持つ
// 2 人目ではない。役の一意性を作っているのは 1 段目だけ。
// さらに 2 段目で落ちると `tryTakeover` の defer が `os.Remove(claim)` で**相手の生きた
// 目印を消す**。
//
// 旧実装 (Stat → O_TRUNC) は同じ順序で相手のファイルを潰していたので改善ではあるが、
// 閉じてはいない。役が 2 人になる生成器は 4 つに絞れており、どれも既知として記録済み:
// 掃除が 1h 超の目印を open 後に浚う / 猶予超過 (設計上の trade-off) /
// takeoverGeneration の parse 非決定性 (未確認リスク) / 見捨てられた goroutine (issue 362)。
// 検査可能な痕跡は「graveyard に、現に保持されている token の lock が入っている」。
//
// 🚨 **O_TRUNC を open に付けない**。付けると開いた時点で中身が消えるので、照合で弾く前に
// 相手の目印を壊す。切り詰めは書けた後に当てる。
//
// 🚨 書き込みの失敗は返す。ただし**失敗しても中身が壊れていない保証は無い** (書けた分だけ残る)。
// 壊れた目印は次の観測者から「中身を読めない目印」に見えて短い fallback の猶予で扱われるが、
// そのとき作成者は既に死んでいる (回収に入っているのがその証拠) ので危険側には倒れない。
// この経路は実行時の I/O 失敗が要るためテストしていない (**未検証**)。
func (l *Locker) refreshTakeoverClaim(claim string, observed time.Time) error {
	f, err := os.OpenFile(claim, os.O_WRONLY, lockFileMode)
	if os.IsNotExist(err) {
		// 掃除に浚われた後。作り直せたら役は自分のまま、負けたら相手のもの。
		return l.placeTakeoverClaim(claim)
	}
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	// 🚨 seam は**打刻を得た直後・照合より前**に置く。後ろへ動かすほど、パス照合
	// (Stat してから O_TRUNC で開く) へ戻す変異で破壊がこの点より前に済んでしまい、
	// 変異が緑のまま通る (実測 2026-09-16: 書き込みの直前でも照合の直後でも素通りした)。
	takeoverRefreshHook()
	if !st.ModTime().Equal(observed) {
		f.Close()
		return fmt.Errorf("%w: 回収の途中で目印が置き直された", errClaimReplaced)
	}
	b, err := l.marshalClaimBody()
	if err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Truncate(int64(len(b))); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// takeoverClaimWho は目印の作成者を人が読める形にする。
func takeoverClaimWho(c *takeoverClaimBody) string {
	if c == nil {
		return "作成者不明の目印"
	}
	return fmt.Sprintf("%s@%s pid=%d が %s に取得", c.User, c.Host, c.PID, c.At)
}
