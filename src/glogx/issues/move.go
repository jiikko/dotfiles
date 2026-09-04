package issues

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// issue ファイルを状態ディレクトリへ移す層 (viewer の n = 「次にやる」の目印)。
//
// 🚨 git mv を使わない: 実ファイルの移動だけを行い、index には触れない。git mv は移動を即座に
// stage するため、同じ repo で並行して作業しているセッションの pathspec なし commit に巻き込まれる
// (dotfiles の commit-with-pathspec ルールが記録している実害)。viewer は「置き場所を変えるだけ」
// に徹し、commit するかどうかは人が決める。
//
// 🚨 上書きしない: 同じ basename が 2 箇所にあるのは viewer が警告する異常 (spec 3 節の
// 「同じファイル名が 2 箇所にあったら警告する」) で、移動でそれを作らない。宛先があれば失敗させる。

// MoveToSubdir は issue を同じ issue ディレクトリ配下の subdir へ移す (subdir="" は直下へ戻す)。
// 戻り値は移動後の絶対パス。
func MoveToSubdir(iss *Issue, subdir string) (string, error) {
	if iss == nil {
		return "", errors.New("移動対象がない")
	}
	base := filepath.Base(iss.Rel)
	destDir := iss.Dir
	if iss.GroupKind == GroupEpic {
		// group issue は epic の外へ出さない。subdir="" は group 直下へ戻すので、
		// claim の解除 (next -> open) も group 内で完結する。
		if iss.GroupKey == "" {
			return "", errors.New("group issue に GroupKey が無い (Scan を通っていない Issue)")
		}
		destDir = iss.GroupKey
	}
	if subdir != "" {
		destDir = filepath.Join(destDir, subdir)
	}
	dest := filepath.Join(destDir, base)
	if dest == iss.Path {
		return iss.Path, nil // 既にそこに居る (呼び出し側が「変化なし」と数える)
	}
	if _, err := os.Stat(dest); err == nil {
		return "", fmt.Errorf("%s には同名のファイルが既にあります", filepath.Join(subdir, base))
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(iss.Path, dest); err != nil {
		return "", err
	}
	return dest, nil
}
