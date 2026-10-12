package safefs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// TrashMove は src を trashDir (ゴミ箱。Finder と同じ ~/.Trash) へ移し、移した先のパスを返す。
// dev, ino は呼び出し側が「この実体を移す」と決めた時点の src の同一性 (Lstat の値)。
//
// 3 つのことを同時に守る:
//   - **上書きしない**。renameatx_np(RENAME_EXCL) で「宛先が無いときだけ移す」を原子的に行う。素の os.Rename は宛先を
//     黙って上書きし、以前ゴミ箱に入れた同名を破壊する。名前が埋まっていたら Finder と同じく連番 (`name 2`) を足す
//   - **経路を差し替えられない**。src の親と移動先を fd で掴み、その fd 経由で同一性を確かめてから rename する
//     (パスで再解決すると、検査と実行のあいだに親を差し替えられる)
//   - **半端なコピーを作らない**。別ボリューム (EXDEV) や RENAME_EXCL の効かない FS では移さずに失敗する。
//     再帰コピーは途中で失敗すると復元をかえって難しくする (ゴミ箱へ移す目的は復元の手段を残すこと)
//
// 「戻す」(Finder の Put Back) の情報は付かない。src の親は最後の要素だけを O_NOFOLLOW で見る (親までの経路の symlink は
// 呼び出し側が解決してから渡す)。
func TrashMove(src, trashDir string, dev, ino uint64, guard Guard) (string, error) {
	if trashDir == "" {
		return "", errors.New("ゴミ箱の場所が分かりません")
	}
	// 🚨 **宛先も検査点を通す** (doctor の issue 234 (b))。src だけを見ていたので、登録済みの src から
	// 未登録のディレクトリへの移動が err == nil で成功し、違反記録も 0 件だった
	// (trashDir はテストが自由に渡せるので、1 行間違えると実データの場所へファイルが移る)。
	if err := check(guard, "trash-dest", trashDir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(trashDir, 0o700); err != nil {
		return "", fmt.Errorf("ゴミ箱を用意できません: %w", err)
	}
	// ゴミ箱の**経路のどこか**に symlink があると、移動先は ~/.Trash の外に落ちる
	if err := NoSymlinkInPath(trashDir); err != nil {
		return "", fmt.Errorf("ゴミ箱を使えません: %w", err)
	}
	srcDir, err := openDirNoFollow(filepath.Dir(src))
	if err != nil {
		return "", fmt.Errorf("移動元の親を開けません: %w", err)
	}
	defer func() { _ = unix.Close(srcDir) }()
	dstDir, err := openDirNoFollow(trashDir)
	if err != nil {
		return "", fmt.Errorf("ゴミ箱を開けません: %w", err)
	}
	defer func() { _ = unix.Close(dstDir) }()

	base := filepath.Base(src)
	var st unix.Stat_t
	if err := unix.Fstatat(srcDir, base, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return "", fmt.Errorf("移動元を確認できません: %w", err)
	}
	// 🚨 doctor の production からは到達しない (planItem が先に同じ検査をする) が、意図して残している (doctor の issue 236 の P3-5)。
	// この関数は**破壊的操作の直前の最後の照合**で、外すと下の比較が (0,0) と実体を比べ、「識別できないまま移動する」経路が
	// 開く。呼び出し元が 1 つ増えた瞬間に穴になる形なので、冗長さと引き換えに残す (list-masked-failure-modes-before-removing-guard.md)
	if dev == 0 && ino == 0 {
		return "", ErrUnidentified
	}
	if d, i := identity(&st); d != dev || i != ino {
		return "", ErrSwapped
	}
	if err := check(guard, "trash", src); err != nil {
		return "", err
	}
	for i := 1; i <= 64; i++ {
		name := base
		if i > 1 {
			name = fmt.Sprintf("%s %d", base, i)
		}
		// 🚨 RENAME_EXCL は宛先が非空ディレクトリでも EEXIST を返す (実測 2026-09-03)。ENOTEMPTY は来ないので見ない
		err := renameExcl(srcDir, base, dstDir, name)
		switch {
		case err == nil:
			return filepath.Join(trashDir, name), nil
		case errors.Is(err, unix.EEXIST):
			continue
		case errors.Is(err, unix.EXDEV):
			return "", errors.New("ゴミ箱と別ボリュームのため移動できません (手で移動してください)")
		case errors.Is(err, unix.ENOTSUP), errors.Is(err, unix.EINVAL):
			// RENAME_EXCL の効かない FS では、上書きの危険を冒すより移動しない
			return "", errors.New("この場所ではゴミ箱への安全な移動ができません (手で移動してください)")
		default:
			return "", err
		}
	}
	return "", fmt.Errorf("ゴミ箱に同名が多すぎます: %s", base)
}
