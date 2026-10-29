package setup

import (
	"context"
	"encoding/xml"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"mac-lan-manager/sshdest"
)

// 有線の名前 (`<ホスト名>.wired.local`): 各 Mac が自分の有線の固定 IP を Bonjour に広告する (2026-10-17 ユーザー決定)。
// `air5.local` は Bonjour が有線と Wi-Fi の両方の IP を返し、どちらへ繋ぐかが決まらない。人が ssh / 画面共有 / smb で打つ名前を有線に
// 向けるためのもの (lan-manager 自身は IP で繋ぐ)。
//
// - root は要らない: ユーザーの LaunchAgent が sh のループ (advertLoop) を起こし続け、ループが /usr/bin/dns-sd -P を子として起こす。
//   名前は登録したプロセスが生きている間だけ残る (mDNSResponder に固定の名前を持たせる設定は無い)。
// - ループは、広告する IP をこの Mac のどの I/F も持っていない間は dns-sd を止める (964): dns-sd を直接 KeepAlive で走らせていた
//   ときは、有線のアダプタを替えて I/F が IP を失った後も古い IP を広告し続け、相手から「名前が内部 IP に引ける = 済み」と読まれ、
//   繋ぐ宛先も届かない名前になった。止めても問い合わせた側のキャッシュには約 4 分残る (下の項。受容)。
// - `.local` で終わらない名前 (`air5.wired`) は Bonjour に問い合わせられない (/etc/resolver を root で書く必要がある。実測)。
// - 済みの判定は名前の解決ではなく、広告する Mac の launchd が走らせている job の実際の引数で見る: 止めた名前も問い合わせた側の
//   キャッシュに約 4 分 (TTL 240) 残った (実測)。plist の中身では見ない (入れ直しの bootstrap が失敗すると、新しい plist の横で古い
//   引数の job が走り続け、新しい IP で済みと読んだ)。
// - 受容 (未確認): 同じ名前を別の Mac が広告していると Bonjour が改名する。dns-sd がそのとき落ちるか走り続けるかは確かめていない
//   (走り続けるなら済みと読む)。帯・IP を変えた後は、古い IP をどの I/F も持っていなければ広告は止まる (上の項) が、名前が新しい IP を
//   指すのは、初期設定を開き直して名前の手順まで進めてから。
// - dns-sd -P は広告する I/F を選べないので、名前は Wi-Fi 側にも見える (受容。747)。

// WiredNameLabel は LaunchAgent の名前 (~/Library/LaunchAgents/<これ>.plist)。
const WiredNameLabel = "com.jiikko.mac-lan-manager.wired-name"

// wiredService は dns-sd -P が名前と一緒に登録する目印のサービスの種類 (名前だけは登録できない)。
const wiredService = "_mac-lan-manager._tcp"

var reLabel = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)

// WiredName は Bonjour のホスト名 (台帳の鍵。"air5.local") から有線の名前 ("air5.wired.local") を作る。
func WiredName(host string) (string, error) {
	label, ok := strings.CutSuffix(strings.ToLower(host), ".local")
	if !ok || !reLabel.MatchString(label) {
		return "", fmt.Errorf("有線の名前を作れないホスト名: %q", host)
	}
	return label + ".wired.local", nil
}

// IsWiredName は name が有線の名前 (WiredName が作る形) か。
func IsWiredName(name string) bool {
	label, ok := strings.CutSuffix(name, ".wired.local")
	return ok && reLabel.MatchString(label)
}

// NameAdvert は広告する名前と IP。
type NameAdvert struct {
	Name string // "air5.wired.local"
	IP   string
}

// Validate は sh と plist に埋め込む値を確かめる。
func (a NameAdvert) Validate() error {
	if !IsWiredName(a.Name) {
		return fmt.Errorf("有線の名前が使えない: %q", a.Name)
	}
	if !ipv4(a.IP) {
		return fmt.Errorf("IPv4 でない: %q", a.IP)
	}
	return nil
}

// args は plist の ProgramArguments。jobArgs は launchctl print の arguments の 4 番目 (AdvertTag)・7 番目 (名前)・8 番目 (IP) を読み、
// RemoveScript は plist の ProgramArguments.6 (名前) を読む。名前と IP の位置は dns-sd を直接起こしていた前の形と同じに保つ
// (前の形で走っている Mac の名前と IP も同じ数え方で読み、止める・入れ直すときに使う)。
func (a NameAdvert) args() []string {
	return []string{"/bin/sh", "-c", advertLoop("/sbin/ifconfig", "/usr/bin/dns-sd", strconv.Itoa(AdvertEvery)), AdvertTag,
		strings.TrimSuffix(a.Name, ".local"), wiredService, a.Name, a.IP}
}

// AdvertEvery はループが ifconfig を見る間隔 (秒。確認の板の説明も読む)。
const AdvertEvery = 10

// AdvertTag はループの形の目印 (sh の $0)。ループの中身を変えたら上げる: 前の形で走っている Mac を「済み」と読まず、初期設定の名前の
// 手順で入れ直させる (Advertises が見る)。
const AdvertTag = "mac-lan-manager-wired-name-v2"

// advertLoop は IP を持っている間だけ dns-sd -P を走らせる sh (1 行。launchctl print は引数を 1 行ずつ出し、jobArgs が行で数えるので
// 改行を入れない)。$1 = dns-sd に渡すインスタンス名、$2 = サービスの種類、$3 = 名前、$4 = IP。every 秒ごとに ifconfig を見る。
// dns-sd が自分で落ちても次の回で起こし直す。止める合図 (bootout の TERM) では子の dns-sd と待ちの sleep も止めてから抜ける。
// ifconfig と dns-sd はテストで偽物に差し替えるため引数で受ける。
func advertLoop(ifconfig, dnssd, every string) string {
	return fmt.Sprintf(`p=; s=; trap '[ -n "$p" ] && kill "$p" 2>/dev/null; [ -n "$s" ] && kill "$s" 2>/dev/null; exit 0' TERM INT HUP; `+
		`while :; do if %s | /usr/bin/grep -qF "inet $4 "; then `+
		`if [ -z "$p" ] || ! kill -0 "$p" 2>/dev/null; then %s -P "$1" "$2" local 9 "$3" "$4" >/dev/null 2>&1 & p=$!; fi; `+
		`elif [ -n "$p" ]; then kill "$p" 2>/dev/null; wait "$p" 2>/dev/null; p=; fi; `+
		`sleep %s & s=$!; wait "$s"; s=; done`, ifconfig, dnssd, every)
}

// plistPath は sh の中で使う plist の置き場 ($HOME を sh に展開させる)。
const plistPath = `"$HOME/Library/LaunchAgents/` + WiredNameLabel + `.plist"`

// InstallScript は LaunchAgent を書いて (書き換えて) 起こし直す sh。sudo は要らない。gui のドメインに入れるので、その Mac に
// ログインしている間に打つ (ssh からでも入る。air5 で実測)。ログインしていなければ bootstrap が失敗して非 0 で抜ける。
func (a NameAdvert) InstallScript() string {
	var items strings.Builder
	for _, s := range a.args() {
		items.WriteString("<string>")
		_ = xml.EscapeText(&items, []byte(s)) // ループの sh は & と > を含む。strings.Builder への書き込みは失敗しない
		items.WriteString("</string>")
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>Label</key><string>` + WiredNameLabel + `</string><key>ProgramArguments</key><array>` + items.String() +
		`</array><key>RunAtLoad</key><true/><key>KeepAlive</key><true/></dict></plist>`
	var b strings.Builder
	b.WriteString("set -e\numask 022\n")
	fmt.Fprintf(&b, "mkdir -p \"$HOME/Library/LaunchAgents\"\np=%s\n", plistPath)
	fmt.Fprintf(&b, "printf '%%s\\n' %s > \"$p.tmp\"\nplutil -lint \"$p.tmp\" >/dev/null\nmv -f \"$p.tmp\" \"$p\"\n", Quote(plist))
	// 前の版が走っていれば止めてから入れ直す (止めずに bootstrap すると「既にある」で失敗する)
	b.WriteString(jobArgs + bootoutLine)
	fmt.Fprintf(&b, "launchctl bootstrap \"gui/$(id -u)\" \"$p\"\n")
	return b.String()
}

// bootoutLine は読み込まれている job を止める sh の断片 (jobArgs の後で使う。out が空 = 読み込まれていない・GUI のドメインが無い
// (誰もログインしていない) ときは打たない: 打つと 3 以外で落ち、消すはずの plist を残した)。止められなければ理由を出して
// 抜ける (|| true で消すと、止められなかったのに「止めた」と出た)。走っていない (rc 3「No such process」。実測) は成功
var bootoutLine = fmt.Sprintf(`if [ -n "$out" ]; then rc=0; launchctl bootout "gui/$(id -u)/%s" 2>/dev/null || rc=$?
if [ "$rc" != 0 ] && [ "$rc" != 3 ]; then echo "mac-lan-manager: 名前の広告を止められない (launchctl bootout rc=$rc)" >&2; exit "$rc"; fi; fi
`, WiredNameLabel)

// jobArgs は走っている job の形の目印・名前・IP を読む sh の断片 (launchctl print の arguments の 4・7・8 番目。sh -c のループの形で
// この並びになることは macOS 27.0.1 の launchctl submit で実測した。state はタブ 1 つ下げの行だけを見る: 1 段深い所にも
// state = active が出る。実測)。走っていなければ st は空。
var jobArgs = fmt.Sprintf(`out=$(launchctl print "gui/$(id -u)/%s" 2>/dev/null) || out=
st=$(printf '%%s\n' "$out" | awk -F' = ' '/^\tstate = /{print $2; exit}')
arg() { printf '%%s\n' "$out" | awk -v k="$1" '/^\targuments = \{/{a=1;n=0;next} a&&/^\t\}/{exit} a{n++; if(n==k){gsub(/^[ \t]+/,""); print; exit}}'; }
tg=$(arg 4); nm=$(arg 7); ip=$(arg 8)
`, WiredNameLabel)

// RemoveScript は name を広告している LaunchAgent を止めて消す sh (無ければ何もしない)。別の名前を広告していれば消さずに抜ける
// (台帳に古い名前の行が残った Mac で、その行を外すと、改名した後の今の広告を止めた)。
func RemoveScript(name string) string {
	return fmt.Sprintf(`p=%s
%sif [ -z "$nm" ] && [ -f "$p" ]; then nm=$(plutil -extract ProgramArguments.6 raw -o - "$p" 2>/dev/null) || nm=; fi
if [ -n "$nm" ] && [ "$nm" != %s ]; then echo "mac-lan-manager: 別の名前 ($nm) を広告している (止めない)" >&2; exit 4; fi # ExitOtherName
%srm -f "$p"
`, plistPath, jobArgs, Quote(name), bootoutLine)
}

// ExitOtherName は RemoveScript が「別の名前を広告している (止めない)」で抜けるときの終了コード。呼び出し側は手で止める案内を出さない
// (案内どおりに止めると、守ったはずの今の広告を消した)。
const ExitOtherName = 4

// RemoveHint は相手で手で止めるときの 1 行 (ssh が届かなかったときに知らせる。bootout だけだと、次のログインで plist から生き返る)。
func RemoveHint() string {
	return "launchctl bootout gui/$(id -u)/" + WiredNameLabel + "; rm ~/Library/LaunchAgents/" + WiredNameLabel + ".plist"
}

// StatusScript は広告の状態を 4 行で出す sh: 1 行目は none (job も plist も無い) / running / stopped、2・3 行目は走っている job の
// 名前と IP、4 行目は形の目印 (走っていなければ 2〜4 行目は空)。
func StatusScript() string {
	return fmt.Sprintf(`p=%s
%sif [ -z "$out" ] && [ ! -f "$p" ]; then echo none; exit 0; fi
if [ -n "$out" ] && { [ -z "$st" ] || [ -z "$nm" ]; }; then echo unknown; exit 0; fi # 読み込まれているのに読めない形 (ParseStatus が err にする)
if [ "$st" = running ]; then echo running; else echo stopped; fi
printf '%%s\n%%s\n%%s\n' "$nm" "$ip" "$tg"
`, plistPath, jobArgs)
}

// NameState は広告の今の状態。
type NameState struct {
	Running bool
	Name    string
	IP      string
	Tag     string // 走っている job の形の目印 (AdvertTag と違えば前の形)
}

// Advertises はこの状態が a を、今の形 (IP を失ったら広告を止めるループ) で広告しているか。前の形 (dns-sd を直接起こす) は済みにしない。
func (s NameState) Advertises(a NameAdvert) bool {
	return s.Running && s.Name == a.Name && s.IP == a.IP && s.Tag == AdvertTag
}

// ParseStatus は StatusScript の出力を読む。知らない形は err (読めなかった。済みにも未実行にも倒さない)。
func ParseStatus(out string) (NameState, error) {
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n") // 末尾の改行は 1 つだけ削る (走っていないときの空の 2〜4 行目を残す)
	switch {
	case len(lines) == 1 && lines[0] == "none":
		return NameState{}, nil
	case len(lines) == 4 && (lines[0] == "running" || lines[0] == "stopped"):
		return NameState{Running: lines[0] == "running", Name: strings.TrimSpace(lines[1]), IP: strings.TrimSpace(lines[2]),
			Tag: strings.TrimSpace(lines[3])}, nil
	}
	return NameState{}, fmt.Errorf("広告の状態を読めない: %q", out)
}

// ReadNameState はこの Mac の広告の状態を読む (run は sh -c を起こす口。live は本物、テストは偽物)。
func ReadNameState(ctx context.Context, run Run) (NameState, error) {
	out, err := run(ctx, []string{"sh", "-c", StatusScript()})
	if err != nil {
		return NameState{}, err
	}
	return ParseStatus(out)
}

// UnadvertiseArgv は相手の有線の名前の広告を止めて消す ssh (鍵で入る。端末は要らない)。
func UnadvertiseArgv(d sshdest.Dest, name string) ([]string, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	if err := (NameAdvert{Name: name, IP: "0.0.0.0"}).Validate(); err != nil {
		return nil, err
	}
	return d.SSHArgv(false, true, "sh -c "+Quote(RemoveScript(name))), nil
}
