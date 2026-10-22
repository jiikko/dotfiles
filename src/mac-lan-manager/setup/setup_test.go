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

	"mac-lan-manager/internal/testwait"
	"mac-lan-manager/netinfo"
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
	testwait.Until(t, what, 10*time.Second, nil, cond)
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

// 新しい IP 以外に届いた接続 (Wi-Fi 越しの既存の接続) からの確認は認めない (有線を確かめたことにならない)。
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
// 持ち主の死んだ lock (待ちの途中の電源断・kill -9) は引き取る。
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

// 戻す先が手動 (ルーターだけ DHCP) / BOOTP なら、その方式へ戻す (DHCP へ倒さない)。
func TestStaticIPRestoresOtherMethods(t *testing.T) {
	for r, want := range map[Restore]string{
		{Method: netinfo.MethodManualDHCPRouter, IP: "10.0.0.5"}: "[-setmanualwithdhcprouter][Thunderbolt Ethernet][10.0.0.5]\n",
		{Method: netinfo.MethodBOOTP}:                            "[-setbootp][Thunderbolt Ethernet]\n",
	} {
		dir, path := sandbox(t)
		s := staticIP(t, dir)
		s.Restore = r
		if err := s.Validate(); err != nil {
			t.Fatal(err)
		}
		if err := runSh(t, path, s.Script()).Wait(); err != nil {
			t.Fatal(err)
		}
		if got := readLog(dir); !strings.HasSuffix(got, want) {
			t.Errorf("%s: log = %q", r.Method, got)
		}
	}
	if err := (Restore{Method: netinfo.MethodManualDHCPRouter}).Validate(); err == nil {
		t.Error("IP の分からない手動 (ルーターだけ DHCP) を戻し先にした")
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
	// 背景は戻しの後に lock を外す (finish の drop: 改名してから消す)。偽の sudo -b は背景を待たないので、ここで待たずに戻ると
	// drop の改名が TempDir の後始末の途中に入り、「directory not empty」で落ちる
	waitFor(t, "背景が lock を外し終える", func() bool {
		m, _ := filepath.Glob(s.lockPath() + "*")
		return len(m) == 0
	})
	// nohup は ssh が切れたときの SIGHUP で止まらないためのもの。SIGHUP は本物の端末が切れないと届かないので、ここでは
	// 配線だけを固定する (挙動は段 4 の前の人手の確認: ssh が切れても変更が最後まで走るか)
	if !strings.Contains(s.RemoteCommand(), "sudo -n -b nohup sh -c ") {
		t.Errorf("背景に回す側が nohup を通っていない: %s", s.RemoteCommand())
	}
}

// 背景の本体が走り出さなければ (ssh の端末が閉じた SIGHUP で nohup の前に死んだ等)、前面が理由を出して非 0 で抜ける。
// sudo -b はすぐ戻るので、印を待たないと何も変わらないまま成功に見えた (air5 で固定 IP にならなかった)。
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
		"戻し方":          func(s *StaticIP) { s.Restore.Method = "" },
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
		"双方向の制御文字の名前":  func(s *StaticIP) { s.Service = "Ethernet\u202eNAL" },
		"C1 の制御文字の名前":  func(s *StaticIP) { s.Service = "Ethernet\u009b2J" },
		"sh が展開する置き場所": func(s *StaticIP) { s.StateDir = "/var/db/$(id)" },
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

// 前の IP が無ければ、帯の中で使われていない最小の番号 (.1〜.9 を飛ばす)。
func TestPickIPLowestFree(t *testing.T) {
	for _, c := range []struct {
		band string
		used []string
		want string
	}{
		{"192.168.100.0/24", nil, "192.168.100.10"},
		{"192.168.100.0/24", []string{"192.168.100.10", "192.168.100.11", "10.0.0.1"}, "192.168.100.12"},
		{"192.168.100.77/24", nil, "192.168.100.10"}, // 帯は頭へ揃える
	} {
		if got, _, err := PickIP(c.band, c.used); err != nil || got != c.want {
			t.Errorf("PickIP(%s, %v) = %q, %v; want %s", c.band, c.used, got, err, c.want)
		}
	}
	if _, _, err := PickIP("192.168.100.0/28", []string{"192.168.100.10", "192.168.100.11", "192.168.100.12", "192.168.100.13", "192.168.100.14"}); err == nil {
		t.Error("空きが無いのに番号を返した (.15 はブロードキャスト)")
	}
	for _, bad := range []string{"x", "fe80::/64", "10.0.0.0/8", "192.168.1.0/31"} {
		if _, _, err := PickIP(bad, nil); err == nil {
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
	mdr := ready
	mdr.SelfMethod = netinfo.MethodManualDHCPRouter
	mdr.Peer.Method, mdr.Peer.IP = netinfo.MethodManualDHCPRouter, "192.168.100.12"
	if s := byID(Plan(mdr)); !s[StepSelfStatic].Done || !s[StepPeerStatic].Done {
		t.Errorf("手動 (ルーターだけ DHCP) で帯の IP を固定と見ない: %+v", s)
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
	o.SelfHost, o.SelfAdvertKnown, o.SelfAdvert = "self.local", true, NameState{Running: true, Name: "self.wired.local", IP: "192.168.100.2"}
	o.Peer.AdvertKnown, o.Peer.Advert = true, NameState{Running: true, Name: "mini.wired.local", IP: "192.168.100.11"}
	for _, st := range Plan(o) {
		if !st.Done {
			t.Errorf("全部済んでいるのに %s が残る", st.ID)
		}
	}
	// 有線の名前: 広告が古い IP・止まっている・読めないなら済みにしない (読めないは理由を出して止める)
	for name, mut := range map[string]func(*Observed){
		"相手の広告が古い IP":     func(o *Observed) { o.Peer.Advert.IP = "192.168.100.99" },
		"相手の広告が止まっている":    func(o *Observed) { o.Peer.Advert.Running = false },
		"この Mac の広告が別の名前": func(o *Observed) { o.SelfAdvert.Name = "other.wired.local" },
	} {
		x := o
		mut(&x)
		for _, st := range Plan(x) {
			if (st.ID == StepSelfName || st.ID == StepPeerName) && st.Done && (name == "この Mac の広告が別の名前") == (st.ID == StepSelfName) {
				t.Errorf("%s: %s を済みにした", name, st.ID)
			}
		}
	}
	x := o
	x.Peer.AdvertKnown = false
	if s := byID(Plan(x)); s[StepPeerName].Done || s[StepPeerName].Blocked == "" {
		t.Errorf("相手の広告を読めないのに理由を出さない: %+v", s[StepPeerName])
	}
	x = o
	x.Peer.LedgerLAN = ""
	if s := byID(Plan(x)); s[StepPeerName].Blocked == "" {
		t.Errorf("台帳に載る前に相手の名前を広告できる: %+v", s[StepPeerName])
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

// 同時に 2 本走らせても変更は 1 本だけ (背景の lock の取り合い)。止まった lock の引き取りも 1 本だけ。
func TestStaticIPConcurrentRunsChangeOnce(t *testing.T) {
	for _, stale := range []bool{false, true} {
		dir, path := sandbox(t)
		a, b := staticIP(t, dir), staticIP(t, dir)
		b.IP, b.Token = "192.168.100.12", "test-token-2"
		a.Window, b.Window = time.Second, time.Second // 2 本が重なる長さ (2 本目は 0.25 秒遅れ、止まった lock の引き取りは 0.5 秒かかる)
		if stale {
			// 「止まった」と見てから外し終えるまでの窓を広げる (本物の窓は µs 級で、自然な回数では当たらない)。2 本目は 0.25 秒遅れて
			// 始めるので、1 本目が外して作り直した lock を、2 本目が「止まった lock」として外しにいく形になる。引き取りが 1 本ずつで
			// なければ 2 本とも進む
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
			if stale { // 次の回も「止まった lock」から始める (作れないと新しい lock で走り、件数は 1 のまま引き取りの窓を見なくなる)
				if err := os.MkdirAll(a.lockPath(), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(a.lockPath()+"/pid", []byte("999999\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

// 取ったばかりの lock (mkdir の後・start を書き終える前) に来た 2 本目は、死んだ lock と読んで引き取らない (読むと 2 本とも変更へ進んだ)。
// 窓は ps 1 回分 (数十 ms) なので、1 本目が最初に打つ ps だけを遅くして窓を広げる。
func TestStaticIPFreshLockIsNotTakenOver(t *testing.T) {
	dir, path := sandbox(t)
	slowPs := "#!/bin/sh\nif mkdir \"" + dir + "/ps-once\" 2>/dev/null; then /bin/sleep 1; fi # sleep-ok: window: start を書き終える前の窓を作る入力\nexec /bin/ps \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "ps"), []byte(slowPs), 0o755); err != nil {
		t.Fatal(err)
	}
	a, b := staticIP(t, dir), staticIP(t, dir)
	b.IP, b.Token = "192.168.100.12", "test-token-2"
	ca := runSh(t, path, a.Script())
	waitFor(t, "1 本目が lock を取って start を書く手前に居る", func() bool {
		_, errLock := os.Stat(a.lockPath() + "/pid")
		_, errOnce := os.Stat(filepath.Join(dir, "ps-once"))
		return errLock == nil && errOnce == nil
	})
	cb := exec.Command("/bin/sh", "-c", b.Script())
	cb.Env = []string{"PATH=" + path}
	if out, err := cb.CombinedOutput(); err == nil || !strings.Contains(string(out), "走っている") {
		t.Errorf("取ったばかりの lock を「走っている」で断らない: err=%v out=%q", err, out)
	}
	if err := ca.Wait(); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(readLog(dir), "-setmanual][Thunderbolt Ethernet][192.168.100.1"); n != 1 {
		t.Errorf("取ったばかりの lock に 2 本目が来て変更が %d 回: %q", n, readLog(dir))
	}
}

// lock を外す途中 (pid を消した後) で止まっても、pid の無い lock を残さない (残ると「取ったばかり」と読んで以後ずっと断る)。
// 途中で止まる rm を偽物で作る: ディレクトリを渡されたら中の pid だけを消して失敗する。
func TestStaticIPInterruptedUnlockDoesNotWedge(t *testing.T) {
	dir, path := sandbox(t)
	halfRm := "#!/bin/sh\nfor a in \"$@\"; do if [ -d \"$a\" ]; then /bin/rm -f \"$a/pid\"; exit 1; fi; done\nexec /bin/rm \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "rm"), []byte(halfRm), 0o755); err != nil {
		t.Fatal(err)
	}
	first := staticIP(t, dir)
	_ = runSh(t, path, first.Script()).Wait() // 確認が来ずに戻して終わる (外す rm が途中で止まる)
	second := staticIP(t, dir)
	second.IP, second.Token = "192.168.100.12", "test-token-2"
	cmd := exec.Command("/bin/sh", "-c", second.Script())
	cmd.Env = []string{"PATH=" + path}
	if out, err := cmd.CombinedOutput(); err != nil || !strings.Contains(readLog(dir), "192.168.100.12") {
		t.Errorf("外す途中で止まった後の 2 本目が進まない: err=%v out=%q log=%q", err, out, readLog(dir))
	}
}

// 引き取りの途中で止まった takeover が残ると以後の引き取りを断る。そのとき takeover も含めた外し方を出す。
func TestStaticIPStaleTakeoverShowsRemedy(t *testing.T) {
	dir, path := sandbox(t)
	s := staticIP(t, dir)
	for _, d := range []string{s.lockPath(), s.lockPath() + ".takeover"} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(s.lockPath()+"/pid", []byte("999999\n"), 0o600); err != nil { // 居ない pid (止まった lock)
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-c", s.Script())
	cmd.Env = []string{"PATH=" + path}
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "rm -rf "+s.lockPath()+" "+s.lockPath()+".takeover") {
		t.Errorf("止まった takeover の外し方を出さない: err=%v out=%q", err, out)
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
	// 書けないと、空の pid を「生きている」と読んで断り、起動時刻の照合を通らないまま緑になる
	if err := os.WriteFile(s.lockPath()+"/pid", pid, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.lockPath()+"/start", st, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-c", s.Script())
	cmd.Env = []string{"PATH=" + path, "LC_ALL=C"}
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "rm -rf "+s.lockPath()+" "+s.lockPath()+".takeover") {
		t.Errorf("生きている持ち主の lock を断らない / takeover も含めた外し方を出さない: err=%v out=%q", err, out)
	}
}

// 相手の待ちが終わった後 (期限切れで戻した後) の確認は、印を書けても非 0 で戻り、印を残さない
// (印は lock の外に置くので書けてしまい、0 で戻ると元に戻った相手を確認済みとして扱った)。
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

// 前に振った IP が帯の中で空いていればそれを返す (アダプタを差し替えて固定し直しても IP を変えない)。使われている・帯の外・網の番号・
// ブロードキャスト・読めないなら使われていない最小の番号。
func TestPickIP(t *testing.T) {
	band := "192.168.100.0/24"
	used := []string{"192.168.100.10", "192.168.100.11", "192.168.100.13"}
	for _, c := range []struct {
		name, prev, want string
		reused           bool
	}{
		{"前の IP が読めない", "192.168.100.13x", "192.168.100.12", false},
		{"前の IP をもう一度振る", "192.168.100.12", "192.168.100.12", true},
		{"前の IP が若い番号でも使う", "192.168.100.5", "192.168.100.5", true},
		{"前の IP が大きい番号でも使う", "192.168.100.200", "192.168.100.200", true},
		{"前の IP を他の Mac が使っている", "192.168.100.13", "192.168.100.12", false},
		{"前の IP が帯の外", "192.168.50.7", "192.168.100.12", false},
		{"前の IP が網の番号", "192.168.100.0", "192.168.100.12", false},
		{"前の IP がブロードキャスト", "192.168.100.255", "192.168.100.12", false},
		{"前の IP が無い", "", "192.168.100.12", false},
	} {
		got, reused, err := PickIP(band, used, c.prev)
		if err != nil || got != c.want || reused != c.reused {
			t.Errorf("%s: PickIP(%q) = %q, %v, %v; want %s, %v", c.name, c.prev, got, reused, err, c.want, c.reused)
		}
	}
}

// 前の IP の候補は先に書いたものから試す (帯の外・使われているものは飛ばす)。使用中の判定は IPv4-mapped の形も同じ IP と読む。
func TestPickIPCandidates(t *testing.T) {
	band := "192.168.100.0/24"
	used := []string{"192.168.100.10", "::ffff:192.168.100.20"}
	if got, reused, _ := PickIP(band, used, "10.0.0.5", "192.168.100.20", "192.168.100.30"); got != "192.168.100.30" || !reused {
		t.Errorf("候補の 3 つ目 = %q, %v (1 つ目は帯の外、2 つ目は使われている)", got, reused)
	}
	if got, reused, _ := PickIP(band, used, "192.168.100.40", "192.168.100.30"); got != "192.168.100.40" || !reused {
		t.Errorf("先の候補を優先しない: %q, %v", got, reused)
	}
}
