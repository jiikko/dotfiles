// Package atomicfile は「途中の状態を残さない」ファイル書き込みを 1 箇所に置く。
// glogx の状態キャッシュ (cache.go の writeAtomic) と issues の本文書き換え (claim バナー) が共有する。
package atomicfile

import (
	"os"
	"path/filepath"
)

// Write は temp + rename で path を置き換える (最終的な mode は perm)。write / Close / rename のどの分岐で失敗しても temp を掃除する。
//
// temp の名前は `<元のファイル名>.tmp.<乱数>` で、**この中で導出する**。
// 🚨 **pattern を引数にしない** (issue 219 の敵対レビューで実測): 引数にすると
// 「production が作る名前」と「テストが glob する名前」が別々のリテラルになり、
// 呼び出し側の 1 行を書き換えるだけで残骸テストが無言で vacuous になる
// (rename 失敗で temp が実際に残る変異を当てても、スイート全体が緑のまま通った)。
// 出所が読める名前という要求は、導出でも同じだけ満たせる。
//
// 掃除する道具を作るなら、writeAtomic 経由の全経路は **再帰の** `**/*.tmp.*` で当たる
// (CI キャッシュは `<base>/github.com/<owner>/<name>.json` なので top level の glob には
// 当たらない)。`doctor-history` の `.<乱数>.tmp` (src/doctor/disk/delete.go) は
// 命名が別なので、別の glob が要る (parallel-each も別命名だったが、2026-09-08 に
// この repo から出た)。
//
// 🚨 **閉じるのは error-return 経路だけ**。CreateTemp と Remove の間で SIGKILL / panic した
// 残骸はこの実装でも残る (issue 219)。
// ⚠️ **Close 分岐の掃除は変異検証の射程外**: RLIMIT_FSIZE では Close 失敗を作れないため、
// この分岐の os.Remove を外してもどのテストも赤くならない (issue 219 で実測)。
func Write(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
