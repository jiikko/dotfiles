// Package lanmgr は mac-lan-manager の画面 (一覧・詳細カード・設定・この Mac の IP のコピー) を持つ。
//
// 単体の bin/mac-lan-manager と glogx の L の板の両方から使う (treefiler の filer と同じ分担)。描画の tick は張らない:
// 時間は呼び出し側が Advance(now) で進め、Busy の間は遅い周期で呼ぶ。外へ出す口は New / Resize / HandleKey / HandleInput / Advance / View / Changed / Reload /
// Busy / OwnsKeys / Binds / CaretPos / Paste / TakeExec / ExecDone / TakeNotices / Close。
// キーの語彙は docs/glogx-ui-guide.md に従う (issue 747 の「枠の見た目とキー」節の表)。
package lanmgr

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
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
	// 開けなかったときに rc=1 で戻り、画面は TUI へすぐ戻るので理由も消える (知らせないと何も起きなかったように見えた。監査の指摘)
	ExitIsFailure bool
}

// Notice は呼び出し側が右下の toast に出す知らせ。OK=false は「効かなかった」(✗ 赤。ガイド §9)。
type Notice struct {
	Text string
	OK   bool
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
	ReadNet   func(ctx context.Context) (netinfo.Snapshot, error) // この Mac のネットワーク
	LocalHost func() string                                       // この Mac の Bonjour の名前 ("kojiM3MacBook-Pro.local")
	StartScan func(now time.Time) Scanner
	IfName    func(index int) string // I/F の番号 → 名前 (dns-sd の if 列を、この Mac の I/F に照らす)
	Ledger    ledger.Store
	PrefsPath string
	SavePrefs func(path string, p prefs.Prefs) error
	Copy      func(s string) error // クリップボードへ
	Now       func() time.Time
	User      string // 台帳にユーザー名が無い相手に使う推測の名前 (live は $USER を入れる。環境を読むのはつなぎの側)
}

type screen int

const (
	scList screen = iota
	scSettings
)

type overlay int

const (
	ovNone    overlay = iota
	ovConfirm         // y/N (台帳への登録・削除)
	ovCopyIP          // この Mac の IP のどれをコピーするか
	ovAlias           // 別名の入力欄
	ovHelp            // 全部のキー
)

// Model は画面の状態。
type Model struct {
	o            Options
	w, h         int
	now          time.Time
	sc           screen
	tab          int    // 0: 管理対象 / 1: 未管理
	cur          [2]int // タブごとのカーソル
	listOff      [2]int // タブごとの一覧の窓の先頭 (描画で WindowOffset から導く)
	listRows     int    // 一覧の窓の行数 (最後に描いたとき。半ページ送りの量)
	keysInFooter bool   // 詳細カードを出す高さが無く、動作のキーを最下行の案内に出している
	setC         int    // 設定の画面のカーソル
	ov           overlay
	ask          *ask
	copyC        int

	filter    lineedit.Line
	filtering bool `layer:"filter"`
	alias     lineedit.Line
	aliasHost string // 別名の入力欄を開いたときの Mac (台帳の鍵)
	band      lineedit.Line
	bandEdit  bool `layer:"band"`

	self      netinfo.Snapshot
	selfErr   error
	host      string // この Mac のホスト名 (台帳の鍵の形)
	hosts     []ledger.Host
	peers     []discover.Peer
	scan      Scanner
	scanErr   error              // 走査の失敗 (見せた後も持つ。Reload で消す)
	wantHost  string             // RestorePlace で戻したいカーソルの Mac (見つかるまで持つ。走査が終われば諦める)
	netCh     <-chan netLoad     // 裏で読んでいるこの Mac のネットワーク (nil = 読んでいない)
	netReady  bool               // この Mac のネットワークを一度は読み終えた (読み直しの間は前の値のまま描く)
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
}

// New は画面を作り、この Mac のネットワーク・台帳・設定を読み、Bonjour の走査を起こす。
func New(o Options) *Model {
	if o.Now == nil {
		o.Now = time.Now //nolint:forbidigo // 注入しないときの既定値 (ここだけが「今」を直に読む)
	}
	m := &Model{o: o, now: o.Now(), w: 120, h: 40}
	m.Reload()
	return m
}

// Reload はこの Mac のネットワーク・台帳・設定を読み直し、走査を起こし直す (r と、glogx の板を開き直したとき。Close の後でも使える)。
func (m *Model) Reload() {
	// 走査の settle の起点は今。最後の Advance の時刻のままだと、閉じていた間の分だけ settle が過ぎた扱いになり、開き直した直後に
	// 走査中の印が消えて tick が止まる (red team の指摘 2026-10-15)
	m.now = m.o.Now()
	// ホスト名は同期に読む (scutil 1 回。実測 12 ms)。裏へ回すと読み終わるまで「この Mac」の照合ができず、台帳と Bonjour の自分の
	// 行が別の Mac として出て、そこへ ssh できた (fable の敵対レビュー 2026-10-16)
	// 読めていれば読み直さない (ホスト名はまず変わらない。configd が詰まると scutil は上限の 3 秒まで戻らず、L を押すたびに止まる)
	if m.o.LocalHost != nil && m.host == "" {
		m.host = ledger.Key(m.o.LocalHost())
	}
	m.startNetLoad()
	if m.o.Ledger != nil {
		// 読めなければ前に読んだ一覧を残す (nil で上書きすると、管理対象の Mac が全部「未管理」に化けた。監査の指摘)
		if hs, err := m.o.Ledger.Load(); err != nil {
			m.note("台帳が読めない: "+err.Error(), false)
		} else {
			m.hosts = hs
		}
	}
	m.pf = prefs.Default()
	if m.o.PrefsPath != "" {
		if p, err := prefs.Load(m.o.PrefsPath); err == nil {
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
	self netinfo.Snapshot
	err  error
}

// startNetLoad はこの Mac のネットワーク (networksetup / ifconfig を順に起こす。実測 約 420 ms) を裏で読み始める。
// 🚨 Update の中で同期に読まない: glogx の L を押すたびに画面がその間止まった (ユーザー報告 2026-10-15)。読み終わるまでは前に読んだ値の
// ままで描き、Busy を立てて呼び出し側の tick を回させ、Advance で取り込む。読み直しが重なったら前の読み込みは取り消して結果も捨てる。
func (m *Model) startNetLoad() {
	m.stopNetLoad()
	readNet := m.o.ReadNet
	if readNet == nil {
		m.netReady = true // 読むものが無い (テスト・ネットワークを見ない呼び出し側)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	ch := make(chan netLoad, 1)
	m.netCh, m.netCancel = ch, cancel
	go func() {
		defer cancel()
		var r netLoad
		r.self, r.err = readNet(ctx)
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
	m.self, m.selfErr = r.self, r.err
	m.netReady = true
	m.netCh, m.netCancel = nil, nil
}

func (m *Model) note(text string, ok bool) { m.notices = append(m.notices, Notice{text, ok}) }

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
	if p.Tab == 0 || p.Tab == 1 {
		m.tab = p.Tab
	}
	m.wantHost = p.Host
	m.seekWantHost()
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
	m.takeNetLoad()
	if m.scan != nil {
		m.peers = m.scan.Snapshot()
		if err := m.scan.Err(); err != nil && m.scanErr == nil {
			m.scanErr = err
			m.note("Bonjour で探せない: "+err.Error(), false) // 1 回だけ (毎 tick 出さない)
		}
	}
	m.seekWantHost()
}

// Changed は走査の結果が変わった合図のチャネル (走査が無ければ nil = 待っても来ない)。呼び出し側は合図を受けたら Advance を呼び、また待つ。
// Busy の間しか tick を張らないので、これを待たないと settle の後に見つかった Mac がキーを押すまで出ない (red team の指摘)。
func (m *Model) Changed() <-chan struct{} {
	if m.scan == nil {
		return nil
	}
	return m.scan.Changed()
}

// Busy は走査が続いているか (呼び出し側はこの間、遅い周期で Advance を呼ぶ。spinner もこの間だけ回る)。
// この Mac のネットワークを裏で読んでいる間も Busy (読み終わりを Advance で取り込むため)。
func (m *Model) Busy() bool { return m.netCh != nil || (m.scan != nil && m.scan.Busy(m.now)) }

// OwnsKeys は全部のキーを画面が持つ層 (入力欄・確認・板・設定) が開いているか (glogx は横断キーも渡す)。
func (m *Model) OwnsKeys() bool {
	return m.filtering || m.bandEdit || m.ov != ovNone || m.sc == scSettings
}

// Binds はこの画面が使うキーか (glogx の横断キー・U より先にこの画面へ渡す。s は status の横断キーと同じ字だが、ここでは ssh)。
// 入力欄・確認・板・設定が開いている間は全部のキー (横断キーで閉じると打っている文字が消える。treefiler の filer.Model.Binds と同じ契約)。
func (m *Model) Binds(key string) bool {
	key = canonKey(key)
	if m.OwnsKeys() {
		return true
	}
	return slices.Contains([]string{"s", "y", "Y", "o", "m", "c", ",", "/", "?", "r", "space", "enter", "tab", "shift+tab", "h", "l", "left", "right"}, key) ||
		listnav.MotionOf(key) != listnav.None
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
// treefiler の filer.Model.ExecDone と同じ判断。全体レビューの指摘 2026-10-15)。
func (m *Model) ExecDone(err error) string {
	var exitErr interface{ ExitCode() int } // *exec.ExitError
	failOnExit := m.execExitIsFailure
	m.execExitIsFailure = false // 1 回だけ読む (次の ExecDone へ漏らさない)
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
func (m *Model) Close() {
	m.stopNetLoad()
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
	lanNote   string // 内部 IP の出どころ (自分の行だけ)
	ssh       bool
	vnc       bool
	smb       bool
	managed   bool   // 台帳に載っている
	ledgerLAN string // 台帳の内部 IP (Bonjour から見えた IP と分ける。台帳へ書き戻すのはこちらだけ)
	seen      bool   // Bonjour で見えている
	others    []netinfo.IP
	instance  string // Bonjour のインスタンス名 (別名が無いときの表示)
}

// ifKind は相手の IP が見えた I/F (この Mac の I/F の名前) の種類。この Mac のサービスとして登録された I/F だけが
// メイン (ルーターあり) / 内部 (ルーター無しの有線) になる。登録の無い I/F (VM の NAT の bridge100・VPN の utun・
// 名前を引けなかった I/F) は netinfo.Classify の種類のまま「その他」に回す: 内部 IP にすると「有線・固定」の札が付き、
// 台帳への登録で固定 IP の欄に書いてしまう (全体レビューの指摘 2026-10-15)。
func (m *Model) ifKind(dev, addr string) netinfo.Kind {
	if dev == "" {
		return netinfo.KindUnknown
	}
	wired, internet := map[string]bool{}, map[string]bool{}
	for _, s := range m.self.Services {
		if s.Internet() {
			internet[s.Device] = true
		} else {
			wired[s.Device] = true
		}
	}
	return netinfo.Classify(dev, addr, wired, internet)
}

// isServiceIF は dev がこの Mac のサービスとして登録された I/F か (169.254 も、登録された有線の上なら内部の網)。
func (m *Model) isServiceIF(dev string) bool {
	return slices.ContainsFunc(m.self.Services, func(s netinfo.Service) bool { return s.Device == dev })
}

// selfLan は設定に従って決めたこの Mac の内部 IP と、その出どころの説明 (推測しない。帯 / I/F で決める)。
// selfLanInfo はこの Mac の内部 IP と、その出どころの説明。Unfixed は「帯に入る IP が無く、ルーターの無い有線の IP を仮に出している」
// (画面の「未固定」の札。説明の文言で判定していたので、文言を直すと札が消えた。監査の指摘)。
type selfLanInfo struct {
	IP, Note string
	Unfixed  bool
}

func (m *Model) selfLan() selfLanInfo {
	if m.pf.LanByBand {
		prefix := m.pf.BandPrefix()
		if prefix == "" {
			return selfLanInfo{Note: "帯が /24 でない (" + m.pf.Band + ")"}
		}
		for _, x := range m.self.IPs {
			if strings.HasPrefix(x.Addr, prefix) {
				return selfLanInfo{IP: x.Addr, Note: "帯 " + m.pf.Band}
			}
		}
		if off := m.self.Offline(); len(off) > 0 && off[0].IP != "" {
			return selfLanInfo{IP: off[0].IP, Note: "帯に入る IP が無い (未固定)", Unfixed: true}
		}
		return selfLanInfo{Note: "帯に入る IP が無い"}
	}
	for _, s := range m.self.Services {
		if s.Device == m.pf.LanIF {
			return selfLanInfo{IP: s.IP, Note: "I/F " + s.Device}
		}
	}
	return selfLanInfo{Note: "I/F " + or(m.pf.LanIF, "(未設定)") + " が無い"}
}

func (m *Model) rows() []row {
	self := row{self: true, host: m.host, alias: "この Mac", user: m.o.User}
	if s, ok := m.self.Main(); ok {
		self.mainIP = s.IP
	}
	sl := m.selfLan()
	self.lanIP, self.lanNote = sl.IP, sl.Note
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
		// (Thunderbolt Ethernet + USB LAN・bridge0) では描くたびに内部 IP が入れ替わり、s の ssh 先が押した瞬間の運で決まった
		// (全体レビューの指摘 2026-10-15)。台帳の内部 IP があればそちらを採る (固定 IP)
		lanPicked := r.ledgerLAN != ""
		for _, idx := range slices.Sorted(maps.Keys(p.Addrs)) {
			ip := p.Addrs[idx]
			dev := ""
			if m.o.IfName != nil {
				dev = m.o.IfName(idx)
			}
			if !m.netReady {
				break // この Mac のどの I/F が有線かを読み終えるまで分けない (分けると全部「その他」になり、s がその先頭へ繋いだ)
			}
			k := m.ifKind(dev, ip)
			// 内部 IP にするのは、この Mac のサービスとして登録された有線の I/F で見えたものだけ。Classify は登録の無い I/F の
			// 169.254 も KindLinkLocal にするので、LinkLocal だけは登録を確かめる (KindWired は登録された I/F にしか付かない)
			wiredService := k == netinfo.KindWired || (k == netinfo.KindLinkLocal && m.isServiceIF(dev))
			switch {
			case k == netinfo.KindInternet:
				if r.mainIP == "" {
					r.mainIP = clean(ip)
				}
			case wiredService && !lanPicked:
				r.lanIP, lanPicked = clean(ip), true
			default:
				r.others = append(r.others, netinfo.IP{Device: dev, Addr: clean(ip), Kind: k})
			}
		}
	}
	slices.SortFunc(out, func(a, b row) int { return strings.Compare(or(a.alias, a.host), or(b.alias, b.host)) })
	return append([]row{self}, out...)
}

// visible は今のタブ・絞り込みで出す行 (この Mac はどちらのタブでも先頭)。
func (m *Model) visible() []row {
	all := m.rows()
	out := []row{all[0]}
	q := strings.ToLower(m.filter.String())
	for _, r := range all[1:] {
		if r.managed != (m.tab == 0) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(r.alias+" "+r.host+" "+r.instance), q) {
			continue
		}
		out = append(out, r)
	}
	return out
}

func (m *Model) count(managed bool) int {
	n := 0
	for _, r := range m.rows()[1:] {
		if r.managed == managed {
			n++
		}
	}
	return n
}

func (m *Model) current() row {
	vis := m.visible()
	m.cur[m.tab] = min(m.cur[m.tab], len(vis)-1)
	return vis[m.cur[m.tab]]
}

// connectIP は繋ぐ先 (内部 IP があればそちら = 有線を通す。無ければメイン。どちらも無ければその他の I/F で見えた IP。
// VM のゲストは VM の網にしか居ないので、その他を最後の手段に残す)。
func (r row) connectIP() string {
	if ip := or(r.lanIP, r.mainIP); ip != "" || len(r.others) == 0 {
		return ip
	}
	return r.others[0].Addr
}

// userFor は ssh のユーザー名 (台帳に無ければ推測の名前と、推測であることの印)。
func (m *Model) userFor(r row) (user string, guessed bool) {
	if r.user != "" {
		return r.user, false
	}
	return m.o.User, true
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// ---- キー ----

// canonKey はキーの表記を 1 つに寄せる。glogx は Space を " " に書き換えてから板へ渡す (tui.go の normalizeSpaceKey) ので、
// "space" だけを見ると glogx の板で Space が台帳の登録ではなく半ページ送り (listnav は " " も下へ送る) に化ける (red team の指摘 2026-10-15)。
func canonKey(key string) string {
	if key == " " {
		return "space"
	}
	return key
}

// HandleKey はキーを 1 つ処理する (打った文字はキーの表記から推す。KeyPressMsg.Text を持たない glogx の板の口)。
func (m *Model) HandleKey(key string) Action { return m.HandleInput(key, lineedit.KeyText(key)) }

// HandleInput はキーを 1 つ処理する (text は打った文字。入力欄に入れる)。
func (m *Model) HandleInput(key, text string) Action {
	key = canonKey(key)
	m.takeNetLoad() // 読み終えていれば、次の tick を待たずにこのキーから効かせる
	m.wantHost = "" // 自分で動かしたら、後から見つかった Mac へカーソルを跳ばさない
	if key == "ctrl+c" {
		return Quit
	}
	switch m.ov {
	case ovConfirm:
		a := m.ask
		m.ov, m.ask = ovNone, nil
		if a != nil && confirm.IsYes(key) {
			n := a.yes()
			m.note(n.Text, n.OK)
		}
		return None // 知らないキーはすべて取り消し (§4)
	case ovCopyIP:
		return m.keyCopyIP(key)
	case ovAlias:
		return m.keyAlias(key, text)
	case ovHelp:
		m.ov = ovNone // どのキーでも閉じる
		return None
	case ovNone:
	}
	if m.sc == scSettings {
		return m.keySettings(key, text)
	}
	if m.filtering { // 入力中は入力欄が先にキーを取る (§7)
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
	}
	return m.keyList(key)
}

// move はカーソルを移動の語彙 (listnav.MotionOf) で動かす。
func move(cur *int, n int, key string, rows int) bool {
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

func (m *Model) keyList(key string) Action {
	// 繋ぐ・コピー・登録は、この Mac のネットワーク (どの I/F が有線か・この Mac の IP) を読み終えてから。読み込み中に受けると
	// 繋ぐ先・登録する内部 IP を誤った分類で決める (fable の敵対レビュー 2026-10-16)
	if !m.netReady && slices.Contains([]string{"s", "Y", "y", "o", "c", "space"}, key) {
		m.note("この Mac のネットワークを読んでいます (少し待ってから)", false)
		return None
	}
	r := m.current()
	user, guessed := m.userFor(r)
	guessNote := ""
	if guessed {
		guessNote = "  (ユーザー名は推測)"
	}
	switch key {
	case "space": // 台帳への登録 / 削除 (Space = 印を付ける。ガイド §2)。一覧では半ページより先に捌く
		m.askLedger(r)
		return None
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
	case "tab", "shift+tab", "h", "l", "left", "right": // タブのある画面では h / l がタブ移動 (§2)
		m.tab = 1 - m.tab
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
		m.note("読み直した", true)
	case "enter":
		m.note("初期設定は次の段で入る (issue 747 の段 4)", false)
	case "s": // シェル (treefiler の s と同じ動詞)
		switch {
		case r.self:
			m.note("この Mac には ssh しない", false)
		case !r.ssh && !r.managed:
			m.note("ssh が閉じている (相手の画面でリモートログインをオンにしてから)", false)
		case r.connectIP() == "":
			m.note("繋ぐ IP が分からない", false)
		default:
			// known_hosts を IP でなく台帳の鍵に結び付ける (IP を付け替えても「鍵が変わった」にしない)
			m.exec = &ExecRequest{Argv: []string{"ssh", "-o", "HostKeyAlias=" + r.host, user + "@" + r.connectIP()}}
			return Exec
		}
	case "Y": // コピー (整形した参照 = コマンド)
		if r.connectIP() == "" {
			m.note("繋ぐ IP が分からない", false)
			break
		}
		m.copy(fmt.Sprintf("ssh %s@%s", user, r.connectIP()), guessNote)
	case "y": // コピー (素の値 = URL)
		if !r.smb {
			m.note("ファイル共有 (smb / 445) を開いていない", false)
			break
		}
		if r.connectIP() == "" {
			m.note("繋ぐ IP が分からない", false)
			break
		}
		m.copy("smb://"+r.connectIP(), "")
	case "o": // 外のアプリで開く (§2)
		if !r.vnc || r.connectIP() == "" {
			m.note("画面共有を広告していない", false)
			break
		}
		m.exec = &ExecRequest{Argv: []string{"open", "vnc://" + r.connectIP()}, ExitIsFailure: true}
		return Exec
	case "m": // 名前の変更 (treefiler と同じ動詞)
		if r.self {
			m.note("この Mac には別名を付けない", false)
			break
		}
		if !r.managed {
			m.note("台帳に無い Mac には別名を付けない (先に space で台帳に登録する)", false)
			break
		}
		m.alias.Reset()
		m.alias.Insert(r.alias)
		m.aliasHost = r.host // 確定の時点のカーソル行ではなく、開いたときの Mac に付ける (入力中に走査で行が増えるとカーソルがずれる)
		m.ov = ovAlias
	}
	return None
}

func (m *Model) copy(s, note string) {
	if m.o.Copy == nil {
		return
	}
	if err := m.o.Copy(s); err != nil {
		m.note("コピーできない: "+err.Error(), false)
		return
	}
	m.note("コピーした: "+s+note, true)
}

// askLedger は台帳への登録 / 削除の確認を出す (どちらも y/N。§4)。
func (m *Model) askLedger(r row) {
	if m.o.Ledger == nil {
		return
	}
	if r.managed {
		m.ask = &ask{title: "台帳から外す", body: []string{r.host + " を管理対象から外す (Mac には何もしない)"}, yes: func() Notice {
			if err := m.o.Ledger.Remove(r.host); err != nil {
				return Notice{"外せない: " + err.Error(), false}
			}
			m.reloadLedger()
			return Notice{"台帳から外した: " + r.host, true}
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
		if lan != "" && !netinfo.IsLinkLocal(lan) {
			h.LAN = lan
		}
		m.ask = &ask{title: "台帳に登録する", body: []string{
			r.host + " を管理対象にする (Mac には何もしない。初期設定は別)",
			"user " + h.User + " / 内部 IP " + or(h.LAN, "-"),
		}, yes: func() Notice {
			if err := m.o.Ledger.Upsert(h); err != nil {
				return Notice{"登録できない: " + err.Error(), false}
			}
			m.reloadLedger()
			return Notice{"台帳に登録した: " + r.host, true}
		}}
	}
	m.ov = ovConfirm
}

func (m *Model) reloadLedger() {
	if hs, err := m.o.Ledger.Load(); err == nil {
		m.hosts = hs
	}
}

func (m *Model) keyCopyIP(key string) Action {
	if move(&m.copyC, 2, key, 2) {
		return None
	}
	switch key {
	case "1", "2":
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

func (m *Model) copySelf(i int) {
	ip := m.selfIPs()[i]
	label := []string{"内部", "メイン"}[i]
	if ip == "" {
		m.note("この Mac の"+label+"の IP が無い", false)
		return
	}
	m.copy(ip, "  (この Mac の"+label+")")
}

func (m *Model) keyAlias(key, text string) Action {
	switch key {
	case "esc":
		m.ov = ovNone
	case "enter":
		m.ov = ovNone
		alias := strings.TrimSpace(m.alias.String())
		// 別名だけを変える (台帳を読み直した上で。古い一覧の行で丸ごと書き戻すと、外された Mac が戻り、Bonjour の 169.254 が固定 IP の欄に入る)
		if err := m.o.Ledger.Update(m.aliasHost, func(h *ledger.Host) { h.Alias = alias }); err != nil {
			m.note("別名を付けられない: "+err.Error(), false)
			m.reloadLedger()
			return None
		}
		m.reloadLedger()
		m.note("別名を付けた: "+or(alias, "(無し)"), true)
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
