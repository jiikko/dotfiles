// Package sshdest は相手の Mac へ ssh する宛先 (ユーザー・IPv4・known_hosts の鍵の名前) と、その ssh の引数を組む。
package sshdest

import (
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"

	"mac-lan-manager/ledger"
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
	if !ValidAlias(d.HostKeyAlias) {
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

var reAlias = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// ValidAlias は HostKeyAlias に使える名前か (known_hosts の行では `,` で名前を並べ、`*` `?` `[` はパターンになる。Validate の注記)。
// Dest を通さずに HostKeyAlias を組む経路 (lanmgr の s) もこれで断る。
func ValidAlias(s string) bool { return reAlias.MatchString(s) }
