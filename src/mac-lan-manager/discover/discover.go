// Package discover は、Bonjour (dns-sd) で LAN の Mac を見つけるときの出力を読む。
//
// dns-sd の -B (サービスの一覧) / -L (インスタンスのホスト名) / -G (ホスト名の IP) は終わらないので、起こす側が上限付きで止める。
// ここは 1 行ずつ読む純関数だけを持つ (出力の形は testdata の本物の出力が正本)。
package discover

import (
	"regexp"
	"strconv"
	"strings"
)

// Browse は dns-sd -B の 1 行 (サービスの追加 / 削除)。
type Browse struct {
	Add      bool   // true = Add / false = Rmv
	If       int    // I/F の番号 (if_nametoindex。27 なら en13 など)
	Service  string // "_ssh._tcp."
	Instance string // "kojiのMac mini" (空白・括弧を含みうる。名前の衝突で " (2)" が付く)
}

// Resolved は dns-sd -L の 1 行 (インスタンス → ホスト名とポート)。
type Resolved struct {
	Instance string // 戻した後の名前 (\032 などの 10 進の文字番号を文字に戻す)
	Host     string // "m2macmini.local" (末尾の . は落とす)
	Port     int
	If       int
}

// Addr は dns-sd -G の 1 行 (ホスト名 → IP)。
type Addr struct {
	Add  bool
	If   int
	Host string // 末尾の . は落とす
	IP   string
}

// stamp は dns-sd の各行の先頭の時刻。🚨 時は 2 桁にそろえず空白で詰める (0〜9 時台は " 0:12:01.848")。行頭を `^\S+` で書いていた
// ころは、0:00〜9:59 の間 -B / -L / -G のどの行も読めず、Bonjour で 1 台も見つからなかった (ユーザー報告 2026-10-16 0 時台。
// fixture が 21 時台に取った出力だけだった)。3 つの読み取りはこの 1 つの定義から作る。
const stamp = `^\s*\d{1,2}:\d{2}:\d{2}\.\d+\s+`

var (
	// 例: "21:28:02.496  Add        3  27 local.               _ssh._tcp.           kojiのMac mini"
	reBrowse = regexp.MustCompile(stamp + `(Add|Rmv)\s+\d+\s+(\d+)\s+\S+\s+(\S+)\s+(.+?)\s*$`)
	// 例: "21:28:48.953  kojiのMac\032mini._ssh._tcp.local. can be reached at m2macmini.local.:22 (interface 27)"
	reLookup = regexp.MustCompile(stamp + `(.+?)\._[^.]+\._(?:tcp|udp)\.\S*\s+can be reached at (\S+?)\.?:(\d+) \(interface (\d+)\)`)
	// 例: "21:27:58.076  Add  40000002      27  m2macmini.local.                       169.254.217.100                              4500"
	reAddr = regexp.MustCompile(stamp + `(Add|Rmv)\s+\S+\s+(\d+)\s+(\S+)\s+(\d+\.\d+\.\d+\.\d+)\s`)
	reDec  = regexp.MustCompile(`\\(\d{3})`)
)

// ParseBrowse は dns-sd -B の 1 行を読む。見出しや STARTING などの行は ok=false。
func ParseBrowse(line string) (Browse, bool) {
	m := reBrowse.FindStringSubmatch(line)
	if m == nil {
		return Browse{}, false
	}
	ifn, _ := strconv.Atoi(m[2])
	return Browse{Add: m[1] == "Add", If: ifn, Service: m[3], Instance: m[4]}, true
}

// ParseLookup は dns-sd -L の 1 行を読む。
func ParseLookup(line string) (Resolved, bool) {
	m := reLookup.FindStringSubmatch(line)
	if m == nil {
		return Resolved{}, false
	}
	port, _ := strconv.Atoi(m[3])
	ifn, _ := strconv.Atoi(m[4])
	return Resolved{Instance: unescape(m[1]), Host: strings.TrimSuffix(m[2], "."), Port: port, If: ifn}, true
}

// ParseAddr は dns-sd -G v4 の 1 行を読む。
func ParseAddr(line string) (Addr, bool) {
	m := reAddr.FindStringSubmatch(line)
	if m == nil {
		return Addr{}, false
	}
	ifn, _ := strconv.Atoi(m[2])
	return Addr{Add: m[1] == "Add", If: ifn, Host: strings.TrimSuffix(m[3], "."), IP: m[4]}, true
}

// unescape は DNS の名前の 10 進の文字番号 (\032 = 空白) と \. などを文字に戻す。
func unescape(s string) string {
	s = reDec.ReplaceAllStringFunc(s, func(e string) string {
		n, err := strconv.Atoi(e[1:])
		if err != nil || n > 255 {
			return e
		}
		return string(rune(n))
	})
	return strings.NewReplacer(`\.`, ".", `\\`, `\`).Replace(s)
}
