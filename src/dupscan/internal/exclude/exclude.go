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
	"strings"

	"dupscan/internal/group"
	"dupscan/internal/norm"
)

// FileName は設定ファイルの名前 (repo の root に置く)。
const FileName = ".dupscan.json"

// Rule は除外 1 つ。Paths / Group / Unit のどれか 1 つを持つ。
type Rule struct {
	Paths  []string `json:"paths,omitempty"`
	Levels []string `json:"levels,omitempty"`
	Group  []string `json:"group,omitempty"`
	Level  string   `json:"level,omitempty"`
	Unit   string   `json:"unit,omitempty"`
	Kind   string   `json:"kind"`
	Reason string   `json:"reason"`

	levels map[norm.Level]bool // Paths の除外を効かせる段 (Levels が無ければ全部)
	level  norm.Level          // Group の除外の段
}

// Config は設定ファイルの中身。
type Config struct {
	Version int    `json:"version"`
	Exclude []Rule `json:"exclude"`
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
	for i := range c.Exclude {
		if err := c.Exclude[i].check(); err != nil {
			return nil, fmt.Errorf("%s: exclude[%d]: %w", FileName, i, err)
		}
	}
	return &c, nil
}

func (r *Rule) check() error {
	n := 0
	for _, has := range []bool{len(r.Paths) > 0, len(r.Group) > 0, r.Unit != ""} {
		if has {
			n++
		}
	}
	if n != 1 {
		return errors.New("paths / group / unit のどれか 1 つだけを書く")
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
			if g == "" || strings.HasPrefix(g, "/") {
				return fmt.Errorf("paths の %q は repo の root からの相対の glob で書く", g)
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

var ordinals = regexp.MustCompile(`#(closure)?[0-9]+`)

// Pin は単位を指す釘: 名前の付いた単位の鍵 (クロージャの出現の順番と多重定義の #n を除く) と本体 (L1) のハッシュの先頭 12 文字。
// 順番を入れないので、前にクロージャや多重定義を足しても外れない。本体を直すと外れる。
func Pin(e *group.Entry) string {
	h := e.Result.Hash[norm.L1]
	return ordinals.ReplaceAllString(e.Unit.Key, "") + "@" + h[:min(len(h), 12)]
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
	b, _ := json.Marshal(Rule{Group: pins, Level: lv.String()})
	return string(b)
}
