// Package exclude は repo の root の .dupscan.json に書いた除外 (誤検知・意図して分けている写し) を読み、report と gate に同じ規則で効かせる。
//
// 除外は、書いたときにあった単位にだけ効く。止める手段は「新しく足された除外」を人が見ることだけなので、まだ無い単位を先回りで覆える
// 形を作らない: 単位は釘 (鍵 + 本体のハッシュ) で指し、1 つの釘が 2 つ以上の単位に当たる除外は効かせない。
package exclude

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"dupscan/internal/group"
	"dupscan/internal/jsonout"
	"dupscan/internal/norm"
)

// FileName は設定ファイルの名前 (repo の root に置く)。
const FileName = ".dupscan.json"

// Rule は除外 1 つ。Paths / Group / Unit / Preset のどれか 1 つを持つ。
type Rule struct {
	Paths []string `json:"paths,omitempty"`
	// Preset は形で組を外す既定の規則の名前 (Presets のどれか)。パスの除外と同じく、書いた後にできた単位にも効く。
	Preset string   `json:"preset,omitempty"`
	Levels []string `json:"levels,omitempty"`
	Group  []string `json:"group,omitempty"`
	Level  string   `json:"level,omitempty"`
	Unit   string   `json:"unit,omitempty"`
	Kind   string   `json:"kind"`
	Reason string   `json:"reason"`
	// Comment は単位の直前のコメントから作った許可の場所 (`file:line`)。設定ファイルには書けない。
	Comment string `json:"comment,omitempty"`

	levels map[norm.Level]bool // Paths の除外を効かせる段 (Levels が無ければ全部)
	level  norm.Level          // Group の除外の段
	// groupMiss は `dupscan:allow-group <人数>` の人数と、同じ本体の単位の数が合わなかったときの、段ごとの数 (合えば空)。
	// 合わない許可は何も消さず、効いていない除外に出す (増えた写しを、許可が黙って覆わない)。
	groupMiss string
	// allowID は allow-group から作った除外の出どころ (コメントの中身: 人数・種別・理由)。gate が、base から変わっていない許可を見分ける。
	// 書いた単位の鍵は入れない (ファイルの移動・型や関数の改名ではコメントの行が diff に出ないので、それで「新しい許可」にしない)。
	allowID string
}

// GroupMiss は `dupscan:allow-group` の人数が合わなかったときの、段ごとの同じ本体の単位の数 (合っていれば空)。
func (r Rule) GroupMiss() string { return r.groupMiss }

// Config は設定ファイルの中身。
type Config struct {
	Version int    `json:"version"`
	Exclude []Rule `json:"exclude"`
	// Shared は共通の置き場所 (使い回すヘルパーを置くファイル) の glob。index がそこの定義を先に並べるだけで、report と gate の判定には効かない
	// (gate を緩めないので、exclude と違い base から増えた分を gate の注記に出さない)。
	Shared []string `json:"shared,omitempty"`
}

// InShared は file が共通の置き場所の glob のどれかに当たるか。
func (c *Config) InShared(file string) bool {
	if c == nil {
		return false
	}
	for _, g := range c.Shared {
		if Match(g, file) {
			return true
		}
	}
	return false
}

// Parse は設定ファイルを読む。形の誤り・知らない欄・種別か理由の無い除外はエラー (黙って除外無しで回さない)。
func Parse(b []byte) (*Config, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("%s: %w", FileName, err)
	}
	if dec.More() {
		return nil, fmt.Errorf("%s: JSON の後に余計な中身がある", FileName)
	}
	if c.Version != 1 {
		return nil, fmt.Errorf("%s: version は 1 (%d が書かれている)", FileName, c.Version)
	}
	for _, g := range c.Shared {
		if err := checkGlob(g); err != nil {
			return nil, fmt.Errorf("%s: shared: %w", FileName, err)
		}
	}
	for i := range c.Exclude {
		if c.Exclude[i].Comment != "" {
			return nil, fmt.Errorf("%s: exclude[%d]: comment は書けない (単位の直前のコメントから作る欄)", FileName, i)
		}
		if err := c.Exclude[i].check(); err != nil {
			return nil, fmt.Errorf("%s: exclude[%d]: %w", FileName, i, err)
		}
	}
	return &c, nil
}

func (r *Rule) check() error {
	n := 0
	for _, has := range []bool{len(r.Paths) > 0, len(r.Group) > 0, r.Unit != "", r.Preset != ""} {
		if has {
			n++
		}
	}
	if n != 1 {
		return errors.New("paths / group / unit / preset のどれか 1 つだけを書く")
	}
	if r.Preset != "" && !knownPreset(r.Preset) {
		return fmt.Errorf("知らない preset %q (使えるのは %s)", r.Preset, strings.Join(Presets, " / "))
	}
	if r.Kind != "false-positive" && r.Kind != "intentional" {
		return fmt.Errorf("kind は false-positive か intentional (%q)", r.Kind)
	}
	if strings.TrimSpace(r.Reason) == "" {
		return errors.New("reason が無い")
	}
	if len(r.Levels) > 0 && len(r.Paths) == 0 {
		return errors.New("levels は paths とだけ書く")
	}
	if r.Level != "" && len(r.Group) == 0 {
		return errors.New("level は group とだけ書く")
	}
	r.levels = map[norm.Level]bool{}
	if len(r.Paths) > 0 {
		for _, g := range r.Paths {
			if err := checkGlob(g); err != nil {
				return fmt.Errorf("paths: %w", err)
			}
		}
		if len(r.Levels) == 0 {
			for _, lv := range norm.Levels {
				r.levels[lv] = true
			}
		}
		for _, s := range r.Levels {
			lv, err := norm.ParseLevel(s)
			if err != nil {
				return err
			}
			r.levels[lv] = true
		}
		// 細かい段だけを外しても、その単位は粗い段の組として出るので効かない。粗い側に閉じた集合だけを受け付ける。
		for lv := range r.levels {
			for _, coarser := range norm.Levels {
				if coarser > lv && !r.levels[coarser] {
					return fmt.Errorf("levels は粗い側を含める (%s を外すなら %s も)", lv, coarser)
				}
			}
		}
	}
	if len(r.Group) > 0 {
		if len(r.Group) < 2 {
			return errors.New("group には釘を 2 つ以上書く")
		}
		lv, err := norm.ParseLevel(r.Level)
		if err != nil {
			return fmt.Errorf("group の level: %w", err)
		}
		r.level = lv
	}
	for _, p := range append(append([]string(nil), r.Group...), r.Unit) {
		if p != "" && !strings.Contains(p, "@") {
			return fmt.Errorf("%q は釘ではない (<鍵>@<本体のハッシュ>。報告の json か -v に出る 1 行を貼る)", p)
		}
	}
	return nil
}

// allowGroupMark は単位の直前のコメントの `dupscan:allow` の後ろに続けて、組ごとの許可にする印 (`// dupscan:allow-group <人数> <種別> <理由>`)。
const allowGroupMark = "-group"

// CommentRules は、単位の直前のコメント `// dupscan:allow <種別> <理由>` を単位の除外にする。
// `// dupscan:allow-group <人数> <種別> <理由>` は、その単位と同じ本体の単位 (細かい段から見て、数がちょうど人数になった段) の組の除外にする。
// 人数を書かせるのは、後から写しが増えたときに許可が黙って覆わないため (コメントには相手の釘を書けないので、顔ぶれを人数で留める)。
// 設定ファイルの単位の除外と同じ釘で指すので、gate の「base に同じ釘があったときだけ効く」と「新しく足された除外」もそのまま効く
// (新しい写しに許可のコメントを添えると、新しく足された除外に出る)。種別か理由の無いコメントは errs に入れる。
func CommentRules(entries []*group.Entry) (rules []Rule, errs []error) {
	for _, e := range entries {
		u := e.Unit
		if !u.HasAllow {
			continue
		}
		// 読む印の規則は作り続ける (gate は base の誤りを捨てるので、ここで飛ばすと base の許可が丸ごと消え、変えていない許可が新しく見える)。
		if u.AllowExtraLine > 0 {
			errs = append(errs, fmt.Errorf("%s:%d: dupscan:allow: 同じ単位に印が 2 つある (読むのは %d 行目だけで、%d 行目は読まない。1 つの単位で読む印は 1 つ。段の違う 2 つの組を消すなら、2 つ目の allow-group は残る組の別のメンバーに書く)",
				u.File, u.AllowExtraLine, u.AllowLine, u.AllowExtraLine))
		}
		if rest, ok := strings.CutPrefix(u.Allow, allowGroupMark); ok {
			r, err := groupCommentRule(e, entries, rest)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s:%d: dupscan:allow-group: %w (書き方は `// dupscan:allow-group <人数> <false-positive|intentional> <理由>`)", u.File, u.AllowLine, err))
				continue
			}
			rules = append(rules, r)
			continue
		}
		kind, reason, _ := strings.Cut(u.Allow, " ")
		r := Rule{Unit: Pin(e), Kind: kind, Reason: strings.TrimSpace(reason), Comment: fmt.Sprintf("%s:%d", u.File, u.AllowLine)}
		if err := r.check(); err != nil {
			errs = append(errs, fmt.Errorf("%s: dupscan:allow: %w (書き方は `// dupscan:allow <false-positive|intentional> <理由>`)", r.Comment, err))
			continue
		}
		rules = append(rules, r)
	}
	return rules, errs
}

// groupCommentRule は `dupscan:allow-group` の後ろ rest (人数・種別・理由) から組の除外を作る。
func groupCommentRule(e *group.Entry, entries []*group.Entry, rest string) (Rule, error) {
	u := e.Unit
	comment := fmt.Sprintf("%s:%d", u.File, u.AllowLine)
	if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
		return Rule{}, fmt.Errorf("知らない印 dupscan:allow%s%s", allowGroupMark, strings.Fields(rest)[0])
	}
	fields := strings.Fields(rest)
	n, err := 0, error(nil)
	if len(fields) > 0 {
		n, err = strconv.Atoi(fields[0])
	}
	if len(fields) == 0 || err != nil || n < 2 {
		return Rule{}, errors.New("人数 (2 以上の整数) が無い")
	}
	kind, reason, _ := strings.Cut(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest), fields[0])), " ")
	reason = strings.TrimSpace(reason)
	// 種別と理由の検査は単位の除外と同じ
	if err := (&Rule{Unit: Pin(e), Kind: kind, Reason: reason}).check(); err != nil {
		return Rule{}, err
	}
	id := strconv.Itoa(n) + "\x00" + kind + "\x00" + reason
	var counts []string
	for _, lv := range append(append([]norm.Level(nil), norm.Levels...), norm.LM) {
		var pins []string
		for _, o := range entries {
			if !o.Unit.Unreadable && (lv != norm.L3 || !o.Result.L3Trivial) && o.Result.Hash[lv] == e.Result.Hash[lv] {
				pins = append(pins, Pin(o))
			}
		}
		if len(pins) == n {
			sort.Strings(pins)
			r := Rule{Group: pins, Level: lv.String(), Kind: kind, Reason: reason, Comment: comment, allowID: id}
			return r, r.check()
		}
		counts = append(counts, fmt.Sprintf("%s %d", lv, len(pins)))
	}
	return Rule{Group: []string{Pin(e)}, Level: norm.L1.String(), level: norm.L1, Kind: kind, Reason: reason, Comment: comment,
		allowID: id, groupMiss: fmt.Sprintf("人数 %d・同じ本体の単位 %s", n, strings.Join(counts, "・"))}, nil
}

// WithComments は設定 c にコメントの許可 rules を足した設定を返す (c は書き換えない)。
func WithComments(c *Config, rules []Rule) *Config {
	if len(rules) == 0 {
		return c
	}
	out := &Config{Version: 1}
	if c != nil {
		out.Exclude = append(out.Exclude, c.Exclude...)
	}
	out.Exclude = append(out.Exclude, rules...)
	return out
}

// checkGlob は repo の root からの相対の glob として読めるか。
func checkGlob(g string) error {
	if g == "" || strings.HasPrefix(g, "/") {
		return fmt.Errorf("%q は repo の root からの相対の glob で書く", g)
	}
	for _, part := range strings.Split(g, "/") {
		if _, err := path.Match(part, ""); err != nil {
			return fmt.Errorf("%q は glob として読めない: %w", g, err)
		}
	}
	return nil
}

var ordinals = regexp.MustCompile(`#(closure)?[0-9]+`)

// Pin は単位を指す釘: 名前の付いた単位の鍵 (クロージャの出現の順番と多重定義の #n を除く) と本体 (L1) のハッシュの先頭 12 文字。
// 順番を入れないので、前にクロージャや多重定義を足しても外れない。本体を直すと外れる。
// クロージャの釘は囲む関数の釘と鍵の部分が同じになる。重ならないのは、norm が根の node の種類 (lambda_literal / function_body) も
// ハッシュに入れるので、クロージャと関数の本体が同じハッシュにならないから。根の種類をハッシュから外すなら、base で比べる対象で
// なかった小さいクロージャの釘が、同じ本体の関数を覆う (gate の Set.known は base の単位が比べる対象だったかを見ない)。
func Pin(e *group.Entry) string {
	h := e.Result.Hash[norm.L1]
	file, rest, _ := strings.Cut(e.Unit.Key, "|") // ファイル名の中の #数字 は削らない
	return file + "|" + ordinals.ReplaceAllString(rest, "") + "@" + h[:min(len(h), 12)]
}

// Match は glob (`*` は 1 階層の中、`**` は 0 以上の階層) が repo の root からの相対パスに当たるか。
func Match(glob, file string) bool {
	return matchParts(strings.Split(glob, "/"), strings.Split(file, "/"))
}

func matchParts(g, f []string) bool {
	if len(g) == 0 {
		return len(f) == 0
	}
	if g[0] == "**" {
		for i := 0; i <= len(f); i++ {
			if matchParts(g[1:], f[i:]) {
				return true
			}
		}
		return false
	}
	if len(f) == 0 {
		return false
	}
	ok, err := path.Match(g[0], f[0])
	return err == nil && ok && matchParts(g[1:], f[1:])
}

// GroupSnippet は組を指す組の除外の 1 行 (kind と reason は空。空のまま貼ると読み込みでエラーになり、書き手に埋めさせる)。
func GroupSnippet(lv norm.Level, members []*group.Entry) string {
	var pins []string
	for _, m := range members {
		pins = append(pins, Pin(m))
	}
	sort.Strings(pins)
	b, _ := jsonout.Line(Rule{Group: pins, Level: lv.String()})
	return string(b)
}
