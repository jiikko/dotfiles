package lanmgr

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
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
type memStore struct{ hosts []ledger.Host }

func (s *memStore) Load() ([]ledger.Host, error) { return slices.Clone(s.hosts), nil }
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
	peers  []discover.Peer
	busy   bool
	closed int
}

func (f *fakeScan) Snapshot() []discover.Peer { return f.peers }
func (f *fakeScan) Busy(time.Time) bool       { return f.busy }
func (f *fakeScan) Close()                    { f.closed++ }
func (f *fakeScan) Changed() <-chan struct{}  { return nil }

var t0 = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

// selfNet はこの Mac のネットワークの偽物 (2026-10-15 の実測と同じ形: en0 = Wi-Fi / en13 = Thunderbolt Ethernet (169.254) / en18 は設定に無い)。
func selfNet() netinfo.Snapshot {
	return netinfo.Snapshot{
		Services: []netinfo.Service{
			{Name: "Thunderbolt Ethernet", Device: "en13", Active: true, IP: "169.254.13.42", Method: "DHCP"},
			{Name: "Wi-Fi", Device: "en0", Active: true, IP: "192.168.50.250", Method: "DHCP", Router: "192.168.50.1"},
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
	h.m.Resize(120, 40)
	return h
}

func (h *harness) keys(ks ...string) Action {
	var a Action
	for _, k := range ks {
		text := ""
		if len([]rune(k)) == 1 {
			text = k
		}
		a = h.m.HandleInput(k, text)
	}
	return a
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
	want := []string{"ssh", "-o", "HostKeyAlias=m2macmini.local", "k2@192.168.100.13"}
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
	if ip, _ := h.m.selfLan(); ip != "169.254.13.42" {
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
	if ip, note := h.m.selfLan(); ip != "192.168.100.11" || strings.Contains(note, "未固定") {
		t.Errorf("selfLan = %q %q", ip, note)
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
	h.m.pf.Kinds = append(h.m.pf.Kinds, netinfo.KindVPN.String())
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

func TestViewFitsWidthAndKeepsQuit(t *testing.T) {
	h := newHarness(t, []ledger.Host{{Host: "studio.local", Alias: "studio", LAN: "192.168.100.12"}}, []discover.Peer{mini})
	for _, w := range []int{80, 120, 172} {
		h.m.Resize(w, 40)
		lines := h.m.View()
		if len(lines) != 40 {
			t.Errorf("w=%d: %d 行 (高さに合わない)", w, len(lines))
		}
		for i, l := range lines {
			if lw := lipgloss.Width(l); lw > w {
				t.Errorf("w=%d: %d 行目が %d 桁 (はみ出す)", w, i, lw)
			}
		}
		if foot := lines[len(lines)-2]; !strings.Contains(foot, "q") { // 下の余白の 1 行上がキーの案内
			t.Errorf("w=%d: 抜ける手段 (q) が案内から落ちた: %q", w, foot)
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
	m.Resize(100, 30)
	if lines := m.View(); len(lines) != 30 {
		t.Errorf("ネットワークが読めなくても画面は出す: %d 行", len(lines))
	}
}
