package setup

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// 有線の名前 (`<ホスト名>.wired.local`): 各 Mac が自分の有線の固定 IP を Bonjour に広告する (2026-10-17 ユーザー決定)。
// `air5.local` は Bonjour が有線と Wi-Fi の両方の IP を返し、どちらへ繋ぐかが決まらない。人が ssh / 画面共有 / smb で打つ名前を有線に
// 向けるためのもの (lan-manager 自身は IP で繋ぐ)。
//
// - root は要らない: ユーザーの LaunchAgent が /usr/bin/dns-sd -P を起こし続ける。名前は登録したプロセスが生きている間だけ残る
//   (mDNSResponder に固定の名前を持たせる設定は無い)。
// - `.local` で終わらない名前 (`air5.wired`) は Bonjour に問い合わせられない (/etc/resolver を root で書く必要がある。実測)。
// - 済みの判定は名前の解決ではなく、広告する Mac の launchd が走らせている job の実際の引数で見る: 止めた名前も問い合わせた側の
//   キャッシュに約 4 分 (TTL 240) 残った (実測)。plist の中身では見ない (入れ直しの bootstrap が失敗すると、新しい plist の横で古い
//   引数の job が走り続け、新しい IP で済みと読んだ。敵対レビュー)。
// - 受容 (未確認): 同じ名前を別の Mac が広告していると Bonjour が改名する。dns-sd がそのとき落ちるか走り続けるかは確かめていない
//   (走り続けるなら済みと読む)。帯・IP を変えた後の古い広告は、初期設定を開き直して名前の手順まで進めるまで残る。
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

// NameAdvert は広告する名前と IP。
type NameAdvert struct {
	Name string // "air5.wired.local"
	IP   string
}

// Validate は sh と plist に埋め込む値を確かめる。
func (a NameAdvert) Validate() error {
	label, ok := strings.CutSuffix(a.Name, ".wired.local")
	if !ok || !reLabel.MatchString(label) {
		return fmt.Errorf("有線の名前が使えない: %q", a.Name)
	}
	if !ipv4(a.IP) {
		return fmt.Errorf("IPv4 でない: %q", a.IP)
	}
	return nil
}

// args は dns-sd の引数 (plist の ProgramArguments)。StatusScript は 6 番目 (名前) と 7 番目 (IP) を読む。
func (a NameAdvert) args() []string {
	return []string{"/usr/bin/dns-sd", "-P", strings.TrimSuffix(a.Name, ".local"), wiredService, "local", "9", a.Name, a.IP}
}

// plistPath は sh の中で使う plist の置き場 ($HOME を sh に展開させる)。
const plistPath = `"$HOME/Library/LaunchAgents/` + WiredNameLabel + `.plist"`

// InstallScript は LaunchAgent を書いて (書き換えて) 起こし直す sh。sudo は要らない。gui のドメインに入れるので、その Mac に
// ログインしている間に打つ (ssh からでも入る。air5 で実測)。ログインしていなければ bootstrap が失敗して非 0 で抜ける。
func (a NameAdvert) InstallScript() string {
	var items strings.Builder
	for _, s := range a.args() {
		items.WriteString("<string>" + s + "</string>") // 名前は英数字・ハイフン・点、IP は数字と点だけ (Validate)。XML の実体参照は要らない
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
// (誰もログインしていない) ときは打たない: 打つと 3 以外で落ち、消すはずの plist を残した。敵対レビュー)。止められなければ理由を出して
// 抜ける (|| true で消すと、止められなかったのに「止めた」と出た)。走っていない (rc 3「No such process」。実測) は成功
var bootoutLine = fmt.Sprintf(`if [ -n "$out" ]; then rc=0; launchctl bootout "gui/$(id -u)/%s" 2>/dev/null || rc=$?
if [ "$rc" != 0 ] && [ "$rc" != 3 ]; then echo "mac-lan-manager: 名前の広告を止められない (launchctl bootout rc=$rc)" >&2; exit "$rc"; fi; fi
`, WiredNameLabel)

// jobArgs は走っている job の名前と IP を読む sh の断片 (launchctl print の arguments の 7・8 番目。state はタブ 1 つ下げの行だけを見る:
// 1 段深い所にも state = active が出る。実測)。走っていなければ st は空。
var jobArgs = fmt.Sprintf(`out=$(launchctl print "gui/$(id -u)/%s" 2>/dev/null) || out=
st=$(printf '%%s\n' "$out" | awk -F' = ' '/^\tstate = /{print $2; exit}')
nm=$(printf '%%s\n' "$out" | awk '/^\targuments = \{/{a=1;n=0;next} a&&/^\t\}/{exit} a{n++; if(n==7){gsub(/^[ \t]+/,""); print; exit}}')
ip=$(printf '%%s\n' "$out" | awk '/^\targuments = \{/{a=1;n=0;next} a&&/^\t\}/{exit} a{n++; if(n==8){gsub(/^[ \t]+/,""); print; exit}}')
`, WiredNameLabel)

// RemoveScript は name を広告している LaunchAgent を止めて消す sh (無ければ何もしない)。別の名前を広告していれば消さずに抜ける
// (台帳に古い名前の行が残った Mac で、その行を外すと、改名した後の今の広告を止めた。敵対レビュー)。
func RemoveScript(name string) string {
	return fmt.Sprintf(`p=%s
%sif [ -z "$nm" ] && [ -f "$p" ]; then nm=$(plutil -extract ProgramArguments.6 raw -o - "$p" 2>/dev/null) || nm=; fi
if [ -n "$nm" ] && [ "$nm" != %s ]; then echo "mac-lan-manager: 別の名前 ($nm) を広告している (止めない)" >&2; exit 4; fi # ExitOtherName
%srm -f "$p"
`, plistPath, jobArgs, Quote(name), bootoutLine)
}

// ExitOtherName は RemoveScript が「別の名前を広告している (止めない)」で抜けるときの終了コード。呼び出し側は手で止める案内を出さない
// (案内どおりに止めると、守ったはずの今の広告を消した。敵対レビュー)。
const ExitOtherName = 4

// RemoveHint は相手で手で止めるときの 1 行 (ssh が届かなかったときに知らせる。bootout だけだと、次のログインで plist から生き返る)。
func RemoveHint() string {
	return "launchctl bootout gui/$(id -u)/" + WiredNameLabel + "; rm ~/Library/LaunchAgents/" + WiredNameLabel + ".plist"
}

// StatusScript は広告の状態を 3 行で出す sh: 1 行目は none (job も plist も無い) / running / stopped、2・3 行目は走っている job の
// 名前と IP (走っていなければ空)。
func StatusScript() string {
	return fmt.Sprintf(`p=%s
%sif [ -z "$out" ] && [ ! -f "$p" ]; then echo none; exit 0; fi
if [ -n "$out" ] && { [ -z "$st" ] || [ -z "$nm" ]; }; then echo unknown; exit 0; fi # 読み込まれているのに読めない形 (ParseStatus が err にする)
if [ "$st" = running ]; then echo running; else echo stopped; fi
printf '%%s\n%%s\n' "$nm" "$ip"
`, plistPath, jobArgs)
}

// NameState は広告の今の状態。
type NameState struct {
	Running bool
	Name    string
	IP      string
}

// Advertises はこの状態が a を広告しているか。
func (s NameState) Advertises(a NameAdvert) bool {
	return s.Running && s.Name == a.Name && s.IP == a.IP
}

// ParseStatus は StatusScript の出力を読む。知らない形は err (読めなかった。済みにも未実行にも倒さない)。
func ParseStatus(out string) (NameState, error) {
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n") // 末尾の改行は 1 つだけ削る (走っていないときの空の 2・3 行目を残す)
	switch {
	case len(lines) == 1 && lines[0] == "none":
		return NameState{}, nil
	case len(lines) == 3 && (lines[0] == "running" || lines[0] == "stopped"):
		return NameState{Running: lines[0] == "running", Name: strings.TrimSpace(lines[1]), IP: strings.TrimSpace(lines[2])}, nil
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
func UnadvertiseArgv(d Dest, name string) ([]string, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	if err := (NameAdvert{Name: name, IP: "0.0.0.0"}).Validate(); err != nil {
		return nil, err
	}
	return d.SSHArgv(false, true, "sh -c "+Quote(RemoveScript(name))), nil
}
