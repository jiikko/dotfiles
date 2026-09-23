package main

import (
	"io/fs"
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
