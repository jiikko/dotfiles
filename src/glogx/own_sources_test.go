package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ownSourceRoots は「glogx の画面に出るコード」の置き場所。glogx 本体と、go.mod の replace で
// 取り込む tuikit (幅の単一情報源 termwidth と、演出・合成の部品がそこにある)。
//
// 🚨 表示の不変条件を走査で守る検査 (VS16 リテラル / 2 本目の幅エンジン) はここを回すこと。
// "." だけを回すと、tuikit へ移した部品が黙って検査対象から外れる。
var ownSourceRoots = []string{".", filepath.Join("..", "tuikit")}

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
	"../doctor":   "VS16 を含む文字列が既にある (2026-09-24 時点)。走査に入れるかは未決 (glogx の部品ではなく別の CLI でもある)",
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
	local := 0
	for _, line := range strings.Split(string(src), "\n") {
		f := strings.Fields(line)
		// replace <mod> => <path> の形だけ見る (版つき / ブロック形式はこの repo に無い)
		if len(f) != 4 || f[0] != "replace" || f[2] != "=>" || !strings.HasPrefix(f[3], "../") {
			continue
		}
		local++
		p := filepath.ToSlash(filepath.Clean(f[3]))
		if roots[p] {
			continue
		}
		if _, ok := ownSourceExcluded[p]; !ok {
			t.Errorf("replace %s => %s が走査の根 (ownSourceRoots) にも除外 (ownSourceExcluded) にも無い", f[1], f[3])
		}
	}
	if local == 0 {
		t.Fatal("go.mod からローカルの replace を 1 つも読めなかった (読み方が壊れている)")
	}
}
