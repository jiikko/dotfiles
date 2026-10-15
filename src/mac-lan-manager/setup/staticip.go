// Package setup は初期設定 (この Mac → 相手) の手順を組む。手順は「観測した状態から残りを組み直す」純関数 (Plan) と、
// 相手の有線を固定 IP にする命令列 (StaticIP) に分ける。ここは命令を組むだけで、起こすのは呼び出し側 (lanmgr の Exec)。
// 要件と決定は issue 747 の「形」「決定」節。
//
// 🚨 本物の networksetup / sudo を起こすのは、人手の確認 (ルーターを空にした -setmanual が通るか・ssh が切れても変更が最後まで
// 走るか) が済んでから。それまではこの package のテストだけが、偽物の networksetup / sudo / nohup で命令列を走らせる。
package setup

import (
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
	"time"
)

// Quote は s を sh の 1 つの語にする (単引用符で包み、中の単引用符は「閉じて \' を挟んで開き直す」)。
func Quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// Restore は固定 IP にする前の設定 (確認が来なければここへ戻す)。
type Restore struct {
	Method string // "DHCP" / "manual"
	IP     string // manual のとき
	Mask   string
	Router string // 空ならルーター無し
}

// StaticIP は相手 (またはこの Mac) の有線のサービスを固定 IP にする 1 回分。
//
// 形 (デッドマンスイッチ。issue 747 の設計の反証レビュー): 背景で Delay 待ってから変え、Window の間に確認の印 (ConfirmPath) が
// 置かれなければ Restore へ戻す。新しい IP で入り直せた側が印を置く (ConfirmCommand)。有線越しに入っていると変えた瞬間に
// ssh が切れるので、変更は ssh から切り離して走らせる (sudo -b + nohup)。
type StaticIP struct {
	Service string // networksetup のサービス名 ("Thunderbolt Ethernet")
	IP      string
	Mask    string // "255.255.255.0"
	Restore Restore
	Token   string // 確認の印の名前 (実行ごとに変える)
	MarkDir string // 確認の印を置くディレクトリ (空なら DefaultMarkDir。テストは sandbox を渡す)
	Owner   string // 確認の印を置くユーザー (相手の Mac に ssh で入るユーザー名。この持ち主の印だけを認める)
	Delay   time.Duration
	Window  time.Duration
}

var (
	reToken = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`)
	reUser  = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,63}$`)
)

// Validate は命令列に埋め込む値を確かめる。外から来た値 (サービス名は相手の networksetup の出力) は引用で語にするが、
// 改行や制御文字は networksetup の引数として意味を持たないので拒む (ログや確認の板に出すときに行を割る)。
func (s StaticIP) Validate() error {
	if s.Service == "" || strings.ContainsFunc(s.Service, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return fmt.Errorf("サービス名が使えない: %q", s.Service)
	}
	for _, v := range []string{s.IP, s.Mask} {
		if a, err := netip.ParseAddr(v); err != nil || !a.Is4() {
			return fmt.Errorf("IPv4 でない: %q", v)
		}
	}
	switch s.Restore.Method {
	case "DHCP":
	case "manual":
		for _, v := range []string{s.Restore.IP, s.Restore.Mask} {
			if a, err := netip.ParseAddr(v); err != nil || !a.Is4() {
				return fmt.Errorf("戻す先が IPv4 でない: %q", v)
			}
		}
		if s.Restore.Router != "" {
			if a, err := netip.ParseAddr(s.Restore.Router); err != nil || !a.Is4() {
				return fmt.Errorf("戻す先のルーターが IPv4 でない: %q", s.Restore.Router)
			}
		}
	default:
		return fmt.Errorf("戻す方式が分からない: %q", s.Restore.Method)
	}
	if !reToken.MatchString(s.Token) {
		return errors.New("確認の印の名前が使えない")
	}
	if d := s.markDir(); !strings.HasPrefix(d, "/") || strings.ContainsFunc(d, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return fmt.Errorf("確認の印の置き場所が使えない: %q", d)
	}
	if s.Delay < 0 || s.Window < time.Second || s.Delay%time.Second != 0 || s.Window%time.Second != 0 {
		return errors.New("待ちの長さが使えない (秒の整数)") // sleep は秒で渡す (900ms が sleep 0 に化けた)
	}
	if !reUser.MatchString(s.Owner) {
		return fmt.Errorf("印の持ち主のユーザー名が使えない: %q", s.Owner)
	}
	return nil
}

// DefaultMarkDir は確認の印の既定の置き場所 (相手の Mac の上)。/var/tmp は再起動で消えない (確認の前に相手が再起動しても、
// 戻す側の待ちは止まっているので印は使われない。古い印は Script が変更の前に消す)。
const DefaultMarkDir = "/var/tmp"

func (s StaticIP) markDir() string {
	if s.MarkDir == "" {
		return DefaultMarkDir
	}
	return s.MarkDir
}

// ConfirmPath は確認の印の置き場所。
func (s StaticIP) ConfirmPath() string { return s.markDir() + "/mac-lan-manager-confirm-" + s.Token }

// ConfirmCommand は新しい IP で入り直した側が置く印 (ssh で相手に打つ 1 行。sh の組み込みだけで書く)。中身は相手の sh が展開する
// $SSH_CONNECTION ("<送った側> <port> <受けた側> <port>") で、Script は受けた側が新しい IP であることを確かめる。
func (s StaticIP) ConfirmCommand() string {
	return `printf '%s\n' "$SSH_CONNECTION" > ` + Quote(s.ConfirmPath())
}

// Script は root で背景に走らせる sh の本文 (Validate の後に呼ぶ)。
func (s StaticIP) Script() string {
	mark := Quote(s.ConfirmPath())
	lockPath := s.markDir() + "/mac-lan-manager-staticip.lock"
	svc := Quote(s.Service)
	restore := "networksetup -setdhcp " + svc
	if s.Restore.Method == "manual" {
		restore = "networksetup -setmanual " + svc + " " + s.Restore.IP + " " + s.Restore.Mask
		if s.Restore.Router != "" {
			restore += " " + s.Restore.Router
		}
	}
	var b strings.Builder
	// 同時に 2 本走らせない: 古い方の期限切れが、新しい方の確定済みの変更を戻した (fable の敵対レビュー)。2 本目は何もせずに抜ける
	fmt.Fprintf(&b, "mkdir %s 2>/dev/null || exit 1\n", Quote(lockPath))
	fmt.Fprintf(&b, "trap %s EXIT\n", Quote("rmdir "+Quote(lockPath)))
	fmt.Fprintf(&b, "rm -rf %s\n", mark) // 前の回の印 (置かれたディレクトリも) で戻しを飛ばさない
	fmt.Fprintf(&b, "sleep %d\n", int(s.Delay/time.Second))
	// 🚨 ルーターを渡さない形 (-setmanual <svc> <ip> <mask>) で「ルーター無し」になるかは未確認 (人手の確認の 1 つ)
	fmt.Fprintf(&b, "networksetup -setmanual %s %s %s\n", svc, s.IP, s.Mask)
	fmt.Fprintf(&b, "i=0\nwhile [ \"$i\" -lt %d ]; do\n", int(s.Window/time.Second))
	// 印を認める条件: 通常のファイル (シンボリックリンクでない)・持ち主が Owner (相手の Mac の他のユーザーは置けない)・中身の
	// $SSH_CONNECTION の 3 語目 (ssh を受けた側の IP) が新しい IP (新しい IP に届いた ssh からしか確認が成り立たない。Wi-Fi 越しの
	// 既存の接続から打っても有線を確かめたことにならない。fable の敵対レビュー P1)。sh の組み込みと /bin の ls だけで書く
	fmt.Fprintf(&b, "  if [ -f %s ] && [ ! -L %s ]; then\n", mark, mark)
	fmt.Fprintf(&b, "    set -- $(ls -ld %s)\n    owner=$3\n    read -r c < %s\n    set -- $c\n", mark, mark)
	fmt.Fprintf(&b, "    if [ \"$owner\" = %s ] && [ \"$3\" = %s ]; then rm -f %s; exit 0; fi\n  fi\n", Quote(s.Owner), s.IP, mark)
	b.WriteString("  sleep 1\n  i=$((i+1))\ndone\n")
	fmt.Fprintf(&b, "%s\n", restore)
	return b.String()
}

// RemoteCommand は ssh -t で相手に打つ 1 行。前面で sudo -v (パスワードを読む) し、変更は sudo -n -b + nohup で ssh から切り離す
// (背景の sudo がパスワードを読もうとすると止まる。ssh が切れても SIGHUP で止まらない)。
func (s StaticIP) RemoteCommand() string {
	return "sudo -v && sudo -n -b nohup sh -c " + Quote(s.Script()) + " >/dev/null 2>&1"
}
