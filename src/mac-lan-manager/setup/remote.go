package setup

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"mac-lan-manager/ledger"
	"mac-lan-manager/netinfo"
)

// Dest は相手へ ssh する先。HostKeyAlias は台帳の鍵 (IP を付け替えても known_hosts の鍵を台帳の鍵に結び付ける)。
type Dest struct {
	User, IP, HostKeyAlias string
}

// Validate は ssh の引数に入れる値を確かめる (user は台帳の手書きか $USER、IP は Bonjour か台帳から来る。先頭の - は ssh が option として読む)。
func (d Dest) Validate() error {
	if !ledger.ValidUser(d.User) {
		return fmt.Errorf("ユーザー名が使えない: %q", d.User)
	}
	if a, err := netip.ParseAddr(d.IP); err != nil || !a.Is4() {
		return fmt.Errorf("IPv4 でない: %q", d.IP)
	}
	// HostKeyAlias は known_hosts の行 (`,` で名前を並べる・`*` `?` `[` はパターン) と ssh の -o の値 (引用と \ を読む) に入る。
	// ssh が照合する名前と ssh-keygen -F で引く名前が食い違わないよう、ホスト名に使う文字だけを通す
	if !reAlias.MatchString(d.HostKeyAlias) {
		return fmt.Errorf("ホスト名が使えない: %q", d.HostKeyAlias)
	}
	return nil
}

// SSHArgv は相手で remote (sh の 1 行) を走らせる ssh の引数。tty は端末を明け渡すとき (sudo のパスワード) に立てる。
// batch は鍵だけで入る (パスワードを聞かない。観測で使う)。宛先の前に -- を置く。
func (d Dest) SSHArgv(tty, batch bool, remote string) []string {
	argv := []string{"ssh", "-o", "HostKeyAlias=" + d.HostKeyAlias, "-o", "ConnectTimeout=5"}
	if batch {
		argv = append(argv, "-o", "BatchMode=yes")
		// 観測は ssh を十数本打つ (鍵の確認・サービスの一覧・サービスごとの設定・I/F ごとの状態)。1 本ずつ繋ぎ直すと air5 で約 12 秒かかった
		// (実測)。接続を使い回す。置き場は ~/.ssh (自分だけが書ける): 誰でも書ける /tmp だと、他のユーザーが先に置いた socket へ繋いで
		// しまいうる。$TMPDIR はパスが長く、ssh が足す 17 文字で socket の上限を超えた (実測)
		if cp := controlPath(); cp != "" {
			argv = append(argv, "-o", "ControlMaster=auto", "-o", "ControlPath="+cp, "-o", "ControlPersist=15")
		}
	}
	if tty {
		argv = append(argv, "-t")
	}
	argv = append(argv, "--", d.User+"@"+d.IP)
	if remote != "" {
		argv = append(argv, remote)
	}
	return argv
}

// controlPath は ssh の接続を使い回す socket の置き場 (%C は宛先から ssh が作る 40 文字の名前。作るとき ssh はさらに 17 文字の
// 一時の後ろを足す)。socket のパスの上限 (104 バイト) に収まらない・~/.ssh が無いなら使い回さない。
// 受容: %C は HostKeyAlias を含まないので、同じ user@IP に別の名前で 15 秒以内に入ると前の接続を通る
// (同じ IP は同じ機械なので実害は小さい)。相手の IP を変えた直後は、旧い IP 宛ての観測が死んだ接続で ObserveTimeout まで待つ。
func controlPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, ".ssh")
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return ""
	}
	p := filepath.Join(dir, "mlm-%C")
	if len(p)-2+40+17 > 103 {
		return ""
	}
	return p
}

// Run は argv を起こして stdout を返す (観測のための口。テストで差し替える)。失敗したときの err は終了コード (ExitCode) を持ち、
// Error() に stderr の文を含める (鍵で入れないか届かないかを stderr で分ける)。
type Run func(ctx context.Context, argv []string) (stdout string, err error)

// ErrUnknown は「鍵で入れない」と「判定できない」(timeout・接続の失敗) を分けるための印。
var ErrUnknown = errors.New("判定できない")

var reAlias = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// PeerNet は ssh で読んだ相手の状態。KeyOK が偽で KeyErr が nil なら「鍵で入れない」と分かった。KeyErr が ErrUnknown なら判定できない。
type PeerNet struct {
	KeyOK  bool
	KeyErr error
	// HostKeyUnknown は相手のホスト鍵をまだ known_hosts に持っていない (初めての相手)。KeyOK は偽・KeyErr は nil で「鍵で入れない」と
	// 同じに扱い、鍵を置く手順 (端末を持つ ssh-copy-id) で人が指紋を確かめて受け入れる
	HostKeyUnknown bool
	NetKnown       bool
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
func ObservePeer(ctx context.Context, run Run, d Dest) PeerNet {
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
		words := []string{Quote(name)}
		for _, a := range args {
			words = append(words, Quote(a))
		}
		return run(ctx, d.SSHArgv(false, true, strings.Join(words, " ")))
	}
	snap, err := netinfo.Read(ctx, remote)
	if err != nil && !errors.Is(err, netinfo.ErrIPsIncomplete) { // IPs は使わない (Offline はサービスだけで決まる)
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
func knownHost(ctx context.Context, run Run, d Dest) (bool, error) {
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
func KeyArgv(d Dest) ([]string, error) {
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
func PeerStaticArgv(d Dest, s StaticIP) ([]string, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return d.SSHArgv(true, false, s.RemoteCommand()), nil
}

// ConfirmArgv は新しい IP で入り直して確認の印を置く ssh。d.IP は新しい IP。
func ConfirmArgv(d Dest, s StaticIP) ([]string, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	if d.IP != s.IP {
		return nil, fmt.Errorf("確認は新しい IP (%s) で入る: %s", s.IP, d.IP)
	}
	return d.SSHArgv(true, false, s.ConfirmCommand()), nil
}
