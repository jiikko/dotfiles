package safefs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// ErrNotEmpty は RemoveEmptyDir の対象に、許した 1 つの空のファイル以外の何かがあるときの失敗。
var ErrNotEmpty = errors.New("空のフォルダではありません")

// RemoveEmptyDir は dir の中のフォルダ name を消す。消してよいのは「空」か「空の普通のファイル keep だけを含む」フォルダだけで、
// keep を消してから rmdir する (keep が空なら keep を見ない)。戻せない操作だが、失うのは空のフォルダと長さ 0 のファイルだけにする:
//   - 中を見るのは、name を O_NOFOLLOW で開いた fd 経由。(dev, ino) が id と違えば断る (パスで再解決すると、確かめた後に差し替えられる)
//   - keep は普通のファイルで、長さ 0・リンク数 1 のときだけ消す (hardlink の片割れは、他の名前から見た中身を持つので消さない)
//   - keep とフォルダ自身の拡張属性 (xattr) は、macOS が付ける由来の印 (com.apple.provenance) だけを許す
//     (xattr は長さ 0 のファイルにも中身を持たせられるので、それ以外は中身とみなす)
//   - 最後の rmdir は kernel が「空でなければ断る」(ENOTEMPTY)。判定の後に何かが書かれても、中身ごと消すことは起きない
//
// 残る窓 (macOS には fd で unlink / rmdir する手段が無く、どちらも名前で解決する):
//   - keep を確かめてから消すまでの間に、keep の名前が中身のあるファイルへ差し替わると、それを消す
//   - 中を確かめてから rmdir するまでの間に、name が別の空のフォルダへ差し替わると、そちらを消す (消えるのは空のフォルダだけ)
func RemoveEmptyDir(dir, name string, id ID, keep string, guard Guard) error {
	if err := ValidName(name); err != nil {
		return err
	}
	if keep != "" {
		if err := ValidName(keep); err != nil {
			return err
		}
	}
	if id == (ID{}) {
		return ErrUnidentified
	}
	pfd, err := openDirNoFollow(dir)
	if err != nil {
		return fmt.Errorf("フォルダを開けません: %w", err)
	}
	defer func() { _ = unix.Close(pfd) }()
	cfd, err := unix.Openat(pfd, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("消すフォルダを開けません: %w", err)
	}
	defer func() { _ = unix.Close(cfd) }()
	var st unix.Stat_t
	if err := unix.Fstat(cfd, &st); err != nil {
		return fmt.Errorf("消すフォルダを確認できません: %w", err)
	}
	if identity(&st) != id {
		return ErrSwapped
	}
	if ok, err := onlyXattrs(cfd, osXattrs); err != nil {
		return fmt.Errorf("消すフォルダの拡張属性を読めません: %w", err)
	} else if !ok {
		return ErrNotEmpty
	}
	names, err := readNames(cfd)
	if err != nil {
		return fmt.Errorf("消すフォルダの中を読めません: %w", err)
	}
	hasKeep := false
	for _, n := range names {
		if keep == "" || n != keep {
			return ErrNotEmpty
		}
		hasKeep = true
	}
	if hasKeep {
		if err := emptyPlainFile(cfd, keep); err != nil {
			return err
		}
		if err := check(guard, "unlink", filepath.Join(dir, name, keep)); err != nil {
			return err
		}
		if err := unix.Unlinkat(cfd, keep, 0); err != nil && !errors.Is(err, unix.ENOENT) {
			return fmt.Errorf("%s を消せません: %w", keep, err)
		}
	}
	if err := check(guard, "rmdir", filepath.Join(dir, name)); err != nil {
		return err
	}
	switch err := unix.Unlinkat(pfd, name, unix.AT_REMOVEDIR); {
	case err == nil:
		return nil
	case errors.Is(err, unix.ENOTEMPTY), errors.Is(err, unix.EEXIST):
		return ErrNotEmpty
	default:
		return err
	}
}

// osXattrs は消すものに付いていてよい拡張属性。macOS がファイルを作ったプロセスの由来を記録する印で、中身ではない
// (2026-10-21 実測: $TMPDIR の TemporaryDirectory.* 300 件のうち 298 件のフォルダに付き、このセッションから作ったファイルにも付く)。
var osXattrs = map[string]bool{"com.apple.provenance": true}

// emptyPlainFile は fd で開いたフォルダの中の name が、長さ 0・リンク数 1・由来の印のほかに拡張属性の無い普通のファイルかを確かめる
// (違えば ErrNotEmpty)。
// O_NOFOLLOW で開いた fd で見るので、symlink の先は見ない。
func emptyPlainFile(dirfd int, name string) error {
	fd, err := unix.Openat(dirfd, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ELOOP) {
		return ErrNotEmpty // symlink
	}
	if err != nil {
		return fmt.Errorf("%s を確認できません: %w", name, err)
	}
	defer func() { _ = unix.Close(fd) }()
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return fmt.Errorf("%s を確認できません: %w", name, err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Size != 0 || st.Nlink != 1 {
		return ErrNotEmpty
	}
	if ok, err := onlyXattrs(fd, osXattrs); err != nil {
		return fmt.Errorf("%s の拡張属性を読めません: %w", name, err)
	} else if !ok {
		return ErrNotEmpty
	}
	return nil
}

// onlyXattrs は fd に付いている拡張属性が全部 allowed に入っているか。
func onlyXattrs(fd int, allowed map[string]bool) (bool, error) {
	sz, err := unix.Flistxattr(fd, nil)
	if err != nil {
		return false, err
	}
	if sz == 0 {
		return true, nil
	}
	buf := make([]byte, sz)
	if sz, err = unix.Flistxattr(fd, buf); err != nil {
		return false, err
	}
	for name := range strings.SplitSeq(strings.TrimRight(string(buf[:sz]), "\x00"), "\x00") {
		if !allowed[name] {
			return false, nil
		}
	}
	return true, nil
}

// readNames は fd で開いたフォルダの中の名前 (`.` と `..` を除く)。fd は複製して読む (os.File の Close が fd を閉じるため。
// 複製は読み位置を共有するが、呼び出し側はこの後 *at 系にしか使わない)。
func readNames(fd int) ([]string, error) {
	dup, err := unix.Dup(fd)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(dup), "dir")
	defer func() { _ = f.Close() }()
	return f.Readdirnames(-1)
}
