// Package ledger は、管理対象の Mac の台帳を読み書きする (台帳は mac-lan-manager を実行している Mac の中だけに持つ)。
//
// 画面と初期設定は Store の口だけを使い、ファイルを直接触らない。共有 (非公開の repo など) を足すときは、この口の裏に
// 置き場を足すか差し替える (今は FileStore だけ)。
package ledger

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"atomicfile"

	"userdirs"
)

// Version は書く形式の版。読んだ台帳の版がこれより新しければ書き込みを拒む (古い版で上書きして中身を失わないため)。
const Version = 1

// Host は台帳の 1 台。鍵は Host (Bonjour の .local のホスト名。表示名と IP は変わりうるので鍵にしない)。
type Host struct {
	Host      string // "m2macmini.local" (小文字・末尾の . 無しに揃える。Key を通す)
	User      string // ssh のユーザー名 (Bonjour からは取れない)
	LAN       string // 内部 IP (固定 IP)
	Alias     string // 自分で付けた別名
	UpdatedAt time.Time
}

// reUser は macOS のアカウント名として使える形 (先頭に - を置けない: ssh の引数にすると option として読まれた)。
var reUser = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,63}$`)

// ValidUser は ssh のユーザー名・chown の持ち主として使ってよい形か。
func ValidUser(s string) bool { return reUser.MatchString(s) }

// ValidLAN は内部 IP として使ってよい形か (IPv4 の 1 つの番地)。
func ValidLAN(s string) bool {
	a, err := netip.ParseAddr(s)
	return err == nil && a.Is4() && a.String() == s
}

// Unusable は台帳の行のうち、繋ぐときに使えない欄 (空は使えないに数えない)。台帳は手で書き換えうるので、読んだ側が使う前に見る。
func (h Host) Unusable() []string {
	var bad []string
	if h.User != "" && !ValidUser(h.User) {
		bad = append(bad, "user")
	}
	if h.LAN != "" && !ValidLAN(h.LAN) {
		bad = append(bad, "lan")
	}
	return bad
}

// Store は台帳の口。
type Store interface {
	Load() ([]Host, error)
	Upsert(h Host) error // 同じ鍵があれば置き換える (UpdatedAt は Store が付ける)
	Remove(host string) error
	// Update は既にある 1 台を f で書き換える (書く直前に読み直す)。台帳に無ければ ErrNotFound (外された Mac を戻さない)。
	// 一部の欄だけを変える (別名など) ときに使う。古い一覧の行で Upsert すると、他の欄を古い値で上書きする
	Update(host string, f func(*Host)) error
}

// ErrNotFound は台帳にその Mac が無い。
var ErrNotFound = errors.New("台帳にその Mac が無い")

// ErrNewerFormat は、台帳がこの版より新しい形式で書かれているので触らない、という失敗。
var ErrNewerFormat = errors.New("台帳がこの版より新しい形式で書かれている")

// Key はホスト名を台帳の鍵の形に揃える (小文字・末尾の . を落とす)。
func Key(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

// EnvDir は置き場所を差し替える環境変数 (テストが本物を触らないため)。
const EnvDir = "MAC_LAN_MANAGER_CONFIG_DIR"

// DefaultPath は台帳の置き場所 ($XDG_CONFIG_HOME/mac-lan-manager/hosts.toml、未設定なら ~/.config/mac-lan-manager/hosts.toml)。
// 前の置き場 (~/.config/glogx/mac-lan-manager/hosts.toml) にあれば初回に移す (userdirs.ConfigFile)。
func DefaultPath() (string, error) {
	if d := os.Getenv(EnvDir); d != "" {
		return filepath.Join(d, "hosts.toml"), nil
	}
	return userdirs.ConfigFile("mac-lan-manager", "hosts.toml", filepath.Join(".config", "glogx", "mac-lan-manager", "hosts.toml"))
}

// writeGuard は書き込みの直前に通す関門。本番は nil (何もしない)。テストは一時ディレクトリの外を拒否する関数を入れる
// (_claude/rules/sandbox-real-destructive-test-apis.md: 拒否は書く前に置く)。
var writeGuard func(path string) error

// FileStore はローカルのファイル 1 つの台帳。
type FileStore struct {
	Path string
	Now  func() time.Time // 「今」の注入 (nil なら time.Now)
}

func (s FileStore) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now() //nolint:forbidigo // 注入しないときの既定値 (ここだけが「今」を直に読む)
}

// Load は台帳を読む。ファイルが無ければ空。
func (s FileStore) Load() ([]Host, error) {
	b, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_, hosts, err := ParseLenient(string(b))
	return hosts, err
}

// Upsert は 1 台を足す / 置き換える。台帳のロックを取ってから読み直して書く (単体のコマンドと glogx の板が同時に書いても、片方の登録を消さない)。
func (s FileStore) Upsert(h Host) error {
	h.Host = Key(h.Host)
	if h.Host == "" {
		return errors.New("ホスト名が空")
	}
	return s.modify(func(hosts []Host) []Host {
		h.UpdatedAt = s.now().UTC().Truncate(time.Second)
		if i := slices.IndexFunc(hosts, func(x Host) bool { return x.Host == h.Host }); i >= 0 {
			hosts[i] = h
			return hosts
		}
		return append(hosts, h)
	})
}

// Update は既にある 1 台を f で書き換える。無ければ ErrNotFound (書かない)。
func (s FileStore) Update(host string, f func(*Host)) error {
	k := Key(host)
	var found bool
	err := s.modify(func(hosts []Host) []Host {
		for i := range hosts {
			if hosts[i].Host == k {
				found = true
				f(&hosts[i])
				hosts[i].Host = k // 鍵は変えさせない
				hosts[i].UpdatedAt = s.now().UTC().Truncate(time.Second)
			}
		}
		return hosts
	})
	if err == nil && !found {
		return ErrNotFound
	}
	return err
}

// Remove は 1 台を消す。無ければ何もしない。
func (s FileStore) Remove(host string) error {
	k := Key(host)
	return s.modify(func(hosts []Host) []Host {
		return slices.DeleteFunc(hosts, func(x Host) bool { return x.Host == k })
	})
}

func (s FileStore) modify(f func([]Host) []Host) error {
	if writeGuard != nil { // 実際に開くパスは本体とロックファイルの 2 つ。両方を関門に通す
		for _, p := range []string{s.Path, s.Path + ".lock"} {
			if err := writeGuard(p); err != nil {
				return err
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	unlock, err := lock(s.Path + ".lock")
	if err != nil {
		return err
	}
	defer unlock()
	var hosts []Host
	if b, err := os.ReadFile(s.Path); err == nil {
		// 版は緩く読んで先に見る (新しい版の台帳は、知らない鍵があっても「新しい版」として断る)
		if ver, _, err := ParseLenient(string(b)); err == nil && ver > Version {
			return fmt.Errorf("%w (版 %d。この版は %d まで)", ErrNewerFormat, ver, Version)
		}
		// 書き戻す前は厳しく読む: 書き直しで黙って消える中身 (知らない鍵・host の無い節・読めない時刻・鍵の重複) があれば書かない
		_, hs, err := Parse(string(b))
		if err != nil {
			return err
		}
		hosts = hs
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	hosts = f(hosts)
	return atomicfile.Write(s.Path, []byte(Format(hosts)), 0o644)
}

// lock は台帳の隣のロックファイルに排他のロック (flock) を取る。読み直しから書き終わるまでを 1 つにする。
// ロックはプロセスが死ねば OS が外すので、残骸で詰まらない (ロックファイル自体は残るが中身は使わない)。
func lock(path string) (unlock func(), err error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o644) // symlink はたどらない (外を指すロックファイルで外に作らない)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

// Format は台帳を書く形 (依存を足さない手書きの TOML の部分集合。treefiler の設定と同じ方針)。鍵の順に並べる。
func Format(hosts []Host) string {
	hs := slices.Clone(hosts)
	slices.SortFunc(hs, func(a, b Host) int { return strings.Compare(a.Host, b.Host) })
	var b strings.Builder
	fmt.Fprintf(&b, "# mac-lan-manager の台帳 (管理対象の Mac)。この Mac の中だけに持つ\nversion = %d\n", Version)
	for _, h := range hs {
		b.WriteString("\n[[host]]\n")
		kv := func(k, v string) {
			if v != "" {
				fmt.Fprintf(&b, "%s = %s\n", k, strconv.Quote(v))
			}
		}
		kv("host", h.Host)
		kv("user", h.User)
		kv("lan", h.LAN)
		kv("alias", h.Alias)
		if !h.UpdatedAt.IsZero() {
			kv("updated_at", h.UpdatedAt.UTC().Format(time.RFC3339))
		}
	}
	return b.String()
}

// Parse は台帳を厳しく読む (書き戻す前に使う)。書き直すと黙って消える中身があれば err にする:
// 知らない鍵・[[host]] の外の version 以外の鍵・version の重複・host の無い節・読めない updated_at・節の中の鍵の重複・ホストの鍵の重複。
func Parse(s string) (version int, hosts []Host, err error) { return parse(s, true) }

// ParseLenient は台帳を緩く読む (一覧に出すために使う)。知らない鍵は読み飛ばし、host の無い節と読めない時刻は捨て、
// 鍵が重複したら先の節を採る。後の版が足した項目を古い版でも表示できるように。形の壊れた行 (= が無い・値の引用が読めない) は err。
func ParseLenient(s string) (version int, hosts []Host, err error) { return parse(s, false) }

// quoted は値の先頭の引用された文字列を戻し、後ろに残った行末のコメント ("# …") を返す。コメント以外が残れば err。
func quoted(v string) (val, comment string, err error) {
	q, err := strconv.QuotedPrefix(v)
	if err != nil {
		return "", "", err
	}
	rest := strings.TrimSpace(v[len(q):])
	if rest != "" && !strings.HasPrefix(rest, "#") {
		return "", "", fmt.Errorf("値の後ろに余計なもの: %q", rest)
	}
	val, err = strconv.Unquote(q)
	return val, rest, err
}

func parse(s string, strict bool) (version int, hosts []Host, err error) {
	version = 1
	sawVersion := false
	var cur *Host
	var seen map[string]bool // 今の節で見た鍵
	for n, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == "[[host]]" {
			hosts = append(hosts, Host{})
			cur = &hosts[len(hosts)-1]
			seen = map[string]bool{}
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return 0, nil, fmt.Errorf("台帳の %d 行目が読めない: %q", n+1, line)
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if cur == nil {
			switch {
			case k == "version":
				if strict && sawVersion {
					return 0, nil, fmt.Errorf("台帳の %d 行目: version が 2 回ある", n+1)
				}
				sawVersion = true
				if !strict { // 緩い読みは行末のコメントを外す (厳しい読みは下の値と同じく、書き戻すと消えるので断る)
					v, _, _ = strings.Cut(v, "#")
					v = strings.TrimSpace(v)
				}
				if version, err = strconv.Atoi(v); err != nil {
					return 0, nil, fmt.Errorf("台帳の版が読めない: %q", v)
				}
			case strict:
				return 0, nil, fmt.Errorf("台帳の %d 行目: [[host]] の外に知らない鍵 %q がある", n+1, k)
			}
			continue
		}
		val, rest, err := quoted(v)
		if err != nil {
			return 0, nil, fmt.Errorf("台帳の %d 行目の値が読めない: %q", n+1, line)
		}
		// 行末のコメントは手で書いた台帳では合法 (TOML)。緩い読みで断ると、一覧の表示ごと止まった。厳しい読みは書き戻すと消えるので断る
		if rest != "" && strict {
			return 0, nil, fmt.Errorf("台帳の %d 行目: 行末のコメントは書き戻すと消える: %q", n+1, line)
		}
		if strict && seen[k] {
			return 0, nil, fmt.Errorf("台帳の %d 行目: 鍵 %q が同じ節に 2 回ある", n+1, k)
		}
		seen[k] = true
		switch k {
		case "host":
			cur.Host = Key(val)
		case "user":
			cur.User = val
		case "lan":
			cur.LAN = val
		case "alias":
			cur.Alias = val
		case "updated_at":
			t, err := time.Parse(time.RFC3339, val)
			if err != nil && strict {
				return 0, nil, fmt.Errorf("台帳の %d 行目: updated_at が読めない: %q", n+1, val)
			}
			if err == nil {
				cur.UpdatedAt = t
			}
		default:
			if strict {
				return 0, nil, fmt.Errorf("台帳の %d 行目: 知らない鍵 %q (この版では書き戻すと消える)", n+1, k)
			}
		}
	}
	keys := map[string]bool{}
	out := hosts[:0]
	for _, h := range hosts {
		switch {
		case h.Host == "":
			if strict {
				return 0, nil, errors.New("台帳に host の無い [[host]] がある")
			}
		case keys[h.Host]:
			if strict {
				return 0, nil, fmt.Errorf("台帳に同じホスト %q が 2 回ある", h.Host)
			}
		default:
			keys[h.Host] = true
			out = append(out, h)
		}
	}
	return version, out, nil
}
