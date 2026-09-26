package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ownSourceRoots は「glogx の画面に出るコード」の置き場所。glogx 本体と、go.mod の replace で
// 取り込む tuikit (幅の単一情報源 termwidth と、演出・合成の部品がそこにある)・doctor・
// ratelimit (利用枠の盤と表の描画 = usage パッケージ)。
//
// 🚨 表示の不変条件を走査で守る検査 (VS16 リテラル / 2 本目の幅エンジン) はここを回すこと。
// "." だけを回すと、tuikit へ移した部品が黙って検査対象から外れる。
var ownSourceRoots = []string{".", filepath.Join("..", "tuikit"), filepath.Join("..", "doctor"), filepath.Join("..", "ratelimit")}

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
	"../termsafe":   "VS16 の除去処理の実装そのもの (VS16 のリテラルを正当に含む)",
	"../subproc":    "外部プロセス起動の安全弁だけで、画面に出す文字列を持たない",
	"../atomicfile": "ファイルの atomic な置き換えだけで、画面に出す文字列を持たない",
}

// ownSourceRoots の正本は go.mod の replace (ローカルの module を取り込んだら、その module は
// glogx の画面に何かを出しうる)。根を一覧から外す退行と、module を足したときの入れ忘れを止める。
// 🚨 「根ごとに .go が 0 件なら落とす」(walkOwnSources) は、一覧に**載っている**根しか見ないので
// これを代わりにできない (tuikit を一覧から消しても 2 つの走査は緑のまま通った。敵対レビュー実測)。
func TestOwnSourceRootsCoverLocalReplaces(t *testing.T) {
	roots := map[string]bool{}
	for _, r := range ownSourceRoots {
		roots[filepath.ToSlash(r)] = true
	}
	replaced := localReplaces(t)
	if len(replaced) == 0 {
		t.Fatal("go.mod からローカルの replace を 1 つも読めなかった (読み方が壊れている)")
	}
	inMod := map[string]bool{}
	for _, r := range replaced {
		inMod[r.path] = true
		if roots[r.path] {
			continue
		}
		if _, ok := ownSourceExcluded[r.path]; !ok {
			t.Errorf("replace %s => %s が走査の根 (ownSourceRoots) にも除外 (ownSourceExcluded) にも無い", r.mod, r.path)
		}
	}
	// 除外に go.mod に無いパスが残っていたら、それは古いエントリ (消した module の理由が残り、
	// 同じ名前の module を足し直したとき黙って走査の外に置かれる)
	for p := range ownSourceExcluded {
		if !inMod[p] {
			t.Errorf("ownSourceExcluded の %s は go.mod の replace に無い (古いエントリ)", p)
		}
	}
}

type localReplace struct{ mod, path string }

// localReplaces は go.mod のローカルの replace を返す (同じ module が版違いで複数あれば全部)。
//
// 読むのは go 自身 (`go mod edit -json`)。自前で字句を読むと、go が受け付ける形 (空白の無い
// `replace(`・引用符・絶対パス・末尾の / が無い `..`) を黙って読み落とした (敵対レビュー実測)。
// ローカルの replace = 置き換え先に版が無いもの (go の規則)。パスは glogx からの相対へ揃える。
func localReplaces(t *testing.T) []localReplace {
	t.Helper()
	out, err := exec.Command("go", "mod", "edit", "-json").Output()
	if err != nil {
		t.Fatalf("go mod edit -json が失敗した: %v", err)
	}
	var mod struct {
		Replace []struct {
			Old struct{ Path string }
			New struct{ Path, Version string }
		}
	}
	if err := json.Unmarshal(out, &mod); err != nil {
		t.Fatalf("go mod edit -json の出力を読めない: %v", err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rs := make([]localReplace, 0, len(mod.Replace))
	for _, r := range mod.Replace {
		if r.New.Version != "" {
			continue // module の置き換え (ローカルのディレクトリではない)
		}
		p := r.New.Path
		if filepath.IsAbs(p) {
			if rel, err := filepath.Rel(wd, p); err == nil {
				p = rel
			}
		}
		rs = append(rs, localReplace{mod: r.Old.Path, path: filepath.ToSlash(filepath.Clean(p))})
	}
	return rs
}
