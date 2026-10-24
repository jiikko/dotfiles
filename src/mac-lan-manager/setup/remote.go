package setup

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"mac-lan-manager/netinfo"
	"mac-lan-manager/sshdest"
)

// Run は argv を起こして stdout を返す (観測のための口。テストで差し替える)。失敗したときの err は終了コード (ExitCode) を持ち、
// Error() に stderr の文を含める (鍵で入れないか届かないかを stderr で分ける)。
type Run func(ctx context.Context, argv []string) (stdout string, err error)

// ErrUnknown は「鍵で入れない」と「判定できない」(timeout・接続の失敗) を分けるための印。
var ErrUnknown = errors.New("判定できない")

// PeerNet は ssh で読んだ相手の状態。KeyOK が偽で KeyErr が nil なら「鍵で入れない」と分かった。KeyErr が ErrUnknown なら判定できない。
type PeerNet struct {
	KeyOK  bool
	KeyErr error
	// HostKeyUnknown は相手のホスト鍵をまだ known_hosts に持っていない (初めての相手)。KeyOK は偽・KeyErr は nil で「鍵で入れない」と
	// 同じに扱い、鍵を置く手順 (端末を持つ ssh-copy-id) で人が指紋を確かめて受け入れる
	HostKeyUnknown bool
	NetKnown       bool
	// NetErr は鍵で入れたのに NetKnown にしなかった理由 (読めなかったサービスの名前か、読み取りの失敗)。画面の板は「読めていない」とだけ
	// 出すので、どのサービスがなぜかは操作のログと dump --peer にだけ出る
	NetErr string
	// AdvertKnown は有線の名前の広告の状態 (Advert) を読めた (wiredname.go)
	AdvertKnown bool
	Advert      NameState
	Service     string // ルーターの無い有線のサービス (Offline の先頭)
	IP          string
	Method      string
	Mask        string
	Router      string
	// Services は相手の全部のサービス (NetKnown のときだけ)。固定し直すときに、前の IP を同じ相手の別のサービスが持っていないかを見る
	// (前のアダプタのサービスは手動の設定を持ったまま残る。Bonjour は相手の I/F ごとの IP を全部は見せない)
	Services []netinfo.Service
}

// exitCoder は終了コードを持つエラー (*exec.ExitError)。
type exitCoder interface{ ExitCode() int }

// ObservePeer は鍵で入れるかを確かめ、入れたら相手の netinfo.Read を ssh 越しに走らせる (読むだけ。設定を変えるコマンドは打たない)。
// ssh の終了コード 255 は ssh 自身の失敗で、鍵で入れない (Permission denied) と届かない (timeout・経路無し) が混ざる。stderr に
// 認証の失敗の形 ("Permission denied (publickey,…)") があるときだけ「鍵で入れない」とし (接続の EPERM や鍵のファイルの
// 読めなさも "Permission denied" を含む)、それ以外は判定できない (ErrUnknown) にする (届かない相手を「鍵が無い」に
// 丸めると、次の手順の ssh-copy-id が同じく届かずに失敗するだけになる)。
func ObservePeer(ctx context.Context, run Run, d sshdest.Dest) PeerNet {
	var p PeerNet
	if err := d.Validate(); err != nil {
		p.KeyErr = fmt.Errorf("%w: %w", ErrUnknown, err)
		return p
	}
	if _, err := run(ctx, d.SSHArgv(false, true, "true")); err != nil {
		var ec exitCoder
		if ctx.Err() == nil && errors.As(err, &ec) && ec.ExitCode() == 255 {
			if strings.Contains(err.Error(), "Permission denied (") {
				return p // 鍵で入れない
			}
			// BatchMode の ssh は初めての相手の指紋を聞けず "Host key verification failed." で落ちる。鍵が変わった相手も同じ 1 行なので、
			// known_hosts に台帳の鍵の名前があるかで分ける (無ければ初めての相手。あれば鍵が変わった: 成りすましか相手の入れ直し)。
			// 分けないと初めての相手は「判定できない」になり、x が全部の手順を断って、指紋を聞ける ssh-copy-id にも辿り着けなかった (air5 で実測)
			if strings.Contains(err.Error(), "Host key verification failed") {
				switch known, kerr := knownHost(ctx, run, d); {
				case kerr == nil && !known:
					p.HostKeyUnknown = true
					return p
				case kerr == nil && known:
					p.KeyErr = fmt.Errorf("%w: 相手のホスト鍵が known_hosts と違う (相手を入れ直したか、別の相手。確かめてから ssh-keygen -R %s で消す)", ErrUnknown, d.HostKeyAlias)
					return p
				}
			}
		}
		p.KeyErr = fmt.Errorf("%w: %w", ErrUnknown, err)
		return p
	}
	p.KeyOK = true
	remote := func(ctx context.Context, name string, args ...string) (string, error) {
		words := append(make([]string, 0, 1+len(args)), Quote(name))
		for _, a := range args {
			words = append(words, Quote(a))
		}
		return run(ctx, d.SSHArgv(false, true, strings.Join(words, " ")))
	}
	snap, err := netinfo.ReadServices(ctx, remote) // IPs は使わない (Offline はサービスだけで決まる)
	// 1 つでも読めなかったサービスがあれば判定できないにする: そのサービスは Offline から黙って外れ、相手に有線が無い・(無し) と言い切る
	// (ルーターの無い有線が 2 つある相手では、もう一方を固定の対象に選びうる)
	switch {
	case err != nil:
		p.NetErr = err.Error()
		return p
	case len(snap.Unread) > 0:
		p.NetErr = "読めないサービス: " + strings.Join(snap.Unread, ", ")
		return p
	}
	p.NetKnown = true
	p.Services = snap.Services
	if off := snap.Offline(); len(off) > 0 {
		s := off[0]
		p.Service, p.IP, p.Method, p.Mask, p.Router = s.Name, s.IP, s.Method, s.Mask, s.Router
	}
	if out, err := run(ctx, d.SSHArgv(false, true, "sh -c "+Quote(StatusScript()))); err == nil {
		if st, err := ParseStatus(out); err == nil {
			p.Advert, p.AdvertKnown = st, true
		}
	}
	return p
}

// knownHost は、ssh がこの宛先の照合に使う known_hosts のどれかに alias の行があるか (読むだけ)。照合先は ssh -G で読む
// (~/.ssh/known_hosts だけを見ると、known_hosts2・/etc/ssh/ssh_known_hosts・config の UserKnownHostsFile にある変わった鍵を
// 「初めての相手」と取り違えた)。無いファイルは行が無いと数える (一度も ssh していない Mac には known_hosts が無く、
// ssh-keygen -F はそのとき 255 で抜けて「読めない」になった)。ssh-keygen -F は見つかれば 0、無ければ 1 で抜ける
func knownHost(ctx context.Context, run Run, d sshdest.Dest) (bool, error) {
	cfg, err := run(ctx, []string{"ssh", "-G", "-o", "HostKeyAlias=" + d.HostKeyAlias, "--", d.User + "@" + d.IP})
	if err != nil {
		return false, err
	}
	var files []string
	for _, l := range strings.Split(cfg, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(l), " "); ok && (k == "userknownhostsfile" || k == "globalknownhostsfile") {
			files = append(files, strings.Fields(v)...)
		}
	}
	if len(files) == 0 {
		return false, errors.New("ssh -G が known_hosts の置き場所を返さない")
	}
	for _, f := range files {
		if _, err := run(ctx, []string{"test", "-e", f}); err != nil {
			var ec exitCoder
			if errors.As(err, &ec) && ec.ExitCode() == 1 {
				continue // 無いファイル
			}
			return false, err // 確かめられない (期限切れ・test を起こせない): 初めての相手と数えない (敵対レビュー)
		}
		_, err := run(ctx, []string{"ssh-keygen", "-F", d.HostKeyAlias, "-f", f})
		if err == nil {
			return true, nil
		}
		var ec exitCoder
		if !errors.As(err, &ec) || ec.ExitCode() != 1 {
			return false, err
		}
	}
	return false, nil
}

// Window と Delay は相手を固定 IP にするときの待ち (変えるまで / 確認を待つ間)。確認は新しい IP で入り直して印を置くので、その分の余裕を取る。
const (
	PeerDelay  = 5 * time.Second
	PeerWindow = 180 * time.Second
)

// PeerStatic は相手の有線を ip にする 1 回分 (確認が来なければ今の設定へ戻す)。
func PeerStatic(p PeerNet, ip, mask, token string) (StaticIP, error) {
	r := Restore{Method: p.Method}
	switch p.Method {
	case netinfo.MethodManual:
		r.IP, r.Mask, r.Router = p.IP, p.Mask, p.Router
	case netinfo.MethodManualDHCPRouter:
		r.IP = p.IP
	}
	s := StaticIP{Service: p.Service, IP: ip, Mask: mask, Restore: r, Token: token, Delay: PeerDelay, Window: PeerWindow}
	return s, s.Validate()
}

// SelfStaticArgv はこの Mac の有線を ip にするコマンド (端末を明け渡して sudo のパスワードを聞く)。ルーターは渡さない
// (ルーター無しになるかは本物の相手で未確認)。
func SelfStaticArgv(service, ip, mask string) ([]string, error) {
	s := StaticIP{Service: service, IP: ip, Mask: mask, Restore: Restore{Method: netinfo.MethodDHCP}, Token: "self-static", Window: time.Second}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return []string{"sudo", "networksetup", "-setmanual", service, ip, mask}, nil
}

// KeyArgv は鍵 (~/.ssh/id_ed25519) が無ければ作り、相手へ置くコマンド (ssh-keygen はパスフレーズを、ssh-copy-id は相手のパスワードを聞く)。
func KeyArgv(d sshdest.Dest) ([]string, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	k := `"$HOME/.ssh/id_ed25519"`
	// 公開鍵の有無も見る: 秘密鍵だけを見ると、.pub が消えた Mac では鍵を作らずに進み、ssh-copy-id が毎回失敗した。
	// .pub は秘密鍵から作り直す (一時ファイルから移す: 失敗で空の .pub が残ると、次から「ある」と読んで同じ失敗になる)
	script := "test -f " + k + " || ssh-keygen -t ed25519 -f " + k + " || exit 1\n" +
		"test -s " + k + ".pub || { ssh-keygen -y -f " + k + " > " + k + ".pub.tmp && mv " + k + ".pub.tmp " + k + ".pub; } || { rm -f " + k + ".pub.tmp; exit 1; }\n" +
		"ssh-copy-id -i " + k + ".pub -o " + Quote("HostKeyAlias="+d.HostKeyAlias) + " " + Quote(d.User+"@"+d.IP)
	return []string{"sh", "-c", script}, nil
}

// PeerStaticArgv は相手を固定 IP にする ssh (端末を明け渡して相手の sudo のパスワードを聞く。変更は ssh から切り離して走る)。
func PeerStaticArgv(d sshdest.Dest, s StaticIP) ([]string, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return d.SSHArgv(true, false, s.RemoteCommand()), nil
}

// ConfirmArgv は新しい IP で入り直して確認の印を置く ssh。d.IP は s.ConfirmIP() (ふつうは新しい IP)。
func ConfirmArgv(d sshdest.Dest, s StaticIP) ([]string, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	if d.IP != s.ConfirmIP() {
		return nil, fmt.Errorf("確認は %s で入る: %s", s.ConfirmIP(), d.IP)
	}
	return d.SSHArgv(true, false, s.ConfirmCommand()), nil
}
