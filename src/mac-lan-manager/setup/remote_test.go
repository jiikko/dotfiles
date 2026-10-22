package setup

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"mac-lan-manager/netinfo"
)

type exitError struct {
	code int
	msg  string
}

func (e exitError) Error() string { return e.msg }
func (e exitError) ExitCode() int { return e.code }

var dest = Dest{User: "koji", IP: "192.168.50.7", HostKeyAlias: "m2macmini.local"}

// fakeRemote は相手の Mac の偽物。ssh の引数の最後 (相手で走る 1 行) で出力を引く。netinfo の fixture を返す。
func fakeRemote(t *testing.T, key error) (Run, *[][]string) {
	t.Helper()
	fix := func(name string) string {
		b, err := os.ReadFile(filepath.Join("..", "netinfo", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	out := map[string]string{
		"'networksetup' '-listnetworkserviceorder'":        fix("serviceorder.txt"),
		"'networksetup' '-getinfo' 'Thunderbolt Ethernet'": fix("getinfo_tb_ethernet.txt"),
		"'networksetup' '-getinfo' 'Wi-Fi'":                fix("getinfo_wifi.txt"),
		"'ifconfig' 'en13'":                                fix("ifconfig_en13.txt"),
		"'ifconfig' 'en0'":                                 fix("ifconfig_en0.txt"),
		// 残りのサービスも本物の返り方で答える (macOS 27.0.1 で実測: 抜いたアダプタは ifconfig が "does not exist" で rc=1、-getinfo は読める)
		"'ifconfig' 'bridge0'":                               "bridge0: flags=8822<BROADCAST,SMART,SIMPLEX,MULTICAST> mtu 1500\n\tstatus: inactive\n",
		"'networksetup' '-getinfo' 'Thunderbolt Bridge'":     "DHCP Configuration\nClient ID: \nIPv6: Automatic\n",
		"'networksetup' '-getinfo' 'USB 10/100/1G/2.5G LAN'": "DHCP Configuration\nClient ID: \nIPv6: Automatic\nEthernet Address: (null)\n",
		"'networksetup' '-getinfo' 'iPhone USB'":             "DHCP Configuration\nClient ID: \nIPv6: Automatic\nEthernet Address: (null)\n",
	}
	gone := map[string]bool{"'ifconfig' 'en10'": true, "'ifconfig' 'en11'": true}
	var calls [][]string
	return func(_ context.Context, argv []string) (string, error) {
		calls = append(calls, argv)
		remote := argv[len(argv)-1]
		if remote == "true" {
			return "", key
		}
		if o, ok := out[remote]; ok {
			return o, nil
		}
		if gone[remote] {
			return "", exitError{1, "exit status 1: ifconfig: interface " + strings.Trim(remote[len("'ifconfig' "):], "'") + " does not exist"}
		}
		return "", exitError{1, "no fake: " + remote}
	}, &calls
}

// 鍵で入れる相手は、ssh 越しに netinfo.Read を走らせて、ルーターの無い有線のサービスを読む。相手で走る 1 行は語ごとに引用する
// (サービス名の空白で語が割れない)。どの ssh も宛先の前に -- を置き、鍵だけで入る (パスワードを聞かない)。
func TestObservePeerReadsNetwork(t *testing.T) {
	run, calls := fakeRemote(t, nil)
	p := ObservePeer(context.Background(), run, dest)
	if !p.KeyOK || p.KeyErr != nil || !p.NetKnown {
		t.Fatalf("PeerNet = %+v", p)
	}
	if p.Service != "Thunderbolt Ethernet" || p.Method != "DHCP" || p.IP != "169.254.13.42" || p.Mask != "255.255.0.0" {
		t.Errorf("有線のサービス = %+v", p)
	}
	// 全部のサービスも残す (固定し直すときに、前の IP を同じ相手の別のサービスが持っていないかを見る)
	if !slices.ContainsFunc(p.Services, func(sv netinfo.Service) bool { return sv.Name == "Thunderbolt Ethernet" }) || len(p.Services) < 2 {
		t.Errorf("Services = %+v", p.Services)
	}
	for _, argv := range *calls {
		i := slices.Index(argv, "--")
		if i < 0 || argv[i+1] != "koji@192.168.50.7" || !slices.Contains(argv, "BatchMode=yes") || slices.Contains(argv, "-t") {
			t.Errorf("観測の ssh の形: %q", argv)
		}
	}
}

// ssh の失敗は、stderr の Permission denied のときだけ「鍵で入れない」(判定できた)。届かない (255 で別の理由)・255 以外・期限切れは
// 判定できない (ErrUnknown)。届かない相手を「鍵が無い」に丸めない。
func TestObservePeerKeyOutcomes(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		key     bool
		unknown bool
	}{
		{"鍵で入れない", exitError{255, "koji@192.168.50.7: Permission denied (publickey,password)."}, false, false},
		{"届かない", exitError{255, "ssh: connect to host 192.168.50.7 port 22: Operation timed out"}, false, true},
		{"接続の EPERM", exitError{255, "ssh: connect to host 192.168.50.7 port 22: Permission denied"}, false, true},
		{"255 以外", exitError{1, "boom"}, false, true},
		{"終了コードの無い失敗", errors.New("exec: not found"), false, true},
	}
	for _, c := range cases {
		run, _ := fakeRemote(t, c.err)
		p := ObservePeer(context.Background(), run, dest)
		if p.KeyOK != c.key || errors.Is(p.KeyErr, ErrUnknown) != c.unknown || p.NetKnown {
			t.Errorf("%s: PeerNet = %+v", c.name, p)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	run, _ := fakeRemote(t, exitError{255, "Permission denied (publickey)."})
	if p := ObservePeer(ctx, run, dest); !errors.Is(p.KeyErr, ErrUnknown) {
		t.Errorf("期限切れの ssh を「鍵で入れない」と読んだ: %+v", p)
	}
}

// ssh の引数に入れる値を確かめる (台帳を手で書き換えて user が "-o…" になっていても、ssh の option にしない)。
func TestDestValidate(t *testing.T) {
	for _, d := range []Dest{
		{User: "-oProxyCommand=x", IP: "192.168.50.7", HostKeyAlias: "a.local"},
		{User: "koji", IP: "192.168.50.7; id", HostKeyAlias: "a.local"},
		{User: "koji", IP: "fe80::1", HostKeyAlias: "a.local"},
		{User: "koji", IP: "192.168.50.7", HostKeyAlias: "a b"},
		{User: "koji", IP: "192.168.50.7", HostKeyAlias: ""},
		{User: "koji", IP: "192.168.50.7", HostKeyAlias: "a.local,b.local"}, // known_hosts では 2 つの名前
		{User: "koji", IP: "192.168.50.7", HostKeyAlias: "*.local"},         // known_hosts ではパターン
		{User: "koji", IP: "192.168.50.7", HostKeyAlias: `a"b.local`},       // ssh の -o の値では引用
	} {
		if d.Validate() == nil {
			t.Errorf("使えない宛先を通した: %+v", d)
		}
		run, calls := fakeRemote(t, nil)
		if p := ObservePeer(context.Background(), run, d); !errors.Is(p.KeyErr, ErrUnknown) || len(*calls) != 0 {
			t.Errorf("使えない宛先に ssh した: %+v %q", p, *calls)
		}
	}
}

// 本番の値 (周期は既定の 1 秒) で組む待ちの形を固定する (テストは周期を縮めて走らせるので、既定の周期が変わっても他のテストは緑のまま)。
func TestPeerStaticProductionTiming(t *testing.T) {
	s, err := PeerStatic(PeerNet{Service: "Thunderbolt Ethernet", Method: "DHCP"}, "192.168.100.12", "255.255.255.0", "tok-12345678")
	if err != nil {
		t.Fatal(err)
	}
	sc := s.Script()
	for _, want := range []string{"\nsleep 5\n", `-lt 180 ]`, "  sleep 1\n"} {
		if !strings.Contains(sc, want) {
			t.Errorf("Script に %q が無い (確認を待つのは 5 秒の後の 180 秒、1 秒ごと)", want)
		}
	}
	if c := s.ConfirmCommand(); !strings.Contains(c, "sleep 1;") || !strings.HasSuffix(c, " 3") {
		t.Errorf("ConfirmCommand が 1 秒ごと 3 周で待たない: %s", c)
	}
}

// 各手順のコマンド: 相手を固定 IP にする ssh は端末を明け渡し (-t)、確認は新しい IP で入る。戻す先は今の設定 (DHCP / 手動の値)。
func TestStepArgv(t *testing.T) {
	p := PeerNet{KeyOK: true, NetKnown: true, Service: "Thunderbolt Ethernet", Method: "DHCP"}
	s, err := PeerStatic(p, "192.168.100.12", "255.255.255.0", "tok-12345678")
	if err != nil || s.Restore.Method != "DHCP" || s.Window != PeerWindow {
		t.Fatalf("PeerStatic = %+v %v", s, err)
	}
	argv, err := PeerStaticArgv(dest, s)
	if err != nil || !slices.Contains(argv, "-t") || argv[len(argv)-1] != s.RemoteCommand() || argv[slices.Index(argv, "--")+1] != "koji@192.168.50.7" {
		t.Errorf("PeerStaticArgv = %q %v", argv, err)
	}
	if _, err := ConfirmArgv(dest, s); err == nil {
		t.Error("古い IP で確認しようとした")
	}
	nd := dest
	nd.IP = s.IP
	if argv, err := ConfirmArgv(nd, s); err != nil || argv[len(argv)-1] != s.ConfirmCommand() {
		t.Errorf("ConfirmArgv = %q %v", argv, err)
	}
	manual := PeerNet{Service: "TB", Method: "manual", IP: "10.0.0.5", Mask: "255.255.255.0", Router: ""}
	if s, err := PeerStatic(manual, "192.168.100.12", "255.255.255.0", "tok-12345678"); err != nil || s.Restore != (Restore{Method: "manual", IP: "10.0.0.5", Mask: "255.255.255.0"}) {
		t.Errorf("手動の戻し先 = %+v %v", s.Restore, err)
	}
	if _, err := PeerStatic(PeerNet{Service: "TB", Method: "manual", IP: "10.0.0.5"}, "192.168.100.12", "255.255.255.0", "tok-12345678"); err == nil {
		t.Error("マスクの分からない手動の設定を戻し先にした")
	}
	argv, err = KeyArgv(dest)
	if err != nil || argv[0] != "sh" || !strings.Contains(argv[2], "ssh-copy-id") || !strings.Contains(argv[2], "'koji@192.168.50.7'") ||
		!strings.Contains(argv[2], "'HostKeyAlias=m2macmini.local'") {
		t.Errorf("KeyArgv = %q %v", argv, err)
	}
	if argv, err := SelfStaticArgv("Thunderbolt Ethernet", "192.168.100.11", "255.255.255.0"); err != nil || !slices.Equal(argv, []string{"sudo", "networksetup", "-setmanual", "Thunderbolt Ethernet", "192.168.100.11", "255.255.255.0"}) {
		t.Errorf("SelfStaticArgv = %q %v", argv, err)
	}
	if _, err := SelfStaticArgv("TB\nx", "192.168.100.11", "255.255.255.0"); err == nil {
		t.Error("改行を含むサービス名を通した")
	}
}

// 初めての相手 (ssh が照合に使う known_hosts のどれにも台帳の鍵の名前が無い) は、BatchMode の ssh が "Host key verification failed." で
// 落ちても「判定できない」にしない (鍵を置く手順の ssh-copy-id で人が指紋を確かめる)。どれかにあるのに落ちたら鍵が変わったので断る (air5 で実測した形)。
// 照合先は ssh -G で読み、無いファイルは行が無いと数える (known_hosts の無い Mac で ssh-keygen -F が 255 になった)。
func TestObservePeerHostKey(t *testing.T) {
	hkFail := exitError{255, "exit status 255: Host key verification failed."}
	const cfg = "hostkeyalias m2macmini.local\nglobalknownhostsfile /etc/ssh/ssh_known_hosts\nuserknownhostsfile /u/.ssh/known_hosts /u/.ssh/known_hosts2\n"
	for _, c := range []struct {
		name    string
		exists  map[string]bool
		keygen  map[string]error // ファイルごとの ssh-keygen -F の結果 (nil は見つかった)
		first   bool
		unknown bool
	}{
		{"known_hosts が 1 つも無い", nil, nil, true, false},
		{"どこにも行が無い", map[string]bool{"/u/.ssh/known_hosts": true}, map[string]error{"/u/.ssh/known_hosts": exitError{1, "exit status 1"}}, true, false},
		{"known_hosts2 で鍵が変わった", map[string]bool{"/u/.ssh/known_hosts": true, "/u/.ssh/known_hosts2": true},
			map[string]error{"/u/.ssh/known_hosts": exitError{1, "exit status 1"}, "/u/.ssh/known_hosts2": nil}, false, true},
		{"global で鍵が変わった", map[string]bool{"/etc/ssh/ssh_known_hosts": true}, map[string]error{"/etc/ssh/ssh_known_hosts": nil}, false, true},
		{"known_hosts を読めない", map[string]bool{"/u/.ssh/known_hosts": true}, map[string]error{"/u/.ssh/known_hosts": exitError{255, "boom"}}, false, true},
		{"ssh -G が失敗する", nil, nil, false, true},
		{"照合先が 0 件", nil, nil, false, true},
		{"test を確かめられない", nil, nil, false, true},
	} {
		var keygenAliases []string
		run := func(_ context.Context, argv []string) (string, error) {
			switch {
			case argv[0] == "ssh" && argv[1] == "-G":
				if !slices.Contains(argv, "HostKeyAlias=m2macmini.local") {
					t.Errorf("%s: ssh -G に台帳の鍵の名前を渡さない: %q", c.name, argv)
				}
				switch c.name {
				case "ssh -G が失敗する":
					return "", exitError{255, "bad config"}
				case "照合先が 0 件":
					return "hostkeyalias m2macmini.local\n", nil
				}
				return cfg, nil
			case argv[0] == "test":
				if c.name == "test を確かめられない" {
					return "", exitError{-1, "signal: killed"}
				}
				if c.exists[argv[2]] {
					return "", nil
				}
				return "", exitError{1, "exit status 1"}
			case argv[0] == "ssh-keygen":
				keygenAliases = append(keygenAliases, argv[2])
				if !c.exists[argv[4]] {
					t.Errorf("%s: 無いファイルを ssh-keygen に渡した: %q", c.name, argv)
				}
				return "", c.keygen[argv[4]]
			}
			return "", hkFail
		}
		p := ObservePeer(context.Background(), run, dest)
		if p.HostKeyUnknown != c.first || errors.Is(p.KeyErr, ErrUnknown) != c.unknown || p.KeyOK {
			t.Errorf("%s: PeerNet = %+v", c.name, p)
		}
		for _, a := range keygenAliases {
			if a != "m2macmini.local" {
				t.Errorf("%s: known_hosts を台帳の鍵の名前で引かない: %q", c.name, a)
			}
		}
	}
}

// サービスを読み終えた後、I/F の IP の途中で期限が来ても、相手の有線は読めたものとして返す (相手の判定はサービスだけで決まる)。
func TestObservePeerDoesNotReadIPs(t *testing.T) {
	run, calls := fakeRemote(t, nil)
	p := ObservePeer(context.Background(), run, dest)
	if !p.NetKnown || p.Service != "Thunderbolt Ethernet" {
		t.Fatalf("前提: 相手の有線を読めない: %+v", p)
	}
	for _, c := range *calls {
		if c[len(c)-1] == "'ifconfig' '-l'" {
			t.Errorf("使わない I/F 全部の IP を ssh で集めた: %q", c)
		}
	}
}

// 期限切れでもないのに読めなかったサービスがあれば、相手のネットワークは「判定できない」にする (そのサービスが Offline から黙って外れ、
// 相手に有線が無いと言い切る)。抜いたアダプタ (ifconfig が "does not exist") は普段から読めないので数えない (TestObservePeerReadsNetwork)
func TestObservePeerUnknownWhenServiceUnread(t *testing.T) {
	for _, broken := range []string{"'ifconfig' 'en13'", "'networksetup' '-getinfo' 'Thunderbolt Ethernet'"} {
		base, _ := fakeRemote(t, nil)
		run := func(c context.Context, argv []string) (string, error) {
			if argv[len(argv)-1] == broken {
				return "", exitError{255, "exit status 255: mux_client_request_session: read from master failed"}
			}
			return base(c, argv)
		}
		p := ObservePeer(context.Background(), run, dest)
		if p.NetKnown {
			t.Errorf("%s を読めなかったのに相手のネットワークを読めたことにした: %+v", broken, p)
		}
		if p.NetErr != "読めないサービス: Thunderbolt Ethernet" { // 画面の板は「読めていない」とだけ出すので、どれがかはここにしか残らない
			t.Errorf("%s: NetErr = %q", broken, p.NetErr)
		}
	}
}

// 秘密鍵はあるが公開鍵 (.pub) が無い Mac では、秘密鍵から .pub を作り直してから ssh-copy-id へ渡す (秘密鍵だけを見ると毎回失敗した)。
// .pub を作り直せなければ空の .pub を残さずに失敗する。
func TestKeyArgvRebuildsMissingPub(t *testing.T) {
	for _, c := range []struct{ keygenOK, emptyPub bool }{{true, false}, {false, false}, {true, true}} {
		keygenOK := c.keygenOK
		dir, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		home, bin := filepath.Join(dir, "home"), filepath.Join(dir, "bin")
		for _, d := range []string{home + "/.ssh", bin} {
			if err := os.MkdirAll(d, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		key := home + "/.ssh/id_ed25519"
		if err := os.WriteFile(key, []byte("private\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if c.emptyPub { // 前の回の失敗で空の .pub が残った Mac
			if err := os.WriteFile(key+".pub", nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		keygen := "#!/bin/sh\n[ \"$1\" = -y ] && echo 'ssh-ed25519 AAAAfake' && exit 0\nexit 1\n"
		if !keygenOK {
			keygen = "#!/bin/sh\nexit 1\n"
		}
		fakes := map[string]string{
			"ssh-keygen": keygen,
			// 渡された -i の公開鍵の中身を記録する (本物の ssh-copy-id は .pub を読んで相手へ置く)
			"ssh-copy-id": "#!/bin/sh\nwhile [ $# -gt 0 ]; do [ \"$1\" = -i ] && { cat \"$2\" > \"" + dir + "/copied\" || exit 1; }; shift; done\n",
		}
		for name, body := range fakes {
			if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		argv, err := KeyArgv(dest)
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(argv[0], argv[1:]...) //nolint:gosec // 偽の ssh-keygen / ssh-copy-id だけが見える PATH
		cmd.Env = []string{"PATH=" + bin + ":/bin", "HOME=" + home}
		out, err := cmd.CombinedOutput()
		copied, _ := os.ReadFile(filepath.Join(dir, "copied"))
		pub, pubErr := os.ReadFile(key + ".pub")
		if keygenOK {
			if err != nil || string(copied) != "ssh-ed25519 AAAAfake\n" {
				t.Errorf(".pub の無い Mac で公開鍵を作り直して渡さない: err=%v out=%q copied=%q", err, out, copied)
			}
			continue
		}
		if err == nil || copied != nil || (pubErr == nil && len(pub) == 0) {
			t.Errorf(".pub を作り直せないのに進んだ / 空の .pub を残した: err=%v copied=%q pub=%q %v", err, copied, pub, pubErr)
		}
	}
}
