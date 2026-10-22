package lanmgr

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/jiikko/dotfiles/src/tuikit/layout"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/jiikko/dotfiles/src/tuikit/widthenv"

	"github.com/jiikko/dotfiles/src/termsafe"

	"mac-lan-manager/discover"
	"mac-lan-manager/ledger"
	"mac-lan-manager/netinfo"
	"mac-lan-manager/prefs"
	"mac-lan-manager/setup"
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
func (s *memStore) Update(host string, f func(*ledger.Host)) error {
	i := slices.IndexFunc(s.hosts, func(x ledger.Host) bool { return x.Host == ledger.Key(host) })
	if i < 0 {
		return ledger.ErrNotFound
	}
	f(&s.hosts[i])
	s.hosts[i].Host = ledger.Key(host) // 本物 (FileStore.edit) と同じく鍵は変えさせない
	return nil
}
func (s *memStore) Merge(h ledger.Host, f func(*ledger.Host)) error {
	if err := s.Update(h.Host, f); !errors.Is(err, ledger.ErrNotFound) {
		return err
	}
	if h.Host = ledger.Key(h.Host); h.Host == "" {
		return errors.New("ホスト名が空") // 本物 (FileStore.Merge) と同じく足さない
	}
	s.hosts = append(s.hosts, h)
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
			{Name: "Thunderbolt Ethernet", Port: "Thunderbolt Ethernet", Device: "en13", Active: true, IP: "169.254.13.42", Method: "DHCP", Read: true},
			{Name: "Wi-Fi", Port: "Wi-Fi", Device: "en0", Active: true, IP: "192.168.50.250", Method: "DHCP", Router: "192.168.50.1", Read: true},
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
	if v := h.m.visible(); len(v) != 1 || v[0].host != "m2macmini.local" {
		t.Errorf("管理対象のタブ = %+v", v)
	}
	h.keys("l")
	if v := h.m.visible(); len(v) != 1 || v[0].host != "air.local" {
		t.Errorf("未管理のタブ (この Mac は出さない) = %+v", v)
	}
	h.keys("l")
	if v := h.m.visible(); len(v) != 1 || !v[0].self {
		t.Errorf("この Mac のタブ = %+v", v)
	}
	h.keys("l")
	if h.m.tab != tabManaged {
		t.Errorf("この Mac のタブの次 = %d (管理対象へ戻る)", h.m.tab)
	}
	h.keys("h")
	if h.m.tab != tabSelf {
		t.Errorf("管理対象の前 = %d (この Mac のタブ)", h.m.tab)
	}
}

// 相手のタブが空でも落ちず、行に効くキーは断る (この Mac の行が必ずあった頃の前提が外れた)。
func TestEmptyPeerTab(t *testing.T) {
	h := newHarness(t, nil, nil)
	h.keys("j", "k", "G")
	if a := h.keys("s"); a != None {
		t.Errorf("行の無いタブで s が %v", a)
	}
	if n := h.m.TakeNotices(); len(n) == 0 || !strings.Contains(n[len(n)-1].Text, "Mac が無い") {
		t.Errorf("断った理由を出していない: %+v", n)
	}
	if v := strings.Join(h.m.View(), "\n"); !strings.Contains(v, "管理対象の Mac はまだ無い") {
		t.Errorf("空のタブの案内が無い:\n%s", v)
	}
}

func TestSSHUsesLanIPAndHostKeyAlias(t *testing.T) {
	h := newHarness(t, []ledger.Host{{Host: "m2macmini.local", User: "k2", LAN: "192.168.100.13"}}, []discover.Peer{mini})
	setSelfStatic(h.m) // この Mac も帯の中 (同じ網でないと有線では繋がない)
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
	h.keys("l") // 未管理のタブの 1 台目 (m2macmini)
	h.keys("Y", "y", "O")
	want := []string{"ssh koji@169.254.217.100", "smb://169.254.217.100", "open smb://169.254.217.100"} // 内部 (有線) を優先
	if !slices.Equal(h.copied, want) {
		t.Errorf("copied = %v, want %v", h.copied, want)
	}
	if n := h.m.TakeNotices(); len(n) != 3 || !strings.Contains(n[0].Text, "推測") {
		t.Errorf("台帳に無い相手のユーザー名は推測と知らせる: %+v", n)
	}
	h.copied = nil
	h.keys("j", "y", "O") // smb を開いていない相手
	if len(h.copied) != 0 {
		t.Errorf("smb を開いていないのにコピーした: %v", h.copied)
	}
	if n := h.m.TakeNotices(); len(n) != 2 || n[0].OK || n[1].OK {
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
	// 台帳に無い相手 (未管理のタブの air.local) にも開かない (自分の行だけを見ていて、この分岐を消しても緑だった)
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

// 別名は別名だけを変える。Bonjour の 169.254 を台帳の固定 IP の欄に書かず、別の画面で外した Mac を戻さない。
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

// この Mac の行の m は断る (台帳の別名・ユーザー名を「この Mac」・$USER で上書きしない)。
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

// IP が分からない相手の y は断る (smb:// だけをコピーしない)。
func TestCopySMBWithoutIPRefused(t *testing.T) {
	h := newHarness(t, nil, []discover.Peer{{Host: "noip.local", SMB: true}})
	h.keys("l", "j", "y", "O")
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
	// 帯は 169.254 の中にあるので、自動の番号が帯を引くことがある: 方式が手動でなければ未固定
	if sl := h.m.selfLan(); sl.IP != "192.168.100.11" || !sl.Unfixed {
		t.Errorf("DHCP で帯を引いた番号を固定と見た: %+v", sl)
	}
	s.Services[0].Method = "manual"
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

// 画面の側でも制御文字が落ちるので、画面だけを見るテストは rows() の無害化を検査できなかった。
// rows() の出力そのものを見る。ホスト名 (鍵) は書き換えず、制御文字を含むものは行ごと捨てる (同じ Mac が 2 行に割れない)。
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
	h.keys("m")
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
	m.TakeNotices()
	// 初期設定は詳細のパネルの enter (一覧の enter はパネルを開くだけで、読み込み中でも開ける)
	for _, ks := range [][]string{{"s"}, {"Y"}, {"y"}, {"O"}, {"o"}, {"a"}, {"c"}, {"space"}, {"enter", "enter"}} {
		k := strings.Join(ks, " ")
		var a Action
		for _, key := range ks {
			a = m.HandleKey(key)
		}
		if a != None || m.ov != ovNone || m.sc == scSetup {
			t.Errorf("読み込み中の %s を受けた (action=%v ov=%v)", k, a, m.ov)
		}
		// 断った理由は読み込み中だから (行の事情 = smb が無い等で断ったのでは、needsNet から漏れても緑になる)
		if n := m.TakeNotices(); len(n) == 0 || !strings.Contains(n[len(n)-1].Text, "確認中") {
			t.Errorf("読み込み中の %s を、読み込み中だからと断っていない: %+v", k, n)
		}
		if m.detailOpen {
			m.HandleKey("esc")
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
	h.keys("l", "l") // この Mac のタブ (管理対象のタブは空で、この Mac の行を通らなかった)
	if !h.m.current().self {
		t.Fatal("前提: カーソルがこの Mac の行に無い")
	}
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
		ReadNet: func(context.Context) (netinfo.Snapshot, error) {
			s := selfNet() // この Mac も帯の中 (同じ網でないと有線では繋がない)
			s.Services[0].Method, s.Services[0].IP = "manual", "192.168.100.10"
			s.IPs[1].Addr = "192.168.100.10"
			return s, nil
		},
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
// いたころは、狭い画面でカーソルの行も詳細カードも画面の外に出た。
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

// 一覧の表のどの行も、渡した幅を超えない (超えると枠の中で折り返し、Mac ごとに 2 行になった。幅 75〜89 の既定の列で再現)。
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

// 一覧が窓に収まらないときだけ右端にスクロールバーを置く。バーがあっても表のどの行も渡した幅を超えず、見出しと罫線も同じ幅に揃う。
func TestListTableScrollbarOnlyWhenOverflow(t *testing.T) {
	long := discover.Peer{Host: "studio.local", Instances: []string{"小嶋の Mac Studio (仕事用・2 階の奥の部屋)"}, SSH: true, VNC: true, SMB: true,
		Addrs: map[int]string{14: "192.168.50.254", 27: "169.254.9.9"}}
	h := newHarness(t, nil, []discover.Peer{mini, long})
	h.m.pf.ShowOthers = true
	h.keys("l")
	hasBar := func(s string) bool { return strings.Contains(s, layout.ScrollbarThumb) }
	if hasBar(h.m.listTable(80, 10)) {
		t.Errorf("窓に収まるのにバーが出ている")
	}
	for w := minWidth; w <= 140; w += 5 { // minWidth: 画面を組む最小の幅 (これ未満は View が表を出さない)
		out := h.m.listTable(w, 1) // 2 行以上を 1 行の窓に入れる = 収まらない
		if !hasBar(out) {
			t.Errorf("幅 %d: 収まらないのにバーが無い", w)
		}
		for i, l := range strings.Split(out, "\n") {
			if lw := termwidth.Of(l); lw != w {
				t.Errorf("幅 %d: %d 行目が %d 桁 (%q)", w, i, lw, termwidth.StripSGR(l))
			}
		}
	}
}

// 縦積みのとき、一覧の窓の行数 (とバーの有無) は選んだ行で変わらない。詳細カードの高さは行ごとに違う (IP が多い相手は行が増える) が、
// それを選んだ行の分だけ引くと、カーソルを動かすだけで窓が伸び縮みし、4 件目を選んだときだけバーが出たり消えたりした。
func TestListWindowDoesNotDependOnSelectedRow(t *testing.T) {
	ps := make([]discover.Peer, 0, 4)
	for i := range 4 {
		p := discover.Peer{Host: fmt.Sprintf("mac%02d.local", i), SSH: true, Addrs: map[int]string{14: fmt.Sprintf("192.168.50.%d", i+2)}}
		if i == 3 {
			p.VNC, p.SMB = true, true
			p.Addrs = map[int]string{14: "192.168.50.9", 27: "169.254.9.9", 5: "10.0.0.5"}
		}
		ps = append(ps, p)
	}
	for hh := 14; hh <= 40; hh++ {
		h := newHarness(t, nil, ps)
		h.m.Resize(80, hh)
		h.m.pf.ShowOthers = true
		h.keys("l")
		var rows []int
		var bars []bool
		for range ps {
			v := strings.Join(h.m.View(), "\n")
			rows = append(rows, h.m.listRows)
			bars = append(bars, strings.Contains(v, layout.ScrollbarThumb))
			h.keys("j")
		}
		for c := range rows {
			if rows[c] != rows[0] || bars[c] != bars[0] {
				t.Errorf("高さ %d: 窓の行数 %v / バー %v (選ぶ行で変わった)", hh, rows, bars)
				break
			}
		}
	}
}

// 初期設定は全画面に切り替えず、開いた詳細のパネルを下に残して、その上へ右に stackDX 桁・下に stackDY 行ずらして重なる
// (ドラクエの戦闘画面のように選んだ順に積む。esc で 1 枚ずつ戻る)。手順のカードが入らない狭い端末では全画面。
func TestSetupStacksOnDetailPanel(t *testing.T) {
	ps := make([]discover.Peer, 0, 4)
	for i := range 4 {
		ps = append(ps, discover.Peer{Host: fmt.Sprintf("mac%02d.local", i), SSH: true, Addrs: map[int]string{14: fmt.Sprintf("192.168.50.%d", i+2)}})
	}
	open := func(w, hh int) (*harness, []string) {
		h := newHarness(t, nil, ps)
		h.m.Resize(w, hh)
		h.keys("l", "j", "enter", "enter")
		if h.m.sc != scSetup || !h.m.detailOpen {
			t.Fatalf("%dx%d: 初期設定がパネルの上に開かない: sc=%d open=%v", w, hh, h.m.sc, h.m.detailOpen)
		}
		lines := h.m.View()
		for i, l := range lines {
			lines[i] = termwidth.StripSGR(l)
		}
		return h, lines
	}
	rowOf := func(lines []string, sub string) int {
		for i, l := range lines {
			if strings.Contains(l, sub) {
				return i
			}
		}
		return -1
	}
	// 72x20 は手順のカード自体が入らない (重ねる前から「高さが足りない」)
	for _, sz := range [][2]int{{100, 22}, {100, 46}, {72, 26}} {
		h, lines := open(sz[0], sz[1])
		r := rowOf(lines, "初期設定  mac01.local")
		if r < 0 {
			t.Fatalf("%v: 初期設定のタイルが無い:\n%s", sz, strings.Join(lines, "\n"))
		}
		// 題の行: パネルの左の枠 (padX 桁目) の stackDX 桁右にタイルの左の枠。パネルを描かないと前者が無く、ずらさないと 2 つが重なる
		if want := strings.Repeat(" ", padX) + "▐" + strings.Repeat(" ", stackDX-1) + "▐"; !strings.HasPrefix(lines[r], want) {
			t.Errorf("%v: タイルがパネルの上にずれて重なっていない: %q", sz, lines[r])
		}
		// パネルの上辺はタイルの上辺の stackDY 行上に覗く (card の上辺は左の枠の 1 桁右から始まる)。ずらさないと上辺どうしが同じ行に重なる
		if top := lines[h.m.panelTop]; !strings.HasPrefix(top, strings.Repeat(" ", padX+1)+"▂▂") {
			t.Errorf("%v: パネルの上辺が覗かない: %q", sz, top)
		}
		if tt := lines[h.m.panelTop+stackDY]; !strings.HasPrefix(tt, strings.Repeat(" ", padX)+"▐"+strings.Repeat(" ", stackDX)+"▂") {
			t.Errorf("%v: タイルの上辺がパネルの上辺の %d 行下にない: %q", sz, stackDY, tt)
		}
		last := ""
		for i := len(lines) - 1; i >= 0 && last == ""; i-- {
			last = strings.TrimSpace(lines[i])
		}
		if !strings.Contains(last, "戻る") || !strings.Contains(last, "x 実行") {
			t.Errorf("%v: 最下行が初期設定の案内でない (タイルに覆われた): %q", sz, last)
		}
		// esc で 1 枚ずつ戻る: 初期設定 → 詳細のパネル → 一覧
		h.keys("esc")
		if h.m.sc != scList || !h.m.detailOpen {
			t.Errorf("%v: esc で詳細のパネルへ戻らない: sc=%d open=%v", sz, h.m.sc, h.m.detailOpen)
		}
		if v := termwidth.StripSGR(strings.Join(h.m.View(), "\n")); strings.Contains(v, "初期設定  mac01.local") || !strings.Contains(v, "mac01.local") {
			t.Errorf("%v: 戻った後にパネルが見えない / 初期設定が残る:\n%s", sz, v)
		}
		h.keys("esc")
		if h.m.detailOpen {
			t.Errorf("%v: 2 回目の esc でパネルが閉じない", sz)
		}
	}
	// どの高さでも、タイルは最下行 (抜け方の案内) とその上の空行を覆わない (実行するもののカードを足すかを高さで決めるので、境の高さで覆いうる)
	for hh := 22; hh <= 46; hh++ {
		_, lines := open(100, hh)
		f := len(lines) - 1
		for f > 0 && strings.TrimSpace(lines[f]) == "" {
			f--
		}
		if !strings.Contains(lines[f], "x 実行") || strings.TrimSpace(lines[f-1]) != "" {
			t.Errorf("100x%d: タイルが最下行かその上の空行を覆った:\n%s\n%s", hh, lines[f-1], lines[f])
		}
	}
	if _, lines := open(100, 46); rowOf(lines, "この手順で実行するもの") < 0 {
		t.Errorf("100x46: 高い端末で実行するもののカードが無い")
	}
	// タイルの幅 (stackDX と右の余白を引いた残り) が手順の行の最小に届かない狭い端末: 全画面 (一覧のタブは出ない)
	_, lines := open(62, 40)
	if all := strings.Join(lines, "\n"); !strings.Contains(all, "初期設定  mac01.local") || strings.Contains(all, "未管理  4") {
		t.Errorf("62x40: 全画面にならない:\n%s", all)
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
// Binds は手で持つ表なので、ふるまいから突き合わせる: キーを打って状態か知らせか起こすプロセスが変わったら「使うキー」。
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
// 文言を直すと札が消えた)。帯に入る IP があれば出さない。
func TestUnfixedBadge(t *testing.T) {
	h := newHarness(t, nil, nil) // selfNet の有線は 169.254 (帯の外)
	if !strings.Contains(termwidth.StripSGR(h.m.header(120)), "未固定") {
		t.Error("帯に入る IP が無いのに「未固定」の札が無い")
	}
	s := selfNet()
	s.Services[0].IP, s.Services[0].Method = "192.168.100.11", "manual"
	s.IPs[1].Addr = "192.168.100.11"
	h.m.self = s
	if strings.Contains(termwidth.StripSGR(h.m.header(120)), "未固定") {
		t.Error("帯に入る IP があるのに「未固定」の札を出した")
	}
}

// 帯の入力欄のキャレットは、欄の文字の直後 (行も桁も) に出る。画面に入らない高さでは出さない (案内や空の行に出ない)。
// 選んでいる行の左の棒の分 1 桁左に出ていた・低い画面で板の外に出ていた。
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
// (長い名前の 1 行の形が動作のキーを押し出し、広い配置の高さ 10〜13 ではキーがどこにも出なかった)。
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
		for _, wired := range []bool{false, true} { // 有線だけに絞っているとタブの横に札が付く (札の分で入力欄が消えた。敵対レビュー)
			typed := strings.Join(in, "")
			for w := minWidth; w <= 200; w++ {
				h := newHarness(t, nil, []discover.Peer{mini})
				h.m.pf.WiredOnly = wired
				h.m.Resize(w, 30)
				h.keys("/")
				h.keys(in...)
				lines := h.m.View()
				x, y, ok := h.m.CaretPos()
				if !ok {
					if w >= 46 { // 実測 (この Mac のタブを足して 43 → 46。狭い幅ではタブの名前を縮める)
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
}

// 幅・高さが足りない画面 (抜け方だけ) では q だけが効く。隠れた確認の板に y が届いて台帳から外れたり、
// 絞り込みに q が文字として入ったり、何も無い画面にキャレットが立ったりしない。
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
// 覚えの無い確認の板が y を待ち、y で台帳から外れた (足りない画面の q で抜けたとき)。
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
// 見えない項目に space が届いて設定を書き換えない (高さを 10 とみなして描き、端末より多い行を返していた)。
func TestEveryScreenFitsHeight(t *testing.T) {
	band := bandKeys()
	for _, w := range []int{minWidth, 80, 120, 160, 200} {
		for hgt := 1; hgt <= 30; hgt++ {
			for _, open := range [][]string{nil, {"G"}, {","}, {",", "G"}, band, {"e"}} {
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
// (user が "-oProxyCommand=…" だと ssh が option として読み、lan に "; echo" が付くとコピーした文字列にそのまま入った)。
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

// 設定ファイルが読めない (権限など) 間は、既定で描いて知らせ、切り替えても保存しない (読めなかった本物の設定を既定で上書きした)。
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
// この Mac の内部 IP にしない (有線かどうかは Wired の 1 つで決める)。
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
// 同じ I/F のサービスが 2 つあっても、→ で次の I/F へ進める (候補は I/F ごとに 1 つ)。
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
// 戻しても緑だった)。
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
// 書かずに入力欄を開いたまま断り、台帳に無い Mac には開かない。
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
	setSelfStatic(h.m) // この Mac も帯の中 (同じ網でないと有線では繋がない)
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
// (Wi-Fi でだけ見えた相手・台帳にだけある相手は出さない)。消えた行にカーソルが残っていても描ける。
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
	for _, r := range h.m.visible() {
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

// $USER が使えない (空・先頭の -) と、台帳にユーザー名の無い相手への s / Y は断る (宛先が "@<IP>" になった)。
// u で空にしたときの知らせには、実際に使う既定の名前を出す。
func TestUnknownUserRefusesShell(t *testing.T) {
	h := newHarness(t, nil, []discover.Peer{mini})
	h.m.o.User = ""
	h.keys("l", "j")
	if h.keys("s") == Exec || len(h.copied) != 0 {
		t.Fatal("ユーザー名が分からないのに ssh を起こした")
	}
	h.keys("Y")
	if len(h.copied) != 0 {
		t.Errorf("ユーザー名が分からないのに ssh のコマンドをコピーした: %v", h.copied)
	}
	if m := New(Options{User: "-oProxyCommand=x", Now: func() time.Time { return t0 }}); m.o.User != "" {
		t.Errorf("使えない $USER を既定のユーザー名にした: %q", m.o.User)
	}

	h = newHarness(t, []ledger.Host{{Host: "m2macmini.local", User: "k2"}}, []discover.Peer{mini})
	h.keys("j", "u")
	h.m.alias.Reset()
	h.keys("enter")
	if n := h.m.TakeNotices(); len(n) == 0 || !strings.Contains(n[len(n)-1].Text, "既定 (koji)") {
		t.Errorf("空にしたときに使う既定の名前を知らせない: %+v", n)
	}
}

// 知らせは e のメッセージ一覧に残り (新しいものが上)、y で選んだ 1 件を、Y で全部を、切らずにコピーする。相手を読めなかった理由
// (ssh の stderr) は toast にせず一覧にだけ残す。件数は上限で古いものから捨てる (ユーザー要望 2026-10-16)。
func TestMessageLogCopies(t *testing.T) {
	h := newHarness(t, nil, []discover.Peer{mini})
	h.keys("h", "s") // この Mac のタブの行: 断りの知らせ
	h.m.TakeNotices()
	long := "exit status 255: " + strings.Repeat("Host key verification failed. ", 10)
	h.m.note(long, false)
	h.keys("e")
	if h.m.ov != ovLog {
		t.Fatalf("e で一覧が開かない (ov=%v)", h.m.ov)
	}
	if v := strings.Join(h.m.View(), "\n"); !strings.Contains(v, "メッセージ一覧  2 件") {
		t.Errorf("一覧の板が出ない")
	}
	h.keys("y")
	if len(h.copied) != 1 || !strings.HasSuffix(h.copied[0], long) || !strings.Contains(h.copied[0], "✗") {
		t.Fatalf("最新の 1 件を全文でコピーしない: %q", h.copied)
	}
	h.keys("j", "y")
	if len(h.copied) != 2 || strings.Contains(h.copied[1], long) {
		t.Errorf("j で選んだ 1 件をコピーしない: %q", h.copied)
	}
	h.keys("Y")
	if got := h.copied[len(h.copied)-1]; strings.Count(got, "\n") != 3 || !strings.HasPrefix(got[9:], "✗ この Mac") || strings.Count(got, long) != 1 {
		t.Errorf("Y で全部 (古い順) をコピーしない: %q", got)
	}
	h.keys("esc")
	h.m.note("相手の出力\x1b]52;c;ZXZpbA==\x07\nrm -rf ~", false) // 相手が書ける stderr の形
	h.keys("e", "y")
	if got := h.copied[len(h.copied)-1]; strings.ContainsAny(got, "\x1b\x07\n") {
		t.Errorf("制御文字・改行を落とさずにコピーした: %q", got)
	}
	h.keys("esc")
	for i := range logCap + 5 {
		h.m.note(strconv.Itoa(i), true)
	}
	if len(h.m.log) != logCap || h.m.log[len(h.m.log)-1].text != strconv.Itoa(logCap+4) {
		t.Errorf("上限で古いものから捨てない: %d 件", len(h.m.log))
	}

	obs := &setup.PeerNet{KeyErr: fmt.Errorf("%w: exit status 255: ssh: connect to host 192.168.50.7 port 22: Operation timed out", setup.ErrUnknown)}
	h2, _ := setupHarness(t, obs)
	if n := h2.m.TakeNotices(); slices.ContainsFunc(n, func(x Notice) bool { return strings.Contains(x.Text, "Operation timed out") }) {
		t.Errorf("相手を読めない理由を toast にも出した: %+v", n)
	}
	if !slices.ContainsFunc(h2.m.log, func(e logEntry) bool { return strings.Contains(e.text, "Operation timed out") }) {
		t.Error("相手を読めない理由 (全文) を一覧に残さない")
	}
}

// 相手の内部 IP が固定かは台帳に書いた固定の番号で決める (帯を 169.254 の中に置いた人もいる。台帳に無い 169.254 は自動割り当て)。
func TestPeerLanFixedByLedger(t *testing.T) {
	fixed := discover.Peer{Host: "m2macmini.local", Instances: []string{"mini"}, SSH: true, Addrs: map[int]string{27: "169.254.100.12"}}
	auto := discover.Peer{Host: "air.local", Instances: []string{"air"}, SSH: true, Addrs: map[int]string{27: "169.254.33.4"}}
	h := newHarness(t, []ledger.Host{{Host: "m2macmini.local", User: "k2", LAN: "169.254.100.12"}}, []discover.Peer{fixed, auto})
	byHost := map[string]row{}
	for _, r := range h.m.rows() {
		byHost[r.host] = r
	}
	if !h.m.lanFixed(byHost["m2macmini.local"]) {
		t.Error("台帳の固定の番号と同じ 169.254 を固定と見ない")
	}
	if h.m.lanFixed(byHost["air.local"]) {
		t.Error("台帳に無い 169.254 を固定と見た")
	}
}

// I/F で決める設定でも、そのサービスが手動の固定でなければ未固定 (自動の 169.254 を固定と見て、一覧の space で台帳に書けた)。
func TestSelfLanByIFUsesMethod(t *testing.T) {
	h := newHarness(t, nil, nil)
	h.m.pf.LanByBand, h.m.pf.LanIF = false, "en13"
	if sl := h.m.selfLan(); sl.IP != "169.254.13.42" || !sl.Unfixed {
		t.Errorf("DHCP の有線を固定と見た: %+v", sl)
	}
	h.m.self.Services[0].Method = "manual"
	h.m.pf.Band = "169.254.0.0/16" // 帯の中の手動 (帯の外の手動は未固定。TestSelfFixedAgreesWithPlan)
	if sl := h.m.selfLan(); sl.Unfixed {
		t.Errorf("手動の固定を未固定と見た: %+v", sl)
	}
}

// 方式が読めない (-getinfo が読めない等) 有線の帯の番号は前と同じく固定と見る (未固定と見て、前は書けた台帳の内部 IP が書けなくなった)。
// 固定の対象は、自動の番号が帯を引いた別の I/F ではなく、手動のサービスか先頭の有線。
func TestSelfLanUnknownMethodAndWiredPick(t *testing.T) {
	h := newHarness(t, nil, nil)
	s := selfNet()
	s.Services[0].IP, s.Services[0].Method = "192.168.100.11", ""
	s.IPs[1].Addr = "192.168.100.11"
	h.m.self = s
	if sl := h.m.selfLan(); sl.Unfixed {
		t.Errorf("方式が読めない帯の番号を未固定と見た: %+v", sl)
	}
	s = selfNet()
	s.Services = append(s.Services, netinfo.Service{Name: "Thunderbolt Bridge", Port: "Thunderbolt Bridge", Device: "bridge0", Active: true, IP: "192.168.100.77", Method: "DHCP", Read: true})
	h.m.self = s
	if w, ok := h.m.selfWired(); !ok || w.Device != "en13" {
		t.Errorf("自動の番号が帯を引いた別の I/F を固定の対象にした: %+v", w)
	}
}

// 一覧の内部 IP は、手動で固定した帯の番号を、自動の番号が帯を引いた別の I/F より先に拾う (並びで別の I/F を拾い「未固定」と出した)。
func TestSelfLanPrefersManualInBand(t *testing.T) {
	h := newHarness(t, nil, nil)
	s := selfNet()
	s.Services[0].IP, s.Services[0].Method = "192.168.100.10", "manual"
	s.Services = append(s.Services, netinfo.Service{Name: "Thunderbolt Bridge", Port: "Thunderbolt Bridge", Device: "bridge0", Active: true, IP: "192.168.100.5", Method: "DHCP", Read: true})
	s.IPs = append([]netinfo.IP{{Device: "bridge0", Addr: "192.168.100.5", Kind: netinfo.KindWired}}, s.IPs...)
	s.IPs[2].Addr = "192.168.100.10" // en13
	h.m.self = s
	if sl := h.m.selfLan(); sl.IP != "192.168.100.10" || sl.Unfixed {
		t.Errorf("手動で固定した en13 ではなく自動の番号を拾った: %+v", sl)
	}
}

// 有線で届くかは、この Mac と相手の内部 IP が同じ網か (両方帯の中か、両方 169.254)。帯は 169.254 の中にも置けるので帯を先に見る。
func TestLanReachable(t *testing.T) {
	for _, c := range []struct {
		self, peer, band string
		want             bool
	}{
		{"169.254.13.42", "169.254.217.100", "192.168.100.0/24", true},
		{"192.168.100.10", "169.254.113.65", "192.168.100.0/24", false},
		{"169.254.13.42", "192.168.100.12", "192.168.100.0/24", false},
		{"192.168.100.10", "192.168.100.12", "192.168.100.0/24", true},
		{"169.254.100.10", "169.254.217.100", "169.254.100.0/24", false}, // 帯の固定と自動の 169.254
		{"169.254.100.10", "169.254.100.12", "169.254.100.0/24", true},
		{"", "169.254.1.1", "192.168.100.0/24", true},
	} {
		if got := lanReachable(c.self, c.peer, c.band); got != c.want {
			t.Errorf("lanReachable(%s, %s, %s) = %v, want %v", c.self, c.peer, c.band, got, c.want)
		}
	}
}

// 有線でしか見えない相手と網が分かれていたら、その他の I/F (VPN 等) へ黙って繋がず、網が分かれている理由で断る。
func TestAwayPeerWithoutWifiRefuses(t *testing.T) {
	wired := discover.Peer{Host: "wired.local", Instances: []string{"wired"}, SSH: true, VNC: true, Addrs: map[int]string{27: "169.254.9.9", 40: "100.64.0.5"}}
	h := newHarness(t, nil, []discover.Peer{wired})
	setSelfStatic(h.m)
	h.m.Advance(t0)
	h.m.tab = 1
	h.cursorTo(t, "wired.local")
	h.m.TakeNotices()
	if h.keys("s") == Exec {
		t.Fatal("有線で網が分かれた相手へ ssh を起こした")
	}
	if n := h.m.TakeNotices(); len(n) == 0 || !strings.Contains(n[0].Text, "網が分かれている") {
		t.Errorf("断りの理由が違う: %+v", n)
	}
	if h.keys("o") == Exec {
		t.Error("有線で網が分かれた相手の画面共有を開いた")
	}
}

// 走査で行が増えても、カーソルは同じ Mac に残る (添字のままだと、名前で前に並ぶ行が増えた分だけ別の Mac に移り、s がそのまま
// 別の相手へ ssh した)。
func TestCursorFollowsMacAcrossScanUpdate(t *testing.T) {
	zed := discover.Peer{Host: "zed.local", Instances: []string{"zed"}, SSH: true, Addrs: map[int]string{14: "192.168.50.20"}}
	h := newHarness(t, nil, []discover.Peer{zed})
	h.keys("l")
	h.cursorTo(t, "zed.local")
	abc := discover.Peer{Host: "abc.local", Instances: []string{"abc"}, SSH: true, Addrs: map[int]string{14: "192.168.50.21"}}
	h.scan.peers = []discover.Peer{zed, abc}
	h.m.Advance(t0.Add(time.Second))
	if got := h.m.current().host; got != "zed.local" {
		t.Errorf("走査で行が増えた後のカーソル = %s (zed.local のはず)", got)
	}
}

func cursorPeer(host, ip string) discover.Peer {
	return discover.Peer{Host: host, Instances: []string{strings.TrimSuffix(host, ".local")}, SSH: true, Addrs: map[int]string{14: ip}}
}

// r で戻したい Mac が見つかった tick で、直前の Mac へ合わせ直してそれを打ち消さない。
func TestCursorReturnsToMacAfterReload(t *testing.T) {
	a, mm, z := cursorPeer("aaa.local", "192.168.50.21"), cursorPeer("mmm.local", "192.168.50.22"), cursorPeer("zed.local", "192.168.50.20")
	h := newHarness(t, nil, []discover.Peer{a, mm, z})
	h.keys("l")
	h.cursorTo(t, "zed.local")
	h.scan.peers, h.scan.busy = []discover.Peer{a}, true // 走査を起こし直すと最初は一部だけ
	h.keys("r")
	awaitNetLoad(h.m)
	h.m.Advance(t0)
	h.scan.peers = []discover.Peer{a, mm, z}
	h.m.Advance(t0.Add(time.Second))
	if got := h.m.current().host; got != "zed.local" {
		t.Errorf("r の後、zed が見つかったのにカーソル = %s", got)
	}
}

// 戻したい Mac を待っている間も、キーを押していないのにカーソルが別の Mac へ移らない。
func TestCursorHoldsWhileWaitingForMac(t *testing.T) {
	a, mm, z := cursorPeer("aaa.local", "192.168.50.21"), cursorPeer("mmm.local", "192.168.50.22"), cursorPeer("zed.local", "192.168.50.20")
	h := newHarness(t, nil, []discover.Peer{a, mm, z})
	h.keys("l")
	h.cursorTo(t, "zed.local")
	h.scan.peers, h.scan.busy = []discover.Peer{mm}, true
	h.keys("r")
	awaitNetLoad(h.m)
	h.m.Advance(t0)
	shown := h.m.current().host
	h.scan.peers = []discover.Peer{a, mm}
	h.m.Advance(t0.Add(time.Second))
	if got := h.m.current().host; got != shown {
		t.Errorf("キーを押していないのにカーソルが %s → %s へ移った", shown, got)
	}
}

// キーの入口で取り込んだ網の読み直しで行が組み変わっても、そのキーは画面で見ていた Mac に効く。
func TestKeyAfterNetLoadActsOnShownMac(t *testing.T) {
	a := discover.Peer{Host: "aaa.local", Instances: []string{"aaa"}, SSH: true, Addrs: map[int]string{30: "169.254.5.5"}}
	z := discover.Peer{Host: "zed.local", Instances: []string{"zed"}, SSH: true, Addrs: map[int]string{27: "169.254.6.6"}}
	h := newHarness(t, nil, []discover.Peer{a, z})
	h.m.o.IfName = func(i int) string { return map[int]string{14: "en0", 27: "en13", 30: "en18"}[i] }
	h.keys("l")
	h.m.pf.WiredOnly = true
	h.cursorTo(t, "zed.local")
	if n := len(h.m.visible()); n != 1 {
		t.Fatalf("前提: aaa は有線に見えない (visible=%d)", n)
	}
	nn := selfNet()
	nn.Services = append(nn.Services, netinfo.Service{Name: "USB LAN", Device: "en18", Active: true, IP: "169.254.85.235", Method: "DHCP", Read: true})
	h.m.o.ReadNet = func(context.Context) (netinfo.Snapshot, error) { return nn, nil }
	h.keys("r")
	h.m.pf.WiredOnly = true // Reload が設定を読み直すので戻す
	h.m.wantHost = ""       // r で積んだ戻したい Mac を外し、キーの入口の取り込みだけを見る
	// 読み終えた結果を、tick より先にキーの入口が取り込む形にする (受け取って、取り込み前のチャネルへ戻す)
	r := <-h.m.netCh
	c := make(chan netLoad, 1)
	c <- r
	h.m.netCh = c
	shown := h.m.current().host
	if a := h.keys("s"); a == Exec {
		x, _ := h.m.TakeExec()
		if !strings.Contains(strings.Join(x.Argv, " "), "HostKeyAlias="+shown) {
			t.Errorf("画面のカーソルは %s なのに s が %q を起こした", shown, x.Argv)
		}
	} else {
		t.Fatalf("前提: s が実行にならない: %v", a)
	}
}

// lan_if 未設定のとき、I/F の無いサービス (VPN) に一致させず「(未設定)」と出す。
func TestSelfLanUnsetIFIgnoresEmptyDevice(t *testing.T) {
	h := newHarness(t, nil, nil)
	h.m.pf.LanByBand, h.m.pf.LanIF = false, ""
	h.m.self.Services = append(h.m.self.Services, netinfo.Service{Name: "VPN (L2TP)", Device: "", Read: true, Method: "DHCP"})
	if n := h.m.selfLan().Note; !strings.Contains(n, "(未設定)") {
		t.Errorf("selfLan note = %q (I/F 未設定と出すはず)", n)
	}
}

// 走査が終わってこの Mac の網だけを読み直している間は、見出しにそれを出す (確認待ちと出さない)。
func TestHeaderWhileOnlyNetReload(t *testing.T) {
	h := newHarness(t, nil, nil)
	block := make(chan struct{})
	defer close(block)
	h.m.o.ReadNet = func(ctx context.Context) (netinfo.Snapshot, error) { <-block; return selfNet(), nil }
	h.scan.busy = false
	h.keys("r")
	h.m.Advance(t0)
	if hd := h.m.header(200); !strings.Contains(hd, "この Mac のネットワーク設定を確認中") {
		t.Errorf("網の読み直しの間の見出し = %q", hd)
	}
}

// Busy に入る裏の仕事ごとに、見出しはその仕事の名前を出す (case が無い仕事は、最後の「有線の名前を確かめている」に落ちる)。
func TestHeaderNamesSpeedAndReach(t *testing.T) {
	for _, c := range []struct {
		name string
		set  func(m *Model)
		want string
	}{
		{"速度測定", func(m *Model) { m.speedRun = &speedRun{} }, "通信速度を測っている"},
		{"疎通の確認", func(m *Model) { m.reachRuns = map[string]*reachRun{"m2macmini.local": {}} }, "疎通を確かめている"},
	} {
		h := newHarness(t, nil, nil)
		h.scan.busy = false
		h.m.Advance(t0)
		c.set(h.m)
		if hd := h.m.header(200); !strings.Contains(hd, c.want) {
			t.Errorf("%s の間の見出し = %q", c.name, hd)
		}
	}
}

// 自動で出た板は、実行の無い板 (台帳) でも案内に Enter を出さない (Enter では実行しない)。
func TestAutoBoardHintOmitsEnter(t *testing.T) {
	h := newHarness(t, nil, nil)
	for _, a := range []*ask{
		{title: "台帳に内部 IP 付きで載せる", body: []string{"x"}, yes: func() Notice { return Notice{"ok", true} }, auto: true},
		{title: "実行", body: []string{"x"}, exec: &ExecRequest{Argv: []string{"true"}}, auto: true},
	} {
		h.m.ask, h.m.ov = a, ovConfirm
		if box, _, _ := h.m.overlayBox(); strings.Contains(box, "Enter") {
			t.Errorf("自動で出た板 %q の案内に Enter がある", a.title)
		}
	}
}

// キーの入口の取り込みで、画面で見ていた Mac が一覧から消えたら (有線だけの表示で、この Mac の USB LAN が外れた等)、行に効くキーは
// 断る (添字のままだと隣の Mac へ繋いだ)。
func TestKeyRefusedWhenShownMacVanishes(t *testing.T) {
	a := discover.Peer{Host: "aaa.local", Instances: []string{"aaa"}, SSH: true, Addrs: map[int]string{30: "169.254.5.5"}}
	z := discover.Peer{Host: "zed.local", Instances: []string{"zed"}, SSH: true, Addrs: map[int]string{27: "169.254.6.6"}}
	h := newHarness(t, nil, []discover.Peer{a, z})
	h.m.o.IfName = func(i int) string { return map[int]string{14: "en0", 27: "en13", 30: "en18"}[i] }
	nn := selfNet()
	nn.Services = append(nn.Services, netinfo.Service{Name: "USB LAN", Device: "en18", Active: true, IP: "169.254.85.235", Method: "DHCP", Read: true})
	h.m.o.ReadNet = func(context.Context) (netinfo.Snapshot, error) { return nn, nil }
	h.keys("r")
	awaitNetLoad(h.m)
	h.keys("l")
	h.m.pf.WiredOnly = true
	h.cursorTo(t, "aaa.local")
	h.m.o.ReadNet = func(context.Context) (netinfo.Snapshot, error) { return selfNet(), nil } // USB LAN が外れた
	h.keys("r")
	h.m.pf.WiredOnly = true
	h.m.wantHost = ""
	r := <-h.m.netCh
	c := make(chan netLoad, 1)
	c <- r
	h.m.netCh = c
	if a := h.keys("s"); a == Exec {
		x, _ := h.m.TakeExec()
		t.Fatalf("見ていた aaa が消えたのに s が %q を起こした", x.Argv)
	}
	if n := h.m.TakeNotices(); len(n) == 0 || !strings.Contains(n[len(n)-1].Text, "一覧が変わった") {
		t.Errorf("断った理由を出していない: %+v", n)
	}
	if a := h.keys("s"); a != Exec { // 次のキーは今のカーソルの Mac に効く (断るのは消えたその 1 回だけ)
		t.Errorf("次の s も断った: %v", a)
	}
}

// 台帳から外すと、相手の有線の名前の広告も ssh の鍵で入って止める。届かなければ止め方を知らせる (台帳から外した相手の名前が、
// 相手の上で広告されたまま残らないように)。
func TestLedgerRemoveStopsWiredName(t *testing.T) {
	for _, c := range []struct {
		err  error
		want string // 知らせに含む語
		ok   bool
	}{
		{nil, "の広告を止めた", true},
		{errors.New("exit status 255"), "launchctl bootout", false},
		// 相手は別の名前を広告している: 止めていないと知らせ、手で止める案内は出さない (案内どおりに止めると今の広告を消した)
		{otherNameError{}, "止めていない", false},
	} {
		h := newHarness(t, []ledger.Host{{Host: "m2macmini.local", User: "koji"}}, []discover.Peer{mini})
		var got [][]string
		h.m.o.RunQuiet = func(_ context.Context, argv []string) error {
			got = append(got, argv)
			return c.err
		}
		h.keys("space")
		if v := strings.Join(h.m.View(), "\n"); !strings.Contains(v, "m2macmini.wired.local") {
			t.Fatalf("板に広告を止めることを出さない:\n%s", v)
		}
		h.keys("y")
		var notes []Notice
		for deadline := time.Now().Add(5 * time.Second); len(h.m.bg) > 0 && time.Now().Before(deadline); {
			h.m.Advance(t0)
			runtime.Gosched()
		}
		notes = h.m.TakeNotices()
		if len(h.store.hosts) != 0 {
			t.Fatalf("台帳から外していない: %+v", h.store.hosts)
		}
		if len(got) != 1 || !slices.Contains(got[0], "koji@192.168.50.7") || !strings.Contains(got[0][len(got[0])-1], "bootout") {
			t.Fatalf("広告を止める ssh = %q", got)
		}
		if !slices.ContainsFunc(notes, func(n Notice) bool { return strings.Contains(n.Text, c.want) && n.OK == c.ok }) {
			t.Errorf("%v: 結果を知らせない: %+v", c.err, notes)
		}
		if errors.As(c.err, new(otherNameError)) && slices.ContainsFunc(notes, func(n Notice) bool { return strings.Contains(n.Text, "launchctl bootout") }) {
			t.Errorf("別の名前を広告している相手に、手で止める案内を出した: %+v", notes)
		}
	}
}

// WaitBG は裏で広告を止める ssh の結果を待って返す (単体のバイナリは閉じた後にこれで結果を出す)。stop が閉じたら待つのをやめ、
// 残りの件数を返す。返した結果は takeBG がもう一度知らせない
func TestWaitBGReturnsResultsOrLeftover(t *testing.T) {
	h := newHarness(t, []ledger.Host{{Host: "m2macmini.local", User: "koji"}}, []discover.Peer{mini})
	release := make(chan struct{})
	h.m.o.RunQuiet = func(context.Context, []string) error { <-release; return nil }
	h.keys("space", "y")
	if h.m.PendingBG() != 1 {
		t.Fatalf("前提: 広告を止める ssh が裏で走っていない (PendingBG=%d)", h.m.PendingBG())
	}
	stop := make(chan struct{})
	close(stop)
	if got, left := h.m.WaitBG(stop); len(got) != 0 || left != 1 || h.m.PendingBG() != 1 {
		t.Fatalf("止めた待ち = %+v left=%d pending=%d (結果を待たずに 1 件残すはず)", got, left, h.m.PendingBG())
	}
	close(release)
	got, left := h.m.WaitBG(nil)
	if left != 0 || len(got) != 1 || !got[0].OK || !strings.Contains(got[0].Text, "の広告を止めた") || h.m.PendingBG() != 0 {
		t.Fatalf("待った結果 = %+v left=%d pending=%d", got, left, h.m.PendingBG())
	}
	h.m.Advance(t0)
	if n := h.m.TakeNotices(); slices.ContainsFunc(n, func(x Notice) bool { return strings.Contains(x.Text, "の広告を止めた") }) {
		t.Errorf("WaitBG で返した結果を takeBG がもう一度知らせた: %+v", n)
	}
}

// stop が閉じていても、届いている結果は取る (select がどちらを取るかに任せない)。届いていない分だけを残して数える
func TestWaitBGTakesArrivedResultsAfterStop(t *testing.T) {
	m := New(Options{})
	waiting, arrived := make(chan Notice), make(chan Notice, 1)
	arrived <- Notice{Text: "b.wired.local の広告を止めた", OK: true}
	m.bg = []<-chan Notice{waiting, arrived}
	stop := make(chan struct{})
	close(stop)
	for range 20 { // 同時に選べるときに取り落とす形は確率でしか出ないので、何度か回す
		m.bg = []<-chan Notice{waiting, arrived}
		got, left := m.WaitBG(stop)
		if len(got) != 1 || left != 1 || m.PendingBG() != 1 {
			t.Fatalf("got=%+v left=%d pending=%d (届いた 1 件を取り、届いていない 1 件を残すはず)", got, left, m.PendingBG())
		}
		arrived <- Notice{Text: "b.wired.local の広告を止めた", OK: true}
	}
}

type otherNameError struct{}

func (otherNameError) Error() string {
	return "exit status 4: mac-lan-manager: 別の名前 (air6.wired.local) を広告している (止めない)"
}
func (otherNameError) ExitCode() int { return setup.ExitOtherName }

// awaitWired は裏で引いている有線の名前を全部取り込む。
func awaitWired(t *testing.T, m *Model) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); m.wiredWait > 0; {
		if time.Now().After(deadline) {
			t.Fatal("有線の名前を引き終えない")
		}
		m.takeWired()
		runtime.Gosched()
	}
}

// wiredHarness は内部 IP を台帳で固定した mini (169.254.217.100) と、名前を引く偽物 (answers の名前 → IP)。
func wiredHarness(t *testing.T, answers map[string][]string) *harness {
	t.Helper()
	h := newHarness(t, []ledger.Host{{Host: "m2macmini.local", User: "koji", LAN: "169.254.217.100"}}, []discover.Peer{mini})
	h.m.o.LookupHost = func(_ context.Context, name string) ([]string, error) { return answers[name], nil }
	h.m.startWiredLookups()
	awaitWired(t, h.m)
	return h
}

// 有線の名前が内部 IP だけに引けるなら、繋ぐ・コピー・画面共有の宛先を名前にする (ホスト鍵の照合は台帳の鍵のまま)。
func TestWiredNameUsedAsTarget(t *testing.T) {
	h := wiredHarness(t, map[string][]string{"m2macmini.wired.local": {"169.254.217.100"}})
	h.cursorTo(t, "m2macmini.local")
	if v := strings.Join(h.m.View(), "\n"); !strings.Contains(v, "m2macmini.wired.local") || !strings.Contains(v, "✓ 引ける") {
		t.Errorf("詳細に有線の名前と状態が出ない:\n%s", v)
	}
	h.keys("Y", "y", "O")
	if want := []string{"ssh koji@m2macmini.wired.local", "smb://m2macmini.wired.local", "open smb://m2macmini.wired.local"}; !slices.Equal(h.copied, want) {
		t.Errorf("copied = %v, want %v", h.copied, want)
	}
	if a := h.keys("s"); a != Exec {
		t.Fatalf("s = %v", a)
	}
	r, _ := h.m.TakeExec()
	if !slices.Contains(r.Argv, "koji@m2macmini.wired.local") || !slices.Contains(r.Argv, "HostKeyAlias=m2macmini.local") {
		t.Errorf("s = %q (名前で繋ぎ、ホスト鍵は台帳の鍵で照合する)", r.Argv)
	}
	if a := h.keys("o"); a != Exec {
		t.Fatalf("o = %v", a)
	}
	if r, _ := h.m.TakeExec(); !slices.Contains(r.Argv, "vnc://m2macmini.wired.local") {
		t.Errorf("o = %q", r.Argv)
	}
	// a は同じ宛先とユーザー名を SyakeScreen の URL で渡す
	if a := h.keys("a"); a != Exec {
		t.Fatalf("a = %v", a)
	}
	if r, _ := h.m.TakeExec(); !slices.Equal(r.Argv, []string{"open", "syakescreen://connect?host=m2macmini.wired.local&user=koji"}) || !r.ExitIsFailure {
		t.Errorf("a = %q (入っていないとき open の失敗を知らせに出す: %v)", r.Argv, r.ExitIsFailure)
	}
}

// SyakeScreen の URL は値を query の符号にして渡す (アプリは user の @ や空白を断る。こちらで崩した形を作らない)
func TestSyakeScreenURLEscapesValues(t *testing.T) {
	if got := syakeScreenURL("192.168.50.7", "ko ji&x=1"); got != "syakescreen://connect?host=192.168.50.7&user=ko+ji%26x%3D1" {
		t.Errorf("syakeScreenURL = %q", got)
	}
}

// a はユーザー名が分からない相手を断る (SyakeScreen は user の無い URL を開かない)。案内にも出さない
func TestSyakeScreenNeedsAUser(t *testing.T) {
	// ユーザー名が分かる相手には、最下行の候補と詳細のカードに a を案内する (下の否定の確かめが、案内の形が変わって空振りしていないことの対照)。
	// 最下行は幅で優先度の低い項目から落とすので、描いた行ではなく候補 (rowHints) で見る
	hasA := func(h *harness) bool {
		return slices.ContainsFunc(h.m.rowHints(h.m.current()), func(k keyHint) bool { return k.key == "a" })
	}
	card := regexp.MustCompile(`o\s+開く\s+a\s+SyakeScreen`)
	h := newHarness(t, nil, []discover.Peer{mini})
	h.keys("l")
	if !hasA(h) || !card.MatchString(stripView(strings.Join(h.m.View(), "\n"))) {
		t.Errorf("ユーザー名が分かるのに a を案内しない (候補=%v)", hasA(h))
	}

	h = newHarness(t, nil, []discover.Peer{mini})
	h.m.o.User = ""
	h.keys("l")
	if v := stripView(strings.Join(h.m.View(), "\n")); hasA(h) || regexp.MustCompile(`a\s+SyakeScreen`).MatchString(v) {
		t.Errorf("ユーザー名が分からないのに a を案内した:\n%s", v)
	}
	h.m.TakeNotices()
	if a := h.keys("a"); a == Exec {
		t.Fatalf("ユーザー名が分からないのに a で開いた")
	}
	if n := h.m.TakeNotices(); len(n) == 0 || n[len(n)-1].Text != noUserNote {
		t.Errorf("断った理由が出ない: %+v", n)
	}
}

// 名前が引けない・別の IP に向いているなら、宛先は IP のまま (渡しても繋がらない・別の Mac に繋がる名前を使わない) で、理由を詳細に出す。
func TestWiredNameNotUsedUnlessItMatches(t *testing.T) {
	for want, answers := range map[string]map[string][]string{
		"まだ広告されていない":   nil,
		"違う IP に向いている": {"m2macmini.wired.local": {"169.254.99.99"}},
		"違う IP に向いてい":  {"m2macmini.wired.local": {"169.254.217.100", "192.168.50.7"}}, // Wi-Fi の IP も混ざる
	} {
		h := wiredHarness(t, answers)
		h.cursorTo(t, "m2macmini.local")
		h.keys("Y")
		if !slices.Equal(h.copied, []string{"ssh koji@169.254.217.100"}) {
			t.Errorf("%s: copied = %v (IP のまま)", want, h.copied)
		}
		if v := strings.Join(h.m.View(), "\n"); !strings.Contains(v, want) {
			t.Errorf("%s: 詳細に理由が出ない:\n%s", want, v)
		}
	}
}

// c の板に、この Mac の有線の名前が引けるときだけコピーできる行を足す。
func TestCopySelfWiredName(t *testing.T) {
	h := newHarness(t, nil, nil)
	nn := selfNet()
	nn.Services[0].Method, nn.Services[0].IP = "manual", "192.168.100.10"
	nn.IPs[1] = netinfo.IP{Device: "en13", Addr: "192.168.100.10", Kind: netinfo.KindWired} // 固定は I/F の IP でも見る (selfLan)
	h.m.o.ReadNet = func(context.Context) (netinfo.Snapshot, error) { return nn, nil }
	awaitNetLoad(h.m)
	h.m.startNetLoad()
	awaitNetLoad(h.m)
	name, err := setup.WiredName(h.m.host)
	if err != nil {
		t.Fatalf("前提: この Mac の名前を作れない (%q): %v", h.m.host, err)
	}
	h.m.o.LookupHost = func(_ context.Context, n string) ([]string, error) {
		if n == name {
			return []string{"192.168.100.10"}, nil
		}
		return nil, nil
	}
	h.m.startWiredLookups()
	awaitWired(t, h.m)
	h.keys("c")
	if v := strings.Join(h.m.View(), "\n"); !strings.Contains(v, name) {
		t.Fatalf("c の板に有線の名前が無い:\n%s", v)
	}
	h.keys("3")
	if len(h.copied) != 1 || h.copied[0] != name {
		t.Errorf("3 で有線の名前をコピーしない: %v", h.copied)
	}

	// 引けない名前は板に理由つきで出すが、コピーさせない (渡しても繋がらない)
	h.copied = nil
	h.m.o.LookupHost = func(context.Context, string) ([]string, error) { return nil, nil }
	h.m.startWiredLookups()
	awaitWired(t, h.m)
	h.m.TakeNotices()
	h.keys("c", "3")
	if len(h.copied) != 0 {
		t.Errorf("引けない名前をコピーした: %v", h.copied)
	}
	if n := h.m.TakeNotices(); len(n) == 0 || !strings.Contains(n[len(n)-1].Text, "広告されていない") {
		t.Errorf("コピーしない理由を出さない: %+v", n)
	}

	// 板を開いている間に行が減って選択が範囲外に残っても、enter で黙って何もしないことにしない (残った最後の行を選ぶ)
	h.keys("c")
	h.m.copyC = 9
	h.keys("enter")
	if n := h.m.TakeNotices(); len(h.copied) == 0 && len(n) == 0 {
		t.Error("範囲外の選択で enter が何もしない")
	}
}

// Wi-Fi で繋ぐ相手 (この Mac が 169.254 のままで、相手だけ帯の固定 = 網が分かれる) には、名前が引けても有線の名前を渡さない
// (有線で届かないので、名前で繋ぐと失敗する)。
func TestWiredNameNotUsedOverWifi(t *testing.T) {
	h := newHarness(t, []ledger.Host{{Host: "m2macmini.local", User: "koji", LAN: "192.168.100.12"}}, []discover.Peer{mini})
	h.m.o.LookupHost = func(context.Context, string) ([]string, error) { return []string{"192.168.100.12"}, nil }
	h.m.startWiredLookups()
	awaitWired(t, h.m)
	h.cursorTo(t, "m2macmini.local")
	if r := h.m.current(); r.connectIP() != "192.168.50.7" {
		t.Fatalf("前提: Wi-Fi で繋ぐ相手でない (connectIP=%s)", r.connectIP())
	}
	h.keys("Y")
	if !slices.Equal(h.copied, []string{"ssh koji@192.168.50.7"}) {
		t.Errorf("copied = %v (Wi-Fi の IP のはず)", h.copied)
	}
	if v := strings.Join(h.m.View(), "\n"); !strings.Contains(v, "網が分かれている") || strings.Contains(v, "✓ 引ける") {
		t.Errorf("詳細が宛先 (Wi-Fi の IP) と食い違う:\n%s", v)
	}
}

// advanceUntil は cond が真になるまで tick を進める (本番と同じ Advance の経路で取り込ませる)。
func advanceUntil(t *testing.T, m *Model, at time.Time, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !cond(); {
		if time.Now().After(deadline) {
			t.Fatal("tick を進めても成り立たない")
		}
		m.Advance(at)
		runtime.Gosched()
	}
}

// r → この Mac の網の読み込み → 名前を引く → tick で取り込む、の配線を通して、宛先が名前になる。引いている間は Busy (tick を回す)。
func TestWiredNameWiredThroughReload(t *testing.T) {
	h := newHarness(t, []ledger.Host{{Host: "m2macmini.local", User: "koji", LAN: "169.254.217.100"}}, []discover.Peer{mini})
	gate := make(chan struct{})
	h.m.o.LookupHost = func(_ context.Context, name string) ([]string, error) {
		<-gate
		if name == "m2macmini.wired.local" {
			return []string{"169.254.217.100"}, nil
		}
		return nil, nil
	}
	h.cursorTo(t, "m2macmini.local")
	h.keys("r")
	awaitNetLoad(h.m)
	if h.m.wiredWait == 0 || !h.m.Busy() {
		t.Fatalf("網を読み終えたのに名前を引き始めない / Busy でない (wait=%d)", h.m.wiredWait)
	}
	close(gate)
	advanceUntil(t, h.m, t0, func() bool { return h.m.wiredWait == 0 })
	if got := h.m.target(h.m.current()); got != "m2macmini.wired.local" {
		t.Errorf("宛先 = %s (名前のはず)", got)
	}
}

// 引いた結果は wiredRefreshAge を過ぎたら引き直し、引き直しの間は前の結果を使う (宛先がちらつかない)。広告が止まっていれば、引き直した
// 結果で IP に戻る。キーの入口でも引き直しを始める (放置した後は tick が回っていない)。wiredMaxAge を過ぎた結果は使わない。
func TestWiredNameRefreshAndExpiry(t *testing.T) {
	var mu sync.Mutex
	answer, gate := []string{"169.254.217.100"}, chan struct{}(nil)
	now := t0
	h := newHarness(t, []ledger.Host{{Host: "m2macmini.local", User: "koji", LAN: "169.254.217.100"}}, []discover.Peer{mini})
	h.m.o.Now = func() time.Time { return now }
	h.m.o.LookupHost = func(context.Context, string) ([]string, error) {
		mu.Lock()
		a, g := answer, gate
		mu.Unlock()
		if g != nil {
			<-g
		}
		return a, nil
	}
	h.m.startWiredLookups()
	advanceUntil(t, h.m, now, func() bool { return h.m.wiredWait == 0 })
	h.cursorTo(t, "m2macmini.local")
	if got := h.m.target(h.m.current()); got != "m2macmini.wired.local" {
		t.Fatalf("前提: 宛先 = %s", got)
	}
	// 広告が止まり、引き直す時期が来た。引き直しの答えは止めておく
	g := make(chan struct{})
	mu.Lock()
	answer, gate = nil, g
	mu.Unlock()
	now = now.Add(wiredRefreshAge + time.Second)
	h.keys("j") // 放置の後の最初のキー: tick が回っていなくても引き直しを始める
	if h.m.wiredWait == 0 {
		t.Fatal("引き直す時期を過ぎたのに、キーで引き直しを始めない")
	}
	if got := h.m.target(h.m.current()); got != "m2macmini.wired.local" {
		t.Errorf("引き直しの間に IP へ戻った (ちらつく): %s", got)
	}
	close(g)
	advanceUntil(t, h.m, now, func() bool { return h.m.wiredWait == 0 })
	if _, ok, note := h.m.wiredState(h.m.current()); ok || !strings.Contains(note, "広告されていない") {
		t.Errorf("引き直した後の状態 = ok=%v %q", ok, note)
	}
	// 引けた結果も、wiredMaxAge を過ぎたら (引き直しが届かないまま) 使わない
	mu.Lock()
	answer, gate = []string{"169.254.217.100"}, nil
	mu.Unlock()
	h.m.startWiredLookups()
	advanceUntil(t, h.m, now, func() bool { return h.m.wiredWait == 0 })
	now = now.Add(wiredMaxAge + time.Second)
	if got := h.m.target(h.m.current()); got != "169.254.217.100" {
		t.Errorf("使える長さを過ぎた結果で名前を使った: %s", got)
	}
}

// 台帳から外した Mac の結果は消す (戻したとき、外す前の「引ける」を引き直さずに使った)。名前を確かめている間の見出しはそれを出す。
func TestWiredEntryDroppedOnLedgerRemove(t *testing.T) {
	h := wiredHarness(t, map[string][]string{"m2macmini.wired.local": {"169.254.217.100"}})
	h.cursorTo(t, "m2macmini.local")
	h.keys("space", "y")
	if _, ok := h.m.wired["m2macmini.local"]; ok {
		t.Error("台帳から外した Mac の名前の結果が残った")
	}

	h2 := wiredHarness(t, nil)
	gate := make(chan struct{})
	defer close(gate)
	h2.m.o.LookupHost = func(context.Context, string) ([]string, error) { <-gate; return nil, nil }
	h2.scan.busy = false
	h2.m.startWiredLookups()
	if h2.m.wiredWait == 0 {
		t.Fatal("前提: 名前を引いていない")
	}
	if hd := h2.m.header(200); !strings.Contains(hd, "有線の名前を確かめている") {
		t.Errorf("名前を確かめている間の見出し = %q", hd)
	}
}

// 引き直しが重なったら、前の回の結果は遅れて届いても使わない (新しい回の数に数えない)。
func TestWiredLookupRoundsDoNotMix(t *testing.T) {
	first, second := make(chan struct{}), make(chan struct{})
	round := 0
	h := newHarness(t, []ledger.Host{{Host: "m2macmini.local", User: "koji", LAN: "169.254.217.100"}}, []discover.Peer{mini})
	var mu sync.Mutex
	h.m.o.LookupHost = func(context.Context, string) ([]string, error) {
		mu.Lock()
		round++
		r := round
		mu.Unlock()
		if r == 1 {
			<-first
			return []string{"169.254.217.100"}, nil // 古い回: 引けた
		}
		<-second
		return nil, nil // 新しい回: 引けない
	}
	h.m.startWiredLookups()
	for { // 1 回目の問い合わせが始まるのを待ってから 2 回目を起こす
		mu.Lock()
		r := round
		mu.Unlock()
		if r == 1 {
			break
		}
		runtime.Gosched()
	}
	h.m.startWiredLookups()
	close(first) // 古い回が先に届く
	close(second)
	advanceUntil(t, h.m, t0, func() bool { return h.m.wiredWait == 0 })
	h.cursorTo(t, "m2macmini.local")
	if got := h.m.target(h.m.current()); got != "169.254.217.100" {
		t.Errorf("古い回の結果を使った: %s", got)
	}
}

// この Mac の網を読み直したら (固定し直して IP が変わった)、結果の期限を待たずに名前を引き直す (期限まで「違う IP」と出し続けない)。
func TestWiredNameRelookupAfterNetReload(t *testing.T) {
	h := newHarness(t, nil, nil)
	ip := "192.168.100.10"
	net := func() netinfo.Snapshot {
		nn := selfNet()
		nn.Services[0].Method, nn.Services[0].IP = "manual", ip
		nn.IPs[1] = netinfo.IP{Device: "en13", Addr: ip, Kind: netinfo.KindWired}
		return nn
	}
	h.m.o.ReadNet = func(context.Context) (netinfo.Snapshot, error) { return net(), nil }
	h.m.o.LookupHost = func(context.Context, string) ([]string, error) { return []string{ip}, nil }
	awaitNetLoad(h.m)
	h.m.startNetLoad()
	awaitNetLoad(h.m)
	advanceUntil(t, h.m, t0, func() bool { return h.m.wiredWait == 0 })
	if _, ok, note := h.m.wiredState(h.m.rows()[0]); !ok {
		t.Fatalf("前提: この Mac の名前が引けない: %q", note)
	}
	ip = "192.168.100.20" // 固定し直した
	h.m.startNetLoad()
	awaitNetLoad(h.m)
	advanceUntil(t, h.m, t0, func() bool { return h.m.wiredWait == 0 })
	if _, ok, note := h.m.wiredState(h.m.rows()[0]); !ok {
		t.Errorf("網を読み直したのに名前を引き直さない: %q", note)
	}
}

// この Mac の「固定」は、画面 (selfLan / lanFixed) と初期設定 (setup.Plan の self) で同じ結論になる。初期設定に渡すのと同じ観測
// (selfWired から組む setupObserved の self の部分) で Plan を組み、帯で決める / I/F で決める の両方で突き合わせる (Observed を手で組むと、
// 画面と初期設定が別のサービスを見ている食い違いを見られなかった)。
func TestSelfFixedAgreesWithPlan(t *testing.T) {
	type c struct {
		name, ip, method string
		bridge           bool // サービスを持たない I/F (VM のブリッジ) に帯の番号がある
	}
	for _, x := range []c{
		{"帯の外の手動", "10.0.0.5", "manual", false},
		{"帯の中の手動", "192.168.100.10", "manual", false},
		{"方式が分からない帯の番号", "192.168.100.10", "", false},
		{"方式が分からない帯の外", "10.0.0.5", "", false},
		{"DHCP + ブリッジに帯の番号", "169.254.13.42", "DHCP", true},
	} {
		for _, byBand := range []bool{true, false} {
			h := newHarness(t, nil, nil)
			nn := selfNet()
			nn.Services[0].Method, nn.Services[0].IP = x.method, x.ip
			nn.IPs[1].Addr = x.ip
			if x.bridge {
				nn.IPs = append(nn.IPs, netinfo.IP{Device: "bridge100", Addr: "192.168.100.1", Kind: netinfo.KindVirtual})
			}
			h.m.o.ReadNet = func(context.Context) (netinfo.Snapshot, error) { return nn, nil }
			awaitNetLoad(h.m)
			h.m.startNetLoad()
			awaitNetLoad(h.m)
			h.m.pf.LanByBand, h.m.pf.LanIF = byBand, "en13"
			o := setup.Observed{Band: h.m.pf.Band}
			if s, ok := h.m.selfWired(); ok {
				o.SelfService, o.SelfIP, o.SelfMethod = s.Name, s.IP, s.Method
			}
			var planDone bool
			for _, st := range setup.Plan(o) {
				if st.ID == setup.StepSelfStatic {
					planDone = st.Done
				}
			}
			self := h.m.rows()[0]
			if got := h.m.lanFixed(self); got != planDone || (planDone && self.lanIP != o.SelfIP) {
				t.Errorf("%s (帯で決める=%v): 画面の固定=%v (内部 IP %s) / 初期設定の済み=%v (%s)", x.name, byBand, got, self.lanIP, planDone, o.SelfIP)
			}
		}
	}
}

// 整合の監査で揃えた案内と効くキー: この Mac の行では Y / y / O / o を断る (自分宛ての ssh をコピーしていた)。ユーザー名が分からないときは
// s / Y を案内しない (最下行・詳細のカード)。初期設定 (enter) は詳細のカードと ? の板に出る。
func TestGuidanceMatchesKeys(t *testing.T) {
	h := newHarness(t, nil, []discover.Peer{mini})
	h.keys("h") // この Mac のタブ
	for _, k := range []string{"Y", "y", "O", "o", "a", "s"} {
		h.copied = nil
		h.m.TakeNotices()
		if a := h.keys(k); a == Exec || len(h.copied) != 0 {
			t.Errorf("この Mac の行で %s が効いた: %v %v", k, a, h.copied)
		}
		// キーごとに、この Mac の行だから断ったと出す (smb / 画面共有が無いことや、ユーザー名の理由で断ったように見せない)
		if n := h.m.TakeNotices(); len(n) == 0 || !strings.Contains(n[len(n)-1].Text, "この Mac には") {
			t.Errorf("%s: この Mac の行だから断ったと出さない: %+v", k, n)
		}
	}

	h = newHarness(t, nil, []discover.Peer{mini})
	h.m.o.User = ""
	h.keys("l")
	v := stripView(strings.Join(h.m.View(), "\n"))
	if strings.Contains(v, "ssh @") || regexp.MustCompile(`(^|\s)s\s+繋ぐ`).MatchString(v) || regexp.MustCompile(`Y\s+コピー`).MatchString(v) {
		t.Errorf("ユーザー名が分からないのに ssh を案内した:\n%s", v)
	}
	if !regexp.MustCompile(`enter\s+詳細・初期設定`).MatchString(v) {
		t.Errorf("詳細のカードに初期設定の入口が無い:\n%s", v)
	}
	h.keys("?")
	if v := stripView(strings.Join(h.m.View(), "\n")); !strings.Contains(v, "初期設定") {
		t.Errorf("? の板に初期設定が無い:\n%s", v)
	}
}

// stripView は描画から色を落とす (案内の文字列を探すため)。
func stripView(s string) string { return termwidth.StripSGR(s) }

// dump の Report は、画面と同じ宛先 (有線の名前) を target に出す (connect_ip だけだと実際に繋ぐ先と食い違った)。
func TestReportShowsTarget(t *testing.T) {
	h := wiredHarness(t, map[string][]string{"m2macmini.wired.local": {"169.254.217.100"}})
	for _, r := range h.m.Report().Rows {
		if r.Host == "m2macmini.local" {
			if r.Target != "m2macmini.wired.local" || !r.WiredOK || !r.LanFixed || !r.Listed {
				t.Errorf("Report の行 = %+v", r)
			}
			return
		}
	}
	t.Fatal("Report に m2macmini が無い")
}

// 管理対象の行の初期設定の印 (! と黄の地。ユーザー選定 2026-10-17)。途中と言い切れる行だけに付け、確かめていない・Wi-Fi で繋ぐ相手には
// 付けない (判定できていない行を途中と言わない)。未管理の行には付けない。
func TestSetupPendingMark(t *testing.T) {
	row := func(h *harness, host string) row {
		t.Helper()
		for _, r := range h.m.rows() {
			if r.host == host {
				return r
			}
		}
		t.Fatalf("%s の行が無い", host)
		return row{}
	}
	// 有線の名前がその内部 IP に引ける = 完了
	done := wiredHarness(t, map[string][]string{"m2macmini.wired.local": {"169.254.217.100"}})
	if done.m.setupPending(row(done, "m2macmini.local")) {
		t.Error("有線の名前が引けるのに途中にした")
	}
	// 広告されていない (引いて 0 件) = 途中
	missing := wiredHarness(t, map[string][]string{})
	if !missing.m.setupPending(row(missing, "m2macmini.local")) {
		t.Error("有線の名前が広告されていないのに途中にしない")
	}
	// 別の IP に向いている = 途中
	other := wiredHarness(t, map[string][]string{"m2macmini.wired.local": {"169.254.1.2"}})
	if !other.m.setupPending(row(other, "m2macmini.local")) {
		t.Error("有線の名前が別の IP に向いているのに途中にしない")
	}
	// 確かめていない (名前を引く口が無い) = 分からない
	unknown := newHarness(t, []ledger.Host{{Host: "m2macmini.local", User: "koji", LAN: "169.254.217.100"}}, []discover.Peer{mini})
	if unknown.m.setupPending(row(unknown, "m2macmini.local")) {
		t.Error("有線の名前を確かめていないのに途中にした")
	}
	// 台帳には帯の番号があるのに、相手は有線で 169.254 (自動割り当て) のまま = 名前がその番号に引けない = 途中
	unfixed := newHarness(t, []ledger.Host{{Host: "m2macmini.local", User: "koji", LAN: "192.168.100.13"}}, []discover.Peer{mini})
	setSelfStatic(unfixed.m) // この Mac は帯の固定 (169.254 のままだと台帳の番号と網が分かれ、Wi-Fi で繋ぐ = 分からない、になる)
	unfixed.m.o.LookupHost = func(context.Context, string) ([]string, error) { return nil, nil }
	unfixed.m.startWiredLookups()
	awaitWired(t, unfixed.m)
	if r := row(unfixed, "m2macmini.local"); !unfixed.m.setupPending(r) {
		t.Errorf("台帳の番号に相手が居ないのに途中にしない (lan=%s)", r.lanIP)
	}
	// この Mac が 169.254 のままで台帳の番号と網が分かれる = Wi-Fi で繋ぐ相手 = 有線の名前を確かめられない = 分からない
	split := newHarness(t, []ledger.Host{{Host: "m2macmini.local", User: "koji", LAN: "192.168.100.13"}}, []discover.Peer{mini})
	split.m.o.LookupHost = func(context.Context, string) ([]string, error) { return nil, nil }
	split.m.startWiredLookups()
	awaitWired(t, split.m)
	if split.m.setupPending(row(split, "m2macmini.local")) {
		t.Error("網が分かれて有線の名前を確かめられないのに途中にした")
	}
	// 台帳に内部 IP が無い = 途中 (見えていなくても)
	noLAN := newHarness(t, []ledger.Host{{Host: "away.local", User: "koji"}}, nil)
	if !noLAN.m.setupPending(row(noLAN, "away.local")) {
		t.Error("台帳に内部 IP が無いのに途中にしない")
	}
	// 未管理の行・この Mac の行には付けない
	unmanaged := newHarness(t, nil, []discover.Peer{mini})
	for _, r := range unmanaged.m.rows() {
		if unmanaged.m.setupPending(r) {
			t.Errorf("%s (self=%v managed=%v) に印を付けた", r.host, r.self, r.managed)
		}
	}

	// 画面: 途中の行は先頭が ! で地が黄、完了の行は印も地も無い (カーソルは別の管理対象 zz.local に置き、カーソルの地と重ねない)
	for _, c := range []struct {
		answers map[string][]string
		want    bool
	}{{map[string][]string{}, true}, {map[string][]string{"m2macmini.wired.local": {"169.254.217.100"}}, false}} {
		h := newHarness(t, []ledger.Host{{Host: "m2macmini.local", User: "koji", LAN: "169.254.217.100"}, {Host: "zz.local", User: "koji", LAN: "192.168.100.20"}}, []discover.Peer{mini})
		h.m.o.LookupHost = func(_ context.Context, name string) ([]string, error) { return c.answers[name], nil }
		h.m.startWiredLookups()
		awaitWired(t, h.m)
		h.m.Resize(120, 40)
		h.cursorTo(t, "zz.local")
		var line string
		for _, l := range h.m.View() {
			if strings.Contains(termwidth.StripSGR(l), "m2macmini") { // 幅 120 ではホスト名の列が … で切れる
				line = l
				break
			}
		}
		if line == "" {
			t.Fatal("一覧に m2macmini の行が無い")
		}
		plain := strings.TrimLeft(termwidth.StripSGR(line), " ▐") // 一覧の枠 (▐) と余白を剥がす
		if got := strings.HasPrefix(plain, "!"); got != c.want {
			t.Errorf("先頭の ! = %v, want %v: %q", got, c.want, plain)
		}
		if got := strings.Contains(line, "48;2;46;39;25"); got != c.want { // cPendBg (#2E2719)
			t.Errorf("黄の地 = %v, want %v", got, c.want)
		}
	}
}

// IP が見えたこの Mac の I/F の線を行に持つ。Wi-Fi (en0) と、ルーターに有線で繋いだ Ethernet (en7) の両方で見えた相手は、I/F 番号の
// 順 (en0 が先) ではなく有線をメインにし、無線の IP は捨てずに「その他」に残す。
func TestRowsLinkPrefersWiredMain(t *testing.T) {
	net := selfNet()
	net.Services = append(net.Services, netinfo.Service{Name: "Ethernet", Port: "Ethernet", Device: "en7", Active: true, IP: "192.168.50.20", Method: "DHCP", Router: "192.168.50.1", Read: true})
	both := discover.Peer{Host: "both.local", SSH: true, Addrs: map[int]string{14: "192.168.50.7", 20: "192.168.50.8", 27: "169.254.217.100"}}
	m := New(Options{
		ReadNet:   func(context.Context) (netinfo.Snapshot, error) { return net, nil },
		StartScan: func(time.Time) Scanner { return &fakeScan{peers: []discover.Peer{both}} },
		IfName:    func(i int) string { return map[int]string{14: "en0", 20: "en7", 27: "en13"}[i] },
		Now:       func() time.Time { return t0 },
	})
	awaitNetLoad(m)
	rs := m.rows()
	r := rs[len(rs)-1]
	if r.mainIP != "192.168.50.8" || r.mainLink != netinfo.LinkWired {
		t.Errorf("メイン = %q (%v), want 192.168.50.8 (有線)", r.mainIP, r.mainLink)
	}
	if r.lanIP != "169.254.217.100" || r.lanLink != netinfo.LinkWired {
		t.Errorf("内部 = %q (%v)", r.lanIP, r.lanLink)
	}
	if len(r.others) != 1 || r.others[0].Addr != "192.168.50.7" || r.others[0].Device != "en0" {
		t.Errorf("その他 = %+v (無線の IP を残す)", r.others)
	}
	// 無線でしか見えない相手のメインは無線
	wifiOnly := discover.Peer{Host: "air.local", SSH: true, Addrs: map[int]string{14: "192.168.50.44"}}
	m2 := New(Options{
		ReadNet:   func(context.Context) (netinfo.Snapshot, error) { return net, nil },
		StartScan: func(time.Time) Scanner { return &fakeScan{peers: []discover.Peer{wifiOnly}} },
		IfName:    func(i int) string { return map[int]string{14: "en0"}[i] },
		Now:       func() time.Time { return t0 },
	})
	awaitNetLoad(m2)
	rs = m2.rows()
	if r := rs[len(rs)-1]; r.mainIP != "192.168.50.44" || r.mainLink != netinfo.LinkWireless || len(r.others) != 0 {
		t.Errorf("無線だけの相手 = %+v", r)
	}
	if self := rs[0]; self.mainLink != netinfo.LinkWireless || self.lanLink != netinfo.LinkWired {
		t.Errorf("この Mac の線: メイン %v / 内部 %v (Wi-Fi / Thunderbolt Ethernet)", self.mainLink, self.lanLink)
	}
}

// 管理対象の内部 IP は台帳の値。今の走査で同じ IP が見えた I/F があるときだけ線が分かり、見えていなければ分からない (括弧を出さない)。
func TestLedgerLanLinkOnlyWhenSeen(t *testing.T) {
	seen := discover.Peer{Host: "studio.local", SSH: true, Addrs: map[int]string{27: "169.254.5.5"}}
	h := newHarness(t, []ledger.Host{
		{Host: "studio.local", User: "koji", LAN: "169.254.5.5"},
		{Host: "mini.local", User: "koji", LAN: "169.254.6.6"},
	}, []discover.Peer{seen, {Host: "mini.local", SSH: true, Addrs: map[int]string{27: "169.254.7.7"}}})
	by := map[string]row{}
	for _, r := range h.m.rows() {
		by[r.host] = r
	}
	if r := by["studio.local"]; r.lanIP != "169.254.5.5" || r.lanLink != netinfo.LinkWired {
		t.Errorf("台帳の IP が見えた相手 = %q (%v)", r.lanIP, r.lanLink)
	}
	if r := by["mini.local"]; r.lanIP != "169.254.6.6" || r.lanLink != netinfo.LinkUnknown {
		t.Errorf("台帳の IP とは別の IP で見えた相手 = %q (%v), want 線は分からない", r.lanIP, r.lanLink)
	}
	if r := by["studio.local"]; len(r.others) != 0 {
		t.Errorf("台帳の内部 IP と同じ IP を「その他」に重ねた: %+v", r.others)
	}
	if r := by["mini.local"]; len(r.others) != 1 || r.others[0].Addr != "169.254.7.7" {
		t.Errorf("台帳と別の IP は「その他」に残す: %+v", r.others)
	}
}

// 一覧と詳細カードで、IP の後ろに線の括弧が出る。線が分からない IP (登録の無い I/F) には括弧を出さない。
func TestViewShowsLinkTags(t *testing.T) {
	vm := discover.Peer{Host: "vm.local", SSH: true, Addrs: map[int]string{30: "192.168.64.3"}}
	h := newHarness(t, nil, []discover.Peer{mini, vm})
	h.m.o.IfName = func(i int) string { return map[int]string{14: "en0", 27: "en13", 30: "bridge100"}[i] }
	h.m.Resize(160, 50)
	h.keys("l") // 未管理のタブ
	h.cursorTo(t, "m2macmini.local")
	view := termwidth.StripSGR(strings.Join(h.m.View(), "\n"))
	for _, want := range []string{"192.168.50.7    (無線)", "169.254.217.100 (有線)", "メイン      192.168.50.7 (無線)"} {
		if !strings.Contains(view, want) {
			t.Errorf("画面に %q が無い:\n%s", want, view)
		}
	}
	if !regexp.MustCompile(`ssh koji@169\.254\.217\.100 +\(有線\)`).MatchString(view) { // 宛先の行にも、繋ぐ IP の線
		t.Errorf("ssh の宛先の行に (有線) が無い:\n%s", view)
	}
	h.cursorTo(t, "vm.local")
	view = termwidth.StripSGR(strings.Join(h.m.View(), "\n"))
	if !strings.Contains(view, "192.168.64.3") || strings.Contains(view, "192.168.64.3 (") || strings.Contains(view, "192.168.64.3    (") {
		t.Errorf("線の分からない IP に括弧を出した / IP が無い:\n%s", view)
	}
}

// 同じ IP が Wi-Fi と有線の 2 つの I/F で見えた相手は、メインを有線にし、同じ IP を「その他」に重ねない (重ねると「その他」の行に
// 無線の I/F の名前と有線の線が並んだ)。有線の I/F の番号が先でも同じ。
func TestSameIPOnTwoIFs(t *testing.T) {
	net := selfNet()
	net.Services = append(net.Services, netinfo.Service{Name: "Ethernet", Port: "Ethernet", Device: "en7", Active: true, IP: "192.168.50.20", Method: "DHCP", Router: "192.168.50.1", Read: true})
	for _, names := range []map[int]string{{14: "en0", 20: "en7"}, {14: "en7", 20: "en0"}} {
		p := discover.Peer{Host: "one.local", SSH: true, Addrs: map[int]string{14: "192.168.50.7", 20: "192.168.50.7"}}
		m := New(Options{
			ReadNet:   func(context.Context) (netinfo.Snapshot, error) { return net, nil },
			StartScan: func(time.Time) Scanner { return &fakeScan{peers: []discover.Peer{p}} },
			IfName:    func(i int) string { return names[i] },
			Now:       func() time.Time { return t0 },
		})
		awaitNetLoad(m)
		rs := m.rows()
		r := rs[len(rs)-1]
		if r.mainIP != "192.168.50.7" || r.mainLink != netinfo.LinkWired || len(r.others) != 0 || m.connectLink(r) != netinfo.LinkWired {
			t.Errorf("%v: メイン %q (%v) / その他 %+v / 繋ぐ線 %v", names, r.mainIP, r.mainLink, r.others, m.connectLink(r))
		}
	}
}

// 選んだ行でも、IP と括弧は 1 つの文字列として描く (セルの中で色を分けると、選んだ行の地の色が IP と括弧の間で途切れた)。
func TestSelectedRowKeepsIPAndTagTogether(t *testing.T) {
	h := newHarness(t, nil, []discover.Peer{mini})
	h.m.Resize(160, 40)
	h.keys("l")
	h.cursorTo(t, "m2macmini.local")
	for _, l := range h.m.View() {
		if strings.Contains(termwidth.StripSGR(l), "m2macmini.local") && strings.Contains(termwidth.StripSGR(l), "(無線)") {
			if !strings.Contains(l, "192.168.50.7    (無線)") {
				t.Errorf("選んだ行の IP と括弧の間に SGR が入った: %q", l)
			}
			return
		}
	}
	t.Fatal("一覧に m2macmini の行が無い")
}

// 台帳の内部 IP が有線の I/F では見えず、同じ IP が Wi-Fi でメインとして見えた相手は、繋ぐ先の線に無線を出す (内部 IP の線が
// 分からないことを理由に括弧を落とさない)。
func TestConnectLinkFallsBackToMainForLedgerIP(t *testing.T) {
	// 169.254 同士なので有線の網は分かれていない (lanAway が偽) 扱いになり、繋ぐ先は内部 IP の分岐を通る
	h := newHarness(t, []ledger.Host{{Host: "air.local", User: "koji", LAN: "169.254.9.9"}},
		[]discover.Peer{{Host: "air.local", SSH: true, Addrs: map[int]string{14: "169.254.9.9"}}})
	var r row
	for _, x := range h.m.rows() {
		if x.host == "air.local" {
			r = x
		}
	}
	if r.lanAway || r.mainIP != "169.254.9.9" {
		t.Fatalf("前提: lanAway=%v メイン=%q (内部 IP の分岐を通らない)", r.lanAway, r.mainIP)
	}
	if r.connectIP() != "169.254.9.9" || r.lanLink != netinfo.LinkUnknown || h.m.connectLink(r) != netinfo.LinkWireless {
		t.Errorf("繋ぐ先 %q / 内部の線 %v / 繋ぐ線 %v, want 無線", r.connectIP(), r.lanLink, h.m.connectLink(r))
	}
}

// vpnPeer は Wi-Fi (14) と VPN (5。en0 より小さい番号) で見える未管理の Mac。分類できないと VPN の IP が「その他」の先頭に来る。
var vpnPeer = discover.Peer{Host: "vpn.local", Instances: []string{"vpn"}, SSH: true, Addrs: map[int]string{5: "100.64.0.20", 14: "192.168.50.7"}}

func vpnIfName(i int) string { return map[int]string{5: "utun4", 14: "en0", 27: "en13"}[i] }

// この Mac のネットワークを読み直して失敗したら、前に読んだ値で分け続け (空の Snapshot で上書きしない)、読めなかったことを知らせる。
// 上書きすると相手の IP が全部「その他」に落ち、s が VPN の IP へ繋いだ。
func TestNetReadFailureKeepsPreviousSelf(t *testing.T) {
	h := newHarness(t, nil, []discover.Peer{vpnPeer})
	h.m.o.IfName = vpnIfName
	h.keys("l")
	h.cursorTo(t, "vpn.local")
	if ip := h.m.current().connectIP(); ip != "192.168.50.7" {
		t.Fatalf("前提: 読めたときの宛先 = %q", ip)
	}
	h.m.o.ReadNet = func(context.Context) (netinfo.Snapshot, error) { return netinfo.Snapshot{}, context.DeadlineExceeded }
	h.m.TakeNotices()
	h.keys("r")
	awaitNetLoad(h.m)
	h.cursorTo(t, "vpn.local")
	if ip := h.m.current().connectIP(); ip != "192.168.50.7" {
		t.Errorf("読み直しに失敗した後の宛先 = %q (前に読んだ値で分けていない)", ip)
	}
	if n := h.m.TakeNotices(); !slices.ContainsFunc(n, func(x Notice) bool { return strings.Contains(x.Text, "読めない") }) {
		t.Errorf("読めなかったことを知らせない: %+v", n)
	}
}

// 最初の読み込みから失敗したら、相手の IP を分けず、繋ぐ・初期設定を「読めなかった」で断る (「確認中」と出し続けない)。
func TestNetReadFailureFirstRefusesActions(t *testing.T) {
	m := New(Options{
		ReadNet: func(context.Context) (netinfo.Snapshot, error) {
			return netinfo.Snapshot{}, errors.New("networksetup が失敗した")
		},
		LocalHost: func() string { return "me.local" },
		StartScan: func(time.Time) Scanner { return &fakeScan{peers: []discover.Peer{vpnPeer}} },
		IfName:    vpnIfName,
		Now:       func() time.Time { return t0 },
		User:      "koji",
	})
	awaitNetLoad(m)
	m.HandleKey("l")
	if r := m.current(); r.host != "vpn.local" || len(r.others) != 0 || r.mainIP != "" {
		t.Fatalf("読めないのに相手の IP を分けた: %+v", r)
	}
	m.Resize(120, 40)
	if v := termwidth.StripSGR(strings.Join(m.View(), "\n")); !strings.Contains(v, "ネットワークを読めない") {
		t.Errorf("見出しに読めないことを出さない (「N 台見つかった」に戻った):\n%s", v)
	}
	if sl := m.selfLan(); !strings.Contains(sl.Note, "読めていない") {
		t.Errorf("この Mac の内部 IP の説明が「無い」と「読めていない」を分けない: %q", sl.Note)
	}
	m.TakeNotices()
	for _, ks := range [][]string{{"s"}, {"enter", "enter"}} { // 初期設定は詳細のパネルの enter
		k := strings.Join(ks, " ")
		var a Action
		for _, key := range ks {
			a = m.HandleKey(key)
		}
		if a != None || m.sc == scSetup {
			t.Errorf("読めないのに %s を受けた", k)
		}
		if n := m.TakeNotices(); len(n) == 0 || !strings.Contains(n[len(n)-1].Text, "読めなかった") {
			t.Errorf("%s を「読めなかった」で断らない: %+v", k, n)
		}
		if m.detailOpen {
			m.HandleKey("esc")
		}
	}
}

// I/F の IP を読み切れなかった回 (サービスは読めた) は、その Snapshot で相手を分ける (サービスだけで分類は決まる)。
func TestNetReadIPsIncompleteStillClassifies(t *testing.T) {
	h := newHarness(t, nil, []discover.Peer{vpnPeer})
	h.m.o.IfName = vpnIfName
	h.m.o.ReadNet = func(context.Context) (netinfo.Snapshot, error) {
		s := selfNet()
		s.IPs = nil
		return s, fmt.Errorf("%w: %w", netinfo.ErrIPsIncomplete, context.DeadlineExceeded)
	}
	h.keys("r")
	awaitNetLoad(h.m)
	if len(h.m.self.IPs) != 0 {
		t.Errorf("サービスまで読めた回の Snapshot を採らない")
	}
	h.keys("l")
	h.cursorTo(t, "vpn.local")
	if ip := h.m.current().connectIP(); ip != "192.168.50.7" {
		t.Errorf("宛先 = %q", ip)
	}
}

// 空のタブを見ている間に裏の読み込みが終わって行が現れたら、次のキーは一度も描かれていない Mac に効かせない
// (カーソルは相手の行に無かったので、見ていた Mac が消えたかでは判定できない)。
func TestKeyRefusedWhenRowsAppearUnderEmptyTab(t *testing.T) {
	release := make(chan struct{})
	m := New(Options{
		ReadNet: func(ctx context.Context) (netinfo.Snapshot, error) {
			select {
			case <-release:
			case <-ctx.Done():
			}
			return selfNet(), nil
		},
		LocalHost: func() string { return "me.local" },
		StartScan: func(time.Time) Scanner { return &fakeScan{peers: []discover.Peer{mini}} },
		IfName:    func(i int) string { return map[int]string{14: "en0", 27: "en13"}[i] },
		Now:       func() time.Time { return t0 },
		User:      "koji",
	})
	m.pf.WiredOnly = true // 読み終えるまで有線かが分からないので、未管理のタブは空
	m.HandleKey("l")
	if len(m.visible()) != 0 {
		t.Fatalf("前提: 未管理のタブが空でない: %v", m.visibleHosts())
	}
	close(release)
	for deadline := time.Now().Add(5 * time.Second); len(m.netCh) == 0; { // 結果が届くまで (取り込みはキーの入口で)
		if time.Now().After(deadline) {
			t.Fatal("読み込みの結果が届かない")
		}
		time.Sleep(time.Millisecond) // sleep-ok: tick: 条件のポーリングの刻み (待ちの実体は上の条件)
	}
	m.TakeNotices()
	if a := m.HandleKey("s"); a == Exec {
		t.Fatal("一度も描かれていない Mac へ s が繋いだ")
	}
	if n := m.TakeNotices(); len(n) == 0 || !strings.Contains(n[len(n)-1].Text, "一覧が変わった") {
		t.Errorf("行が変わったことで断らない: %+v", n)
	}
	if a := m.HandleKey("s"); a != Exec { // 描かれた後 (次のキー) は効く
		t.Errorf("行が出た後の s が効かない: %v %+v", a, m.TakeNotices())
	}
}

// 「その他の IP に含める種類」は相手の行の +N の札と詳細カードにも効く (この Mac の行だけを絞っていた)。
// 繋ぐ先の最後の手段 (connectIP) は絞らない。
func TestPeerOthersFollowKinds(t *testing.T) {
	vm := discover.Peer{Host: "vm.local", Instances: []string{"vm"}, SSH: true, Addrs: map[int]string{5: "100.64.0.20"}}
	h := newHarness(t, nil, []discover.Peer{vm})
	h.m.o.IfName = vpnIfName
	h.m.pf.ShowOthers = true
	h.keys("l")
	h.cursorTo(t, "vm.local")
	r := h.m.current()
	if len(h.m.shownOthers(r)) != 0 || r.connectIP() != "100.64.0.20" {
		t.Errorf("既定 (VPN を含めない) で相手の VPN を出した / 繋ぐ先まで絞った: shown=%v connect=%q", h.m.shownOthers(r), r.connectIP())
	}
	if v := termwidth.StripSGR(strings.Join(h.m.View(), "\n")); strings.Contains(v, "+1") || strings.Contains(v, "100.64.0.20  ") && strings.Contains(v, "その他の IP") {
		t.Errorf("画面に絞った種類が出た:\n%s", v)
	}
	h.m.pf.Kinds = append(h.m.pf.Kinds, netinfo.KindVPN.Key())
	if v := termwidth.StripSGR(strings.Join(h.m.View(), "\n")); !strings.Contains(v, "+1") || !strings.Contains(v, "その他の IP") {
		t.Errorf("VPN を含めたのに出ない:\n%s", v)
	}
}

// I/F で決める設定でも、帯の行は値を描き、編集中は入力欄を出す (薄い行は値の代わりに説明を描き、入力欄もキャレットの文字も消えた)。
func TestBandRowEditableWhenLanByIF(t *testing.T) {
	h := newHarness(t, nil, nil)
	h.m.pf.LanByBand = false
	h.m.Resize(120, 40)
	h.keys(bandKeys()...)
	if !h.m.bandEdit {
		t.Fatal("前提: 帯の編集に入っていない")
	}
	lines := h.m.View()
	x, y, ok := h.m.CaretPos()
	if !ok {
		t.Fatal("帯の入力欄にキャレットが無い")
	}
	line := termwidth.StripSGR(lines[y])
	if i := strings.Index(line, "0/24"); i < 0 || x != termwidth.Of(line[:i])+termwidth.Of("0/24") {
		t.Errorf("キャレットの行に入力欄が無い / 欄の文字の直後に無い: x=%d %q", x, line)
	}
}

// 帯の行で space / ← → を押したら enter での編集を案内する (互いを指して堂々巡りにしない)。
func TestBandRowRefusalPointsToEnter(t *testing.T) {
	h := newHarness(t, nil, nil)
	h.keys(",")
	for h.m.setC != setBand {
		h.keys("j")
	}
	for _, k := range []string{"space", "right", "left"} {
		h.m.TakeNotices()
		h.keys(k)
		if n := h.m.TakeNotices(); len(n) == 0 || !strings.Contains(n[len(n)-1].Text, "enter で編集") {
			t.Errorf("帯の行の %s の案内 = %+v", k, n)
		}
	}
}

// 狭い端末 (幅 36〜41) でも、別名・ユーザー名の入力の板は前置きと欄を 1 行に収め、キャレットを欄の中に立てる。
func TestAliasFieldFitsNarrowWidths(t *testing.T) {
	for _, key := range []string{"m", "u"} {
		for w := minWidth; w <= 44; w++ {
			h := newHarness(t, []ledger.Host{{Host: "m2macmini.local", User: "k2"}}, []discover.Peer{mini})
			h.m.Resize(w, 30)
			h.keys("j", key)
			h.m.alias.Reset()
			for range 24 {
				h.m.HandleInput("a", "a")
			}
			lines := h.m.View()
			x, y, ok := h.m.CaretPos()
			if !ok {
				t.Fatalf("%s w=%d: キャレットが無い", key, w)
			}
			line := termwidth.StripSGR(lines[y])
			if !strings.Contains(line, h.m.edit.label) || !strings.Contains(line, "aaaa") {
				t.Errorf("%s w=%d: キャレットの行に前置きと欄が無い (割れた): %q", key, w, line)
			}
			if x >= w || termwidth.Of(line) < x {
				t.Errorf("%s w=%d: キャレット x=%d が画面か行の外: %q", key, w, x, line)
			}
		}
	}
}

// space の確認の板を出している間に別のプロセスが同じ Mac を載せたら、y はその行を残す (古い一覧の値で上書きして別名・内部 IP を消した)。
func TestSpaceRegisterKeepsRowWrittenMeanwhile(t *testing.T) {
	h := newHarness(t, nil, []discover.Peer{mini})
	h.keys("l")
	h.cursorTo(t, "m2macmini.local")
	h.keys("space")
	if h.m.ov != ovConfirm {
		t.Fatal("前提: 確認の板が出ない")
	}
	h.store.hosts = append(h.store.hosts, ledger.Host{Host: "m2macmini.local", User: "k2", Alias: "mini", LAN: "192.168.100.12"})
	h.keys("y")
	if len(h.store.hosts) != 1 || h.store.hosts[0] != (ledger.Host{Host: "m2macmini.local", User: "k2", Alias: "mini", LAN: "192.168.100.12"}) {
		t.Errorf("別のプロセスが載せた行を上書きした: %+v", h.store.hosts)
	}
	h = newHarness(t, nil, []discover.Peer{mini}) // 誰も載せていなければ今どおり足す
	h.keys("l")
	h.cursorTo(t, "m2macmini.local")
	h.keys("space", "y")
	if len(h.store.hosts) != 1 || h.store.hosts[0].Host != "m2macmini.local" || h.store.hosts[0].User != "koji" {
		t.Errorf("登録しない: %+v", h.store.hosts)
	}
}

// 書いた後の台帳の読み直しに失敗したら知らせる (黙ると一覧は古いまま「未管理」で、理由が出なかった)。
func TestReloadLedgerFailureIsNoticed(t *testing.T) {
	h := newHarness(t, nil, []discover.Peer{mini})
	h.keys("l")
	h.cursorTo(t, "m2macmini.local")
	h.keys("space")
	h.store.loadErr = errors.New("permission denied")
	h.m.TakeNotices()
	h.keys("y")
	if n := h.m.TakeNotices(); !slices.ContainsFunc(n, func(x Notice) bool { return strings.Contains(x.Text, "読み直せない") }) {
		t.Errorf("読み直しの失敗を知らせない: %+v", n)
	}
}

// 一覧の enter は詳細のパネルを重ねる (低い端末では一覧の下の詳細カードが 1 行に畳まれるので、どの高さでも詳細を見られるように)。
// パネルは最下行の案内 (抜け方) を覆わず、中の enter で初期設定を開き、q / esc は glogx を閉じずに一覧へ戻る。
func TestDetailPanelOpensOverListAndLeadsToSetup(t *testing.T) {
	h := newHarness(t, []ledger.Host{{Host: "m2macmini.local", User: "k2", LAN: "192.168.100.13", Alias: "mini"}}, []discover.Peer{mini})
	h.m.Resize(100, 22)
	h.cursorTo(t, "m2macmini.local")
	if v := stripView(strings.Join(h.m.View(), "\n")); !strings.Contains(v, "enter で詳細") {
		t.Fatalf("低い端末で、畳んだ詳細に enter の案内が無い:\n%s", v)
	}
	if a := h.keys("enter"); a != None || !h.m.detailOpen || !h.m.OwnsKeys() {
		t.Fatalf("enter で詳細のパネルが開かない (action=%v open=%v)", a, h.m.detailOpen)
	}
	lines := h.m.View()
	v := stripView(strings.Join(lines, "\n"))
	for _, want := range []string{"m2macmini.local", "ssh k2@", "有線・固定"} {
		if !strings.Contains(v, want) {
			t.Errorf("パネルに %q が無い:\n%s", want, v)
		}
	}
	last := ""
	for i := len(lines) - 1; i >= 0 && last == ""; i-- {
		last = strings.TrimSpace(stripView(lines[i]))
	}
	if !strings.Contains(last, "戻る") || !strings.Contains(last, "初期設定") {
		t.Errorf("最下行がパネルの案内でない / パネルに覆われた: %q", last)
	}
	// 詳細のパネルの中でも a で SyakeScreen を開き、パネルは開いたまま
	if a := h.keys("a"); a != Exec || !h.m.detailOpen {
		t.Fatalf("パネルの中で a が効かない (action=%v open=%v)", a, h.m.detailOpen)
	}
	if r, _ := h.m.TakeExec(); len(r.Argv) != 2 || !strings.HasPrefix(r.Argv[1], "syakescreen://connect?") || !strings.Contains(r.Argv[1], "user=k2") {
		t.Errorf("パネルの a = %q", r.Argv)
	}
	if a := h.keys("q"); a != None || h.m.detailOpen {
		t.Fatalf("q でパネルが閉じない / glogx ごと閉じた (action=%v)", a)
	}
	h.keys("enter", "enter")
	if h.m.sc != scSetup || !h.m.detailOpen {
		t.Errorf("パネルの enter で初期設定がパネルの上に開かない (sc=%d open=%v)", h.m.sc, h.m.detailOpen)
	}
}

// 高い端末でもパネルは最下行の上まで伸びる: 内容の高さで止めると、パネルの下から一覧の中の詳細カードが覗き、同じ中身が 2 つ見えた。
func TestDetailPanelFillsTallTerminal(t *testing.T) {
	h := newHarness(t, []ledger.Host{{Host: "m2macmini.local", User: "k2", LAN: "192.168.100.13", Alias: "mini"}}, []discover.Peer{mini})
	h.m.Resize(100, 46)
	h.cursorTo(t, "m2macmini.local")
	if n := strings.Count(stripView(strings.Join(h.m.View(), "\n")), "ssh k2@"); n != 1 {
		t.Fatalf("前提: 開く前の詳細カードの ssh の行が %d 個", n)
	}
	h.keys("enter")
	if n := strings.Count(stripView(strings.Join(h.m.View(), "\n")), "ssh k2@"); n != 1 {
		t.Errorf("パネルの下から一覧の詳細カードが覗く (ssh の行が %d 個)", n)
	}
}

// 入らない高さでは、最後の行を送りの案内にして j/k で送る。パネルの間は一覧のカーソルを動かさない。動作のキーは一覧と同じに効く。
func TestDetailPanelScrollsAndKeepsRowActions(t *testing.T) {
	h := newHarness(t, []ledger.Host{
		{Host: "studio.local", User: "koji", LAN: "192.168.100.12", Alias: "studio"},
		{Host: "m2macmini.local", User: "k2", LAN: "192.168.100.13", Alias: "mini"},
	}, []discover.Peer{mini})
	h.m.Resize(72, 20)
	h.cursorTo(t, "m2macmini.local")
	cur := h.m.cur[h.m.tab]
	h.keys("enter")
	lines := h.m.View()
	if v := stripView(strings.Join(lines, "\n")); !strings.Contains(v, "↓ j/k で続き") || h.m.detailMax == 0 {
		t.Fatalf("入らない高さで送りの案内が無い (max=%d):\n%s", h.m.detailMax, v)
	}
	last := ""
	for i := len(lines) - 1; i >= 0 && last == ""; i-- {
		last = strings.TrimSpace(stripView(lines[i]))
	}
	if !strings.Contains(last, "戻る") { // 中身が入り切らない高さでも、最下行 (抜け方の案内) を覆わない
		t.Errorf("最下行がパネルに覆われた: %q", last)
	}
	h.keys("j")
	if h.m.detailOff != 1 || h.m.cur[h.m.tab] != cur {
		t.Errorf("j でパネルを送らない / 一覧のカーソルが動いた (off=%d cur=%d→%d)", h.m.detailOff, cur, h.m.cur[h.m.tab])
	}
	h.keys("G")
	if v := stripView(strings.Join(h.m.View(), "\n")); !strings.Contains(v, "↑ k で戻る") {
		t.Errorf("送りきっても戻りの案内にならない:\n%s", v)
	}
	if a := h.keys("s"); a != Exec || !h.m.detailOpen {
		t.Errorf("パネルの s が一覧と同じに効かない (action=%v open=%v)", a, h.m.detailOpen)
	}
}

// パネルの中の space は台帳の登録 / 削除 (半ページの送りにしない。パネルの本文が「space 台帳から外す」を案内している)。
func TestDetailPanelSpaceAsksLedger(t *testing.T) {
	h := newHarness(t, []ledger.Host{{Host: "m2macmini.local", User: "k2", LAN: "192.168.100.13", Alias: "mini"}}, []discover.Peer{mini})
	h.cursorTo(t, "m2macmini.local")
	h.keys("enter", "space")
	if h.m.ov != ovConfirm {
		t.Errorf("パネルの space で台帳の確認が出ない (ov=%v off=%d)", h.m.ov, h.m.detailOff)
	}
}

// 行の並びが変わっても (別名の付け替え)、パネルは開いた Mac を指し続ける。開いた Mac が消えたら閉じる。Close でも閉じる。
func TestDetailPanelStaysOnOpenedMac(t *testing.T) {
	h := newHarness(t, []ledger.Host{
		{Host: "m2macmini.local", User: "k2", LAN: "192.168.100.13", Alias: "mini"},
		{Host: "studio.local", User: "koji", LAN: "192.168.100.12", Alias: "studio"},
	}, []discover.Peer{mini})
	h.cursorTo(t, "m2macmini.local")
	h.keys("enter", "m")
	for range "mini" {
		h.keys("backspace")
	}
	h.keys("z", "e", "t", "a", "enter") // 並びが studio, zeta に変わる (添字はそのまま)
	h.keys("j")                         // パネルのキー: カーソルを開いた Mac へ戻してから送る
	if !h.m.detailOpen || h.m.current().host != "m2macmini.local" {
		t.Fatalf("別名を変えた後、パネルが別の Mac を指した (open=%v host=%q)", h.m.detailOpen, h.m.current().host)
	}
	h.m.detailHost = "gone.local" // 開いた Mac が並びから消えた
	h.m.View()
	if h.m.detailOpen {
		t.Error("開いた Mac が消えたのにパネルを開いたまま描いた")
	}
	h.keys("enter")
	h.m.Close()
	if h.m.detailOpen {
		t.Error("Close でパネルが閉じない")
	}
}
