package lanmgr

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/jiikko/dotfiles/src/tuikit/widthenv"

	"github.com/jiikko/dotfiles/src/termsafe"

	"mac-lan-manager/discover"
	"mac-lan-manager/ledger"
	"mac-lan-manager/netinfo"
	"mac-lan-manager/prefs"
)

func TestMain(m *testing.M) { widthenv.ExitIfUnsupported(); m.Run() }

// memStore は台帳の偽物 (メモリの中だけ。本物のファイルを書かない)。
type memStore struct {
	hosts   []ledger.Host
	loadErr error
}

func (s *memStore) Load() ([]ledger.Host, error) {
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	return slices.Clone(s.hosts), nil
}
func (s *memStore) Upsert(h ledger.Host) error {
	h.Host = ledger.Key(h.Host)
	if i := slices.IndexFunc(s.hosts, func(x ledger.Host) bool { return x.Host == h.Host }); i >= 0 {
		s.hosts[i] = h
		return nil
	}
	s.hosts = append(s.hosts, h)
	return nil
}
func (s *memStore) Update(host string, f func(*ledger.Host)) error {
	i := slices.IndexFunc(s.hosts, func(x ledger.Host) bool { return x.Host == ledger.Key(host) })
	if i < 0 {
		return ledger.ErrNotFound
	}
	f(&s.hosts[i])
	return nil
}
func (s *memStore) Remove(host string) error {
	s.hosts = slices.DeleteFunc(s.hosts, func(x ledger.Host) bool { return x.Host == ledger.Key(host) })
	return nil
}

type fakeScan struct {
	err    error
	peers  []discover.Peer
	busy   bool
	closed int
}

func (f *fakeScan) Snapshot() []discover.Peer { return f.peers }
func (f *fakeScan) Busy(time.Time) bool       { return f.busy }
func (f *fakeScan) Close()                    { f.closed++ }
func (f *fakeScan) Changed() <-chan struct{}  { return nil }
func (f *fakeScan) Err() error                { return f.err }

var t0 = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

// selfNet はこの Mac のネットワークの偽物 (2026-10-15 の実測と同じ形: en0 = Wi-Fi / en13 = Thunderbolt Ethernet (169.254) / en18 は設定に無い)。
func selfNet() netinfo.Snapshot {
	return netinfo.Snapshot{
		Services: []netinfo.Service{
			{Name: "Thunderbolt Ethernet", Device: "en13", Active: true, IP: "169.254.13.42", Method: "DHCP", Read: true},
			{Name: "Wi-Fi", Device: "en0", Active: true, IP: "192.168.50.250", Method: "DHCP", Router: "192.168.50.1", Read: true},
		},
		IPs: []netinfo.IP{
			{Device: "en0", Addr: "192.168.50.250", Kind: netinfo.KindInternet},
			{Device: "en13", Addr: "169.254.13.42", Kind: netinfo.KindLinkLocal},
			{Device: "en18", Addr: "169.254.85.235", Kind: netinfo.KindLinkLocal},
			{Device: "utun4", Addr: "100.64.0.9", Kind: netinfo.KindVPN},
		},
	}
}

type harness struct {
	m      *Model
	store  *memStore
	scan   *fakeScan
	copied []string
	saved  []prefs.Prefs
}

func newHarness(t *testing.T, hosts []ledger.Host, peers []discover.Peer) *harness {
	t.Helper()
	h := &harness{store: &memStore{hosts: hosts}, scan: &fakeScan{peers: peers}}
	h.m = New(Options{
		ReadNet:   func(context.Context) (netinfo.Snapshot, error) { return selfNet(), nil },
		LocalHost: func() string { return "kojiM3MacBook-Pro.local" },
		StartScan: func(time.Time) Scanner { return h.scan },
		IfName:    func(i int) string { return map[int]string{14: "en0", 27: "en13"}[i] },
		Ledger:    h.store,
		PrefsPath: "settings.toml (偽物)",
		SavePrefs: func(_ string, p prefs.Prefs) error { h.saved = append(h.saved, p); return nil },
		Copy:      func(s string) error { h.copied = append(h.copied, s); return nil },
		Now:       func() time.Time { return t0 },
		User:      "koji",
	})
	awaitNetLoad(h.m)
	h.m.Resize(120, 40)
	return h
}

// awaitNetLoad は裏で読んでいるこの Mac のネットワークを待って取り込む (テストを読み込みの速さに依らせない)。
func awaitNetLoad(m *Model) {
	if m.netCh != nil {
		m.applyNetLoad(<-m.netCh)
	}
}

func (h *harness) keys(ks ...string) Action {
	var a Action
	for _, k := range ks {
		a = h.m.HandleKey(k)
	}
	return a
}

// bandKeys は設定を開いて帯の入力欄に入るキーの列 (設定の項目の並びが変わっても setBand に追従する)。
func bandKeys() []string {
	ks := []string{","}
	for range setBand {
		ks = append(ks, "j")
	}
	return append(ks, "enter")
}

// cursorTo はカーソルを host の行へ動かす (今のタブの中で。合わなければテストを止める: 合わないまま検査すると別の行を見て通った)。
func (h *harness) cursorTo(t *testing.T, host string) {
	t.Helper()
	h.keys("g") // 先頭から探す (下にしか動かないと、上にある行を見つけられない)
	for range len(h.m.visible()) {
		if h.m.current().host == host {
			return
		}
		h.keys("j")
	}
	if h.m.current().host != host {
		t.Fatalf("前提: カーソルを %s に合わせられない (今 %+v)", host, h.m.current())
	}
}

var mini = discover.Peer{Host: "m2macmini.local", Instances: []string{"kojiのMac mini"}, SSH: true, VNC: true, SMB: true,
	Addrs: map[int]string{14: "192.168.50.7", 27: "169.254.217.100"}}

func TestRowsMergeLedgerAndBonjour(t *testing.T) {
	h := newHarness(t, []ledger.Host{
		{Host: "studio.local", User: "koji", LAN: "192.168.100.12", Alias: "studio"}, // 台帳にあるが見えていない
		{Host: "m2macmini.local", User: "k2", LAN: "192.168.100.13", Alias: "mini"},  // 台帳にあり見えている
	}, []discover.Peer{mini, {Host: "kojis-macbook-air.local", Instances: []string{"air"}, VNC: true, Addrs: map[int]string{14: "192.168.50.44"}},
		{Host: "kojiM3MacBook-Pro.local", SSH: true, VNC: true}}) // 自分自身も Bonjour に出る
	rows := h.m.rows()
	if !rows[0].self || rows[0].lanIP != "169.254.13.42" || rows[0].mainIP != "192.168.50.250" || !rows[0].vnc {
		t.Errorf("この Mac = %+v", rows[0])
	}
	if len(rows) != 4 {
		t.Fatalf("rows = %d (自分を相手として二重に出していないか): %+v", len(rows), rows)
	}
	byHost := map[string]row{}
	for _, r := range rows[1:] {
		byHost[r.host] = r
	}
	m := byHost["m2macmini.local"]
	// 台帳の内部 IP (固定) を Bonjour の 169.254 より優先し、Wi-Fi 側 (en0 = ルーターあり) はメインに分ける
	if !m.managed || !m.seen || m.lanIP != "192.168.100.13" || m.mainIP != "192.168.50.7" || m.user != "k2" || m.alias != "mini" {
		t.Errorf("mini = %+v", m)
	}
	if s := byHost["studio.local"]; !s.managed || s.seen {
		t.Errorf("studio (見えていない台帳) = %+v", s)
	}
	if a := byHost["kojis-macbook-air.local"]; a.managed || a.lanIP != "" || a.mainIP != "192.168.50.44" {
		t.Errorf("air (未管理) = %+v", a)
	}
}

func TestTabsSplitManaged(t *testing.T) {
	h := newHarness(t, []ledger.Host{{Host: "m2macmini.local", Alias: "mini"}}, []discover.Peer{mini, {Host: "air.local", SSH: true}})
	if v := h.m.visible(); len(v) != 2 || v[1].host != "m2macmini.local" {
		t.Errorf("管理対象のタブ = %+v", v)
	}
	h.keys("l")
	if v := h.m.visible(); len(v) != 2 || v[1].host != "air.local" || !v[0].self {
		t.Errorf("未管理のタブ (この Mac は先頭に固定) = %+v", v)
	}
}

func TestSSHUsesLanIPAndHostKeyAlias(t *testing.T) {
	h := newHarness(t, []ledger.Host{{Host: "m2macmini.local", User: "k2", LAN: "192.168.100.13"}}, []discover.Peer{mini})
	if a := h.keys("j", "s"); a != Exec {
		t.Fatalf("s = %v, want Exec", a)
	}
	r, ok := h.m.TakeExec()
	want := []string{"ssh", "-o", "HostKeyAlias=m2macmini.local", "--", "k2@192.168.100.13"}
	if !ok || !slices.Equal(r.Argv, want) {
		t.Errorf("argv = %v, want %v", r.Argv, want)
	}
	if _, ok := h.m.TakeExec(); ok {
		t.Error("TakeExec が 2 回取れた")
	}
}

func TestCopyKeys(t *testing.T) {
	h := newHarness(t, nil, []discover.Peer{mini, {Host: "nosmb.local", SSH: true, Addrs: map[int]string{14: "192.168.50.9"}}})
	h.keys("l", "j") // 未管理のタブの 1 台目 (m2macmini)
	h.keys("Y", "y")
	want := []string{"ssh koji@169.254.217.100", "smb://169.254.217.100"} // 内部 (有線) を優先
	if !slices.Equal(h.copied, want) {
		t.Errorf("copied = %v, want %v", h.copied, want)
	}
	if n := h.m.TakeNotices(); len(n) != 2 || !strings.Contains(n[0].Text, "推測") {
		t.Errorf("台帳に無い相手のユーザー名は推測と知らせる: %+v", n)
	}
	h.copied = nil
	h.keys("j", "y") // smb を開いていない相手
	if len(h.copied) != 0 {
		t.Errorf("smb を開いていないのにコピーした: %v", h.copied)
	}
	if n := h.m.TakeNotices(); len(n) != 1 || n[0].OK {
		t.Errorf("断りは ✗ で出す: %+v", n)
	}
}

func TestCopySelfIP(t *testing.T) {
	h := newHarness(t, nil, nil)
	h.keys("c", "2")
	h.keys("c", "1")
	if !slices.Equal(h.copied, []string{"192.168.50.250", "169.254.13.42"}) {
		t.Errorf("copied = %v", h.copied)
	}
	h.keys("c", "j", "enter")
	if h.copied[len(h.copied)-1] != "192.168.50.250" {
		t.Errorf("j + enter で 2 番目 (メイン) をコピーしなかった: %v", h.copied)
	}
}

func TestSpaceRegistersWithConfirm(t *testing.T) {
	h := newHarness(t, nil, []discover.Peer{mini})
	h.keys("l", "j", "space")
	if !h.m.OwnsKeys() {
		t.Fatal("確認の板が開いていない")
	}
	h.keys("x") // 知らないキーは取り消し (§4)
	if len(h.store.hosts) != 0 {
		t.Fatal("取り消したのに登録した")
	}
	h.keys("space", "y")
	if len(h.store.hosts) != 1 || h.store.hosts[0].Host != "m2macmini.local" || h.store.hosts[0].User != "koji" {
		t.Fatalf("台帳 = %+v", h.store.hosts)
	}
	if h.m.count(true) != 1 {
		t.Error("登録した Mac が管理対象に移っていない")
	}
	h.keys("h") // 管理対象のタブ
	h.keys("j", "space", "enter")
	if len(h.store.hosts) != 0 {
		t.Errorf("外していない: %+v", h.store.hosts)
	}
}

func TestAliasOnlyForManaged(t *testing.T) {
	h := newHarness(t, []ledger.Host{{Host: "m2macmini.local", User: "k2"}}, []discover.Peer{mini})
	h.keys("j", "m")
	for _, r := range "mini" {
		h.m.HandleInput(string(r), string(r))
	}
	h.keys("enter")
	if h.store.hosts[0].Alias != "mini" || h.store.hosts[0].User != "k2" {
		t.Errorf("台帳 = %+v", h.store.hosts)
	}
	h.keys("l", "m") // 未管理のタブのこの Mac
	if h.m.ov == ovAlias {
		t.Error("この Mac に別名の欄を開いた")
	}
	// 台帳に無い相手 (未管理のタブの air.local) にも開かない (自分の行だけを見ていて、この分岐を消しても緑だった。監査の指摘)
	h = newHarness(t, nil, []discover.Peer{{Host: "air.local", Instances: []string{"air"}, SSH: true, Addrs: map[int]string{14: "192.168.50.9"}}})
	h.keys("l")
	h.cursorTo(t, "air.local")
	if h.m.current().managed {
		t.Fatalf("前提: air.local が管理対象になっている")
	}
	h.keys("m")
	if h.m.ov == ovAlias {
		t.Error("台帳に無い Mac に別名の欄を開いた")
	}
}

// red team の P2-1 / P2-2: 別名は別名だけを変える。Bonjour の 169.254 を台帳の固定 IP の欄に書かず、別の画面で外した Mac を戻さない。
func TestAliasDoesNotWriteBonjourIPOrResurrect(t *testing.T) {
	h := newHarness(t, []ledger.Host{{Host: "m2macmini.local", User: "k2"}}, []discover.Peer{mini})
	h.keys("j", "m", "x", "enter")
	if got := h.store.hosts[0]; got.LAN != "" || got.Alias != "x" {
		t.Errorf("台帳 = %+v (Bonjour の IP を固定 IP の欄に書いた)", got)
	}
	h.keys("m", "y")
	h.store.hosts = nil // 別の画面 (glogx の板など) が外した
	h.keys("enter")
	if len(h.store.hosts) != 0 {
		t.Errorf("外された Mac が台帳に戻った: %+v", h.store.hosts)
	}
	if n := h.m.TakeNotices(); len(n) == 0 || n[len(n)-1].OK {
		t.Errorf("外された Mac への別名は失敗で知らせる: %+v", n)
	}
}

// red team の P2-3: この Mac の行の m は断る (台帳の別名・ユーザー名を「この Mac」・$USER で上書きしない)。
func TestAliasRefusedOnSelf(t *testing.T) {
	h := newHarness(t, []ledger.Host{{Host: "kojim3macbook-pro.local", User: "other", Alias: "mine"}}, nil)
	h.keys("m", "enter")
	if got := h.store.hosts[0]; got.User != "other" || got.Alias != "mine" {
		t.Errorf("この Mac の台帳の行が書き換わった: %+v", got)
	}
}

// 登録のときも、自動割り当て (169.254) は固定 IP の欄に書かない。
func TestRegisterSkipsLinkLocal(t *testing.T) {
	h := newHarness(t, nil, []discover.Peer{mini})
	h.keys("l", "j", "space", "y")
	if len(h.store.hosts) != 1 || h.store.hosts[0].LAN != "" {
		t.Errorf("台帳 = %+v (169.254 を固定 IP の欄に書いた)", h.store.hosts)
	}
}

// red team の P3-5: IP が分からない相手の y は断る (smb:// だけをコピーしない)。
func TestCopySMBWithoutIPRefused(t *testing.T) {
	h := newHarness(t, nil, []discover.Peer{{Host: "noip.local", SMB: true}})
	h.keys("l", "j", "y")
	if len(h.copied) != 0 {
		t.Errorf("IP が無いのにコピーした: %v", h.copied)
	}
}

func TestSettingsToggleAndSave(t *testing.T) {
	h := newHarness(t, nil, nil)
	h.keys(",")
	if !h.m.OwnsKeys() {
		t.Fatal("設定の画面で全部のキーを持たない")
	}
	h.keys("space") // メイン IP を切る
	if h.m.pf.ShowMain || len(h.saved) != 1 {
		t.Errorf("pf = %+v saved = %d", h.m.pf, len(h.saved))
	}
	h.keys("j", "j", "j", "right") // 決め方 → I/F で決める
	if h.m.pf.LanByBand {
		t.Error("決め方が変わっていない")
	}
	h.keys("j", "j", "right") // I/F の候補を巡る (ルーターの無い I/F は en13 だけ)
	if h.m.pf.LanIF != "en13" {
		t.Errorf("LanIF = %q", h.m.pf.LanIF)
	}
	if ip := h.m.selfLan().IP; ip != "169.254.13.42" {
		t.Errorf("I/F で決めた内部 IP = %q", ip)
	}
	h.keys("q")
	if h.m.sc != scList {
		t.Error("q で設定を閉じない")
	}
}

func TestSelfLanByBandFindsFixedIP(t *testing.T) {
	h := newHarness(t, nil, nil)
	s := selfNet()
	s.Services[0].IP = "192.168.100.11"
	s.IPs[1].Addr = "192.168.100.11"
	h.m.self = s
	if sl := h.m.selfLan(); sl.IP != "192.168.100.11" || sl.Unfixed {
		t.Errorf("selfLan = %+v", sl)
	}
}

func TestOthersFollowKinds(t *testing.T) {
	h := newHarness(t, nil, nil)
	got := func() []string {
		var s []string
		for _, x := range h.m.rows()[0].others {
			s = append(s, x.Device)
		}
		return s
	}
	if !slices.Equal(got(), []string{"en18"}) { // 既定は VPN を含めない
		t.Errorf("others = %v", got())
	}
	h.m.pf.Kinds = append(h.m.pf.Kinds, netinfo.KindVPN.Key())
	if !slices.Equal(got(), []string{"en18", "utun4"}) {
		t.Errorf("others (VPN を含める) = %v", got())
	}
}

// red team の指摘 (テストの穴): 画面の側でも制御文字が落ちるので、画面だけを見るテストは rows() の無害化を検査できなかった。
// rows() の出力そのものを見る。ホスト名 (鍵) は書き換えず、制御文字を含むものは行ごと捨てる (P3-7: 同じ Mac が 2 行に割れない)。
func TestRowsAreSanitizedAndKeysNotRewritten(t *testing.T) {
	evil := "\x1b]0;pwned\x07evil\u202e"
	h := newHarness(t, []ledger.Host{{Host: "a.local", Alias: evil, User: evil}, {Host: "bad\u202e.local"}},
		[]discover.Peer{{Host: "b.local", Instances: []string{evil}, SSH: true}, {Host: "bad\u202e.local", SSH: true}})
	for _, r := range h.m.rows() {
		for _, f := range []string{r.host, r.alias, r.user, r.instance, r.lanIP, r.mainIP} {
			if !termsafe.IsPlain(f) {
				t.Errorf("rows() に無害化されていない値: %q (%+v)", f, r)
			}
		}
		if strings.Contains(r.host, "bad") {
			t.Errorf("制御文字を含むホスト名の行を出した: %+v", r)
		}
	}
}

// 外から来た名前 (Bonjour のインスタンス名・ホスト名・台帳の別名) に制御文字が入っていても、画面へ素通りさせない。
func TestSanitizesExternalNames(t *testing.T) {
	evil := "\x1b]0;pwned\x07evil\x1b[31m"
	h := newHarness(t, []ledger.Host{{Host: "a.local", Alias: evil, User: evil}}, []discover.Peer{{Host: "b.local", Instances: []string{evil}, SSH: true}})
	h.m.Resize(200, 40)
	for _, tab := range []string{"", "l"} {
		if tab != "" {
			h.keys(tab)
		}
		for _, l := range h.m.View() {
			if strings.Contains(l, "pwned") || strings.Contains(l, "\x07") || strings.Contains(l, "\x1b]0;") {
				t.Fatalf("制御文字が画面に出た: %q", l)
			}
		}
	}
}

func TestCloseStopsScanner(t *testing.T) {
	h := newHarness(t, nil, nil)
	h.keys("r") // 読み直しで起こし直すときも前の走査を止める
	if h.scan.closed != 1 {
		t.Errorf("読み直しで前の走査を止めていない: closed = %d", h.scan.closed)
	}
	if a := h.keys("q"); a != Quit {
		t.Fatalf("q = %v", a)
	}
	h.m.Close()
	h.m.Close() // 2 回呼んでも 2 回は止めない
	if h.scan.closed != 2 {
		t.Errorf("closed = %d, want 2", h.scan.closed)
	}
}

func TestReadNetErrorStillOpens(t *testing.T) {
	m := New(Options{ReadNet: func(context.Context) (netinfo.Snapshot, error) {
		return netinfo.Snapshot{}, errors.New("no networksetup")
	}, Now: func() time.Time { return t0 }})
	awaitNetLoad(m)
	if !m.netReady {
		t.Error("読み込みに失敗したまま「読んでいます」で操作を断り続ける")
	}
	m.Resize(100, 30)
	if lines := m.View(); len(lines) != 30 {
		t.Errorf("ネットワークが読めなくても画面は出す: %d 行", len(lines))
	}
}

// 入力欄を開いている間は、glogx の横断キー (i / R / D / F / L / U) もこの画面のキー (打っている文字を横断で消さない)。
// 開いていなければ横断キーは glogx へ譲る (s だけはここの ssh)。
func TestBindsYieldsCrossKeysOnlyOutsideInput(t *testing.T) {
	h := newHarness(t, nil, []discover.Peer{mini})
	for _, k := range []string{"i", "R", "D", "F", "L", "U"} {
		if h.m.Binds(k) {
			t.Errorf("一覧で %q を取っている (glogx の横断キーが効かない)", k)
		}
	}
	if !h.m.Binds("s") || !h.m.Binds("j") {
		t.Error("s (ssh) / j (移動) を取っていない")
	}
	h.keys("/", "i", "space", "L")
	for _, k := range []string{"i", "R", "L", "U"} {
		if !h.m.Binds(k) {
			t.Errorf("絞り込みの入力中に %q を譲った (打っている文字が横断で消える)", k)
		}
	}
	if got := h.m.filter.String(); got != "i L" {
		t.Errorf("絞り込みの文字 = %q, want %q (HandleKey が space と字を入れる)", got, "i L")
	}
}

// glogx は Space を " " に書き換えてから板へ渡す。" " でも "space" と同じく台帳の登録の確認が開く (半ページ送りに化けない)。
func TestSpaceSpelledAsBlankRegisters(t *testing.T) {
	h := newHarness(t, nil, []discover.Peer{mini})
	h.keys("l", "j")
	if !h.m.Binds(" ") {
		t.Error(`Binds(" ") が false`)
	}
	h.m.HandleKey(" ")
	if h.m.ov != ovConfirm {
		t.Fatalf(`" " で台帳の登録の確認が開かない (ov=%v)`, h.m.ov)
	}
}

// Reload は走査の settle の起点を今にする (最後の Advance の古い時刻で起こすと、開き直した直後に走査中が消える)。
func TestReloadStartsScanAtNow(t *testing.T) {
	now := t0
	var started []time.Time
	m := New(Options{
		StartScan: func(at time.Time) Scanner { started = append(started, at); return &fakeScan{} },
		Now:       func() time.Time { return now },
	})
	now = t0.Add(10 * time.Minute)
	m.Reload()
	if len(started) != 2 || !started[1].Equal(now) {
		t.Errorf("走査の起点 = %v, want 2 回目は %v", started, now)
	}
}

// ルーターの無い I/F が 2 つある相手でも、内部 IP は描くたびに同じ (I/F の番号の小さい方)。map の順で選ぶと揺れる。
func TestLanIPIsDeterministicAcrossIFs(t *testing.T) {
	net := selfNet()
	net.Services = append(net.Services, netinfo.Service{Name: "USB LAN", Device: "en14", Active: true, Method: "manual", IP: "192.168.100.2", Read: true})
	two := discover.Peer{Host: "two.local", SSH: true, Addrs: map[int]string{168: "192.168.100.5", 32: "169.254.1.1"}}
	m := New(Options{
		ReadNet:   func(context.Context) (netinfo.Snapshot, error) { return net, nil },
		StartScan: func(time.Time) Scanner { return &fakeScan{peers: []discover.Peer{two}} },
		IfName:    func(i int) string { return map[int]string{32: "en13", 168: "en14"}[i] },
		Now:       func() time.Time { return t0 },
	})
	awaitNetLoad(m)
	for i := range 200 {
		rs := m.rows()
		if got := rs[len(rs)-1].lanIP; got != "169.254.1.1" {
			t.Fatalf("%d 回目の内部 IP = %q, want 169.254.1.1 (I/F の番号の小さい方)", i, got)
		}
	}
}

// この Mac のサービスに無い I/F (VM の NAT の bridge100) で見えた相手は、内部 IP (有線・固定) にしない。その他の IP に種類つきで出し、
// 繋ぐ先は最後の手段としてそれを使う。台帳に登録しても固定 IP の欄に書かない。
func TestVirtualIFIsNotLan(t *testing.T) {
	vm := discover.Peer{Host: "vm.local", SSH: true, Addrs: map[int]string{30: "192.168.64.3"}}
	m := New(Options{
		ReadNet:   func(context.Context) (netinfo.Snapshot, error) { return selfNet(), nil },
		StartScan: func(time.Time) Scanner { return &fakeScan{peers: []discover.Peer{vm}} },
		IfName:    func(i int) string { return map[int]string{30: "bridge100"}[i] },
		Now:       func() time.Time { return t0 },
		Ledger:    &memStore{},
		User:      "koji",
	})
	awaitNetLoad(m)
	rs := m.rows()
	r := rs[len(rs)-1]
	if r.lanIP != "" || r.mainIP != "" {
		t.Errorf("VM の網の IP を内部 / メインにした: lan=%q main=%q", r.lanIP, r.mainIP)
	}
	if len(r.others) != 1 || r.others[0].Kind != netinfo.KindVirtual || r.connectIP() != "192.168.64.3" {
		t.Errorf("その他 = %+v / 繋ぐ先 = %q", r.others, r.connectIP())
	}
	m.HandleKey("l")
	m.HandleKey("j")
	m.HandleKey("space")
	m.HandleKey("y")
	hs, _ := m.o.Ledger.Load()
	if len(hs) != 1 || hs[0].LAN != "" {
		t.Errorf("台帳 = %+v (VM の網の IP を固定 IP の欄に書いた)", hs)
	}
}

type exitCodeError struct{}

func (exitCodeError) Error() string { return "exit status 255" }
func (exitCodeError) ExitCode() int { return 255 }

// ssh の終了コード (相手のシェルの最後のコマンド・接続できなかった 255) は「起こせなかった」と言わない。起動できなかったときだけ言う。
func TestExecDoneIgnoresExitCode(t *testing.T) {
	h := newHarness(t, nil, []discover.Peer{mini})
	h.keys("l", "j", "s") // ssh
	h.m.TakeExec()
	if w := h.m.ExecDone(fmt.Errorf("ssh: %w", exitCodeError{})); w != "" {
		t.Errorf("ssh の終了コードで知らせた: %q", w)
	}
	if w := h.m.ExecDone(errors.New(`exec: "ssh": executable file not found in $PATH`)); w == "" {
		t.Error("起動できなかったのに知らせない")
	}
	// open (画面共有) は開けなかったら rc=1 で戻る。こちらは終了コードを失敗として知らせる
	h.keys("o")
	if r, _ := h.m.TakeExec(); len(r.Argv) == 0 || r.Argv[0] != "open" {
		t.Fatalf("o が open を頼まない: %+v", r)
	}
	if w := h.m.ExecDone(fmt.Errorf("open: %w", exitCodeError{})); !strings.Contains(w, "開けなかった") {
		t.Errorf("open が開けなかったのに知らせない: %q", w)
	}
	if w := h.m.ExecDone(fmt.Errorf("x: %w", exitCodeError{})); w != "" {
		t.Errorf("前の open の判定が次の ExecDone に漏れた: %q", w)
	}
}

// 別名は入力欄を開いたときの Mac に付く (入力中に走査で行が増えてカーソルがずれても、別の Mac に付けない)。
func TestAliasGoesToMacOpenedFor(t *testing.T) {
	h := newHarness(t, []ledger.Host{{Host: "m2macmini.local", User: "k2"}, {Host: "studio.local", User: "koji"}}, []discover.Peer{mini})
	h.keys("j", "m")
	opened := h.m.current().host
	h.m.cur[h.m.tab]++ // 入力中にカーソルの下の行が入れ替わった
	if h.m.current().host == opened {
		t.Fatal("前提が作れていない: カーソルの下が同じ Mac のまま")
	}
	h.keys("x", "enter")
	for _, x := range h.store.hosts {
		if want := map[bool]string{true: "x", false: ""}[x.Host == opened]; x.Alias != want {
			t.Errorf("%s の別名 = %q, want %q", x.Host, x.Alias, want)
		}
	}
}

// この Mac のネットワークは裏で読む (New / Reload は待たずに返る。glogx の L が止まらない)。読み終わるまでは Busy で、Advance で取り込む。
// 読み直しが重なったら前の読み込みを取り消し (ctx が取り消される)、前の結果を待たずに新しい結果を載せる。
func TestNetLoadIsAsyncAndSuperseded(t *testing.T) {
	release, cancelled := make(chan struct{}), make(chan struct{})
	m := New(Options{
		ReadNet: func(ctx context.Context) (netinfo.Snapshot, error) {
			select { // 1 回目は止めておく (古い読み込み)
			case <-release:
			case <-ctx.Done():
				close(cancelled)
			}
			return netinfo.Snapshot{Services: []netinfo.Service{{Name: "古い", Device: "en9", Read: true}}}, nil
		},
		Now: func() time.Time { return t0 },
	})
	defer close(release)
	if !m.Busy() {
		t.Fatal("読み込み中なのに Busy でない (呼び出し側の tick が回らず取り込まれない)")
	}
	m.Advance(t0)
	if m.netReady {
		t.Fatal("New が読み込みを待った / 読み終わっていないものを取り込んだ")
	}
	m.o.ReadNet = func(context.Context) (netinfo.Snapshot, error) { return selfNet(), nil }
	m.Reload()
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("読み直しで前の読み込みを取り消していない")
	}
	done := make(chan struct{})
	go func() { awaitNetLoad(m); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("読み直しの結果が来ない (前の読み込みを待っている / 読み直していない)")
	}
	if len(m.self.Services) != 2 || m.Busy() {
		t.Errorf("読み直しの結果が取り込まれていない: %+v busy=%v", m.self.Services, m.Busy())
	}
}

// 読み込み中も「この Mac」は 1 行 (ホスト名は同期に読むので、台帳と Bonjour の自分の行を照合できる)。繋ぐ・コピー・登録は読み終えるまで断る
// (どの I/F が有線か分からないまま繋ぐ先を決めない)。読み終えたら効く。
func TestLoadingKeepsSelfSingleAndRefusesActions(t *testing.T) {
	release := make(chan struct{})
	me := discover.Peer{Host: "me.local", SSH: true, Addrs: map[int]string{14: "192.168.50.250"}}
	m := New(Options{
		ReadNet: func(ctx context.Context) (netinfo.Snapshot, error) {
			select {
			case <-release:
			case <-ctx.Done():
			}
			return selfNet(), nil
		},
		LocalHost: func() string { return "me.local" },
		StartScan: func(time.Time) Scanner { return &fakeScan{peers: []discover.Peer{me, mini}} },
		IfName:    func(i int) string { return map[int]string{14: "en0", 27: "en13"}[i] },
		// x.local は台帳に内部 IP があるので、読み込み中でも行に IP が出る (ガードが無いと s がそこへ繋ぐ)
		Ledger: &memStore{hosts: []ledger.Host{{Host: "me.local", User: "koji"}, {Host: "x.local", User: "koji", LAN: "192.168.100.9"}}},
		Now:    func() time.Time { return t0 },
		User:   "koji",
	})
	for _, r := range m.rows() {
		if !r.self && r.host == "me.local" {
			t.Fatalf("読み込み中に「この Mac」が別の行として出た: %+v", r)
		}
	}
	for _, r := range m.rows() {
		if len(r.others) > 0 || (!r.self && r.mainIP != "") {
			t.Errorf("読み込み中に相手の IP を分類した: %+v (どの I/F が有線か分からないまま「その他」に入れる)", r)
		}
	}
	m.HandleKey("j") // 管理対象のタブの x.local (台帳の内部 IP がある)
	if m.current().host != "x.local" || m.current().connectIP() == "" {
		t.Fatalf("前提が作れていない: %+v", m.current())
	}
	m.Resize(120, 40)
	foot := termwidth.StripSGR(m.footer())
	for _, k := range []string{"s 繋ぐ", "Y コピー", "space 台帳", "c この Mac"} {
		if strings.Contains(foot, k) {
			t.Errorf("読み込み中に断るキー %q を案内に出した: %q", k, foot)
		}
	}
	for _, k := range []string{"s", "Y", "y", "o", "c", "space"} {
		if a := m.HandleKey(k); a != None || m.ov != ovNone {
			t.Errorf("読み込み中の %s を受けた (action=%v ov=%v)", k, a, m.ov)
		}
	}
	m.HandleKey("l")
	m.HandleKey("j")
	close(release)
	awaitNetLoad(m)
	if a := m.HandleKey("s"); a != Exec {
		t.Errorf("読み終えた後の s が効かない: %v (notices %+v)", a, m.TakeNotices())
	}
}

// Place はカーソルが「この Mac」の行なら Host を空にする (戻すときに自分の行を探さない)。
func TestPlaceOnSelfHasNoHost(t *testing.T) {
	h := newHarness(t, nil, []discover.Peer{mini})
	if p := h.m.Place(); p.Host != "" {
		t.Errorf("この Mac の行で Host = %q", p.Host)
	}
}

// RestorePlace: カーソルの Mac が走査で後から見つかったら、見つかった時点で合わせる。走査が終わっても出なければ諦める。
// その前に自分で動かしたら、後から見つかっても跳ばない。
func TestRestorePlaceWaitsForLatePeer(t *testing.T) {
	h := newHarness(t, nil, nil)
	h.scan.busy = true
	h.m.RestorePlace(Place{Tab: 1, Host: "m2macmini.local"})
	if h.m.tab != 1 {
		t.Fatalf("タブが戻らない: %d", h.m.tab)
	}
	h.scan.peers = []discover.Peer{{Host: "a.local", SSH: true}, mini}
	h.m.Advance(t0)
	if got := h.m.current().host; got != "m2macmini.local" {
		t.Errorf("後から見つかった Mac にカーソルが合わない: %q", got)
	}

	h = newHarness(t, nil, nil)
	h.scan.busy = true
	h.m.RestorePlace(Place{Tab: 1, Host: "m2macmini.local"})
	h.keys("j") // 自分で動かした
	h.scan.peers = []discover.Peer{{Host: "a.local", SSH: true}, mini}
	h.m.Advance(t0)
	if got := h.m.current().host; got == "m2macmini.local" {
		t.Error("自分で動かした後に、後から見つかった Mac へ跳んだ")
	}

	h = newHarness(t, nil, nil)
	h.m.RestorePlace(Place{Tab: 1, Host: "gone.local"}) // 走査は終わっている
	if h.m.wantHost != "" {
		t.Error("走査が終わっているのに、居ない Mac を待ち続ける")
	}
}

// 名前の列は中身の最長に合わせて伸びる (固定の 12 桁では「kojiのMac mini」も切れた。ユーザー要望 2026-10-15)。幅が足りずホスト名の列を
// 出さないときは、その幅を名前の列へ返す (縮めたまま返さないと、ホスト名も名前も両方切れる)。
func TestNameColumnFitsContent(t *testing.T) {
	for _, w := range []int{120, 100} { // 100 はホスト名の列が入らず、名前の列へ幅を返す側 (90 は他の列だけで埋まる)
		h := newHarness(t, nil, []discover.Peer{mini})
		h.m.Resize(w, 40) // 幅の検査 (高さは一覧に全部の行が入る分)
		h.keys("l")
		if view := termwidth.StripSGR(strings.Join(h.m.View(), "\n")); !strings.Contains(view, "kojiのMac mini") {
			t.Errorf("幅 %d で名前が切れた:\n%s", w, view)
		}
	}
}

// 読み込みの結果が届いてから次の tick (Advance) までの間に押したキーも効く (キーを受けたときに取り込む)。
func TestKeyAppliesFinishedNetLoad(t *testing.T) {
	m := New(Options{
		ReadNet:   func(context.Context) (netinfo.Snapshot, error) { return selfNet(), nil },
		LocalHost: func() string { return "me.local" },
		Ledger:    &memStore{hosts: []ledger.Host{{Host: "x.local", User: "koji", LAN: "192.168.100.9"}}},
		Now:       func() time.Time { return t0 },
	})
	for deadline := time.Now().Add(5 * time.Second); len(m.netCh) == 0; {
		if time.Now().After(deadline) {
			t.Fatal("読み込みの結果が届かない")
		}
		time.Sleep(time.Millisecond) // sleep-ok: tick: 条件のポーリングの刻み (待ちの実体は上の条件)
	}
	m.HandleKey("j")
	if a := m.HandleKey("s"); a != Exec {
		t.Errorf("届いた結果を取り込まず「読んでいます」で断った: %v %+v", a, m.TakeNotices())
	}
}

// 走査そのものが失敗したら (dns-sd を起こせない等)、0 台の「居ない」と分けて見せる: 知らせを 1 回・見出しに ✗・Report に理由。
func TestScanErrorIsShownNotZero(t *testing.T) {
	h := newHarness(t, nil, nil)
	h.scan.err = errors.New("dns-sd -B _ssh._tcp を起こせない")
	h.m.TakeNotices()
	h.m.Advance(t0)
	h.m.Advance(t0)
	ns := h.m.TakeNotices()
	if len(ns) != 1 || ns[0].OK || !strings.Contains(ns[0].Text, "起こせない") {
		t.Errorf("知らせ = %+v (1 回だけ・✗ で出す)", ns)
	}
	if view := termwidth.StripSGR(strings.Join(h.m.View(), "\n")); !strings.Contains(view, "探せない") || strings.Contains(view, "台見つかった") {
		t.Errorf("見出しが「0 台見つかった」のまま:\n%s", view)
	}
	if r := h.m.Report(); !strings.Contains(r.ScanError, "起こせない") {
		t.Errorf("Report.ScanError = %q", r.ScanError)
	}
	h.keys("?") // 理由は ? の板にも出る (知らせは数秒で消える)
	if box, _, _ := h.m.overlayBox(); !strings.Contains(termwidth.StripSGR(box), "起こせない") {
		t.Errorf("? の板に走査の失敗の理由が無い")
	}
}

// 読み直し (r) で台帳が読めなかったら、前に読んだ一覧を残す (管理対象を「未管理」に化けさせない)。読めないことは知らせる。
func TestReloadKeepsLedgerOnReadError(t *testing.T) {
	h := newHarness(t, []ledger.Host{{Host: "m2macmini.local", User: "k2"}}, []discover.Peer{mini})
	h.store.loadErr = errors.New("壊れた台帳")
	h.m.TakeNotices()
	h.keys("r")
	awaitNetLoad(h.m)
	if h.m.count(true) != 1 {
		t.Errorf("台帳が読めないと管理対象が消えた (%d 台)", h.m.count(true))
	}
	if ns := h.m.TakeNotices(); len(ns) == 0 || !strings.Contains(ns[0].Text, "台帳が読めない") {
		t.Errorf("読めないことを知らせない: %+v", ns)
	}
}

// 一覧は画面の高さに収める: 行が多くても、カーソルの行と詳細カード (動作のキーの案内) は画面の中に残る。全部を描いて下で切って
// いたころは、狭い画面でカーソルの行も詳細カードも画面の外に出た (監査の指摘 2026-10-16)。
func TestListFitsHeightKeepingCursorAndDetail(t *testing.T) {
	peers := make([]discover.Peer, 0, 40)
	for i := range 40 {
		peers = append(peers, discover.Peer{Host: fmt.Sprintf("mac%02d.local", i), Instances: []string{fmt.Sprintf("Mac %02d", i)}, SSH: true,
			Addrs: map[int]string{14: fmt.Sprintf("192.168.50.%d", 100+i)}})
	}
	// 13〜17 行は詳細カードも 1 行の形も入らないので、動作のキーは最下行の案内に出る
	for _, size := range [][2]int{{120, 40}, {120, 30}, {120, 24}, {100, 18}, {100, 17}, {120, 14}, {100, 13}, {200, 30}} {
		h := newHarness(t, nil, peers)
		h.m.Resize(size[0], size[1])
		h.keys("l")
		for range 20 {
			h.keys("j")
		}
		lines := h.m.View()
		if len(lines) != size[1] {
			t.Fatalf("%v: %d 行 (高さを超えた)", size, len(lines))
		}
		view := termwidth.StripSGR(strings.Join(lines, "\n"))
		cur := h.m.current()
		if !strings.Contains(view, cur.instance) {
			t.Errorf("%v: カーソルの行 (%s) が画面に無い:\n%s", size, cur.instance, view)
		}
		// 動作のキーの案内 (s 繋ぐ) は残る。高さがあれば詳細カード、無ければ 1 行に畳んだ形で
		if !strings.Contains(view, "繋ぐ") {
			t.Errorf("%v: 動作のキーの案内 (s 繋ぐ) が画面に無い:\n%s", size, view)
		}
		if size[1] >= 40 && !strings.Contains(view, "ssh koji@") {
			t.Errorf("%v: 高さがあるのに詳細カードを畳んだ:\n%s", size, view)
		}
	}
}

// どの板も画面の高さを超えない (? の板は 22 行あり、高さ 21 以下で View が高さより多い行を返した)。
func TestOverlaysFitHeight(t *testing.T) {
	for _, key := range []string{"?", "c", "/"} {
		for _, hgt := range []int{12, 18, 21} {
			h := newHarness(t, nil, []discover.Peer{mini})
			h.m.Resize(100, hgt)
			h.keys(key)
			if n := len(h.m.View()); n != hgt {
				t.Errorf("%s の板・高さ %d: %d 行", key, hgt, n)
			}
		}
	}
}

// どの幅でも、画面は高さちょうどで、端末より広い行を出さず、抜け方 (q) は見える (幅 40 で全体が折り返して最下行の案内が消えた)。
func TestViewFitsNarrowWidths(t *testing.T) {
	for _, w := range []int{1, 8, 30, 40, 45, 46, 50, 60} {
		for _, key := range []string{"", "?", "c"} {
			h := newHarness(t, nil, []discover.Peer{mini})
			h.m.Resize(w, 24)
			if key != "" {
				h.keys(key)
			}
			lines := h.m.View()
			if len(lines) != 24 {
				t.Errorf("幅 %d %q: %d 行", w, key, len(lines))
			}
			for i, l := range lines {
				if lw := termwidth.Of(l); lw > w {
					t.Errorf("幅 %d %q: %d 行目が %d 桁", w, key, i, lw)
					break
				}
			}
			view := termwidth.StripSGR(strings.Join(lines, "\n"))
			last := strings.TrimSpace(termwidth.StripSGR(lines[len(lines)-1]))
			exit := strings.Contains(view, "q 閉じる") || (w < 20 && last == "q") // 20 桁未満の画面は最下行が「q」だけのことがある
			if key == "" && !exit {
				t.Errorf("幅 %d: 抜け方 (q 閉じる) が見えない", w)
			}
		}
	}
}

// 一覧の表のどの行も、渡した幅を超えない (超えると枠の中で折り返し、Mac ごとに 2 行になった。幅 75〜89 の既定の列で再現。監査の指摘)。
// 列を全部出す設定・長い名前でも、落とす順に列を落として収める。
func TestListTableFitsWidth(t *testing.T) {
	long := discover.Peer{Host: "studio.local", Instances: []string{"小嶋の Mac Studio (仕事用・2 階の奥の部屋)"}, SSH: true, VNC: true, SMB: true,
		Addrs: map[int]string{14: "192.168.50.254", 27: "169.254.9.9"}}
	h := newHarness(t, nil, []discover.Peer{mini, long})
	h.m.pf.ShowOthers = true
	h.keys("l")
	for w := 30; w <= 140; w += 5 {
		for i, l := range strings.Split(h.m.listTable(w, 10), "\n") {
			if lw := termwidth.Of(l); lw > w {
				t.Errorf("幅 %d: %d 行目が %d 桁 (%q)", w, i, lw, termwidth.StripSGR(l))
			}
		}
	}
}

// 最下行の案内は幅で落とすが、q (抜け方) は必ず残し、? (全部のキー) は s より先に採る (80 桁で ? が真っ先に消えた)。
// hintfit は入らない項目を飛ばして短い後続を入れるので、? が入らない狭い幅では、より短い s が入ることはある。
func TestFooterKeepsHelpKey(t *testing.T) {
	for _, w := range []int{60, 80, 100} {
		h := newHarness(t, nil, nil)
		h.m.Resize(w, 30)
		f := termwidth.StripSGR(h.m.footer())
		if !strings.Contains(f, "q 閉じる") || !strings.Contains(f, "? 全部のキー") {
			t.Errorf("幅 %d: %q", w, f)
		}
		if termwidth.Of(f) > w-2*padX {
			t.Errorf("幅 %d: 案内が %d 桁", w, termwidth.Of(f))
		}
	}
}

// 板は画面の幅に収まり、閉じ方の案内 (どのキーでも閉じる / esc やめる) が切れない (幅 66 未満で ? の板の右と案内が切れた)。
func TestOverlaysKeepHintWhenNarrow(t *testing.T) {
	for _, c := range []struct{ key, hint string }{{"?", "閉じる"}, {"c", "やめる"}} {
		for _, w := range []int{40, 50, 60} {
			h := newHarness(t, nil, []discover.Peer{mini})
			h.m.Resize(w, 40)
			h.keys(c.key)
			box, _, _ := h.m.overlayBox()
			view := termwidth.StripSGR(box)
			for _, l := range strings.Split(view, "\n") {
				if termwidth.Of(l) > w {
					t.Errorf("%s の板・幅 %d: %d 桁の行", c.key, w, termwidth.Of(l))
					break
				}
			}
			if !strings.Contains(strings.ReplaceAll(view, "\n", ""), c.hint) {
				t.Errorf("%s の板・幅 %d: 案内 (%s) が無い:\n%s", c.key, w, c.hint, view)
			}
		}
	}
}

// 設定の板: 帯の入力欄のキャレットは入力欄の行に出る (狭い幅で行が折り返し、キャレットが別の行・最下行の案内に出た)。
// 選んでいる項目は画面に入る (24 行以下で最後の項目が画面の外のままカーソルだけ進んだ)。
func TestSettingsCaretAndWindow(t *testing.T) {
	for _, size := range [][2]int{{120, 40}, {80, 24}, {60, 18}} {
		h := newHarness(t, nil, nil)
		h.m.Resize(size[0], size[1])
		h.keys(bandKeys()...) // 帯を編集
		lines := h.m.View()
		x, y, ok := h.m.CaretPos()
		if !ok || y < 0 || y >= len(lines) {
			t.Fatalf("%v: キャレットが無い / 画面の外 (%d,%d)", size, x, y)
		}
		if line := termwidth.StripSGR(lines[y]); !strings.Contains(line, "192.168.100.0/24") {
			t.Errorf("%v: キャレットの行 %d が帯の入力欄でない: %q", size, y, line)
		}
		if x <= 0 || x >= size[0] {
			t.Errorf("%v: キャレットの桁 %d が画面の外", size, x)
		}
		h.keys("esc")
		for range 20 { // 最後の項目まで
			h.keys("j")
		}
		view := termwidth.StripSGR(strings.Join(h.m.View(), "\n"))
		if last := otherKinds[len(otherKinds)-1].String(); !strings.Contains(view, last) {
			t.Errorf("%v: 選んでいる最後の項目 (%s) が画面に無い", size, last)
		}
	}
}

// 一覧で何かをするキーは全部 Binds に載っている (載っていないと、glogx の板で同じ字の横断キー・C・X・U に黙って取られる)。
// Binds は手で持つ表なので、ふるまいから突き合わせる: キーを打って状態か知らせか起こすプロセスが変わったら「使うキー」(監査の指摘)。
func TestBindsCoversHandledKeys(t *testing.T) {
	keys := []string{"space", "enter", "tab", "shift+tab", "left", "right", "up", "down", "backspace", "ctrl+d", "ctrl+u"}
	for c := '!'; c <= '~'; c++ {
		keys = append(keys, string(c))
	}
	snap := func(h *harness) string {
		m := h.m
		return fmt.Sprint(m.tab, m.cur, m.ov, m.sc, m.filtering, m.exec != nil, len(m.notices), len(h.copied), len(h.store.hosts))
	}
	for _, k := range keys {
		if k == "q" || k == "ctrl+c" { // 閉じる (glogx が自分で捌く)
			continue
		}
		h := newHarness(t, nil, []discover.Peer{mini, {Host: "b.local", SSH: true, Addrs: map[int]string{14: "192.168.50.9"}}})
		h.keys("l", "j")
		before, bound := snap(h), h.m.Binds(k) // 打つ前に見る (打った後は板が開いて Binds が全部のキーを取る)
		a := h.m.HandleKey(k)
		if (a != None || snap(h) != before) && !bound {
			t.Errorf("%q は一覧で使うのに Binds に無い (glogx の板で横取りされる)", k)
		}
	}
}

// 帯に入る IP が無く、ルーターの無い有線の IP を仮に出しているときは「未固定」の札が出る (札は判定の結果で出す。文言で判定していたので
// 文言を直すと札が消えた。監査の指摘)。帯に入る IP があれば出さない。
func TestUnfixedBadge(t *testing.T) {
	h := newHarness(t, nil, nil) // selfNet の有線は 169.254 (帯の外)
	if !strings.Contains(termwidth.StripSGR(h.m.header(120)), "未固定") {
		t.Error("帯に入る IP が無いのに「未固定」の札が無い")
	}
	s := selfNet()
	s.IPs[1].Addr = "192.168.100.11"
	h.m.self = s
	if strings.Contains(termwidth.StripSGR(h.m.header(120)), "未固定") {
		t.Error("帯に入る IP があるのに「未固定」の札を出した")
	}
}

// 帯の入力欄のキャレットは、欄の文字の直後 (行も桁も) に出る。画面に入らない高さでは出さない (案内や空の行に出ない)。
// 選んでいる行の左の棒の分 1 桁左に出ていた・低い画面で板の外に出ていた (fable の敵対レビュー)。
func TestSettingsCaretExact(t *testing.T) {
	for _, w := range []int{120, 80, 60, 48, 40} {
		for hgt := 10; hgt <= 40; hgt++ {
			h := newHarness(t, nil, nil)
			h.m.Resize(w, hgt)
			h.keys(bandKeys()...)
			lines := h.m.View()
			if len(lines) != hgt {
				t.Fatalf("%dx%d: %d 行", w, hgt, len(lines))
			}
			x, y, ok := h.m.CaretPos()
			if !ok {
				continue // 入力欄が画面に入らない高さ・幅
			}
			// 狭い欄では入力欄が末尾 (".168.100.0/24") を見せる。キャレットは文字の末尾 "0/24" の直後
			line := termwidth.StripSGR(lines[y])
			i := strings.Index(line, "0/24")
			if i < 0 {
				t.Errorf("%dx%d: キャレットの行 %d に入力欄が無い: %q", w, hgt, y, line)
				continue
			}
			if want := termwidth.Of(line[:i]) + termwidth.Of("0/24"); x != want {
				t.Errorf("%dx%d: キャレットの桁 %d (入力欄の文字の直後は %d): %q", w, hgt, x, want, line)
			}
		}
	}
}

// 一覧の板の右辺は、画面を組む幅 (minWidth) 以上のどの幅でも切れない (表の最小より狭い板では右辺が切れた)。
func TestListFrameIntactAtNarrowWidths(t *testing.T) {
	for w := minWidth; w <= 70; w++ {
		h := newHarness(t, nil, []discover.Peer{mini})
		h.m.Resize(w, 40)
		h.keys("l")
		for _, l := range h.m.View() {
			l = termwidth.StripSGR(l)
			if strings.Contains(l, "▐") && !strings.Contains(l, "▌") {
				t.Errorf("幅 %d: 板の右辺が切れた: %q", w, l)
				break
			}
		}
	}
}

// 設定の板は、どの高さでも下の枠と影まで画面に入る (枠と余白の行数を定数で数えていたころは 1 行少なく、影や下の枠が切れた)。
func TestSettingsFrameFitsHeight(t *testing.T) {
	for hgt := 18; hgt <= 40; hgt++ {
		h := newHarness(t, nil, nil)
		h.m.Resize(100, hgt)
		h.keys(",")
		view := termwidth.StripSGR(strings.Join(h.m.View(), "\n"))
		if !strings.Contains(view, "▀") || !strings.Contains(view, "▆") {
			t.Errorf("高さ %d: 設定の板の下の枠か影が切れた", hgt)
		}
	}
}

// どの幅・高さでも、選んでいる Mac の動作のキー (s 繋ぐ) と q が最下行に残る。名前が長い Mac・広い配置・低い画面を含む
// (長い名前の 1 行の形が動作のキーを押し出し、広い配置の高さ 10〜13 ではキーがどこにも出なかった。opus の敵対レビュー)。
// カーソルの行まで入らない高さは「高さが足りない」の画面になり、そこでも q は残る。
func TestFooterKeepsActionKeys(t *testing.T) {
	long := discover.Peer{Host: "long.local", Instances: []string{strings.Repeat("とても長い名前の Mac ", 6)}, SSH: true,
		Addrs: map[int]string{14: "192.168.50.9", 27: "192.168.100.9"}}
	for _, w := range []int{minWidth, 44, 60, 100, 120, 200} {
		for hgt := 10; hgt <= 24; hgt++ {
			h := newHarness(t, nil, []discover.Peer{long, mini})
			h.m.Resize(w, hgt)
			h.keys("l")
			h.cursorTo(t, "long.local")
			lines := h.m.View()
			if len(lines) != hgt {
				t.Fatalf("%dx%d: %d 行", w, hgt, len(lines))
			}
			last := "" // 最下行の案内 (枠の下の余白を除いた最後の行)
			for i := len(lines) - 1; i >= 0 && strings.TrimSpace(last) == ""; i-- {
				last = termwidth.StripSGR(lines[i])
			}
			if !strings.Contains(last, "q 閉じる") {
				t.Errorf("%dx%d: 最下行に q 閉じる が無い: %q", w, hgt, last)
			}
			view := termwidth.StripSGR(strings.Join(lines, "\n"))
			if strings.Contains(view, "高さが足りない") {
				if hgt > 12 {
					t.Errorf("%dx%d: カーソルの行が入る高さで「高さが足りない」を出した", w, hgt)
				}
				continue
			}
			// 最小の幅 36 は ? と q で埋まる (? の板から全部のキーに辿れる)
			if w >= 44 && !strings.Contains(last, "繋ぐ") {
				t.Errorf("%dx%d: 最下行に動作のキー (s 繋ぐ) が無い: %q", w, hgt, last)
			}
			if !listHasRow(lines, "192.168.100.9") {
				t.Errorf("%dx%d: 一覧の板にカーソルの行が無い:\n%s", w, hgt, view)
			}
		}
	}
}

// listHasRow は、一覧の板 (行の最初の ▐…▌) の中に want を含む行があるか。右の詳細カードや最下行の案内に同じ値が出ても数えない。
// 詳細カードを下に積む配置では、その板のコマンド (ssh koji@) と 内部 の行を除く (一覧の行には出ない語)。
func listHasRow(lines []string, want string) bool {
	for _, l := range lines {
		l = termwidth.StripSGR(l)
		i := strings.Index(l, "▐")
		if i < 0 {
			continue
		}
		j := strings.Index(l[i:], "▌")
		if j < 0 {
			continue
		}
		seg := l[i : i+j]
		if strings.Contains(seg, want) && !strings.Contains(seg, "ssh koji@") && !strings.Contains(seg, "内部") {
			return true
		}
	}
	return false
}

// 絞り込みの入力中のキャレットは、入力した文字の直後に立つ (右寄せを仮定した式は、間の空白が 1 桁に詰まる幅 43〜44 で 2 桁左にずれた)。
// 入力欄が入る幅 (43 以上) では必ず出る。全角の入力と広い配置も見る。
func TestFilterCaretExact(t *testing.T) {
	for _, in := range [][]string{{"m", "i", "n"}, {"み", "に", "ま", "っ", "く"}} {
		typed := strings.Join(in, "")
		for w := minWidth; w <= 200; w++ {
			h := newHarness(t, nil, []discover.Peer{mini})
			h.m.Resize(w, 30)
			h.keys("/")
			h.keys(in...)
			lines := h.m.View()
			x, y, ok := h.m.CaretPos()
			if !ok {
				if w >= 43 {
					t.Errorf("幅 %d (%s): 入力欄が入る幅でキャレットが出ない", w, typed)
				}
				continue
			}
			// 狭い入力欄は末尾だけを見せる。キャレットは見えている文字の直後
			line := termwidth.StripSGR(lines[y])
			i := strings.LastIndex(line, "/ ")
			txt := strings.TrimRight(line[max(i, 0)+2:], " ")
			if i < 0 || txt == "" || !strings.HasSuffix(typed, txt) {
				t.Errorf("幅 %d (%s): キャレットの行 %d に入力欄が無い: %q", w, typed, y, line)
				continue
			}
			if want := termwidth.Of(line[:i]) + 2 + termwidth.Of(txt); x != want {
				t.Errorf("幅 %d (%s): キャレットの桁 %d (入力した文字の直後は %d): %q", w, typed, x, want, line)
			}
		}
	}
}

// 幅・高さが足りない画面 (抜け方だけ) では q だけが効く。隠れた確認の板に y が届いて台帳から外れたり、
// 絞り込みに q が文字として入ったり、何も無い画面にキャレットが立ったりしない (opus の敵対レビュー)。
func TestTooSmallAcceptsOnlyQuit(t *testing.T) {
	for _, size := range [][2]int{{80, 12}, {80, 10}, {30, 40}} {
		managed := []ledger.Host{{Host: "m2macmini.local"}}
		h := newHarness(t, managed, []discover.Peer{mini})
		h.m.Resize(size[0], size[1])
		h.keys("j", "space") // 台帳から外す確認 (見えない)
		view := termwidth.StripSGR(strings.Join(h.m.View(), "\n"))
		if !strings.Contains(view, "が足りない") {
			t.Fatalf("%v: 足りない画面にならない:\n%s", size, view)
		}
		if h.keys("y") != None || len(h.store.hosts) != 1 {
			t.Errorf("%v: 見えない確認の板に y が届いた (台帳 %d 台)", size, len(h.store.hosts))
		}
		if h.m.Binds("L") || !h.m.Binds("C") {
			t.Errorf("%v: 確認の板を開いたまま足りない画面では、L (glogx の開閉) だけを渡して他は取る", size)
		}
		bare := newHarness(t, nil, []discover.Peer{mini}) // 層の無い足りない画面: 閉じるキーだけを取り、L や横断キーは glogx へ渡す
		bare.m.Resize(size[0], size[1])
		bare.m.View()
		if bare.m.Binds("L") || bare.m.Binds("C") || !bare.m.Binds("q") || !bare.m.Binds("esc") {
			t.Errorf("%v: 層の無い足りない画面で閉じるキー以外を取った", size)
		}
		if h.keys("esc") != Quit || h.keys("q") != Quit {
			t.Errorf("%v: q / esc で閉じない", size)
		}

		h = newHarness(t, nil, []discover.Peer{mini})
		h.m.Resize(size[0], size[1])
		h.keys("/", "z")
		h.m.View()
		for _, k := range []string{"C", "s", "x"} {
			if !h.m.Binds(k) {
				t.Errorf("%v: 入力欄を開いたまま足りない画面になったら %s も取る (glogx の横断キーに渡すと update が走り、文字が消えた)", size, k)
			}
		}
		if _, _, ok := h.m.CaretPos(); ok {
			t.Errorf("%v: 足りない画面にキャレットを立てた", size)
		}
		h.m.Paste("PASTED")
		if h.m.filter.String() != "z" {
			t.Errorf("%v: 見えない絞り込みに貼り付けが入った: %q", size, h.m.filter.String())
		}
		if a := h.keys("q"); a != Quit || h.m.filter.String() != "z" {
			t.Errorf("%v: 絞り込み中の q が閉じずに文字として入った (action=%v filter=%q)", size, a, h.m.filter.String())
		}

		// 大きさを戻して描き直せば、また全部のキーが効く (足りない画面の印を戻さないと、q 以外がずっと効かなかった)
		h = newHarness(t, nil, []discover.Peer{mini})
		h.m.Resize(size[0], size[1])
		h.m.View()
		h.m.Resize(120, 40)
		h.m.View()
		if h.keys("/") != None || !h.m.filtering {
			t.Errorf("%v: 大きさを戻した後に / が効かない", size)
		}
	}
}

// 閉じる (Close) と、開いていた確認・入力欄・設定を畳む。glogx は Model を使い回すので、残すと L で開き直した直後に
// 覚えの無い確認の板が y を待ち、y で台帳から外れた (足りない画面の q で抜けたとき。opus の敵対レビュー)。
func TestCloseFoldsLayers(t *testing.T) {
	band := bandKeys()
	open := map[string][]string{"確認": {"j", "space"}, "絞り込み": {"/", "z"}, "別名": {"j", "m"}, "設定": {","}, "帯の編集": band}
	for name, ks := range open {
		h := newHarness(t, []ledger.Host{{Host: "m2macmini.local"}}, []discover.Peer{mini})
		h.keys(ks...)
		if !h.m.OwnsKeys() {
			t.Fatalf("%s: 前提の層が開いていない", name)
		}
		h.m.Resize(30, 10) // 設定の画面は高さ 10 でも描くので、幅で足りない画面にする
		h.m.View()
		if h.keys("q") != Quit {
			t.Fatalf("%s: 足りない画面の q で閉じない", name)
		}
		h.m.Close() // glogx の hide と単体の終了が呼ぶ
		h.m.Reload()
		h.m.Resize(120, 40)
		h.m.View()
		if h.m.OwnsKeys() || h.m.bandEdit {
			t.Errorf("%s: 開き直したら層が残っていた", name)
		}
		h.keys("y")
		if len(h.store.hosts) != 1 {
			t.Errorf("%s: 開き直した直後の y で台帳が変わった (%d 台)", name, len(h.store.hosts))
		}
	}
}

// 最下行の s 繋ぐ は、s が効く Mac にだけ出す (ssh が閉じた未管理の Mac には出さない。案内と効くキーを同じ述語で決める)。
func TestFooterShellHintFollowsKey(t *testing.T) {
	vncOnly := discover.Peer{Host: "vnc.local", Instances: []string{"画面共有だけ"}, VNC: true, Addrs: map[int]string{14: "192.168.50.8"}}
	h := newHarness(t, nil, []discover.Peer{vncOnly})
	h.keys("l")
	h.cursorTo(t, "vnc.local")
	lines := h.m.View()
	last := termwidth.StripSGR(lines[len(lines)-padY-1])
	if strings.Contains(last, "繋ぐ") {
		t.Errorf("ssh が閉じた Mac で s 繋ぐ を出した: %q", last)
	}
	if h.keys("s") == Exec {
		t.Errorf("ssh が閉じた Mac で s が ssh を起こした")
	}
}

// どの画面でも View は高さちょうどの行を返す。設定の画面で項目の行が入らない高さは「高さが足りない」の画面になり、
// 見えない項目に space が届いて設定を書き換えない (高さを 10 とみなして描き、端末より多い行を返していた。opus の敵対レビュー)。
func TestEveryScreenFitsHeight(t *testing.T) {
	band := bandKeys()
	for _, w := range []int{minWidth, 80, 120, 160, 200} {
		for hgt := 1; hgt <= 30; hgt++ {
			for _, open := range [][]string{nil, {"G"}, {","}, {",", "G"}, band} {
				h := newHarness(t, nil, []discover.Peer{mini})
				h.keys(open...)
				h.m.Resize(w, hgt)
				lines := h.m.View()
				if len(lines) != hgt {
					t.Errorf("%dx%d %v: %d 行 (高さと違う)", w, hgt, open, len(lines))
				}
				for i, l := range lines {
					if lw := lipgloss.Width(l); lw > w {
						t.Errorf("%dx%d %v: %d 行目が %d 桁 (はみ出す)", w, hgt, open, i, lw)
					}
				}
				if len(open) == 0 || open[0] != "," {
					continue
				}
				view := termwidth.StripSGR(strings.Join(lines, "\n"))
				if h.m.small {
					before, edit, txt := len(h.saved), h.m.bandEdit, h.m.band.String()
					h.keys("space", "x", "enter")
					if len(h.saved) != before || h.m.bandEdit != edit || h.m.band.String() != txt {
						t.Errorf("%dx%d: 見えない設定の項目に space / enter が届いた", w, hgt)
					}
					continue
				}
				if !strings.Contains(view, "設定") {
					t.Errorf("%dx%d %v: 設定の画面が出ていない:\n%s", w, hgt, open, view)
				}
			}
		}
	}
}

// 台帳の user / lan は手で書き換えうる。使えない形なら使わずに知らせ、ssh の引数にもクリップボードにも出さない
// (user が "-oProxyCommand=…" だと ssh が option として読み、lan に "; echo" が付くとコピーした文字列にそのまま入った。監査の指摘)。
func TestUnusableLedgerFieldsAreNotUsed(t *testing.T) {
	bad := []ledger.Host{{Host: "m2macmini.local", User: "-oProxyCommand=touch /tmp/x", LAN: "192.168.100.13; echo pwn"}}
	h := newHarness(t, bad, []discover.Peer{mini})
	notes := h.m.TakeNotices()
	if len(notes) != 2 {
		t.Errorf("使えない欄の知らせ = %+v (user と lan の 2 つ)", notes)
	}
	r := h.m.current()
	if r.host != "m2macmini.local" {
		h.keys("j")
		r = h.m.current()
	}
	if r.user != "" || r.ledgerLAN != "" || strings.Contains(r.lanIP, "pwn") { // 内部 IP は Bonjour で見えた値で埋まってよい
		t.Fatalf("使えない欄を行に入れた: user=%q ledgerLAN=%q lan=%q", r.user, r.ledgerLAN, r.lanIP)
	}
	if h.keys("s") != Exec {
		t.Fatalf("s が ssh を起こさない")
	}
	ex, _ := h.m.TakeExec()
	for _, a := range ex.Argv {
		if strings.Contains(a, "Proxy") || strings.Contains(a, "pwn") {
			t.Errorf("ssh の引数に台帳の不正な値が入った: %q", ex.Argv)
		}
	}
	h.keys("Y")
	// 別名を付けると台帳を読み直す (reloadLedger)。その経路でも不正な値を戻さない (Reload の中だけで確かめていたころは戻った)
	h.keys("m", "enter")
	if r := h.m.current(); r.user != "" || r.ledgerLAN != "" {
		t.Errorf("別名を付けた後に不正な値が戻った: user=%q ledgerLAN=%q", r.user, r.ledgerLAN)
	}
	h.keys("Y")
	if len(h.copied) < 2 {
		t.Fatalf("前提: Y が 2 回コピーしていない: %q", h.copied)
	}
	for _, c := range h.copied {
		if strings.Contains(c, "Proxy") || strings.Contains(c, "pwn") {
			t.Errorf("コピーに台帳の不正な値が入った: %q", c)
		}
	}
}

// 帯は読んだ形 (Masked) で保存する。"192.168.100.5/24" を入れても判定は "192.168.100.0/24" なので、保存・表示もそれにそろえる。
func TestBandIsSavedNormalized(t *testing.T) {
	h := newHarness(t, nil, nil)
	h.keys(bandKeys()...)
	for range 30 {
		h.keys("backspace")
	}
	for _, r := range "192.168.100.5/24" {
		h.m.HandleInput(string(r), string(r))
	}
	h.keys("enter")
	if len(h.saved) == 0 || h.saved[len(h.saved)-1].Band != "192.168.100.0/24" {
		t.Errorf("保存した帯 = %+v", h.saved)
	}
}

// 設定ファイルが読めない (権限など) 間は、既定で描いて知らせ、切り替えても保存しない (読めなかった本物の設定を既定で上書きした。監査の指摘)。
func TestUnreadablePrefsAreNotOverwritten(t *testing.T) {
	var saved []prefs.Prefs
	m := New(Options{
		ReadNet:   func(context.Context) (netinfo.Snapshot, error) { return selfNet(), nil },
		LocalHost: func() string { return "kojiM3MacBook-Pro.local" },
		StartScan: func(time.Time) Scanner { return &fakeScan{} },
		Ledger:    &memStore{},
		PrefsPath: t.TempDir(), // ディレクトリは読めない (ErrNotExist ではない読み取りの失敗)
		SavePrefs: func(_ string, p prefs.Prefs) error { saved = append(saved, p); return nil },
		Now:       func() time.Time { return t0 },
		User:      "koji",
	})
	awaitNetLoad(m)
	m.Resize(120, 40)
	if notes := m.TakeNotices(); len(notes) == 0 || !strings.Contains(notes[0].Text, "設定が読めない") {
		t.Errorf("読めない設定を知らせない: %+v", notes)
	}
	m.HandleKey(",")
	m.HandleKey("space")
	if len(saved) != 0 {
		t.Errorf("読めなかった設定を上書きした: %+v", saved)
	}
}

// -getinfo が読めなかったサービスの I/F で見えた 169.254 は、相手の内部 IP にしない。I/F で決める設定でも、読めない I/F の IP を
// この Mac の内部 IP にしない (有線かどうかは Wired の 1 つで決める。監査の第 3 波の敵対レビュー)。
func TestUnreadServiceIsNotLan(t *testing.T) {
	net := selfNet()
	net.Services[1].Read, net.Services[1].Router = false, "" // Wi-Fi (en0) の -getinfo が読めなかった
	h := newHarness(t, nil, []discover.Peer{{Host: "p.local", Instances: []string{"p"}, SSH: true, Addrs: map[int]string{14: "169.254.5.5"}}})
	h.m.self = net
	h.m.Resize(120, 40)
	for _, r := range h.m.rows() {
		if r.host == "p.local" && r.lanIP == "169.254.5.5" {
			t.Errorf("読めない Wi-Fi で見えた 169.254 を内部 IP にした: %+v", r)
		}
	}
	// サービスに登録の無い I/F (en18) で見えた 169.254 も内部 IP にしない (Classify は名前の判定で「自動割り当て」にする)
	h.m.o.IfName = func(i int) string { return map[int]string{14: "en0", 27: "en13", 31: "en18"}[i] }
	h.m.peers = []discover.Peer{{Host: "q.local", Instances: []string{"q"}, SSH: true, Addrs: map[int]string{31: "169.254.6.6"}}}
	for _, r := range h.m.rows() {
		if r.host == "q.local" && r.lanIP == "169.254.6.6" {
			t.Errorf("登録の無い I/F で見えた 169.254 を内部 IP にした: %+v", r)
		}
	}
	h.m.pf.LanByBand, h.m.pf.LanIF = false, "en0"
	if sl := h.m.selfLan(); sl.IP != "" {
		t.Errorf("読めない I/F の IP をこの Mac の内部 IP にした: %+v", sl)
	}
}

// I/F で決める設定の候補: 保存してある I/F が今は候補に無くても、黙って別の I/F に替えて保存しない (先頭に「今は使えない」で残す)。
// 同じ I/F のサービスが 2 つあっても、→ で次の I/F へ進める (候補は I/F ごとに 1 つ。監査の第 3 波の敵対レビュー)。
func TestLanIFChoicesKeepSavedAndSkipDuplicates(t *testing.T) {
	h := newHarness(t, nil, nil)
	net := selfNet()
	net.Services = append(net.Services,
		netinfo.Service{Name: "Thunderbolt Ethernet 2", Device: "en13", Active: true, Read: true},
		netinfo.Service{Name: "USB LAN", Device: "en14", Active: true, Read: true})
	h.m.self = net
	h.m.pf.LanByBand, h.m.pf.LanIF = false, "en10" // 保存してある en10 は今は無い
	ifs := h.m.lanIFs()
	devs := make([]string, 0, len(ifs))
	for _, s := range ifs {
		devs = append(devs, s.Device)
	}
	if !slices.Equal(devs, []string{"en10", "en13", "en14"}) {
		t.Fatalf("候補 = %v (保存値を先頭に残し、en13 は 1 回だけ)", devs)
	}
	h.keys(",")
	for range setIF {
		h.keys("j")
	}
	if view := termwidth.StripSGR(strings.Join(h.m.View(), "\n")); !strings.Contains(view, "今は使えない") {
		t.Errorf("保存してある I/F が候補に無いことを画面に出さない:\n%s", view)
	}
	if len(h.saved) != 0 {
		t.Fatalf("開いただけで保存した: %+v", h.saved)
	}
	got := make([]string, 0, 3)
	for range 3 {
		h.keys("right")
		got = append(got, h.m.pf.LanIF)
	}
	if !slices.Equal(got, []string{"en13", "en14", "en13"}) { // 選び直した後は「今は使えない」の候補は消える
		t.Errorf("→ の順 = %v (en13 の 2 つ目で止まらない)", got)
	}
}

// lanmgr 側も I/F ごとの分類で決める: en0 に未接続の「Wi-Fi 2」(ルーター無し) と Wi-Fi (ルーターあり) が載っても、en0 を I/F の候補に
// 出さず、I/F で決める設定でも Wi-Fi の IP を内部 IP にしない (netinfo の Offline だけが守っていて、lanmgr の 2 か所はサービスごとの判定に
// 戻しても緑だった。監査の第 3 波の敵対レビュー)。
func TestSharedIFIsNotLanInModel(t *testing.T) {
	h := newHarness(t, nil, nil)
	net := selfNet()
	net.Services = append([]netinfo.Service{{Name: "Wi-Fi 2", Device: "en0", Active: true, IP: "192.168.50.250", Read: true}}, net.Services...)
	h.m.self = net
	h.m.pf.LanByBand, h.m.pf.LanIF = false, "en0"
	if sl := h.m.selfLan(); sl.IP != "" {
		t.Errorf("Wi-Fi の IP を内部 IP にした: %+v", sl)
	}
	for _, s := range h.m.lanIFs() {
		if s.Device == "en0" && s.Name != "(今は使えない)" {
			t.Errorf("en0 を I/F の候補に出した: %+v", s)
		}
	}
}

// 無効のサービスは内部 IP の I/F・I/F の候補・この Mac のメインにも使わない。
func TestDisabledServiceIsNotUsedInModel(t *testing.T) {
	h := newHarness(t, nil, nil)
	net := selfNet()
	net.Services = append([]netinfo.Service{{Name: "古い設定", Device: "en13", Active: true, IP: "10.9.9.9", Router: "10.9.9.1", Read: true, Disabled: true}}, net.Services...)
	h.m.self = net
	h.m.pf.LanByBand, h.m.pf.LanIF = false, "en13"
	if sl := h.m.selfLan(); sl.IP == "10.9.9.9" {
		t.Errorf("無効のサービスの IP を内部 IP にした: %+v", sl)
	}
	for _, s := range h.m.lanIFs() {
		if s.Disabled {
			t.Errorf("無効のサービスを I/F の候補にした: %+v", s)
		}
	}
	if self := h.m.rows()[0]; self.mainIP != "192.168.50.250" {
		t.Errorf("この Mac のメイン = %q (無効のサービスをメインにした)", self.mainIP)
	}
}

// u は台帳の ssh のユーザー名だけを変える (別名・内部 IP は触らない)。使えない名前 (先頭の - は ssh が option として読む) は
// 書かずに入力欄を開いたまま断り、台帳に無い Mac には開かない (issue 747 の決定 2026-10-16)。
func TestUserEditChangesOnlyUser(t *testing.T) {
	h := newHarness(t, []ledger.Host{{Host: "m2macmini.local", User: "k2", Alias: "mini", LAN: "192.168.100.13"}}, []discover.Peer{mini})
	typ := func(s string) {
		for _, r := range s {
			h.m.HandleInput(string(r), string(r))
		}
	}
	h.keys("j", "u")
	if h.m.ov != ovAlias || h.m.alias.String() != "k2" {
		t.Fatalf("u で今のユーザー名の入力欄が開かない (ov=%v %q)", h.m.ov, h.m.alias.String())
	}
	h.m.alias.Reset()
	typ("-oProxyCommand=x")
	h.keys("enter")
	if h.store.hosts[0].User != "k2" || h.m.ov != ovAlias {
		t.Fatalf("使えない名前を書いた・入力欄を閉じた: %+v ov=%v", h.store.hosts, h.m.ov)
	}
	h.m.alias.Reset()
	typ("koji")
	h.keys("enter")
	if got := h.store.hosts[0]; got != (ledger.Host{Host: "m2macmini.local", User: "koji", Alias: "mini", LAN: "192.168.100.13"}) {
		t.Errorf("台帳 = %+v (ユーザー名だけを変える)", got)
	}
	if h.keys("s"); true {
		if r, ok := h.m.TakeExec(); !ok || r.Argv[len(r.Argv)-1] != "koji@192.168.100.13" {
			t.Errorf("変えたユーザー名で繋がない: %v", r.Argv)
		}
	}

	h.keys("u") // 空にすると既定 (この Mac のユーザー名) に戻す
	h.m.alias.Reset()
	h.keys("enter")
	if h.store.hosts[0].User != "" || h.m.ov == ovAlias {
		t.Errorf("空にして既定へ戻せない: %+v", h.store.hosts)
	}
	h.keys("u") // 台帳が空なら欄も空で開く (推測の名前で開くと、そのまま enter で台帳に固定された)
	if h.m.alias.String() != "" {
		t.Errorf("台帳の空の欄を推測の名前 %q で開いた", h.m.alias.String())
	}
	h.keys("esc")

	h = newHarness(t, nil, []discover.Peer{{Host: "air.local", Instances: []string{"air"}, SSH: true, Addrs: map[int]string{14: "192.168.50.9"}}})
	h.keys("l")
	h.cursorTo(t, "air.local")
	h.keys("u")
	if h.m.ov == ovAlias {
		t.Error("台帳に無い Mac にユーザー名の欄を開いた")
	}
}

// 設定の「出す相手: 有線で見えた相手だけ」は、この Mac の有線の I/F に見えた相手だけを一覧とタブの件数に出し、設定に保存する
// (Wi-Fi でだけ見えた相手・台帳にだけある相手は出さない。issue 747 の決定)。消えた行にカーソルが残っていても描ける。
func TestWiredOnlyHidesWifiOnlyPeers(t *testing.T) {
	air := discover.Peer{Host: "air.local", Instances: []string{"air"}, SSH: true, Addrs: map[int]string{14: "192.168.50.9"}}
	h := newHarness(t, nil, []discover.Peer{mini, air})
	h.keys("l")
	h.cursorTo(t, "air.local")
	if h.m.count(false) != 2 {
		t.Fatalf("前提: 未管理が %d 台", h.m.count(false))
	}
	h.keys(",")
	for h.m.setC != setWired {
		h.keys("j")
	}
	h.keys("l", ",")
	if len(h.saved) == 0 || !h.saved[len(h.saved)-1].WiredOnly {
		t.Fatalf("有線だけを保存しない: %+v", h.saved)
	}
	hosts := []string{}
	for _, r := range h.m.visible()[1:] {
		hosts = append(hosts, r.host)
	}
	if !slices.Equal(hosts, []string{"m2macmini.local"}) || h.m.count(false) != 1 {
		t.Errorf("有線だけの一覧 = %v / 件数 %d (mini だけ)", hosts, h.m.count(false))
	}
	if v := strings.Join(h.m.View(), "\n"); !strings.Contains(v, "未管理  1") || !strings.Contains(v, "有線で見えた相手だけ") {
		t.Errorf("タブの件数が一覧と食い違う・絞っていることを出さない")
	}
	if h.m.current().host != "m2macmini.local" {
		t.Errorf("消えた行にカーソルが残った: %q", h.m.current().host)
	}
}
