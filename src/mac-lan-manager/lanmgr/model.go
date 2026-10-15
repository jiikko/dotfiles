// Package lanmgr は mac-lan-manager の画面 (一覧・詳細カード・設定・この Mac の IP のコピー) を持つ。
//
// 単体の bin/mac-lan-manager と glogx の L の板の両方から使う (treefiler の filer と同じ分担)。描画の tick は張らない:
// 時間は呼び出し側が Advance(now) で進め、Busy の間は遅い周期で呼ぶ。外へ出す口は New / Resize / HandleInput / Advance / View /
// Busy / OwnsKeys / Binds / CaretPos / Paste / TakeExec / ExecDone / TakeNotices / Close。
// キーの語彙は docs/glogx-ui-guide.md に従う (issue 747 の「枠の見た目とキー」節の表)。
package lanmgr

import (
	"context"
	"fmt"
	"os"
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
type ExecRequest struct{ Argv []string }

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
	Close()
}

// Options は外とのつなぎ。nil のものは本物を使う。
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
	User      string // 台帳にユーザー名が無い相手に使う推測の名前 (既定は $USER)
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
	o     Options
	w, h  int
	now   time.Time
	sc    screen
	tab   int    // 0: 管理対象 / 1: 未管理
	cur   [2]int // タブごとのカーソル
	setC  int    // 設定の画面のカーソル
	ov    overlay
	ask   *ask
	copyC int

	filter    lineedit.Line
	filtering bool `layer:"filter"`
	alias     lineedit.Line
	band      lineedit.Line
	bandEdit  bool `layer:"band"`

	self    netinfo.Snapshot
	selfErr error
	host    string // この Mac のホスト名 (台帳の鍵の形)
	hosts   []ledger.Host
	peers   []discover.Peer
	scan    Scanner
	pf      prefs.Prefs

	notices []Notice
	exec    *ExecRequest

	// 入力欄に端末のカーソルを置く位置 (View が描くときに控える。-1 は入力欄が無い)
	caretFX, caretFY int // 絞り込み
	caretMX, caretMY int // 板の中の入力欄 (別名・帯)
	bandCol, bandRow int // 設定の板の帯の入力欄 (桁と、板の中の行)
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
	if o.User == "" {
		o.User = os.Getenv("USER")
	}
	m := &Model{o: o, now: o.Now(), w: 120, h: 40}
	m.reload()
	return m
}

// reload はこの Mac のネットワーク・台帳・設定を読み直し、走査を起こし直す (r)。
func (m *Model) reload() {
	if m.o.ReadNet != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		m.self, m.selfErr = m.o.ReadNet(ctx)
		cancel()
	}
	if m.o.LocalHost != nil {
		m.host = ledger.Key(m.o.LocalHost())
	}
	if m.o.Ledger != nil {
		hs, err := m.o.Ledger.Load()
		if err != nil {
			m.note("台帳が読めない: "+err.Error(), false)
		}
		m.hosts = hs
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
	if m.o.StartScan != nil {
		m.scan = m.o.StartScan(m.now)
		m.peers = m.scan.Snapshot()
	}
}

func (m *Model) note(text string, ok bool) { m.notices = append(m.notices, Notice{text, ok}) }

// Resize は画面の大きさを伝える。
func (m *Model) Resize(w, h int) { m.w, m.h = w, h }

// Advance は時間を進め、走査の途中の結果を取り込む。
func (m *Model) Advance(now time.Time) {
	m.now = now
	if m.scan != nil {
		m.peers = m.scan.Snapshot()
	}
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
func (m *Model) Busy() bool { return m.scan != nil && m.scan.Busy(m.now) }

// OwnsKeys は全部のキーを画面が持つ層 (入力欄・確認・板・設定) が開いているか (glogx は横断キーも渡す)。
func (m *Model) OwnsKeys() bool {
	return m.filtering || m.bandEdit || m.ov != ovNone || m.sc == scSettings
}

// Binds はこの画面が使うキーか (glogx の横断キーより先にこの画面へ渡す。s は status の横断キーと同じ字だが、ここでは ssh)。
func (m *Model) Binds(key string) bool {
	return slices.Contains([]string{"s", "y", "Y", "o", "m", "c", ",", "/", "?", "r", "space", "enter", "tab", "shift+tab", "h", "l", "left", "right"}, key)
}

// TakeExec は起こしてほしいプロセス (HandleInput が Exec を返したとき)。
func (m *Model) TakeExec() (ExecRequest, bool) {
	if m.exec == nil {
		return ExecRequest{}, false
	}
	r := *m.exec
	m.exec = nil
	return r, true
}

// ExecDone は起こしたプロセスから戻ったとき。起動できなかったら知らせの文を返す。
func (m *Model) ExecDone(err error) string {
	if err != nil {
		return "起こせなかった: " + err.Error()
	}
	return ""
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

// ifKind は I/F の名前がこの Mac のどの側か (インターネット側 = メイン / それ以外 = 内部)。
func (m *Model) isInternetIF(dev string) bool {
	for _, s := range m.self.Services {
		if s.Device == dev {
			return s.Internet()
		}
	}
	return false
}

// selfLan は設定に従って決めたこの Mac の内部 IP と、その出どころの説明 (推測しない。帯 / I/F で決める)。
func (m *Model) selfLan() (ip, note string) {
	if m.pf.LanByBand {
		prefix := m.pf.BandPrefix()
		if prefix == "" {
			return "", "帯が /24 でない (" + m.pf.Band + ")"
		}
		for _, x := range m.self.IPs {
			if strings.HasPrefix(x.Addr, prefix) {
				return x.Addr, "帯 " + m.pf.Band
			}
		}
		if off := m.self.Offline(); len(off) > 0 && off[0].IP != "" {
			return off[0].IP, "帯に入る IP が無い (未固定)"
		}
		return "", "帯に入る IP が無い"
	}
	for _, s := range m.self.Services {
		if s.Device == m.pf.LanIF {
			return s.IP, "I/F " + s.Device
		}
	}
	return "", "I/F " + or(m.pf.LanIF, "(未設定)") + " が無い"
}

func (m *Model) rows() []row {
	self := row{self: true, host: m.host, alias: "この Mac", user: m.o.User}
	if s, ok := m.self.Main(); ok {
		self.mainIP = s.IP
	}
	self.lanIP, self.lanNote = m.selfLan()
	for _, x := range m.self.IPs {
		if x.Addr != self.mainIP && x.Addr != self.lanIP && slices.Contains(m.pf.Kinds, x.Kind.String()) {
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
		for idx, ip := range p.Addrs {
			dev := ""
			if m.o.IfName != nil {
				dev = m.o.IfName(idx)
			}
			if m.isInternetIF(dev) {
				r.mainIP = clean(ip)
			} else if r.lanIP == "" || !r.managed {
				r.lanIP = clean(ip) // 台帳の内部 IP があればそちらを採る (固定 IP)
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

// connectIP は繋ぐ先 (内部 IP があればそちら = 有線を通す。無ければメイン)。
func (r row) connectIP() string { return or(r.lanIP, r.mainIP) }

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

// HandleInput はキーを 1 つ処理する (text は打った文字。入力欄に入れる)。
func (m *Model) HandleInput(key, text string) Action {
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
	if move(&m.cur[m.tab], len(m.visible()), key, m.h) {
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
		m.reload()
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
		m.exec = &ExecRequest{Argv: []string{"open", "vnc://" + r.connectIP()}}
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
			lan, _ = m.selfLan()
		}
		if lan != "" && !strings.HasPrefix(lan, "169.254.") {
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
	lan, _ := m.selfLan()
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
		r := m.current()
		alias := strings.TrimSpace(m.alias.String())
		// 別名だけを変える (台帳を読み直した上で。古い一覧の行で丸ごと書き戻すと、外された Mac が戻り、Bonjour の 169.254 が固定 IP の欄に入る)
		if err := m.o.Ledger.Update(r.host, func(h *ledger.Host) { h.Alias = alias }); err != nil {
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
