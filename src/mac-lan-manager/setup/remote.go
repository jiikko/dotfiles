package setup

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
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
	if d.HostKeyAlias == "" || strings.ContainsFunc(d.HostKeyAlias, func(r rune) bool { return r <= ' ' || r == 0x7f || r == '=' }) {
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

// Run は argv を起こして stdout を返す (観測のための口。テストで差し替える)。失敗したときの err は終了コード (ExitCode) を持ち、
// Error() に stderr の文を含める (鍵で入れないか届かないかを stderr で分ける)。
type Run func(ctx context.Context, argv []string) (stdout string, err error)

// ErrUnknown は「鍵で入れない」と「判定できない」(timeout・接続の失敗) を分けるための印。
var ErrUnknown = errors.New("判定できない")

// PeerNet は ssh で読んだ相手の状態。KeyOK が偽で KeyErr が nil なら「鍵で入れない」と分かった。KeyErr が ErrUnknown なら判定できない。
type PeerNet struct {
	KeyOK    bool
	KeyErr   error
	NetKnown bool
	Service  string // ルーターの無い有線のサービス (Offline の先頭)
	IP       string
	Method   string
	Mask     string
	Router   string
}

// exitCoder は終了コードを持つエラー (*exec.ExitError)。
type exitCoder interface{ ExitCode() int }

// ObservePeer は鍵で入れるかを確かめ、入れたら相手の netinfo.Read を ssh 越しに走らせる (読むだけ。設定を変えるコマンドは打たない)。
// ssh の終了コード 255 は ssh 自身の失敗で、鍵で入れない (Permission denied) と届かない (timeout・経路無し) が混ざる。stderr に
// 認証の失敗の形 ("Permission denied (publickey,…)") があるときだけ「鍵で入れない」とし (接続の EPERM や鍵のファイルの
// 読めなさも "Permission denied" を含む。敵対レビュー)、それ以外は判定できない (ErrUnknown) にする (届かない相手を「鍵が無い」に
// 丸めると、次の手順の ssh-copy-id が同じく届かずに失敗するだけになる。issue 792)。
func ObservePeer(ctx context.Context, run Run, d Dest) PeerNet {
	var p PeerNet
	if err := d.Validate(); err != nil {
		p.KeyErr = fmt.Errorf("%w: %w", ErrUnknown, err)
		return p
	}
	if _, err := run(ctx, d.SSHArgv(false, true, "true")); err != nil {
		var ec exitCoder
		if ctx.Err() == nil && errors.As(err, &ec) && ec.ExitCode() == 255 && strings.Contains(err.Error(), "Permission denied (") {
			return p // 鍵で入れない
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
	if err != nil {
		return p
	}
	p.NetKnown = true
	if off := snap.Offline(); len(off) > 0 {
		s := off[0]
		p.Service, p.IP, p.Method, p.Mask, p.Router = s.Name, s.IP, s.Method, s.Mask, s.Router
	}
	return p
}

// Window と Delay は相手を固定 IP にするときの待ち (変えるまで / 確認を待つ間)。確認は新しい IP で入り直して印を置くので、その分の余裕を取る。
const (
	PeerDelay  = 5 * time.Second
	PeerWindow = 180 * time.Second
)

// PeerStatic は相手の有線を ip にする 1 回分 (確認が来なければ今の設定へ戻す)。
func PeerStatic(p PeerNet, ip, token string) (StaticIP, error) {
	r := Restore{Method: p.Method}
	if p.Method == "manual" {
		r.IP, r.Mask, r.Router = p.IP, p.Mask, p.Router
	}
	s := StaticIP{Service: p.Service, IP: ip, Mask: "255.255.255.0", Restore: r, Token: token, Delay: PeerDelay, Window: PeerWindow}
	return s, s.Validate()
}

// SelfStaticArgv はこの Mac の有線を ip にするコマンド (端末を明け渡して sudo のパスワードを聞く)。ルーターは渡さない
// (ルーター無しになるかは issue 789 の 1 で確かめる)。
func SelfStaticArgv(service, ip string) ([]string, error) {
	s := StaticIP{Service: service, IP: ip, Mask: "255.255.255.0", Restore: Restore{Method: "DHCP"}, Token: "self-static", Window: time.Second}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return []string{"sudo", "networksetup", "-setmanual", service, ip, "255.255.255.0"}, nil
}

// KeyArgv は鍵 (~/.ssh/id_ed25519) が無ければ作り、相手へ置くコマンド (ssh-keygen はパスフレーズを、ssh-copy-id は相手のパスワードを聞く)。
func KeyArgv(d Dest) ([]string, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	k := `"$HOME/.ssh/id_ed25519"`
	script := "test -f " + k + " || ssh-keygen -t ed25519 -f " + k + " || exit 1\n" +
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
