package setup

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sandbox は偽の networksetup / sudo / nohup だけが見える PATH を作る。偽の networksetup は引数を 1 つずつ [..] で囲んで log に書く。
// 🚨 本物 (/usr/sbin/networksetup・/usr/bin/sudo・/usr/bin/nohup) は PATH に入れない (`<sandbox>:/bin`)。走らせる前に、3 つとも
// sandbox の中に解決することを確かめ、外へ解決したら走らせずに落とす (_claude/rules/sandbox-real-destructive-test-apis.md: 拒否は実行の前)。
func sandbox(t *testing.T) (dir, path string) {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fakes := map[string]string{
		"networksetup": `for a in "$@"; do printf '[%s]' "$a"; done >> "` + d + `/log"; echo >> "` + d + `/log"`,
		// sudo -v は認証だけ。sudo -n -b <cmd> は <cmd> を背景で起こして戻る (本物と同じく呼び出し側は待たない)
		"sudo":  `if [ "$1" = -v ]; then exit 0; fi; if [ "$1" = -n ] && [ "$2" = -b ]; then shift 2; "$@" & exit 0; fi; echo "fake sudo: $*" >&2; exit 1`,
		"nohup": `exec "$@"`,
	}
	for name, body := range fakes {
		if err := os.WriteFile(filepath.Join(d, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	path = d + ":/bin"
	cmd := exec.Command("/bin/sh", "-c", "command -v networksetup; command -v sudo; command -v nohup")
	cmd.Env = []string{"PATH=" + path}
	out, err := cmd.Output()
	lines := strings.Fields(string(out))
	if err != nil || len(lines) != 3 {
		t.Fatalf("偽物の解決を確かめられない (走らせない): %v %q", err, out)
	}
	for _, l := range lines {
		if !strings.HasPrefix(l, d+"/") {
			t.Fatalf("%s が sandbox の外に解決した (走らせない)", l)
		}
	}
	return d, path
}

func runSh(t *testing.T, path, script string, env ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.Env = append([]string{"PATH=" + path}, env...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	return cmd
}

func readLog(dir string) string { b, _ := os.ReadFile(filepath.Join(dir, "log")); return string(b) }

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); !cond(); {
		if time.Now().After(deadline) {
			t.Fatalf("%s が 10 秒で成り立たない", what)
		}
		time.Sleep(20 * time.Millisecond) // sleep-ok: tick: 条件のポーリングの刻み (待ちの実体は上の条件)
	}
}

func me(t *testing.T) string {
	t.Helper()
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	return u.Username
}

func staticIP(t *testing.T, dir string) StaticIP {
	return StaticIP{Service: "Thunderbolt Ethernet", IP: "192.168.100.11", Mask: "255.255.255.0",
		Restore: Restore{Method: "DHCP"}, Token: "test-token-1", MarkDir: dir, Owner: me(t), Window: time.Second}
}

// viaNewIP は新しい IP (192.168.100.11) に届いた ssh の $SSH_CONNECTION。viaWifi は Wi-Fi の IP に届いた既存の接続。
const (
	viaNewIP = "SSH_CONNECTION=192.168.100.2 50000 192.168.100.11 22"
	viaWifi  = "SSH_CONNECTION=192.168.50.250 50000 192.168.50.7 22"
)

// 確認の印が来なければ、固定 IP にした後で元 (DHCP) に戻す (相手を置き去りにしない)。
func TestStaticIPRollsBackWithoutConfirm(t *testing.T) {
	dir, path := sandbox(t)
	s := staticIP(t, dir)
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := runSh(t, path, s.Script()).Wait(); err != nil {
		t.Fatal(err)
	}
	want := "[-setmanual][Thunderbolt Ethernet][192.168.100.11][255.255.255.0]\n[-setdhcp][Thunderbolt Ethernet]\n"
	if got := readLog(dir); got != want {
		t.Errorf("log = %q, want %q", got, want)
	}
}

// 期限内に確認の印が置かれたら戻さず、印を消して終わる。前の回の印が残っていても、それでは戻しを飛ばさない。
func TestStaticIPKeepsWhenConfirmed(t *testing.T) {
	dir, path := sandbox(t)
	s := staticIP(t, dir)
	s.Window = 5 * time.Second
	if err := os.WriteFile(s.ConfirmPath(), nil, 0o644); err != nil { // 前の回の印
		t.Fatal(err)
	}
	cmd := runSh(t, path, s.Script())
	waitFor(t, "固定 IP にする", func() bool { return strings.Contains(readLog(dir), "-setmanual") })
	if _, err := os.Stat(s.ConfirmPath()); err == nil {
		t.Fatal("前の回の印を消さずに変えた (古い印で戻しを飛ばす)")
	}
	if err := runSh(t, path, s.ConfirmCommand(), viaNewIP).Wait(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if got := readLog(dir); strings.Contains(got, "-setdhcp") {
		t.Errorf("確認したのに戻した: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "mac-lan-manager-staticip.lock")); err == nil {
		t.Error("終わったのに 2 本目を止める印 (lock) が残った")
	}
	if _, err := os.Stat(s.ConfirmPath()); err == nil {
		t.Error("確認の印が残った")
	}
}

// 戻す先が手で振った IP なら、その IP (とルーター) に戻す。
func TestStaticIPRestoresManual(t *testing.T) {
	dir, path := sandbox(t)
	s := staticIP(t, dir)
	s.Restore = Restore{Method: "manual", IP: "10.0.0.5", Mask: "255.0.0.0", Router: "10.0.0.1"}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := runSh(t, path, s.Script()).Wait(); err != nil {
		t.Fatal(err)
	}
	if got := readLog(dir); !strings.HasSuffix(got, "[-setmanual][Thunderbolt Ethernet][10.0.0.5][255.0.0.0][10.0.0.1]\n") {
		t.Errorf("log = %q", got)
	}
}

// サービス名は相手の networksetup の出力 (外から来た値)。引用・$()・; を含んでも 1 つの引数のまま渡り、sh に解釈されない。
func TestStaticIPServiceNameIsOneWord(t *testing.T) {
	dir, path := sandbox(t)
	s := staticIP(t, dir)
	s.Service = `Eth'er; touch "` + dir + `/pwned"; $(touch ` + dir + `/pwned2) x`
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := runSh(t, path, s.Script()).Wait(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readLog(dir), "["+s.Service+"]") {
		t.Errorf("サービス名が 1 つの引数で渡っていない: %q", readLog(dir))
	}
	for _, f := range []string{"pwned", "pwned2"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			t.Errorf("サービス名の中身を sh が実行した (%s)", f)
		}
	}
}

// ssh で打つ 1 行 (sudo -v → sudo -n -b nohup sh -c ...) も、ssh が戻った後に背景で変更と戻しまで走る。
func TestRemoteCommandRunsDetached(t *testing.T) {
	dir, path := sandbox(t)
	s := staticIP(t, dir)
	if err := runSh(t, path, s.RemoteCommand()).Wait(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "背景で戻しまで走る", func() bool { return strings.Contains(readLog(dir), "-setdhcp") })
	// nohup は ssh が切れたときの SIGHUP で止まらないためのもの。SIGHUP は本物の端末が切れないと届かないので、ここでは
	// 配線だけを固定する (挙動は段 4 の前の人手の確認: ssh が切れても変更が最後まで走るか。issue 747)
	if !strings.Contains(s.RemoteCommand(), "sudo -n -b nohup sh -c ") {
		t.Errorf("背景に回す側が nohup を通っていない: %s", s.RemoteCommand())
	}
}

// 埋め込む値の検査: 制御文字のサービス名・IPv4 でない値・知らない戻し方・使えない印の名前と置き場所・短すぎる待ちを拒む。
func TestStaticIPValidate(t *testing.T) {
	base := staticIP(t, "/var/tmp")
	for name, mut := range map[string]func(*StaticIP){
		"改行入りのサービス名": func(s *StaticIP) { s.Service = "a\nb" },
		"空のサービス名":    func(s *StaticIP) { s.Service = "" },
		"IPv6":       func(s *StaticIP) { s.IP = "fe80::1" },
		"IP でない":     func(s *StaticIP) { s.Mask = "255.255.255" },
		"戻し方":        func(s *StaticIP) { s.Restore.Method = "BOOTP" },
		"戻す IP":      func(s *StaticIP) { s.Restore = Restore{Method: "manual", IP: "x", Mask: "255.0.0.0"} },
		"印の名前":       func(s *StaticIP) { s.Token = "a b" },
		"相対の置き場所":    func(s *StaticIP) { s.MarkDir = "tmp" },
		"待ちが短い":      func(s *StaticIP) { s.Window = 0 },
		"秒でない待ち":     func(s *StaticIP) { s.Delay = 900 * time.Millisecond },
		"持ち主":        func(s *StaticIP) { s.Owner = "a b" },
	} {
		s := base
		mut(&s)
		if s.Validate() == nil {
			t.Errorf("%s を通した", name)
		}
	}
	if err := base.Validate(); err != nil {
		t.Errorf("正しい値を拒んだ: %v", err)
	}
}

func TestSuggestIP(t *testing.T) {
	for _, c := range []struct {
		band string
		used []string
		want string
	}{
		{"192.168.100.0/24", nil, "192.168.100.10"},
		{"192.168.100.0/24", []string{"192.168.100.10", "192.168.100.11", "10.0.0.1"}, "192.168.100.12"},
		{"192.168.100.77/24", nil, "192.168.100.10"}, // 帯は頭へ揃える
	} {
		if got, err := SuggestIP(c.band, c.used); err != nil || got != c.want {
			t.Errorf("SuggestIP(%s, %v) = %q, %v; want %s", c.band, c.used, got, err, c.want)
		}
	}
	if _, err := SuggestIP("192.168.100.0/28", []string{"192.168.100.10", "192.168.100.11", "192.168.100.12", "192.168.100.13", "192.168.100.14"}); err == nil {
		t.Error("空きが無いのに番号を返した (.15 はブロードキャスト)")
	}
	for _, bad := range []string{"x", "fe80::/64", "10.0.0.0/8", "192.168.1.0/31"} {
		if _, err := SuggestIP(bad, nil); err == nil {
			t.Errorf("使えない帯 %q を通した", bad)
		}
	}
}

// 手順は観測から組み直す: 済んだものは Done、前が済まない手順は理由つきで止める。
func TestPlan(t *testing.T) {
	band := "192.168.100.0/24"
	ready := Observed{Band: band, SelfService: "Thunderbolt Ethernet", SelfIP: "192.168.100.2", SelfMethod: "manual",
		Peer: PeerObs{Host: "mini.local", WifiIP: "192.168.50.7", LanIP: "169.254.1.1", SSHOpen: true, KeyOK: true, NetKnown: true,
			Service: "Thunderbolt Ethernet", IP: "169.254.1.1", Method: "DHCP"}}
	byID := func(ss []Step) map[StepID]Step {
		m := map[StepID]Step{}
		for _, s := range ss {
			m[s.ID] = s
		}
		return m
	}
	s := byID(Plan(ready))
	if !s[StepSelfStatic].Done || !s[StepKey].Done || s[StepPeerStatic].Done || s[StepPeerStatic].Blocked != "" || s[StepPeerStatic].Via != "192.168.50.7" {
		t.Errorf("相手を固定する手前: %+v", s)
	}

	o := ready
	o.SelfMethod, o.SelfIP = "DHCP", "169.254.13.42"
	if s := byID(Plan(o)); s[StepSelfStatic].Done || s[StepPeerStatic].Blocked == "" {
		t.Errorf("この Mac が未固定なのに相手を固定へ進める: %+v", s)
	}
	o = ready
	o.Peer.KeyOK, o.Peer.SSHOpen = false, false
	if s := byID(Plan(o)); s[StepKey].Blocked == "" || s[StepPeerStatic].Blocked == "" {
		t.Errorf("ssh が閉じているのに鍵 / 固定へ進める: %+v", s)
	}
	o = ready
	o.Peer.WifiIP = ""
	if s := byID(Plan(o)); s[StepPeerStatic].Via != "169.254.1.1" {
		t.Errorf("Wi-Fi で入れないときは有線から: %+v", s[StepPeerStatic])
	}
	o = ready
	o.Peer.Method, o.Peer.IP, o.Peer.Managed, o.Peer.LedgerLAN = "manual", "192.168.100.11", true, "192.168.100.11"
	for _, st := range Plan(o) {
		if !st.Done {
			t.Errorf("全部済んでいるのに %s が残る", st.ID)
		}
	}
	o.Peer.LedgerLAN = "192.168.100.99" // 台帳の内部 IP が古い
	if s := byID(Plan(o)); s[StepLedger].Done {
		t.Error("台帳の内部 IP が今の IP と違うのに済み扱い")
	}
	o = ready
	o.Band = ""
	if s := byID(Plan(o)); !strings.Contains(s[StepSelfStatic].Blocked, "帯") {
		t.Errorf("帯が無いのに、帯が無いと言わない: %+v", s[StepSelfStatic])
	}
	o = ready
	o.Peer.Method, o.Peer.IP = "manual", ready.SelfIP // この Mac と同じ IP
	if s := byID(Plan(o)); s[StepPeerStatic].Done || !strings.Contains(s[StepPeerStatic].Blocked, "同じ") {
		t.Errorf("この Mac と同じ IP を済みにした: %+v", s[StepPeerStatic])
	}
	o = ready
	o.Peer.LedgerLAN = "192.168.100.99"
	if s := byID(Plan(o)); s[StepLedger].Done {
		t.Error("台帳の内部 IP が今の IP と違うのに済み扱い")
	}
}

// Quote は sh の 1 つの語に戻る (引用・空白・$ ・改行を含む値でも)。
func TestQuote(t *testing.T) {
	for _, v := range []string{"plain", "with space", `it's`, `$(echo x) ; "q"`, "a\nb", ""} {
		out, err := exec.Command("/bin/sh", "-c", "printf %s "+Quote(v)).Output()
		if err != nil || string(out) != v {
			t.Errorf("Quote(%q) を sh に通すと %q (%v)", v, out, err)
		}
	}
}

// 確認の印は「新しい IP に届いた ssh」からのものだけを認める。Wi-Fi 越しの既存の接続から打っても、有線を確かめたことにならないので戻す
// (fable の敵対レビュー P1)。印の場所にディレクトリを置かれても戻しは飛ばない。
func TestStaticIPConfirmMustComeViaNewIP(t *testing.T) {
	for name, prep := range map[string]func(s StaticIP, path string) *exec.Cmd{
		"Wi-Fi 越しの確認": func(s StaticIP, path string) *exec.Cmd { return runSh(t, path, s.ConfirmCommand(), viaWifi) },
		// 持ち主が違う印 (相手の Mac の他のユーザーが置いた形。ここでは「期待する持ち主」を別の名前にして作る)
		"持ち主の違う印": func(s StaticIP, path string) *exec.Cmd { return runSh(t, path, s.ConfirmCommand(), viaNewIP) },
		"置かれたディレクトリ": func(s StaticIP, path string) *exec.Cmd {
			return runSh(t, path, "mkdir "+Quote(s.ConfirmPath()))
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir, path := sandbox(t)
			s := staticIP(t, dir)
			s.Window = 2 * time.Second
			if name == "持ち主の違う印" {
				s.Owner = "someone-else"
			}
			cmd := runSh(t, path, s.Script())
			waitFor(t, "固定 IP にする", func() bool { return strings.Contains(readLog(dir), "-setmanual") })
			if err := prep(s, path).Wait(); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Wait(); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(readLog(dir), "-setdhcp") {
				t.Errorf("新しい IP からの確認でないのに戻さなかった: %q", readLog(dir))
			}
		})
	}
}

// 2 本目は 1 本目が終わるまで何もせずに抜ける (古い方の期限切れが新しい方の確定済みの変更を戻した。fable の敵対レビュー P2)。
func TestStaticIPRefusesConcurrentRun(t *testing.T) {
	dir, path := sandbox(t)
	first := staticIP(t, dir)
	first.Window = 3 * time.Second
	cmd := runSh(t, path, first.Script())
	waitFor(t, "1 本目が固定 IP にする", func() bool { return strings.Contains(readLog(dir), "-setmanual") })
	second := staticIP(t, dir)
	second.IP, second.Token = "192.168.100.12", "test-token-2"
	if err := runSh(t, path, second.Script()).Wait(); err == nil {
		t.Error("1 本目が走っている間に 2 本目が走った")
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readLog(dir), "192.168.100.12") {
		t.Errorf("2 本目が変更した: %q", readLog(dir))
	}
}
