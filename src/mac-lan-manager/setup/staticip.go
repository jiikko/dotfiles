// Package setup は初期設定 (この Mac → 相手) の手順を組む。手順は「観測した状態から残りを組み直す」純関数 (Plan) と、
// 相手の有線を固定 IP にする命令列 (StaticIP) に分ける。ここは命令を組むだけで、起こすのは呼び出し側 (lanmgr の Exec)。
// 要件と決定は issue 747 の「形」「決定」節。
//
// 🚨 本物の networksetup / sudo を起こすのは、人手の確認 (ルーターを空にした -setmanual が通るか・ssh が切れても変更が最後まで
// 走るか・sudo の use_pty の有無で SIGHUP を nohup が捨てられるか) が済んでから。それまではこの package のテストだけが、
// 偽物の networksetup / sudo / nohup で命令列を走らせる。
package setup

import (
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
	"time"

	"mac-lan-manager/ledger"
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
// 形 (デッドマンスイッチ。issue 747 の設計の反証レビュー): 背景で Delay 待ってから変え、Window の間に確認の印が置かれなければ
// Restore へ戻す。新しい IP で入り直した側が印を置く (ConfirmCommand)。有線越しに入っていると変えた瞬間に ssh が切れるので、
// 変更は ssh から切り離して走らせる (sudo -b + nohup)。
//
// 🚨 印と lock は root だけが書ける状態ディレクトリ (StateDir。0700) に置く。誰でも書ける /var/tmp に置いていたころは、
// 相手の Mac の他のユーザーが印を先に置く・FIFO に差し替えて root の read を止める・symlink で確認の書き込みを別のファイルへ
// 向ける・lock を先に作って以後を全部止める、ができた (fable の敵対レビュー 2 周)。root のディレクトリなら、これらは全部起きない。
type StaticIP struct {
	Service string // networksetup のサービス名 ("Thunderbolt Ethernet")
	IP      string
	Mask    string // "255.255.255.0"
	Restore Restore
	Token   string // 確認の印の名前 (実行ごとに変える)
	// StateDir は root だけが書ける状態ディレクトリ (空なら DefaultStateDir)。既定以外はテストの差し込み口: 誰でも書ける親の下を
	// 渡すと、ls -ld で確かめた後にディレクトリごと差し替えられる (既定の /var/db は root の 0755 なので起きない。fable の敵対レビュー)
	StateDir string
	// StateOwner は状態ディレクトリの持ち主として認めるユーザー (空なら root。テストは自分の名前を渡す。sh の外の道具 (id) を使わない)
	StateOwner string
	Delay      time.Duration
	Window     time.Duration
}

// DefaultStateDir は状態ディレクトリの既定 (相手の Mac の上)。/var/db は再起動で消えず、root の場所。
const DefaultStateDir = "/var/db/mac-lan-manager"

var (
	reToken = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`)
)

func ipv4(v string) bool {
	a, err := netip.ParseAddr(v)
	return err == nil && a.Is4()
}

// Validate は命令列に埋め込む値を確かめる。外から来た値 (サービス名は相手の networksetup の出力) は引用で語にするが、
// 改行や制御文字は networksetup の引数として意味を持たないので拒む (ログや確認の板に出すときに行を割る)。
func (s StaticIP) Validate() error {
	if s.Service == "" || strings.ContainsFunc(s.Service, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return fmt.Errorf("サービス名が使えない: %q", s.Service)
	}
	if !ipv4(s.IP) || !ipv4(s.Mask) {
		return fmt.Errorf("IPv4 でない: %q / %q", s.IP, s.Mask)
	}
	switch s.Restore.Method {
	case "DHCP":
	case "manual":
		if !ipv4(s.Restore.IP) || !ipv4(s.Restore.Mask) || (s.Restore.Router != "" && !ipv4(s.Restore.Router)) {
			return fmt.Errorf("戻す先が IPv4 でない: %+v", s.Restore)
		}
	default:
		return fmt.Errorf("戻す方式が分からない: %q", s.Restore.Method)
	}
	if !reToken.MatchString(s.Token) {
		return errors.New("確認の印の名前が使えない")
	}
	if d := s.stateDir(); !strings.HasPrefix(d, "/") || strings.ContainsFunc(d, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return fmt.Errorf("状態ディレクトリが使えない: %q", d)
	}
	if s.StateOwner != "" && !ledger.ValidUser(s.StateOwner) {
		return fmt.Errorf("状態ディレクトリの持ち主の名前が使えない: %q", s.StateOwner)
	}
	if s.Delay < 0 || s.Window < time.Second || s.Delay%time.Second != 0 || s.Window%time.Second != 0 {
		return errors.New("待ちの長さが使えない (秒の整数)") // sleep は秒で渡す (900ms が sleep 0 に化けた)
	}
	return nil
}

func (s StaticIP) stateDir() string {
	if s.StateDir == "" {
		return DefaultStateDir
	}
	return s.StateDir
}

// ConfirmPath は確認の印の置き場所 (状態ディレクトリの中)。
func (s StaticIP) ConfirmPath() string { return s.stateDir() + "/confirm-" + s.Token }

func (s StaticIP) lockPath() string { return s.stateDir() + "/staticip.lock" }

// prepare は状態ディレクトリを root の 0700 で用意し、lock の持ち主が生きていれば断る sh の断片 (前面の事前確認と背景の本体で使う)。
// 状態ディレクトリが symlink・ディレクトリでない・root のものでないなら断る (他の誰かが先に作った場所は使わない)。
func (s StaticIP) prepare(take bool) string {
	dir, lock := Quote(s.stateDir()), Quote(s.lockPath())
	takeover := Quote(s.lockPath() + ".takeover")
	var b strings.Builder
	b.WriteString("umask 077\nset -f\n")
	fmt.Fprintf(&b, "if [ -L %s ] || { [ -e %s ] && [ ! -d %s ]; }; then echo 'mac-lan-manager: 状態ディレクトリが使えない' >&2; exit 1; fi\n", dir, dir, dir)
	fmt.Fprintf(&b, "mkdir -p %s && chmod 700 %s || exit 1\n", dir, dir)
	owner := s.StateOwner
	if owner == "" {
		owner = "root"
	}
	fmt.Fprintf(&b, "set -- $(ls -ld %s)\nif [ \"$3\" != %s ]; then echo 'mac-lan-manager: 状態ディレクトリの持ち主が違う' >&2; exit 1; fi\n", dir, Quote(owner))
	// lock の持ち主が生きているか: pid が居て、その起動時刻が lock に書いた時刻と同じ。pid だけだと、電源断の後の再起動で同じ番号を
	// 別のプロセスが使い、死んだ lock が永久に「走っている」になった (fable の敵対レビュー 3 周目)
	fmt.Fprintf(&b, "alive() { p=$(cat %s/pid 2>/dev/null); st=$(cat %s/start 2>/dev/null); [ -n \"$p\" ] && kill -0 \"$p\" 2>/dev/null && [ \"$(LC_ALL=C ps -o lstart= -p \"$p\" 2>/dev/null)\" = \"$st\" ]; }\n", lock, lock)
	fmt.Fprintf(&b, "refuse() { echo \"mac-lan-manager: 固定 IP への切り替えが走っている (pid $p)。止まっているなら sudo rm -rf %s で外す\" >&2; exit 1; }\n", strings.Trim(lock, "'"))
	fmt.Fprintf(&b, "if [ -d %s ] && alive; then refuse; fi\n", lock)
	if take {
		// 死んだ lock の引き取りは 1 本ずつ (takeover を mkdir できた 1 本だけが、もう一度生死を見てから外す)。rm -rf と mkdir を
		// それぞれが打つと、後の rm -rf が先の lock を消して 2 本とも進んだ (fable の敵対レビュー 3 周目 P2)
		b.WriteString("tk=0\n")
		fmt.Fprintf(&b, "if [ -d %s ]; then\n", lock)
		fmt.Fprintf(&b, "  mkdir %s 2>/dev/null || { echo 'mac-lan-manager: 別の切り替えが止まった lock を引き取っている' >&2; exit 1; }\n  tk=1\n", takeover)
		fmt.Fprintf(&b, "  if [ -d %s ] && alive; then rmdir %s; refuse; fi\n  rm -rf %s\nfi\n", lock, takeover, lock)
		fmt.Fprintf(&b, "if ! mkdir %s 2>/dev/null; then [ \"$tk\" = 1 ] && rmdir %s; echo 'mac-lan-manager: 別の切り替えが先に始まった' >&2; exit 1; fi\n", lock, takeover)
		fmt.Fprintf(&b, "echo $$ > %s/pid\nLC_ALL=C ps -o lstart= -p $$ > %s/start\n", lock, lock)
		fmt.Fprintf(&b, "[ \"$tk\" = 1 ] && rmdir %s\n", takeover)
	}
	return b.String()
}

// Script は root で背景に走らせる sh の本文 (Validate の後に呼ぶ)。
func (s StaticIP) Script() string {
	mark, lock := Quote(s.ConfirmPath()), Quote(s.lockPath())
	svc := Quote(s.Service)
	restore := "networksetup -setdhcp " + svc
	if s.Restore.Method == "manual" {
		restore = "networksetup -setmanual " + svc + " " + s.Restore.IP + " " + s.Restore.Mask
		if s.Restore.Router != "" {
			restore += " " + s.Restore.Router
		}
	}
	var b strings.Builder
	b.WriteString(s.prepare(true))
	// 終わり方を問わず (確認・期限切れ・SIGTERM / SIGHUP)、変えたまま確認が無ければ戻してから lock を外す。shutdown は TERM を配るので、
	// 待ちの途中で相手を再起動しても新しい IP のまま残らない (fable の敵対レビュー)
	fmt.Fprintf(&b, "changed=0\nconfirmed=0\nfinish() { if [ \"$changed\" = 1 ] && [ \"$confirmed\" = 0 ]; then %s; fi; rm -rf %s; }\n", restore, lock)
	// INT / TERM / HUP の trap は EXIT の trap をシグナルでも走らせるため。macOS の /bin/sh (bash) はこれが無くても EXIT を走らせるので、
	// この Mac の上のテストでは外しても緑のまま (変異で確かめた)。EXIT をシグナルで走らせない sh (dash など) のために残す
	b.WriteString("trap 'finish' EXIT\ntrap 'exit 1' INT TERM HUP\n")
	fmt.Fprintf(&b, "rm -rf %s\n", mark) // 前の回の印で戻しを飛ばさない
	fmt.Fprintf(&b, "sleep %d\n", int(s.Delay/time.Second))
	// 🚨 ルーターを渡さない形 (-setmanual <svc> <ip> <mask>) で「ルーター無し」になるかは未確認 (人手の確認の 1 つ)
	fmt.Fprintf(&b, "changed=1\nnetworksetup -setmanual %s %s %s\n", svc, s.IP, s.Mask)
	fmt.Fprintf(&b, "i=0\nwhile [ \"$i\" -lt %d ]; do\n", int(s.Window/time.Second))
	// 印を認める条件: 通常のファイルで、中身の $SSH_CONNECTION の 3 語目 (ssh を受けた側の IP) が新しい IP。新しい IP に届いた ssh
	// からしか確認が成り立たない (Wi-Fi 越しの既存の接続から打っても有線を確かめたことにならない。fable の敵対レビュー P1)。
	// 印は root の 0700 のディレクトリにしか無いので、差し替え・FIFO・symlink は起きない
	fmt.Fprintf(&b, "  if [ -f %s ] && [ ! -L %s ]; then\n    read -r c < %s\n    set -- $c\n", mark, mark, mark)
	fmt.Fprintf(&b, "    if [ \"$3\" = %s ]; then confirmed=1; rm -f %s; exit 0; fi\n  fi\n", s.IP, mark)
	b.WriteString("  sleep 1\n  i=$((i+1))\ndone\n")
	return b.String()
}

// RemoteCommand は ssh -t で相手に打つ 1 行。前面で sudo -v (パスワードを読む) し、走っているものが無いかを前面で確かめて理由を出し
// (背景に回すと失敗が見えない。fable の敵対レビュー)、変更は sudo -n -b + nohup で ssh から切り離す (背景の sudo がパスワードを
// 読もうとすると止まる。ssh が切れても SIGHUP で止まらない)。
//
// 限界 (受容。issue 747): 前面の確かめと背景の lock の取得の間 (ssh 1 往復) に別の操作者が先に取ると、後の方は背景で黙って抜ける
// (変更はしない)。電源断・kill -9 では戻しが走らず新しい IP のまま残る (次の回は止まった lock を引き取るだけ)。
func (s StaticIP) RemoteCommand() string {
	return "sudo -v && sudo -n sh -c " + Quote(s.prepare(false)) + " && sudo -n -b nohup sh -c " + Quote(s.Script()) + " >/dev/null 2>&1"
}

// ConfirmCommand は新しい IP で入り直した側が打つ 1 行 (ssh -t で。sudo がパスワードを聞く)。印は root が状態ディレクトリに書く。
// 中身は相手の sh が展開する $SSH_CONNECTION ("<送った側> <port> <受けた側> <port>") で、Script は受けた側が新しい IP であることを
// 確かめる (sudo は環境を引き継がないので、値は sudo の外で読んで引数で渡す)。🚨 相手のログインシェルが sh / bash / zsh である前提
// (fish では `c=` が、tcsh では引用の中の書き方が通らない)。
func (s StaticIP) ConfirmCommand() string {
	return `c="$SSH_CONNECTION"; sudo sh -c 'umask 077; printf "%s\n" "$1" > "$2"' _ "$c" ` + Quote(s.ConfirmPath())
}
