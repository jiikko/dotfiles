package chromecookie

// Chrome の Cookie DB / Local Storage は、ロックを避けるため作業領域へコピーしてから読む。
// コピーの中身は「なりすましログインできる資格情報そのもの」なので、**プロセスが終わった
// 時点で必ず消えている**ことを構造で保証する。
//
// 🚨 「必ず消す」を defer だけに任せない。Go の既定ではシグナル（Ctrl-C）でプロセスが
// 即終了して defer が走らず、フルディスクアクセスで守られた領域の完全なコピーが、
// 守られていない $TMPDIR に残る。
//
// 3 段構え（段ごとにテストを持つ。workspace_test.go / signal_cleanup_test.go）:
//  ① defer による即時削除 — 正常終了・エラー・panic を覆う
//  ② シグナル（SIGINT/SIGTERM/SIGHUP）を捕まえ、登録済みの削除を実行してから終了する
//  ③ ②でも間に合わない終わり方（SIGKILL・強制終了・電源断）に備え、**起動時に**
//     「自分が作った親ディレクトリ直下」「名前が <pid>-… の形」「その pid が生きていない」の
//     3 条件をすべて満たすものだけを消す（呼び出し側の main の先頭で 1 回呼ぶ。
//     資格情報を読む経路に置くと、help のような資格情報を読まないコマンドでは走らない）
//
// ③ は破壊的操作なので、条件に合わないものは一切触らない（母集合を広げない）。

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Workspace は 1 つのツールの作業領域（~/Library/Caches/<app>/extract）と、その中に作った
// 一時コピーの後始末の登録簿を持つ。プロセスに 1 つだけ作り、main の先頭で
// InstallCleanupOnSignal と SweepStaleTempDirs を呼ぶ。
//
// 🚨 app はツールごとに別の名前にする。同じ名前を 2 つのツールが使うと、③の掃除が
// 相手の生きている実行の作業領域を母集合に入れる（pid の生死で守られるが、7 日を超えた
// ものは消す）。
type Workspace struct {
	app string

	mu    sync.Mutex
	paths map[string]struct{}
	// closing は②（シグナル経路）が後始末を始めたことを表す。以後は新しい
	// 一時ディレクトリを作らせない（mu の下で読み書きする）。
	closing bool
}

// NewWorkspace は app（~/Library/Caches 直下のディレクトリ名。例: "slack-cli"）の作業領域を返す。
// app は 1 コンポーネントの名前でなければならない（区切り文字・"." / ".." は panic）。
// 作業領域はこの時点では作らない（最初の NewTempDir で作る）。
func NewWorkspace(app string) *Workspace {
	if app == "" || app == "." || app == ".." || strings.ContainsAny(app, `/\`) || app != filepath.Base(app) {
		panic(fmt.Sprintf("chromecookie: 不正な app 名 %q", app))
	}
	return &Workspace{app: app, paths: map[string]struct{}{}}
}

// errCleanupClosing は終了処理中に一時ディレクトリを作ろうとしたことを表す。
var errCleanupClosing = errors.New("終了処理中のため、一時コピーを作りません")

// RunAllCleanups は登録済みのパスをすべて削除する。
// ①の defer と②のシグナル経路の両方から呼ばれるが、二重呼び出しは無害。
//
// 🚨 削除は③と同じく「検証済みの作業領域を開いた fd」経由で行う。パス文字列を
// os.RemoveAll に渡すと、途中のディレクトリがシンボリックリンクに差し替えられていた場合に
// **リンク先を再帰削除する**（③だけ塞いで①②に同じ穴を残していた。敵対的レビューが実証）。
//
// 🚨 検証（openVerifiedTempRoot）と fd 経由の削除は冗長な関係にある（変異検証で実測）:
// 検証が先に弾くので、削除だけを os.RemoveAll に戻す変異は現行のテストでは素通りする。
// fd 経由が単独で守るのは「検証を通ってから削除するまでの差し替え」だけで、これは
// 決定論的なテストを書けていない（残っている未検証リスク）。両方を外す変異は red になる。
//
// 🚨 削除ループはロックを保持したまま回す。先に map を空にしてロックを外すと、
// シグナル経路が「消すものは無い」と判断して os.Exit し、**削除途中のコピーが残る**。
//
// 🚨 登録簿から消すのは**削除に成功したものだけ**。削除前に消すと、失敗したパスは
// 同じプロセスの中で二度と再試行されない（① の defer → エラー経路 → main の defer と
// 複数回呼ばれるのは、再試行の機会でもある）。
func (w *Workspace) RunAllCleanups() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.runAllCleanupsLocked()
}

// shutdownCleanups は②（シグナル経路）の後始末。「終了中」を立ててから全件を消す。
//
// 🚨 「終了中」は後始末と**同じロックの下で**立てる。後始末が終わってから os.Exit までの間に
// main 側が NewTempDir を進めると、作られたコピー（Cookie DB / leveldb）は誰にも消されずに残る。
// 立てた後は NewTempDir が作成そのものを断る（作成と登録は同じロックの下で行う）。
func (w *Workspace) shutdownCleanups() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closing = true
	w.runAllCleanupsLocked()
}

// runAllCleanupsLocked は cleanupMu を保持した状態で呼ぶ。
func (w *Workspace) runAllCleanupsLocked() {
	if len(w.paths) == 0 {
		return
	}

	root := w.tempRoot()
	names := make([]string, 0, len(w.paths))
	byName := make(map[string]string, len(w.paths))
	for p := range w.paths {
		// 想定外の場所が登録されていたら触らない（登録経路は newTempDirWith だけ）。
		//
		// 🚨 これは**冗長**な検査（変異検証で確認）。削除は removeVerified が
		// 検証済み root からの相対名で行うので、作業領域の外を消すことは構造的に起きない。
		// このガードが単独で担っているのは「気づける形にする」ことだけ（警告を出す）。
		// 冗長だからと外すときは、removeVerified が相対名のままかを必ず確かめること。
		if filepath.Dir(p) != root {
			fmt.Fprintf(os.Stderr, "警告: 想定外の後始末対象を無視しました: %s\n", p)
			continue
		}
		names = append(names, filepath.Base(p))
		byName[filepath.Base(p)] = p
	}
	removed, err := w.removeVerified(names...)
	for _, name := range removed {
		delete(w.paths, byName[name])
	}
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "警告: 一時コピーを削除できませんでした: %v\n", err)
		}
	}
}

// resetFunc / cleanupFunc は handleCleanupSignal の引数の取り違えを**コンパイルエラー**にする。
//
// 🚨 素の func() のままだと、callsite で reset と cleanup を入れ替えても
// build も全テストも通った（実測）。順序が本質なのに、順序を決める callsite が
// 無検査だった。**callsite で明示変換する**こと（パラメータの型を名前付きにするだけでは、
// 無名の func() から両方へ代入できてしまい効かない）。
type (
	resetFunc   func()
	cleanupFunc func()
)

// cleanupSignals は②が捕まえるシグナル。
var cleanupSignals = []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT}

// InstallCleanupOnSignal は②を仕掛ける。main の先頭で 1 回だけ呼ぶ。
//
// 捕まえるのは「人が実際に送れて、プロセスを終わらせるシグナル」:
//
//	SIGINT  Ctrl-C
//	SIGTERM kill の既定
//	SIGHUP  端末が閉じた
//	SIGQUIT Ctrl-\
//
// 🚨 SIGQUIT を外さないこと。**鍵盤から届く**ので Ctrl-C と同じ頻度で起こりうるのに、
// 捕まえていないと復号済みの cookie を含むコピーが $TMPDIR に残る（実測で踏んだ）。
// 代償として Go 既定の SIGQUIT（goroutine スタックダンプを出して終了）は無くなるが、
// 資格情報のコピーを残さない方を採る。signal_cleanup_test.go が実際にシグナルを撃って
// 4 つすべてを固定している。
//
// SIGKILL / SIGSTOP は捕まえられない（OS の仕様）。そちらは③（起動時の掃除）が受け持つ。
// SIGABRT は意図的に捕まえない — Go ランタイムが致命的エラーで自分に送るシグナルで、
// 横取りするとクラッシュの報告経路を壊す。
func (w *Workspace) InstallCleanupOnSignal() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, cleanupSignals...)
	go func() {
		sig := <-ch
		handleCleanupSignal(sig,
			resetFunc(func() { signal.Reset(cleanupSignals...) }),
			cleanupFunc(w.shutdownCleanups),
			os.Exit)
	}()
}

// handleCleanupSignal は②の本体。
//
// 🚨 **順序が本質**: 先に既定へ戻してから後始末する。逆にすると、後始末が長引いている間
// どのシグナルでも止められなくなる（Ctrl-C を連打しても効かず、SIGQUIT の脱出口も塞がる）。
// 依存を引数で受けるのは、その順序を実シグナル無しで検査できるようにするため。
func handleCleanupSignal(sig os.Signal, reset resetFunc, cleanup cleanupFunc, exit func(int)) {
	reset()
	cleanup()
	if s, ok := sig.(syscall.Signal); ok {
		exit(128 + int(s)) // シェルの慣習（SIGINT=130 / SIGTERM=143）
		return
	}
	exit(1)
}

// 作業領域は ~/Library/Caches/<app>/extract。
//
// 🚨 $TMPDIR は使わない。未設定時の os.TempDir() は /tmp を返し、/tmp は誰でも書けるので、
// **別ユーザー**が先に同名のシンボリックリンクを作って居座れる（sticky bit は新規作成を妨げない）。
// こちらからは消せないため、資格情報の読み取りが恒久的に壊れる。HOME 配下ならこの形は無い。
//
// 🚨 ただし「環境変数で動かない」わけではない。os.UserCacheDir() は darwin では
// $HOME/Library/Caches を返すので、**HOME を差し替えれば作業領域も破壊的な掃除の母集合も動く**
// （実測。$XDG_CACHE_HOME は darwin では見ない）。HOME 未設定はエラーになる（fail-closed）ので、
// 残る穴は「HOME を任意の場所へ向けられる呼び出し側」だけ。これは $TMPDIR と同じ性質で、
// $TMPDIR を捨てた理由は上の「別ユーザーが居座れる」ほうだけが根拠になる。
// 相対パスの HOME（cwd 依存になる）は tempRootParent が拒否する。
const tempRootName = "extract"

// staleAge はこれより古い残骸を、pid の生死に関わらず消す閾値。
//
// 🚨 pid の生死だけで判定すると、**番号が再利用された残骸は永久に消えない**。
// また $TMPDIR 時代は OS（dirhelper）が数日で回収していたが、~/Library/Caches には
// それに相当する仕組みを見つけられなかったので、寿命の上限は自分で持つ必要がある。
const staleAge = 7 * 24 * time.Hour

// tempRootParent は作業領域の親（~/Library/Caches/<app>）を返す。
func (w *Workspace) tempRootParent() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("キャッシュディレクトリを決められません: %w", err)
	}
	// 🚨 相対パスを弾く。HOME が相対だと作業領域が cwd 依存になり、
	// 「カレントディレクトリに一切依存しない」という前提が崩れる（実測: HOME="." で再現）。
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("キャッシュディレクトリが絶対パスではありません（HOME を確認してください）: %s", dir)
	}
	return filepath.Join(dir, w.app), nil
}

// tempRoot は作業領域のパスを返す（決められないときは空文字）。
func (w *Workspace) tempRoot() string {
	parent, err := w.tempRootParent()
	if err != nil {
		return ""
	}
	return filepath.Join(parent, tempRootName)
}

// openVerifiedChild は parent の直下の name を**検証したうえで**開く。
//
// 検証の中身と、それぞれが何を防いでいるか:
//
//	Lstat で symlink を拒否   os.Root は「ルート内に留まる**相対**シンボリックリンクは追う」
//	                          （絶対リンクだけを拒否する。実測で確認した）
//	SameFile で同一性を確認   Lstat した実体と、実際に開いた実体が同じであることを見る。
//	                          名前を 2 回解決することによる差し替えの窓を閉じる
//	uid を確認（fail-closed） 所有者が自分でなければ使わない。型アサーションに失敗したら
//	                          「判定不能」なので拒否する（素通りさせない）
//
// 🚨 最初の 2 つは**冗長**な関係にある（変異検証で実測）: symlink を張られた場合、
// Lstat はリンク自身・Stat(".") はリンク先を指すので、symlink 判定を外しても
// SameFile 側が食い違いを検出して拒否する。片方だけを外す変異は素通りするため、
// 外すときは「もう片方が本当に同じものを守るか」を確かめること。意図的に両方残す
// （symlink 判定は意図が読めるエラーメッセージを出せる、という別の役目もある）。
//
// ディレクトリかどうかは別途見ない — 通常ファイルに対して OpenRoot が
// "not a directory" を返すため（実測）。冗長な検査は置かない。
func openVerifiedChild(parent *os.Root, name, display string, uid int, afterLstat func(string)) (*os.Root, error) {
	// Lstat なのでシンボリックリンクを追わない（追ってから調べたのでは遅い）。
	want, err := parent.Lstat(name)
	if err != nil {
		return nil, err // 存在しない = まだ一度も使っていない。これは正常
	}
	if want.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s がシンボリックリンクです（削除してください）: %s", name, display)
	}
	if afterLstat != nil {
		afterLstat(name) // テストが「検証中の差し替え」を再現するための窓
	}
	child, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	got, err := child.Stat(".")
	if err != nil {
		_ = child.Close()
		return nil, err
	}
	if !os.SameFile(want, got) {
		_ = child.Close()
		return nil, fmt.Errorf("%s が検証中に差し替えられました: %s", name, display)
	}
	// 🚨 実測（darwin / os パッケージ由来の FileInfo）ではこの分岐に到達しない
	// （動的型は常に *syscall.Stat_t）。つまりテストでは守れない。それでも
	// 「判定不能なら拒否」を置くのは、別 platform・別実装で型が変わったときに
	// 黙って素通りさせないため。テストが無いことを承知で残している。
	st, ok := got.Sys().(*syscall.Stat_t)
	if !ok {
		_ = child.Close()
		return nil, fmt.Errorf("%s の所有者を判定できません: %s", name, display)
	}
	if int(st.Uid) != uid {
		_ = child.Close()
		return nil, fmt.Errorf("%s の所有者が自分ではありません（削除してください）: %s", name, display)
	}
	// 🚨 所有者が自分でも、group / other に権限があれば使わない。作成時の 0700 は既存の
	// ディレクトリには効かない（MkdirAll は既存を直さない）。他人が書けると、作った <pid>-* を
	// symlink に差し替えられ、コピーをリンク先へ書かせられる（敵対的レビューの指摘）。
	if got.Mode().Perm()&0o077 != 0 {
		_ = child.Close()
		return nil, fmt.Errorf("%s が自分以外にも開かれています（%v。chmod 700 するか削除してください）: %s", name, got.Mode().Perm(), display)
	}
	return child, nil
}

// openVerifiedTempRoot は作業領域を**検証したうえで**ディレクトリ fd として開く。
// 検証に失敗したら開かない（呼び出し側は 1 件も触らないこと）。何も作らない。
//
// 🚨 破壊的操作を行う側がこの検証を通ること。検証が作成側にしか無いと、
// 掃除が作業領域のシンボリックリンクを追って**リンク先を再帰削除する**（実証済み）。
//
// 🚨 検証は**最後の 1 コンポーネントだけでは足りない**。途中のディレクトリ
// （~/Library/Caches/<app>）を差し替えれば、検証済みの root ごと任意の場所へ移せる。
// os.UserCacheDir() を起点に 1 コンポーネントずつ降りる。
func (w *Workspace) openVerifiedTempRoot() (*os.Root, error) {
	return w.openVerifiedTempRootWith(os.Getuid(), nil)
}

// openVerifiedTempRootWith は所有者として扱う uid と、検証中に割り込む関数を受ける。
//
// 🚨 テスト専用の引数だが、**グローバル変数にはしない**。パッケージ変数の seam は
// (a) 同期が無いので、シグナル経路をインプロセスで検査した瞬間にデータレースになる
// (b) 「seam を消す正当な整理」が変異検証で赤くなり、red が退行の証拠として読めなくなる
// という 2 つの穴を作る（どちらも実測で確認）。引数ならどちらも構造的に起きない。
func (w *Workspace) openVerifiedTempRootWith(uid int, afterLstat func(string)) (*os.Root, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("キャッシュディレクトリを決められません: %w", err)
	}
	if !filepath.IsAbs(base) {
		return nil, fmt.Errorf("キャッシュディレクトリが絶対パスではありません（HOME を確認してください）: %s", base)
	}
	r, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	// 🚨 display はホップごとに進める。進めないと 2 ホップ目のエラーが
	// 実在しないパスを表示し、差し替えを報告する画面が嘘をつく。
	path := base
	for _, name := range []string{w.app, tempRootName} {
		path = filepath.Join(path, name)
		child, err := openVerifiedChild(r, name, path, uid, afterLstat)
		_ = r.Close() // 子は自前の fd を持つので、親は閉じてよい
		if err != nil {
			return nil, err
		}
		r = child
	}
	return r, nil
}

// removeVerified は検証済みの作業領域から names（いずれも 1 コンポーネント）を削除する。
// 戻り値 removed は削除に成功した（= もう存在しない）名前。
//
// 🚨 削除の実装をここ 1 本に寄せる。①（defer）と②（シグナル経路）で別々に書くと、
// 片方だけがパス文字列の os.RemoveAll のまま取り残される（実際に起きた）。
func (w *Workspace) removeVerified(names ...string) (removed []string, err error) {
	if len(names) == 0 {
		return nil, nil
	}
	r, err := w.openVerifiedTempRoot()
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	var firstErr error
	for _, name := range names {
		if name != filepath.Base(name) || name == "." || name == ".." {
			// 呼び出し側の組み立てミス。作業領域の外を指しうるので触らない。
			if firstErr == nil {
				firstErr = fmt.Errorf("削除対象が作業領域の直下ではありません: %s", name)
			}
			continue
		}
		if err := r.RemoveAll(name); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		removed = append(removed, name)
	}
	return removed, firstErr
}

// ensureTempRoot は作業領域を 0700 で用意し、検証して返す。
func (w *Workspace) ensureTempRoot() (string, error) {
	root := w.tempRoot()
	if root == "" {
		return "", errors.New("作業領域のパスを決められません")
	}
	// 🚨 0700。作業領域には復号済みの資格情報のコピーが入る。
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	r, err := w.openVerifiedTempRoot()
	if err != nil {
		return "", err
	}
	_ = r.Close()
	return root, nil
}

// SweepStaleTempDirs は③。過去の実行が SIGKILL 等で残したものだけを消す。
// 条件に 1 つでも合わなければ触らない（判断できないものは残す方へ倒す）。
func (w *Workspace) SweepStaleTempDirs() {
	w.sweepStaleTempDirs(os.Getuid(), nil)
}

// sweepStaleTempDirs は SweepStaleTempDirs の引数版（テストが所有者と割り込みを差し替える）。
func (w *Workspace) sweepStaleTempDirs(uid int, afterLstat func(string)) {
	r, err := w.openVerifiedTempRootWith(uid, afterLstat)
	if err != nil {
		// 存在しない = まだ何も残していない（正常）。それ以外は検証に失敗したということなので、
		// **1 件も消さずに**黙って引き下がる。ただし沈黙はしない（何が起きたか分からなくなる）。
		if !errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "警告: 作業領域を検証できないため掃除を中止しました: %v\n", err)
		}
		return
	}
	defer func() { _ = r.Close() }()

	dir, err := r.Open(".")
	if err != nil {
		return
	}
	entries, err := dir.ReadDir(-1)
	_ = dir.Close()
	if err != nil {
		return
	}

	self := os.Getpid()
	for _, e := range entries {
		if !e.IsDir() {
			continue // ディレクトリ以外は対象外
		}
		pid, ok := pidFromTempDirName(e.Name())
		if !ok || pid == self {
			// 名前の形が違う / 自分のものは触らない。
			// 🚨 この !ok と processAlive の pid<=0 ガードは現状「互いを覆う」冗長な関係にある
			//（形が違う → pid 0 → 判定不能 → 消さない）。片方を外す変異は素通りするので、
			// 外すときは「もう片方が本当に同じものを守るか」を確かめること。意図的に両方残す。
			continue
		}
		// 🚨 pid の生死だけで判定しない。番号が再利用されると永久に消えなくなる。
		// 十分に古いものは「持ち主はもう居ない」とみなす（名前の条件は緩めない）。
		if processAlive(pid) && !isStale(r, e.Name()) {
			continue // 生きているプロセスの、新しいものは触らない（並行実行）
		}
		// 🚨 削除は fd 起点（r.RemoveAll）で行う。パスを組み直して os.RemoveAll に渡すと、
		// 検証した root とは別の場所を消しうる。
		if err := r.RemoveAll(e.Name()); err != nil {
			// 失敗を無音にしない（残り続けている事実に気づけなくなる）。
			fmt.Fprintf(os.Stderr, "警告: 一時コピーの残骸を削除できませんでした (%s): %v\n", e.Name(), err)
		}
	}
}

// isStale は root 直下の name が staleAge より古いかを返す。判定できないときは false（消さない方へ倒す）。
//
// 🚨 os.DirEntry.Info() を使わないこと。go1.25.0 では、入れ子に開いた Root から読んだ DirEntry の
// Info() が cwd 相対のパス（"extract/./<name>"）を lstat して失敗し、古い残骸が永久に消えなくなる
// （CI の go1.25.0 で TestSweepRemovesStaleEntriesEvenIfPidAlive が落ちて判明。go1.26 では再現しない）。
// 検証済みの root を起点に Lstat する。
func isStale(r *os.Root, name string) bool {
	info, err := r.Lstat(name)
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) > staleAge
}

// pidFromTempDirName は "<pid>-<乱数>" 形式のディレクトリ名から pid を取り出す。
// この形式でないものは対象外（false を返す）。
func pidFromTempDirName(name string) (int, bool) {
	i := strings.IndexByte(name, '-')
	if i <= 0 {
		return 0, false
	}
	head := name[:i]
	pid, err := strconv.Atoi(head)
	if err != nil || pid <= 0 {
		return 0, false
	}
	// 🚨 往復一致を要求する。Atoi は "+1" / "007" / "0000000000000001" を受けてしまい、
	// 自分が作らない名前まで掃除の母集合に入る（MkdirTemp が作るのは十進・前置ゼロ無し）。
	if strconv.Itoa(pid) != head {
		return 0, false
	}
	return pid, true
}

// processAlive は pid のプロセスが生きているかを返す。
// 判断できないとき（権限が無い等）は「生きている」に倒す = 消さない方へ倒す。
//
// 🚨 os.FindProcess + Process.Signal を使わないこと。消えたプロセスに対して
// ESRCH ではなく os.ErrProcessDone を返すため、ESRCH だけを見る判定は
// 「常に生きている」に落ちて掃除が 1 件も走らなくなる。
// kill(2) を直接呼び、errno をそのまま判定する。
func processAlive(pid int) bool {
	// 🚨 pid 0 / 負値を kill(2) に渡さない。0 は「自分のプロセスグループ全体」、
	// 負値は「プロセスグループ指定」を意味し、生死判定にならない（成功して
	// 「生きている」に見える）。ここでは判定不能として扱い、消さない方へ倒す。
	if pid <= 0 {
		return true
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true // シグナルを送れた = 生きている
	}
	if errors.Is(err, syscall.ESRCH) {
		return false // そんなプロセスは無い = 死んでいる
	}
	return true // EPERM 等、判断できないときは消さない
}

// tempRootError は作業領域の異常を、場所と対処つきの EnvError にする。
func (w *Workspace) tempRootError(err error) error {
	where := w.tempRoot()
	if where == "" {
		where = "~/Library/Caches/" + w.app + "/" + tempRootName
	}
	return &EnvError{
		Msg: fmt.Sprintf(
			"作業領域（%s）を用意できませんでした。\n"+
				"  このディレクトリと親（%s）が、自分の所有する通常のディレクトリ（シンボリックリンクではない）であることを確認してください。\n"+
				"  心当たりが無ければ、中身ごと削除して再実行すると作り直されます。",
			where, filepath.Dir(where)),
		Err: err,
	}
}

// NewTempDir は「①②③すべての対象になる」一時ディレクトリを作る。
// 戻り値の cleanup は ① として defer で呼ぶこと。
//
// Cookie DB と Local Storage(leveldb) の両方がこの関数を通る。片方だけ別経路で
// os.MkdirTemp すると、その残骸は②③のどちらにも拾われない。
func (w *Workspace) NewTempDir() (dir string, cleanup func(), err error) {
	return w.newTempDirWith(nil)
}

// newTempDirWith は NewTempDir の本体。afterMkdir はテストが「作成と登録の間」に
// 割り込むための窓（本番は nil）。グローバル変数にしない理由は openVerifiedTempRootWith と同じ。
//
// 🚨 作成（MkdirTemp）と登録は**同じロックの下で**行う。ロックの外で作ってから登録すると、
// その間に②が後始末を終えて os.Exit へ進んだとき、作ったディレクトリは誰にも消されない。
// 終了処理中（cleanupClosing）なら作らずに断る。
//
// 🚨 失敗はすべて EnvError で返す。作業領域は全プロファイル共通なので、ここが壊れている
// （シンボリックリンク・所有者違い・作成失敗・終了処理中）とどのプロファイルを試しても同じ。
// 素のエラーのままだと、探索が「そのプロファイルには無い」として次へ進み、最後に
// 「Chrome でログインしてから」という無関係な案内に化ける（setup で再現した）。
func (w *Workspace) newTempDirWith(afterMkdir func()) (dir string, cleanup func(), err error) {
	root, err := w.ensureTempRoot()
	if err != nil {
		return "", nil, w.tempRootError(err)
	}
	w.mu.Lock()
	if w.closing {
		w.mu.Unlock()
		return "", nil, w.tempRootError(errCleanupClosing)
	}
	// ディレクトリ名に pid を埋める（③がこれを見て「生きていない実行の残骸」を判定する）。
	d, err := os.MkdirTemp(root, fmt.Sprintf("%d-", os.Getpid()))
	if err != nil {
		w.mu.Unlock()
		return "", nil, w.tempRootError(err)
	}
	if afterMkdir != nil {
		afterMkdir()
	}
	w.paths[d] = struct{}{}
	w.mu.Unlock()
	// 🚨 ①も②③と同じ「検証済みの作業領域を開いた fd」経由で消す。
	// ここだけパス文字列の os.RemoveAll に戻すと、作業領域を差し替えられたときに
	// リンク先を消す経路が復活する（①の窓は「作業中ずっと」なので最も広い）。
	return d, func() { _, _ = w.removeVerified(filepath.Base(d)) }, nil
}
