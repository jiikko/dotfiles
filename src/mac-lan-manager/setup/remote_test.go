package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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
		"'ifconfig' '-l'":                                  "lo0 en0 en13",
	}
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
	for _, argv := range *calls {
		i := slices.Index(argv, "--")
		if i < 0 || argv[i+1] != "koji@192.168.50.7" || !slices.Contains(argv, "BatchMode=yes") || slices.Contains(argv, "-t") {
			t.Errorf("観測の ssh の形: %q", argv)
		}
	}
}

// ssh の失敗は、stderr の Permission denied のときだけ「鍵で入れない」(判定できた)。届かない (255 で別の理由)・255 以外・期限切れは
// 判定できない (ErrUnknown)。届かない相手を「鍵が無い」に丸めない (issue 792)。
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
	s, err := PeerStatic(PeerNet{Service: "Thunderbolt Ethernet", Method: "DHCP"}, "192.168.100.12", "tok-12345678")
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
	s, err := PeerStatic(p, "192.168.100.12", "tok-12345678")
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
	if s, err := PeerStatic(manual, "192.168.100.12", "tok-12345678"); err != nil || s.Restore != (Restore{Method: "manual", IP: "10.0.0.5", Mask: "255.255.255.0"}) {
		t.Errorf("手動の戻し先 = %+v %v", s.Restore, err)
	}
	if _, err := PeerStatic(PeerNet{Service: "TB", Method: "manual", IP: "10.0.0.5"}, "192.168.100.12", "tok-12345678"); err == nil {
		t.Error("マスクの分からない手動の設定を戻し先にした")
	}
	argv, err = KeyArgv(dest)
	if err != nil || argv[0] != "sh" || !strings.Contains(argv[2], "ssh-copy-id") || !strings.Contains(argv[2], "'koji@192.168.50.7'") ||
		!strings.Contains(argv[2], "'HostKeyAlias=m2macmini.local'") {
		t.Errorf("KeyArgv = %q %v", argv, err)
	}
	if argv, err := SelfStaticArgv("Thunderbolt Ethernet", "192.168.100.11"); err != nil || !slices.Equal(argv, []string{"sudo", "networksetup", "-setmanual", "Thunderbolt Ethernet", "192.168.100.11", "255.255.255.0"}) {
		t.Errorf("SelfStaticArgv = %q %v", argv, err)
	}
	if _, err := SelfStaticArgv("TB\nx", "192.168.100.11"); err == nil {
		t.Error("改行を含むサービス名を通した")
	}
}

// 初めての相手 (known_hosts に台帳の鍵の名前が無い) は、BatchMode の ssh が "Host key verification failed." で落ちても「判定できない」に
// しない (鍵を置く手順の ssh-copy-id で人が指紋を確かめる)。known_hosts にあるのに落ちたら鍵が変わったので断る (air5 で実測した形)。
func TestObservePeerHostKey(t *testing.T) {
	hkFail := exitError{255, "exit status 255: Host key verification failed."}
	for _, c := range []struct {
		name    string
		keygen  error
		first   bool
		unknown bool
	}{
		{"初めての相手", exitError{1, "exit status 1"}, true, false},
		{"鍵が変わった", nil, false, true},
		{"known_hosts を読めない", exitError{255, "boom"}, false, true},
	} {
		var keygenArgv []string
		run := func(_ context.Context, argv []string) (string, error) {
			if argv[0] == "ssh-keygen" {
				keygenArgv = argv
				return "", c.keygen
			}
			return "", hkFail
		}
		p := ObservePeer(context.Background(), run, dest)
		if p.HostKeyUnknown != c.first || errors.Is(p.KeyErr, ErrUnknown) != c.unknown || p.KeyOK {
			t.Errorf("%s: PeerNet = %+v", c.name, p)
		}
		if !slices.Equal(keygenArgv, []string{"ssh-keygen", "-F", "m2macmini.local"}) {
			t.Errorf("%s: known_hosts を台帳の鍵の名前で引かない: %q", c.name, keygenArgv)
		}
	}
}
