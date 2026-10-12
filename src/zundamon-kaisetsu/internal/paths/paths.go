// Package paths はパスの解決 (Python の posixpath.realpath と同じ手順)・台本からの相対パス・作業 dir (<台本>.work)。
package paths

import (
	"os"
	"path/filepath"
	"strings"

	"zundamon-kaisetsu/internal/pyjson"
)

func IsFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

// ResolveScriptRelative は台本に書いたパス (~ と相対パス可) を、台本の場所から解いた実際のパスにする。
// 立ち絵の cast.*.faces と図解の image.src が同じ規則で解く
func ResolveScriptRelative(scriptPath, p string) string {
	p = expandUser(p)
	if !filepath.IsAbs(p) {
		p = filepath.Join(filepath.Dir(scriptPath), p)
	}
	return ResolvePath(p)
}

// WorkDir は <台本名>.work/ (Python の Path.stem と同じく、最後の拡張子だけを外す)。
func WorkDir(scriptPath string) string {
	return filepath.Join(filepath.Dir(scriptPath), pyjson.Stem(filepath.Base(scriptPath))+".work")
}

func expandUser(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// ResolvePath は Python の Path.resolve() (strict でない)。相対パスは物理的な作業ディレクトリ (symlink を解決したもの) から
// たどり、要素を 1 つずつ進めて、symlink ならリンク先へ置き換える。".." は解決した後の親へ戻る (先に字面で畳むと、
// ".." の直前が symlink のとき別のファイルを指す。敵対的レビュー P2-3)。リンク先が存在しなくても (dangling) リンク先へ
// 進む。存在しない要素から先は字面のまま足す。手順は Python の posixpath.realpath と同じで、解決中・解決済みのリンクを
// seen に記録する: 解決中のリンクに戻ってきたらループとみなしてリンクのまま進め、解決済みのリンクは結果を使い回す
// (深さだけで打ち切ると、ループと長い連鎖で結果が Python と違い、a -> a/../a/../a で指数的に時間を食う。3 周目 P3)。
func ResolvePath(p string) string {
	seen := map[string]*string{}
	start := "/"
	if !filepath.IsAbs(p) {
		wd, err := os.Getwd()
		if err != nil {
			return p
		}
		start = resolveFrom("/", wd, seen)
	}
	return resolveFrom(start, p, seen)
}

// resolveFrom は解決済みの dir cur から p の要素をたどる。seen の値が nil のリンクは解決中。
func resolveFrom(cur, p string, seen map[string]*string) string {
	if filepath.IsAbs(p) {
		cur = "/"
	}
	for _, comp := range strings.Split(p, "/") {
		switch comp {
		case "", ".":
			continue
		case "..":
			cur = filepath.Dir(cur)
			continue
		}
		next := filepath.Join(cur, comp)
		if r, ok := seen[next]; ok {
			if r != nil {
				cur = *r // 解決済み
			} else {
				cur = next // 解決中のリンクに戻ってきた (ループ)。Python の strict でない realpath と同じくリンクのまま進む
			}
			continue
		}
		fi, err := os.Lstat(next)
		if err != nil || fi.Mode()&os.ModeSymlink == 0 {
			cur = next // 通常のファイル・dir か、存在しない (字面のまま進む)
			continue
		}
		target, err := os.Readlink(next)
		if err != nil {
			cur = next
			continue
		}
		seen[next] = nil
		cur = resolveFrom(cur, target, seen) // 相対のリンク先はリンクのある dir から
		resolved := cur
		seen[next] = &resolved
	}
	return cur
}
