package setup

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// sandbox は偽の networksetup / sudo / nohup だけが見える PATH を作る。偽の networksetup は引数を 1 つずつ [..] で囲んで log に書く。
// 偽の sudo: -v は認証だけ / -n -b <cmd> は背景で起こして戻る / -n <cmd> と <cmd> (確認) は前面で起こす。
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
		"sudo": `if [ "$1" = -v ]; then exit 0; fi
if [ "$1" = -n ]; then shift; fi
if [ "$1" = -b ]; then shift; "$@" & exit 0; fi
exec "$@"`,
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
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // テストが止めるときに子 (sleep) ごと止められるように
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

// staticIP の状態ディレクトリは sandbox の下 (テストの中では「root のディレクトリ」の役を自分が持つ)。印を見る周期を縮め、
// 確認の来ない経路の待ちを短くする (本番の 1 秒刻みだと setup のテストが約 40 秒かかった)。
func staticIP(t *testing.T, dir string) StaticIP {
	t.Helper()
	return StaticIP{Service: "Thunderbolt Ethernet", IP: "192.168.100.11", Mask: "255.255.255.0",
		Restore: Restore{Method: "DHCP"}, Token: "test-token-1", StateDir: dir + "/state", StateOwner: me(t), Tick: 50 * time.Millisecond, Window: 300 * time.Millisecond}
}

// viaNewIP は新しい IP (192.168.100.11) に届いた ssh の $SSH_CONNECTION。viaWifi は Wi-Fi の IP に届いた既存の接続。
const (
	viaNewIP = "SSH_CONNECTION=192.168.100.2 50000 192.168.100.11 22"
	viaWifi  = "SSH_CONNECTION=192.168.50.250 50000 192.168.50.7 22"
)

const wantSet = "[-setmanual][Thunderbolt Ethernet][192.168.100.11][255.255.255.0]\n"

// 確認の印が来なければ、固定 IP にした後で元 (DHCP) に戻し、lock を外す (相手を置き去りにしない)。
func TestStaticIPRollsBackWithoutConfirm(t *testing.T) {
	dir, path := sandbox(t)
	s := staticIP(t, dir)
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := runSh(t, path, s.Script()).Wait(); err != nil {
		t.Fatal(err)
	}
	if got, want := readLog(dir), wantSet+"[-setdhcp][Thunderbolt Ethernet]\n"; got != want {
		t.Errorf("log = %q, want %q", got, want)
	}
	if _, err := os.Stat(s.lockPath()); err == nil {
		t.Error("終わったのに lock が残った")
	}
	if fi, err := os.Stat(s.StateDir); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("状態ディレクトリが 0700 でない: %v %v", fi.Mode(), err)
	}
}

// 新しい IP に届いた ssh から確認したら戻さない。前の回の印が残っていても、それでは戻しを飛ばさない。
func TestStaticIPKeepsWhenConfirmed(t *testing.T) {
	dir, path := sandbox(t)
	s := staticIP(t, dir)
	s.Window = 2 * time.Second // 確認で抜けるので、長くても待たない
	if err := os.MkdirAll(s.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.ConfirmPath(), []byte("192.168.100.2 50000 192.168.100.11 22\n"), 0o600); err != nil { // 前の回の印
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
	if got := readLog(dir); got != wantSet {
		t.Errorf("確認したのに戻した: %q", got)
	}
	if _, err := os.Stat(s.lockPath()); err == nil {
		t.Error("終わったのに lock が残った")
	}
}

// 新しい IP 以外に届いた接続 (Wi-Fi 越しの既存の接続) からの確認は認めない (有線を確かめたことにならない。fable の敵対レビュー P1)。
// 印の中身が glob でも展開しない (set -f)。
func TestStaticIPConfirmMustComeViaNewIP(t *testing.T) {
	for name, env := range map[string]string{
		"Wi-Fi 越しの確認": viaWifi,
		"glob の中身":    "SSH_CONNECTION=a b *",
	} {
		t.Run(name, func(t *testing.T) {
			dir, path := sandbox(t)
			s := staticIP(t, dir)
			s.Window = 500 * time.Millisecond
			if name == "glob の中身" { // 作業ディレクトリに新しい IP の名前のファイルがあっても、それに化けない
				if err := os.WriteFile(filepath.Join(dir, s.IP), nil, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cmd := runSh(t, path, "cd "+Quote(dir)+" && "+s.Script())
			waitFor(t, "固定 IP にする", func() bool { return strings.Contains(readLog(dir), "-setmanual") })
			if err := runSh(t, path, s.ConfirmCommand(), env).Wait(); err == nil {
				t.Error("受け取られなかった確認が 0 で戻った")
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

// 走っている間の 2 本目は前面で理由を出して断り (背景で黙って抜けると、変えたはずが何も変わらないのが見えない)、何もしない。
// 持ち主の死んだ lock (待ちの途中の電源断・kill -9) は引き取る (fable の敵対レビュー)。
func TestStaticIPLock(t *testing.T) {
	dir, path := sandbox(t)
	first := staticIP(t, dir)
	first.Window = time.Second // 2 本目の前面の確かめが走る間、1 本目が lock を持っている
	cmd := runSh(t, path, first.Script())
	waitFor(t, "1 本目が固定 IP にする", func() bool { return strings.Contains(readLog(dir), "-setmanual") })
	second := staticIP(t, dir)
	second.IP, second.Token = "192.168.100.12", "test-token-2"
	pre := exec.Command("/bin/sh", "-c", second.RemoteCommand())
	pre.Env = []string{"PATH=" + path}
	out, err := pre.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "走っている") {
		t.Errorf("走っている間の 2 本目を前面で断らない: err=%v out=%q", err, out)
	}
	// 前面の確かめをすり抜けた 2 本目 (確かめと lock の間に先を越された) の本体は、lock を取れずに抜け、走り出した印を書かない
	// (書くと前面が待ちを抜けて成功に見える。印は lock の後)
	if err := runSh(t, path, second.Script()).Wait(); err == nil {
		t.Errorf("lock を持たれているのに 2 本目の本体が成功した")
	}
	if _, err := os.Stat(second.startedPath()); !os.IsNotExist(err) {
		t.Errorf("lock を取れなかった背景が走り出した印を書いた: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readLog(dir), "192.168.100.12") {
		t.Errorf("2 本目が変更した: %q", readLog(dir))
	}

	// 死んだ lock: 持ち主の pid が居ない
	if err := os.MkdirAll(second.lockPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second.lockPath()+"/pid", []byte("999999\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runSh(t, path, second.Script()).Wait(); err != nil {
		t.Fatalf("死んだ lock を引き取らない: %v", err)
	}
	if !strings.Contains(readLog(dir), "192.168.100.12") {
		t.Errorf("死んだ lock の後に変更しない: %q", readLog(dir))
	}
}

// 状態ディレクトリが symlink・持ち主違いなら使わない (他の誰かが先に作った場所に印や lock を置かない)。
func TestStaticIPRefusesForeignStateDir(t *testing.T) {
	dir, path := sandbox(t)
	s := staticIP(t, dir)
	if err := os.Symlink(dir, s.StateDir); err != nil {
		t.Fatal(err)
	}
	if err := runSh(t, path, s.Script()).Wait(); err == nil || readLog(dir) != "" {
		t.Errorf("symlink の状態ディレクトリを使った: err=%v log=%q", err, readLog(dir))
	}
	dir2, path2 := sandbox(t)
	s2 := staticIP(t, dir2)
	s2.StateOwner = "someone-else"
	if err := runSh(t, path2, s2.Script()).Wait(); err == nil || readLog(dir2) != "" {
		t.Errorf("持ち主の違う状態ディレクトリを使った: err=%v log=%q", err, readLog(dir2))
	}
}

// 待ちの途中で止められたら (SIGTERM。相手の shutdown は TERM を配る)、戻してから終わる (新しい IP のまま残さない)。
func TestStaticIPRollsBackOnTerm(t *testing.T) {
	dir, path := sandbox(t)
	s := staticIP(t, dir)
	s.Window = 30 * time.Second
	cmd := runSh(t, path, s.Script())
	waitFor(t, "固定 IP にする", func() bool { return strings.Contains(readLog(dir), "-setmanual") })
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	waitFor(t, "戻す", func() bool { return strings.Contains(readLog(dir), "-setdhcp") })
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) // 残った sleep (自分が起こしたグループだけ)
	if _, err := os.Stat(s.lockPath()); err == nil {
		t.Error("止められた後に lock が残った")
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

// ssh で打つ 1 行 (sudo -v → 前面の事前確認 → sudo -n -b nohup sh -c ...) も、ssh が戻った後に背景で変更と戻しまで走る。
func TestRemoteCommandRunsDetached(t *testing.T) {
	dir, path := sandbox(t)
	s := staticIP(t, dir)
	s.Delay = time.Second // 前面が戻った時点でまだ変えていないことを見る
	if err := runSh(t, path, s.RemoteCommand()).Wait(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.startedPath()); !os.IsNotExist(err) {
		t.Errorf("走り出した印を前面が消していない: %v", err)
	}
	// 印は変更より前に書く (変更の後だと、有線越しの前面の ssh が変更で切れて、変えたのに失敗に見える)
	if strings.Contains(readLog(dir), "-setmanual") {
		t.Errorf("前面が戻る前に変更が走った (印を変更の後に書いている): %q", readLog(dir))
	}
	waitFor(t, "背景で戻しまで走る", func() bool { return strings.Contains(readLog(dir), "-setdhcp") })
	// nohup は ssh が切れたときの SIGHUP で止まらないためのもの。SIGHUP は本物の端末が切れないと届かないので、ここでは
	// 配線だけを固定する (挙動は段 4 の前の人手の確認: ssh が切れても変更が最後まで走るか。issue 747)
	if !strings.Contains(s.RemoteCommand(), "sudo -n -b nohup sh -c ") {
		t.Errorf("背景に回す側が nohup を通っていない: %s", s.RemoteCommand())
	}
}

// 背景の本体が走り出さなければ (ssh の端末が閉じた SIGHUP で nohup の前に死んだ等)、前面が理由を出して非 0 で抜ける。
// sudo -b はすぐ戻るので、印を待たないと何も変わらないまま成功に見えた (issue 789: air5 で固定 IP にならなかった)。
func TestRemoteCommandFailsWhenBackgroundDoesNotStart(t *testing.T) {
	dir, path := sandbox(t)
	// 背景の子が本体に届かずに死ぬ形を、何も走らせない nohup で作る
	if err := os.WriteFile(filepath.Join(dir, "nohup"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := staticIP(t, dir)
	s.startWait = 500 * time.Millisecond
	cmd := exec.Command("/bin/sh", "-c", s.RemoteCommand())
	cmd.Env = []string{"PATH=" + path}
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "始まらなかった") {
		t.Errorf("背景が走り出さないのに成功した: err=%v out=%q", err, out)
	}
}

// 埋め込む値の検査。
func TestStaticIPValidate(t *testing.T) {
	base := staticIP(t, "/var/db")
	for name, mut := range map[string]func(*StaticIP){
		"改行入りのサービス名":   func(s *StaticIP) { s.Service = "a\nb" },
		"空のサービス名":      func(s *StaticIP) { s.Service = "" },
		"IPv6":         func(s *StaticIP) { s.IP = "fe80::1" },
		"IP でない":       func(s *StaticIP) { s.Mask = "255.255.255" },
		"戻し方":          func(s *StaticIP) { s.Restore.Method = "BOOTP" },
		"戻す IP":        func(s *StaticIP) { s.Restore = Restore{Method: "manual", IP: "x", Mask: "255.0.0.0"} },
		"印の名前":         func(s *StaticIP) { s.Token = "a b" },
		"相対の置き場所":      func(s *StaticIP) { s.StateDir = "tmp" },
		"待ちが短い":        func(s *StaticIP) { s.Window = 0 },
		"周期で割り切れない待ち":  func(s *StaticIP) { s.Delay = 930 * time.Millisecond },
		"周期で割り切れない窓":   func(s *StaticIP) { s.Window = 330 * time.Millisecond },
		"本番の周期で秒でない待ち": func(s *StaticIP) { s.Tick, s.Window, s.Delay = 0, time.Second, 900*time.Millisecond },
		"1 秒より長い周期":    func(s *StaticIP) { s.Tick, s.Window = 2*time.Second, 4*time.Second },
		"10 ms より短い周期": func(s *StaticIP) { s.Tick, s.Window = 5*time.Millisecond, 300*time.Millisecond },
		"持ち主":          func(s *StaticIP) { s.StateOwner = "a b" },
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

// 同時に 2 本走らせても変更は 1 本だけ (背景の lock の取り合い)。止まった lock の引き取りも 1 本だけ (fable の敵対レビュー 3 周目 P2)。
func TestStaticIPConcurrentRunsChangeOnce(t *testing.T) {
	for _, stale := range []bool{false, true} {
		dir, path := sandbox(t)
		a, b := staticIP(t, dir), staticIP(t, dir)
		b.IP, b.Token = "192.168.100.12", "test-token-2"
		a.Window, b.Window = time.Second, time.Second // 2 本が重なる長さ (2 本目は 0.25 秒遅れ、止まった lock の引き取りは 0.5 秒かかる)
		if stale {
			// 「止まった」と見てから外し終えるまでの窓を広げる (本物の窓は µs 級で、自然な回数では当たらない)。2 本目は 0.25 秒遅れて
			// 始めるので、1 本目が外して作り直した lock を、2 本目が「止まった lock」として外しにいく形になる。引き取りが 1 本ずつで
			// なければ 2 本とも進む (fable の敵対レビュー 3 周目で再現した順序)
			slowRm := "#!/bin/sh\n/bin/sleep 0.5 # sleep-ok: window: 外すまでの窓を作る入力\nexec /bin/rm \"$@\"\n"
			if err := os.WriteFile(filepath.Join(dir, "rm"), []byte(slowRm), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(a.lockPath(), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(a.lockPath()+"/pid", []byte("999999\n"), 0o600); err != nil { // 居ない pid
				t.Fatal(err)
			}
		}
		for range 4 {
			if err := os.Remove(filepath.Join(dir, "log")); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			ca := runSh(t, path, a.Script())
			if stale {
				time.Sleep(250 * time.Millisecond) // sleep-ok: window: 2 本目をずらして始める (上の窓に入れる)
			}
			cb := runSh(t, path, b.Script())
			_ = ca.Wait() // 負けた側は rc=1 で抜ける
			_ = cb.Wait()
			if n := strings.Count(readLog(dir), "-setmanual][Thunderbolt Ethernet][192.168.100.1"); n != 1 {
				t.Fatalf("stale=%v: 同時の 2 本で変更が %d 回: %q", stale, n, readLog(dir))
			}
			if stale { // 次の回も「止まった lock」から始める
				_ = os.MkdirAll(a.lockPath(), 0o700)
				_ = os.WriteFile(a.lockPath()+"/pid", []byte("999999\n"), 0o600)
			}
		}
	}
}

// pid が生きていても、起動時刻が lock に書いた時刻と違えば別のプロセス (再起動の後に同じ番号が使われた) なので引き取る。
// 持ち主が本当に生きているときは断り、外し方を出す。
func TestStaticIPLockPidReuse(t *testing.T) {
	dir, path := sandbox(t)
	s := staticIP(t, dir)
	if err := os.MkdirAll(s.lockPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	pid := []byte(strconv.Itoa(os.Getpid()) + "\n") // 生きている (このテスト自身)
	if err := os.WriteFile(s.lockPath()+"/pid", pid, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.lockPath()+"/start", []byte("Thu Jan  1 00:00:00 1970\n"), 0o600); err != nil { // 違う起動時刻
		t.Fatal(err)
	}
	if err := runSh(t, path, s.Script()).Wait(); err != nil || !strings.Contains(readLog(dir), "-setmanual") {
		t.Fatalf("再利用された pid の lock を引き取らない: err=%v log=%q", err, readLog(dir))
	}

	// 本当に生きている持ち主 (起動時刻も合う) なら断り、外し方を出す
	if err := os.MkdirAll(s.lockPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	ps := exec.Command("/bin/ps", "-o", "lstart=", "-p", strconv.Itoa(os.Getpid()))
	ps.Env = []string{"LC_ALL=C"} // Script と同じ書式 (ps の起動時刻はロケールで書式が変わる)
	st, err := ps.Output()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(s.lockPath()+"/pid", pid, 0o600)
	_ = os.WriteFile(s.lockPath()+"/start", st, 0o600)
	cmd := exec.Command("/bin/sh", "-c", s.Script())
	cmd.Env = []string{"PATH=" + path, "LC_ALL=C"}
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "rm -rf") {
		t.Errorf("生きている持ち主の lock を断らない / 外し方を出さない: err=%v out=%q", err, out)
	}
}

// 相手の待ちが終わった後 (期限切れで戻した後) の確認は、印を書けても非 0 で戻り、印を残さない
// (印は lock の外に置くので書けてしまい、0 で戻ると元に戻った相手を確認済みとして扱った。敵対レビュー)。
func TestStaticIPConfirmAfterWindowFails(t *testing.T) {
	dir, path := sandbox(t)
	s := staticIP(t, dir)
	if err := runSh(t, path, s.Script()).Wait(); err != nil {
		t.Fatal(err)
	}
	if err := runSh(t, path, s.ConfirmCommand(), viaNewIP).Wait(); err == nil {
		t.Error("待ちが終わった後の確認が 0 で戻った")
	}
	if _, err := os.Stat(s.ConfirmPath()); err == nil {
		t.Error("受け取られなかった印が残った")
	}
}

// メインの網 (IP とマスク) が帯を含むなら、メインの IP が帯の外でもこの Mac の固定を止める。
func TestPlanBlocksBandOverlappingMain(t *testing.T) {
	o := Observed{Band: "192.168.100.0/24", SelfService: "Thunderbolt Ethernet", SelfIP: "169.254.13.42", SelfMethod: "DHCP",
		SelfMainIP: "192.168.0.10", SelfMainMask: "255.255.0.0", Peer: PeerObs{WifiIP: "192.168.0.20", SSHOpen: true}}
	for _, s := range Plan(o) {
		if s.ID == StepSelfStatic && !strings.Contains(s.Blocked, "メインの網") {
			t.Errorf("帯を含むメインの網でこの Mac の固定を止めない: %+v", s)
		}
	}
}
