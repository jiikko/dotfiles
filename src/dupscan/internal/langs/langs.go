// Package langs は adapter の一覧と、ファイルの拡張子でどの adapter が読むかを決める。
//
// コマンドはここだけを見る (特定の言語の package を直に import しない)。言語を足すときは All に 1 つ足す。
package langs

import (
	"fmt"
	"path"

	"dupscan/internal/lang"
	"dupscan/internal/lang/golang"
	"dupscan/internal/lang/swift"
)

// All は adapter の一覧。1 つの拡張子は 1 つの adapter だけが持つ (TestExtensionsAreUnique)。
var All = []lang.Adapter{swift.Adapter, golang.Adapter}

// Extensions は全部の adapter が読む拡張子 (走査でこれ以外のファイルは読まない)。
func Extensions() []string {
	var out []string
	for _, a := range All {
		out = append(out, a.Extensions...)
	}
	return out
}

// Symbols は def / refs が読む拡張子 (定義と参照を集める adapter のもの) と、集めない adapter。
func Symbols() (exts []string, skipped []lang.Adapter) {
	for _, a := range All {
		if a.Symbols {
			exts = append(exts, a.Extensions...)
		} else {
			skipped = append(skipped, a)
		}
	}
	return exts, skipped
}

// For は file を読む adapter。
func For(file string) (lang.Adapter, bool) {
	ext := path.Ext(file)
	for _, a := range All {
		for _, e := range a.Extensions {
			if e == ext {
				return a, true
			}
		}
	}
	return lang.Adapter{}, false
}

// IsTestPath は file を読む adapter の慣習で test のファイルか。読む adapter が無ければ false。
func IsTestPath(file string) bool {
	a, ok := For(file)
	return ok && a.IsTestPath(file)
}

// ToolIgnoredOnly は files が 1 つ以上あり、全部がその言語のツールの読まない置き場 (lang.Adapter.ToolIgnores) にあるか。
func ToolIgnoredOnly(files []string) bool {
	for _, f := range files {
		a, ok := For(f)
		if !ok || a.ToolIgnores == nil || !a.ToolIgnores(f) {
			return false
		}
	}
	return len(files) > 0
}

// Parsers は 1 つの goroutine の中で使う、adapter ごとの読み手 (初めて読む言語のときに作る)。
type Parsers struct {
	opt lang.Options
	ps  map[string]lang.Parser
}

// NewParsers は opt で読む Parsers を作る。
func NewParsers(opt lang.Options) *Parsers {
	return &Parsers{opt: opt, ps: map[string]lang.Parser{}}
}

// Parse は file をその拡張子の adapter で読む。
func (p *Parsers) Parse(file string, src []byte) (lang.FileResult, error) {
	a, ok := For(file)
	if !ok {
		return lang.FileResult{}, fmt.Errorf("%s を読む adapter が無い", file)
	}
	r, ok := p.ps[a.Name]
	if !ok {
		var err error
		if r, err = a.NewParser(p.opt); err != nil {
			return lang.FileResult{}, err
		}
		p.ps[a.Name] = r
	}
	return r.Parse(file, src)
}

// Close は作った読み手を全部閉じる。
func (p *Parsers) Close() {
	for _, r := range p.ps {
		r.Close()
	}
}
