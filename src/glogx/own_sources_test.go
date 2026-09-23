package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// ownSourceRoots は「glogx の画面に出るコード」の置き場所。glogx 本体と、go.mod の replace で
// 取り込む tuikit (幅の単一情報源 termwidth と、演出・合成の部品がそこにある)。
//
// 🚨 表示の不変条件を走査で守る検査 (VS16 リテラル / 2 本目の幅エンジン) はここを回すこと。
// "." だけを回すと、tuikit へ移した部品が黙って検査対象から外れる。
var ownSourceRoots = []string{".", filepath.Join("..", "tuikit"), filepath.Join("..", "doctor")}

// walkOwnSources は ownSourceRoots を順に filepath.WalkDir し、各エントリで fn を呼ぶ。
// どれかの根で .go を 1 つも見なかったら落とす (根が移動・改名されると、その根の検査が
// 「違反 0 件 = 緑」に化けるため)。
func walkOwnSources(t *testing.T, fn fs.WalkDirFunc) error {
	t.Helper()
	for _, root := range ownSourceRoots {
		seen := 0
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(path, ".go") {
				seen++
			}
			return fn(path, d, err)
		})
		if err != nil {
			return err
		}
		if seen == 0 {
			t.Fatalf("%s で .go を 1 つも見なかった (根が消えたか、走査が壊れている)", root)
		}
	}
	return nil
}

// ownSourceExcluded は go.mod で replace しているが、表示の不変条件の走査に**入れない** module と
// その理由。ここにも ownSourceRoots にも無い replace は TestOwnSourceRootsCoverLocalReplaces が落とす。
var ownSourceExcluded = map[string]string{
	"../termsafe": "VS16 の除去処理の実装そのもの (VS16 のリテラルを正当に含む)",
}

// ownSourceRoots の正本は go.mod の replace (ローカルの module を取り込んだら、その module は
// glogx の画面に何かを出しうる)。根を一覧から外す退行と、module を足したときの入れ忘れを止める。
// 🚨 「根ごとに .go が 0 件なら落とす」(walkOwnSources) は、一覧に**載っている**根しか見ないので
// これを代わりにできない (tuikit を一覧から消しても 2 つの走査は緑のまま通った。敵対レビュー実測)。
func TestOwnSourceRootsCoverLocalReplaces(t *testing.T) {
	src, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	roots := map[string]bool{}
	for _, r := range ownSourceRoots {
		roots[filepath.ToSlash(r)] = true
	}
	replaced := localReplaces(string(src))
	if len(replaced) == 0 {
		t.Fatal("go.mod からローカルの replace を 1 つも読めなかった (読み方が壊れている)")
	}
	for mod, p := range replaced {
		if roots[p] {
			continue
		}
		if _, ok := ownSourceExcluded[p]; !ok {
			t.Errorf("replace %s => %s が走査の根 (ownSourceRoots) にも除外 (ownSourceExcluded) にも無い", mod, p)
		}
	}
	// 除外に go.mod に無いパスが残っていたら、それは古いエントリ (消した module の理由が残り、
	// 同じ名前の module を足し直したとき黙って走査の外に置かれる)
	inMod := map[string]bool{}
	for _, p := range replaced {
		inMod[p] = true
	}
	for p := range ownSourceExcluded {
		if !inMod[p] {
			t.Errorf("ownSourceExcluded の %s は go.mod の replace に無い (古いエントリ)", p)
		}
	}
}

// localReplaces は go.mod のローカルの replace (パスが ./ か ../ で始まるもの) を module → パスで返す。
// 1 行の形 (`replace a => ../a`) とブロックの形 (`replace ( ... )`)、行末のコメント、両辺の版
// (`a v1 => ../a`) を扱う。
func localReplaces(gomod string) map[string]string {
	out := map[string]string{}
	inBlock := false
	for _, line := range strings.Split(gomod, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		f := strings.Fields(line)
		switch {
		case len(f) == 0:
			continue
		case inBlock && f[0] == ")":
			inBlock = false
			continue
		case !inBlock && f[0] == "replace" && len(f) == 2 && f[1] == "(":
			inBlock = true
			continue
		case !inBlock && f[0] == "replace":
			f = f[1:]
		case !inBlock:
			continue
		}
		// f = <mod> [ver] => <path> [ver]
		arrow := slices.Index(f, "=>")
		if arrow < 1 || arrow+1 >= len(f) {
			continue
		}
		p := f[arrow+1]
		if strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../") {
			out[f[0]] = filepath.ToSlash(filepath.Clean(p))
		}
	}
	return out
}

// localReplaces の読み方を、go.mod に現れうる形で固定する (本物の go.mod は 1 行の形しか無いので、
// ほかの形はここでしか通らない)。
func TestLocalReplacesForms(t *testing.T) {
	got := localReplaces(`module m

replace a => ../a
replace b v1.2.3 => ../b // 版とコメント付き
replace c => github.com/x/c v1.0.0
replace (
	d => ./d
	e v0.1.0 => ../e v0.1.0
	f => example.com/f v1
)
`)
	want := map[string]string{"a": "../a", "b": "../b", "d": "d", "e": "../e"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %q, want %q", k, got[k], v)
		}
	}
}
