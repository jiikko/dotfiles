package tmpclean

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// areaCount は 1 つの領域の集計 (読み取りだけ)。
type areaCount struct {
	Root       string        `json:"root"`
	Total      int           `json:"total"`      // 根の配下の全部 (ファイルも dir も。根自身は数えない)
	TopLevel   int           `json:"top_level"`  // 根の直下
	EmptyDirs  int           `json:"empty_dirs"` // 根の直下の空の dir
	Bytes      int64         `json:"bytes"`      // 見えたファイルの大きさの合計
	Unreadable int           `json:"unreadable"` // 読めなかった項目 (macOS が保護している置き場など)
	Prefixes   []prefixCount `json:"prefixes,omitempty"`
}

type prefixCount struct {
	Prefix string `json:"prefix"`
	Count  int    `json:"count"`
}

type countReport struct {
	Session      areaCount  `json:"session"`
	Tmpdir       *areaCount `json:"tmpdir,omitempty"`
	OtherPrivate int        `json:"other_private_tmp"` // /private/tmp の直下で claude-<uid> 以外 (消さない)
}

// randomSuffix は mktemp などが名前の末尾に付ける乱数の部分 (接頭辞でまとめるときに落とす)。
var randomSuffix = regexp.MustCompile(`[0-9A-Za-z]{6,}$`)

func countArea(root string, prefixes bool) areaCount {
	c := areaCount{Root: root}
	byPrefix := map[string]int{}
	for _, name := range readDirSorted(root) {
		c.TopLevel++
		if prefixes {
			byPrefix[randomSuffix.ReplaceAllString(name, "")]++
		}
		p := filepath.Join(root, name)
		if ents, err := os.ReadDir(p); err == nil && len(ents) == 0 && isRealDir(p) {
			c.EmptyDirs++
		}
	}
	_ = filepath.WalkDir(root, func(path string, de fs.DirEntry, err error) error {
		if err != nil {
			c.Unreadable++
			if de != nil && de.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if path == root {
			return nil
		}
		c.Total++
		if de.Type().IsRegular() {
			if fi, err := de.Info(); err == nil {
				c.Bytes += fi.Size()
			}
		}
		return nil
	})
	for k, v := range byPrefix {
		c.Prefixes = append(c.Prefixes, prefixCount{Prefix: k, Count: v})
	}
	sort.Slice(c.Prefixes, func(i, j int) bool {
		if c.Prefixes[i].Count != c.Prefixes[j].Count {
			return c.Prefixes[i].Count > c.Prefixes[j].Count
		}
		return c.Prefixes[i].Prefix < c.Prefixes[j].Prefix
	})
	if len(c.Prefixes) > 15 {
		c.Prefixes = c.Prefixes[:15]
	}
	return c
}

func count(d deps) (countReport, error) {
	r, err := resolveRoots(d, false)
	if err != nil {
		return countReport{}, err
	}
	rep := countReport{Session: countArea(r.session, false)}
	if r.tmpdir != "" {
		t := countArea(r.tmpdir, true)
		rep.Tmpdir = &t
	}
	parent := filepath.Dir(r.session)
	for _, name := range readDirSorted(parent) {
		if filepath.Join(parent, name) != r.session && !strings.HasPrefix(name, ".") {
			rep.OtherPrivate++
		}
	}
	return rep, nil
}
