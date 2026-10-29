// Package lanmgr は mac-lan-manager の画面を持つ。
//
// 画面は一覧 (管理対象 / 未管理 / この Mac のタブ)・詳細カード・設定・この Mac の IP と有線の名前のコピー・初期設定 (setup.go)・
// メッセージ一覧 (msglog.go)。有線の名前を引いて宛先に使う (wiredname.go)。
//
// 単体の bin/mac-lan-manager と glogx の L の板の両方から使う (treefiler の filer と同じ分担)。描画の tick は張らない:
// 時間は呼び出し側が Advance(now) で進め、Busy の間は遅い周期で呼ぶ。外へ出す口は New / Resize / HandleKey / HandleInput / Advance / View / Changed / Reload /
// Busy / OwnsKeys / Binds / CaretPos / Paste / TakeExec / ExecDone / TakeNotices / Close / Place / RestorePlace / Report。
// キーの語彙は docs/glogx-ui-guide.md に従う。
package lanmgr

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jiikko/dotfiles/src/termsafe"
	"github.com/jiikko/dotfiles/src/tuikit/confirm"
	"github.com/jiikko/dotfiles/src/tuikit/lineedit"
	"github.com/jiikko/dotfiles/src/tuikit/listnav"

	"mac-lan-manager/discover"
	"mac-lan-manager/ledger"
	"mac-lan-manager/netinfo"
	"mac-lan-manager/prefs"
	"mac-lan-manager/reach"
	"mac-lan-manager/setup"
	"mac-lan-manager/speedtest"
	"mac-lan-manager/sshdest"
)

// Action は HandleInput の結果 (呼び出し側がすること)。
type Action int

// HandleInput の結果。
const (
	None Action = iota // 画面の中で済んだ
	Quit               // 閉じる (単体なら終了、glogx の板なら一覧へ戻る)
	Exec               // 端末を明け渡してプロセスを起こす (中身は TakeExec)
)

// ExecRequest は呼び出し側に起こしてほしいプロセス (前面で端末を明け渡す)。
type ExecRequest struct {
	Argv []string
	// ExitIsFailure は終了コード ≠ 0 を失敗として知らせるか。ssh は相手のシェルの終了コードで抜けるので失敗と言わない。open は
	// 開けなかったときに rc=1 で戻り、画面は TUI へすぐ戻るので理由も消える (知らせないと何も起きなかったように見えた)
	ExitIsFailure bool
	// Banner は端末を渡す前に出す板 (初期設定の手順。何を起こし、何を聞かれるか)。空なら出さない
	Banner string
}

// RunArgv は呼び出し側が実際に起こす argv。Banner があれば、画面を消して板を出してから Argv を起こし、失敗したら終了コードを出して
// Enter を待つ (端末を渡すと TUI の枠が消えて素のプロンプトだけになり、失敗の出力も TUI に戻った瞬間に消えた。ユーザー報告)。
// 単体の main と glogx の板は、どちらもこれを起こす
func (r ExecRequest) RunArgv() []string {
	if r.Banner == "" {
		return r.Argv
	}
	const script = `printf '\033[2J\033[H%s\n\n' "$1"; shift; "$@"; rc=$?; ` +
		`if [ "$rc" -ne 0 ]; then printf '\n失敗した (終了コード %d)。Enter で mac-lan-manager に戻る ' "$rc"; read -r _; fi; exit "$rc"`
	return append([]string{"/bin/sh", "-c", script, "mac-lan-manager", r.Banner}, r.Argv...)
}

// Notice は呼び出し側が右下の toast に出す知らせ。OK=false は「効かなかった」(✗ 赤。ガイド §9)。Info は進行中の知らせ
// (… シアン。OK より先に見る。始めたことを ✓ 緑で出すと、終わったように見える)。
type Notice struct {
	Text string
	OK   bool
	Info bool
}

// Scanner は Bonjour の走査の口 (discover.Scanner。テストで差し替える)。
type Scanner interface {
	Snapshot() []discover.Peer
	Busy(now time.Time) bool
	Changed() <-chan struct{} // 結果が変わった合図 (呼び出し側はこれを待って Advance を呼ぶ。後から見つかった Mac を出すため)
	Err() error               // 走査そのものの失敗 (dns-sd を起こせない等。0 台の「居ない」と「探せていない」を分ける)
	// Close は走査を止め、Changed を閉じる (待っている側は ok=false で抜ける)。🚨 閉じない実装を足すと、glogx の L の開き直しや r の
	// たびに古いチャネルを待つ goroutine が 1 本ずつ残る (lan_view.go の rearm と main.go の watch が ok=false で抜けるのを当てにする。
	// discover.Scanner は TestCloseClosesChanged で守る)。何度呼んでもよい
	Close()
}

// Options は外とのつなぎ。nil のものは使わない (本物のつなぎは live パッケージ。テストは偽物を渡す)。
type Options struct {
	ReadNet func(ctx context.Context) (netinfo.Snapshot, error) // この Mac のネットワーク
	// LookupHost は名前を引く (有線の名前が今その Mac の内部 IP に引けるか。wiredname.go。nil なら引かない = いつも IP で繋ぐ)
	LookupHost func(ctx context.Context, name string) ([]string, error)
	// RunQuiet は端末を渡さずにコマンドを起こす (台帳から外した相手の名前の広告を止める ssh。nil なら止めない)
	RunQuiet func(ctx context.Context, argv []string) error
	// ReadAdvert はこの Mac の有線の名前の広告の状態 (setup.ReadNameState。nil なら読まない = 初期設定の手順は「読めていない」で止まる)
	ReadAdvert func(ctx context.Context) (setup.NameState, error)
	LocalHost  func() string // この Mac の Bonjour の名前 ("kojiM3MacBook-Pro.local")
	StartScan  func(now time.Time) Scanner
	IfName     func(index int) string // I/F の番号 → 名前 (dns-sd の if 列を、この Mac の I/F に照らす)
	Ledger     ledger.Store
	PrefsPath  string
	SavePrefs  func(path string, p prefs.Prefs) error
	// Log は操作のログの 1 行を受け取る (oplog.go。nil なら記録しない。live が ~/Library/Logs/mac-lan-manager.log へ書く)
	Log  func(line string)
	Copy func(s string) error // クリップボードへ
	Now  func() time.Time
	User string // 台帳にユーザー名が無い相手に使う推測の名前 (live は $USER を入れる。環境を読むのはつなぎの側)
	// Observe は相手を ssh で読む (段 4 の初期設定。読むだけ)。live は setup.ObservePeer を本物の ssh で走らせる。nil なら観測しない
	Observe func(ctx context.Context, d sshdest.Dest) setup.PeerNet
	// SpeedTest は相手との通信速度を測る (帯域を使い切るので確認の板の y の後だけ。speed.go)。now は時計。nil なら測らない
	SpeedTest func(ctx context.Context, d sshdest.Dest, now func() time.Time) speedtest.Result
	// Reach は名前ごとに疎通を確かめる (p。reach.go)。nil なら確かめない
	Reach func(ctx context.Context, names []string) []reach.Name
	// HostHints は最下行のキーの案内を呼び出し側が描く (glogx は枠の外の最下行に Hints を出す)。真なら View は案内の行と
	// その上の空行を描かず、その分を本文に回す
	HostHints bool
	// Passive は画面を出さないサブコマンド (dump / speedtest / ping) の Model。Advance が自動で起こす仕事 (一覧を開いたときの
	// 疎通の確認) と操作のログを止める。Advance に自動の仕事を足すときはこれを見る (呼び出し側に Options を 1 つずつ nil にさせると、
	// 足した仕事が dump の中でも走る)
	Passive bool
}

type screen int

const (
	scList screen = iota
	scSettings
	scSetup // 相手 1 台の初期設定 (段 4。setup.go)
)

type overlay int

const (
	ovNone    overlay = iota
	ovConfirm         // y/N (台帳への登録・削除)
	ovCopyIP          // この Mac の IP のどれをコピーするか
	ovLog             // メッセージ一覧 (msglog.go)
	ovAlias           // 台帳の欄 (別名 / ユーザー名) の入力欄
	ovHelp            // 全部のキー
	ovSpeed           // 通信速度を測る経路を選んで y (speed.go)
)

// Model は画面の状態。
type Model struct {
	o           Options
	w, h        int
	now         time.Time
	sc          screen
	tab         int          // tabManaged / tabUnmanaged / tabSelf
	cur         [numTabs]int // タブごとのカーソル
	listOff     [numTabs]int // タブごとの一覧の窓の先頭 (描画で WindowOffset から導く)
	listTop     int          // 一覧のタブの下の画面上の行 (最後に描いたとき。初期設定のタイルを重ねる起点 view.go)
	listRows    int          // 一覧の窓の行数 (最後に描いたとき。半ページ送りの量)
	tooShort    bool         // 一覧の板に 1 行も入らない高さ (描いたとき)
	prefsUnread bool         // 設定ファイルが読めなかった (既定で描き、保存しない)
	small       bool         // 最後に描いた画面が幅・高さの足りない画面 (抜け方だけ)。HandleInput は q しか受けない
	// detailOpen は一覧の上に重ねた詳細のパネル (一覧で Enter。中で Enter を押すと初期設定)。一覧の下の詳細カードは高さが足りないと
	// 1 行に畳まれるので、どの高さでも詳細を見られるようにする。detailOff はパネルの中の送り、detailMax はその上限と、
	// 最後に描いたパネルの位置と高さ (view.go の detailPanel が描くたびに決める。キーはそれで送りを止める)
	detailOpen bool
	// detailHost / detailSelf はパネルを開いた行の Mac。行の並びが変わっても (別名の付け替え・裏の読み直し)、パネルは同じ Mac を
	// 指し続ける: キーのたびにカーソルをこの Mac へ戻し、見つからなければ閉じる (添字のままだと、黙って隣の Mac に入れ替わった)
	detailHost string
	detailSelf bool
	detailOff  int
	detailMax  int
	panelTop   int
	su         *setupState // 初期設定の画面 (開いていなければ nil)
	// pending は相手ごとの確認待ち (固定 IP にした後、新しい IP で確認するまで)。claimed はこの起動で相手に割り当てた番号
	// (台帳に載る前の新しい IP を、次の相手の番号の計算で避ける)。どちらも画面を閉じても残す
	pending map[string]*pendingConfirm
	claimed map[string]string
	// unconf は「固定 IP にしたが、まだ確認していない」相手 (値は期限)。ある間は台帳の手順を断る
	unconf map[string]time.Time
	// tried は相手ごとに固定 IP にしようとした番号の集合 (確認した番号・期限の後の観測で固定が見えた番号だけ外す。上書きすると、
	// 2 回目の試行で 1 回目の番号の守りが消えた)。期限で unconf が解けた後も残す: Bonjour は相手が戻っても A レコードを
	// 引き直さず (discover の resolve はサービスの出し直しのときだけ引く)、一覧の行に古い番号が残る
	tried map[string]map[string]bool
	// confirmed は期限内に確認した新しい IP (有線だけの相手を開き直したときの宛先)
	confirmed map[string]string
	// setupExec は初期設定の手順として渡したプロセスの印 (ExecDone が setupExecDone へ渡す。nil は手順ではないプロセス)
	setupExec *setupExec
	setC      int // 設定の画面のカーソル
	ov        overlay
	ask       *ask
	copyC     int

	filter    lineedit.Line
	filtering bool `layer:"filter"`
	alias     lineedit.Line
	edit      hostEdit // 入力欄で書き換えている台帳の欄 (別名 / ユーザー名)
	// log はメッセージ一覧 (古い順。msglog.go)。logC は一覧のカーソル (0 が最新)、logRows / logOffPrev は描いた窓
	log                       []logEntry
	logC, logRows, logOffPrev int
	band                      lineedit.Line
	bandEdit                  bool `layer:"band"`

	self    netinfo.Snapshot
	selfErr error
	// selfAdvert はこの Mac の有線の名前の広告の状態 (selfAdvertKnown のときだけ有効。網と一緒に読む)
	selfAdvert      setup.NameState
	selfAdvertKnown bool
	bg              []<-chan Notice                   // 裏で走らせている後始末 (台帳から外した相手の名前の広告を止める) の結果
	speedRun        *speedRun                         // 測っている途中の通信速度 (speed.go。同時に 1 本)
	speed           map[string]map[string]speedResult // 相手・経路ごとの最後の通信速度 (台帳の鍵 → IP → 結果)
	speedAsk        *speedAsk                         // 経路を選ぶ板 (ovSpeed の間だけ)
	reachRuns       map[string]*reachRun              // 疎通を確かめている途中の回 (reach.go。台帳の鍵 → 回。相手ごとに 1 本)
	reach           map[string]reachResult            // 相手ごとの最後の疎通の結果 (台帳の鍵 → 結果)
	// wired は有線の名前を引いた結果 (台帳の鍵 → 結果。wiredname.go)。wiredCh / wiredWait は引いている途中の回
	wired     map[string]wiredLookup
	wiredCh   <-chan wiredResult
	wiredWait int
	host      string // この Mac のホスト名 (台帳の鍵の形)
	hosts     []ledger.Host
	peers     []discover.Peer
	scan      Scanner
	scanErr   error // 走査の失敗 (見せた後も持つ。Reload で消す)
	// cursorLost はこのキーの入口の取り込みで、画面で見ていた Mac が一覧から消えた (行に効くキーを断る)
	cursorLost bool
	wantHost   string         // RestorePlace で戻したいカーソルの Mac (見つかるまで持つ。走査が終われば諦める)
	netCh      <-chan netLoad // 裏で読んでいるこの Mac のネットワーク (nil = 読んでいない)
	netReady   bool           // この Mac のネットワークを一度は読み終えた (読み直しの間は前の値のまま描く)
	// selfOK は self が使える (サービスまで読めた) か。読めなかった回は前の self を残す: 空の Snapshot で上書きすると、相手の IP が
	// 全部「その他」に落ち、s が VPN の IP へ繋いだ
	selfOK    bool
	netCancel context.CancelFunc // その取り消し
	pf        prefs.Prefs

	notices           []Notice
	exec              *ExecRequest
	execExitIsFailure bool // 最後に渡したプロセスの終了コードを失敗と見るか (ExecDone が読む)

	// 入力欄に端末のカーソルを置く位置 (View が描くときに控える。-1 は入力欄が無い)
	caretFX, caretFY   int // 絞り込み
	caretMX, caretMY   int // 板の中の入力欄 (別名・帯)
	bandCol, bandRow   int // 設定の板の帯の入力欄 (桁と、板の中の行)
	setLabelW          int // 設定の板の名前の列の幅 (描いたとき。キャレットの桁に使う)
	setOff, setOffPrev int // 設定の板の窓の先頭 (描いたとき / 前に描いたとき)
}

// ask は y/N の確認の板 (知らないキーはすべて取り消し。ガイド §4)。
type ask struct {
	title string
	body  []string
	yes   func() Notice
	// exec は y で端末を明け渡して起こすプロセス (初期設定の手順。yes の代わり)。step / static は戻ったときに setupExecDone へ渡す
	exec   *ExecRequest
	step   setup.StepID
	static *setup.StaticIP
	// auto は読み込み中に押した x の予約で、読み終えたときに自動で出した板。Enter では実行しない (進捗のモーダルの間に押し続けた
	// Enter が、裏で開いた板をそのまま実行した。この Mac の固定は自動では戻らない)
	auto bool
	// selfOnly は「この Mac」の詳細から出した、この Mac だけの手順 (askSelfStatic)。相手の初期設定の画面の外でも起こしてよい。enter では実行しない
	selfOnly bool
}

// New は画面を作り、この Mac のネットワーク・台帳・設定を読み、Bonjour の走査を起こす。
func New(o Options) *Model {
	if o.Passive {
		o.Log = nil
	}
	if !ledger.ValidUser(o.User) { // $USER は検査されていない値。空・使えない名前なら「分からない」にする (空のまま ssh すると宛先が "@<IP>" になった)
		o.User = ""
	}
	if o.Now == nil {
		o.Now = time.Now //nolint:forbidigo // 注入しないときの既定値 (ここだけが「今」を直に読む)
	}
	m := &Model{o: o, now: o.Now(), w: 120, h: 40, pending: map[string]*pendingConfirm{}, claimed: map[string]string{}, unconf: map[string]time.Time{}, tried: map[string]map[string]bool{}, confirmed: map[string]string{}}
	m.Reload()
	return m
}

// Reload はこの Mac のネットワーク・台帳・設定を読み直し、走査を起こし直す (r と、glogx の板を開き直したとき。Close の後でも使える)。
func (m *Model) Reload() {
	m.logf("reload")
	m.forgetReach()
	// 走査を起こし直すと行が空に近い所から増え直すので、カーソルの Mac を戻したい Mac として持つ (添字のままだと、増えた行の分だけ
	// 別の Mac に移り、s / o がそのまま別の相手へ繋いだ)
	if m.wantHost == "" {
		m.wantHost = m.cursorHost()
	}
	// 走査の settle の起点は今。最後の Advance の時刻のままだと、閉じていた間の分だけ settle が過ぎた扱いになり、開き直した直後に
	// 走査中の印が消えて tick が止まる
	m.now = m.o.Now()
	// ホスト名は同期に読む (scutil 1 回。実測 12 ms)。裏へ回すと読み終わるまで「この Mac」の照合ができず、台帳と Bonjour の自分の
	// 行が別の Mac として出て、そこへ ssh できた
	// 読めていれば読み直さない (ホスト名はまず変わらない。configd が詰まると scutil は上限の 3 秒まで戻らず、L を押すたびに止まる)
	if m.o.LocalHost != nil && m.host == "" {
		m.host = ledger.Key(m.o.LocalHost())
	}
	m.startNetLoad()
	if m.o.Ledger != nil {
		// 読めなければ前に読んだ一覧を残す (nil で上書きすると、管理対象の Mac が全部「未管理」に化けた)
		if hs, err := m.o.Ledger.Load(); err != nil {
			m.note("台帳が読めない: "+err.Error(), false)
		} else {
			m.setHosts(hs, true)
		}
	}
	m.pf, m.prefsUnread = prefs.Default(), false
	if m.o.PrefsPath != "" {
		if p, err := prefs.Load(m.o.PrefsPath); err != nil {
			// 読めない (権限など) 間は既定で描くが保存しない: 1 つ切り替えると、読めなかった本物の設定を既定で上書きした
			m.prefsUnread = true
			m.note("設定が読めない (読めるまで変更を保存しない): "+err.Error(), false)
		} else {
			m.pf = p
		}
	}
	if m.scan != nil {
		m.scan.Close()
	}
	m.scanErr = nil
	if m.o.StartScan != nil {
		m.scan = m.o.StartScan(m.now)
		m.peers = m.scan.Snapshot()
	}
}

// netLoad はこの Mac のネットワークを裏で読んだ結果。
type netLoad struct {
	self      netinfo.Snapshot
	err       error
	advert    setup.NameState
	advertErr error // 読めなかった (nil で advert が有効)
}

// startNetLoad はこの Mac のネットワーク (networksetup / ifconfig を順に起こす。実測 約 420 ms) を裏で読み始める。
// 🚨 Update の中で同期に読まない: glogx の L を押すたびに画面がその間止まった (ユーザー報告 2026-10-15)。読み終わるまでは前に読んだ値の
// ままで描き、Busy を立てて呼び出し側の tick を回させ、Advance で取り込む。読み直しが重なったら前の読み込みは取り消して結果も捨てる。
func (m *Model) startNetLoad() {
	m.stopNetLoad()
	readNet, readAdvert := m.o.ReadNet, m.o.ReadAdvert
	if readNet == nil {
		m.netReady, m.selfOK = true, true // 読むものが無い (テスト・ネットワークを見ない呼び出し側)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	ch := make(chan netLoad, 1)
	m.netCh, m.netCancel = ch, cancel
	go func() {
		defer cancel()
		var r netLoad
		r.self, r.err = readNet(ctx)
		r.advertErr = errors.New("読む口が無い")
		if readAdvert != nil {
			r.advert, r.advertErr = readAdvert(ctx)
		}
		ch <- r // 容量 1 なので、読む側が捨てていても止まらない
	}()
}

// stopNetLoad は裏の読み込みを取り消す (結果は待たない。読み込みは 5 秒で必ず終わる)。
func (m *Model) stopNetLoad() {
	if m.netCancel != nil {
		m.netCancel()
	}
	m.netCh, m.netCancel = nil, nil
}

// takeNetLoad は裏の読み込みが終わっていれば取り込む (待たない)。
func (m *Model) takeNetLoad() {
	select {
	case r := <-m.netCh:
		m.applyNetLoad(r)
	default:
	}
}

func (m *Model) applyNetLoad(r netLoad) {
	// I/F の IP を読み切れなかった回 (ErrIPsIncomplete) はサービスまで正しいので採る (相手の分類はサービスだけで決まる)
	if r.err == nil || errors.Is(r.err, netinfo.ErrIPsIncomplete) {
		m.self, m.selfOK = r.self, true
	}
	m.selfErr = r.err
	switch {
	case r.err == nil:
	case errors.Is(r.err, netinfo.ErrIPsIncomplete):
		m.note("この Mac の I/F の IP を読み切れなかった (r で読み直す): "+r.err.Error(), false)
	case m.selfOK:
		m.note("この Mac のネットワークを読めない (前に読んだ値で分ける。r で読み直す): "+r.err.Error(), false)
	default:
		m.note("この Mac のネットワークを読めない (繋ぐ・コピー・登録・初期設定は読めるまで断る。r で読み直す): "+r.err.Error(), false)
	}
	m.selfAdvert, m.selfAdvertKnown = r.advert, r.advertErr == nil
	m.netReady = true
	m.startWiredLookups() // この Mac の内部 IP が分かってから引く (固定かどうかで名前を持つかが決まる。netReady の後: 前だと行の内部 IP が分類されなかった)
	m.netCh, m.netCancel = nil, nil
}

func (m *Model) note(text string, ok bool) {
	m.notices = append(m.notices, Notice{Text: text, OK: ok})
	m.record(text, ok)
}

// noteInfo は進行中の知らせ (始めた・待っている)。メッセージ一覧には成功の色で残す。
func (m *Model) noteInfo(text string) {
	m.notices = append(m.notices, Notice{Text: text, OK: true, Info: true})
	m.record(text, true)
}

// Resize は画面の大きさを伝える。
func (m *Model) Resize(w, h int) {
	m.w, m.h = w, h
	m.listRows = 0 // 窓の行数は次に描くまで分からない (古い値で半ページ送りしない。それまでは枠の内側の高さで送る)
}

// Place は今のタブとカーソルの Mac (glogx が L を出したまま閉じたときに覚え、次の起動で戻す)。
type Place struct {
	Tab  int    `json:"tab"`
	Host string `json:"host"` // 台帳の鍵 (.local のホスト名)。この Mac の行なら空
}

// Place は今の場所。
func (m *Model) Place() Place {
	p := Place{Tab: m.tab}
	if r := m.current(); !r.self {
		p.Host = r.host
	}
	return p
}

// RestorePlace は場所を戻す。カーソルの Mac がまだ一覧に無ければ (Bonjour の走査で後から見つかる)、見つかったときに合わせる。
func (m *Model) RestorePlace(p Place) {
	if p.Tab >= 0 && p.Tab < numTabs {
		m.tab = p.Tab
	}
	m.wantHost = p.Host
	m.seekWantHost()
}

// cursorHost はカーソルのある相手の Mac (この Mac の行・行が無いときは "")。
func (m *Model) cursorHost() string {
	vis := m.visible()
	if len(vis) == 0 {
		return ""
	}
	if r := vis[min(m.cur[m.tab], len(vis)-1)]; !r.self { // current() は添字を詰めて書き換える。毎キー・毎 tick に呼ぶので読むだけにする
		return r.host
	}
	return ""
}

// visibleHosts は今のタブの行の Mac (画面に出ている行の集合)。
func (m *Model) visibleHosts() []string {
	vis := m.visible()
	out := make([]string, len(vis))
	for i, r := range vis {
		out[i] = r.host
	}
	return out
}

// followCursor は行を組み直した後、カーソルを host の行に合わせ直す (行は名前で並ぶので、走査で増えた行の分だけ添字が別の Mac を
// 指した)。居なくなっていれば添字のまま。
func (m *Model) followCursor(host string) bool {
	if host == "" {
		return true
	}
	for i, r := range m.visible() {
		if r.host == host && !r.self {
			m.cur[m.tab] = i
			return true
		}
	}
	return false
}

// seekWantHost は戻したい Mac が一覧に出ていればカーソルを合わせる。走査が終わっても出なければ諦める (居なくなった Mac)。
func (m *Model) seekWantHost() {
	if m.wantHost == "" {
		return
	}
	for i, r := range m.visible() {
		if r.host == m.wantHost && !r.self {
			m.cur[m.tab] = i
			m.wantHost = ""
			return
		}
	}
	if !m.Busy() {
		m.wantHost = ""
	}
}

// Advance は時間を進め、走査の途中の結果を取り込む。
func (m *Model) Advance(now time.Time) {
	m.now = now
	held := m.cursorHost()
	m.takeNetLoad()
	m.takeObserve()
	m.takeBG()
	m.takeSpeed()
	m.takeReach()
	m.takeWired()
	m.refreshWired()
	m.settleProgress(true)
	m.expireConfirms()
	if m.scan != nil {
		m.peers = m.scan.Snapshot()
		if err := m.scan.Err(); err != nil && m.scanErr == nil {
			m.scanErr = err
			m.note("Bonjour で探せない: "+err.Error(), false) // 1 回だけ (毎 tick 出さない)
		}
	}
	// 走査の結果を取り込んだ後で確かめる名前を決める (前に置くと 1 拍前の行で決め、走査が落ち着いた後の合図 1 回で名前が変わった
	// 行は、次のキーまで「…」のまま何も走らなかった)
	m.autoReach()
	// 先に今の Mac へ合わせ直し、戻したい Mac が出ていればそちらが上書きする (逆順だと、見つけて合わせたカーソルを直前の Mac へ戻した。
	// 戻したい Mac を待つ間も今の Mac には合わせる。待つ間だけ添字のままにすると、その間に増えた行の分だけ別の Mac に移った)
	m.followCursor(held)
	m.seekWantHost()
}

// Changed は走査の結果が変わった合図のチャネル (走査が無ければ nil = 待っても来ない)。呼び出し側は合図を受けたら Advance を呼び、また待つ。
// Busy の間しか tick を張らないので、これを待たないと settle の後に見つかった Mac がキーを押すまで出ない。
func (m *Model) Changed() <-chan struct{} {
	if m.scan == nil {
		return nil
	}
	return m.scan.Changed()
}

// Busy は走査が続いているか (呼び出し側はこの間、遅い周期で Advance を呼ぶ。spinner もこの間だけ回る)。
// この Mac のネットワークを裏で読んでいる間も Busy (読み終わりを Advance で取り込むため)。
func (m *Model) Busy() bool {
	return m.busyJob() != nil
}

// OwnsKeys は一覧の上に層 (入力欄・確認・板・設定・測定の進捗) が開いているか (開いていれば glogx は横断キーも渡す。
// 渡さないと L・横断キーが層の裏で板を閉じ、Close が測定を黙って取り消す)。
func (m *Model) OwnsKeys() bool {
	return m.top() != layerList
}

// Binds はこの画面が使うキーか (glogx の横断キー・U より先にこの画面へ渡す。s は status の横断キーと同じ字だが、ここでは ssh)。
// 入力欄・確認・板・設定が開いている間は全部のキー (横断キーで閉じると打っている文字が消える。treefiler の filer.Model.Binds と同じ契約)。
func (m *Model) Binds(key string) bool {
	key = canonKey(key)
	// 足りない画面で層が閉じていれば閉じるキーだけを取り、L や横断キーは glogx に渡す (渡さないと L で閉じなかった)。
	// 層が開いていれば下の OwnsKeys で全部取る (HandleInput は q / esc 以外を捨てる)。渡すと、絞り込みに打った C で
	// claude update が確認なしに走り、横断キーで閉じて打っていた文字が消えた
	if m.small && !m.OwnsKeys() {
		return key == "q" || key == "esc"
	}
	if m.small && key == "L" { // 層が開いていても L (glogx の開閉) は渡す。案内「L/q: 閉じる」のとおり閉じ、hide の Close が層を畳む
		return false
	}
	if m.OwnsKeys() {
		return true
	}
	return slices.Contains(listKeys, key) ||
		listnav.MotionOf(key) != listnav.None
}

// setupExec は初期設定の手順として渡したプロセスの印。
type setupExec struct {
	host   string
	step   setup.StepID
	static *setup.StaticIP
}

// handOff は起こすプロセスを渡す。se は初期設定の手順の印 (手順でなければ nil)。プロセスと印を 1 か所で決める: 一覧の s / o が
// 前の印を消し忘れると、閉じた後に届かなかった手順の印が無関係な ssh の戻りに流用され、確認の成功として扱われた
func (m *Model) handOff(r ExecRequest, se *setupExec) {
	m.logf("exec %s", argvSummary(r.Argv))
	m.exec, m.setupExec = &r, se
}

// TakeExec は起こしてほしいプロセス (HandleInput が Exec を返したとき)。
func (m *Model) TakeExec() (ExecRequest, bool) {
	if m.exec == nil {
		return ExecRequest{}, false
	}
	r := *m.exec
	m.exec = nil
	m.execExitIsFailure = r.ExitIsFailure
	return r, true
}

// ExecDone は起こしたプロセスから戻ったとき。起動できなかったときだけ知らせの文を返す。終了コードは失敗と言わない
// (ssh は相手のシェルの最後のコマンドの終了コードで抜ける。接続できなかった 255 も ssh 自身が端末に理由を出している。
// treefiler の filer.Model.ExecDone と同じ判断)。
func (m *Model) ExecDone(err error) string {
	m.logf("exec done err=%v", err)
	var exitErr interface{ ExitCode() int } // *exec.ExitError
	failOnExit := m.execExitIsFailure
	m.execExitIsFailure = false       // 1 回だけ読む (次の ExecDone へ漏らさない)
	if se := m.setupExec; se != nil { // 初期設定の手順: 成功なら次へ進め、どちらでも相手を読み直す
		m.setupExec = nil
		failed := err != nil && (!errors.As(err, &exitErr) || failOnExit)
		m.setupExecDone(se.host, se.step, se.static, failed)
	}
	switch {
	case err == nil:
		return ""
	case errors.As(err, &exitErr):
		if failOnExit {
			return fmt.Sprintf("開けなかった (終了コード %d)", exitErr.ExitCode())
		}
		return ""
	}
	return "起こせなかった: " + err.Error()
}

// TakeNotices は積んだ知らせを取り出す。
func (m *Model) TakeNotices() []Notice {
	n := m.notices
	m.notices = nil
	return n
}

// Paste は貼り付け (入力欄が開いているときだけ入れる。改行とタブは空白にする。ガイド §7)。
func (m *Model) Paste(s string) {
	if m.small { // 足りない画面では入力欄が見えない
		return
	}
	s = strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(s)
	switch {
	case m.filtering:
		m.filter.Insert(s)
	case m.ov == ovAlias:
		m.alias.Insert(s)
	case m.bandEdit:
		m.band.Insert(s)
	}
}

// Close は走査を止める (自分が起こした dns-sd を全部止める)。
// 開いていた一時的な層 (確認・板・入力欄・設定) も畳む: glogx はこの Model を使い回すので、残すと L で開き直した直後に
// 覚えの無い確認の板が y を待っていた (足りない画面の q で抜けたとき)。
func (m *Model) Close() {
	m.ov, m.ask, m.speedAsk = ovNone, nil, nil
	if m.filtering {
		m.filtering = false
		m.filter.Reset()
	}
	m.bandEdit = false
	m.detailOpen = false
	m.closeSetup()
	// 渡したプロセスの印 (setupExec) も消す: 閉じた後の戻りは届かないことがあり (glogx は閉じた板に ExecDone を渡さない)、残すと後の
	// 無関係な ssh の戻りが確認の成功として扱われた。未確認の印は起こした時点で立てているので、戻りに頼らない
	m.setupExec = nil
	m.sc = scList
	m.stopNetLoad()
	m.stopSpeed() // 閉じた板の裏で帯域を使い続けない
	m.stopReach()
	if m.scan != nil {
		m.scan.Close()
		m.scan = nil
	}
}

// ---- 一覧の行 ----

// row は一覧の 1 行 (この Mac / 台帳 / Bonjour で見えた Mac をまとめたもの)。
type row struct {
	self      bool
	host      string // 鍵 (.local のホスト名)
	alias     string
	user      string // 台帳のユーザー名 (無ければ "")
	mainIP    string
	lanIP     string
	mainDev   string       // メイン IP が見えたこの Mac の I/F (降ろして「その他」へ回すときに要る)
	mainLink  netinfo.Link // メイン IP が見えたこの Mac の I/F の線 (有線 / 無線)
	lanLink   netinfo.Link // 内部 IP が見えたこの Mac の I/F の線 (台帳の内部 IP は、今の走査で見えた I/F と照合できたときだけ)
	lanNote   string       // 内部 IP の出どころ (自分の行だけ)
	ssh       bool
	vnc       bool
	smb       bool
	managed   bool   // 台帳に載っている
	ledgerLAN string // 台帳の内部 IP (Bonjour から見えた IP と分ける。台帳へ書き戻すのはこちらだけ)
	seen      bool   // Bonjour で見えている
	wired     bool   // Bonjour でこの Mac の有線の I/F に見えた (台帳の内部 IP ではなく、今の走査で)
	others    []netinfo.IP
	lanAway   bool   // 内部 IP がこの Mac の有線と別の網 (片方だけ固定して 169.254 と帯の番号に分かれた・この Mac に有線が無い): 有線では届かないので繋ぐ先にしない
	noWire    bool   // lanAway の理由が「この Mac に内部の網の有線が無い (無線だけ)」
	instance  string // Bonjour のインスタンス名 (別名が無いときの表示)
}

// ifKind は相手の IP が見えた I/F (この Mac の I/F の名前) の種類。この Mac のサービスとして登録された I/F だけが
// メイン (ルーターあり) / 内部 (ルーター無しの有線) になる。登録の無い I/F (VM の NAT の bridge100・VPN の utun・
// 名前を引けなかった I/F) は netinfo.Classify の種類のまま「その他」に回す: 内部 IP にすると「有線・固定」の札が付き、
// 台帳への登録で固定 IP の欄に書いてしまう。
func (m *Model) ifKind(dev, addr string) netinfo.Kind {
	if dev == "" {
		return netinfo.KindUnknown
	}
	return m.self.Kind(dev, addr)
}

// selfLan は設定に従って決めたこの Mac の内部 IP と、その出どころの説明 (推測しない。帯 / I/F で決める)。
// selfLanInfo はこの Mac の内部 IP と、その出どころの説明。Unfixed は「帯に入る IP が無く、ルーターの無い有線の IP を仮に出している」
// (画面の「未固定」の札。説明の文言で判定していたので、文言を直すと札が消えた)。
type selfLanInfo struct {
	IP, Note string
	Unfixed  bool
}

// lanFixed は行の内部 IP が固定か。固定の帯は設定で 169.254 の中にも置ける (既定は 192.168.100.0/24) ので「169.254 なら未固定」とは決めない:
// この Mac はサービスの方式、相手は台帳に書いた固定の番号 (初期設定で確かめてから書く) で決める。台帳に無い 169.254 は自動割り当てと見る
// (相手の方式は Bonjour からは分からない)。169.254 でない番号は前から固定の扱い。
func (m *Model) lanFixed(r row) bool {
	switch {
	case r.lanIP == "":
		return false
	case r.self:
		return !m.selfLan().Unfixed
	case r.ledgerLAN != "" && r.lanIP == r.ledgerLAN:
		return true
	}
	return !netinfo.IsLinkLocal(r.lanIP)
}

func (m *Model) selfLan() selfLanInfo {
	if !m.selfOK { // 「無い」と「読めていない」を分ける
		return selfLanInfo{Note: "この Mac のネットワークを読めていない"}
	}
	// 「固定」は初期設定 (setup.Plan の self) と同じ 1 つの規則: 初期設定が固定するのと同じサービス (selfWired) が、手動で帯の中。
	// 画面と初期設定で別々に組むと、I/F で決める設定では方式だけで固定と出し、帯で決める設定ではサービスを持たない I/F (VM のブリッジ等) の
	// 帯の番号を内部 IP と出して、どちらも初期設定は固定し直そうとした。固定かは setup.SelfFixed
	_, bandErr := netinfo.ParseBand(m.pf.Band)
	fixed := func(s netinfo.Service) bool { return setup.SelfFixed(m.pf.Band, s.Method, s.IP) }
	if m.pf.LanByBand {
		if bandErr != nil {
			return selfLanInfo{Note: "帯が使えない形 (" + m.pf.Band + ")"}
		}
		s, ok := m.selfWired()
		switch {
		case !ok || s.IP == "":
			return selfLanInfo{Note: "帯に入る IP が無い"}
		case !netinfo.InBand(m.pf.Band, s.IP):
			return selfLanInfo{IP: s.IP, Note: "帯に入る IP が無い (未固定)", Unfixed: true}
		}
		return selfLanInfo{IP: s.IP, Note: "帯 " + m.pf.Band, Unfixed: !fixed(s)}
	}
	for _, s := range m.self.Enabled() {
		if m.pf.LanIF != "" && s.Device == m.pf.LanIF { // 未設定 ("") を I/F の無いサービス (VPN) に一致させない
			if !m.self.LanIF(s.Device) { // 設定が読めない・ルーターがある・無線の I/F の IP を内部 IP として出さない (I/F の候補と同じ判定)
				return selfLanInfo{Note: "I/F " + s.Device + " (" + cmp.Or(s.Port, "種類不明") + ") は内部の網に使えない (読めない・ルーターがある・無線か iPhone USB)"}
			}
			// 方式で決める (自動の 169.254 を固定と見て台帳に書けた)。帯は I/F で決めるときも固定の判定と番号の提案に使う
			note := "I/F " + s.Device
			switch {
			case bandErr != nil:
				note += " (帯が使えない形)"
			case !netinfo.InBand(m.pf.Band, s.IP):
				note += " (帯 " + m.pf.Band + " の外)"
			}
			return selfLanInfo{IP: s.IP, Note: note, Unfixed: !fixed(s)}
		}
	}
	return selfLanInfo{Note: "I/F " + cmp.Or(m.pf.LanIF, "(未設定)") + " が無い"}
}

// rows は台帳・Bonjour・この Mac の網から一覧の行を組む (分類はここと ifKind / selfLan / connectIP / userFor に閉じ、UI の状態を読まない)。
// inventory 型へ切り出していないのは、読む側 (画面・Report・初期設定) が既にこの関数を共有していて重複が無く、切り出しても
// 変更の波及も読む範囲もほとんど変わらないため。分類の規則が画面と独立に変わり始めたら再評価する
func (m *Model) rows() []row {
	self := row{self: true, host: m.host, alias: "この Mac", user: m.o.User}
	if s, ok := m.self.Main(); ok {
		self.mainIP, self.mainLink = s.IP, m.self.Link(s.Device)
	}
	sl := m.selfLan()
	self.lanIP, self.lanNote, self.lanLink = sl.IP, sl.Note, m.self.LinkOf(sl.IP)
	for _, x := range m.self.IPs {
		if x.Addr != self.mainIP && x.Addr != self.lanIP && slices.Contains(m.pf.Kinds, x.Kind.Key()) {
			self.others = append(self.others, x)
		}
	}
	byHost := map[string]*row{}
	out := make([]row, 0, len(m.hosts)+len(m.peers))
	// 🚨 外から来た文字列 (台帳・Bonjour の名前) はここで 1 回だけ無害化する。描画の側は無害化済みとして扱う。
	// ただしホスト名は鍵なので書き換えない: 制御文字を含むホスト名は捨てる (無害化で鍵が変わると、同じ Mac が台帳の行と
	// Bonjour の行の 2 つに割れる。termsafe の IsPlain の注記どおり、identity に使う値は書き換えず落とす)
	clean := termsafe.PlainLine
	for _, h := range m.hosts {
		if !termsafe.IsPlain(h.Host) {
			continue
		}
		if h.Host == m.host {
			self.managed = true
			continue
		}
		out = append(out, row{host: h.Host, alias: clean(h.Alias), user: clean(h.User), lanIP: clean(h.LAN), ledgerLAN: clean(h.LAN), managed: true})
	}
	for i := range out {
		byHost[out[i].host] = &out[i]
	}
	for _, p := range m.peers {
		k := ledger.Key(p.Host)
		if !termsafe.IsPlain(k) {
			continue
		}
		if k == m.host {
			self.ssh, self.vnc, self.smb, self.seen = p.SSH, p.VNC, p.SMB, true
			continue
		}
		r := byHost[k]
		if r == nil {
			out = append(out, row{host: k})
			r = &out[len(out)-1]
			byHost[k] = r
		}
		r.seen, r.ssh, r.vnc, r.smb = true, p.SSH, p.VNC, p.SMB
		if len(p.Instances) > 0 {
			r.instance = clean(p.Instances[0])
		}
		// I/F の番号の順に見て、それぞれ最初に見えた IP を採る。map の順で回すと、ルーターの無い I/F が 2 つある相手
		// (Thunderbolt Ethernet + USB LAN・bridge0) では描くたびに内部 IP が入れ替わり、s の ssh 先が押した瞬間の運で決まった。
		// 台帳の内部 IP があればそちらを採る (固定 IP)
		lanPicked := r.ledgerLAN != ""
		for _, idx := range slices.Sorted(maps.Keys(p.Addrs)) {
			ip := p.Addrs[idx]
			dev := ""
			if m.o.IfName != nil {
				dev = m.o.IfName(idx)
			}
			if !m.selfOK {
				break // この Mac のどの I/F が有線かを読めるまで分けない (分けると全部「その他」になり、s がその先頭へ繋いだ)
			}
			k := m.ifKind(dev, ip)
			link := m.self.Link(dev)
			// 内部 IP にするのは、この Mac の内部の網の I/F (LanIF) で見えたものだけ。169.254 も、その I/F の上なら内部の網。
			// Kind では決めない (登録の無い I/F の 169.254 も KindLinkLocal になる。iPhone USB は KindWired だが内部の網には使わない)
			wiredService := m.self.LanIF(dev)
			r.wired = r.wired || wiredService
			switch {
			case wiredService && r.ledgerLAN != "" && clean(ip) == r.ledgerLAN:
				r.lanLink = link // 台帳の内部 IP が今見えた I/F (見えていなければ線は分からないまま)。内部 IP と同じなので「その他」に重ねない
			case k == netinfo.KindInternet && r.mainIP == "":
				r.mainIP, r.mainDev, r.mainLink = clean(ip), dev, link
			case k == netinfo.KindInternet && r.mainLink == netinfo.LinkWireless && link == netinfo.LinkWired:
				// 有線と無線の両方で見えたら有線をメインにする (I/F 番号の順だと Wi-Fi の en0 が先に来て、有線で繋がっているのに
				// メインが「無線」と出た)。降ろした方も、2 つ目のメインも捨てずに「その他」に残す
				if clean(ip) != r.mainIP { // 同じ IP が 2 つの I/F で見えただけなら「その他」に重ねない
					r.others = append(r.others, netinfo.IP{Device: r.mainDev, Addr: r.mainIP, Kind: netinfo.KindInternet})
				}
				r.mainIP, r.mainDev, r.mainLink = clean(ip), dev, link
			case k == netinfo.KindInternet && clean(ip) == r.mainIP:
				// 同じ IP が別の I/F でも見えた: 「その他」に重ねない (重ねると「その他」の線がメインの線と食い違って見えた)。
				// 「Wi-Fi でも見えている」ことは画面から消えるが、出すのはメインの I/F の線 1 つと決めている
			case wiredService && !lanPicked:
				r.lanIP, r.lanLink, lanPicked = clean(ip), link, true
			default:
				r.others = append(r.others, netinfo.IP{Device: dev, Addr: clean(ip), Kind: k})
			}
		}
	}
	noWire := m.selfNoWire()
	for i := range out {
		// この Mac に有線が無いと self.lanIP は空で、lanReachable は「分からない = 届く」と読む。台帳の内部 IP へ繋ぎにいかない
		out[i].noWire = noWire && out[i].lanIP != ""
		out[i].lanAway = out[i].noWire || !lanReachable(self.lanIP, out[i].lanIP, m.pf.Band)
	}
	slices.SortFunc(out, func(a, b row) int { return strings.Compare(cmp.Or(a.alias, a.host), cmp.Or(b.alias, b.host)) })
	return append([]row{self}, out...)
}

// タブ。相手の一覧 (管理対象 / 未管理) にこの Mac は出さず、この Mac は自分のタブにだけ出す (一覧は他の Mac を見る場所。
// 両方のタブの先頭に固定していた。ユーザー指示 2026-10-17)
const (
	tabManaged = iota
	tabUnmanaged
	tabSelf
	numTabs
)

// visible は今のタブの行 (相手のタブは空でありうる)。
func (m *Model) visible() []row {
	all := m.rows()
	if m.tab == tabSelf {
		return all[:1]
	}
	out := make([]row, 0, len(all)-1)
	q := strings.ToLower(m.filter.String())
	for _, r := range all[1:] {
		if r.managed != (m.tab == tabManaged) || !m.listed(r) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(r.alias+" "+r.host+" "+r.instance), q) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// listed は設定で一覧に出す相手か (タブの件数と一覧が同じ述語を通る。絞り込みの文字はタブの件数に含めない)。
func (m *Model) listed(r row) bool {
	return !m.pf.WiredOnly || r.wired
}

func (m *Model) count(managed bool) int {
	n := 0
	for _, r := range m.rows()[1:] {
		if r.managed == managed && m.listed(r) {
			n++
		}
	}
	return n
}

// current はカーソルの行。今のタブに行が無ければ空の行 (host も IP も無い。行に効くキーは keyList が先に断る)。
func (m *Model) current() row {
	vis := m.visible()
	if len(vis) == 0 {
		m.cur[m.tab] = 0
		return row{}
	}
	m.cur[m.tab] = max(min(m.cur[m.tab], len(vis)-1), 0)
	return vis[m.cur[m.tab]]
}

// shellRefusal は s (ssh) を断る理由。空なら繋げる。最下行の案内もこれで s を出すか決める (案内と効くキーを 1 つの述語にする)。
func (r row) shellRefusal() string {
	switch {
	case r.self:
		return "この Mac には ssh しない"
	case !r.ssh && !r.managed:
		return "ssh が閉じている (相手の画面でリモートログインをオンにしてから)"
	case r.connectIP() == "":
		return cmp.Or(r.awayReason(), "繋ぐ IP が分からない")
	}
	return ""
}

// sshRefusal は s を断る理由 (行の理由に、ユーザー名が分からないことを足す)。最下行・詳細のカード・s のキーが同じ述語を見る
// (行だけを見ていたので、$USER が使えないときに s と Y を案内しながら断り、カードは "ssh @<IP>" を描いた。整合の監査)。
func (m *Model) sshRefusal(r row) string {
	if why := r.shellRefusal(); why != "" {
		return why
	}
	if user, _ := m.userFor(r); user == "" {
		return noUserNote
	}
	// HostKeyAlias=<host> を組む (Bonjour の名前の , や * が known_hosts の照合を崩す)。Mac の LocalHostName は英数字と - だけなので
	// 断られるのは Mac 以外の機器 (Avahi の UTF-8 のホスト名など) だけ。それらは IP で ssh する (受容)
	if !sshdest.ValidAlias(r.host) {
		return fmt.Sprintf("ホスト名 %q は ssh の鍵の名前に使えない (英数字と . _ - だけ)", r.host)
	}
	return ""
}

// noUserNote は ssh のユーザー名が分からないときの断り (u は台帳に載っている行でだけ効く)。
const noUserNote = "ssh のユーザー名が分からない ($USER が使えない。台帳に載せてから u で書く)"

// awayReason は、有線で別の網に居て Wi-Fi も無いので繋げない理由 (そうでなければ "")。
func (r row) awayReason() string {
	if r.noWire && r.mainIP == "" {
		return "この Mac に内部の網の有線が無く、相手も Wi-Fi に見えない。この Mac に有線を挿すか、相手を Wi-Fi に繋ぐ"
	}
	if r.lanAway && r.mainIP == "" {
		return "有線で網が分かれている (この Mac か相手の片方だけが固定 IP)。相手も初期設定で固定するか、Wi-Fi に繋ぐ"
	}
	return ""
}

// shownOthers は画面に出す「その他の IP」(設定の種類で絞る)。r.others は絞らない: 繋ぐ先の最後の手段 (connectIP) と、初期設定で
// 避ける番号にも使う (種類を切った VM の網のゲストへ繋げなくなる・重なる番号を提案する)。この Mac の行は rows で絞ってある
func (m *Model) shownOthers(r row) []netinfo.IP {
	return slices.DeleteFunc(slices.Clone(r.others), func(x netinfo.IP) bool { return !slices.Contains(m.pf.Kinds, x.Kind.Key()) })
}

// connectIP は繋ぐ先 (内部 IP があればそちら = 有線を通す。無ければメイン。どちらも無ければその他の I/F で見えた IP。
// VM のゲストは VM の網にしか居ないので、その他を最後の手段に残す)。
func (r row) connectIP() string {
	lan := r.lanIP
	if r.lanAway {
		lan = "" // 有線で届かない: 片方だけ固定すると、送り元が固定の番号になり、169.254 のままの相手は返事を Wi-Fi 側へ出して届かない (air5 で実測)
		if r.mainIP == "" {
			return "" // その他の I/F (VPN 等) へは落とさない (見当違いの IP へ黙って繋いだ。敵対レビュー)。理由は awayReason
		}
	}
	if ip := cmp.Or(lan, r.mainIP); ip != "" || len(r.others) == 0 {
		return ip
	}
	return r.others[0].Addr
}

// connectLink は繋ぐ先 (connectIP) の線。connectIP と同じ順 (内部 → メイン → その他の先頭) で、選んだ方の線を返す。
// IP の値で引き直さない: 同じ IP が有線と無線の両方の I/F で見えると、どちらの線か値からは決まらない。
//
// 🚨 線は「この Mac のどの I/F で相手の IP が見えたか」で、実際に送る I/F ではない。この Mac が同じサブネットに Wi-Fi と有線の
// 両方で居ると、macOS はサービスの並び順の上の I/F から送るので、(有線) と出ても Wi-Fi から出ていることがある。送る I/F を
// 確かめるなら route get <IP> を引く (相手ごとに外部コマンドが要るので入れていない。同じサブネットに 2 本繋ぐ形が実際に出たら再評価)。
func (m *Model) connectLink(r row) netinfo.Link {
	switch ip := r.connectIP(); {
	case ip == "":
		return netinfo.LinkUnknown
	case ip == r.lanIP && !r.lanAway && r.lanLink != netinfo.LinkUnknown:
		return r.lanLink
	case ip == r.mainIP: // 台帳の内部 IP が有線では見えず、同じ IP がメインとして見えたときもここ (メインの線は分かっている)
		return r.mainLink
	case ip == r.lanIP:
		return netinfo.LinkUnknown
	case len(r.others) > 0:
		return m.self.Link(r.others[0].Device)
	}
	return netinfo.LinkUnknown
}

// lanReachable は、この Mac の内部 IP (self) から相手の内部 IP (peer) へ有線で届くか。両方が 169.254 (自動) か、両方が帯の中なら同じ網。
// 片方だけ固定すると網が分かれ、送り元の番号に相手が返事をできない。この Mac の内部 IP が分からなければ届くと見る (前と同じ)。
func lanReachable(self, peer, band string) bool {
	switch {
	case self == "" || peer == "":
		return true
	case netinfo.InBand(band, peer) || netinfo.InBand(band, self): // 帯を先に (帯は 169.254 の中にも置ける。敵対レビュー)
		return netinfo.InBand(band, peer) && netinfo.InBand(band, self)
	case netinfo.IsLinkLocal(peer) || netinfo.IsLinkLocal(self):
		return netinfo.IsLinkLocal(peer) && netinfo.IsLinkLocal(self)
	}
	return true
}

// userFor は ssh のユーザー名 (台帳に無ければ推測の名前と、推測であることの印)。
func (m *Model) userFor(r row) (user string, guessed bool) {
	if r.user != "" {
		return r.user, false
	}
	return m.o.User, true
}

// ---- キー ----

// canonKey はキーの表記を 1 つに寄せる。glogx は Space を " " に書き換えてから板へ渡す (glogx の app.NormalizeSpaceKey) ので、
// "space" だけを見ると glogx の板で Space が台帳の登録ではなく半ページ送り (listnav は " " も下へ送る) に化ける。
func canonKey(key string) string {
	if key == " " {
		return "space"
	}
	return key
}

// HandleKey はキーを 1 つ処理する (打った文字はキーの表記から推す。KeyPressMsg.Text を持たない glogx の板の口)。
// dupscan:allow intentional 板ごとに返す型 (Action / Result) と受け口が違う 1 行の委譲。lineedit.KeyText で文字を推す約束を、板の口の形に合わせて各板が持つ
func (m *Model) HandleKey(key string) Action { return m.HandleInput(key, lineedit.KeyText(key)) }

// HandleInput はキーを 1 つ処理する (text は打った文字。入力欄に入れる)。
func (m *Model) HandleInput(key, text string) Action {
	m.logKey(key)
	key = canonKey(key)
	held := m.cursorHost()
	var shown []string // カーソルが相手の行に無い (空のタブ等) とき、画面に出ていた行
	if held == "" {
		shown = m.visibleHosts()
	}
	m.takeNetLoad() // 読み終えていれば、次の tick を待たずにこのキーから効かせる
	m.takeObserve()
	m.refreshWired() // 放置した後は tick が回っていない: 引き直す時期を過ぎていればここで始める (取り込みは、引いている間は Busy で回る tick が行う)
	// 取り込みで行が組み変わっても、このキーは画面で見ていた Mac に効かせる。見ていた Mac が消えたら (有線が外れて有線だけの表示から
	// 落ちた等)、行に効くキーは断る (添字のままだと隣の Mac に繋いだ)
	// カーソルが相手の行に無かったなら、行の集合が変わったことで断る (空のタブを見ている間に読み込みが終わると、一度も描かれていない
	// 先頭の Mac へ s が繋いだ)
	m.cursorLost = !m.followCursor(held) || (held == "" && !slices.Equal(shown, m.visibleHosts()))
	if key == "ctrl+c" {
		return Quit
	}
	// 幅・高さが足りない画面 (抜け方だけ) を出している間は、閉じるキー (q / esc) だけを効かせる。隠れた確認の板に y が届いて
	// 台帳から外したり、絞り込みに q が文字として入ったりした。開いていた層は Close が畳む
	if m.small {
		if key == "q" || key == "esc" {
			return Quit
		}
		return None
	}
	m.wantHost = "" // 自分で動かしたら、後から見つかった Mac へカーソルを跳ばさない (効かないキーでは消さない)
	switch m.top() {
	case layerSpeedProgress:
		return m.keySpeedProgress(key)
	case layerConfirm:
		a := m.ask
		if a != nil && a.exec != nil && key == "c" { // 実行するコマンドの全文をコピーする (板は閉じない。うまくいかないときに相談できるように。ユーザー要望)
			m.copyAs(shellLine(a.exec.Argv), "実行するコマンド (全文)")
			return None
		}
		if a != nil && (a.auto || a.selfOnly) && key == "enter" {
			// 自動で出した板と、enter で開くこの Mac だけの板は y でだけ実行する (板は閉じない)。後者は enter を続けて押すと、
			// パネル → 板 → 自動では戻らないこの Mac の変更まで進んだ
			return None
		}
		m.ov, m.ask = ovNone, nil
		if a != nil && confirm.IsYes(key) {
			if a.exec != nil {
				if m.su == nil && !a.selfOnly { // 初期設定の画面の外で手順を起こさない (相手の名前の無い手順は、未確認の印も確認待ちも立たない。敵対レビュー)
					m.note("初期設定の画面が閉じている (開き直して x)", false)
					return None
				}
				if a.step == stepConfirm && m.conf() == nil { // 板を出している間に期限が切れた (相手は元に戻っている)
					m.note("確認の期限が過ぎた (r でリロード)", false)
					return None
				}
				se := &setupExec{step: a.step, static: a.static}
				if a.selfOnly {
					se.host = m.host // 戻ったときに setupExecDone がこの Mac の網を読み直す (相手の画面は無いので観測はしない)
				} else if m.su != nil {
					se.host = m.su.host
					if a.step == setup.StepPeerStatic && a.static != nil {
						// 起こす時点で未確認の印を立てる (戻ったときに立てると、戻りを受けない経路 (glogx で閉じた後) で立たなかった)
						m.markUnconf(m.su.host, a.static.IP)
					}
				}
				m.handOff(*a.exec, se)
				return Exec
			}
			n := a.yes()
			m.note(n.Text, n.OK)
		}
		return None // 知らないキーはすべて取り消し (§4)
	case layerCopyIP:
		return m.keyCopyIP(key)
	case layerLog:
		return m.keyLog(key)
	case layerAlias:
		return m.keyAlias(key, text)
	case layerSpeedAsk:
		return m.keySpeed(key)
	case layerHelp:
		m.ov = ovNone // どのキーでも閉じる
		return None
	case layerSetup, layerSetupProgress:
		return m.keySetup(key)
	case layerSettings:
		return m.keySettings(key, text)
	case layerDetail:
		return m.keyDetail(key)
	case layerFilter: // 入力中は入力欄が先にキーを取る (§7)
		switch key {
		case "esc":
			m.filtering = false
			m.filter.Reset()
		case "enter":
			m.filtering = false
		default:
			m.filter.Key(key, text)
			m.cur[m.tab] = 0
		}
		return None
	case layerList:
	}
	return m.keyList(key)
}

// move はカーソルを移動の語彙 (listnav.MotionOf) で動かす。
func move(cur *int, n int, key string, rows int) bool {
	defer func() { *cur = max(*cur, 0) }() // 行が無いタブ (n=0) で n-1 = -1 を入れない
	switch listnav.MotionOf(key) {
	case listnav.Down:
		*cur = min(*cur+1, n-1)
	case listnav.Up:
		*cur = max(*cur-1, 0)
	case listnav.HalfDown:
		*cur = min(*cur+listnav.Half(rows), n-1)
	case listnav.HalfUp:
		*cur = max(*cur-listnav.Half(rows), 0)
	case listnav.Top:
		*cur = 0
	case listnav.Bottom:
		*cur = n - 1
	case listnav.None:
		return false
	}
	return true
}

// netRefused は、この Mac のネットワークを読み終えていなければ理由を知らせて true (needsNet のキーと初期設定が断る)。
func (m *Model) netRefused() bool {
	if m.selfOK {
		return false
	}
	if m.netReady && m.netCh == nil {
		m.note("この Mac のネットワークを読めなかった (r で読み直す)", false)
	} else {
		m.note("この Mac のネットワーク設定を確認中 (少し待ってから)", false)
	}
	return true
}

// detailFollow はカーソルをパネルを開いた Mac へ戻す (見つからなければ false)。
func (m *Model) detailFollow() bool {
	if m.cursorLost {
		return false
	}
	if m.detailSelf {
		return m.current().self
	}
	return m.followCursor(m.detailHost)
}

// detailShowsOpened はカーソルの行がパネルを開いた Mac か (描画はカーソルを動かさずに確かめる)。
func (m *Model) detailShowsOpened() bool {
	r := m.current()
	if m.detailSelf {
		return r.self
	}
	return !r.self && r.host != "" && r.host == m.detailHost
}

// keyDetail は詳細のパネルのキー: enter で初期設定、動作のキーは一覧と同じ、移動のキーでパネルの中を送り、q / esc で一覧へ戻る。
// パネルが開いている間はカーソルを動かさない (見ている Mac と動作の相手を食い違わせない)。
func (m *Model) keyDetail(key string) Action {
	if !m.detailFollow() { // 見ていた Mac が一覧から消えた
		m.detailOpen = false
		m.note("一覧が変わった (カーソルの Mac を見直してから)", false)
		return None
	}
	r := m.current()
	switch {
	case key == "q" || key == "esc":
		m.detailOpen = false
	case key == "enter":
		if m.netRefused() {
			return None
		}
		m.openSetup(r) // この Mac の行では断る。開いてもパネルは閉じない (初期設定はパネルの上に重ね、esc で 1 枚ずつ戻る)
	case slices.Contains(detailKeys, key): // 移動の語彙より先に (space は半ページの送りでもある。一覧の keyList と同じ順)
		return m.keyList(key)
	case listnav.MotionOf(key) != listnav.None:
		off := m.detailOff
		_ = move(&off, m.detailMax+1, key, max(m.h-m.panelTop-4, 2))
		m.detailOff = off
	}
	return None
}

func (m *Model) keyList(key string) Action {
	// 繋ぐ・コピー・登録は、この Mac のネットワーク (どの I/F が有線か・この Mac の IP) を読み終えてから。読み込み中に受けると
	// 繋ぐ先・登録する内部 IP を誤った分類で決める
	if slices.Contains(needsNet, key) && m.netRefused() {
		return None
	}
	if m.cursorLost && slices.Contains(rowKeys, key) {
		m.note("一覧が変わった (カーソルの Mac を見直してから)", false)
		return None
	}
	if len(m.visible()) == 0 && slices.Contains(rowKeys, key) {
		m.note("このタブには Mac が無い", false)
		return None
	}
	r := m.current()
	if a := rowActionOf(key); a != nil { // 移動の語彙より先に (space は半ページの送りでもある)
		return m.runAction(a, r)
	}
	switch key {
	case "q", "esc":
		if key == "esc" && m.filter.String() != "" {
			m.filter.Reset()
			return None
		}
		return Quit
	}
	rows := m.listRows
	if rows <= 0 {
		rows = m.bodyHeight()
	}
	if move(&m.cur[m.tab], len(m.visible()), key, rows) {
		return None
	}
	switch key {
	case "tab", "l", "right": // タブのある画面では h / l がタブ移動 (§2)
		m.tab = (m.tab + 1) % numTabs
	case "shift+tab", "h", "left":
		m.tab = (m.tab + numTabs - 1) % numTabs
	case "/":
		m.filtering = true
	case "?":
		m.ov = ovHelp
	case ",":
		m.sc, m.setC = scSettings, 0
	case "c":
		m.ov, m.copyC = ovCopyIP, 0
	case "r":
		m.Reload()
		m.note("リロードした", true)
	case "enter":
		m.detailOpen, m.detailOff, m.detailHost, m.detailSelf = true, 0, r.host, r.self
	case "e": // メッセージ一覧 (msglog.go)
		m.openLog()
	}
	return None
}

// unadvertise は台帳から外した相手の名前の広告を裏で止める。結果は takeBG が知らせる (届かない相手を待って画面を止めない)。
func (m *Model) unadvertise(host, name string, argv []string) {
	run := m.o.RunQuiet
	ch := make(chan Notice, 1)
	m.bg = append(m.bg, ch)
	m.logf("unadvertise start %s %s", host, argvSummary(argv))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := run(ctx, argv); err != nil {
			var ec interface{ ExitCode() int }
			if errors.As(err, &ec) && ec.ExitCode() == setup.ExitOtherName {
				ch <- Notice{Text: name + " の広告は止めていない: 相手は別の名前を広告している (" + err.Error() + ")", OK: false}
				return
			}
			ch <- Notice{Text: name + " の広告を止められなかった (" + err.Error() + ")。相手で: " + setup.RemoveHint(), OK: false}
			return
		}
		ch <- Notice{Text: name + " の広告を止めた", OK: true}
	}()
}

// takeBG は裏の後始末の結果を取り込む。
func (m *Model) takeBG() {
	m.bg = slices.DeleteFunc(m.bg, func(ch <-chan Notice) bool {
		select {
		case n := <-ch:
			m.note(n.Text, n.OK)
			return true
		default:
			return false
		}
	})
}

// WaitBG は裏の後始末 (unadvertise) の結果を待って返す。単体のバイナリが終わる前に呼ぶ: Close は取り消さないので ssh は走りきるが、
// 待たずに終わると結果 (止められなかったときの止め方) を知らせる先が無い。glogx は Model を使い回し、次に開いたときに takeBG が
// 知らせるので呼ばない。stop が閉じたら待つのをやめ、まだ結果の来ない件数を left に返す (各 ssh は 20 秒で打ち切る)
func (m *Model) WaitBG(stop <-chan struct{}) (got []Notice, left int) {
	stopped := false
	m.bg = slices.DeleteFunc(m.bg, func(ch <-chan Notice) bool {
		select { // 届いている分は待たずに取る (stop と同時に来ていると select はどちらを取るか決まらず、届いた結果を落とした)
		case n := <-ch:
			got = append(got, n)
			return true
		default:
		}
		if stopped {
			return false
		}
		select {
		case n := <-ch:
			got = append(got, n)
			return true
		case <-stop:
			stopped = true
			return false
		}
	})
	return got, len(m.bg)
}

// PendingBG は結果の来ていない裏の後始末の件数。
func (m *Model) PendingBG() int { return len(m.bg) }

// smbURL は r のファイル共有の URL (smb://<宛先>)。繋げるかは表 (rowActions) の smbRefusal が先に決める (表を通さずに呼ばない)。
func (m *Model) smbURL(r row) string { return "smb://" + m.target(r) }

func (m *Model) copy(s, note string) { m.copyAs(s, s+note) }

// copyAs は s をコピーし、知らせには shown を出す (メッセージ一覧の全文を知らせへ写すと、一覧が同じ本文で埋まった)。
func (m *Model) copyAs(s, shown string) {
	if m.o.Copy == nil {
		return
	}
	if err := m.o.Copy(s); err != nil {
		m.note("コピーできない: "+err.Error(), false)
		return
	}
	m.note("コピーした: "+shown, true)
}

// askLedger は台帳への登録 / 削除の確認を出す (どちらも y/N。§4)。
func (m *Model) askLedger(r row) {
	if m.o.Ledger == nil {
		return
	}
	if r.managed {
		body := []string{r.host + " を管理対象から外す"}
		name, nerr := setup.WiredName(r.host)
		d, derr := m.destFor(r)
		argv, aerr := setup.UnadvertiseArgv(d, name)
		stop := m.o.RunQuiet != nil && !r.self && nerr == nil && derr == nil && aerr == nil
		if stop {
			body = append(body, "相手の有線の名前 ("+name+") の広告も止める (ssh の鍵で入る。届かなければ止め方を知らせる)")
		} else {
			body = append(body, "Mac には何もしない (有線の名前の広告は止めない)")
		}
		m.ask = &ask{title: "台帳から外す", body: body, yes: func() Notice {
			if err := m.o.Ledger.Remove(r.host); err != nil {
				return Notice{Text: "外せない: " + err.Error(), OK: false}
			}
			delete(m.wired, r.host) // 外す前に引いた「引ける」を使い続けない (戻したときに引き直す)
			m.reloadLedger()
			if stop {
				m.unadvertise(r.host, name, argv)
			}
			return Notice{Text: "台帳から外した: " + r.host, OK: true}
		}}
	} else {
		if r.host == "" {
			m.note("ホスト名が分からない Mac は登録できない", false)
			return
		}
		user, _ := m.userFor(r)
		// 台帳の内部 IP の欄は固定 IP のためのもの。自動割り当て (169.254) は起動のたびに変わるので書かない
		h := ledger.Host{Host: r.host, User: user}
		lan := r.lanIP
		if r.self {
			lan = m.selfLan().IP
		}
		if why := m.ledgerBlocked(r.host, lan); why != "" {
			m.note(why, false)
			return
		}
		if lan != "" && m.lanFixed(r) {
			h.LAN = lan
		}
		m.ask = &ask{title: "台帳に登録する", body: []string{
			r.host + " を管理対象にする (Mac には何もしない。初期設定は別)",
			"user " + cmp.Or(h.User, "(無し。繋ぐときはこの Mac のユーザー名)") + " / 内部 IP " + cmp.Or(h.LAN, "-"),
		}, yes: func() Notice {
			// 板を出している間に別のプロセス (glogx の L・単体のコマンド) が同じ Mac を載せていたら、その行を残す (古い一覧の値で
			// 置き換えると別名・内部 IP を消した)。空の欄だけ埋める
			err := m.o.Ledger.Merge(h, func(x *ledger.Host) {
				x.User, x.LAN = cmp.Or(x.User, h.User), cmp.Or(x.LAN, h.LAN)
			})
			if err != nil {
				return Notice{Text: "登録できない: " + err.Error(), OK: false}
			}
			m.reloadLedger()
			return Notice{Text: "台帳に登録した: " + r.host, OK: true}
		}}
	}
	m.ov = ovConfirm
}

func (m *Model) reloadLedger() {
	hs, err := m.o.Ledger.Load()
	if err != nil { // 黙ると、書けたのに一覧は古いまま (「未管理」のまま) で理由が出ない
		m.note("台帳を読み直せない (一覧は古いまま。r でリロード): "+err.Error(), false)
		return
	}
	m.setHosts(hs, false) // 知らせは Reload で出している (別名・登録のたびに繰り返さない)
	m.startWiredLookups() // 台帳の内部 IP が変わると、名前を持つ Mac と引く先が変わる
}

// setHosts は読んだ台帳を一覧に入れる。m.hosts に入れる経路はここだけ (Reload と、別名・登録・外した後の reloadLedger の 2 つが
// 同じ確かめを通る。Reload の中だけで確かめていたころは、別名を付けた後に不正な user / lan が戻り、コピーした ssh の文字列に入った)。
// 手で書き換えうる欄は使う前に確かめる: user が "-o…" だと ssh が option として読み、lan が IP でないと
// 引数に何でも入った。
func (m *Model) setHosts(hs []ledger.Host, tell bool) {
	for i := range hs {
		for _, f := range hs[i].Unusable() {
			if tell {
				m.note(fmt.Sprintf("台帳の %s の %s が使えない形なので使わない", hs[i].Host, f), false)
			}
			if f == "user" {
				hs[i].User = ""
			} else {
				hs[i].LAN = ""
			}
		}
	}
	m.hosts = hs
}

func (m *Model) keyCopyIP(key string) Action {
	n := len(m.selfCopyItems())
	m.copyC = min(m.copyC, n-1) // 板を開いている間に行が減った (この Mac が未固定に戻った) ら、残った最後の行を選ぶ
	if move(&m.copyC, n, key, n) {
		return None
	}
	switch key {
	case "1", "2", "3":
		m.ov = ovNone
		m.copySelf(int(key[0] - '1'))
	case "enter", "c": // 開いたキーでもう一度押すと、選んでいる方をコピー
		m.ov = ovNone
		m.copySelf(m.copyC)
	case "esc", "q", "h", "left":
		m.ov = ovNone
	}
	return None
}

// selfIPs はこの Mac の IP (0 = 内部 / 1 = メイン)。
func (m *Model) selfIPs() [2]string {
	lan := m.selfLan().IP
	main := ""
	if s, ok := m.self.Main(); ok {
		main = s.IP
	}
	return [2]string{lan, main}
}

// selfCopyItems は c の板に並べるもの (この Mac の内部 IP・メイン IP・有線の名前)。板の表示とコピーは同じ並びを見る。
func (m *Model) selfCopyItems() []copyItem {
	ips := m.selfIPs()
	items := []copyItem{{label: "内部", value: ips[0]}, {label: "メイン", value: ips[1]}}
	if name, ok, note := m.wiredState(m.rows()[0]); name != "" {
		it := copyItem{label: "有線の名前", value: name, note: note}
		if !ok {
			it.value = "" // 引けない名前はコピーさせない (渡しても繋がらない)
			it.shown = name
		}
		items = append(items, it)
	}
	return items
}

// copyItem は c の板の 1 行。value が空ならコピーできない (shown は代わりに見せる値、note は理由)。
type copyItem struct{ label, value, shown, note string }

func (m *Model) copySelf(i int) {
	items := m.selfCopyItems()
	if i < 0 || i >= len(items) {
		m.note(fmt.Sprintf("%d 行目は無い (この Mac の有線の名前は、内部 IP を固定すると出る)", i+1), false) // 黙って閉じない
		return
	}
	it := items[i]
	if it.value == "" {
		m.note("この Mac の"+it.label+"が無い"+map[bool]string{true: " (" + it.note + ")", false: ""}[it.note != ""], false)
		return
	}
	m.copy(it.value, "  (この Mac の"+it.label+")")
}

// hostEdit は入力欄で書き換える台帳の 1 欄。host は開いたときの Mac (確定の時点のカーソル行ではない。入力中に走査で行が増えると
// カーソルがずれる)。check は入れた値を確かめる (空なら通す)。
type hostEdit struct {
	host, label, title, done string
	check                    func(string) string
	set                      func(*ledger.Host, string)
	emptyMeans               func(*Model) string // 空にしたときに実際に使う値 (知らせに出す。nil なら「(無し)」)
}

var (
	aliasEdit = hostEdit{label: "別名", title: "別名を付ける", done: "別名を付けた", set: func(h *ledger.Host, v string) { h.Alias = v }}
	userEdit  = hostEdit{label: "ユーザー", title: "ssh のユーザー名を変える", done: "ユーザー名を変えた",
		check: func(v string) string {
			if v != "" && !ledger.ValidUser(v) { // 空は「既定 (この Mac のユーザー名) に戻す」
				return "ユーザー名が使えない: " + strconv.Quote(v)
			}
			return ""
		}, set: func(h *ledger.Host, v string) { h.User = v },
		emptyMeans: func(m *Model) string { return "既定 (" + cmp.Or(m.o.User, "分からない") + ")" }}
)

// openEdit は台帳に載っている Mac の 1 欄の入力欄を開く。
func (m *Model) openEdit(r row, e hostEdit, cur string) {
	if r.self {
		m.note("この Mac の別名・ユーザー名はここでは変えない", false)
		return
	}
	if !r.managed {
		m.note("台帳に無い Mac は変えられない (先に space で台帳に登録する)", false)
		return
	}
	e.host = r.host
	m.edit = e
	m.alias.Reset()
	m.alias.Insert(cur)
	m.ov = ovAlias
}

func (m *Model) keyAlias(key, text string) Action {
	switch key {
	case "esc":
		m.ov = ovNone
	case "enter":
		e, v := m.edit, strings.TrimSpace(m.alias.String())
		if e.check != nil {
			if why := e.check(v); why != "" {
				m.note(why, false) // 入力欄は開いたまま (直して打ち直せる)
				return None
			}
		}
		m.ov = ovNone
		// その欄だけを変える (台帳を読み直した上で。古い一覧の行で丸ごと書き戻すと、外された Mac が戻り、Bonjour の 169.254 が固定 IP の欄に入る)
		if err := m.o.Ledger.Update(e.host, func(h *ledger.Host) { e.set(h, v) }); err != nil {
			m.note(e.label+"を変えられない: "+err.Error(), false)
			m.reloadLedger()
			return None
		}
		m.reloadLedger()
		shown := cmp.Or(v, "(無し)")
		if v == "" && e.emptyMeans != nil {
			shown = e.emptyMeans(m)
		}
		m.note(e.done+": "+shown, true)
	default:
		m.alias.Key(key, text)
	}
	return None
}

// CaretPos は入力欄のキャレットの位置 (端末のカーソルを置く。IME の変換中の文字が欄に出るように。§7)。
func (m *Model) CaretPos() (x, y int, ok bool) {
	switch {
	case m.filtering:
		return m.caretFX, m.caretFY, m.caretFY >= 0
	case m.ov == ovAlias, m.bandEdit:
		return m.caretMX, m.caretMY, m.caretMY >= 0
	}
	return 0, 0, false
}

// syakeScreenURL は SyakeScreen (my-products の screen-share-app) で相手を開く URL。パスワードは載せない (アプリは載っていても捨てる)。
// 形の正本はアプリの ConnectURL.parse (`syakescreen://connect?host=<宛先>&user=<ユーザー名>`)
func syakeScreenURL(host, user string) string {
	u := url.URL{Scheme: "syakescreen", Host: "connect", RawQuery: url.Values{"host": {host}, "user": {user}}.Encode()}
	return u.String()
}
